package plan

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/workspace"
)

// Worktree isolation and integration, against a real repository.
//
// These are the tests the runner's own suite deliberately does not carry: the loop
// tests inject a stub keeper so they stay about verdicts and the ledger, which
// leaves exactly one place — here — where the git behavior has to be real. Stubbing
// git and then asserting against the stub would prove nothing about the thing that
// actually has to work.

// gitRepo builds a repository with one commit and returns its root. Tests that need
// git are skipped rather than failed when it is missing, so the suite still runs on
// a machine without it.
func gitRepo(t *testing.T) string {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "runner@example.test"},
		{"config", "user.name", "csdd runner"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := runGit(root, args...); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	writeRepoFile(t, root, "README.md", "base\n")
	commitAll(t, root, "initial commit")
	return root
}

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	if out, err := runGit(dir, "add", "-A"); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := runGit(dir, "commit", "-m", msg); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
}

func readRepoFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

func newTrees(root string) gitTrees { return gitTrees{root: root, slug: "p", base: "main"} }

// TestWorktreesIsolateConcurrentFeats is the property the whole design exists for:
// two feats being worked at the same time cannot see each other's edits, so neither
// can break the other's build or stage the other's half-written file.
func TestWorktreesIsolateConcurrentFeats(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	a, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := trees.Ensure("feat-b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("each feat needs its own tree, both got %s", a)
	}

	writeRepoFile(t, a, "shared.go", "package a // written by feat-a\n")
	if got := readRepoFile(t, b, "shared.go"); got != "" {
		t.Errorf("feat-b must not see feat-a's uncommitted edit, got %q", got)
	}
	if got := readRepoFile(t, root, "shared.go"); got != "" {
		t.Errorf("the run's base must not see an in-flight edit either, got %q", got)
	}
}

// TestEnsureIsIdempotentAndKeepsUncommittedWork covers the `continue` path, and the
// one place this design departs from the tool it is modelled on. That tool can always
// recreate a tree from its branch because it commits on pause; the csdd runner never
// authors a commit, so a second attempt has to get the SAME tree back — with the
// half-finished work its predecessor left sitting in it.
func TestEnsureIsIdempotentAndKeepsUncommittedWork(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	first, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, first, "half-done.go", "package a // attempt 1 got this far\n")

	second, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("the same feat must come back to the same tree, got %s then %s", first, second)
	}
	if got := readRepoFile(t, second, "half-done.go"); !strings.Contains(got, "attempt 1") {
		t.Errorf("the next attempt must inherit its predecessor's unfinished work, got %q", got)
	}
}

// TestEnsureRestoresACrashedTreeFromItsBranch is the resume path: the directory is
// gone (a killed run, a cleaned disk) but the branch survived, so the committed work
// comes back. This is what makes the tree disposable and the branch the durable half.
func TestEnsureRestoresACrashedTreeFromItsBranch(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	dir, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "landed.go", "package a // committed before the crash\n")
	commitAll(t, dir, "feat-a: partial work")

	// Simulate the host dying and the directory being lost, leaving git's
	// registration dangling — the exact debris Ensure has to clear.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	restored, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatalf("a crashed tree must be restorable from its branch: %v", err)
	}
	if got := readRepoFile(t, restored, "landed.go"); !strings.Contains(got, "committed before the crash") {
		t.Errorf("committed work must survive the tree, got %q", got)
	}
}

// TestWorktreeResolvesAsItsOwnWorkspaceRoot is the difference between real isolation
// and the appearance of it.
//
// Every `csdd` command the session runs walks UP from its working directory looking
// for .csdd/ or .claude/. A fresh worktree holds only TRACKED files, and in a typical
// csdd workspace .csdd/ is transient while .claude/ and specs/ are gitignored — so
// without a marker the walk leaves the worktree and lands on the shared repository
// root, where every concurrent session would author its spec. The worktrees would
// exist and isolate nothing.
func TestWorktreeResolvesAsItsOwnWorkspaceRoot(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	dir, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if got := workspace.Find(dir); !sameDir(got, dir) {
		t.Errorf("a session running in the worktree must resolve the worktree as its root, got %s want %s", got, dir)
	}
	// And from a subdirectory, which is where a session actually works.
	sub := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := workspace.Find(sub); !sameDir(got, dir) {
		t.Errorf("the walk from a subdirectory must stop at the worktree, got %s want %s", got, dir)
	}

	// The marker survives a restore, or the second attempt at a feat loses isolation.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	restored, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if got := workspace.Find(restored); !sameDir(got, restored) {
		t.Errorf("a restored worktree must still be its own root, got %s want %s", got, restored)
	}
}

