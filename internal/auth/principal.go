// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"errors"
	"strings"
)

// CredentialKind identifies the authentication mechanism.
type CredentialKind string

const (
	// CredentialSession represents an interactive browser session.
	CredentialSession CredentialKind = "session"

	// CredentialToken represents a Personal Access Token (PAT).
	CredentialToken CredentialKind = "token"

	// CredentialUnixSocket represents a trusted local operator on the UNIX
	// domain socket.
	CredentialUnixSocket CredentialKind = "unix_socket"
)

// Action represents an authorized operation within the system.
type Action string

const (
	// ActionTokensRead allows reading and listing API tokens.
	ActionTokensRead Action = "tokens:read"

	// ActionTokensWrite allows creating and revoking API tokens.
	ActionTokensWrite Action = "tokens:write"

	// ActionUsersRead allows viewing user accounts and security profiles.
	ActionUsersRead Action = "users:read"

	// ActionUsersWrite allows provisioning, modifying, or deleting users.
	ActionUsersWrite Action = "users:write"

	// ActionAccountRead allows viewing own profile, 2FA devices, and session.
	ActionAccountRead Action = "account:read"

	// ActionAccountWrite allows updating own password and enrolling 2FA.
	ActionAccountWrite Action = "account:write"

	// ActionSystemRead allows viewing system status, metrics, and health.
	ActionSystemRead Action = "system:read"

	// ActionSystemAdmin allows administrative control and migrations.
	ActionSystemAdmin Action = "system:admin"
)

// ErrForbidden indicates that the principal lacks role or scope permissions.
var ErrForbidden = errors.New(
	"forbidden: insufficient role or scope permissions",
)

// ValidScopes lists all approved discrete action scopes and wildcards.
var ValidScopes = []string{
	"*",
	"tokens:*", "tokens:read", "tokens:write",
	"users:*", "users:read", "users:write",
	"account:*", "account:read", "account:write",
	"system:*", "system:read", "system:admin",
}

// IsValidScope reports whether a scope string conforms to the approved
// vocabulary.
func IsValidScope(scope string) bool {
	switch scope {
	case "*",
		"tokens:*", "tokens:read", "tokens:write",
		"users:*", "users:read", "users:write",
		"account:*", "account:read", "account:write",
		"system:*", "system:read", "system:admin":
		return true
	default:
		return false
	}
}

// AllActions enumerates every discrete action defined in the system.
var AllActions = []Action{
	ActionTokensRead,
	ActionTokensWrite,
	ActionUsersRead,
	ActionUsersWrite,
	ActionAccountRead,
	ActionAccountWrite,
	ActionSystemRead,
	ActionSystemAdmin,
}

// RolePermitsRole reports whether the granter role contains all permissions
// of the grantee role without assuming a linear role hierarchy.
func RolePermitsRole(granter, grantee Role) bool {
	if !granter.IsValid() || !grantee.IsValid() {
		return false
	}
	for _, action := range AllActions {
		if RoleHasPermission(grantee, action) &&
			!RoleHasPermission(granter, action) {
			return false
		}
	}
	return true
}

// RoleRank returns the relative privilege tier of a role for ceiling
// comparisons (Admin > Maintainer/Operator > Viewer).
func RoleRank(r Role) int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleMaintainer, RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// Principal represents the authenticated actor making a request across any
// transport.
type Principal struct {
	ID             string
	Username       string
	Role           Role
	TokenRole      Role
	CredentialKind CredentialKind
	Scopes         []string
}

// Can reports whether the principal is authorized to perform the action.
func (p *Principal) Can(action Action) bool {
	return Authorize(p, action) == nil
}

// HasScope reports whether the principal possesses a specific token scope.
func (p *Principal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.Scopes {
		if s == "*" || s == scope {
			return true
		}
	}
	return false
}

// RoleHasPermission reports whether a role inherently permits an action.
func RoleHasPermission(role Role, action Action) bool {
	switch role {
	case RoleAdmin:
		return true

	case RoleMaintainer, RoleOperator:
		switch action {
		case ActionTokensRead, ActionTokensWrite,
			ActionAccountRead, ActionAccountWrite,
			ActionSystemRead:
			return true
		default:
			return false
		}

	case RoleViewer:
		switch action {
		case ActionAccountRead, ActionAccountWrite, ActionSystemRead:
			return true
		default:
			return false
		}

	default:
		return false
	}
}

// TokenHasScope reports whether a set of token scopes permits an action.
func TokenHasScope(scopes []string, action Action) bool {
	target := string(action)
	parts := strings.SplitN(target, ":", 2)
	prefix := ""
	if len(parts) == 2 {
		prefix = parts[0] + ":*"
	}

	for _, s := range scopes {
		trimmed := strings.TrimSpace(s)
		if trimmed == "*" || trimmed == target ||
			(prefix != "" && trimmed == prefix) {
			return true
		}
	}
	return false
}

// Authorize applies the centralized authorization policy.
// Effective Permissions = Owner Role Perms ∩ PAT Role Perms ∩ Scopes.
func Authorize(p *Principal, action Action) error {
	if p == nil {
		return ErrUnauthorized
	}

	// 1. Owner's role defines the maximum permissions.
	if !RoleHasPermission(p.Role, action) {
		return ErrForbidden
	}

	// 2. Interactive sessions and trusted local Unix socket inherit full
	// role permissions.
	if p.CredentialKind == CredentialSession ||
		p.CredentialKind == CredentialUnixSocket {
		return nil
	}

	// 3. Tokens must also satisfy the PAT's configured role restriction
	// and explicit scope bounds.
	if p.CredentialKind == CredentialToken {
		if !p.TokenRole.IsValid() ||
			!RoleHasPermission(p.TokenRole, action) {
			return ErrForbidden
		}
		if !TokenHasScope(p.Scopes, action) {
			return ErrForbidden
		}
		return nil
	}

	return ErrForbidden
}
