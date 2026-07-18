# ADR 0002: Discover external capabilities at runtime

- Status: Accepted
- Date: 2026-07-17

## Evidence reviewed

- Roblox Creator Hub, “Connect to the Roblox Studio MCP server”: the bundled stdio launchers are `%LOCALAPPDATA%\\Roblox\\mcp.bat` and `/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP`; `list_roblox_studios` and `set_active_studio` provide explicit multi-instance selection.
- OpenAI Codex CLI manual: stable non-interactive `codex exec`, JSONL output, explicit sandbox/model/reasoning flags, saved CLI authentication, and thread resume.
- Anthropic Claude Code CLI reference: print mode, JSON/stream-JSON output, resume, model, permissions, MCP configuration, turn and budget controls evolve independently.
- Rojo 7 project format: project files use `.project.json`; a serve port may be configured or overridden.
- Go release history: Go 1.26 is current stable; the source remains compatible with installed Go 1.25.1 and CI checks both the minimum and current lines.
- SQLite WAL and PRAGMA documentation: WAL is persistent, checkpoints are operationally important, `NORMAL` is the intended performance/safety balance in WAL mode, and busy handling remains necessary.
- SvelteKit static adapter documentation/package metadata: the UI can be emitted as static assets with an SPA fallback.
- GitHub runner documentation: standard `macos-latest` is currently arm64, so CI can test—not merely cross-compile—the macOS arm64 build.

## Decision

No integration assumes a version from its version string alone. Each adapter locates the executable (including a user override), records version/auth output, parses help, JSONL, or MCP discovery as appropriate, and enables only observed/documented capabilities. Missing or unsupported capabilities yield actionable diagnostics and leave mock mode available.

---

# ADR 0002: Динамическое обнаружение внешних возможностей (Русский)

- Статус: Принято
- Дата: 2026-07-17

## Рассмотренные источники

- Roblox Creator Hub: встроенные stdio-launcher — `%LOCALAPPDATA%\\Roblox\\mcp.bat` и `/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP`; `list_roblox_studios` и `set_active_studio` позволяют явно выбирать несколько instance.
- Руководство OpenAI Codex CLI: стабильный non-interactive `codex exec`, JSONL output, явные sandbox/model/reasoning flags, сохранённая CLI-авторизация и resume thread.
- Anthropic Claude Code CLI: print mode, JSON/stream-JSON output, resume, model, permissions, MCP configuration, лимиты turns и budget развиваются независимо.
- Формат Rojo 7: файлы проекта имеют расширение `.project.json`; порт serve можно настроить или переопределить.
- История релизов Go: Go 1.26 — текущая стабильная версия; исходный код совместим с Go 1.25.1, CI проверяет минимальную и текущую линии.
- Документация SQLite WAL/PRAGMA: WAL постоянен, checkpoint важны в эксплуатации, `NORMAL` — ожидаемый баланс производительности и безопасности в WAL, обработка busy всё ещё необходима.
- Статический адаптер SvelteKit: UI можно выпускать как статические assets с SPA fallback.
- Документация GitHub runner: стандартный `macos-latest` сейчас arm64, поэтому CI способен тестировать, а не только кросс-компилировать macOS arm64 build.

## Решение

Ни одна интеграция не доверяет только строке версии. Каждый адаптер ищет executable (включая пользовательский override), записывает вывод версии/auth, разбирает help, JSONL или MCP discovery и включает только наблюдаемые/документированные возможности. Отсутствующие или неподдерживаемые возможности дают понятную диагностику, а mock mode остаётся доступным.
