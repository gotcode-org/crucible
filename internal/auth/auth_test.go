// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/id"
	"gotcode.org/crucible/internal/storage"
)

func TestAESGCMCrypter(t *testing.T) {
	t.Parallel()

	key, err := auth.NewRandomKey()
	if err != nil {
		t.Fatalf("generating random master key: %v", err)
	}

	crypter, err := auth.NewAESGCMCrypter(key)
	if err != nil {
		t.Fatalf("constructing crypter: %v", err)
	}

	// Invalid key length check
	if _, kErr := auth.NewAESGCMCrypter([]byte("too-short")); kErr == nil {
		t.Errorf("expected error for short key, got nil")
	}

	plaintext := []byte("JBSWY3DPEHPK3PXP") // 16-char base32
	ciphertext, err := crypter.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypting plaintext: %v", err)
	}

	if ciphertext == string(plaintext) {
		t.Errorf("ciphertext should not match plaintext")
	}

	decrypted, err := crypter.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypting ciphertext: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted %q, expected %q", string(decrypted), string(plaintext))
	}

	// Tampered ciphertext
	tampered := "AAAA" + ciphertext[4:]
	if _, err := crypter.Decrypt(tampered); err == nil {
		t.Errorf("expected error decrypting tampered ciphertext, got nil")
	}
}

func TestTOTPGenerationAndValidation(t *testing.T) {
	t.Parallel()

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("generating secret: %v", err)
	}

	now := time.Now().UTC()
	code, err := auth.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("generating totp code: %v", err)
	}

	if len(code) != 6 {
		t.Errorf("expected 6-digit code, got %s", code)
	}

	// Immediate validation
	if !auth.ValidateCode(secret, code, now, 1) {
		t.Errorf("expected code %s to be valid at generation time", code)
	}

	// Within +/- 30s drift window
	pastTime := now.Add(-30 * time.Second)
	if !auth.ValidateCode(secret, code, pastTime, 1) {
		t.Errorf("expected code to be valid with -30s drift")
	}

	futureTime := now.Add(30 * time.Second)
	if !auth.ValidateCode(secret, code, futureTime, 1) {
		t.Errorf("expected code to be valid with +30s drift")
	}

	// Beyond drift window (+90s)
	distantFuture := now.Add(90 * time.Second)
	if auth.ValidateCode(secret, code, distantFuture, 1) {
		t.Errorf("expected code to be rejected beyond skew window")
	}

	// Invalid code format
	if auth.ValidateCode(secret, "123", now, 1) {
		t.Errorf("expected 3-digit code to fail validation")
	}
	if auth.ValidateCode(secret, "abcdef", now, 1) {
		t.Errorf("expected non-digit code to fail validation")
	}
}

func TestBuildOTPAuthURIAndQR(t *testing.T) {
	t.Parallel()

	secret := "JBSWY3DPEHPK3PXP"
	uri := auth.BuildOTPAuthURI("ops_user", "Crucible", secret)

	if !strings.HasPrefix(uri, "otpauth://totp/Crucible:ops_user?") {
		t.Errorf("unexpected uri prefix: %s", uri)
	}
	if !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") {
		t.Errorf("uri missing secret: %s", uri)
	}

	svg, err := auth.GenerateQRCodeSVG(uri, 256)
	if err != nil {
		t.Fatalf("generating svg: %v", err)
	}
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Errorf("invalid svg output")
	}

	termQR, err := auth.GenerateQRCodeTerminal(uri)
	if err != nil {
		t.Fatalf("generating terminal qr: %v", err)
	}
	if len(termQR) == 0 {
		t.Errorf("empty terminal qr output")
	}
}

func TestPAMAuthenticatorInputValidation(t *testing.T) {
	t.Parallel()

	pam := auth.NewPAMAuthenticator(auth.PAMConfig{
		HelperPath: "/nonexistent/chkpwd",
	})
	if pam.Provider() != "pam" {
		t.Errorf("expected pam provider name, got %s", pam.Provider())
	}

	ctx := context.Background()
	// Malformed username
	if _, err := pam.Authenticate(ctx, "bad user; rm -rf", "pass"); err == nil {
		t.Errorf("expected validation error on malformed username")
	}
	if _, err := pam.Authenticate(ctx, "", "pass"); err == nil {
		t.Errorf("expected validation error on empty username")
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory sqlite db: %v", err)
	}

	for _, f := range []string{
		"001_initial.sql",
		"004_sqlite_users.sql",
		"005_password_lifecycle.sql",
	} {
		schemaPath := filepath.Join("..", "storage", "schema", f)
		schemaSQL, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Fatalf("reading schema file %s: %v", schemaPath, err)
		}
		if _, err := db.Exec(string(schemaSQL)); err != nil {
			t.Fatalf("applying schema %s: %v", f, err)
		}
	}

	return db
}

