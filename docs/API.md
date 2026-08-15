# Local API

The API lives under `/api/v1`. The OpenAPI document is embedded at `/api/v1/openapi.yaml` and sourced from `internal/api/openapi.yaml`.

The browser first POSTs the one-use token to `/session/bootstrap`; success sets `studioforge_session` as HttpOnly and SameSite=Strict. All remaining endpoints require that cookie. Mutating requests require `Origin` to match the exact listener Host. Errors use:

```json
{"error":{"code":"validation","message":"...","requestId":"uuid"}}
```

`POST /runs` accepts `Idempotency-Key`. `POST /runs/{id}/{action}` supports pause, resume, cancel, and restart; resume submits a new run that continues the target run's saved provider session, and pause performs a controlled cancel rather than suspending the live process. `/events` is SSE; event IDs are SQLite sequence IDs. Browsers reconnect automatically, and a client can pass `Last-Event-ID` or `?after=` for replay.

`POST /runs` and `restart` both refuse with 409 `task_dependencies_incomplete` when the attached task's dependencies (direct or transitive) haven't all reached `completed`; the error's `details.blockers` array lists each blocking task's ID, title, and status. `resume` and an automatic playtest-correction run are exempt, since both continue an already-started run.

Starting the daemon with `--safe-mode` makes `POST /runs`, `resume`, `restart`, `POST /decisions/{id}/resolve` with `approve:true`, `POST /projects/{id}/sync`, and `POST /projects/{id}/open-studio` all refuse with 409 `safe_mode` — every one of them ends at the same enforcement point, `scheduler.Manager.Submit`, or (for Rojo/Studio) a lower-level flag with the same name. `pause` and `cancel` stay available, since they only stop work already running.

