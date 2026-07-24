# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/). Pre-release versions use the
`vMAJOR.MINOR.PATCH-alpha.N` naming scheme (later `-beta.N`, `-rc.N` as needed).

## [Unreleased]

## [0.5.0-beta.3] - 2026-07-24

### Changed

- Left navigation is grouped into three labeled sections — Work (Chat, Tasks), Project (Projects,
  Overview, Team), Monitoring (Activity, Runs, Studio sessions) — plus a standalone Settings entry,
  replacing a flat 9-item list (`web/src/routes/+page.svelte`'s `navGroups`, `web/src/app.css`). The
  top bar's project selector is now hidden on the four views where it had no effect (Activity, Runs,
  Studio sessions, Settings — `showProjectSwitch`), and the sidebar nav label for Activity is
  shortened to "Activity"/«Активность» in both languages (the Activity view's own page heading is
  unchanged: "Global activity"/«Общая активность»).
- Typing `/` in the chat composer now opens a filterable slash-command menu
  (`ChatView.svelte`'s `SLASH_COMMANDS`/`showSlashMenu`) listing `/task`, `/build`, `/playtest`,
  `/plan`, `/do`, and `/open` with a localized one-line description for each, instead of requiring the
  operator to already know a command's name.
- Empty states are informative instead of misleading or absent: a fresh install's Projects view now
  shows "No projects yet. Create the first one to get started." with a **New project** button, instead
  of the filter-mismatch message it previously showed even with zero projects registered
  (`ProjectsView.svelte`'s `hasAnyProjects`); the Tasks board, Runs list, and Chat thread list each get
  a dedicated empty message instead of an empty board or blank panel; and Activity's empty-state text
  no longer references a "demo run" button that does not exist.
- Overview's project-health and git cards no longer show hardcoded fakes ("Verified" / "Active" for
  every project regardless of its history): the health card now shows the project's actual last-run
  status (or "No data yet" when it has none), and the git card reads "No data yet" until real git
  status is wired up (`OverviewView.svelte`).
- Dev-facing jargon is replaced with human, localized labels throughout Overview/Settings/Team/Studios:
  effort levels (low/medium/high/xhigh → Low/Medium/High/Extra high), permission profiles
  (read-only/workspace-write/danger-full-access → "Read only"/"Write in project"/"Full access
  (unsafe)"), provider "Mock" → "Demo (no AI)", raw dependency/state codes (ok/missing/present/
  active/stopped/none, play/edit) → localized words, server-default thread titles "New chat"/"Chat"
  localized at render (`displayThreadTitle`), the seeded default agent role now localized instead of
  hardcoded English, and the New Project dialog's "Canonical path" field relabeled "Project folder"
  with an example-path hint.
- Activity's table gained a time-updated column, empty resource cells now render nothing instead of a
  dash, the Projects view's count label reads grammatically in Russian ("Проекты: 3" instead of a raw
  pluralization glued to a number), an "All projects" chip appears on Team and Tasks when no project is
  selected, and the first-run wizard now reassures that missing tools can be configured later in
  Settings instead of only listing what's missing.

### Fixed

- The **Start agent run** button on project cards and the Overview, and the **Run this agent** button
  on Team, silently did nothing when clicked. Both now open Chat with the corresponding project
  selected; Team's variant also sets that agent as the thread's lead agent, via the new
  `web/src/lib/uiIntents.ts` (`setPendingLeadAgent`) consumed by `ChatView.svelte`'s
  `applyPendingLead`.
- A failed run's `run.error` was recorded by the backend but never rendered anywhere in the UI. It now
  shows as a danger banner in the Runs view's detail panel (`RunsView.svelte`), an error strip in Chat
  for the thread's most recent failed run (`ChatView.svelte`'s `failedRunError`), and a hover tooltip
  on a failed run's status chip in Activity (`ActivityView.svelte`).
