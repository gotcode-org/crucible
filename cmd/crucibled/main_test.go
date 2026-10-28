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

func TestCrucibledCommands(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "crucible.yaml")
	unitFile := filepath.Join(tmpDir, "crucibled.service")

	tests := []struct {
		name       string
		args       []string
		wantSubstr string
	}{
		{
			name:       "root help",
			args:       []string{"--help"},
			wantSubstr: "Crucible Automation Server Daemon",
		},
		{
			name:       "version command",
			args:       []string{"version"},
			wantSubstr: "crucibled",
		},
		{
			name:       "version dependencies flag",
			args:       []string{"version", "--dependencies"},
			wantSubstr: "crucibled",
		},
		{
			name:       "config help",
			args:       []string{"config", "--help"},
			wantSubstr: "Manage and inspect server daemon configuration",
		},
		{
			name:       "config init dev",
			args:       []string{"config", "init", "-o", cfgFile},
			wantSubstr: "Server configuration written to",
		},
		{
			name:       "config validate",
			args:       []string{"config", "validate", "-c", cfgFile},
			wantSubstr: "is valid",
		},
		{
			name:       "systemd help",
			args:       []string{"systemd", "--help"},
			wantSubstr: "Manage and generate systemd integration artifacts",
		},
		{
			name:       "systemd unit stdout",
			args:       []string{"systemd", "unit"},
			wantSubstr: "AmbientCapabilities=CAP_NET_BIND_SERVICE",
		},
		{
			name:       "systemd unit file output",
			args:       []string{"systemd", "unit", "-o", unitFile},
			wantSubstr: "Systemd unit written to",
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
