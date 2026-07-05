package cli

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestResolvePinggyTokenPersistsAndGitignores(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CSDD_PINGGY_TOKEN", "")

	// 1) Explicit token is returned, saved (0600), and gitignored.
	if got := resolvePinggyToken(dir, "TOK123"); got != "TOK123" {
		t.Fatalf("explicit token = %q, want TOK123", got)
	}
	saved, err := os.ReadFile(filepath.Join(dir, ".pinggy-token"))
	if err != nil {
		t.Fatalf("token file not written: %v", err)
	}
	if strings.TrimSpace(string(saved)) != "TOK123" {
		t.Errorf("saved token = %q", saved)
	}
	if fi, _ := os.Stat(filepath.Join(dir, ".pinggy-token")); fi != nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("token file perms = %v, want 0600", fi.Mode().Perm())
	}
	if gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); !strings.Contains(string(gi), ".pinggy-token") {
		t.Errorf(".gitignore should list .pinggy-token, got:\n%s", gi)
	}

	// 2) No explicit token → reads back the saved file.
	if got := resolvePinggyToken(dir, ""); got != "TOK123" {
		t.Errorf("saved token reread = %q, want TOK123", got)
	}

	// 3) Env beats the saved file when no explicit token is given.
	t.Setenv("CSDD_PINGGY_TOKEN", "ENVTOK")
	if got := resolvePinggyToken(dir, ""); got != "ENVTOK" {
		t.Errorf("env token = %q, want ENVTOK", got)
	}

	// 4) Nothing anywhere → empty (free tier).
	empty := t.TempDir()
	t.Setenv("CSDD_PINGGY_TOKEN", "")
	if got := resolvePinggyToken(empty, ""); got != "" {
		t.Errorf("no token anywhere = %q, want empty", got)
	}
}
