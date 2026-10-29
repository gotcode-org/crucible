// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"context"
	"sync"
	"time"
)

type memoryUserRecord struct {
	Password string
	Role     Role
	Groups   []string
}

// MemoryAuthenticator provides an in-memory Authenticator for automated
// unit tests and lightning-fast developer isolation without OS accounts.
type MemoryAuthenticator struct {
	mu    sync.RWMutex
	users map[string]memoryUserRecord
}

// NewMemoryAuthenticator constructs a fresh MemoryAuthenticator.
func NewMemoryAuthenticator() *MemoryAuthenticator {
	return &MemoryAuthenticator{
		users: make(map[string]memoryUserRecord),
	}
}

// Provider returns "memory".
func (m *MemoryAuthenticator) Provider() string {
	return "memory"
}

// ResolveRole retrieves the assigned role for the user from in-memory records.
func (m *MemoryAuthenticator) ResolveRole(
	_ context.Context,
	username string,
) (Role, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.users[username]
	if !ok {
		return "", false
	}
	return rec.Role, true
}

// AddUser seeds or updates an in-memory user credential.
func (m *MemoryAuthenticator) AddUser(
	username, password string,
	role Role,
	groups ...string,
) *MemoryAuthenticator {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[username] = memoryUserRecord{
		Password: password,
		Role:     role,
		Groups:   groups,
	}
	return m
}

// Authenticate verifies credentials against the in-memory user registry.
func (m *MemoryAuthenticator) Authenticate(
	_ context.Context,
	username, password string,
) (*UserIdentity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, ok := m.users[username]
	if !ok || rec.Password != password {
		return nil, ErrInvalidCredentials
	}

	return &UserIdentity{
		Username: username,
		Role:     rec.Role,
		Provider: m.Provider(),
		Groups:   rec.Groups,
	}, nil
}

// MemoryStore provides a full in-memory implementation of DeviceStore,
// SessionStore, ChallengeStore, InviteStore, and UserStore.
type MemoryStore struct {
	mu         sync.RWMutex
	users      map[string]*User
	devices    map[string]*TOTPDevice
	sessions   map[string]*Session
	challenges map[string]*Challenge
	invites    map[string]*TOTPInvite // keyed by token
}

// NewMemoryStore initializes a thread-safe MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:      make(map[string]*User),
		devices:    make(map[string]*TOTPDevice),
		sessions:   make(map[string]*Session),
		challenges: make(map[string]*Challenge),
		invites:    make(map[string]*TOTPInvite),
	}
}

// CreateUser registers a new persistent user in memory.
func (m *MemoryStore) CreateUser(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.Username]; ok {
		return ErrUserExists
	}
	copied := *u
	if copied.PasswordChangedAt.IsZero() {
		copied.PasswordChangedAt = time.Now().UTC()
	}
	m.users[u.Username] = &copied
	return nil
}

// UpdateUser updates user role, role override, and active status in memory.
func (m *MemoryStore) UpdateUser(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.users[u.Username]
	if !ok {
		return ErrUserNotFound
	}
	existing.Role = u.Role
	existing.RoleOverride = u.RoleOverride
	existing.IsActive = u.IsActive
	existing.MustChangePassword = u.MustChangePassword
	return nil
}

// UpdatePassword updates the password hash in memory.
func (m *MemoryStore) UpdatePassword(
	_ context.Context,
	username, passwordHash string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.users[username]
	if !ok {
		return ErrUserNotFound
	}
	existing.PasswordHash = &passwordHash
	existing.PasswordChangedAt = time.Now().UTC()
	existing.MustChangePassword = false
	return nil
}

// SetMustChangePassword updates the must_change_password flag in memory.
func (m *MemoryStore) SetMustChangePassword(
	_ context.Context,
	username string,
	mustChange bool,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.users[username]
	if !ok {
		return ErrUserNotFound
	}
	existing.MustChangePassword = mustChange
	return nil
}

// DeleteUser removes a user record from memory.
func (m *MemoryStore) DeleteUser(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[username]; !ok {
		return ErrUserNotFound
	}
	delete(m.users, username)
	return nil
}

// ListUsers returns all users in memory.
func (m *MemoryStore) ListUsers(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []User
	for _, u := range m.users {
		list = append(list, *u)
	}
	return list, nil
}

