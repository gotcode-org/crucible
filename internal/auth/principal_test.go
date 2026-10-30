// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth_test

import (
	"errors"
	"testing"

	"gotcode.org/crucible/internal/auth"
)

func TestAuthorize_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		principal *auth.Principal
		action    auth.Action
		wantErr   error
	}{
		{
			name:      "nil principal fails with ErrUnauthorized",
			principal: nil,
			action:    auth.ActionTokensRead,
			wantErr:   auth.ErrUnauthorized,
		},
		{
			name: "admin session permits all actions",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialSession,
			},
			action:  auth.ActionUsersWrite,
			wantErr: nil,
		},
		{
			name: "operator session permits operator action",
			principal: &auth.Principal{
				Username:       "op",
				Role:           auth.RoleOperator,
				CredentialKind: auth.CredentialSession,
			},
			action:  auth.ActionTokensWrite,
			wantErr: nil,
		},
		{
			name: "operator session denies admin action",
			principal: &auth.Principal{
				Username:       "op",
				Role:           auth.RoleOperator,
				CredentialKind: auth.CredentialSession,
			},
			action:  auth.ActionUsersWrite,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "viewer session denies operator action",
			principal: &auth.Principal{
				Username:       "viewer",
				Role:           auth.RoleViewer,
				CredentialKind: auth.CredentialSession,
			},
			action:  auth.ActionTokensRead,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "unix socket inherits admin role",
			principal: &auth.Principal{
				Username:       "local-operator",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialUnixSocket,
			},
			action:  auth.ActionSystemAdmin,
			wantErr: nil,
		},
		{
			name: "admin token with wildcard scope permits admin action",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"*"},
			},
			action:  auth.ActionUsersWrite,
			wantErr: nil,
		},
		{
			name: "admin token with exact scope permits action",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"tokens:read", "tokens:write"},
			},
			action:  auth.ActionTokensRead,
			wantErr: nil,
		},
		{
			name: "admin token with resource wildcard scope permits action",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"tokens:*"},
			},
			action:  auth.ActionTokensWrite,
			wantErr: nil,
		},
		{
			name: "admin token restricted by scope is denied unapproved action",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"tokens:read"},
			},
			action:  auth.ActionUsersWrite,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "admin token with read-only tokens denied token write",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"tokens:read"},
			},
			action:  auth.ActionTokensWrite,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "operator token with wildcard cannot exceed role to admin",
			principal: &auth.Principal{
				Username:       "op",
				Role:           auth.RoleOperator,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"*"},
			},
			action:  auth.ActionUsersWrite,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "operator token with explicit admin scope clipped by role",
			principal: &auth.Principal{
				Username:       "op",
				Role:           auth.RoleOperator,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"users:write"},
			},
			action:  auth.ActionUsersWrite,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "token with empty scopes is denied all actions",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{},
			},
			action:  auth.ActionTokensRead,
			wantErr: auth.ErrForbidden,
		},
		{
			name: "token with unknown scope fails closed",
			principal: &auth.Principal{
				Username:       "admin",
				Role:           auth.RoleAdmin,
				CredentialKind: auth.CredentialToken,
				Scopes:         []string{"unknown:scope"},
			},
			action:  auth.ActionTokensRead,
			wantErr: auth.ErrForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := auth.Authorize(tc.principal, tc.action)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("expected nil err, got %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}

			// Also verify Principal.Can() parity
			can := tc.principal.Can(tc.action)
			if tc.wantErr == nil && !can {
				t.Fatal("expected Principal.Can() to be true")
			}
			if tc.wantErr != nil && can {
				t.Fatal("expected Principal.Can() to be false")
			}
		})
	}
}

func TestPrincipal_HasScope(t *testing.T) {
	t.Parallel()

	var nilPrincipal *auth.Principal
	if nilPrincipal.HasScope("*") {
		t.Fatal("nil principal should not have any scope")
	}

	wildcardPrincipal := &auth.Principal{
		Scopes: []string{"*"},
	}
	if !wildcardPrincipal.HasScope("tokens:read") {
		t.Fatal("wildcard principal should match any scope query")
	}

	scopedPrincipal := &auth.Principal{
		Scopes: []string{"tokens:read", "system:read"},
	}
	if !scopedPrincipal.HasScope("tokens:read") {
		t.Fatal("expected tokens:read scope to be present")
	}
	if scopedPrincipal.HasScope("tokens:write") {
		t.Fatal("tokens:write scope should not be present")
	}
}
