# StudioForge guide (English)

> **Public beta.** Check the [GitHub Releases page](https://github.com/10kkyvl/studioforge/releases) for the current build. Some features described below are implemented in code but not yet reachable from the running app; each such case is marked explicitly. See [Known Limitations](../KNOWN_LIMITATIONS.md) for the full verification matrix.

## Installation

### Windows 10/11 amd64

Download the `windows-amd64.zip`, verify its SHA-256 against `SHA256SUMS.txt`, extract it to a user-writable directory, and run:

```powershell
./studioforge.exe --mock
```

Unsigned development builds may trigger SmartScreen. Verify the checksum and publisher source before choosing **More info → Run anyway**. A signed release should be preferred when available.

### macOS 12+ Apple Silicon

Download `macos-arm64.zip`, verify with `shasum -a 256`, extract, and open `StudioForge.app`. For an unsigned development build, use Control-click → Open once. Do not run commands that globally disable Gatekeeper. The archive contains no Node.js runtime.

From source, install Go 1.25+, Node.js 22+, npm, and Git, then run `./scripts/dev.ps1` on Windows or `./scripts/dev.sh` on macOS.

## First run

The wizard checks the data directory, database, Git, Claude Code/authentication, an OpenRouter API key's verification state, Rojo, and the official Studio MCP launcher. Each item shows the detected version/path (or, for OpenRouter, key state and catalog reachability) and remediation. **Open dashboard** records completion; Doctor remains available in Settings.

`--safe-mode` disables provider workers, MCP, and Rojo. Data, backups, exports, and diagnostics remain available. `--mock` seeds three isolated demo workspaces and exercises the production domain/API.

## OpenRouter

OpenRouter is a fundamentally different provider from Claude Code: it is an HTTP API, not a local CLI, and StudioForge drives it with its own in-process bounded tool loop rather than execing a subprocess. It needs an API key — **required even for free models** — set either as the `OPENROUTER_API_KEY` environment variable or saved in **Settings → Agents and integrations → OpenRouter**:

```powershell
$env:OPENROUTER_API_KEY = "sk-or-..."
```

However you set it, the key is written to the OS secure credential store (Windows Credential Manager / macOS Keychain), with an environment-variable and session-only fallback when that store is unavailable. It is never written to SQLite, run events, application logs, or the diagnostic bundle. `studioforge doctor` and the Settings card report only the key's verification state (`not_configured`/`unverified`/`configured`/`invalid`) and source — never the key itself.

The model picker is backed by OpenRouter's public Models API, cached for 6 hours with a manual refresh, a last-good-cache fallback, and a bundled dated snapshot as a last resort. It shows tool, vision, context, free, and verification capabilities. Known non-tool models are rejected; unknown, stale, and dynamically routed IDs such as `openrouter/free` require an explicit per-model compatibility confirmation, and the backend refreshes the catalog again before execution. A curated shortlist (Free automatic, Free recommended, Best coding, Balanced, Fast and cheap, Strong reasoning, Large context) sits above the full catalog. **Free models are less stable** — more variable quality/latency, lower rate limits, availability that can change — and suit small tasks better than long unattended runs; StudioForge never silently falls back to a paid model from free mode. Runs execute local workspace tools (list/read/search/grep/create/edit/patch/mkdir/git/run_command, gated by the agent's permission profile) and Roblox Studio MCP tools through a live per-run client, persist only completed assistant turns per chat thread while streaming one live bubble, and enforce a conservative budget gate before every model request.

## NVIDIA NIM

Add an NVIDIA API key in **Settings → Agents and integrations**, then choose an NVIDIA model for an
agent. Temporary network failures, timeouts, rate limits, and interrupted streams are retried
automatically. Vision-capable models such as Kimi K2.6 receive pasted images and the latest image
returned by Studio's `screen_capture`; text-only models receive no hidden screenshot payload.

