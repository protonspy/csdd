package frontmatter

import (
	"reflect"
	"testing"
)

func TestParseNoFrontmatter(t *testing.T) {
	fm := Parse("# Hello\nbody text\n")
	if len(fm.Fields) != 0 {
		t.Errorf("expected empty fields, got %v", fm.Fields)
	}
	if fm.Body != "# Hello\nbody text\n" {
		t.Errorf("body mismatch: %q", fm.Body)
	}
}

func TestParseScalarFields(t *testing.T) {
	in := "---\n" +
		"inclusion: always\n" +
		"name: api-design\n" +
		"description: \"REST API patterns.\"\n" +
		"---\n\n# Body\n"
	fm := Parse(in)
	if got := fm.AsString("inclusion", ""); got != "always" {
		t.Errorf("inclusion = %q", got)
	}
	if got := fm.AsString("name", ""); got != "api-design" {
		t.Errorf("name = %q", got)
	}
	if got := fm.AsString("description", ""); got != "REST API patterns." {
		t.Errorf("description = %q", got)
	}
	if fm.Body == "" {
		t.Errorf("body should not be empty")
	}
}

func TestParseInlineArray(t *testing.T) {
	in := "---\nfileMatchPattern: [\"src/api/**/*\", \"**/*Controller.*\"]\n---\n"
	fm := Parse(in)
	got := fm.AsStringSlice("fileMatchPattern")
	want := []string{"src/api/**/*", "**/*Controller.*"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AsStringSlice = %v, want %v", got, want)
	}
}

func TestParseEmptyArrayAndBool(t *testing.T) {
	in := "---\nflag: true\nempty: []\nflag2: false\n---\n"
	fm := Parse(in)
	if v, ok := fm.Fields["flag"].(bool); !ok || v != true {
		t.Errorf("flag bool parse failed: %v", fm.Fields["flag"])
	}
	if v, ok := fm.Fields["flag2"].(bool); !ok || v != false {
		t.Errorf("flag2 bool parse failed: %v", fm.Fields["flag2"])
	}
	if v, ok := fm.Fields["empty"].([]string); !ok || len(v) != 0 {
		t.Errorf("empty list parse failed: %v", fm.Fields["empty"])
	}
}

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	in := "---\n# a comment\n\nname: thing\n---\n"
	fm := Parse(in)
	if got := fm.AsString("name", ""); got != "thing" {
		t.Errorf("name = %q", got)
	}
	if len(fm.Fields) != 1 {
		t.Errorf("expected 1 field, got %d", len(fm.Fields))
	}
}

func TestParseUnclosedFrontmatter(t *testing.T) {
	in := "---\nname: thing\n# no closing fence\n"
	fm := Parse(in)
	// Unclosed -> treat whole input as body, no fields.
	if len(fm.Fields) != 0 {
		t.Errorf("unclosed frontmatter must yield no fields")
	}
}

func TestAsStringFallback(t *testing.T) {
	fm := Frontmatter{Fields: map[string]any{"x": 42}}
	if got := fm.AsString("x", "def"); got != "def" {
		t.Errorf("non-string field should return default, got %q", got)
	}
	if got := fm.AsString("missing", "def"); got != "def" {
		t.Errorf("missing key should return default, got %q", got)
	}
}

func TestAsStringSliceFallback(t *testing.T) {
	fm := Frontmatter{Fields: map[string]any{"x": "not-a-slice"}}
	if got := fm.AsStringSlice("x"); got != nil {
		t.Errorf("non-slice field should return nil, got %v", got)
	}
	if got := fm.AsStringSlice("missing"); got != nil {
		t.Errorf("missing key should return nil")
	}
}

func TestCSVRespectsQuotes(t *testing.T) {
	in := "---\nfileMatchPattern: [\"a, b\", 'c', d]\n---\n"
	fm := Parse(in)
	got := fm.AsStringSlice("fileMatchPattern")
	want := []string{"a, b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CSV: got %v want %v", got, want)
	}
}

func TestParseCRLFFrontmatter(t *testing.T) {
	in := "---\r\ninclusion: always\r\nname: api-design\r\n---\r\n\r\n# Body\r\n"
	fm := Parse(in)
	if got := fm.AsString("inclusion", ""); got != "always" {
		t.Errorf("CRLF inclusion = %q, want always", got)
	}
	if got := fm.AsString("name", ""); got != "api-design" {
		t.Errorf("CRLF name = %q, want api-design", got)
	}
}

func TestParseBOMFrontmatter(t *testing.T) {
	bom := string(rune(0xFEFF))
	in := bom + "---\ninclusion: always\n---\n# Body\n"
	fm := Parse(in)
	if got := fm.AsString("inclusion", ""); got != "always" {
		t.Errorf("BOM-prefixed frontmatter did not parse: inclusion = %q", got)
	}
}

func TestParseStripsInlineComment(t *testing.T) {
	in := "---\ninclusion: always # standard mode\nname: thing\n---\n"
	fm := Parse(in)
	if got := fm.AsString("inclusion", ""); got != "always" {
		t.Errorf("inline comment not stripped: inclusion = %q", got)
	}
}

func TestParseHashInsideQuotesKept(t *testing.T) {
	in := "---\ndescription: \"has a # hash\"\n---\n"
	fm := Parse(in)
	if got := fm.AsString("description", ""); got != "has a # hash" {
		t.Errorf("quoted hash mangled: description = %q", got)
	}
}

func TestParseFoldedBlockScalar(t *testing.T) {
	in := "---\ndescription: >\n  first line\n  second line\nname: thing\n---\n"
	fm := Parse(in)
	if got := fm.AsString("description", ""); got != "first line second line" {
		t.Errorf("folded block scalar = %q, want %q", got, "first line second line")
	}
	if got := fm.AsString("name", ""); got != "thing" {
		t.Errorf("field after block scalar lost: name = %q", got)
	}
}

func TestParseLiteralBlockScalar(t *testing.T) {
	in := "---\ndescription: |\n  line one\n  line two\n---\n"
	fm := Parse(in)
	if got := fm.AsString("description", ""); got != "line one\nline two" {
		t.Errorf("literal block scalar = %q, want %q", got, "line one\nline two")
	}
}

func TestParseEscapedQuotesInArray(t *testing.T) {
	in := "---\ntools: [\"a, b\", c]\n---\n"
	fm := Parse(in)
	got := fm.AsStringSlice("tools")
	if len(got) != 2 || got[0] != "a, b" || got[1] != "c" {
		t.Errorf("array with embedded comma mis-split: %v", got)
	}
}
