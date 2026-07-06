package graph

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/protonspy/csdd/internal/manifest"
	"github.com/protonspy/csdd/internal/workspace"
)

// StateRel is the incremental build state — per-source content hashes and their
// cached extraction fragments. It lives in .csdd/ (operational state), is local
// and regenerable, and is not committed (§5.9).
const StateRel = ".csdd/state.json"

// sourceCache is one source's cached contribution: the content hash it was
// extracted at and the fragments that extraction produced. Reusing it skips
// re-parsing an unchanged file (R13.2 "skip its re-extraction").
type sourceCache struct {
	Hash      string     `json:"hash"`
	Fragments []Fragment `json:"fragments"`
}

// buildState is the on-disk incremental cache.
type buildState struct {
	Version int                    `json:"version"`
	Sources map[string]sourceCache `json:"sources"`
}

// BuildIncremental builds the graph reusing cached fragments for unchanged
// sources: a source whose content hash is unchanged is not re-extracted; a
// changed source has its entire prior contribution replaced (replace-per-source);
// a deleted source is pruned from the cache (R13.2). The assembled result is
// byte-identical to a full Build, because the same fragments feed the same
// deterministic Assemble regardless of which were cached (R13.3).
func BuildIncremental(root string) (*Graph, error) {
	return buildIncrementalWith(root, defaultExtractors())
}

func buildIncrementalWith(root string, extractors []Extractor) (*Graph, error) {
	sources, err := collectSources(root, extractors)
	if err != nil {
		return nil, err
	}
	state := loadState(root)
	next := &buildState{Version: 1, Sources: map[string]sourceCache{}}

	var nodes []Node
	var edges []Edge
	for _, src := range sources {
		h := manifest.Hash(string(src.Content))
		if cached, ok := state.Sources[src.Path]; ok && cached.Hash == h {
			// Unchanged: reuse without re-parsing.
			next.Sources[src.Path] = cached
			for _, f := range cached.Fragments {
				nodes = append(nodes, f.Nodes...)
				edges = append(edges, f.Edges...)
			}
			continue
		}
		frags, err := dispatch(extractors, src)
		if err != nil {
			return nil, err
		}
		if frags == nil {
			frags = []Fragment{}
		}
		next.Sources[src.Path] = sourceCache{Hash: h, Fragments: frags}
		for _, f := range frags {
			nodes = append(nodes, f.Nodes...)
			edges = append(edges, f.Edges...)
		}
	}
	// Deleted sources simply vanish: next only contains current paths.

	g := assemble(root, nodes, edges)
	if err := saveState(root, next); err != nil {
		return nil, err
	}
	return g, nil
}

// assemble is the shared post-extraction pipeline (merge → ownership → resolve →
// references → sort → meta), factored out so full and incremental builds run the
// identical assembly and therefore produce identical output.
func assemble(root string, nodes []Node, edges []Edge) *Graph {
	g := New()
	g.Nodes = dedupNodes(nodes)
	g.Links = dedupEdges(edges)
	assembleSpecOwnership(g)
	resolveCitedPaths(g, root)
	resolveGoImports(g, root)
	g.Pending = resolveReferences(g)
	sortGraph(g)
	setMeta(g)
	return g
}

func statePath(root string) string {
	return filepath.Join(root, filepath.FromSlash(StateRel))
}

// loadState reads the incremental cache, returning an empty state when it is
// absent or unreadable (a corrupt cache just forces a full re-extraction).
func loadState(root string) *buildState {
	data, err := os.ReadFile(statePath(root))
	if err != nil {
		return &buildState{Version: 1, Sources: map[string]sourceCache{}}
	}
	var st buildState
	if err := json.Unmarshal(data, &st); err != nil || st.Sources == nil {
		return &buildState{Version: 1, Sources: map[string]sourceCache{}}
	}
	return &st
}

func saveState(root string, st *buildState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return workspace.AtomicWrite(statePath(root), data, 0o644)
}
