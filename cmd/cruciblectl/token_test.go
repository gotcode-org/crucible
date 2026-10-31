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

	"gotcode.org/crucible/internal/tokens"
)

func TestTokenCommand_Help(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name: "token_root_help",
			args: []string{"token", "--help"},
			expected: []string{
				"create", "list", "revoke", "review", "audit",
			},
		},
		{
			name: "token_create_help",
			args: []string{"token", "create", "--help"},
			expected: []string{
				"--name", "--role", "--expires-in", "--server",
			},
		},
		{
			name: "token_list_help",
			args: []string{"token", "list", "--help"},
			expected: []string{
				"--all",
				"--user",
				"--review-required",
				"--server",
			},
		},
		{
			name:     "token_revoke_help",
			args:     []string{"token", "revoke", "--help"},
			expected: []string{"<token-id>", "--server"},
		},
		{
			name:     "token_review_help",
			args:     []string{"token", "review", "--help"},
			expected: []string{"<token-id>", "--server"},
		},
		{
			name:     "token_audit_help",
			args:     []string{"token", "audit", "--help"},
			expected: []string{"--server"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tc.args)

			if err := cmd.ExecuteContext(
				context.Background(),
			); err != nil {
				t.Fatalf("executing %v: %v", tc.args, err)
			}

			out := buf.String()
			for _, exp := range tc.expected {
				if !strings.Contains(out, exp) {
					t.Errorf(
						"output missing %q: %s",
						exp,
						out,
					)
				}
			}
		})
	}
}

func TestTokenCommand_Create(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tokens" ||
				r.Method != http.MethodPost {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			authHdr := r.Header.Get("Authorization")
			if authHdr != "Bearer cb_pat_testsession" {
				http.Error(
					w,
					"unauthorized",
					http.StatusUnauthorized,
				)
				return
			}

			var req struct {
				Name      string `json:"name"`
				Role      string `json:"role"`
				ExpiresIn string `json:"expires_in"`
			}
			if err := json.NewDecoder(
				r.Body,
			).Decode(&req); err != nil {
				http.Error(
					w,
					err.Error(),
					http.StatusBadRequest,
				)
				return
			}

			exp := time.Now().UTC().Add(30 * 24 * time.Hour)
			res := tokens.CreateTokenResult{
				Token: &tokens.APIToken{
					ID:          "tok_01TESTPAT001",
					Name:        req.Name,
					TokenPrefix: "cb_pat_9f82ab12",
					Role:        "operator",
					Username:    "alice",
					ExpiresAt:   &exp,
					CreatedAt:   time.Now().UTC(),
				},
				RawToken: "cb_pat_" +
					"9f82ab1234567890abcdef0123456789",
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(res); err != nil {
				http.Error(
					w,
					err.Error(),
					http.StatusInternalServerError,
				)
			}
		}),
	)
	defer mockServer.Close()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "create",
		"--name", "deploy-token",
		"--role", "operator",
		"--scopes", "*",
		"--server", mockServer.URL,
		"--token", "cb_pat_testsession",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("token create failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(
		out,
		"Personal Access Token created successfully!",
	) ||
		!strings.Contains(out, "tok_01TESTPAT001") ||
		!strings.Contains(out, "deploy-token") ||
		!strings.Contains(
			out,
			"cb_pat_9f82ab1234567890abcdef0123456789",
		) {
		t.Errorf("output missing expected fields: %s", out)
	}
}

func TestTokenCommand_Create_RequiresScopes(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "create",
		"--name", "deploy-token",
		"--role", "operator",
		"--server", "http://127.0.0.1:8080",
		"--token", "cb_pat_testsession",
	})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error when scopes are omitted, got nil")
	}
	if !strings.Contains(err.Error(), "token scopes are required") {
		t.Fatalf(
			"expected 'token scopes are required' error, got: %v",
			err,
		)
	}
}

