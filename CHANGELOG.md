# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/). Pre-release versions use the
`vMAJOR.MINOR.PATCH-alpha.N` naming scheme.

## [Unreleased]

Nothing yet.

## [0.1.0-alpha.1] - Unreleased

This is the first public release of StudioForge. There is no prior public release and no earlier
version to diff against, so this entry describes the initial public alpha as a whole rather than a
set of incremental changes.

### Added

- Local Go daemon with an embedded, bilingual (English/Russian) SvelteKit browser UI; binds loopback
  only unless `--unsafe-host` is explicitly given, protected by a one-use bootstrap token exchanged
  for an HttpOnly, SameSite session cookie.
- Multi-project registry with safe/canonical project roots and symlink containment.
- SQLite storage (pure Go, no CGO) with embedded migrations, WAL mode, integrity checks, backups, and
  portable metadata export/import (metadata, agents, and tasks only; does not copy project source,
  and import requires an existing project root).
- Fair scheduler with global/per-project/per-provider/per-model concurrency limits, per-project writer
  leases, budget checks, pause/resume/cancel/restart, heartbeat leases, and interrupted-run recovery
  on startup.
- Persisted run events with a live Server-Sent Events stream (`/api/v1/events`).
- Claude Code provider: runs the real `claude` CLI in non-interactive mode, discovers supported flags
  by parsing its `--help` output, streams and classifies NDJSON events, and supports session resume
  and usage reporting.
- Codex CLI provider: runs `codex exec --json`, normalizes JSONL events, supports workspace
  sandboxing, saved CLI authentication, thread resume, and classified failures.
- Orchestrator delegation: when the chosen agent's role contains "orchestrator", other enabled agents
  are passed to Claude's native `--agents` flag as subagents.
- Official Roblox Studio MCP integration: discovers Roblox's own Studio MCP launcher, generates a
  per-run MCP configuration, applies a tool allowlist scoped to the agent's permission profile, speaks
  MCP JSON-RPC over stdio, and includes an `mcp-shim` subcommand so an agent keeps its tool list even
  when another client holds the launcher's single WebSocket-host slot. StudioForge does not
  reimplement Studio operations and ships no Roblox Studio plugin; it complements Roblox's official
  MCP tooling.
- Automatic git checkpoint before every non-plan Claude run, so the working tree can be reverted.
- Rojo build-and-open integration: compiles a place file and opens it in Roblox Studio.
- Static project context: `.agent/constitution.yaml` and `.agent/requirements.md`, read verbatim and
  prepended to the agent's system prompt (no indexing, embeddings, or code scanning).
- Diagnostics: `studioforge doctor`, an optional redacted diagnostic bundle (`--bundle`), and a
  `/api/v1/diagnostics` endpoint.
- Secret redaction applied to diagnostic bundles.
- Deterministic three-project mock demo (`--mock`) that runs without Claude, Roblox Studio, or Rojo.
- Packaging for Windows (amd64 zip) and macOS (arm64 `.app`); both are unsigned development builds.
- CLI surface: `studioforge [--port --host --data-dir --no-open --log-level --safe-mode --mock]`,
  `doctor`, `export`, `import`, `mcp-shim`, `--version`.

### Changed

- Raised the Go module's `go` directive from 1.25.0 to 1.25.12.
- `scripts/dev.sh` and `scripts/dev.ps1` no longer inject `--mock` unconditionally. They forward
  arguments verbatim, so `--mock` selects the demo and omitting it runs against real projects.
- Removed internal development artifacts from the repository ahead of the public release.
- Restructured the README for the public alpha.

### Fixed

- Safe mode no longer allows the `resume` and `restart` run actions to queue an AI worker. Only run
  creation was checked before, so safe mode could be bypassed through an existing run. Pause and
  cancel remain available because they only stop work in progress.
- The CI `gofmt` step now fails when files need formatting. `gofmt -l` exits 0 and only prints
  filenames, so the check could never fail.
- Fixed invalid YAML in the CI workflow and both issue-form templates, where `${{ ... }}` expressions
  and a `?` character sat unquoted inside flow mappings.

### Security

- Loopback-only binding by default (`--unsafe-host` is required to change it), a one-use bootstrap
  token exchanged for an HttpOnly, SameSite session cookie, Host/Origin validation on mutating
  requests, no CORS headers at all, and project roots canonicalized at registration.
- Secret redaction applied to diagnostic bundles. Application logs and stored run transcripts are not
  redacted; the path traversal and symlink-escape guard is implemented but currently has no caller.
- Raised the Go floor to 1.25.12. `govulncheck` reported 19 reachable Go standard-library
  vulnerabilities against Go 1.25.1 and 13 against Go 1.25.3; at 1.25.12 it reports none. There are
  no reachable third-party vulnerabilities.
- Release publishing now creates a draft, pre-release-flagged GitHub release rather than publishing
  immediately, and packaging can be run manually without publishing anything.

### Known limitations

Several implemented, unit-tested packages have no caller yet and are not reachable through the
running application: project memory, the task dependency DAG, git status/diff/rollback, asset
quarantine, and Rojo live-sync sessions (as opposed to Rojo build, which is wired). The Studio
Sessions view shows demo data rather than discovered Studio instances, and Decisions has no live
producer. Studio access is fail-closed to a single open Studio instance and is available to Claude
runs only, not Codex runs. Both platform packages are unsigned. See
[docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md) for the full list.
