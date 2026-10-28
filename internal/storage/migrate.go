// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed schema/*.sql
var embeddedSchemaFS embed.FS

// MigrationRecord tracks an applied SQL schema migration version.
type MigrationRecord struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

// Migrate executes all embedded raw SQL schema migrations in numerical order.
func Migrate(ctx context.Context, db *sql.DB) error {
	subFS, err := fs.Sub(embeddedSchemaFS, "schema")
	if err != nil {
		return fmt.Errorf("accessing embedded schema directory: %w", err)
	}
	return MigrateFS(ctx, db, subFS)
}

// MigrateFS executes all *.sql migration scripts located in the provided
// fs.FS.
func MigrateFS(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	if db == nil {
		return errors.New("database connection pool cannot be nil")
	}

	if err := initMigrationTable(ctx, db); err != nil {
		return fmt.Errorf("initializing schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("reading migration scripts from filesystem: %w", err)
	}

	type migrationFile struct {
		version  int
		fileName string
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		name := entry.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}

		ver, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil {
			continue
		}

		files = append(files, migrationFile{version: ver, fileName: name})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	applied, err := GetAppliedVersions(ctx, db)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}

	for _, file := range files {
		if applied[file.version] {
			continue
		}

		data, err := fs.ReadFile(fsys, file.fileName)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", file.fileName, err)
		}

		if err := applyMigration(ctx, db, file.version, file.fileName, string(data)); err != nil {
			return fmt.Errorf("applying migration %s (version %d): %w", file.fileName, file.version, err)
		}
	}

	return nil
}

func initMigrationTable(ctx context.Context, db *sql.DB) error {
	query := `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	_, err := db.ExecContext(ctx, query)
	return err
}

// GetAppliedVersions returns a set of integer versions that have already run.
func GetAppliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version ASC")
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

// GetAppliedMigrations returns the complete audit log of applied migrations.
func GetAppliedMigrations(ctx context.Context, db *sql.DB) ([]MigrationRecord, error) {
	query := "SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC"
	rows, err := db.QueryContext(ctx, query)
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
		if t, parseErr := time.Parse(time.RFC3339, appliedStr); parseErr == nil {
			rec.AppliedAt = t
		} else if t, parseErr := time.Parse("2006-01-02 15:04:05", appliedStr); parseErr == nil {
			rec.AppliedAt = t
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}

func applyMigration(ctx context.Context, db *sql.DB, version int, name string, sqlScript string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			_ = rbErr
		}
	}()

	// Execute raw external SQL migration script
	if _, err := tx.ExecContext(ctx, sqlScript); err != nil {
		return fmt.Errorf("executing migration script: %w", err)
	}

	recordQuery := "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, CURRENT_TIMESTAMP)"
	if _, err := tx.ExecContext(ctx, recordQuery, version, filepath.Base(name)); err != nil {
		return fmt.Errorf("recording migration history: %w", err)
	}

	return tx.Commit()
}
