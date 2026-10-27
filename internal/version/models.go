// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package version defines domain models, hexagonal ports, and CQRS query services
// for Crucible build, runtime, and dependency version metadata.
package version

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidVersion indicates that build metadata failed semantic validation.
var ErrInvalidVersion = errors.New("invalid build version metadata")

// BuildMetadata encapsulates compile-time linker variables and runtime platform attributes.
type BuildMetadata struct {
	Version      string `json:"version"`
	GitBranch    string `json:"git_branch"`
	GitCommit    string `json:"git_commit"`
	GitTreeState string `json:"git_tree_state"`
	BuildDate    string `json:"build_date"`
	GoVersion    string `json:"go_version"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
}

// Validate checks that required build metadata fields are non-empty.
func (m BuildMetadata) Validate() error {
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("%w: version cannot be empty", ErrInvalidVersion)
	}
	if strings.TrimSpace(m.Platform) == "" {
		return fmt.Errorf("%w: platform cannot be empty", ErrInvalidVersion)
	}
	return nil
}

// DependencyInfo represents a Go module dependency resolved from runtime/debug info.
type DependencyInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Sum     string `json:"sum,omitempty"`
}

// Info aggregates application metadata, compile-time build parameters, and dependency tree.
type Info struct {
	AppName      string           `json:"app_name"`
	Build        BuildMetadata    `json:"build"`
	Dependencies []DependencyInfo `json:"dependencies,omitempty"`
}

// String returns a formatted multiline text representation of version info.
func (i Info) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s\n", i.AppName, i.Build.Version))
	sb.WriteString(fmt.Sprintf("  Git Commit:     %s\n", i.Build.GitCommit))
	sb.WriteString(fmt.Sprintf("  Git Branch:     %s\n", i.Build.GitBranch))
	sb.WriteString(fmt.Sprintf("  Git Tree State: %s\n", i.Build.GitTreeState))
	sb.WriteString(fmt.Sprintf("  Build Date:     %s\n", i.Build.BuildDate))
	sb.WriteString(fmt.Sprintf("  Go Version:     %s\n", i.Build.GoVersion))
	sb.WriteString(fmt.Sprintf("  Compiler:       %s\n", i.Build.Compiler))
	sb.WriteString(fmt.Sprintf("  Platform:       %s", i.Build.Platform))
	return sb.String()
}

// Short returns a concise, single-line version identifier.
func (i Info) Short() string {
	return fmt.Sprintf("%s %s (%s, %s)", i.AppName, i.Build.Version, i.Build.GitCommit, i.Build.Platform)
}

// JSON returns an indented JSON string representation.
func (i Info) JSON() (string, error) {
	bytes, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal version info: %w", err)
	}
	return string(bytes), nil
}
