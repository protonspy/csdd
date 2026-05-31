# @protonspy/csdd-mcp

**An [MCP](https://modelcontextprotocol.io) server that exposes the [`csdd`](https://github.com/protonspy/csdd) CLI as tools — one tool per subcommand.**

`csdd` governs the Claude Code Spec-Driven Development workflow (steering, specs,
skills, sub-agents, MCP servers) and validates the contract mechanically. This
server lets an MCP-capable agent drive `csdd` **directly as tools**, instead of
shelling out to a terminal — same operations, same phase gates, same exit codes.

```
agent ──(MCP/stdio)──▶ csdd-mcp ──(execFile)──▶ csdd binary ──▶ .claude/ · specs/
```

Each tool builds a `csdd` argv, runs the binary headlessly (`NO_COLOR=1`, no TTY
so confirmations auto-decline), and returns its `stdout`/`stderr`. A non-zero
exit becomes an MCP error result; **exit `2` (validation failure) is surfaced
distinctly** so the agent can tell "your spec is invalid" from "the command
broke".

---

## Requirements

- **Node.js ≥ 18** (the published package ships compiled JS).
- **The `csdd` binary**, reachable by the server — see [Locating the csdd binary](#locating-the-csdd-binary).

---

## Install & configure

The server runs over **stdio**; point your MCP client at it.

### Claude Code

```bash
# project scope (writes .mcp.json) — or use --scope user for all projects
claude mcp add csdd -- npx -y @protonspy/csdd-mcp
```

Or add it to `.mcp.json` by hand:

```json
{
  "mcpServers": {
    "csdd": {
      "command": "npx",
      "args": ["-y", "@protonspy/csdd-mcp"],
      "env": { "CSDD_BIN": "/usr/local/bin/csdd" }
    }
  }
}
```

> `env.CSDD_BIN` is optional — drop it if `csdd` is on your `PATH`. See below.

### Any MCP client

Launch `npx -y @protonspy/csdd-mcp` (or `csdd-mcp` if installed globally) as a
stdio server. The process stays alive serving stdio until the client closes the
pipe.

---

## Locating the csdd binary

The server resolves `csdd` once, on first use, in this order (first hit wins):

| # | Source | When it applies |
|---|--------|-----------------|
| 1 | **`$CSDD_BIN`** | Explicit absolute path. Always wins — use this if in doubt. |
| 2 | **Platform package** `@protonspy/csdd-<os>-<arch>` | When the matching prebuilt-binary package is installed alongside (e.g. co-installed with `@protonspy/csdd`). |
| 3 | **Sibling repo binary** (`../csdd`, `../../csdd`) | When running from a checkout of the csdd repo. |
| 4 | **`csdd` on `$PATH`** | Last resort, resolved by the OS at spawn time. |

If none resolve, calls fail with **exit `127`** and a message telling you to set
`CSDD_BIN`, install `@protonspy/csdd`, or put `csdd` on your `PATH`.

> **Recommended setup for end users:** `npm install -g @protonspy/csdd` (puts
> `csdd` on `PATH`, satisfying #4), or set `CSDD_BIN` to an absolute path.

### Environment

| Variable | Effect |
|----------|--------|
| `CSDD_BIN` | Absolute path to the `csdd` binary. Highest-priority resolution. |
| `NO_COLOR` | Forced to `1` for every call so output is ANSI-free (you don't set this). |

---

## Result & error semantics

Every tool returns a text result. The mapping from the `csdd` exit code is:

| Exit | `isError` | Result text |
|------|-----------|-------------|
| `0` | `false` | `stdout` (and any `stderr` as an unlabelled warning); `(ok, no output)` if silent. |
| `2` | `true` | Prefixed `csdd validation failed (exit 2):` — a contract/validation problem. |
| other | `true` | Prefixed `csdd failed (exit <n>):`; `stderr` is labelled `[stderr]`. |
| `127` | `true` | Binary not found — includes guidance to set `CSDD_BIN` / install / fix `PATH`. |

---

## Tool reference

**35 tools**, grouped by the resource they manage. Conventions:

- Every tool accepts an optional **`root`** — the workspace root (the directory
  containing `.claude/`). Omit it to walk up from the server's working directory.
- Destructive / gate-breaking tools take **`force`** (boolean). Without it,
  deletes are refused and phase gates hold.
- `?` marks an optional parameter; everything else is required.

### Workspace

| Tool | Parameters | What it does |
|------|------------|--------------|
| `csdd_version` | — | Print the underlying `csdd` binary version (diagnostic / connectivity check). |
| `csdd_init` | `withBaseline?`, `root?` | Bootstrap a Claude Code workspace: `.claude/` layout, `CLAUDE.md`, `csdd.md`, `.mcp.json`, rules, shipped agents/skills/commands/hooks, guides. Idempotent. `withBaseline` also scaffolds the standard steering files and imports them into `CLAUDE.md`. |

### 🧭 steering — project memory (`.claude/steering/*.md`)

| Tool | Parameters | What it does |
|------|------------|--------------|
| `csdd_steering_init` | `root?` | Create `.claude/steering/` and the 6 standard files (product, tech, structure, security, testing, api-conventions). |
| `csdd_steering_create` | `name`, `inclusion`, `pattern?[]`, `description?`, `title?`, `force?`, `root?` | Create a custom steering file. `inclusion` ∈ `always · fileMatch · manual · auto`. `fileMatch` requires ≥1 `pattern`; `auto` requires a `description`. |
| `csdd_steering_list` | `root?`, `inclusion?` | List steering files with inclusion mode; optionally filter by `inclusion`. |
| `csdd_steering_show` | `name`, `root?` | Print a steering file (frontmatter + body). |
| `csdd_steering_delete` | `name`, `force?`, `root?` | Delete a steering file (`force` required). Foundational files (product, tech, structure) are protected. |
| `csdd_steering_validate` | `name?`, `root?` | Validate frontmatter/structure. Omit `name` to validate all. Exit 2 on issues. |

`inclusion` controls *when* the steering loads: `always` (always-on), `fileMatch`
(when files match a `pattern`), `manual` (`#name`), `auto` (when its `description`
matches the context).

### 📐 spec — per-feature contract (`specs/<feature>/`)

| Tool | Parameters | What it does |
|------|------------|--------------|
| `csdd_spec_init` | `feature`, `language?`, `root?` | Create `specs/<feature>/spec.json` (phase = initial, no approvals). `language` defaults to `en`. |
| `csdd_spec_list` | `root?` | List specs with current phase, approved phases, and readiness. |
| `csdd_spec_show` | `feature`, `root?` | Show a spec's `spec.json` metadata and its artifacts. |
| `csdd_spec_status` | `feature`, `root?` | Combined `show` + `validate` for a spec. |
| `csdd_spec_generate` | `feature`, `artifact`, `force?`, `root?` | Generate an artifact. `artifact` ∈ `requirements · design · tasks · research · bugfix`. **Phase gates apply** (see below); `force` bypasses them. |
| `csdd_spec_approve` | `feature`, `phase`, `force?`, `root?` | Approve a phase. `phase` ∈ `requirements · design · tasks`. Validates first; `force` approves despite issues/missing prior approvals. |
| `csdd_spec_validate` | `feature`, `root?` | Validate EARS phrasing, traceability, task annotations, parallel safety. Exit 2 on issues. |
| `csdd_spec_delete` | `feature`, `force?`, `root?` | Delete `specs/<feature>/` recursively (`force` required). |

> **Phase gates (enforced, not advisory):** `design` needs `requirements`
> approved; `tasks` needs `design` approved. Generating out of order fails with
> **exit 2** unless `force` is passed. `ready_for_implementation` flips to `true`
> only when all three phases are approved. `research` and `bugfix` are ungated.

### 🛠️ skill — workflow bundles (`.claude/skills/<name>/`)

| Tool | Parameters | What it does |
|------|------------|--------------|
| `csdd_skill_create` | `name`, `description`, `title?`, `root?` | Create `.claude/skills/<name>/` with `SKILL.md` (+ `references/`, `assets/`, `scripts/`). `description` is the one-line activation trigger. |
| `csdd_skill_list` | `root?` | List skills with their descriptions. |
| `csdd_skill_show` | `name`, `root?` | List a skill's files and print `SKILL.md`. |
| `csdd_skill_add_reference` | `skill`, `file`, `root?` | Add a reference file under `references/`. Path traversal is rejected. |
| `csdd_skill_add_script` | `skill`, `file`, `root?` | Add a script file under `scripts/`. Path traversal is rejected. |
| `csdd_skill_add_asset` | `skill`, `file`, `root?` | Add an asset file under `assets/`. Path traversal is rejected. |
| `csdd_skill_validate` | `name`, `root?` | Validate structure + frontmatter; report line/token counts. Exit 2 on issues. |
| `csdd_skill_delete` | `name`, `force?`, `root?` | Delete `.claude/skills/<name>/` recursively (`force` required). |

### 🤖 agent — custom sub-agents (`.claude/agents/<name>.md`)

| Tool | Parameters | What it does |
|------|------------|--------------|
| `csdd_agent_create` | `name`, `description`, `tools?[]`, `model?`, `title?`, `force?`, `root?` | Create a least-privilege sub-agent (default tools: `Read`, `Grep`). `description` tells the orchestrator when to pick it. `model` ∈ `sonnet · opus · haiku`. |
| `csdd_agent_list` | `root?` | List agents with their tools and descriptions. |
| `csdd_agent_show` | `name`, `root?` | Print an agent file. |
| `csdd_agent_delete` | `name`, `force?`, `root?` | Delete `.claude/agents/<name>.md` (`force` required). |

### 🔌 mcp — MCP servers in `.mcp.json`

| Tool | Parameters | What it does |
|------|------------|--------------|
| `csdd_mcp_add` | `name`, `command?`, `arg?[]`, `url?`, `type?`, `env?[]`, `disabled?`, `autoApprove?[]`, `force?`, `root?` | Add a server. Provide **either** `command` (+`arg`) for stdio **or** `url` (+`type`) for remote — never both. `type` ∈ `sse · http` (default `http`). `env`/`autoApprove` are `KEY=VALUE` / tool-name lists. `force` replaces an existing entry. |
| `csdd_mcp_list` | `root?` | List servers with type, state, and endpoint. |
| `csdd_mcp_show` | `name`, `root?` | Print a server's config as pretty JSON. |
| `csdd_mcp_remove` | `name`, `force?`, `root?` | Remove a server (`force` required). |
| `csdd_mcp_enable` | `name`, `root?` | Enable a server (`disabled = false`). |
| `csdd_mcp_disable` | `name`, `root?` | Disable a server (`disabled = true`). |
| `csdd_mcp_validate` | `root?` | Validate `.mcp.json` for schema errors. Exit 2 on issues. |

> Avoid `autoApprove` — it breaks least-privilege by auto-approving tool calls.

---

## A typical agent flow

Tool calls that take one feature from idea to ready-to-implement:

```jsonc
csdd_init           { "withBaseline": true }
csdd_spec_init      { "feature": "photo-albums" }

csdd_spec_generate  { "feature": "photo-albums", "artifact": "requirements" }
csdd_spec_validate  { "feature": "photo-albums" }            // exit 2 → fix what it flags
csdd_spec_approve   { "feature": "photo-albums", "phase": "requirements" }

csdd_spec_generate  { "feature": "photo-albums", "artifact": "design" }   // gated on the approval above
csdd_spec_approve   { "feature": "photo-albums", "phase": "design" }

csdd_spec_generate  { "feature": "photo-albums", "artifact": "tasks" }
csdd_spec_approve   { "feature": "photo-albums", "phase": "tasks" }
// → spec.json: ready_for_implementation = true
```

`csdd_spec_status { "feature": "photo-albums" }` between steps shows phase,
approvals, and validation issues in one call.

---

## Development

```bash
npm install
npm run build      # tsc → dist/
npm test           # build + Node's built-in test runner (node:test)
npm run test:run   # tests only, against the current dist/ (no rebuild)
npm run dev        # tsc --watch
```

Tests are TypeScript run through `node:test` with native **type stripping**, so
they need **Node ≥ 22.18** (dev-only — the published package still targets Node
≥ 18). They exercise the argv builders, result formatting, binary resolution,
`runCsdd` against a stub binary, and every tool's argv mapping. See `test/`.

---

## License

MIT
