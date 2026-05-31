import { z } from "zod";
import { bool, flag, forceField, rootArg, rootField, type ToolDef } from "../tooldef.js";

const feature = z.string().describe("Feature name (specs/<feature>/).");

export const specTools: ToolDef[] = [
  {
    name: "csdd_spec_init",
    title: "Spec init",
    description:
      "Create specs/<feature>/spec.json (phase=initial, no approvals, not ready for implementation).",
    inputSchema: {
      feature,
      language: z.string().optional().describe("Spec language (default: en)."),
      root: rootField,
    },
    toArgs: (p) => ["spec", "init", p.feature, ...flag("--language", p.language), ...rootArg(p)],
  },
  {
    name: "csdd_spec_list",
    title: "Spec list",
    description: "List specs with current phase, approved phases, and readiness.",
    inputSchema: { root: rootField },
    toArgs: (p) => ["spec", "list", ...rootArg(p)],
  },
  {
    name: "csdd_spec_show",
    title: "Spec show",
    description: "Show a spec's metadata (spec.json) and its artifacts.",
    inputSchema: { feature, root: rootField },
    toArgs: (p) => ["spec", "show", p.feature, ...rootArg(p)],
  },
  {
    name: "csdd_spec_status",
    title: "Spec status",
    description: "Combined show + validate for a spec.",
    inputSchema: { feature, root: rootField },
    toArgs: (p) => ["spec", "status", p.feature, ...rootArg(p)],
  },
  {
    name: "csdd_spec_generate",
    title: "Spec generate artifact",
    description:
      "Generate a spec artifact. Phase gates apply: design needs requirements approved, tasks needs design approved (use force to bypass). research/bugfix are ungated.",
    inputSchema: {
      feature,
      artifact: z
        .enum(["requirements", "design", "tasks", "research", "bugfix"])
        .describe("Which artifact to generate."),
      force: forceField,
      root: rootField,
    },
    toArgs: (p) => [
      "spec",
      "generate",
      p.feature,
      ...flag("--artifact", p.artifact),
      ...bool("--force", p.force),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_spec_approve",
    title: "Spec approve phase",
    description:
      "Approve a spec phase (requirements|design|tasks). Validates first; force approves despite issues/missing prior approvals. Sets ready_for_implementation only when all three are approved.",
    inputSchema: {
      feature,
      phase: z.enum(["requirements", "design", "tasks"]).describe("Phase to approve."),
      force: forceField,
      root: rootField,
    },
    toArgs: (p) => [
      "spec",
      "approve",
      p.feature,
      ...flag("--phase", p.phase),
      ...bool("--force", p.force),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_spec_validate",
    title: "Spec validate",
    description:
      "Validate a spec: EARS phrasing, traceability, task annotations, parallel safety. Exit 2 on issues.",
    inputSchema: { feature, root: rootField },
    toArgs: (p) => ["spec", "validate", p.feature, ...rootArg(p)],
  },
  {
    name: "csdd_spec_delete",
    title: "Spec delete",
    description: "Delete specs/<feature>/ recursively. Requires force.",
    inputSchema: { feature, force: forceField, root: rootField },
    toArgs: (p) => ["spec", "delete", p.feature, ...bool("--force", p.force), ...rootArg(p)],
  },
];
