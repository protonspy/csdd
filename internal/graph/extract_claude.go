package graph

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
)

// claudeExtractor indexes the Claude Code workspace configuration: steering and
// generation rules, skills, custom agents, MCP servers, and the CLAUDE.md entry
// point. Frontmatter is parsed with internal/frontmatter so node attributes stay
// consistent with the rest of csdd.
type claudeExtractor struct{}

func (claudeExtractor) Matches(p string) bool {
	if p == "CLAUDE.md" || p == ".mcp.json" {
		return true
	}
	if !strings.HasPrefix(p, ".claude/") {
		return false
	}
	base := path.Base(p)
	switch {
	case strings.HasPrefix(p, ".claude/rules/") && strings.HasSuffix(p, ".md"):
		return true
	case strings.HasPrefix(p, ".claude/steering/") && strings.HasSuffix(p, ".md"):
		return true
	case strings.HasPrefix(p, ".claude/skills/") && base == "SKILL.md":
		return true
	case strings.HasPrefix(p, ".claude/agents/") && strings.HasSuffix(p, ".md"):
		return true
	}
	return false
}

func (claudeExtractor) Extract(src Source) ([]Fragment, error) {
	switch {
	case src.Path == "CLAUDE.md":
		return claudeEntry(src), nil
	case src.Path == ".mcp.json":
		return claudeMCP(src), nil
	case strings.HasPrefix(src.Path, ".claude/skills/"):
		return claudeSkill(src), nil
	case strings.HasPrefix(src.Path, ".claude/agents/"):
		return claudeAgent(src), nil
	case strings.HasPrefix(src.Path, ".claude/rules/"), strings.HasPrefix(src.Path, ".claude/steering/"):
		return claudeSteering(src), nil
	}
	return nil, nil
}

func claudeEntry(src Source) []Fragment {
	return []Fragment{{Nodes: []Node{{
		ID: "entry_claude_md", Label: "CLAUDE.md", FileType: TypeSteering,
		SourceFile: src.Path, SourceLocation: "L1",
		Attrs: map[string]any{"kind": "entry"},
	}}}}
}

func claudeSteering(src Source) []Fragment {
	fm := frontmatter.Parse(string(src.Content))
	name := path.Base(src.Path)
	kind := "steering"
	if strings.HasPrefix(src.Path, ".claude/rules/") {
		kind = "rule"
	}
	attrs := map[string]any{"kind": kind}
	if incl := fm.AsString("inclusion", ""); incl != "" {
		attrs["inclusion"] = incl
	}
	if desc := fm.AsString("description", ""); desc != "" {
		attrs["description"] = desc
	}
	id := MakeID("steering", kind, strings.TrimSuffix(name, ".md"))
	frag := Fragment{Nodes: []Node{{
		ID: id, Label: name, FileType: TypeSteering,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: attrs,
	}}}
	// Cited repo paths in the body become code_ref references.
	appendCited(&frag, src, id)
	return []Fragment{frag}
}

func claudeSkill(src Source) []Fragment {
	// .claude/skills/<name>/SKILL.md — the skill name is the directory.
	rel := strings.TrimPrefix(src.Path, ".claude/skills/")
	name := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		name = rel[:i]
	}
	fm := frontmatter.Parse(string(src.Content))
	attrs := map[string]any{}
	if desc := fm.AsString("description", ""); desc != "" {
		attrs["description"] = desc
	}
	label := fm.AsString("name", name)
	id := MakeID("skill", name)
	frag := Fragment{Nodes: []Node{{
		ID: id, Label: label, FileType: TypeSkill,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: attrs,
	}}}
	appendCited(&frag, src, id)
	return []Fragment{frag}
}

func claudeAgent(src Source) []Fragment {
	name := strings.TrimSuffix(path.Base(src.Path), ".md")
	fm := frontmatter.Parse(string(src.Content))
	attrs := map[string]any{}
	if desc := fm.AsString("description", ""); desc != "" {
		attrs["description"] = desc
	}
	if model := fm.AsString("model", ""); model != "" {
		attrs["model"] = model
	}
	label := fm.AsString("name", name)
	id := MakeID("agent", name)
	frag := Fragment{Nodes: []Node{{
		ID: id, Label: label, FileType: TypeAgent,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: attrs,
	}}}
	appendCited(&frag, src, id)
	return []Fragment{frag}
}

func claudeMCP(src Source) []Fragment {
	var frag Fragment
	servers := parseMCPServers(src.Content)
	for _, name := range servers {
		frag.Nodes = append(frag.Nodes, Node{
			ID: MakeID("mcp", name), Label: name, FileType: TypeMCP,
			SourceFile: src.Path, SourceLocation: "L1",
		})
	}
	return []Fragment{frag}
}

// parseMCPServers returns the server names declared under "mcpServers" in a
// .mcp.json file, sorted for deterministic output. A malformed file yields no
// servers (the graph stays honest rather than guessing).
func parseMCPServers(content []byte) []string {
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil
	}
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// appendCited scans the source body for cited repo paths and appends code_ref
// references from fromID. Kept a helper because every .claude artifact does it.
func appendCited(frag *Fragment, src Source, fromID string) {
	for i, raw := range normLines(src.Content) {
		for _, cited := range citedPathsInline(raw) {
			emitCitedPath(src, i, cited, fromID, frag)
		}
	}
}
