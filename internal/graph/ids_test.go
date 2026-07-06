package graph

import "testing"

func TestNormalizeID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase spaces", "Album Service", "album_service"},
		{"trims edge underscores", "  --Foo--  ", "foo"},
		{"collapses runs", "a...b   c", "a_b_c"},
		{"casefold", "REQ.1.2", "req_1_2"},
		{"keeps digits", "Requirement 1", "requirement_1"},
		{"keeps accents", "Café Ação", "café_ação"},
		{"keeps cjk", "設計 node", "設計_node"},
		{"empty", "", ""},
		{"only punctuation", "---", ""},
		{"nfkc fold ligature", "ﬁle", "file"},
		{"nfkc fullwidth", "ＡＢ", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeID(c.in)
			if got != c.want {
				t.Fatalf("NormalizeID(%q) = %q, want %q", c.in, got, c.want)
			}
			// Idempotence: normalizing an already-normalized ID is a no-op.
			if again := NormalizeID(got); again != got {
				t.Fatalf("NormalizeID not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestMakeID(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"crit", "graph-index", "1", "2"}, "crit_graph_index_1_2"},
		{[]string{"task", "foo", "1.1"}, "task_foo_1_1"},
		{[]string{"design", "foo", "AlbumService"}, "design_foo_albumservice"},
		{[]string{"", "spec", ""}, "spec"},      // empty parts dropped
		{[]string{"a", "", "b"}, "a_b"},         // no doubled separators
		{[]string{"Café", "Ação"}, "café_ação"}, // unicode preserved
	}
	for _, c := range cases {
		got := MakeID(c.parts...)
		if got != c.want {
			t.Fatalf("MakeID(%v) = %q, want %q", c.parts, got, c.want)
		}
		if again := NormalizeID(got); again != got {
			t.Fatalf("MakeID output not normalized-stable: %q -> %q", got, again)
		}
	}
}
