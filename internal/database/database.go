package database

import (
	"context"
	"crypto/sha256"
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

	"github.com/10kkyvl/studioforge/internal/config"
	"github.com/10kkyvl/studioforge/internal/migrations"
	_ "modernc.org/sqlite"
)

type DB struct {
	SQL  *sql.DB
	Path string
	FTS5 bool
	// existing is true when Open found a non-empty database before opening it.
	// It lets the migration runner avoid creating a pointless snapshot for a
	// fresh install while still protecting databases that already contain data.
	existing      bool
	persistentURI bool
	writerVersion string
}

func Open(ctx context.Context, path string) (*DB, error) {
	return OpenWithVersion(ctx, path, config.Version)
}

// OpenWithVersion is the version-aware form of Open. The application uses
// config.Version through Open; tests and tools can pass an explicit version
// to exercise downgrade protection without changing global build metadata.
func OpenWithVersion(ctx context.Context, path, writerVersion string) (*DB, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	existing := false
	persistentURI := strings.HasPrefix(path, "file:") && !strings.Contains(path, "mode=memory")
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if info, err := os.Stat(path); err == nil {
			existing = info.Mode().IsRegular() && info.Size() > 0
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect database path: %w", err)
		}
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
	db := &DB{SQL: sqldb, Path: path, existing: existing, persistentURI: persistentURI, writerVersion: writerVersion}
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
	entries, err := fs.ReadDir(migrations.Files, "sql")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	type embeddedMigration struct {
		name string
		body []byte
		hash string
	}
	embedded := make(map[string]embeddedMigration)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := fs.ReadFile(migrations.Files, "sql/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(body))
		embedded[entry.Name()] = embeddedMigration{name: entry.Name(), body: body, hash: hash}
	}

	// Preflight is read-only. It checks every known hash and every ledger
	// version before metadata or application migrations can change the file.
	var ledgerExists int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&ledgerExists); err != nil {
		return fmt.Errorf("inspect migration ledger: %w", err)
	}
	ledgerColumns := map[string]bool{}
	ledgerRecords := map[string]migrationLedgerRecord{}
	var ledgerRecordList []migrationLedgerRecord
	if ledgerExists > 0 {
		ledgerColumns, err = migrationLedgerColumns(ctx, d.SQL)
		if err != nil {
			return err
		}
		records, err := readMigrationLedger(ctx, d.SQL, ledgerColumns)
		if err != nil {
			return err
		}
		for _, record := range records {
			ledgerRecords[record.version] = record
		}
		ledgerRecordList = records
	}
	for _, record := range ledgerRecordList {
		version := record.version
		if _, ok := embedded[version]; !ok {
			writer, backup := record.writerVersion, record.backupPath
			if writer == "" || backup == "" {
				var stateVersion, stateBackup string
				_ = d.SQL.QueryRowContext(ctx, `SELECT app_version, last_backup_path FROM schema_migration_state WHERE id = 1`).Scan(&stateVersion, &stateBackup)
				if writer == "" {
					writer = stateVersion
				}
				if backup == "" {
					backup = stateBackup
				}
			}
			if writer == "" {
				writer = "unknown release"
			}
			if backup == "" {
				backup = "no migration snapshot recorded"
			}
			return fmt.Errorf("database schema is newer than this StudioForge build: migration %s was written by %s; restore the backup at %s", version, writer, backup)
		}
		if record.checksum != "" && record.checksum != embedded[version].hash {
			return fmt.Errorf("migration checksum mismatch for %s: database has %s, embedded migration is %s", version, record.checksum, embedded[version].hash)
		}
	}
	pending := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if _, applied := ledgerRecords[entry.Name()]; !applied {
			pending = append(pending, entry.Name())
		}
	}
	legacyLedger := ledgerExists > 0 && !ledgerColumns["checksum"]
	for _, record := range ledgerRecords {
		if record.checksum == "" {
			legacyLedger = true
			break
		}
	}
	backupPath := ""
	if d.persistentURI && (len(pending) > 0 || legacyLedger) {
		return errors.New("migration snapshots require a plain file path; persistent file: URIs are refused for upgrades")
	}
	if d.existing && (len(pending) > 0 || legacyLedger) {
		target := "ledger-hardening"
		if len(pending) > 0 {
			target = pending[0]
		} else if len(entries) > 0 {
			target = entries[len(entries)-1].Name()
		}
		backupPath, err = d.ensureMigrationBackup(ctx, target)
		if err != nil {
			return fmt.Errorf("backup before migration upgrade: %w", err)
		}
	}
	if _, err := d.SQL.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version TEXT PRIMARY KEY,
