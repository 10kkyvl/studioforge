# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/). Pre-release versions use the
`vMAJOR.MINOR.PATCH-alpha.N` naming scheme.

## [Unreleased]

### Added

- A self-correcting Studio playtest validation loop. When a Claude agent opts in
  (`validateAfterRun`, off by default) and a non-plan run completed with `workspace-write`
  permission or above and an actual Studio MCP grant, StudioForge now opens its own Studio MCP
  connection (independent of the agent's own, already-exited one) and enters Play mode, polls
  `get_console_output` for a bounded window (default 30s, `playtest_window_seconds` in Settings),
  takes one `screen_capture`, exits Play mode, and classifies the console into `passed`, `failed`
  (script errors, infinite yield warnings, uncaught exceptions), or `inconclusive` (no signal, or
  Studio became unreachable mid-playtest). The whole pass is fail-open: an absent launcher,
  ambiguous or closed Studio, or a malformed tool response all resolve to `inconclusive` and never
  fail the run. On `failed`, up to `maxCorrectionRuns` (default 1, per agent) follow-up correction
  runs are scheduled through the normal scheduler — same writer lease, same budget ceiling, same
  Git checkpoint — resuming the same CLI session with the console errors and screenshot reference
  folded into the prompt. A run's `validation` field (`none`/`passed`/`failed`/`inconclusive`/
  `corrected`/`correction_failed`) and correction-run linkage (`parentRunId`/`correctionDepth`) are
  now part of every run record and shown as a badge and link on the Runs view.
- Real Studio session discovery. The Studio Sessions view's new **Refresh** button
  (`POST /api/v1/studio/sessions/refresh`) opens a live Studio MCP connection, lists every open Roblox
  Studio instance, and persists them — replacing the `--mock`-only demo rows the view previously
  always showed on a real install. Unlike granting Studio access to a run, this listing deliberately
  does not refuse when more than one instance is open. An instance is auto-bound to a registered
  project when its reported name unambiguously matches that project's expected place file name, and a
  later refresh never overrides an existing manual **Bind project** choice. Refresh is manual, not a
  background poll, and is a no-op under `--mock`. An absent Studio MCP launcher now shows a clear
  "Studio MCP not detected" state instead of an empty list.

## [0.2.0-alpha.1] - 2026-07-20

### Added

- Agents can now ask the operator a closed, multiple-choice question mid-conversation instead of
  only free text. A completed assistant message that carries a fenced `studioforge-question` block
  (2-4 options) is detected by the scheduler, published as a new `question` run event, and parks the
  run in the pre-existing but previously unreachable `waiting_decision` status instead of `completed`.
  The chat view renders it as a card with one button per option; clicking
  answers exactly as typing that option's label would and, because a run in `waiting_decision` is now
  resumable just like a completed one, continues the same underlying CLI session. The deterministic
  `--mock` provider demonstrates it: send a chat message containing "question test" to see a sample.
- The standing system prompt every agent, subagent, and restarted run receives now also steers agents
  toward the Studio MCP tools instead of hand-written Luau: `generate_mesh`/`generate_material`/
  `generate_procedural_model` before faking geometry or textures with a script, `search_asset` then
  `insert_asset` before generating something from scratch, `wait_job_finished` after kicking off an
  async generation or asset job, `subagent`/`skill` delegation for well-scoped Studio-side work, and
  `screen_capture`/console/playtest checks before reporting a visual or gameplay result as done. It
  also documents the `studioforge-question` convention used by the feature above.
- The project overview now shows the Roblox Studio MCP launcher's status alongside Rojo's, and the
  Settings diagnostics card now shows each dependency's `help` text (previously computed by
  `studioforge doctor` but never rendered anywhere). A fresh install that hasn't enabled Studio as an
  MCP server inside Roblox Studio's own Assistant menu now sees that on the dashboard, with the exact
  steps to fix it, instead of only discovering it indirectly when an agent run silently proceeds
  without Studio access.
- A completed run now shows a **Changed files** panel in chat with the working tree's diff against
  the pre-run checkpoint commit (`GET /api/v1/runs/{id}/diff`, backed by the previously-unwired
  `internal/gitops` package). The panel is empty rather than an error for a project with no Git repo
  of its own.
- Task creation now accepts a `dependencies` field naming other tasks in the same project. The task
  graph is validated for cycles before a task is created (a cycle is rejected with 400), and the
  Tasks view offers a checkbox list of the project's existing tasks when creating a new one. Run
  execution does not yet check whether a task's dependencies are done — see the roadmap.
