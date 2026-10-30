# Crucible — AI Development Handoff

> **Purpose:** A concise, verified handoff between development sessions and AI assistants.
> **Last updated:** 2026-10-30 14:02 EDT
> **Repository inspected:** `/home/jovens/code/forks/crucible` local checkout
> **Branch:** `development`
> **HEAD:** `2739c81683db140900aac247c19c9c14ad6644da`
> **Important:** Current source changes are uncommitted. The branch has a prior signed tag; it does not include the follow-up working-tree changes.

## Current checkout update — 2026-10-30 14:02 EDT

This update is authoritative for the local checkout and supersedes older
mirror-side state notes below.

- **Repository:** `/home/jovens/code/forks/crucible`, the local Crucible
  checkout; remotes are `origin` (developer fork) and `upstream` (mainline).
- **Branch and base:** `development`, HEAD `2739c81683db140900aac247c19c9c14ad6644da`, one commit ahead of `upstream/main`.
- **Existing published tag:** `pr/storage-adapter-boundaries-for-v0.1.0` points to HEAD before this follow-up. It does not include the current working-tree changes.
- **Approved objective:** Complete the remaining storage-boundary scope: remove public raw SQL access, use concrete SQLite adapters at composition, reject PAT creation for unknown users, and make server dependencies explicit.
- **Implemented in the working tree:** removed `SQLiteAdapter.DB()` and exported `WithinTransaction`; constructors now accept `*SQLiteAdapter`; token creation checks a feature-owned user lookup and token persistence no longer creates user rows; `NewServer` takes named dependencies and does not construct fallback services; file-backed SQLite connections receive foreign-key and busy-timeout pragmas individually.
- **Tests and docs:** added storage API and pooled-connection checks plus a missing-user token invariant test; updated server fixtures, TODO completion states, recommendation dispositions, and documentation references to the corrected `RECOMMENDATIONS.md` filename.
- **Verification:** full `make` passed, including lint, gosec, govulncheck, `CGO_ENABLED=0 go test -v ./...`, and builds. `git diff --check` passed.
- **Working-tree state:** follow-up code and documentation changes are uncommitted. The existing `AGENTS.md` filename-reference change and recommendations-file rename are preserved.
- **Next:** review the working diff and prepare a new commit proposal. Do not move the existing signed PR tag without separate explicit approval.

## 1. Current state

The local `development` branch remains based on `upstream/main` and contains the
previous committed adapter-boundary change plus its pushed signed tag. The
current follow-up is in the working tree only. See the current checkout update
above for exact state and verification; the mirror-side notes later in this
file are historical context, not current repository state.

## 2. Project identity and confirmed direction

The project context document records the following owner-confirmed direction:

- Crucible is a Go modular monolith using Hexagonal Architecture
  (Ports & Adapters) and CQRS.
- `crucibled` is the server/daemon; `cruciblectl` is a separate CLI client.
- The CLI and web UI use the same daemon management API/application-facing
  capabilities. They must not duplicate business logic.
- The CLI is intended to support remote network access and local Unix-socket
  access.
- SQLite is the initial database. PostgreSQL and MySQL are future adapter
  targets, not permission to implement them prematurely.
- Distributed workers are part of the broader intended architecture; preserve
  the standalone-first delivery approach.
- Product intent must not be confused with proof that a feature is implemented.

See root `CONTEXT.md` for the fuller background and
`RECOMMENDATIONS.md` for the architecture review.

## 3. Important working rules

Read root and nested `AGENTS.md` files and `CONTRIBUTING.md` before work.
The root guidance includes strict rules that must be preserved:

- Never autonomously run `git commit`, `git tag`, or `git push`.
- Obtain explicit owner approval for each such action.
- Follow the repository's DCO and signed-tag workflow when the owner authorizes
  those operations.
- Do not add AI attribution trailers.
- Do not introduce dependencies without approval.
- Do not turn recommendations into implementation requirements without
  approval.
- Inspect current code and tests before relying on historical review findings.
- Keep changes focused, preserve user changes, and report exact verification.
- Update this handoff after meaningful work and important decisions.

The expanded AI collaboration rules are in root `AGENTS.md`.

## 4. Architecture review recommendations — current disposition

The source-level review findings were revalidated against the local checkout.
The following are resolved in the working tree and verified by the full `make`
run:

1. Remove raw `*sql.DB` and `*sql.Tx` access from the exported storage API.
2. Keep SQLite auth/token adapters in `internal/storage` and inject them at
   composition.
3. Remove HTTP adapter fallback construction and provide named dependencies.
4. Reject token creation for unknown users without persisting either a user or
   a token row.

Other recommendations remain open or planned: cross-process setup atomicity,
concurrent migration startup, configuration decoupling, API request limits,
and future execution/cluster/audit contracts. See `RECOMMENDATIONS.md` and
`TODO.md` for current status. These remaining items are outside the approved
scope of this follow-up.

## 5. Current active objective

**Active objective:** Finish the approved storage-boundary follow-up, review
its working-tree diff, and prepare a commit proposal. No commit or tag update is
authorized by the current scope approval.

## 6. Changes in this handoff update

| File area | Change |
|---|---|
| `internal/storage` | Remove public SQL accessors, accept `*SQLiteAdapter` in SQLite store constructors, and initialize connection-local SQLite pragmas via the DSN. |
| `internal/tokens` | Add a narrow `UserLookup` port; reject token creation when the user is absent; remove placeholder-user insertion. |
| `internal/server` and `cmd/crucibled` | Replace positional/variadic server wiring with named dependencies; construct query and token services at the daemon composition root. |
| Tests | Add missing-user, exported-boundary, and pooled-connection pragma checks; migrate fixtures to concrete adapters. |
| Planning/docs | Check off the completed TODO items, update recommendation dispositions, and correct references to `RECOMMENDATIONS.md`. |

## 7. Verification

- `make` passed on 2026-10-30: module tidy/download, golangci-lint, gosec,
  govulncheck, `CGO_ENABLED=0 go test -v ./...`, and both binary builds.
- Affected package tests passed: `internal/auth`, `internal/tokens`,
  `internal/storage`, and `internal/server`.
- `git diff --check` passed.
- No commit, tag, or push was performed for the follow-up changes.

## 8. Decisions and constraints to preserve

- `CONTEXT.md` remains the stable project/product context document.
- `RECOMMENDATIONS.md` remains review advice, not an approved task list.
- `docs/AI-HANDOFF.md` is the concise, evolving implementation/session status.
- The owner must approve architectural changes and implementation scope.
- The primary internal Git repository is authoritative; GitHub is a mirror.
- The handoff must never imply that mirror state proves local or primary state.

## 9. Open questions / next steps

1. Review the complete working-tree diff, including the pending `AGENTS.md`
   filename-reference change and the recommendations file rename.
2. Propose a Conventional Commit message with DCO signoff and wait for owner
   approval before committing.
3. The existing pushed PR tag points to the prior commit; decide separately
   whether the follow-up should use a new PR tag or another approved PR update.

**Next agreed task:** Review this completed scope and prepare the commit
proposal. Do not commit or move the published tag without explicit approval.
