package plan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
)

// Filesystem isolation for the squad.
//
// A squad runs several claude sessions at once, and every one of them edits code,
// runs the plan's Quality Gates, and drives git itself. Pointed at one working tree
// that is not concurrency with extra steps — it is two agents sharing an index.
// So each feat gets its own git worktree on its own branch, which is the same
// primitive the brief already hands the orchestrating session one level down
// ("Dispatch (P) tasks in DIFFERENT _Boundary:_ groups concurrently (worktree
// isolation)"), applied one level up to whole feats.
//
// The branch is the durable half and the worktree is the disposable one. A feat
// that reports `continue` keeps its branch and gets its worktree back on the next
// attempt; a feat that is delivered has its branch merged into the run's base and
// its worktree removed. That split is what makes a crash cheap: the tree is
// recreatable from the branch, so losing it loses nothing.
//
// Integration is the part a per-instance-worktree tool does NOT have to solve.
// Isolated sessions working independent tasks can simply leave their branches for a
// human to review and push. A csdd plan is a DAG: `c` depends on `a` and `b`, and a
// worktree cut from an untouched base would not contain a line of either — the
// Depends column would go quiet exactly where it matters most. So the runner merges
// a feat into the run's base the moment its `done` clears the verdict gate, and
// every later worktree is cut from that updated base. The human PR gate still sits
// where it always did: after the run, over the base branch.

// treeKeeper owns the isolated worktree each feat's sessions run in. The runner
// holds it behind an interface so the loop's own tests keep running with plain
// directories and no git at all — the git-backed behavior has its own tests against
// a real repository.
type treeKeeper interface {
	// Ensure returns the path to the feat's worktree, creating it when it does not
	// exist yet and restoring it from the feat's branch when an earlier attempt left
	// one behind. Repeated calls for the same feat return the same tree.
	Ensure(feat string) (string, error)
	// Integrate lands the feat's branch on the run's base so that every feat
	// dispatched afterwards is cut from a base that contains it.
	Integrate(feat string) error
	// Discard removes the feat's worktree directory. The branch survives — it is
	// what the PR is opened from, and what a later attempt is restored from.
	Discard(feat string) error
}

// MergeConflictError signals that a delivered feat could not be landed on the run's
// base because it conflicts with what is already there.
//
// It is a work outcome, not an infrastructure failure: the feat is genuinely
// finished, it just no longer applies to a base that moved under it. The runner
// therefore treats it like a verdict-gate rejection — the feat comes back as partial
// work with a handoff naming the conflicting files, and the next session rebases —
// rather than failing the feat or, worse, marking it delivered on a merge that never
// happened.
type MergeConflictError struct {
	Feat   string
	Branch string
	Files  []string // the conflicting paths, as git reported them
	Detail string   // git's own output, for the failure log
}

func (e *MergeConflictError) Error() string {
	if len(e.Files) == 0 {
		return fmt.Sprintf("merging %s into the run base conflicted", e.Branch)
	}
	return fmt.Sprintf("merging %s into the run base conflicted in %s", e.Branch, strings.Join(e.Files, ", "))
}

// gitTrees is the production treeKeeper: one worktree per feat, all cut from the
// run's base branch and merged back into it.
type gitTrees struct {
	root string // the repository root — also the tree the run integrates into
	slug string // plan slug, so two plans in one repo never share a branch
	base string // the branch the run integrates into (resolved at preflight)
}

// treesDir is where a plan's worktrees live: under the runner's own transient state
// dir, which is already gitignored, so the trees never show up as untracked noise in
// the repository they were cut from.
func (g gitTrees) treesDir() string {
	return filepath.Join(stateDir(g.root, g.slug), "trees")
}

func (g gitTrees) path(feat string) string { return filepath.Join(g.treesDir(), feat) }

// branch is the feat's branch name. Namespaced by plan so two plans that happen to
// name a feat the same thing do not collide, and prefixed so `git branch` makes it
// obvious which branches a run created.
func (g gitTrees) branch(feat string) string {
	return sanitizeBranch("csdd/" + g.slug + "/" + feat)
}

// sanitizeBranch reduces a name to a safe subset of what git accepts in a ref.
// Plan and feat slugs are already kebab-checked upstream, so in practice this
// changes nothing — it is here because the branch name is assembled from user
// input, and a ref that git rejects would fail at `worktree add` with a message
// about refs rather than about the feat.
func sanitizeBranch(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, " ", "-"))
	s = reUnsafeBranch.ReplaceAllString(s, "")
	s = reRepeatedDash.ReplaceAllString(s, "-")
	return strings.Trim(s, "-/")
}

var (
	reUnsafeBranch = regexp.MustCompile(`[^a-z0-9\-_/.]+`)
	reRepeatedDash = regexp.MustCompile(`-+`)
)

