// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/id"
)

func TestSQLiteAdapter_DoesNotExportSQLAccess(t *testing.T) {
	t.Parallel()

	typeOfAdapter := reflect.TypeOf((*SQLiteAdapter)(nil))
	for _, method := range []string{"DB", "WithinTransaction"} {
		if _, ok := typeOfAdapter.MethodByName(method); ok {
			t.Errorf("SQLiteAdapter unexpectedly exports %s", method)
		}
	}
}

func TestSQLiteAdapter_OpenAndPragmas(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// In-memory test
	memAdapter, err := NewSQLiteAdapterPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("NewSQLiteAdapterPath(:memory:) error: %v", err)
	}
	defer func() {
		if closeErr := memAdapter.Close(); closeErr != nil {
			t.Errorf("memAdapter.Close() error: %v", closeErr)
		}
	}()

	var fkEnabled int
	if queryErr := memAdapter.db.
		QueryRowContext(ctx, "PRAGMA foreign_keys;").
		Scan(&fkEnabled); queryErr != nil {
		t.Fatalf("querying foreign_keys pragma: %v", queryErr)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys = 1, got %d", fkEnabled)
	}
	assertPooledConnectionPragmas(ctx, t, memAdapter)

	// File-backed test with WAL
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	fileAdapter, err := NewSQLiteAdapter(ctx, config.StorageConfig{
		DatabasePath:  dbPath,
		WALMode:       true,
		BusyTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter() error: %v", err)
	}
	defer func() {
		if closeErr := fileAdapter.Close(); closeErr != nil {
			t.Errorf("fileAdapter.Close() error: %v", closeErr)
		}
	}()

	var journalMode string
	if queryErr := fileAdapter.db.
		QueryRowContext(ctx, "PRAGMA journal_mode;").
		Scan(&journalMode); queryErr != nil {
		t.Fatalf("querying journal_mode: %v", queryErr)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode = 'wal', got %s", journalMode)
	}

	assertPooledConnectionPragmas(ctx, t, fileAdapter)
}

func assertPooledConnectionPragmas(
	ctx context.Context,
	t *testing.T,
	adapter *SQLiteAdapter,
) {
	t.Helper()
	conn1, err := adapter.db.Conn(ctx)
	if err != nil {
		t.Fatalf("opening first pooled connection: %v", err)
	}
	defer func() {
		if closeErr := conn1.Close(); closeErr != nil {
			t.Errorf("closing first pooled connection: %v", closeErr)
		}
	}()
	conn2, err := adapter.db.Conn(ctx)
	if err != nil {
		t.Fatalf("opening second pooled connection: %v", err)
	}
	defer func() {
		if closeErr := conn2.Close(); closeErr != nil {
			t.Errorf("closing second pooled connection: %v", closeErr)
		}
	}()
	settings := []struct {
		name    string
		want    int
		minimum int
	}{
		{name: "foreign_keys", want: 1},
		{name: "busy_timeout", minimum: 5000},
		{name: "synchronous", want: 1},
		{name: "temp_store", want: 2},
		{name: "cache_size", want: -64000},
	}
	for _, conn := range []*sql.Conn{conn1, conn2} {
		for _, setting := range settings {
			var got int
			query := "PRAGMA " + setting.name + ";"
			if err := conn.QueryRowContext(ctx, query).Scan(&got); err != nil {
				t.Fatalf("querying pooled %s pragma: %v", setting.name, err)
			}
			matches := got == setting.want
			if setting.minimum > 0 {
				matches = got >= setting.minimum
			}
			if !matches {
				t.Errorf("pooled %s pragma: got %d, want %d (minimum %d)",
					setting.name, got, setting.want, setting.minimum)
			}
		}
	}
}

func TestStorage_CommandAndQueryServices(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter, err := NewSQLiteAdapterPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open in-memory adapter: %v", err)
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Errorf("adapter.Close() error: %v", closeErr)
		}
	}()

	cmdSvc := NewCommandService(adapter)
	querySvc := NewQueryService(adapter)

	// 1. Initial status before migrations
	st1, err := querySvc.GetDatabaseStatus(
		ctx,
		GetDatabaseStatusQuery{},
	)
	if err != nil {
		t.Fatalf("querySvc.GetDatabaseStatus() error: %v", err)
	}
	if st1.MigrationCount != 0 {
		t.Errorf("expected 0 migrations initially, got %d", st1.MigrationCount)
	}

	// 2. Execute migrations through CommandService
	res, err := cmdSvc.RunMigrations(ctx, RunMigrationsCommand{})
	if err != nil {
		t.Fatalf("cmdSvc.RunMigrations() error: %v", err)
	}
	if len(res.AppliedMigrations) < 3 {
		t.Errorf("expected >= 3 applied migrations, got %d",
			len(res.AppliedMigrations))
	}

	// 3. Query applied migrations through QueryService
	records, err := querySvc.GetAppliedMigrations(
		ctx,
		GetAppliedMigrationsQuery{},
	)
	if err != nil {
		t.Fatalf("querySvc.GetAppliedMigrations() error: %v", err)
	}
	if len(records) < 3 {
		t.Errorf("expected >= 3 records, got %d", len(records))
	}

	// 4. Status post-migration
	st2, err := querySvc.GetDatabaseStatus(
		ctx,
		GetDatabaseStatusQuery{},
	)
	if err != nil {
		t.Fatalf("querySvc.GetDatabaseStatus() post-migration error: %v", err)
	}
	if st2.MigrationCount != 5 {
		t.Errorf("expected MigrationCount = 5, got %d", st2.MigrationCount)
	}
	if st2.LatestVersion != 5 {
		t.Errorf("expected LatestVersion = 5, got %d", st2.LatestVersion)
	}
	if st2.TablesCount < 18 {
		t.Errorf("expected >= 18 tables, got %d", st2.TablesCount)
	}
	if st2.Driver != "sqlite" {
		t.Errorf("expected driver = 'sqlite', got %s", st2.Driver)
	}
}

