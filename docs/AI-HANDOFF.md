# Crucible — AI Development Handoff

> **Purpose:** Verified local state and concise guidance for the next development session.
> **Last updated:** 2026-10-30
> **Repository:** `/home/jovens/code/forks/crucible`
> **Branch:** `repair/storage-boundary-hardening`
> **HEAD:** Local follow-up commit on top of `854b304cdac232363c6f556e6562cbc8cbf48871`.
> **Working tree:** Clean.

## Current checkout

- `upstream/main` and the fork's `origin/main` are at `854b304`. The fork was
  fast-forwarded before preparing the local follow-up commit.
- The local follow-up commit is not pushed. The existing tag and remote
  `repair/storage-boundary-hardening` branch still point to the earlier commit
  that upstream has already merged.
- This document reports only the checkout above. It does not claim to know the
  state of another private-server checkout or GitHub mirror.

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

`TODO.md` already tracks request-body limits, migration concurrency,
configuration decoupling, API behavior documentation, and the architecture
stabilization exit gate. Revalidate these against source before implementation.
The audit ledger remains an existing later Phase 1 milestone. Distributed
worker delivery semantics remain design work; the plan and roadmap reject an
unqualified exactly-once guarantee.

## Verification status

- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp go test ./internal/storage ./internal/auth` — passed.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp go test ./internal/server ./cmd/crucibled` — passed with permission to bind local test sockets. The sandboxed run could not open sockets.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp make check` — passed, including
  dependency verification, gosec, govulncheck, all tests with `CGO_ENABLED=0`,
  and the full race-enabled suite.
- `/home/jovens/go/bin/golangci-lint run ./...` — passed.
- `GOCACHE=/tmp/crucible-gocache GOTMPDIR=/tmp make build` — passed for both
  binaries with `CGO_ENABLED=0`.
- `git diff --check` — passed after the final documentation edits.
- No tag update or push has been made for the local follow-up commit.

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

1. Review the local commit and this handoff.
2. Wait for explicit owner approval before pushing the feature branch or
   replacing/updating a signed PR tag.
