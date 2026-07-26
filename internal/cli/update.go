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
// from the embedded template tree: generation rules, versioned templates, and
// the shipped skills/agents/commands/hooks.
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
	}
	// Hooks are opt-in at init time, so only manage/refresh them when the
	// workspace actually has them — otherwise `csdd update` would silently re-add
	// hooks a project deliberately omitted.
	if pathExists(paths.Hooks(root)) {
		trees = append(trees, struct {
			base                string
			fn                  func(fs.FS) (map[string]string, error)
			exec                bool
			preserveFrontmatter []string
		}{paths.Hooks(root), templater.HookFiles, true, nil})
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

	// Deterministic order so dry-run previews and reports are stable.
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// loadWorkspaceManifest reads the csdd-managed manifest, preferring the new
// .csdd/manifest.json home and falling back to the legacy
// .claude/.csdd-manifest.json when the new path is absent (R14.1). Workspaces
// that only have the legacy file keep working unchanged (R14.2).
func loadWorkspaceManifest(root string) (*manifest.Manifest, bool, error) {
	m, exists, err := manifest.Load(paths.StateManifest(root))
	if err != nil {
		return nil, false, err
	}
	if exists {
		return m, true, nil
	}
	return manifest.Load(paths.Manifest(root))
}

// saveWorkspaceManifest always writes to the new .csdd/manifest.json path, so the
// next save transparently migrates a legacy workspace forward (R14.1).
func saveWorkspaceManifest(root string, m *manifest.Manifest, now time.Time) error {
	return m.Save(paths.StateManifest(root), version, now)
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
	prior, _, _ := loadWorkspaceManifest(root)
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
	return saveWorkspaceManifest(root, m, now)
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
	if err := rejectPositionals("update", fset); err != nil {
		render.Err(err.Error())
		return 1
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
	if conflicts > 0 && !opts.force && !opts.yes {
		render.Warn(fmt.Sprintf("%d file(s) you edited differ from this version:", conflicts))
		for _, c := range plan.changes {
			if c.kind == kindConflict {
				render.Info("  ! " + c.rel)
			}
		}
		render.Info("Your current versions are saved as .old before the new ones are written.")
		if stdinIsInteractive() {
			if !confirm("Override these files?") {
				opts.skipConflicts = true
				render.Info("Keeping your versions untouched. Re-run with --yes to override.")
			}
		} else {
			// Non-interactive (CI, an agent driving the CLI): proceed, but never
			// silently — the conflicts were just listed above and each edited file
			// is preserved as .old. Pass --yes to suppress this notice, or answer
			// interactively to choose per run.
			render.Info("Non-interactive: applying new versions; your edits are kept as .old. (Pass --yes to acknowledge, or run interactively to choose.)")
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
	kindRemoved                    // no longer shipped and never edited, deleted
	kindOrphaned                   // no longer shipped but edited, left in place
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
	base, existed, err := loadWorkspaceManifest(opts.root)
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
		// Preserve the user's copy as .old, then write the new version. Carry the
		// original file's mode over so a backed-up hook script stays executable.
		if !opts.force {
			old := nextOldPath(f.Abs)
			ch.backup = filepath.ToSlash(workspace.Relative(opts.root, old))
			if !opts.dryRun {
				mode := os.FileMode(0o644)
				if info, statErr := os.Stat(f.Abs); statErr == nil {
					mode = info.Mode().Perm()
				}
				if err := os.WriteFile(old, diskBytes, mode); err != nil {
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

	pruned, err := pruneRetired(opts, base, files)
	if err != nil {
		return res, err
	}
	res.changes = append(res.changes, pruned...)

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

// pruneRetired removes the artifacts the manifest records but this csdd version
// no longer ships.
//
// Without it `update` only ever added. Retiring a skill or an agent upstream left
// every existing workspace carrying it forever: the file stayed on disk, the
// model kept seeing it in the skills listing, and nothing ever said it was gone.
// The manifest already stopped listing them — recordManifest rebuilds from the
// current shipped set — so the entry vanished while the file it described did
// not, which is the one state that makes the record useless.
//
// The policy mirrors the update policy exactly, read in the other direction:
//
//   - pristine (disk still matches the recorded baseline) → delete it. csdd wrote
//     it, csdd owns it, nobody changed it.
//   - edited → keep it and say so. The user made it theirs; deleting their work
//     because upstream lost interest is not an upgrade.
//   - already gone → nothing to do.
//
// --dry-run reports the plan without touching anything.
func pruneRetired(opts updateOptions, base *manifest.Manifest, files []managedFile) ([]fileChange, error) {
	shipped := make(map[string]bool, len(files))
	for _, f := range files {
		shipped[f.Rel] = true
	}
	retired := make([]string, 0, len(base.Files))
	for rel := range base.Files {
		if !shipped[rel] {
			retired = append(retired, rel)
		}
	}
	sort.Strings(retired) // deterministic output; map order is not

	var out []fileChange
	for _, rel := range retired {
		abs := filepath.Join(opts.root, filepath.FromSlash(rel))
		diskBytes, rerr := os.ReadFile(abs)
		if os.IsNotExist(rerr) {
			continue // the manifest outlived the file; recordManifest drops the entry
		}
		if rerr != nil {
			return nil, rerr
		}
		if !retiredIsPristine(string(diskBytes), base.Files[rel]) {
			out = append(out, fileChange{rel: rel, kind: kindOrphaned})
			continue
		}
		if !opts.dryRun {
			if err := os.Remove(abs); err != nil {
				return nil, err
			}
			pruneEmptyParents(opts.root, abs)
		}
		out = append(out, fileChange{rel: rel, kind: kindRemoved})
	}
	return out, nil
}

// retiredIsPristine reports whether on-disk content still matches the baseline
// the manifest recorded for a retired artifact.
//
// Both the raw hash and the execution-override-stripped hash are accepted,
// because the baseline was written with whichever form that artifact's kind used
// — and the kind is exactly what a retired artifact no longer tells us, since it
// is gone from the shipped set. Stripping is a no-op on content without those
// frontmatter keys, so trying it costs nothing and never widens what counts as
// pristine beyond "equal to what csdd wrote, give or take the local model/effort
// overrides csdd itself sanctions".
func retiredIsPristine(content, baseline string) bool {
	if baseline == "" {
		return false // no baseline to compare against: treat as the user's
	}
	if manifest.Hash(content) == baseline {
		return true
	}
	return manifest.Hash(stripFrontmatterFields(content, managedExecutionOverrideKeys)) == baseline
}

// pruneEmptyParents removes directories left empty by a deletion, up to (but
// never including) the workspace root. A retired skill is a whole directory —
// deleting its SKILL.md and leaving the folder behind still shows the skill to
// anyone listing the tree.
func pruneEmptyParents(root, abs string) {
	dir := filepath.Dir(abs)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for {
		d, err := filepath.Abs(dir)
		if err != nil || d == rootAbs || !strings.HasPrefix(d, rootAbs+string(filepath.Separator)) {
			return
		}
		entries, err := os.ReadDir(d)
		if err != nil || len(entries) > 0 {
			return
		}
		if os.Remove(d) != nil {
			return
		}
		dir = filepath.Dir(d)
	}
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
	var added, updated, current, conflicts, skipped, removed, orphaned int
	for _, c := range r.changes {
		switch c.kind {
		case kindAdded:
			added++
		case kindUpdated:
			updated++
		case kindCurrent:
			current++
		case kindRemoved:
			removed++
		case kindOrphaned:
			orphaned++
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
		case kindRemoved:
			render.Info("- remove   " + c.rel + "  (no longer shipped)")
		case kindOrphaned:
			render.Info("? orphan   " + c.rel + "  (no longer shipped, but you edited it — kept)")
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
	summary := fmt.Sprintf("%s: %d added, %d updated, %d unchanged, %d conflict(s), %d skipped.", verb, added, updated, current, conflicts, skipped)
	// Retirements are appended rather than folded into the counts above, so a run
	// that removes nothing reads exactly as it always did.
	if removed > 0 || orphaned > 0 {
		summary += fmt.Sprintf(" %d removed, %d orphaned.", removed, orphaned)
	}
	render.OK(summary)

	switch {
	case dryRun:
		render.Info("No files were written. Re-run without --dry-run to apply.")
	case orphaned > 0:
		render.Info("Orphaned files are no longer shipped by csdd but you had edited them — they are yours now; delete them if you no longer want them.")
	case conflicts > 0:
		render.Info("Review the .old backups, fold in anything you customized, then delete them (`" + prog() + " clean`).")
	case skipped > 0:
		render.Info("Skipped your edited files. Re-run with --yes to take the new versions (yours kept as .old).")
	}
}
