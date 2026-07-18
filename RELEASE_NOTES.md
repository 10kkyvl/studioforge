# StudioForge v0.1.0-alpha.1 — release notes

This is the first public release of StudioForge. There is no prior public release and no earlier
version to compare against.

## What this is

StudioForge is an open-source development workflow that helps Claude Code build, understand, test,
and maintain Roblox projects.

It does not reimplement Roblox Studio operations, and this repository contains no Roblox Studio
plugin. Instead, it detects and launches Roblox's own official Studio MCP launcher
(`%LOCALAPPDATA%\Roblox\mcp.bat` on Windows, `RobloxStudio.app/Contents/MacOS/StudioMCP` on macOS)
and speaks real MCP JSON-RPC over stdio to it. StudioForge adds a project-level layer around that
official tooling: per-run MCP configuration generation, a tool allowlist scoped by the agent's
permission profile, and an MCP stdio shim so an agent keeps its tool list even when another client
holds the launcher's single WebSocket-host slot. It is a complement to the official Roblox MCP
tooling, not a replacement for it.

## What actually works today

- A local Go daemon with an embedded English/Russian browser UI, loopback-only by default, protected
  by a one-use bootstrap token and an HttpOnly session cookie.
- A multi-project registry with safe/canonical project roots and symlink containment.
- SQLite storage (pure Go, no CGO) with migrations, WAL mode, integrity checks, backups, and portable
  metadata export/import (metadata, agents, and tasks only — not project source).
- A fair scheduler: concurrency limits, per-project writer leases, budget checks, pause/resume/
  cancel/restart, heartbeat leases, and interrupted-run recovery.
- Persisted run events with a live SSE stream.
- A Claude Code provider that runs the real `claude` CLI, discovers its supported flags, streams and
  classifies NDJSON events, and supports session resume and usage reporting.
- A Codex CLI provider that runs `codex exec --json` with workspace sandboxing, saved authentication,
  thread resume, and classified failures.
- Orchestrator delegation to other enabled agents through Claude's native `--agents` flag.
- Roblox Studio MCP handed to Claude runs, fail-closed to exactly one open Studio instance (see
  Known limitations for why).
- An automatic git checkpoint before every non-plan Claude run.
- Rojo build-and-open: compiling a place file and opening it in Studio.
- Static project context: `.agent/constitution.yaml` and `.agent/requirements.md`, read verbatim into
  the system prompt.
- Diagnostics (`studioforge doctor`, a redacted diagnostic bundle) and secret redaction in logs and
  bundles.
- A deterministic three-project mock demo (`--mock`) that needs no Claude, Studio, or Rojo.
- Packaged Windows (amd64) and macOS (arm64) builds.

## What's present in code but not yet reachable

Being upfront about this: several packages are implemented and unit-tested, but nothing in the
running application calls them yet, so a user cannot reach them through the UI or API today:

- Project memory (`internal/memory`) — a working SQLite FTS5-backed store exists, but no run reads or
  writes it.
- The task dependency DAG (`internal/tasks/dag.go`) — cycle detection is implemented, but the task
  creation API accepts no `dependencies` field.
- Git status/diff/rollback (`internal/gitops`) — implemented and tested, but no API endpoint exposes
  it. (This is separate from the automatic per-run git checkpoint, which is wired and does run.)
- Asset quarantine (`internal/roblox/assets`) — a status-transition validator with no caller; the
  Assets view in the UI is a bare empty state.
- Rojo live-sync sessions — the session manager can start/stop/track a `rojo serve` session, but no
  HTTP endpoint calls it. (Rojo build-and-open, listed above, is the part that is wired.)
- Decisions — the resolution endpoint and view exist, but no live run creates a decision; only the
  mock demo seeds any.
- Real Roblox Studio instance discovery — the Studio Sessions view currently shows demo data, not
  discovered instances.

See [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md) and
[docs/ROADMAP.md](docs/ROADMAP.md) for the plan around these.

## Installing

### From source (Windows and macOS)

Requirements: Go 1.25.3 or newer, Node.js 22 or newer, npm, and Git.

```powershell
git clone https://github.com/10kkyvl/studioforge.git
Set-Location studioforge
./scripts/dev.ps1 --no-open
```

