// Package templater renders the embedded artifact templates.
// Templates live next to this package and are embedded at compile time so
// the kspec binary is fully self-contained. FS is exported so tests and
// tools can reuse the same template tree the production binary uses.
package templater

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"text/template"
)

//go:embed all:templates
var FS embed.FS

// Render loads a template by its FS-relative path and executes it with data.
// The function uses Go's text/template "{{ }}" syntax; passing nil data is
// valid for static templates. Accepting fs.FS (interface) instead of embed.FS
// (struct) lets tests substitute an in-memory tree via testing/fstest.
func Render(efs fs.FS, path string, data any) (string, error) {
	raw, err := fs.ReadFile(efs, path)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", path, err)
	}
	tmpl, err := template.New(path).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", path, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", path, err)
	}
	return buf.String(), nil
}

// Static returns the raw text of a template without rendering, useful for
// templates that contain no placeholders.
func Static(efs fs.FS, path string) (string, error) {
	raw, err := fs.ReadFile(efs, path)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", path, err)
	}
	return string(raw), nil
}

// RuleFiles enumerates every .kiro/settings/rules/ template, returning a
// filename → rendered-content map. All rule templates are static.
func RuleFiles(efs fs.FS) (map[string]string, error) {
	const dir = "templates/rules"
	entries, err := fs.ReadDir(efs, dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := dir + "/" + e.Name()
		raw, err := fs.ReadFile(efs, path)
		if err != nil {
			return nil, err
		}
		// Strip the ".tmpl" suffix when emitting on disk.
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		out[name] = string(raw)
	}
	return out, nil
}

// WorkflowTemplateFiles returns the versioned templates that belong under
// .kiro/settings/templates/. The embedded tree uses implementation-oriented
// names; this function emits the canonical on-disk layout from the guide.
func WorkflowTemplateFiles(efs fs.FS) (map[string]string, error) {
	out := map[string]string{}

	specEntries, err := fs.ReadDir(efs, "templates/spec")
	if err != nil {
		return nil, err
	}
	for _, e := range specEntries {
		if e.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(efs, "templates/spec/"+e.Name())
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		if name == "spec.json" {
			name = "init.json"
		}
		out["specs/"+name] = string(raw)
	}

	steeringEntries, err := fs.ReadDir(efs, "templates/steering")
	if err != nil {
		return nil, err
	}
	for _, e := range steeringEntries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		// templates/steering/custom.md.tmpl is the *render* template: it holds
		// Go text/template directives that `steering create` executes. The
		// on-disk reference under steering-custom/ must be clean, fill-in
		// markdown, so it comes from its own template (below), never the raw
		// render source — otherwise it ships with `{{...}}` directives exposed.
		if name == "custom.md" {
			continue
		}
		raw, err := fs.ReadFile(efs, "templates/steering/"+e.Name())
		if err != nil {
			return nil, err
		}
		out["steering/"+name] = string(raw)
	}

	customRef, err := fs.ReadFile(efs, "templates/steering-custom/custom.md.tmpl")
	if err != nil {
		return nil, err
	}
	out["steering-custom/custom.md"] = string(customRef)

	return out, nil
}