- Every run that completes now leaves a short project-memory entry (its own prompt, truncated), and
  the next run in that project surfaces up to five relevant past entries as a "Relevant project
  memory" block in its system prompt. This wires up the previously-unwired `internal/memory` store;
  it is a small addition to the existing simple prompt path, not the larger structured template
  mentioned under Removed below.
- `internal/diagnostics` (the `studioforge doctor` and diagnostic-bundle code path) now has unit test
  coverage — `Doctor.Run` across present/absent tool paths and `Doctor.ExportBundle`'s secret
  redaction — where previously it had none.

### Removed

- The Decisions feature — the `Decision` record, the `resolveDecision` endpoint, and the Decisions
  view — is gone. It never had a live producer in any release, and it fully duplicated the live
  `studioforge-question` / `waiting_decision` interactive-question feature (unaffected by this
  change; that feature only ever shared a word with Decisions, not any code). A new migration
  (`006_drop_decisions.sql`) drops the `decisions` table.
- The unused structured prompt template (`internal/prompts.Assemble`/`Input`) and the dead
  `DecisionRequest`/`PlaytestResult`/`ReviewResult` struct definitions it carried are deleted. The
  system prompt every run actually receives is unchanged: `prompts.ForRun`, now also carrying the
  memory block described above.
- The asset quarantine validator (`internal/roblox/assets`) and the empty Assets view placeholder
  are deleted; no asset scanning, upload handling, or Marketplace automation exists or is planned
  near-term.
- The unused Studio instance/binding tracker (`internal/roblox/studio.Service`) is deleted. It had
  no live caller — the Studio-bind endpoint already used the database store directly. `studio.Opener`
  (used to actually open a project in Studio) is unaffected.

### Fixed

- `GET /api/v1/runs/{id}/diff` (see Added) no longer leaks raw `git` CLI output into the Changed
  files panel for a project with no Git repository of its own. `git diff HEAD` outside a repo prints
  a large "not a git repository... usage: git diff --no-index..." block to stderr instead of a clean
  error, and that text was surfacing verbatim in the panel. The diff endpoint now checks
  `rev-parse --git-dir` first — the same pre-check `internal/gitcheckpoint.Checkpoint` already used —
  and returns an empty diff with no error when the project isn't a Git repo.
- Chat no longer retires a run's progress strip while the agent is still working. The provider
  streams its own JSON verbatim under the `status` event type, so a sub-agent finishing
  (`subtype: "task_notification"`, `status: "completed"`) read as the whole run ending. Long
  orchestrator runs that delegate work therefore looked stopped for their remaining minutes, with
  the reply arriving later out of nowhere. A run is now considered finished only when the scheduler
  itself says so, via its `scheduler.state` raw type.
- Relatedly, `waiting_decision` — the status a run now parks in when an agent asks a question (see
  Added) — was added to the set of statuses that mark a run as finished-for-now on the client,
  alongside `completed`/`failed`/`cancelled`. Without it, the chat view kept showing "Working…"
  forever after a question was asked, because the client never recognized that the run had actually
  stopped executing to wait for an answer. Found via manual end-to-end testing.
- Fixed a file descriptor leak in `studioforge doctor --bundle`: exporting a diagnostics bundle
  created the archive file but never closed it, so repeated exports over a long-running daemon
  accumulated open handles instead of releasing them.
- The shared Studio-status probe that backs the chat badge no longer dies for every waiting caller
  when one of them cancels. Concurrent lookups for the same project's Studio status shared one
  in-flight probe, but that probe ran on the first caller's own request context, so cancelling that
  first request also failed every other caller still waiting on the shared result. The probe now
  runs on its own timeout-bound context instead.
- Stopping a supervised subprocess (Rojo live-sync sessions today) now waits for the process to
  actually be reaped on the force-kill path before returning, instead of returning as soon as the
  kill signal was sent, which could let a caller believe a process was gone while it was still
  exiting. Reading a subprocess's output lines is now non-blocking, so a process whose output nobody
  is currently draining fast enough no longer backpressures its own stdout/stderr pipe.
- A Rojo live-sync session's log output is now actually drained and logged in production. Previously
  only tests consumed it, so in the running daemon a session's output could fill the channel's
  buffer and backpressure — and eventually wedge — the `rojo serve` subprocess once it filled.
