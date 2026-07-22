# Local API

The API lives under `/api/v1`. The OpenAPI document is embedded at `/api/v1/openapi.yaml` and sourced from `internal/api/openapi.yaml`.

The browser first POSTs the one-use token to `/session/bootstrap`; success sets `studioforge_session` as HttpOnly and SameSite=Strict. All remaining endpoints require that cookie. Mutating requests require `Origin` to match the exact listener Host. Errors use:

```json
{"error":{"code":"validation","message":"...","requestId":"uuid"}}
```

`POST /runs` accepts `Idempotency-Key`. `POST /runs/{id}/{action}` supports pause, resume, cancel, and restart; resume submits a new run that continues the target run's saved provider session, and pause performs a controlled cancel rather than suspending the live process. `/events` is SSE; event IDs are SQLite sequence IDs. Browsers reconnect automatically, and a client can pass `Last-Event-ID` or `?after=` for replay.

`GET /runs/{id}/diff` diffs against that run's own git checkpoint commit when one was recorded, falling back to the working tree against `HEAD` otherwise. `POST /runs/{id}/rollback` non-destructively restores a run's checkpoint commit onto a new `studioforge/rollback-<timestamp>` branch (400 if the run has no checkpoint, 409 if the project's write lease is currently held by another run). `GET /projects/{id}/git/status` and `POST /projects/{id}/git/tag` expose the project's `git status` and create an annotated tag.

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

`GET /runs/{id}/diff` сравнивает с коммитом git-чекпоинта этого запуска, если он был записан, иначе — с рабочим деревом относительно `HEAD`. `POST /runs/{id}/rollback` неразрушающе восстанавливает коммит чекпоинта запуска в новую ветку `studioforge/rollback-<timestamp>` (400, если у запуска нет чекпоинта; 409, если write-lease проекта сейчас удерживает другой запуск). `GET /projects/{id}/git/status` и `POST /projects/{id}/git/tag` отдают `git status` проекта и создают аннотированный тег.

Stack trace не возвращается. Тела запросов ограничены по размеру, неизвестные JSON-поля отклоняются.
