# Testing guide

Full Windows verification:

```powershell
./scripts/test.ps1
go test -race ./...
```

The suite covers migrations/pragmas, isolation, default-agent repair and agent CRUD, runtime settings, write bursts, backup, budgets, scheduler fairness and state machines, lease cancellation/deadlock, multi-project runs/recovery, SSE replay/slow clients, Host/Origin/session security, path traversal/symlinks, prompts/redaction, memory FTS/fallback, process termination, Codex JSONL/auth/resume argument handling, Claude stream/failures/resume capabilities, Studio binding, Rojo lifecycle, Git rollback, quarantine, and portable export. Browser E2E creates a real project, creates and launches its mock agent, saves integration settings, and checks for console errors.

Vitest verifies i18n parity and the typed API error client. Playwright builds a real Go binary, performs secure first-run exchange, changes locale, starts a run, opens the Tasks DAG (including a task with no dependencies), and visits core screens with console-error assertions.

The Roblox Studio adapter includes an in-process JSON-RPC stdio fixture test. With an MCP-enabled Studio place open, run the opt-in live handshake, capability discovery, and instance-list smoke:

```powershell
$env:STUDIOFORGE_REAL_STUDIO = '1'
go test ./internal/roblox/mcp -run TestRealStudioMCP -v -count=1
```

If the official launcher responds but no Studio instance is connected to its local WebSocket host, the test reports that exact state and skips instead of claiming a live place verification.

CI runs Windows and Linux Go tests on minimum/current Go, the race detector on Linux, Playwright on Linux, a native Windows build, and a native Apple Silicon macOS build/smoke.

## What CI cannot verify

CI has no Roblox Studio, no authenticated Claude or Codex account, and no GUI. Everything involving those is exercised against fake CLIs (`testdata/fakes/`) or skipped. The following therefore has to be checked by hand before a release, and the result recorded in the pull request or release notes.

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

- [ ] `studioforge doctor` reports correct detected paths and versions for Git, Claude Code, Codex, Rojo, and the Studio MCP launcher on this machine.
- [ ] `studioforge doctor --bundle diagnostics.zip` produces an archive, and a manual read of its contents shows no tokens, cookies, or credential-shaped strings.

### Claude Code integration (needs an authenticated, billable account)

- [ ] A real run starts, streams events into the interface, and completes with a recorded usage figure.
- [ ] Cancelling a run terminates the `claude` process tree.
- [ ] Resuming a session continues the prior conversation.
- [ ] A Git checkpoint commit exists in the project repository from before the run, and `git diff` against it shows exactly what the agent changed.

### Codex integration (needs saved CLI authentication)

- [ ] A real `codex exec` run streams events and completes.
- [ ] Studio access is correctly *not* offered to a Codex agent.

### Roblox Studio access (needs Studio with its official MCP launcher)

- [ ] With exactly one Studio instance open, a Claude run is granted Studio access and can call an allowlisted tool.
- [ ] With two or more Studio instances open, access is refused and the run continues without Studio rather than guessing an instance.
- [ ] With no Studio open, the run proceeds without Studio access and says so.
- [ ] A `read-only` profile cannot call a tool that modifies the open place.
- [ ] A `workspace-write` profile cannot call `upload_image`, `store_image`, `http_get`, `user_keyboard_input`, or `user_mouse_input`.
- [ ] With another MCP client already holding the launcher's WS-host slot, the shim still advertises a usable tool list.

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

Набор покрывает миграции/pragma, изоляцию, восстановление default agent и agent CRUD, runtime settings, bursts записи, backup, budgets, справедливость scheduler и state machine, отмену/deadlock lease, многопроектные runs/recovery, SSE replay/медленных клиентов, безопасность Host/Origin/session, path traversal/symlink, prompts/redaction, memory FTS/fallback, завершение процессов, Codex JSONL/auth/resume args, Claude stream/failures/resume capabilities, Studio binding, жизненный цикл Rojo, Git rollback, quarantine и portable export. Browser E2E создаёт настоящий проект, создаёт и запускает его mock agent, сохраняет integration settings и проверяет отсутствие console errors.

Vitest проверяет паритет i18n и типизированный API client. Playwright собирает реальный Go binary, выполняет защищённый first-run exchange, меняет locale, запускает run и открывает core screens с проверкой console errors и базовой доступности.

Playwright также открывает DAG задач, включая задачу без зависимостей, и проверяет отсутствие ошибок консоли. Адаптер Roblox Studio имеет fixture-тест JSON-RPC stdio. Для live smoke с открытым MCP-enabled place выполните:

```powershell
$env:STUDIOFORGE_REAL_STUDIO = '1'
go test ./internal/roblox/mcp -run TestRealStudioMCP -v -count=1
```

Если официальный launcher отвечает, но ни один instance Studio не подключён к локальному WebSocket host, тест честно сообщает это состояние и выполняет skip, не заявляя live-проверку place.

CI запускает Go-тесты на Windows и Linux для минимальной/текущей Go, race detector на Linux, Playwright на Linux, нативную Windows-сборку и нативную macOS arm64 build/smoke.
