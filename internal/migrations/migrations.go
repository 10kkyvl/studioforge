package migrations

import "embed"

// Files contains ordered, immutable database migrations.
//
//go:embed sql/*.sql
var Files embed.FS
