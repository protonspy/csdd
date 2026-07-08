package graph

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// QueryOptions tunes retrieval. Hops is the neighborhood radius (default 1).
// Budget is the token budget honored by the M4 engine; the v1 label matcher
// ignores it (the surface is stable across the M1→M4 swap, §5.11).
type QueryOptions struct {
	Hops   int
	Budget int
}

// Neighbor is one edge incident to a matched node, with the node on the far end
// and the edge's real direction relative to the match.
type Neighbor struct {
	Direction string `json:"direction"` // "out" (match→node) or "in" (node→match)
	Relation  string `json:"relation"`
	Node      Node   `json:"node"`
}

// Match is a query hit: the node, the tier it matched on, and its neighborhood.
type Match struct {
	Node      Node       `json:"node"`
	Tier      string     `json:"tier"` // exact | prefix | substring
	Neighbors []Neighbor `json:"neighbors"`
}

// Tier ranks; lower is stronger.
const (
	tierExact     = "exact"
	tierPrefix    = "prefix"
	tierSubstring = "substring"
)

func tierRank(t string) int {
	switch t {
	case tierExact:
		return 0
	case tierPrefix:
		return 1
	default:
		return 2
	}
}

// defaultTokenBudget is the rendering budget (R13.1) when the caller passes 0.
const defaultTokenBudget = 2000

// hubGateFloor is the minimum degree at which hub-gating can kick in — a node is
// never treated as a super-hub below this, regardless of the p99 (R13.1).
const hubGateFloor = 50

// Query returns nodes matching terms, scored by label tier weighted by inverse
// document frequency, each with its (hub-gated) neighborhood, rendered within a
// token budget (R13.1). At small corpus size this reduces to the v1 label
// matcher — tier ordering, full neighborhoods, nothing truncated — so the M1
// query behavior is preserved (§5.11: same surface, the internals grow).
func Query(g *Graph, terms string, opts QueryOptions) []Match {
	hops := opts.Hops
	if hops <= 0 {
		hops = 1
	}
	budget := opts.Budget
	if budget <= 0 {
		budget = defaultTokenBudget
	}
	toks := tokenize(terms)
	if len(toks) == 0 {
		return nil
	}
	idf := buildIDF(g, toks)
	hubThreshold := hubThreshold(g)

	// Score every node: sum over terms of tier_weight(term) × idf(term). A node
	// matching any term becomes a hit, so every term that matches anything is
	// seeded by at least its best node (R13.1: ≥1 seed per term).
	type scored struct {
		node  Node
		score float64
		tier  string
	}
	var hits []scored
	for _, n := range g.Nodes {
		total := 0.0
		bestRank := 99
		bestTier := ""
		for _, tk := range toks {
			w, tier := termTierWeight(n, tk)
			if w == 0 {
				continue
			}
			total += w * idf[tk]
			if r := tierRank(tier); r < bestRank {
				bestRank = r
				bestTier = tier
			}
		}
		if total > 0 {
			hits = append(hits, scored{node: n, score: total, tier: bestTier})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if ri, rj := tierRank(hits[i].tier), tierRank(hits[j].tier); ri != rj {
			return ri < rj
		}
		return hits[i].node.ID < hits[j].node.ID
	})

	// Render within the token budget, attaching each hit's hub-gated neighborhood.
	var matches []Match
	spent := 0
	for _, h := range hits {
		cost := estimateTokens(h.node.Label) + 6
		if spent+cost > budget && len(matches) > 0 {
			break
		}
		spent += cost
		nbrs := neighborhoodGated(g, h.node.ID, hops, hubThreshold)
		kept := make([]Neighbor, 0, len(nbrs))
		for _, nb := range nbrs {
			c := estimateTokens(nb.Node.Label) + 4
			if spent+c > budget {
				break
			}
			spent += c
			kept = append(kept, nb)
		}
		matches = append(matches, Match{Node: h.node, Tier: h.tier, Neighbors: kept})
	}
	return matches
}

