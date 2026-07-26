package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKebabCheck(t *testing.T) {
	cases := []struct {
		in   string
		fail bool
	}{
		{"good", false},
		{"good-name", false},
		{"good-name-123", false},
		{"BadName", true},
		{"-leading-dash", true},
		{"trailing-", true},
		{"123-leading-digit", true},
		{"under_score", true},
		{"", true},
	}
	for _, c := range cases {
		err := KebabCheck(c.in, "thing")
		if c.fail && err == nil {
			t.Errorf("KebabCheck(%q) expected error, got nil", c.in)
		}
		if !c.fail && err != nil {
			t.Errorf("KebabCheck(%q) unexpected error: %v", c.in, err)
		}
	}
}

func TestFindAndResolve(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Find(nested)
	want, _ := filepath.Abs(root)
	if got != want {
		t.Errorf("Find(%q) = %q, want %q", nested, got, want)
	}
	// Find with no .claude/ falls back to the input itself.
	scratch := t.TempDir()
	fallback := Find(scratch)
	wantFallback, _ := filepath.Abs(scratch)
	if fallback != wantFallback {
		t.Errorf("Find without .claude: got %q want %q", fallback, wantFallback)
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Explicit --root path that exists.
	got, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve(%q) error: %v", root, err)
	}
	abs, _ := filepath.Abs(root)
	if got != abs {
		t.Errorf("Resolve(%q) = %q, want %q", root, got, abs)
	}
	// Explicit --root that does NOT exist.
	if _, err := Resolve(filepath.Join(root, "missing")); err == nil {
		t.Error("Resolve with missing --root should fail")
	}
	// Empty arg falls back to Find(cwd).
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	got, err = Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") error: %v", err)
	}
	// os.Getwd (called inside Resolve for the empty-arg case) returns the
	// physical path, which on macOS resolves the /var → /private/var symlink
	// that filepath.Abs leaves intact. Compare physical paths so the assertion
	// is portable; the lexical equality still covers platforms without symlinks.
	absEval, _ := filepath.EvalSymlinks(abs)
	gotEval, _ := filepath.EvalSymlinks(got)
	if got != abs && gotEval != absEval {
		t.Errorf("Resolve(\"\") = %q, want %q", got, abs)
	}
}

func TestSafeWriteAndWriteFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "file.md")

	created, err := SafeWrite(target, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("first SafeWrite should report created=true")
	}
	created, err = SafeWrite(target, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second SafeWrite should report created=false")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "hello" {
		t.Errorf("SafeWrite mutated existing file: got %q", string(data))
	}

	// WriteFile without overwrite refuses to clobber.
	if err := WriteFile(target, "no", false); err == nil {
		t.Error("WriteFile should refuse to overwrite without flag")
	}
	if err := WriteFile(target, "new", true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "new" {
		t.Errorf("WriteFile didn't overwrite: %q", string(data))
	}
}

func TestSteeringAndSpecsDirRequireInit(t *testing.T) {
	root := t.TempDir()
	if _, err := SteeringDir(root); err == nil {
		t.Error("SteeringDir without .claude/steering should fail")
	}
	if _, err := SpecsDir(root); err == nil {
		t.Error("SpecsDir without specs should fail")
	}
	_ = os.MkdirAll(filepath.Join(root, ".claude", "steering"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "specs"), 0o755)
	if _, err := SteeringDir(root); err != nil {
		t.Errorf("SteeringDir after init: %v", err)
	}
	if _, err := SpecsDir(root); err != nil {
		t.Errorf("SpecsDir after init: %v", err)
	}
}

func TestSkillAndAgentRoots(t *testing.T) {
	root := t.TempDir()
	skills := SkillRoot(root)
	if !strings.HasSuffix(skills, filepath.Join(".claude", "skills")) {
		t.Errorf("SkillRoot path mismatch: %s", skills)
	}
	agents := AgentRoot(root)
	if !strings.HasSuffix(agents, filepath.Join(".claude", "agents")) {
		t.Errorf("AgentRoot path mismatch: %s", agents)
	}
	// Both are pure resolvers: merely asking for the path must not create it.
	if _, err := os.Stat(skills); err == nil {
		t.Error("SkillRoot must not create the directory as a side effect")
	}
	if _, err := os.Stat(agents); err == nil {
		t.Error("AgentRoot must not create the directory as a side effect")
	}
}

func TestRelative(t *testing.T) {
	root := t.TempDir()
	got := Relative(root, filepath.Join(root, "sub", "file.md"))
	if got != filepath.Join("sub", "file.md") {
		t.Errorf("Relative got %q", got)
	}
	// filepath.Rel returns something computable for any two absolute paths,
	// so we just confirm we get back a non-empty string.
	if Relative("/some/other", "/totally/different") == "" {
		t.Error("Relative on absolute paths should still return a value")
	}
}

