// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config

import (
	"context"
	"io/fs"
)

// QueryHandler defines the inbound hexagonal port for configuration read
// queries.
type QueryHandler interface {
	// GetServerConfig resolves effective ServerConfig applying the 4-tier
	// precedence.
	GetServerConfig(ctx context.Context, query GetServerConfigQuery) (*ServerConfig, error)

	// GetClientConfig resolves effective ClientConfig and active profile
	// settings.
	GetClientConfig(ctx context.Context, query GetClientConfigQuery) (*ClientConfig, error)

	// ValidateServerConfig checks syntax and domain validation rules for server
	// configs.
	ValidateServerConfig(ctx context.Context, query ValidateConfigQuery) error

	// ValidateClientConfig checks syntax and domain validation rules for client
	// configs.
	ValidateClientConfig(ctx context.Context, query ValidateConfigQuery) error

	// GenerateSystemdUnit renders a production systemd service unit.
	GenerateSystemdUnit(ctx context.Context, query GenerateSystemdUnitQuery) (string, error)
}

// CommandHandler defines the inbound hexagonal port for configuration write
// commands.
type CommandHandler interface {
	// InitServerConfig generates and writes an initial server configuration file.
	InitServerConfig(ctx context.Context, cmd InitServerConfigCommand) (string, error)

	// InitClientConfig generates and writes an initial client configuration file.
	InitClientConfig(ctx context.Context, cmd InitClientConfigCommand) (string, error)

	// SetClientProfile creates or updates a named profile in client
	// configuration.
	SetClientProfile(ctx context.Context, cmd SetClientProfileCommand) error
}

// SourceReader defines the outbound hexagonal port for reading files from
// storage.
type SourceReader interface {
	// ReadConfigFile reads raw bytes of the file located at path.
	ReadConfigFile(ctx context.Context, path string) ([]byte, error)

	// FileExists checks whether the file at path exists.
	FileExists(ctx context.Context, path string) (bool, error)
}

// EnvReader defines the outbound hexagonal port for reading environment
// variables.
type EnvReader interface {
	// LookupEnv retrieves the value of the environment variable named by key.
	LookupEnv(key string) (string, bool)
}

// FileWriter defines the outbound hexagonal port for writing files to storage.
type FileWriter interface {
	// WriteConfigFile writes data to path with the specified filesystem
	// permissions.
	WriteConfigFile(ctx context.Context, path string, data []byte, perm fs.FileMode) error
}

// SystemdUnitRenderer defines the outbound hexagonal port for rendering
// systemd service units.
type SystemdUnitRenderer interface {
	// Render produces a formatted systemd unit file from the provided options.
	Render(opts SystemdOptions) (string, error)
}
