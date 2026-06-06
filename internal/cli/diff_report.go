package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/session"
	"github.com/protonspy/csdd/internal/workspace"
)

// defaultMaxLines is the per-file recorded-line cap when --max-lines is omitted.
// It bounds the JSON artifact and the dashboard without distorting the summary
// (addition/deletion totals stay exact past the cap).
const defaultMaxLines = 2000

// specDiffReport records a structured diff of base → working tree for a spec into
// specs/<feature>/diff-report.json (the convention the dashboard's Changes view
// reads). It is the diff sibling of specTestReport: read-only git only, never
// touching the index or working tree.
func specDiffReport(args []string) int {
	fs := flag.NewFlagSet("spec diff-report", flag.ContinueOnError)
	var root, base string
	var maxLines int
	addRoot(fs, &root)
	fs.StringVar(&base, "base", "", "Base ref to diff against (default: auto-detect origin/main, main, origin/master, master). Diff is merge-base(HEAD, ref) → working tree.")
	fs.IntVar(&maxLines, "max-lines", defaultMaxLines, "Per-file recorded-line cap (0 = unlimited).")
	positionals, err := parseFlags(fs, args)
	if err != nil {
		return failOnFlagParse(err)
	}
	if len(positionals) < 1 {
		render.Err("usage: " + prog() + " spec diff-report FEATURE [--base REF] [--max-lines N]")
		return 1
	}
	feature := positionals[0]
	// Guard the positional before it is joined onto specs/ (no path traversal),
	// mirroring session.safeFeature on the read path.
	if feature == "" || strings.Contains(feature, "..") || strings.ContainsAny(feature, `/\`) {
		render.Err("invalid feature name: " + strconv.Quote(feature))
		return 1
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	sdir := filepath.Join(paths.Specs(r), feature)
	if !pathExists(sdir) {
		render.Err("spec not found: " + feature)
		return 1
	}
	if err := ensureGitRepo(r); err != nil {
		render.Err("diff reporting requires a git repository: " + err.Error())
		return 1
	}
	ref, sha, err := resolveBase(r, base)
	if err != nil {
		render.Err(err.Error())
		return 1
	}

	// Tracked changes: base → working tree (committed-on-branch + uncommitted).
	// sha is the resolved merge-base commit; trailing "--" separates it from pathspecs.
	patch, err := runGit(r, "diff", "--no-color", "-M", sha, "--")
	if err != nil {
		render.Err("git diff failed: " + err.Error())
		return 1
	}
	files := session.ParseUnifiedDiff(patch, maxLines)

	// Untracked, non-ignored files appear as additions (work in progress).
	untracked, err := runGit(r, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		render.Err("git ls-files failed: " + err.Error())
		return 1
	}
	for _, rel := range strings.Split(untracked, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full := filepath.Join(r, rel)
		// Do not follow symlinks: an untracked symlink could point outside the
		// repo (or at an ignored file) and leak its contents into the served diff.
		if fi, lerr := os.Lstat(full); lerr != nil || fi.Mode()&os.ModeSymlink != 0 {
			if lerr == nil {
				render.Warn("skip untracked symlink " + rel)
			}
			continue
		}
		content, rerr := os.ReadFile(full)
		if rerr != nil {
			render.Warn("skip untracked " + rel + ": " + rerr.Error())
			continue
		}
		files = append(files, session.AddedFile(rel, content, maxLines))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	diff := session.SpecDiff{
		Feature:   feature,
		UpdatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Base:      ref,
		BaseSHA:   sha,
		Head:      "working-tree",
		Summary:   session.Summarize(files),
		Files:     files,
	}
	for _, f := range files {
		if f.Truncated {
			diff.Truncated = true
			break
		}
	}

	b, err := json.MarshalIndent(diff, "", "  ")
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	target := filepath.Join(sdir, session.SpecDiffFile)
	if err := os.WriteFile(target, append(b, '\n'), 0o644); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.OK("wrote " + workspace.Relative(r, target))
	render.Info(fmt.Sprintf("base %s (%s) → working tree", ref, shortSHA(sha)))
	render.Info(fmt.Sprintf("%d file(s) · +%d · -%d", diff.Summary.Files, diff.Summary.Additions, diff.Summary.Deletions))
	if diff.Truncated {
		render.Warn("some files truncated at --max-lines=" + strconv.Itoa(maxLines))
	}
	return 0
}

// runGit runs a read-only git subcommand in dir and returns its stdout. git is
// invoked with an argv slice (no shell), and only read-only subcommands are ever
// passed, so the index and working tree are never modified.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return out.String(), fmt.Errorf("%s", msg)
		}
		return out.String(), err
	}
	return out.String(), nil
}

// ensureGitRepo reports whether dir is inside a git work tree.
func ensureGitRepo(dir string) error {
	out, err := runGit(dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "true" {
		return fmt.Errorf("not a git work tree")
	}
	return nil
}

// resolveBase resolves the diff base. With an override it uses that ref; without
// one it picks the first existing of origin/main, main, origin/master, master.
// It returns the ref name and the merge-base commit with HEAD.
func resolveBase(dir, override string) (string, string, error) {
	candidates := []string{"origin/main", "main", "origin/master", "master"}
	if override != "" {
		candidates = []string{override}
	}
	ref := ""
	for _, c := range candidates {
		// --end-of-options so a ref beginning with '-' can never be parsed as a git option.
		if _, err := runGit(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", c+"^{commit}"); err == nil {
			ref = c
			break
		}
	}
	if ref == "" {
		if override != "" {
			return "", "", fmt.Errorf("unknown base ref: %s", override)
		}
		return "", "", fmt.Errorf("could not auto-detect a base ref (looked for origin/main, main, origin/master, master); pass --base")
	}
	sha, err := runGit(dir, "merge-base", "--end-of-options", "HEAD", ref)
	if err != nil {
		return "", "", fmt.Errorf("no merge-base between HEAD and %s: %s", ref, err)
	}
	return ref, strings.TrimSpace(sha), nil
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
