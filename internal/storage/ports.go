// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
)

// Engine defines the inbound hexagonal port for the SQLite persistence
// subsystem.
type Engine interface {
	// DB returns the underlying standard library SQL connection pool.
	DB() *sql.DB

	// Path returns the canonical filesystem path of the database.
	Path() string

	// Ping checks the health of the SQLite connection pool.
	Ping(ctx context.Context) error

	// Migrate executes external schema migrations embedded in the binary.
	Migrate(ctx context.Context) error

	// Status queries operational diagnostics and metadata for the database.
	Status(ctx context.Context) (*Status, error)

	// Close terminates the connection pool and checkpoints WAL journals.
	Close() error
}

// Transactor defines an interface for executing operations within an isolated
// transaction.
type Transactor interface {
	// WithinTransaction runs fn inside a database transaction, rolling
	// back on error.
	WithinTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error
}
