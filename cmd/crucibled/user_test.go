// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gotcode.org/crucible/internal/storage"
)

func setupTestDBForUserCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "user_cli_test.db")

	ctx := context.Background()
	engine, err := openEngine(ctx, "", dbPath)
	if err != nil {
		t.Fatalf("openEngine error: %v", err)
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	cmdSvc := storage.NewCommandService(engine)
	if _, err := cmdSvc.RunMigrations(
		ctx,
		storage.RunMigrationsCommand{},
	); err != nil {
		t.Fatalf("RunMigrations error: %v", err)
	}

	return dbPath
}

func TestUserCommands_Help(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "user_root_help",
			args:     []string{"user", "--help"},
			expected: []string{"add", "list", "passwd", "delete"},
		},
		{
			name:     "user_add_help",
			args:     []string{"user", "add", "--help"},
			expected: []string{"--username", "--role", "--password"},
		},
		{
			name:     "user_list_help",
			args:     []string{"user", "list", "--help"},
			expected: []string{"--database-path"},
		},
		{
			name:     "user_passwd_help",
			args:     []string{"user", "passwd", "--help"},
			expected: []string{"--password"},
		},
		{
			name:     "user_delete_help",
			args:     []string{"user", "delete", "--help"},
			expected: []string{"--force"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tc.args)

			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("executing %v: %v", tc.args, err)
			}

			out := buf.String()
			for _, exp := range tc.expected {
				if !strings.Contains(out, exp) {
					t.Errorf("output missing %q: %s", exp, out)
				}
			}
		})
	}
}

func TestUserCommands_Lifecycle(t *testing.T) {
	t.Parallel()
	dbPath := setupTestDBForUserCLI(t)
	ctx := context.Background()

	// 1. Initial list empty
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "list", "-d", dbPath})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("list empty error: %v", err)
	}
	if !strings.Contains(buf.String(), "No users found") {
		t.Errorf("expected No users found, got: %s", buf.String())
	}

	// 2. Add user admin
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"user", "add",
		"-u", "admin",
		"-r", "admin",
		"-p", "initialPassword123!",
		"-d", dbPath,
	})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("add user error: %v", err)
	}
	if !strings.Contains(buf.String(), "created successfully") {
		t.Errorf("expected success message, got: %s", buf.String())
	}

	// 3. List users
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "list", "-d", dbPath})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("list users error: %v", err)
	}
	if !strings.Contains(buf.String(), "admin") {
		t.Errorf("expected admin in table, got: %s", buf.String())
	}

	// 4. Update password
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"user", "passwd", "admin",
		"-p", "newPassword456!",
		"-d", dbPath,
	})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("passwd error: %v", err)
	}
	if !strings.Contains(buf.String(), "Password updated successfully") {
		t.Errorf("expected success message, got: %s", buf.String())
	}

	// 5. Delete user
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"user", "delete", "admin",
		"-f",
		"-d", dbPath,
	})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if !strings.Contains(buf.String(), "deleted successfully") {
		t.Errorf("expected success message, got: %s", buf.String())
	}

	// 6. List empty again
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"user", "list", "-d", dbPath})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("list empty after delete error: %v", err)
	}
	if !strings.Contains(buf.String(), "No users found") {
		t.Errorf("expected No users found after delete, got: %s", buf.String())
	}
}
