#!/usr/bin/env node
// Launcher for the csdd CLI distributed via npm.
//
// The actual Go binary ships in a per-platform optional dependency
// (@protonspy/csdd-<platform>-<arch>). npm installs only the package whose
// "os"/"cpu" match the host, so this shim just resolves that package's binary
// and execs it — forwarding argv, stdio, signals, and the exit code. No
// postinstall, no network at install time.
import { spawn } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);

const PLATFORM = { darwin: "darwin", linux: "linux", win32: "win32" }[process.platform];
const ARCH = { x64: "x64", arm64: "arm64" }[process.arch];

if (!PLATFORM || !ARCH) {
  console.error(
    `csdd: unsupported platform ${process.platform}/${process.arch}. ` +
      `Prebuilt binaries are available for linux, macOS, and Windows on x64/arm64.`
  );
  process.exit(1);
}

const pkg = `@protonspy/csdd-${PLATFORM}-${ARCH}`;
const binName = process.platform === "win32" ? "csdd.exe" : "csdd";

let binPath;
try {
  binPath = require.resolve(`${pkg}/bin/${binName}`);
} catch {
  console.error(
    `csdd: could not find the native binary for ${PLATFORM}-${ARCH}.\n` +
      `The optional dependency "${pkg}" was not installed.\n` +
      `Reinstall without --no-optional / --ignore-optional, or report an issue at\n` +
      `https://github.com/protonspy/csdd/issues`
  );
  process.exit(1);
}

// When invoked via `npx @protonspy/csdd` (which is `npm exec` under the hood),
// echo that exact spelling in the binary's --help / usage output. A global
// install runs this same launcher as the bare `csdd` command — there npm is not
// in the picture (npm_command is unset), so the binary keeps its default name.
// An explicit CSDD_PROG always wins.
const env = { ...process.env };
if (!env.CSDD_PROG) {
  const argv1 = process.argv[1] || "";
  const viaNpx =
    process.env.npm_command === "exec" ||
    argv1.includes("/_npx/") ||
    argv1.includes("\\_npx\\");
  if (viaNpx) env.CSDD_PROG = "npx @protonspy/csdd";
}

const child = spawn(binPath, process.argv.slice(2), { stdio: "inherit", env });

// Forward terminating signals so the TUI shuts down cleanly.
for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(sig, () => {
    try {
      child.kill(sig);
    } catch {
      /* child already gone */
    }
  });
}

child.on("error", (err) => {
  console.error(`csdd: failed to launch binary: ${err.message}`);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    // Re-raise the signal so the parent exit status reflects it.
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 0);
  }
});
