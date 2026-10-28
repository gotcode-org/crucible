// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// Status provides operational metrics and diagnostics for the SQLite database.
type Status struct {
	DatabasePath    string
	FileExists      bool
	FileSizeBytes   int64
	IntegrityOK     bool
	JournalMode     string
	ForeignKeysOn   bool
	BusyTimeoutMs   int
	LatestVersion   int
	MigrationCount  int
	TablesCount     int
	LastMigrationAt time.Time
}

// Status inspects the underlying SQLite connection pool and metadata.
func (d *DB) Status(ctx context.Context) (*Status, error) {
	st := &Status{
		DatabasePath: d.path,
	}

	populateFileMetrics(st, d.path)

	if err := populatePragmas(ctx, d.db, st); err != nil {
		return nil, err
	}

	if err := populateMigrationMetrics(ctx, d.db, st); err != nil {
		return nil, err
	}

	return st, nil
}

func populateFileMetrics(st *Status, path string) {
	if path == ":memory:" {
		st.FileExists = true
		return
	}

	info, err := os.Stat(path)
	if err == nil {
		st.FileExists = true
		st.FileSizeBytes = info.Size()
	}
}

func populatePragmas(ctx context.Context, db *sql.DB, st *Status) error {
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check;").
		Scan(&integrity); err != nil {
		return fmt.Errorf("checking integrity: %w", err)
	}
	st.IntegrityOK = (integrity == "ok")

	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").
		Scan(&st.JournalMode); err != nil {
		return fmt.Errorf("checking journal_mode: %w", err)
	}

	var fk int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys;").
		Scan(&fk); err != nil {
		return fmt.Errorf("checking foreign_keys: %w", err)
	}
	st.ForeignKeysOn = (fk == 1)

	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout;").
		Scan(&st.BusyTimeoutMs); err != nil {
		return fmt.Errorf("checking busy_timeout: %w", err)
	}

	tablesQuery := "SELECT COUNT(*) FROM sqlite_master " +
		"WHERE type = 'table' AND name NOT LIKE 'sqlite_%';"
	if err := db.QueryRowContext(ctx, tablesQuery).
		Scan(&st.TablesCount); err != nil {
		return fmt.Errorf("counting tables: %w", err)
	}

	return nil
}

func populateMigrationMetrics(
	ctx context.Context,
	db *sql.DB,
	st *Status,
) error {
	var migTableExists int
	checkQuery := "SELECT COUNT(*) FROM sqlite_master " +
		"WHERE type = 'table' AND name = 'schema_migrations';"
	if err := db.QueryRowContext(ctx, checkQuery).
		Scan(&migTableExists); err != nil {
		return fmt.Errorf("checking migrations table existence: %w", err)
	}

	if migTableExists == 0 {
		return nil
	}

	statsQuery := "SELECT COUNT(*), COALESCE(MAX(version), 0) " +
		"FROM schema_migrations;"
	if err := db.QueryRowContext(ctx, statsQuery).
		Scan(&st.MigrationCount, &st.LatestVersion); err != nil {
		return fmt.Errorf("querying migration stats: %w", err)
	}

	var appliedStr sql.NullString
	lastQuery := "SELECT applied_at FROM schema_migrations " +
		"ORDER BY version DESC LIMIT 1;"
	if err := db.QueryRowContext(ctx, lastQuery).
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
