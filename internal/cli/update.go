package cli

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/frontmatter"
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
	Rel                 string   // workspace-relative, forward-slash: manifest key + display name
	Abs                 string   // absolute on-disk path
	Content             string   // the content this csdd version ships for the file
	Exec                bool     // chmod 0755 after writing (hook scripts)
	PreserveFrontmatter []string // local scalar overrides update carries forward
}

var managedExecutionOverrideKeys = []string{"model", "effort"}

// collectManagedFiles enumerates every pure-csdd artifact `csdd init` scaffolds
// from the embedded template tree: generation rules, versioned templates, the
// shipped skills/agents/commands/hooks, the canonical guide, and csdd.md.
func collectManagedFiles(root string, templates embed.FS) ([]managedFile, error) {
	var out []managedFile
	add := func(abs, content string, exec bool, preserveFrontmatter []string) {
		out = append(out, managedFile{
			Rel:                 filepath.ToSlash(workspace.Relative(root, abs)),
			Abs:                 abs,
			Content:             content,
			Exec:                exec,
			PreserveFrontmatter: preserveFrontmatter,
		})
	}

	rules, err := templater.RuleFiles(templates)
	if err != nil {
		return nil, err
	}
	for name, c := range rules {
		add(filepath.Join(paths.Rules(root), name), c, false, nil)
	}

	versioned, err := templater.WorkflowTemplateFiles(templates)
	if err != nil {
		return nil, err
	}
	for rel, c := range versioned {
		add(filepath.Join(paths.Templates(root), filepath.FromSlash(rel)), c, false, nil)
	}

	trees := []struct {
		base                string
		fn                  func(fs.FS) (map[string]string, error)
		exec                bool
		preserveFrontmatter []string
	}{
		{paths.Skills(root), templater.SkillFiles, false, managedExecutionOverrideKeys},
		{paths.Agents(root), templater.AgentFiles, false, managedExecutionOverrideKeys},
		{paths.Commands(root), templater.CommandFiles, false, nil},
		{paths.Hooks(root), templater.HookFiles, true, nil},
	}
	for _, t := range trees {
		entries, err := t.fn(templates)
		if err != nil {
			return nil, err
		}
		for rel, c := range entries {
			add(filepath.Join(t.base, filepath.FromSlash(rel)), c, t.exec, t.preserveFrontmatter)
		}
	}

	guide, err := templater.Static(templates, "templates/guides/claude-code-sdd.md.tmpl")
	if err != nil {
		return nil, err
	}
	add(filepath.Join(root, "docs", "guides", "claude-code-sdd.md"), guide, false, nil)

	csddmd, err := templater.Static(templates, "templates/root/csdd.md.tmpl")
	if err != nil {
		return nil, err
	}
	add(filepath.Join(root, "csdd.md"), csddmd, false, nil)

	// Deterministic order so dry-run previews and reports are stable.
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// recordManifest rewrites the workspace manifest to record the shipped-content
// hash of every managed file for this csdd version. Both `init` and `update`
// call it so the baseline always reflects what csdd last wrote — which is what
// the next update compares the user's on-disk files against. Files in skipped
// (conflicts the user declined to override) keep their prior baseline, so they
// are re-detected as conflicts on the next run.
func recordManifest(root string, templates embed.FS, now time.Time, skipped map[string]bool) error {
	files, err := collectManagedFiles(root, templates)
	if err != nil {
		return err
	}
	prior, _, _ := manifest.Load(paths.Manifest(root))
	m := manifest.New()
	for _, f := range files {
		if skipped[f.Rel] {
			if h, ok := prior.Files[f.Rel]; ok {
				m.Files[f.Rel] = h
				continue
			}
		}
		m.Files[f.Rel] = managedBaselineHash(f, f.Content)
	}
	return m.Save(paths.Manifest(root), version, now)
}

type updateOptions struct {
	root          string
	dryRun        bool
	force         bool
	yes           bool
	skipConflicts bool // internal: leave user-edited conflicts untouched
}

func runUpdate(args []string, templates embed.FS) int {
	fset := flag.NewFlagSet("update", flag.ContinueOnError)
	var opts updateOptions
	addRoot(fset, &opts.root)
	fset.BoolVar(&opts.dryRun, "dry-run", false, "Preview changes without writing anything.")
	fset.BoolVar(&opts.force, "force", false, "Overwrite your edits in place WITHOUT creating .old backups.")
	fset.BoolVar(&opts.yes, "yes", false, "Override user-edited files without the interactive confirmation (their version is still kept as .old).")
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
	now := time.Now()

	// Plan first (no writes) so we can confirm before overriding any file the
	// user edited. On a dry run, just show the plan.
	plan, err := updateWorkspace(updateOptions{root: root, dryRun: true, force: opts.force}, templates, now)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if opts.dryRun {
		plan.report(true)
		return 0
	}

	// When the user edited files that this version changed, ask before clobbering
	// them — but only interactively. Non-interactive runs (CI, agents) keep the
	// safe-by-backup behaviour: apply and preserve the old copy as .old.
	conflicts := plan.countConflicts()
	if conflicts > 0 && !opts.force && !opts.yes && stdinIsInteractive() {
		render.Warn(fmt.Sprintf("%d file(s) you edited differ from this version and would be overridden:", conflicts))
		for _, c := range plan.changes {
			if c.kind == kindConflict {
				render.Info("  ! " + c.rel)
			}
		}
		render.Info("Your current versions are saved as .old before the new ones are written.")
		if !confirm("Override these files?") {
			opts.skipConflicts = true
			render.Info("Keeping your versions untouched. Re-run with --yes to override.")
		}
	}

	res, err := updateWorkspace(opts, templates, now)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	res.report(false)
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
	rel     string
	kind    changeKind
	backup  string // workspace-relative .old path created (conflict, non-force)
	skipped bool   // conflict left untouched (user declined the override)
}

type updateResult struct {
	changes  []fileChange
	firstRun bool // no manifest existed: baselines are unknown this run
}

// countConflicts returns how many managed files differ from this version and the
// recorded baseline (i.e. the user edited them).
func (r updateResult) countConflicts() int {
	n := 0
	for _, c := range r.changes {
		if c.kind == kindConflict {
			n++
		}
	}
	return n
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
		shipped := managedBaselineHash(f, f.Content)

		diskBytes, rerr := os.ReadFile(f.Abs)
		if os.IsNotExist(rerr) {
			if !opts.dryRun {
				if err := writeManaged(f, f.Content); err != nil {
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
		diskBaselineHash := managedBaselineHash(f, string(diskBytes))
		writeContent := managedWriteContent(f, string(diskBytes))
		if diskHash == manifest.Hash(writeContent) || diskBaselineHash == shipped {
			res.changes = append(res.changes, fileChange{rel: f.Rel, kind: kindCurrent})
			continue
		}

		if known, ok := base.Files[f.Rel]; ok && diskBaselineHash == known {
			// Disk matches the last baseline csdd wrote: the user never edited it,
			// so refreshing in place loses nothing.
			if !opts.dryRun {
				if err := writeManaged(f, writeContent); err != nil {
					return res, err
				}
			}
			res.changes = append(res.changes, fileChange{rel: f.Rel, kind: kindUpdated})
			continue
		}

		// Conflict: the file differs from both the shipped version and the known
		// baseline (or there is no baseline).
		ch := fileChange{rel: f.Rel, kind: kindConflict}
		if opts.skipConflicts {
			// The user declined the override: leave their file exactly as-is.
			ch.skipped = true
			res.changes = append(res.changes, ch)
			continue
		}
		// Preserve the user's copy as .old, then write the new version.
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
			if err := writeManaged(f, writeContent); err != nil {
				return res, err
			}
		}
		res.changes = append(res.changes, ch)
	}

	if !opts.dryRun {
		skipped := map[string]bool{}
		for _, c := range res.changes {
			if c.skipped {
				skipped[c.rel] = true
			}
		}
		if err := recordManifest(opts.root, templates, now, skipped); err != nil {
			return res, err
		}
	}
	return res, nil
}

// managedBaselineHash returns the hash used for manifest and pristine checks.
// For managed agents and skills, model/effort are local execution overrides:
// update must preserve them, but they should not make an otherwise-pristine
// shipped artifact look user-edited.
func managedBaselineHash(f managedFile, content string) string {
	return manifest.Hash(stripFrontmatterFields(content, f.PreserveFrontmatter))
}

// managedWriteContent overlays preserved frontmatter scalar values from the
// existing file onto this version's shipped content.
func managedWriteContent(f managedFile, existing string) string {
	if len(f.PreserveFrontmatter) == 0 {
		return f.Content
	}
	fm := frontmatter.Parse(existing)
	values := map[string]string{}
	for _, key := range f.PreserveFrontmatter {
		if value := fm.AsString(key, ""); value != "" {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return f.Content
	}
	return upsertFrontmatterFields(f.Content, f.PreserveFrontmatter, values)
}

// writeManaged writes a managed file's effective content, creating parent dirs and
// restoring the executable bit for hook scripts.
func writeManaged(f managedFile, content string) error {
	if err := os.MkdirAll(filepath.Dir(f.Abs), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(f.Abs, []byte(content), 0o644); err != nil {
		return err
	}
	if f.Exec {
		_ = os.Chmod(f.Abs, 0o755)
	}
	return nil
}

func stripFrontmatterFields(content string, keys []string) string {
	if len(keys) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	end := frontmatterEnd(lines)
	if end < 0 {
		return content
	}
	keySet := stringSet(keys)
	out := make([]string, 0, len(lines))
	out = append(out, lines[0])
	for _, line := range lines[1:end] {
		if key, ok := frontmatterLineKey(line); ok && keySet[key] {
			continue
		}
		out = append(out, line)
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

func upsertFrontmatterFields(content string, order []string, values map[string]string) string {
	lines := strings.Split(content, "\n")
	end := frontmatterEnd(lines)
	if end < 0 {
		return content
	}
	keySet := stringSet(order)
	out := make([]string, 0, len(lines)+len(values))
	out = append(out, lines[0])
	for _, line := range lines[1:end] {
		if key, ok := frontmatterLineKey(line); ok && keySet[key] {
			continue
		}
		out = append(out, line)
	}
	for _, key := range order {
		if value, ok := values[key]; ok {
			out = append(out, key+": "+value)
		}
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

func frontmatterEnd(lines []string) int {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i
		}
	}
	return -1
}

func frontmatterLineKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", false
	}
	return strings.TrimSpace(line[:idx]), true
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
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
	var added, updated, current, conflicts, skipped int
	for _, c := range r.changes {
		switch c.kind {
		case kindAdded:
			added++
		case kindUpdated:
			updated++
		case kindCurrent:
			current++
		case kindConflict:
			if c.skipped {
				skipped++
			} else {
				conflicts++
			}
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
			switch {
			case c.skipped:
				render.Info("! skip     " + c.rel + "  (kept your version; --yes to override)")
			case c.backup != "":
				render.Info("! merge    " + c.rel + "  (your version kept → " + c.backup + ")")
			default:
				render.Info("! overwrite " + c.rel + "  (--force; no backup)")
			}
		}
	}

	verb := "Update complete"
	if dryRun {
		verb = "Dry run"
	}
	render.OK(fmt.Sprintf("%s: %d added, %d updated, %d unchanged, %d conflict(s), %d skipped.", verb, added, updated, current, conflicts, skipped))

	switch {
	case dryRun:
		render.Info("No files were written. Re-run without --dry-run to apply.")
	case conflicts > 0:
		render.Info("Review the .old backups, fold in anything you customized, then delete them (`" + prog() + " clean`).")
	case skipped > 0:
		render.Info("Skipped your edited files. Re-run with --yes to take the new versions (yours kept as .old).")
	}
}
