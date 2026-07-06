package cli

import (
	"embed"
	"os"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/textutil"
	"github.com/protonspy/csdd/internal/workspace"
)

// Markers delimiting the csdd-managed steering import block inside CLAUDE.md.
const (
	steeringMarkerStart = "<!-- csdd:steering:start -->"
	steeringMarkerEnd   = "<!-- csdd:steering:end -->"
)

// Markers delimiting the csdd-managed knowledge-base section inside CLAUDE.md
// (the graph/wiki workflow moments + the tech-contract protocol).
const (
	knowledgeMarkerStart = "<!-- csdd:knowledge:start -->"
	knowledgeMarkerEnd   = "<!-- csdd:knowledge:end -->"
)

// ensureKnowledgeSection writes the knowledge-base workflow moments (R16.3) and
// the tech-contract protocol (R17.4, R17.7) into the managed block of CLAUDE.md.
// It fills the empty markers a fresh template ships with, and — for a legacy
// CLAUDE.md that predates them — appends the whole section (markers + content).
// A no-op when CLAUDE.md is absent. Returns whether the file was changed.
func ensureKnowledgeSection(root string, templates embed.FS) (bool, error) {
	entry := paths.Entry(root)
	data, err := os.ReadFile(entry)
	if err != nil {
		return false, nil // no CLAUDE.md — nothing to wire
	}
	body, err := templater.Static(templates, "templates/root/knowledge-section.md.tmpl")
	if err != nil {
		return false, err
	}
	body = strings.TrimRight(body, "\n")
	text := textutil.NormalizeNewlines(string(data))

	start := strings.Index(text, knowledgeMarkerStart)
	end := strings.Index(text, knowledgeMarkerEnd)
	var rebuilt string
	switch {
	case start != -1 && end != -1 && end > start:
		// Fill (or refresh) the content between existing markers.
		before := text[:start+len(knowledgeMarkerStart)]
		after := text[end:]
		newBlock := before + "\n" + body + "\n" + after
		if newBlock == text {
			return false, nil
		}
		rebuilt = newBlock
	default:
		// Legacy CLAUDE.md without the markers: append the managed section.
		section := "\n## Knowledge base — graph, wiki, tech contract\n\n" +
			knowledgeMarkerStart + "\n" + body + "\n" + knowledgeMarkerEnd + "\n"
		if strings.HasSuffix(text, "\n") {
			rebuilt = text + section
		} else {
			rebuilt = text + "\n" + section
		}
	}
	if err := workspace.AtomicWrite(entry, []byte(rebuilt), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

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
	// Normalize line endings before matching: on a CRLF CLAUDE.md the existing
	// import line is "imp\r\n", which neither dedup check below would match, so a
	// duplicate @-import was appended on every run.
	text := textutil.NormalizeNewlines(string(data))
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
	if err := workspace.AtomicWrite(entry, []byte(rebuilt), 0o644); err != nil {
		return 0, err
	}
	return len(additions), nil
}
