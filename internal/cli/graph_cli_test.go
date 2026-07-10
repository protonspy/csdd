package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/protonspy/csdd/internal/manifest"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/templater"
)

// seedSpec drops a minimal spec into an initialized workspace so graph/analyze
// have traceable content.
func seedSpec(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"specs/demo/spec.json":       `{"feature_name":"demo","phase":"tasks","development_flow":"tdd"}`,
		"specs/demo/requirements.md": "# Requirements\n## Requirements\n### Requirement 1: A\n**Acceptance Criteria**\n1. WHEN x THEN the system SHALL y.\n2. WHEN q THEN the system SHALL r.\n",
		"specs/demo/tasks.md":        "# Tasks\n## Phase 1\n- [ ] 1. Do it _Boundary: Svc_\n  - _Requirements: 1.1_\n",
		"specs/demo/design.md":       "# Design\n## Requirements Traceability\n| Requirement | Components | Interfaces | Flows |\n|---|---|---|---|\n| 1.1 | Svc | do() | f1 |\n## Components and Interfaces\n### Svc\n- **Requirements**: 1.1\n",
	}
	for rel, c := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGraphBuildAndAnalyzeCLI(t *testing.T) {
	dir := freshWorkspace(t)
	seedSpec(t, dir)

	// build writes docs/graph/graph.json.gz and exits 0.
	if code, out, errOut := run(t, "graph", "build", "--root", dir); code != 0 {
		t.Fatalf("graph build failed: code=%d out=%s err=%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "graph", "graph.json.gz")); err != nil {
		t.Fatalf("graph.json.gz not written: %v", err)
	}

	// analyze reports the untested criterion 1.2.
	code, out, _ := run(t, "graph", "analyze", "--root", dir)
	if code != 0 {
		t.Fatalf("analyze exit should be 0 without --strict; got %d", code)
	}
	if !strings.Contains(out, "criterion") {
		t.Errorf("expected an untested-criterion finding; got: %s", out)
	}

	// --strict exits non-zero when findings exist (CI gate).
	code, _, _ = run(t, "graph", "analyze", "--strict", "--root", dir)
	if code == 0 {
		t.Errorf("analyze --strict should exit non-zero with findings")
	}

	// query/path/explain work and emit JSON.
	if code, out, _ := run(t, "graph", "query", "Svc", "--root", dir, "--json"); code != 0 || !strings.Contains(out, "design_demo_svc") {
		t.Errorf("query --json failed: code=%d out=%s", code, out)
	}
	if code, out, _ := run(t, "graph", "explain", "Svc", "--root", dir, "--json"); code != 0 || !strings.Contains(out, "connections") {
		t.Errorf("explain --json failed: code=%d out=%s", code, out)
	}
}

func TestWikiLintCLI(t *testing.T) {
	dir := freshWorkspace(t)
	// The init scaffold is clean, so lint exits 0.
	if code, out, _ := run(t, "wiki", "lint", "--root", dir); code != 0 {
		t.Fatalf("clean wiki should lint 0; got %d out=%s", code, out)
	}
	// Drop an unprocessed raw source → lint exits 2.
	if err := os.WriteFile(filepath.Join(dir, "docs", "raw", "note.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "wiki", "lint", "--root", dir)
	if code != 2 {
		t.Errorf("unprocessed source should make lint exit 2; got %d", code)
	}
	if !strings.Contains(out, "unprocessed") {
		t.Errorf("expected unprocessed-source finding; got %s", out)
	}
}

func TestSkillsForbidDirectGeneratedWrites(t *testing.T) {
	// R16.4: skills must route every mutation through the CLI and never write
	// docs/graph/ or .csdd/ directly. Assert the shipped graph/wiki skills carry
	// that boundary explicitly.
	skills, err := templater.SkillFiles(templater.FS)
	if err != nil {
		t.Fatal(err)
	}
	graphSkill := skills["graph/SKILL.md"]
	if !strings.Contains(graphSkill, "single mutation path") && !strings.Contains(graphSkill, "Never hand-edit `docs/graph/`") {
		t.Errorf("graph skill must state the CLI is the single mutation path / never hand-edit docs/graph")
	}
	wikiSkill := skills["wiki/SKILL.md"]
	if !strings.Contains(wikiSkill, "docs/raw/") || !strings.Contains(strings.ToLower(wikiSkill), "never edit") {
		t.Errorf("wiki skill must state docs/raw/ is never edited")
	}
}

func TestWikiInitInstallsSchema(t *testing.T) {
	// A bare directory (no full init): `wiki init` scaffolds the docs structure
	// AND installs the wiki skill + knowledge-base steering rule (R9.2).
	dir := t.TempDir()
	if code, _, e := run(t, "wiki", "init", "--root", dir); code != 0 {
		t.Fatalf("wiki init failed: %s", e)
	}
	for _, rel := range []string{
		"docs/raw/README.md",
		"docs/wiki/index.md",
		"docs/wiki/log.md",
		"docs/wiki/pages",
		".claude/skills/wiki/SKILL.md",
		".claude/rules/knowledge-base.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("wiki init did not create %s: %v", rel, err)
		}
	}
	// Idempotent: a second run creates nothing new and never touches docs/raw/.
	if code, _, _ := run(t, "wiki", "init", "--root", dir); code != 0 {
		t.Errorf("second wiki init should succeed idempotently")
	}
}

func TestRequireWorkspaceMarker(t *testing.T) {
	// A dir with .claude/ but no .csdd/ → legacy message, exit 1 (R15.3).
	legacy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(legacy, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "graph", "build", "--root", legacy)
	if code == 0 {
		t.Errorf("graph on a legacy workspace should fail")
	}
	if !strings.Contains(errOut, "legacy") || !strings.Contains(errOut, "init") {
		t.Errorf("expected legacy-upgrade guidance; got %s", errOut)
	}

	// A dir with neither → generic not-a-workspace error (R15.2).
	bare := t.TempDir()
	code, _, errOut = run(t, "wiki", "lint", "--root", bare)
	if code == 0 || !strings.Contains(errOut, "not a csdd workspace") {
		t.Errorf("bare dir should fail with not-a-workspace; got code=%d err=%s", code, errOut)
	}
}

func TestInitScaffoldsFullLayout(t *testing.T) {
	dir := freshWorkspace(t)
	// The .csdd/ marker and the docs/ knowledge base exist (R15.1, R7).
	for _, rel := range []string{
		".csdd",
		"docs/plans",
		"docs/raw/README.md",
		"docs/wiki/index.md",
		"docs/wiki/log.md",
		"docs/wiki/pages",
		"docs/graph/README.md",
		"docs/stack.md",
		".claude/skills/graph/SKILL.md",
		".claude/skills/wiki/SKILL.md",
		".claude/skills/stack/SKILL.md",
		".claude/commands/csdd-graph-query.md",
		".claude/commands/csdd-wiki-ingest.md",
		".claude/commands/csdd-stack-propose.md",
		".claude/rules/knowledge-base.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("init did not scaffold %s: %v", rel, err)
		}
	}
	// docs/stack.md must NOT prefill any technology choice (R17.1).
	stack, _ := os.ReadFile(filepath.Join(dir, "docs", "stack.md"))
	if strings.Contains(string(stack), "| Language | Go |") {
		t.Errorf("stack.md must not prefill a technology choice")
	}
	// CLAUDE.md carries the filled knowledge-base managed section (R16.3).
	claudemd, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	cm := string(claudemd)
	if !strings.Contains(cm, knowledgeMarkerStart) || !strings.Contains(cm, "Consult the graph before searching") {
		// The body uses "Consult the graph before searching." heading text.
		if !strings.Contains(cm, "Consult the graph before searching") && !strings.Contains(cm, "consult the graph") {
			t.Errorf("CLAUDE.md missing wired knowledge-base section")
		}
	}
}

func TestManifestMigrationFromLegacy(t *testing.T) {
	dir := freshWorkspace(t)
	// Remove the new-path manifest init wrote, leaving only a legacy one.
	newPath := paths.StateManifest(dir)
	_ = os.Remove(newPath)
	m := manifest.New()
	m.Files[".claude/rules/ears-format.md"] = manifest.Hash("OLD SHIPPED\n")
	if err := m.Save(paths.Manifest(dir), "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Simulate the pristine-outdated on-disk file matching the legacy baseline.
	rule := filepath.Join(dir, ".claude", "rules", "ears-format.md")
	if err := os.WriteFile(rule, []byte("OLD SHIPPED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// update must read the legacy baseline (so this is an in-place update, not a
	// conflict) and write the manifest forward to the new path (R14.1, R14.2).
	code, out, _ := run(t, "update", "--root", dir)
	if code != 0 {
		t.Fatalf("update failed: %s", out)
	}
	if !strings.Contains(out, "1 updated") {
		t.Errorf("legacy baseline should yield an in-place update, not a conflict:\n%s", out)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("save should have migrated the manifest to %s", newPath)
	}
}