`GET /runs/{id}/diff` diffs against that run's own git checkpoint commit when one was recorded, falling back to the working tree against `HEAD` otherwise. `POST /runs/{id}/rollback` with an empty body (or one naming no files/hunks) non-destructively restores a run's checkpoint commit onto a new `studioforge/rollback-<timestamp>` branch (400 `no_checkpoint` if the run has no checkpoint, 409 `project_busy` if the project's write lease is currently held by another run). A body naming `{"files": [...], "hunks": [{"path","index"}]}` instead reverts only that selection in place — per-file checkout/`git rm` and per-hunk `git apply -R`, each hunk validated with `git apply --check -R` first — behind its own safety checkpoint, so a partial rollback is itself undoable; it refuses with 400 `invalid_selection` for a selection that doesn't match the run's diff, or 409 `dirty_worktree`, `later_change_conflict`, `patch_check_failed`, or `not_git_repo`. `GET /projects/{id}/git/status` and `POST /projects/{id}/git/tag` expose the project's `git status` and create an annotated tag.

An agent (`POST /projects/{id}/agents`, `POST /projects/{id}/agents/{agentID}`) carries `networkPolicy` (`unrestricted`/`registry-only`/`none`, default `unrestricted`) governing the network `run_command` can reach — real `sandbox-exec` enforcement on macOS, a fail-closed refusal to start on Windows and Linux for anything stricter than `unrestricted` — and `reviewBeforeApply` (default `false`), which pauses a run in `waiting_decision` at the boundary of a turn that edited a file. See [docs/SECURITY.md](SECURITY.md#network-egress-policy-for-run_command) and [ADR 0006](adr/0006-network-egress-policy.md) for the first, [docs/SECURITY.md](SECURITY.md#review-before-apply-gate) and [ADR 0007](adr/0007-review-before-apply-gate.md) for the second.

`POST /runs/{id}/review` resolves a run parked by `reviewBeforeApply`: `{"action": "apply"|"reject"|"apply-selected", "files": [...], "hunks": [{"path","index"}]}`. `apply` keeps every change and completes the run; `reject` reverts the run's entire diff in place through the same `SelectiveRollback` a manual selective rollback uses (never the branch-switching `SafeRollback`); `apply-selected` takes the files/hunks to **keep**, re-parses the diff fresh from the review's checkpoint, and reverts everything else — a keep-list that no longer matches the real diff fails closed with 400 `invalid_selection`. This is apply-then-revert, not a pre-action gate: the edits a review's diff describes are already on disk by the time the operator sees them. The project's write lease is held for the review's duration, transferred to it from the run rather than released and reacquired. An unresolved review expires after `review_gate_expiry_hours` (setting, default 24, range 1–168) — only the lease is released; there is no automatic apply or revert, the run stays in `waiting_decision`, and the edits stay on disk, leaving the decision to an ordinary `POST /runs/{id}/rollback`. Response: `{"status": "applied"|"rejected"|"partial", "safetyCommit", "revertedFiles", "revertedHunks"}`; 409 `review_not_pending`/`review_expired` if the review can no longer be resolved.

`GET /openrouter/status`, `POST /openrouter/key`, `DELETE /openrouter/key`, and `POST /openrouter/key/test` manage the OpenRouter API key. The key itself is never returned or logged — every response reports only its verification state (`not_configured`/`unverified`/`configured`/`invalid`) and source (`keychain`/`session`/`env`); a 503 `openrouter_unavailable` means the daemon has no credential manager wired up at all. `GET /openrouter/models` returns the cached model catalog (add `?refresh=1` to force a live refetch), filtered to tool-capable models plus the curated recommendation list; `GET /openrouter/capabilities` reports a specific model's vision/tool support and pricing. `POST /runs/{id}/{action}` on a run whose `provider` is the removed `"codex"` returns 409 for `restart` and `resume` — that run stays fully readable, just not re-runnable.

`POST /settings` validates `claude_path`, `rojo_path`, `git_path`, and `studio_mcp_path` as executable
paths before persisting anything (400 `invalid_tool_path` on a directory, a missing file, a value with
embedded arguments, or anything else that cannot resolve to a real, runnable file) — a rejected value
fails the whole request, none of the submitted settings are saved. `review_gate_expiry_hours` must be an
integer between 1 and 168 (400 `invalid_review_gate_expiry_hours` otherwise); it defaults to 24.

No stack trace is returned. Request bodies are bounded and unknown JSON fields are rejected.

---

# Локальный API (Русский)

API расположен по пути `/api/v1`. Документ OpenAPI доступен по `/api/v1/openapi.yaml` и берётся из `internal/api/openapi.yaml`.

Сначала браузер отправляет одноразовый токен POST-запросом на `/session/bootstrap`; при успехе устанавливается `studioforge_session` с атрибутами HttpOnly и SameSite=Strict. Все дальнейшие endpoints требуют эту cookie. Изменяющие запросы требуют, чтобы `Origin` точно совпадал с Host listener. Ошибки имеют вид:

```json
{"error":{"code":"validation","message":"...","requestId":"uuid"}}
```

`POST /runs` принимает `Idempotency-Key`. `POST /runs/{id}/{action}` поддерживает pause, resume, cancel и restart; resume отправляет новый запуск, продолжающий сохранённую сессию провайдера целевого запуска, а pause выполняет контролируемую отмену, а не приостановку живого процесса. `/events` использует SSE; идентификаторы событий — последовательные номера SQLite. Браузер переподключается автоматически; для воспроизведения можно передать `Last-Event-ID` или `?after=`.

`POST /runs` и `restart` отказывают с 409 `task_dependencies_incomplete`, если зависимости прикреплённой задачи (прямые или транзитивные) не все в статусе `completed`; массив `details.blockers` в ошибке перечисляет ID, title и status каждой блокирующей задачи. `resume` и автоматический correction-запуск от этой проверки освобождены, поскольку оба продолжают уже начатый запуск.

Запуск демона с `--safe-mode` заставляет `POST /runs`, `resume`, `restart`, `POST /decisions/{id}/resolve` с `approve:true`, `POST /projects/{id}/sync` и `POST /projects/{id}/open-studio` отказывать с 409 `safe_mode` — все они заканчиваются в одной точке enforcement, `scheduler.Manager.Submit`, либо (для Rojo/Studio) в отдельном флаге с тем же именем. `pause` и `cancel` остаются доступны, поскольку лишь останавливают уже идущую работу.

`GET /runs/{id}/diff` сравнивает с коммитом git-чекпоинта этого запуска, если он был записан, иначе — с рабочим деревом относительно `HEAD`. `POST /runs/{id}/rollback` с пустым телом (или без выбранных files/hunks) неразрушающе восстанавливает коммит чекпоинта запуска в новую ветку `studioforge/rollback-<timestamp>` (400 `no_checkpoint`, если у запуска нет чекпоинта; 409 `project_busy`, если write-lease проекта сейчас удерживает другой запуск). Тело вида `{"files": [...], "hunks": [{"path","index"}]}` вместо этого откатывает на месте только выбранное — checkout/`git rm` по файлу и `git apply -R` по hunk'у, каждый предварительно проверенный через `git apply --check -R`, — за собственным защитным чекпоинтом, поэтому частичный откат тоже можно отменить; отказ приходит как 400 `invalid_selection`, если выбор не соответствует diff'у запуска, либо как 409 `dirty_worktree`, `later_change_conflict`, `patch_check_failed` или `not_git_repo`. `GET /projects/{id}/git/status` и `POST /projects/{id}/git/tag` отдают `git status` проекта и создают аннотированный тег.

Агент (`POST /projects/{id}/agents`, `POST /projects/{id}/agents/{agentID}`) несёт `networkPolicy` (`unrestricted`/`registry-only`/`none`, по умолчанию `unrestricted`), управляющую тем, до какой сети дотягивается `run_command`, — реальный enforcement через `sandbox-exec` на macOS, отказ закрыто до старта на Windows и Linux для любой политики строже `unrestricted`, — и `reviewBeforeApply` (по умолчанию `false`), которая приостанавливает запуск в `waiting_decision` на границе хода, отредактировавшего файл. См. [docs/SECURITY.md](SECURITY.md#network-egress-policy-for-run_command) и [ADR 0006](adr/0006-network-egress-policy.md) для первой, [docs/SECURITY.md](SECURITY.md#review-before-apply-gate) и [ADR 0007](adr/0007-review-before-apply-gate.md) для второй.

`POST /runs/{id}/review` разрешает запуск, запаркованный `reviewBeforeApply`: `{"action": "apply"|"reject"|"apply-selected", "files": [...], "hunks": [{"path","index"}]}`. `apply` сохраняет все изменения и завершает запуск; `reject` откатывает весь diff запуска на месте через тот же `SelectiveRollback`, что использует ручной селективный откат (никогда не переключающий ветку `SafeRollback`); `apply-selected` принимает файлы/hunk'ы, которые нужно **сохранить**, заново разбирает diff от чекпоинта ревью и откатывает всё остальное — список сохранения, переставший соответствовать реальному diff, отказывает закрыто с 400 `invalid_selection`. Это apply-then-revert, а не pre-action gate: правки, которые описывает diff ревью, уже на диске к моменту, когда их видит оператор. Write-лиза проекта удерживается на время ревью, передаётся ему от запуска, а не освобождается и захватывается заново. Неразрешённое ревью истекает через `review_gate_expiry_hours` (настройка, по умолчанию 24, диапазон 1–168) — освобождается только лиза; автоматического apply или revert нет, запуск остаётся в `waiting_decision`, а правки остаются на диске, оставляя решение за обычным `POST /runs/{id}/rollback`. Ответ: `{"status": "applied"|"rejected"|"partial", "safetyCommit", "revertedFiles", "revertedHunks"}`; 409 `review_not_pending`/`review_expired`, если ревью больше нельзя разрешить.

`POST /settings` проверяет `claude_path`, `rojo_path`, `git_path` и `studio_mcp_path` как пути к
исполняемым файлам до того, как что-либо будет сохранено (400 `invalid_tool_path` для каталога,
отсутствующего файла, значения со встроенными аргументами или всего, что не резолвится в реальный
исполняемый файл) — отказ по одному значению отклоняет весь запрос, ни одна из переданных настроек
не сохраняется. `review_gate_expiry_hours` должен быть целым числом от 1 до 168 (иначе 400
`invalid_review_gate_expiry_hours`); по умолчанию — 24.

Stack trace не возвращается. Тела запросов ограничены по размеру, неизвестные JSON-поля отклоняются.
