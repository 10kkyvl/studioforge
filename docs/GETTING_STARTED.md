# Getting started with StudioForge

StudioForge is a public **beta** (currently preparing the v0.5.0-rc.1 release candidate). This
guide gets you from a clean checkout (or a downloaded package) to a running daemon, a registered
project, and your first agent run.

After setup you get a single local Go daemon with an embedded browser UI. It manages one or more
Roblox project registrations, agent definitions backed by Claude Code or OpenRouter, a SQLite-backed
run history and event log, a fair per-project scheduler, and optional access to Roblox Studio through
Roblox's own official Studio MCP launcher. Several features described in the codebase are not yet
reachable from the UI or API — see [Known Limitations](KNOWN_LIMITATIONS.md) before you plan work
around them.

---

## Prerequisites

| Tool | Version | Needed to build from source | Needed to run a packaged binary |
| --- | --- | --- | --- |
| Go | 1.25.12 or newer | Yes | No |
| Node.js | 22 or newer | Yes | No |
| npm | (bundled with Node.js) | Yes | No |
| Git | any recent version | Yes (clone the repo; also used for checkpoints) | Optional (only if you want Git checkpoints for your project) |

A packaged binary (the Windows zip or the macOS `.app`) embeds the built frontend and needs neither Go
nor Node.js at runtime. `go build` also works directly from a clean checkout without Node.js, because
the built frontend (`internal/webui/dist`) is committed to the repository; you only need Node.js if
you are changing the frontend and want to rebuild it.

---

## Supported operating systems

- **Windows amd64** — packaged target, fully supported.
- **macOS arm64 (Apple Silicon)** — packaged target, fully supported.
- **Linux** — builds and runs from source (the daemon itself is CGO-free Go plus embedded SQLite), but
  is not part of the packaging pipeline and is not Studio-capable: Roblox Studio does not run on Linux,
  so there is no Studio MCP launcher to detect there. Everything else (daemon, UI, OpenRouter provider,
  database, Git checkpoints) works.

---

## Installation

### (a) From source

PowerShell (Windows):

```powershell
git clone https://github.com/10kkyvl/studioforge.git
Set-Location studioforge
./scripts/dev.ps1 --no-open
```

macOS / Linux shell:

```sh
git clone https://github.com/10kkyvl/studioforge.git
cd studioforge
./scripts/dev.sh --no-open
```

Both scripts check that `go`, `node`, and `npm` are on `PATH`, run `npm ci && npm run build` in `web/`,
and then run `go run ./cmd/studioforge` with any extra arguments you passed appended — the scripts pass
your flags straight through and do not force `--mock`. Add `--mock` yourself (e.g.
`./scripts/dev.ps1 --no-open --mock`) if you want the seeded demo (see below); omit it to run the
daemon from source against your own project.

### (b) From a release package

Building a package cross-compiles both packaged targets (Windows amd64 and macOS arm64) regardless of
which OS you build on, since `CGO_ENABLED=0` and Go's cross-compilation are used.

PowerShell (Windows):

```powershell
./scripts/package.ps1
Expand-Archive ./artifacts/StudioForge-<version>-windows-amd64.zip ./StudioForge
./StudioForge/studioforge.exe --mock
```

macOS shell:

```sh
./scripts/package.sh
cd artifacts
unzip StudioForge-<version>-macos-arm64.zip
open StudioForge.app
```

Both packages are **unsigned development builds**. On Windows, SmartScreen may warn; verify the archive
against `SHA256SUMS.txt` before choosing "More info -> Run anyway". On macOS, Gatekeeper requires a
one-time Control-click -> Open the first time you launch `StudioForge.app`; never disable Gatekeeper
globally to work around this.

---

## First launch

On startup, the daemon prints two lines to stdout:

```text
STUDIOFORGE_URL=http://127.0.0.1:PORT
STUDIOFORGE_BOOTSTRAP=<one-use-token>
```

`STUDIOFORGE_URL` is the loopback address the daemon bound (a free port is chosen automatically unless
you pass `--port`). The bootstrap token is a cryptographically random, **one-use** value: the browser
tab opened for you exchanges it for an `HttpOnly`, `SameSite=Strict` session cookie good for 24 hours.
If you copy the URL fragment into a second tab or reload after the token has been consumed, the
exchange fails with 401 — start the daemon again (or use `--no-open` and use the printed URL from the
same run once).

