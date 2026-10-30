// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package tokens_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/internal/tokens"
)

func setupTestDB(t *testing.T) *storage.SQLiteAdapter {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.NewSQLiteAdapterPath(
		context.Background(),
		dbPath,
		true,
		5000,
	)
	if err != nil {
		t.Fatalf("opening sqlite adapter: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("running sqlite migrations: %v", err)
	}

	users := storage.NewSQLiteAuthStore(db)
	for _, user := range []auth.User{
		{Username: "operator", Role: auth.RoleOperator, IsActive: true},
		{Username: "alice", Role: auth.RoleAdmin, IsActive: true},
	} {
		if err := users.CreateUser(context.Background(), &user); err != nil {
			t.Fatalf("seeding user %s: %v", user.Username, err)
		}
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
	svc := tokens.NewTokenService(store, storage.NewSQLiteAuthStore(db))
	ctx := context.Background()

	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username:  "operator",
		Name:      "laptop-cli",
		Role:      auth.RoleOperator,
		Scopes:    []string{"tokens:read", "tokens:write"},
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

func TestTokenService_CreateTokenRequiresExistingUser(t *testing.T) {
	t.Parallel()

	engine := setupTestDB(t)
	defer func() {
		if err := engine.Close(); err != nil {
			t.Errorf("closing test adapter: %v", err)
		}
	}()

	userStore := storage.NewSQLiteAuthStore(engine)
	tokenStore := storage.NewSQLiteTokenStore(engine)
	service := tokens.NewTokenService(tokenStore, userStore)
	_, err := service.CreateToken(
		context.Background(),
		tokens.CreateTokenCommand{
			Username: "missing-user",
			Name:     "must-not-be-created",
			Scopes:   []string{"*"},
		},
	)
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("expected auth.ErrUserNotFound, got %v", err)
	}

	if _, userErr := userStore.GetUser(
		context.Background(),
		"missing-user",
	); !errors.Is(userErr, auth.ErrUserNotFound) {
		t.Fatalf("missing user was created: %v", userErr)
	}
	tokensForUser, err := tokenStore.ListUserTokens(
		context.Background(),
		"missing-user",
	)
	if err != nil {
		t.Fatalf("listing tokens for missing user: %v", err)
	}
	if len(tokensForUser) != 0 {
		t.Fatalf("expected no persisted tokens, got %d", len(tokensForUser))
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
	svc := tokens.NewTokenService(store, storage.NewSQLiteAuthStore(db))
	ctx := context.Background()

	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "operator",
		Name:     "revoke-target",
		Role:     auth.RoleOperator,
		Scopes:   []string{"tokens:read"},
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
	svc := tokens.NewTokenService(store, storage.NewSQLiteAuthStore(db))
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
	svc := tokens.NewTokenService(store, storage.NewSQLiteAuthStore(db))
	ctx := context.Background()

	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "alice",
		Name:     "concurrent-key",
		Role:     auth.RoleAdmin,
		Scopes:   []string{"*"},
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

func TestValidateScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		scopes    []string
		want      []string
		wantErrIs error
	}{
		{
			name:      "empty slice",
			scopes:    []string{},
			wantErrIs: tokens.ErrScopesRequired,
		},
		{
			name:      "nil slice",
			scopes:    nil,
			wantErrIs: tokens.ErrScopesRequired,
		},
		{
			name:      "whitespace only elements",
			scopes:    []string{"", "   ", "\t"},
			wantErrIs: tokens.ErrScopesRequired,
		},
		{
			name:   "wildcard",
			scopes: []string{"*"},
			want:   []string{"*"},
		},
		{
			name:   "valid discrete scopes",
			scopes: []string{"tokens:read", "tokens:write", "users:read"},
			want:   []string{"tokens:read", "tokens:write", "users:read"},
		},
		{
			name:   "deduplication",
			scopes: []string{"tokens:read", "tokens:read", "tokens:write"},
			want:   []string{"tokens:read", "tokens:write"},
		},
		{
			name:   "whitespace trimming",
			scopes: []string{"  tokens:read  ", " system:read "},
			want:   []string{"tokens:read", "system:read"},
		},
		{
			name:      "invalid scope syntax",
			scopes:    []string{"not_a_valid_scope"},
			wantErrIs: tokens.ErrInvalidScope,
		},
		{
			name:      "invalid mixed with valid",
			scopes:    []string{"tokens:read", "invalid:scope"},
			wantErrIs: tokens.ErrInvalidScope,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokens.ValidateScopes(tc.scopes)
			if tc.wantErrIs != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf(
						"expected error is %v, got: %v",
						tc.wantErrIs,
						err,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf(
					"expected len %d, got %d (%v)",
					len(tc.want),
					len(got),
					got,
				)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf(
						"idx %d: want %s, got %s",
						i,
						tc.want[i],
						got[i],
					)
				}
			}
		})
	}
}

