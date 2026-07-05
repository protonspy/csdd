// Package frontmatter parses the minimal YAML subset used by Claude Code
// artifacts: scalar strings, booleans, and inline arrays like `["a", "b"]`.
// Multi-line YAML, anchors, and nested mappings are out of scope by design.
package frontmatter

import (
	"strings"

	"github.com/protonspy/csdd/internal/textutil"
)

// Frontmatter holds parsed key/value pairs plus the remaining markdown body.
type Frontmatter struct {
	Fields map[string]any
	Body   string
}

// Parse extracts YAML frontmatter delimited by `---` fences at the head of text.
// When no frontmatter is present, the entire text is returned in Body. Input is
// line-ending normalized and BOM-stripped first, so a CRLF file written by a
// Windows editor (or a file Notepad prefixed with a UTF-8 BOM) parses identically
// to an LF one — otherwise the opening `---` fence would fail to match and the
// whole file would be treated as body with no fields.
func Parse(text string) Frontmatter {
	text = textutil.NormalizeNewlines(text)
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Frontmatter{Fields: map[string]any{}, Body: text}
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Frontmatter{Fields: map[string]any{}, Body: text}
	}
	fields := map[string]any{}
	for i := 1; i < end; {
		raw := lines[i]
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			i++
			continue
		}
		idx := strings.Index(raw, ":")
		if idx < 0 {
			i++
			continue
		}
		key := strings.TrimSpace(raw[:idx])
		value := strings.TrimSpace(raw[idx+1:])
		// A YAML block scalar (`key: >` / `key: |`) folds the following
		// more-indented lines into the value; without this they would be dropped
		// and the field left as a bare ">" or "|".
		if isBlockScalarIndicator(value) {
			folded, next := readBlockScalar(lines, i+1, end, value)
			fields[key] = folded
			i = next
			continue
		}
		fields[key] = parseValue(stripInlineComment(value))
		i++
	}
	body := strings.Join(lines[end+1:], "\n")
	return Frontmatter{Fields: fields, Body: strings.TrimLeft(body, "\n")}
}

// stripInlineComment removes a trailing "# comment" from an unquoted scalar. Per
// YAML, a '#' begins a comment only when it is outside quotes and preceded by
// whitespace, so `inclusion: always # standard` yields "always", while a '#'
// inside quotes (or with no leading space) stays part of the value.
func stripInlineComment(v string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble && i > 0 && (v[i-1] == ' ' || v[i-1] == '\t'):
			return strings.TrimRight(v[:i], " \t")
		}
	}
	return v
}

// isBlockScalarIndicator reports whether a value is a lone block-scalar header:
// '|' or '>' optionally followed by chomping/indentation indicators (-, +, digit)
// and an inline comment. `> some text` (letters after the indicator) is not one.
func isBlockScalarIndicator(v string) bool {
	if v == "" || (v[0] != '|' && v[0] != '>') {
		return false
	}
	rest := strings.TrimSpace(stripInlineComment(v[1:]))
	for _, c := range rest {
		if c != '-' && c != '+' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// readBlockScalar folds the block-scalar body starting at lines[start] up to end.
// A folded scalar ('>') joins lines with spaces; a literal one ('|') keeps
// newlines. It returns the value and the index of the first line after the block.
func readBlockScalar(lines []string, start, end int, indicator string) (string, int) {
	folded := indicator[0] == '>'
	var content []string
	baseIndent := -1
	i := start
	for ; i < end; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			content = append(content, "")
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if baseIndent == -1 {
			baseIndent = indent
		}
		if indent < baseIndent {
			break // dedented to a sibling key: the block ends here
		}
		content = append(content, line[baseIndent:])
	}
	for len(content) > 0 && content[len(content)-1] == "" {
		content = content[:len(content)-1]
	}
	if folded {
		return strings.Join(strings.Fields(strings.Join(content, " ")), " "), i
	}
	return strings.Join(content, "\n"), i
}

func parseValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		if inner == "" {
			return []string{}
		}
		parts := splitCSV(inner)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, stripQuotes(strings.TrimSpace(p)))
		}
		return out
	}
	if low := strings.ToLower(raw); low == "true" || low == "false" {
		return low == "true"
	}
	return stripQuotes(raw)
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitCSV honors quoted segments so commas inside strings don't split values.
func splitCSV(line string) []string {
	var out []string
	var buf strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote != 0 {
			buf.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			buf.WriteByte(ch)
			continue
		}
		if ch == ',' {
			out = append(out, buf.String())
			buf.Reset()
			continue
		}
		buf.WriteByte(ch)
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

// AsString returns the value for key as a string, falling back to def.
func (fm Frontmatter) AsString(key, def string) string {
	v, ok := fm.Fields[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

// AsStringSlice returns the value for key as a string slice, falling back to nil.
func (fm Frontmatter) AsStringSlice(key string) []string {
	v, ok := fm.Fields[key]
	if !ok {
		return nil
	}
	if s, ok := v.([]string); ok {
		return s
	}
	return nil
}
