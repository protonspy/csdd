package tui

import (
	"embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/protonspy/csdd/internal/cli"
	"github.com/protonspy/csdd/internal/workspace"
)

// wizardKind picks which set of steps the wizard runs.
type wizardKind int

const (
	wizSteering wizardKind = iota
	wizSpec
	wizSkill
	wizAgent
	wizMCP
	wizExport
)

// fieldKind branches the rendering and key handling per step.
type fieldKind int

const (
	fText fieldKind = iota
	fSelect
	fMultiSelect
)

// field is one step in a wizard. The renderer reads `kind` to decide whether
// to draw a text input, a single-select list, or a multi-select list.
type field struct {
	name     string // dict key used to assemble the final options struct
	label    string // header shown above the input
	hint     string // grey helper line below the input
	kind     fieldKind
	required bool
	input    textinput.Model
	choices  []string
	selIdx   int
	multi    map[int]bool // for multi-select
	skipIf   func(state map[string]any) bool
	validate func(value string) error
}

var kebabRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func validateKebab(v string) error {
	if !kebabRe.MatchString(v) {
		return fmt.Errorf("must be kebab-case (lowercase, hyphen-separated)")
	}
	return nil
}

func validateNonEmpty(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("required")
	}
	return nil
}

// wizardModel walks a slice of fields, collecting answers into state.
// When the last field passes validation, it calls the backend handler.
type wizardModel struct {
	kind      wizardKind
	templates embed.FS
	root      string
	title     string
	fields    []*field
	state     map[string]any
	cursor    int
	err       string
}

func newWizard(kind wizardKind, templates embed.FS, root string) wizardModel {
	w := wizardModel{
		kind:      kind,
		templates: templates,
		root:      root,
		state:     map[string]any{},
	}
	switch kind {
	case wizSteering:
		w.title = "Create steering"
		w.fields = steeringFields()
	case wizSpec:
		w.title = "Spec wizard"
		w.fields = specFields()
	case wizSkill:
		w.title = "Create skill"
		w.fields = skillFields()
	case wizAgent:
		w.title = "Create agent"
		w.fields = agentFields()
	case wizMCP:
		w.title = "Add MCP server"
		w.fields = mcpFields()
	case wizExport:
		w.title = "Export workspace"
		w.fields = exportFields()
	}
	return w
}

func (w wizardModel) Init() tea.Cmd {
	if len(w.fields) == 0 {
		return nil
	}
	// Only text inputs need focus; select/multi-select fields keep their
	// textinput.Model zero-valued, which means the internal cursor is nil
	// and calling Focus() on it panics.
	if w.fields[0].kind == fText {
		return w.fields[0].input.Focus()
	}
	return nil
}

// advance moves the cursor forward, skipping conditional fields.
// Returns done=true when there are no more fields, plus an optional
// focus command for the next text field so the cursor blink starts.
func (w *wizardModel) advance() (bool, tea.Cmd) {
	for {
		w.cursor++
		if w.cursor >= len(w.fields) {
			return true, nil
		}
		f := w.fields[w.cursor]
		if f.skipIf != nil && f.skipIf(w.state) {
			continue
		}
		if f.kind == fText {
			return false, f.input.Focus()
		}
		return false, nil
	}
}

func (w *wizardModel) retreat() tea.Cmd {
	if w.cursor == 0 {
		return nil
	}
	for {
		w.cursor--
		if w.cursor < 0 {
			w.cursor = 0
			return nil
		}
		f := w.fields[w.cursor]
		if f.skipIf != nil && f.skipIf(w.state) {
			continue
		}
		if f.kind == fText {
			return f.input.Focus()
		}
		return nil
	}
}

// commit captures the current field's value into state and validates.
func (w *wizardModel) commit() error {
	f := w.fields[w.cursor]
	switch f.kind {
	case fText:
		val := strings.TrimSpace(f.input.Value())
		if !f.required && val == "" {
			w.state[f.name] = ""
			return nil
		}
		if f.validate != nil {
			if err := f.validate(val); err != nil {
				return err
			}
		}
		w.state[f.name] = val
	case fSelect:
		if len(f.choices) == 0 {
			return fmt.Errorf("no choices configured")
		}
		w.state[f.name] = f.choices[f.selIdx]
	case fMultiSelect:
		var picked []string
		for i, c := range f.choices {
			if f.multi[i] {
				picked = append(picked, c)
			}
		}
		w.state[f.name] = picked
	}
	return nil
}

