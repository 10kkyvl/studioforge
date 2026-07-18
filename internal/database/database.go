package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/migrations"
	_ "modernc.org/sqlite"
)

type DB struct {
	SQL  *sql.DB
	Path string
	FTS5 bool
}

func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		path = filepath.Clean(abs)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := sqliteDSN(path)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqldb.SetMaxOpenConns(8)
	sqldb.SetMaxIdleConns(4)
	sqldb.SetConnMaxIdleTime(5 * time.Minute)
	db := &DB{SQL: sqldb, Path: path}
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := db.applyMigrations(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	db.FTS5 = db.enableFTS(ctx)
	return db, nil
}

func sqliteDSN(path string) string {
	if path == ":memory:" {
		return "file:studioforge-memory?mode=memory&cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	if strings.HasPrefix(path, "file:") {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		return path + separator + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	pathURI := strings.ReplaceAll(url.PathEscape(filepath.ToSlash(path)), "%2F", "/")
	pathURI = strings.ReplaceAll(pathURI, "%3A", ":")
	u := &url.URL{}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	return "file:" + pathURI + "?" + q.Encode()
}

func (d *DB) applyMigrations(ctx context.Context) error {
	if _, err := d.SQL.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	entries, err := fs.ReadDir(migrations.Files, "sql")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var exists int
		err := d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", entry.Name()).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if exists > 0 {
			continue
		}
		body, err := fs.ReadFile(migrations.Files, "sql/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		tx, err := d.SQL.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)", entry.Name(), Now()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (d *DB) enableFTS(ctx context.Context) bool {
	_, err := d.SQL.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts
USING fts5(id UNINDEXED, project_id UNINDEXED, content, summary)`)
	return err == nil
}

func (d *DB) Integrity(ctx context.Context) error {
	var result string
	if err := d.SQL.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("integrity check failed: %s", result)
	}
	rows, err := d.SQL.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign key check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign key check found violations")
	}
	return rows.Err()
}

func (d *DB) JournalMode(ctx context.Context) string {
	var mode string
	if err := d.SQL.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return "unknown"
	}
	return strings.ToLower(mode)
}

func (d *DB) Checkpoint(ctx context.Context) error {
	_, err := d.SQL.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (d *DB) Backup(ctx context.Context, target string) error {
	if target == "" {
		return errors.New("backup target is required")
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve backup target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("backup target already exists: %s", abs)
	}
	_, err = d.SQL.ExecContext(ctx, "VACUUM INTO ?", abs)
	if err != nil {
		return fmt.Errorf("sqlite backup: %w", err)
	}
	return nil
}

func (d *DB) Close() error { return d.SQL.Close() }

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