Pass `--no-open` to suppress the automatic browser launch (useful in scripts/CI or when you want to
open the URL yourself).

Runtime data — the SQLite database, backups, exports, logs, generated MCP configs, and the instance
lock file — is stored in the OS user configuration directory under a `StudioForge` folder (for
example `%AppData%\StudioForge` on Windows, `~/Library/Application Support/StudioForge` on macOS)
unless you override it with `--data-dir PATH`. Run `studioforge doctor` at any time to see the
effective data directory.

---

## Trying it with zero external dependencies

```sh
studioforge --mock
```

`--mock` seeds and runs a deterministic three-project demo (a mobile obby, a tycoon, and an arena
prototype) that exercises the real domain model and API without needing Claude Code, an OpenRouter
API key, Roblox Studio, or Rojo installed. Each demo project gets its own on-disk workspace under
`<data-dir>/demo-projects/<id>/` with a `default.project.json` and a `.agent/` folder, three preset
agents (orchestrator/engineer/QA), a budget, and a completed sample run with a usage record.

**Be explicit with yourself about what is fake here.** The Studio Sessions rows shown for the demo
projects, and the task dependency links between the demo tasks, are seeded rows inserted once by
`--mock` — they are not produced by a live Studio connection. Task dependencies are a real, live
feature now (create a task with a `dependencies` field and it is persisted, cycle-checked, and enforced
before a run may start — see [Known Limitations](KNOWN_LIMITATIONS.md) for the resume/restart nuance),
so on a real project you can create your own instead of only seeing the demo ones. Outside of `--mock`,
nothing in the product currently creates Studio session rows from a live run.

`--safe-mode` is the complementary flag: it disables AI provider workers, MCP, and Rojo while keeping
data, backups, exports, and diagnostics available — useful for inspecting or maintaining a data
directory without launching any external process.

---

## Configuration

