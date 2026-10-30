# Crucible — AI Development Handoff

> **Purpose:** Verified local state and concise guidance for the next development session.
> **Last updated:** 2026-10-30
> **Repository:** `/home/jovens/code/forks/crucible`
> **Branch:** `pr/phase1-stabilization`
> **HEAD:** Single DCO-signed PR summary commit based on `origin/main` at
> `854b304`; see `git log -1` for its full hash.
> **Working tree:** Clean after the PR summary commit.

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
- **Review status:** Correct this handoff and `RECOMMENDATIONS.md` to distinguish
  the current branch from its earlier review baselines. Correct the schema
  comment that claimed leases guarantee exactly-once job execution.
- **Milestone:** Add pooled SQLite pragma verification to the existing Phase 1
  architecture-stabilization scope. Setup atomicity was already tracked.

## Remaining Phase 1 work

Migration startup is serialized using SQLite's database-level `BEGIN IMMEDIATE`
lock with the applied-version check inside the lock. Bounded JSON decoding is
implemented for every current JSON handler, using a 1 MiB limit and 413 for
oversized requests. Regression tests cover independent SQLite adapters,
rollback, decoder limits, and authenticated token/authentication endpoints.

PAT scopes remain metadata and are not enforced. `docs/api.md` records current
behavior and a proposed role/scope/transport contract, explicitly pending owner
decisions about scope names, endpoint mapping, validation, and principal
representation. Do not implement scope enforcement until those decisions are
approved. SQLite configuration decoupling, the complete API contract, and the
Phase 1 stabilization gate remain open in `TODO.md`. The audit ledger remains
an existing later Phase 1 milestone. Distributed worker delivery semantics
remain design work; the plan and roadmap reject an unqualified exactly-once
guarantee.

## Verification status

- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp go test ./internal/storage ./internal/auth` — passed.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp go test ./internal/server ./cmd/crucibled` — passed with permission to bind local test sockets. The sandboxed run could not open sockets.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp make check` — passed after the
  migration, request-body, and test changes. This runs dependency
  verification, gosec, govulncheck, CGO-disabled tests, and race tests.
- `/home/jovens/go/bin/golangci-lint run ./...` — passed.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp make build` — passed for both
  binaries with `CGO_ENABLED=0`.
- `CGO_ENABLED=0 GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp go build ./...` —
  passed after the current code changes.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp go test -race ./...` — passed
  after the current code and test changes.
- `XDG_CACHE_HOME=/tmp/crucible-xdg-cache GOCACHE=/tmp/crucible-gocache
  GOTMPDIR=/tmp /home/jovens/go/bin/golangci-lint run ./...` — passed.
- `git diff --check` — passed after the current documentation edits.
- `git verify-tag pr/sqlite-startup-safety-for-v0.1.0` — passed; the signed
  tag targets summary commit `c57b9f1`.
- The summary branch and updated signed tag were pushed to the fork. The
  hosting-service PR state remains unverified.

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

1. Finish and verify the current stabilization changes; update this handoff
   with final checks and worktree status.
2. Obtain an owner decision on the proposed authorization contract before
   implementing PAT-scope enforcement.
3. Verify PR status when the private Git remote is accessible. Do not commit,
   tag, or push without the explicit approval required by `AGENTS.md`.
