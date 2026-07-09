package plan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/manifest"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
)

// Dir returns the plan directory docs/plans/<slug>/ under root.
func Dir(root, slug string) string {
	return filepath.Join(paths.DocsPlans(root), slug)
}

// planMDPath / seedsDir / planJSONPath / logPath are the fixed files inside a
// plan directory (§2), centralized so the layout lives in one place.
func planMDPath(dir string) string   { return filepath.Join(dir, "plan.md") }
func seedsDir(dir string) string     { return filepath.Join(dir, "seeds") }
func planJSONPath(dir string) string { return filepath.Join(dir, "plan.json") }

// Load reads and parses docs/plans/<slug>/plan.md, tagging the result with the
// directory slug. A missing plan.md is an error; a malformed table is not (it
// becomes grammar findings on the returned doc, per Parse).
func Load(root, slug string) (*PlanDoc, error) {
	dir := Dir(root, slug)
	data, err := os.ReadFile(planMDPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("plan not found: %s (looked for %s)", slug, filepath.ToSlash(planMDPath(Dir("", slug))))
		}
		return nil, err
	}
	doc := Parse(string(data))
	doc.Slug = slug
	return doc, nil
}

// List returns the slugs of every plan under docs/plans/ (a directory holding a
// plan.md), in lexical order. Top-level draft files like PLAN-*.md are not plans.
func List(root string) ([]string, error) {
	base := paths.DocsPlans(root)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(planMDPath(filepath.Join(base, e.Name()))); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// HashPlan computes the approval-binding content hash over plan.md, every file
// under seeds/, and the Decided rows of docs/stack.md, sorted by workspace path
// (R4.1, §5.4). Content is line-ending normalized (via manifest.Hash) so a CRLF
// re-checkout never looks like drift — the exact discipline phaseContentHash uses
// for specs. The per-file path is folded into the hashed stream so renaming a seed
// changes the hash even when its bytes do not.
//
// The stack rows belong in the hash because Brief inlines them in full (R7.2):
// they are part of the contract a session is handed, so a decision recorded as a
// new row genuinely changes the contract, and the approval must re-bind to it.
func HashPlan(root, slug string) (string, error) {
	return hashPlan(root, slug, true)
}

// HashPlanCore hashes only plan.md + seeds/** — the part of the contract nobody
// may change while a run is live. A session recording a decision moves HashPlan
// (the stack rows are inside it) but never HashPlanCore; an edit to the plan
// itself moves both. The runner compares the two to tell "decision recorded:
// re-bind and continue" from "the approved plan changed: stop".
func HashPlanCore(root, slug string) (string, error) {
	return hashPlan(root, slug, false)
}

func hashPlan(root, slug string, includeStack bool) (string, error) {
	dir := Dir(root, slug)
	var b strings.Builder
	planMD, err := os.ReadFile(planMDPath(dir))
	if err != nil {
		return "", err
	}
	writeHashEntry(&b, "plan.md", planMD)

	var seeds []string
	_ = filepath.WalkDir(seedsDir(dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // seeds/ is optional; an unreadable entry is simply skipped
		}
		if !d.IsDir() {
			seeds = append(seeds, p)
		}
		return nil
	})
	sort.Strings(seeds)
	for _, p := range seeds {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			rel = p
		}
		writeHashEntry(&b, filepath.ToSlash(rel), data)
	}
	if includeStack {
		if rows := hashableStackRows(root); rows != "" {
			writeHashEntry(&b, "docs/stack.md#decided", []byte(rows))
		}
	}
	return manifest.Hash(b.String()), nil
}

// hashableStackRows renders the Decided table as a canonical, order-independent
// stream — one sorted line per row, every cell folded in, so editing a row's Why
// or Version is drift just as much as changing its Choice. Only the table is
// hashed, never the surrounding prose: a typo fix in stack.md's Rules section must
// not invalidate an approval. A workspace with no tech contract yields "", which
// keeps its hash byte-identical to what a pre-stack-rows csdd computed.
func hashableStackRows(root string) string {
	rows := decidedRows(root)
	if len(rows) == 0 {
		return ""
	}
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		r := rows[name]
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n", name, r.Domain, r.Choice, r.Version, r.Why, r.Refs)
	}
	return b.String()
}

// writeHashEntry appends one canonical (path, content) record to the hash stream.
// The NUL delimiter cannot appear in a text path or normalized markdown body, so
// no two distinct file sets can collide onto the same stream.
func writeHashEntry(b *strings.Builder, relPath string, content []byte) {
	b.WriteString(relPath)
	b.WriteByte('\n')
	b.WriteString(textutil.NormalizeNewlines(string(content)))
	b.WriteString("\n\x00\n")
}
