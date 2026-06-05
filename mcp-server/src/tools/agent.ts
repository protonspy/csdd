import { z } from "zod";
import {
  bool,
  flag,
  forceField,
  multi,
  rootArg,
  rootField,
  type ToolDef,
} from "../tooldef.js";

const agentName = z.string().describe("Agent name (.claude/agents/<name>.md).");

export const agentTools: ToolDef[] = [
  {
    name: "csdd_agent_create",
    title: "Agent create",
    description:
      "Create a custom sub-agent with least-privilege tools (default: Read, Grep). Description tells the orchestrator when to pick it.",
    inputSchema: {
      name: agentName,
      description: z.string().describe("When the orchestrator should select this agent."),
      tools: z
        .array(z.string())
        .optional()
        .describe("Tool names to grant (e.g. Read, Grep, Bash, Edit). Repeatable."),
      model: z.string().optional().describe("Model override (e.g. sonnet, opus, haiku)."),
      title: z.string().optional().describe("Document title (defaults to Title Case of name)."),
      force: forceField,
      root: rootField,
    },
    toArgs: (p) => [
      "agent",
      "create",
      p.name,
      ...flag("--description", p.description),
      ...multi("--tools", p.tools),
      ...flag("--model", p.model),
      ...flag("--title", p.title),
      ...bool("--force", p.force),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_agent_list",
    title: "Agent list",
    description: "List agents with their tools and descriptions.",
    inputSchema: { root: rootField },
    toArgs: (p) => ["agent", "list", ...rootArg(p)],
  },
  {
    name: "csdd_agent_show",
    title: "Agent show",
    description: "Print an agent file.",
    inputSchema: { name: agentName, root: rootField },
    toArgs: (p) => ["agent", "show", p.name, ...rootArg(p)],
  },
  {
    name: "csdd_agent_delete",
    title: "Agent delete",
    description: "Delete .claude/agents/<name>.md. Requires force.",
    inputSchema: { name: agentName, force: forceField, root: rootField },
    toArgs: (p) => ["agent", "delete", p.name, ...bool("--force", p.force), ...rootArg(p)],
  },
];