// TestIntegrateRefusesUncommittedWork closes the gap between the two things that
// must agree before a feat is recorded delivered: the verdict gate reads FILES, the
// merge carries COMMITS. A session that wrote every artifact and never committed
// satisfies the first and delivers nothing through the second — and because a
// delivered feat's worktree is discarded, the work would be gone with the ledger
// claiming it landed.
func TestIntegrateRefusesUncommittedWork(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	dir, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "a.go", "package a // written, never committed\n")

	err = trees.Integrate("feat-a")
	if err == nil {
		t.Fatal("uncommitted work must not be accepted as delivered")
	}
	var unc *UncommittedWorkError
	if !asUncommitted(err, &unc) {
		t.Fatalf("the refusal must be typed so the runner hands the feat back, got %T: %v", err, err)
	}
	if !containsSubstring(unc.Paths, "a.go") {
		t.Errorf("the refusal must name what was left behind, got %v", unc.Paths)
	}
	if got := readRepoFile(t, root, "a.go"); got != "" {
		t.Errorf("nothing may have reached the base, got %q", got)
	}

	// Committing is the whole fix — the work itself was already finished.
	commitAll(t, dir, "feat-a: commit the work")
	if err := trees.Integrate("feat-a"); err != nil {
		t.Fatalf("the same feat must land once committed: %v", err)
	}
	if got := readRepoFile(t, root, "a.go"); !strings.Contains(got, "never committed") {
		t.Errorf("the base must carry the feat after the commit, got %q", got)
	}
}

// TestIntegrateLandsTheFeatOnTheBase is the half the reference implementation never
// needed: its instances are independent, so leaving each on its branch is enough. A
// csdd plan is a DAG, so a delivered feat has to be ON the base before the feat that
// depends on it is cut from it.
func TestIntegrateLandsTheFeatOnTheBase(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	dir, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "a.go", "package a // delivered\n")
	commitAll(t, dir, "feat-a: deliver")

	if err := trees.Integrate("feat-a"); err != nil {
		t.Fatalf("a delivered feat must land on the base: %v", err)
	}
	if got := readRepoFile(t, root, "a.go"); !strings.Contains(got, "delivered") {
		t.Errorf("the base must contain the merged feat, got %q", got)
	}

	// And the point of merging at all: the NEXT feat is cut from a base that
	// contains its dependency.
	next, err := trees.Ensure("feat-b")
	if err != nil {
		t.Fatal(err)
	}
	if got := readRepoFile(t, next, "a.go"); !strings.Contains(got, "delivered") {
		t.Errorf("a dependent feat must be cut from a base carrying its dependency, got %q", got)
	}
}

