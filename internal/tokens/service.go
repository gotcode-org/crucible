// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package tokens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/id"
)

// ValidateScopes validates and deduplicates requested token scopes.
// An empty slice is rejected with ErrScopesRequired.
func ValidateScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, ErrScopesRequired
	}
	seen := make(map[string]struct{}, len(scopes))
	var result []string
	for _, s := range scopes {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if !auth.IsValidScope(trimmed) {
			return nil, fmt.Errorf(
				"%w: %q",
				ErrInvalidScope,
				trimmed,
			)
		}
		if _, ok := seen[trimmed]; !ok {
			seen[trimmed] = struct{}{}
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil, ErrScopesRequired
	}
	return result, nil
}

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
	if s.userLookup == nil {
		return nil, errors.New("user lookup service unconfigured")
	}
	user, err := s.userLookup.GetUser(ctx, username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidToken
	}
	if !user.IsActive {
		return nil, ErrOwnerInactive
	}

	scopes, err := ValidateScopes(cmd.Scopes)
	if err != nil {
		return nil, err
	}

	// Owner role ceiling: token role cannot elevate above the owner's
	// current effective role. If an explicit sub-role is requested,
	// ensure it is within the owner's permitted action set.
	if cmd.Role != "" && !cmd.Role.IsValid() {
		return nil, ErrInvalidToken
	}
	role := user.EffectiveRole()
	if cmd.Role.IsValid() && auth.RolePermitsRole(role, cmd.Role) {
		role = cmd.Role
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

// checkTokenValidity inspects token status flags, expiration, review
// requirements, and scope validity.
func checkTokenValidity(tok *APIToken, now time.Time) error {
	if tok.Revoked {
		return ErrTokenRevoked
	}
	if tok.ExpiresAt != nil && !tok.ExpiresAt.IsZero() &&
		now.After(*tok.ExpiresAt) {
		return ErrTokenExpired
	}
	if tok.RequiresReview {
		return ErrTokenRequiresReview
	}
	if len(tok.Scopes) == 0 {
		return ErrScopesRequired
	}
	for _, sc := range tok.Scopes {
		if !auth.IsValidScope(sc) {
			return ErrInvalidScope
		}
	}
	return nil
}

// ValidateToken checks a raw token string against the store, verifies its
// active status and owner account, touches last-used telemetry, and binds
// effective role permissions to the owner's current role.
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
	if vErr := checkTokenValidity(tok, now); vErr != nil {
		return nil, vErr
	}

	if s.userLookup == nil {
		return nil, errors.New("user lookup service unconfigured")
	}
	user, err := s.userLookup.GetUser(ctx, tok.Username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidToken
	}
	if !user.IsActive {
		return nil, ErrOwnerInactive
	}

	// Dynamic owner role derivation: token permissions can never exceed
	// the owner's current effective role, and owner promotions/demotions
	// take effect immediately. Preserve the configured token role as its
	// restriction while attaching the owner's current effective role.
	tok.OwnerRole = user.EffectiveRole()

	// Update telemetry
	if tErr := s.store.TouchToken(
		ctx,
		tok.ID,
		clientIP,
		now,
	); tErr != nil {
		return nil, tErr
	}
	tok.LastUsedAt = &now
	if clientIP != "" {
		tok.LastUsedIP = &clientIP
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

// ReviewToken confirms a review-required token after owner/admin review.
func (s *TokenService) ReviewToken(
	ctx context.Context,
	cmd ReviewTokenCommand,
) error {
	idStr := strings.TrimSpace(cmd.ID)
	reviewer := strings.TrimSpace(cmd.Reviewer)
	if idStr == "" {
		return ErrTokenNotFound
	}
	if reviewer == "" {
		return auth.ErrForbidden
	}

	tok, err := s.store.GetTokenByID(ctx, idStr)
	if err != nil {
		return err
	}
	if tok == nil {
		return ErrTokenNotFound
	}
	if tok.Revoked {
		return ErrTokenRevoked
	}
	if !tok.RequiresReview {
		return ErrTokenReviewNotRequired
	}

	// Only the token's owner or an administrator can review/reconfirm it.
	if !cmd.IsAdmin && tok.Username != reviewer {
		return auth.ErrForbidden
	}

	return s.store.ReviewToken(ctx, idStr, reviewer, time.Now().UTC())
}
