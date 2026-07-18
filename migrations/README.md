# Database migrations

The executable embeds and applies the authoritative SQL files from `internal/migrations/sql`. They are kept inside the Go package because `go:embed` cannot reach outside its package tree.

---

# Миграции базы данных (Русский)

Executable встраивает и применяет авторитетные SQL-файлы из `internal/migrations/sql`. Они находятся внутри Go-пакета, потому что `go:embed` не может обращаться за пределы дерева своего пакета.
