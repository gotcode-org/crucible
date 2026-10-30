// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
)

// CommandHandler defines the inbound hexagonal port for database
// write/mutation operations such as applying schema migrations.
type CommandHandler interface {
	// RunMigrations executes pending schema migrations on the database engine.
	RunMigrations(
		ctx context.Context,
		cmd RunMigrationsCommand,
	) (*RunMigrationsResult, error)
}

// QueryHandler defines the inbound hexagonal port for read-only database
// inspection, integrity verification, and status queries.
type QueryHandler interface {
	// GetDatabaseStatus retrieves operational metrics, pragmas, and integrity.
	GetDatabaseStatus(
		ctx context.Context,
		q GetDatabaseStatusQuery,
	) (*Status, error)

	// GetAppliedMigrations returns the chronological record of schema
	// migrations.
	GetAppliedMigrations(
		ctx context.Context,
		q GetAppliedMigrationsQuery,
	) ([]MigrationRecord, error)
}

// Engine defines the outbound hexagonal port for relational database adapters
// (e.g. SQLite, PostgreSQL, MySQL).
type Engine interface {
	// Driver returns the canonical SQL driver name (e.g. "sqlite", "postgres").
	Driver() string

	// Path returns the canonical database filesystem path or DSN endpoint.
	Path() string

	// Ping checks connectivity to the database engine.
	Ping(ctx context.Context) error

	// Migrate executes external schema migrations embedded in the binary.
	Migrate(ctx context.Context) error

	// Status queries operational diagnostics, integrity, and runtime metrics.
	Status(ctx context.Context) (*Status, error)

	// AppliedMigrations retrieves the list of executed migrations.
	AppliedMigrations(ctx context.Context) ([]MigrationRecord, error)

	// Close terminates the connection pool and checkpoints pending journals.
	Close() error
}
