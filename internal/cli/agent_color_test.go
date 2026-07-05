package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentCreateColor(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "agent", "create", "color-agent",
		"--description", "d", "--color", "purple", "--root", dir)
	if code != 0 {
		t.Fatalf("agent create with color failed (code=%d): %s", code, errOut)
	}
	body := readAgentMD(t, dir, "color-agent")
	if !strings.Contains(body, "color: purple") {
		t.Errorf("agent missing color line:\n%s", body)
	}
}

func TestAgentCreateNoColorNoLine(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "agent", "create", "plain-color-agent", "--description", "d", "--root", dir)
	body := readAgentMD(t, dir, "plain-color-agent")
	if strings.Contains(body, "color:") {
		t.Errorf("agent without --color must not carry a color line:\n%s", body)
	}
}

func TestAgentCreateInvalidColorRejected(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, _ := run(t, "agent", "create", "bad-color-agent",
		"--description", "d", "--color", "teal", "--root", dir)
	if code != 1 {
		t.Errorf("invalid --color should exit 1, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/agents/bad-color-agent.md")); !os.IsNotExist(err) {
		t.Errorf("invalid --color must not create the agent file (err=%v)", err)
	}
}
