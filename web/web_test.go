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
	"gotcode.org/crucible/internal/tokens"
	"gotcode.org/crucible/web"
)

func TestWebFS_EmbeddedAssets(t *testing.T) {
	t.Parallel()

	expectedFiles := []string{
		"templates/base.html",
		"templates/dashboard.html",
		"templates/public_dashboard.html",
		"templates/login.html",
		"templates/2fa.html",
		"templates/enforce_2fa.html",
		"templates/enforce_password.html",
		"templates/settings_2fa.html",
		"templates/settings_password.html",
		"templates/settings_users.html",
		"templates/settings_tokens.html",
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
		"account-menu-btn",
		"account-dropdown",
		`data-nav-group="operator"`,
		`data-nav-group="reports"`,
		`data-nav-group="user"`,
		`data-nav-active="true"`,
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(html, substr) {
			t.Errorf("rendered template missing expected substring %q", substr)
		}
	}
}

func TestRenderStandalone_LoginAnd2FA(t *testing.T) {
	t.Parallel()

	var loginBuf bytes.Buffer
	err := web.RenderStandalone(&loginBuf, "login.html", map[string]any{
		"Version":  "v0.1.0",
		"Username": "operator",
		"Error":    "invalid credentials",
	})
	if err != nil {
		t.Fatalf("RenderStandalone login.html failed: %v", err)
	}
	loginHTML := loginBuf.String()
	if !strings.Contains(loginHTML, "Operator Authentication") ||
		!strings.Contains(loginHTML, "invalid credentials") {
		t.Errorf("login template missing expected content")
	}
	if !strings.Contains(loginHTML, "Native SQLite / TOTP 2FA") {
		t.Errorf("login template default missing Native SQLite badge")
	}

	var pamBuf bytes.Buffer
	err = web.RenderStandalone(&pamBuf, "login.html", map[string]any{
		"Version":      "v0.1.0",
		"AuthProvider": "pam",
	})
	if err != nil {
		t.Fatalf("RenderStandalone pam login.html failed: %v", err)
	}
	if !strings.Contains(pamBuf.String(), "Linux PAM / TOTP 2FA") {
		t.Errorf("login template missing Linux PAM badge")
	}

	var twofaBuf bytes.Buffer
	err = web.RenderStandalone(&twofaBuf, "2fa.html", map[string]any{
		"Username":       "operator",
		"ChallengeToken": "ch_test_123",
	})
	if err != nil {
		t.Fatalf("RenderStandalone 2fa.html failed: %v", err)
	}
	twofaHTML := twofaBuf.String()
	if !strings.Contains(twofaHTML, "Two-Factor Verification") ||
		!strings.Contains(twofaHTML, "ch_test_123") {
		t.Errorf("2fa template missing expected content")
	}
}