// TestIntegrateReportsAConflictAndRollsBack pins the failure mode that must not
// corrupt the run. Two feats edited the same lines; the second cannot land. The base
// has to come back exactly as it was — a half-merged base would be inherited by
// every feat dispatched afterwards.
func TestIntegrateReportsAConflictAndRollsBack(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)
	writeRepoFile(t, root, "shared.go", "package p\n\nconst V = 0\n")
	commitAll(t, root, "add shared")

	a, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := trees.Ensure("feat-b")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, a, "shared.go", "package p\n\nconst V = 1 // feat-a\n")
	commitAll(t, a, "feat-a: bump")
	writeRepoFile(t, b, "shared.go", "package p\n\nconst V = 2 // feat-b\n")
	commitAll(t, b, "feat-b: bump")

	if err := trees.Integrate("feat-a"); err != nil {
		t.Fatalf("the first feat should land cleanly: %v", err)
	}
	err = trees.Integrate("feat-b")
	var conflict *MergeConflictError
	if err == nil {
		t.Fatal("the second feat edits the same line and must report a conflict")
	}
	if !asMergeConflict(err, &conflict) {
		t.Fatalf("a conflict must be typed so the runner can hand the feat back, got %T: %v", err, err)
	}
	if len(conflict.Files) == 0 || !containsSubstring(conflict.Files, "shared.go") {
		t.Errorf("the conflict must name the file so the handoff can, got %v", conflict.Files)
	}

	// Rolled back: the base still holds feat-a's version, cleanly, with no merge in
	// progress and nothing staged.
	if got := readRepoFile(t, root, "shared.go"); !strings.Contains(got, "feat-a") {
		t.Errorf("the base must be left on feat-a's merged state, got %q", got)
	}
	if strings.Contains(readRepoFile(t, root, "shared.go"), "<<<<<<<") {
		t.Error("the base must never be left holding conflict markers")
	}
	dirty, err := gitDirty(root)
	if err != nil {
		t.Fatal(err)
	}
	// Filtered the same way preflight filters it: the runner's own state dir holds
	// the worktrees themselves, so it is always "untracked" here and is never the
	// operator's work — which is the only thing a rollback could have stranded.
	if dirty = withoutRunnerState(dirty); len(dirty) > 0 {
		t.Errorf("a rolled-back merge must leave the base clean, got %v", dirty)
	}
}

// TestDiscardRemovesTheTreeAndKeepsTheBranch: the branch is what the PR is opened
// from and what a later attempt is restored from, so removal must be one-sided.
func TestDiscardRemovesTheTreeAndKeepsTheBranch(t *testing.T) {
	root := gitRepo(t)
	trees := newTrees(root)

	dir, err := trees.Ensure("feat-a")
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "feat-a: deliver")

	if err := trees.Discard("feat-a"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the worktree directory must be gone, stat err = %v", err)
	}
	if !trees.branchExists(trees.branch("feat-a")) {
		t.Error("the branch must survive the tree — it carries the delivered work")
	}
	// Discarding twice is what a resumed or retried cleanup does; it must not fail.
	if err := trees.Discard("feat-a"); err != nil {
		t.Errorf("discard must be idempotent, got %v", err)
	}
}

// TestPreflightGitNamesWhatIsWrong covers the four refusals, because each is a way
// the run would otherwise go wrong deep into a plan instead of failing at the start.
func TestPreflightGitNamesWhatIsWrong(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git is not on PATH")
	}

	// Not a repository at all.
	if _, err := preflightGit(t.TempDir()); err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Errorf("a non-repository must be refused by name, got %v", err)
	}

	// A clean repository on a branch: the happy path, and it reports the base.
	root := gitRepo(t)
	base, err := preflightGit(root)
	if err != nil {
		t.Fatalf("a clean repository on a branch must pass: %v", err)
	}
	if base != "main" {
		t.Errorf("the base should be the checked-out branch, got %q", base)
	}

	// Uncommitted changes: the run merges into this tree, so it must refuse first.
	writeRepoFile(t, root, "dirty.txt", "uncommitted\n")
	if _, err := preflightGit(root); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("a dirty tree must be refused before the run starts, got %v", err)
	}
	commitAll(t, root, "clean up")

	// Detached HEAD: nothing to merge into.
	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(root, "checkout", "--detach", strings.TrimSpace(head)); err != nil {
		t.Fatalf("detach: %v: %s", err, out)
	}
	if _, err := preflightGit(root); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Errorf("a detached HEAD must be refused by name, got %v", err)
	}
}

