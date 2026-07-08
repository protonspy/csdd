package telegram

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/tmp/proj")
	want := filepath.Join("/tmp/proj", ".csdd", "bot.json")
	if got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	in := Config{Token: "123:abc", ChatID: "987654321", IntervalSeconds: 7}
	if err := Save(root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestSaveUsesRestrictivePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes are not meaningful on windows")
	}
	root := t.TempDir()
	if err := Save(root, Config{Token: "t", ChatID: "c"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(ConfigPath(root))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 600 (config holds a secret token)", fi.Mode().Perm())
	}
}

func TestLoadMissingIsActionable(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a missing config")
	}
	if !strings.Contains(err.Error(), "telegram init") {
		t.Fatalf("error should point at `telegram init`, got: %v", err)
	}
}

func TestValidate(t *testing.T) {
	if err := (Config{Token: "t", ChatID: "c"}).Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := (Config{ChatID: "c"}).Validate(); err == nil {
		t.Fatal("empty token should be rejected")
	}
	if err := (Config{Token: "t", ChatID: "  "}).Validate(); err == nil {
		t.Fatal("blank chat id should be rejected")
	}
}
