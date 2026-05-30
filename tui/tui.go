// Package tui hosts the interactive Bubble Tea front end.
// Every action the TUI surfaces calls into the cmd package so the wire format
// matches the CLI exactly — there is one source of truth for behavior.
package tui

import (
	"embed"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/protonspy/kspec/internal/workspace"
)

// Run is the entry point invoked by main when no CLI subcommand is provided.
func Run(templates embed.FS) error {
	root, err := workspace.Resolve("")
	if err != nil {
		// If --root resolution fails (no workspace yet), default to cwd.
		root = "."
	}
	app := newApp(templates, root)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// screenID enumerates the top-level TUI views.
type screenID int

const (
	screenMenu screenID = iota
	screenSteering
	screenSpec
	screenSkill
	screenAgent
	screenMCP
	screenBrowser
	screenResult
)

// App is the root Bubble Tea model. It owns shared state (workspace root,
// terminal size, embedded templates) and delegates Update/View to whichever
// sub-model corresponds to the active screen.
type App struct {
	templates embed.FS
	root      string
	width     int
	height    int

	screen screenID
	menu   menuModel
	wiz    wizardModel
	browse browserModel

	// Last result line displayed at the top of every screen after an action.
	lastResult    string
	lastResultErr bool
}

func newApp(templates embed.FS, root string) *App {
	a := &App{
		templates: templates,
		root:      root,
		screen:    screenMenu,
	}
	a.menu = newMenu()
	return a
}

func (a *App) Init() tea.Cmd { return nil }

// switchToWizard wires the active wizard for a given resource kind.
func (a *App) switchToWizard(kind wizardKind) {
	a.wiz = newWizard(kind, a.templates, a.root)
	switch kind {
	case wizSteering:
		a.screen = screenSteering
	case wizSpec:
		a.screen = screenSpec
	case wizSkill:
		a.screen = screenSkill
	case wizAgent:
		a.screen = screenAgent
	case wizMCP:
		a.screen = screenMCP
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
	case tea.KeyMsg:
		// Global keys: q quits from any screen, esc returns to menu (except inside text inputs).
		if a.screen == screenMenu {
			if m.String() == "q" || m.String() == "ctrl+c" {
				return a, tea.Quit
			}
		} else {
			if m.String() == "ctrl+c" {
				return a, tea.Quit
			}
		}
	case resultMsg:
		a.lastResult = m.text
		a.lastResultErr = m.isErr
		a.screen = screenResult
		return a, nil
	case backToMenuMsg:
		a.screen = screenMenu
		a.lastResult = ""
		return a, nil
	case openWizardMsg:
		a.switchToWizard(m.kind)
		return a, a.wiz.Init()
	case openBrowserMsg:
		a.browse = newBrowser(a.root)
		a.screen = screenBrowser
		return a, a.browse.Init()
	}

	var cmd tea.Cmd
	switch a.screen {
	case screenMenu:
		a.menu, cmd = a.menu.Update(msg)
	case screenSteering, screenSpec, screenSkill, screenAgent, screenMCP:
		a.wiz, cmd = a.wiz.Update(msg)
	case screenBrowser:
		a.browse, cmd = a.browse.Update(msg)
	case screenResult:
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.String() == "enter" || k.String() == "esc" {
				a.screen = screenMenu
				a.lastResult = ""
			}
		}
	}
	return a, cmd
}

func (a *App) View() string {
	header := Styles.Title.Render(" kspec — Kiro Workflow ") +
		"  " + Styles.Dim.Render("root: "+a.root)
	footer := Styles.Hint.Render("q quit · esc back · enter confirm")

	var body string
	switch a.screen {
	case screenMenu:
		body = a.menu.View()
	case screenSteering, screenSpec, screenSkill, screenAgent, screenMCP:
		body = a.wiz.View()
	case screenBrowser:
		body = a.browse.View(a.width, a.height)
	case screenResult:
		style := Styles.OK
		prefix := "✓"
		if a.lastResultErr {
			style = Styles.Err
			prefix = "✗"
		}
		body = style.Render(prefix+" "+a.lastResult) + "\n\n" +
			Styles.Hint.Render("press enter to return to the menu")
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s\n", header, body, footer)
}

// ---- shared messages -----------------------------------------------------

type backToMenuMsg struct{}
type openWizardMsg struct{ kind wizardKind }
type openBrowserMsg struct{}
type resultMsg struct {
	text  string
	isErr bool
}

func cmdBack() tea.Cmd                { return func() tea.Msg { return backToMenuMsg{} } }
func cmdOpen(kind wizardKind) tea.Cmd { return func() tea.Msg { return openWizardMsg{kind} } }
func cmdBrowser() tea.Cmd             { return func() tea.Msg { return openBrowserMsg{} } }
func cmdResult(text string, isErr bool) tea.Cmd {
	return func() tea.Msg { return resultMsg{text: text, isErr: isErr} }
}