// tokenize splits a query into normalized, stopword-filtered terms.
func tokenize(q string) []string {
	fields := splitWords(NormalizeID(q))
	var out []string
	for _, f := range fields {
		if f == "" || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		// All stopwords (or a single normalized token): fall back to the whole
		// normalized query so a lookup like "the-x" still resolves.
		if whole := NormalizeID(q); whole != "" {
			return []string{whole}
		}
	}
	return out
}

func splitWords(normalized string) []string {
	return strings.Split(normalized, "_")
}

var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "to": true, "and": true,
	"or": true, "in": true, "on": true, "for": true, "is": true, "are": true,
}

// buildIDF computes inverse document frequency per term over node labels+IDs.
func buildIDF(g *Graph, terms []string) map[string]float64 {
	n := float64(len(g.Nodes))
	if n == 0 {
		n = 1
	}
	idf := map[string]float64{}
	for _, tk := range terms {
		df := 0
		for _, node := range g.Nodes {
			if termTierWeightRank(node, tk) > 0 {
				df++
			}
		}
		idf[tk] = math.Log(1 + n/float64(1+df))
	}
	return idf
}

// termTierWeight scores how a single term matches a node and returns (weight, tier).
func termTierWeight(n Node, term string) (float64, string) {
	nl := NormalizeID(n.Label)
	id := n.ID
	switch {
	case nl == term || id == term:
		return 3, tierExact
	case strings.HasPrefix(nl, term) || strings.HasPrefix(id, term):
		return 2, tierPrefix
	case strings.Contains(nl, term) || strings.Contains(id, term):
		return 1, tierSubstring
	}
	return 0, ""
}

func termTierWeightRank(n Node, term string) float64 {
	w, _ := termTierWeight(n, term)
	return w
}

// hubThreshold returns the degree above which a node is a super-hub not traversed
// *through* — max(p99 of degrees, floor). At small scale the floor dominates, so
// nothing is gated (v1 behavior preserved).
func hubThreshold(g *Graph) int {
	deg := degreeMap(g)
	if len(deg) == 0 {
		return hubGateFloor
	}
	degrees := make([]int, 0, len(deg))
	for _, d := range deg {
		degrees = append(degrees, d)
	}
	sort.Ints(degrees)
	idx := int(0.99 * float64(len(degrees)-1))
	p99 := degrees[idx]
	if p99 < hubGateFloor {
		return hubGateFloor
	}
	return p99
}

// neighborhoodGated is neighborhood() with hub-gating: a super-hub can be a
// neighbor, but BFS never expands *through* it into its own neighbors (R13.1).
func neighborhoodGated(g *Graph, id string, hops, hubThreshold int) []Neighbor {
	idx := g.nodeIndex()
	adj := buildAdjacency(g)
	deg := degreeMap(g)
	visited := map[string]bool{id: true}
	var out []Neighbor
	frontier := []string{id}
	for depth := 0; depth < hops; depth++ {
		var next []string
		for _, cur := range frontier {
			// Do not expand through a super-hub (but the hub itself was already
			// added as a neighbor when it was first reached).
			if cur != id && deg[cur] > hubThreshold {
				continue
			}
			for _, a := range adj[cur] {
				if visited[a.to] {
					continue
				}
				visited[a.to] = true
				if nn := idx[a.to]; nn != nil {
					out = append(out, Neighbor{Direction: a.dir, Relation: a.relation, Node: *nn})
				}
				next = append(next, a.to)
			}
		}
		frontier = next
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Relation != out[j].Relation {
			return out[i].Relation < out[j].Relation
		}
		return out[i].Node.ID < out[j].Node.ID
	})
	return out
}

// estimateTokens is a coarse token estimate for budget accounting (~4 chars/token).
func estimateTokens(s string) int {
	t := len(s) / 4
	if t < 1 {
		t = 1
	}
	return t
}

