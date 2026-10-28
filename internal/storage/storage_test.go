// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/storage"
)

func TestULID_MonotonicityAndFormat(t *testing.T) {
	t.Parallel()

	id := storage.NewRunID()
	if !storage.IsValidULID(id, storage.PrefixRun) {
		t.Fatalf("expected valid prefixed run ULID, got: %s", id)
	}

	ts, err := storage.ParseTimestamp(id, storage.PrefixRun)
	if err != nil {
		t.Fatalf("failed to parse timestamp from %s: %v", id, err)
	}

	if time.Since(ts) > 5*time.Second || time.Since(ts) < -5*time.Second {
		t.Errorf("parsed timestamp %v differs too much from now", ts)
	}

	// Strictly monotonic ordering in tight loop
	const count = 500
	ids := make([]string, count)
	for i := 0; i < count; i++ {
		ids[i] = storage.NewTokenID()
	}

	for i := 0; i < count-1; i++ {
		if ids[i] >= ids[i+1] {
			t.Fatalf("monotonicity violation: ids[%d]=%s >= ids[%d]=%s", i, ids[i], i+1, ids[i+1])
		}
	}
}

func TestULID_Concurrency(t *testing.T) {
	t.Parallel()

	const goroutines = 10
	const perGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	idMap := sync.Map{}

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				id := storage.NewDeviceID()
				if !storage.IsValidULID(id, storage.PrefixDevice) {
					t.Errorf("invalid ULID generated: %s", id)
				}
				if _, loaded := idMap.LoadOrStore(id, true); loaded {
					t.Errorf("collision detected on ULID: %s", id)
				}
			}
		}()
	}

	wg.Wait()
}

func TestDB_OpenAndPragmas(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// In-memory test
	memDB, err := storage.OpenPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("OpenPath(:memory:) error: %v", err)
	}
	defer func() {
		if closeErr := memDB.Close(); closeErr != nil {
			t.Errorf("memDB.Close() error: %v", closeErr)
		}
	}()

	var fkEnabled int
	if queryErr := memDB.DB().QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&fkEnabled); queryErr != nil {
		t.Fatalf("querying foreign_keys pragma: %v", queryErr)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys = 1, got %d", fkEnabled)
	}

	// File-backed test with WAL
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	fileDB, err := storage.Open(ctx, config.StorageConfig{
		DatabasePath:  dbPath,
		WALMode:       true,
		BusyTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("storage.Open() error: %v", err)
	}
	defer func() {
		if closeErr := fileDB.Close(); closeErr != nil {
			t.Errorf("fileDB.Close() error: %v", closeErr)
		}
	}()

	var journalMode string
	if queryErr := fileDB.DB().QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); queryErr != nil {
		t.Fatalf("querying journal_mode: %v", queryErr)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode = 'wal', got %s", journalMode)
	}
}

func TestDB_Migrations(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbInstance, err := storage.OpenPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open in-memory DB: %v", err)
	}
	defer func() {
		if closeErr := dbInstance.Close(); closeErr != nil {
			t.Errorf("dbInstance.Close() error: %v", closeErr)
		}
	}()

	// 1. Initial migration run
	if migErr := dbInstance.Migrate(ctx); migErr != nil {
		t.Fatalf("db.Migrate() failed: %v", migErr)
	}

	// 2. Verify migration tracking
	migrations, err := storage.GetAppliedMigrations(ctx, dbInstance.DB())
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(migrations) < 3 {
		t.Errorf("expected at least 3 migrations applied, got %d", len(migrations))
	}

	// 3. Idempotency test (re-running migrations must succeed without errors)
	if migErr := dbInstance.Migrate(ctx); migErr != nil {
		t.Fatalf("second db.Migrate() run failed: %v", migErr)
	}

	// 4. Verify core schema tables exist and accept inserts
	userID := "jovens"
	_, err = dbInstance.DB().ExecContext(ctx,
		"INSERT INTO users (username, role_override) VALUES (?, ?)",
		userID, "admin")
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	tokenID := storage.NewTokenID()
	_, err = dbInstance.DB().ExecContext(ctx,
		"INSERT INTO api_tokens (id, name, token_prefix, token_hash, username, role) VALUES (?, ?, ?, ?, ?, ?)",
		tokenID, "ci-runner", "cb_pat_test", "hash123", userID, "admin")
	if err != nil {
		t.Fatalf("inserting api_token: %v", err)
	}
}

