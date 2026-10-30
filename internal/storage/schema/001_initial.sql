-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.

PRAGMA foreign_keys = ON;

-- Centralized Ansible Inventories (YAML & INI)
CREATE TABLE IF NOT EXISTS inventories (
    id             TEXT PRIMARY KEY,        -- slug, e.g. "production", "homelab"
    name           TEXT NOT NULL,           -- Display name
    format         TEXT NOT NULL DEFAULT 'yaml', -- yaml | ini | netbox
    content        TEXT NOT NULL,           -- Raw YAML or INI text
    variables_json TEXT NOT NULL DEFAULT '{}', -- Global extra vars
    host_count     INTEGER NOT NULL DEFAULT 0,
    group_count    INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by     TEXT NOT NULL            -- PAM user or token name
);

-- Cached NetBox Dynamic Inventory Topology
CREATE TABLE IF NOT EXISTS netbox_inventory_cache (
    id            TEXT PRIMARY KEY,         -- ULID (nbc_01JBC...)
    config_file   TEXT NOT NULL,            -- e.g. "inventory/netbox.yaml"
    topology_json TEXT NOT NULL,            -- Full parsed device & group JSON
    device_count  INTEGER NOT NULL DEFAULT 0,
    synced_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Federated Cluster Worker Nodes with Availability, Health & Telemetry
CREATE TABLE IF NOT EXISTS worker_nodes (
    id            TEXT PRIMARY KEY,         -- ULID (wrk_01JBC...) or hostname
    hostname      TEXT NOT NULL,
    datacenter    TEXT NOT NULL,
    labels_json   TEXT NOT NULL DEFAULT '[]',
    health        TEXT NOT NULL DEFAULT 'up',     -- up | down
    availability  TEXT NOT NULL DEFAULT 'active', -- active | drain | paused
    active_jobs   INTEGER NOT NULL DEFAULT 0,
    cpu_percent   REAL,                           -- e.g. 18.4
    mem_used_mb   INTEGER,                        -- e.g. 1120
    mem_total_mb  INTEGER,                        -- e.g. 4096
    mem_percent   REAL,                           -- e.g. 27.3
    load_avg      TEXT,                           -- e.g. "0.35, 0.40, 0.28"
    last_ping     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Users (Identity & Role Overrides)
CREATE TABLE IF NOT EXISTS users (
    username      TEXT PRIMARY KEY,
    role_override TEXT,                    -- Optional manual role override (e.g. 'admin')
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login    DATETIME
);

-- Multi-Device Two-Factor Authentication (Multiple TOTP Tokens per User)
-- Supports shared administrative accounts (e.g. root) where multiple operators
-- register their own authenticator apps, as well as multi-device redundancy.
CREATE TABLE IF NOT EXISTS user_totp_devices (
    id            TEXT PRIMARY KEY,        -- ULID (dev_01JBC...)
    username      TEXT NOT NULL,           -- Owner (e.g. 'root', 'jovens')
    name          TEXT NOT NULL,           -- e.g. "Work Phone", "YubiKey"
    secret_key    TEXT NOT NULL,           -- Base32 TOTP secret (encrypted at rest)
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  DATETIME,
    enabled       INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Secure One-Time TOTP Invite URLs (For Shared Accounts e.g. root)
CREATE TABLE IF NOT EXISTS totp_invites (
    id            TEXT PRIMARY KEY,        -- ULID (inv_01JBC...)
    token         TEXT NOT NULL UNIQUE,    -- High-entropy signed invite token
    username      TEXT NOT NULL,           -- Target account (e.g. 'root')
    label         TEXT NOT NULL,           -- e.g. "Alice Workstation"
    created_by    TEXT NOT NULL,           -- Operator who issued invite
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,       -- 24-hour TTL
    used_at       DATETIME,                -- NULL until consumed
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Built-in SSH Keypairs (Git Deploy Key & Ansible Managed Nodes Key)
CREATE TABLE IF NOT EXISTS ssh_keys (
    id            TEXT PRIMARY KEY,        -- ULID (ssh_01JBC...)
    purpose       TEXT NOT NULL UNIQUE,    -- 'git_sync' | 'ansible_nodes'
    name          TEXT NOT NULL,           -- e.g. "Default Git Deploy Key"
    key_type      TEXT NOT NULL DEFAULT 'ed25519', -- 'ed25519' | 'rsa'
    public_key    TEXT NOT NULL,
    private_key   TEXT NOT NULL,           -- Encrypted at rest
    fingerprint   TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by    TEXT NOT NULL
);

-- Personal Access Tokens (CLI & Automation with Single Revocation)
CREATE TABLE IF NOT EXISTS api_tokens (
    id            TEXT PRIMARY KEY,        -- ULID (tok_01JBC...)
    name          TEXT NOT NULL,           -- e.g. "laptop-cli", "ci-runner"
    token_prefix  TEXT NOT NULL,           -- e.g. "cb_pat_9f82..." (display prefix)
    token_hash    TEXT NOT NULL UNIQUE,    -- SHA-256 hash of plaintext token
    username      TEXT NOT NULL,           -- Owner PAM username
    role          TEXT NOT NULL,           -- viewer | operator | maintainer | admin
    scopes_json   TEXT NOT NULL DEFAULT '["*"]',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  DATETIME,
    last_used_ip  TEXT,
    expires_at    DATETIME,                -- NULL = never expires
    revoked       INTEGER NOT NULL DEFAULT 0,
    revoked_at    DATETIME,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Distributed cluster leases coordinate scheduled execution ownership; leases
-- do not guarantee exactly-once external side effects.
CREATE TABLE IF NOT EXISTS cluster_leases (
    slot_key      TEXT PRIMARY KEY,        -- SHA256(schedule_id + scheduled_time)
    holder_node   TEXT NOT NULL,           -- Node ID holding the lock
    fencing_token INTEGER NOT NULL,        -- Monotonic fencing token
    acquired_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,       -- Heartbeat lease expiration
    status        TEXT NOT NULL DEFAULT 'active' -- active | completed | failed | expired
);

-- Peered Crucible Controllers (Multi-Server Redundancy)
CREATE TABLE IF NOT EXISTS cluster_peers (
    node_id       TEXT PRIMARY KEY,
    address       TEXT NOT NULL,           -- host:port
    status        TEXT NOT NULL DEFAULT 'connected', -- connected | disconnected | degraded
    last_seen     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_sync_seq INTEGER NOT NULL DEFAULT 0
);

-- Active User Web Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,        -- ULID (ses_01JBC...)
    username      TEXT NOT NULL,
    role          TEXT NOT NULL,           -- viewer | operator | maintainer | admin
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Ephemeral Pre-Auth 2FA Challenges (5-minute TTL)
CREATE TABLE IF NOT EXISTS auth_challenges (
    token         TEXT PRIMARY KEY,        -- Cryptographic challenge token
    username      TEXT NOT NULL,
    expires_at    DATETIME NOT NULL
);

-- Core Runs, Events, and Schedules
CREATE TABLE IF NOT EXISTS runs (
    id             TEXT PRIMARY KEY,       -- ULID (run_01JBC...)
    schedule_id    TEXT,
    config_path    TEXT NOT NULL,
    visibility     TEXT NOT NULL DEFAULT 'private', -- 'public' | 'private'
    worker_id      TEXT,
    executing_node TEXT NOT NULL,          -- Controller node managing execution
    status         TEXT NOT NULL DEFAULT 'running',
    started_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at    DATETIME,
    FOREIGN KEY(worker_id) REFERENCES worker_nodes(id)
);

CREATE TABLE IF NOT EXISTS job_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        TEXT NOT NULL,           -- ULID
    job_name      TEXT NOT NULL,
    group_name    TEXT NOT NULL,
    state         TEXT NOT NULL,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    recorded_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS schedules (
    id            TEXT PRIMARY KEY,        -- ULID (sch_01JBC...)
    name          TEXT NOT NULL,
    config_path   TEXT NOT NULL,
    trigger_type  INTEGER NOT NULL,        -- 0: manual, 1: cron, 2: interval
    cron_expr     TEXT,
    run_at        DATETIME,
    env_json      TEXT NOT NULL DEFAULT '{}',
    visibility    TEXT NOT NULL DEFAULT 'private', -- 'public' | 'private'
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Vault Secrets Engine Metadata & Verification (Singleton)
CREATE TABLE IF NOT EXISTS vault_metadata (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    is_initialized       INTEGER NOT NULL DEFAULT 0,
    kdf_salt             BLOB NOT NULL,          -- 16-byte Argon2id salt
    verification_hash    BLOB NOT NULL,          -- Hash to verify unseal passphrase / shares
    key_shares_total     INTEGER NOT NULL DEFAULT 1,
    key_shares_threshold INTEGER NOT NULL DEFAULT 1,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Encrypted Secrets at Rest (AES-256-GCM)
CREATE TABLE IF NOT EXISTS vault_secrets (
    id                   TEXT PRIMARY KEY,       -- ULID (sec_01JBC...)
    name                 TEXT UNIQUE NOT NULL,   -- e.g. "default", "prod_vault"
    secret_type          TEXT NOT NULL,          -- "ansible_vault_pass" | "kv" | "token"
    ciphertext           BLOB NOT NULL,          -- AES-256-GCM encrypted payload
    nonce                BLOB NOT NULL,          -- 12-byte initialization vector
    description          TEXT,
    created_by           TEXT NOT NULL,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Ephemeral Signed View Tokens for Sharing Private Run Reports
CREATE TABLE IF NOT EXISTS report_view_tokens (
    token         TEXT PRIMARY KEY,        -- High-entropy crypto token
    run_id        TEXT NOT NULL,           -- ULID
    created_by    TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,
    FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
);