// labelTier reports the strongest tier at which node matches the normalized query,
// or "" for no match. ID and label are both consulted (IDs are already normalized).
func labelTier(n Node, q string) string {
	nl := NormalizeID(n.Label)
	id := n.ID
	switch {
	case nl == q || id == q:
		return tierExact
	case strings.HasPrefix(nl, q) || strings.HasPrefix(id, q):
		return tierPrefix
	case strings.Contains(nl, q) || strings.Contains(id, q):
		return tierSubstring
	}
	return ""
}

// arc is one directed adjacency entry with the direction relative to the node it
// is stored under.
type arc struct {
	to       string
	relation string
	dir      string // "out" if the stored node is the edge source, else "in"
}

// buildAdjacency builds an undirected adjacency (both directions) preserving each
// edge's real orientation via arc.dir.
func buildAdjacency(g *Graph) map[string][]arc {
	adj := map[string][]arc{}
	for _, e := range g.Links {
		adj[e.Source] = append(adj[e.Source], arc{to: e.Target, relation: e.Relation, dir: "out"})
		adj[e.Target] = append(adj[e.Target], arc{to: e.Source, relation: e.Relation, dir: "in"})
	}
	for k := range adj {
		sort.SliceStable(adj[k], func(i, j int) bool {
			if adj[k][i].relation != adj[k][j].relation {
				return adj[k][i].relation < adj[k][j].relation
			}
			return adj[k][i].to < adj[k][j].to
		})
	}
	return adj
}

// PathStep is one hop along a path: the edge (with its real direction) and the
// node it leads to.
type PathStep struct {
	Relation  string `json:"relation"`
	Direction string `json:"direction"` // "forward" (with the edge) or "backward"
	Node      Node   `json:"node"`
}

// PathResult is a resolved shortest path from From to To. FromTier and ToTier
// record how each endpoint query matched its node (exact | prefix | substring),
// so a caller can tell an exact hit from a fuzzy one that silently resolved to an
// unexpected node.
type PathResult struct {
	From     Node       `json:"from"`
	To       Node       `json:"to"`
	FromTier string     `json:"from_tier"`
	ToTier   string     `json:"to_tier"`
	Steps    []PathStep `json:"steps"`
}

// Path returns the shortest path between the nodes A and B resolve to, rendering
// each edge with its real direction (R2.2). It errors when A and B resolve to the
// same node, or when either does not resolve, or when no path exists.
func Path(g *Graph, a, b string, maxHops int) (*PathResult, error) {
	from, fromTier, ok := resolveNode(g, a)
	if !ok {
		return nil, fmt.Errorf("no node matches %q", a)
	}
	to, toTier, ok := resolveNode(g, b)
	if !ok {
		return nil, fmt.Errorf("no node matches %q", b)
	}
	if from.ID == to.ID {
		return nil, fmt.Errorf("both endpoints resolve to the same node (%s)", from.ID)
	}
	if maxHops <= 0 {
		maxHops = len(g.Nodes) + 1
	}
	adj := buildAdjacency(g)
	// BFS over the undirected graph, remembering the arc used to reach each node.
	type crumb struct {
		prev string
		via  arc
	}
	prev := map[string]crumb{from.ID: {}}
	queue := []string{from.ID}
	depth := map[string]int{from.ID: 0}
	found := false
	for len(queue) > 0 && !found {
		cur := queue[0]
		queue = queue[1:]
		if depth[cur] >= maxHops {
			continue
		}
		for _, a := range adj[cur] {
			if _, seen := prev[a.to]; seen {
				continue
			}
			prev[a.to] = crumb{prev: cur, via: a}
			depth[a.to] = depth[cur] + 1
			if a.to == to.ID {
				found = true
				break
			}
			queue = append(queue, a.to)
		}
	}
	if _, ok := prev[to.ID]; !ok {
		return nil, fmt.Errorf("no path from %s to %s within %d hops", from.ID, to.ID, maxHops)
	}
	idx := g.nodeIndex()
	var steps []PathStep
	for cur := to.ID; cur != from.ID; {
		c := prev[cur]
		// c.via is the arc stored under c.prev. dir=="out" means the real edge runs
		// prev→cur (we traverse it forward); dir=="in" means it runs cur→prev (we
		// traverse it backward).
		dir := "forward"
		if c.via.dir == "in" {
			dir = "backward"
		}
		// A CLI-built graph never has a dangling edge (resolveReferences prunes them),
		// but an externally loaded NetworkX graph can — guard so Path errors instead
		// of panicking, matching Query/Explain.
		node := idx[cur]
		if node == nil {
			return nil, fmt.Errorf("path traverses an edge to a missing node %q (graph is inconsistent)", cur)
		}
		steps = append(steps, PathStep{Relation: c.via.relation, Direction: dir, Node: *node})
		cur = c.prev
	}
	// steps were collected target→source; reverse to source→target order.
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return &PathResult{From: *from, To: *to, FromTier: fromTier, ToTier: toTier, Steps: steps}, nil
}

