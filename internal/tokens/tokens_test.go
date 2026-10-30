// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package tokens_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/internal/tokens"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	dsn := dbPath + "?_pragma=busy_timeout(5000)&" +
		"_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}

	for _, f := range []string{
		"001_initial.sql",
		"004_sqlite_users.sql",
		"005_password_lifecycle.sql",
	} {
		p := filepath.Join("..", "storage", "schema", f)
		sqlBytes, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading schema file %s: %v", p, err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			t.Fatalf("applying schema %s: %v", f, err)
		}
	}

	// Insert test users to satisfy foreign key constraints
	ensureUser := `
INSERT INTO users (username, role, is_active, created_at)
VALUES ('operator', 'operator', 1, CURRENT_TIMESTAMP),
       ('alice', 'admin', 1, CURRENT_TIMESTAMP)`
	if _, err := db.Exec(ensureUser); err != nil {
		t.Fatalf("seeding test users: %v", err)
	}

	return db
}

func TestTokenFormatAndHashing(t *testing.T) {
	t.Parallel()

	raw, prefix, hash, err := tokens.GenerateRawToken()
	if err != nil {
		t.Fatalf("generating raw token: %v", err)
	}

	if !strings.HasPrefix(raw, tokens.TokenPrefix) {
		t.Fatalf(
			"expected prefix %s in raw token: %s",
			tokens.TokenPrefix,
			raw,
		)
	}
	if !strings.HasPrefix(prefix, tokens.TokenPrefix) {
		t.Fatalf(
			"expected prefix %s in prefix: %s",
			tokens.TokenPrefix,
			prefix,
		)
	}
	if !strings.HasSuffix(prefix, "...") {
		t.Fatalf("expected trailing ellipsis in prefix: %s", prefix)
	}

	if !tokens.ValidateTokenFormat(raw) {
		t.Fatalf("expected raw token format to be valid: %s", raw)
	}
	if tokens.ValidateTokenFormat("invalid_token") {
		t.Fatal("expected invalid token to fail format validation")
	}

	computedHash := tokens.HashToken(raw)
	if computedHash != hash {
		t.Fatalf(
			"hash mismatch: expected %s, got %s",
			hash,
			computedHash,
		)
	}
}

func TestTokenService_CreateAndValidate(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store)
	ctx := context.Background()

	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username:  "operator",
		Name:      "laptop-cli",
		Role:      auth.RoleOperator,
		Scopes:    []string{"read", "write"},
		ExpiresIn: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("creating token: %v", err)
	}
	if res.Token == nil || res.RawToken == "" {
		t.Fatal("expected non-nil token and raw token string")
	}

	validated, vErr := svc.ValidateToken(ctx, res.RawToken, "192.168.1.50")
	if vErr != nil {
		t.Fatalf("validating token: %v", vErr)
	}
	if validated.ID != res.Token.ID {
		t.Fatalf(
			"expected token ID %s, got %s",
			res.Token.ID,
			validated.ID,
		)
	}

	list, lErr := svc.ListTokens(ctx, "operator")
	if lErr != nil {
		t.Fatalf("listing tokens: %v", lErr)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 token, got %d", len(list))
	}
	if list[0].LastUsedAt == nil {
		t.Fatal("expected LastUsedAt to be populated after validation")
	}
	if list[0].LastUsedIP == nil || *list[0].LastUsedIP != "192.168.1.50" {
		t.Fatalf(
			"expected LastUsedIP 192.168.1.50, got %v",
			list[0].LastUsedIP,
		)
	}
}

func TestTokenService_Revocation(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store)
	ctx := context.Background()

	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "operator",
		Name:     "revoke-target",
		Role:     auth.RoleOperator,
	})
	if err != nil {
		t.Fatalf("creating token: %v", err)
	}

	rErr := svc.RevokeToken(ctx, tokens.RevokeTokenCommand{
		ID:       res.Token.ID,
		Username: "operator",
	})
	if rErr != nil {
		t.Fatalf("revoking token: %v", rErr)
	}

	_, vErr := svc.ValidateToken(ctx, res.RawToken, "192.168.1.50")
	if vErr == nil {
		t.Fatal("expected error validating revoked token, got nil")
	}
	if vErr != tokens.ErrTokenRevoked {
		t.Fatalf("expected ErrTokenRevoked, got %v", vErr)
	}
}

func TestTokenService_Expiration(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store)
	ctx := context.Background()

	// Create already-expired token directly via store
	now := time.Now().UTC().Add(-1 * time.Hour)
	fakeRaw := "cb_pat_" + strings.Repeat("a", 64)
	tok := &tokens.APIToken{
		ID:          "tok_expired_test",
		Name:        "expired-token",
		TokenPrefix: "cb_pat_11223344...",
		TokenHash:   tokens.HashToken(fakeRaw),
		Username:    "operator",
		Role:        auth.RoleOperator,
		Scopes:      []string{"*"},
		CreatedAt:   now.Add(-2 * time.Hour),
		ExpiresAt:   &now,
		Revoked:     false,
	}
	if err := store.CreateToken(ctx, tok); err != nil {
		t.Fatalf("persisting test expired token: %v", err)
	}

	raw := "cb_pat_" + strings.Repeat("a", 64)
	_, vErr := svc.ValidateToken(ctx, raw, "127.0.0.1")
	if vErr == nil {
		t.Fatal("expected error validating expired token")
	}
	if vErr != tokens.ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", vErr)
	}
}

func TestTokenService_Concurrency(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store)
	ctx := context.Background()

	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "alice",
		Name:     "concurrent-key",
		Role:     auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("creating token: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, vErr := svc.ValidateToken(
				ctx,
				res.RawToken,
				"10.0.0.1",
			)
			if vErr != nil {
				errCh <- vErr
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for e := range errCh {
		t.Errorf("concurrent validation error: %v", e)
	}
}
