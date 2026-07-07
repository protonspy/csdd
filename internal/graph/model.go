// Package graph is csdd's knowledge base: a deterministic, persistent graph over
// the artifacts csdd governs (specs/, .claude/, CLAUDE.md, .mcp.json), the
// documentation under docs/ (wiki + raw sources), the tech contract
// (docs/stack.md + dependency manifests), and — from M3 — the project's Go
// source. It answers csdd's core question ("what connects this requirement to
// the code, and what breaks if I change this task?") by traversal instead of by
// re-reading files, and it is the substrate the spec-lint (`csdd graph analyze`)
// and wiki-lint operations run on.
//
// The model is a straight port of graphify's {nodes, edges} contract with a
// single canonical node ID and confidence tiers, serialised as NetworkX
// node-link JSON so the graph is directly loadable by NetworkX/graphify tooling.
package graph

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Confidence tiers label how an edge was derived. EXTRACTED is an explicit
// annotation or an exact cited path (score 1.0); INFERRED is resolved by
// name/path matching (discrete 0.55–0.95); AMBIGUOUS is genuine uncertainty
// surfaced for the human gate (0.1–0.3). Structural ambiguity that is not one of
// these is omitted, never emitted as a noisy edge.
const (
	Extracted = "EXTRACTED"
	Inferred  = "INFERRED"
	Ambiguous = "AMBIGUOUS"
)

// Node file_type values — the canonical node vocabulary (PLAN §5.5). Kept as
// constants so extractors never spell a type wrong and drift the schema.
const (
	TypeSpec      = "spec"
	TypeRequire   = "requirement"
	TypeCriterion = "criterion"
	TypeDesign    = "design"
	TypeInterface = "interface"
	TypeFlow      = "flow"
	TypeTask      = "task"
	TypeSteering  = "steering"
	TypeSkill     = "skill"
	TypeAgent     = "agent"
	TypeMCP       = "mcp"
	TypeCodeRef   = "code_ref"
	TypeWikiPage  = "wiki_page"
	TypeRawSource = "raw_source"
	TypeTech      = "tech"
	TypeCode      = "code"
	TypePlan      = "plan"
	TypeFeat      = "feat"
	TypeADR       = "adr"
	TypeTerm      = "term"
)

// Edge relation values — the canonical edge vocabulary (PLAN §5.5).
const (
	RelOwns         = "owns"
	RelHasCriterion = "has_criterion"
	RelRealizes     = "realizes"
	RelTracedTo     = "traced_to"
	RelImplements   = "implements"
	RelExercises    = "exercises"
	RelRefinedBy    = "refined_by"
	RelInBoundary   = "in_boundary"
	RelDependsOn    = "depends_on"
	RelComponentDep = "component_dep"
	RelReferences   = "references"
	RelReuses       = "reuses"
	RelGoverns      = "governs"
	RelRelatedTo    = "related_to"
	RelLinksTo      = "links_to"
	RelDerivedFrom  = "derived_from"
	RelUsesTech     = "uses_tech"
	RelContains     = "contains"
	RelImports      = "imports"
	RelPlans        = "plans"
	RelCites        = "cites"
	RelSupersededBy = "superseded_by"
)

// NodeTypes and EdgeRelations mirror the seed graph.json meta block so the
// generated file always advertises the full vocabulary regardless of which
// corpora happen to be present in a given workspace.
var NodeTypes = []string{
	TypeSpec, TypeRequire, TypeCriterion, TypeDesign, TypeInterface, TypeFlow,
	TypeTask, TypeSteering, TypeSkill, TypeAgent, TypeMCP, TypeCodeRef,
	TypeWikiPage, TypeRawSource, TypeTech, TypeCode, TypePlan, TypeFeat,
	TypeADR, TypeTerm,
}

var EdgeRelations = []string{
	RelOwns, RelHasCriterion, RelRealizes, RelTracedTo, RelImplements,
	RelExercises, RelRefinedBy, RelInBoundary, RelDependsOn, RelComponentDep,
	RelReferences, RelReuses, RelGoverns, RelRelatedTo, RelLinksTo,
	RelDerivedFrom, RelUsesTech, RelContains, RelImports, RelPlans,
	RelCites, RelSupersededBy,
}

// Node is one artifact or entity. The five fixed fields are always present;
// Attrs carries type-specific metadata (task status, parallel flag, frontmatter
// tags, tech version, …) that is flattened into the node object on the wire.
type Node struct {
	ID             string
	Label          string
	FileType       string
	SourceFile     string
	SourceLocation string
	Attrs          map[string]any
}