// Ensure creates or restores the feat's worktree. The three cases are the whole
// lifecycle.
//
// A LIVE tree is reused untouched. This is where the runner departs from the tool
// this design is modelled on, and deliberately: that one can always recreate a tree
// from its branch because it commits on pause, whereas the csdd runner never
// authors a commit — the session owns git — so a feat that reported `continue` has
// its unfinished work sitting uncommitted in that tree. Cutting a fresh one would
// silently discard it.
//
// A surviving BRANCH with no tree is restored onto a new one: the resume path after
// a crash or a removed directory. Committed work comes back; that is what the
// branch is for.
//
// A feat nobody has touched gets a new branch cut from the run's base, resolved to
// a commit rather than passed as a branch name so the tree starts from exactly what
// was merged and inherits nothing else.
func (g gitTrees) Ensure(feat string) (string, error) {
	path := g.path(feat)
	if live, err := liveWorktree(path); err != nil {
		return "", err
	} else if live {
		return path, nil
	}
	// Clear both kinds of debris a killed run leaves: a registration git still
	// holds, and the directory itself. `git worktree add` refuses a target that is
	// either, and neither is an error worth reporting — they are the normal state
	// after a crash.
	_, _ = runGit(g.root, "worktree", "remove", "--force", path)
	_ = os.RemoveAll(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	branch := g.branch(feat)
	if g.branchExists(branch) {
		if out, err := runGit(g.root, "worktree", "add", path, branch); err != nil {
			return "", fmt.Errorf("could not restore the worktree for %s from %s: %w: %s", feat, branch, err, out)
		}
		return path, nil
	}
	base, err := g.baseCommit()
	if err != nil {
		return "", err
	}
	if out, err := runGit(g.root, "worktree", "add", "-b", branch, path, base); err != nil {
		return "", fmt.Errorf("could not create the worktree for %s: %w: %s", feat, err, out)
	}
	return path, nil
}

// baseCommit resolves the run's base branch to the commit a new worktree is cut
// from. It is re-resolved per feat, not captured once: every merge moves the base,
// and a feat dispatched after its dependency landed must be cut from a base that
// contains it — which is the whole reason the runner merges at all.
func (g gitTrees) baseCommit() (string, error) {
	out, err := runGit(g.root, "rev-parse", g.base)
	if err != nil {
		if isUnbornHead(out) {
			return "", fmt.Errorf("the repository has no commits yet; make an initial commit before running the plan")
		}
		return "", fmt.Errorf("could not resolve the run base %q: %s", g.base, out)
	}
	return strings.TrimSpace(out), nil
}

// isUnbornHead recognizes git's several ways of saying "this repository has no
// commit yet", which is the one repository state that cannot host a worktree and is
// worth naming plainly instead of relaying git's wording.
func isUnbornHead(out string) bool {
	for _, s := range []string{"ambiguous argument", "not a valid object name", "unknown revision"} {
		if strings.Contains(out, s) {
			return true
		}
	}
	return false
}

// liveWorktree reports whether path is a working tree git can still use. Checking
// the directory and its .git entry is both cheaper and more honest than parsing
// `worktree list`: what matters is whether the tree is usable right now, and an
// orphaned directory whose .git went away is not.
func liveWorktree(path string) (bool, error) {
	for _, p := range []string{path, filepath.Join(path, ".git")} {
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("could not inspect the worktree at %s: %w", path, err)
		}
	}
	return true, nil
}

// Integrate merges the feat's branch into the run's base.
//
// --no-ff on purpose: one merge commit per feat keeps the run's shape legible in
// the history, and it means a feat can be reverted as a unit. The merge runs in the
// repository root, which preflight pinned to the base branch with a clean tree, and
// a conflict is rolled back so the base is never left mid-merge.
func (g gitTrees) Integrate(feat string) error {
	branch := g.branch(feat)
	out, err := runGit(g.root, "merge", "--no-ff", "--no-edit",
		"-m", fmt.Sprintf("Merge feat %s of plan %s", feat, g.slug), branch)
	if err == nil {
		return nil
	}
	conflicts, _ := runGit(g.root, "diff", "--name-only", "--diff-filter=U")
	// Roll back before returning: leaving the base half-merged would make the next
	// feat's worktree, and the next merge, operate on a tree nobody chose.
	if _, abortErr := runGit(g.root, "merge", "--abort"); abortErr != nil {
		return fmt.Errorf("merging %s failed and could not be rolled back: %v: %s", branch, abortErr, out)
	}
	return &MergeConflictError{Feat: feat, Branch: branch, Files: gitLines(conflicts), Detail: out}
}

// Discard removes the feat's worktree and prunes git's administrative record of it.
// The branch is deliberately left alone: it carries the delivered work, it is what
// the PR is opened from, and deleting it would throw away the only durable copy of
// a feat whose merge has not been pushed anywhere yet.
func (g gitTrees) Discard(feat string) error {
	path := g.path(feat)
	// Removal is best-effort in the same way creation is: whether git still holds a
	// registration or the directory is already gone, the goal state is the same and
	// neither starting point is an error. Only a directory that survives all of it
	// is worth reporting.
	_, _ = runGit(g.root, "worktree", "remove", "--force", path)
	_, _ = runGit(g.root, "worktree", "prune")
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("could not remove the worktree for %s: %w", feat, err)
	}
	return nil
}