- A git-checkpoint failure before an agent run is no longer silently discarded: the error is now
  logged, so a project that couldn't be committed (a dirty submodule, a lock held by another Git
  process, and so on) leaves a trace instead of failing to checkpoint with no record anywhere.
- Every frontend request now carries a 15-second timeout. This app keeps one `EventSource` open per
  tab for the whole session, and Chrome caps a profile to 6 concurrent connections per origin on
  HTTP/1.1; once that budget was exhausted a `fetch` could sit pending indefinitely instead of
  failing, wedging any UI awaiting it (a spinner that only clears in a `finally`). A timed-out request
  now surfaces as a normal error instead of staying stuck.
- `static()` silently served `index.html` for any missing asset, including ones with a file extension
  (`.js`, `.css`, `.woff2`, ...) — a real 404 that used to make the browser try to execute HTML as a
  JS module, so the app never booted. Only extensionless (client-side route) paths fall back now, and
  `Content-Type` is hardcoded for the file kinds the frontend actually ships instead of trusting
  `mime.TypeByExtension`, which reads the Windows registry and can report a bogus type (observed:
  `text/plain` for `.js`) on a stripped-down or mangled machine.
- Fixed duplicate event replay on SSE reconnect. `GET /api/v1/events` accepted both the browser's
  native `Last-Event-ID` header and an `after` query parameter but always preferred the query
  parameter, even when it was stale, which defeated the browser's own reconnect cursor and
  re-delivered already-seen events. The endpoint now prefers `Last-Event-ID` when both are present,
  and reconnecting the client's event stream (for example, on tab visibility change) now reuses the
  existing connection instead of tearing it down and reopening it with a stale cursor, which was the
  other half of the same symptom.
- The client-side half of that same reconnect fix had a gap: `connectEvents`/`openSharedStream` were
  made idempotent so repeated calls reuse one shared `EventSource`, but the page's own `connectStream`
  still called `disconnect()` unconditionally before every reconnect attempt. Since the page is
  normally the only subscriber, that dropped the subscriber count to zero, which closed the shared
  stream anyway — so every tab visibility change (switching away and back, restoring a minimized
  window) still tore down and reopened the connection instead of leaving the live one alone, exactly
  what the idempotency was meant to prevent. `connectStream` now tracks whether this tab already holds
  a live subscription and skips reconnecting when it does.
- That still left the worse bug in the same function: `connectStream` also returned before opening a
  connection at all whenever `document.hidden` was true, including on the very first call, from
  `initialize()` at mount. A tab that starts hidden — opened in the background, restored by the
  browser, or otherwise never receiving an actual hidden-to-visible `visibilitychange` transition —
  therefore never opened a live stream for its entire session: no error, no console warning, just a
  chat that never showed live progress and a "Working…" that never resolved. `connectStream` no longer
  gates on `document.hidden` at all; the `visibilitychange` handler still closes the connection when a
  tab genuinely goes hidden (so an abandoned background tab doesn't hold a permanent slot in Chrome's
  6-connections-per-origin budget) and reopens it on return, but establishing the *first* connection no
  longer depends on that event ever firing. Sending a chat message now also defensively ensures the
  stream is connected, as a second line of defense that needs no visibility signal at all.
- The SSE message handler's `try/catch` covered both `JSON.parse` and the delivery of the parsed event
  to every subscriber in one block, so an exception thrown while a subscriber processed a live event
  (not just malformed server data) was silently swallowed with no console error — the connection would
  stay open and healthy while the UI simply stopped updating. Parsing and delivery are now separate, so
  only genuinely malformed payloads are treated as recoverable; a bug in event handling itself is no
  longer hidden.
- Two silent error paths in the scheduler now log loudly instead of vanishing: a failed database
  write when persisting a run's status transition, so a run whose stored status disagrees with what
  the event stream reported now leaves a trace; and a lost project write-lease during execution, so
  a run whose lease was reaped mid-flight no longer keeps executing without the mutual-exclusion
  guarantee it depends on — losing the lease now cancels the provider process and fails the run
  instead of letting it continue unprotected.
- The app no longer opens a live SSE connection when the initial data snapshot fails to load. It
  previously opened one anyway, behind the error screen, on a session the operator couldn't see yet.
- The Alt+1..9 view-switch keyboard shortcut no longer fires while typing in an input, textarea, or
  contenteditable element, and no longer misfires on AltGr — common on non-US/RU keyboards, which
  Windows reports as both `ctrlKey` and `altKey`, previously indistinguishable from a plain Alt combo.