func (w wizardModel) Update(msg tea.Msg) (wizardModel, tea.Cmd) {
	if w.cursor >= len(w.fields) {
		return w, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		// Let textinput components handle non-key messages (blink, etc.).
		if w.fields[w.cursor].kind == fText {
			var cmd tea.Cmd
			w.fields[w.cursor].input, cmd = w.fields[w.cursor].input.Update(msg)
			return w, cmd
		}
		return w, nil
	}
	switch k.String() {
	case "esc":
		return w, cmdBack()
	case "enter":
		if err := w.commit(); err != nil {
			w.err = err.Error()
			return w, nil
		}
		w.err = ""
		done, focusCmd := w.advance()
		if done {
			return w, w.submit()
		}
		return w, focusCmd
	case "shift+tab":
		focusCmd := w.retreat()
		return w, focusCmd
	}

	f := w.fields[w.cursor]
	switch f.kind {
	case fText:
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return w, cmd
	case fSelect:
		switch k.String() {
		case "up", "k":
			if f.selIdx > 0 {
				f.selIdx--
			}
		case "down", "j":
			if f.selIdx < len(f.choices)-1 {
				f.selIdx++
			}
		}
	case fMultiSelect:
		switch k.String() {
		case "up", "k":
			if f.selIdx > 0 {
				f.selIdx--
			}
		case "down", "j":
			if f.selIdx < len(f.choices)-1 {
				f.selIdx++
			}
		case " ":
			if f.multi == nil {
				f.multi = map[int]bool{}
			}
			f.multi[f.selIdx] = !f.multi[f.selIdx]
		}
	}
	return w, nil
}

func (w wizardModel) View() string {
	if w.cursor >= len(w.fields) {
		return Styles.Hint.Render("submitting…")
	}
	f := w.fields[w.cursor]
	var b strings.Builder
	b.WriteString(Styles.Heading.Render(w.title) + "  " +
		Styles.Dim.Render(fmt.Sprintf("step %d of %d", w.cursor+1, len(w.fields))) + "\n\n")
	b.WriteString(Styles.Item.Render(f.label) + "\n")
	if f.hint != "" {
		b.WriteString(Styles.Hint.Render(f.hint) + "\n")
	}
	b.WriteString("\n")
	switch f.kind {
	case fText:
		b.WriteString(f.input.View() + "\n")
	case fSelect:
		for i, c := range f.choices {
			prefix := "  "
			line := Styles.Item.Render(c)
			if i == f.selIdx {
				prefix = Styles.Selected.Render("▸ ")
				line = Styles.Selected.Render(c)
			}
			b.WriteString(prefix + line + "\n")
		}
	case fMultiSelect:
		for i, c := range f.choices {
			marker := "[ ]"
			if f.multi[i] {
				marker = Styles.OK.Render("[x]")
			}
			prefix := "  "
			line := Styles.Item.Render(c)
			if i == f.selIdx {
				prefix = Styles.Selected.Render("▸ ")
				line = Styles.Selected.Render(c)
			}
			b.WriteString(prefix + marker + " " + line + "\n")
		}
		b.WriteString("\n" + Styles.Hint.Render("space toggles · enter confirms"))
	}
	if w.err != "" {
		b.WriteString("\n" + Styles.Err.Render("✗ "+w.err))
	}
	b.WriteString("\n\n" + Styles.Hint.Render("enter next · shift+tab back · esc cancel"))
	return b.String()
}

