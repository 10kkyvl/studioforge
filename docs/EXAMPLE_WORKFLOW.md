# Example workflow

This is a small, reproducible walkthrough of StudioForge, split into two tracks. The repository
ships no bundled example Roblox place, so Track B uses your own project directory instead of a
fixture.

- **Track A — No external dependencies**: `studioforge --mock`. No Claude, Codex, Git, Rojo, or
  Roblox Studio required. Everything you see is either the daemon's own domain/API running for
  real, or data seeded once at startup — this page tells you which is which.
- **Track B — With a real project**: registers one of your own directories, creates the two
  optional `.agent/` context files, creates a Claude Code agent, sends one instruction through
  chat, and verifies the result with the automatic Git checkpoint.

Every command below is taken from `cmd/studioforge/main.go`, `internal/app/app.go`, and
`internal/api/api.go` — nothing here is invented.

## Track A: no external dependencies

### Commands

From a packaged build (see the root [README](../README.md) for how to obtain one):

```powershell
./studioforge.exe --mock
```

From source (builds the embedded frontend first, then runs the daemon — see
[scripts/dev.ps1](../scripts/dev.ps1) / [scripts/dev.sh](../scripts/dev.sh)):

```powershell
./scripts/dev.ps1 --mock
```

```sh
./scripts/dev.sh --mock
```

The console prints two lines and opens your default browser automatically (pass `--no-open` to
suppress that and open the printed URL yourself):

```text
STUDIOFORGE_URL=http://127.0.0.1:PORT
STUDIOFORGE_BOOTSTRAP=<one-use token>
```

The port is chosen freely each run unless you pass `--port`, so expect a different number every
time.

### What you will see

`--mock` seeds three isolated demo projects on first run (`internal/database/demo.go`), each with
its own on-disk workspace under the data directory's `demo-projects/` folder:

| Project | Tag | Agents | Tasks |
|---|---|---|---|
| Skyline Obby (`demo-obby`) | Gameplay | Forge Lead (orchestrator), Builder (engineer), Verifier (QA) | Lock gameplay contract (completed), Implement milestone slice (running), Review and playtest (blocked) |
| Harbor Tycoon (`demo-tycoon`) | Economy | same three roles | same three tasks |
| Neon Arena (`demo-arena`) | Prototype | same three roles | same three tasks |

All demo agents use the built-in `mock` provider — no Claude/Codex CLI is exec'd. Each project also
shows one already-`completed` run in its history with a small mock cost (`$0.28`, `$0.47`, and
`$0.66` respectively).

**This is seeded demo data, not something StudioForge discovered live:**

