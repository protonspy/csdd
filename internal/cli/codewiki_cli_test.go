package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codewikiWorkspace lays down a workspace holding a checkout and a document over
// it, and returns the workspace root.
func codewikiWorkspace(t *testing.T, doc string) string {
	t.Helper()
	dir := freshWorkspace(t)
	repo := filepath.Join(dir, "docs", "raw", "widget")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "raw", "acme-widget.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const cliSoundDoc = `<!-- csdd-codewiki v1 | acme/widget | src: docs/raw/widget | 2026-07-26T00:00:00Z | 1 sections -->

## Structure

└── 1 Overview

<<< SECTION: 1 Overview [1-overview] >>>

# Overview

Widget [main.go:1-3]().
`

func TestCodewikiSkillAndCommandInstalled(t *testing.T) {
	dir := freshWorkspace(t)
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "codewiki", "SKILL.md")); err != nil {
		t.Fatalf("codewiki skill not installed: %v", err)
	}
	if code, _, errOut := run(t, "skill", "validate", "codewiki", "--root", dir); code != 0 {
		t.Errorf("codewiki skill failed validation (code=%d): %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "commands", "csdd-codewiki.md")); err != nil {
		t.Errorf("/csdd-codewiki command not installed: %v", err)
	}

	skill := readFile(t, filepath.Join(dir, ".claude", "skills", "codewiki", "SKILL.md"))
	for _, want := range []string{
		"<<< SECTION:",               // the delimiter contract
		"csdd codewiki lint",         // the gate
		"/csdd-wiki-ingest",          // the hand-off, not done here
		"Copy file paths",            // the anti-hallucination rule
		"docs/raw/<owner>-<repo>.md", // where the document lands
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("codewiki skill missing %q", want)
		}
	}

	// The wiki skill must route a dropped repository here rather than trying to
	// ingest it file by file.
	wiki := readFile(t, filepath.Join(dir, ".claude", "skills", "wiki", "SKILL.md"))
	if !strings.Contains(wiki, "codewiki") {
		t.Error("wiki skill does not route source checkouts to codewiki")
	}
	raw := readFile(t, filepath.Join(dir, "docs", "raw", "README.md"))
	if !strings.Contains(raw, "/csdd-codewiki") {
		t.Error("docs/raw/README.md does not document the repository drop")
	}
}

// `csdd wiki init` alone must also lay the codewiki skill down: the dropzone
// README it writes describes the repo workflow, so the skill has to be there.
func TestWikiInitInstallsCodewikiSkill(t *testing.T) {
	dir := t.TempDir()
	if code, _, errOut := run(t, "wiki", "init", "--root", dir); code != 0 {
		t.Fatalf("wiki init failed (code=%d): %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "codewiki", "SKILL.md")); err != nil {
		t.Errorf("wiki init did not install the codewiki skill: %v", err)
	}
}

func TestCodewikiLintCleanDocument(t *testing.T) {
	dir := codewikiWorkspace(t, cliSoundDoc)
	code, out, errOut := run(t, "codewiki", "lint", "docs/raw/acme-widget.md", "--root", dir)
	if code != 0 {
		t.Fatalf("clean document must exit 0, got %d: %s%s", code, out, errOut)
	}
	if !strings.Contains(out, "no findings") {
		t.Errorf("expected a clean report, got: %s", out)
	}
}

func TestCodewikiLintExitsNonZeroOnFindings(t *testing.T) {
	dir := codewikiWorkspace(t, strings.Replace(cliSoundDoc, "[main.go:1-3]()", "[gone.go:1-3]()", 1))
	code, out, _ := run(t, "codewiki", "lint", "docs/raw/acme-widget.md", "--root", dir)
	if code != 2 {
		t.Errorf("findings must exit 2 (CI-gateable), got %d", code)
	}
	if !strings.Contains(out, "gone.go") {
		t.Errorf("expected the unresolved citation in the report, got: %s", out)
	}
}

// With no argument the whole dropzone is linted — the form a CI gate uses.
func TestCodewikiLintDiscoversTheDropzone(t *testing.T) {
	dir := codewikiWorkspace(t, cliSoundDoc)
	if err := os.WriteFile(filepath.Join(dir, "docs", "raw", "article.md"), []byte("Just an article.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "codewiki", "lint", "--root", dir)
	if code != 0 {
		t.Fatalf("expected a clean sweep, got %d: %s%s", code, out, errOut)
	}
	if strings.Contains(out, "article.md") {
		t.Errorf("a plain article carries no provenance header and must not be linted: %s", out)
	}
}

func TestCodewikiLintJSON(t *testing.T) {
	dir := codewikiWorkspace(t, strings.Replace(cliSoundDoc, "[main.go:1-3]()", "[main.go:1-99]()", 1))
	code, out, _ := run(t, "codewiki", "lint", "docs/raw/acme-widget.md", "--root", dir, "--json")
	if code != 2 {
		t.Errorf("findings must exit 2, got %d", code)
	}
	var got struct {
		Documents []struct {
			Path     string `json:"path"`
			Repo     string `json:"repo"`
			Findings []struct {
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"findings"`
		} `json:"documents"`
		Faults int `json:"faults"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json must emit parseable JSON: %v (%s)", err, out)
	}
	if got.Faults != 1 || len(got.Documents) != 1 || len(got.Documents[0].Findings) != 1 {
		t.Fatalf("unexpected report shape: %+v", got)
	}
	if got.Documents[0].Findings[0].Kind != "citation" {
		t.Errorf("expected a citation finding, got %q", got.Documents[0].Findings[0].Kind)
	}
	if !strings.Contains(filepath.ToSlash(got.Documents[0].Repo), "docs/raw/widget") {
		t.Errorf("report must name the resolved checkout, got %q", got.Documents[0].Repo)
	}
}

func TestCodewikiRejectsUnknownActionAndBadTarget(t *testing.T) {
	dir := codewikiWorkspace(t, cliSoundDoc)
	if code, _, _ := run(t, "codewiki", "frobnicate", "--root", dir); code == 0 {
		t.Error("an unknown action must not exit 0")
	}
	if code, _, _ := run(t, "codewiki", "lint", "docs/raw/nope.md", "--root", dir); code == 0 {
		t.Error("a missing document must not exit 0")
	}
	if code, _, _ := run(t, "codewiki", "--help"); code != 0 {
		t.Error("--help must exit 0")
	}
}
