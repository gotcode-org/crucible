// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package tokens

import (
	"context"
	"time"

	"gotcode.org/crucible/internal/auth"
)

// UserLookup defines the minimum user query required to create a token.
type UserLookup interface {
	GetUser(ctx context.Context, username string) (*auth.User, error)
}

// TokenStore defines the outbound SPI persistence port for Personal Access
// Tokens. Implementations interact with the underlying database without
// leaking SQL or driver specifics to callers.
type TokenStore interface {
	// CreateToken persists a new token record.
	CreateToken(ctx context.Context, tok *APIToken) error

	// GetTokenByHash retrieves a token by its SHA-256 hash.
	GetTokenByHash(ctx context.Context, hash string) (*APIToken, error)

	// GetTokenByID retrieves a token by its ULID identifier.
	GetTokenByID(ctx context.Context, id string) (*APIToken, error)

	// ListUserTokens retrieves all active and revoked tokens for a user.
	ListUserTokens(
		ctx context.Context,
		username string,
	) ([]APIToken, error)

	// TouchToken records the last-used timestamp and client IP address.
	TouchToken(
		ctx context.Context,
		id string,
		ip string,
		at time.Time,
	) error

	// RevokeToken marks a token as revoked.
	RevokeToken(
		ctx context.Context,
		id string,
		username string,
		at time.Time,
	) error
}

// Service defines the inbound CQRS application service port for Personal
// Access Tokens.
type Service interface {
	// CreateToken generates and persists a new Personal Access Token.
	CreateToken(
		ctx context.Context,
		cmd CreateTokenCommand,
	) (*CreateTokenResult, error)

	// ValidateToken inspects a raw token string (e.g. from Bearer header)
	// against the store, verifying active status and updating telemetry.
	ValidateToken(
		ctx context.Context,
		rawToken string,
		clientIP string,
	) (*APIToken, error)

	// ListTokens returns all tokens owned by the specified operator.
	ListTokens(ctx context.Context, username string) ([]APIToken, error)

	// RevokeToken marks an operator's token as revoked.
	RevokeToken(ctx context.Context, cmd RevokeTokenCommand) error
}
