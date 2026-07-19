package plan

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// cpuTicks returns the total CPU time (user+system, in clock ticks) consumed by
// every live process in the child's process group.
//
// This is the signal that lets a silent session survive: while the session shells
// out to a long build or test run it emits nothing at all, but the work burns CPU.
// A process parked on a read that will never return — a prompt on /dev/tty, a dead
// socket — burns none. Comparing this total between polls distinguishes the two,
// which elapsed time alone cannot.
//
// One scan of /proc costs a few hundred small reads and only runs after output has
// already gone quiet, so it is off the hot path. Any unreadable entry is skipped:
// processes exit mid-scan routinely, and a short count is not a hang signal — the
// watchdog only acts when the total is perfectly flat across a whole idle window.
func cpuTicks(cmd *exec.Cmd) uint64 {
	if cmd.Process == nil {
		return 0
	}
	pgid := cmd.Process.Pid // with Setpgid, the group id is the child's pid

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var total uint64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a pid directory
		}
		buf, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // exited between ReadDir and here
		}
		if pg, cpu, ok := parseProcStat(string(buf)); ok && pg == pgid {
			total += cpu
		}
	}
	return total
}

// parseProcStat pulls the process group id and utime+stime out of a /proc/<pid>/stat
// line. The comm field (field 2) is parenthesized and may itself contain spaces and
// parens, so fields are counted from the LAST ')' rather than by splitting the whole
// line — the standard way to read this file safely.
func parseProcStat(stat string) (pgid int, cpu uint64, ok bool) {
	close := strings.LastIndexByte(stat, ')')
	if close < 0 {
		return 0, 0, false
	}
	// After ')' the fields are 3..N, so field n sits at index n-3.
	f := strings.Fields(stat[close+1:])
	const (
		idxPgrp  = 5 - 3
		idxUtime = 14 - 3
		idxStime = 15 - 3
	)
	if len(f) <= idxStime {
		return 0, 0, false
	}
	pg, err := strconv.Atoi(f[idxPgrp])
	if err != nil {
		return 0, 0, false
	}
	utime, err1 := strconv.ParseUint(f[idxUtime], 10, 64)
	stime, err2 := strconv.ParseUint(f[idxStime], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return pg, utime + stime, true
}
