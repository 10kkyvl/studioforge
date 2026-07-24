# Security policy

## Supported versions

Security fixes are made on the latest `main` and the latest tagged release.

## Reporting a vulnerability

Do not file a public issue for an unpatched vulnerability. Use GitHub private vulnerability reporting when enabled. Include impact, reproduction steps, affected platform/version, and a minimal proof of concept without real credentials or user projects.

## Local security model

StudioForge binds to loopback by default, creates a cryptographically random one-use bootstrap token, exchanges it for an HttpOnly SameSite cookie, validates Host and Origin on mutating requests, and sets no CORS headers at all. Project roots are canonicalized when they are registered. Provider processes receive a reduced environment. Known credential formats are redacted from diagnostic bundles, from stored run event payloads before they are written to the database, and from StudioForge's own `slog` application log lines.

One scope limit, stated precisely because the difference matters: the `mcp-shim` subcommand's stdout (MCP protocol traffic) and `studioforge doctor`'s JSON report never go through `slog`, so the log-redaction handler does not touch them — the doctor bundle path has its own separate redaction call site instead. The path traversal and symlink-escape guard is implemented but currently has no caller, because no endpoint accepts a project-relative path; registration-time canonicalization is what is actually enforced today. See [docs/SECURITY.md](docs/SECURITY.md) for the full model.

Localhost is not treated as a trust boundary. Malware running as the same OS user can still access user files and local processes. Keep the workstation and external CLIs updated.

## Dangerous actions

It is a design rule of this project that production publishing, destructive file operations, force Git operations, production DataStore changes, and unreviewed Marketplace scripts require an explicit Decision before they may proceed.

In the current alpha this is a rule for contributors, not an enforced runtime control: the Decision record, its API endpoint, and its review interface exist, but no live run produces a Decision yet, so the mechanism must not be relied on as a safety gate. The controls that do apply today are the permission-profile tool allowlist, fail-closed Studio access, budget ceilings, project-root path containment, and the Git checkpoint taken before each non-plan Claude run. Safe mode disables AI workers and external tool launches while preserving diagnostics and export.

An agent running under a permissive profile can change your project files. The Git checkpoint is a recovery mechanism, not a preventative one.

Never include API keys, OAuth tokens, complete environment dumps, private prompts, or user source in a report. Claude Code v1 authentication remains owned by Claude Code; StudioForge does not store the Anthropic token in SQLite. The OpenRouter API key is kept in the OS secure credential store (Windows Credential Manager / macOS Keychain), with an environment-variable and session-only fallback when the store is unavailable — it is never written to SQLite, run events, application logs, or the diagnostic bundle, and it is required even to run free models.

---

# Политика безопасности (Русский)

## Поддерживаемые версии

Исправления безопасности выпускаются для актуальной ветки `main` и последнего помеченного релиза.

## Сообщение об уязвимости

Не создавайте публичный issue для неустранённой уязвимости. Используйте приватное сообщение об уязвимости GitHub, если оно включено. Укажите влияние, шаги воспроизведения, затронутую платформу/версию и минимальный proof of concept без реальных учётных данных и пользовательских проектов.

## Локальная модель безопасности

По умолчанию StudioForge слушает только loopback, создаёт криптографически случайный одноразовый bootstrap-токен и обменивает его на HttpOnly SameSite-cookie. Изменяющие запросы проверяют Host и Origin; заголовки CORS не выставляются вовсе. Корни проектов канонизируются при регистрации. Процессы provider получают сокращённое окружение. Известные форматы учётных данных редактируются в диагностических архивах, в сохранённых run event перед записью в базу и в собственных `slog`-логах приложения.

Уточнение, потому что разница важна: stdout подкоманды `mcp-shim` (MCP-трафик) и JSON-отчёт `studioforge doctor` не проходят через `slog`, поэтому handler редактирования логов их не затрагивает — у пути diagnostic bundle есть собственная отдельная точка редактирования. Проверка path traversal и выхода по symlink реализована, но сейчас не вызывается, так как ни один endpoint не принимает путь относительно проекта; фактически действует канонизация при регистрации. Полная модель: [docs/SECURITY.md](docs/SECURITY.md).

Localhost не является границей доверия: вредоносная программа того же пользователя ОС всё ещё может читать пользовательские файлы и локальные процессы. Поддерживайте рабочую станцию и внешние CLI в актуальном состоянии.

## Опасные действия

Правило проекта: публикация в production, разрушительные операции с файлами, принудительные Git-операции, изменения production DataStore и непроверенные скрипты Marketplace требуют явного Decision.

В текущей alpha это правило для контрибьюторов, а не работающий runtime-контроль: запись Decision, её API-endpoint и интерфейс review существуют, но живые запуски пока не создают Decision, поэтому полагаться на этот механизм как на защитный барьер нельзя. Сегодня действуют: allowlist инструментов по permission profile, fail-closed доступ к Studio, бюджетные лимиты, containment в границах корня проекта и Git checkpoint перед каждым не-plan запуском Claude. Safe mode отключает AI-workers и запуск внешних инструментов, сохраняя диагностику и экспорт.

Агент с разрешительным профилем может изменить файлы вашего проекта. Git checkpoint — механизм восстановления, а не предотвращения.

Никогда не включайте в отчёт API-ключи, OAuth-токены, полный дамп окружения, приватные prompt или исходники пользователя. Авторизацией Claude Code v1 управляет сам Claude Code; StudioForge не хранит токен Anthropic в SQLite.
