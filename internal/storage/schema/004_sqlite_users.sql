-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.

-- SQLite Native User Authentication Credentials and Active State
ALTER TABLE users ADD COLUMN password_hash TEXT;
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'viewer';
ALTER TABLE users ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1;