func TestSQLiteStore_User(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteAuthStore(db)

	u := &auth.User{
		Username:  "admin",
		CreatedAt: time.Now().UTC(),
	}
	roleAdmin := auth.RoleAdmin
	u.RoleOverride = &roleAdmin

	if err := store.UpsertUser(ctx, u); err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	fetchedUser, err := store.GetUser(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if fetchedUser.Username != "admin" ||
		*fetchedUser.RoleOverride != auth.RoleAdmin {
		t.Errorf("mismatched user: %+v", fetchedUser)
	}
}

func TestSQLiteStore_Device(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteAuthStore(db)

	devID := id.NewDeviceID()
	dev := &auth.TOTPDevice{
		ID:              devID,
		Username:        "admin",
		Name:            "Work Phone",
		EncryptedSecret: "enc_blob_secret",
		CreatedAt:       time.Now().UTC(),
		Enabled:         true,
	}

	if err := store.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}

	fetchedDev, err := store.GetDevice(ctx, devID)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if fetchedDev.Name != "Work Phone" || !fetchedDev.Enabled {
		t.Errorf("mismatched device: %+v", fetchedDev)
	}

	devices, err := store.ListUserDevices(ctx, "admin")
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("expected 1 device, got %d", len(devices))
	}

	if rErr := store.RevokeDevice(ctx, devID, "admin"); rErr != nil {
		t.Fatalf("revoke device: %v", rErr)
	}
	remaining, lErr := store.ListUserDevices(ctx, "admin")
	if lErr != nil || len(remaining) != 0 {
		t.Errorf("expected 0 active devices after revoke, got %d", len(remaining))
	}
}

