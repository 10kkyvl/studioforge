# Local API

The API lives under `/api/v1`. The OpenAPI document is embedded at `/api/v1/openapi.yaml` and sourced from `internal/api/openapi.yaml`.

The browser first POSTs the one-use token to `/session/bootstrap`; success sets `studioforge_session` as HttpOnly and SameSite=Strict. All remaining endpoints require that cookie. Mutating requests require `Origin` to match the exact listener Host. Errors use:

```json
{"error":{"code":"validation","message":"...","requestId":"uuid"}}
```

`POST /runs` accepts `Idempotency-Key`. `POST /runs/{id}/{action}` supports pause, resume, cancel, and restart; resume submits a new run that continues the target run's saved provider session, and pause performs a controlled cancel rather than suspending the live process. `/events` is SSE; event IDs are SQLite sequence IDs. Browsers reconnect automatically, and a client can pass `Last-Event-ID` or `?after=` for replay.

`POST /runs` and `restart` both refuse with 409 `task_dependencies_incomplete` when the attached task's dependencies (direct or transitive) haven't all reached `completed`; the error's `details.blockers` array lists each blocking task's ID, title, and status. `resume` and an automatic playtest-correction run are exempt, since both continue an already-started run.

`GET /runs/{id}/diff` diffs against that run's own git checkpoint commit when one was recorded, falling back to the working tree against `HEAD` otherwise. `POST /runs/{id}/rollback` with an empty body (or one naming no files/hunks) non-destructively restores a run's checkpoint commit onto a new `studioforge/rollback-<timestamp>` branch (400 `no_checkpoint` if the run has no checkpoint, 409 `project_busy` if the project's write lease is currently held by another run). A body naming `{"files": [...], "hunks": [{"path","index"}]}` instead reverts only that selection in place — per-file checkout/`git rm` and per-hunk `git apply -R`, each hunk validated with `git apply --check -R` first — behind its own safety checkpoint, so a partial rollback is itself undoable; it refuses with 400 `invalid_selection` for a selection that doesn't match the run's diff, or 409 `dirty_worktree`, `later_change_conflict`, `patch_check_failed`, or `not_git_repo`. `GET /projects/{id}/git/status` and `POST /projects/{id}/git/tag` expose the project's `git status` and create an annotated tag.

`GET /openrouter/status`, `POST /openrouter/key`, `DELETE /openrouter/key`, and `POST /openrouter/key/test` manage the OpenRouter API key. The key itself is never returned or logged — every response reports only its verification state (`not_configured`/`unverified`/`configured`/`invalid`) and source (`keychain`/`session`/`env`); a 503 `openrouter_unavailable` means the daemon has no credential manager wired up at all. `GET /openrouter/models` returns the cached model catalog (add `?refresh=1` to force a live refetch), filtered to tool-capable models plus the curated recommendation list; `GET /openrouter/capabilities` reports a specific model's vision/tool support and pricing. `POST /runs/{id}/{action}` on a run whose `provider` is the removed `"codex"` returns 409 for `restart` and `resume` — that run stays fully readable, just not re-runnable.

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

`GET /runs/{id}/diff` сравнивает с коммитом git-чекпоинта этого запуска, если он был записан, иначе — с рабочим деревом относительно `HEAD`. `POST /runs/{id}/rollback` с пустым телом (или без выбранных files/hunks) неразрушающе восстанавливает коммит чекпоинта запуска в новую ветку `studioforge/rollback-<timestamp>` (400 `no_checkpoint`, если у запуска нет чекпоинта; 409 `project_busy`, если write-lease проекта сейчас удерживает другой запуск). Тело вида `{"files": [...], "hunks": [{"path","index"}]}` вместо этого откатывает на месте только выбранное — checkout/`git rm` по файлу и `git apply -R` по hunk'у, каждый предварительно проверенный через `git apply --check -R`, — за собственным защитным чекпоинтом, поэтому частичный откат тоже можно отменить; отказ приходит как 400 `invalid_selection`, если выбор не соответствует diff'у запуска, либо как 409 `dirty_worktree`, `later_change_conflict`, `patch_check_failed` или `not_git_repo`. `GET /projects/{id}/git/status` и `POST /projects/{id}/git/tag` отдают `git status` проекта и создают аннотированный тег.

Stack trace не возвращается. Тела запросов ограничены по размеру, неизвестные JSON-поля отклоняются.
