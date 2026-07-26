package plan

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// The progress watchdog supervises the `claude` session the runner spawns.
//
// It measures progress, not elapsed time. A session that spends forty minutes on
// one feat is healthy as long as it keeps working; a session parked forever on
// something that will never answer does nothing at all. A wall-clock cap cannot
// tell those apart — one generous enough for the first never fires, and one tight
// enough for the second kills real work mid-feat. That is why `plan run` had no
// timeout at all: the only honest bound is a progress bound.
//
// A child is making progress when either holds:
//
//   - it produced a byte on stdout or stderr — with --output-format stream-json
//     that is one NDJSON event per assistant message and per tool result;
//   - CPU time advanced anywhere in its process group. This covers the session's
//     silent stretches: a long `go test` the session shells out to prints nothing
//     for minutes while burning CPU, whereas a command parked on /dev/tty burns
//     none.
//
// Only when both stay flat for the idle limit is the child declared hung and its
// whole process group killed. Platforms with no CPU probe (see cpuTicks) degrade
// to output-only, still strictly better than the unbounded wait it replaces.
//
// A kill surfaces as an ordinary session error, so the loop's existing
// failure/stall accounting handles it: one hung session never strands the run.

// defaultSessionIdle is deliberately generous — the watchdog exists to end
// indefinite hangs, not to police slowness. Overridable per run (--session-idle).
const defaultSessionIdle = 15 * time.Minute

const (
	// pollInterval is how often progress is sampled: one atomic load, plus a /proc
	// scan only once output has actually gone quiet.
	pollInterval = 5 * time.Second
	// killGrace is how long a killed group gets to exit on SIGTERM before SIGKILL.
	killGrace = 5 * time.Second
	// maxCapture bounds retained child output so a runaway session cannot exhaust
	// memory. The stream is consumed incrementally; this is only the copy kept for
	// limit detection and error messages.
	maxCapture = 4 << 20
	// maxLine bounds the in-flight line buffer used to split stdout into lines.
	maxLine = 8 << 20
)

// noProgressError reports a child killed by the watchdog. The runner folds it into
// the feat's rolling history like any other session error, so a hang costs one
// iteration rather than the whole run.
type noProgressError struct {
	idle time.Duration
	last string // the last activity observed, for a actionable message
}

func (e *noProgressError) Error() string {
	where := ""
	if e.last != "" {
		where = fmt.Sprintf(" (last activity: %s)", e.last)
	}
	return fmt.Sprintf("the claude session made no progress for %s%s — no output and no CPU in its process group, "+
		"so it was killed as hung; raise --session-idle if this was real work",
		e.idle.Round(time.Second), where)
}

// supervised is one watchdog-supervised subprocess.
type supervised struct {
	cmd  *exec.Cmd
	idle time.Duration
	// onLine, if set, receives each stdout line as it arrives, letting the caller
	// consume the NDJSON stream incrementally instead of waiting for exit.
	onLine func(string)
	// lastActivity, if set, names what the child was last seen doing; it is read
	// only when reporting a hang.
	lastActivity func() string
	// poll overrides how often progress is sampled. Zero means pollInterval; tests
	// set it small so a hang is detected in milliseconds rather than seconds.
	poll time.Duration
}

