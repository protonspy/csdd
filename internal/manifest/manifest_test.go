package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	m, exists, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing manifest should not error: %v", err)
	}
	if exists {
		t.Error("exists should be false for a missing manifest")
	}
	if m == nil || m.Files == nil {
		t.Fatal("Load must return a usable empty manifest")
	}
	if len(m.Files) != 0 {
		t.Errorf("empty manifest should have no files, got %d", len(m.Files))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".csdd-manifest.json")
	m := New()
	m.Files[".claude/rules/ears-format.md"] = Hash("body")
	when := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	if err := m.Save(path, "v1.2.3", when); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, exists, err := Load(path)
	if err != nil || !exists {
		t.Fatalf("load after save: exists=%v err=%v", exists, err)
	}
	if got.CsddVersion != "v1.2.3" {
		t.Errorf("version not persisted: %q", got.CsddVersion)
	}
	if got.UpdatedAt != "2026-06-03T10:00:00Z" {
		t.Errorf("timestamp not persisted: %q", got.UpdatedAt)
	}
	if got.Files[".claude/rules/ears-format.md"] != Hash("body") {
		t.Errorf("file hash not persisted: %v", got.Files)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := Load(path); err == nil || !exists {
		t.Errorf("malformed manifest should error with exists=true; got exists=%v err=%v", exists, err)
	}
}

func TestHashStableAndDistinct(t *testing.T) {
	if Hash("a") != Hash("a") {
		t.Error("hash must be stable for identical content")
	}
	if Hash("a") == Hash("b") {
		t.Error("hash must differ for different content")
	}
	if got := Hash("a"); got[:7] != "sha256:" {
		t.Errorf("hash should carry the sha256: prefix, got %q", got)
	}
}
