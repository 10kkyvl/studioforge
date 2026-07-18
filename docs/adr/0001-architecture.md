# ADR 0001: Local modular monolith

- Status: Accepted
- Date: 2026-07-17

## Decision

StudioForge is a modular Go monolith with a static SvelteKit SPA embedded in the executable. Domain packages depend on small interfaces; SQLite, HTTP, Claude Code, Roblox Studio MCP, Rojo, Git, and operating-system behavior remain adapters. A single SQLite database uses WAL, foreign keys, `synchronous=NORMAL`, and a 5-second busy timeout. Project source stays at the canonical user-selected path.

The scheduler owns per-project queues and uses a global resource lease manager. Resource keys are sorted before acquisition. A run declares resources before execution and keeps project, provider, and model limits visible to the scheduler.

SSE is the first event transport. Events are committed to SQLite before publication, carry monotonically increasing identifiers, can be replayed after `Last-Event-ID`, and use bounded subscriber buffers.

## Rationale

This shape produces one portable, CGO-free executable, keeps deployment local, and preserves provider and transport substitution points without introducing a second runtime service.

## Consequences

- The web toolchain is required to build from source but not at runtime.
- SQLite writes are short and serialized where ordering matters.
- Multiple Studio windows are explicit resources; ambiguous bindings fail closed.
- Experimental same-project parallel writers remain disabled.

---

# ADR 0001: Локальный модульный монолит (Русский)

- Статус: Принято
- Дата: 2026-07-17

## Решение

StudioForge — модульный монолит на Go со статическим SvelteKit SPA, встроенным в executable. Domain-пакеты зависят от малых интерфейсов; SQLite, HTTP, Claude Code, Roblox Studio MCP, Rojo, Git и поведение ОС остаются адаптерами. Одна база SQLite использует WAL, внешние ключи, `synchronous=NORMAL` и busy timeout 5 секунд. Исходники проекта остаются по каноническому выбранному пользователем пути.

Scheduler владеет очередями проектов и использует глобальный менеджер resource lease. Ключи ресурсов сортируются перед захватом. Run объявляет ресурсы до выполнения, а лимиты проекта, provider и модели видимы scheduler.

SSE — первый транспорт событий. События фиксируются в SQLite до публикации, имеют монотонно растущие идентификаторы, могут быть воспроизведены после `Last-Event-ID` и используют ограниченные буферы подписчиков.

## Обоснование

Такая форма даёт один переносимый CGO-free executable, сохраняет локальное развёртывание и точки замены provider/транспорта без второго runtime-сервиса.

## Последствия

- Web toolchain нужен для сборки из исходников, но не во время выполнения.
- Записи SQLite короткие и сериализуются там, где важен порядок.
- Несколько окон Studio — явные ресурсы; неоднозначные привязки отклоняются безопасно.
- Экспериментальные параллельные writers одного проекта остаются отключёнными.
