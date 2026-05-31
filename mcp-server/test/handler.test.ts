import test, { before, after } from "node:test";
import assert from "node:assert/strict";

import { makeHandler, type ToolDef } from "../dist/tooldef.js";
import { makeFakeCsdd, type FakeCsdd } from "./helpers.ts";

// makeHandler wires toArgs -> runCsdd -> toMcpResult. We point CSDD_BIN at the
// stub so the full chain runs without a real csdd build. resolveCsddBin caches
// on first use, so every handler in this process resolves to the same stub.
let fake: FakeCsdd;

before(() => {
  fake = makeFakeCsdd();
  process.env.CSDD_BIN = fake.bin;
});

after(() => fake.cleanup());

function defWith(toArgs: ToolDef["toArgs"]): ToolDef {
  return {
    name: "csdd_test",
    title: "test",
    description: "test",
    inputSchema: {},
    toArgs,
  };
}

test("handler runs the built argv and echoes stdout back as text", async () => {
  const handler = makeHandler(defWith((p) => ["echo", p.feature]));
  const res = await handler({ feature: "albums" });
  assert.equal(res.isError, false);
  assert.equal(res.content[0].text, JSON.stringify(["echo", "albums"]));
});

test("handler surfaces a validation failure (exit 2) as an MCP error", async () => {
  const handler = makeHandler(defWith(() => ["exit2"]));
  const res = await handler({});
  assert.equal(res.isError, true);
  assert.match(res.content[0].text, /validation failed \(exit 2\)/);
});

test("handler tolerates being invoked with no params object", async () => {
  const handler = makeHandler(defWith(() => ["silent"]));
  const res = await handler(undefined as unknown as Record<string, unknown>);
  assert.equal(res.isError, false);
  assert.equal(res.content[0].text, "(ok, no output)");
});

test("handler runs csdd with params.root as the working directory", async () => {
  const handler = makeHandler(defWith(() => ["env"]));
  // Stub's "env" mode prints its cwd; the temp dir holding the stub is a real
  // directory we can assert against.
  const root = fake.bin.replace(/\/[^/]+$/, "");
  const res = await handler({ root });
  assert.match(res.content[0].text, new RegExp(`CWD=${root}(\\n|$)`));
  assert.match(res.content[0].text, /NO_COLOR=1/);
});
