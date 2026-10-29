// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SQLiteStore implements DeviceStore, SessionStore, ChallengeStore,
// InviteStore, and UserStore on top of modernc.org/sqlite database pool.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates an auth persistence adapter backed by SQLite.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// CreateUser registers a new persistent user in SQLite.
func (s *SQLiteStore) CreateUser(ctx context.Context, u *User) error {
	var roleOverrideStr *string
	if u.RoleOverride != nil {
		str := string(*u.RoleOverride)
		roleOverrideStr = &str
	}

	role := string(u.Role)
	if role == "" {
		role = string(RoleViewer)
	}

	isActive := 1
	if !u.IsActive {
		isActive = 0
	}

	now := u.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	mustChangeInt := 1
	if !u.MustChangePassword {
		mustChangeInt = 0
	}

	pwdChanged := u.PasswordChangedAt
	if pwdChanged.IsZero() {
		pwdChanged = now
	}

	query := `
INSERT INTO users (
	username, password_hash, role, role_override, is_active,
	must_change_password, password_changed_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(
		ctx,
		query,
		u.Username,
		u.PasswordHash,
		role,
		roleOverrideStr,
		isActive,
		mustChangeInt,
		pwdChanged.UTC().Format(time.RFC3339),
		now.UTC().Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrUserExists
		}
		return fmt.Errorf("creating user %s: %w", u.Username, err)
	}
	return nil
}

// UpdateUser updates user role, role override, and active status in SQLite.
func (s *SQLiteStore) UpdateUser(ctx context.Context, u *User) error {
	var roleOverrideStr *string
	if u.RoleOverride != nil {
		str := string(*u.RoleOverride)
		roleOverrideStr = &str
	}

	isActive := 1
	if !u.IsActive {
		isActive = 0
	}

	mustChangeInt := 0
	if u.MustChangePassword {
		mustChangeInt = 1
	}

	query := `
UPDATE users
SET role = ?, role_override = ?, is_active = ?, must_change_password = ?
WHERE username = ?`

	res, err := s.db.ExecContext(
		ctx,
		query,
		string(u.Role),
		roleOverrideStr,
		isActive,
		mustChangeInt,
		u.Username,
	)
	if err != nil {
		return fmt.Errorf("updating user %s: %w", u.Username, err)
	}

	rows, rErr := res.RowsAffected()
	if rErr == nil && rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdatePassword updates the Argon2id password hash for a user, resetting
// the must_change_password flag and refreshing the password_changed_at
// timestamp.
func (s *SQLiteStore) UpdatePassword(
	ctx context.Context,
	username, passwordHash string,
) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `
UPDATE users
SET password_hash = ?, must_change_password = 0, password_changed_at = ?
WHERE username = ?`
	res, err := s.db.ExecContext(ctx, query, passwordHash, now, username)
	if err != nil {
		return fmt.Errorf("updating password for %s: %w", username, err)
	}
	rows, rErr := res.RowsAffected()
	if rErr == nil && rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetMustChangePassword updates the must_change_password flag in SQLite.
func (s *SQLiteStore) SetMustChangePassword(
	ctx context.Context,
	username string,
	mustChange bool,
) error {
	mustChangeInt := 0
	if mustChange {
		mustChangeInt = 1
	}
	query := `UPDATE users SET must_change_password = ? WHERE username = ?`
	res, err := s.db.ExecContext(ctx, query, mustChangeInt, username)
	if err != nil {
		return fmt.Errorf(
			"setting must_change_password for %s: %w",
			username,
			err,
		)
	}
	rows, rErr := res.RowsAffected()
	if rErr == nil && rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// DeleteUser removes a user record and cascades deletions in SQLite.
func (s *SQLiteStore) DeleteUser(ctx context.Context, username string) error {
	query := `DELETE FROM users WHERE username = ?`
	res, err := s.db.ExecContext(ctx, query, username)
	if err != nil {
		return fmt.Errorf("deleting user %s: %w", username, err)
	}
	rows, rErr := res.RowsAffected()
	if rErr == nil && rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

type userRowScan struct {
	uname         string
	passHash      sql.NullString
	roleStr       sql.NullString
	roleOverride  sql.NullString
	isActiveInt   int
	mustChangeInt int
	pwdChangedStr sql.NullString
	createdAtStr  string
	lastLoginStr  sql.NullString
}

func (r *userRowScan) toUser() User {
	u := User{
		Username:           r.uname,
		IsActive:           r.isActiveInt == 1,
		MustChangePassword: r.mustChangeInt == 1,
		CreatedAt:          parseSQLiteTime(r.createdAtStr),
	}
	if r.pwdChangedStr.Valid && r.pwdChangedStr.String != "" {
		u.PasswordChangedAt = parseSQLiteTime(r.pwdChangedStr.String)
	} else {
		u.PasswordChangedAt = u.CreatedAt
	}
	if r.passHash.Valid && r.passHash.String != "" {
		h := r.passHash.String
		u.PasswordHash = &h
	}
	if r.roleStr.Valid && r.roleStr.String != "" {
		u.Role = Role(r.roleStr.String)
	} else {
		u.Role = RoleViewer
	}
	if r.roleOverride.Valid && r.roleOverride.String != "" {
		ro := Role(r.roleOverride.String)
		u.RoleOverride = &ro
	}
	if r.lastLoginStr.Valid && r.lastLoginStr.String != "" {
		t := parseSQLiteTime(r.lastLoginStr.String)
		u.LastLogin = &t
	}
	return u
}

// ListUsers returns all persistent users ordered alphabetically.
func (s *SQLiteStore) ListUsers(ctx context.Context) ([]User, error) {
	query := `
SELECT username, password_hash, role, role_override, is_active,
       must_change_password, password_changed_at, created_at, last_login
FROM users
ORDER BY username ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer func() {
		if cErr := rows.Close(); cErr != nil {
			return
		}
	}()

	var users []User
	for rows.Next() {
		var row userRowScan
		if sErr := rows.Scan(
			&row.uname,
			&row.passHash,
			&row.roleStr,
			&row.roleOverride,
			&row.isActiveInt,
			&row.mustChangeInt,
			&row.pwdChangedStr,
			&row.createdAtStr,
			&row.lastLoginStr,
		); sErr != nil {
			return nil, fmt.Errorf("scanning user row: %w", sErr)
		}
		users = append(users, row.toUser())
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating user rows: %w", err)
	}

	return users, nil
}