// TestSafeWriteMkdirAllFailure forces the MkdirAll branch to fail by
// pointing at a path nested inside an existing regular file.
func TestSafeWriteAndWriteFileMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// regular is a file, not a directory; MkdirAll(filepath.Dir(<regular>/sub/x))
	// will try to create regular/sub which fails because regular is not a dir.
	target := filepath.Join(regular, "sub", "x.md")
	if _, err := SafeWrite(target, "content"); err == nil {
		t.Error("SafeWrite should fail when path component is not a directory")
	}
	if err := WriteFile(target, "content", true); err == nil {
		t.Error("WriteFile should fail when path component is not a directory")
	}
}

// TestFindAtFilesystemRoot exercises the parent == cur termination branch.
func TestFindAtFilesystemRoot(t *testing.T) {
	got := Find("/")
	if got == "" {
		t.Error("Find(/) should still return a path")
	}
}

func TestResolveExplicitRoot(t *testing.T) {
	// Hit Resolve's `if arg != ""` happy branch with an absolute path.
	got, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Error("expected non-empty resolved path")
	}
}

// TestRelativeFallback exercises the `if err != nil` branch in Relative.
// filepath.Rel returns an error when one path is relative and the other is
// absolute — that triggers the fallback path.
func TestRelativeFallback(t *testing.T) {
	got := Relative("relative-base", "/absolute/target")
	if got != "/absolute/target" {
		t.Errorf("Relative should fall back to target on error: got %q", got)
	}
}

// setFakeHome points os.UserHomeDir at dir for the duration of the test. Go reads
// USERPROFILE on Windows and HOME elsewhere; setting both keeps the helper
// portable without branching on GOOS.
func setFakeHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFindPrefersStateMarker locks the marker that means "csdd workspace". Only
// csdd writes .csdd/, so it wins over a .claude/ that some other tool may have
// left in a parent.
func TestFindPrefersStateMarker(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	nested := filepath.Join(inner, "a", "b")
	mkdirs(t, nested, filepath.Join(outer, ".claude"), filepath.Join(inner, ".csdd"))
	setFakeHome(t, t.TempDir())

	want, _ := filepath.Abs(inner)
	if got := Find(nested); got != want {
		t.Errorf("Find(%q) = %q, want the .csdd/ marker at %q", nested, got, want)
	}
}

// TestFindAcceptsLegacyClaudeMarker keeps workspaces created before the marker
// moved resolving exactly as they did.
func TestFindAcceptsLegacyClaudeMarker(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	mkdirs(t, nested, filepath.Join(root, ".claude"))
	setFakeHome(t, t.TempDir())

	want, _ := filepath.Abs(root)
	if got := Find(nested); got != want {
		t.Errorf("Find(%q) = %q, want the legacy .claude/ root %q", nested, got, want)
	}
}

// TestFindNeverResolvesToTheHomeDirectory is the regression that matters.
//
// ~/.claude is Claude Code's global config directory and exists on every machine
// running Claude Code. Before the guard, any command run outside a workspace but
// under $HOME resolved its root to $HOME — so `csdd skill list` listed the user's
// GLOBAL skills, and `csdd skill delete` would have deleted one.
//
// The assertion is deliberately "not home" rather than an exact path: whatever
// lives above the fake home on the test machine may legitimately be found, and
// the property under test is only that the home directory itself is never it.
func TestFindNeverResolvesToTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	nested := filepath.Join(home, "projects", "not-a-workspace")
	mkdirs(t, nested, filepath.Join(home, ".claude"))
	setFakeHome(t, home)

	homeAbs, _ := filepath.Abs(home)
	if got := Find(nested); got == homeAbs {
		t.Errorf("Find(%q) resolved to the user's home %q — ~/.claude is Claude Code's global config, not a csdd workspace", nested, homeAbs)
	}
}

// TestFindUsesHomeWhenItCarriesTheCsddMarker draws the line precisely: the guard
// is about the AMBIGUOUS marker, not about the home directory being off-limits.
// A home directory that csdd itself initialized carries .csdd/ and still resolves.
func TestFindUsesHomeWhenItCarriesTheCsddMarker(t *testing.T) {
	home := t.TempDir()
	nested := filepath.Join(home, "a")
	mkdirs(t, nested, filepath.Join(home, ".claude"), filepath.Join(home, ".csdd"))
	setFakeHome(t, home)

	want, _ := filepath.Abs(home)
	if got := Find(nested); got != want {
		t.Errorf("Find(%q) = %q, want %q — an explicit .csdd/ marker makes the root unambiguous", nested, got, want)
	}
}
