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

	// ErrScopesRequired indicates at least one scope must be supplied.
	ErrScopesRequired = errors.New("at least one token scope is required")

	// ErrOwnerInactive indicates the token owner account is inactive.
	ErrOwnerInactive = errors.New("token owner account is inactive or disabled")

	// ErrRoleExceedsCeiling indicates requested token role exceeds owner
	// role permissions.
	ErrRoleExceedsCeiling = errors.New(
		"requested token role exceeds owner role permissions",
	)

	// ErrTokenRequiresReview indicates the token has a legacy wildcard
	// scope and must be explicitly reviewed and reconfirmed by its owner
	// or an administrator.
	ErrTokenRequiresReview = errors.New(
		"personal access token requires review before use",
	)
)

// APIToken represents a persistent Personal Access Token (PAT) record in the
// database. The plaintext secret is never stored; only its SHA-256 hash is
// persisted.
type APIToken struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	TokenPrefix    string     `json:"token_prefix"`
	TokenHash      string     `json:"-"`
	Username       string     `json:"username"`
	Role           auth.Role  `json:"role"`
	OwnerRole      auth.Role  `json:"owner_role,omitempty"`
	Scopes         []string   `json:"scopes"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP     *string    `json:"last_used_ip,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Revoked        bool       `json:"revoked"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	RequiresReview bool       `json:"requires_review"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy     *string    `json:"reviewed_by,omitempty"`
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

// Status returns a human-readable lifecycle state ("active", "expired",
// "revoked", or "review_required").
func (t APIToken) Status() string {
	if t.Revoked {
		return "revoked"
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.IsZero() &&
		time.Now().UTC().After(*t.ExpiresAt) {
		return "expired"
	}
	if t.RequiresReview {
		return "review_required"
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

// ReviewTokenCommand contains parameters to confirm a review-required token.
type ReviewTokenCommand struct {
	ID       string
	Reviewer string
	IsAdmin  bool
}