// submit hands the collected state to the matching cli backend so the result
// matches a direct CLI invocation exactly.
func (w wizardModel) submit() tea.Cmd {
	return func() tea.Msg {
		switch w.kind {
		case wizSteering:
			opts := cli.SteeringCreateOptions{
				Root:        w.root,
				Name:        getStr(w.state, "name"),
				Inclusion:   getStr(w.state, "inclusion"),
				Patterns:    splitCSV(getStr(w.state, "patterns")),
				Description: getStr(w.state, "description"),
				Title:       getStr(w.state, "title"),
			}
			if err := cli.SteeringCreate(w.templates, opts); err != nil {
				return resultMsg{text: err.Error(), isErr: true}
			}
			return resultMsg{text: "steering '" + opts.Name + "' created (inclusion=" + opts.Inclusion + ")"}
		case wizSpec:
			op := getStr(w.state, "operation")
			feature := getStr(w.state, "feature")
			switch op {
			case "init":
				if err := workspace.KebabCheck(feature, "feature"); err != nil {
					return resultMsg{text: err.Error(), isErr: true}
				}
				// Use the CLI Run path to avoid duplicating logic for spec init.
				code := cli.Run([]string{"spec", "init", feature}, w.templates)
				if code != 0 {
					return resultMsg{text: "spec init failed (exit " + fmt.Sprint(code) + ")", isErr: true}
				}
				return resultMsg{text: "spec '" + feature + "' initialized"}
			case "generate":
				artifact := getStr(w.state, "artifact")
				if err := cli.SpecGenerate(w.templates, cli.SpecGenerateOptions{
					Root: w.root, Feature: feature, Artifact: artifact,
				}); err != nil {
					return resultMsg{text: err.Error(), isErr: true}
				}
				return resultMsg{text: "generated " + artifact + " for " + feature}
			case "approve":
				phase := getStr(w.state, "phase")
				if err := cli.SpecApprove(cli.SpecApproveOptions{
					Root: w.root, Feature: feature, Phase: phase,
				}); err != nil {
					return resultMsg{text: err.Error(), isErr: true}
				}
				return resultMsg{text: feature + ": " + phase + " approved"}
			}
		case wizSkill:
			opts := cli.SkillCreateOptions{
				Root:        w.root,
				Name:        getStr(w.state, "name"),
				Description: getStr(w.state, "description"),
				Title:       getStr(w.state, "title"),
			}
			if err := cli.SkillCreate(w.templates, opts); err != nil {
				return resultMsg{text: err.Error(), isErr: true}
			}
			return resultMsg{text: "skill '" + opts.Name + "' created"}
		case wizAgent:
			model := getStr(w.state, "model")
			if model == "none" {
				model = ""
			}
			tools, _ := w.state["tools"].([]string)
			if len(tools) == 0 {
				tools = []string{"Read", "Grep"}
			}
			opts := cli.AgentCreateOptions{
				Root:        w.root,
				Name:        getStr(w.state, "name"),
				Description: getStr(w.state, "description"),
				Tools:       tools,
				Model:       model,
				Title:       getStr(w.state, "title"),
			}
			if err := cli.AgentCreate(w.templates, opts); err != nil {
				return resultMsg{text: err.Error(), isErr: true}
			}
			return resultMsg{text: "agent '" + opts.Name + "' created"}
		case wizMCP:
			transport := getStr(w.state, "transport")
			opts := cli.MCPAddOptions{
				Root:     w.root,
				Name:     getStr(w.state, "name"),
				Env:      splitCSV(getStr(w.state, "env")),
				Disabled: getStr(w.state, "disabled") == "yes",
			}
			if transport == "stdio" {
				opts.Command = getStr(w.state, "command")
				opts.Args = splitCSV(getStr(w.state, "args"))
			} else {
				opts.URL = getStr(w.state, "url")
				opts.Type = transport
			}
			if err := cli.MCPAdd(opts); err != nil {
				return resultMsg{text: err.Error(), isErr: true}
			}
			return resultMsg{text: "mcp server '" + opts.Name + "' added (" + transport + ")"}
		case wizExport:
			target := getStr(w.state, "target")
			opts := cli.ExportOptions{
				Root:  w.root,
				Out:   getStr(w.state, "out"),
				Force: getStr(w.state, "force") == "yes",
			}
			var err error
			switch target {
			case "kiro":
				err = cli.ExportKiro(opts)
			case "codex":
				err = cli.ExportCodex(opts)
			default:
				return resultMsg{text: "unknown export target: " + target, isErr: true}
			}
			if err != nil {
				return resultMsg{text: err.Error(), isErr: true}
			}
			return resultMsg{text: "exported workspace to " + target + " format"}
		}
		return resultMsg{text: "nothing to do"}
	}
}

func getStr(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- field definitions per wizard ----------------------------------------

func newTextInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 240
	ti.Width = 50
	return ti
}

func steeringFields() []*field {
	return []*field{
		{
			name: "name", label: "Name", hint: "kebab-case (e.g. api-conventions)",
			kind: fText, required: true, input: newTextInput("api-conventions"),
			validate: validateKebab,
		},
		{
			name: "inclusion", label: "Inclusion mode", kind: fSelect,
			required: true, choices: workspace.InclusionModes,
		},
		{
			name: "patterns", label: "fileMatch patterns",
			hint: "comma-separated globs (e.g. src/api/**/*, **/*Controller.*)",
			kind: fText, required: true, input: newTextInput("src/api/**/*, **/*Controller.*"),
			skipIf: func(s map[string]any) bool { return getStr(s, "inclusion") != "fileMatch" },
			validate: func(v string) error {
				if len(splitCSV(v)) == 0 {
					return fmt.Errorf("at least one pattern required")
				}
				return nil
			},
		},
		{
			name: "description", label: "Description",
			hint: "the trigger sentence the agent reads",
			kind: fText, required: true, input: newTextInput("Logging/metrics. Use when adding instrumentation."),
			skipIf:   func(s map[string]any) bool { return getStr(s, "inclusion") != "auto" },
			validate: validateNonEmpty,
		},
		{
			name: "title", label: "Title (optional)", hint: "default: derived from name",
			kind: fText, input: newTextInput(""),
		},
	}
}

