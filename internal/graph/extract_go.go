package graph

import (
	"path"
	"strings"
)

// goExtractor (M3) indexes Go source into code nodes via stdlib go/parser
// (syntax-only, cgo-free): one node per package, file, and top-level
// declaration, with containment and import edges. It excludes vendor/, hidden
// directories, and testdata/ fixtures by default, and tags _test.go files.
type goExtractor struct{}

func (goExtractor) Matches(p string) bool {
	if !strings.HasSuffix(p, ".go") {
		return false
	}
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "vendor" || seg == "testdata" {
			return false
		}
		if len(seg) > 1 && strings.HasPrefix(seg, ".") {
			return false // hidden directory
		}
	}
	return true
}
