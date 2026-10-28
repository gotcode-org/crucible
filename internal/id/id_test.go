// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package id_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"gotcode.org/crucible/internal/id"
)

func TestULID_MonotonicityAndFormat(t *testing.T) {
	t.Parallel()

	runID := id.NewRunID()
	if !id.IsValid(runID, id.PrefixRun) {
		t.Fatalf("expected valid prefixed run ULID, got: %s", runID)
	}

	ts, err := id.ParseTimestamp(runID, id.PrefixRun)
	if err != nil {
		t.Fatalf("id.ParseTimestamp() error: %v", err)
	}

	now := time.Now().UTC()
	if diff := now.Sub(ts); diff < -time.Second || diff > 5*time.Second {
		t.Errorf("timestamp drifted beyond tolerance: got %v, now %v", ts, now)
	}

	const count = 100
	ids := make([]string, count)
	for i := 0; i < count; i++ {
		ids[i] = id.NewTokenID()
		if !id.IsValid(ids[i], id.PrefixToken) {
			t.Fatalf("invalid token id at %d: %s", i, ids[i])
		}
	}

	for i := 0; i < count-1; i++ {
		if ids[i] >= ids[i+1] {
			t.Fatalf(
				"monotonicity violation: ids[%d]=%s >= ids[%d]=%s",
				i, ids[i],
				i+1, ids[i+1],
			)
		}
	}
}

func TestULID_Concurrency(t *testing.T) {
	t.Parallel()

	const goroutines = 10
	const perGoroutine = 50

	var wg sync.WaitGroup
	idChan := make(chan string, goroutines*perGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				devID := id.NewDeviceID()
				if !id.IsValid(devID, id.PrefixDevice) {
					t.Errorf("invalid dev id: %s", devID)
				}
				idChan <- devID
			}
		}()
	}

	wg.Wait()
	close(idChan)

	seen := make(map[string]bool)
	for v := range idChan {
		if seen[v] {
			t.Errorf("collision detected for ID: %s", v)
		}
		seen[v] = true
	}

	if len(seen) != goroutines*perGoroutine {
		t.Errorf(
			"expected %d unique IDs, got %d",
			goroutines*perGoroutine,
			len(seen),
		)
	}
}

func TestULID_AllPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		id     string
		prefix string
	}{
		{"run", id.NewRunID(), id.PrefixRun},
		{"token", id.NewTokenID(), id.PrefixToken},
		{"device", id.NewDeviceID(), id.PrefixDevice},
		{"invite", id.NewInviteID(), id.PrefixInvite},
		{"secret", id.NewSecretID(), id.PrefixSecret},
		{"session", id.NewSessionID(), id.PrefixSession},
		{"audit", id.NewAuditID(), id.PrefixAudit},
		{"schedule", id.NewScheduleID(), id.PrefixSchedule},
		{"ssh_key", id.NewSSHKeyID(), id.PrefixSSHKey},
		{"worker", id.NewWorkerID(), id.PrefixWorker},
		{"netbox", id.NewNetboxCacheID(), id.PrefixNetbox},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !id.IsValid(tt.id, tt.prefix) {
				t.Errorf("expected valid ID with prefix %q, got: %s",
					tt.prefix, tt.id)
			}
			if !strings.HasPrefix(tt.id, tt.prefix) {
				t.Errorf("expected prefix %q, got %s", tt.prefix, tt.id)
			}
		})
	}
}

func TestULID_ValidationErrors(t *testing.T) {
	t.Parallel()

	if id.IsValid("invalid_ulid", id.PrefixRun) {
		t.Error("expected false for invalid ulid")
	}
	if id.IsValid("run_short", id.PrefixRun) {
		t.Error("expected false for short ulid")
	}
	if id.IsValid("tok_01JBCXYZ89ABCDEFGHJKMNPQRS", id.PrefixRun) {
		t.Error("expected false for prefix mismatch")
	}

	_, err := id.ParseTimestamp("invalid", id.PrefixRun)
	if err == nil {
		t.Error("expected error for parsing invalid prefix")
	}
}
