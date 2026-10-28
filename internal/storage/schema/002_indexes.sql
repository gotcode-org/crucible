-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.

-- Covering and composite indexes for high-throughput queries
CREATE INDEX IF NOT EXISTS idx_inventories_format
    ON inventories (format);

CREATE INDEX IF NOT EXISTS idx_workers_routing
    ON worker_nodes (health, availability);

CREATE INDEX IF NOT EXISTS idx_tokens_hash
    ON api_tokens (token_hash);

CREATE INDEX IF NOT EXISTS idx_tokens_user
    ON api_tokens (username);

CREATE INDEX IF NOT EXISTS idx_totp_devices_user
    ON user_totp_devices (username);

CREATE INDEX IF NOT EXISTS idx_totp_invites_token
    ON totp_invites (token);

CREATE INDEX IF NOT EXISTS idx_ssh_keys_purpose
    ON ssh_keys (purpose);

CREATE INDEX IF NOT EXISTS idx_sessions_user
    ON sessions (username);

CREATE INDEX IF NOT EXISTS idx_runs_status
    ON runs (status);

CREATE INDEX IF NOT EXISTS idx_runs_visibility
    ON runs (visibility, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_schedules_visibility
    ON schedules (visibility);

CREATE INDEX IF NOT EXISTS idx_job_events_run
    ON job_events (run_id);

CREATE INDEX IF NOT EXISTS idx_vault_secrets_name
    ON vault_secrets (name);

CREATE INDEX IF NOT EXISTS idx_report_view_tokens_run
    ON report_view_tokens (run_id);
