package graph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoExtractor(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/proj\n\ngo 1.22\n",
		"internal/store/store.go": `package store

import "example.com/proj/internal/db"

type Store struct{ db *db.DB }

func New() *Store { return &Store{} }

func (s *Store) Save(x int) error { return nil }

const Version = "1"
`,
		"internal/db/db.go": `package db

type DB struct{}

func Open() *DB { return &DB{} }
`,
		"internal/store/store_test.go": `package store

import "testing"

func TestSave(t *testing.T) {}
`,
		"vendor/lib/lib.go":            "package lib\nfunc F() {}\n",
		"internal/store/testdata/x.go": "package x\nfunc G() {}\n",
		// A spec that cites a real Go file — should resolve code_ref → code.
		"specs/f/tasks.md":  "# Tasks\n- [ ] 1. Build store `internal/store/store.go`\n  - _Requirements: 1.1_\n",
		"specs/f/spec.json": `{"feature_name":"f"}`,
	}
	for rel, c := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Package/file/decl nodes exist (R12.1).
	pkgID := goPkgID("internal/store")
	if nodeByID(g, pkgID) == nil {
		t.Fatalf("missing store package node %s", pkgID)
	}
	fileID := MakeID("go", "file", "internal/store/store.go")
	if nodeByID(g, fileID) == nil {
		t.Fatalf("missing store.go file node")
	}
	saveID := MakeID("go", "decl", "internal/store/store.go", "Store.Save")
	if n := nodeByID(g, saveID); n == nil {
		t.Fatalf("missing method decl node %s", saveID)
	} else if n.Attrs["kind"] != "method" {
		t.Errorf("Save should be a method; got %v", n.Attrs["kind"])
	}

	// Containment edges (R12.2).
	if !hasEdge(g, pkgID, RelContains, fileID) {
		t.Errorf("missing contains pkg → file")
	}
	if !hasEdge(g, fileID, RelContains, saveID) {
		t.Errorf("missing contains file → Save")
	}

	// Import edge resolved to the in-module package (R12.2).
	if !hasEdge(g, pkgID, RelImports, goPkgID("internal/db")) {
		t.Errorf("missing imports store → db (resolved)")
	}

	// Exclusions: vendor/ and testdata/ produce no nodes (R12.4).
	if nodeByID(g, goPkgID("vendor/lib")) != nil {
		t.Errorf("vendor/ must be excluded")
	}
	if nodeByID(g, goPkgID("internal/store/testdata")) != nil {
		t.Errorf("testdata/ must be excluded")
	}

	// _test.go included but tagged (R12.4).
	testFileID := MakeID("go", "file", "internal/store/store_test.go")
	if n := nodeByID(g, testFileID); n == nil {
		t.Errorf("_test.go file should be indexed")
	} else if n.Attrs["test"] != true {
		t.Errorf("_test.go file should be tagged test=true; got %v", n.Attrs)
	}

	// Cited path resolved code_ref → real code file node (R12.3): the edge now
	// targets the code file, and the placeholder code_ref is gone.
	if !hasEdge(g, taskID("f", "1"), RelReuses, fileID) {
		t.Errorf("cited path should resolve to the code file node via reuses")
	}
	if nodeByID(g, codeRefID("internal/store/store.go")) != nil {
		t.Errorf("placeholder code_ref should be replaced by the code node")
	}
}
