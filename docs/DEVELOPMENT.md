# Development guide

This document covers local setup, the build/test toolchain, and how to extend StudioForge. It describes
the repository as it exists today; see [ARCHITECTURE.md](ARCHITECTURE.md) for how the pieces fit
together and [CONTRIBUTING.md](../CONTRIBUTING.md) for the contribution workflow itself.

## Local setup

Requirements: Go 1.25.12+ and Node.js 22+. Node is a build-time dependency only — the production binary
embeds the compiled frontend and needs no Node.js at runtime.

```powershell
git clone https://github.com/10kkyvl/studioforge.git
cd studioforge
./scripts/dev.ps1 --no-open
```

```bash
git clone https://github.com/10kkyvl/studioforge.git
cd studioforge
./scripts/dev.sh --mock --no-open
```

Both scripts install frontend dependencies, build the SPA once, and then `go run` the daemon, forwarding
their arguments straight through. Pass `--mock` for the seeded three-project demo that needs no Claude,
Roblox Studio, or Rojo; omit it to run against real registered projects.

Earlier versions of these scripts injected `--mock` unconditionally, which made the flag meaningless and
left no documented way to run from source against a real project. Both scripts now forward arguments
verbatim.

## Dependency management

- **Go modules**: standard `go.mod`/`go.sum` at the repository root (module
  `github.com/10kkyvl/studioforge`, `go 1.25.12`). The only direct dependency is
  `modernc.org/sqlite` (pure Go, no CGO); everything else in `go.sum` is its transitive closure.
- **npm**: `web/package.json` plus a **committed** `web/package-lock.json`. Always install with
  `npm ci` (not `npm install`) so the lockfile is respected exactly, both locally and in CI.
- **The embedded frontend is committed**: `internal/webui/dist` (the SvelteKit static-adapter output,
  post-processed to externalize its bootstrap script into `bootstrap.js` so the daemon can serve a strict
  `script-src 'self'` CSP) is checked into the repository. This is what lets `go build ./cmd/studioforge`
  produce a working binary without Node.js installed at all.
- **Any frontend change requires a rebuild, and the rebuild must be committed.** After editing anything
  under `web/`, run:

  ```powershell
  cd web
  npm ci
  npm run build
  cd ..
  git add internal/webui/dist
  ```

  The build is **byte-reproducible**: running `npm run build` again with no source changes regenerates
  `internal/webui/dist` byte-for-byte identical to what is already committed. If your rebuild produces a
  diff you did not expect, that is a signal something is non-deterministic (or your source change is
  larger than you think) — not something to work around by hand-editing `dist`.

## Command table

| Task | Command |
| --- | --- |
| Run from source (mock mode, no external tools needed) | `./scripts/dev.ps1 --mock --no-open` / `./scripts/dev.sh --mock --no-open` |
| Run from source against real projects | `./scripts/dev.ps1 --no-open` / `./scripts/dev.sh --no-open` |
| Full verification (frontend + Go, matches CI) | `./scripts/test.ps1` / `./scripts/test.sh` |
| Cross-build both release targets | `./scripts/build.ps1` / `./scripts/build.sh` |
| Build release archives + `SHA256SUMS.txt` | `./scripts/package.ps1` / `./scripts/package.sh` |
| Go unit/integration tests only | `go test ./...` |
| Go vet | `go vet ./...` |
| Go format check | `gofmt -l cmd internal testdata` |
| Frontend type check | `cd web && npm run check` |
| Frontend lint (check only) | `cd web && npm run lint` |
| Frontend format (write) | `cd web && npm run format` |
| Frontend unit tests (vitest) | `cd web && npm test` |
| Frontend production build | `cd web && npm run build` |
| Frontend E2E (Playwright) | `cd web && npm run test:e2e` |

`scripts/test.ps1`/`test.sh` run, in order: `npm ci`, `npm run check`, `npm run lint`, `npm test`,
`npm run build`, `npm run test:e2e` in `web/`, then `gofmt -l`, `go vet ./...`, and `go test ./...` from
the repository root. CI (`ci.yml`) runs this same core sequence, plus two things the local scripts do
not: `npm audit --audit-level=high` in the frontend job, and a separate Linux-only `go test -race ./...`
job — see Testing below.

`scripts/build.ps1`/`build.sh` build the frontend once, then cross-compile
`windows-amd64/studioforge.exe` and `darwin-arm64/studioforge` with `CGO_ENABLED=0` and `-ldflags` that
inject `Version`/`Commit`/`BuildDate` from `git describe`/`git rev-parse` into `internal/config`.
`scripts/package.ps1`/`package.sh` call `build` and then stage a Windows zip and an (unsigned) macOS
`.app` bundle into `artifacts/`, alongside a `SHA256SUMS.txt`.

## Linting and formatting

