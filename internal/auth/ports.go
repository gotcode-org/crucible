// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"context"
	"time"
)

// Authenticator defines the outbound hexagonal port for verifying primary
// credentials against an identity provider (e.g. Linux PAM, database, LDAP,
// or OAuth2/OIDC).
type Authenticator interface {
	// Provider returns the canonical identifier of this authentication backend
	// (e.g. "pam", "database", "ldap", "oauth").
	Provider() string

	// Authenticate verifies the given username and password, returning the
	// resolved UserIdentity on success or ErrInvalidCredentials on failure.
	Authenticate(
		ctx context.Context,
		username, password string,
	) (*UserIdentity, error)
}

// RoleResolver defines an optional port for authenticators capable of
// resolving an operator's role without requiring their password.
type RoleResolver interface {
	ResolveRole(ctx context.Context, username string) (Role, bool)
}

// DeviceStore defines the outbound port for persisting multi-device TOTP 2FA
// credentials.
type DeviceStore interface {
	CreateDevice(ctx context.Context, dev *TOTPDevice) error
	GetDevice(ctx context.Context, id string) (*TOTPDevice, error)
	ListUserDevices(ctx context.Context, username string) ([]TOTPDevice, error)
	RevokeDevice(ctx context.Context, id, username string) error
	TouchDevice(ctx context.Context, id string, at time.Time) error
}

// SessionStore defines the outbound port for managing active user sessions.
type SessionStore interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	RevokeSession(ctx context.Context, id string) error
}

// ChallengeStore defines the outbound port for ephemeral 5-minute pre-auth
// 2FA challenges.
type ChallengeStore interface {
	CreateChallenge(ctx context.Context, ch *Challenge) error
	ConsumeChallenge(ctx context.Context, token string) (*Challenge, error)
}

// InviteStore defines the outbound port for single-use shared account
// onboarding invite links.
type InviteStore interface {
	CreateInvite(ctx context.Context, inv *TOTPInvite) error
	GetInvite(ctx context.Context, token string) (*TOTPInvite, error)
	ConsumeInvite(ctx context.Context, id string, at time.Time) error
}

// UserStore defines the outbound port for persistent user credentials,
// records, and explicit role overrides in SQLite.
type UserStore interface {
	CreateUser(ctx context.Context, u *User) error
	UpdateUser(ctx context.Context, u *User) error
	UpdatePassword(ctx context.Context, username, passwordHash string) error
	DeleteUser(ctx context.Context, username string) error
	ListUsers(ctx context.Context) ([]User, error)
	CountUsers(ctx context.Context) (int, error)
	UpsertUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, username string) (*User, error)
	TouchUserLogin(ctx context.Context, username string, at time.Time) error
}

// SecretCrypter defines the outbound port for encrypting and decrypting
// sensitive TOTP secrets at rest.
type SecretCrypter interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(ciphertext string) ([]byte, error)
}

// CommandHandler defines the inbound hexagonal port for write mutations
// in the authentication subsystem.
type CommandHandler interface {
	// Login authenticates Factor 1 credentials and initiates 2FA challenge if
	// devices are enrolled.
	Login(ctx context.Context, cmd LoginCommand) (*LoginResult, error)

	// Verify2FA validates a 6-digit passcode against enrolled devices for a
	// pending challenge.
	Verify2FA(ctx context.Context, cmd Verify2FACommand) (*SessionResult, error)

	// PrepareEnrollment generates a fresh uncommitted TOTP secret and QR code.
	PrepareEnrollment(
		ctx context.Context,
		username string,
	) (*PrepareEnrollmentResult, error)

	// EnrollDevice commits an enrolled 2FA device after validating an initial
	// passcode.
	EnrollDevice(
		ctx context.Context,
		cmd EnrollDeviceCommand,
	) (*TOTPDevice, error)

	// RevokeDevice deletes an enrolled 2FA authenticator device.
	RevokeDevice(ctx context.Context, cmd RevokeDeviceCommand) error

	// CreateInvite issues a single-use 24-hour onboarding invite token.
	CreateInvite(
		ctx context.Context,
		cmd CreateInviteCommand,
	) (*TOTPInvite, error)

	// RedeemInvite consumes a shared account onboarding invite and commits a
	// new device.
	RedeemInvite(
		ctx context.Context,
		cmd RedeemInviteCommand,
	) (*TOTPDevice, error)

	// Logout invalidates the active user session.
	Logout(ctx context.Context, cmd LogoutCommand) error
}

// QueryHandler defines the inbound hexagonal port for read-only queries
// in the authentication subsystem.
type QueryHandler interface {
	// ValidateSession inspects a session token and verifies it has not expired.
	ValidateSession(ctx context.Context, q ValidateSessionQuery) (*Session, error)

	// ListDevices returns all active 2FA devices registered for the given user.
	ListDevices(ctx context.Context, q ListDevicesQuery) ([]TOTPDevice, error)

	// GetInvite retrieves unconsumed invite metadata by invite token.
	GetInvite(ctx context.Context, q GetInviteQuery) (*TOTPInvite, error)
}
