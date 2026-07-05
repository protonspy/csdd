// Resolve the csdd binary and run it headlessly, capturing structured output.
//
// Resolution order (first hit wins):
//   1. $CSDD_BIN                          — explicit override
//   2. platform optionalDependency        — @protonspy/csdd-<platform>-<arch>
//      (the same packages the npm launcher uses)
//   3. sibling repo binary                — ../csdd (when running from the repo)
//   4. "csdd" on $PATH                     — last-resort lookup at spawn time
import { execFile } from "node:child_process";
import { createRequire } from "node:module";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);

// platform/arch -> npm package name (must match the npm launcher's TARGETS).
const PKG: Record<string, string> = {
  "linux-x64": "@protonspy/csdd-linux-x64",
  "linux-arm64": "@protonspy/csdd-linux-arm64",
  "darwin-x64": "@protonspy/csdd-darwin-x64",
  "darwin-arm64": "@protonspy/csdd-darwin-arm64",
  "win32-x64": "@protonspy/csdd-win32-x64",
  "win32-arm64": "@protonspy/csdd-win32-arm64",
};

const binName = process.platform === "win32" ? "csdd.exe" : "csdd";

function fromPlatformPackage(): string | null {
  const pkgName = PKG[`${process.platform}-${process.arch}`];
  if (!pkgName) return null;
  try {
    const pkgJson = require.resolve(`${pkgName}/package.json`);
    const candidate = join(pkgJson, "..", "bin", binName);
    return existsSync(candidate) ? candidate : null;
  } catch {
    return null;
  }
}

function fromSiblingRepo(): string | null {
  // dist/csdd.js -> packageRoot = mcp-server -> repo root holds ./csdd
  const here = dirname(fileURLToPath(import.meta.url));
  for (const up of ["..", "../.."]) {
    const candidate = join(here, up, binName);
    if (existsSync(candidate)) return candidate;
  }
  return null;
}

let cached: string | null = null;

/** Locate the csdd binary, caching the result. Falls back to a bare "csdd". */
export function resolveCsddBin(): string {
  if (cached) return cached;
  cached =
    process.env.CSDD_BIN ||
    fromPlatformPackage() ||
    fromSiblingRepo() ||
    binName; // let the OS resolve it on $PATH at spawn time
  return cached;
}

export interface CsddResult {
  ok: boolean;
  exitCode: number;
  stdout: string;
  stderr: string;
}

/**
 * Run `csdd` with the given argv. Never rejects on a non-zero exit — the exit
 * code is returned so each tool can map it to an MCP error result.
 * `cwd` controls workspace discovery when no --root flag is passed.
 */
export function runCsdd(argv: string[], cwd?: string): Promise<CsddResult> {
  const bin = resolveCsddBin();
  return new Promise((resolve) => {
    execFile(
      bin,
      argv,
      {
        cwd: cwd || process.cwd(),
        // NO_COLOR keeps output free of ANSI escapes; csdd is non-interactive
        // (confirm() returns false) when stdin is not a TTY, which it isn't here.
        env: { ...process.env, NO_COLOR: "1" },
        maxBuffer: 16 * 1024 * 1024,
        windowsHide: true,
      },
      (err, stdout, stderr) => {
        const code =
          err && typeof (err as NodeJS.ErrnoException).code === "string"
            ? // spawn failure (ENOENT etc.) — surface as exit 127
              127
            : ((err as { code?: number } | null)?.code ?? 0);
        if (err && code === 127) {
          resolve({
            ok: false,
            exitCode: 127,
            stdout: stdout?.toString() ?? "",
            stderr:
              (stderr?.toString() ?? "") +
              `\ncsdd binary not found or not executable: ${bin}\n` +
              `Set CSDD_BIN, install @protonspy/csdd, or put csdd on PATH.`,
          });
          return;
        }
        resolve({
          ok: code === 0,
          exitCode: typeof code === "number" ? code : 1,
          stdout: stdout?.toString() ?? "",
          stderr: stderr?.toString() ?? "",
        });
      },
    );
  });
}
