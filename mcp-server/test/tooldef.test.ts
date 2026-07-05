import test from "node:test";
import assert from "node:assert/strict";

import {
  flag,
  bool,
  multi,
  rootArg,
  toMcpResult,
} from "../dist/tooldef.js";
import type { CsddResult } from "../dist/csdd.js";

// --- argv builders ---------------------------------------------------------

test("flag emits --name value for non-empty values", () => {
  assert.deepEqual(flag("--language", "pt"), ["--language", "pt"]);
  assert.deepEqual(flag("--n", 0), ["--n", "0"], "0 is a real value, not empty");
  assert.deepEqual(flag("--n", false), ["--n", "false"]);
});

test("flag omits empty / null / undefined", () => {
  assert.deepEqual(flag("--x", ""), []);
  assert.deepEqual(flag("--x", null), []);
  assert.deepEqual(flag("--x", undefined), []);
});

test("bool emits the flag only when truthy", () => {
  assert.deepEqual(bool("--force", true), ["--force"]);
  assert.deepEqual(bool("--force", false), []);
  assert.deepEqual(bool("--force", undefined), []);
});

test("multi repeats --name per array item, stringifying", () => {
  assert.deepEqual(multi("--tools", ["Read", "Grep"]), [
    "--tools",
    "Read",
    "--tools",
    "Grep",
  ]);
  assert.deepEqual(multi("--arg", [1, 2]), ["--arg", "1", "--arg", "2"]);
});

test("multi yields nothing for non-arrays / empty", () => {
  assert.deepEqual(multi("--x", undefined), []);
  assert.deepEqual(multi("--x", "scalar"), []);
  assert.deepEqual(multi("--x", []), []);
});

test("rootArg passes --root through only when set", () => {
  assert.deepEqual(rootArg({ root: "/proj" }), ["--root", "/proj"]);
  assert.deepEqual(rootArg({}), []);
  assert.deepEqual(rootArg({ root: "" }), []);
});

// --- result formatting -----------------------------------------------------

const ok = (over: Partial<CsddResult> = {}): CsddResult => ({
  ok: true,
  exitCode: 0,
  stdout: "",
  stderr: "",
  ...over,
});

test("toMcpResult: success with stdout", () => {
  const r = toMcpResult(ok({ stdout: "all good\n" }));
  assert.equal(r.isError, false);
  assert.equal(r.content[0].text, "all good");
});

test("toMcpResult: success with no output gets a friendly placeholder", () => {
  const r = toMcpResult(ok());
  assert.equal(r.isError, false);
  assert.equal(r.content[0].text, "(ok, no output)");
});

test("toMcpResult: success keeps stderr unlabelled (warnings)", () => {
  const r = toMcpResult(ok({ stdout: "done", stderr: "heads up" }));
  assert.equal(r.isError, false);
  assert.match(r.content[0].text, /done/);
  assert.match(r.content[0].text, /heads up/);
  assert.doesNotMatch(r.content[0].text, /\[stderr\]/);
});

test("toMcpResult: exit 2 is flagged as a validation failure", () => {
  const r = toMcpResult(ok({ ok: false, exitCode: 2, stderr: "bad EARS" }));
  assert.equal(r.isError, true);
  assert.match(r.content[0].text, /^csdd validation failed \(exit 2\):/);
  assert.match(r.content[0].text, /\[stderr\] bad EARS/);
});

test("toMcpResult: generic non-zero exit", () => {
  const r = toMcpResult(ok({ ok: false, exitCode: 3, stderr: "boom" }));
  assert.equal(r.isError, true);
  assert.match(r.content[0].text, /^csdd failed \(exit 3\):/);
  assert.match(r.content[0].text, /\[stderr\] boom/);
});

test("toMcpResult: failure with no output names the exit code", () => {
  const r = toMcpResult(ok({ ok: false, exitCode: 1 }));
  assert.equal(r.isError, true);
  assert.match(r.content[0].text, /csdd exited with code 1/);
});