func TestRenderStandalone_Enforce2FA(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := web.RenderStandalone(&buf, "enforce_2fa.html", map[string]any{
		"OperatorUser": "operator",
		"Error":        "invalid 2fa passcode",
		"EnrollPrep": map[string]any{
			"SecretBase32": "JBSWY3DPEHPK3PXP",
			"QRCodeSVG":    "<svg></svg>",
		},
	})
	if err != nil {
		t.Fatalf("RenderStandalone enforce_2fa.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Two-Factor Authentication Required") ||
		!strings.Contains(html, "JBSWY3DPEHPK3PXP") ||
		!strings.Contains(html, "invalid 2fa passcode") {
		t.Errorf("enforce_2fa template missing expected content")
	}
	if strings.Contains(html, "<aside") ||
		strings.Contains(html, "account-menu-btn") {
		t.Errorf("enforce_2fa should not contain sidebar or account menu")
	}
}

func TestRenderStandalone_EnforcePassword(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := web.RenderStandalone(&buf, "enforce_password.html", map[string]any{
		"OperatorUser": "operator",
		"Version":      "v0.1.0",
		"Error":        "passwords do not match",
		"Next":         "/operator",
	})
	if err != nil {
		t.Fatalf("RenderStandalone enforce_password.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Password Change Required") ||
		!strings.Contains(html, "Current Password") ||
		!strings.Contains(html, "New Password") ||
		!strings.Contains(html, "Confirm New Password") ||
		!strings.Contains(html, "passwords do not match") {
		t.Errorf("enforce_password template missing expected content")
	}
	if strings.Contains(html, "<aside") ||
		strings.Contains(html, "account-menu-btn") {
		t.Errorf("enforce_password should not contain sidebar or account menu")
	}
}

func TestRenderTemplate_Settings2FA(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"Title":        "Security & 2FA",
		"Version":      "v0.1.0",
		"ActiveNav":    "settings_2fa",
		"Breadcrumb":   "SECURITY / 2FA",
		"HTTPBind":     "127.0.0.1",
		"HTTPPort":     8080,
		"DaemonMode":   "standalone",
		"OperatorUser": "root",
		"AuthState":    "AUTHENTICATED",
		"Storage": &storage.Status{
			Driver: "sqlite",
		},
		"Devices": []map[string]any{
			{
				"ID":        "dev_01JBC",
				"Name":      "YubiKey Primary",
				"CreatedAt": time.Now().UTC(),
			},
		},
		"EnrollPrep": map[string]any{
			"SecretBase32": "JBSWY3DPEHPK3PXP",
			"QRCodeSVG":    "<svg></svg>",
		},
		"Enforced": true,
	}

	var buf bytes.Buffer
	if err := web.RenderTemplate(&buf, "settings_2fa.html", data); err != nil {
		t.Fatalf("RenderTemplate settings_2fa.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "YubiKey Primary") ||
		!strings.Contains(html, "JBSWY3DPEHPK3PXP") ||
		!strings.Contains(html, "Two-Factor Authentication Required") {
		t.Errorf("rendered settings_2fa missing expected content")
	}
}

func TestRenderTemplate_SettingsPassword(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"Title":        "Change Password",
		"Version":      "v0.1.0",
		"ActiveNav":    "settings_password",
		"Breadcrumb":   "SETTINGS / PASSWORD",
		"HTTPBind":     "127.0.0.1",
		"HTTPPort":     8080,
		"DaemonMode":   "standalone",
		"OperatorUser": "alice",
		"AuthState":    "AUTHENTICATED",
		"Storage": &storage.Status{
			Driver: "sqlite",
		},
		"Enforced": true,
		"Next":     "/operator",
		"Message":  "Password rotated",
		"Error":    "Invalid credential",
	}

	var buf bytes.Buffer
	if err := web.RenderTemplate(
		&buf,
		"settings_password.html",
		data,
	); err != nil {
		t.Fatalf("RenderTemplate settings_password.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Password Change Required") ||
		!strings.Contains(html, "Change Password") ||
		!strings.Contains(html, "Current Password") ||
		!strings.Contains(html, "New Password") ||
		!strings.Contains(html, "Confirm New Password") {
		t.Errorf("rendered settings_password missing expected content")
	}
}

func TestRenderTemplate_SettingsUsers(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	data := map[string]any{
		"Title":        "User Management",
		"Version":      "v0.1.0",
		"ActiveNav":    "settings_users",
		"Breadcrumb":   "SETTINGS / USERS",
		"HTTPBind":     "127.0.0.1",
		"HTTPPort":     8080,
		"DaemonMode":   "standalone",
		"OperatorUser": "root",
		"OperatorRole": "admin",
		"AuthState":    "AUTHENTICATED",
		"Storage": &storage.Status{
			Driver: "sqlite",
		},
		"Users": []map[string]any{
			{
				"Username":           "alice",
				"Role":               "operator",
				"IsActive":           true,
				"MustChangePassword": true,
				"PasswordChangedAt":  now,
				"LastLogin":          &now,
			},
			{
				"Username":           "bob",
				"Role":               "admin",
				"IsActive":           false,
				"MustChangePassword": false,
				"PasswordChangedAt":  now,
				"LastLogin":          nil,
			},
		},
		"Message": "User alice created",
	}

	var buf bytes.Buffer
	if err := web.RenderTemplate(
		&buf,
		"settings_users.html",
		data,
	); err != nil {
		t.Fatalf("RenderTemplate settings_users.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "User Management") ||
		!strings.Contains(html, "alice") ||
		!strings.Contains(html, "bob") ||
		!strings.Contains(html, `data-nav-group="admin"`) ||
		!strings.Contains(html, "Add New Operator") ||
		!strings.Contains(html, "Reset Operator Password") {
		t.Errorf("rendered settings_users missing expected content")
	}
}

func TestRenderTemplate_SettingsTokens(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	data := map[string]any{
		"Title":        "Personal Access Tokens",
		"Version":      "v0.1.0",
		"ActiveNav":    "settings_tokens",
		"Breadcrumb":   "SETTINGS / TOKENS",
		"HTTPBind":     "127.0.0.1",
		"HTTPPort":     8080,
		"DaemonMode":   "standalone",
		"OperatorUser": "alice",
		"OperatorRole": "admin",
		"AuthState":    "AUTHENTICATED",
		"Storage": &storage.Status{
			Driver: "sqlite",
		},
		"CreatedToken": map[string]any{
			"RawToken": "cb_pat_0123456789abcdef0123456789abcdef",
		},
		"Tokens": []tokens.APIToken{
			{
				ID:          "tok-1",
				Name:        "laptop-cli",
				TokenPrefix: "cb_pat_01234567",
				Username:    "alice",
				Role:        "admin",
				CreatedAt:   now,
				ExpiresAt:   &now,
				Revoked:     false,
			},
			{
				ID:          "tok-2",
				Name:        "ci-worker",
				TokenPrefix: "cb_pat_98765432",
				Username:    "alice",
				Role:        "operator",
				CreatedAt:   now,
				Revoked:     true,
			},
		},
		"Message": "Token generated",
	}

	var buf bytes.Buffer
	if err := web.RenderTemplate(
		&buf,
		"settings_tokens.html",
		data,
	); err != nil {
		t.Fatalf("RenderTemplate settings_tokens.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Personal Access Tokens") ||
		!strings.Contains(html, "laptop-cli") ||
		!strings.Contains(html, "cb_pat_0123456789abcdef") ||
		!strings.Contains(
			html,
			"Dual-Transport Security Architecture",
		) ||
		!strings.Contains(html, "Generate New Token") {
		t.Errorf("rendered settings_tokens missing expected content")
	}
}

func TestRenderStandalone_PublicDashboard(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := web.RenderStandalone(&buf, "public_dashboard.html", map[string]any{
		"Title":           "Public Monitor",
		"Version":         "v0.1.0",
		"HTTPBind":        "127.0.0.1",
		"HTTPPort":        8080,
		"DaemonMode":      "standalone",
		"IsAuthenticated": false,
		"Uptime":          "12m 30s",
	})
	if err != nil {
		t.Fatalf("RenderStandalone public_dashboard.html failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Public Pipeline Monitor") ||
		!strings.Contains(html, "Operator Sign In") ||
		!strings.Contains(html, "Sovereign Execution") {
		t.Errorf("public_dashboard template missing expected content")
	}

	var authBuf bytes.Buffer
	err = web.RenderStandalone(
		&authBuf,
		"public_dashboard.html",
		map[string]any{
			"Title":           "Public Monitor",
			"Version":         "v0.1.0",
			"HTTPBind":        "127.0.0.1",
			"HTTPPort":        8080,
			"DaemonMode":      "standalone",
			"IsAuthenticated": true,
			"OperatorUser":    "alice",
			"OperatorRole":    "admin",
			"Uptime":          "12m 30s",
		},
	)
	if err != nil {
		t.Fatalf("RenderStandalone auth public_dashboard failed: %v", err)
	}
	authHTML := authBuf.String()
	if !strings.Contains(authHTML, "alice") ||
		!strings.Contains(authHTML, "ADMIN") ||
		!strings.Contains(authHTML, "Console") {
		t.Errorf("auth public_dashboard missing expected content")
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