func TestTokenService_ScopesRequiredAndValidated(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store, storage.NewSQLiteAuthStore(db))
	ctx := context.Background()

	// 1. Empty scopes rejected
	_, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "operator",
		Name:     "empty-scopes-pat",
		Role:     auth.RoleOperator,
		Scopes:   []string{},
	})
	if !errors.Is(err, tokens.ErrScopesRequired) {
		t.Fatalf("expected ErrScopesRequired, got: %v", err)
	}

	// 2. Unrecognized scope rejected
	_, err = svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "operator",
		Name:     "invalid-scope-pat",
		Role:     auth.RoleOperator,
		Scopes:   []string{"arbitrary:scope"},
	})
	if !errors.Is(err, tokens.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got: %v", err)
	}
}

func TestTokenService_InactiveAndDeletedOwner(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	authStore := storage.NewSQLiteAuthStore(db)
	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store, authStore)
	ctx := context.Background()

	// Seed inactive user
	inactiveUser := auth.User{
		Username: "disabled-user",
		Role:     auth.RoleOperator,
		IsActive: false,
	}
	if err := authStore.CreateUser(ctx, &inactiveUser); err != nil {
		t.Fatalf("creating inactive user: %v", err)
	}

	// 1. Create token for inactive user fails
	_, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "disabled-user",
		Name:     "disabled-token",
		Scopes:   []string{"*"},
	})
	if !errors.Is(err, tokens.ErrOwnerInactive) {
		t.Fatalf("expected ErrOwnerInactive on create, got: %v", err)
	}

	// 2. Active user creates token, then user is disabled
	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "operator",
		Name:     "operator-pat",
		Scopes:   []string{"tokens:read"},
	})
	if err != nil {
		t.Fatalf("creating token: %v", err)
	}

	// Validate succeeds initially
	if _, vErr := svc.ValidateToken(
		ctx,
		res.RawToken,
		"10.0.0.1",
	); vErr != nil {
		t.Fatalf("unexpected validation error: %v", vErr)
	}

	// Deactivate user
	opUser, gErr := authStore.GetUser(ctx, "operator")
	if gErr != nil {
		t.Fatalf("getting operator user: %v", gErr)
	}
	opUser.IsActive = false
	if uErr := authStore.UpdateUser(ctx, opUser); uErr != nil {
		t.Fatalf("deactivating operator: %v", uErr)
	}

	// Validate fails for deactivated user
	_, vErr := svc.ValidateToken(ctx, res.RawToken, "10.0.0.1")
	if !errors.Is(vErr, tokens.ErrOwnerInactive) {
		t.Fatalf("expected ErrOwnerInactive on validate, got: %v", vErr)
	}

	// 3. User is deleted: validate fails closed
	resAlice, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "alice",
		Name:     "alice-pat",
		Scopes:   []string{"*"},
	})
	if err != nil {
		t.Fatalf("creating alice token: %v", err)
	}
	if dErr := authStore.DeleteUser(ctx, "alice"); dErr != nil {
		t.Fatalf("deleting alice: %v", dErr)
	}

	_, vErr = svc.ValidateToken(ctx, resAlice.RawToken, "10.0.0.1")
	if vErr == nil {
		t.Fatal("expected error validating token for deleted user, got nil")
	}
}

