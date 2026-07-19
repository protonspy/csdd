package plan

import "testing"

// parseProcStat is the only fiddly part of the CPU probe: the comm field is
// parenthesized and may itself contain spaces and parens, so fields cannot be
// found by splitting the line. Getting this wrong would silently misread the
// process group and make the watchdog blind to real CPU activity.
//
// This test lives in a _linux file because parseProcStat does: /proc is the only
// place it applies, and a shared test file would fail to build everywhere else.
func TestParseProcStatHandlesCommWithSpacesAndParens(t *testing.T) {
	if got, _, ok := parseProcStat("42 (weird )( name) S 1 7 7 0 -1 0 0 0 0 0 11 22 0 0"); !ok || got != 7 {
		t.Errorf("want pgid 7 parsed past a nasty comm, got %d (ok=%v)", got, ok)
	}
	if _, cpu, ok := parseProcStat("42 (sh) S 1 7 7 0 -1 0 0 0 0 0 11 22 0 0"); !ok || cpu != 33 {
		t.Errorf("want utime+stime = 33, got %d (ok=%v)", cpu, ok)
	}
	if _, _, ok := parseProcStat("garbage"); ok {
		t.Error("a malformed stat line must not parse")
	}
}
