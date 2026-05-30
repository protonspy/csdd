package tui

import "github.com/charmbracelet/lipgloss"

// Palette is intentionally narrow: four colors do every job, which keeps the
// TUI legible on dim and dark terminals alike.
var (
	colorPrimary = lipgloss.Color("#7DD3FC") // cyan
	colorAccent  = lipgloss.Color("#86EFAC") // green
	colorWarn    = lipgloss.Color("#FCD34D") // yellow
	colorError   = lipgloss.Color("#F87171") // red
	colorDim     = lipgloss.Color("#9CA3AF") // gray
	colorMuted   = lipgloss.Color("#6B7280")
)

// Styles bundles every reusable lipgloss style so screens don't reinvent them.
var Styles = struct {
	Title    lipgloss.Style
	Heading  lipgloss.Style
	Hint     lipgloss.Style
	Selected lipgloss.Style
	Item     lipgloss.Style
	Field    lipgloss.Style
	FieldOK  lipgloss.Style
	FieldErr lipgloss.Style
	OK       lipgloss.Style
	Err      lipgloss.Style
	Warn     lipgloss.Style
	Dim      lipgloss.Style
	Box      lipgloss.Style
}{
	Title:    lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Padding(0, 1),
	Heading:  lipgloss.NewStyle().Foreground(colorAccent).Bold(true),
	Hint:     lipgloss.NewStyle().Foreground(colorDim).Italic(true),
	Selected: lipgloss.NewStyle().Foreground(colorPrimary).Bold(true),
	Item:     lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")),
	Field:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")),
	FieldOK:  lipgloss.NewStyle().Foreground(colorAccent),
	FieldErr: lipgloss.NewStyle().Foreground(colorError),
	OK:       lipgloss.NewStyle().Foreground(colorAccent),
	Err:      lipgloss.NewStyle().Foreground(colorError),
	Warn:     lipgloss.NewStyle().Foreground(colorWarn),
	Dim:      lipgloss.NewStyle().Foreground(colorMuted),
	Box:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorDim).Padding(0, 1),
}
