# Known limitations

## Implemented in code, but not reachable from the running product

These packages are written and covered by unit tests, but nothing in the API or app layer calls them, so a user cannot reach them. They are listed here rather than presented as features. Wiring or removing them is the first item on the [roadmap](ROADMAP.md).

- **Project memory** (`internal/memory`) is a SQLite FTS5-backed store with `Put`/`Search` and its own migrations, and it has no non-test caller. No run writes or reads a memory entry, and the `Memory` field in prompt assembly is never populated. StudioForge does not currently carry memory across runs.
- **Task dependency graphs** (`internal/tasks`) include cycle detection that is never called outside its test. The task-creation endpoint accepts no `dependencies` field, so a real project cannot create a dependency; the only `task_dependencies` rows come from the `--mock` demo seed.
- **Git status, diff, rollback, and tag** (`internal/gitops`) are implemented and tested but exposed by no HTTP endpoint. The separate Git checkpoint taken before each non-plan Claude run is wired and does work.
- **Asset quarantine** (`internal/roblox/assets`) is a status-transition validator with no caller. There is no asset scanning, upload handling, or Marketplace integration, and the Assets view in the interface is an empty placeholder with no API call behind it.
- **Rojo live-sync sessions** — the `rojo serve` start/stop/log lifecycle and per-project port allocation are unit-tested, but no endpoint starts or queries a session. Only the Rojo *build and open* path is reachable.
- **Decisions** have a record type, a resolve endpoint, and a review interface, but no live run produces one; only the demo seed inserts them. The Decision mechanism must not be relied on as a safety gate in this release.
- **Playtest and review result types** are defined in prompt assembly but are never constructed or parsed. There is no automated playtest validation or self-correction loop.
- **The structured prompt template** (`internal/prompts.Assemble`) has no caller outside its own test. The system prompt actually sent to a provider is a simpler concatenation built in `internal/api`: the agent's stored system prompt plus the two static `.agent` context files. The richer multi-section template — including its memory, playtest, and review sections — is not what runs.
- **The Studio service type** (`internal/roblox/studio.Service`) has no live caller; the studio-bind endpoint uses the database store directly.
- **The path traversal and symlink-escape guard** (`projects.PathGuard.Resolve`) has no caller. No endpoint currently accepts a project-relative path, so registration-time canonicalization is the containment that actually runs. The guard is ready for the first endpoint that needs it.
- **Secret redaction** runs only in the diagnostic-bundle writer. Application logs and stored run transcripts are not redacted.

## Operational limitations

- Same-project multiple writers are intentionally disabled; one writer lease protects each project. Read-only parallel analyzers can be added without changing the resource contract.
- Studio MCP capabilities vary with Roblox Studio. The adapter and mocks are tested, but a real playtest needs an open, MCP-enabled Studio.
- Studio access is granted to a run only when exactly one Studio instance is open. Claude Code runs its own MCP client, and `set_active_studio` is per-connection state, so StudioForge cannot pin the instance on the agent's connection from outside; the launcher accepts no instance-selection argument either. With several Studios open, access is therefore refused rather than guessed at, and the run continues without Studio. `mcp.Client.SelectStudio` governs StudioForge's own connection only. Real Studio instances are also not yet discovered into the Studio Sessions view; the rows shown there are demo data.
- Studio access applies to Claude runs. The Codex adapter has no `--mcp-config` equivalent, so Codex agents cannot reach Studio.
- Studio tools are auto-approved by agent permission profile: `read-only` gets observation only, `workspace-write` adds tools that change the open place, and tools reaching past it (`upload_image`, `store_image`, `http_get`, `user_keyboard_input`, `user_mouse_input`) require `danger-full-access`.
- `--max-turns` does not exist in current Claude Code, so an agent's max-turns limit is dropped by the capability gate and does not bound a Claude run. Budget ceilings still apply.
- A Claude run inherits the operator's own Claude Code configuration — `CLAUDE.md`, hooks, plugins and skills — which is billed to every run and makes behaviour depend on the local install. `--strict-mcp-config` isolates the run from the operator's other MCP servers, but only when Studio access was granted, since it is emitted alongside `--mcp-config`; a run without Studio access inherits those servers too. Nothing isolates the rest. Claude Code's `--bare` would, yet it requires `ANTHROPIC_API_KEY` and cannot use OAuth or subscription authentication.
- Claude flags are capability-gated. A fake CLI covers stream, malformed, auth, rate-limit, budget, crash, and resume mechanics; actual model availability/cost remains account-specific.
- Codex uses the documented non-interactive JSONL CLI contract and saved CLI authentication. A fake CLI verifies event normalization and failures; actual account quotas and model availability remain account-specific. The Codex executable bundled inside a Windows Store app may not be launchable by another process, so a separately installed CLI path can be configured.
- The portable project archive contains metadata, agents, and tasks. It does not copy source and requires an existing root on import.
- The macOS package is unsigned until a maintainer supplies Apple signing/notarization credentials. The Windows package is likewise an unsigned development build.
- Windows Credential Manager and macOS Keychain are adapter boundaries in v1; Claude authentication remains owned by Claude Code, so no Anthropic token is stored.
- Asset quarantine state and review contracts are implemented, while automated Marketplace insertion/playtest requires a live Studio MCP session.
- Detailed run-event retention is schema-ready but currently relies on manual database maintenance; no automatic pruning is enabled in this release.

