// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package tokens implements the Personal Access Tokens (PATs) subsystem,
// providing high-entropy cryptographic token generation, SHA-256 storage
// hashing, and scope validation.
package tokens

import (
	"errors"
	"time"

	"gotcode.org/crucible/internal/auth"
)

var (
	// ErrTokenNotFound indicates the token identifier or hash was not
	// found.
	ErrTokenNotFound = errors.New("personal access token not found")

	// ErrTokenRevoked indicates the token was previously revoked.
	ErrTokenRevoked = errors.New("personal access token has been revoked")

	// ErrTokenExpired indicates the token has passed its expiration time.
	ErrTokenExpired = errors.New("personal access token has expired")

	// ErrInvalidToken indicates the token format or checksum is malformed.
	ErrInvalidToken = errors.New("invalid personal access token")

	// ErrTokenNameRequired indicates a non-empty name must be supplied.
	ErrTokenNameRequired = errors.New("token name is required")

	// ErrInvalidScope indicates one or more requested scopes are
	// unrecognized.
	ErrInvalidScope = errors.New("unrecognized token scope")
)

// APIToken represents a persistent Personal Access Token (PAT) record in the
// database. The plaintext secret is never stored; only its SHA-256 hash is
// persisted.
type APIToken struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	TokenHash   string     `json:"-"`
	Username    string     `json:"username"`
	Role        auth.Role  `json:"role"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP  *string    `json:"last_used_ip,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Revoked     bool       `json:"revoked"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// IsActive reports whether the token is currently valid and unexpired.
func (t APIToken) IsActive(now time.Time) bool {
	if t.Revoked {
		return false
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.IsZero() &&
		now.After(*t.ExpiresAt) {
		return false
	}
	return true
}

// Status returns a human-readable lifecycle state ("active", "expired", or
// "revoked").
func (t APIToken) Status() string {
	if t.Revoked {
		return "revoked"
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.IsZero() &&
		time.Now().UTC().After(*t.ExpiresAt) {
		return "expired"
	}
	return "active"
}

// CreateTokenCommand contains parameters for generating a new PAT.
type CreateTokenCommand struct {
	Username  string
	Name      string
	Role      auth.Role
	Scopes    []string
	ExpiresIn time.Duration // 0 means no expiration
}

// CreateTokenResult wraps the stored token metadata along with the plaintext
// token that is revealed to the operator exactly once.
type CreateTokenResult struct {
	Token    *APIToken `json:"token"`
	RawToken string    `json:"raw_token"`
}

// RevokeTokenCommand contains parameters to revoke an existing token.
type RevokeTokenCommand struct {
	ID       string
	Username string
}
