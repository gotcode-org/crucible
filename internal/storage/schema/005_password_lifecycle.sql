-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.

-- Password Lifecycle, Forced First-Login Reset, and Periodic Expiry
ALTER TABLE users ADD COLUMN
    must_change_password INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN
    password_changed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;
