package textutil

import "testing"

func TestNormalizeNewlines(t *testing.T) {
	bom := string(rune(0xFEFF)) // UTF-8 BOM, built here to keep the source ASCII
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lf unchanged", "a\nb\n", "a\nb\n"},
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
		{"lone cr", "a\rb\r", "a\nb\n"},
		{"mixed", "a\r\nb\nc\r", "a\nb\nc\n"},
		{"bom stripped", bom + "---\n", "---\n"},
		{"bom plus crlf", bom + "a\r\nb", "a\nb"},
		{"empty", "", ""},
		{"bom mid-string kept", "a" + bom + "b", "a" + bom + "b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeNewlines(c.in); got != c.want {
				t.Errorf("NormalizeNewlines(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
