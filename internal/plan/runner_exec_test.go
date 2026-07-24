package plan

import (
	"strings"
	"testing"
)

// argAfter returns the token following flag in args, or "" if flag is absent or
// has no successor. It lets the tests assert flag/value pairs positionally.
func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// TestSessionArgsModelEffort: a pinned model/effort become --model/--effort flags,
// so the orchestrating session runs on the tier the run chose (not the ambient
// default). The JSON envelope and bypass flag are always present. The brief is NOT
// in argv — it is fed on stdin (see TestSessionArgsBriefNotInArgv), so -p carries no
// positional here.
func TestSessionArgsModelEffort(t *testing.T) {
	args := sessionArgs(0, "opus", "high")

	if !hasArg(args, "-p") {
		t.Errorf("-p (print mode) must be present: %v", args)
	}
	if got := argAfter(args, claudeFlags.model); got != "opus" {
		t.Errorf("--model should be opus, got %q", got)
	}
	if got := argAfter(args, claudeFlags.effort); got != "high" {
		t.Errorf("--effort should be high, got %q", got)
	}
	if !hasArg(args, claudeFlags.bypass) {
		t.Errorf("every session must run bypass-mode: %v", args)
	}
	if hasArg(args, claudeFlags.maxBudget) {
		t.Errorf("a zero budget must omit --max-budget-usd: %v", args)
	}
}

// TestSessionArgsInheritWhenEmpty: an empty model/effort omits its flag so the
// session inherits the ambient default rather than being pinned to an empty value.
func TestSessionArgsInheritWhenEmpty(t *testing.T) {
	args := sessionArgs(0, "", "")
	if hasArg(args, claudeFlags.model) {
		t.Errorf("empty model must omit --model: %v", args)
	}
	if hasArg(args, claudeFlags.effort) {
		t.Errorf("empty effort must omit --effort: %v", args)
	}
	// Whitespace-only is treated as empty.
	if a := sessionArgs(0, "  ", "\t"); hasArg(a, claudeFlags.model) || hasArg(a, claudeFlags.effort) {
		t.Errorf("whitespace-only model/effort must omit their flags: %v", a)
	}
}

// TestSessionArgsBudget: a positive budget pins --max-budget-usd formatted to cents.
func TestSessionArgsBudget(t *testing.T) {
	args := sessionArgs(2.5, "sonnet", "medium")
	if got := argAfter(args, claudeFlags.maxBudget); got != "2.50" {
		t.Errorf("--max-budget-usd should be 2.50, got %q", got)
	}
}

// TestSessionArgsBriefNotInArgv pins the fix for the Windows 32,767-char command-line
// limit: the brief must never appear in the argument vector, no matter how large,
// because it is fed to the child on stdin. A regression that put it back in argv
// would make large-feat sessions fail to spawn on Windows (CreateProcess), which is
// exactly the failure this guards.
func TestSessionArgsBriefNotInArgv(t *testing.T) {
	big := strings.Repeat("x", 40_000) // larger than the Windows command-line cap
	args := sessionArgs(0, "opus", "high")
	for _, a := range args {
		if strings.Contains(a, big) || len(a) > 4096 {
			t.Fatalf("brief-sized content must not appear in argv; got an arg of len %d", len(a))
		}
	}
	_ = big
}

// TestSessionModelLabel renders the run-header label from whatever was pinned,
// falling back to "session default" when neither model nor effort is set.
func TestSessionModelLabel(t *testing.T) {
	cases := []struct{ model, effort, want string }{
		{"opus", "high", "orchestrator: opus/high"},
		{"sonnet", "", "orchestrator: sonnet"},
		{"", "xhigh", "orchestrator: effort xhigh"},
		{"", "", "orchestrator: session default"},
		{"  ", "  ", "orchestrator: session default"},
	}
	for _, c := range cases {
		if got := sessionModelLabel(c.model, c.effort); got != c.want {
			t.Errorf("sessionModelLabel(%q,%q) = %q, want %q", c.model, c.effort, got, c.want)
		}
	}
}
