// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"context"
	"strings"
)

// QueryService implements the QueryHandler port handling read-only queries
// for active sessions, enrolled devices, and pending invite tokens.
type QueryService struct {
	sessionStore SessionStore
	deviceStore  DeviceStore
	inviteStore  InviteStore
	userStore    UserStore
}

// NewQueryService constructs a fresh QueryService with outbound stores.
func NewQueryService(
	sessionStore SessionStore,
	deviceStore DeviceStore,
	inviteStore InviteStore,
	userStore ...UserStore,
) *QueryService {
	var uStore UserStore
	if len(userStore) > 0 {
		uStore = userStore[0]
	}
	return &QueryService{
		sessionStore: sessionStore,
		deviceStore:  deviceStore,
		inviteStore:  inviteStore,
		userStore:    uStore,
	}
}

// ValidateSession inspects a session token and verifies it has not expired.
func (q *QueryService) ValidateSession(
	ctx context.Context,
	query ValidateSessionQuery,
) (*Session, error) {
	sessionID := strings.TrimSpace(query.SessionID)
	if sessionID == "" {
		return nil, ErrSessionExpired
	}
	sess, err := q.sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if q.userStore != nil && sess != nil {
		if u, uErr := q.userStore.GetUser(ctx, sess.Username); uErr == nil &&
			u != nil {
			if !u.IsActive {
				return nil, ErrSessionExpired
			}
			sess.MustChangePassword = u.MustChangePassword
		}
	}
	return sess, nil
}

// ListDevices returns all active 2FA devices registered for the given user.
func (q *QueryService) ListDevices(
	ctx context.Context,
	query ListDevicesQuery,
) ([]TOTPDevice, error) {
	username := strings.TrimSpace(query.Username)
	if username == "" {
		return nil, nil
	}
	return q.deviceStore.ListUserDevices(ctx, username)
}

// GetInvite retrieves unconsumed invite metadata by invite token.
func (q *QueryService) GetInvite(
	ctx context.Context,
	query GetInviteQuery,
) (*TOTPInvite, error) {
	token := strings.TrimSpace(query.InviteToken)
	if token == "" {
		return nil, ErrInviteExpired
	}
	return q.inviteStore.GetInvite(ctx, token)
}

// ListUsers returns all persistent users in the system.
func (q *QueryService) ListUsers(
	ctx context.Context,
	_ ListUsersQuery,
) ([]User, error) {
	if q.userStore == nil {
		return nil, nil
	}
	return q.userStore.ListUsers(ctx)
}

// GetUser retrieves a single user record by username.
func (q *QueryService) GetUser(
	ctx context.Context,
	query GetUserQuery,
) (*User, error) {
	if q.userStore == nil {
		return nil, ErrUserNotFound
	}
	username := strings.TrimSpace(query.Username)
	if username == "" {
		return nil, ErrUserNotFound
	}
	return q.userStore.GetUser(ctx, username)
}
