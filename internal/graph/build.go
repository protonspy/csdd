package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Build walks the workspace at root through the default extractors and assembles
// the result into a deterministic knowledge graph. It is a full rebuild — at
// M1–M3 corpus size this is milliseconds and trivially deterministic (§5.9).
func Build(root string) (*Graph, error) {
	return BuildWith(root, defaultExtractors())
}

// BuildWith is Build with an explicit extractor set, so tests can drive a subset.
func BuildWith(root string, extractors []Extractor) (*Graph, error) {
	sources, warnings, err := collectSources(root, extractors)
	if err != nil {
		return nil, err
	}
	var nodes []Node
	var edges []Edge
	for _, src := range sources {
		frags, err := dispatch(extractors, src)
		if err != nil {
			return nil, err
		}
		for _, f := range frags {
			nodes = append(nodes, f.Nodes...)
			edges = append(edges, f.Edges...)
		}
	}
	g := assemble(root, nodes, edges)
	g.Warnings = warnings
	return g, nil
}

// dedupNodes merges nodes by ID. Identity fields take the last writer; Attrs are
// unioned (existing keys preserved, incoming keys overlaid), so a technology that
// is both declared in the contract and used by a manifest keeps both markers
// regardless of which file was walked first (§5.6 tech matching).
func dedupNodes(in []Node) []Node {
	idx := map[string]int{}
	var out []Node
	for _, n := range in {
		if i, ok := idx[n.ID]; ok {
			out[i] = mergeNode(out[i], n)
			continue
		}
		idx[n.ID] = len(out)
		out = append(out, n)
	}
	return out
}

