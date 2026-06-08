package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readAgentMD(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/agents", name+".md"))
	if err != nil {
		t.Fatalf("read agent md: %v", err)
	}
	return string(data)
}

func TestAgentCreateEffort(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "agent", "create", "effort-agent",
		"--description", "d", "--effort", "high", "--root", dir)
	if code != 0 {
		t.Fatalf("agent create with effort failed (code=%d): %s", code, errOut)
	}
	body := readAgentMD(t, dir, "effort-agent")
	if !strings.Contains(body, "effort: high") {
		t.Errorf("agent missing effort line:\n%s", body)
	}
}

func TestAgentCreateModelAndEffort(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "agent", "create", "both-agent",
		"--description", "d", "--model", "sonnet", "--effort", "max", "--root", dir)
	if code != 0 {
		t.Fatalf("agent create with model+effort should succeed, got exit %d", code)
	}
	body := readAgentMD(t, dir, "both-agent")
	for _, want := range []string{"model: sonnet", "effort: max"} {
		if !strings.Contains(body, want) {
			t.Errorf("agent missing %q:\n%s", want, body)
		}
	}
}

func TestAgentCreateNoEffortNoLine(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "agent", "create", "plain-agent", "--description", "d", "--root", dir)
	body := readAgentMD(t, dir, "plain-agent")
	if strings.Contains(body, "effort:") {
		t.Errorf("agent without --effort must not carry an effort line:\n%s", body)
	}
	if strings.Contains(body, "model:") {
		t.Errorf("agent without --model must not carry a model line:\n%s", body)
	}
}

func TestAgentCreateInvalidEffortRejected(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "agent", "create", "bad-agent",
		"--description", "d", "--effort", "HIGH", "--root", dir)
	if code != 1 {
		t.Errorf("invalid --effort should exit 1, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/agents/bad-agent.md")); !os.IsNotExist(err) {
		t.Errorf("invalid --effort must not create the agent file (err=%v)", err)
	}
}
