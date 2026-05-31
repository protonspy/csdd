import test from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { tmpdir } from "node:os";

// A separate file (= separate process) so resolveCsddBin's cache starts empty
// and picks up this bogus path. A spawn failure (ENOENT) must surface as a
// graceful exit-127 result with guidance, never an unhandled rejection.
const missing = join(tmpdir(), "definitely-not-a-real-csdd-binary-xyz");
process.env.CSDD_BIN = missing;

const { runCsdd } = await import("../dist/csdd.js");

test("runCsdd maps a missing binary to exit 127 with guidance", async () => {
  const r = await runCsdd(["version"]);
  assert.equal(r.ok, false);
  assert.equal(r.exitCode, 127);
  assert.match(r.stderr, /not found or not executable/);
  assert.match(r.stderr, /CSDD_BIN/);
  assert.match(r.stderr, new RegExp(missing.replace(/[.\\]/g, "\\$&")));
});
