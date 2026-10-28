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

func TestCrucibledDBCommands(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "crucible_test.db")

	tests := []struct {
		name       string
		args       []string
		wantSubstr string
	}{
		{
			name:       "db help",
			args:       []string{"db", "--help"},
			wantSubstr: "Inspect and manage the SQLite database lifecycle",
		},
		{
			name:       "db migrate help",
			args:       []string{"db", "migrate", "--help"},
			wantSubstr: "Apply all pending embedded schema migrations",
		},
		{
			name:       "db status help",
			args:       []string{"db", "status", "--help"},
			wantSubstr: "Display operational metrics, WAL mode, integrity",
		},
		{
			name:       "db migrate initial run",
			args:       []string{"db", "migrate", "--database-path", dbFile},
			wantSubstr: "Database successfully migrated",
		},
		{
			name:       "db migrate verifies 001_initial.sql",
			args:       []string{"db", "migrate", "--database-path", dbFile},
			wantSubstr: "001_initial.sql",
		},
		{
			name:       "db status post-migration",
			args:       []string{"db", "status", "--database-path", dbFile},
			wantSubstr: "Schema Version",
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

func TestCrucibledDBStatus_OutputDetails(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "details_test.db")

	// 1. Run migrations
	migCmd := newRootCmd()
	var migBuf bytes.Buffer
	migCmd.SetOut(&migBuf)
	migCmd.SetErr(&migBuf)
	migCmd.SetArgs([]string{"db", "migrate", "-d", dbFile})
	if err := migCmd.Execute(); err != nil {
		t.Fatalf("migrate command failed: %v", err)
	}

	migOutput := migBuf.String()
	for _, expectedMigration := range []string{
		"001_initial.sql",
		"002_indexes.sql",
		"003_audit_ledger.sql",
	} {
		if !strings.Contains(migOutput, expectedMigration) {
			t.Errorf("expected migration table to contain %q:\n%s",
				expectedMigration, migOutput)
		}
	}

	// 2. Query status
	statusCmd := newRootCmd()
	var statusBuf bytes.Buffer
	statusCmd.SetOut(&statusBuf)
	statusCmd.SetErr(&statusBuf)
	statusCmd.SetArgs([]string{"db", "status", "-d", dbFile})
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("status command failed: %v", err)
	}

	statusOutput := statusBuf.String()
	expectedFields := []string{
		"Database Path",
		"Integrity Check",
		"Journal Mode",
		"Foreign Keys",
		"Busy Timeout",
		"Schema Version",
		"Tables Count",
		"Applied Migrations",
	}

	for _, field := range expectedFields {
		if !strings.Contains(statusOutput, field) {
			t.Errorf("expected status output to contain %q:\n%s",
				field, statusOutput)
		}
	}
}
