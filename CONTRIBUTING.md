# Contributing to StudioForge

Thanks for considering a contribution. StudioForge is a public alpha maintained by a small number of people, so this guide is short on ceremony and specific about the things that actually cause problems.

## Before you start

Open an issue first for anything that changes behavior, the database schema, the HTTP API, or a provider contract. Small fixes — a bug, a doc correction, a test — can go straight to a pull request.

If you are proposing a feature, say which of these it is: wiring up one of the packages that exists but is not reachable (see the feature status table in the README), extending something that already works, or something new. The first category is the most useful right now.

## Setup

```sh
git clone https://github.com/10kkyvl/studioforge.git
cd studioforge
./scripts/dev.sh --mock --no-open      # or ./scripts/dev.ps1 on Windows
```

You need Go 1.25.12+, Node.js 22+, npm, and Git. Claude Code, Codex CLI, Roblox Studio, and Rojo are optional — the `--mock` demo and the test suite run without any of them.

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for the full command reference and directory layout.

## Running the checks

Run the full suite before opening a pull request:

```sh
./scripts/test.sh      # macOS/Linux
./scripts/test.ps1     # Windows
```

That runs `npm ci`, `npm run check`, `npm run lint`, `npm test`, `npm run build`, and the Playwright e2e suite in `web/`, then `gofmt -l`, `go vet ./...`, and `go test ./...`.

Two notes:

- `go test -race ./...` requires CGO and will not run on a Windows box with `CGO_ENABLED=0`. CI runs it on Linux.
- The opt-in smoke tests behind `STUDIOFORGE_REAL_CLAUDE=1` and `STUDIOFORGE_REAL_STUDIO=1` need a real, billable Claude account and an open Roblox Studio. They do not run in CI. If your change touches the Claude, Codex, Studio MCP, or Rojo adapters, run them locally and say so in the pull request.

## Coding style

- Go code is formatted with `gofmt` and must pass `go vet`. Frontend code is formatted with Prettier (`npm run format`) and type-checked with `svelte-check`.
- Keep domain packages independent of adapters. Business logic should not import HTTP, SQLite, Claude, Codex, Roblox, or Rojo specifics — those live behind interfaces at the edges.
- Add project-isolation and failure-path tests for every stateful feature. "It works when everything succeeds" is not enough; the interesting cases here are interrupted runs, lost leases, and refused Studio access.
- Update English and Russian user-facing strings together and run the i18n parity test.

## Things not to add

- **No credentials.** No API keys, tokens, cookies, `.env` files, or account identifiers, including in tests. Use obviously fake fixtures on reserved domains (`example.invalid`).
- **No proprietary Roblox assets** without explicit permission from the rights holder. Do not commit place files, models, images, or audio you do not own.
- **No bypass-permission defaults.** Do not widen a permission profile, auto-approve a tool that reaches outside the open place, or make Studio selection implicit.
- **No secret logging** and no removal of redaction from logs or diagnostic bundles.
- **No destructive rollback.** Git operations must stay non-destructive; do not add force-push or hard-reset paths.

## Adding a new Studio MCP tool to the allowlist

StudioForge does not implement Studio tools — it allowlists tools exposed by Roblox's official launcher. To add one:

1. Add it to the allowlist in `internal/roblox/mcp/config.go`, in the correct permission tier. Anything that reaches beyond the open place — network access, uploads, synthetic input — belongs in `danger-full-access`, not `workspace-write`.
2. Add a test asserting the tier, including a negative test proving a lower profile is refused.
3. Document the tool and its tier in `docs/SECURITY.md`.

## Adding a provider adapter

Follow the shape of `internal/providers/claudecode` and `internal/providers/codex`: discover capabilities at runtime rather than assuming flags exist, stream and classify events, support cancellation, and never widen permissions to make something work. Add a fake CLI under `testdata/fakes/` and test the stream, malformed-output, auth-failure, rate-limit, budget, crash, and resume paths against it.

## Commits and pull requests

Write focused commits that explain the user-visible reason for the change, not just the mechanics.

Pull request checklist:

- [ ] An issue exists for behavior, schema, API, or provider-contract changes.
- [ ] `./scripts/test.sh` or `./scripts/test.ps1` passes.
- [ ] New or changed behavior has tests, including a failure path.
- [ ] User-facing documentation is updated in the same pull request.
- [ ] EN and RU strings are updated together, if any were touched.
- [ ] The pull request states which verification was mocked, cross-compiled, or run against real hardware, Studio, or a Claude account.
- [ ] No credentials, proprietary assets, or permission-widening defaults.

If a change adds a capability, update the feature status table in the README and, if it moves something out of the "present but not reachable" list, remove it from there too. Documentation drift is the specific problem this project is trying to avoid.

## Code of conduct

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