```sh
git clone https://github.com/10kkyvl/studioforge.git
cd studioforge
./scripts/dev.sh --no-open
```

The command prints a local URL and a one-use bootstrap token. Omit `--no-open` to open the
authenticated local page automatically.

### Packaged binary (Windows and macOS)

Build and package from source:

```powershell
./scripts/package.ps1
Expand-Archive ./artifacts/StudioForge-<version>-windows-amd64.zip ./StudioForge
./StudioForge/studioforge.exe --mock
```

On macOS, extract `StudioForge-<version>-macos-arm64.zip` and open `StudioForge.app`. The build is
unsigned, so Finder requires a one-time Control-click → Open; do not disable Gatekeeper globally to
work around this.

Pre-built archives will also be attached to the `v0.1.0-alpha.1` GitHub release once it is tagged.

## Who this is for

Independent Roblox developers and small teams who are already using Claude Code and want
project-level structure around it: multi-project orchestration, task tracking, budget and
concurrency control, and scoped access to Roblox Studio through the official MCP launcher, instead of
ad hoc scripts and a single long-lived terminal session.

## Who this is not for yet

- Anyone who needs signed or notarized installers today — both packages are unsigned.
- Anyone who needs Codex agents to reach Roblox Studio — Studio access is Claude-only right now.
- Anyone who needs multiple simultaneous writers per project, remote or multi-user access, or a
  hosted service — none of that exists.
- Anyone relying on screenshot-driven iteration, automated playtest validation, or persistent
  cross-session project memory — none of that is wired up yet, even where some backing code exists
  (see above).

## Known limitations

The full list is in [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md). The largest ones:
Studio access is granted only when exactly one Studio instance is open, and only to Claude runs; a
Claude run inherits the operator's own Claude Code configuration (`CLAUDE.md`, hooks, plugins,
skills), which is billed to every run; the portable project archive copies metadata/agents/tasks but
not source, and import needs an existing project root; and detailed run-event retention has no
automatic pruning yet.

## This is an alpha

APIs, configuration file formats, installation steps, and internal architecture may change between
alpha releases, and there is no migration path guaranteed between them. Do not build long-term
automation on top of anything described here without expecting it to change in a later alpha.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security note

StudioForge binds to loopback only unless `--unsafe-host` is explicitly given, and it adds no remote
authentication of its own — do not expose it to an untrusted network. Both packaged binaries are
unsigned development builds. Diagnostic bundles produced by `studioforge doctor --bundle` are
redacted, but review the contents of any bundle before sharing it with anyone.

## Verification performed before this release

These commands were actually run against this codebase, with the results noted:

- `gofmt -l cmd internal testdata` — clean.
- `go vet ./...` — pass.
- `go test ./...` — pass, all packages ok (44 test files).
- `cd web && npm ci` — 0 vulnerabilities.
- `npm run check` — 4079 files, 0 errors, 0 warnings.
- `npm run lint` (prettier --check) — pass.
- `npm test` (vitest) — 2 files, 12 tests passed.
- `npm run build` — pass; the rebuilt `internal/webui/dist` was byte-identical to the committed copy.
- `npm run test:e2e` (Playwright) — 1 test passed.
- `go build ./...` — pass on Go 1.25.12.
- `govulncheck ./...` — reported 19 reachable Go standard-library vulnerabilities on Go 1.25.1 and 13
  on Go 1.25.3. The `go.mod` `go` directive was raised from 1.25.0 to 1.25.12, after which
  `govulncheck` reports **no vulnerabilities found**. There are no reachable third-party
  vulnerabilities.
- Daemon smoke test — the built binary starts in both `--mock` and real mode, serves the embedded SPA,
  returns `{"status":"ok"}` from `/api/v1/health`, returns 401 for an unauthenticated
  `/api/v1/snapshot`, and refuses a non-loopback `--host` without `--unsafe-host`.
- `studioforge doctor` — ran and correctly reported the tooling detected on the development machine.
- `go test -race ./...` — could **not** be run on the Windows development machine, since it requires
  CGO. It runs instead in CI on `ubuntu-latest` (the `race` job in `.github/workflows/ci.yml`).
