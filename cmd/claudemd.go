package cmd

import (
	"os"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
)

// Markers delimiting the csdd-managed steering import block inside CLAUDE.md.
const (
	steeringMarkerStart = "<!-- csdd:steering:start -->"
	steeringMarkerEnd   = "<!-- csdd:steering:end -->"
)

// ensureSteeringImports inserts `@.claude/steering/<name>` import lines into the
// managed block of CLAUDE.md, one per steering file name (e.g. "product.md").
// It is idempotent and a safe no-op when CLAUDE.md is missing or has no managed
// block (so a hand-managed CLAUDE.md is never clobbered). Returns the count of
// import lines newly added.
func ensureSteeringImports(root string, names ...string) (int, error) {
	entry := paths.Entry(root)
	data, err := os.ReadFile(entry)
	if err != nil {
		return 0, nil // no CLAUDE.md yet — nothing to wire
	}
	text := string(data)
	start := strings.Index(text, steeringMarkerStart)
	end := strings.Index(text, steeringMarkerEnd)
	if start == -1 || end == -1 || end < start {
		return 0, nil // unmanaged CLAUDE.md — do not touch
	}
	blockStart := start + len(steeringMarkerStart)
	block := text[blockStart:end]

	var additions []string
	for _, n := range names {
		// Claude Code resolves @-imports with forward slashes relative to the
		// importing file (the repo root), regardless of host OS.
		imp := "@" + paths.ClaudeDir + "/" + paths.SteeringSeg + "/" + n
		if !strings.Contains(block, imp+"\n") && !strings.HasSuffix(strings.TrimRight(block, "\n"), imp) {
			additions = append(additions, imp)
		}
	}
	if len(additions) == 0 {
		return 0, nil
	}

	existing := strings.TrimRight(strings.TrimSpace(block), "\n")
	lines := additions
	if existing != "" {
		lines = append([]string{existing}, additions...)
	}
	rebuilt := text[:blockStart] + "\n" + strings.Join(lines, "\n") + "\n" + text[end:]
	if err := os.WriteFile(entry, []byte(rebuilt), 0o644); err != nil {
		return 0, err
	}
	return len(additions), nil
}
