package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/protonspy/csdd/internal/workspace"
)

// Canonical on-disk locations for the generated knowledge index. docs/graph/ is
// the only generated subtree inside docs/ (PLAN §2).
const (
	GraphDirRel  = "docs/graph"
	GraphJSONRel = "docs/graph/graph.json"
	GraphHTMLRel = "docs/graph/graph.html"
	GraphLogRel  = "docs/graph/log.md"
)

// GraphJSONPath is the absolute path of graph.json under root.
func GraphJSONPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(GraphJSONRel))
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

// WriteBuild persists the graph to docs/graph/graph.json and appends one line to
// docs/graph/log.md (R4.2). now is injected so the log entry is testable and the
// JSON itself carries no time. Returns the JSON bytes written.
func WriteBuild(root string, g *Graph, now time.Time) ([]byte, error) {
	data, err := Marshal(g)
	if err != nil {
		return nil, err
	}
	if err := workspace.AtomicWrite(GraphJSONPath(root), data, 0o644); err != nil {
		return nil, err
	}
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
