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

	// 3. Owner promotion does not widen token beyond configured role
	testTokenOwnerPromotion(ctx, t, svc, authStore, rawOpTok)

	// 4. Admin creates viewer-restricted token; retains viewer bounds
	testTokenRestrictedRolePreserved(ctx, t, svc)
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
	if aliceTok.OwnerRole != auth.RoleAdmin {
		t.Fatalf(
			"expected owner role admin, got %s",
			aliceTok.OwnerRole,
		)
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
	if demotedTok.Role != auth.RoleAdmin {
		t.Fatalf(
			"expected token role preserved as admin, got %s",
			demotedTok.Role,
		)
	}
	if demotedTok.OwnerRole != auth.RoleViewer {
		t.Fatalf(
			"expected owner role updated to viewer, got %s",
			demotedTok.OwnerRole,
		)
	}

	p := &auth.Principal{
		Username:       demotedTok.Username,
		Role:           demotedTok.OwnerRole,
		TokenRole:      demotedTok.Role,
		CredentialKind: auth.CredentialToken,
		Scopes:         demotedTok.Scopes,
	}
	if p.Can(auth.ActionTokensWrite) {
		t.Fatal("demoted owner token permitted ActionTokensWrite")
	}
	if !p.Can(auth.ActionSystemRead) {
		t.Fatal("demoted owner token denied ActionSystemRead")
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
	if promotedTok.Role != auth.RoleOperator {
		t.Fatalf(
			"expected token role retained as operator, got %s",
			promotedTok.Role,
		)
	}
	if promotedTok.OwnerRole != auth.RoleAdmin {
		t.Fatalf(
			"expected owner role elevated to admin, got %s",
			promotedTok.OwnerRole,
		)
	}

	p := &auth.Principal{
		Username:       promotedTok.Username,
		Role:           promotedTok.OwnerRole,
		TokenRole:      promotedTok.Role,
		CredentialKind: auth.CredentialToken,
		Scopes:         promotedTok.Scopes,
	}
	if p.Can(auth.ActionUsersWrite) {
		t.Fatal("promoted owner token permitted ActionUsersWrite")
	}
	if !p.Can(auth.ActionTokensWrite) {
		t.Fatal("promoted owner token denied ActionTokensWrite")
	}
}

func testTokenRestrictedRolePreserved(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
) {
	t.Helper()
	res, err := svc.CreateToken(ctx, tokens.CreateTokenCommand{
		Username: "alice",
		Name:     "viewer-restricted-pat",
		Role:     auth.RoleViewer,
		Scopes:   []string{"*"},
	})
	if err != nil {
		t.Fatalf("creating viewer-restricted token: %v", err)
	}
	if res.Token.Role != auth.RoleViewer {
		t.Fatalf("expected role viewer, got %s", res.Token.Role)
	}

	tok, vErr := svc.ValidateToken(ctx, res.RawToken, "127.0.0.1")
	if vErr != nil {
		t.Fatalf("validating viewer-restricted token: %v", vErr)
	}
	if tok.Role != auth.RoleViewer {
		t.Fatalf("expected preserved role viewer, got %s", tok.Role)
	}

	p := &auth.Principal{
		Username:       tok.Username,
		Role:           tok.OwnerRole,
		TokenRole:      tok.Role,
		CredentialKind: auth.CredentialToken,
		Scopes:         tok.Scopes,
	}
	if p.Can(auth.ActionTokensWrite) {
		t.Fatal("expected viewer token to be denied ActionTokensWrite")
	}
	if p.Can(auth.ActionUsersWrite) {
		t.Fatal("expected viewer token to be denied ActionUsersWrite")
	}
	if !p.Can(auth.ActionSystemRead) {
		t.Fatal("expected viewer token to permit ActionSystemRead")
	}
}

func verifyUnreviewedTokenFailsClosed(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
	rawToken string,
) {
	t.Helper()
	_, vErr := svc.ValidateToken(ctx, rawToken, "192.168.1.100")
	if !errors.Is(vErr, tokens.ErrTokenRequiresReview) {
		t.Fatalf("expected ErrTokenRequiresReview, got %v", vErr)
	}

	tokList, lErr := svc.ListTokens(ctx, "alice")
	if lErr != nil {
		t.Fatalf("listing tokens: %v", lErr)
	}
	if len(tokList) != 1 || !tokList[0].RequiresReview {
		t.Fatalf("expected 1 review-required token, got %+v", tokList)
	}
	if tokList[0].LastUsedAt != nil {
		t.Fatalf(
			"expected last_used_at to remain nil " +
				"on denied attempt",
		)
	}
}

func verifyTokenReviewAndUnblock(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
	tokenStore *storage.SQLiteTokenStore,
	rawToken string,
	tokenID string,
) {
	t.Helper()
	unauthErr := svc.ReviewToken(ctx, tokens.ReviewTokenCommand{
		ID:       tokenID,
		Reviewer: "operator",
		IsAdmin:  false,
	})
	if !errors.Is(unauthErr, auth.ErrForbidden) {
		t.Fatalf("expected auth.ErrForbidden: %v", unauthErr)
	}

	authErr := svc.ReviewToken(ctx, tokens.ReviewTokenCommand{
		ID:       tokenID,
		Reviewer: "alice",
		IsAdmin:  false,
	})
	if authErr != nil {
		t.Fatalf("owner review failed: %v", authErr)
	}

	unblockedTok, gErr := tokenStore.GetTokenByID(ctx, tokenID)
	if gErr != nil {
		t.Fatalf("fetching unblocked token: %v", gErr)
	}
	if unblockedTok.RequiresReview {
		t.Fatalf("expected RequiresReview to be false")
	}
	if unblockedTok.ReviewedBy == nil ||
		*unblockedTok.ReviewedBy != "alice" {
		t.Fatalf(
			"expected ReviewedBy = 'alice', got %v",
			unblockedTok.ReviewedBy,
		)
	}
	if unblockedTok.ReviewedAt == nil {
		t.Fatalf("expected ReviewedAt timestamp")
	}

	validTok, vErr2 := svc.ValidateToken(ctx, rawToken, "192.168.1.100")
	if vErr2 != nil {
		t.Fatalf("validating reviewed token failed: %v", vErr2)
	}
	if validTok.LastUsedAt == nil {
		t.Fatalf("expected last_used_at to be populated after success")
	}
}

func verifyAdminReviewOfOperatorToken(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
	tokenStore *storage.SQLiteTokenStore,
) {
	t.Helper()
	rawOp, prefixOp, hashOp, gErr := tokens.GenerateRawToken()
	if gErr != nil {
		t.Fatalf("generating operator raw token: %v", gErr)
	}
	opTokID := "tok_legacy_op_02"
	if cErr := tokenStore.CreateToken(ctx, &tokens.APIToken{
		ID:             opTokID,
		Name:           "operator-legacy-pat",
		TokenPrefix:    prefixOp,
		TokenHash:      hashOp,
		Username:       "operator",
		Role:           auth.RoleOperator,
		Scopes:         []string{"*"},
		CreatedAt:      time.Now().UTC().Add(-24 * time.Hour),
		RequiresReview: true,
	}); cErr != nil {
		t.Fatalf("inserting operator token: %v", cErr)
	}

	adminReviewErr := svc.ReviewToken(ctx, tokens.ReviewTokenCommand{
		ID:       opTokID,
		Reviewer: "alice",
		IsAdmin:  true,
	})
	if adminReviewErr != nil {
		t.Fatalf(
			"admin review of operator token failed: %v",
			adminReviewErr,
		)
	}

	opValidTok, vOpErr := svc.ValidateToken(ctx, rawOp, "127.0.0.1")
	if vOpErr != nil {
		t.Fatalf("validating reviewed operator token: %v", vOpErr)
	}
	if opValidTok.Role != auth.RoleOperator {
		t.Fatalf("expected role operator, got %s", opValidTok.Role)
	}
}

func TestTokenService_LegacyWildcardReviewAndFailClosed(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing test db: %v", cErr)
		}
	}()

	ctx := context.Background()
	authStore := storage.NewSQLiteAuthStore(db)
	tokenStore := storage.NewSQLiteTokenStore(db)
	svc := tokens.NewTokenService(tokenStore, authStore)

	rawToken, prefix, hash, err := tokens.GenerateRawToken()
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}
	tokenID := "tok_legacy_wildcard_01"
	legacyTok := &tokens.APIToken{
		ID:             tokenID,
		Name:           "legacy-wildcard-token",
		TokenPrefix:    prefix,
		TokenHash:      hash,
		Username:       "alice",
		Role:           auth.RoleAdmin,
		Scopes:         []string{"*"},
		CreatedAt:      time.Now().UTC().Add(-48 * time.Hour),
		RequiresReview: true,
	}
	if cErr := tokenStore.CreateToken(ctx, legacyTok); cErr != nil {
		t.Fatalf("inserting legacy token: %v", cErr)
	}

	verifyUnreviewedTokenFailsClosed(ctx, t, svc, rawToken)
	verifyTokenReviewAndUnblock(ctx, t, svc, tokenStore, rawToken, tokenID)
	verifyAdminReviewOfOperatorToken(ctx, t, svc, tokenStore)
	testScopeIntegrityFailClosed(ctx, t, svc, tokenStore)
}

