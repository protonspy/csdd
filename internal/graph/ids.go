package graph

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// reNonWord collapses every run of non-word characters (anything that is not a
// Unicode letter or number) into a single underscore. Using the Unicode classes
// \p{L}\p{N} keeps CJK ideographs and accented letters intact rather than
// stripping them, which matters for non-English artifact titles.
var reNonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// reUnderscores squashes any accidental underscore runs left over after
// normalization into one.
var reUnderscores = regexp.MustCompile(`_+`)

// NormalizeID is the single canonical ID function — graphify's lesson #1: one
// place, imported by extraction and assembly, so the same artifact always yields
// the same node ID across rebuilds (no ghost duplicates). It applies NFKC so
// visually-identical Unicode spellings collapse, replaces non-word runs with
// underscores, and casefolds. It is idempotent: NormalizeID(NormalizeID(s)) == NormalizeID(s).
func NormalizeID(s string) string {
	s = norm.NFKC.String(s)
	s = reNonWord.ReplaceAllString(s, "_")
	s = reUnderscores.ReplaceAllString(s, "_")
	return strings.ToLower(strings.Trim(s, "_"))
}

// MakeID joins the given parts with underscores after normalizing each, dropping
// empties so a blank part never produces a doubled separator. The result is
// itself a normalized, idempotent ID.
func MakeID(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if n := NormalizeID(p); n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, "_")
}
