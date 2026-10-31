# Crucible — AI Development Handoff

> **Purpose:** Verified local state and concise guidance for the next
> development session.  
> **Last updated:** 2026-10-30  
> **Repository:** `/home/jovens/code/forks/crucible`  
> **Branch:** `development`  
> **Base commit:** `d3aad993077523a9967445ecd73c5aa962b41703`  
> (`upstream/main`)  
> **Working tree:** Uncommitted security hardening changes (addressing
> `FOLLOWUP.md`).

## Current checkout

- `origin/main` and `upstream/main` are at `d3aad99` following the signed
  squash merge of `pr/pat-authorization-hardening-for-v0.1.0`.
- The active branch is `development`, tracking `upstream/main`.
- Security follow-up items from `FOLLOWUP.md` have been implemented and
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

## Hardened PAT authorization changes (FOLLOWUP.md resolution)

Following peer model review in `FOLLOWUP.md`, the following authorization
boundaries have been hardened:

1. **PAT Configured Role Restriction Preservation**:
   - `TokenService.ValidateToken()` no longer overwrites `tok.Role`. It
     retains the stored configured role and populates `tok.OwnerRole` with
     `user.EffectiveRole()`.
   - `auth.Session` and `auth.Principal` now track both `Role` (owner ceiling)
     and `TokenRole` (configured PAT restriction).
   - In `auth.Authorize()`, permissions are derived via true action-set
     intersection across all transports:
     `Effective = Owner Role Perms ∩ PAT Configured Role Perms ∩ PAT Scopes`.
   - Replaced linear `auth.RoleRank` comparisons with action-set subset helper
     `auth.RolePermitsRole(granter, grantee Role) bool`.

2. **Ceiling and Boundary Dynamics**:
   - An administrator creating a viewer-restricted PAT retains viewer bounds;
     it is never elevated to admin upon validation.
   - Owner demotions immediately restrict active PATs dynamically.
   - Owner promotions elevate the ceiling but never widen an existing PAT
     beyond its configured role or explicit scopes.
   - Invalid or empty configured token roles fail closed (`ErrForbidden`).

3. **Legacy Wildcard PAT Inventory & Non-Destructive Strategy**:
   - Investigated git archaeology (`0b367d4`, `adfa253`, `ee59cad`, `d3aad99`).
   - SQLite table `api_tokens` stores no creation version or explicit flag,
     and `internal/audit` is not yet implemented (Milestone 1-09).
   - Database records cannot distinguish whether `["*"]` was typed explicitly
     or assigned by historical CLI/Web defaults.
   - As directed in `FOLLOWUP.md`, no automatic destructive migration was run.
   - Under the three-way intersection model, legacy wildcard PATs are bounded
     by `Owner Role ∩ Configured Role`, preventing privilege escalation.
   - Documented safe operator audit query without secret/hash exposure:
     ```sql
     SELECT id, name, username, role, scopes_json, created_at, last_used_at
     FROM api_tokens
     WHERE revoked = 0
       AND (scopes_json = '["*"]' OR json_extract(scopes_json, '$[0]') = '*')
     ORDER BY created_at DESC;
     ```

4. **Regression Tests**:
   - Added table-driven tests in `internal/auth/principal_test.go`
     verifying role intersections, demotion/promotion ceilings, and invalid
     role handling.
   - Added unit tests in `internal/tokens/tokens_test.go` verifying configured
     role preservation, owner demotion, owner promotion, and sub-role capping.
   - Added HTTP integration tests in `internal/server/server_test.go` verifying
     that viewer-restricted and operator-restricted tokens enforce expected
     allow/deny outcomes across both TCP HTTP and UNIX domain sockets.

## Verification status

- `go test -race ./...` — passed across all packages.
- `golangci-lint run ./...` — clean (zero issues).
- `make check` (`tidy`, `lint`, `sec`, `vuln`, `test`) — fully verified.
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

1. Present exact diff, verification results, and proposed commit message to
   the user for review and confirmation.
2. Await explicit approval before committing or pushing.
