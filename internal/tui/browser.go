package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/protonspy/csdd/internal/paths"
)

// browserItem represents one navigable artifact (a file under the workspace).
type browserItem struct {
	kind  string // "steering" | "spec" | "skill" | "agent"
	label string // displayed label
	path  string // absolute path to read for preview
}

type browserModel struct {
	root   string
	items  []browserItem
	cursor int
	err    string

	// previewPath/previewRaw cache the currently-selected file's contents so the
	// render path (View) never touches disk. The file is read once, in Update,
	// when the selection changes — critical on WSL's /mnt/c 9p mount where a
	// per-frame os.ReadFile made scrolling visibly janky.
	previewPath string
	previewRaw  string
	previewErr  bool
}

func newBrowser(root string) browserModel {
	b := browserModel{root: root}
	b.refresh()
	b.loadPreview()
	return b
}

// loadPreview reads the currently-selected item's file into the cache. It is a
// no-op when the cached path already matches, so repeated calls are cheap.
func (b *browserModel) loadPreview() {
	if b.cursor < 0 || b.cursor >= len(b.items) {
		b.previewPath, b.previewRaw, b.previewErr = "", "", false
		return
	}
	path := b.items[b.cursor].path
	if path == b.previewPath && !b.previewErr {
		return
	}
	data, err := os.ReadFile(path)
	b.previewPath = path
	if err != nil {
		b.previewRaw, b.previewErr = "could not read "+path, true
		return
	}
	b.previewRaw, b.previewErr = string(data), false
}

func (b *browserModel) refresh() {
	b.items = nil
	b.err = ""
	// steering
	if entries, err := os.ReadDir(paths.Steering(b.root)); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			b.items = append(b.items, browserItem{
				kind: "steering", label: n,
				path: filepath.Join(paths.Steering(b.root), n),
			})
		}
	}
	// specs (show spec.json for now; user can navigate further if we extend)
	if entries, err := os.ReadDir(paths.Specs(b.root)); err == nil {
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			specDir := filepath.Join(paths.Specs(b.root), n)
			files, _ := os.ReadDir(specDir)
			var artifacts []string
			for _, f := range files {
				if !f.IsDir() {
					artifacts = append(artifacts, f.Name())
				}
			}
			sort.Strings(artifacts)
			for _, a := range artifacts {
				b.items = append(b.items, browserItem{
					kind: "spec", label: n + "/" + a,
					path: filepath.Join(specDir, a),
				})
			}
		}
	}
	// skills (.claude/skills/)
	skillsBase := paths.Skills(b.root)
	if entries, err := os.ReadDir(skillsBase); err == nil {
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			b.items = append(b.items, browserItem{
				kind: "skill", label: n,
				path: filepath.Join(skillsBase, n, "SKILL.md"),
			})
		}
	}
	// agents (.claude/agents/)
	agentsBase := paths.Agents(b.root)
	if entries, err := os.ReadDir(agentsBase); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			b.items = append(b.items, browserItem{
				kind: "agent", label: n,
				path: filepath.Join(agentsBase, n),
			})
		}
	}
	// mcp servers (.mcp.json) — one item per configured server,
	// all previewing the shared config file.
	mcpPath := paths.MCP(b.root)
	if data, err := os.ReadFile(mcpPath); err == nil {
		var cfg struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(data, &cfg) == nil {
			var names []string
			for n := range cfg.MCPServers {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				b.items = append(b.items, browserItem{
					kind: "mcp", label: n, path: mcpPath,
				})
			}
		}
	}
}

func (b browserModel) Init() tea.Cmd { return nil }

func (b browserModel) Update(msg tea.Msg) (browserModel, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}
	switch k.String() {
	case "esc":
		return b, cmdBack()
	case "up", "k":
		if b.cursor > 0 {
			b.cursor--
			b.loadPreview()
		}
	case "down", "j":
		if b.cursor < len(b.items)-1 {
			b.cursor++
			b.loadPreview()
		}
	case "r":
		b.refresh()
		b.loadPreview()
	}
	return b, nil
}

func (b browserModel) View(width, height int) string {
	if len(b.items) == 0 {
		return Styles.Hint.Render("no artifacts yet — create something from the menu first") +
			"\n\n" + Styles.Hint.Render("esc returns to menu")
	}
	// Compute pane widths (avoid degenerate sizes on tiny terminals).
	listW := width / 3
	if listW < 24 {
		listW = 24
	}
	previewW := width - listW - 6
	if previewW < 24 {
		previewW = 60
	}
	bodyH := height - 8
	if bodyH < 8 {
		bodyH = 16
	}

	var list strings.Builder
	list.WriteString(Styles.Heading.Render("Artifacts") + "\n\n")
	visible := bodyH - 2
	start := 0
	if b.cursor >= visible {
		start = b.cursor - visible + 1
	}
	end := start + visible
	if end > len(b.items) {
		end = len(b.items)
	}
	for i := start; i < end; i++ {
		it := b.items[i]
		prefix := "  "
		line := fmt.Sprintf("%s · %s", Styles.Dim.Render(it.kind), it.label)
		if i == b.cursor {
			prefix = Styles.Selected.Render("▸ ")
			line = Styles.Selected.Render(it.label) + "  " + Styles.Dim.Render(it.kind)
		}
		list.WriteString(prefix + line + "\n")
	}

	preview := b.previewText(previewW, bodyH-2)
	listBox := Styles.Box.Width(listW).Height(bodyH).Render(list.String())
	previewBox := Styles.Box.Width(previewW).Height(bodyH).Render(
		Styles.Heading.Render(b.items[b.cursor].label) + "\n\n" + preview,
	)
	hint := Styles.Hint.Render("↑/↓ navigate · r refresh · esc back")
	return lipgloss.JoinHorizontal(lipgloss.Top, listBox, "  ", previewBox) + "\n" + hint
}

// previewText formats the cached file contents to fit the preview pane. It does
// no IO — the content was loaded in loadPreview when the selection last changed.
func (b browserModel) previewText(width, height int) string {
	if b.previewErr {
		return Styles.Err.Render(b.previewRaw)
	}
	lines := strings.Split(b.previewRaw, "\n")
	if len(lines) > height {
		lines = lines[:height]
		lines = append(lines, Styles.Dim.Render("…"))
	}
	for i, l := range lines {
		// Measure and slice by runes so multi-byte UTF-8 lines are never cut
		// mid-codepoint (which would render as a mojibake glyph).
		if r := []rune(l); len(r) > width {
			lines[i] = string(r[:width-1]) + "…"
		}
	}
	return strings.Join(lines, "\n")
}
