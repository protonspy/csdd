package graph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
)

// includeStdImports controls whether standard-library imports become nodes/edges.
// Off by default (§5.7): std imports are noise for a project graph.
const includeStdImports = false

// Extract parses one Go file syntax-only (no go/types, no module resolution, so
// no network and no toolchain dependency) into code nodes and containment/import
// edges. The package node is re-emitted per file and deduped by ID in Assemble.
func (goExtractor) Extract(src Source) ([]Fragment, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src.Path, src.Content, parser.SkipObjectResolution)
	if err != nil {
		// A file that does not parse is skipped rather than failing the whole build
		// (honesty over recall — a syntactically broken file has no reliable structure).
		return nil, nil
	}
	isTest := strings.HasSuffix(src.Path, "_test.go")
	dir := path.Dir(src.Path)
	if dir == "." {
		dir = ""
	}
	pkgName := file.Name.Name
	pkgNodeID := goPkgID(dir)
	fileNodeID := MakeID("go", "file", src.Path)

	var frag Fragment
	frag.Nodes = append(frag.Nodes, Node{
		ID: pkgNodeID, Label: pkgName, FileType: TypeCode,
		SourceFile: dir, SourceLocation: "L1",
		Attrs: map[string]any{"kind": "package", "package": pkgName},
	})
	fileAttrs := map[string]any{"kind": "file"}
	if isTest {
		fileAttrs["test"] = true
	}
	frag.Nodes = append(frag.Nodes, Node{
		ID: fileNodeID, Label: path.Base(src.Path), FileType: TypeCode,
		SourceFile: src.Path, SourceLocation: "L1", Attrs: fileAttrs,
	})
	frag.Edges = append(frag.Edges, Edge{
		Source: pkgNodeID, Target: fileNodeID, Relation: RelContains,
		Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
	})

	for _, decl := range file.Decls {
		for _, d := range goDecls(src.Path, decl, fset, isTest) {
			frag.Nodes = append(frag.Nodes, d)
			frag.Edges = append(frag.Edges, Edge{
				Source: fileNodeID, Target: d.ID, Relation: RelContains,
				Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
			})
		}
	}

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if importPath == "" {
			continue
		}
		if isStdImport(importPath) && !includeStdImports {
			continue
		}
		impID := MakeID("go", "import", importPath)
		frag.Nodes = append(frag.Nodes, Node{
			ID: impID, Label: importPath, FileType: TypeCode,
			SourceFile: importPath, SourceLocation: "L1",
			Attrs: map[string]any{"kind": "import", "import_path": importPath},
		})
		frag.Edges = append(frag.Edges, Edge{
			Source: pkgNodeID, Target: impID, Relation: RelImports,
			Confidence: Extracted, ConfidenceScore: 1.0, SourceFile: src.Path,
		})
	}
	return []Fragment{frag}, nil
}

// goPkgID keys a package node by its directory (relative to the workspace root).
func goPkgID(dir string) string {
	if dir == "" {
		return MakeID("go", "pkg", "root")
	}
	return MakeID("go", "pkg", dir)
}

// goDecls turns one top-level declaration into code nodes (a func/method, a type,
// or exported vars/consts).
func goDecls(filePath string, decl ast.Decl, fset *token.FileSet, isTest bool) []Node {
	loc := "L" + itoa(fset.Position(decl.Pos()).Line)
	mkNode := func(key, label, kind string) Node {
		attrs := map[string]any{"kind": kind}
		if isTest {
			attrs["test"] = true
		}
		return Node{
			ID: MakeID("go", "decl", filePath, key), Label: label, FileType: TypeCode,
			SourceFile: filePath, SourceLocation: loc, Attrs: attrs,
		}
	}
	switch d := decl.(type) {
	case *ast.FuncDecl:
		name := d.Name.Name
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := receiverName(d.Recv.List[0].Type)
			key := recv + "." + name
			return []Node{mkNode(key, "func "+key, "method")}
		}
		return []Node{mkNode(name, "func "+name, "func")}
	case *ast.GenDecl:
		var out []Node
		switch d.Tok {
		case token.TYPE:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					out = append(out, mkNode(ts.Name.Name, "type "+ts.Name.Name, "type"))
				}
			}
		case token.VAR, token.CONST:
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					if ast.IsExported(n.Name) {
						out = append(out, mkNode(n.Name, kind+" "+n.Name, kind))
					}
				}
			}
		}
		return out
	}
	return nil
}

// receiverName returns the (possibly pointer) receiver type name for a method.
func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // generic receiver T[P]
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	}
	return "recv"
}

// isStdImport reports whether an import path is from the standard library (its
// first path segment carries no dot, e.g. "fmt" or "encoding/json").
func isStdImport(importPath string) bool {
	first := importPath
	if i := strings.IndexByte(importPath, '/'); i >= 0 {
		first = importPath[:i]
	}
	return !strings.Contains(first, ".")
}
