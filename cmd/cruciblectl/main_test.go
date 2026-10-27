// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCruciblectlCommands(t *testing.T) {
	t.Parallel()

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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
