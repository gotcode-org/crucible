// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package version

import (
	"context"
	"runtime"
	"runtime/debug"
)

// Default compile-time values injected via linker -ldflags during build.
var (
	gitVersion   = "v0.1.0-dev"
	gitCommit    = "unknown"
	gitBranch    = "main"
	gitTreeState = "clean"
	buildDate    = "unknown"
)

// RuntimeMetadataProvider implements the MetadataProvider port by querying compile-time
// variables, Go runtime constants, and embedded debug build information.
type RuntimeMetadataProvider struct{}

// NewRuntimeMetadataProvider constructs a new RuntimeMetadataProvider adapter.
func NewRuntimeMetadataProvider() *RuntimeMetadataProvider {
	return &RuntimeMetadataProvider{}
}

// GetBuildMetadata extracts compile-time linker variables and runtime environment properties.
func (r *RuntimeMetadataProvider) GetBuildMetadata(_ context.Context) (BuildMetadata, error) {
	return BuildMetadata{
		Version:      gitVersion,
		GitBranch:    gitBranch,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
	}, nil
}

// GetDependencies resolves third-party Go modules from embedded debug build info.
func (r *RuntimeMetadataProvider) GetDependencies(_ context.Context) ([]DependencyInfo, error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, nil
	}

	deps := make([]DependencyInfo, 0, len(bi.Deps))
	for _, dep := range bi.Deps {
		if dep == nil {
			continue
		}
		deps = append(deps, DependencyInfo{
			Path:    dep.Path,
			Version: dep.Version,
			Sum:     dep.Sum,
		})
	}
	return deps, nil
}