// run starts the child, streams both pipes, and waits for it under the watchdog.
// It returns the captured stdout and stderr and the child's error — or a
// *noProgressError when the watchdog killed it.
func (s supervised) run() (stdout, stderr string, err error) {
	if s.idle <= 0 {
		s.idle = defaultSessionIdle
	}
	// The three failures below all mean the same thing: the child never ran. They
	// are wrapped as *SpawnError so the runner can tell "the session failed" from
	// "there was no session", which is the difference between charging the feat an
	// attempt and not (R1.1). Sniffing the error string would be fragile; the exec
	// boundary itself is the honest signal.
	outPipe, err := s.cmd.StdoutPipe()
	if err != nil {
		return "", "", &SpawnError{Err: err}
	}
	errPipe, err := s.cmd.StderrPipe()
	if err != nil {
		return "", "", &SpawnError{Err: err}
	}
	// Put the child in its own process group: what actually hangs is often a
	// grandchild (a shell the session spawned waiting on credentials, a stuck MCP
	// server), and killing only `claude` would leave it holding the pipe open.
	setProcessGroup(s.cmd)
	if err := s.cmd.Start(); err != nil {
		return "", "", &SpawnError{Err: err}
	}

	var (
		ticks   atomic.Uint64 // bytes read from either stream; any byte is progress
		hung    atomic.Bool
		outSink = &capture{}
		errSink = &capture{}
		pumps   sync.WaitGroup
		stopped = make(chan struct{})
	)

	pumps.Add(2)
	go func() { defer pumps.Done(); pump(outPipe, &ticks, outSink, s.onLine) }()
	go func() { defer pumps.Done(); pump(errPipe, &ticks, errSink, nil) }()

	watchDone := make(chan struct{})
	go func() { defer close(watchDone); s.watch(&ticks, &hung, stopped) }()

	// Both pipes reach EOF when the child exits, including when the watchdog kills it.
	pumps.Wait()
	waitErr := s.cmd.Wait()
	close(stopped)
	<-watchDone

	if hung.Load() {
		last := ""
		if s.lastActivity != nil {
			last = s.lastActivity()
		}
		return outSink.String(), errSink.String(), &noProgressError{idle: s.idle, last: last}
	}
	return outSink.String(), errSink.String(), waitErr
}

// watch samples progress until the child exits or goes idle past the limit.
func (s supervised) watch(ticks *atomic.Uint64, hung *atomic.Bool, stopped <-chan struct{}) {
	poll := s.poll
	if poll <= 0 {
		poll = pollInterval
	}
	t := time.NewTicker(poll)
	defer t.Stop()

	lastOut := ticks.Load()
	lastCPU := cpuTicks(s.cmd)
	var quiet time.Duration

	for {
		select {
		case <-stopped:
			return
		case <-t.C:
			// Output first: it is a free atomic load and the common case. The /proc
			// scan only runs once the child has actually gone quiet.
			if out := ticks.Load(); out != lastOut {
				lastOut, quiet = out, 0
				lastCPU = cpuTicks(s.cmd)
				continue
			}
			if cpu := cpuTicks(s.cmd); cpu != lastCPU {
				lastCPU, quiet = cpu, 0
				continue
			}
			if quiet += poll; quiet >= s.idle {
				// Record the verdict before killing, so run() cannot observe the
				// child's death without also observing why it died.
				hung.Store(true)
				killProcessGroup(s.cmd)
				return
			}
		}
	}
}

// pump drains r, counting every byte as progress, capturing it, and handing
// complete lines to onLine as they arrive.
func pump(r io.Reader, ticks *atomic.Uint64, sink *capture, onLine func(string)) {
	buf := make([]byte, 32*1024)
	var partial []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			ticks.Add(uint64(n))
			sink.Write(buf[:n])
			if onLine != nil {
				partial = splitLines(append(partial, buf[:n]...), onLine)
			}
		}
		if err != nil {
			if onLine != nil && len(partial) > 0 {
				onLine(string(partial))
			}
			return
		}
	}
}

// splitLines emits every complete line in b to onLine and returns the remainder.
// A remainder past maxLine is dropped rather than grown without bound.
func splitLines(b []byte, onLine func(string)) []byte {
	for {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break
		}
		onLine(string(bytes.TrimSuffix(b[:i], []byte("\r"))))
		b = b[i+1:]
	}
	if len(b) > maxLine {
		return nil
	}
	// Copy: b aliases the caller's scratch buffer, reused on the next read.
	return append([]byte(nil), b...)
}

// capture is a bounded, concurrency-safe sink for child output.
type capture struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (c *capture) Write(p []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := maxCapture - c.buf.Len()
	if room <= 0 {
		c.truncated = true
		return
	}
	if len(p) > room {
		p, c.truncated = p[:room], true
	}
	c.buf.Write(p)
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.truncated {
		return c.buf.String() + "\n[output truncated]"
	}
	return c.buf.String()
}