// ExplainConnection is one neighbor in an explain result, with the neighbor's own
// degree (so connections can be ordered by hub-ness).
type ExplainConnection struct {
	Direction string `json:"direction"`
	Relation  string `json:"relation"`
	Node      Node   `json:"node"`
	Degree    int    `json:"degree"`
}

// ExplainResult is a node plus its connections ordered by neighbor degree. Tier
// records how the label query matched the node (exact | prefix | substring).
type ExplainResult struct {
	Node        Node                `json:"node"`
	Tier        string              `json:"tier"`
	Connections []ExplainConnection `json:"connections"`
}

// Explain returns the node label resolves to plus its connections ordered by
// neighbor degree, descending (R2.3).
func Explain(g *Graph, label string) (*ExplainResult, error) {
	n, tier, ok := resolveNode(g, label)
	if !ok {
		return nil, fmt.Errorf("no node matches %q", label)
	}
	deg := degreeMap(g)
	idx := g.nodeIndex()
	adj := buildAdjacency(g)
	conns := []ExplainConnection{}
	seen := map[string]bool{}
	for _, a := range adj[n.ID] {
		key := a.dir + "|" + a.relation + "|" + a.to
		if seen[key] {
			continue
		}
		seen[key] = true
		nb := idx[a.to]
		if nb == nil {
			continue
		}
		conns = append(conns, ExplainConnection{
			Direction: a.dir, Relation: a.relation, Node: *nb, Degree: deg[a.to],
		})
	}
	sort.SliceStable(conns, func(i, j int) bool {
		if conns[i].Degree != conns[j].Degree {
			return conns[i].Degree > conns[j].Degree
		}
		if conns[i].Relation != conns[j].Relation {
			return conns[i].Relation < conns[j].Relation
		}
		return conns[i].Node.ID < conns[j].Node.ID
	})
	return &ExplainResult{Node: *n, Tier: tier, Connections: conns}, nil
}

func degreeMap(g *Graph) map[string]int {
	deg := map[string]int{}
	for _, e := range g.Links {
		deg[e.Source]++
		deg[e.Target]++
	}
	return deg
}

// resolveNode maps a user query (an ID or a label) to a single node, using the
// same tiers as Query and breaking ties by ID for determinism. It returns the
// matched node and the tier (exact | prefix | substring) it matched at, so
// callers can distinguish an exact hit from a fuzzy substring resolution.
func resolveNode(g *Graph, query string) (*Node, string, bool) {
	q := NormalizeID(query)
	if q == "" {
		return nil, "", false
	}
	best := -1
	bestRank := 99
	bestTier := ""
	for i := range g.Nodes {
		tier := labelTier(g.Nodes[i], q)
		if tier == "" {
			continue
		}
		r := tierRank(tier)
		if r < bestRank || (r == bestRank && (best < 0 || g.Nodes[i].ID < g.Nodes[best].ID)) {
			bestRank = r
			bestTier = tier
			best = i
		}
	}
	if best < 0 {
		return nil, "", false
	}
	return &g.Nodes[best], bestTier, true
}