// Edge is one relationship. Endpoints are node IDs. Confidence is the tier
// label; ConfidenceScore is its numeric weight. SourceFile records which file
// the edge was extracted from (its provenance).
//
// Ref carries the raw reference text as written in the source (e.g. "1.1" or a
// component name) for edges derived from an annotation whose target is resolved
// at assembly time. It is never part of the node-link wire format (marshalEdge
// omits it), but it IS carried in the incremental fragment cache, so it is an
// exported field with an explicit tag. It lets assembly report an unresolved
// reference (PendingRef) with the exact string the author wrote.
type Edge struct {
	Source          string
	Target          string
	Relation        string
	Confidence      string
	ConfidenceScore float64
	SourceFile      string
	Ref             string `json:"ref,omitempty"`
}

// PendingRef is a reference (a numeric annotation ID or a boundary component
// name) that did not resolve to any node in the assembled graph. In csdd the gap
// is the product: these are reported by `analyze`, never silently dropped.
type PendingRef struct {
	FromID     string // the node that carried the annotation
	FromLabel  string // its human label (for the finding message)
	Relation   string // realizes | depends_on | in_boundary | ...
	TargetID   string // the unresolved node ID
	Ref        string // the raw reference as written (e.g. "9.9" or "GhostComponent")
	SourceFile string
}

// Graph is a directed knowledge graph. Meta is the node-link "graph" block.
// Pending holds unresolved references discovered during assembly; it is not part
// of the node-link wire format (it is a build-time diagnostic consumed by
// analyze), so a Graph reloaded from disk has an empty Pending slice.
type Graph struct {
	Directed   bool
	Multigraph bool
	Meta       map[string]any
	Nodes      []Node
	Links      []Edge
	Pending    []PendingRef
}

// New returns an empty directed graph with the canonical meta block seeded.
func New() *Graph {
	return &Graph{
		Directed:   true,
		Multigraph: false,
		Meta:       map[string]any{},
		Nodes:      []Node{},
		Links:      []Edge{},
	}
}

// nodeLink is the on-disk (NetworkX node-link) envelope. Nodes and links are
// json.RawMessage because each element is a flat object merging the fixed fields
// with the Attrs map — encoded/decoded by (Node|Edge).marshal below.
type nodeLink struct {
	Directed   bool              `json:"directed"`
	Multigraph bool              `json:"multigraph"`
	Meta       map[string]any    `json:"graph"`
	Nodes      []json.RawMessage `json:"nodes"`
	Links      []json.RawMessage `json:"links"`
}

// reservedNodeKeys and reservedEdgeKeys are the fixed field names that must not
// be clobbered by (or duplicated into) the flattened Attrs map on decode.
var reservedNodeKeys = map[string]bool{
	"id": true, "label": true, "file_type": true,
	"source_file": true, "source_location": true,
}

// MarshalJSON emits the graph as NetworkX node-link JSON. Node Attrs are
// flattened alongside the fixed keys; attr keys are written in sorted order so
// the output is byte-identical across runs (Go map iteration is randomized).
func (g *Graph) MarshalJSON() ([]byte, error) {
	nl := nodeLink{
		Directed:   g.Directed,
		Multigraph: g.Multigraph,
		Meta:       g.Meta,
		Nodes:      make([]json.RawMessage, 0, len(g.Nodes)),
		Links:      make([]json.RawMessage, 0, len(g.Links)),
	}
	if nl.Meta == nil {
		nl.Meta = map[string]any{}
	}
	for _, n := range g.Nodes {
		raw, err := marshalNode(n)
		if err != nil {
			return nil, err
		}
		nl.Nodes = append(nl.Nodes, raw)
	}
	for _, e := range g.Links {
		raw, err := marshalEdge(e)
		if err != nil {
			return nil, err
		}
		nl.Links = append(nl.Links, raw)
	}
	return json.Marshal(nl)
}

// orderedObject encodes a map as a JSON object with keys emitted in a caller-
// supplied order, so node/edge objects are deterministic and human-readable
// (fixed fields first, then sorted attrs).
func orderedObject(pairs [][2]any) ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, kv := range pairs {
		if i > 0 {
			buf = append(buf, ',')
		}
		k, err := json.Marshal(kv[0])
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(kv[1])
		if err != nil {
			return nil, err
		}
		buf = append(buf, k...)
		buf = append(buf, ':')
		buf = append(buf, v...)
	}
	buf = append(buf, '}')
	return buf, nil
}

