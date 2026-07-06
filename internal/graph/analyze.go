package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Finding is one mechanical contract violation surfaced by analyze. Corpus tags
// which knowledge domain it belongs to (spec | wiki | tech), so `csdd wiki lint`
// can render exactly the wiki subset (§5.10 — lint is analyze filtered).
type Finding struct {
	Kind    string `json:"kind"`
	Corpus  string `json:"corpus"`
	NodeID  string `json:"node_id,omitempty"`
	Label   string `json:"label,omitempty"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
}

// GodNode is a hub — a node whose degree stands out — surfaced so reviewers see
// where the graph concentrates (§5.10).
type GodNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Degree int    `json:"degree"`
}

// GapReport is the full analysis: every finding plus the god-node hubs.
type GapReport struct {
	Findings []Finding `json:"findings"`
	GodNodes []GodNode `json:"god_nodes,omitempty"`
}

// Corpus tags.
const (
	corpusSpec = "spec"
	corpusWiki = "wiki"
	corpusTech = "tech"
)

// Finding kinds.
const (
	kindUntestedRequirement = "untested_requirement"
	kindOrphanTask          = "orphan_task"
	kindUnimplementedCompo  = "unimplemented_component"
	kindPendingReference    = "pending_reference"
	kindRangeShorthand      = "range_shorthand"
	kindDependencyCycle     = "dependency_cycle"
	kindBrokenWikilink      = "broken_wikilink"
	kindOrphanPage          = "orphan_page"
	kindIndexMissingPage    = "index_missing_page"
	kindIndexDanglingEntry  = "index_dangling_entry"
	kindLogFormat           = "log_format"
	kindUnprocessedSource   = "unprocessed_source"
	kindUndeclaredTech      = "undeclared_tech"
	kindPhantomTech         = "phantom_tech"
	kindUnrefinedTech       = "unrefined_tech"
	kindNoTechContract      = "no_tech_contract"
)

// Analyze runs every mechanical check over an assembled graph. root is used only
// for the wiki index/log file checks (which read content the graph does not
// carry); everything else is derived from the graph itself.
func Analyze(g *Graph, root string) GapReport {
	var r GapReport
	r.Findings = append(r.Findings, specFindings(g)...)
	r.Findings = append(r.Findings, wikiFindings(g, root)...)
	r.Findings = append(r.Findings, techFindings(g, root)...)
	r.GodNodes = godNodes(g)
	sortFindings(r.Findings)
	return r
}

// WikiFindings returns the wiki-corpus subset of a report — exactly what
// `csdd wiki lint` renders (§5.10: lint is analyze filtered to the wiki corpus).
func WikiFindings(r GapReport) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Corpus == corpusWiki {
			out = append(out, f)
		}
	}
	return out
}

// edgeSets precomputes inbound/outbound edge presence keyed by relation.
type edgeSets struct {
	outByRel map[string]map[string]bool // relation → source → true
	inByRel  map[string]map[string]bool // relation → target → true
}

func buildEdgeSets(g *Graph) edgeSets {
	es := edgeSets{outByRel: map[string]map[string]bool{}, inByRel: map[string]map[string]bool{}}
	for _, e := range g.Links {
		if es.outByRel[e.Relation] == nil {
			es.outByRel[e.Relation] = map[string]bool{}
		}
		if es.inByRel[e.Relation] == nil {
			es.inByRel[e.Relation] = map[string]bool{}
		}
		es.outByRel[e.Relation][e.Source] = true
		es.inByRel[e.Relation][e.Target] = true
	}
	return es
}

func (es edgeSets) hasOut(rel, id string) bool {
	m := es.outByRel[rel]
	return m != nil && m[id]
}
func (es edgeSets) hasIn(rel, id string) bool {
	m := es.inByRel[rel]
	return m != nil && m[id]
}

var reRange = regexp.MustCompile(`\d+\.\d+\s*[-–—]\s*\d+`)

func specFindings(g *Graph) []Finding {
	es := buildEdgeSets(g)
	var out []Finding
	for _, n := range g.Nodes {
		switch n.FileType {
		case TypeCriterion:
			// R3.1 untested requirement: a criterion no task realizes.
			if !es.hasIn(RelRealizes, n.ID) {
				out = append(out, Finding{
					Kind: kindUntestedRequirement, Corpus: corpusSpec, NodeID: n.ID, Label: n.Label,
					Message: "acceptance criterion has no task realizing it", File: n.SourceFile,
				})
			}
		case TypeTask:
			// R3.2 orphan task: a task with no _Requirements annotation.
			if !es.hasOut(RelRealizes, n.ID) {
				out = append(out, Finding{
					Kind: kindOrphanTask, Corpus: corpusSpec, NodeID: n.ID, Label: n.Label,
					Message: "task has no _Requirements annotation (realizes no criterion)", File: n.SourceFile,
				})
			}
		case TypeDesign:
			// R3.3 unimplemented component: a design component no task targets.
			if !es.hasIn(RelInBoundary, n.ID) {
				out = append(out, Finding{
					Kind: kindUnimplementedCompo, Corpus: corpusSpec, NodeID: n.ID, Label: n.Label,
					Message: "design component has no task in its boundary (_Boundary)", File: n.SourceFile,
				})
			}
		}
	}
	// R3.4 pending references (incl. range shorthand as a distinct kind).
	for _, p := range g.Pending {
		if p.Relation == RelLinksTo {
			continue // handled in wikiFindings
		}
		if !reportedRefRelations[p.Relation] {
			continue
		}
		kind := kindPendingReference
		msg := "annotation references a nonexistent ID: " + refDisplay(p)
		if reRange.MatchString(p.Ref) {
			kind = kindRangeShorthand
			msg = "range shorthand is not supported (list IDs explicitly): " + p.Ref
		}
		out = append(out, Finding{
			Kind: kind, Corpus: corpusSpec, NodeID: p.FromID, Label: p.FromLabel,
			Message: msg, File: p.SourceFile,
		})
	}
	// R3.5 depends_on cycles.
	for _, cyc := range dependencyCycles(g) {
		out = append(out, Finding{
			Kind: kindDependencyCycle, Corpus: corpusSpec,
			Message: "task dependency cycle: " + strings.Join(cyc, " → "),
		})
	}
	return out
}

func refDisplay(p PendingRef) string {
	if p.Ref != "" {
		return p.Ref + " (→ " + p.TargetID + ")"
	}
	return p.TargetID
}

func wikiFindings(g *Graph, root string) []Finding {
	es := buildEdgeSets(g)
	var out []Finding

	// R10.1 broken wikilinks (from assembly's dangling links_to).
	for _, p := range g.Pending {
		if p.Relation != RelLinksTo {
			continue
		}
		out = append(out, Finding{
			Kind: kindBrokenWikilink, Corpus: corpusWiki, NodeID: p.FromID, Label: p.FromLabel,
			Message: "broken [[wikilink]] to \"" + p.Ref + "\" (no such page)", File: p.SourceFile,
		})
	}

	indexed := readIndexEntries(root)
	// R10.2 orphan pages; R10.3a pages missing from index.md.
	for _, n := range g.Nodes {
		if !isWikiContentPage(n) {
			continue
		}
		inIndex := indexed.hasPage(n.ID)
		hasInbound := es.hasIn(RelLinksTo, n.ID)
		if !inIndex && !hasInbound {
			out = append(out, Finding{
				Kind: kindOrphanPage, Corpus: corpusWiki, NodeID: n.ID, Label: n.Label,
				Message: "orphan page: no inbound [[wikilink]] and not listed in index.md", File: n.SourceFile,
			})
		}
		if indexed.present && !inIndex {
			out = append(out, Finding{
				Kind: kindIndexMissingPage, Corpus: corpusWiki, NodeID: n.ID, Label: n.Label,
				Message: "page not listed in index.md", File: n.SourceFile,
			})
		}
	}
	// R10.3b index entries pointing at missing files.
	pageFiles := map[string]bool{}
	for _, n := range g.Nodes {
		if isWikiContentPage(n) {
			pageFiles[n.SourceFile] = true
		}
	}
	for _, entry := range indexed.entryPaths {
		if !pageFiles[entry] && !pathExists(root, entry) {
			out = append(out, Finding{
				Kind: kindIndexDanglingEntry, Corpus: corpusWiki,
				Message: "index.md entry points at a missing file: " + entry,
				File:    "docs/wiki/index.md",
			})
		}
	}
	// R10.4 log.md format.
	out = append(out, logFormatFindings(root)...)
	// R10.5 unprocessed raw sources.
	for _, n := range g.Nodes {
		if n.FileType != TypeRawSource {
			continue
		}
		if !es.hasIn(RelDerivedFrom, n.ID) {
			out = append(out, Finding{
				Kind: kindUnprocessedSource, Corpus: corpusWiki, NodeID: n.ID, Label: n.Label,
				Message: "unprocessed raw source: dropped in docs/raw/ but no wiki page derives from it", File: n.SourceFile,
			})
		}
	}
	return out
}

func isWikiContentPage(n Node) bool {
	return n.FileType == TypeWikiPage && strings.HasPrefix(n.SourceFile, "docs/wiki/pages/")
}

func techFindings(g *Graph, root string) []Finding {
	es := buildEdgeSets(g)
	var out []Finding
	// R17.5 no contract → a lint finding, not an error.
	if !pathExists(root, "docs/stack.md") {
		out = append(out, Finding{
			Kind: kindNoTechContract, Corpus: corpusTech,
			Message: "no tech contract (docs/stack.md); stack decisions are undocumented",
		})
	}
	// A dependency is "undeclared" only when a manifest declares it (R17.3) — a
	// design-level External dependency is an intent, not a manifest fact.
	usedByManifest := map[string]bool{}
	nodeType := map[string]string{}
	for _, n := range g.Nodes {
		nodeType[n.ID] = n.FileType
	}
	for _, e := range g.Links {
		if e.Relation == RelUsesTech && nodeType[e.Source] == TypeCodeRef {
			usedByManifest[e.Target] = true
		}
	}
	for _, n := range g.Nodes {
		if n.FileType != TypeTech {
			continue
		}
		declared := asBool(n.Attrs["from_contract"])
		used := es.hasIn(RelUsesTech, n.ID)
		switch {
		case usedByManifest[n.ID] && !declared:
			out = append(out, Finding{
				Kind: kindUndeclaredTech, Corpus: corpusTech, NodeID: n.ID, Label: n.Label,
				Message: "undeclared tech decision: \"" + n.Label + "\" is used but absent from the contract", File: n.SourceFile,
			})
		case declared && !used:
			out = append(out, Finding{
				Kind: kindPhantomTech, Corpus: corpusTech, NodeID: n.ID, Label: n.Label,
				Message: "phantom tech: \"" + n.Label + "\" is in the contract but no usage was detected", File: n.SourceFile,
			})
		}
		if declared {
			if asString(n.Attrs["version"]) == "" || asString(n.Attrs["refs"]) == "" {
				out = append(out, Finding{
					Kind: kindUnrefinedTech, Corpus: corpusTech, NodeID: n.ID, Label: n.Label,
					Message: "unrefined tech: \"" + n.Label + "\" is missing Version or Refs", File: n.SourceFile,
				})
			}
		}
	}
	return out
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// godNodes returns the highest-degree hubs (top few, degree ≥ 4), so the report
// highlights concentration without listing every leaf.
func godNodes(g *Graph) []GodNode {
	deg := map[string]int{}
	label := map[string]string{}
	for _, n := range g.Nodes {
		label[n.ID] = n.Label
	}
	for _, e := range g.Links {
		deg[e.Source]++
		deg[e.Target]++
	}
	var hubs []GodNode
	for id, d := range deg {
		if d >= 4 {
			hubs = append(hubs, GodNode{ID: id, Label: label[id], Degree: d})
		}
	}
	sort.Slice(hubs, func(i, j int) bool {
		if hubs[i].Degree != hubs[j].Degree {
			return hubs[i].Degree > hubs[j].Degree
		}
		return hubs[i].ID < hubs[j].ID
	})
	if len(hubs) > 10 {
		hubs = hubs[:10]
	}
	return hubs
}

func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].Corpus != f[j].Corpus {
			return f[i].Corpus < f[j].Corpus
		}
		if f[i].Kind != f[j].Kind {
			return f[i].Kind < f[j].Kind
		}
		if f[i].NodeID != f[j].NodeID {
			return f[i].NodeID < f[j].NodeID
		}
		return f[i].Message < f[j].Message
	})
}

// --- wiki index.md / log.md helpers -----------------------------------------

type indexEntries struct {
	present    bool
	pageIDs    map[string]bool
	entryPaths []string
}

func (ie indexEntries) hasPage(id string) bool { return ie.pageIDs[id] }

var reIndexLink = regexp.MustCompile(`\]\(([^)]+\.md)\)`)

// readIndexEntries parses docs/wiki/index.md for the pages it catalogs, via both
// markdown links to pages/*.md and [[wikilinks]].
func readIndexEntries(root string) indexEntries {
	ie := indexEntries{pageIDs: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "index.md"))
	if err != nil {
		return ie
	}
	ie.present = true
	text := stripHTMLComments(string(data))
	for _, m := range reIndexLink.FindAllStringSubmatch(text, -1) {
		target := strings.TrimSpace(m[1])
		if strings.ContainsAny(target, "<>") {
			continue // a placeholder in the entry-format doc, not a real entry
		}
		ie.pageIDs[wikiTargetID(target)] = true
		ie.entryPaths = append(ie.entryPaths, normalizeIndexPath(target))
	}
	for _, m := range reWikiLink.FindAllStringSubmatch(text, -1) {
		if strings.ContainsAny(m[1], "<>") {
			continue
		}
		ie.pageIDs[wikiTargetID(m[1])] = true
	}
	return ie
}

// normalizeIndexPath resolves an index entry link to a workspace-relative path
// so its existence can be checked. Relative links are taken from docs/wiki/.
func normalizeIndexPath(target string) string {
	if strings.HasPrefix(target, "docs/") {
		return target
	}
	return "docs/wiki/" + strings.TrimPrefix(target, "./")
}

var reLogEntry = regexp.MustCompile(`^## \[(\d{4}-\d{2}-\d{2})\]\s+(\w+)\s+\|\s+(.+)$`)
var logOps = map[string]bool{"ingest": true, "query": true, "lint": true, "refactor": true}

