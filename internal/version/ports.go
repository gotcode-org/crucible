// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package version

import "context"

// MetadataProvider defines the outbound hexagonal port for querying build environment
// and runtime dependency information.
type MetadataProvider interface {
	// GetBuildMetadata retrieves compile-time linker variables and runtime host properties.
	GetBuildMetadata(ctx context.Context) (BuildMetadata, error)

	// GetDependencies returns the list of embedded module dependencies if available.
	GetDependencies(ctx context.Context) ([]DependencyInfo, error)
}

// QueryHandler defines the inbound hexagonal port for handling version read queries.
type QueryHandler interface {
	// Handle processes a GetVersionQuery and returns aggregated version info.
	Handle(ctx context.Context, query GetVersionQuery) (Info, error)
}
