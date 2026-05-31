#!/usr/bin/env node
// Assemble the npm publish tree from the release artifacts.
//
// Usage: node npm/scripts/build-packages.mjs <tag> [artifactsDir]
//
//   <tag>          the release tag, e.g. "v1.2.3" (the leading "v" is stripped
//                  for the npm version; the raw tag is used to locate the
//                  artifact tarballs produced by the release workflow).
//   [artifactsDir] directory holding csdd_<tag>_<goos>_<goarch>.{tar.gz,zip}
//                  (default: "artifacts").
//
// Output: npm/dist/
//   csdd/                       root package (launcher + optionalDependencies)
//   csdd-<platform>-<arch>/     one per target, carrying the native binary
//
// The Go binaries are reused as-is from the release artifacts (they already
// carry the version baked in via -ldflags), so the npm binary is byte-identical
// to the GitHub release binary.
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const SCOPE = "@protonspy";

// Go target -> npm platform/arch + os/cpu constraints.
const TARGETS = [
  { goos: "linux", goarch: "amd64", node: "linux-x64", os: "linux", cpu: "x64" },
  { goos: "linux", goarch: "arm64", node: "linux-arm64", os: "linux", cpu: "arm64" },
  { goos: "darwin", goarch: "amd64", node: "darwin-x64", os: "darwin", cpu: "x64" },
  { goos: "darwin", goarch: "arm64", node: "darwin-arm64", os: "darwin", cpu: "arm64" },
  { goos: "windows", goarch: "amd64", node: "win32-x64", os: "win32", cpu: "x64" },
];

const scriptDir = dirname(fileURLToPath(import.meta.url));
const npmDir = resolve(scriptDir, "..");
const repoRoot = resolve(npmDir, "..");

const tag = process.argv[2];
if (!tag) {
  console.error("error: missing <tag> argument (e.g. v1.2.3)");
  process.exit(1);
}
const version = tag.replace(/^v/, "");
const artifactsDir = resolve(process.argv[3] ?? join(repoRoot, "artifacts"));
const outDir = join(npmDir, "dist");

// Preflight: every platform artifact must exist before we touch npm/dist, so a
// version mismatch (e.g. `npm-build VERSION=v0.1.2` against v0.1.1 tarballs)
// fails with a clear message instead of a cryptic tar/unzip error — and never
// wipes a good npm/dist or leaves a half-built one that breaks `npm-publish`.
const missingArtifacts = TARGETS.map((t) => {
  const ext = t.goos === "windows" ? "zip" : "tar.gz";
  return join(artifactsDir, `csdd_${tag}_${t.goos}_${t.goarch}.${ext}`);
}).filter((f) => !existsSync(f));
if (missingArtifacts.length > 0) {
  console.error(
    `error: missing release artifacts for ${tag} in ${artifactsDir}:\n` +
      missingArtifacts.map((f) => "  " + f).join("\n") +
      `\nbuild them first:  make dist VERSION=${tag}`
  );
  process.exit(1);
}

rmSync(outDir, { recursive: true, force: true });
mkdirSync(outDir, { recursive: true });

const repository = {
  type: "git",
  url: "git+https://github.com/protonspy/csdd.git",
};

// --- per-platform packages -------------------------------------------------
const optionalDependencies = {};
for (const t of TARGETS) {
  const pkgName = `${SCOPE}/csdd-${t.node}`;
  const binName = t.goos === "windows" ? "csdd.exe" : "csdd";
  const pkgDir = join(outDir, `csdd-${t.node}`);
  const binDir = join(pkgDir, "bin");
  mkdirSync(binDir, { recursive: true });

  const base = `csdd_${tag}_${t.goos}_${t.goarch}`;
  if (t.goos === "windows") {
    execFileSync("unzip", ["-o", join(artifactsDir, `${base}.zip`), "-d", binDir], {
      stdio: "inherit",
    });
  } else {
    execFileSync("tar", ["-xzf", join(artifactsDir, `${base}.tar.gz`), "-C", binDir], {
      stdio: "inherit",
    });
    chmodSync(join(binDir, binName), 0o755);
  }

  writeFileSync(
    join(pkgDir, "package.json"),
    JSON.stringify(
      {
        name: pkgName,
        version,
        description: `csdd native binary for ${t.node}`,
        repository,
        license: "Apache-2.0",
        os: [t.os],
        cpu: [t.cpu],
        files: [`bin/${binName}`],
      },
      null,
      2
    ) + "\n"
  );
  optionalDependencies[pkgName] = version;
  console.log(`built ${pkgName}@${version}`);
}

// --- root package ----------------------------------------------------------
const rootSrc = join(npmDir, "csdd");
const rootOut = join(outDir, "csdd");
mkdirSync(join(rootOut, "bin"), { recursive: true });
cpSync(join(rootSrc, "bin", "csdd.js"), join(rootOut, "bin", "csdd.js"));
cpSync(join(rootSrc, "README.md"), join(rootOut, "README.md"));

const rootPkg = JSON.parse(readFileSync(join(rootSrc, "package.json"), "utf8"));
rootPkg.version = version;
rootPkg.optionalDependencies = optionalDependencies;
writeFileSync(join(rootOut, "package.json"), JSON.stringify(rootPkg, null, 2) + "\n");
console.log(`built ${rootPkg.name}@${version}`);

// --- mcp-server package ----------------------------------------------------
// Ship @protonspy/csdd-mcp in lockstep with the CLI: stamp it to the same
// release version and pin the per-platform csdd binaries it resolves to that
// exact version, then stage its pre-built dist/ for publish. Build it first:
//   npm --prefix mcp-server ci && npm --prefix mcp-server run build
const mcpSrc = join(repoRoot, "mcp-server");
const mcpDist = join(mcpSrc, "dist");
if (!existsSync(mcpDist)) {
  console.error(
    "error: mcp-server/dist not found — build it first:\n" +
      "  npm --prefix mcp-server ci && npm --prefix mcp-server run build\n" +
      "  (or `make mcp-dist`)"
  );
  process.exit(1);
}
const mcpOut = join(outDir, "csdd-mcp");
mkdirSync(mcpOut, { recursive: true });
cpSync(mcpDist, join(mcpOut, "dist"), { recursive: true });
cpSync(join(mcpSrc, "README.md"), join(mcpOut, "README.md"));

const mcpPkg = JSON.parse(readFileSync(join(mcpSrc, "package.json"), "utf8"));
mcpPkg.version = version;
for (const k of Object.keys(mcpPkg.optionalDependencies ?? {})) {
  mcpPkg.optionalDependencies[k] = version; // exact, in lockstep with the binary
}
delete mcpPkg.devDependencies; // not needed by consumers of the published package
delete mcpPkg.scripts; // prepublishOnly would re-run tsc against a src/ we don't ship
writeFileSync(join(mcpOut, "package.json"), JSON.stringify(mcpPkg, null, 2) + "\n");
console.log(`built ${mcpPkg.name}@${version}`);
