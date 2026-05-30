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
