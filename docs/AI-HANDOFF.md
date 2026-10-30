# Crucible — AI Development Handoff

> **Purpose:** Verified local state and concise guidance for the next
> development session.
> **Last updated:** 2026-10-30
> **Repository:** `/home/jovens/code/forks/crucible`
> **Branch:** `pr/phase1-stabilization`
> **HEAD:** Shared authorization and PAT scope enforcement commit based on
> `origin/main` at `854b304`; see `git log -1` for its full hash.
> **Working tree:** Clean after authorization and scope enforcement commit.

## Current checkout

- `origin/main` and `upstream/main` are at `854b304`; this is the base of the
  single-commit PR summary.
- The original development branch `repair/storage-boundary-hardening` remains
  local. Its changes were pushed to `origin/repair/sqlite-startup-safety` at
  `4f18a57` before preparing the summary commit.
- The summary branch `pr/phase1-stabilization` and signed tag
  `pr/sqlite-startup-safety-for-v0.1.0` identify the consolidated changes.
- The hosting service PR state has not been verified. Do not infer PR status
  from branch or tag refs.

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

## Follow-up changes in the local commit

- **SQLite connection settings:** Apply `foreign_keys`, `busy_timeout`,
  `synchronous`, `temp_store`, and `cache_size` through the driver DSN so each
  pooled connection receives them. Use a unique shared in-memory SQLite URI so
  pooled connections see the same in-memory database. Keep WAL initialization
  separate and verify it as a database-level setting.
- **First administrator setup:** Add an auth-owned setup persistence port.
  SQLite conditionally inserts the first user and inserts its initial session
  in one transaction. The database, rather than only the service mutex,
  enforces the single-setup invariant across separate adapters. Add concurrency
  and rollback regression tests.
- **Review status:** Correct this handoff and `RECOMMENDATIONS.md` to
  distinguish the current branch from its earlier review baselines. Correct
  the schema comment that claimed leases guarantee exactly-once job execution.
- **Milestone:** Add pooled SQLite pragma verification to the existing Phase 1
  architecture-stabilization scope. Setup atomicity was already tracked.

## Remaining Phase 1 work

Migration startup is serialized using SQLite's database-level `BEGIN IMMEDIATE`
lock with the applied-version check inside the lock. Bounded JSON decoding is
implemented for every current JSON handler, using a 1 MiB limit and 413 for
oversized requests.

PAT scope enforcement and transport authorization parity have been
implemented under the intersection model:
`Effective Permissions = User Role Permissions ∩ PAT Scope Permissions`.
Interactive web sessions and local Unix-socket connections inherit full role
permissions (with the local Unix socket representing the trusted local-admin
identity). Remote TCP PAT requests are strictly clipped by their granted
scopes. Handlers across HTTP, API, and Web settings enforce discrete action
permissions, and `cruciblectl token create` provides the `--scopes` flag.
Regression tests cover scoped tokens, operator token clipping, invalid scopes,
and Unix socket parity.

SQLite configuration decoupling and Phase 1 stabilization gate remain open
in `TODO.md`. The audit ledger remains an existing later Phase 1 milestone.
Distributed worker delivery semantics remain design work.

## Verification status

- `go test -race ./...` — passed across all packages.
- `go test -v ./internal/auth -run "TestAuthorize"` — passed (15 table cases).
- `go test -v -race ./internal/server`
  `-run "TestAuthAPI_TokenScopeEnforcement"` — passed.
- `go test -v ./cmd/cruciblectl` — passed.
- No commit, tag update, or push has been made for the current uncommitted
  changes.

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

1. Have the peer model verify the authorization and scope enforcement
   changes.
2. Verify PR status when the private Git remote is accessible. Do not commit,
   tag, or push without the explicit approval required by `AGENTS.md`.