// detectCollisions scans the raw (pre-dedup) nodes for a single ID that carries
// two or more distinct labels — the signature of an accidental ID collision (e.g.
// spec "foo-bar" and "foo_bar", or criterion 1.1 of spec "a-1" and 1.1.1 of spec
// "a") where dedup silently keeps one artifact and drops the other. Tech nodes are
// exempt: they merge across the contract, manifests, and design refs by design,
// legitimately carrying different labels (contract "chi" vs "github.com/go-chi/chi/v5")
// for the same normalized node. Results are deterministically ordered.
func detectCollisions(nodes []Node) []NodeCollision {
	labelSet := map[string]map[string]bool{}
	fileSet := map[string]map[string]bool{}
	ftype := map[string]string{}
	var ids []string
	for _, n := range nodes {
		if n.FileType == TypeTech {
			continue
		}
		if labelSet[n.ID] == nil {
			labelSet[n.ID] = map[string]bool{}
			fileSet[n.ID] = map[string]bool{}
			ids = append(ids, n.ID)
		}
		labelSet[n.ID][n.Label] = true
		fileSet[n.ID][n.SourceFile] = true
		ftype[n.ID] = n.FileType
	}
	var out []NodeCollision
	for _, id := range ids {
		if len(labelSet[id]) < 2 {
			continue
		}
		out = append(out, NodeCollision{
			ID: id, FileType: ftype[id],
			Labels: sortedKeys(labelSet[id]), Files: sortedKeys(fileSet[id]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mergeNode(existing, incoming Node) Node {
	merged := incoming
	if merged.Attrs == nil && existing.Attrs == nil {
		return merged
	}
	attrs := map[string]any{}
	for k, v := range existing.Attrs {
		attrs[k] = v
	}
	for k, v := range incoming.Attrs {
		attrs[k] = v
	}
	merged.Attrs = attrs
	return merged
}

// dedupEdges removes duplicate (source, target, relation) triples, keeping the
// first (which preserves the strongest/first-declared confidence and the ref).
func dedupEdges(in []Edge) []Edge {
	seen := map[[3]string]bool{}
	var out []Edge
	for _, e := range in {
		key := [3]string{e.Source, e.Target, e.Relation}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

var reSpecMember = regexp.MustCompile(`^specs/([^/]+)/`)

// assembleSpecOwnership synthesizes a spec node for every spec folder that has
// any node and adds `owns` edges from it to the requirement/design/task nodes it
// contains. Centralizing spec membership here keeps extractors ignorant of
// cross-file structure (the (spec,N,M) reconciliation of §5.9).
func assembleSpecOwnership(g *Graph) {
	// Discover spec folders from node source files and ensure a spec node exists.
	present := map[string]bool{}
	for _, n := range g.Nodes {
		present[n.ID] = true
	}
	folderSeen := map[string]bool{}
	for _, n := range g.Nodes {
		m := reSpecMember.FindStringSubmatch(n.SourceFile)
		if m == nil {
			continue
		}
		spec := m[1]
		if folderSeen[spec] {
			continue
		}
		folderSeen[spec] = true
		sid := specNodeID(spec)
		if !present[sid] {
			g.Nodes = append(g.Nodes, Node{
				ID: sid, Label: spec, FileType: TypeSpec,
				SourceFile: "specs/" + spec + "/", SourceLocation: "L1",
			})
			present[sid] = true
		}
	}
	// owns edges spec → {requirement, design, task}.
	for _, n := range g.Nodes {
		switch n.FileType {
		case TypeRequire, TypeDesign, TypeTask:
			m := reSpecMember.FindStringSubmatch(n.SourceFile)
			if m == nil {
				continue
			}
			g.Links = append(g.Links, Edge{
				Source: specNodeID(m[1]), Target: n.ID, Relation: RelOwns,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: "specs/" + m[1] + "/spec.json",
			})
		}
	}
	g.Links = dedupEdges(g.Links)
}

// resolveCitedPaths reclassifies references/reuses edges by whether the cited
// path exists on disk, and — when M3 code nodes are present — retargets them from
// the placeholder code_ref to the real code file node (§5.7 resolution pass).
func resolveCitedPaths(g *Graph, root string) {
	idx := g.nodeIndex()
	codeFileByPath := map[string]string{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.FileType == TypeCode && n.Attrs["kind"] == "file" {
			codeFileByPath[n.SourceFile] = n.ID
		}
	}
	replaced := map[string]bool{}
	for i := range g.Links {
		e := &g.Links[i]
		if e.Relation != RelReferences && e.Relation != RelReuses {
			continue
		}
		tgt := idx[e.Target]
		if tgt == nil || tgt.FileType != TypeCodeRef {
			continue
		}
		cited := tgt.SourceFile
		if codeID, ok := codeFileByPath[cited]; ok {
			// Upgrade artifact→code traceability from placeholder to the real node.
			e.Target = codeID
			e.Relation = RelReuses
			e.Confidence = Extracted
			e.ConfidenceScore = 1.0
			replaced[tgt.ID] = true
			continue
		}
		// A citation to a generated/state path (docs/graph/*, .csdd/*) must classify
		// the same whether or not a prior build has produced that file — otherwise
		// the build's own output would make the next build's graph.json differ
		// (non-deterministic). Treat those as planned references, never reuses.
		if !isGeneratedPath(cited) && pathExists(root, cited) {
			e.Relation = RelReuses
			e.Confidence = Extracted
			e.ConfidenceScore = 1.0
		} else {
			e.Relation = RelReferences
			e.Confidence = Inferred
			e.ConfidenceScore = 0.85
		}
	}
	if len(replaced) > 0 {
		g.Nodes = removeNodes(g.Nodes, replaced)
	}
}

var reGoModule = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// resolveGoImports rewrites in-module import edges from the import-path
// placeholder node to the real package node, using the module path from go.mod.
// Cross-module and std imports are left as-is (out of scope for M3, §5.7).
func resolveGoImports(g *Graph, root string) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return
	}
	m := reGoModule.FindSubmatch(data)
	if m == nil {
		return
	}
	modulePath := string(m[1])
	pkgExists := map[string]bool{}
	for _, n := range g.Nodes {
		if n.FileType == TypeCode && n.Attrs["kind"] == "package" {
			pkgExists[n.ID] = true
		}
	}
	idx := g.nodeIndex()
	replaced := map[string]bool{}
	for i := range g.Links {
		e := &g.Links[i]
		if e.Relation != RelImports {
			continue
		}
		tgt := idx[e.Target]
		if tgt == nil || tgt.Attrs["kind"] != "import" {
			continue
		}
		ip, _ := tgt.Attrs["import_path"].(string)
		if ip == "" || !strings.HasPrefix(ip, modulePath) {
			continue
		}
		dir := strings.TrimPrefix(strings.TrimPrefix(ip, modulePath), "/")
		pkgID := goPkgID(dir)
		if pkgExists[pkgID] {
			e.Target = pkgID
			replaced[tgt.ID] = true
		}
	}
	if len(replaced) > 0 {
		g.Nodes = removeNodes(g.Nodes, replaced)
		g.Links = dedupEdges(g.Links)
	}
}

// reportedRefRelations are the spec-traceability relations whose dangling target
// is a reported gap (a pending reference), not a silent drop.
var reportedRefRelations = map[string]bool{
	RelRealizes:   true,
	RelDependsOn:  true,
	RelInBoundary: true,
	RelTracedTo:   true,
	RelRefinedBy:  true,
}

// resolveReferences scans every edge for an endpoint that resolved to no node.
// Spec-traceability danglers and broken [[wikilinks]] become PendingRef findings
// (the gap is the product); other inferred danglers are dropped silently. The
// returned edges are the resolvable graph; Pending carries the diagnostics.
func resolveReferences(g *Graph) []PendingRef {
	present := map[string]bool{}
	labels := map[string]string{}
	for _, n := range g.Nodes {
		present[n.ID] = true
		labels[n.ID] = n.Label
	}
	var kept []Edge
	var pending []PendingRef
	for _, e := range g.Links {
		srcOK, tgtOK := present[e.Source], present[e.Target]
		if srcOK && tgtOK {
			kept = append(kept, e)
			continue
		}
		switch {
		case reportedRefRelations[e.Relation]:
			// The reference is whichever endpoint failed to resolve; the endpoint
			// that did resolve is the anchor that carried the annotation. For
			// realizes/depends_on/in_boundary the target is the ref; for
			// traced_to/refined_by the source (a criterion) is the ref.
			anchor, missing := e.Source, e.Target
			if !srcOK && tgtOK {
				anchor, missing = e.Target, e.Source
			}
			if srcOK || tgtOK {
				pending = append(pending, PendingRef{
					FromID: anchor, FromLabel: labels[anchor], Relation: e.Relation,
					TargetID: missing, Ref: e.Ref, SourceFile: e.SourceFile,
				})
			}
		case e.Relation == RelLinksTo && srcOK:
			pending = append(pending, PendingRef{
				FromID: e.Source, FromLabel: labels[e.Source], Relation: e.Relation,
				TargetID: e.Target, Ref: e.Ref, SourceFile: e.SourceFile,
			})
		case e.Relation == RelDerivedFrom && srcOK:
			// A page's `sources:` provenance that names no raw_source under docs/raw/
			// (a typo, or the `docs/raw/` prefix left on) is a broken provenance link,
			// not a silent drop — the gap is the product.
			pending = append(pending, PendingRef{
				FromID: e.Source, FromLabel: labels[e.Source], Relation: e.Relation,
				TargetID: e.Target, Ref: e.Ref, SourceFile: e.SourceFile,
			})
		}
		// Other non-reported danglers (component_dep, related_to, …) are dropped:
		// their absence is not a contract violation.
	}
	g.Links = kept
	return pending
}

// removeNodes returns nodes with any ID in drop removed.
func removeNodes(nodes []Node, drop map[string]bool) []Node {
	out := nodes[:0]
	for _, n := range nodes {
		if drop[n.ID] {
			continue
		}
		out = append(out, n)
	}
	return out
}

// sortGraph imposes a total, deterministic order on nodes and edges so an
// unchanged corpus yields byte-identical JSON (§5.9, R7.2).
func sortGraph(g *Graph) {
	sort.SliceStable(g.Nodes, func(i, j int) bool {
		return g.Nodes[i].ID < g.Nodes[j].ID
	})
	sort.SliceStable(g.Links, func(i, j int) bool {
		a, b := g.Links[i], g.Links[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.SourceFile < b.SourceFile
	})
	sort.SliceStable(g.Pending, func(i, j int) bool {
		a, b := g.Pending[i], g.Pending[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		return a.TargetID < b.TargetID
	})
}

// setMeta stamps the node-link graph block with the canonical vocabulary. It
// carries NO wall-clock time so the output stays byte-stable across rebuilds
// (the build timestamp lives in log.md instead).
func setMeta(g *Graph) {
	g.Meta = map[string]any{
		"schema_version":    1,
		"generated_by":      "csdd graph build",
		"node_types":        NodeTypes,
		"edge_relations":    EdgeRelations,
		"confidence_levels": []string{Extracted, Inferred, Ambiguous},
		"hyperedges":        []any{},
	}
}

// isGeneratedPath reports whether a cited path lives in a csdd-generated or
// operational-state subtree, whose existence must not influence build output.
func isGeneratedPath(rel string) bool {
	return strings.HasPrefix(rel, "docs/graph/") || strings.HasPrefix(rel, ".csdd/")
}

// pathExists reports whether a workspace-relative path exists on disk.
func pathExists(root, rel string) bool {
	if rel == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}
