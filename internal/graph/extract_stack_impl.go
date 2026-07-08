package graph

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Matches claims docs/stack.md (the tech contract) and the dependency manifests.
func (stackExtractor) Matches(p string) bool {
	if p == "docs/stack.md" {
		return true
	}
	switch path.Base(p) {
	case "go.mod", "package.json", "pyproject.toml", "requirements.txt":
		return true
	}
	return false
}

func (stackExtractor) Extract(src Source) ([]Fragment, error) {
	if src.Path == "docs/stack.md" {
		return stackContract(src), nil
	}
	return stackManifest(src), nil
}

// techID keys a tech node by its short, normalized name so a contract entry and
// the dependency that realizes it collapse onto the same node (matched by
// normalized name, §5.6).
func techID(name string) string { return MakeID("tech", techKey(name)) }

// techKey reduces a raw dependency or contract choice to a comparable short name:
// the last path segment (for module paths like github.com/go-chi/chi) with any
// version specifier stripped, normalized.
var reMajorVersionSeg = regexp.MustCompile(`^v[0-9]+$`)

// techKey reduces a raw dependency or contract choice to a comparable short name.
// A scoped npm package (@scope/name) keeps its scope so it never collides with a
// same-named bare package (@monaco-editor/react must not fold onto react). A
// module path (github.com/go-chi/chi/v5) reduces to its last non-version segment
// so the contract entry "chi" still matches. Everything else normalizes as-is.
func techKey(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "@") {
		// npm scoped package: normalize the whole "@scope/name" (drop any version).
		if i := strings.IndexAny(name, "@<>=~! "); i > 0 {
			// keep the leading @, cut a trailing "@version" if present
			if v := strings.LastIndexByte(name, '@'); v > 0 {
				name = name[:v]
			}
		}
		return NormalizeID(name)
	}
	// Take the last path segment, skipping a Go-module major-version suffix.
	segs := strings.Split(strings.TrimRight(name, "/"), "/")
	if len(segs) > 0 {
		last := segs[len(segs)-1]
		if reMajorVersionSeg.MatchString(last) && len(segs) > 1 {
			last = segs[len(segs)-2]
		}
		name = last
	}
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return r == '=' || r == '>' || r == '<' || r == '~' || r == '!' || r == '^' || r == '@' || r == ' ' || r == '\t'
	})
	if len(fields) > 0 {
		name = fields[0]
	}
	return NormalizeID(name)
}

// stackContract parses the Decided table of docs/stack.md into tech nodes. The
// Rules and Open-questions sections are prose and are not parsed.
func stackContract(src Source) []Fragment {
	lines := normLines(src.Content)
	var frag Fragment
	inDecided := false
	headerSeen := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(rtrim(raw))
		if strings.HasPrefix(trimmed, "## ") {
			title := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			inDecided = strings.HasPrefix(title, "decided")
			headerSeen = false
			continue
		}
		if !inDecided || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitCells(trimmed)
		if isTableSeparator(cells) {
			continue
		}
		if !headerSeen {
			headerSeen = true // first row is the column header
			continue
		}
		if len(cells) < 2 {
			continue
		}
		choice := cells[1]
		if choice == "" || choice == "—" || choice == "-" {
			continue
		}
		attrs := map[string]any{"status": "decided", "from_contract": true}
		if cells[0] != "" {
			attrs["domain"] = cells[0]
		}
		version := ""
		if len(cells) > 2 {
			version = strings.TrimSpace(cells[2])
		}
		refs := ""
		if len(cells) > 4 {
			refs = strings.TrimSpace(cells[4])
		}
		attrs["version"] = version
		attrs["refs"] = refs
		frag.Nodes = append(frag.Nodes, Node{
			ID: techID(choice), Label: choice, FileType: TypeTech,
			SourceFile: src.Path, SourceLocation: loc(i), Attrs: attrs,
		})
	}
	return []Fragment{frag}
}

