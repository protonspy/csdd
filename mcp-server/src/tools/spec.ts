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
  {
    name: "csdd_spec_test_report",
    title: "Spec test report",
    description:
      "Record per-spec unit-test evidence into specs/<feature>/test-report.json (surfaced by the dashboard). With run=true it executes the tests (the per-language default command, or cmd) and parses the JUnit + coverage reports they produce into the JSON contract; lang/path auto-discover reports (python|typescript|java|go|rust); or pass an explicit junit and/or coverage file. The run exits non-zero when tests fail, so it gates the task.",
    inputSchema: {
      feature,
      run: z
        .boolean()
        .optional()
        .describe("Execute the tests before parsing (per-language default command, or cmd)."),
      cmd: z
        .string()
        .optional()
        .describe("Test command to execute with run=true (overrides the per-language default; validated against the language's tooling)."),
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
      ...flag("--cmd", p.cmd),
      ...flag("--lang", p.lang),
      ...flag("--path", p.path),
      ...flag("--junit", p.junit),
      ...flag("--coverage", p.coverage),
      ...rootArg(p),
    ],
  },
];
