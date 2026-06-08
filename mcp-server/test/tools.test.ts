import test from "node:test";
import assert from "node:assert/strict";

import { allTools, miscTools } from "../dist/registry.js";
import type { ToolDef } from "../dist/tooldef.js";

const byName = new Map(allTools.map((t) => [t.name, t] as const));

function argv(name: string, params: Record<string, unknown>): string[] {
  const def = byName.get(name);
  assert.ok(def, `tool ${name} is registered`);
  return (def as ToolDef).toArgs(params);
}

// Every tool, with a representative set of params, mapped to the exact csdd
// argv it should produce. Keeping this exhaustive is the point: the coverage
// test below fails if a registered tool is missing from this table.
const CASES: Array<[name: string, params: Record<string, unknown>, expected: string[]]> = [
  // misc
  ["csdd_version", {}, ["version"]],

  // steering
  ["csdd_steering_init", {}, ["steering", "init"]],
  ["csdd_steering_init", { root: "/p" }, ["steering", "init", "--root", "/p"]],
  [
    "csdd_steering_create",
    { name: "obs", inclusion: "auto", description: "d" },
    ["steering", "create", "obs", "--inclusion", "auto", "--description", "d"],
  ],
  [
    "csdd_steering_create",
    { name: "api", inclusion: "fileMatch", pattern: ["a", "b"], title: "API", force: true, root: "/p" },
    ["steering", "create", "api", "--inclusion", "fileMatch", "--pattern", "a", "--pattern", "b", "--title", "API", "--force", "--root", "/p"],
  ],
  ["csdd_steering_list", {}, ["steering", "list"]],
  ["csdd_steering_list", { inclusion: "always", root: "/p" }, ["steering", "list", "--root", "/p", "--inclusion", "always"]],
  ["csdd_steering_show", { name: "tech" }, ["steering", "show", "tech"]],
  ["csdd_steering_delete", { name: "x" }, ["steering", "delete", "x"]],
  ["csdd_steering_delete", { name: "x", force: true, root: "/p" }, ["steering", "delete", "x", "--force", "--root", "/p"]],
  ["csdd_steering_validate", {}, ["steering", "validate"]],
  ["csdd_steering_validate", { name: "tech", root: "/p" }, ["steering", "validate", "tech", "--root", "/p"]],

  // spec
  ["csdd_spec_init", { feature: "f" }, ["spec", "init", "f"]],
  ["csdd_spec_init", { feature: "f", language: "pt", root: "/p" }, ["spec", "init", "f", "--language", "pt", "--root", "/p"]],
  ["csdd_spec_list", {}, ["spec", "list"]],
  ["csdd_spec_show", { feature: "f" }, ["spec", "show", "f"]],
  ["csdd_spec_status", { feature: "f" }, ["spec", "status", "f"]],
  ["csdd_spec_generate", { feature: "f", artifact: "design" }, ["spec", "generate", "f", "--artifact", "design"]],
  ["csdd_spec_generate", { feature: "f", artifact: "tasks", force: true, root: "/p" }, ["spec", "generate", "f", "--artifact", "tasks", "--force", "--root", "/p"]],
  ["csdd_spec_approve", { feature: "f", phase: "requirements" }, ["spec", "approve", "f", "--phase", "requirements"]],
  ["csdd_spec_approve", { feature: "f", phase: "design", force: true }, ["spec", "approve", "f", "--phase", "design", "--force"]],
  ["csdd_spec_validate", { feature: "f" }, ["spec", "validate", "f"]],
  ["csdd_spec_delete", { feature: "f", force: true }, ["spec", "delete", "f", "--force"]],
  [
    "csdd_spec_test_report",
    { feature: "f", lang: "go", path: "tests/" },
    ["spec", "test-report", "f", "--lang", "go", "--path", "tests/"],
  ],
  [
    "csdd_spec_test_report",
    { feature: "f", run: true, cmd: "go test ./...", junit: "junit.xml", coverage: "coverage.out", root: "/p" },
    ["spec", "test-report", "f", "--run", "--cmd", "go test ./...", "--junit", "junit.xml", "--coverage", "coverage.out", "--root", "/p"],
  ],
  ["csdd_spec_diff_report", { feature: "f" }, ["spec", "diff-report", "f"]],
  [
    "csdd_spec_diff_report",
    { feature: "f", base: "main", maxLines: 500, root: "/p" },
    ["spec", "diff-report", "f", "--base", "main", "--max-lines", "500", "--root", "/p"],
  ],

  // skill
  ["csdd_skill_create", { name: "s", description: "d" }, ["skill", "create", "s", "--description", "d"]],
  ["csdd_skill_create", { name: "s", description: "d", title: "S", root: "/p" }, ["skill", "create", "s", "--description", "d", "--title", "S", "--root", "/p"]],
  [
    "csdd_skill_create",
    { name: "s", description: "d", model: "opus", effort: "low" },
    ["skill", "create", "s", "--description", "d", "--model", "opus", "--effort", "low"],
  ],
  ["csdd_skill_list", {}, ["skill", "list"]],
  ["csdd_skill_show", { name: "s" }, ["skill", "show", "s"]],
  ["csdd_skill_add_reference", { skill: "s", file: "r.md" }, ["skill", "add-reference", "s", "r.md"]],
  ["csdd_skill_add_script", { skill: "s", file: "x.sh", root: "/p" }, ["skill", "add-script", "s", "x.sh", "--root", "/p"]],
  ["csdd_skill_add_asset", { skill: "s", file: "a.png" }, ["skill", "add-asset", "s", "a.png"]],
  ["csdd_skill_validate", { name: "s" }, ["skill", "validate", "s"]],
  ["csdd_skill_delete", { name: "s", force: true }, ["skill", "delete", "s", "--force"]],

  // agent
  ["csdd_agent_create", { name: "rev", description: "d" }, ["agent", "create", "rev", "--description", "d"]],
  [
    "csdd_agent_create",
    { name: "rev", description: "d", tools: ["Read", "Grep"], model: "opus", title: "Rev", force: true, root: "/p" },
    ["agent", "create", "rev", "--description", "d", "--tools", "Read", "--tools", "Grep", "--model", "opus", "--title", "Rev", "--force", "--root", "/p"],
  ],
  [
    "csdd_agent_create",
    { name: "rev", description: "d", effort: "high" },
    ["agent", "create", "rev", "--description", "d", "--effort", "high"],
  ],
  [
    "csdd_agent_create",
    { name: "rev", description: "d", model: "opus", effort: "max" },
    ["agent", "create", "rev", "--description", "d", "--model", "opus", "--effort", "max"],
  ],
  ["csdd_agent_list", {}, ["agent", "list"]],
  ["csdd_agent_show", { name: "rev" }, ["agent", "show", "rev"]],
  ["csdd_agent_delete", { name: "rev", force: true }, ["agent", "delete", "rev", "--force"]],
];

