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
	Username           string   `json:"username"`
	DisplayName        string   `json:"display_name,omitempty"`
	Role               Role     `json:"role"`
	Provider           string   `json:"provider"`
	Groups             []string `json:"groups,omitempty"`
	MustChangePassword bool     `json:"must_change_password"`
}

// User represents persistent user credentials and metadata in SQLite.
type User struct {
	Username           string     `json:"username"`
	PasswordHash       *string    `json:"-"`
	Role               Role       `json:"role"`
	RoleOverride       *Role      `json:"role_override,omitempty"`
	IsActive           bool       `json:"is_active"`
	MustChangePassword bool       `json:"must_change_password"`
	PasswordChangedAt  time.Time  `json:"password_changed_at"`
	CreatedAt          time.Time  `json:"created_at"`
	LastLogin          *time.Time `json:"last_login,omitempty"`
}

// IsPasswordExpired reports whether the user's password age exceeds the
// specified expiry interval. If interval <= 0, passwords never expire.
func (u *User) IsPasswordExpired(interval time.Duration) bool {
	if interval <= 0 {
		return false
	}
	ref := u.PasswordChangedAt
	if ref.IsZero() {
		ref = u.CreatedAt
	}
	if ref.IsZero() {
		return false
	}
	return time.Since(ref) > interval
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
	ID                 string         `json:"id"`
	Username           string         `json:"username"`
	Role               Role           `json:"role"`
	TokenRole          Role           `json:"token_role,omitempty"`
	MustChangePassword bool           `json:"must_change_password"`
	CreatedAt          time.Time      `json:"created_at"`
	ExpiresAt          time.Time      `json:"expires_at"`
	Scopes             []string       `json:"scopes,omitempty"`
	CredentialKind     CredentialKind `json:"credential_kind,omitempty"`
}

// Principal returns the authorization Principal associated with this session.
func (s *Session) Principal() *Principal {
	if s == nil {
		return nil
	}
	kind := s.CredentialKind
	if kind == "" {
		kind = CredentialSession
	}
	return &Principal{
		ID:             s.ID,
		Username:       s.Username,
		Role:           s.Role,
		TokenRole:      s.TokenRole,
		CredentialKind: kind,
		Scopes:         s.Scopes,
	}
}

// Can reports whether the session principal is authorized to perform action.
func (s *Session) Can(action Action) bool {
	if s == nil {
		return false
	}
	return s.Principal().Can(action)
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

// ChangePasswordCommand carries parameters for self-service password update.
type ChangePasswordCommand struct {
	Username        string
	CurrentPassword string
	NewPassword     string
	MinLength       int
}

// InitialSetupCommand carries parameters for the first-boot setup wizard.
type InitialSetupCommand struct {
	Username        string
	Password        string
	ConfirmPassword string
	MinLength       int
}

// AdminCreateUserCommand carries parameters for admin user provisioning.
type AdminCreateUserCommand struct {
	AdminUsername      string
	Username           string
	Password           string
	Role               Role
	MustChangePassword bool
}

// AdminResetPasswordCommand carries parameters for admin password reset.
type AdminResetPasswordCommand struct {
	AdminUsername      string
	Username           string
	NewPassword        string
	MustChangePassword bool
}

// AdminUpdateUserCommand carries parameters for admin user modification.
type AdminUpdateUserCommand struct {
	AdminUsername      string
	Username           string
	Role               Role
	IsActive           bool
	MustChangePassword bool
}

// AdminDeleteUserCommand carries parameters for admin user deletion.
type AdminDeleteUserCommand struct {
	AdminUsername string
	Username      string
}

// ListUsersQuery requests all persistent users in the system.
type ListUsersQuery struct{}

// GetUserQuery requests a single user by username.
type GetUserQuery struct {
	Username string
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
	ErrPasswordTooShort = errors.New(
		"password does not meet minimum length requirement",
	)
	ErrPasswordUnchanged = errors.New(
		"new password cannot match current password",
	)
	ErrCannotDeleteSelf = errors.New(
		"cannot delete currently authenticated user account",
	)
	ErrCannotDeactivateSelf = errors.New(
		"cannot deactivate currently authenticated user account",
	)
	ErrCannotDemoteSelf = errors.New(
		"cannot demote currently authenticated admin account",
	)
	ErrSetupCompleted = errors.New(
		"setup has already been completed",
	)
)
