// Test helpers. The MCP server shells out to a `csdd` binary; rather than
// depend on a real build, these tests point CSDD_BIN at a tiny Node stub that
// mimics the bits of csdd behaviour the server cares about (exit codes,
// stdout/stderr, and that NO_COLOR / cwd are passed through).
import { mkdtempSync, writeFileSync, chmodSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// The stub dispatches on its first argument so a single cached binary can cover
// every runCsdd scenario:
//   exit2        -> stderr + exit 2 (validation failure)
//   fail         -> stderr + exit 3 (generic failure)
//   silent       -> exit 0, no output
//   stderr-ok    -> stdout + stderr, exit 0 (warning on success)
//   env          -> print NO_COLOR and cwd so the caller can assert them
//   <anything>   -> echo the full argv as JSON, exit 0
const STUB_SRC = `#!/usr/bin/env node
const args = process.argv.slice(2);
const mode = args[0];
switch (mode) {
  case "exit2":
    process.stderr.write("validation boom\\n");
    process.exit(2);
  case "fail":
    process.stderr.write("kaboom\\n");
    process.exit(3);
  case "silent":
    process.exit(0);
  case "stderr-ok":
    process.stdout.write("out line\\n");
    process.stderr.write("warn line\\n");
    process.exit(0);
  case "env":
    process.stdout.write("NO_COLOR=" + (process.env.NO_COLOR ?? "") + "\\n");
    process.stdout.write("CWD=" + process.cwd() + "\\n");
    process.exit(0);
  default:
    process.stdout.write(JSON.stringify(args) + "\\n");
    process.exit(0);
}
`;

export interface FakeCsdd {
  /** Absolute path to the executable stub. */
  bin: string;
  /** Remove the temp directory holding the stub. */
  cleanup: () => void;
}

/** Write an executable csdd stub to a fresh temp dir and return its path. */
export function makeFakeCsdd(): FakeCsdd {
  const dir = mkdtempSync(join(tmpdir(), "csdd-mcp-test-"));
  const bin = join(dir, "fake-csdd.mjs");
  writeFileSync(bin, STUB_SRC);
  chmodSync(bin, 0o755);
  return { bin, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}