func testScopeIntegrityFailClosed(
	ctx context.Context,
	t *testing.T,
	svc *tokens.TokenService,
	store *storage.SQLiteTokenStore,
) {
	t.Helper()

	// Empty scopes:
	rawEmpty, prefixEmpty, hashEmpty, gErr := tokens.GenerateRawToken()
	if gErr != nil {
		t.Fatalf("generating empty-scopes raw token: %v", gErr)
	}
	if cErr := store.CreateToken(ctx, &tokens.APIToken{
		ID:          "tok_empty_scopes",
		Name:        "empty-scopes-pat",
		TokenPrefix: prefixEmpty,
		TokenHash:   hashEmpty,
		Username:    "alice",
		Role:        auth.RoleAdmin,
		Scopes:      []string{},
		CreatedAt:   time.Now().UTC(),
	}); cErr != nil {
		t.Fatalf("inserting empty-scopes token: %v", cErr)
	}
	_, errEmpty := svc.ValidateToken(ctx, rawEmpty, "127.0.0.1")
	if !errors.Is(errEmpty, tokens.ErrScopesRequired) {
		t.Fatalf(
			"expected ErrScopesRequired for empty scopes, got %v",
			errEmpty,
		)
	}

	// Unknown / invalid scope:
	rawUnknown, prefixUnknown, hashUnknown, gErr2 :=
		tokens.GenerateRawToken()
	if gErr2 != nil {
		t.Fatalf("generating unknown-scope raw token: %v", gErr2)
	}
	if cErr := store.CreateToken(ctx, &tokens.APIToken{
		ID:          "tok_unknown_scope",
		Name:        "unknown-scope-pat",
		TokenPrefix: prefixUnknown,
		TokenHash:   hashUnknown,
		Username:    "alice",
		Role:        auth.RoleAdmin,
		Scopes:      []string{"nonexistent:action"},
		CreatedAt:   time.Now().UTC(),
	}); cErr != nil {
		t.Fatalf("inserting unknown-scope token: %v", cErr)
	}
	_, errUnknown := svc.ValidateToken(ctx, rawUnknown, "127.0.0.1")
	if !errors.Is(errUnknown, tokens.ErrInvalidScope) {
		t.Fatalf(
			"expected ErrInvalidScope for unknown scope, got %v",
			errUnknown,
		)
	}
}
