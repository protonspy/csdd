import test, { after } from "node:test";
import assert from "node:assert/strict";
import { realpathSync } from "node:fs";
import { tmpdir } from "node:os";

import { runCsdd, resolveCsddBin } from "../dist/csdd.js";
import { makeFakeCsdd } from "./helpers.ts";

// Resolve to the stub for the whole file. CSDD_BIN is the highest-priority
// source in resolveCsddBin, and the result is cached process-wide.
const fake = makeFakeCsdd();
process.env.CSDD_BIN = fake.bin;

after(() => fake.cleanup());

test("resolveCsddBin honours the CSDD_BIN override", () => {
  assert.equal(resolveCsddBin(), fake.bin);
});

test("resolveCsddBin caches the first resolution", () => {
  process.env.CSDD_BIN = "/some/other/path";
  assert.equal(resolveCsddBin(), fake.bin, "later CSDD_BIN changes are ignored");
  process.env.CSDD_BIN = fake.bin; // restore for the runCsdd cases below
});

test("runCsdd captures stdout and a zero exit as ok", async () => {
  const r = await runCsdd(["hello", "world"]);
  assert.equal(r.ok, true);
  assert.equal(r.exitCode, 0);
  assert.equal(r.stdout.trim(), JSON.stringify(["hello", "world"]));
  assert.equal(r.stderr, "");
});

test("runCsdd reports exit 2 (validation) without rejecting", async () => {
  const r = await runCsdd(["exit2"]);
  assert.equal(r.ok, false);
  assert.equal(r.exitCode, 2);
  assert.match(r.stderr, /validation boom/);
});

test("runCsdd reports a generic non-zero exit", async () => {
  const r = await runCsdd(["fail"]);
  assert.equal(r.ok, false);
  assert.equal(r.exitCode, 3);
  assert.match(r.stderr, /kaboom/);
});

test("runCsdd handles a silent success", async () => {
  const r = await runCsdd(["silent"]);
  assert.equal(r.ok, true);
  assert.equal(r.stdout, "");
  assert.equal(r.stderr, "");
});

test("runCsdd keeps stderr alongside stdout on success", async () => {
  const r = await runCsdd(["stderr-ok"]);
  assert.equal(r.ok, true);
  assert.match(r.stdout, /out line/);
  assert.match(r.stderr, /warn line/);
});

test("runCsdd forces NO_COLOR and runs in the given cwd", async () => {
  const cwd = realpathSync(tmpdir());
  const r = await runCsdd(["env"], cwd);
  assert.match(r.stdout, /NO_COLOR=1/);
  assert.match(r.stdout, new RegExp(`CWD=${cwd}(\\n|$)`));
});
