// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// OSFileSystemAdapter implements SourceReader and FileWriter using the local
// OS.
type OSFileSystemAdapter struct{}

// NewOSFileSystemAdapter constructs a new OSFileSystemAdapter.
func NewOSFileSystemAdapter() *OSFileSystemAdapter {
	return &OSFileSystemAdapter{}
}

// ReadConfigFile reads the file at path from the local filesystem.
func (a *OSFileSystemAdapter) ReadConfigFile(_ context.Context, path string) ([]byte, error) {
	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath) // #nosec G304 - Config path explicitly specified by operator
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", cleanPath, err)
	}
	return data, nil
}

// FileExists verifies if a file exists at the given filesystem path.
func (a *OSFileSystemAdapter) FileExists(_ context.Context, path string) (bool, error) {
	cleanPath := filepath.Clean(path)
	info, err := os.Stat(cleanPath)
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat path %s: %w", cleanPath, err)
}

// WriteConfigFile writes data to path atomically using a temporary file in the
// same directory.
func (a *OSFileSystemAdapter) WriteConfigFile(_ context.Context, path string, data []byte, perm fs.FileMode) error {
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)

	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".crucible-cfg-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmpFile.Name()

	committed := false
	defer func() {
		if !committed {
			if closeErr := tmpFile.Close(); closeErr != nil {
				_ = closeErr
			}
			if rmErr := os.Remove(tmpName); rmErr != nil && !os.IsNotExist(rmErr) {
				_ = rmErr
			}
		}
	}()

	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", tmpName, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("writing to %s: %w", tmpName, err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("syncing file %s: %w", tmpName, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing file %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, cleanPath); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpName, cleanPath, err)
	}

	committed = true
	return nil
}