func TestTokenService_OwnerRoleCeilingAndDynamicUpdates(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	authStore := storage.NewSQLiteAuthStore(db)
	store := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(store, authStore)
	ctx := context.Background()

	// 1. Cannot elevate operator even if token created with admin role
	rawOpTok := testTokenCannotElevateOperator(ctx, t, svc)

	// 2. Owner demotion immediately reduces effective role permissions
	testTokenOwnerDemotion(ctx, t, svc, authStore)

	// 3. Owner promotion reflects higher role immediately
	testTokenOwnerPromotion(ctx, t, svc, authStore, rawOpTok)
}

func testTokenCannotElevateOperator(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
) string {
	t.Helper()
	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "operator",
		Name:     "elevated-attempt",
		Role:     auth.RoleAdmin,
		Scopes:   []string{"*"},
	})
	if err != nil {
		t.Fatalf("creating token: %v", err)
	}
	if res.Token.Role != auth.RoleOperator {
		t.Fatalf(
			"expected role capped to operator, got %s",
			res.Token.Role,
		)
	}

	tok, vErr := svc.ValidateToken(ctx, res.RawToken, "127.0.0.1")
	if vErr != nil {
		t.Fatalf("validating token: %v", vErr)
	}
	if tok.Role != auth.RoleOperator {
		t.Fatalf("expected validated role operator, got %s", tok.Role)
	}
	return res.RawToken
}

func testTokenOwnerDemotion(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
	authStore *storage.SQLiteAuthStore,
) {
	t.Helper()
	aliceRes, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "alice",
		Name:     "alice-pat",
		Role:     auth.RoleAdmin,
		Scopes:   []string{"*"},
	})
	if err != nil {
		t.Fatalf("creating alice token: %v", err)
	}
	aliceTok, vErr := svc.ValidateToken(ctx, aliceRes.RawToken, "127.0.0.1")
	if vErr != nil {
		t.Fatalf("validating alice token: %v", vErr)
	}
	if aliceTok.Role != auth.RoleAdmin {
		t.Fatalf("expected role admin, got %s", aliceTok.Role)
	}

	aliceUser, gErr := authStore.GetUser(ctx, "alice")
	if gErr != nil {
		t.Fatalf("fetching alice: %v", gErr)
	}
	aliceUser.Role = auth.RoleViewer
	if uErr := authStore.UpdateUser(ctx, aliceUser); uErr != nil {
		t.Fatalf("demoting alice: %v", uErr)
	}

	demotedTok, dErr := svc.ValidateToken(ctx, aliceRes.RawToken, "127.0.0.1")
	if dErr != nil {
		t.Fatalf("validating demoted alice token: %v", dErr)
	}
	if demotedTok.Role != auth.RoleViewer {
		t.Fatalf("expected role viewer after demote, got %s", demotedTok.Role)
	}
}

func testTokenOwnerPromotion(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
	authStore *storage.SQLiteAuthStore,
	rawToken string,
) {
	t.Helper()
	opUser, gErr := authStore.GetUser(ctx, "operator")
	if gErr != nil {
		t.Fatalf("fetching operator: %v", gErr)
	}
	opUser.Role = auth.RoleAdmin
	if uErr := authStore.UpdateUser(ctx, opUser); uErr != nil {
		t.Fatalf("promoting operator: %v", uErr)
	}

	promotedTok, pErr := svc.ValidateToken(ctx, rawToken, "127.0.0.1")
	if pErr != nil {
		t.Fatalf("validating promoted operator token: %v", pErr)
	}
	if promotedTok.Role != auth.RoleAdmin {
		t.Fatalf(
			"expected role admin after promote, got %s",
			promotedTok.Role,
		)
	}
}