func TestTokenCommand_List(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)
	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tokens" ||
				r.Method != http.MethodGet {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			toks := []tokens.APIToken{
				{
					ID:          "tok_01TESTLIST1",
					Name:        "workstation-cli",
					TokenPrefix: "cb_pat_11223344",
					Role:        "admin",
					Username:    "alice",
					CreatedAt:   now,
					ExpiresAt:   &exp,
					Revoked:     false,
				},
				{
					ID:          "tok_01TESTLIST2",
					Name:        "ci-builder",
					TokenPrefix: "cb_pat_aabbccdd",
					Role:        "operator",
					Username:    "alice",
					CreatedAt:   now,
					Revoked:     true,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(toks); err != nil {
				http.Error(
					w,
					err.Error(),
					http.StatusInternalServerError,
				)
			}
		}),
	)
	defer mockServer.Close()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "list",
		"--server", mockServer.URL,
		"--token", "cb_pat_valid",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("token list failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "TOKEN ID") ||
		!strings.Contains(out, "workstation-cli") ||
		!strings.Contains(out, "ci-builder") ||
		!strings.Contains(out, "active") ||
		!strings.Contains(out, "revoked") {
		t.Errorf("output missing expected token list content: %s", out)
	}
}

func TestTokenCommand_Revoke(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tokens" ||
				r.Method != http.MethodDelete {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("id") != "tok_01TOREVOKE" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	defer mockServer.Close()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "revoke", "tok_01TOREVOKE",
		"--server", mockServer.URL,
		"--token", "cb_pat_valid",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("token revoke failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(
		out,
		"Token tok_01TOREVOKE revoked successfully.",
	) {
		t.Errorf("output missing expected revocation message: %s", out)
	}
}

func TestTokenCommand_RemoteTokenRequired(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "list",
		"--server", "http://127.0.0.1:9999",
	})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf(
			"expected error when remote TCP has no token",
		)
	}
	if !strings.Contains(err.Error(), "authentication token is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTokenCommand_Review(t *testing.T) {
	t.Parallel()

	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tokens/review" ||
				r.Method != http.MethodPost {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			var req struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(
				r.Body,
			).Decode(&req); err != nil {
				http.Error(
					w,
					err.Error(),
					http.StatusBadRequest,
				)
				return
			}
			if req.ID != "tok_01TOREVIEW" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if encErr := json.NewEncoder(w).Encode(
				map[string]string{
					"status": "reviewed",
					"id":     req.ID,
				},
			); encErr != nil {
				t.Errorf(
					"encoding review response: %v",
					encErr,
				)
			}
		}),
	)
	defer mockServer.Close()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "review", "tok_01TOREVIEW",
		"--server", mockServer.URL,
		"--token", "cb_pat_valid",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("token review failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(
		out,
		"Token tok_01TOREVIEW reviewed successfully.",
	) {
		t.Errorf("output missing expected review message: %s", out)
	}
}

func TestTokenCommand_List_ReviewRequired(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tokens" ||
				r.Method != http.MethodGet {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("review_required") != "true" {
				http.Error(
					w,
					"bad request",
					http.StatusBadRequest,
				)
				return
			}
			toks := []tokens.APIToken{
				{
					ID:             "tok_01LEGACY1",
					Name:           "legacy-pat",
					TokenPrefix:    "cb_pat_99887766",
					Role:           "admin",
					Username:       "alice",
					CreatedAt:      now,
					RequiresReview: true,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if encErr := json.NewEncoder(
				w,
			).Encode(toks); encErr != nil {
				t.Errorf("encoding list response: %v", encErr)
			}
		}),
	)
	defer mockServer.Close()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "list", "--review-required",
		"--server", mockServer.URL,
		"--token", "cb_pat_valid",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("token list --review-required failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "TOKEN ID") ||
		!strings.Contains(out, "legacy-pat") ||
		!strings.Contains(out, "review_required") {
		t.Errorf("output missing review_required status: %s", out)
	}
}

func TestTokenCommand_Audit(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/tokens" ||
				r.Method != http.MethodGet {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("review_required") != "true" ||
				r.URL.Query().Get("all") != "true" {
				http.Error(
					w,
					"bad request",
					http.StatusBadRequest,
				)
				return
			}
			toks := []tokens.APIToken{
				{
					ID:             "tok_01AUDIT1",
					Name:           "audit-legacy-pat",
					TokenPrefix:    "cb_pat_44556677",
					Role:           "operator",
					Username:       "bob",
					CreatedAt:      now,
					RequiresReview: true,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if encErr := json.NewEncoder(
				w,
			).Encode(toks); encErr != nil {
				t.Errorf("encoding list response: %v", encErr)
			}
		}),
	)
	defer mockServer.Close()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"token", "audit",
		"--server", mockServer.URL,
		"--token", "cb_pat_valid",
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("token audit failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "TOKEN ID") ||
		!strings.Contains(out, "audit-legacy-pat") ||
		!strings.Contains(out, "review_required") {
		t.Errorf(
			"audit output missing expected token content: %s",
			out,
		)
	}
}
