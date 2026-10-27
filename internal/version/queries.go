// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package version

import (
	"context"
	"errors"
	"fmt"
)

// ErrNilProvider is returned when constructing QueryService with an uninitialized provider.
var ErrNilProvider = errors.New("metadata provider cannot be nil")

// GetVersionQuery encapsulates input parameters for retrieving version metadata.
type GetVersionQuery struct {
	AppName             string
	IncludeDependencies bool
}

// QueryService implements the QueryHandler inbound port, coordinating version metadata resolution.
type QueryService struct {
	provider MetadataProvider
}

// NewQueryService constructs a new QueryService backed by the given MetadataProvider.
func NewQueryService(provider MetadataProvider) (*QueryService, error) {
	if provider == nil {
		return nil, ErrNilProvider
	}
	return &QueryService{provider: provider}, nil
}

// DefaultService constructs a QueryService configured with the RuntimeMetadataProvider adapter.
func DefaultService() *QueryService {
	svc, err := NewQueryService(NewRuntimeMetadataProvider())
	if err != nil {
		// Invariant: NewRuntimeMetadataProvider() never returns nil
		panic(err)
	}
	return svc
}

// GetVersion is a convenience wrapper for Handle.
func (s *QueryService) GetVersion(ctx context.Context, query GetVersionQuery) (Info, error) {
	return s.Handle(ctx, query)
}

// Handle executes the GetVersionQuery, retrieving build metadata and optional dependencies.
func (s *QueryService) Handle(ctx context.Context, query GetVersionQuery) (Info, error) {
	build, err := s.provider.GetBuildMetadata(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("failed to retrieve build metadata: %w", err)
	}

	if err := build.Validate(); err != nil {
		return Info{}, fmt.Errorf("build metadata validation failed: %w", err)
	}

	appName := query.AppName
	if appName == "" {
		appName = "Crucible"
	}

	info := Info{
		AppName: appName,
		Build:   build,
	}

	if query.IncludeDependencies {
		deps, err := s.provider.GetDependencies(ctx)
		if err != nil {
			return Info{}, fmt.Errorf("failed to retrieve dependencies: %w", err)
		}
		info.Dependencies = deps
	}

	return info, nil
}
