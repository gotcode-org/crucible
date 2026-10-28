// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import "time"

// Status provides operational metrics and diagnostics for the database engine.
type Status struct {
	Driver          string
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

// MigrationRecord captures metadata for an applied schema migration script.
type MigrationRecord struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

// RunMigrationsCommand encapsulates parameters for executing schema
// migrations.
type RunMigrationsCommand struct {
	TargetVersion int
}

// RunMigrationsResult contains the outcome of a migration execution command.
type RunMigrationsResult struct {
	Driver            string
	DatabasePath      string
	AppliedMigrations []MigrationRecord
	Duration          time.Duration
}

// GetDatabaseStatusQuery requests runtime operational metrics for the
// database.
type GetDatabaseStatusQuery struct{}

// GetAppliedMigrationsQuery requests the history of applied migrations.
type GetAppliedMigrationsQuery struct{}
