package plan

import (
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows: there is no fork/setpgid equivalent, and
// bounding a tree properly would need a Job Object. killProcessGroup compensates
// by shelling out to taskkill /T.
func setProcessGroup(*exec.Cmd) {}

// killProcessGroup terminates the child and its descendants. `taskkill /T /F`
// walks the tree, which is the closest Windows equivalent to signalling a process
// group; if it is unavailable, fall back to killing the direct child.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if err := kill.Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
