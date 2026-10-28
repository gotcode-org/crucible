// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package web_test

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"time"

	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/web"
)

func TestWebFS_EmbeddedAssets(t *testing.T) {
	t.Parallel()

	expectedFiles := []string{
		"templates/base.html",
		"templates/dashboard.html",
		"static/css/style.css",
		"static/js/app.js",
	}

	for _, path := range expectedFiles {
		t.Run(path, func(t *testing.T) {
			info, err := fs.Stat(web.FS, path)
			if err != nil {
				t.Fatalf("embedded file %s not found: %v", path, err)
			}
			if info.IsDir() {
				t.Errorf("expected %s to be a file, got directory", path)
			}
			if info.Size() == 0 {
				t.Errorf("expected %s to have non-zero size", path)
			}
		})
	}
}

func TestRenderTemplate_Dashboard(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	data := map[string]any{
		"Title":        "Command Overview",
		"Version":      "v0.1.0",
		"ActiveNav":    "overview",
		"Breadcrumb":   "COMMAND_OVERVIEW",
		"HTTPBind":     "127.0.0.1",
		"HTTPPort":     8080,
		"SocketPath":   "/run/crucibled/crucible.sock",
		"DaemonMode":   "standalone",
		"OperatorUser": "operator@localhost",
		"AuthState":    "LOCAL_SESSION",
		"Uptime":       "42m 12s",
		"StartedAt":    now.Add(-42 * time.Minute),
		"Storage": &storage.Status{
			Driver:          "sqlite",
			DatabasePath:    "/var/lib/crucible/crucible.db",
			FileExists:      true,
			FileSizeBytes:   10240,
			IntegrityOK:     true,
			JournalMode:     "wal",
			ForeignKeysOn:   true,
			BusyTimeoutMs:   5000,
			LatestVersion:   3,
			MigrationCount:  3,
			TablesCount:     8,
			LastMigrationAt: now,
		},
		"Migrations": []storage.MigrationRecord{
			{
				Version:   1,
				Name:      "001_initial.sql",
				AppliedAt: now.Add(-10 * time.Minute),
			},
			{
				Version:   2,
				Name:      "002_indexes.sql",
				AppliedAt: now.Add(-5 * time.Minute),
			},
			{
				Version:   3,
				Name:      "003_audit_ledger.sql",
				AppliedAt: now,
			},
		},
	}

	var buf bytes.Buffer
	if err := web.RenderTemplate(&buf, "dashboard.html", data); err != nil {
		t.Fatalf("RenderTemplate failed: %v", err)
	}

	html := buf.String()
	expectedSubstrings := []string{
		"<!DOCTYPE html>",
		"CRUCIBLE",
		"RUNNER ENGINE",
		"Command Overview",
		"v0.1.0",
		"127.0.0.1:8080",
		"SQLITE",
		"001_initial.sql",
		"002_indexes.sql",
		"003_audit_ledger.sql",
		"/run/crucibled/crucible.sock",
		"GET /healthz",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(html, substr) {
			t.Errorf("rendered template missing expected substring %q", substr)
		}
	}
}

func TestRenderTemplate_NotFound(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := web.RenderTemplate(&buf, "nonexistent.html", nil)
	if err == nil {
		t.Fatalf("expected error for nonexistent template, got nil")
	}
}
