# StudioForge v0.6.0

## English

StudioForge 0.6.0 brings project controls, reviewable changes, and more reliable agent runs.

- Review pending file changes, inspect checkpoint ranges, and revert individual files or hunks.
- Manage project memory: inspect, edit, pin, delete, and trace entries to their source runs.
  Automatic memory now deduplicates entries, limits retention, and records run outcomes.
- See live changes and Studio screenshots in chat, answer structured agent questions,
  and inspect playtest console observations with configurable polling.
- Studio operation history remains available alongside Git changes and run records.
- Process containment and project command controls are strengthened; protection varies by OS.
- Database upgrades verify migration checksums and snapshot existing data before upgrading.
- Fixes cover macOS startup and credentials, Claude cancellation, event reconnection,
  chat streaming, OpenRouter history and context budgets, and resource cleanup.

### Downloads

- `StudioForge-v0.6.0-windows-amd64.zip` — Windows amd64.
- `StudioForge-v0.6.0-macos-arm64.zip` — macOS Apple Silicon.
- `SHA256SUMS.txt` — archive checksums.

Packages are unsigned. Back up your data and projects before upgrading, then replace the
application. Review gates cover file changes; Studio operations and other non-file effects
are outside that gate. Windows Job Objects constrain process lifetime and resources, not
filesystem access or network egress. See `docs/KNOWN_LIMITATIONS.md` for platform limitations.

## Русский

StudioForge 0.6.0 добавляет управление проектами, проверку изменений и повышает надёжность запусков агентов.

- Проверка изменений перед принятием, сравнение контрольных точек и откат отдельных файлов или фрагментов.
- Управление памятью проекта: просмотр, редактирование, закрепление, удаление и переход к исходному запуску.
  Автоматическая память устраняет дубликаты, ограничивает срок хранения по числу записей и сохраняет результаты запусков.
- Изменения и скриншоты Studio в чате, структурированные вопросы агента и наблюдения консоли плейтеста.
- История операций Studio сохраняется вместе с Git-изменениями и запусками.
- Усилены ограничения процессов и команд проекта; возможности защиты зависят от ОС.
- Обновление базы проверяет контрольные суммы миграций и создаёт снимок существующих данных.
- Исправлены запуск и работа с учётными данными на macOS, отмена Claude, восстановление потока событий,
  отображение чата, история и контекст OpenRouter, освобождение ресурсов.

Архивы доступны для Windows amd64 и macOS Apple Silicon; контрольные суммы — в `SHA256SUMS.txt`.
Приложение не подписано. Перед обновлением сохраните резервные копии данных и проектов.
Проверка изменений охватывает файлы; операции Studio и другие внешние эффекты в неё не входят.
Windows Job Objects ограничивают время жизни и ресурсы процессов, но не доступ к файлам или сети.
Подробности — в `docs/KNOWN_LIMITATIONS.md`.
