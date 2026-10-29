// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package auth provides hexagonal CQRS authentication, session management,
// multi-device RFC 6238 TOTP 2FA, and pluggable identity provider adapters.
package auth

import (
	"errors"
	"time"
)

// Standard RBAC roles within Crucible.
const (
	RoleViewer     Role = "viewer"
	RoleOperator   Role = "operator"
	RoleMaintainer Role = "maintainer"
	RoleAdmin      Role = "admin"
)

// Role defines the authorization privilege tier of an authenticated user.
type Role string

// IsValid reports whether the role is a recognized Crucible role.
func (r Role) IsValid() bool {
	switch r {
	case RoleViewer, RoleOperator, RoleMaintainer, RoleAdmin:
		return true
	default:
		return false
	}
}

// UserIdentity represents an authenticated identity returned by an
// upstream Authenticator provider (e.g. Linux PAM, database, LDAP, OAuth).
type UserIdentity struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name,omitempty"`
	Role        Role     `json:"role"`
	Provider    string   `json:"provider"`
	Groups      []string `json:"groups,omitempty"`
}

// User represents persistent user credentials and metadata in SQLite.
type User struct {
	Username     string     `json:"username"`
	PasswordHash *string    `json:"-"`
	Role         Role       `json:"role"`
	RoleOverride *Role      `json:"role_override,omitempty"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`
}

// EffectiveRole returns RoleOverride if valid, otherwise base Role.
func (u *User) EffectiveRole() Role {
	if u.RoleOverride != nil && u.RoleOverride.IsValid() {
		return *u.RoleOverride
	}
	if u.Role.IsValid() {
		return u.Role
	}
	return RoleViewer
}

// TOTPDevice represents an enrolled 2FA authenticator device.
type TOTPDevice struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	Name            string     `json:"name"`
	EncryptedSecret string     `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	Enabled         bool       `json:"enabled"`
}

// TOTPInvite represents a single-use onboarding invitation for shared
// administrative accounts (e.g. root).
type TOTPInvite struct {
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	Username  string     `json:"username"`
	Label     string     `json:"label"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// Session represents an active authenticated user session.
type Session struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Challenge represents an ephemeral 5-minute pre-auth challenge token
// issued between successful credential verification and TOTP 2FA input.
type Challenge struct {
	Token     string    `json:"token"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LoginCommand carries credentials for primary factor authentication.
type LoginCommand struct {
	Username string
	Password string
}

// LoginResult contains the outcome of primary factor authentication.
type LoginResult struct {
	Requires2FA    bool
	ChallengeToken string
	Session        *Session
	User           *UserIdentity
}

// Verify2FACommand carries parameters to solve a pre-auth 2FA challenge.
type Verify2FACommand struct {
	ChallengeToken string
	Passcode       string
}

// SessionResult contains session metadata following successful 2FA.
type SessionResult struct {
	Session  *Session
	User     *UserIdentity
	DeviceID string
}

// PrepareEnrollmentResult contains an uncommitted secret and QR code for
// authenticator setup.
type PrepareEnrollmentResult struct {
	SecretBase32 string `json:"secret_base32"`
	OTPAuthURI   string `json:"otpauth_uri"`
	QRCodeSVG    string `json:"qr_code_svg"`
}

// EnrollDeviceCommand registers a new 2FA authenticator device.
type EnrollDeviceCommand struct {
	Username     string
	DeviceName   string
	SecretBase32 string
	Passcode     string
}

// RevokeDeviceCommand revokes an enrolled 2FA authenticator device.
type RevokeDeviceCommand struct {
	DeviceID string
	Username string
}

// CreateInviteCommand generates a single-use 24h onboarding invite token.
type CreateInviteCommand struct {
	Username  string
	Label     string
	CreatedBy string
}

// RedeemInviteCommand verifies and consumes a shared account invite token.
type RedeemInviteCommand struct {
	InviteToken  string
	SecretBase32 string
	Passcode     string
}

// LogoutCommand invalidates an active session.
type LogoutCommand struct {
	SessionID string
}

// ValidateSessionQuery checks the validity of an active session token.
type ValidateSessionQuery struct {
	SessionID string
}

// ListDevicesQuery requests all enrolled 2FA devices for a user.
type ListDevicesQuery struct {
	Username string
}

// GetInviteQuery requests metadata for an invite token.
type GetInviteQuery struct {
	InviteToken string
}

// Common authentication errors.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrChallengeExpired   = errors.New("2fa challenge expired or invalid")
	ErrInvalidPasscode    = errors.New("invalid 2fa passcode")
	ErrDeviceNotFound     = errors.New("2fa device not found")
	ErrInviteExpired      = errors.New("invite token expired or invalid")
	ErrInviteAlreadyUsed  = errors.New("invite token has already been redeemed")
	ErrSessionExpired     = errors.New("session expired or invalid")
	ErrUserNotFound       = errors.New("user account not found")
	ErrUserExists         = errors.New("user account already exists")
	ErrUserDisabled       = errors.New("user account is deactivated")
	ErrUnauthorized       = errors.New("insufficient privileges")
	ErrAccessDenied       = errors.New(
		"access denied: user does not belong to an authorized group",
	)
)