// CountUsers returns the total count of registered users in SQLite.
func (s *SQLiteStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM users`
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return count, nil
}

// UpsertUser inserts or updates user role override metadata.
func (s *SQLiteStore) UpsertUser(ctx context.Context, u *User) error {
	var roleOverrideStr *string
	if u.RoleOverride != nil {
		str := string(*u.RoleOverride)
		roleOverrideStr = &str
	}

	var lastLoginStr *string
	if u.LastLogin != nil {
		str := u.LastLogin.UTC().Format(time.RFC3339)
		lastLoginStr = &str
	}

	role := string(u.Role)
	if role == "" {
		role = string(RoleViewer)
	}

	isActive := 1
	if !u.IsActive && u.PasswordHash != nil {
		isActive = 0
	}

	mustChangeInt := 1
	if !u.MustChangePassword {
		mustChangeInt = 0
	}
	pwdChanged := u.PasswordChangedAt
	if pwdChanged.IsZero() {
		pwdChanged = u.CreatedAt
	}
	if pwdChanged.IsZero() {
		pwdChanged = time.Now().UTC()
	}

	query := `
INSERT INTO users (
	username, password_hash, role, role_override, is_active,
	must_change_password, password_changed_at, created_at, last_login
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(username) DO UPDATE SET
	password_hash = COALESCE(excluded.password_hash, users.password_hash),
	role = COALESCE(excluded.role, users.role),
	role_override = excluded.role_override,
	is_active = excluded.is_active,
	must_change_password = excluded.must_change_password,
	password_changed_at = COALESCE(
		excluded.password_changed_at,
		users.password_changed_at
	),
	last_login = COALESCE(excluded.last_login, users.last_login)`

	_, err := s.db.ExecContext(
		ctx,
		query,
		u.Username,
		u.PasswordHash,
		role,
		roleOverrideStr,
		isActive,
		mustChangeInt,
		pwdChanged.UTC().Format(time.RFC3339),
		u.CreatedAt.UTC().Format(time.RFC3339),
		lastLoginStr,
	)
	if err != nil {
		return fmt.Errorf("upserting user %s: %w", u.Username, err)
	}
	return nil
}

// GetUser retrieves persistent user record and role override from SQLite.
func (s *SQLiteStore) GetUser(
	ctx context.Context,
	username string,
) (*User, error) {
	query := `
SELECT username, password_hash, role, role_override, is_active,
       must_change_password, password_changed_at, created_at, last_login
FROM users
WHERE username = ?`

	var row userRowScan
	err := s.db.QueryRowContext(ctx, query, username).Scan(
		&row.uname,
		&row.passHash,
		&row.roleStr,
		&row.roleOverride,
		&row.isActiveInt,
		&row.mustChangeInt,
		&row.pwdChangedStr,
		&row.createdAtStr,
		&row.lastLoginStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user %s: %w", username, err)
	}

	u := row.toUser()
	return &u, nil
}

// TouchUserLogin updates the user's last login timestamp.
func (s *SQLiteStore) TouchUserLogin(
	ctx context.Context,
	username string,
	at time.Time,
) error {
	ensureUserQuery := `
INSERT INTO users (username, created_at, last_login)
VALUES (?, ?, ?)
ON CONFLICT(username) DO UPDATE SET last_login = excluded.last_login`

	atStr := at.UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, ensureUserQuery, username, atStr, atStr)
	if err != nil {
		return fmt.Errorf("touching user login %s: %w", username, err)
	}
	return nil
}

// CreateDevice registers a new 2FA authenticator device.
func (s *SQLiteStore) CreateDevice(
	ctx context.Context,
	dev *TOTPDevice,
) error {
	// Satisfy foreign key constraint on users table
	nowStr := time.Now().UTC().Format(time.RFC3339)
	ensureUser := `
INSERT INTO users (username, created_at)
VALUES (?, ?)
ON CONFLICT(username) DO NOTHING`
	if _, err := s.db.ExecContext(
		ctx,
		ensureUser,
		dev.Username,
		nowStr,
	); err != nil {
		return fmt.Errorf("ensuring user before device creation: %w", err)
	}

	query := `
INSERT INTO user_totp_devices (
	id, username, name, secret_key, created_at, last_used_at, enabled
) VALUES (?, ?, ?, ?, ?, ?, ?)`

	var lastUsedStr *string
	if dev.LastUsedAt != nil {
		sVal := dev.LastUsedAt.UTC().Format(time.RFC3339)
		lastUsedStr = &sVal
	}

	enabledInt := 0
	if dev.Enabled {
		enabledInt = 1
	}

	_, err := s.db.ExecContext(
		ctx,
		query,
		dev.ID,
		dev.Username,
		dev.Name,
		dev.EncryptedSecret,
		dev.CreatedAt.UTC().Format(time.RFC3339),
		lastUsedStr,
		enabledInt,
	)
	if err != nil {
		return fmt.Errorf("inserting totp device: %w", err)
	}
	return nil
}

// GetDevice retrieves an enrolled TOTP device by its unique ID.
func (s *SQLiteStore) GetDevice(
	ctx context.Context,
	id string,
) (*TOTPDevice, error) {
	query := `
SELECT id, username, name, secret_key, created_at, last_used_at, enabled
FROM user_totp_devices
WHERE id = ?`

	var (
		dev         TOTPDevice
		createdAt   string
		lastUsedStr sql.NullString
		enabledInt  int
	)

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&dev.ID,
		&dev.Username,
		&dev.Name,
		&dev.EncryptedSecret,
		&createdAt,
		&lastUsedStr,
		&enabledInt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeviceNotFound
		}
		return nil, fmt.Errorf("querying device %s: %w", id, err)
	}

	dev.CreatedAt = parseSQLiteTime(createdAt)
	if lastUsedStr.Valid && lastUsedStr.String != "" {
		t := parseSQLiteTime(lastUsedStr.String)
		dev.LastUsedAt = &t
	}
	dev.Enabled = enabledInt == 1

	return &dev, nil
}

// ListUserDevices returns all active enrolled devices for a user.
func (s *SQLiteStore) ListUserDevices(
	ctx context.Context,
	username string,
) ([]TOTPDevice, error) {
	query := `
SELECT id, username, name, secret_key, created_at, last_used_at, enabled
FROM user_totp_devices
WHERE username = ? AND enabled = 1
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, username)
	if err != nil {
		return nil, fmt.Errorf("listing devices for %s: %w", username, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	var devices []TOTPDevice
	for rows.Next() {
		var (
			dev         TOTPDevice
			createdAt   string
			lastUsedStr sql.NullString
			enabledInt  int
		)
		if err := rows.Scan(
			&dev.ID,
			&dev.Username,
			&dev.Name,
			&dev.EncryptedSecret,
			&createdAt,
			&lastUsedStr,
			&enabledInt,
		); err != nil {
			return nil, fmt.Errorf("scanning totp device: %w", err)
		}
		dev.CreatedAt = parseSQLiteTime(createdAt)
		if lastUsedStr.Valid && lastUsedStr.String != "" {
			t := parseSQLiteTime(lastUsedStr.String)
			dev.LastUsedAt = &t
		}
		dev.Enabled = enabledInt == 1
		devices = append(devices, dev)
	}

	return devices, rows.Err()
}

// RevokeDevice disables or removes an enrolled device.
func (s *SQLiteStore) RevokeDevice(
	ctx context.Context,
	id, username string,
) error {
	query := `
UPDATE user_totp_devices
SET enabled = 0
WHERE id = ? AND username = ? AND enabled = 1`

	res, err := s.db.ExecContext(ctx, query, id, username)
	if err != nil {
		return fmt.Errorf("revoking device %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking revoke rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// TouchDevice updates the last used timestamp on an enrolled device.
func (s *SQLiteStore) TouchDevice(
	ctx context.Context,
	id string,
	at time.Time,
) error {
	query := `
UPDATE user_totp_devices
SET last_used_at = ?
WHERE id = ?`

	atStr := at.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, query, atStr, id)
	if err != nil {
		return fmt.Errorf("touching device %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking touch rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// CreateSession persists an active user session in SQLite.
func (s *SQLiteStore) CreateSession(
	ctx context.Context,
	sess *Session,
) error {
	// Satisfy foreign key constraint on users table
	nowStr := time.Now().UTC().Format(time.RFC3339)
	ensureUser := `
INSERT INTO users (username, created_at)
VALUES (?, ?)
ON CONFLICT(username) DO NOTHING`
	if _, err := s.db.ExecContext(
		ctx,
		ensureUser,
		sess.Username,
		nowStr,
	); err != nil {
		return fmt.Errorf("ensuring user before session creation: %w", err)
	}

	query := `
INSERT INTO sessions (id, username, role, created_at, expires_at)
VALUES (?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(
		ctx,
		query,
		sess.ID,
		sess.Username,
		string(sess.Role),
		sess.CreatedAt.UTC().Format(time.RFC3339),
		sess.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}
	return nil
}

// GetSession retrieves an active session by its unique ID.
func (s *SQLiteStore) GetSession(
	ctx context.Context,
	id string,
) (*Session, error) {
	query := `
SELECT id, username, role, created_at, expires_at
FROM sessions
WHERE id = ?`

	var (
		sess      Session
		roleStr   string
		createdAt string
		expiresAt string
	)

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&sess.ID,
		&sess.Username,
		&roleStr,
		&createdAt,
		&expiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("querying session %s: %w", id, err)
	}

	sess.Role = Role(roleStr)
	sess.CreatedAt = parseSQLiteTime(createdAt)
	sess.ExpiresAt = parseSQLiteTime(expiresAt)

	if time.Now().UTC().After(sess.ExpiresAt) {
		if rErr := s.RevokeSession(ctx, id); rErr != nil {
			_ = rErr
		}
		return nil, ErrSessionExpired
	}

	return &sess, nil
}

// RevokeSession invalidates and deletes an active session.
func (s *SQLiteStore) RevokeSession(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("revoking session %s: %w", id, err)
	}
	return nil
}

// CreateChallenge records an ephemeral pre-auth 2FA challenge token.
func (s *SQLiteStore) CreateChallenge(
	ctx context.Context,
	ch *Challenge,
) error {
	query := `
INSERT INTO auth_challenges (token, username, expires_at)
VALUES (?, ?, ?)`

	_, err := s.db.ExecContext(
		ctx,
		query,
		ch.Token,
		ch.Username,
		ch.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("inserting auth challenge: %w", err)
	}
	return nil
}

// ConsumeChallenge retrieves and single-use consumes a challenge token.
func (s *SQLiteStore) ConsumeChallenge(
	ctx context.Context,
	token string,
) (*Challenge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning challenge transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil &&
			!errors.Is(rbErr, sql.ErrTxDone) {
			_ = rbErr
		}
	}()

	query := `
SELECT token, username, expires_at
FROM auth_challenges
WHERE token = ?`

	var (
		ch        Challenge
		expiresAt string
	)

	err = tx.QueryRowContext(ctx, query, token).Scan(
		&ch.Token,
		&ch.Username,
		&expiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrChallengeExpired
		}
		return nil, fmt.Errorf("querying challenge: %w", err)
	}

	deleteQuery := `DELETE FROM auth_challenges WHERE token = ?`
	if _, err := tx.ExecContext(ctx, deleteQuery, token); err != nil {
		return nil, fmt.Errorf("consuming challenge: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing challenge consumption: %w", err)
	}

	ch.ExpiresAt = parseSQLiteTime(expiresAt)
	if time.Now().UTC().After(ch.ExpiresAt) {
		return nil, ErrChallengeExpired
	}

	return &ch, nil
}

// CreateInvite records a single-use onboarding invite.
func (s *SQLiteStore) CreateInvite(
	ctx context.Context,
	inv *TOTPInvite,
) error {
	// Satisfy foreign key constraint on users table
	nowStr := time.Now().UTC().Format(time.RFC3339)
	ensureUser := `
INSERT INTO users (username, created_at)
VALUES (?, ?)
ON CONFLICT(username) DO NOTHING`
	if _, err := s.db.ExecContext(
		ctx,
		ensureUser,
		inv.Username,
		nowStr,
	); err != nil {
		return fmt.Errorf("ensuring user before invite creation: %w", err)
	}

	query := `
INSERT INTO totp_invites (
	id, token, username, label, created_by, created_at, expires_at, used_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`

	_, err := s.db.ExecContext(
		ctx,
		query,
		inv.ID,
		inv.Token,
		inv.Username,
		inv.Label,
		inv.CreatedBy,
		inv.CreatedAt.UTC().Format(time.RFC3339),
		inv.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("inserting totp invite: %w", err)
	}
	return nil
}

// GetInvite retrieves invite metadata by token.
func (s *SQLiteStore) GetInvite(
	ctx context.Context,
	token string,
) (*TOTPInvite, error) {
	query := `
SELECT id, token, username, label, created_by, created_at, expires_at, used_at
FROM totp_invites
WHERE token = ?`

	var (
		inv       TOTPInvite
		createdAt string
		expiresAt string
		usedAtStr sql.NullString
	)

	err := s.db.QueryRowContext(ctx, query, token).Scan(
		&inv.ID,
		&inv.Token,
		&inv.Username,
		&inv.Label,
		&inv.CreatedBy,
		&createdAt,
		&expiresAt,
		&usedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInviteExpired
		}
		return nil, fmt.Errorf("querying invite: %w", err)
	}

	inv.CreatedAt = parseSQLiteTime(createdAt)
	inv.ExpiresAt = parseSQLiteTime(expiresAt)
	if usedAtStr.Valid && usedAtStr.String != "" {
		t := parseSQLiteTime(usedAtStr.String)
		inv.UsedAt = &t
	}

	if inv.UsedAt != nil {
		return nil, ErrInviteAlreadyUsed
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return nil, ErrInviteExpired
	}

	return &inv, nil
}

// ConsumeInvite marks an invite token as consumed.
func (s *SQLiteStore) ConsumeInvite(
	ctx context.Context,
	id string,
	at time.Time,
) error {
	query := `
UPDATE totp_invites
SET used_at = ?
WHERE id = ? AND used_at IS NULL`

	atStr := at.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, query, atStr, id)
	if err != nil {
		return fmt.Errorf("consuming invite %s: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking consume invite rows affected: %w", err)
	}
	if rows == 0 {
		return ErrInviteAlreadyUsed
	}
	return nil
}

func parseSQLiteTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999", s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