- The **Studio Sessions** view shows two rows ("Studio — Skyline Obby," active; "Studio — Neon
  Arena," inactive). These are not real Roblox Studio instances StudioForge found — real Studio
  discovery into that view is not implemented; the rows are seed data marked `mock: true` in the
  database.
- The task list's dependency arrows (build waits on design, review waits on build) come from the
  same seed, but task dependencies are a real, live feature now: the **Tasks** view lets you pick
  dependencies when creating a task on any project, and the API rejects a dependency cycle. What is
  still missing is enforcement — a run does not check whether a task's dependencies are finished
  before starting (see [Known Limitations](KNOWN_LIMITATIONS.md)).

### Optional: trigger one live run

Everything above is static seed data, but you can still exercise a real, live run without any
external dependency: open a project's **Chat** view and send any message to its default thread. The
`mock` provider executes for real (`internal/providers/mock/mock.go`) and streams roughly seven
scripted events about 90&nbsp;ms apart — a start status, two partial assistant messages, a simulated
tool call, an artifact note, a usage/cost event, and a final message reading "Acceptance criteria
verified in mock mode." — then reports `$0.42` in cost. This is a genuine run through the same
scheduler, event stream, and persistence path a Claude or Codex run uses; only the "provider" is
scripted.

### Expected output

- The dashboard lists exactly three projects, each with a colored tag and a project card.
- Opening **Team builder** for any of the three shows three enabled `mock` agents.
- Opening **Tasks** shows three tasks per project with the statuses in the table above.
- Sending a chat message (optional step) produces a short streamed reply within about a second and a
  persisted transcript you can scroll back to.

### If it does not work

| Symptom | Check |
|---|---|
| Command exits immediately with an error | Run `./studioforge.exe doctor --mock` (or `go run ./cmd/studioforge doctor --mock` from source) and read the `database`/`dataDirectory` checks. |
| Browser does not open | Look for the `STUDIOFORGE_URL=` line in the console and open it yourself; a headless environment cannot open a browser regardless. |
| "Another instance" / immediate exit | A previous StudioForge process is still holding the data directory's lock; stop it first or point `--data-dir` elsewhere. |
| Blank page in the browser | Check the browser console and `GET /api/v1/health`; a from-source build must run `npm run build` in `web/` before `go build`, which `scripts/dev.ps1`/`dev.sh` already do for you. |

## Track B: with a real project

### Prerequisites

- A packaged StudioForge build, or Go 1.25+/Node.js 22+/npm to run from source.
- `git`, reachable on `PATH` (or configured under Settings).
- Claude Code CLI installed and authenticated (`claude --version` and `claude auth status`). This
  walkthrough deliberately uses Claude, not Codex or the mock provider, because the automatic Git
  checkpoint step (below) only runs before a **Claude** run — see
  [docs/SECURITY.md](SECURITY.md#command-execution).
- A project directory you own. It can be empty (StudioForge can create it and scaffold a minimal
  Rojo skeleton) or an existing Rojo/Roblox project. Ideally it is already a Git repository —
  `git init` it first if not — since the checkpoint step is a silent no-op outside a Git repo.

### Steps

1. **Start the daemon** (not `--mock` this time), and open the printed URL if it did not open a
   browser for you:

   ```powershell
   ./studioforge.exe
   ```

2. **Register your project.** On the dashboard, click **New project**, fill in **Project name** and
   **Canonical path** (point it at your directory; tick **Create directory if missing** if it does
   not exist yet), leave **Open in Studio after creating** unchecked, and click **Register project**.
   This calls `POST /api/v1/projects`, which canonicalizes the path (resolving symlinks — see
   [docs/SECURITY.md](SECURITY.md#local-file-access)), writes `default.project.json` plus
   `src/server/Main.server.lua` and `src/client/Main.client.lua` only if the folder has no
   `default.project.json` yet, and creates one default agent automatically using your Settings'
   default provider (Codex, unless you changed it).

3. **Run the doctor check.** In a separate terminal:

   ```powershell
   ./studioforge.exe doctor
   ```

   (or `go run ./cmd/studioforge doctor` from source). It is safe to run this alongside the running
   daemon. Read the JSON it prints: `dependencies.git`, `dependencies.claude`, and
   `dependencies.codex` each report `status` of `ok`, `warning` (found but not authenticated),
   `missing`, or `error`, plus the detected `path`/`version` and a `help` string. Confirm `claude`
   shows `ok`. `dependencies.studioMcp` and `dependencies.rojo` are not needed for this walkthrough.

4. **Add the optional context files.** Create two files in your project's root — exactly these two
   names are the only project context StudioForge reads (`internal/projects/context.go`); their
   content is read verbatim and prepended to every run's system prompt for this project. There is no
   indexing, embeddings, or code scanning involved.

   `.agent/constitution.yaml`:

   ```yaml
   architecture:
     server_authoritative: true
     unrelated_refactors: forbidden
   safety:
     production_publish_requires_confirmation: true
   ```

   `.agent/requirements.md`:

   ```markdown
   # Requirements

   - Build a small obstacle-course prototype.
   - Keep gameplay logic server-authoritative; the client only reads state.
   - Do not touch anything outside src/ and .agent/ without asking first.
   ```

5. **Create a Claude agent.** Open **Team builder** for your project, click **Add agent**, and fill
   in: **Agent name** (e.g. "Claude Builder"), **Provider** = `Claude Code`, leave **Model** blank
   for the CLI default, **Reasoning effort** = `medium`, **Permission profile** =
   `workspace-write` (lets Claude edit files; see the tier table in
   [docs/SECURITY.md](SECURITY.md#roblox-studio-access) if you also plan to reach Roblox Studio),
   and click **Create agent**.

6. **Make it the thread's lead agent.** Open **Chat** for the project. The chat composer always
   submits without naming a specific agent — the server picks the project's **lead agent** if one is
   set, otherwise the first enabled agent (`internal/api/api.go`, `createRun`). Use the **Lead
   agent** dropdown in the chat header and select the Claude agent you just created; otherwise your
   message may run against the original default (Codex) agent instead.

7. **Send one concrete instruction.** Make sure the mode toggle reads **Do** (not **Plan** — Plan
   mode makes Claude propose without editing, and it also skips the checkpoint step). Type a small,
   concrete instruction into the composer, for example:

   ```text
   Create src/server/Greeting.server.lua that prints a welcome message when the server starts.
   Then append a line to .agent/requirements.md noting that the greeting script was added.
   ```

   Press **Enter** or click **Send**. This calls `POST /api/v1/runs`.

8. **Watch the streamed events.** The chat panel shows a live "Working…" progress strip and streams
   Claude's events as they arrive over the `/api/v1/events` SSE stream — status, message, and tool
   events — finishing with the agent's final reply and its terminal status.

9. **Verify the result with the Git checkpoint.** Because the agent is on the Claude provider and
   the run was not in Plan mode, StudioForge committed the project's prior state right before
   starting the agent (`internal/gitcheckpoint/gitcheckpoint.go`), with the message "StudioForge
   checkpoint before agent run." In a terminal, from the project root:

   ```powershell
   git log --oneline -n 5
   git status
   git diff
   ```

   `git log` shows the checkpoint commit. `git status` shows the agent's edits as uncommitted
   changes sitting on top of it (a new `Greeting.server.lua` and a modified `requirements.md`, for
   the example above). `git diff` shows exactly what changed. This is the recovery mechanism: if you
   do not like the result, `git checkout -- .` (to discard uncommitted edits) or `git reset --hard
   <checkpoint-hash>` returns you to the state the checkpoint captured. There is no rollback button
   in the UI for this yet — see [Known Limitations](KNOWN_LIMITATIONS.md).

### Expected output

- Step 2 returns a `201` and the project appears on the dashboard with a default thread already
  created.
- Step 3's doctor output shows `"status": "ok"` for `git` and `claude`.
- Step 7's run finishes with a terminal status (`completed`, or `failed`/`cancelled` if something
  went wrong) and a final assistant bubble in the chat transcript.
- Step 9 shows exactly one new commit titled "StudioForge checkpoint before agent run," and `git
  diff` shows the file(s) the agent touched. If the agent made no changes (for example, because you
  used `read-only`), `git status` shows a clean tree on top of the checkpoint — that is also correct
  behavior, not a failure.

### If it does not work

| Symptom | Check |
|---|---|
| "Provider is not configured" / "provider_auth" when sending | Re-run `studioforge doctor`; `claude` must report `Available` and `Authenticated`. Run `claude auth status` directly. |
| No checkpoint commit appears | Confirm the agent's **Provider** is `Claude Code` (not Codex/Mock), the mode was **Do** (not **Plan**), and the project directory is a Git repository (`git rev-parse --git-dir` succeeds in it). |
| Chat message runs against the wrong agent | Re-check the **Lead agent** dropdown in the chat header — the composer has no per-message agent picker. |
| "AI workers are disabled in safe mode" | You started with `--safe-mode`; restart without it. |
| `.agent/` files seem to have no effect | Confirm the exact file names and location: `<project root>/.agent/constitution.yaml` and `<project root>/.agent/requirements.md`. Any other file name in `.agent/` is not read by StudioForge. |
| Blank UI / 401 after reusing an old URL | Bootstrap tokens are one-use; restart the daemon or keep using the browser tab that was opened for you — see [Known Limitations](KNOWN_LIMITATIONS.md). |

## What this workflow does not demonstrate

Both tracks stay inside what is actually wired end to end. They intentionally do **not** exercise:

- **Git rollback through the UI** — `internal/gitops.SafeRollback`/`Tag` are implemented and tested,
  but no HTTP endpoint exposes them; use the `git` commands in step 9 instead. (`Status`/`DiffHead`
  are wired, behind the chat view's Changed files panel.)
- **Rojo live-sync sessions** — `rojo serve` session management (`internal/rojo`) is implemented and
  unit-tested, but no endpoint starts, stops, or queries one; only Rojo *build* (used by **Open in
  Studio**) is reachable today.
- **Automated playtest validation** — there is no wired path that starts Play mode, reads the
  console, and produces a structured pass/fail result on its own.
- **Run execution respecting task dependencies** — dependencies can be created and are validated for
  cycles, but a run does not check whether a task's dependencies are done before starting.

See [Known Limitations](KNOWN_LIMITATIONS.md) for the complete list.

## See also

- [GETTING_STARTED.md](GETTING_STARTED.md) — installation and first run.
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — a fuller troubleshooting reference.
- [ARCHITECTURE.md](ARCHITECTURE.md) — how the daemon, scheduler, and providers fit together.
- [SECURITY.md](SECURITY.md) — the security model referenced throughout this walkthrough.