Open **Settings** in the UI to override the executable paths StudioForge uses for Claude Code, Rojo,
Git, and the Studio MCP launcher (`studio_mcp_path`), plus the default provider/model/effort and
global concurrency. Changes apply immediately, without restarting the daemon — the underlying
providers pick up the new executable path on the next run. Changing the **default model** also
re-points every existing project's agents to the new model (agents already on a different model are
left alone); the default provider and effort still apply only to newly created projects. Leaving a field blank falls back to
resolving the tool from `PATH` (or, for the Studio MCP launcher, the platform-specific default
location). OpenRouter has no executable path — it is an HTTP API — and is configured with an API key
in **Settings** instead (see [Connecting OpenRouter](#connecting-openrouter) below).

Run `studioforge doctor` to see what StudioForge actually detected:

```sh
studioforge doctor
```

This reports, as JSON, the StudioForge version/commit/build date, OS/arch, data directory, database
integrity and WAL status, FTS5 availability, safe/mock mode, and a per-dependency check (`git`,
`claude`, `rojo`, `studioMcp`, `openrouter`) with detected path, version string, and a status of `ok`,
`warning` (found but not authenticated), `error`, or `missing`, plus a `dataDirectory` writability
check. The `openrouter` check reports the API key's verification state and whether the model catalog
is reachable, since there is no executable or version string for an HTTP provider. The same report
backs the in-app Settings integration cards.

---

## Connecting Claude Code

StudioForge does not implement its own Claude client: it execs the real `claude` binary you already
have installed, in non-interactive print mode, and parses `claude --help` at runtime to discover which
flags your installed version supports. Authentication is entirely owned by Claude Code — StudioForge
never reads or stores an Anthropic token. Verify your own install independently:

```sh
claude --version
claude auth status
```

Because StudioForge execs your `claude` binary, **a run inherits the operator's own Claude Code
configuration** — `CLAUDE.md`, hooks, plugins, and skills from your local install — and every run is
billed to your Claude account like any other Claude Code invocation. `--strict-mcp-config` is emitted
to isolate a run from your other configured MCP servers, but only when the run was also granted Studio
access (since it rides alongside `--mcp-config`); a run without Studio access still inherits your other
MCP servers. There is currently no way to fully isolate a run from your local Claude Code configuration
without `--bare`, which in turn requires `ANTHROPIC_API_KEY` and cannot use OAuth/subscription
authentication.

If Windows PATH resolves an inaccessible executable, set an explicit path in **Settings**.

---

## Connecting OpenRouter

OpenRouter is a different kind of provider from Claude Code: it is not a local CLI, it is an HTTP
API, and StudioForge drives it with its own in-process agent loop rather than execing a subprocess.
You need an OpenRouter account and an API key — **an API key is required even to run free models**.

Set the key one of two ways:

- Environment variable: `OPENROUTER_API_KEY`.
- **Settings -> Agents and integrations -> OpenRouter**: paste the key and click **Test connection**.

Either way, the key is written to the OS secure credential store (Windows Credential Manager on
Windows, Keychain on macOS) — never to SQLite, run events, application logs, or the diagnostic
bundle. If the secure store is unavailable, StudioForge falls back to holding the key in memory for
the current session only (not persisted); the environment variable is checked last. `studioforge
doctor` and the Settings integration card both report the key's state (`not_configured` /
`unverified` / `configured` / `invalid`) and its source (keychain / session / env), never the key
itself.

The model picker is populated from OpenRouter's public Models API, cached for 6 hours with a manual
refresh action, falling back to the last successfully fetched copy and then to a bundled snapshot if
OpenRouter's API cannot be reached at all. Only models that support tool calling are offered, since
StudioForge's agent loop is built around tool use and not every OpenRouter model supports it. A
curated shortlist (Free automatic, Free recommended, Best coding, Balanced, Fast and cheap, Strong
reasoning, Large context) sits above the full catalog; **Free automatic** (`openrouter/free`)
auto-picks a capable free model for you. Free models are worth treating differently from paid ones:
quality, latency, and rate limits vary more, and availability can change without notice, so they suit
small tasks better than long unattended runs — StudioForge will never silently switch a free-mode run
to a paid model to compensate.

Image attachments are sent as real image data (not just referenced by path) to vision-capable models;
attaching an image to a model without vision support fails the run with a clear error rather than
silently dropping the image.

---

## Roblox Studio setup

StudioForge does **not** ship a Roblox Studio plugin — there is nothing to install from this repository
into Studio. It uses Roblox's own official Studio MCP launcher:

- Windows: `%LOCALAPPDATA%\Roblox\mcp.bat`
- macOS: `/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP`

In Roblox Studio, update to a current version, open **Assistant -> ... -> Manage MCP Servers**, and
enable **Studio as MCP server**. `studioforge doctor` reports whether the launcher was found.

**The fail-closed single-instance rule:** a run is only granted Studio access when exactly one open
Studio instance holds that project's place (or, with only one Studio instance open at all and no place
match configured, that single instance). This is not an arbitrary restriction — it reflects a real
constraint: Claude Code runs its own MCP client, `set_active_studio` is state on a single connection,
and StudioForge cannot pin an instance onto the agent's own connection from outside; the launcher also
accepts no instance-selection argument. With several Studio instances open, StudioForge refuses access
rather than guessing which one the agent should use, and the run simply continues without Studio.

Studio access is granted to **Claude and OpenRouter runs**. Claude reaches the launcher through a
generated `--mcp-config`; OpenRouter's in-process agent loop opens its own live per-run MCP client to
the same launcher instead. Both are subject to the identical fail-closed single-instance rule and the
same permission-profile tool allowlist.

Studio tools are auto-approved by the agent's permission profile: `read-only` gets observation tools
only; `workspace-write` adds tools that change the open place; tools that reach past the open place
(`upload_image`, `store_image`, `http_get`, `user_keyboard_input`, `user_mouse_input`) require
`danger-full-access`.

---

## Rojo

Install the Rojo 7 CLI from the official Rojo documentation and verify it:

```sh
rojo --version
```

What actually works today: StudioForge builds a project's `default.project.json` into a place file
under `<project>/.studioforge/<name>.rbxl` and opens it in Roblox Studio (this is what happens when you
open a project or when Studio access needs a specific place opened automatically). Live-sync is also
wired end to end: `POST`/`DELETE /api/v1/projects/{id}/sync` start and stop a `rojo serve` session, and
the chat view can start/stop it with status, port, and recent logs shown in the Overview view. It
requires a `default.project.json` at the project root, and it only carries files into an already-open
Studio — the Rojo Studio plugin still has to be told to Connect once per session from inside Studio;
StudioForge cannot press that button for you.

---

## Your first workflow

1. **Create a project.** In the UI, register an existing directory or create a new one; StudioForge
   stores its canonical path and a fingerprint (it does not copy your source into application data).
2. **Confirm it in doctor.** Run `studioforge doctor` and check that the database, Git, and any
   providers you plan to use report `ok` (or `warning` for "found but not authenticated").