for (const [name, params, expected] of CASES) {
  test(`toArgs ${name} ${JSON.stringify(params)}`, () => {
    assert.deepEqual(argv(name, params), expected);
  });
}

// --- registry invariants ---------------------------------------------------

test("every registered tool is exercised by the CASES table", () => {
  const covered = new Set(CASES.map(([name]) => name));
  const missing = allTools.map((t) => t.name).filter((n) => !covered.has(n));
  assert.deepEqual(missing, [], "tools missing a toArgs test case");
});

test("tool names are unique", () => {
  const names = allTools.map((t) => t.name);
  assert.equal(new Set(names).size, names.length);
});

test("every tool has the required, well-formed fields", () => {
  for (const t of allTools) {
    assert.match(t.name, /^csdd(_[a-z]+)+$/, `name shape: ${t.name}`);
    assert.ok(t.title && t.title.length > 0, `${t.name} has a title`);
    assert.ok(t.description && t.description.length > 0, `${t.name} has a description`);
    assert.equal(typeof t.inputSchema, "object", `${t.name} inputSchema is an object`);
    assert.equal(typeof t.toArgs, "function", `${t.name} toArgs is a function`);
  }
});

test("miscTools are included in allTools", () => {
  for (const m of miscTools) {
    assert.ok(byName.has(m.name), `${m.name} present in allTools`);
  }
});

test("the first argv token is always a known csdd resource/command", () => {
  const roots = new Set(["version", "steering", "spec", "skill", "agent"]);
  for (const t of allTools) {
    // Call with empty params; we only inspect the leading command token, which
    // never depends on user input.
    const head = t.toArgs({})[0];
    assert.ok(roots.has(head), `${t.name} -> ${head}`);
  }
});