// logFormatFindings flags docs/wiki/log.md headings that start with "## [" but do
// not match the documented `## [YYYY-MM-DD] <op> | <title>` format (R10.4).
func logFormatFindings(root string) []Finding {
	data, err := os.ReadFile(filepath.Join(root, "docs", "wiki", "log.md"))
	if err != nil {
		return nil
	}
	var out []Finding
	stripped := stripHTMLComments(strings.ReplaceAll(string(data), "\r\n", "\n"))
	for i, raw := range strings.Split(stripped, "\n") {
		line := strings.TrimRight(raw, " \t")
		if !strings.HasPrefix(line, "## [") {
			continue
		}
		m := reLogEntry.FindStringSubmatch(line)
		if m == nil || !logOps[strings.ToLower(m[2])] {
			out = append(out, Finding{
				Kind: kindLogFormat, Corpus: corpusWiki,
				Message: "log.md entry does not match `## [YYYY-MM-DD] <op> | <title>` (op ∈ ingest|query|lint|refactor): " + strings.TrimSpace(line),
				File:    "docs/wiki/log.md", NodeID: "", Label: "L" + itoa(i+1),
			})
		}
	}
	return out
}

// --- depends_on cycle detection (Tarjan SCC) --------------------------------

// dependencyCycles returns the node-label cycles among depends_on edges, one per
// strongly-connected component of size > 1 (or a self-loop), deterministically
// ordered.
func dependencyCycles(g *Graph) [][]string {
	adj := map[string][]string{}
	label := map[string]string{}
	selfLoop := map[string]bool{}
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		label[n.ID] = n.Label
	}
	for _, e := range g.Links {
		if e.Relation != RelDependsOn {
			continue
		}
		if e.Source == e.Target {
			selfLoop[e.Source] = true
		}
		adj[e.Source] = append(adj[e.Source], e.Target)
		nodes[e.Source] = true
		nodes[e.Target] = true
	}
	for k := range adj {
		sort.Strings(adj[k])
	}
	sccs := tarjanSCC(nodes, adj)
	var cycles [][]string
	for _, comp := range sccs {
		if len(comp) > 1 || (len(comp) == 1 && selfLoop[comp[0]]) {
			labels := make([]string, len(comp))
			for i, id := range comp {
				if l := label[id]; l != "" {
					labels[i] = l
				} else {
					labels[i] = id
				}
			}
			cycles = append(cycles, labels)
		}
	}
	sort.Slice(cycles, func(i, j int) bool {
		return strings.Join(cycles[i], "\x00") < strings.Join(cycles[j], "\x00")
	})
	return cycles
}

func tarjanSCC(nodes map[string]bool, adj map[string][]string) [][]string {
	ids := make([]string, 0, len(nodes))
	for n := range nodes {
		ids = append(ids, n)
	}
	sort.Strings(ids)

	index := 0
	indices := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, seen := indices[w]; !seen {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if indices[w] < low[v] {
					low[v] = indices[w]
				}
			}
		}
		if low[v] == indices[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			sort.Strings(comp)
			out = append(out, comp)
		}
	}
	for _, v := range ids {
		if _, seen := indices[v]; !seen {
			strongconnect(v)
		}
	}
	return out
}
