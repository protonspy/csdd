package graph

import (
	"os"
	"strings"
	"testing"
)

func TestExportHTMLSelfContained(t *testing.T) {
	g, dir := buildFixture(t)
	path, err := ExportHTML(dir, g, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)

	// Self-contained: no external asset is fetched (R6.2). No src=/href= to a
	// remote host, no CDN link.
	for _, bad := range []string{"src=\"http", "href=\"http", "src='http", "//cdn", "unpkg.com", "jsdelivr"} {
		if strings.Contains(html, bad) {
			t.Errorf("graph.html must be self-contained; found %q", bad)
		}
	}
	// The graph payload is embedded and "</" is escaped so the JSON can never
	// terminate the <script> element early (R6.2 / task 13.1).
	if strings.Contains(html, "</script>") && !strings.Contains(html, "const GRAPH =") {
		t.Errorf("expected embedded graph data")
	}
	if !strings.Contains(html, "const GRAPH =") {
		t.Errorf("graph data not embedded")
	}
	// No raw "</" from the data survived escaping inside the data payload region.
	dataStart := strings.Index(html, "const GRAPH =")
	dataEnd := strings.Index(html[dataStart:], "\n") + dataStart
	if strings.Contains(html[dataStart:dataEnd], "</") {
		t.Errorf("unescaped </ in embedded data line")
	}
}
