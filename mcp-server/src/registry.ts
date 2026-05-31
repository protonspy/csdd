// The full set of MCP tools this server exposes — one per csdd subcommand,
// plus a diagnostic version tool. Kept separate from index.ts so it can be
// imported (e.g. by tests) without booting the stdio transport.
import { type ToolDef } from "./tooldef.js";
import { initTools } from "./tools/init.js";
import { steeringTools } from "./tools/steering.js";
import { specTools } from "./tools/spec.js";
import { skillTools } from "./tools/skill.js";
import { agentTools } from "./tools/agent.js";
import { mcpTools } from "./tools/mcp.js";

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
  ...initTools,
  ...steeringTools,
  ...specTools,
  ...skillTools,
  ...agentTools,
  ...mcpTools,
];
