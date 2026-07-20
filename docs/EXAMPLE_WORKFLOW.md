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
- **Run execution respecting task dependencies** — dependencies can be created and are validated for
  cycles, but a run does not check whether a task's dependencies are done before starting.

See [Known Limitations](KNOWN_LIMITATIONS.md) for the complete list.

## Track C: the self-correcting playtest validation loop

This extends Track B and needs the same prerequisites, plus an open Roblox Studio holding the
project's own place (see **Open in Studio** on the project card, or turn on **Open Studio
automatically before a run** in Settings).

1. In **Team builder**, edit the Claude agent from Track B: check **Validate with a Studio playtest
   after each run**, and leave **Max correction runs** at its default of 1. This setting is per agent
   and off by default — turning it on is what makes the loop opt-in, not automatic.
2. Confirm the agent's **Permission profile** is `workspace-write` or `danger-full-access` — the loop
   never runs for `read-only`, the same tier rule as the Studio tool allowlist itself
   (`docs/SECURITY.md#roblox-studio-access`).
3. Send an instruction in **Do** mode (not **Plan** — the loop never runs for a plan-mode turn) that
   would leave a script error if done wrong, for example a script referencing a Studio instance path
   that does not exist.
4. After the run reaches its normal completion, watch the **Runs** view: a `validation` badge appears
   on the run row once the daemon's own Studio MCP connection has entered Play mode, polled the
   console for the configured window (`playtest_window_seconds` in Settings, default 30 seconds),
   taken a screenshot, and exited Play mode again.
5. If the console showed a script error or an infinite-yield warning, the badge reads **Playtest
   failed**, and a second run appears in the list linked back to the first (**Correction of** on the
   correction run, **Correction scheduled** on the original) — it resumes the same chat session with
   the console error lines and the screenshot reference already in its prompt, so the agent does not
   need to re-discover what went wrong.
6. If the correction's own playtest later passes, the original run's badge updates to **Fixed by
   correction**; if the agent's `maxCorrectionRuns` is exhausted without a pass, it reads **Correction
   failed** instead — the loop never retries silently past that bound, and it never fails the *original*
   run over a playtest outcome either way.

### Expected output

- The Runs view shows a validation badge for both the original and the correction run once each one's
  Play-mode pass completes.
- A run whose Studio grant was withheld (ambiguous or closed Studio, `read-only` profile, Codex
  provider, plan mode, or the agent simply not opted in) shows no validation badge at all — the loop is
  fail-open, exactly like Studio access itself.

### If it does not work

| Symptom | Check |
|---|---|
| No validation badge ever appears | Confirm **Validate with a Studio playtest after each run** is checked on the agent, the profile is `workspace-write`+, the run was in **Do** mode, and the chat header's Studio badge showed a match — a withheld Studio grant silently skips validation. |
| Badge reads "Playtest inconclusive" every time | The console produced no usable text, or Studio became unreachable mid-playtest (closed, crashed, or another MCP client took the connection) — this is fail-open by design, not a bug; check `docs/KNOWN_LIMITATIONS.md` for the heuristic classifier's caveats. |
| No correction run appears after a failure | Check the agent's **Max correction runs** — a value of 0 disables corrections entirely, and a lineage that already reached the limit stops scheduling more. |

## Track D: real Studio session discovery

This does not require a run at all — just an open Roblox Studio holding some project's place (see
**Open in Studio** on a project card).

1. Open the **Studio Sessions** view and click **Refresh**. This is a manual action deliberately: every
   refresh spawns the Studio MCP launcher, which competes with a running agent for Studio's single WS
   host slot, so nothing polls it in the background.
2. A real, open Studio instance now appears as a card showing its reported name, play/edit state, and
   (if its name unambiguously matches a registered project's expected place file name) that project
   already selected in the **Bind project** dropdown.
3. For an instance that did not auto-match, pick a project manually from **Bind project**. Click
   **Refresh** again — the manual choice is not overridden by the new discovery pass.
4. With no Studio MCP launcher installed at all, the view shows **Studio MCP not detected** instead of
   an empty list or an error.

### Expected output

- Every open Studio instance appears as its own card, even with several open at once — this view lists,
  it does not gate access the way a run's own Studio grant does.
- A manual **Bind project** choice survives repeated refreshes.
- Under `--mock`, **Refresh** is a no-op; the seeded demo rows are unaffected.

### If it does not work

| Symptom | Check |
|---|---|
| "Studio MCP not detected" always shows, even with Studio open | Confirm Roblox Studio's own MCP launcher is installed/enabled (`docs/KNOWN_LIMITATIONS.md` — this is the same launcher detection `Provision` uses); an operator-configured `studio_mcp_path` override in Settings may also be pointed at the wrong location. |
| An instance's play state is blank | Best-effort only — a failure reading that one instance's state leaves it unknown rather than dropping the instance from the list. |
| A newly opened Studio never auto-binds | Its reported file name must exactly match the registered project's expected place name (case-insensitively); a place opened from somewhere other than **Open in Studio** will not match, and needs a manual **Bind project** instead. |

## Track E: Rojo live-sync session

Needs a project already open in Studio (**Open in Studio**) and the Rojo Studio plugin connected once
per session (the plugin's own **Connect** button inside Studio — StudioForge cannot press it for you).

1. In the chat header, click the sync badge to start a live-sync session
   (`POST /api/v1/projects/{id}/sync`).
2. Open the project's **Overview**. The new **Rojo live-sync session** panel now reads **Running**, with
   the allocated port shown underneath, and a scrollable panel of the session's most recent `rojo serve`
   log lines.
3. Edit and save a `.lua`/`.luau` file under the project's Rojo tree. The edit reaches the already-open
   Studio through the session without restarting it (the actual point of live-sync, not new in this
   track); the Overview panel's log lines update to reflect it.
4. Click the sync badge again to stop the session (`DELETE /api/v1/projects/{id}/sync`) — the Overview
   panel reverts to **Stopped** and the log lines disappear, since they belong to that session, not a
   history across sessions.

### Expected output

- The Overview panel's running/stopped state always matches the chat header's own sync badge — both
  read the same `project.sync` status.
- The log lines shown are this session's own most recent output (bounded to the last 100 lines), not a
  file the operator has to go find on disk or in the daemon's own logs.

### If it does not work

| Symptom | Check |
|---|---|
| The Overview panel never shows **Running** | Confirm the sync badge in chat was actually toggled on — a session that failed to start (e.g. Rojo not installed) reports an error there rather than silently leaving the badge on. |
| No log lines ever appear | `rojo serve` itself may not have printed anything yet — the fake used in tests emits one startup line immediately, but a real `rojo serve` may take a moment. |
| Studio never receives the edit despite an active session | The Rojo Studio plugin still needs its own one-time **Connect** click inside Studio per session — the sync badge's hint exists for exactly this; see `docs/KNOWN_LIMITATIONS.md`. |

## See also

- [GETTING_STARTED.md](GETTING_STARTED.md) — installation and first run.
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — a fuller troubleshooting reference.
- [ARCHITECTURE.md](ARCHITECTURE.md) — how the daemon, scheduler, and providers fit together.
- [SECURITY.md](SECURITY.md) — the security model referenced throughout this walkthrough.