func specFields() []*field {
	return []*field{
		{
			name: "operation", label: "Operation", kind: fSelect, required: true,
			choices: []string{"init", "generate", "approve"},
		},
		{
			name: "feature", label: "Feature name", hint: "kebab-case",
			kind: fText, required: true, input: newTextInput("photo-albums"),
			validate: validateKebab,
		},
		{
			name: "artifact", label: "Artifact", kind: fSelect, required: true,
			choices: workspace.SpecArtifacts,
			skipIf:  func(s map[string]any) bool { return getStr(s, "operation") != "generate" },
		},
		{
			name: "phase", label: "Phase to approve", kind: fSelect, required: true,
			choices: workspace.SpecPhases,
			skipIf:  func(s map[string]any) bool { return getStr(s, "operation") != "approve" },
		},
	}
}

func skillFields() []*field {
	return []*field{
		{
			name: "name", label: "Name", hint: "kebab-case (e.g. spec-tasks)",
			kind: fText, required: true, input: newTextInput("spec-tasks"),
			validate: validateKebab,
		},
		{
			name: "description", label: "Description",
			hint: "one-sentence activation trigger — what task and when to use",
			kind: fText, required: true,
			input:    newTextInput("Generate tasks.md with boundary/depends annotations."),
			validate: validateNonEmpty,
		},
		{
			name: "title", label: "Title (optional)", hint: "default: derived from name",
			kind: fText, input: newTextInput(""),
		},
	}
}

func agentFields() []*field {
	return []*field{
		{
			name: "name", label: "Name", hint: "kebab-case (e.g. code-reviewer)",
			kind: fText, required: true, input: newTextInput("code-reviewer"),
			validate: validateKebab,
		},
		{
			name: "description", label: "Description",
			hint: "when the orchestrator should pick this agent",
			kind: fText, required: true,
			input:    newTextInput("Read-only adversarial reviewer"),
			validate: validateNonEmpty,
		},
		{
			name: "tools", label: "Allowed tools (least privilege)",
			hint: "space toggles · default: Read + Grep",
			kind: fMultiSelect, choices: []string{"Read", "Grep", "Bash", "Edit", "Write", "WebFetch"},
		},
		{
			name: "model", label: "Model override (optional)", kind: fSelect,
			choices: []string{"none", "sonnet", "opus", "haiku"},
		},
		{
			name: "title", label: "Title (optional)", hint: "default: derived from name",
			kind: fText, input: newTextInput(""),
		},
	}
}

func mcpFields() []*field {
	return []*field{
		{
			name: "name", label: "Name", hint: "kebab-case (e.g. filesystem, linear)",
			kind: fText, required: true, input: newTextInput("filesystem"),
			validate: validateKebab,
		},
		{
			name: "transport", label: "Transport", kind: fSelect, required: true,
			choices: []string{"stdio", "sse", "http"},
		},
		{
			name: "command", label: "Command", hint: "executable for a stdio server",
			kind: fText, required: true, input: newTextInput("npx"),
			skipIf:   func(s map[string]any) bool { return getStr(s, "transport") != "stdio" },
			validate: validateNonEmpty,
		},
		{
			name: "args", label: "Arguments", hint: "comma-separated (e.g. -y, @scope/server, .)",
			kind: fText, input: newTextInput("-y, @modelcontextprotocol/server-filesystem, ."),
			skipIf: func(s map[string]any) bool { return getStr(s, "transport") != "stdio" },
		},
		{
			name: "url", label: "URL", hint: "endpoint for a remote server",
			kind: fText, required: true, input: newTextInput("https://mcp.example.com/sse"),
			skipIf:   func(s map[string]any) bool { return getStr(s, "transport") == "stdio" },
			validate: validateNonEmpty,
		},
		{
			name: "env", label: "Environment (optional)", hint: "comma-separated K=V (e.g. TOKEN=xxx)",
			kind: fText, input: newTextInput(""),
		},
		{
			name: "disabled", label: "Start disabled?", kind: fSelect,
			choices: []string{"no", "yes"},
		},
	}
}

func exportFields() []*field {
	return []*field{
		{
			name: "target", label: "Target format", kind: fSelect, required: true,
			choices: []string{"kiro", "codex"},
		},
		{
			name: "out", label: "Output directory (optional)", hint: "default: project root",
			kind: fText, input: newTextInput(""),
		},
		{
			name: "force", label: "Overwrite existing files?", kind: fSelect,
			choices: []string{"no", "yes"},
		},
	}
}
