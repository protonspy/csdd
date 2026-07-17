package plan

import "testing"

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
// default). The brief, JSON envelope, and bypass flag are always present.
func TestSessionArgsModelEffort(t *testing.T) {
	args := sessionArgs("BRIEF", 0, "opus", "high")

	if got := argAfter(args, "-p"); got != "BRIEF" {
		t.Errorf("brief should follow -p, got %q", got)
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
	args := sessionArgs("BRIEF", 0, "", "")
	if hasArg(args, claudeFlags.model) {
		t.Errorf("empty model must omit --model: %v", args)
	}
	if hasArg(args, claudeFlags.effort) {
		t.Errorf("empty effort must omit --effort: %v", args)
	}
	// Whitespace-only is treated as empty.
	if a := sessionArgs("BRIEF", 0, "  ", "\t"); hasArg(a, claudeFlags.model) || hasArg(a, claudeFlags.effort) {
		t.Errorf("whitespace-only model/effort must omit their flags: %v", a)
	}
}

// TestSessionArgsBudget: a positive budget pins --max-budget-usd formatted to cents.
func TestSessionArgsBudget(t *testing.T) {
	args := sessionArgs("BRIEF", 2.5, "sonnet", "medium")
	if got := argAfter(args, claudeFlags.maxBudget); got != "2.50" {
		t.Errorf("--max-budget-usd should be 2.50, got %q", got)
	}
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
