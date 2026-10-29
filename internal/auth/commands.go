// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gotcode.org/crucible/internal/id"
)

// SessionTTL defines the active lifespan of an authenticated session.
const SessionTTL = 24 * time.Hour

// ChallengeTTL defines the lifespan of a pre-auth 2FA verification challenge.
const ChallengeTTL = 5 * time.Minute

// InviteTTL defines the lifespan of a shared account onboarding invite.
const InviteTTL = 24 * time.Hour

// CommandService implements the CommandHandler port handling state mutations
// for authentication, multi-device 2FA, invites, and sessions.
type CommandService struct {
	auth             Authenticator
	deviceStore      DeviceStore
	sessionStore     SessionStore
	challengeStore   ChallengeStore
	inviteStore      InviteStore
	userStore        UserStore
	crypter          SecretCrypter
	restrictToGroups bool
}

// NewCommandService constructs a fresh CommandService wired with outbound SPI
// adapters.
func NewCommandService(
	auth Authenticator,
	deviceStore DeviceStore,
	sessionStore SessionStore,
	challengeStore ChallengeStore,
	inviteStore InviteStore,
	userStore UserStore,
	crypter SecretCrypter,
	restrictToGroups bool,
) *CommandService {
	return &CommandService{
		auth:             auth,
		deviceStore:      deviceStore,
		sessionStore:     sessionStore,
		challengeStore:   challengeStore,
		inviteStore:      inviteStore,
		userStore:        userStore,
		crypter:          crypter,
		restrictToGroups: restrictToGroups,
	}
}

// Login authenticates Factor 1 credentials and checks if 2FA is required.
func (s *CommandService) Login(
	ctx context.Context,
	cmd LoginCommand,
) (*LoginResult, error) {
	if s.auth == nil {
		return nil, errors.New("authenticator not configured")
	}

	identity, err := s.auth.Authenticate(ctx, cmd.Username, cmd.Password)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Apply database role override if present
	if s.userStore != nil {
		if u, uErr := s.userStore.GetUser(ctx, identity.Username); uErr == nil {
			if u.RoleOverride != nil && u.RoleOverride.IsValid() {
				identity.Role = *u.RoleOverride
			}
		}
	}

	if s.restrictToGroups && identity.Role == RoleViewer {
		return nil, ErrAccessDenied
	}

	devices, err := s.deviceStore.ListUserDevices(ctx, identity.Username)
	if err != nil {
		return nil, fmt.Errorf("listing user 2fa devices: %w", err)
	}

	if len(devices) > 0 {
		return s.createPreAuthChallenge(ctx, identity)
	}

	return s.createDirectSession(ctx, identity)
}

func (s *CommandService) createPreAuthChallenge(
	ctx context.Context,
	user *UserIdentity,
) (*LoginResult, error) {
	challengeToken, tokErr := generateSecureToken(32)
	if tokErr != nil {
		return nil, fmt.Errorf("generating challenge token: %w", tokErr)
	}

	ch := &Challenge{
		Token:     challengeToken,
		Username:  user.Username,
		ExpiresAt: time.Now().UTC().Add(ChallengeTTL),
	}

	if err := s.challengeStore.CreateChallenge(ctx, ch); err != nil {
		return nil, fmt.Errorf("persisting auth challenge: %w", err)
	}

	return &LoginResult{
		Requires2FA:    true,
		ChallengeToken: challengeToken,
		User:           user,
	}, nil
}

func (s *CommandService) createDirectSession(
	ctx context.Context,
	user *UserIdentity,
) (*LoginResult, error) {
	now := time.Now().UTC()
	sess := &Session{
		ID:        id.NewSessionID(),
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}

	if err := s.sessionStore.CreateSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	if s.userStore != nil {
		if tErr := s.userStore.TouchUserLogin(ctx, user.Username, now); tErr != nil {
			_ = tErr
		}
	}

	return &LoginResult{
		Requires2FA: false,
		Session:     sess,
		User:        user,
	}, nil
}

// Verify2FA validates a 6-digit passcode against all enrolled devices for a
// pending challenge.
func (s *CommandService) Verify2FA(
	ctx context.Context,
	cmd Verify2FACommand,
) (*SessionResult, error) {
	ch, err := s.challengeStore.ConsumeChallenge(ctx, cmd.ChallengeToken)
	if err != nil {
		return nil, ErrChallengeExpired
	}

	devices, err := s.deviceStore.ListUserDevices(ctx, ch.Username)
	if err != nil {
		return nil, fmt.Errorf("retrieving user devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, ErrDeviceNotFound
	}

	now := time.Now().UTC()
	matchedDevice := s.findMatchingDevice(devices, cmd.Passcode, now)
	if matchedDevice == nil {
		return nil, ErrInvalidPasscode
	}

	if tErr := s.deviceStore.TouchDevice(
		ctx,
		matchedDevice.ID,
		now,
	); tErr != nil {
		return nil, fmt.Errorf("touching device: %w", tErr)
	}

	role := s.resolveUserRole(ctx, ch.Username, now)

	sess := &Session{
		ID:        id.NewSessionID(),
		Username:  ch.Username,
		Role:      role,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}

	if err := s.sessionStore.CreateSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("creating post-2fa session: %w", err)
	}

	userIdent := &UserIdentity{
		Username: ch.Username,
		Role:     role,
		Provider: "crucible",
	}

	return &SessionResult{
		Session:  sess,
		User:     userIdent,
		DeviceID: matchedDevice.ID,
	}, nil
}

