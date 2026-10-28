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
	"path/filepath"
	"time"

	"gotcode.org/crucible/internal/config"
	_ "modernc.org/sqlite" // Pure Go SQLite driver (CGO_ENABLED=0)
)

// DB encapsulates the connection pool and execution lifecycle for SQLite.
type DB struct {
	db   *sql.DB
	path string
}

// Open initializes a pure Go SQLite database connection pool using
// StorageConfig.
func Open(ctx context.Context, cfg config.StorageConfig) (*DB, error) {
	return OpenPath(ctx, cfg.DatabasePath, cfg.WALMode, cfg.BusyTimeoutMs)
}

// OpenPath initializes an SQLite database at the target path with specified
// pragmas.
func OpenPath(ctx context.Context, dbPath string, walMode bool, busyTimeoutMs int) (*DB, error) {
	if dbPath == "" {
		dbPath = config.DefaultDevDBPath
	}

	cleanPath := filepath.Clean(dbPath)

	// In-memory databases (:memory:) bypass directory creation
	if cleanPath != ":memory:" {
		dir := filepath.Dir(cleanPath)
		// Ensure least-privilege private directory (0700)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
		}
	}

	if busyTimeoutMs <= 0 {
		busyTimeoutMs = 5000
	}

	// Open connection pool using the pure Go modernc driver
	sqlDB, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database at %s: %w", cleanPath, err)
	}

	// Configure connection limits for WAL multi-reader concurrency
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	dbInstance := &DB{
		db:   sqlDB,
		path: cleanPath,
	}

	if err := dbInstance.configurePragmas(ctx, walMode, busyTimeoutMs); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			return nil, fmt.Errorf("configuring pragmas (%w) and closing (%v)", err, closeErr)
		}
		return nil, fmt.Errorf("configuring database pragmas: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging database (%w) and closing (%v)", err, closeErr)
		}
		return nil, fmt.Errorf("pinging SQLite database: %w", err)
	}

	return dbInstance, nil
}

func (d *DB) configurePragmas(ctx context.Context, walMode bool, busyTimeoutMs int) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		fmt.Sprintf("PRAGMA busy_timeout = %d;", busyTimeoutMs),
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -64000;", // 64MB cache
	}

	// In-memory databases do not use WAL journal mode
	if walMode && d.path != ":memory:" {
		pragmas = append([]string{"PRAGMA journal_mode = WAL;"}, pragmas...)
	}

	for _, pragma := range pragmas {
		if _, err := d.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("executing pragma %q: %w", pragma, err)
		}
	}

	return nil
}

// DB exposes the underlying *sql.DB connection pool.
func (d *DB) DB() *sql.DB {
	return d.db
}

// Path returns the canonical filesystem path of the database.
func (d *DB) Path() string {
	return d.path
}

// Ping verifies connectivity to the SQLite database.
func (d *DB) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Migrate executes all embedded schema migrations on this database.
func (d *DB) Migrate(ctx context.Context) error {
	return Migrate(ctx, d.db)
}

// Close gracefully shuts down the database connection pool.
func (d *DB) Close() error {
	return d.db.Close()
}

// WithinTransaction runs fn inside an isolated SQL transaction.
func (d *DB) WithinTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
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
