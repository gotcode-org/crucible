// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
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

func TestSQLiteAdapter_ConcurrentMigrationStartup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dbPath := filepath.Join(t.TempDir(), "concurrent-migrations.db")
	first, err := NewSQLiteAdapterPath(ctx, dbPath, false, 20000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := first.Close(); closeErr != nil {
			t.Errorf("closing first adapter: %v", closeErr)
		}
	}()
	second, err := NewSQLiteAdapterPath(ctx, dbPath, false, 20000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := second.Close(); closeErr != nil {
			t.Errorf("closing second adapter: %v", closeErr)
		}
	}()

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, adapter := range []*SQLiteAdapter{first, second} {
		go func(adapter *SQLiteAdapter) {
			<-start
			results <- adapter.Migrate(ctx)
		}(adapter)
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent migration failed: %v", err)
		}
	}

	var count int
	if err := first.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&count); err != nil {
		t.Fatalf("querying migration count: %v", err)
	}
	if count != 6 {
		t.Fatalf("migration count = %d, want 6", count)
	}
}

func TestSQLiteAdapter_FailedMigrationRollsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter, err := NewSQLiteAdapterPath(
		ctx,
		filepath.Join(t.TempDir(), "failed-migration.db"),
		false,
		5000,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Errorf("closing adapter: %v", closeErr)
		}
	}()

	fsys := fstest.MapFS{
		"001_fails.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE migration_rollback_probe (id INTEGER);\n" +
				"THIS IS NOT SQL;"),
		},
	}
	if err := adapter.migrateFS(ctx, fsys); err == nil {
		t.Fatal("expected malformed migration to fail")
	}
	var tableCount, migrationCount int
	if err := adapter.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND "+
			"name='migration_rollback_probe'",
	).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if err := adapter.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 || migrationCount != 0 {
		t.Fatalf("failed migration persisted state: table=%d records=%d",
			tableCount, migrationCount)
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
	if st2.MigrationCount != 6 {
		t.Errorf(
			"expected MigrationCount = 6, got %d",
			st2.MigrationCount,
		)
	}
	if st2.LatestVersion != 6 {
		t.Errorf(
			"expected LatestVersion = 6, got %d",
			st2.LatestVersion,
		)
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

func buildPartialSchemaFS(t *testing.T, maxVer int) fstest.MapFS {
	t.Helper()
	subFS, err := fs.Sub(embeddedSQLiteSchemaFS, "schema")
	if err != nil {
		t.Fatalf("accessing embedded schema: %v", err)
	}

	partialFS := make(fstest.MapFS)
	for i := 1; i <= maxVer; i++ {
		entries, rErr := fs.ReadDir(subFS, ".")
		if rErr != nil {
			t.Fatalf("reading embedded schema: %v", rErr)
		}
		for _, e := range entries {
			prefix := fmt.Sprintf("%03d_", i)
			if strings.HasPrefix(e.Name(), prefix) {
				data, readErr := fs.ReadFile(subFS, e.Name())
				if readErr != nil {
					t.Fatalf(
						"reading %s: %v",
						e.Name(),
						readErr,
					)
				}
				partialFS[e.Name()] = &fstest.MapFile{
					Data: data,
				}
			}
		}
	}
	return partialFS
}

func seedLegacyTokenFixture(
	ctx context.Context,
	t *testing.T,
	adapter *SQLiteAdapter,
) {
	t.Helper()
	_, err := adapter.db.ExecContext(ctx, `
INSERT INTO users (
	username, password_hash, role, is_active, must_change_password
) VALUES ('legacy_user', 'hash', 'admin', 1, 0)`)
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}

	seedQuery := `
INSERT INTO api_tokens (
	id, name, token_prefix, token_hash, username, role, scopes_json,
	created_at, revoked
) VALUES
('tok_wildcard', 'Wildcard', 'cb_pat_1', 'hash1', 'legacy_user', 'admin',
 '["*"]', CURRENT_TIMESTAMP, 0),
('tok_multi', 'MultiWild', 'cb_pat_2', 'hash2', 'legacy_user', 'admin',
 '["*","tokens:read"]', CURRENT_TIMESTAMP, 0),
('tok_discrete', 'Discrete', 'cb_pat_3', 'hash3', 'legacy_user', 'admin',
 '["tokens:read"]', CURRENT_TIMESTAMP, 0),
('tok_empty', 'Empty', 'cb_pat_4', 'hash4', 'legacy_user', 'admin',
 '[]', CURRENT_TIMESTAMP, 0),
('tok_malformed', 'BadJSON', 'cb_pat_5', 'hash5', 'legacy_user', 'admin',
 'malformed-json', CURRENT_TIMESTAMP, 0),
('tok_revoked', 'Revoked', 'cb_pat_6', 'hash6', 'legacy_user', 'admin',
 '["*"]', CURRENT_TIMESTAMP, 1)`
	if _, sErr := adapter.db.ExecContext(ctx, seedQuery); sErr != nil {
		t.Fatalf("seeding legacy token fixture: %v", sErr)
	}
}

func assertLegacyTokensReviewState(
	ctx context.Context,
	t *testing.T,
	tokenStore *SQLiteTokenStore,
) {
	t.Helper()
	toks, lErr := tokenStore.ListUserTokens(ctx, "legacy_user")
	if lErr != nil {
		t.Fatalf("listing tokens: %v", lErr)
	}

	expectedReview := map[string]bool{
		"tok_wildcard":  true,
		"tok_multi":     true,
		"tok_discrete":  false,
		"tok_empty":     false,
		"tok_malformed": false,
		"tok_revoked":   false,
	}

	for _, tok := range toks {
		want, ok := expectedReview[tok.ID]
		if !ok {
			t.Errorf("unexpected token in result: %s", tok.ID)
			continue
		}
		if tok.RequiresReview != want {
			t.Errorf(
				"token %s: RequiresReview = %v, want %v",
				tok.ID,
				tok.RequiresReview,
				want,
			)
		}
	}
}

func verifyTokenReviewAuditState(
	ctx context.Context,
	t *testing.T,
	tokenStore *SQLiteTokenStore,
	adapter *SQLiteAdapter,
) {
	t.Helper()
	reviewTime := time.Now().UTC().Truncate(time.Second)
	if revErr := tokenStore.ReviewToken(
		ctx,
		"tok_wildcard",
		"admin_reviewer",
		reviewTime,
	); revErr != nil {
		t.Fatalf("reviewing token: %v", revErr)
	}

	reviewedTok, gErr := tokenStore.GetTokenByID(ctx, "tok_wildcard")
	if gErr != nil {
		t.Fatalf("getting reviewed token: %v", gErr)
	}
	if reviewedTok.RequiresReview {
		t.Errorf("expected RequiresReview = false after review")
	}
	if reviewedTok.ReviewedBy == nil ||
		*reviewedTok.ReviewedBy != "admin_reviewer" {
		t.Errorf(
			"expected ReviewedBy = 'admin_reviewer', got %v",
			reviewedTok.ReviewedBy,
		)
	}
	if reviewedTok.ReviewedAt == nil {
		t.Errorf("expected ReviewedAt to be set")
	}

	// Re-run migration SQL update directly to assert reviewed token
	// is not reset.
	_, execErr := adapter.db.ExecContext(ctx, `
UPDATE api_tokens
SET requires_review = 1
WHERE revoked = 0
  AND reviewed_at IS NULL
  AND (
      scopes_json LIKE '%"*"%'
      OR (json_valid(scopes_json) AND json_extract(scopes_json, '$[0]') = '*')
  );`)
	if execErr != nil {
		t.Fatalf("re-running update query: %v", execErr)
	}

	afterTok, gErr2 := tokenStore.GetTokenByID(ctx, "tok_wildcard")
	if gErr2 != nil {
		t.Fatalf("getting token after update: %v", gErr2)
	}
	if afterTok.RequiresReview {
		t.Errorf(
			"reviewed token was unexpectedly re-flagged " +
				"as requiring review",
		)
	}
}

func TestStorage_LegacyWildcardPATReviewMigration_FixtureAndIdempotency(
	t *testing.T,
) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy-fixture.db")
	adapter, err := NewSQLiteAdapterPath(ctx, dbPath, false, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := adapter.Close(); closeErr != nil {
			t.Errorf("closing adapter: %v", closeErr)
		}
	}()

	partialFS := buildPartialSchemaFS(t, 5)
	if mErr := adapter.migrateFS(ctx, partialFS); mErr != nil {
		t.Fatalf("migrating partial schema (1-5): %v", mErr)
	}

	seedLegacyTokenFixture(ctx, t, adapter)

	if fullErr := adapter.Migrate(ctx); fullErr != nil {
		t.Fatalf("running full migration 006: %v", fullErr)
	}

	tokenStore := NewSQLiteTokenStore(adapter)
	assertLegacyTokensReviewState(ctx, t, tokenStore)

	if rErr := adapter.Migrate(ctx); rErr != nil {
		t.Fatalf("re-running migration (idempotency check): %v", rErr)
	}

	verifyTokenReviewAuditState(ctx, t, tokenStore, adapter)
}
