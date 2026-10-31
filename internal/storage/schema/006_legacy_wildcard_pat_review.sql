-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.

-- Mark legacy wildcard PATs for explicit owner/admin review
ALTER TABLE api_tokens ADD COLUMN
    requires_review INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_tokens ADD COLUMN
    reviewed_at DATETIME;
ALTER TABLE api_tokens ADD COLUMN
    reviewed_by TEXT;

-- Flag existing unrevoked tokens with wildcard scopes as requiring review.
-- Tokens created under previous versions where omitted scopes defaulted
-- to '*' are ambiguous and fail closed until explicitly reconfirmed by
-- their owner or an administrator.
UPDATE api_tokens
SET requires_review = 1
WHERE revoked = 0
  AND reviewed_at IS NULL
  AND (
      scopes_json LIKE '%"*"%'
      OR (json_valid(scopes_json) AND json_extract(scopes_json, '$[0]') = '*')
  );
