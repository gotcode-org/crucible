// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package storage provides pure Go SQLite persistence in WAL mode, external
// raw SQL schema migration execution, and monotonic ULID generation.
package storage

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Standard category prefixes for monotonic entity identifiers.
const (
	PrefixRun      = "run_"
	PrefixToken    = "tok_"
	PrefixDevice   = "dev_"
	PrefixInvite   = "inv_"
	PrefixSecret   = "sec_"
	PrefixSession  = "ses_"
	PrefixAudit    = "aud_"
	PrefixSchedule = "sch_"
	PrefixSSHKey   = "ssh_"
	PrefixWorker   = "wrk_"
	PrefixNetbox   = "nbc_"
)

// ULIDGenerator generates strictly monotonic, concurrency-safe ULID strings.
type ULIDGenerator struct {
	mu      sync.Mutex
	entropy io.Reader
}

var (
	defaultGenOnce sync.Once
	defaultGen     *ULIDGenerator
)

// DefaultGenerator returns the package singleton monotonic ULID generator.
func DefaultGenerator() *ULIDGenerator {
	defaultGenOnce.Do(func() {
		defaultGen = NewULIDGenerator(rand.Reader)
	})
	return defaultGen
}

// NewULIDGenerator constructs a generator using crypto entropy and
// monotonic pool.
func NewULIDGenerator(reader io.Reader) *ULIDGenerator {
	return &ULIDGenerator{
		entropy: ulid.Monotonic(reader, 0),
	}
}

// NewID creates a 26-character time-ordered ULID with an optional category
// prefix.
func (g *ULIDGenerator) NewID(prefix string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UTC()
	u, err := ulid.New(ulid.Timestamp(now), g.entropy)
	if err != nil {
		return "", fmt.Errorf("generating monotonic ULID: %w", err)
	}

	return prefix + u.String(), nil
}

// MustNewID creates a prefixed ULID, panicking only on cryptographic
// entropy failure.
func (g *ULIDGenerator) MustNewID(prefix string) string {
	id, err := g.NewID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

// Convenience constructors backed by the default generator:

// NewRunID generates a monotonic identifier for execution runs (run_01JBC...).
func NewRunID() string {
	return DefaultGenerator().MustNewID(PrefixRun)
}

// NewTokenID generates a monotonic identifier for API tokens (tok_01JBC...).
func NewTokenID() string {
	return DefaultGenerator().MustNewID(PrefixToken)
}

// NewDeviceID generates an identifier for registered 2FA TOTP devices
// (dev_01JBC...).
func NewDeviceID() string {
	return DefaultGenerator().MustNewID(PrefixDevice)
}

// NewInviteID generates an identifier for one-time TOTP invites
// (inv_01JBC...).
func NewInviteID() string {
	return DefaultGenerator().MustNewID(PrefixInvite)
}

// NewSecretID generates an identifier for encrypted vault secrets
// (sec_01JBC...).
func NewSecretID() string {
	return DefaultGenerator().MustNewID(PrefixSecret)
}

// NewSessionID generates an identifier for authenticated web sessions
// (ses_01JBC...).
func NewSessionID() string {
	return DefaultGenerator().MustNewID(PrefixSession)
}

// NewAuditID generates an identifier for immutable audit ledger entries
// (aud_01JBC...).
func NewAuditID() string {
	return DefaultGenerator().MustNewID(PrefixAudit)
}

// NewScheduleID generates an identifier for recurring schedules
// (sch_01JBC...).
func NewScheduleID() string {
	return DefaultGenerator().MustNewID(PrefixSchedule)
}

// NewSSHKeyID generates an identifier for managed deploy/host keys
// (ssh_01JBC...).
func NewSSHKeyID() string {
	return DefaultGenerator().MustNewID(PrefixSSHKey)
}

// NewWorkerID generates an identifier for cluster worker nodes
// (wrk_01JBC...).
func NewWorkerID() string {
	return DefaultGenerator().MustNewID(PrefixWorker)
}

// NewNetboxCacheID generates an identifier for cached topology snapshots
// (nbc_01JBC...).
func NewNetboxCacheID() string {
	return DefaultGenerator().MustNewID(PrefixNetbox)
}

// IsValidULID checks if the given string represents a valid prefixed or raw
// ULID.
func IsValidULID(rawID string, expectedPrefix string) bool {
	if expectedPrefix != "" && !strings.HasPrefix(rawID, expectedPrefix) {
		return false
	}

	ulidPart := strings.TrimPrefix(rawID, expectedPrefix)
	if len(ulidPart) != 26 {
		return false
	}

	_, err := ulid.Parse(ulidPart)
	return err == nil
}

// ParseTimestamp extracts the UTC creation time encoded within a prefixed
// ULID.
func ParseTimestamp(rawID string, prefix string) (time.Time, error) {
	if prefix != "" && !strings.HasPrefix(rawID, prefix) {
		return time.Time{}, errors.New("raw ID does not contain expected prefix")
	}

	ulidPart := strings.TrimPrefix(rawID, prefix)
	u, err := ulid.Parse(ulidPart)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing ULID: %w", err)
	}

	return ulid.Time(u.Time()), nil
}