func marshalNode(n Node) ([]byte, error) {
	pairs := [][2]any{
		{"id", n.ID},
		{"label", n.Label},
		{"file_type", n.FileType},
		{"source_file", n.SourceFile},
		{"source_location", n.SourceLocation},
	}
	for _, k := range sortedAttrKeys(n.Attrs, reservedNodeKeys) {
		pairs = append(pairs, [2]any{k, n.Attrs[k]})
	}
	return orderedObject(pairs)
}

func marshalEdge(e Edge) ([]byte, error) {
	pairs := [][2]any{
		{"source", e.Source},
		{"target", e.Target},
		{"relation", e.Relation},
		{"confidence", e.Confidence},
		{"confidence_score", e.ConfidenceScore},
		{"source_file", e.SourceFile},
	}
	return orderedObject(pairs)
}

// sortedAttrKeys returns the attr keys not shadowed by a reserved field, sorted.
func sortedAttrKeys(attrs map[string]any, reserved map[string]bool) []string {
	if len(attrs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		if reserved[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// UnmarshalJSON parses NetworkX node-link JSON, splitting each flat node object
// back into the fixed fields plus the residual Attrs map. It is the inverse of
// MarshalJSON, so a round-trip preserves every field (R8.1 interoperability).
func (g *Graph) UnmarshalJSON(data []byte) error {
	var nl nodeLink
	if err := json.Unmarshal(data, &nl); err != nil {
		return err
	}
	g.Directed = nl.Directed
	g.Multigraph = nl.Multigraph
	g.Meta = nl.Meta
	if g.Meta == nil {
		g.Meta = map[string]any{}
	}
	g.Nodes = make([]Node, 0, len(nl.Nodes))
	for _, raw := range nl.Nodes {
		n, err := unmarshalNode(raw)
		if err != nil {
			return err
		}
		g.Nodes = append(g.Nodes, n)
	}
	g.Links = make([]Edge, 0, len(nl.Links))
	for _, raw := range nl.Links {
		e, err := unmarshalEdge(raw)
		if err != nil {
			return err
		}
		g.Links = append(g.Links, e)
	}
	return nil
}

func unmarshalNode(raw json.RawMessage) (Node, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return Node{}, err
	}
	n := Node{}
	getString(m, "id", &n.ID)
	getString(m, "label", &n.Label)
	getString(m, "file_type", &n.FileType)
	getString(m, "source_file", &n.SourceFile)
	getString(m, "source_location", &n.SourceLocation)
	for k, v := range m {
		if reservedNodeKeys[k] {
			continue
		}
		if n.Attrs == nil {
			n.Attrs = map[string]any{}
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			return Node{}, err
		}
		n.Attrs[k] = val
	}
	return n, nil
}

func unmarshalEdge(raw json.RawMessage) (Edge, error) {
	var obj struct {
		Source          string  `json:"source"`
		Target          string  `json:"target"`
		Relation        string  `json:"relation"`
		Confidence      string  `json:"confidence"`
		ConfidenceScore float64 `json:"confidence_score"`
		SourceFile      string  `json:"source_file"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Edge{}, err
	}
	return Edge{
		Source:          obj.Source,
		Target:          obj.Target,
		Relation:        obj.Relation,
		Confidence:      obj.Confidence,
		ConfidenceScore: obj.ConfidenceScore,
		SourceFile:      obj.SourceFile,
	}, nil
}

func getString(m map[string]json.RawMessage, key string, dst *string) {
	if raw, ok := m[key]; ok {
		_ = json.Unmarshal(raw, dst)
	}
}

// nodeIndex builds an id → *Node lookup over the current node slice.
func (g *Graph) nodeIndex() map[string]*Node {
	idx := make(map[string]*Node, len(g.Nodes))
	for i := range g.Nodes {
		idx[g.Nodes[i].ID] = &g.Nodes[i]
	}
	return idx
}

// HasNode reports whether a node with the given ID exists.
func (g *Graph) HasNode(id string) bool {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return true
		}
	}
	return false
}

// String renders a short human summary (node/edge counts), handy in errors.
func (g *Graph) String() string {
	return fmt.Sprintf("graph(%d nodes, %d edges)", len(g.Nodes), len(g.Links))
}
