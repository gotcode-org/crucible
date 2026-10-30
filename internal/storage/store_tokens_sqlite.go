// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gotcode.org/crucible/internal/auth"
	tokendomain "gotcode.org/crucible/internal/tokens"
)

// SQLiteTokenStore implements the token persistence port on top of SQLite.
type SQLiteTokenStore struct {
	db *sql.DB
}

// NewSQLiteTokenStore creates a token persistence adapter backed by SQLite.
func NewSQLiteTokenStore(db *sql.DB) *SQLiteTokenStore {
	return &SQLiteTokenStore{db: db}
}

// CreateToken persists a new token record.
func (s *SQLiteTokenStore) CreateToken(ctx context.Context, tok *tokendomain.APIToken) error {
	scopesJSON, err := json.Marshal(tok.Scopes)
	if err != nil {
		return fmt.Errorf("marshaling token scopes: %w", err)
	}

	createdAtStr := tok.CreatedAt.UTC().Format(time.RFC3339)
	var expiresAtStr *string
	if tok.ExpiresAt != nil && !tok.ExpiresAt.IsZero() {
		exp := tok.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAtStr = &exp
	}

	// Satisfy foreign key constraint on users table
	nowStr := time.Now().UTC().Format(time.RFC3339)
	ensureUser := `
INSERT INTO users (username, created_at)
VALUES (?, ?)
ON CONFLICT(username) DO NOTHING`
	if _, uErr := s.db.ExecContext(
		ctx,
		ensureUser,
		tok.Username,
		nowStr,
	); uErr != nil {
		return fmt.Errorf(
			"ensuring user before token creation: %w",
			uErr,
		)
	}

	query := `
INSERT INTO api_tokens (
	id, name, token_prefix, token_hash, username, role, scopes_json,
	created_at, expires_at, revoked
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`

	_, err = s.db.ExecContext(
		ctx,
		query,
		tok.ID,
		tok.Name,
		tok.TokenPrefix,
		tok.TokenHash,
		tok.Username,
		string(tok.Role),
		string(scopesJSON),
		createdAtStr,
		expiresAtStr,
	)
	if err != nil {
		return fmt.Errorf("inserting api token %s: %w", tok.ID, err)
	}
	return nil
}

// GetTokenByHash retrieves an active or revoked token by its SHA-256 hash.
func (s *SQLiteTokenStore) GetTokenByHash(
	ctx context.Context,
	hash string,
) (*tokendomain.APIToken, error) {
	query := `
SELECT id, name, token_prefix, token_hash, username, role, scopes_json,
       created_at, last_used_at, last_used_ip, expires_at, revoked, revoked_at
FROM api_tokens
WHERE token_hash = ?`

	row := s.db.QueryRowContext(ctx, query, hash)
	return s.scanToken(row)
}

