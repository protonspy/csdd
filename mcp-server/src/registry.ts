// The MCP tools this server exposes — the csdd *development-flow* resources
// (steering, spec, skill, agent), plus a diagnostic version tool. Workspace
// setup, maintenance, and config management (init, update, mcp, export) are intentionally NOT exposed:
// those are one-time CLI operations a human runs, not part of the iterative
// loop the agent drives. Kept separate from index.ts so it can be imported
// (e.g. by tests) without booting the stdio transport.
import { type ToolDef } from "./tooldef.js";
import { steeringTools } from "./tools/steering.js";
import { specTools } from "./tools/spec.js";
import { skillTools } from "./tools/skill.js";
import { agentTools } from "./tools/agent.js";

export const miscTools: ToolDef[] = [
  {
    name: "csdd_version",
    title: "csdd version",
    description: "Print the version of the underlying csdd binary (diagnostic).",
    inputSchema: {},
    toArgs: () => ["version"],
  },
];

export const allTools: ToolDef[] = [
  ...miscTools,
  ...steeringTools,
  ...specTools,
  ...skillTools,
  ...agentTools,
];
