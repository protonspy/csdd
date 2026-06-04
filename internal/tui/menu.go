package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// menuItem couples a label with the action that fires when the user picks it.
type menuItem struct {
	label  string
	desc   string
	action tea.Cmd
}

type menuModel struct {
	items  []menuItem
	cursor int
}

func newMenu() menuModel {
	return menuModel{
		items: []menuItem{
			{"Create steering", "Add a project-memory file with chosen inclusion mode.", cmdOpen(wizSteering)},
			{"Create spec", "Bootstrap or generate a feature spec artifact.", cmdOpen(wizSpec)},
			{"Create skill", "Scaffold a SKILL.md bundle in .claude/skills.", cmdOpen(wizSkill)},
			{"Create agent", "Define a custom sub-agent with least-privilege tools.", cmdOpen(wizAgent)},
			{"Add MCP server", "Register a workspace MCP server in .mcp.json.", cmdOpen(wizMCP)},
			{"Export workspace", "Convert steering/specs/MCP to Kiro or Codex format.", cmdOpen(wizExport)},
			{"Browse artifacts", "Navigate steering/specs/skills/agents/mcp with preview.", cmdBrowser()},
			{"Quit", "Exit the TUI (CLI remains available for headless use).", tea.Quit},
		},
	}
}

func (m menuModel) Update(msg tea.Msg) (menuModel, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		return m, m.items[m.cursor].action
	}
	return m, nil
}

func (m menuModel) View() string {
	var b strings.Builder
	b.WriteString(Styles.Heading.Render("Main menu") + "\n\n")
	for i, it := range m.items {
		marker := "  "
		label := Styles.Item.Render(it.label)
		if i == m.cursor {
			marker = Styles.Selected.Render("▸ ")
			label = Styles.Selected.Render(it.label)
		}
		b.WriteString(marker + label + "  " + Styles.Dim.Render(it.desc) + "\n")
	}
	return b.String()
}
