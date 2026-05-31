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

const inclusion = z
  .enum(["always", "fileMatch", "manual", "auto"])
  .describe(
    "When the steering loads: always (always-on), fileMatch (when files match --pattern), manual (#name), auto (when description matches context).",
  );

export const steeringTools: ToolDef[] = [
  {
    name: "csdd_steering_init",
    title: "Steering init",
    description:
      "Create .claude/steering/ and populate the 6 standard files (product, tech, structure, security, testing, api-conventions).",
    inputSchema: { root: rootField },
    toArgs: (p) => ["steering", "init", ...rootArg(p)],
  },
  {
    name: "csdd_steering_create",
    title: "Steering create",
    description:
      "Create a custom steering file with inclusion metadata. fileMatch requires at least one pattern; auto requires a description.",
    inputSchema: {
      name: z.string().describe("Steering file name (no extension)."),
      inclusion,
      pattern: z
        .array(z.string())
        .optional()
        .describe("Glob pattern(s) for fileMatch inclusion. Repeatable."),
      description: z
        .string()
        .optional()
        .describe("One-line description; required for auto inclusion."),
      title: z.string().optional().describe("Document title (defaults to Title Case of name)."),
      force: forceField,
      root: rootField,
    },
    toArgs: (p) => [
      "steering",
      "create",
      p.name,
      ...flag("--inclusion", p.inclusion),
      ...multi("--pattern", p.pattern),
      ...flag("--description", p.description),
      ...flag("--title", p.title),
      ...bool("--force", p.force),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_steering_list",
    title: "Steering list",
    description: "List steering files with inclusion mode and inclusion-specific extras.",
    inputSchema: {
      root: rootField,
      inclusion: inclusion.optional().describe("Filter by inclusion mode."),
    },
    toArgs: (p) => ["steering", "list", ...rootArg(p), ...flag("--inclusion", p.inclusion)],
  },
  {
    name: "csdd_steering_show",
    title: "Steering show",
    description: "Print a steering file (frontmatter + body).",
    inputSchema: { name: z.string().describe("Steering file name."), root: rootField },
    toArgs: (p) => ["steering", "show", p.name, ...rootArg(p)],
  },
  {
    name: "csdd_steering_delete",
    title: "Steering delete",
    description:
      "Delete a steering file. Requires force. Foundational files (product, tech, structure) are protected.",
    inputSchema: {
      name: z.string().describe("Steering file name."),
      force: forceField,
      root: rootField,
    },
    toArgs: (p) => ["steering", "delete", p.name, ...bool("--force", p.force), ...rootArg(p)],
  },
  {
    name: "csdd_steering_validate",
    title: "Steering validate",
    description:
      "Validate steering frontmatter and structure. Omit name to validate all. Exit 2 on issues.",
    inputSchema: {
      name: z.string().optional().describe("Validate only this file; omit for all."),
      root: rootField,
    },
    toArgs: (p) => ["steering", "validate", ...(p.name ? [p.name] : []), ...rootArg(p)],
  },
];
