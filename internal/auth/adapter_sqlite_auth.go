// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"context"
	"errors"
	"strings"
)

// SQLiteAuthenticator implements the Authenticator port using native
// database user records with Argon2id password verification.
type SQLiteAuthenticator struct {
	store UserStore
}

// NewSQLiteAuthenticator creates an Authenticator backed by SQLite UserStore.
func NewSQLiteAuthenticator(store UserStore) *SQLiteAuthenticator {
	return &SQLiteAuthenticator{store: store}
}

// Provider returns "sqlite".
func (a *SQLiteAuthenticator) Provider() string {
	return "sqlite"
}

// ResolveRole resolves the operator role from persistent SQLite user records.
func (a *SQLiteAuthenticator) ResolveRole(
	ctx context.Context,
	username string,
) (Role, bool) {
	if a.store == nil {
		return "", false
	}
	u, err := a.store.GetUser(ctx, username)
	if err != nil {
		return "", false
	}
	return u.EffectiveRole(), true
}

// Authenticate verifies credentials against the SQLite users table.
func (a *SQLiteAuthenticator) Authenticate(
	ctx context.Context,
	username, password string,
) (*UserIdentity, error) {
	username = strings.TrimSpace(username)
	if username == "" || !validUsernameRe.MatchString(username) {
		return nil, ErrInvalidCredentials
	}

	u, err := a.store.GetUser(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !u.IsActive {
		return nil, ErrUserDisabled
	}

	if u.PasswordHash == nil || *u.PasswordHash == "" {
		return nil, ErrInvalidCredentials
	}

	match, vErr := VerifyPassword(password, *u.PasswordHash)
	if vErr != nil || !match {
		return nil, ErrInvalidCredentials
	}

	role := u.EffectiveRole()

	return &UserIdentity{
		Username: u.Username,
		Role:     role,
		Provider: a.Provider(),
	}, nil
}