---

# Известные ограничения (Русский)

- Несколько writers одного проекта намеренно отключены: один writer lease защищает каждый проект. Параллельные read-only analyzers можно добавить без изменения resource contract.
- Возможности Studio MCP зависят от Roblox Studio. Адаптер и mock протестированы, но реальный playtest требует открытой Studio с включённым MCP.
- Доступ к Studio выдаётся запуску, только если открыт ровно один instance. У Claude Code собственный MCP-клиент, а `set_active_studio` — состояние соединения, поэтому StudioForge не может закрепить instance на соединении агента извне; аргумента выбора instance у launcher тоже нет. При нескольких открытых Studio доступ не выдаётся, а не угадывается, и запуск продолжается без Studio. `mcp.Client.SelectStudio` управляет только собственным соединением StudioForge. Реальные instance пока не попадают в раздел Сессии Studio: показанные там строки — демо-данные.
- Доступ к Studio работает для запусков Claude. У адаптера Codex нет аналога `--mcp-config`, поэтому агенты Codex до Studio не дотягиваются.
- Инструменты Studio авто-одобряются по permission profile агента: `read-only` только наблюдает, `workspace-write` добавляет изменение открытого place, а инструменты за его пределами (`upload_image`, `store_image`, `http_get`, `user_keyboard_input`, `user_mouse_input`) требуют `danger-full-access`.
- Флага `--max-turns` в текущем Claude Code нет, поэтому лимит max-turns отбрасывается capability-гейтом и не ограничивает запуск Claude. Бюджетные лимиты продолжают действовать.
- Запуск Claude наследует конфигурацию Claude Code самого оператора — `CLAUDE.md`, hooks, плагины и skills. Это оплачивается в каждом запуске и ставит поведение в зависимость от локальной установки. `--strict-mcp-config` изолирует запуск от остальных MCP-серверов оператора, но только когда доступ к Studio выдан, поскольку добавляется вместе с `--mcp-config`; запуск без доступа к Studio наследует и эти серверы. Остальное не изолирует ничто. `--bare` изолировал бы, но требует `ANTHROPIC_API_KEY` и не работает с OAuth и подпиской.
- Флаги Claude зависят от discovery возможностей. Fake CLI покрывает stream, malformed output, auth, rate-limit, budget, crash и resume; доступность модели и стоимость зависят от аккаунта.
- Codex использует документированный non-interactive JSONL CLI contract и сохранённую CLI-авторизацию. Fake CLI проверяет нормализацию событий и ошибки; реальные квоты и модели зависят от аккаунта. Executable Codex внутри Windows Store app может быть недоступен другому процессу, поэтому можно задать путь к отдельно установленному CLI.
- Portable archive содержит metadata, agents и tasks. Он не копирует исходники и при импорте требует существующий root.
- Пакет macOS не подписан, пока мейнтейнер не предоставит данные Apple для signing/notarization. Windows-пакет также является неподписанной development-сборкой.
- Windows Credential Manager и macOS Keychain — границы адаптеров v1; auth Claude остаётся у Claude Code, поэтому токен Anthropic не хранится.
- Состояния asset quarantine и review contracts реализованы, но автоматическая вставка Marketplace/playtest требует живой Studio MCP session.
- Политика хранения подробных run events подготовлена схемой, но сейчас зависит от ручного обслуживания базы; автоматического удаления в этом релизе нет.
