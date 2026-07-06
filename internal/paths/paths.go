// Package paths centralizes the on-disk layout of a Claude Code workspace.
// Every directory and file name the CLI reads or writes is defined here so the
// layout lives in exactly one place. The workspace is Claude Code-native: the
// marker directory is .claude/, the entry point is CLAUDE.md, MCP servers live
// in .mcp.json at the repo root, and per-feature specs live under specs/.
package paths

import "path/filepath"

// ClaudeDir is the Claude Code configuration directory. Historically it also
// marked a csdd workspace root; the marker is moving to StateDir (.csdd/), which
// csdd owns exclusively (see StateDir).
const ClaudeDir = ".claude"

// StateDir is csdd's operational-state directory — the csdd analog of .git/. Its
// presence at the project root is the workspace marker (R15): hash manifests,
// incremental build state, and caches live here, never knowledge artifacts.
const StateDir = ".csdd"

// StateManifestSeg is the csdd-managed artifact manifest under .csdd/, migrated
// from the legacy .claude/.csdd-manifest.json (R14). It drives `csdd update`.
const StateManifestSeg = "manifest.json"

// Documentation knowledge-base segments under docs/ (R7). docs/graph/ is the only
// generated subtree; the rest is human/LLM-authored.
const (
	DocsSeg      = "docs"
	DocsPlansSeg = "plans"
	DocsRawSeg   = "raw"
	DocsWikiSeg  = "wiki"
	DocsGraphSeg = "graph"
	StackSeg     = "stack.md"
)

// Entry-point files at the workspace root.
const (
	EntryFile = "CLAUDE.md" // agent entry point, imports steering via @-references
	MCPFile   = ".mcp.json" // Model Context Protocol server config (Claude Code convention)
)

// Relative segment names. Most live under .claude/; specs/ lives at the root.
const (
	SpecsSeg     = "specs"               // repo-root: per-feature SDD contracts
	RulesSeg     = "rules"               // .claude/rules: generation rules + review gates
	SkillsSeg    = "skills"              // .claude/skills
	AgentsSeg    = "agents"              // .claude/agents
	CommandsSeg  = "commands"            // .claude/commands: slash commands (e.g. /csdd-commit)
	SteeringSeg  = "steering"            // .claude/steering: project memory
	TemplatesSeg = "templates"           // .claude/templates: versioned spec/steering templates
	HooksSeg     = "hooks"               // .claude/hooks: deterministic automation scripts
	SettingsSeg  = "settings.json"       // .claude/settings.json: hooks + permissions
	ManifestSeg  = ".csdd-manifest.json" // .claude/.csdd-manifest.json: csdd-managed artifact hashes (drives `csdd update`)
)

// Claude returns the .claude/ directory under root.
func Claude(root string) string { return filepath.Join(root, ClaudeDir) }

// Specs returns the repo-root specs/ directory.
func Specs(root string) string { return filepath.Join(root, SpecsSeg) }

// Rules returns .claude/rules/.
func Rules(root string) string { return filepath.Join(root, ClaudeDir, RulesSeg) }

// Skills returns .claude/skills/.
func Skills(root string) string { return filepath.Join(root, ClaudeDir, SkillsSeg) }

// Agents returns .claude/agents/.
func Agents(root string) string { return filepath.Join(root, ClaudeDir, AgentsSeg) }

// Commands returns .claude/commands/.
func Commands(root string) string { return filepath.Join(root, ClaudeDir, CommandsSeg) }

// Steering returns .claude/steering/.
func Steering(root string) string { return filepath.Join(root, ClaudeDir, SteeringSeg) }

// Templates returns .claude/templates/.
func Templates(root string) string { return filepath.Join(root, ClaudeDir, TemplatesSeg) }

// Hooks returns .claude/hooks/.
func Hooks(root string) string { return filepath.Join(root, ClaudeDir, HooksSeg) }

// Settings returns .claude/settings.json.
func Settings(root string) string { return filepath.Join(root, ClaudeDir, SettingsSeg) }

// Manifest returns .claude/.csdd-manifest.json, the record of csdd-managed
// artifacts and their content hashes that `csdd update` reconciles against.
func Manifest(root string) string { return filepath.Join(root, ClaudeDir, ManifestSeg) }

// State returns the .csdd/ operational-state directory (the workspace marker).
func State(root string) string { return filepath.Join(root, StateDir) }

// StateManifest returns .csdd/manifest.json, the current home of the csdd-managed
// artifact manifest.
func StateManifest(root string) string { return filepath.Join(root, StateDir, StateManifestSeg) }

// Docs returns the repo-root docs/ knowledge-base directory.
func Docs(root string) string { return filepath.Join(root, DocsSeg) }

// DocsPlans returns docs/plans/.
func DocsPlans(root string) string { return filepath.Join(root, DocsSeg, DocsPlansSeg) }

// DocsRaw returns docs/raw/, the immutable raw-source dropzone.
func DocsRaw(root string) string { return filepath.Join(root, DocsSeg, DocsRawSeg) }

// DocsWiki returns docs/wiki/, the LLM-authored structured knowledge base.
func DocsWiki(root string) string { return filepath.Join(root, DocsSeg, DocsWikiSeg) }

// DocsGraph returns docs/graph/, the only generated subtree in docs/.
func DocsGraph(root string) string { return filepath.Join(root, DocsSeg, DocsGraphSeg) }

// Stack returns docs/stack.md, the human-authored tech contract.
func Stack(root string) string { return filepath.Join(root, DocsSeg, StackSeg) }

// MCP returns the repo-root .mcp.json.
func MCP(root string) string { return filepath.Join(root, MCPFile) }

// Entry returns the repo-root CLAUDE.md.
func Entry(root string) string { return filepath.Join(root, EntryFile) }
