import { z } from "zod";
import { bool, flag, forceField, rootArg, rootField, type ToolDef } from "../tooldef.js";

const feature = z.string().describe("Feature name (specs/<feature>/).");

export const specTools: ToolDef[] = [
  {
    name: "csdd_spec_init",
    title: "Spec init",
    description:
      "Create specs/<feature>/spec.json (phase=initialized, no approvals, not ready for implementation).",
    inputSchema: {
      feature,
      language: z.string().optional().describe("Spec language (default: en)."),
      flow: z
        .enum(["unit", "tdd", "tdd-e2e"])
        .optional()
        .describe("Development flow: unit (tests after code) | tdd (test-first, default) | tdd-e2e (TDD + e2e). Default: steering default, else tdd."),
      root: rootField,
    },
    toArgs: (p) => [
      "spec",
      "init",
      p.feature,
      ...flag("--language", p.language),
      ...flag("--flow", p.flow),
      ...rootArg(p),
    ],
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
  {
    name: "csdd_spec_test_report",
    title: "Spec test report",
    description:
      "Record per-spec unit-test evidence into specs/<feature>/test-report.json (surfaced by the dashboard). With run=true it executes the tests (the per-language default command, or cmd) and parses the JUnit + coverage reports they produce into the JSON contract; lang/path auto-discover reports (python|typescript|java|go|rust); or pass an explicit junit and/or coverage file. The run exits non-zero when tests fail, so it gates the task. Use fast=true for the Tier-2 task-exit gate (coverage-free, ~3x quicker) and omit it for the Tier-3 feat-exit run, where coverage is collected. Pass task to file the result under one task ID so concurrent implementers preserve each other's evidence.",
    inputSchema: {
      feature,
      run: z
        .boolean()
        .optional()
        .describe("Execute the tests before parsing (per-language default command, or cmd)."),
      cmd: z
        .string()
        .optional()
        .describe("Test command to execute with run=true (overrides the per-language default; validated against the language's tooling AND against test-exclusion/selection flags, which are recorded as an attention)."),
      fast: z
        .boolean()
        .optional()
        .describe("With run=true and no cmd, use the language's coverage-free command (the Tier-2 task-exit gate). Omit for the Tier-3 feat-exit run, which collects coverage."),
      task: z
        .string()
        .optional()
        .describe("Task ID this run is evidence for. Files the result under that task and preserves every other task's result."),
      lang: z
        .enum(["python", "typescript", "java", "go", "rust"])
        .optional()
        .describe("Language: selects the run default command and the coverage format to auto-discover."),
      path: z
        .string()
        .optional()
        .describe("Directory to run in / discover JUnit+coverage reports under (e.g. tests/). Defaults to the workspace root."),
      junit: z.string().optional().describe("Explicit JUnit XML report to parse for test counts."),
      coverage: z
        .string()
        .optional()
        .describe("Explicit coverage report to parse (lcov/Cobertura/JaCoCo/Go coverprofile)."),
      root: rootField,
    },
    toArgs: (p) => [
      "spec",
      "test-report",
      p.feature,
      ...bool("--run", p.run),
      ...bool("--fast", p.fast),
      ...flag("--cmd", p.cmd),
      ...flag("--task", p.task),
      ...flag("--lang", p.lang),
      ...flag("--path", p.path),
      ...flag("--junit", p.junit),
      ...flag("--coverage", p.coverage),
      ...rootArg(p),
    ],
  },
];