Messages sent while an agent is running wait in the same chat queue and continue that conversation
in order. Opening Studio preserves an existing saved `.rbxl`; Rojo is used only to create a missing
place file, never to silently replace saved Studio work.

## Claude Code

Install and authenticate Claude Code using Anthropic's current official instructions. Verify independently:

```powershell
claude --version
claude auth status
```

StudioForge reads `claude --help` and enables only observed flags. Runs use print mode, stream JSON, reduced environment variables, bounded turns/budget, safe permission mode, and optional generated MCP configuration. Unsupported flags are omitted. Authentication stays in Claude Code; StudioForge does not store the token.

## Roblox Studio MCP

Update Roblox Studio, open **Assistant → … → Manage MCP Servers**, and enable **Studio as MCP server**. Official launchers:

- Windows: `cmd.exe /c %LOCALAPPDATA%\Roblox\mcp.bat`
- macOS: `/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP`

StudioForge discovers actual MCP tools and fails clearly when a required capability is absent. Studio access is fail-closed: a run is granted Studio access only when exactly one Studio instance is open — for both Claude and OpenRouter runs, through two different mechanisms (a generated `--mcp-config` for Claude, a live per-run MCP client for OpenRouter's in-process loop) sharing the same fail-closed decision and tool allowlist. Claude Code and OpenRouter's own loop each run their own MCP client, so StudioForge cannot pin an instance on the agent's connection from outside, and the official launcher accepts no instance-selection argument — with several Studios open, access is refused rather than guessed, and the run continues without Studio. The **Studio sessions** view discovers real open Studio instances and lets you bind one to a project, but discovery is manual — a **Refresh** click (`POST /api/v1/studio/sessions/refresh`) — not an automatic background poll, since every probe spawns a launcher process that competes with a running agent for Studio's single WS host slot. Under `--mock`, refresh is a no-op and the seeded demo rows are unaffected. One instance is an exclusive resource for modifying/playtest operations.

**Not implemented in this beta** (see [Known Limitations](../KNOWN_LIMITATIONS.md)): the intended design is a playtest contract of select instance → read state → start → simulate input → collect console/screenshots → stop → structured result → bug tasks. StudioForge does not automate playtesting or capture screenshots on its own today.

## Rojo

Install Rojo 7 CLI and the Studio plugin from official Rojo documentation. Verify:

```powershell
rojo --version
```

Each project selects a `*.project.json`. StudioForge invokes Rojo to build a place file and open it in Studio. Live-sync is also wired: `POST`/`DELETE /api/v1/projects/{id}/sync` start and stop a `rojo serve <file> --port <unique-port>` session, supervised as a subprocess with streamed stdout/stderr, duplicate-session refusal, and stop/restart, with status/port/recent logs shown in the Overview view and start/stop available from the chat view. It requires a `default.project.json` at the project root and only carries files into an already-open Studio — the Rojo Studio plugin still needs a one-time manual Connect inside Studio each session. A VS Code extension does not replace the CLI.

## Projects and agent teams

Register an existing directory or create a new one. StudioForge stores its canonical path/fingerprint; it does not copy source into application data. Every project receives a default agent, including older registered projects that had none. The **Team builder** can create, edit, enable/disable, and launch agents with Claude Code, OpenRouter, or mock providers. A version-controlled `.agent/` folder may contain a `constitution.yaml` and a `requirements.md`; StudioForge reads exactly these two files verbatim and prepends them to every run's system prompt. Nothing else under `.agent/` (architecture notes, prompts, skills, or memory) is read today. Runtime transcripts and usage remain in SQLite.

Demo projects have separate orchestrator, builder, and verifier agents. Provider/model aliases (`fast`, `balanced`, `reasoning`, `premium`) are domain values; adapters map them. Permission profiles, concurrency, runtime, turns, and budget are per agent.

**Settings → Agents and integrations** controls the default provider/model/effort, global concurrency, and executable overrides for Claude Code, Rojo, Git, and Roblox Studio MCP, plus the OpenRouter API key (no executable path — it's an HTTP API). Empty executable fields use PATH or platform discovery. Changes apply immediately and the integration cards show the effective path/version (or key state, for OpenRouter), authentication state, and remediation.

## Multi-project concurrency

The scheduler is round-robin across project queues. Different projects can hold writer leases simultaneously. A project has one writer by default; same-project writers wait on `project:<id>:write`. Resources are sorted and acquired atomically to prevent deadlock. Provider/model/global/project ceilings are checked before dispatch. Events are persisted before SSE publication.

Pause and resume are cooperative at event boundaries. Cancel terminates the provider handle/process tree. Runs active during daemon failure become `interrupted`; restart creates a new auditable run. Histories, agents, tasks, usage, and budgets are filtered by `project_id`. (A project-scoped memory store — SQLite full-text search with Put/Search — now writes one entry per completed run and surfaces up to five relevant past entries into the next run's system prompt; it is a minimal wiring, not a summarized or curated memory.)

## Permissions and safety

- Keep the default loopback listener. `--unsafe-host` is an explicit escape hatch, not remote access hardening.
- **There is no general operator-approval gate before a dangerous action** — the `Decision` record type, resolve endpoint (`POST /api/v1/decisions/{id}/resolve`), and review UI are scoped to exactly one producer, an exhausted playtest-correction budget; nothing pauses a run before an arbitrary file edit, destructive command, or publish. The interactive-question feature (`studioforge-question`) covers an agent pausing mid-run for input, not a general approval gate before a destructive action.
- **Git rollback and tag are wired:** `POST /api/v1/runs/{id}/rollback` (`internal/gitops.SafeRollback`, rolling back to a `studioforge/rollback-<timestamp>` branch at a verified commit, never force-resetting or removing untracked files) and `POST /api/v1/projects/{id}/git/tag` (`internal/gitops.Tag`) are both wired end to end, with a confirm dialog in the chat view. Rollback needs a stored checkpoint for that run and is refused while a run holds the project's write lease. `Status` and `DiffHead` are also wired — the chat view shows a completed run's diff against its checkpoint commit. (StudioForge auto-commits a Git checkpoint before every non-plan Claude run, which is what rollback and the diff panel use.)
- Canonical path and symlink checks prevent adapters from escaping registered roots.

## Backups, export, and import

StudioForge creates an automatic SQLite backup at most once per 24 hours and offers **Create backup** in Settings. Backups use SQLite `VACUUM INTO` while the database is open.

```powershell
studioforge export --project PROJECT_ID --output project.zip
studioforge import --file project.zip
studioforge import --file project.zip --apply --path C:\existing\project
```

Portable export contains project metadata, agents, and tasks—not source. Task dependencies (create a task with a `dependencies` field naming other task IDs in the project; a cycle is rejected) are included with the tasks they belong to, though run execution does not yet check whether a task's dependencies are finished. Import always previews missing paths and conflicts before `--apply`.

## Troubleshooting

- **Another instance:** use the already-running window or stop that process cleanly. A stale PID lock is removed on next start.
- **Blank UI:** ensure release assets were built before Go; official builds embed them. Check browser console and `/api/v1/health`.
- **401 after copying URL:** bootstrap tokens are one-use. Start the daemon again or use the browser session originally opened.
- **Claude missing/auth warning:** run `claude --version` and `claude auth status`; update/re-authenticate through Claude Code.
- **OpenRouter key not configured/invalid:** add a key in **Settings → Agents and integrations → OpenRouter** (or set `OPENROUTER_API_KEY`) and click **Test connection**; an `invalid` state means OpenRouter rejected the stored key, so generate a new one.
- **Restart/Resume fails on an old run:** a run saved with `provider="codex"` from before the Codex CLI provider was removed stays readable as history (shown with a **Legacy provider** badge) but cannot be restarted or resumed.
- **Studio ambiguous:** Studio access is granted only when exactly one instance is open; close the extra Studio windows, leave a single instance open, and retry. (Use **Refresh** in the Studio sessions view to re-scan real instances and **Bind project** to attach one manually; discovery is not automatic.)
- **Rojo unavailable:** install CLI, confirm `rojo --version`, select a `.project.json`, and verify the port is not blocked.
- **Database:** run `studioforge doctor --bundle diagnostics.zip`; restore only from a known-good backup while the daemon is stopped.

## Keyboard and accessibility

Use Tab/Shift+Tab across all controls. Focus rings are always visible. Alt+1…Alt+9 moves between the first nine navigation sections. Tables scroll at narrow widths, cards collapse to one column, and the event log keeps only a bounded visible window while all events remain persisted.

## Known limitations

See [Known Limitations](../KNOWN_LIMITATIONS.md) for the verification-specific platform and integration matrix.

---

# Руководство StudioForge (Русский)

Полная русская инструкция находится в [docs/ru/README.md](../ru/README.md). Ниже приведено её краткое содержание для читателей этой двуязычной страницы.

## Установка и запуск

Для разработки требуются Go 1.25+, Node.js 22+, npm и Git. Выполните `./scripts/dev.ps1 --no-open` в Windows либо `./scripts/dev.sh --no-open` в macOS/Linux. Для готового Windows archive распакуйте zip и запустите `studioforge.exe --mock`; в macOS распакуйте arm64 archive и откройте `StudioForge.app`.

Wizard проверяет каталог данных, SQLite, Git, Claude Code/auth, OpenRouter (наличие ключа и каталог моделей), Rojo и официальный Studio MCP launcher. `--safe-mode` отключает workers и внешние инструменты, а `--mock` создаёт три независимых demo workspace. Runtime не требует Node.js.

## OpenRouter, Claude, Studio MCP и Rojo

StudioForge работает с OpenRouter через HTTP API с собственным встроенным agent-циклом; ключ OpenRouter хранится в системном хранилище учётных данных ОС (Windows Credential Manager / macOS Keychain), а не в SQLite, и задаётся в Настройках. StudioForge читает `claude --help` и добавляет только доступные flags. Токены Claude не сохраняются. Пути к Claude, Rojo, Git и Studio MCP задаются в **Настройки → Агенты и интеграции** и применяются сразу. Для Studio MCP включите **Studio as MCP server** в Roblox Studio: доступ выдаётся run'у только когда открыт ровно один экземпляр Studio (StudioForge не может закрепить instance на чужом MCP-соединении), а при нескольких открытых Studio доступ просто не выдаётся. Экран **Studio sessions** обнаруживает реальные открытые instances Studio и позволяет привязать один к проекту, но обнаружение запускается вручную кнопкой **Обновить**, а не фоновым опросом (в режиме `--mock` эта кнопка — no-op, demo-строки не меняются). Сборка (build) Rojo доступна из приложения; live-sync тоже подключён — `POST`/`DELETE /api/v1/projects/{id}/sync` запускают и останавливают сессию `rojo serve <file> --port <unique-port>`, доставляя файлы в уже открытую Studio, где плагину Rojo всё равно нужно один раз за сессию нажать Connect вручную — см. [Known Limitations](../KNOWN_LIMITATIONS.md).

## Эксплуатация

Очередь честно чередует проекты; второй writer одного проекта ждёт `project:<id>:write`. Pause/resume выполняются между событиями, cancel завершает provider/process tree, interrupted runs доступны для restart. Backup использует SQLite `VACUUM INTO`; portable export не копирует source и всегда preview-ится перед import.

## Проверка и доступность

Запускайте `./scripts/test.ps1` и `go test -race ./...`. Используйте Tab/Shift+Tab; focus всегда видим, Alt+1…Alt+9 переключает первые девять разделов, а responsive layout корректно сжимается. Ограничения и фактическая platform matrix находятся в [Known Limitations](../KNOWN_LIMITATIONS.md).