func TestAuditLedger_ImmutabilityTriggers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbInstance, err := storage.OpenPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}
	defer func() {
		if closeErr := dbInstance.Close(); closeErr != nil {
			t.Errorf("dbInstance.Close() error: %v", closeErr)
		}
	}()

	if migErr := dbInstance.Migrate(ctx); migErr != nil {
		t.Fatalf("db.Migrate() failed: %v", migErr)
	}

	auditID := storage.NewAuditID()
	insertQuery := `
INSERT INTO audit_ledger (
    id, timestamp, actor_id, actor_type, ip_address, action, resource_type,
    resource_id, status, details, previous_hash, record_hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = dbInstance.DB().ExecContext(ctx, insertQuery,
		auditID, time.Now().UTC().Format(time.RFC3339Nano),
		"usr_01JBCDEF", "user", "127.0.0.1", "auth.login.success",
		"session", "ses_01JBCDEF", "SUCCESS", "{}",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
	)
	if err != nil {
		t.Fatalf("inserting audit record: %v", err)
	}

	// Attempting to UPDATE an audit record must be rejected by the
	// anti-tamper trigger.
	_, updateErr := dbInstance.DB().ExecContext(ctx,
		"UPDATE audit_ledger SET status = 'FAILURE' WHERE id = ?", auditID)
	if updateErr == nil {
		t.Fatal("expected error updating audit_ledger due to anti-tamper trigger, got nil")
	}

	// Attempting to DELETE an audit record must be rejected by the
	// anti-tamper trigger.
	_, deleteErr := dbInstance.DB().ExecContext(ctx,
		"DELETE FROM audit_ledger WHERE id = ?", auditID)
	if deleteErr == nil {
		t.Fatal("expected error deleting from audit_ledger due to anti-tamper trigger, got nil")
	}
}

func TestForeignKeyCascades(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbInstance, err := storage.OpenPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}
	defer func() {
		if closeErr := dbInstance.Close(); closeErr != nil {
			t.Errorf("dbInstance.Close() error: %v", closeErr)
		}
	}()

	if migErr := dbInstance.Migrate(ctx); migErr != nil {
		t.Fatalf("db.Migrate() failed: %v", migErr)
	}

	username := "test_operator"
	_, err = dbInstance.DB().ExecContext(ctx,
		"INSERT INTO users (username) VALUES (?)", username)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	deviceID := storage.NewDeviceID()
	_, err = dbInstance.DB().ExecContext(ctx,
		"INSERT INTO user_totp_devices (id, username, name, secret_key) VALUES (?, ?, ?, ?)",
		deviceID, username, "Work Phone", "SECRET32")
	if err != nil {
		t.Fatalf("inserting totp device: %v", err)
	}

	// Delete user; cascade should delete user_totp_devices
	_, err = dbInstance.DB().ExecContext(ctx,
		"DELETE FROM users WHERE username = ?", username)
	if err != nil {
		t.Fatalf("deleting user: %v", err)
	}

	var count int
	err = dbInstance.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_totp_devices WHERE username = ?", username).Scan(&count)
	if err != nil {
		t.Fatalf("counting totp devices: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 totp devices after cascade delete, got %d", count)
	}
}

func TestTransactor_CommitAndRollback(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbInstance, err := storage.OpenPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}
	defer func() {
		if closeErr := dbInstance.Close(); closeErr != nil {
			t.Errorf("dbInstance.Close() error: %v", closeErr)
		}
	}()

	if migErr := dbInstance.Migrate(ctx); migErr != nil {
		t.Fatalf("db.Migrate() failed: %v", migErr)
	}

	// Commit success
	err = dbInstance.WithinTransaction(ctx, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, "INSERT INTO users (username) VALUES (?)", "user_commit")
		return execErr
	})
	if err != nil {
		t.Fatalf("WithinTransaction commit error: %v", err)
	}

	var exists bool
	err = dbInstance.DB().QueryRowContext(ctx,
		"SELECT 1 FROM users WHERE username = 'user_commit'").Scan(&exists)
	if err != nil || !exists {
		t.Fatalf("expected user_commit to exist: %v", err)
	}

	// Rollback on error
	err = dbInstance.WithinTransaction(ctx, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx, "INSERT INTO users (username) VALUES (?)", "user_rollback"); execErr != nil {
			return execErr
		}
		return errors.New("simulated transaction failure")
	})
	if err == nil {
		t.Fatal("expected error from WithinTransaction, got nil")
	}

	var rollbackCount int
	err = dbInstance.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE username = 'user_rollback'").Scan(&rollbackCount)
	if err != nil {
		t.Fatalf("querying user_rollback: %v", err)
	}
	if rollbackCount != 0 {
		t.Errorf("expected user_rollback to be 0 after rollback, got %d", rollbackCount)
	}
}
