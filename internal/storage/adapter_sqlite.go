// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package storage provides relational persistence adapters, CQRS command
// and query services, and schema migration execution.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gotcode.org/crucible/internal/config"
	_ "modernc.org/sqlite" // Pure Go SQLite driver (CGO_ENABLED=0)
)

//go:embed schema/*.sql
var embeddedSQLiteSchemaFS embed.FS

// SQLiteAdapter implements the Engine port for pure Go SQLite.
type SQLiteAdapter struct {
	db   *sql.DB
	path string
}

// Ensure SQLiteAdapter satisfies Engine.
var _ Engine = (*SQLiteAdapter)(nil)

// NewSQLiteAdapter constructs an SQLite engine adapter from StorageConfig.
func NewSQLiteAdapter(
	ctx context.Context,
	cfg config.StorageConfig,
) (*SQLiteAdapter, error) {
	return NewSQLiteAdapterPath(
		ctx,
		cfg.DatabasePath,
		cfg.WALMode,
		cfg.BusyTimeoutMs,
	)
}

// NewSQLiteAdapterPath constructs an SQLite engine adapter at the specified
// path with WAL configuration.
func NewSQLiteAdapterPath(
	ctx context.Context,
	dbPath string,
	walMode bool,
	busyTimeoutMs int,
) (*SQLiteAdapter, error) {
	if dbPath == "" {
		dbPath = config.DefaultDevDBPath
	}

	cleanPath := filepath.Clean(dbPath)

	if cleanPath != ":memory:" {
		dir := filepath.Dir(cleanPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
		}
	}

	if busyTimeoutMs <= 0 {
		busyTimeoutMs = 5000
	}

	dsn := cleanPath
	if cleanPath != ":memory:" {
		params := url.Values{}
		params.Add("_pragma", fmt.Sprintf("busy_timeout=%d", busyTimeoutMs))
		params.Add("_pragma", "foreign_keys=ON")
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn += separator + params.Encode()
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf(
			"opening SQLite database at %s: %w",
			cleanPath,
			err,
		)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	adapter := &SQLiteAdapter{
		db:   sqlDB,
		path: cleanPath,
	}

	if err := adapter.configurePragmas(ctx, walMode, busyTimeoutMs); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			return nil, fmt.Errorf("closing db (%w) on pragma error: %v", err, closeErr)
		}
		return nil, fmt.Errorf("configuring database pragmas: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			return nil, fmt.Errorf("closing db (%w) on ping error: %v", err, closeErr)
		}
		return nil, fmt.Errorf("pinging SQLite database: %w", err)
	}

	return adapter, nil
}

func (a *SQLiteAdapter) configurePragmas(
	ctx context.Context,
	walMode bool,
	busyTimeoutMs int,
) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		fmt.Sprintf("PRAGMA busy_timeout = %d;", busyTimeoutMs),
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -64000;",
	}

	if walMode && a.path != ":memory:" {
		pragmas = append([]string{"PRAGMA journal_mode = WAL;"}, pragmas...)
	}

	for _, pragma := range pragmas {
		if _, err := a.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("executing pragma %q: %w", pragma, err)
		}
	}

	return nil
}

// Driver returns the canonical driver name.
func (a *SQLiteAdapter) Driver() string {
	return "sqlite"
}

// Path returns the canonical filesystem path of the database.
func (a *SQLiteAdapter) Path() string {
	return a.path
}

// Ping checks connectivity to the SQLite database.
func (a *SQLiteAdapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// Close terminates the connection pool.
func (a *SQLiteAdapter) Close() error {
	return a.db.Close()
}

// withinTransaction executes a callback within an isolated transaction.
func (a *SQLiteAdapter) withinTransaction(
	ctx context.Context,
	fn func(tx *sql.Tx) error,
) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}

	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			_ = rbErr
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

// Migrate executes all embedded SQLite schema migrations.
func (a *SQLiteAdapter) Migrate(ctx context.Context) error {
	subFS, err := fs.Sub(embeddedSQLiteSchemaFS, "schema")
	if err != nil {
		return fmt.Errorf("accessing embedded schema directory: %w", err)
	}
	return a.migrateFS(ctx, subFS)
}

func (a *SQLiteAdapter) migrateFS(ctx context.Context, fsys fs.FS) error {
	if err := a.initMigrationTable(ctx); err != nil {
		return fmt.Errorf("initializing schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("reading migration scripts: %w", err)
	}

	type migFile struct {
		version  int
		fileName string
	}

	var files []migFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}

		ver, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil {
			continue
		}

		files = append(files, migFile{version: ver, fileName: entry.Name()})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	applied, err := a.getAppliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}

	for _, file := range files {
		if applied[file.version] {
			continue
		}

		data, readErr := fs.ReadFile(fsys, file.fileName)
		if readErr != nil {
			return fmt.Errorf("reading migration file %s: %w", file.fileName, readErr)
		}

		if err := a.applyMigrationScript(
			ctx,
			file.version,
			file.fileName,
			string(data),
		); err != nil {
			return fmt.Errorf(
				"applying migration %s (%d): %w",
				file.fileName,
				file.version,
				err,
			)
		}
	}

	return nil
}

