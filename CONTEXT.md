# Crucible — Project Context for AI-Assisted Development

> Handoff note for future development sessions. Read this file before making code changes.
> Last reviewed: 2026-10-30
> Repository: https://github.com/gotcode-org/crucible
> Production baseline reviewed: `832bad8b9c82e5f2768143e2cb4f32247ab6dd1a` (reported as the 2026-10-29 main-branch head during the review).

## Project identity and working agreement

Crucible is being developed as a Go **monolith** using **Hexagonal Architecture (Ports & Adapters)** and **CQRS**. The production repository is the source of truth for implementation work. There is a separate prototype repository, `gotcode-org/prototype-crucible`; do not treat it as the production codebase or copy its design blindly.

The intended direction discussed with the project owner is:

- A multi-binary product: `crucibled` is the server/daemon, and `cruciblectl` is a separate CLI client for managing it. The CLI connects remotely over the network or locally through a Unix socket.
- The web UI and `cruciblectl` use the same management API operations and application-facing contracts. The CLI is a client, not a second implementation of business logic.
- The management API is intended to grow to cover schedules, job submission and control, and attachment to live log streams.
- A modular monolith, not a collection of microservices.
- Hexagonal boundaries: application/domain code owns ports; infrastructure supplies adapters; the composition root wires concrete implementations.
- CQRS separation between commands and queries where it adds clarity.
- SQLite as the initial database, with PostgreSQL and MySQL adapters as future targets.
- Distributed workers are part of the intended architecture.
- Network isolation and a management UI are also part of the broader product direction.

Treat the bullets above as product/architecture intent, not proof that every capability is implemented today.

## Repository orientation

The review examined these areas:

- `cmd/crucibled/start.go`: daemon startup and dependency composition.
- `internal/storage/`: storage engine port, SQLite adapter, migrations, and storage command/query services.
- `internal/auth/`: authentication application services, ports, and SQLite persistence adapter.
- `internal/tokens/`: token service, ports, and SQLite persistence adapter.
- `internal/server/adapter_http.go`: HTTP server construction and inbound adapter.

The daemon startup path currently initializes SQLite, runs migrations, constructs auth/token persistence and services, and starts HTTP and UNIX-socket listeners. The separate `cruciblectl` binary is intended to call the same management API used by the web UI, choosing either a remote network endpoint or the daemon's local Unix socket.

The storage package includes an Engine port and command/query services. The command/query services appear focused on storage administration, migrations, and diagnostics; they should not automatically become a generic repository or CRUD layer.

## Architectural decisions to preserve

1. Keep the application organized around capabilities/features such as auth and tokens.
2. Keep persistence interfaces (outbound ports) owned by the feature/application packages that need them.
3. Put concrete database implementations in infrastructure.
4. Keep concrete adapter construction in the composition root (currently daemon startup), not inside inbound adapters or application services.
5. Keep CQRS command/query responsibilities distinct where useful; avoid ceremony for its own sake.
6. Keep SQLite and embedded, versioned migrations as the initial implementation unless a deliberate decision changes that.
7. Avoid introducing an ORM, a generic repository framework, or a homemade SQL-dialect abstraction without a demonstrated need.
8. Prefer small, incremental refactors with tests over broad rewrites.

## Known architecture observations from the read-only review

These are findings against the reviewed baseline, not guarantees about the current branch after later changes:

- `internal/storage/ports.go` exposes `DB() *sql.DB` through the Engine abstraction and uses `*sql.Tx` in the transaction callback. This leaks SQL implementation details across the port.
- Auth and tokens each have feature-owned persistence ports, but their SQLite implementations currently live in the feature packages (`internal/auth/adapter_sqlite.go` and `internal/tokens/adapter_sqlite.go`).
- `cmd/crucibled/start.go` constructs the SQLite engine and then obtains its raw database handle to construct the auth and token stores.
- `internal/server/adapter_http.go` accepts an optional token service and can construct a SQLite token store/service as a fallback. An inbound HTTP adapter should not select or construct infrastructure.
- `storage.NewSQLiteAdapter` accepts application config directly, coupling the adapter to `internal/config`.
- Token persistence contains an `ensureUser` insert path that can create a user row as a side effect of token creation. This risks creating an incomplete user record instead of enforcing the user-exists invariant.
- Auth's in-process setup mutex does not by itself coordinate multiple daemon processes sharing a database.
- SQLite migration startup should be reviewed for concurrent-process safety if multiple roles/processes may initialize the same database.

See [RECOMMENDATIONS.md](RECOMMENDATIONS.md) for the prioritized remediation plan and rationale.

## Multi-binary model and management API

The project owner has clarified the intended executable and client model:

- `crucibled` is the server/daemon.
- `cruciblectl` is a separate CLI client used to manage the daemon remotely over the network or locally through a Unix socket.
- The web UI and `cruciblectl` use the same management API operations. Business rules live in the daemon's application layer, not in either client.
- Local Unix-socket access is an intentional trust boundary: admins with authorized local/SSH access may manage the daemon without a PAT or browser login. Restrictive socket ownership and permissions control access; remote TCP clients authenticate normally. This owner clarification supersedes earlier wording that treated the socket as no different from remote authorization.
- The API is intended to support management of schedules, running and controlling jobs, attaching to live log streams, and additional operator workflows over time.
- The CLI is a distinct binary, not a manager/worker/combined mode of the daemon. Worker processes and distributed execution are separate runtime/architecture concerns and should be documented independently from the CLI's role.

The API operations and business rules must be shared across web UI, remote CLI, and Unix-socket CLI access. Authentication maps each transport to an explicit principal: remote callers use sessions or PATs, while authorized local socket callers receive the documented local-admin principal based on protected socket access. Do not interpret the socket's local trust as a separate business-logic path.

## How to work in this repository

- Start with the production repo, not the prototype.
- Before editing, inspect the current branch, working tree, recent commits, relevant interfaces, and tests. This handoff can become stale.
- Do not assume the reviewed commit is still HEAD.
- Do not push to the default branch directly. Use a focused feature/refactor branch and propose changes for review.
- Do not make unrelated cleanup changes while working on an architectural phase.
- Add or update tests with each behavior or boundary change.
- Run formatting, focused package tests, and the full test suite when practical; report exactly what was and was not run.
- Never claim tests passed unless they were actually executed.
- Do not add PostgreSQL/MySQL support until the application boundaries and transaction semantics are clear enough to support them cleanly.
- Preserve backward compatibility only where the project has an explicit compatibility requirement; otherwise discuss the impact before changing public CLI/API contracts.

## Suggested first development task

Begin with the first recommendation in [RECOMMENDATIONS.md](RECOMMENDATIONS.md): remove raw `*sql.DB` leakage from application-facing storage ports and callers. First inspect the current implementation and tests, then design a database-independent transaction boundary. Keep the change small and prove that auth/token application code does not need to know about `database/sql` or SQLite. Do not combine this with a full persistence-directory reorganization unless tests or code structure make that necessary.

## Session handoff checklist

At the beginning of a new session:

1. Read this file and `RECOMMENDATIONS.md`.
2. Check current branch and HEAD; compare with the baseline SHA above.
3. Inspect the exact files involved before relying on historical findings.
4. Confirm the immediate task and whether changes should remain local, be committed to a branch, or be opened as a PR.
5. Keep the owner informed before any destructive, broad, or default-branch operation.
