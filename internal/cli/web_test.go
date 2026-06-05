package cli

import (
	"strings"
	"testing"
)

func TestRunWebFlagParsing(t *testing.T) {
	// Each of these must fail before the server binds, so the test never blocks.
	if code := runWeb([]string{"--port", "not-a-number"}); code != 1 {
		t.Errorf("bad --port: code = %d, want 1", code)
	}
	if code := runWeb([]string{"--root", "/no/such/path/xyzzy"}); code != 1 {
		t.Errorf("bad --root: code = %d, want 1", code)
	}
	if code := runWeb([]string{"-h"}); code != 0 {
		t.Errorf("help: code = %d, want 0", code)
	}
}

func TestHelpMentionsWeb(t *testing.T) {
	if !strings.Contains(helpText(), "web") {
		t.Error("help text should mention the web command")
	}
}
