// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config

import "os"

// OSEnvironmentAdapter implements ConfigEnvReader using the operating system
// environment.
type OSEnvironmentAdapter struct{}

// NewOSEnvironmentAdapter constructs a new OSEnvironmentAdapter.
func NewOSEnvironmentAdapter() *OSEnvironmentAdapter {
	return &OSEnvironmentAdapter{}
}

// LookupEnv retrieves the value of the environment variable named by key.
func (a *OSEnvironmentAdapter) LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
