# Testing guide

Full Windows verification:

```powershell
./scripts/test.ps1
go test -race ./...
```

The suite covers migrations/pragmas, isolation, default-agent repair and agent CRUD, runtime settings, write bursts, backup, budgets, scheduler fairness and state machines, lease cancellation/deadlock, multi-project runs/recovery, SSE replay/slow clients, Host/Origin/session security, path traversal/symlinks, prompts/redaction, memory FTS/fallback, process termination, Claude stream/failures/resume capabilities, the OpenRouter agent loop (tool execution against a fake `agenttools.Workspace`, streamed/malformed-response handling, budget and image-attachment failure paths), the OpenRouter credential manager (keychain/session/env precedence, save/remove/test-connection), the model catalog (live fetch, TTL expiry, last-good-cache fallback, embedded snapshot), conversation persistence and history sanitization/compaction, legacy-Codex-run read-back and the removed-provider restart/resume error, Studio binding, Rojo lifecycle, Git rollback, quarantine, and portable export. Browser E2E creates a real project, creates and launches its mock agent, saves integration settings, and checks for console errors.

This pass added deterministic regression tests, with no sleep-based flakiness for the concurrency-sensitive ones: the supervisor duplicate-ID race (`internal/processes`), honest pause together with cancel/pause races and storage-write-failure consistency (`internal/scheduler`), validation aborting to `inconclusive` on write-lease loss, correction-checkpoint ordering and idempotency, and the shared `studioforge-question` contract between the backend's `detectQuestion` and the frontend's `questionCard` (`web/src/lib`).

Vitest verifies i18n parity and the typed API error client. Playwright builds a real Go binary, performs secure first-run exchange, changes locale, starts a run, opens the Tasks DAG (including a task with no dependencies), and visits core screens with console-error assertions.

The Roblox Studio adapter includes an in-process JSON-RPC stdio fixture test. With an MCP-enabled Studio place open, run the opt-in live handshake, capability discovery, and instance-list smoke:

```powershell
$env:STUDIOFORGE_REAL_STUDIO = '1'
go test ./internal/roblox/mcp -run TestRealStudioMCP -v -count=1
```

If the official launcher responds but no Studio instance is connected to its local WebSocket host, the test reports that exact state and skips instead of claiming a live place verification.

CI runs Windows and Linux Go tests on minimum/current Go, the race detector on Linux, Playwright on Linux, a native Windows build, and a native Apple Silicon macOS build/smoke.

## What CI cannot verify

CI has no Roblox Studio, no authenticated Claude account, no configured OpenRouter API key, and no GUI. Everything involving those is exercised against fake CLIs (`testdata/fakes/`) or a fake OpenRouter HTTP endpoint, or skipped. The following therefore has to be checked by hand before a release, and the result recorded in the pull request or release notes.

Run `go test -race ./...` on Linux or macOS; it requires CGO and will not run on a Windows box with `CGO_ENABLED=0`.

## Manual verification checklist

Mark each item pass, fail, or not-tested. Do not report an item as passing unless it was actually run.

### Daemon and interface

- [ ] `studioforge --mock --no-open` starts, prints `STUDIOFORGE_URL` and a bootstrap token, and `/api/v1/health` returns `{"status":"ok"}`.
- [ ] Opening the printed URL authenticates once; reloading without the token still works via the session cookie.
- [ ] `/api/v1/snapshot` without a session returns 401.
- [ ] `studioforge --host 0.0.0.0` is refused without `--unsafe-host`.
- [ ] The embedded SPA is served from the binary with no Node.js present at runtime.
- [ ] Switching locale between English and Russian leaves no untranslated strings on the visited screens.

### Diagnostics

- [ ] `studioforge doctor` reports correct detected paths and versions for Git, Claude Code, Rojo, and the Studio MCP launcher on this machine, and the correct key-verification state and catalog reachability for OpenRouter.
- [ ] `studioforge doctor --bundle diagnostics.zip` produces an archive, and a manual read of its contents shows no tokens, cookies, or credential-shaped strings.

### Claude Code integration (needs an authenticated, billable account)

- [ ] A real run starts, streams events into the interface, and completes with a recorded usage figure.
- [ ] Cancelling a run terminates the `claude` process tree.
- [ ] Resuming a session continues the prior conversation.
- [ ] A Git checkpoint commit exists in the project repository from before the run, and `git diff` against it shows exactly what the agent changed.

### OpenRouter integration (needs a real API key and a small budget)

