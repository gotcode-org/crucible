// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"gotcode.org/crucible/internal/auth"
)

func setupTestSQLiteAuthStore(
	t *testing.T,
) (*SQLiteAdapter, *SQLiteAuthStore) {
	t.Helper()
	adapter, err := NewSQLiteAdapterPath(
		context.Background(),
		":memory:",
		false,
		5000,
	)
	if err != nil {
		t.Fatalf("opening in-memory sqlite adapter: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Logf("closing test adapter: %v", closeErr)
		}
	})
	if err := adapter.Migrate(context.Background()); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	store := NewSQLiteAuthStore(adapter)
	return adapter, store
}

func TestSQLiteAuthStore_ProvisionInitialAdminRollsBackSessionFailure(
	t *testing.T,
) {
	t.Parallel()
	ctx := context.Background()
	adapter, store := setupTestSQLiteAuthStore(t)

	// Seed an orphan session with foreign-key checks disabled on one connection.
	// Reusing its primary key makes the setup session insert fail after the user
	// insert, allowing this test to verify both writes roll back together.
	conn, err := adapter.db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquiring seed connection: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("disabling foreign keys for seed: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO sessions (id, username, role, created_at, expires_at)
VALUES ('duplicate-session', 'admin', 'admin', ?, ?)
`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339)); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("closing seed connection after insert failure: %v", closeErr)
		}
		t.Fatalf("seeding conflicting session: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("closing seed connection: %v", err)
	}

	now := time.Now().UTC()
	user := &auth.User{
		Username:          "admin",
		Role:              auth.RoleAdmin,
		IsActive:          true,
		CreatedAt:         now,
		PasswordChangedAt: now,
	}
	session := &auth.Session{
		ID:        "duplicate-session",
		Username:  "admin",
		Role:      auth.RoleAdmin,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := store.ProvisionInitialAdmin(ctx, user, session); err == nil {
		t.Fatal("expected duplicate session insert to fail")
	}
	var userCount int
	if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatalf("counting users after failed setup: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("failed setup left %d user rows, want none", userCount)
	}
	var sessionCount int
	if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&sessionCount); err != nil {
		t.Fatalf("counting sessions after failed setup: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("failed setup changed the seeded session count to %d, want 1", sessionCount)
	}
}

func TestSQLiteAuthenticator_Authenticate(t *testing.T) {
	t.Parallel()
	_, store := setupTestSQLiteAuthStore(t)
	ctx := context.Background()

	hash, hErr := auth.HashPassword("SuperSecret123!")
	if hErr != nil {
		t.Fatalf("auth.HashPassword error: %v", hErr)
	}

	u := &auth.User{
		Username:     "alice",
		PasswordHash: &hash,
		Role:         auth.RoleOperator,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}

	authenticator := auth.NewSQLiteAuthenticator(store)

	// Valid authentication
	ident, err := authenticator.Authenticate(ctx, "alice", "SuperSecret123!")
	if err != nil {
		t.Fatalf("Authenticate valid error: %v", err)
	}
	if ident.Username != "alice" || ident.Role != auth.RoleOperator {
		t.Errorf("unexpected identity: %+v", ident)
	}
	if ident.Provider != "sqlite" {
		t.Errorf("unexpected provider: %s", ident.Provider)
	}

	// Wrong password
	_, err = authenticator.Authenticate(ctx, "alice", "WrongPass")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("expected auth.ErrInvalidCredentials for wrong pass, got: %v", err)
	}

	// Non-existent user
	_, err = authenticator.Authenticate(ctx, "bob", "SuperSecret123!")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("expected auth.ErrInvalidCredentials for missing user, got: %v", err)
	}

	// Deactivated user
	disabledUser := &auth.User{
		Username:     "dave",
		PasswordHash: &hash,
		Role:         auth.RoleOperator,
		IsActive:     false,
		CreatedAt:    time.Now().UTC(),
	}
	if dErr := store.CreateUser(ctx, disabledUser); dErr != nil {
		t.Fatalf("CreateUser dave error: %v", dErr)
	}
	_, err = authenticator.Authenticate(ctx, "dave", "SuperSecret123!")
	if !errors.Is(err, auth.ErrUserDisabled) {
		t.Errorf("expected auth.ErrUserDisabled, got: %v", err)
	}

	// Role override
	adminOverride := auth.RoleAdmin
	u.RoleOverride = &adminOverride
	if uErr := store.UpdateUser(ctx, u); uErr != nil {
		t.Fatalf("UpdateUser error: %v", uErr)
	}
	ident, err = authenticator.Authenticate(ctx, "alice", "SuperSecret123!")
	if err != nil {
		t.Fatalf("Authenticate with override error: %v", err)
	}
	if ident.Role != auth.RoleAdmin {
		t.Errorf("expected auth.RoleAdmin from override, got: %s", ident.Role)
	}
}

func TestSQLiteAuthenticator_ResolveRole(t *testing.T) {
	t.Parallel()
	_, store := setupTestSQLiteAuthStore(t)
	ctx := context.Background()

	hash, hErr := auth.HashPassword("pass")
	if hErr != nil {
		t.Fatalf("auth.HashPassword error: %v", hErr)
	}
	u := &auth.User{
		Username:     "resolver_user",
		PasswordHash: &hash,
		Role:         auth.RoleAdmin,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}

	authenticator := auth.NewSQLiteAuthenticator(store)
	resRole, found := authenticator.ResolveRole(ctx, "resolver_user")
	if !found || resRole != auth.RoleAdmin {
		t.Errorf("expected auth.RoleAdmin, got %s (found: %v)", resRole, found)
	}

	_, missingFound := authenticator.ResolveRole(ctx, "nonexistent")
	if missingFound {
		t.Errorf("expected false for nonexistent user")
	}
}

func TestSQLiteStore_UserCRUD(t *testing.T) {
	t.Parallel()
	_, store := setupTestSQLiteAuthStore(t)
	ctx := context.Background()

	count, cErr := store.CountUsers(ctx)
	if cErr != nil || count != 0 {
		t.Fatalf("expected count 0, got %d, err: %v", count, cErr)
	}

	hash, hErr := auth.HashPassword("pass1")
	if hErr != nil {
		t.Fatalf("auth.HashPassword error: %v", hErr)
	}
	u := &auth.User{
		Username:     "carol",
		PasswordHash: &hash,
		Role:         auth.RoleViewer,
		IsActive:     true,
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}

	// Duplicate create should return auth.ErrUserExists
	if err := store.CreateUser(ctx, u); !errors.Is(err, auth.ErrUserExists) {
		t.Errorf("expected auth.ErrUserExists on duplicate create, got: %v", err)
	}

	count, cErr = store.CountUsers(ctx)
	if cErr != nil || count != 1 {
		t.Errorf("expected count 1, got %d, err: %v", count, cErr)
	}

	// Delete
	if err := store.DeleteUser(ctx, "carol"); err != nil {
		t.Fatalf("DeleteUser error: %v", err)
	}
	_, err := store.GetUser(ctx, "carol")
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("expected auth.ErrUserNotFound after delete, got: %v", err)
	}
}

func TestSQLiteStore_UserListAndUpdate(t *testing.T) {
	t.Parallel()
	_, store := setupTestSQLiteAuthStore(t)
	ctx := context.Background()

	hash, hErr := auth.HashPassword("pass1")
	if hErr != nil {
		t.Fatalf("auth.HashPassword error: %v", hErr)
	}
	u := &auth.User{
		Username:     "carol",
		PasswordHash: &hash,
		Role:         auth.RoleViewer,
		IsActive:     true,
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}

	// List
	users, lErr := store.ListUsers(ctx)
	if lErr != nil || len(users) != 1 {
		t.Fatalf(
			"ListUsers expected 1 user, got: %v, err: %v",
			len(users),
			lErr,
		)
	}
	if users[0].Username != "carol" || users[0].Role != auth.RoleViewer {
		t.Errorf("unexpected user in list: %+v", users[0])
	}

	// Update Password
	newHash, nhErr := auth.HashPassword("pass2")
	if nhErr != nil {
		t.Fatalf("auth.HashPassword pass2 error: %v", nhErr)
	}
	if err := store.UpdatePassword(ctx, "carol", newHash); err != nil {
		t.Fatalf("UpdatePassword error: %v", err)
	}
	fetched, gErr := store.GetUser(ctx, "carol")
	if gErr != nil {
		t.Fatalf("GetUser error: %v", gErr)
	}
	if *fetched.PasswordHash != newHash {
		t.Errorf("password hash not updated")
	}
}
