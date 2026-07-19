//go:build !linux

package plan

import "os/exec"

// cpuTicks has no portable implementation outside Linux's /proc, so the watchdog
// degrades to its output signal alone: a child that prints nothing for the whole
// idle window is treated as hung, even if it was quietly burning CPU.
//
// That is a weaker test — a genuinely silent, long-running command can be killed
// where Linux would have spared it — but it still bounds a hang that would
// otherwise last forever, and --session-idle tunes it. Returning a constant makes
// the CPU comparison in watch() always report "unchanged", so output alone decides.
func cpuTicks(*exec.Cmd) uint64 { return 0 }
