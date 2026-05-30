// Package frontmatter parses the minimal YAML subset used by Claude Code
// artifacts: scalar strings, booleans, and inline arrays like `["a", "b"]`.
// Multi-line YAML, anchors, and nested mappings are out of scope by design.
package frontmatter

import (
	"strings"
)

// Frontmatter holds parsed key/value pairs plus the remaining markdown body.
type Frontmatter struct {
	Fields map[string]any
	Body   string
}

// Parse extracts YAML frontmatter delimited by `---` fences at the head of text.
// When no frontmatter is present, the entire text is returned in Body.
func Parse(text string) Frontmatter {
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
	for _, raw := range lines[1:end] {
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		idx := strings.Index(raw, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(raw[:idx])
		value := strings.TrimSpace(raw[idx+1:])
		fields[key] = parseValue(value)
	}
	body := strings.Join(lines[end+1:], "\n")
	return Frontmatter{Fields: fields, Body: strings.TrimLeft(body, "\n")}
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