applied_at TEXT NOT NULL,
checksum TEXT NOT NULL DEFAULT '',
writer_version TEXT NOT NULL DEFAULT '',
backup_path TEXT NOT NULL DEFAULT '')`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	if err := ensureMigrationLedgerColumns(ctx, d.SQL); err != nil {
		return err
	}
	if _, err := d.SQL.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migration_state (
id INTEGER PRIMARY KEY CHECK(id = 1),
app_version TEXT NOT NULL DEFAULT '',
last_backup_path TEXT NOT NULL DEFAULT '',
updated_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration state: %w", err)
	}
	if legacyLedger {
		for version, record := range ledgerRecords {
			if record.checksum == "" {
				if _, err := d.SQL.ExecContext(ctx, `UPDATE schema_migrations SET checksum = ?, backup_path = CASE WHEN backup_path = '' THEN ? ELSE backup_path END WHERE version = ?`, embedded[version].hash, backupPath, version); err != nil {
					return fmt.Errorf("adopt checksum for migration %s: %w", version, err)
				}
			}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		migration := embedded[entry.Name()]
		if _, applied := ledgerRecords[entry.Name()]; applied {
			continue
		}
		tx, err := d.SQL.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, string(migration.body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at, checksum, writer_version, backup_path)
			VALUES(?, ?, ?, ?, ?)`, entry.Name(), Now(), migration.hash, d.writerVersion, backupPath); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migration_state(id, app_version, last_backup_path, updated_at)
		VALUES(1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET app_version=excluded.app_version,
		last_backup_path=CASE WHEN excluded.last_backup_path <> '' THEN excluded.last_backup_path ELSE schema_migration_state.last_backup_path END,
		updated_at=excluded.updated_at`, d.writerVersion, backupPath, Now()); err != nil {
		return fmt.Errorf("record migration state: %w", err)
	}
	return nil
}

type migrationLedgerRecord struct {
	version, checksum, writerVersion, backupPath string
}

func migrationLedgerColumns(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(schema_migrations)")
	if err != nil {
		return nil, fmt.Errorf("inspect migration ledger columns: %w", err)
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read migration ledger columns: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read migration ledger columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close migration ledger columns: %w", err)
	}
	return columns, nil
}

func readMigrationLedger(ctx context.Context, db *sql.DB, columns map[string]bool) ([]migrationLedgerRecord, error) {
	selectColumns := []string{"version"}
	if columns["checksum"] {
		selectColumns = append(selectColumns, "checksum")
	}
	if columns["writer_version"] {
		selectColumns = append(selectColumns, "writer_version")
	}
	if columns["backup_path"] {
		selectColumns = append(selectColumns, "backup_path")
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(selectColumns, ", ")+" FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("inspect migration ledger: %w", err)
	}
	defer rows.Close()
	var records []migrationLedgerRecord
	for rows.Next() {
		var record migrationLedgerRecord
		var checksum, writer, backup sql.NullString
		scan := []any{&record.version}
		if columns["checksum"] {
			scan = append(scan, &checksum)
		}
		if columns["writer_version"] {
			scan = append(scan, &writer)
		}
		if columns["backup_path"] {
			scan = append(scan, &backup)
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, fmt.Errorf("read migration ledger: %w", err)
		}
		record.checksum, record.writerVersion, record.backupPath = checksum.String, writer.String, backup.String
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	return records, nil
}

func ensureMigrationLedgerColumns(ctx context.Context, db *sql.DB) error {
	columns, err := migrationLedgerColumns(ctx, db)
	if err != nil {
		return err
	}
	for _, column := range []string{
		"checksum TEXT NOT NULL DEFAULT ''",
		"writer_version TEXT NOT NULL DEFAULT ''",
		"backup_path TEXT NOT NULL DEFAULT ''",
	} {
		name := strings.Fields(column)[0]
		if columns[name] {
			continue
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE schema_migrations ADD COLUMN "+column); err != nil {
			return fmt.Errorf("add migration ledger column %s: %w", name, err)
		}
	}
	return nil
}

func (d *DB) ensureMigrationBackup(ctx context.Context, version string) (string, error) {
	if d.Path == ":memory:" || d.persistentURI || strings.HasPrefix(d.Path, "file:") {
		return "", errors.New("migration snapshots require a file-backed database")
	}
	base := strings.TrimSuffix(filepath.Base(version), filepath.Ext(version))
	dir := filepath.Join(filepath.Dir(d.Path), "backups")
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	for attempt := 0; attempt < 100; attempt++ {
		suffix := stamp
		if attempt > 0 {
			suffix += fmt.Sprintf("-%d", attempt)
		}
		target := filepath.Join(dir, "migration-before-"+base+"-"+suffix+".db")
		if _, err := os.Stat(target); err == nil {
			// A target from an earlier run may be stale or incomplete. Never
			// reuse it; the unique name makes every new upgrade self-contained.
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := d.Backup(ctx, target); err != nil {
			// VACUUM INTO can leave a partial file when interrupted. This
			// target belongs to this attempt, so remove it before returning.
			_ = os.Remove(target)
			return "", err
		}
		info, err := os.Stat(target)
		if err != nil {
			_ = os.Remove(target)
			return "", fmt.Errorf("inspect migration backup: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			_ = os.Remove(target)
			return "", fmt.Errorf("migration backup is empty: %s", target)
		}
		if err := rotateMigrationBackups(dir, 3, filepath.Base(target)); err != nil {
			return "", err
		}
		return target, nil
	}
	return "", errors.New("could not allocate a unique migration backup path")
}

func rotateMigrationBackups(dir string, keep int, preserve string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var backups []string
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && !entry.IsDir() && info.Mode().IsRegular() && info.Size() > 0 && strings.HasPrefix(entry.Name(), "migration-before-") && strings.HasSuffix(entry.Name(), ".db") {
			backups = append(backups, entry.Name())
		}
	}
	sort.Strings(backups)
	if len(backups) <= keep {
		return nil
	}
	remove := len(backups) - keep
	for _, name := range backups {
		if remove == 0 {
			break
		}
		if name == preserve {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("rotate migration backup %s: %w", name, err)
		}
		remove--
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
