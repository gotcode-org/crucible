# Crucible — AI Development Handoff

> **Purpose:** A concise, verified handoff between development sessions and AI assistants.
> **Last updated:** 2026-10-30
> **Repository inspected:** `gotcode-org/crucible` GitHub mirror
> **Branch:** `docs/ai-workflow-guidance`
> **HEAD at handoff creation:** `bc91d14096fdad792c6cecde3ced0c5696d5eb4f`
> **Important:** This is a mirror-side documentation branch. The primary internal Git
> repository and the user's local working tree were not accessible during this update.

## Current local planning update — 2026-10-30

This entry supersedes the older checkout-state claims below for the current
workspace only. The current checkout is on branch `development` at HEAD
`832bad8` with existing user changes present. Its primary-versus-mirror status
was not verified during this planning task.

- **Owner-authorized objective:** Add the architecture review findings to the
  current Phase 1 milestone and TODO before more feature work proceeds.
- **Changed:** `PLAN.md` now contains Milestone 1-08a, an architecture
  stabilization and operational-contract gate before remaining Phase 1 feature
  milestones. `TODO.md` tracks its revalidation, implementation, contract,
  SLO, verification, and exit-gate work.
- **Roadmap alignment:** Phase 1 Milestone 1-08a is limited to confirmed
  findings in the existing codebase. Audit implementation remains in the
  already-planned Phase 1 Milestone 1-09. Job/attempt and artifact contracts
  are tracked before runner implementation; worker protocol contracts are
  tracked before Phase 2 implementation; reliability objectives are tracked
  for Phase 5. Numeric SLO targets await workload baselines. Phase 2 validates
  the stabilized storage boundary against worker needs and does not mandate a
  premature SQL dialect framework.
- **Revalidation completed:** `RECOMENDATIONS.md` now records source/test
  inspection dispositions against HEAD `832bad8`. Confirmed findings include
  SQL leakage, feature-local SQLite adapters, HTTP fallback wiring, implicit
  user creation during token persistence, first-admin race/partial-write risk,
  migration race risk, and SQLite configuration coupling. Job/attempt, worker,
  artifact, audit, and SLO items are planned or incomplete rather than verified
  defects. The recommendation's claim that SLO work was already in the
  baseline roadmap was incorrect.
- **Owner clarification:** Unix-socket access intentionally grants a local
  administrator principal to admins with authorized local/SSH access. This
  supersedes the earlier context wording that said socket access must not
  bypass remote authentication. `CONTEXT.md`, `PLAN.md`, and `docs/api.md` now
  record the clarified trust boundary; this behavior is not a defect.
- **Not done:** No application code or tests were changed or run. This was a
  static source/test inspection; no test, build, lint, security scan, or runtime
  check was executed. Dispositions do not authorize fixes beyond the approved
  stabilization scope.
- **Documentation check:** `git diff --check -- PLAN.md TODO.md` completed
  without whitespace errors. No application tests, build, lint, or security
  checks were run because this change only updates planning documentation.
- **Next step:** Work through the confirmed Phase 1 stabilization items before
  starting further Phase 1 feature milestones. Resolve the open exactly-once
  versus at-least-once design question before distributed coordination work.

## 1. Current state

### Verified in this documentation branch

- Branch `docs/ai-workflow-guidance` was created from `docs/project-context`.
- Root `AGENTS.md` was extended with AI collaboration, scope-control,
  recommendation-validation, handoff-maintenance, and end-of-task rules.
- This file was created to provide a repeatable session handoff.
- No application source code was changed as part of this documentation update.
- No commit, tag, or push was performed by an AI agent in the user's primary
  repository. The GitHub connector created the documentation branch and its
  file-history commits on the mirror only.

### Not verified

- Current state of the primary internal Git repository.
- Whether the mirror's documentation branch is current with the primary repo.
- Current local branch, working-tree changes, remotes, and HEAD on the user's
  Mac.
