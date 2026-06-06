package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/protonspy/csdd/internal/session"
)

// gitEnv returns a deterministic, config-isolated environment for git in tests.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repoWithChanges builds a git repo: a base commit on main, a feature branch
// with a committed edit + a new committed file, an uncommitted edit, an
// untracked file, and a .gitignore'd file. Returns the repo dir.
func repoWithChanges(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init")
	write(t, dir, "a.txt", "base one\nbase two\nbase three\n")
	write(t, dir, ".gitignore", "secret.txt\n")
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-m", "base")
	runGitT(t, dir, "branch", "-M", "main")

	runGitT(t, dir, "checkout", "-b", "feature")
	write(t, dir, "a.txt", "base one\nCHANGED two\nbase three\n")
	write(t, dir, "feat.txt", "new committed file\n")
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-m", "feature work")

	// Uncommitted edit to a tracked file.
	write(t, dir, "a.txt", "base one\nCHANGED two\nbase three\nappended line\n")
	// Untracked (included) and ignored (excluded) files.
	write(t, dir, "u.txt", "untracked alpha\nuntracked beta\n")
	write(t, dir, "secret.txt", "should be ignored\n")
	return dir
}

func readDiff(t *testing.T, root, feature string) session.SpecDiff {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "specs", feature, session.SpecDiffFile))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var d session.SpecDiff
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return d
}

func fileByPath(files []session.DiffFile, path string) *session.DiffFile {
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
}

func TestResolveBase(t *testing.T) {
	dir := repoWithChanges(t)

	ref, sha, err := resolveBase(dir, "")
	if err != nil || ref != "main" || sha == "" {
		t.Fatalf("auto-detect: ref=%q sha=%q err=%v", ref, sha, err)
	}
	if ref2, _, err := resolveBase(dir, "main"); err != nil || ref2 != "main" {
		t.Fatalf("explicit main: ref=%q err=%v", ref2, err)
	}
	if _, _, err := resolveBase(dir, "no-such-ref"); err == nil {
		t.Fatalf("unknown ref should error")
	}
}

func TestEnsureGitRepo(t *testing.T) {
	if err := ensureGitRepo(repoWithChanges(t)); err != nil {
		t.Fatalf("git repo should pass: %v", err)
	}
	if err := ensureGitRepo(t.TempDir()); err == nil {
		t.Fatalf("non-git dir should error")
	}
}

func TestSpecDiffReportGolden(t *testing.T) {
	root := repoWithChanges(t)
	if err := os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}

	aBefore, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if code := specDiffReport([]string{"demo", "--root", root}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	d := readDiff(t, root, "demo")
	if d.Feature != "demo" || d.Base != "main" || d.BaseSHA == "" || d.Head != "working-tree" || d.UpdatedAt == "" {
		t.Fatalf("header: %+v", d)
	}
	if a := fileByPath(d.Files, "a.txt"); a == nil || a.Status != "modified" {
		t.Errorf("a.txt: %+v", a)
	}
	if f := fileByPath(d.Files, "feat.txt"); f == nil || f.Status != "added" {
		t.Errorf("feat.txt: %+v", f)
	}
	if u := fileByPath(d.Files, "u.txt"); u == nil || u.Status != "added" || u.Additions != 2 {
		t.Errorf("u.txt (untracked): %+v", u)
	}
	if s := fileByPath(d.Files, "secret.txt"); s != nil {
		t.Errorf("secret.txt should be excluded by .gitignore: %+v", s)
	}
	if d.Summary.Files != len(d.Files) || d.Summary.Files < 3 {
		t.Errorf("summary: %+v (files=%d)", d.Summary, len(d.Files))
	}

	// Read-only invariant (req 1.5): nothing staged, and the tracked file is
	// byte-for-byte unchanged on disk.
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = root
	cmd.Env = gitEnv()
	if out, _ := cmd.Output(); len(out) != 0 {
		t.Errorf("command staged changes: %s", out)
	}
	if aAfter, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(aAfter) != string(aBefore) {
		t.Errorf("tracked file mutated by diff-report:\nbefore=%q\nafter=%q", aBefore, aAfter)
	}
}

func TestSpecDiffReportRejectsTraversal(t *testing.T) {
	root := repoWithChanges(t)
	if code := specDiffReport([]string{"../../etc", "--root", root}); code != 1 {
		t.Fatalf("traversal feature exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(root)), "etc", session.SpecDiffFile)); err == nil {
		t.Errorf("must not write outside specs/")
	}
}

func TestSpecDiffReportSkipsUntrackedSymlink(t *testing.T) {
	root := repoWithChanges(t)
	if err := os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A secret outside the repo, reachable only via an untracked symlink.
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("TOP SECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if code := specDiffReport([]string{"demo", "--root", root}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	d := readDiff(t, root, "demo")
	if f := fileByPath(d.Files, "link.txt"); f != nil {
		t.Errorf("untracked symlink must not be embedded: %+v", f)
	}
}

func TestSpecDiffReportMissingSpec(t *testing.T) {
	root := repoWithChanges(t)
	if code := specDiffReport([]string{"nope", "--root", root}); code != 1 {
		t.Fatalf("missing spec exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(root, "specs", "nope")); !os.IsNotExist(err) {
		t.Errorf("must not create the spec dir")
	}
}

func TestSpecDiffReportNotGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code := specDiffReport([]string{"demo", "--root", root}); code != 1 {
		t.Fatalf("non-git exit = %d, want 1", code)
	}
}

func TestSpecDiffReportUnknownBase(t *testing.T) {
	root := repoWithChanges(t)
	if err := os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code := specDiffReport([]string{"demo", "--base", "no-such-ref", "--root", root}); code != 1 {
		t.Fatalf("unknown base exit = %d, want 1", code)
	}
}

// TestSpecDiffReportEndToEnd ties the writer to the two consumers: LoadSpecDetail
// surfaces the Diff, and writing the report changes a changedetect snapshot (the
// signal the SSE hub turns into a dashboard refresh — requirements 6.1, 6.5).
func TestSpecDiffReportEndToEnd(t *testing.T) {
	root := repoWithChanges(t)
	if err := os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "specs", "demo", "spec.json"), []byte(`{"feature_name":"demo","phase":"tasks"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Before generating: no diff, and capture the change-detection baseline.
	d, err := session.LoadSpecDetail(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if d.Diff != nil {
		t.Fatalf("Diff should be nil before generating")
	}
	before := session.TakeSnapshot(root)

	if code := specDiffReport([]string{"demo", "--root", root}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	after := session.TakeSnapshot(root)
	if !session.Changed(before, after) {
		t.Errorf("writing diff-report.json should change the changedetect snapshot")
	}
	d, err = session.LoadSpecDetail(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if d.Diff == nil || d.Diff.Base != "main" || len(d.Diff.Files) == 0 {
		t.Errorf("LoadSpecDetail did not surface the diff: %+v", d.Diff)
	}
}

func TestSpecDiffReportMaxLines(t *testing.T) {
	root := repoWithChanges(t)
	if err := os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A big untracked file to force truncation at a small cap.
	var big string
	for i := 0; i < 100; i++ {
		big += "line\n"
	}
	write(t, root, "big.txt", big)

	if code := specDiffReport([]string{"demo", "--max-lines", "5", "--root", root}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	d := readDiff(t, root, "demo")
	if !d.Truncated {
		t.Errorf("report should be flagged truncated at --max-lines 5")
	}
	if b := fileByPath(d.Files, "big.txt"); b == nil || !b.Truncated {
		t.Errorf("big.txt should be truncated: %+v", b)
	}
}