// stackManifest parses a dependency manifest into a code_ref node (the manifest)
// and a tech node + uses_tech edge per declared dependency.
func stackManifest(src Source) []Fragment {
	var deps []string
	eco := ""
	switch path.Base(src.Path) {
	case "go.mod":
		deps, eco = parseGoMod(src.Content), "go"
	case "package.json":
		deps, eco = parsePackageJSON(src.Content), "npm"
	case "pyproject.toml":
		deps, eco = parsePyproject(src.Content), "python"
	case "requirements.txt":
		deps, eco = parseRequirementsTxt(src.Content), "python"
	}
	manifestID := codeRefID(src.Path)
	frag := Fragment{Nodes: []Node{{
		ID: manifestID, Label: path.Base(src.Path), FileType: TypeCodeRef,
		SourceFile: src.Path, SourceLocation: "L1",
		Attrs: map[string]any{"kind": "manifest", "ecosystem": eco},
	}}}
	seen := map[string]bool{}
	// The manifest itself declares the project language (go.mod's `go` directive,
	// a Python manifest's ecosystem). Emit it as a used tech so a `| Language | Go |`
	// contract row resolves instead of being flagged phantom. is_language exempts it
	// from the undeclared-tech lint (the language is implicit, not a dependency you
	// list in the contract).
	if lang := ecosystemLanguage(eco); lang != "" {
		lid := techID(lang)
		seen[lid] = true
		frag.Nodes = append(frag.Nodes, Node{
			ID: lid, Label: lang, FileType: TypeTech,
			SourceFile: src.Path, SourceLocation: "L1",
			Attrs: map[string]any{"status": "used", "from_manifest": true, "is_language": true, "ecosystem": eco},
		})
		frag.Edges = append(frag.Edges, Edge{
			Source: manifestID, Target: lid, Relation: RelUsesTech,
			Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
		})
	}
	for _, dep := range deps {
		id := techID(dep)
		if seen[id] {
			continue
		}
		seen[id] = true
		frag.Nodes = append(frag.Nodes, Node{
			ID: id, Label: dep, FileType: TypeTech,
			SourceFile: src.Path, SourceLocation: "L1",
			Attrs: map[string]any{"status": "used", "from_manifest": true, "ecosystem": eco},
		})
		frag.Edges = append(frag.Edges, Edge{
			Source: manifestID, Target: id, Relation: RelUsesTech,
			Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
		})
	}
	return []Fragment{frag}
}

// ecosystemLanguage maps a manifest ecosystem to the programming language it
// implies, or "" when the mapping is ambiguous (npm can be JS or TS, so it is left
// undeclared). Used to satisfy a `| Language | ... |` contract row.
func ecosystemLanguage(eco string) string {
	switch eco {
	case "go":
		return "Go"
	case "python":
		return "Python"
	default:
		return ""
	}
}

var reGoRequireLine = regexp.MustCompile(`^\s*([a-zA-Z0-9.\-/]+)\s+v[0-9]`)

// parseGoMod returns the direct module dependencies (skipping `// indirect`).
func parseGoMod(content []byte) []string {
	var out []string
	inBlock := false
	for _, raw := range normLines(content) {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "require (") {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		target := line
		if strings.HasPrefix(line, "require ") {
			target = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		} else if !inBlock {
			continue
		}
		if strings.Contains(target, "// indirect") {
			continue
		}
		if m := reGoRequireLine.FindStringSubmatch(target); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func parsePackageJSON(content []byte) []string {
	var doc struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil
	}
	var out []string
	for name := range doc.Dependencies {
		out = append(out, name)
	}
	for name := range doc.DevDependencies {
		out = append(out, name)
	}
	// Sort so the extracted order is deterministic (map iteration is randomized);
	// last-writer-wins node dedup must not depend on walk order.
	sort.Strings(out)
	return out
}

var rePyDepArrayItem = regexp.MustCompile(`["']([A-Za-z0-9._\-]+)\s*(?:[=<>~!].*)?["']`)
var rePyPoetryDep = regexp.MustCompile(`^([A-Za-z0-9._\-]+)\s*=`)

// parsePyproject does a pragmatic scan: PEP 621 `dependencies = [ "a", "b>=1" ]`
// arrays and `[tool.poetry.dependencies]` tables. Full TOML parsing is avoided to
// stay dependency-free.
func parsePyproject(content []byte) []string {
	var out []string
	lines := normLines(content)
	inArray := false
	inPoetry := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			inPoetry = strings.HasPrefix(line, "[tool.poetry.dependencies]")
			inArray = false
			continue
		}
		if strings.HasPrefix(line, "dependencies") && strings.Contains(line, "[") {
			inArray = true
		}
		if inArray {
			for _, m := range rePyDepArrayItem.FindAllStringSubmatch(line, -1) {
				out = append(out, m[1])
			}
			if strings.Contains(line, "]") {
				inArray = false
			}
			continue
		}
		if inPoetry {
			if m := rePyPoetryDep.FindStringSubmatch(line); m != nil && m[1] != "python" {
				out = append(out, m[1])
			}
		}
	}
	return out
}

func parseRequirementsTxt(content []byte) []string {
	var out []string
	for _, raw := range normLines(content) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		name := strings.FieldsFunc(line, func(r rune) bool {
			return r == '=' || r == '>' || r == '<' || r == '~' || r == '!' || r == ' ' || r == ';' || r == '['
		})
		if len(name) > 0 && name[0] != "" {
			out = append(out, name[0])
		}
	}
	return out
}