- Fixed four project-switching race conditions in the chat view: stale thread, message, lead-agent,
  and pace data could appear after rapidly switching projects; a task attached in one project could
  leak into a run submitted after switching to another; an image upload that resolved after its
  project had already been switched away from could pollute the new project's pending attachments;
  and double-clicking "New chat" could create two threads instead of one.
- Fixed a question card rendering bug: each option button was keyed on its own label text, so two
  options sharing a label (a plausible mistake for an agent to make) would collapse or misrender in
  Svelte's keyed `{#each}` block. Options are now keyed by their position instead.
- The frontend's `studioforge-question` fence regex accepted a closing fence with no newline before
  it, while the scheduler's regex that decides whether a run actually parks in `waiting_decision`
  required one. A message the backend never treated as a question could still render as a question
  card once persisted and reloaded from history. Both regexes now agree.
- A Rojo live-sync session's drained output is no longer logged at `Info` level per line, which could
  flood the log during an active sync session with no way to quiet it; it now logs at `Debug`.
- A supervised subprocess whose output nobody drains fast enough now counts every dropped line instead
  of only warning once and then going silent for the rest of the run.
- Fixed a real connection leak in the shared SSE client: on a transient network error, `onerror` set
  the shared `EventSource` reference to `null`, but the browser can and does automatically reconnect
  that same connection afterward — leaving the app with no reference to a connection that was actually
  still alive. A tab that later went to the background could then never find that connection to close
  it, pinning one of Chrome's 6 connections-per-origin slots for the rest of the tab's life. The
  reference is no longer cleared on error; the existing `readyState` check already correctly detects a
  connection that is genuinely and permanently closed.
- `studioforge doctor --bundle` closed its output file via both an explicit `Close()` on success and a
  deferred `Close()` that ran regardless, and on a write failure it removed the target file before
  closing the handle still open on it rather than after — fragile ordering that risked the delete
  failing while the file was still held open. Export now closes the file exactly once, and always
  before removing it on a failure.
- The chat progress bar and live message updates no longer disappear after switching away from the
  Chat tab and back. `ChatView`'s tracking of an in-flight run was purely local component state, so it
  was lost whenever the component remounted, leaving no visible sign that a run was still executing.
  It is now reconciled against the server's run list on thread load instead.
- Archiving a project — the app's only "delete" action, there is no hard-delete endpoint — no longer
  leaves it fully reachable. The top bar project switcher and the default project selection previously
  drew from the unfiltered project list, so an archived project stayed selectable and everything scoped
  to it, including its chats, remained fully usable. Both now exclude archived projects, and archiving
  the currently selected project automatically falls back to another active one.
- The Codex adapter silently dropped the run's entire system prompt — the standing house rules
  (including "answer in the operator's language"), the project's context, and the agent's own persona
  never reached the Codex CLI at all, only the raw user message did. `codex exec` has no
  `--append-system-prompt` equivalent, so Codex-backed agents had no language instruction whatsoever,
  which is why an operator writing in Russian could still get an English reply once a Codex-backed run
  was involved. The system prompt is now folded into the prompt text itself and re-sent on every turn,
  including resumed ones, mirroring how the Claude adapter already re-sends `--append-system-prompt` on
  every call so a long session keeps being reminded rather than drifting once the operator stops
  repeating themselves.
- The house rules' "Using the Studio MCP tools" section told every agent to reach for
  `generate_mesh`/`search_asset`/`wait_job_finished`/etc. unconditionally, but those tools are only ever
  wired up for Claude runs with a workspace-write-or-higher permission profile — a Codex run or a
  read-only agent (a real, supported configuration) following that guidance would have every such call
  denied with no warning that this was expected. The section now says so upfront, and `start_stop_play`
  — previously grouped with the always-available `screen_capture`/`get_console_output` confirmation
  tools — is now called out as needing the same non-read-only profile as the rest of the section, since
  it changes play state rather than just observing it.
- The language rule in house rules only judged "the operator's most recent message" as a whole, so a
  Russian question that happened to paste a Roblox error log, stack trace, or script snippet — routine
  in this kind of chat — could read as a language switch and pull the whole reply into English, even on
  the Claude adapter, which already received the rule every turn. The rule is now explicit that only the
  operator's own prose sets the reply language; pasted code, logs, console output and other quoted
  English inside their message is data being shown, not a language switch, and the reply stays in the
  operator's language even when everything else being read and acted on (files, tool output, docs) is in
  English.

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
