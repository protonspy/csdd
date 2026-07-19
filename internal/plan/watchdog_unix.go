//go:build !windows

package plan

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup puts the child in its own process group so the watchdog can
// signal the whole tree. Without it, killing `claude` would leave whatever it
// spawned — the shell waiting on credentials, a stuck MCP server — alive and
// holding the output pipe open, so run() would still never return.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup terminates the child's whole process group: SIGTERM first so a
// well-behaved tree can clean up, then SIGKILL after a grace period for whatever
// ignored it. A negative pid addresses the group; with Setpgid the group id equals
// the child's pid. ESRCH (already gone) is expected and ignored.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.AfterFunc(killGrace, func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
}
