# Local API

The API lives under `/api/v1`. The OpenAPI document is embedded at `/api/v1/openapi.yaml` and sourced from `internal/api/openapi.yaml`.

The browser first POSTs the one-use token to `/session/bootstrap`; success sets `studioforge_session` as HttpOnly and SameSite=Strict. All remaining endpoints require that cookie. Mutating requests require `Origin` to match the exact listener Host. Errors use:

```json
{"error":{"code":"validation","message":"...","requestId":"uuid"}}
```

`POST /runs` accepts `Idempotency-Key`. `POST /runs/{id}/{action}` supports pause, resume, cancel, and restart. `/events` is SSE; event IDs are SQLite sequence IDs. Browsers reconnect automatically, and a client can pass `Last-Event-ID` or `?after=` for replay.

No stack trace is returned. Request bodies are bounded and unknown JSON fields are rejected.

---

# Локальный API (Русский)

API расположен по пути `/api/v1`. Документ OpenAPI доступен по `/api/v1/openapi.yaml` и берётся из `internal/api/openapi.yaml`.

Сначала браузер отправляет одноразовый токен POST-запросом на `/session/bootstrap`; при успехе устанавливается `studioforge_session` с атрибутами HttpOnly и SameSite=Strict. Все дальнейшие endpoints требуют эту cookie. Изменяющие запросы требуют, чтобы `Origin` точно совпадал с Host listener. Ошибки имеют вид:

```json
{"error":{"code":"validation","message":"...","requestId":"uuid"}}
```

`POST /runs` принимает `Idempotency-Key`. `POST /runs/{id}/{action}` поддерживает pause, resume, cancel и restart. `/events` использует SSE; идентификаторы событий — последовательные номера SQLite. Браузер переподключается автоматически; для воспроизведения можно передать `Last-Event-ID` или `?after=`.

Stack trace не возвращается. Тела запросов ограничены по размеру, неизвестные JSON-поля отклоняются.