// TestSquadRunDeliversThroughRealWorktrees is the whole capability in one run: two
// independent feats worked CONCURRENTLY in their own trees, then a third that
// depends on both and can only succeed if their code reached it through the merge.
//
// It is the test that would have caught the design being wrong rather than the code
// — a squad whose feats cannot see their dependencies delivers a plan that does not
// build.
func TestSquadRunDeliversThroughRealWorktrees(t *testing.T) {
	root := gitRepo(t)
	seedApprovedPlan(t, root, squadPlan)

	probe := &concurrencyProbe{rendezvous: make(chan struct{}), want: 2, barrierAll: true}
	h := baseHooks(t, root)
	h.Trees = nil // the real keeper: preflight resolves the base and cuts real trees
	h.Session = func(req SessionRequest) (SessionOutcome, error) {
		probe.enter(req.Feat)
		defer probe.leave()
		// `c` depends on a and b, so by the time it runs their merged files must be
		// present in its freshly-cut tree. Assert it from inside the session, which
		// is the only place that sees what the session would actually have to build against.
		if req.Feat.Slug == "c" {
			for _, dep := range []string{"a", "b"} {
				if got := readRepoFile(t, req.Dir, dep+".go"); !strings.Contains(got, "delivered") {
					probe.record(fmt.Errorf("c's worktree is missing dependency %s: %q", dep, got))
				}
			}
		}
		if err := deliverSpecFiles(req.Dir, req.Feat.Slug); err != nil {
			return SessionOutcome{}, err
		}
		writeRepoFile(t, req.Dir, req.Feat.Slug+".go", "package p // "+req.Feat.Slug+" delivered\n")
		commitAll(t, req.Dir, "feat "+req.Feat.Slug+": deliver")
		return SessionOutcome{Verdict: Verdict{Status: VerdictDone}}, nil
	}

	sum, err := Run(RunOptions{Root: root, Slug: "p", SquadLimit: 2, Hooks: h, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range probe.errs {
		t.Error(e)
	}
	if !sum.Completed || sum.Steps != 3 {
		t.Fatalf("every feat should be delivered through its own tree: completed=%v steps=%d (%s)",
			sum.Completed, sum.Steps, sum.Reason)
	}
	if probe.peak < 2 {
		t.Errorf("a and b are independent and must have run at once, peak was %d", probe.peak)
	}
	// Every feat's code is on the base, and no worktree survived the run.
	for _, feat := range []string{"a", "b", "c"} {
		if got := readRepoFile(t, root, feat+".go"); !strings.Contains(got, "delivered") {
			t.Errorf("%s must be merged into the base, got %q", feat, got)
		}
		if _, err := os.Stat(newTrees(root).path(feat)); !os.IsNotExist(err) {
			t.Errorf("%s's worktree should have been discarded, stat err = %v", feat, err)
		}
		if !newTrees(root).branchExists(newTrees(root).branch(feat)) {
			t.Errorf("%s's branch must survive for the PR", feat)
		}
	}
}

// seedApprovedPlan lays a plan into a real repository and approves it, committing
// everything so the tree is clean when preflight looks at it.
func seedApprovedPlan(t *testing.T, root, planMD string) {
	t.Helper()
	writeRepoFile(t, root, "docs/stack.md", "# Tech contract\n\n## Decided\n\n"+
		"| Domain | Choice | Version | Why | Refs |\n|---|---|---|---|---|\n"+
		"| Language | Go | 1.22 | speed | — |\n\n## Rules\n")
	writeRepoFile(t, root, "docs/wiki/pages/storage-design.md", "# Storage Design\n")
	writeRepoFile(t, root, filepath.Join("docs", "plans", "p", "plan.md"), planMD)
	doc, err := Load(root, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApprovePlan(root, doc, fixedNow()); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "seed the plan")
}

// asMergeConflict is errors.As, spelled out to keep the import surface of this file
// to what it is actually testing.
func asMergeConflict(err error, target **MergeConflictError) bool {
	c, ok := err.(*MergeConflictError)
	if ok {
		*target = c
	}
	return ok
}

func asUncommitted(err error, target **UncommittedWorkError) bool {
	u, ok := err.(*UncommittedWorkError)
	if ok {
		*target = u
	}
	return ok
}

func containsSubstring(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
