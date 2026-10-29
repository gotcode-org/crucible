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
}

// NewQueryService constructs a fresh QueryService with outbound stores.
func NewQueryService(
	sessionStore SessionStore,
	deviceStore DeviceStore,
	inviteStore InviteStore,
) *QueryService {
	return &QueryService{
		sessionStore: sessionStore,
		deviceStore:  deviceStore,
		inviteStore:  inviteStore,
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
	return q.sessionStore.GetSession(ctx, sessionID)
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