- Whether the source-level findings in `recommendations.md` remain valid at
  the current production HEAD.
- Current build, test, lint, security-scan, or runtime status.

Do not treat the historical baseline recorded in `CONTEXT.md` as current
without checking the primary repository.

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
`recommendations.md` for the architecture review.

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

## 4. Architecture review recommendations — status not revalidated

The previous read-only review identified areas to investigate, not automatically
approved implementation tasks:

1. Remove raw `*sql.DB` / SQL transaction leakage from application-facing
   storage boundaries.
2. Keep concrete persistence adapters behind feature-owned ports and compose
   them at the daemon composition root.
3. Avoid infrastructure construction or fallback dependency creation inside
   inbound HTTP adapters.
4. Define transaction boundaries and database-independent transaction
   semantics where required.
5. Remove implicit user creation as a side effect of token persistence.
6. Make first-time setup atomic across independent service instances/processes.
7. Review SQLite migration concurrency during simultaneous startup.
8. Decouple SQLite adapter options from application configuration.
9. Define the shared management API contract for the web UI and
   `cruciblectl`, including remote and Unix-socket transports.
10. Stabilize persistence boundaries before adding PostgreSQL/MySQL adapters.

The review also contains broader product/runtime recommendations covering job
and attempt lifecycle, manager/worker contracts, authorization, logs/reports,
API semantics, SLOs, audit-ledger quality, distributed failure handling, and
architectural contract testing.

**Status:** All findings above require revalidation against the current primary
repository before implementation. Priority labels in `recommendations.md` do
not grant implementation approval.

## 5. Current active objective

**Active objective:** Transfer the reviewed AI workflow guidance from the
GitHub mirror branch into the user's primary internal repository, then use
Codex in VS Code against the primary local checkout.

This is a documentation/setup objective, not authorization to implement
application changes.

## 6. Changes in this handoff update

| File | Change |
|---|---|
| `AGENTS.md` | Added AI collaboration rules: read-before-acting, architecture and decision discipline, recommendation validation, required handoff maintenance, and end-of-task reporting. |
| `docs/AI-HANDOFF.md` | Added this initial handoff, explicitly separating verified mirror-branch facts from unknown primary-repository state. |

No application code changes were made as part of this update.

## 7. Verification

- Confirmed the branch was created from `docs/project-context`.
- Confirmed the updated `AGENTS.md` was written to this branch.
- Confirmed the handoff file was created on this branch.
- No application tests, builds, linters, security scans, or runtime checks were
  run; this was a documentation-only change through the GitHub mirror.
- Primary repository state remains unverified.

## 8. Decisions and constraints to preserve

- `CONTEXT.md` remains the stable project/product context document.
- `recommendations.md` remains review advice, not an approved task list.
- `docs/AI-HANDOFF.md` is the concise, evolving implementation/session status.
- The owner must approve architectural changes and implementation scope.
- The primary internal Git repository is authoritative; GitHub is a mirror.
- The handoff must never imply that mirror state proves local or primary state.

## 9. Open questions / next steps

1. Copy or selectively merge the updated `AGENTS.md` and
   `docs/AI-HANDOFF.md` into the primary internal repository on a separate
   documentation branch.
2. On the Mac, have Codex inspect the primary checkout read-only first:
   applicable instructions, current branch/HEAD/status/remotes, project docs,
   and actual architecture.
3. Compare the primary checkout's `AGENTS.md` with this branch's version.
   Preserve any primary-only instructions and resolve conflicts explicitly;
   do not blindly replace the primary file.
4. Populate or refresh this handoff from verified primary-repository state.
5. Revalidate the architecture recommendations against the actual current
   source before selecting the first implementation task.
6. Confirm whether SLO and audit-ledger roadmap items have since been added or
   changed; the prior review context said these were already planned.

**Next agreed task:** Complete the read-only Codex inspection of the primary
local repository and report its findings. Do not begin implementation until
the owner approves a specific scope.
