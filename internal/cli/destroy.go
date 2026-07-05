package cli

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/workspace"
)

// runDestroy tears the csdd workspace back down to a clean slate — the inverse
// of `csdd init`. It removes the entire .claude/ tree plus the pre-push hook
// and, by explicit design choice, the shared entry files CLAUDE.md and
// .mcp.json in full. It deliberately PRESERVES specs/, which holds the user's
// real specification work.
//
// Because the deletion is irreversible — and CLAUDE.md/.mcp.json may carry
// content beyond csdd's own — it always prints a destructive-action alert and
// requires either an interactive "yes" or --force before touching the disk.
// --dry-run previews the exact set without deleting anything.
func runDestroy(args []string, _ embed.FS) int {
	fset := flag.NewFlagSet("destroy", flag.ContinueOnError)
	var root string
	var dryRun bool
	var force bool
	addRoot(fset, &root)
	fset.BoolVar(&dryRun, "dry-run", false, "List what would be removed without deleting anything.")
	addForce(fset, &force)
	if err := fset.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	if err := rejectPositionals("destroy", fset); err != nil {
		render.Err(err.Error())
		return 1
	}

	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if info, statErr := os.Stat(paths.Claude(r)); statErr != nil || !info.IsDir() {
		render.Err("not a csdd workspace (no .claude/ at " + r + "). Nothing to destroy.")
		return 1
	}

	// Only the targets that actually exist, so the preview and the summary count
	// reflect reality on a partially-scaffolded workspace.
	var present []string
	for _, t := range destroyTargets(r) {
		if pathExists(t) {
			present = append(present, t)
		}
	}
	if len(present) == 0 {
		render.OK("nothing to remove; the csdd workspace is already gone.")
		return 0
	}

	// The destructive-action alert: shown on every path (dry-run included) so the
	// user always sees exactly what is at stake before anything is deleted.
	render.Warn(render.Bold("DESTRUCTIVE — this permanently deletes the csdd workspace and cannot be undone."))
	render.Info("workspace: " + r)
	render.Info("the following will be removed:")
	for _, t := range present {
		render.Info("  - " + workspace.Relative(r, t))
	}
	if pathExists(paths.Specs(r)) {
		render.Info(render.Green("preserved") + ": specs/  (your specification work is kept)")
	}
	render.Warn("CLAUDE.md and .mcp.json are deleted in full, including any content you added beyond csdd.")

	if dryRun {
		render.OK(fmt.Sprintf("Dry run: %d item(s) would be removed. Re-run with --force to delete.", len(present)))
		return 0
	}

	if !force {
		if !confirm("Permanently delete this csdd workspace (specs/ is kept)?") {
			render.Warn("aborted; nothing was removed. Pass --force to skip this prompt.")
			return 1
		}
	}

	removed := 0
	for _, t := range present {
		rel := workspace.Relative(r, t)
		if err := os.RemoveAll(t); err != nil {
			render.Warn("could not remove " + rel + ": " + err.Error())
			continue
		}
		render.Info("removed " + rel)
		removed++
	}
	// Sweep up dirs that only existed to hold a csdd file, but leave any the user
	// has put their own content into (removeIfEmpty is a no-op on non-empty dirs).
	removeIfEmpty(filepath.Join(r, ".githooks"))

	render.OK(fmt.Sprintf("Destroyed csdd workspace: %d item(s) removed. specs/ preserved. Run `%s init` to scaffold again.", removed, prog()))
	return 0
}

// destroyTargets is the ordered set of paths `csdd destroy` removes, matching the
// scaffold `csdd init` lays down minus specs/. .claude/ is removed as a whole
// tree (rules, templates, shipped skills/agents/commands/hooks, steering,
// settings.json, manifest), so individual sub-paths need not be listed.
func destroyTargets(root string) []string {
	return []string{
		paths.Claude(root), // entire .claude/ tree
		paths.Entry(root),  // CLAUDE.md (root) — deleted in full
		paths.MCP(root),    // .mcp.json (root) — deleted in full
		filepath.Join(root, ".githooks", "pre-push"),
	}
}

// removeIfEmpty deletes dir only when it has no remaining entries, so destroy can
// tidy now-orphaned scaffold directories without ever discarding user content.
func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(dir)
}