func TestSQLiteStore_Session(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteAuthStore(db)

	sessID := id.NewSessionID()
	sess := &auth.Session{
		ID:        sessID,
		Username:  "admin",
		Role:      auth.RoleAdmin,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	fetchedSess, err := store.GetSession(ctx, sessID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if fetchedSess.Username != "admin" {
		t.Errorf("mismatched session username: %s", fetchedSess.Username)
	}

	if err := store.RevokeSession(ctx, sessID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := store.GetSession(ctx, sessID); err == nil {
		t.Errorf("expected error fetching revoked session")
	}
}

func TestSQLiteStore_ChallengeAndInvite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Errorf("closing db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteAuthStore(db)

	// Challenge
	ch := &auth.Challenge{
		Token:     "test_challenge_token",
		Username:  "admin",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	if err := store.CreateChallenge(ctx, ch); err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	consumed, err := store.ConsumeChallenge(ctx, "test_challenge_token")
	if err != nil {
		t.Fatalf("consume challenge: %v", err)
	}
	if consumed.Username != "admin" {
		t.Errorf("mismatched challenge username: %s", consumed.Username)
	}

	if _, rErr := store.ConsumeChallenge(
		ctx,
		"test_challenge_token",
	); rErr == nil {
		t.Errorf("expected error on replaying challenge")
	}

	// Invite
	invID := id.NewInviteID()
	inv := &auth.TOTPInvite{
		ID:        invID,
		Token:     "invite_token_123",
		Username:  "admin",
		Label:     "Alice Laptop",
		CreatedBy: "admin",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if iErr := store.CreateInvite(ctx, inv); iErr != nil {
		t.Fatalf("create invite: %v", iErr)
	}

	fetchedInv, fErr := store.GetInvite(ctx, "invite_token_123")
	if fErr != nil {
		t.Fatalf("get invite: %v", fErr)
	}
	if fetchedInv.Label != "Alice Laptop" {
		t.Errorf("mismatched invite: %+v", fetchedInv)
	}

	if cErr := store.ConsumeInvite(ctx, invID, time.Now().UTC()); cErr != nil {
		t.Fatalf("consume invite: %v", cErr)
	}
	if _, gErr := store.GetInvite(ctx, "invite_token_123"); gErr == nil {
		t.Errorf("expected error getting consumed invite")
	}
}

func setupTestCQRS() (*auth.CommandService, *auth.QueryService) {
	memAuth := auth.NewMemoryAuthenticator()
	memAuth.AddUser("operator", "secretpass", auth.RoleOperator)

	memStore := auth.NewMemoryStore()
	crypter := auth.NewPlaintextCrypter()

	cmdSvc := auth.NewCommandService(
		memAuth,
		memStore,
		memStore,
		memStore,
		memStore,
		memStore,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(memStore, memStore, memStore)
	return cmdSvc, querySvc
}

func TestCQRS_RestrictToGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	memAuth := auth.NewMemoryAuthenticator()
	memAuth.AddUser("unapproved", "secretpass", auth.RoleViewer)
	memAuth.AddUser("approved_op", "secretpass", auth.RoleOperator)

	memStore := auth.NewMemoryStore()
	crypter := auth.NewPlaintextCrypter()

	cmdSvc := auth.NewCommandService(
		memAuth,
		memStore,
		memStore,
		memStore,
		memStore,
		memStore,
		crypter,
		true, // restrictToGroups = true
	)

	// Unapproved user (RoleViewer) must be rejected with ErrAccessDenied
	_, err := cmdSvc.Login(ctx, auth.LoginCommand{
		Username: "unapproved",
		Password: "secretpass",
	})
	if !errors.Is(err, auth.ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied for unapproved user, got %v", err)
	}

	// Approved user (RoleOperator) must succeed
	res, aErr := cmdSvc.Login(ctx, auth.LoginCommand{
		Username: "approved_op",
		Password: "secretpass",
	})
	if aErr != nil || res.Session == nil {
		t.Fatalf("expected successful login for approved user, got %v", aErr)
	}
}

func TestCQRS_InitialLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cmdSvc, querySvc := setupTestCQRS()

	loginRes, err := cmdSvc.Login(ctx, auth.LoginCommand{
		Username: "operator",
		Password: "secretpass",
	})
	if err != nil {
		t.Fatalf("initial login: %v", err)
	}
	if loginRes.Requires2FA {
		t.Errorf("expected Requires2FA to be false before enrollment")
	}
	if loginRes.Session == nil {
		t.Fatalf("expected active session returned")
	}

	sess, err := querySvc.ValidateSession(ctx, auth.ValidateSessionQuery{
		SessionID: loginRes.Session.ID,
	})
	if err != nil || sess.Username != "operator" {
		t.Errorf("failed to validate initial session: %v", err)
	}
}

func TestCQRS_EnrollAndChallenge2FA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cmdSvc, querySvc := setupTestCQRS()

	// Prepare
	prep, err := cmdSvc.PrepareEnrollment(ctx, "operator")
	if err != nil {
		t.Fatalf("prepare enrollment: %v", err)
	}

	initialCode, err := auth.GenerateCode(prep.SecretBase32, time.Now().UTC())
	if err != nil {
		t.Fatalf("generating initial code: %v", err)
	}

	dev, err := cmdSvc.EnrollDevice(ctx, auth.EnrollDeviceCommand{
		Username:     "operator",
		DeviceName:   "Primary YubiKey",
		SecretBase32: prep.SecretBase32,
		Passcode:     initialCode,
	})
	if err != nil {
		t.Fatalf("enroll device: %v", err)
	}

	// Login triggers 2FA
	login2FA, err := cmdSvc.Login(ctx, auth.LoginCommand{
		Username: "operator",
		Password: "secretpass",
	})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if !login2FA.Requires2FA || login2FA.ChallengeToken == "" {
		t.Fatalf("expected 2FA challenge token")
	}

	// Solve challenge
	code2FA, err := auth.GenerateCode(prep.SecretBase32, time.Now().UTC())
	if err != nil {
		t.Fatalf("generating 2fa passcode: %v", err)
	}

	sessRes, err := cmdSvc.Verify2FA(ctx, auth.Verify2FACommand{
		ChallengeToken: login2FA.ChallengeToken,
		Passcode:       code2FA,
	})
	if err != nil {
		t.Fatalf("verify 2fa: %v", err)
	}
	if sessRes.DeviceID != dev.ID {
		t.Errorf("expected device id %s, got %s", dev.ID, sessRes.DeviceID)
	}
	if sessRes.Session.Role != auth.RoleOperator {
		t.Errorf(
			"expected session role %s, got %s",
			auth.RoleOperator,
			sessRes.Session.Role,
		)
	}
	if sessRes.User.Role != auth.RoleOperator {
		t.Errorf(
			"expected user role %s, got %s",
			auth.RoleOperator,
			sessRes.User.Role,
		)
	}

	devices, err := querySvc.ListDevices(ctx, auth.ListDevicesQuery{
		Username: "operator",
	})
	if err != nil || len(devices) != 1 {
		t.Fatalf("expected 1 enrolled device, got %d", len(devices))
	}
}

func TestCQRS_InviteAndRedeem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cmdSvc, querySvc := setupTestCQRS()

	inv, err := cmdSvc.CreateInvite(ctx, auth.CreateInviteCommand{
		Username:  "operator",
		Label:     "Backup Phone",
		CreatedBy: "operator",
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	backupSecret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("generate backup secret: %v", err)
	}
	backupCode, err := auth.GenerateCode(backupSecret, time.Now().UTC())
	if err != nil {
		t.Fatalf("generate backup code: %v", err)
	}

	redeemedDev, err := cmdSvc.RedeemInvite(ctx, auth.RedeemInviteCommand{
		InviteToken:  inv.Token,
		SecretBase32: backupSecret,
		Passcode:     backupCode,
	})
	if err != nil {
		t.Fatalf("redeem invite: %v", err)
	}

	devices, err := querySvc.ListDevices(ctx, auth.ListDevicesQuery{
		Username: "operator",
	})
	if err != nil || len(devices) != 1 {
		t.Errorf("expected 1 device after invite, got %d", len(devices))
	}

	if rErr := cmdSvc.RevokeDevice(ctx, auth.RevokeDeviceCommand{
		DeviceID: redeemedDev.ID,
		Username: "operator",
	}); rErr != nil {
		t.Fatalf("revoke device: %v", rErr)
	}

	remaining, lErr := querySvc.ListDevices(ctx, auth.ListDevicesQuery{
		Username: "operator",
	})
	if lErr != nil || len(remaining) != 0 {
		t.Errorf("expected 0 devices after revoke, got %d", len(remaining))
	}
}

func TestPasswordLifecycleAndAdminUserManagement(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	defer func() {
		if cErr := db.Close(); cErr != nil {
			t.Logf("closing test db: %v", cErr)
		}
	}()

	store := storage.NewSQLiteAuthStore(db)
	authenticator := auth.NewSQLiteAuthenticator(store).
		WithPasswordExpiry(30 * 24 * time.Hour)

	masterKey := make([]byte, 32)
	crypter, cErr := auth.NewAESGCMCrypter(masterKey)
	if cErr != nil {
		t.Fatalf("creating crypter: %v", cErr)
	}
	cmdSvc := auth.NewCommandService(
		authenticator,
		store,
		store,
		store,
		store,
		store,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(store, store, store, store)
	ctx := context.Background()

	// 1. Seed admin user
	adminHash, hErr := auth.HashPassword("AdminPass12345!")
	if hErr != nil {
		t.Fatalf("hashing admin password: %v", hErr)
	}
	adminUser := &auth.User{
		Username:           "admin",
		PasswordHash:       &adminHash,
		Role:               auth.RoleAdmin,
		IsActive:           true,
		MustChangePassword: false,
		PasswordChangedAt:  time.Now().UTC(),
		CreatedAt:          time.Now().UTC(),
	}
	if err := store.CreateUser(ctx, adminUser); err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	// 2. Admin provisions user with must_change_password = true
	created, err := cmdSvc.AdminCreateUser(ctx, auth.AdminCreateUserCommand{
		AdminUsername:      "admin",
		Username:           "bob",
		Password:           "TempPass12345!",
		Role:               auth.RoleOperator,
		MustChangePassword: true,
	})
	if err != nil {
		t.Fatalf("AdminCreateUser: %v", err)
	}
	if !created.MustChangePassword {
		t.Errorf("expected MustChangePassword true")
	}

	// 3. Bob logs in: session should have MustChangePassword = true
	loginRes, lErr := cmdSvc.Login(ctx, auth.LoginCommand{
		Username: "bob",
		Password: "TempPass12345!",
	})
	if lErr != nil {
		t.Fatalf("bob login: %v", lErr)
	}
	if loginRes.Session == nil || !loginRes.Session.MustChangePassword {
		t.Errorf("expected session.MustChangePassword = true")
	}

	// 4. Bob changes password
	cErr = cmdSvc.ChangePassword(ctx, auth.ChangePasswordCommand{
		Username:        "bob",
		CurrentPassword: "TempPass12345!",
		NewPassword:     "NewSecurePass12345!",
		MinLength:       12,
	})
	if cErr != nil {
		t.Fatalf("ChangePassword error: %v", cErr)
	}

	// 5. Query bob: MustChangePassword should now be false
	bobU, qErr := querySvc.GetUser(ctx, auth.GetUserQuery{Username: "bob"})
	if qErr != nil {
		t.Fatalf("GetUser bob: %v", qErr)
	}
	if bobU.MustChangePassword {
		t.Errorf("expected MustChangePassword false after change")
	}

	// 6. Test password expiry check
	pastTime := time.Now().UTC().Add(-45 * 24 * time.Hour)
	bobU.PasswordChangedAt = pastTime
	if !bobU.IsPasswordExpired(30 * 24 * time.Hour) {
		t.Errorf("expected password to be expired for 45-day age")
	}
}

func setupInitialTestServices(
	t *testing.T,
) (*auth.CommandService, *auth.QueryService, func()) {
	t.Helper()
	db := setupTestDB(t)
	store := storage.NewSQLiteAuthStore(db)
	authenticator := auth.NewSQLiteAuthenticator(store)
	masterKey := make([]byte, 32)
	crypter, err := auth.NewAESGCMCrypter(masterKey)
	if err != nil {
		t.Fatalf("creating crypter: %v", err)
	}
	cmdSvc := auth.NewCommandService(
		authenticator,
		store,
		store,
		store,
		store,
		store,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(store, store, store, store)
	cleanup := func() {
		if cErr := db.Close(); cErr != nil {
			t.Logf("closing test db: %v", cErr)
		}
	}
	return cmdSvc, querySvc, cleanup
}

func TestInitialSetup_Validation(t *testing.T) {
	t.Parallel()
	cmdSvc, _, cleanup := setupInitialTestServices(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Empty password
	_, err := cmdSvc.InitialSetup(ctx, auth.InitialSetupCommand{
		Username: "admin",
		Password: "",
	})
	if err == nil {
		t.Fatal("expected error on empty password")
	}

	// 2. Mismatched confirm password
	_, err = cmdSvc.InitialSetup(ctx, auth.InitialSetupCommand{
		Username:        "admin",
		Password:        "Password123!",
		ConfirmPassword: "DifferentPassword123!",
	})
	if err == nil {
		t.Fatal("expected error on mismatched confirm password")
	}

	// 3. Too short password
	_, err = cmdSvc.InitialSetup(ctx, auth.InitialSetupCommand{
		Username:  "admin",
		Password:  "Short1!",
		MinLength: 12,
	})
	if !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}
}

func TestInitialSetup_SingleRunBarrier(t *testing.T) {
	t.Parallel()
	cmdSvc, querySvc, cleanup := setupInitialTestServices(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Initial state: store has 0 users -> NeedsSetup must be true
	needs, err := querySvc.NeedsSetup(ctx)
	if err != nil || !needs {
		t.Fatalf("expected NeedsSetup=true on fresh database, err=%v", err)
	}

	// 2. Successful Initial Setup
	sess, err := cmdSvc.InitialSetup(ctx, auth.InitialSetupCommand{
		Username:        "sysadmin",
		Password:        "SuperSecretPass123!",
		ConfirmPassword: "SuperSecretPass123!",
		MinLength:       8,
	})
	if err != nil {
		t.Fatalf("InitialSetup: %v", err)
	}
	if sess == nil || sess.Username != "sysadmin" {
		t.Fatalf("unexpected session username: %+v", sess)
	}

	// 3. NeedsSetup must now be false
	needs, err = querySvc.NeedsSetup(ctx)
	if err != nil || needs {
		t.Fatalf("expected NeedsSetup=false after setup, err=%v", err)
	}

	// 4. Single-run barrier: second attempt must fail
	_, err = cmdSvc.InitialSetup(ctx, auth.InitialSetupCommand{
		Username:        "intruder",
		Password:        "HackerPassword123!",
		ConfirmPassword: "HackerPassword123!",
	})
	if !errors.Is(err, auth.ErrSetupCompleted) {
		t.Fatalf(
			"expected ErrSetupCompleted on re-run attempt, got: %v",
			err,
		)
	}

	// 5. Verify user in database
	u, qErr := querySvc.GetUser(ctx, auth.GetUserQuery{
		Username: "sysadmin",
	})
	if qErr != nil || u == nil {
		t.Fatalf("expected sysadmin user in db, err: %v", qErr)
	}
	if u.Role != auth.RoleAdmin {
		t.Errorf("expected active admin user, got: %+v", u)
	}
}
