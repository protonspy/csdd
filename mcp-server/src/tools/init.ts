import { z } from "zod";
import { bool, rootArg, rootField, type ToolDef } from "../tooldef.js";

export const initTools: ToolDef[] = [
  {
    name: "csdd_init",
    title: "Init workspace",
    description:
      "Bootstrap a Claude Code workspace: create .claude/ layout (steering, specs, skills, agents, commands, hooks), CLAUDE.md, csdd.md, .mcp.json, rules, shipped agents/skills/commands/hooks, and guides. Idempotent. Use withBaseline to also scaffold product/tech/structure/security/testing/api-conventions steering files.",
    inputSchema: {
      withBaseline: z
        .boolean()
        .optional()
        .describe("Also scaffold the standard steering files (product, tech, structure, security, testing, api-conventions) and import them into CLAUDE.md."),
      root: rootField,
    },
    toArgs: (p) => ["init", ...bool("--with-baseline", p.withBaseline), ...rootArg(p)],
  },
];
