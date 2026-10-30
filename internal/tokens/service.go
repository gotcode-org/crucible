// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package tokens

import (
	"context"
	"strings"
	"time"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/id"
)

// TokenService implements the inbound application service port for Personal
// Access Tokens.
type TokenService struct {
	store      TokenStore
	userLookup UserLookup
}

// NewTokenService constructs a token service with its persistence and user
// lookup ports.
func NewTokenService(store TokenStore, userLookup UserLookup) *TokenService {
	return &TokenService{store: store, userLookup: userLookup}
}

// CreateToken generates and persists a new Personal Access Token.
func (s *TokenService) CreateToken(
	ctx context.Context,
	cmd CreateTokenCommand,
) (*CreateTokenResult, error) {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return nil, ErrTokenNameRequired
	}
	username := strings.TrimSpace(cmd.Username)
	if username == "" {
		return nil, ErrInvalidToken
	}
	if _, err := s.userLookup.GetUser(ctx, username); err != nil {
		return nil, err
	}

	role := cmd.Role
	if !role.IsValid() {
		role = auth.RoleOperator
	}

	scopes := cmd.Scopes
	if len(scopes) == 0 {
		scopes = []string{"*"}
	}

	rawToken, prefix, hash, err := GenerateRawToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	if cmd.ExpiresIn > 0 {
		exp := now.Add(cmd.ExpiresIn)
		expiresAt = &exp
	}

	tok := &APIToken{
		ID:          id.NewTokenID(),
		Name:        name,
		TokenPrefix: prefix,
		TokenHash:   hash,
		Username:    username,
		Role:        role,
		Scopes:      scopes,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Revoked:     false,
	}

	if err := s.store.CreateToken(ctx, tok); err != nil {
		return nil, err
	}

	return &CreateTokenResult{
		Token:    tok,
		RawToken: rawToken,
	}, nil
}

// ValidateToken checks a raw token string against the store, verifies its
// active status, and touches last-used telemetry.
func (s *TokenService) ValidateToken(
	ctx context.Context,
	rawToken string,
	clientIP string,
) (*APIToken, error) {
	if !ValidateTokenFormat(rawToken) {
		return nil, ErrInvalidToken
	}

	hash := HashToken(rawToken)
	tok, err := s.store.GetTokenByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, ErrTokenNotFound
	}

	now := time.Now().UTC()
	if tok.Revoked {
		return nil, ErrTokenRevoked
	}
	if tok.ExpiresAt != nil && !tok.ExpiresAt.IsZero() &&
		now.After(*tok.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	// Update telemetry
	if tErr := s.store.TouchToken(
		ctx,
		tok.ID,
		clientIP,
		now,
	); tErr != nil {
		return nil, tErr
	}

	return tok, nil
}

// ListTokens returns all tokens registered for the given user.
func (s *TokenService) ListTokens(
	ctx context.Context,
	username string,
) ([]APIToken, error) {
	return s.store.ListUserTokens(ctx, strings.TrimSpace(username))
}

// RevokeToken marks an existing token as revoked.
func (s *TokenService) RevokeToken(
	ctx context.Context,
	cmd RevokeTokenCommand,
) error {
	idStr := strings.TrimSpace(cmd.ID)
	userStr := strings.TrimSpace(cmd.Username)
	if idStr == "" {
		return ErrTokenNotFound
	}
	return s.store.RevokeToken(ctx, idStr, userStr, time.Now().UTC())
}
