package cli

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/protonspy/csdd/internal/manifest"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

// managedFile is one csdd-owned artifact that `csdd update` reconciles: pure
// shipped content the binary embeds. User-owned files — steering bodies, specs,
// .mcp.json, settings.json, CLAUDE.md, and any custom (non-shipped) skill or
// agent — are deliberately NOT collected here, so update can never clobber them.
type managedFile struct {
	Rel     string // workspace-relative, forward-slash: manifest key + display name
	Abs     string // absolute on-disk path
	Content string // the content this csdd version ships for the file
	Exec    bool   // chmod 0755 after writing (hook scripts)
}

// collectManagedFiles enumerates every pure-csdd artifact `csdd init` scaffolds
// from the embedded template tree: generation rules, versioned templates, the
// shipped skills/agents/commands/hooks, the canonical guide, and csdd.md.
func collectManagedFiles(root string, templates embed.FS) ([]managedFile, error) {
	var out []managedFile
	add := func(abs, content string, exec bool) {
		out = append(out, managedFile{
			Rel:     filepath.ToSlash(workspace.Relative(root, abs)),
			Abs:     abs,
			Content: content,
			Exec:    exec,
		})
	}

	rules, err := templater.RuleFiles(templates)
	if err != nil {
		return nil, err
	}
	for name, c := range rules {
		add(filepath.Join(paths.Rules(root), name), c, false)
	}

	versioned, err := templater.WorkflowTemplateFiles(templates)
	if err != nil {
		return nil, err
	}
	for rel, c := range versioned {
		add(filepath.Join(paths.Templates(root), filepath.FromSlash(rel)), c, false)
	}

	trees := []struct {
		base string
		fn   func(fs.FS) (map[string]string, error)
		exec bool
	}{
		{paths.Skills(root), templater.SkillFiles, false},
		{paths.Agents(root), templater.AgentFiles, false},
		{paths.Commands(root), templater.CommandFiles, false},
		{paths.Hooks(root), templater.HookFiles, true},
	}
	for _, t := range trees {
		entries, err := t.fn(templates)
		if err != nil {
			return nil, err
		}
		for rel, c := range entries {
			add(filepath.Join(t.base, filepath.FromSlash(rel)), c, t.exec)
		}
	}

	guide, err := templater.Static(templates, "templates/guides/claude-code-sdd.md.tmpl")
	if err != nil {
		return nil, err
	}
	add(filepath.Join(root, "docs", "guides", "claude-code-sdd.md"), guide, false)

	csddmd, err := templater.Static(templates, "templates/root/csdd.md.tmpl")
	if err != nil {
		return nil, err
	}
	add(filepath.Join(root, "csdd.md"), csddmd, false)

	// Deterministic order so dry-run previews and reports are stable.
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// recordManifest rewrites the workspace manifest to record the shipped-content
// hash of every managed file for this csdd version. Both `init` and `update`
// call it so the baseline always reflects what csdd last wrote — which is what
// the next update compares the user's on-disk files against.
func recordManifest(root string, templates embed.FS, now time.Time) error {
	files, err := collectManagedFiles(root, templates)
	if err != nil {
		return err
	}
	m := manifest.New()
	for _, f := range files {
		m.Files[f.Rel] = manifest.Hash(f.Content)
	}
	return m.Save(paths.Manifest(root), version, now)
}

type updateOptions struct {
	root   string
	dryRun bool
	force  bool
}

func runUpdate(args []string, templates embed.FS) int {
	fset := flag.NewFlagSet("update", flag.ContinueOnError)
	var opts updateOptions
	addRoot(fset, &opts.root)
	fset.BoolVar(&opts.dryRun, "dry-run", false, "Preview changes without writing anything.")
	fset.BoolVar(&opts.force, "force", false, "Overwrite your edits in place WITHOUT creating .old backups.")
	if err := fset.Parse(args); err != nil {
		return failOnFlagParse(err)
	}

	root, err := workspace.Resolve(opts.root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if info, statErr := os.Stat(paths.Claude(root)); statErr != nil || !info.IsDir() {
		render.Err("not a csdd workspace (no .claude/ at " + root + "). Run `" + prog() + " init` first.")
		return 1
	}
	opts.root = root

	res, err := updateWorkspace(opts, templates, time.Now())
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	res.report(opts.dryRun)
	return 0
}

// changeKind classifies what update did (or would do) to one managed file.
type changeKind int

const (
	kindCurrent  changeKind = iota // on disk and already the shipped version
	kindAdded                      // missing on disk, written fresh
	kindUpdated                    // pristine but outdated, refreshed in place
	kindConflict                   // user-edited; new version installed, old kept as .old
)

type fileChange struct {
	rel    string
	kind   changeKind
	backup string // workspace-relative .old path created (conflict, non-force)
}

type updateResult struct {
	changes  []fileChange
	firstRun bool // no manifest existed: baselines are unknown this run
}

// updateWorkspace reconciles every managed file against its shipped content and
// the recorded baseline, applying the safety policy:
//
//   - missing            → write it (a new artifact in this version)
//   - identical          → leave it (already current)
//   - pristine, outdated → overwrite in place (the user never touched it)
//   - edited by the user → keep the user's copy as <file>-N.old, then write the
//     new version (so nothing is lost and the workspace still moves forward)
//
// --force skips the .old backup; --dry-run computes the plan without writing.
func updateWorkspace(opts updateOptions, templates embed.FS, now time.Time) (updateResult, error) {
	var res updateResult
	files, err := collectManagedFiles(opts.root, templates)
	if err != nil {
		return res, err
	}
	base, existed, err := manifest.Load(paths.Manifest(opts.root))
	if err != nil {
		return res, err
	}
	res.firstRun = !existed

	for _, f := range files {
		shipped := manifest.Hash(f.Content)

		diskBytes, rerr := os.ReadFile(f.Abs)
		if os.IsNotExist(rerr) {
			if !opts.dryRun {
				if err := writeManaged(f); err != nil {
					return res, err
				}
			}
			res.changes = append(res.changes, fileChange{rel: f.Rel, kind: kindAdded})
			continue
		}
		if rerr != nil {
			return res, rerr
		}

		diskHash := manifest.Hash(string(diskBytes))
		if diskHash == shipped {
			res.changes = append(res.changes, fileChange{rel: f.Rel, kind: kindCurrent})
			continue
		}

		if known, ok := base.Files[f.Rel]; ok && diskHash == known {
			// Disk matches the last baseline csdd wrote: the user never edited it,
			// so refreshing in place loses nothing.
			if !opts.dryRun {
				if err := writeManaged(f); err != nil {
					return res, err
				}
			}
			res.changes = append(res.changes, fileChange{rel: f.Rel, kind: kindUpdated})
			continue
		}

		// Conflict: the file differs from both the shipped version and the known
		// baseline (or there is no baseline). Preserve the user's copy as .old.
		ch := fileChange{rel: f.Rel, kind: kindConflict}
		if !opts.force {
			old := nextOldPath(f.Abs)
			ch.backup = filepath.ToSlash(workspace.Relative(opts.root, old))
			if !opts.dryRun {
				if err := os.WriteFile(old, diskBytes, 0o644); err != nil {
					return res, err
				}
			}
		}
		if !opts.dryRun {
			if err := writeManaged(f); err != nil {
				return res, err
			}
		}
		res.changes = append(res.changes, ch)
	}

	if !opts.dryRun {
		if err := recordManifest(opts.root, templates, now); err != nil {
			return res, err
		}
	}
	return res, nil
}

// writeManaged writes a managed file's shipped content, creating parent dirs and
// restoring the executable bit for hook scripts.
func writeManaged(f managedFile) error {
	if err := os.MkdirAll(filepath.Dir(f.Abs), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(f.Abs, []byte(f.Content), 0o644); err != nil {
		return err
	}
	if f.Exec {
		_ = os.Chmod(f.Abs, 0o755)
	}
	return nil
}

// nextOldPath returns the first free <abs>-N.old (N counting up from 1), so a
// file conflicting across several updates accrues -1.old, -2.old, … rather than
// overwriting an earlier backup. It only stats paths; it never writes.
func nextOldPath(abs string) string {
	for n := 1; ; n++ {
		cand := fmt.Sprintf("%s-%d.old", abs, n)
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
}

func (r updateResult) report(dryRun bool) {
	var added, updated, current, conflicts int
	for _, c := range r.changes {
		switch c.kind {
		case kindAdded:
			added++
		case kindUpdated:
			updated++
		case kindCurrent:
			current++
		case kindConflict:
			conflicts++
		}
	}

	if r.firstRun {
		render.Warn("No manifest found (first update). Recording a baseline now; files that differ from this version are preserved as -N.old before the new version is written.")
	}

	for _, c := range r.changes {
		switch c.kind {
		case kindAdded:
			render.Info("+ add      " + c.rel)
		case kindUpdated:
			render.Info("~ update   " + c.rel)
		case kindConflict:
			if c.backup != "" {
				render.Info("! merge    " + c.rel + "  (your version kept → " + c.backup + ")")
			} else {
				render.Info("! overwrite " + c.rel + "  (--force; no backup)")
			}
		}
	}

	verb := "Update complete"
	if dryRun {
		verb = "Dry run"
	}
	render.OK(fmt.Sprintf("%s: %d added, %d updated, %d unchanged, %d conflict(s).", verb, added, updated, current, conflicts))

	switch {
	case dryRun:
		render.Info("No files were written. Re-run without --dry-run to apply.")
	case conflicts > 0:
		render.Info("Review the .old backups, fold in anything you customized, then delete them.")
	}
}