func (a *SQLiteAdapter) initMigrationTable(ctx context.Context) error {
	query := `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	_, err := a.db.ExecContext(ctx, query)
	return err
}

func (a *SQLiteAdapter) getAppliedVersions(
	ctx context.Context,
) (map[int]bool, error) {
	rows, err := a.db.QueryContext(
		ctx,
		"SELECT version FROM schema_migrations ORDER BY version ASC",
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	applied := make(map[int]bool)
	for rows.Next() {
		var ver int
		if err := rows.Scan(&ver); err != nil {
			return nil, err
		}
		applied[ver] = true
	}

	return applied, rows.Err()
}

func (a *SQLiteAdapter) applyMigrationScript(
	ctx context.Context,
	version int,
	name string,
	sqlScript string,
) error {
	return a.withinTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, sqlScript); err != nil {
			return fmt.Errorf("executing migration script: %w", err)
		}

		recordQuery := "INSERT INTO schema_migrations (version, name, applied_at) " +
			"VALUES (?, ?, CURRENT_TIMESTAMP)"
		if _, err := tx.ExecContext(
			ctx,
			recordQuery,
			version,
			filepath.Base(name),
		); err != nil {
			return fmt.Errorf("recording migration history: %w", err)
		}

		return nil
	})
}

// AppliedMigrations queries the historical record of applied migrations.
func (a *SQLiteAdapter) AppliedMigrations(
	ctx context.Context,
) ([]MigrationRecord, error) {
	query := "SELECT version, name, applied_at FROM schema_migrations " +
		"ORDER BY version ASC"
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	var records []MigrationRecord
	for rows.Next() {
		var rec MigrationRecord
		var appliedStr string
		if err := rows.Scan(&rec.Version, &rec.Name, &appliedStr); err != nil {
			return nil, err
		}
		if t, pErr := time.Parse(time.RFC3339, appliedStr); pErr == nil {
			rec.AppliedAt = t
		} else if t, pErr := time.Parse(
			"2006-01-02 15:04:05",
			appliedStr,
		); pErr == nil {
			rec.AppliedAt = t
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}

// Status inspects the operational metrics and diagnostics of the SQLite
// database.
func (a *SQLiteAdapter) Status(ctx context.Context) (*Status, error) {
	st := &Status{
		Driver:       "sqlite",
		DatabasePath: a.path,
	}

	a.populateFileMetrics(st)

	if err := a.populatePragmas(ctx, st); err != nil {
		return nil, err
	}

	if err := a.populateMigrationMetrics(ctx, st); err != nil {
		return nil, err
	}

	return st, nil
}

func (a *SQLiteAdapter) populateFileMetrics(st *Status) {
	if a.path == ":memory:" {
		st.FileExists = true
		return
	}

	info, err := os.Stat(a.path)
	if err == nil {
		st.FileExists = true
		st.FileSizeBytes = info.Size()
	}
}

func (a *SQLiteAdapter) populatePragmas(
	ctx context.Context,
	st *Status,
) error {
	var integrity string
	if err := a.db.QueryRowContext(ctx, "PRAGMA integrity_check;").
		Scan(&integrity); err != nil {
		return fmt.Errorf("checking integrity: %w", err)
	}
	st.IntegrityOK = (integrity == "ok")

	if err := a.db.QueryRowContext(ctx, "PRAGMA journal_mode;").
		Scan(&st.JournalMode); err != nil {
		return fmt.Errorf("checking journal_mode: %w", err)
	}

	var fk int
	if err := a.db.QueryRowContext(ctx, "PRAGMA foreign_keys;").
		Scan(&fk); err != nil {
		return fmt.Errorf("checking foreign_keys: %w", err)
	}
	st.ForeignKeysOn = (fk == 1)

	if err := a.db.QueryRowContext(ctx, "PRAGMA busy_timeout;").
		Scan(&st.BusyTimeoutMs); err != nil {
		return fmt.Errorf("checking busy_timeout: %w", err)
	}

	tablesQuery := "SELECT COUNT(*) FROM sqlite_master " +
		"WHERE type = 'table' AND name NOT LIKE 'sqlite_%';"
	if err := a.db.QueryRowContext(ctx, tablesQuery).
		Scan(&st.TablesCount); err != nil {
		return fmt.Errorf("counting tables: %w", err)
	}

	return nil
}

func (a *SQLiteAdapter) populateMigrationMetrics(
	ctx context.Context,
	st *Status,
) error {
	var migTableExists int
	checkQuery := "SELECT COUNT(*) FROM sqlite_master " +
		"WHERE type = 'table' AND name = 'schema_migrations';"
	if err := a.db.QueryRowContext(ctx, checkQuery).
		Scan(&migTableExists); err != nil {
		return fmt.Errorf("checking migrations table existence: %w", err)
	}

	if migTableExists == 0 {
		return nil
	}

	statsQuery := "SELECT COUNT(*), COALESCE(MAX(version), 0) " +
		"FROM schema_migrations;"
	if err := a.db.QueryRowContext(ctx, statsQuery).
		Scan(&st.MigrationCount, &st.LatestVersion); err != nil {
		return fmt.Errorf("querying migration stats: %w", err)
	}

	var appliedStr sql.NullString
	lastQuery := "SELECT applied_at FROM schema_migrations " +
		"ORDER BY version DESC LIMIT 1;"
	if err := a.db.QueryRowContext(ctx, lastQuery).
		Scan(&appliedStr); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("querying last migration time: %w", err)
	}

	if appliedStr.Valid {
		parseMigrationTime(appliedStr.String, st)
	}

	return nil
}

func parseMigrationTime(raw string, st *Status) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		st.LastMigrationAt = t
		return
	}
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		st.LastMigrationAt = t
	}
}
