// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotcode.org/crucible/internal/auth"
	_ "modernc.org/sqlite"
)

func setupTestSQLiteAuthStore(t *testing.T) (*sql.DB, *SQLiteAuthStore) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory sqlite db: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Logf("closing test db: %v", closeErr)
		}
	})

	for _, f := range []string{
		"001_initial.sql",
		"004_sqlite_users.sql",
		"005_password_lifecycle.sql",
	} {
		schemaPath := filepath.Join("schema", f)
		schemaSQL, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Fatalf("reading schema file %s: %v", schemaPath, err)
		}
		if _, err := db.Exec(string(schemaSQL)); err != nil {
			t.Fatalf("applying schema %s: %v", f, err)
		}
	}

	store := NewSQLiteAuthStore(db)
	return db, store
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
