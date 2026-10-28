// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"fmt"
	"time"
)

// CommandService coordinates write and mutation operations against an Engine
// adapter.
type CommandService struct {
	engine Engine
}

// NewCommandService instantiates a CommandHandler backed by the provided
// Engine port.
func NewCommandService(engine Engine) CommandHandler {
	return &CommandService{
		engine: engine,
	}
}

// RunMigrations applies pending schema migrations via the underlying engine
// adapter.
func (s *CommandService) RunMigrations(
	ctx context.Context,
	_ RunMigrationsCommand,
) (*RunMigrationsResult, error) {
	start := time.Now()

	if err := s.engine.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("executing migrations: %w", err)
	}

	records, err := s.engine.AppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieving applied migrations: %w", err)
	}

	return &RunMigrationsResult{
		Driver:            s.engine.Driver(),
		DatabasePath:      s.engine.Path(),
		AppliedMigrations: records,
		Duration:          time.Since(start),
	}, nil
}