// CountUsers returns the total count of registered users in memory.
func (m *MemoryStore) CountUsers(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users), nil
}

// UpsertUser saves or updates user metadata in memory.
func (m *MemoryStore) UpsertUser(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *u
	m.users[u.Username] = &copied
	return nil
}

// GetUser retrieves a user by username.
func (m *MemoryStore) GetUser(
	_ context.Context,
	username string,
) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	copied := *u
	return &copied, nil
}

// TouchUserLogin updates the user's last login timestamp.
func (m *MemoryStore) TouchUserLogin(
	_ context.Context,
	username string,
	at time.Time,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[username]
	if !ok {
		u = &User{Username: username, CreatedAt: at}
		m.users[username] = u
	}
	u.LastLogin = &at
	return nil
}

// CreateDevice registers a new 2FA authenticator device.
func (m *MemoryStore) CreateDevice(_ context.Context, dev *TOTPDevice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *dev
	m.devices[dev.ID] = &copied
	return nil
}

// GetDevice retrieves an enrolled device by its unique ID.
func (m *MemoryStore) GetDevice(
	_ context.Context,
	id string,
) (*TOTPDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dev, ok := m.devices[id]
	if !ok {
		return nil, ErrDeviceNotFound
	}
	copied := *dev
	return &copied, nil
}

// ListUserDevices returns all active enrolled devices for a user.
func (m *MemoryStore) ListUserDevices(
	_ context.Context,
	username string,
) ([]TOTPDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []TOTPDevice
	for _, d := range m.devices {
		if d.Username == username && d.Enabled {
			list = append(list, *d)
		}
	}
	return list, nil
}

// RevokeDevice deletes or disables an enrolled device.
func (m *MemoryStore) RevokeDevice(
	_ context.Context,
	id, username string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[id]
	if !ok || dev.Username != username {
		return ErrDeviceNotFound
	}
	delete(m.devices, id)
	return nil
}

// TouchDevice updates the last used timestamp on an enrolled device.
func (m *MemoryStore) TouchDevice(
	_ context.Context,
	id string,
	at time.Time,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[id]
	if !ok {
		return ErrDeviceNotFound
	}
	dev.LastUsedAt = &at
	return nil
}

// CreateSession persists an active user session.
func (m *MemoryStore) CreateSession(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *s
	m.sessions[s.ID] = &copied
	return nil
}

// GetSession retrieves an active session by its ID.
func (m *MemoryStore) GetSession(
	_ context.Context,
	id string,
) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionExpired
	}
	if time.Now().UTC().After(s.ExpiresAt) {
		return nil, ErrSessionExpired
	}
	copied := *s
	return &copied, nil
}

// RevokeSession invalidates an active session.
func (m *MemoryStore) RevokeSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

// CreateChallenge records an ephemeral pre-auth 2FA challenge token.
func (m *MemoryStore) CreateChallenge(
	_ context.Context,
	ch *Challenge,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *ch
	m.challenges[ch.Token] = &copied
	return nil
}

// ConsumeChallenge retrieves and single-use consumes a challenge token.
func (m *MemoryStore) ConsumeChallenge(
	_ context.Context,
	token string,
) (*Challenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.challenges[token]
	if !ok {
		return nil, ErrChallengeExpired
	}
	delete(m.challenges, token)
	if time.Now().UTC().After(ch.ExpiresAt) {
		return nil, ErrChallengeExpired
	}
	copied := *ch
	return &copied, nil
}

// CreateInvite records a single-use onboarding invite.
func (m *MemoryStore) CreateInvite(_ context.Context, inv *TOTPInvite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *inv
	m.invites[inv.Token] = &copied
	return nil
}

// GetInvite retrieves invite metadata by token.
func (m *MemoryStore) GetInvite(
	_ context.Context,
	token string,
) (*TOTPInvite, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inv, ok := m.invites[token]
	if !ok {
		return nil, ErrInviteExpired
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return nil, ErrInviteExpired
	}
	copied := *inv
	return &copied, nil
}

// ConsumeInvite marks an invite token as consumed.
func (m *MemoryStore) ConsumeInvite(
	_ context.Context,
	id string,
	at time.Time,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inv := range m.invites {
		if inv.ID == id {
			if inv.UsedAt != nil {
				return ErrInviteAlreadyUsed
			}
			inv.UsedAt = &at
			return nil
		}
	}
	return ErrInviteExpired
}
