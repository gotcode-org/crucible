// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCruciblectlCommands(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "cruciblectl.yaml")

	tests := []struct {
		name       string
		args       []string
		wantSubstr string
	}{
		{
			name:       "root help",
			args:       []string{"--help"},
			wantSubstr: "Crucible Operator Command-Line Interface",
		},
		{
			name:       "version command",
			args:       []string{"version"},
			wantSubstr: "cruciblectl",
		},
		{
			name:       "version dependencies flag",
			args:       []string{"version", "--dependencies"},
			wantSubstr: "cruciblectl",
		},
		{
			name:       "config help",
			args:       []string{"config", "--help"},
			wantSubstr: "Manage client connection profiles and credentials",
		},
		{
			name:       "config init",
			args:       []string{"config", "init", "-o", cfgFile, "-s", "https://127.0.0.1:8443", "-t", "cb_pat_test123"},
			wantSubstr: "Client configuration written to",
		},
		{
			name:       "config validate",
			args:       []string{"config", "validate", "-c", cfgFile},
			wantSubstr: "is valid",
		},
		{
			name:       "config use-context",
			args:       []string{"config", "use-context", "default", "-c", cfgFile},
			wantSubstr: "Switched active profile context to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("cmd.Execute() error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q:\n%s", tt.wantSubstr, out)
			}
		})
	}
}