func (s *CommandService) findMatchingDevice(
	devices []TOTPDevice,
	passcode string,
	at time.Time,
) *TOTPDevice {
	for i := range devices {
		dev := &devices[i]
		secretBytes, err := s.crypter.Decrypt(dev.EncryptedSecret)
		if err != nil {
			continue
		}
		if ValidateCode(string(secretBytes), passcode, at, 1) {
			return dev
		}
	}
	return nil
}

func (s *CommandService) resolveUserRole(
	ctx context.Context,
	username string,
	at time.Time,
) Role {
	role := RoleViewer
	if username == "root" {
		role = RoleAdmin
	}
	if resolver, ok := s.auth.(RoleResolver); ok {
		if r, found := resolver.ResolveRole(ctx, username); found &&
			r.IsValid() {
			role = r
		}
	}
	if s.userStore != nil {
		if u, uErr := s.userStore.GetUser(ctx, username); uErr == nil {
			if u.EffectiveRole().IsValid() {
				role = u.EffectiveRole()
			}
		}
		if tErr := s.userStore.TouchUserLogin(ctx, username, at); tErr != nil {
			return role
		}
	}
	return role
}

// PrepareEnrollment generates a fresh uncommitted TOTP secret and QR code.
func (s *CommandService) PrepareEnrollment(
	_ context.Context,
	username string,
) (*PrepareEnrollmentResult, error) {
	secret, err := GenerateSecret()
	if err != nil {
		return nil, fmt.Errorf("generating totp secret: %w", err)
	}

	uri := BuildOTPAuthURI(username, DefaultIssuer, secret)
	svg, err := GenerateQRCodeSVG(uri, 256)
	if err != nil {
		return nil, fmt.Errorf("generating qr code svg: %w", err)
	}

	return &PrepareEnrollmentResult{
		SecretBase32: secret,
		OTPAuthURI:   uri,
		QRCodeSVG:    svg,
	}, nil
}

// EnrollDevice commits an enrolled 2FA device after validating an initial
// passcode.
func (s *CommandService) EnrollDevice(
	ctx context.Context,
	cmd EnrollDeviceCommand,
) (*TOTPDevice, error) {
	if !ValidateCode(cmd.SecretBase32, cmd.Passcode, time.Now().UTC(), 1) {
		return nil, ErrInvalidPasscode
	}

	encSecret, err := s.crypter.Encrypt([]byte(cmd.SecretBase32))
	if err != nil {
		return nil, fmt.Errorf("encrypting device secret: %w", err)
	}

	name := strings.TrimSpace(cmd.DeviceName)
	if name == "" {
		name = "Authenticator App"
	}

	dev := &TOTPDevice{
		ID:              id.NewDeviceID(),
		Username:        cmd.Username,
		Name:            name,
		EncryptedSecret: encSecret,
		CreatedAt:       time.Now().UTC(),
		Enabled:         true,
	}

	if err := s.deviceStore.CreateDevice(ctx, dev); err != nil {
		return nil, fmt.Errorf("persisting enrolled device: %w", err)
	}

	return dev, nil
}

// RevokeDevice deletes or disables an enrolled 2FA authenticator device.
func (s *CommandService) RevokeDevice(
	ctx context.Context,
	cmd RevokeDeviceCommand,
) error {
	return s.deviceStore.RevokeDevice(ctx, cmd.DeviceID, cmd.Username)
}

// CreateInvite issues a single-use 24-hour onboarding invite token.
func (s *CommandService) CreateInvite(
	ctx context.Context,
	cmd CreateInviteCommand,
) (*TOTPInvite, error) {
	token, err := generateSecureToken(32)
	if err != nil {
		return nil, fmt.Errorf("generating invite token: %w", err)
	}

	now := time.Now().UTC()
	label := strings.TrimSpace(cmd.Label)
	if label == "" {
		label = "Workstation"
	}

	inv := &TOTPInvite{
		ID:        id.NewInviteID(),
		Token:     token,
		Username:  cmd.Username,
		Label:     label,
		CreatedBy: cmd.CreatedBy,
		CreatedAt: now,
		ExpiresAt: now.Add(InviteTTL),
	}

	if err := s.inviteStore.CreateInvite(ctx, inv); err != nil {
		return nil, fmt.Errorf("persisting invite: %w", err)
	}

	return inv, nil
}

// RedeemInvite consumes a shared account onboarding invite and commits a
// new device.
func (s *CommandService) RedeemInvite(
	ctx context.Context,
	cmd RedeemInviteCommand,
) (*TOTPDevice, error) {
	inv, err := s.inviteStore.GetInvite(ctx, cmd.InviteToken)
	if err != nil {
		return nil, err
	}

	if !ValidateCode(cmd.SecretBase32, cmd.Passcode, time.Now().UTC(), 1) {
		return nil, ErrInvalidPasscode
	}

	encSecret, err := s.crypter.Encrypt([]byte(cmd.SecretBase32))
	if err != nil {
		return nil, fmt.Errorf("encrypting redeemed secret: %w", err)
	}

	now := time.Now().UTC()
	dev := &TOTPDevice{
		ID:              id.NewDeviceID(),
		Username:        inv.Username,
		Name:            inv.Label,
		EncryptedSecret: encSecret,
		CreatedAt:       now,
		Enabled:         true,
	}

	if err := s.deviceStore.CreateDevice(ctx, dev); err != nil {
		return nil, fmt.Errorf("persisting invite device: %w", err)
	}

	if err := s.inviteStore.ConsumeInvite(ctx, inv.ID, now); err != nil {
		return nil, fmt.Errorf("marking invite consumed: %w", err)
	}

	return dev, nil
}

// Logout invalidates the active user session.
func (s *CommandService) Logout(
	ctx context.Context,
	cmd LogoutCommand,
) error {
	return s.sessionStore.RevokeSession(ctx, cmd.SessionID)
}

func generateSecureToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