func TestAuditLedger_ImmutabilityTriggers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter, err := NewSQLiteAdapterPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open adapter: %v", err)
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Errorf("adapter.Close() error: %v", closeErr)
		}
	}()

	if migErr := adapter.Migrate(ctx); migErr != nil {
		t.Fatalf("adapter.Migrate() failed: %v", migErr)
	}

	auditID := id.NewAuditID()
	insertQuery := `
INSERT INTO audit_ledger (
    id, timestamp, actor_id, actor_type, ip_address, action, resource_type,
    resource_id, status, details, previous_hash, record_hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = adapter.db.ExecContext(
		ctx, insertQuery,
		auditID, time.Now().UTC().Format(time.RFC3339Nano),
		"usr_01JBCDEF", "user", "127.0.0.1", "auth.login.success",
		"session", "ses_01JBCDEF", "SUCCESS", "{}",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
	)
	if err != nil {
		t.Fatalf("inserting audit record: %v", err)
	}

	// Attempting to UPDATE an audit record must be rejected by trigger.
	_, updateErr := adapter.db.ExecContext(
		ctx,
		"UPDATE audit_ledger SET status = 'FAILURE' WHERE id = ?",
		auditID,
	)
	if updateErr == nil {
		t.Fatal(
			"expected error updating audit_ledger due to anti-tamper trigger",
		)
	}

	// Attempting to DELETE an audit record must be rejected by trigger.
	_, deleteErr := adapter.db.ExecContext(
		ctx,
		"DELETE FROM audit_ledger WHERE id = ?",
		auditID,
	)
	if deleteErr == nil {
		t.Fatal(
			"expected error deleting from audit_ledger due to anti-tamper trigger",
		)
	}
}

func TestForeignKeyCascades(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter, err := NewSQLiteAdapterPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open adapter: %v", err)
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Errorf("adapter.Close() error: %v", closeErr)
		}
	}()

	if migErr := adapter.Migrate(ctx); migErr != nil {
		t.Fatalf("adapter.Migrate() failed: %v", migErr)
	}

	username := "test_operator"
	_, err = adapter.db.ExecContext(
		ctx,
		"INSERT INTO users (username) VALUES (?)",
		username,
	)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	deviceID := id.NewDeviceID()
	_, err = adapter.db.ExecContext(
		ctx,
		"INSERT INTO user_totp_devices (id, username, name, secret_key) "+
			"VALUES (?, ?, ?, ?)",
		deviceID, username, "Work Phone", "SECRET32",
	)
	if err != nil {
		t.Fatalf("inserting totp device: %v", err)
	}

	// Delete user; cascade should delete user_totp_devices
	_, err = adapter.db.ExecContext(
		ctx,
		"DELETE FROM users WHERE username = ?",
		username,
	)
	if err != nil {
		t.Fatalf("deleting user: %v", err)
	}

	var count int
	err = adapter.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM user_totp_devices WHERE username = ?",
		username,
	).Scan(&count)
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

	adapter, err := NewSQLiteAdapterPath(ctx, ":memory:", false, 5000)
	if err != nil {
		t.Fatalf("failed to open adapter: %v", err)
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Errorf("adapter.Close() error: %v", closeErr)
		}
	}()

	if migErr := adapter.Migrate(ctx); migErr != nil {
		t.Fatalf("adapter.Migrate() failed: %v", migErr)
	}

	// Commit success
	err = adapter.withinTransaction(ctx, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(
			ctx,
			"INSERT INTO users (username) VALUES (?)",
			"user_commit",
		)
		return execErr
	})
	if err != nil {
		t.Fatalf("WithinTransaction commit error: %v", err)
	}

	var exists bool
	err = adapter.db.QueryRowContext(
		ctx,
		"SELECT 1 FROM users WHERE username = 'user_commit'",
	).Scan(&exists)
	if err != nil || !exists {
		t.Fatalf("expected user_commit to exist: %v", err)
	}

	// Rollback on error
	err = adapter.withinTransaction(ctx, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(
			ctx,
			"INSERT INTO users (username) VALUES (?)",
			"user_rollback",
		); execErr != nil {
			return execErr
		}
		return errors.New("simulated transaction failure")
	})
	if err == nil {
		t.Fatal("expected error from WithinTransaction, got nil")
	}

	var rollbackCount int
	err = adapter.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM users WHERE username = 'user_rollback'",
	).Scan(&rollbackCount)
	if err != nil {
		t.Fatalf("querying user_rollback: %v", err)
	}
	if rollbackCount != 0 {
		t.Errorf(
			"expected user_rollback to be 0 after rollback, got %d",
			rollbackCount,
		)
	}
}
