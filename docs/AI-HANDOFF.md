# Crucible — AI Development Handoff

> **Purpose:** Verified local state and concise guidance for the next
> development session.  
> **Last updated:** 2026-10-31  
> **Repository:** `/home/jovens/code/forks/crucible`  
> **Branch:** `development`  
> **Base commit:** `0c606a255dd06fe49cf84c83ca2da74bea21ad0a`  
> (`upstream/main`)  
> **Working tree:** Uncommitted security changes implementing fail-closed
> review transition for legacy wildcard PATs (addressing `FOLLOWUP.md`).

## Current checkout

- `origin/main` and `upstream/main` are at `0c606a2` following the signed
  squash merge of `pr/pat-restriction-preservation-for-v0.1.0`.
- The active branch is `development`, tracking `upstream/main`.
- Security follow-up items from `FOLLOWUP.md` (fail-closed transition for
  legacy wildcard PATs) have been implemented and verified locally in the
  working tree without committing, tagging, or pushing.

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

## Hardened Legacy PAT Wildcard Scope Transition (FOLLOWUP.md)

Following peer model review in `FOLLOWUP.md`, legacy wildcard PATs whose
explicit consent cannot be established are subject to a review-required,
fail-closed transition:

1. **Durable Review State via Migration**
   (`006_legacy_wildcard_pat_review.sql`):
   - Added `requires_review INTEGER NOT NULL DEFAULT 0`,
     `reviewed_at DATETIME`, `reviewed_by TEXT` to SQLite table `api_tokens`.
   - Migration flags existing unrevoked wildcard tokens
     (`scopes_json LIKE '%"*"%'` or
     `json_extract(scopes_json, '$[0]') = '*'`) where `reviewed_at IS NULL`.
   - Malformed JSON rows are safely handled via `json_valid()`.
   - Fully idempotent: re-running the migration never clears or re-flags
     previously reviewed tokens.

2. **Validation Fails Closed Before Telemetry or Authorization**:
   - `TokenService.ValidateToken()` immediately returns
     `ErrTokenRequiresReview` when `RequiresReview` is true.
   - Denied requests return `403 Forbidden` across HTTP and Unix sockets:
     `Forbidden: Personal access token requires review before use`.
   - Last-used telemetry (`last_used_at`, `last_used_ip`) is never touched on
     denied attempts.
   - Ordinary token listing or inspection never clears review status.

3. **Reconfirmation Paths & Provenance Recording**:
   - CQRS Command `ReviewTokenCommand` handled by `TokenService.ReviewToken()`.
   - Authorized callers: only the token owner or an admin may review a token;
     unauthorized callers receive `auth.ErrForbidden`.
   - REST endpoint: `POST /api/v1/tokens/review` (`{"id":"<token_id>"}`).
   - Web Console: `POST /settings/tokens/review` with UI badge `REVIEW REQ`
     and action buttons for confirmation or revocation.
   - CLI: `cruciblectl token review <id>` for operator workflows.
   - Records reviewer username and UTC timestamp upon reconfirmation.

4. **Operator Audit Tooling (Safe Metadata Only)**:
   - CLI: `cruciblectl token audit` and
     `cruciblectl token list --review-required`.
   - REST: `GET /api/v1/tokens?review_required=true` (and `all=true`).
   - Output displays safe metadata only: ID, name, prefix, username, role,
     created date, and review status. Never exposes raw token or token hash.

5. **Strict Scope Integrity**:
   - Tokens with empty, null, or invalid scopes fail closed
     (`ErrScopesRequired`, `ErrInvalidScope`).

6. **Ceiling Invariance**:
   - Reconfirmed wildcard PATs remain bounded by
     `Owner Role ∩ PAT Configured Role`, preventing privilege escalation.

## Verification status

- `go test -race ./...` — passed across all packages.
- `golangci-lint run ./...` — clean (zero issues).
- `make check` (`tidy`, `lint`, `sec`, `vuln`, `test` with `-race`) — clean.
- `make build` — built `crucibled` and `cruciblectl` with `CGO_ENABLED=0`.
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

## Next steps

1. Present exact diff, verification results, operational impact, and proposed
   commit message to the user for review and confirmation.
2. Await explicit approval before committing or pushing.
