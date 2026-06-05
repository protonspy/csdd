# csdd

**Claude Spec-Driven Development — as an executable contract.**

`csdd` is a single Go binary that turns the Spec-Driven Development (SDD)
workflow for [Claude Code](https://claude.com/claude-code) into a contract that
is validated mechanically — for humans *and* AI agents.

## Run

No install needed — `npx` fetches the right prebuilt binary for your platform:

```bash
npx @protonspy/csdd --help
npx @protonspy/csdd                # interactive TUI
```

Prefer the short `csdd` command on your `PATH`? Install it globally:

```bash
npm install -g @protonspy/csdd     # then: csdd
```

This package ships a thin launcher; the native binary for your platform is
pulled in automatically as an optional dependency (no postinstall scripts, no
download at install time). Prebuilt for linux, macOS, and Windows on x64/arm64.

## Usage

```bash
npx @protonspy/csdd                                              # interactive TUI
npx @protonspy/csdd spec generate photo-albums --artifact requirements   # headless / CI
```

> Installed globally? Drop the `npx @protonspy/` prefix and just call `csdd`.

See the [full documentation](https://github.com/protonspy/csdd#readme).

## License

[Apache-2.0](https://github.com/protonspy/csdd/blob/main/LICENSE)
