package render

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// captureOutput swaps os.Stdout and os.Stderr for pipes, runs f, then returns
// what f wrote to each. The render package is tiny and forces direct stdout/
// stderr writes, so the test must hijack the file descriptors.
func captureOutput(t *testing.T, f func()) (string, string) {
	t.Helper()
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, rOut) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, rErr) }()
	f()
	_ = wOut.Close()
	_ = wErr.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldOut, oldErr
	return outBuf.String(), errBuf.String()
}

func TestIsTTY(t *testing.T) {
	// A pipe is never a TTY.
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if isTTY(w) {
		t.Error("isTTY should return false for a pipe")
	}
}

func TestColorHelpersDoNotPanic(t *testing.T) {
	// useColor is false under `go test` since stdout isn't a TTY, so the
	// helpers should pass strings through unchanged.
	for _, f := range []func(string) string{Cyan, Green, Yellow, Red, Bold} {
		if got := f("x"); got != "x" {
			t.Errorf("color helper should be a no-op without TTY, got %q", got)
		}
	}
}

// TestDieDefaultExitCode and TestDieCustomExitCode use the well-known
// subprocess pattern because Die() calls os.Exit, which would terminate the
// test binary itself if invoked directly.
func TestDieDefaultExitCode(t *testing.T) {
	if os.Getenv("KSPEC_TEST_DIE") == "1" {
		Die("boom")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDieDefaultExitCode$")
	cmd.Env = append(os.Environ(), "KSPEC_TEST_DIE=1")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("default Die() should exit 1, got %d", exitErr.ExitCode())
	}
}

func TestDieCustomExitCode(t *testing.T) {
	if os.Getenv("KSPEC_TEST_DIE") == "7" {
		Die("boom", 7)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDieCustomExitCode$")
	cmd.Env = append(os.Environ(), "KSPEC_TEST_DIE=7")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Errorf("Die(..., 7) should exit 7, got %d", exitErr.ExitCode())
	}
}

func TestInfoOKWarnErr(t *testing.T) {
	out, errOut := captureOutput(t, func() {
		Info("hello")
		OK("good")
		Warn("careful")
		Err("bad")
	})
	if !strings.Contains(out, "hello") || !strings.Contains(out, "good") {
		t.Errorf("stdout missing expected substrings: %q", out)
	}
	if !strings.Contains(errOut, "careful") || !strings.Contains(errOut, "bad") {
		t.Errorf("stderr missing expected substrings: %q", errOut)
	}
}
