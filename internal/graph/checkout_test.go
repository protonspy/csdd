package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawTree lays down a workspace with the given root-relative files and
// returns its root. Directories named in dirs are created empty.
func writeRawTree(t *testing.T, files map[string]string, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// rawSourcePaths returns the docs/raw/ paths collectSources claimed, which is
// exactly the set that becomes raw_source nodes and, unprocessed, becomes
// `csdd wiki lint` findings.
func rawSourcePaths(t *testing.T, root string) []string {
	t.Helper()
	sources, _, err := collectSources(root, defaultExtractors())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range sources {
		if strings.HasPrefix(s.Path, "docs/raw/") {
			out = append(out, s.Path)
		}
	}
	return out
}

func assertRawSources(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("raw sources = %v, want %v", got, want)
	}
}

// A source checkout is material the codewiki skill reads, not a source to
// ingest. Indexing it file-by-file is what turned `csdd wiki lint` into a wall
// of unprocessed-source findings for every repository dropped in the dropzone.
func TestSourceCheckoutIsNotIndexedFileByFile(t *testing.T) {
	root := writeRawTree(t, map[string]string{
		"docs/raw/widget/go.mod":    "module widget\n",
		"docs/raw/widget/main.go":   "package main\n",
		"docs/raw/widget/src/a.go":  "package src\n",
		"docs/raw/widget/README.md": "# Widget\n",
		"docs/raw/acme-widget.md":   "<!-- csdd-codewiki v1 | acme/widget -->\n",
		"docs/raw/article.md":       "An article.\n",
		"docs/wiki/index.md":        "# Index\n",
	})
	assertRawSources(t, rawSourcePaths(t, root), []string{
		"docs/raw/acme-widget.md", "docs/raw/article.md",
	})
}

func TestCheckoutIsDetectedByAnyMarker(t *testing.T) {
	for _, marker := range []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml", "Gemfile"} {
		root := writeRawTree(t, map[string]string{
			"docs/raw/proj/" + marker:  "x\n",
			"docs/raw/proj/deep/f.txt": "y\n",
		})
		if got := rawSourcePaths(t, root); len(got) != 0 {
			t.Errorf("%s should mark a checkout, still indexed %v", marker, got)
		}
	}
	// A bare .git/ is enough even with no manifest — the walker never descends
	// into .git itself, so detection has to stat for it.
	root := writeRawTree(t, map[string]string{"docs/raw/proj/f.txt": "y\n"}, "docs/raw/proj/.git")
	if got := rawSourcePaths(t, root); len(got) != 0 {
		t.Errorf(".git should mark a checkout, still indexed %v", got)
	}
}

// A directory of dropped notes is not a checkout, and must keep being indexed
// file by file — that is the behaviour every existing corpus relies on.
func TestPlainDirectoryUnderRawIsStillIndexed(t *testing.T) {
	root := writeRawTree(t, map[string]string{
		"docs/raw/notes/a.md":       "A\n",
		"docs/raw/notes/b.md":       "B\n",
		"docs/raw/notes/deep/c.txt": "C\n",
	})
	assertRawSources(t, rawSourcePaths(t, root), []string{
		"docs/raw/notes/a.md", "docs/raw/notes/b.md", "docs/raw/notes/deep/c.txt",
	})
}

// An archive is material once it has been unpacked beside itself; one nobody
// extracted is still an unprocessed source, and must stay flagged.
func TestOnlyExtractedArchivesAreTreatedAsMaterial(t *testing.T) {
	root := writeRawTree(t, map[string]string{
		"docs/raw/widget.zip":       "PK\n",
		"docs/raw/widget/go.mod":    "module widget\n",
		"docs/raw/orphan.zip":       "PK\n",
		"docs/raw/bundle.tar.gz":    "gz\n",
		"docs/raw/bundle/README.md": "# Bundle\n",
	})
	// bundle/ carries no checkout marker, so its files stay indexed; the point
	// here is that bundle.tar.gz itself drops out and orphan.zip does not.
	assertRawSources(t, rawSourcePaths(t, root), []string{
		"docs/raw/bundle/README.md", "docs/raw/orphan.zip",
	})
}

func TestReadmeExemptionStillHolds(t *testing.T) {
	root := writeRawTree(t, map[string]string{
		"docs/raw/README.md": "# dropzone\n",
		"docs/raw/real.md":   "content\n",
	})
	assertRawSources(t, rawSourcePaths(t, root), []string{"docs/raw/real.md"})
}
