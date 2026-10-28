// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package server

import "context"

// Server defines the outbound hexagonal port for orchestrating crucibled
// network listeners and embedded HTTP handlers.
type Server interface {
	// Start binds the TCP and UNIX domain socket listeners and begins serving
	// requests.
	Start(ctx context.Context) error

	// Shutdown gracefully terminates listeners and drains active HTTP
	// connections within the provided context deadline.
	Shutdown(ctx context.Context) error

	// HTTPAddr returns the effective bound TCP network address.
	HTTPAddr() string

	// SocketPath returns the configured filesystem path for the UNIX domain
	// socket.
	SocketPath() string
}
