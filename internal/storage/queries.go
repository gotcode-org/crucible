// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"fmt"
)

// QueryService coordinates read-only queries against an Engine adapter.
type QueryService struct {
	engine Engine
}

// NewQueryService instantiates a QueryHandler backed by the provided Engine
// port.
func NewQueryService(engine Engine) QueryHandler {
	return &QueryService{
		engine: engine,
	}
}

// GetDatabaseStatus retrieves operational diagnostics, integrity, and metrics.
func (s *QueryService) GetDatabaseStatus(
	ctx context.Context,
	_ GetDatabaseStatusQuery,
) (*Status, error) {
	st, err := s.engine.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying database status: %w", err)
	}
	return st, nil
}

// GetAppliedMigrations returns the chronological record of schema migrations.
func (s *QueryService) GetAppliedMigrations(
	ctx context.Context,
	_ GetAppliedMigrationsQuery,
) ([]MigrationRecord, error) {
	records, err := s.engine.AppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieving applied migrations: %w", err)
	}
	return records, nil
}