// branchExists reports whether the feat's branch is already in the repository — the
// signal that an earlier attempt left committed work behind, so this worktree must
// be restored from it rather than cut fresh from the base.
func (g gitTrees) branchExists(branch string) bool {
	_, err := runGit(g.root, "show-ref", "--verify", "refs/heads/"+branch)
	return err == nil
}

// preflightGit proves the repository can host the run and returns the branch feats
// are cut from and merged back into.
//
// Each check maps to a way the run would otherwise go wrong rather than fail:
// without git there is no isolation at all; outside a repository there is nothing to
// cut a worktree from; on a detached HEAD there is no branch to merge into; and with
// a dirty tree the merge would either refuse mid-run or sweep uncommitted work into
// a feat's history. All four are cheap to check now and expensive to discover on
// feat seven of thirty.
func preflightGit(root string) (string, error) {
	if !gitAvailable() {
		return "", fmt.Errorf("the `git` CLI is not available on PATH; the runner needs it to give each feat its own worktree")
	}
	repo, err := gitRepoRoot(root)
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository; `csdd plan run` isolates each feat in its own git worktree, so it needs one", root)
	}
	if !sameDir(repo, root) {
		return "", fmt.Errorf("the workspace root %s is not the git repository root (%s); run the plan from the repository root so feats merge into the tree you expect", root, repo)
	}
	base, err := gitCurrentBranch(root)
	if err != nil {
		return "", err
	}
	dirty, err := gitDirty(root)
	if err != nil {
		return "", err
	}
	if dirty = withoutRunnerState(dirty); len(dirty) > 0 {
		return "", fmt.Errorf("the repository has uncommitted changes and the run merges every delivered feat into %s; commit or stash them first (%d path(s), e.g. %s)",
			base, len(dirty), firstDirtyPath(dirty))
	}
	return base, nil
}

// withoutRunnerState drops the runner's own state directory from a dirty listing.
//
// .csdd/ holds the ledger, the session records and — now — the feats' worktrees. It
// is transient by design and the repository is expected to ignore it, but a
// workspace that does not would otherwise refuse every run for the runner's own
// bookkeeping, and would start reporting itself dirty MID-run as soon as the first
// worktree was created. Neither is the operator's uncommitted work, which is the
// only thing this check is protecting.
func withoutRunnerState(dirty []string) []string {
	var out []string
	for _, line := range dirty {
		path := filepath.ToSlash(firstDirtyPath([]string{line}))
		if path == paths.StateDir || path == paths.StateDir+"/" || strings.HasPrefix(path, paths.StateDir+"/") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// sameDir reports whether two paths name the same directory.
//
// Resolved through EvalSymlinks rather than compared as strings: on Windows a temp
// path routinely arrives in 8.3 short form (JOANDE~1.SIL) while git reports the long
// one, and those two spellings share almost no characters. Case-insensitive
// comparison is the last step, not the only one.
func sameDir(a, b string) bool {
	return strings.EqualFold(resolveDir(a), resolveDir(b))
}

func resolveDir(p string) string {
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		abs = filepath.Clean(p)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.ToSlash(abs)
}

// firstDirtyPath pulls the path out of git's porcelain status line ("XY <path>") for
// the error message, so the operator is pointed at a file rather than at a count.
func firstDirtyPath(dirty []string) string {
	line := dirty[0]
	if len(line) > 3 {
		return strings.TrimSpace(line[2:])
	}
	return line
}

// --- git plumbing --------------------------------------------------------------

// runGit executes one git command in dir and returns its combined output. Every git
// interaction in this package goes through it, so there is one place that decides
// how git is invoked — `-C` rather than a mutated process working directory, which
// matters when several feats are being set up around the same loop turn.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitAvailable reports whether the git CLI is on PATH.
func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// gitRepoRoot resolves the repository root containing dir, or an error when dir is
// not inside a repository at all.
func gitRepoRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository: %s", dir, out)
	}
	return filepath.Clean(strings.TrimSpace(out)), nil
}

// gitCurrentBranch reports the checked-out branch of root. A detached HEAD yields an
// error: the run needs a branch to merge into, and "HEAD" is not one.
func gitCurrentBranch(root string) (string, error) {
	out, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("could not read the current branch: %s", out)
	}
	branch := strings.TrimSpace(out)
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("the repository is on a detached HEAD; check out the branch the run should integrate into")
	}
	return branch, nil
}

// gitDirty returns the porcelain status lines of root, empty when the tree is clean.
func gitDirty(root string) ([]string, error) {
	out, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("could not read the repository status: %s", out)
	}
	return gitLines(out), nil
}

// splitLines splits git output into non-empty, trimmed lines.
func gitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
