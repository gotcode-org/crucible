// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gotcode.org/crucible/internal/auth"
)

func TestAuthCommand_Help(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "auth_root_help",
			args:     []string{"auth", "--help"},
			expected: []string{"login", "2fa"},
		},
		{
			name:     "auth_login_help",
			args:     []string{"auth", "login", "--help"},
			expected: []string{"--username", "--passcode", "--server"},
		},
		{
			name:     "auth_2fa_help",
			args:     []string{"auth", "2fa", "--help"},
			expected: []string{"devices", "revoke", "invite"},
		},
		{
			name:     "auth_2fa_devices_help",
			args:     []string{"auth", "2fa", "devices", "--help"},
			expected: []string{"--server", "--token"},
		},
		{
			name:     "auth_2fa_revoke_help",
			args:     []string{"auth", "2fa", "revoke", "--help"},
			expected: []string{"<device-id>", "--token"},
		},
		{
			name:     "auth_2fa_invite_help",
			args:     []string{"auth", "2fa", "invite", "--help"},
			expected: []string{"--username", "--label", "--token"},
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

func handleMockLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Username == "admin" && req.Password == "validpass" {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"requires_2fa": false,
			"session": &auth.Session{
				ID:        "ses_01TESTSESSION",
				Username:  "admin",
				Role:      auth.RoleAdmin,
				ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
			},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if req.Username == "2fa_user" && req.Password == "validpass" {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"requires_2fa":    true,
			"challenge_token": "ch_01TESTCHALLENGE",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func handleMockVerify2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeToken string `json:"challenge_token"`
		Passcode       string `json:"passcode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ChallengeToken == "ch_01TESTCHALLENGE" && req.Passcode == "123456" {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"session": &auth.Session{
				ID:        "ses_01POST2FASESSION",
				Username:  "2fa_user",
				Role:      auth.RoleAdmin,
				ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
			},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	http.Error(w, "invalid passcode", http.StatusUnauthorized)
}

func handleMockDevices(w http.ResponseWriter, r *http.Request) {
	authHdr := r.Header.Get("Authorization")
	if authHdr != "Bearer ses_01TESTSESSION" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]auth.TOTPDevice{
			{
				ID:        "dev_01PHONE",
				Name:      "Work Phone",
				CreatedAt: time.Now().UTC(),
				Enabled:   true,
			},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if r.Method == http.MethodDelete &&
		r.URL.Query().Get("id") == "dev_01PHONE" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func handleMockInvite(w http.ResponseWriter, r *http.Request) {
	authHdr := r.Header.Get("Authorization")
	if authHdr != "Bearer ses_01TESTSESSION" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&auth.TOTPInvite{
		ID:        "inv_01TEST",
		Token:     "tok_invite_secret_123",
		Username:  "root",
		Label:     "Alice Workstation",
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func setupMockAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			handleMockLogin(w, r)
		case "/api/v1/auth/verify-2fa":
			handleMockVerify2FA(w, r)
		case "/api/v1/auth/devices":
			handleMockDevices(w, r)
		case "/api/v1/auth/invite":
			handleMockInvite(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func TestAuthCommand_LoginDirect(t *testing.T) {
	t.Parallel()
	ts := setupMockAuthServer(t)

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader("validpass\n"))
	cmd.SetArgs([]string{
		"auth", "login",
		"--server", ts.URL,
		"-u", "admin",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if !strings.Contains(buf.String(), "ses_01TESTSESSION") {
		t.Errorf("login output missing session id: %s", buf.String())
	}
}

func TestAuthCommand_LoginInteractivePrompt(t *testing.T) {
	t.Parallel()
	ts := setupMockAuthServer(t)

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader("admin\nvalidpass\n"))
	cmd.SetArgs([]string{
		"auth", "login",
		"--server", ts.URL,
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("interactive login failed: %v", err)
	}
	if !strings.Contains(buf.String(), "ses_01TESTSESSION") {
		t.Errorf("login output missing session id: %s", buf.String())
	}
}

func TestAuthCommand_LoginWith2FA(t *testing.T) {
	t.Parallel()
	ts := setupMockAuthServer(t)

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader("validpass\n"))
	cmd.SetArgs([]string{
		"auth", "login",
		"--server", ts.URL,
		"-u", "2fa_user",
		"--passcode", "123456",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("2fa login failed: %v", err)
	}
	if !strings.Contains(buf.String(), "ses_01POST2FASESSION") {
		t.Errorf("2fa login missing session id: %s", buf.String())
	}
}

func TestAuthCommand_DevicesAndRevoke(t *testing.T) {
	t.Parallel()
	ts := setupMockAuthServer(t)

	// List
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"auth", "2fa", "devices",
		"--server", ts.URL,
		"-t", "ses_01TESTSESSION",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("list devices failed: %v", err)
	}
	if !strings.Contains(buf.String(), "dev_01PHONE") {
		t.Errorf("devices missing device id: %s", buf.String())
	}

	// Revoke
	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"auth", "2fa", "revoke", "dev_01PHONE",
		"--server", ts.URL,
		"-t", "ses_01TESTSESSION",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("revoke device failed: %v", err)
	}
	if !strings.Contains(buf.String(), "revoked successfully") {
		t.Errorf("revoke output missing success: %s", buf.String())
	}
}

func TestAuthCommand_Invite(t *testing.T) {
	t.Parallel()
	ts := setupMockAuthServer(t)

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"auth", "2fa", "invite",
		"--server", ts.URL,
		"-t", "ses_01TESTSESSION",
		"--username", "root",
		"--label", "Alice Workstation",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("invite failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "tok_invite_secret_123") ||
		!strings.Contains(out, "Alice Workstation") {
		t.Errorf("invite output missing token or label: %s", out)
	}
}
