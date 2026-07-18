# Database schema and operations

The CGO-free `modernc.org/sqlite` driver is used through `database/sql`. Every connection receives `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, and `busy_timeout=5000`. Migrations are embedded from `internal/migrations/sql` and recorded in `schema_migrations` inside the same transaction as each migration.

Project-scoped tables include `projects`, settings/documents/tags, agents/skills, tasks/dependencies, runs/events/artifacts, mailbox, memory, decisions, Studio sessions, leases/processes/checkpoints, assets/reviews, and budgets/usage. Foreign keys, status checks, unique constraints, and filter indexes are part of the migration.

Run events are append-only. Long external processes never hold SQL transactions. Batch insertion assigns sequence IDs before publication. FTS5 is capability-tested; if creation or querying fails, memory search uses project-scoped `LIKE` ordering.

Doctor executes `PRAGMA integrity_check` and `foreign_key_check`. Graceful shutdown performs `wal_checkpoint(TRUNCATE)`. Backups use `VACUUM INTO`, so the database and WAL remain a coherent source while the daemon is running.

---

# Схема и эксплуатация базы данных (Русский)

CGO-free драйвер `modernc.org/sqlite` используется через `database/sql`. Каждое соединение получает `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON` и `busy_timeout=5000`. Миграции встраиваются из `internal/migrations/sql` и записываются в `schema_migrations` в той же транзакции, что и каждая миграция.

Таблицы с областью проекта включают `projects`, settings/documents/tags, agents/skills, tasks/dependencies, runs/events/artifacts, mailbox, memory, decisions, Studio sessions, leases/processes/checkpoints, assets/reviews и budgets/usage. В миграции входят внешние ключи, проверки статусов, уникальные ограничения и индексы фильтрации.

События run являются append-only. Длительные внешние процессы никогда не держат SQL-транзакции. Пакетная вставка назначает sequence ID до публикации. FTS5 проверяется по возможностям; если создание или запрос недоступны, поиск памяти использует project-scoped `LIKE`-сортировку.

Doctor запускает `PRAGMA integrity_check` и `foreign_key_check`. Корректное завершение выполняет `wal_checkpoint(TRUNCATE)`. Backup использует `VACUUM INTO`, поэтому база и WAL остаются согласованным источником во время работы daemon.
