# npm distribution

`csdd` is distributed on npm using the **per-platform optional dependencies**
pattern (the same model as esbuild / swc / the Codex CLI):

- **`csdd/`** — the package users install (`@protonspy/csdd`). Contains only the
  `bin/csdd.js` launcher and declares the five platform packages as
  `optionalDependencies`.
- **`@protonspy/csdd-<platform>-<arch>`** — one package per target, each carrying
  the prebuilt Go binary plus `"os"`/`"cpu"` constraints. npm installs only the
  one matching the host, so there is no postinstall script and no install-time
  download.

`scripts/build-packages.mjs` assembles the full publish tree under `npm/dist/`
(git-ignored) from the release artifacts. The CI release job runs it and then
publishes every package.

## Targets

| Go (`GOOS`/`GOARCH`) | npm package                      | `os` / `cpu`        |
| -------------------- | -------------------------------- | ------------------- |
| `linux` / `amd64`    | `@protonspy/csdd-linux-x64`      | `linux` / `x64`     |
| `linux` / `arm64`    | `@protonspy/csdd-linux-arm64`    | `linux` / `arm64`   |
| `darwin` / `amd64`   | `@protonspy/csdd-darwin-x64`     | `darwin` / `x64`    |
| `darwin` / `arm64`   | `@protonspy/csdd-darwin-arm64`   | `darwin` / `arm64`  |
| `windows` / `amd64`  | `@protonspy/csdd-win32-x64`      | `win32` / `x64`     |

> Windows arm64 is not built by the release matrix yet, so it has no npm package.

## Publishing

CI publishes automatically on every `v*` tag via **npm Trusted Publishing
(OIDC)** — no token stored in the repo, provenance generated automatically. See
the `npm-publish` job in `.github/workflows/release.yml`.

### One-time bootstrap (required before OIDC works)

A Trusted Publisher can only be configured for a package that already has at
least one published version. So the very first release of each of the **6
packages** must be published manually:

```bash
npm login                       # with 2FA
node npm/scripts/build-packages.mjs vX.Y.Z artifacts   # needs the release artifacts locally
for d in npm/dist/csdd-*/; do npm publish "$d" --access public; done
npm publish npm/dist/csdd --access public
```

Then, on npmjs.com, for **each** of the 6 packages: *Settings → Trusted
Publisher → GitHub Actions* → repo `protonspy/csdd`, workflow `release.yml`.
After that, tagging a release is all it takes.
