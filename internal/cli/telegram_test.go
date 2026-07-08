package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/telegram"
)

func TestTelegramInitWritesConfigAndGitignore(t *testing.T) {
	root := t.TempDir()
	code := telegramInit([]string{"--token", "42:secret", "--chat-id", "987654321", "--interval", "3", "--no-test", "--root", root})
	if code != 0 {
		t.Fatalf("telegramInit exit = %d, want 0", code)
	}

	cfg, err := telegram.Load(root)
	if err != nil {
		t.Fatalf("Load after init: %v", err)
	}
	if cfg.Token != "42:secret" || cfg.ChatID != "987654321" || cfg.IntervalSeconds != 3 {
		t.Fatalf("config not persisted as expected: %+v", cfg)
	}

	gi, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), "/.csdd/bot.json") {
		t.Fatalf("bot.json (secret token) must be gitignored; .gitignore:\n%s", gi)
	}
}

func TestTelegramInitRejectsMissingCredentials(t *testing.T) {
	// Non-interactive, no flags/env → cannot resolve token/chat → refuse.
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	root := t.TempDir()
	if code := telegramInit([]string{"--no-test", "--root", root}); code != 1 {
		t.Fatalf("expected exit 1 with no credentials, got %d", code)
	}
	if _, err := os.Stat(telegram.ConfigPath(root)); !os.IsNotExist(err) {
		t.Fatal("no config file should be written when credentials are missing")
	}
}

func TestTelegramRunWithoutConfigFails(t *testing.T) {
	if code := telegramRun([]string{"--root", t.TempDir()}); code != 1 {
		t.Fatalf("expected exit 1 when config is absent, got %d", code)
	}
}

func TestRunTelegramDispatch(t *testing.T) {
	if code := runTelegram(nil); code != 1 {
		t.Fatalf("no action should be a usage error (1), got %d", code)
	}
	if code := runTelegram([]string{"bogus"}); code != 1 {
		t.Fatalf("unknown action should be an error (1), got %d", code)
	}
	if code := runTelegram([]string{"--help"}); code != 0 {
		t.Fatalf("--help should exit 0, got %d", code)
	}
}