3. **Create or verify an agent.** New projects automatically get a default agent; open the Team builder
   to inspect it, or create additional agents with a specific provider, model alias, permission profile,
   and budget.
4. **Send a chat message.** Sending a message in a project thread starts a run (`POST /api/v1/runs`)
   for the selected agent.
5. **Watch events.** The UI streams live run events over Server-Sent Events (`GET /api/v1/events`);
   events are persisted before being published, so nothing is lost if you reload.
6. **Note the automatic checkpoint.** For Claude-provider runs that are not in `plan` mode, StudioForge
   auto-commits the project's working tree (`git add -A` + a commit authored as `StudioForge`) before
   the run starts, so you can revert an agent's changes with ordinary Git. This is best-effort: a
   project that is not a Git repository, or has nothing to commit, is a silent no-op, and a checkpoint
   failure never blocks the run. OpenRouter and mock-provider runs are not checkpointed.

---

## Optional project context

If a project's root contains `.agent/constitution.yaml` and/or `.agent/requirements.md`, StudioForge
reads them **verbatim** (no parsing, no templating) and prepends them to the system prompt of every run
in that project. These two exact filenames are the only project context files StudioForge reads —
nothing else in `.agent/` (or elsewhere in the project) is scanned, indexed, or embedded.

Example `.agent/constitution.yaml`:

```yaml
architecture:
  server_authoritative: true
  unrelated_refactors: forbidden
safety:
  production_publish_requires_confirmation: true
```

Example `.agent/requirements.md`:

```markdown
# Requirements

Exercise concurrent scheduling, review, and playtest flows.
```

Adapt the content freely — these are illustrative, not a schema StudioForge validates.

---

## Uninstall / cleanup

1. Run `studioforge doctor` first to see the exact data directory path (`dataPath` in the JSON output).
2. Stop the daemon, then delete that data directory. This removes the SQLite database, backups,
   exports, logs, generated per-run MCP configs, and (if you used `--mock`) the seeded demo project
   workspaces.
3. Delete the extracted package directory (`StudioForge-windows-amd64/` or `StudioForge.app`) if you
   installed from a release archive.
4. Git checkpoint commits are **not** removed by any of the above — they are ordinary commits made
   directly in your own project's Git history, and remain there like any other commit you would revert
   or leave in place with normal Git commands.

---

## Common mistakes

- **Several Roblox Studio instances open at once.** Access is refused by design, not by bug — see the
  fail-closed rule above. Close down to a single relevant instance, or check
  [Troubleshooting](TROUBLESHOOTING.md) for how to confirm which instance holds which project's place.
- **Expecting a run saved under the old Codex provider to restart or resume.** The Codex CLI provider
  is removed; those runs remain visible in history with a **Legacy provider** badge, but Restart and
  Resume return a controlled error instead of relaunching a CLI that no longer runs.
- **Expecting Resume to re-check a task's dependencies.** `POST /api/v1/runs` and a user-initiated
  Restart both refuse (409 `task_dependencies_incomplete`) unless every dependency of the attached task
  has reached `completed`, but Resume and an automatic playtest-correction run skip that check, since
  both continue a lineage that already started rather than beginning fresh — see
  [Known Limitations](KNOWN_LIMITATIONS.md).
- **Expecting rollback to work without a stored checkpoint, or while a run is active.**
  `POST /api/v1/runs/{id}/rollback` is wired and has a confirm dialog in the chat view, but it needs a
  checkpoint recorded for that run and is refused while a run holds the project's write lease.
- **Binding to a non-loopback host without understanding `--unsafe-host`.** StudioForge adds no remote
  authentication of its own; binding beyond loopback is an explicit escape hatch, not a hardening
  feature, and should not be exposed to an untrusted network.
- **Assuming `--max-turns` bounds a run.** The flag does not exist in current Claude Code, so an agent's
  configured max-turns limit is dropped by the capability gate and does not bound a Claude run. Budget
  ceilings still apply and do bound cost.

---

## See also

- [../README.md](../README.md) — project overview and quick reference.
- [ARCHITECTURE.md](ARCHITECTURE.md) — how the pieces fit together.
- [../SECURITY.md](../SECURITY.md) — local security model and how to report a vulnerability.
- [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) — the authoritative list of what is implemented,
  experimental, or present in code but not user-reachable.
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — problem/cause/fix reference for common issues.