// GetTokenByID retrieves a token by its ULID identifier.
func (s *SQLiteTokenStore) GetTokenByID(
	ctx context.Context,
	id string,
) (*tokendomain.APIToken, error) {
	query := `
SELECT id, name, token_prefix, token_hash, username, role, scopes_json,
       created_at, last_used_at, last_used_ip, expires_at, revoked, revoked_at
FROM api_tokens
WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanToken(row)
}

// ListUserTokens retrieves all tokens belonging to an operator, or all tokens
// if username is empty.
func (s *SQLiteTokenStore) ListUserTokens(
	ctx context.Context,
	username string,
) ([]tokendomain.APIToken, error) {
	var rows *sql.Rows
	var err error

	if username != "" {
		query := `
SELECT id, name, token_prefix, token_hash, username, role, scopes_json,
       created_at, last_used_at, last_used_ip, expires_at, revoked, revoked_at
FROM api_tokens
WHERE username = ?
ORDER BY created_at DESC`
		rows, err = s.db.QueryContext(ctx, query, username)
	} else {
		query := `
SELECT id, name, token_prefix, token_hash, username, role, scopes_json,
       created_at, last_used_at, last_used_ip, expires_at, revoked, revoked_at
FROM api_tokens
ORDER BY created_at DESC`
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf(
			"listing tokens for %s: %w",
			username,
			err,
		)
	}
	defer func() {
		if cErr := rows.Close(); cErr != nil {
			_ = cErr
		}
	}()

	var tokens []tokendomain.APIToken
	for rows.Next() {
		tok, err := s.scanTokenRow(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, *tok)
	}
	return tokens, rows.Err()
}

// TouchToken updates the last-used timestamp and IP for an active token.
func (s *SQLiteTokenStore) TouchToken(
	ctx context.Context,
	id string,
	ip string,
	at time.Time,
) error {
	query := `
UPDATE api_tokens
SET last_used_at = ?, last_used_ip = ?
WHERE id = ? AND revoked = 0`

	atStr := at.UTC().Format(time.RFC3339)
	var ipPtr *string
	if ip != "" {
		ipPtr = &ip
	}

	_, err := s.db.ExecContext(ctx, query, atStr, ipPtr, id)
	if err != nil {
		return fmt.Errorf("touching token %s: %w", id, err)
	}
	return nil
}

// RevokeToken marks a token as revoked. If username is non-empty, the token
// must belong to that username.
func (s *SQLiteTokenStore) RevokeToken(
	ctx context.Context,
	id string,
	username string,
	at time.Time,
) error {
	atStr := at.UTC().Format(time.RFC3339)
	var res sql.Result
	var err error

	if username != "" {
		query := `
UPDATE api_tokens
SET revoked = 1, revoked_at = ?
WHERE id = ? AND username = ? AND revoked = 0`
		res, err = s.db.ExecContext(ctx, query, atStr, id, username)
	} else {
		query := `
UPDATE api_tokens
SET revoked = 1, revoked_at = ?
WHERE id = ? AND revoked = 0`
		res, err = s.db.ExecContext(ctx, query, atStr, id)
	}
	if err != nil {
		return fmt.Errorf("revoking token %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking revoke rows affected: %w", err)
	}
	if rows == 0 {
		return tokendomain.ErrTokenNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *SQLiteTokenStore) scanToken(r rowScanner) (*tokendomain.APIToken, error) {
	tok, err := s.scanTokenRow(r)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tokendomain.ErrTokenNotFound
	}
	return tok, err
}

func (s *SQLiteTokenStore) scanTokenRow(r rowScanner) (*tokendomain.APIToken, error) {
	var (
		tok          tokendomain.APIToken
		roleStr      string
		scopesRaw    string
		createdStr   string
		lastUsedStr  sql.NullString
		lastUsedIP   sql.NullString
		expiresStr   sql.NullString
		revokedInt   int
		revokedAtStr sql.NullString
	)

	err := r.Scan(
		&tok.ID,
		&tok.Name,
		&tok.TokenPrefix,
		&tok.TokenHash,
		&tok.Username,
		&roleStr,
		&scopesRaw,
		&createdStr,
		&lastUsedStr,
		&lastUsedIP,
		&expiresStr,
		&revokedInt,
		&revokedAtStr,
	)
	if err != nil {
		return nil, err
	}

	tok.Role = auth.Role(roleStr)
	tok.Revoked = revokedInt == 1

	if jErr := json.Unmarshal(
		[]byte(scopesRaw),
		&tok.Scopes,
	); jErr != nil {
		tok.Scopes = []string{"*"}
	}

	if t, pErr := time.Parse(time.RFC3339, createdStr); pErr == nil {
		tok.CreatedAt = t
	}

	if lastUsedStr.Valid {
		t, pErr := time.Parse(time.RFC3339, lastUsedStr.String)
		if pErr == nil {
			tok.LastUsedAt = &t
		}
	}
	if lastUsedIP.Valid {
		ip := lastUsedIP.String
		tok.LastUsedIP = &ip
	}
	if expiresStr.Valid {
		t, pErr := time.Parse(time.RFC3339, expiresStr.String)
		if pErr == nil {
			tok.ExpiresAt = &t
		}
	}
	if revokedAtStr.Valid {
		t, pErr := time.Parse(time.RFC3339, revokedAtStr.String)
		if pErr == nil {
			tok.RevokedAt = &t
		}
	}

	return &tok, nil
}
