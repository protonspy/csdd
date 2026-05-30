package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/livelo/kspec/cmd"
	"github.com/livelo/kspec/internal/templater"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestMenuNavigationAndSelection(t *testing.T) {
	m := newMenu()
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m, _ = m.Update(key(tea.KeyDown))
	if m.cursor != 1 {
		t.Errorf("after down cursor = %d, want 1", m.cursor)
	}
	m, _ = m.Update(key(tea.KeyUp))
	if m.cursor != 0 {
		t.Errorf("after up cursor = %d, want 0", m.cursor)
	}
	// Up at the top is clamped.
	m, _ = m.Update(key(tea.KeyUp))
	if m.cursor != 0 {
		t.Errorf("up at top should clamp to 0, got %d", m.cursor)
	}
	// Enter fires the selected item's action command.
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd == nil {
		t.Error("enter should return a non-nil action command")
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Error("menu View should render content")
	}
}

func TestBrowserListsAndPreviews(t *testing.T) {
	dir := freshTUIWorkspace(t)
	// init --with-baseline already wrote steering files; add one explicitly too.
	if code := cmd.Run([]string{"steering", "create", "sec", "--inclusion", "always", "--root", dir}, templater.FS); code != 0 {
		t.Fatalf("steering create: exit %d", code)
	}
	b := newBrowser(dir)
	if len(b.items) == 0 {
		t.Fatal("expected at least one artifact")
	}
	// Register an MCP server so the browser surfaces an "mcp" item too.
	if code := cmd.Run([]string{"mcp", "add", "filesystem", "--command", "npx", "--root", dir}, templater.FS); code != 0 {
		t.Fatalf("mcp add: exit %d", code)
	}
	b = newBrowser(dir)
	var sawSteering, sawMCP bool
	for _, it := range b.items {
		switch it.kind {
		case "steering":
			sawSteering = true
		case "mcp":
			sawMCP = true
		}
	}
	if !sawSteering {
		t.Error("expected a steering item in the browser")
	}
	if !sawMCP {
		t.Error("expected an mcp item in the browser")
	}
	// Navigation + refresh must not panic and must keep the cursor in range.
	b, _ = b.Update(key(tea.KeyDown))
	b, _ = b.Update(key(tea.KeyUp))
	b, _ = b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if b.cursor < 0 || b.cursor >= len(b.items) {
		t.Errorf("cursor out of range after navigation: %d", b.cursor)
	}
	if out := b.View(120, 40); strings.TrimSpace(out) == "" {
		t.Error("browser View should render content")
	}
}

func TestBrowserEmptyWorkspace(t *testing.T) {
	dir := t.TempDir() // no .kiro / .agents at all
	b := newBrowser(dir)
	if len(b.items) != 0 {
		t.Errorf("expected no items in a bare directory, got %d", len(b.items))
	}
	if out := b.View(120, 40); !strings.Contains(out, "no artifacts") {
		t.Errorf("empty browser should explain there is nothing yet:\n%s", out)
	}
}

// TestPreviewTextRuneSafe verifies the rune-based line truncation: a line of
// multi-byte runes wider than the pane is cut without splitting a codepoint.
func TestPreviewTextRuneSafe(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.md")
	// 8 alpha runes (2 bytes each in UTF-8).
	if err := os.WriteFile(p, []byte("αααααααα"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := previewText(p, 5, 10)
	if !strings.Contains(out, "…") {
		t.Errorf("wide line should be truncated with an ellipsis: %q", out)
	}
	// A byte-based cut would leave a dangling half-codepoint that decodes to the
	// Unicode replacement char; rune-based truncation must not.
	if strings.ContainsRune(out, '�') {
		t.Errorf("truncation split a UTF-8 codepoint: %q", out)
	}
}

func TestPreviewTextMissingFile(t *testing.T) {
	out := previewText(filepath.Join(t.TempDir(), "nope.md"), 40, 10)
	if !strings.Contains(out, "could not read") {
		t.Errorf("missing file preview should report the error: %q", out)
	}
}

func TestAppSwitchToWizard(t *testing.T) {
	a := newApp(templater.FS, t.TempDir())
	if a.screen != screenMenu {
		t.Fatalf("new app should start on the menu, got %d", a.screen)
	}
	if a.Init() != nil {
		t.Error("App.Init should be a no-op (nil)")
	}
	a.switchToWizard(wizSpec)
	if a.screen != screenSpec {
		t.Errorf("switchToWizard(wizSpec) screen = %d, want %d", a.screen, screenSpec)
	}
	if strings.TrimSpace(a.View()) == "" {
		t.Error("App.View should render content")
	}
}