- **Go**: `gofmt -l cmd internal testdata` must report no files; `go vet ./...` must pass clean. Neither
  is optional — both run in CI (`ci.yml`, the `go` job) against Go `1.25.x` and `1.26.x` on both
  `ubuntu-latest` and `windows-latest`.
- **Frontend**: `prettier --check .` (via `npm run lint`) for formatting, `npm run format` to apply
  fixes, and `svelte-kit sync && svelte-check --tsconfig ./tsconfig.json` (via `npm run check`) for
  TypeScript/Svelte type checking.

## Testing

- **Go**: 44 test files across the packages listed in the repository layout below. `internal/diagnostics`,
  `internal/webui`, `internal/models`, and `internal/migrations` currently have **no test files** — they
  are exercised only indirectly, through the tests of packages that call them (e.g.
  `internal/database`'s tests exercise the embedded migrations).
- **`go test -race ./...` requires CGO and does not run on this Windows dev box** (there is no configured
  C toolchain in this environment). It is not part of `scripts/test.ps1`. CI runs it on `ubuntu-latest`
  in a dedicated `race` job (`ci.yml`), so the race detector is always exercised before merge even though
  it cannot be run locally on Windows. If you have a Linux machine or WSL with a C toolchain available,
  you can run it there directly.
- **Frontend**: `vitest run` (`npm test`) covers, among other things, i18n catalog parity between the `en`
  and `ru` string tables (`web/src/lib/i18n.test.ts`) and the typed API error client
  (`web/src/lib/api.test.ts`). `playwright test` (`npm run test:e2e`, `web/e2e/studioforge.spec.ts`)
  builds a real Go binary, performs a secure first-run bootstrap exchange, changes locale, starts a run,
  opens the Tasks DAG view, and visits the core screens with console-error assertions.
- **Opt-in real smoke tests**: most of the suite runs entirely against fakes and never touches a real
  external tool. Two environment variables gate the tests that do:
  - `STUDIOFORGE_REAL_CLAUDE=1` — enables `TestRealClaudeSmoke`
    (`internal/providers/claudecode/real_smoke_test.go`) and the Claude-only live tests in
    `internal/app/live_studio_smoke_test.go` (e.g. `TestLiveClaudeAcceptsAgents`). These are billable:
    they run a real, authenticated `claude` CLI turn.
  - `STUDIOFORGE_REAL_STUDIO=1` (together with `STUDIOFORGE_REAL_CLAUDE=1`) — enables
    `TestLiveClaudeReachesStudio` and `TestLiveClaudePlanModeReachesStudio`
    (`internal/app/live_studio_smoke_test.go`), and the Roblox Studio MCP live handshake test
    (`internal/roblox/mcp/real_smoke_test.go`, run with `go test ./internal/roblox/mcp -run
    TestRealStudioMCP -v -count=1`). These need Roblox Studio open with an MCP-enabled place.

  Without these variables set, `go test ./...` (including in CI) never exercises a real `claude`,
  `codex`, Roblox Studio, or MCP launcher — everything is driven by the four fake CLIs below.
- **Fake CLIs** (`testdata/fakes/`): `fakeclaude`, `fakecodex`, `fakerojo`, and `fakestudiomcp` are small
  Go programs, each built to a temporary directory by the tests that need them, standing in for the real
  `claude`, `codex`, `rojo`, and Roblox Studio MCP launcher binaries. They let the provider adapters,
  scheduler, and MCP transport be tested deterministically (including stream/malformed-output/auth/
  rate-limit/budget/crash/resume paths) without any real account or install.

## Debugging

- `--log-level debug` on the daemon command raises the `slog` level to debug for startup, scheduler, and
  provider-adapter logging.
- `studioforge doctor` (optionally with `--bundle <path>.zip`) runs the same dependency/database checks
  the UI's diagnostics panel uses and prints a JSON report; `--bundle` also writes a redacted diagnostic
  zip (`security.Redact` is applied to its contents, and project source, environment variables, and
  prompts are explicitly excluded).
- `--mock` runs the deterministic three-project demo with no external tools required — the fastest way to
  reproduce a UI issue without Claude, Codex, Rojo, or Studio installed.
- `--safe-mode` disables run creation entirely (`POST /api/v1/runs` returns 409) and forces scheduler
  concurrency limits to 1, which is useful for isolating whether a problem is in the daemon/UI or in an
  active run.

## Directory structure

```
cmd/studioforge/            CLI entry point and subcommands (doctor, export, import, mcp-shim)
internal/
  api/                      HTTP handlers, security middleware, SSE, static SPA serving
  app/                      daemon startup/shutdown wiring
  config/                   Options, version metadata, loopback host check
  database/                 SQLite connection, migrations application, Store query layer
  diagnostics/              Doctor: dependency checks, diagnostic bundle export
  events/                   Hub: persisted-then-published run event fan-out
  gitcheckpoint/            auto-commit before non-plan Claude runs (wired)
  gitops/                   status/diff/rollback/tag (not wired to an endpoint)
  memory/                   FTS5-backed store (no live caller)
  migrations/               embedded ordered .sql migration files
  models/                   shared DTOs
  platform/                 data dir, single-instance lock, browser launch, secret store, toolpath
  portable/                 export/import archive format
  processes/                subprocess supervisor, minimal-environment allowlist
  projects/                 path guard, fingerprint, scaffold, static context loader
  prompts/                  structured prompt assembly template (no live caller)
  providers/                Provider interface + claudecode, codex, mock adapters
  resources/                atomic resource lease manager
  roblox/
    assets/                 quarantine transition validator (no caller)
    mcp/                    launcher discovery, stdio transport, client, provisioner, shim
    studio/                 place builder/launcher, instance/binding tracker (tracker unwired)
  rojo/                     build/plugin-install (wired) + serve session lifecycle (unwired)
  scheduler/                fair queue, run state machine, concurrency/budget enforcement
  security/                 secret redaction
  tasks/                    task DAG validator (no live caller)
  webui/                    //go:embed of the built SvelteKit dist
web/                        SvelteKit 5 SPA source
  e2e/                      Playwright end-to-end tests
  src/lib/                  shared TypeScript: i18n catalogs, typed API client, view components
scripts/                    dev/build/test/package, .sh and .ps1 pairs
testdata/fakes/             fakeclaude, fakecodex, fakerojo, fakestudiomcp
migrations/                 (embedded via internal/migrations)
docs/                       this document, ARCHITECTURE.md, ADRs, API/DATABASE/TESTING/KNOWN_LIMITATIONS
.github/workflows/          ci.yml, release.yml
```

## Adding a new provider adapter

1. Create `internal/providers/<name>/<name>.go` implementing `providers.Provider`: `Diagnose(ctx)
   providers.Diagnostics`, `Start(ctx, providers.RunRequest) (providers.RunHandle, error)`,
   `Resume(ctx, providers.ResumeRequest) (providers.RunHandle, error)`, and `Cancel(ctx, runID) error`.
   Model it on `internal/providers/codex/codex.go` for a simpler starting point (no capability-gated flag
   building, unlike `claudecode`).
2. Discover capabilities rather than assuming a version means a feature exists — see ADR 0002. The
   `claudecode` adapter's `parseCapabilities` (parsing `claude --help` output) is the pattern to follow if
   your CLI's flags evolve independently of its version string.
3. Keep the package independent of HTTP, SQLite, and the other adapters — it should depend only on
   `internal/providers` (for the shared types) and `internal/processes` (for `MinimalEnvironment` and, if
   needed, `Supervisor`).
4. Register an instance of the new adapter in the `adapters` map built in `internal/app.Run`, alongside
   `mock`, `claude`, and `codex`.
5. Add the new provider name to the validation lists in `internal/api/api.go`: `normalizeAgent`'s
   provider check, and the `settings` handler's `default_provider` validation.
6. Add a fake CLI under `testdata/fakes/<name>/` for deterministic tests, following the existing
   `fakeclaude`/`fakecodex` pattern, and add unit tests for argument building and event normalization.

## Adding or allowlisting a new Studio MCP tool

1. Add the tool's name to the correct tier in `internal/roblox/mcp/config.go`: `readOnlyTools` (pure
   observation), `workspaceTools` (changes the open place), or `reachingTools` (reaches past the place —
   Marketplace uploads, arbitrary HTTP, synthetic desktop input).
2. Add it to `OfficialTools` in the same file, so `shim.fallbackTools` also advertises it when no live
   schema is available from the launcher.
3. There is no other registration step — the shim and the provisioner both derive their behavior from
   these two lists, and the allowlist (not server registration) is what actually grants a non-interactive
   Claude run permission to call the tool.

## i18n parity requirement

`web/src/lib/i18n.ts` defines two flat string catalogs, `en` and `ru`, as plain TypeScript objects.
`web/src/lib/i18n.test.ts` asserts that both catalogs have exactly the same set of keys and that no value
is empty. In practice this means: **when you add, rename, or remove a user-facing string, update both
`en` and `ru` in the same change**, and run `npm test` before committing — a key present in one catalog
and missing from the other fails CI.

## Contribution workflow

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full checklist. In summary: discuss large behavior or
schema changes in an issue first; keep domain packages independent of the HTTP/SQLite/Claude/Roblox/Rojo
adapters; add project-isolation and failure-path tests for any new stateful feature; run
`./scripts/test.ps1` (or `.sh`) before opening a pull request; update English and Russian strings
together; and do not add bypass-permission defaults, secret logging, destructive rollback behavior, or
implicit Studio instance selection.
