package cli

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/workspace"
)

// runClean removes the *-N.old backups that `csdd update` leaves next to a
// managed file when it overrides a version the user had edited. It only ever
// deletes backups of files csdd manages — never user content — by globbing the
// known managed paths, so it cannot touch an unrelated *.old file.
func runClean(args []string, templates embed.FS) int {
	fset := flag.NewFlagSet("clean", flag.ContinueOnError)
	var root string
	var dryRun bool
	addRoot(fset, &root)
	fset.BoolVar(&dryRun, "dry-run", false, "List the .old backups without deleting them.")
	if err := fset.Parse(args); err != nil {
		return failOnFlagParse(err)
	}
	if err := rejectPositionals("clean", fset); err != nil {
		render.Err(err.Error())
		return 1
	}

	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if info, statErr := os.Stat(paths.Claude(r)); statErr != nil || !info.IsDir() {
		render.Err("not a csdd workspace (no .claude/ at " + r + "). Run `" + prog() + " init` first.")
		return 1
	}

	files, err := collectManagedFiles(r, templates)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	var olds []string
	for _, f := range files {
		matches, _ := filepath.Glob(f.Abs + "-*.old")
		olds = append(olds, matches...)
	}
	sort.Strings(olds)

	if len(olds) == 0 {
		render.OK("no .old backups to clean.")
		return 0
	}

	removed := 0
	for _, p := range olds {
		rel := workspace.Relative(r, p)
		if dryRun {
			render.Info("would remove " + rel)
			continue
		}
		if err := os.Remove(p); err != nil {
			render.Warn("could not remove " + rel + ": " + err.Error())
			continue
		}
		render.Info("removed " + rel)
		removed++
	}

	if dryRun {
		render.OK(fmt.Sprintf("Dry run: %d .old backup(s) would be removed. Re-run without --dry-run to delete.", len(olds)))
	} else {
		render.OK(fmt.Sprintf("Cleaned %d .old backup(s).", removed))
	}
	return 0
}