- The sidebar footer showed the literal run-status word "Interrupted"/«Прерван» whenever the live SSE
  stream dropped, which read as a run problem rather than a connection problem. It now shows
  "Online"/"Connection lost — reconnecting…" (`footer.online`/`footer.reconnecting`), and the footer
  version no longer renders a doubled "vv" prefix
  (`snapshot.diagnostics.version.replace(/^v/, '')` in `web/src/routes/+page.svelte`). The footer also
  now shows a "Demo mode" chip under `--mock`.
- The session-error screen shown for an invalid or expired local session had hardcoded English text and
  a vague message; it is now localized in English and Russian with actionable copy ("Open StudioForge
  again from the app…", new `session.title`/`session.body`/`session.retry` keys). Load and action
  failures also now surface a human-readable reason through a new `friendlyError` mapping
  (`web/src/lib/api.ts`) — timeout, network, session-expired (401/403), not-found (404), and server
  error (5xx) — instead of a raw message like "HTTP 500".
- A stale slash-command confirmation (e.g. from `/task` or `/plan`) could keep showing after switching
  to a different thread or project. `ChatView.svelte` now clears `commandInfo` on both a thread switch
  and a project switch.
- The Runs view's own message composer, which created thread-less "orphan" runs duplicating what Chat
  already does, is removed; the panel now shows a hint that new runs start from Chat
  (`RunsView.svelte`).
- Creating or updating an agent in Team showed the wrong toast text — creating one echoed the "Create
  agent" button label and updating one echoed "Settings saved" — instead of a real confirmation; both
  now show "Agent created"/"Agent updated" (`team.createdToast`/`team.updatedToast`).

## [0.5.0-beta.2] - 2026-07-23

### Fixed

- Shutting down the scheduler (`internal/scheduler.Manager.Close`) no longer returns while an in-flight
  run goroutine is still writing its final status to the store: `Close` now waits for every spawned run
  to finish before returning, closing a shutdown-ordering hole where a run could still call
  `store.UpdateRun` after the caller had already closed the database.

## [0.5.0-beta.1] - 2026-07-23

### Changed

- Default theme is now **System** (follows the OS light/dark preference); it was previously always
  Dark. Switching themes now transitions smoothly, scrollbars are themed to match either palette, and
  focus-visible outlines are consistent across interactive elements (`web/src/app.css`).

### Fixed

- Light theme: the `--border`, `--success`, `--warning`, and `--danger` CSS custom properties were
  referenced throughout `app.css` and several view components but never defined for the light palette,
  producing invisible borders and off-palette status colors in the Runs view, chat, and the OpenRouter
  model picker. All four are now defined per theme.
- Theme flash on load: the stored theme is now applied before the app mounts (new `web/src/lib/theme.ts`,
  wired through the new `web/src/hooks.client.ts`), the `theme-color` meta tag now follows the active
  theme instead of always reporting dark, and choosing Dark explicitly keeps `color-scheme: dark` even on
  a light-OS machine.
- Primary button text no longer fails WCAG contrast in the light theme (new `--accent-contrast` token).
- `<html lang>` now follows the selected UI language instead of staying fixed, so hyphenation and
  assistive technology get the correct language.
- Slash-command feedback in chat (`/task`, `/plan`, `/do`, `/open`, and the command-list help text) is
  now localized in English and Russian instead of hardcoded English.
- Long run and thread titles now ellipsize instead of overflowing their row.
- `GET /api/v1/events` no longer injects an HTTP 500 JSON body mid-stream when event replay fails after
  streaming has already begun (`internal/api/api.go`); the stream now ends cleanly and the client's
  existing `Last-Event-ID` reconnect logic picks it back up. A replay failure before any events have
  streamed still returns the original 500.
- A task attached to a run (`POST /api/v1/runs` with `taskId`) no longer gets stuck in `running` when the
  run fails to submit: the task's status is now set to `running` only after the run is created
  successfully, and a failure to persist that status transition is logged instead of failing the run.

## [0.5.0-alpha.1] - 2026-07-22

### Added

- NVIDIA NIM is available as a first-class provider with secure API-key storage and a focused model
  list. Vision-capable NVIDIA models can now inspect images pasted into chat and screenshots captured
  from Roblox Studio.
- Messages sent while an agent is busy are queued in the same conversation. The active run remains
  visible, queued messages can be removed independently, and each follow-up resumes the prior context.

- New OpenRouter provider (`internal/providers/openrouter`). Unlike Claude Code, which runs as a
  local CLI subprocess, OpenRouter is an HTTP API and StudioForge drives it with its own in-process
  bounded agent loop (`agentloop.go`) — no subprocess is spawned for it. The loop streams from
  OpenRouter and executes local workspace tools (list/read/search/grep/create/edit/patch/mkdir/git/
  run_command, `internal/providers/openrouter/agenttools`) gated by the same read-only /
  workspace-write / danger-full-access permission profiles used elsewhere, plus Roblox Studio MCP
  tools through a live per-run MCP client wrapped by `internal/providers/openrouter/mcpbridge`, which
  enforces the permission profile's tool allowlist fail-closed exactly like the Claude path. The API
  key (`internal/providers/openrouter/credential`) is kept in the OS secure credential store (Windows
  Credential Manager / macOS Keychain) via `internal/platform.SecretStore`, with an environment
  variable (`OPENROUTER_API_KEY`) and an in-memory session-only fallback when the store is
  unavailable — the key is never written to SQLite, run events, application logs, or the diagnostic
  bundle, and it is required even to run free models. Its verification state is tracked in the
  `openrouter_key_state` application setting (`not_configured` / `unverified` / `configured` /
  `invalid`). The model catalog (`internal/providers/openrouter/catalog`) fetches OpenRouter's public
  Models API, caches it in the new `openrouter_model_cache` table (`fetched_at` plus the raw JSON
  payload, `internal/migrations/sql/012_openrouter_model_cache.sql`) with a 6-hour TTL and manual
  refresh, falls back to the last-good cached copy on a fetch error, and as a last resort to an
  embedded dated snapshot (`FallbackSnapshotDate`) when the cache is empty. The picker reports tool,
  vision, context, free, and verification capabilities for text models; known models without tools
  are rejected for coding agents, while unknown or stale models require an explicit per-model
  confirmation. A curated, hand-reviewed set of recommendations (`catalog/curated.go`, reviewed
  2026-07-21) groups models into Free automatic (`openrouter/free`, whose eventual model capabilities
  remain unverified), Free recommended, Best coding, Balanced, Fast and cheap, Strong reasoning, and Large
  context; free models are called out as less stable — variable quality and latency, lower rate
  limits, availability that can change — and best suited to small tasks, and free mode never silently
  falls back to a paid model. Provider routing always forces `require_parameters: true` so tool calls
  keep working regardless of which upstream provider OpenRouter selects; optional Advanced settings
  (`Provider.SetRouting`) cover data-collection preference, zero-data-retention, and fallback
  ordering, but no experimental server-side tools or plugins are ever enabled. The doctor report
  gained an `openrouter` dependency check (key state plus catalog reachability) alongside `git`,
  `claude`, and `rojo` — there is no executable path for OpenRouter, since it is not a CLI.
- Post-run Studio playtest validation, budget ceilings re-checked every turn, and usage/cost
  reporting (preferring OpenRouter's own `usage.cost`, falling back to catalog pricing when it is
  exactly zero) now work the same way for OpenRouter runs as for Claude Code runs.
- The OpenRouter provider now persists its conversation per chat thread instead of starting fresh on
  every run: each user prompt, assistant turn (with its tool calls), and tool result is written to a
  new `openrouter_messages` table (`internal/migrations/sql/011_openrouter_conversations.sql`,
  FK'd to `chat_threads(id)` with `ON DELETE CASCADE`) as the agent loop produces it, and the next
  run on that thread loads and replays the history before its first request. This survives a daemon
  restart, since it is read straight from SQLite rather than kept in memory, and it is model-agnostic
  — a thread can switch models between runs and still replay the same history. A history saved by an
  interrupted run (an assistant tool call with no persisted tool result yet) is made safe to resume
  by `sanitizeHistory`, which synthesizes a placeholder tool result for any dangling tool call and
  drops orphaned tool messages, so OpenRouter's tool-call pairing requirement is never violated on
  replay. When a thread's accumulated history grows past roughly 75K tokens, `compactMessages`
  deterministically trims it before each request — whole turns are dropped from the oldest end first
  (never splitting an assistant message from its tool results), a single fixed system note marks the
  gap, and, only if still oversized, older tool results are shrunk to a small cap; no model call and
  no fabricated content are involved, and the run emits one `openrouter.compacted` status event the
  first time it happens. Persistence is entirely best-effort and wired through a small
  `ConversationStore` seam (`internal/providers/openrouter/conversation.go`) backed by the database
  in `internal/app/conversation.go`: a database error never fails a run, and a run with no thread ID
  or no store configured behaves exactly as before.
- The OpenRouter provider now accepts image attachments on vision-capable models and wires the model
  catalog in for capability and pricing lookups. `RunRequest.Attachments` (plumbed from
  `POST /api/v1/runs`'s already-validated `body.Attachments` through `scheduler.Job.Attachments`, a
  fresh-user-turn-only field left empty on resume/restart/correction runs) carries project-relative
  attachment paths as data, not just as text in the prompt. `Provider.SetModelInfo` gives the agent
  loop a `catalog`-backed lookup of a model's vision support and per-token pricing; when a run
  attaches images to a model that isn't vision-capable (or whose capabilities aren't known), it fails
  with a controlled `openrouter.image_unsupported` error instead of silently dropping the image or
  switching models. Otherwise `buildUserMessage` re-resolves each attachment through the run's
  `agenttools.Workspace` (so a path can never escape the project root), reads and MIME-sniffs it
  (`image/png|jpeg|gif|webp` only, 10 MB cap), and folds it into the request as a base64 `data:` URL
  content part; a missing or invalid attachment is skipped, not fatal. Only the attachment's path is
  ever persisted (`StoredMessage.Attachments`), never the image bytes, and on resume `storedToMessages`
  rebuilds a stored user turn's image parts the same way — re-validating each path — only when the
  current model is vision-capable, otherwise replaying the stored text alone. Turn cost prefers
  OpenRouter's own `usage.cost`; only when cost is absent and the catalog has complete pricing does
  the loop estimate prompt, completion, cache-read/cache-write, reasoning, request, and image cost,
  marking that turn's `openrouter.usage` event `estimated: true`. `Provider.SetRouting` exposes operator-facing
  provider-routing preferences (`allow_fallbacks`, `data_collection`, `zdr`, `order`), but
  `require_parameters: true` is always forced regardless of configuration, and no experimental
  server-side tools or plugins are ever enabled. `internal/app/app.go` constructs the catalog service
  once at startup and wires both seams from settings; the catalog's own network fetch happens lazily
  on first use (falling back to the DB cache or the embedded snapshot), never blocking startup.

### Changed

- Changing the **default model** in Settings now re-points every existing agent of the selected
  provider to the new model, including OpenRouter agents used by current chat threads. OpenRouter
  models are capability-checked before the setting is saved. The default effort still applies only
  to newly created projects.

### Fixed

- Existing `.rbxl` files are no longer rebuilt and overwritten when Studio is reopened. Rojo builds
  a place only when the target place file does not exist.
- NVIDIA and OpenRouter now retry transient network failures, timeouts, interrupted streams, rate
  limits, and temporary upstream errors with bounded exponential backoff. Completed Studio tool calls
  are not executed twice during recovery.
- Studio screenshots are decoded as image content instead of being dumped as base64 text into the
  run log. The latest screenshot is attached to the next request for a vision-capable model.

- OpenRouter coding-agent turns now tolerate longer bursts of pre-stream 429/502/503/504 responses,
  retrying with cancellable exponential backoff for roughly 30 seconds before failing the run.
- OpenRouter streaming deltas now update one transient live bubble per assistant turn. Only the final
  turn is persisted, reload filters legacy partial events, and text on either side of tool calls stays
  in separate messages.
- OpenRouter credential deletion now propagates secure-store failures, verifies absence, clears the
  session fallback only after success, and reports an environment key that remains active. Connection
  tests now distinguish invalid keys, missing keys, network failures, timeouts, and upstream failures.
- OpenRouter budgets are checked before every request without an extra final-answer call, cap
  `max_tokens` from conservative remaining-cost estimates, and fail closed when pricing or usage is
  insufficient to bound another paid request.
- Model compatibility is validated on create, update, start, restart, resume, and approved correction
  runs. A live refresh catches models removed after selection, and backend validation does not trust
  curated UI entries.
- OpenRouter HTTP streaming now has a default timeout, bounded safe error reads, cancellation-aware
  retry backoff, strict completion/tool-call validation, actual fallback-model accounting, and no raw
  reasoning content in events or persistence.
- Empty pending-decision snapshots now serialize as `[]` instead of `null`, preventing the Runs view
  from crashing when no operator decision is waiting.

### Removed

- The Codex CLI provider. It no longer runs, is not discovered by `studioforge doctor`, has no
  executable-path setting (`codex_path` is gone from Settings and from the app-settings whitelist),
  reads no `CODEX_HOME`, and cannot be selected for a new run or agent — `Scheduler.Diagnose` reports
  it unconfigured and `Scheduler.Submit` rejects it. Runs saved earlier with `provider="codex"` are
  not migrated, rewritten, or deleted: they remain fully readable in history, serialize normally, and
  are shown in the UI with a **Legacy provider** badge ("This run used a removed provider and is
  read-only history."). Restart and Resume on a legacy Codex run now return a controlled 409
  (`this run used the removed "codex" provider and is read-only history; it cannot be restarted or
  resumed`) instead of attempting to relaunch a CLI that is no longer wired up.

## [0.3.0-alpha.1] - 2026-07-20

### Added

- Runs are now linked to the git checkpoint taken before they started. Every checkpoint
  `gitcheckpoint.Checkpoint` commits (before a non-plan Claude run and before a scheduled correction
  run) is persisted as a `checkpoints` row once its run exists, so `GET /api/v1/runs/{id}/diff` diffs
  against that exact commit instead of the generic `HEAD` whenever one is recorded. A new
  `POST /api/v1/runs/{id}/rollback` non-destructively restores a run's checkpoint commit onto a new
  `studioforge/rollback-<timestamp>` branch (`gitops.Client.SafeRollback` - the run's own branch and
  history are never touched, reset, or force-pushed); it refuses with 409 while the project's write
  lease is held by another run, and with 400 when the run has no recorded checkpoint. `GET
  /api/v1/projects/{id}/git/status` and `POST /api/v1/projects/{id}/git/tag` expose the rest of
  `internal/gitops`, which previously had no HTTP endpoint. The chat diff panel shows a "Roll back to
  before this run" action, with an explicit confirmation step, whenever the current run has a
  checkpoint.
- Stuck-run escalation: a Claude run that genuinely stalls — its provider goes completely silent,
  or it loops the same Studio MCP tool-call cycle without making progress — now pauses itself and
  asks the operator to continue or stop instead of running away unattended. On by default
  (`stuck_detection_enabled`), with a per-agent `stuckDetectionDisabled` opt-out. Two checks, either
  of which trips it: no provider event at all (no streamed text, no tool call, no tool result) for
  `stuck_idle_seconds` (default 600 — long local builds and playtests legitimately produce silence,
  so anything under ten minutes is not treated as a stall), or the same short tool-call cycle
  repeating `stuck_repetition_cap` times in a row with no file edit and no newly distinct
  console/tool-result text in between. Wall-clock duration and event counts are deliberately not
  signals: an actively streaming run is working, however long it takes, and never trips either
  check. It reuses the existing `waiting_decision`/`studioforge-question` machinery end to end — no
  new event type or run status — so the escalation renders as an ordinary question card, live and
  after a reload, with a short summary of what the run was doing and its recent console/playtest
  observations. Clicking **Continue testing** resumes the exact same CLI session with stuck
  detection suppressed for that resumed run — the operator said keep going, so it will not re-ask;
  typing a free-text reply also resumes the session, with detection still armed. A distinct
  **Stop** button cancels the run cleanly, which also works for any other run parked in
  `waiting_decision` (including the agent's own natural question), both from the chat and from the
  Activity view. A run's `stuckEscalated` field records that its own termination was this
  escalation (migration `009_stuck_detection.sql`).
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
- Recent Rojo live-sync log lines, surfaced on the project Overview. A live-sync session
  (`rojo.Session.RecentLines`) now keeps its own bounded buffer (the last 100 lines) of its `rojo serve`
  output, folded into `models.SyncStatus` the same way the project payload already carries port and
  active state; the Overview's new **Rojo live-sync session** panel shows running/stopped, the
  allocated port, and those recent log lines. Added a regression test proving a session the operator
  never explicitly stopped still dies on daemon shutdown (`processes.Supervisor.Close`), the actual
  restart/shutdown path, distinct from the existing explicit-Stop-then-Close test.
- Operator decisions, scoped to one producer: when the playtest validation loop's correction budget
  is exhausted, StudioForge now proposes a `Decision` instead of silently giving up, shown as an inline
  Approve/Dismiss banner on the Runs view. Approving (`POST /api/v1/decisions/{id}/resolve` with
  `{"approve": true}`) submits the exact proposed correction run through the normal scheduler — same
  writer lease, same budget ceiling, same Git checkpoint; denying schedules nothing. This is a fresh,
  narrower `decisions` table (migration `008_decisions.sql`) than the one removed early in the alpha,
  and is not a general pre-action approval gate — it never pauses a run before a file edit, a
  destructive command, or a publish.

### Changed

- Pause is now honest. It previously only stopped the daemon from draining the provider's events
  while the agent process kept running — still spending tokens and editing files behind a row that
  already claimed `paused`. Pausing a run now performs a controlled cancel: it stops the provider
  process by the same safe path Cancel uses, records the real usage and the provider session,
  releases the project write lease, and only then writes `paused`, so the store is never told
  `paused` while the agent is still working. A paused run stays resumable — its saved session means
  the next message in the thread continues it, and `POST /api/v1/runs/{id}/resume` now submits a
  fresh continuation run that resumes that session rather than un-parking a still-live process.
  Paused runs survive a daemon restart (`RecoverInterrupted` leaves them untouched) and stay
  resumable, and cancelling a paused run works. The web UI now treats `paused` as a state whose live
  process has stopped (the elapsed timer stops) while keeping the Resume action available. Because
  the interrupted turn's partial edits stay on disk, a resume is a new turn continuing the same
  conversation, not a byte-for-byte revival of the killed process.
- SQLite is now the single source of truth for a run's lifecycle state. The scheduler publishes a
  run's status event only after the store write that records it succeeds; if that write fails it no
  longer publishes a false `completed`/`failed`/`cancelled`/`paused` status. Instead it publishes a
  distinct `scheduler.storage_error` infrastructure-failure event (logged with the run and project
  id) and leaves the row at its last persisted status, which `RecoverInterrupted` recovers as
  `interrupted` on the next daemon restart. The web UI treats `scheduler.storage_error` as ending the
  run so it never hangs on "Working…" after a storage failure, and `scheduler.transition` now returns
  its write error to callers instead of swallowing it.
- The scheduler's stuck-escalation question now offers two options, **Continue testing** and
  **Stop here**, to satisfy the same 2-4-option `studioforge-question` contract now enforced for
  every question card (see Fixed). Its behavior is otherwise unchanged: **Continue testing** still
  resumes the session with detection suppressed, and free text still resumes it with detection armed.

### Removed

- The dead `assets`/`asset_reviews` tables (migration `010_drop_assets.sql`), left over from the
  asset-quarantine feature whose Go package, view, route, and i18n strings were already removed in an
  earlier commit; nothing in this codebase read or wrote either table. This does not touch the
  official Studio MCP tools `insert_asset`/`search_asset` (`internal/roblox/mcp/config.go`'s
  `workspaceTools` allowlist) — those were never part of the removed package and remain fully
  functional.

### Security

- Stored run event payloads (`internal/database/runs.go`'s `AppendEvents`, the single write path for
  every persisted `run_events` row) are now passed through `security.Redact` before being written,
  the same pattern-based redaction previously applied only inside the `studioforge doctor --bundle`
  diagnostic archive. Redaction runs on the payload's decoded value tree (each string leaf, not the
  already-marshaled JSON text), so a match can never land on JSON structural characters and corrupt
  the stored payload. Each string leaf is also checked against its own JSON key name
  (`security.IsSensitiveKey` — `api_key`, `token`, `secret`, `password`, and compound forms like
  `access_token`) and redacted wholesale when the key looks sensitive, not only when the value's own
  text happens to match one of the content patterns — a secret parsed out of JSON no longer carries
  the surrounding `key=`/`key:` text a plain regex needs to fire on, so key-name awareness is what
  actually catches it in this shape.

### Fixed

- Roblox Studio launching a duplicate window when the already-open Studio's MCP connection is owned
  by another client (Claude Desktop, Cursor, or a lingering session). Roblox grants the plugin's WS
  host slot to one client at a time, so a held slot makes the launcher list *no instances with no
  error* — indistinguishable from no Studio at all. The provisioner's auto-open and the manual
  **Open Studio** button both read that empty listing as "safe to launch" and stacked a new window;
  both now run the same running-process tie-break the rest of the provisioner already used
  (`Provision`/`Status`) and withhold with the host-taken notice instead of launching.
- Roblox Studio launching a second, duplicate window on top of one already open. Two causes, both
  fixed: (1) the Studio MCP provisioner's auto-open used to fire whenever *no instance matched* the
  project, even if some other Studio instance was already open — it now only opens Studio when no
  instance is open at all, and otherwise withholds with a notice naming what is actually open next to
  what was expected; (2) `studio.Opener` (shared by the provisioner's auto-open and the manual **Open
  Studio** chat button) now tracks in-flight opens per place name and refuses to relaunch one already
  underway for 90 seconds — long enough to outlast the provisioner's own 45-second wait for a freshly
  opened Studio to appear, so a run giving up on that wait no longer reads as "nothing happened" and
  triggers a second launch. The manual button also checks whether Studio is already open for the
  project before launching (`POST /api/v1/projects/{id}/open-studio` now reports `alreadyOpen: true`
  instead of relaunching, or refuses with `409 studio_mismatch` when other instances are open but none
  match), reusing the same decision the provisioner's auto-open makes. Creating a project with
  **Open Studio** checked now goes through the same check, closing a third path that previously
  launched unconditionally.
- The run Cancel button failing with "Failed to fetch" partway through a long, heavy run.
  `scheduler.Manager.Cancel` used to hold its single mutex across a synchronous DB write and the
  blocking process-kill call (`taskkill.exe /T` on Windows), while the run's own goroutine
  reacquired that same mutex on every streamed event — real head-of-line contention on a run
  streaming hundreds of events, long enough for the browser's fetch to fail at the transport level.
  `Cancel` now only cancels the run's own context while holding the lock and returns immediately;
  the "cancelling" transition and the actual process termination (`RunHandle.Cancel`/`Wait`) now run
  inside the run's own goroutine, off the shared mutex. `POST /api/v1/runs/{id}/cancel` now responds
  `202 Accepted`. This also surfaced (and fixes) a related latent race: a cancelled run's context
  expiring at the same instant its event stream closed could make the run loop's `select` take the
  "stream ended" branch instead of the cancellation branch, silently dropping the terminal DB write
  and leaving the run stuck at `running` forever; the run loop now checks the context itself once the
  stream ends, not only which `select` case happened to fire. A run cancelled while it was in this
  post-stream tail (e.g. mid-playtest-validation) now correctly lands on `cancelled` instead of
  stubbornly finishing as `completed`.
- A race starting two processes with the same ID in `processes.Supervisor`. The ID-uniqueness check
  and the child-process start were not atomic, so two concurrent `Start` calls for the same ID could
  both launch a child and clobber each other's map entry, leaking a supervised process; a slow reaper
  could also delete a newer process's entry when an older same-ID process exited. IDs are now reserved
  atomically before the (slow, unlocked) start, a failed start clears the reservation, and the reaper
  deletes a map entry only when it still refers to the same process instance.
- The post-run Studio playtest validation continuing to drive Studio after the project write lease
  was lost. The separate validation heartbeat only logged the loss and stopped renewing while the
  validator kept running. Validation now runs under its own cancellable context that is cancelled the
  instant the lease is lost; it waits for the validator to stop, records the outcome as `inconclusive`
  with an infrastructure notice (never `passed`), and schedules no correction run and proposes no
  operator decision.
- A correction run's Git checkpoint could be left orphaned. The checkpoint was committed before the
  correction run was created, so a failed admission (provider not configured, scheduler closed, or a
  create-run error) left a commit belonging to a run that never existed. A correction run is now
  created first, its checkpoint is bound to that run id and recorded, and only then is it admitted to
  the executable queue; a checkpoint that genuinely fails aborts the correction (marked `failed`,
  never run) instead of running it as if a rollback point still existed, and correction scheduling is
  idempotent (keyed on the parent run) so a retry never creates a second checkpoint.
- Question-block (`studioforge-question`) validation is now enforced on the backend, matching the
  frontend, instead of trusting the frontend alone. A fenced block parks a run in `waiting_decision`
  only when it is the message's single block, its JSON body is within an 8 KB cap, its question is
  non-empty (≤2000 chars), and it carries 2-4 options with non-empty, length-bounded (≤120 char
  label, ≤600 char description), unique labels; a malformed, oversized, or multi-block message is
  treated as ordinary text and never parks the run.
- The Chat view no longer silently drops "Studio MCP withheld" notices (e.g. ambiguous or mismatched
  Studio instances) — they now render as a visible banner instead of only being discoverable in the
  raw run event log. A run-action ("Stop"/pause/resume/restart) that fails on a genuine network error
  now shows a retryable message with a **Retry** button instead of the raw browser "Failed to fetch"
  text, in both languages.
- Reopening a thread after restarting the daemon could show a dead run as still "Working…" with a
  live, ever-growing elapsed timer and an active Stop button. The backend already recovers any run
  left `starting`/`running`/`cancelling` when the daemon stops to `interrupted`
  (`Store.RecoverInterrupted`, run at every startup), but the web UI's own terminal-status set
  (`isRunTerminal` in `web/src/lib/runStatus.ts`) never included `interrupted`, so it kept picking
  that recovered run back up as the thread's "active" run. `interrupted` is now terminal there too.

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
