-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.

-- Append-Only Cryptographically Hash-Chained Audit Ledger
CREATE TABLE IF NOT EXISTS audit_ledger (
    seq            INTEGER PRIMARY KEY AUTOINCREMENT, -- Monotonic sequence
    id             TEXT NOT NULL UNIQUE,              -- ULID (aud_01JBC...)
    timestamp      TEXT NOT NULL,                     -- ISO 8601 UTC with ns
    actor_id       TEXT NOT NULL,                     -- User or Token ULID
    actor_type     TEXT NOT NULL,                     -- user|token|system|peer
    ip_address     TEXT NOT NULL,                     -- Client IP or "unix"
    action         TEXT NOT NULL,                     -- e.g. auth.login.success
    resource_type  TEXT NOT NULL,                     -- run|secret|node|token
    resource_id    TEXT NOT NULL,                     -- Target ULID or slug
    status         TEXT NOT NULL,                     -- SUCCESS|FAILURE|DENIED
    details        TEXT NOT NULL DEFAULT '{}',        -- Canonical scrubbed JSON
    previous_hash  TEXT NOT NULL,                     -- SHA-256 of seq-1
    record_hash    TEXT NOT NULL                      -- SHA-256 of record
);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_timestamp
    ON audit_ledger (timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_actor
    ON audit_ledger (actor_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_action
    ON audit_ledger (action, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_resource
    ON audit_ledger (resource_type, resource_id);

-- Anti-tamper triggers: audit records are cryptographically immutable
CREATE TRIGGER IF NOT EXISTS prevent_audit_update
BEFORE UPDATE ON audit_ledger
BEGIN
    SELECT RAISE(ABORT, 'security violation: audit_ledger records are cryptographically immutable and cannot be updated');
END;

CREATE TRIGGER IF NOT EXISTS prevent_audit_delete
BEFORE DELETE ON audit_ledger
BEGIN
    SELECT RAISE(ABORT, 'security violation: audit_ledger records are cryptographically immutable and cannot be deleted');
END;
