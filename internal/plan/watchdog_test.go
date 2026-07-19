package plan

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shell runs a /bin/sh script under the watchdog with test-speed tuning.
func shell(t *testing.T, script string, idle time.Duration) (stdout, stderr string, err error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("watchdog process-behavior tests assume a POSIX shell")
	}
	return supervised{
		cmd:  exec.Command("sh", "-c", script),
		idle: idle,
		poll: 20 * time.Millisecond,
	}.run()
}

// A child that produces no output and burns no CPU is exactly the hang `plan run`
// used to wait on forever. It must be killed and reported, not waited on.
func TestWatchdogKillsSilentIdleChild(t *testing.T) {
	start := time.Now()
	_, _, err := shell(t, "sleep 60", 100*time.Millisecond)

	var np *noProgressError
	if !errors.As(err, &np) {
		t.Fatalf("want *noProgressError, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("watchdog took %s to fire; it should act on the idle budget", elapsed)
	}
	if !strings.Contains(np.Error(), "--session-idle") {
		t.Errorf("hang message should point at the tunable, got: %v", np)
	}
}

// The discriminator that makes the watchdog safe: a child that prints nothing for
// the whole window but is genuinely working must SURVIVE. Without the CPU probe
// this is indistinguishable from the hang above, and a long silent build inside a
// session would be killed mid-flight.
func TestWatchdogSparesSilentButBusyChild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("CPU probing needs /proc; other platforms degrade to output-only by design")
	}
	// Busy-loop in the shell: no output at all, plenty of CPU.
	_, _, err := shell(t, "i=0; while [ $i -lt 400000 ]; do i=$((i+1)); done", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("a silent but CPU-burning child must not be killed, got: %v", err)
	}
}

// A child that keeps talking is making progress by the output signal alone, which
// is the path every non-Linux platform relies on.
func TestWatchdogSparesChattyChild(t *testing.T) {
	_, _, err := shell(t, "for i in 1 2 3 4 5 6; do echo tick; sleep 0.05; done", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("a child producing output must not be killed, got: %v", err)
	}
}

// Killing must reach the whole tree. The thing that actually hangs is usually a
// grandchild — a shell the session spawned waiting on input — and killing only the
// direct child would leave it alive holding the pipe, so run() would never return.
func TestWatchdogKillsGrandchildren(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("verifying the grandchild died reads /proc")
	}
	marker := filepath.Join(t.TempDir(), "grandchild.pid")
	// The shell spawns a background sleeper, records its pid, then blocks forever.
	script := "sh -c 'sleep 60' & echo $! > " + marker + "; sleep 60"

	_, _, err := shell(t, script, 100*time.Millisecond)
	var np *noProgressError
	if !errors.As(err, &np) {
		t.Fatalf("want the parent killed as hung, got %v", err)
	}

	raw, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Skipf("grandchild pid was never recorded: %v", readErr)
	}
	pid := strings.TrimSpace(string(raw))
	// SIGKILL is delivered after killGrace; give the group time to actually die.
	deadline := time.Now().Add(killGrace + 5*time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/proc/" + pid); os.IsNotExist(err) {
			return // the grandchild went down with the group
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("grandchild %s survived the group kill", pid)
}

// Output must still reach the caller; the limit-notice scan and error messages
// both read it.
func TestSupervisedCapturesBothStreams(t *testing.T) {
	stdout, stderr, err := shell(t, "echo to-out; echo to-err 1>&2", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "to-out") {
		t.Errorf("stdout not captured: %q", stdout)
	}
	if !strings.Contains(stderr, "to-err") {
		t.Errorf("stderr not captured: %q", stderr)
	}
}

// A failing child reports its exit error, not a hang.
func TestSupervisedReportsExitError(t *testing.T) {
	_, _, err := shell(t, "exit 3", time.Minute)
	if err == nil {
		t.Fatal("want an exit error")
	}
	var np *noProgressError
	if errors.As(err, &np) {
		t.Fatalf("a clean non-zero exit is not a hang: %v", err)
	}
}

func TestSplitLinesEmitsCompleteLinesAndKeepsRemainder(t *testing.T) {
	var got []string
	rest := splitLines([]byte("alpha\r\nbeta\npartial"), func(l string) { got = append(got, l) })
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("want [alpha beta], got %q", got)
	}
	if string(rest) != "partial" {
		t.Errorf("want the trailing fragment held back, got %q", rest)
	}
}

func TestCaptureIsBounded(t *testing.T) {
	var c capture
	c.Write(make([]byte, maxCapture+4096))
	c.Write([]byte("more"))
	if got := len(c.String()); got > maxCapture+64 {
		t.Fatalf("capture grew past its bound: %d bytes", got)
	}
	if !strings.Contains(c.String(), "[output truncated]") {
		t.Error("truncation should be visible in the captured text")
	}
}
