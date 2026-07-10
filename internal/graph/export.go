package graph

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/protonspy/csdd/internal/workspace"
)

// Canonical on-disk locations for the generated knowledge index. docs/graph/ is
// the only generated subtree inside docs/ (PLAN §2). The graph is persisted
// gzip-compressed (graph.json.gz): the node-link JSON is an opaque generated
// artifact, so storing it as a binary blob shrinks the tree ~6× and turns a merge
// conflict into a `csdd graph build` instead of a hand-merge of thousands of JSON
// lines. Every consumer (the CLI and the web dashboard) decompresses on read.
const (
	GraphDirRel  = "docs/graph"
	GraphGzRel   = "docs/graph/graph.json.gz"
	GraphHTMLRel = "docs/graph/graph.html"
	GraphLogRel  = "docs/graph/log.md"

	// graphJSONLegacyRel is the pre-compression plaintext file. WriteBuild removes
	// it on the first build so a tree upgraded from an older csdd never carries both.
	graphJSONLegacyRel = "docs/graph/graph.json"
)

// GraphGzPath is the absolute path of graph.json.gz under root.
func GraphGzPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(GraphGzRel))
}

// Marshal renders the graph as deterministic, human-readable NetworkX node-link
// JSON: one node and one edge object per line (clean git diffs, §5.9), attrs in
// sorted order, and NO wall-clock timestamp (byte-stable across rebuilds, R7.2).
func Marshal(g *Graph) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"directed\": %t,\n", g.Directed)
	fmt.Fprintf(&b, "  \"multigraph\": %t,\n", g.Multigraph)

	meta := g.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.MarshalIndent(meta, "  ", "  ")
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "  \"graph\": %s,\n", metaJSON)

	if err := writeArray(&b, "nodes", len(g.Nodes), func(i int) ([]byte, error) {
		return marshalNode(g.Nodes[i])
	}); err != nil {
		return nil, err
	}
	b.WriteString(",\n")
	if err := writeArray(&b, "links", len(g.Links), func(i int) ([]byte, error) {
		return marshalEdge(g.Links[i])
	}); err != nil {
		return nil, err
	}
	b.WriteString("\n}\n")
	return b.Bytes(), nil
}

func writeArray(b *bytes.Buffer, key string, n int, item func(int) ([]byte, error)) error {
	if n == 0 {
		fmt.Fprintf(b, "  %q: []", key)
		return nil
	}
	fmt.Fprintf(b, "  %q: [\n", key)
	for i := 0; i < n; i++ {
		raw, err := item(i)
		if err != nil {
			return err
		}
		b.WriteString("    ")
		b.Write(raw)
		if i < n-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]")
	return nil
}

// MarshalGz renders the deterministic node-link JSON (see Marshal) and gzips it.
// The compressed blob is itself byte-stable across rebuilds (R7.2): the payload
// is byte-stable, the compression level is fixed, and the 10-byte gzip header is
// pinned by zeroing ModTime (Go's default OS byte is already 255/unknown). The
// only thing that can shift the bytes is a Go toolchain upgrade changing DEFLATE
// output — a one-time reshuffle, not per-build churn.
func MarshalGz(g *Graph) ([]byte, error) {
	data, err := Marshal(g)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	zw.ModTime = time.Time{} // no wall-clock in the header — keep the blob byte-stable
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadGraphBytes reads docs/graph/graph.json.gz and returns the decompressed
// node-link JSON. It is the one reader every consumer goes through, so callers
// never touch the gzip framing directly (the "csdd decompresses to use the graph"
// contract). Returns the underlying os error (e.g. not-exist) when the file is
// absent, so callers can distinguish "no graph yet" from a corrupt blob.
func ReadGraphBytes(root string) ([]byte, error) {
	f, err := os.Open(GraphGzPath(root))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("graph.json.gz is not valid gzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	return io.ReadAll(zr)
}

// ReadGraph loads the persisted graph, decompressing and parsing it back into a
// *Graph via the node-link UnmarshalJSON. Build-time diagnostics (Pending,
// Warnings, Collisions) are not part of the wire format, so a reloaded graph
// carries none of them (see model.go).
func ReadGraph(root string) (*Graph, error) {
	data, err := ReadGraphBytes(root)
	if err != nil {
		return nil, err
	}
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// WriteBuild persists the graph to docs/graph/graph.json.gz and appends one line
// to docs/graph/log.md (R4.2). now is injected so the log entry is testable and
// the JSON itself carries no time. Returns the compressed bytes written.
func WriteBuild(root string, g *Graph, now time.Time) ([]byte, error) {
	data, err := MarshalGz(g)
	if err != nil {
		return nil, err
	}
	if err := workspace.AtomicWrite(GraphGzPath(root), data, 0o644); err != nil {
		return nil, err
	}
	// Migration: drop any pre-gzip graph.json a prior csdd wrote so the tree never
	// carries both the stale plaintext and the live compressed index.
	_ = os.Remove(filepath.Join(root, filepath.FromSlash(graphJSONLegacyRel)))
	if err := appendBuildLog(root, g, now); err != nil {
		return nil, err
	}
	return data, nil
}

// appendBuildLog appends a Karpathy-format heading to docs/graph/log.md, creating
// the file with a header when it does not yet exist.
func appendBuildLog(root string, g *Graph, now time.Time) error {
	logPath := filepath.Join(root, filepath.FromSlash(GraphLogRel))
	pending := len(g.Pending)
	entry := fmt.Sprintf("## [%s] build | %d nodes / %d edges (%d pending)\n",
		now.UTC().Format("2006-01-02"), len(g.Nodes), len(g.Links), pending)

	existing, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		header := "# csdd graph — build log\n\n" +
			"Append-only, chronological. One line per build. Parseable with\n" +
			"`grep \"^## \\[\" log.md | tail -5`.\n\n"
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(logPath, []byte(header+entry), 0o644)
	}
	if err != nil {
		return err
	}
	out := append(existing, '\n')
	out = append(out, []byte(entry)...)
	return os.WriteFile(logPath, out, 0o644)
}
