// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package id provides concurrency-safe monotonic ULID generation with
// domain-specific entity prefixes.
package id

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

// Generator generates strictly monotonic, concurrency-safe ULID strings.
type Generator struct {
	mu      sync.Mutex
	entropy io.Reader
}

var (
	defaultGenOnce sync.Once
	defaultGen     *Generator
)

// DefaultGenerator returns the package singleton monotonic ULID generator.
func DefaultGenerator() *Generator {
	defaultGenOnce.Do(func() {
		defaultGen = NewGenerator(rand.Reader)
	})
	return defaultGen
}

// NewGenerator constructs a generator using cryptographic entropy and
// a monotonic reader.
func NewGenerator(reader io.Reader) *Generator {
	return &Generator{
		entropy: ulid.Monotonic(reader, 0),
	}
}

// New creates a 26-character time-ordered ULID with an optional category
// prefix.
func (g *Generator) New(prefix string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UTC()
	u, err := ulid.New(ulid.Timestamp(now), g.entropy)
	if err != nil {
		return "", fmt.Errorf("generating monotonic ULID: %w", err)
	}

	return prefix + u.String(), nil
}

// MustNew creates a prefixed ULID, panicking only on cryptographic
// entropy failure.
func (g *Generator) MustNew(prefix string) string {
	id, err := g.New(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

// Convenience constructors backed by the default generator:

// NewRunID generates a monotonic identifier for execution runs (run_01JBC...).
func NewRunID() string {
	return DefaultGenerator().MustNew(PrefixRun)
}

// NewTokenID generates a monotonic identifier for API tokens (tok_01JBC...).
func NewTokenID() string {
	return DefaultGenerator().MustNew(PrefixToken)
}

// NewDeviceID generates an identifier for registered 2FA TOTP devices
// (dev_01JBC...).
func NewDeviceID() string {
	return DefaultGenerator().MustNew(PrefixDevice)
}

// NewInviteID generates an identifier for one-time TOTP invites
// (inv_01JBC...).
func NewInviteID() string {
	return DefaultGenerator().MustNew(PrefixInvite)
}

// NewSecretID generates an identifier for encrypted vault secrets
// (sec_01JBC...).
func NewSecretID() string {
	return DefaultGenerator().MustNew(PrefixSecret)
}

// NewSessionID generates an identifier for authenticated web sessions
// (ses_01JBC...).
func NewSessionID() string {
	return DefaultGenerator().MustNew(PrefixSession)
}

// NewAuditID generates an identifier for immutable audit ledger entries
// (aud_01JBC...).
func NewAuditID() string {
	return DefaultGenerator().MustNew(PrefixAudit)
}

// NewScheduleID generates an identifier for recurring schedules
// (sch_01JBC...).
func NewScheduleID() string {
	return DefaultGenerator().MustNew(PrefixSchedule)
}

// NewSSHKeyID generates an identifier for managed deploy/host keys
// (ssh_01JBC...).
func NewSSHKeyID() string {
	return DefaultGenerator().MustNew(PrefixSSHKey)
}

// NewWorkerID generates an identifier for cluster worker nodes
// (wrk_01JBC...).
func NewWorkerID() string {
	return DefaultGenerator().MustNew(PrefixWorker)
}

// NewNetboxCacheID generates an identifier for cached topology snapshots
// (nbc_01JBC...).
func NewNetboxCacheID() string {
	return DefaultGenerator().MustNew(PrefixNetbox)
}

// IsValid checks if the given string represents a valid prefixed or raw
// ULID.
func IsValid(rawID string, expectedPrefix string) bool {
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
		return time.Time{}, errors.New("raw ID does not match expected prefix")
	}

	ulidPart := strings.TrimPrefix(rawID, prefix)
	u, err := ulid.Parse(ulidPart)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing ULID: %w", err)
	}

	return ulid.Time(u.Time()), nil
}
