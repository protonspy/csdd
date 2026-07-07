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

// HashPlan computes the approval-binding content hash over plan.md plus every
// file under seeds/, sorted by workspace path (R4.1, §5.4). Content is
// line-ending normalized (via manifest.Hash) so a CRLF re-checkout never looks
// like drift — the exact discipline phaseContentHash uses for specs. The
// per-file path is folded into the hashed stream so renaming a seed changes the
// hash even when its bytes do not.
func HashPlan(dir string) (string, error) {
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
	return manifest.Hash(b.String()), nil
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