- [ ] Saving a key in Settings reports `configured` after **Test connection**; deleting it reports `not_configured`.
- [ ] A real run against a free model (`openrouter/free`) and a real run against a paid model both stream events and complete with a recorded usage/cost figure.
- [ ] Restarting the daemon and continuing an OpenRouter thread replays its prior conversation correctly (no duplicated or dropped turns).
- [ ] Attaching an image to a vision-capable model succeeds; attaching one to a non-vision model fails the run with `openrouter.image_unsupported` instead of silently dropping it.
- [ ] Studio access is correctly offered to an OpenRouter agent under the same fail-closed rule as Claude.
- [ ] Restart/Resume on an old run saved with `provider="codex"` (from a pre-upgrade database, or inserted directly for the test) returns a controlled error and does not attempt to exec anything.

### Roblox Studio access (needs Studio with its official MCP launcher)

- [ ] With exactly one Studio instance open, a Claude run and an OpenRouter run are both granted Studio access and can call an allowlisted tool.
- [ ] With two or more Studio instances open, access is refused and the run continues without Studio rather than guessing an instance.
- [ ] With no Studio open, the run proceeds without Studio access and says so.
- [ ] A `read-only` profile cannot call a tool that modifies the open place.
- [ ] A `workspace-write` profile cannot call `upload_image`, `store_image`, `http_get`, `user_keyboard_input`, or `user_mouse_input`.
- [ ] With another MCP client already holding the launcher's WS-host slot, the shim still advertises a usable tool list to a Claude run; an OpenRouter run's grant is withheld instead (no shim fallback for its direct client), and the run continues without Studio.

### Rojo

- [ ] A `*.project.json` builds and the resulting place opens in Studio.
- [ ] A deliberately broken project file produces a clear error rather than a silent failure.

### Packaging

- [ ] `./scripts/package.ps1` produces both archives plus `SHA256SUMS.txt`, and every checksum verifies.
- [ ] The extracted Windows binary runs with `--mock` on a machine without Go or Node.js.
- [ ] The macOS `.app` launches on Apple Silicon. Unsigned builds need a one-time Control-click → **Open**; never disable Gatekeeper globally.

---

# Руководство по тестированию (Русский)

Полная проверка в Windows:

```powershell
./scripts/test.ps1
go test -race ./...
```

Набор покрывает миграции/pragma, изоляцию, восстановление default agent и agent CRUD, runtime settings, bursts записи, backup, budgets, справедливость scheduler и state machine, отмену/deadlock lease, многопроектные runs/recovery, SSE replay/медленных клиентов, безопасность Host/Origin/session, path traversal/symlink, prompts/redaction, memory FTS/fallback, завершение процессов, Claude stream/failures/resume capabilities, agent loop OpenRouter (выполнение инструментов на фейковом `agenttools.Workspace`, обработку streamed/malformed-ответов, пути отказа budget и image-attachment), credential manager OpenRouter (приоритет keychain/session/env, save/remove/test-connection), каталог моделей (живой fetch, истечение TTL, fallback на last-good-cache, embedded snapshot), сохранение переписки и санитизацию/сжатие истории, чтение legacy-запусков Codex и контролируемую ошибку restart/resume для удалённого провайдера, Studio binding, жизненный цикл Rojo, Git rollback, quarantine и portable export. Browser E2E создаёт настоящий проект, создаёт и запускает его mock agent, сохраняет integration settings и проверяет отсутствие console errors.

Этот проход добавил детерминированные регрессионные тесты без flakiness на sleep для тех, что связаны с конкурентностью: гонка дублирующихся ID в supervisor (`internal/processes`), честная пауза вместе с гонками cancel/pause и согласованностью при отказе записи в хранилище (`internal/scheduler`), прерывание валидации в `inconclusive` при потере write-lease, порядок и идемпотентность checkpoint коррекции, и общий контракт `studioforge-question` между backend'ным `detectQuestion` и frontend'ным `questionCard` (`web/src/lib`).

Vitest проверяет паритет i18n и типизированный API client. Playwright собирает реальный Go binary, выполняет защищённый first-run exchange, меняет locale, запускает run и открывает core screens с проверкой console errors и базовой доступности.

Playwright также открывает DAG задач, включая задачу без зависимостей, и проверяет отсутствие ошибок консоли. Адаптер Roblox Studio имеет fixture-тест JSON-RPC stdio. Для live smoke с открытым MCP-enabled place выполните:

```powershell
$env:STUDIOFORGE_REAL_STUDIO = '1'
go test ./internal/roblox/mcp -run TestRealStudioMCP -v -count=1
```

Если официальный launcher отвечает, но ни один instance Studio не подключён к локальному WebSocket host, тест честно сообщает это состояние и выполняет skip, не заявляя live-проверку place.

CI запускает Go-тесты на Windows и Linux для минимальной/текущей Go, race detector на Linux, Playwright на Linux, нативную Windows-сборку и нативную macOS arm64 build/smoke.
