# Crucible — AI Development Handoff

> **Purpose:** Verified local state and concise guidance for the next
> development session.
> **Last updated:** 2026-10-30
> **Repository:** `/home/jovens/code/forks/crucible`
> **Branch:** `development`
> **Base commit:** `ee59cad` (merged mainline `upstream/main`)
> **Working tree:** Uncommitted security hardening changes (addressing
> `FOLLOWUP.md`).

## Current checkout

- `origin/main` and `upstream/main` are at `ee59cad` following the signed
  squash merge of `pr/pat-scope-enforcement-for-v0.1.0`.
- The active branch is `development`, tracking `upstream/main`.
- All security hardening items from `FOLLOWUP.md` have been implemented and
  verified locally in the working tree without committing, tagging, or pushing.

## Project direction to preserve

- Crucible is a Go modular monolith using hexagonal architecture and CQRS.
- `crucibled` is the daemon; `cruciblectl` is its separate management client.
  The CLI and web UI use the same daemon API and application rules.
- SQLite is the current persistence implementation. Keep ports feature-owned
  and concrete adapters in `internal/storage`; do not add an ORM, generic SQL
  dialect layer, or speculative PostgreSQL/MySQL adapter.
- Keep the standalone product useful before implementing distributed worker
  behavior. Define failure and delivery semantics before that work.
- Local Unix-socket administration is an intentional trust boundary for
  authorized local/SSH administrators; remote clients authenticate normally.

## Hardened PAT authorization changes

Following peer model review in `FOLLOWUP.md`, the following authorization
boundaries have been hardened:

1. **Owner Role Ceiling & Dynamic Updates**:
   - `TokenService.ValidateToken()` retrieves the owner via `UserLookup` and
     derives the token's effective role directly from `user.EffectiveRole()`.
   - Token metadata can never outrank or exceed the owner's current role.
   - Owner promotions and demotions take effect dynamically across all of
     their active PATs without token reissuance.
   - Admin-created tokens for other users are strictly capped to the target
     user's effective role.

2. **Owner Lifecycle Enforcement**:
   - Inactive (`!user.IsActive`) or deleted owner accounts are rejected at
     token validation with `ErrOwnerInactive` / `ErrUserNotFound` (HTTP 401
     Unauthorized).
   - Inactive users cannot create new tokens (`ErrOwnerInactive`).

3. **Mandatory Explicit Scopes (Removal of Wildcard Defaults)**:
   - `TokenService.CreateToken()` validates requested scopes via
     `ValidateScopes()`. Omitted or empty scopes return `ErrScopesRequired`.
   - Unrecognized scope strings fail closed with `ErrInvalidScope`.
   - `cruciblectl token create` requires `--scopes` (flag default changed
     from `*` to `nil`).
   - Web UI modal requires explicit scope input and removes default `*` value.
   - API endpoint `POST /api/v1/tokens` returns `400 Bad Request` if `scopes`
     is omitted or empty.
   - Wildcard access is granted only when `*` is explicitly requested.

4. **Legacy Token Compatibility Strategy**:
   - Tokens with empty, null, or corrupted `scopes_json` in SQLite fail closed
     (`tok.Scopes = nil`), granting zero permissions (`403 Forbidden`).
   - `api_tokens` table schema default in `001_initial.sql` updated to `'[]'`.
   - Operators can audit affected tokens before deployment using:
     ```sql
     SELECT id, name, username, role, scopes_json, created_at
     FROM api_tokens
     WHERE scopes_json IS NULL
        OR scopes_json = ''
        OR scopes_json = '[]'
        OR username NOT IN (SELECT username FROM users WHERE is_active = 1);
     ```

## Remaining Phase 1 work

- Decouple SQLite adapter options from application config at composition root.
- Close the Phase 1 architecture stabilization gate after owner confirmation.
- Author audit ledger specification and milestone.

## Verification status

- `go test -race ./...` — passed across all packages.
- `golangci-lint run ./...` — clean.
- `make check` — verified tidy, lint, gosec, govulncheck, and test.
- Line length validation: strictly <= 79 columns across all modified files.
- ZERO autonomous git commits, tags, or pushes performed.

## Working rules

- Read `AGENTS.md`, `CONTRIBUTING.md`, `CONTEXT.md`, `PLAN.md`, and
  `RECOMMENDATIONS.md` before making changes; inspect the current branch and
  worktree rather than trusting this handoff blindly.
- Add regression coverage for behavior changes and report the exact checks
  performed.
- Do not commit, tag, push, or merge without the explicit approval required by
  `AGENTS.md`. Do not add AI attribution trailers.
- Keep recommendations distinct from approved implementation scope and update
  this handoff after meaningful changes.

## Next steps

1. Request user review and verification by the peer model.
2. Present exact proposed commit message and wait for explicit confirmation.
