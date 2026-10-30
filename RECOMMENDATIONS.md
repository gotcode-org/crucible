# Crucible — Architecture Review Recommendations

> Read-only review of production repository `gotcode-org/crucible`.
> Review baseline: `832bad8b9c82e5f2768143e2cb4f32247ab6dd1a` (reported as the 2026-10-29 main-branch head).
> Prepared: 2026-10-30
>
> These are recommendations, not a statement that changes have already been made. Re-check the current code before implementing; the repository may have advanced.

## Executive summary

The project has a sound starting direction: a Go modular monolith, feature-owned persistence ports, a composition root, CQRS separation, SQLite as the first adapter, and embedded versioned migrations. Preserve those decisions.

The most important architectural issue is that the storage abstraction currently exposes `database/sql` types, which undermines the intended hexagonal boundary and makes future database adapters harder. Address that first, incrementally. Then tighten dependency construction, enforce domain invariants in persistence, and make startup/migration behavior safe for multiple processes.

Do **not** respond to these issues with a broad rewrite, an ORM, or a generic SQL framework.

## Prioritized recommendations

### P0 — Remove SQL implementation leakage from storage ports

**Observed:** `internal/storage/ports.go` exposes `DB() *sql.DB` on the Engine abstraction and accepts `func(tx *sql.Tx) error` in its transaction abstraction.

**Why it matters:** Callers that can obtain `*sql.DB` or `*sql.Tx` are coupled to Go's SQL implementation and the current relational adapter. This weakens the port, makes alternate implementations awkward, and encourages application code to issue infrastructure-level SQL.

**Recommendation:**

- Remove `DB() *sql.DB` from the application-facing Engine contract.
- Replace the raw-`*sql.Tx` callback with a small, database-independent unit-of-work/transaction port, or narrower operation-specific ports if a general transaction abstraction is not actually needed.
- Keep SQL-specific transaction creation and commit/rollback behavior inside the SQLite adapter.
- Make the composition root pass feature adapters their required ports, not a raw database handle.
- Avoid exposing a universal query/exec interface as a disguised generic repository; define only the operations the application needs.

**Acceptance criteria:**

- Application/domain packages do not import `database/sql` solely to reach infrastructure.
- Auth/token services cannot access the raw SQLite connection.
- Transaction semantics are documented and tested, including rollback on failure.
- Existing behavior remains covered by tests.

### P1 — Keep persistence adapters in infrastructure; keep ports with their consumers

**Observed:** Auth and tokens own useful persistence interfaces in their `ports.go` files, but their SQLite implementations currently live alongside application code in `internal/auth/adapter_sqlite.go` and `internal/tokens/adapter_sqlite.go`.

**Recommendation:**

- Keep `DeviceStore`, `SessionStore`, `ChallengeStore`, `InviteStore`, `UserStore`, and token-store interfaces owned by their respective feature packages.
- Move concrete SQLite implementations into an infrastructure location, for example `internal/storage/sqlite/auth_store.go` and `internal/storage/sqlite/token_store.go`.
- Keep constructors and SQL schema details in the adapter package.
- Let the composition root import concrete infrastructure packages and inject implementations through feature-owned interfaces.

This is a boundary improvement, not a reason to reorganize every package at once. It can follow the port cleanup or be done as a separate mechanical change with tests.

### P1 — Make HTTP server dependencies explicit

**Observed:** `internal/server/adapter_http.go` accepts an optional variadic token service. If none is supplied, it constructs a SQLite token store/service using `engine.DB()`. It also creates a storage QueryService when one is not provided.

**Why it matters:** An inbound adapter should translate transport requests into application calls. It should not decide which database adapter to use or silently construct application services. Optional fallback wiring can also hide a missing dependency until runtime.

**Recommendation:**

- Require the token service and other required collaborators explicitly.
- Prefer a named dependency/config struct if the constructor has several dependencies.
- Keep all concrete adapter selection and service construction in the composition root.
- Retain defaults only for genuinely optional presentation/transport configuration, not core infrastructure.

**Acceptance criteria:** Constructing the HTTP server does not instantiate SQLite stores or choose persistence implementations.

### P1 — Enforce the user-exists invariant when creating tokens

**Observed:** The SQLite token adapter's `CreateToken()` path performs an `INSERT INTO users ... ON CONFLICT DO NOTHING` (`ensureUser`) to satisfy the token table's foreign key. This can create a partial user row as a side effect of token creation.

**Why it matters:** Token creation should not silently manufacture an incomplete user aggregate/record. The user identity lifecycle belongs to the appropriate user/auth workflow.

**Recommendation:**

- Remove the implicit user creation from token persistence.
- Ensure the application service validates that the referenced user exists through an appropriate port, or rely on a well-defined application workflow that guarantees it.
- Keep the database foreign key as the final integrity guard.
- Return a meaningful not-found/invariant error when the user does not exist.

**Acceptance criteria:** Attempting to create a token for an unknown user fails and leaves no user or token row behind.

### P1 — Make first-time setup atomic and safe across processes

**Observed:** `internal/auth/commands.go` uses an in-process `sync.Mutex` around initial setup. That coordinates callers using that one service instance only.

**Why it matters:** If multiple daemon processes or manager instances share the database, an in-memory mutex cannot enforce a global one-time setup invariant. Setup may also involve multiple persistence writes (for example, initial admin and session creation).

**Recommendation:**

- Define the exact invariant: e.g. only one initial administrator may be established for a fresh installation.
- Enforce it at the database/application transaction boundary, not only with a process-local mutex.
- Make the relevant writes atomic.
- Add concurrency tests using separate service instances/connections against the same database.
- Keep the mutex only if it still helps reduce local contention; do not treat it as the correctness mechanism.

### P1 — Review SQLite migration concurrency

**Observed:** The migration runner reads applied migration versions and then applies pending migrations in transactions. If multiple processes start concurrently against the same database, both may observe the same migration as pending.

**Recommendation:**

- Re-read/validate migration state while holding an appropriate database-level lock or serialize migration execution.
- Ensure the migration record and schema changes commit atomically.
- Handle duplicate/racing migration attempts deterministically.
- Add a test that starts migration concurrently from independent connections/process-like adapter instances.

Do this before relying on multiple manager/worker processes starting against the same SQLite database.

### P2 — Decouple SQLite adapter configuration from application config

**Observed:** `storage.NewSQLiteAdapter(ctx, cfg config.StorageConfig)` imports and accepts `internal/config` types, and the adapter uses an application-config default database path.

**Recommendation:**

- Define a small SQLite-specific options type in the infrastructure package (path, connection/pool settings, pragmas, etc. as actually needed).
- Translate application configuration into adapter options in the composition root.
- Keep environment/config parsing outside the adapter.
- Avoid speculative configuration knobs until required.

### P1 — Establish the multi-binary client/server contract

**Clarification from the project owner:** Crucible is intentionally a multi-binary product. `crucibled` is the server/daemon. `cruciblectl` is a separate CLI client used to manage that server remotely over the network or locally through a Unix socket. The CLI is intended to use the same API operations exposed to the web UI; it is not a second business-logic implementation and must not reach into daemon internals or persistence directly.

The web UI and CLI are therefore two inbound clients of a shared management API/application surface. This is compatible with the modular-monolith and hexagonal architecture: the daemon hosts the application and API, while the CLI is a separate executable that speaks the public management contract.

**Recommendation:**

- Treat `crucibled` and `cruciblectl` as distinct binaries with clearly separated responsibilities; do not describe manager/worker/combined as mutually exclusive modes of a single executable.
- Define and version a stable management API contract shared by the web UI and CLI. Keep business rules in application use cases, not duplicated in either client.
- Support remote network connections and local Unix-socket connections through the same API operations. Preserve the owner's intended local-admin trust boundary: restrictive socket ownership and mode grant local administration to authorized SSH/local administrators, while remote TCP requests use PATs or browser sessions. Make this transport-specific principal mapping explicit and testable.
- Design the API incrementally for schedule management, job submission/control, and attaching to live log streams, alongside other operator workflows.
- Define streaming behavior explicitly (authentication, authorization, reconnect/resume semantics, backpressure, cancellation, and bounded resource use) for log attachment and other long-lived operations.
- Define the principal, permission, token-scope, validation, and audit rules for each origin. The protected local socket intentionally maps authorized local administrators to a local-admin principal; remote CLI and web requests use their authenticated identities.
- Keep CLI configuration and UX concerns in `cruciblectl`; keep server composition and infrastructure selection in `crucibled`.
- Align README, PLAN, examples, deployment docs, and command help with this multi-binary model.

### P3 — Add PostgreSQL and MySQL adapters only after boundaries stabilize

**Recommendation:**

- First stabilize persistence ports, transaction boundaries, error mapping, and migration ownership.
- Implement PostgreSQL and MySQL as separate adapters with adapter-specific migrations and integration tests.
- Account for differences in parameter placeholders, locking/concurrency, data types, constraints, and transaction behavior.
- Do not build a generic SQL dialect engine prematurely.
- Use a shared behavioral contract/test suite where practical, with adapter-specific tests for database semantics.

## Suggested implementation sequence

1. **Storage port boundary:** remove `DB()` and raw `*sql.Tx` from application-facing interfaces; add tests.
2. **Auth adapter boundary:** move auth SQLite implementation behind infrastructure while preserving auth-owned ports.
3. **Token adapter boundary:** do the same for tokens and remove implicit user creation.
4. **HTTP composition:** make server dependencies explicit and remove infrastructure fallback construction.
5. **Transaction semantics:** introduce only the smallest database-independent transaction/unit-of-work abstraction required by real use cases.
6. **Setup invariant:** make first-time initialization atomic across service instances/processes.
7. **Migration safety:** serialize/guard migration execution and test concurrent startup.
8. **Config boundary:** decouple SQLite options from application config.
9. **Management API contract:** document the shared API used by the web UI and `cruciblectl`, including remote and Unix-socket transports; add contract tests.
10. **Documentation alignment:** update README, PLAN, examples, deployment docs, and CLI help to describe the multi-binary model.
11. **PostgreSQL adapter**, then **MySQL adapter**, each with integration and behavioral contract tests.

Steps 2–5 may need slight reordering depending on the transaction API discovered during step 1. Keep each PR focused and independently reviewable.

## Suggested package shape (illustrative, not a mandate)

```text
internal/
  auth/
    ports.go             # auth-owned outbound interfaces
    commands.go
    queries.go
    models.go
  tokens/
    ports.go             # token-owned outbound interfaces
    service.go
    models.go
  storage/
    ports.go             # database-independent storage contracts only
    commands.go          # storage administration use cases
    queries.go
    sqlite/
      engine.go
      auth_store.go
      token_store.go
      migrations.go
    postgres/
    mysql/
```

Do not create empty adapter packages before they are needed. The key rule is dependency direction, not the exact directory names.

## Good existing decisions to preserve

- Feature-owned outbound ports.
- A composition root for concrete dependency wiring.
- CQRS separation where it clarifies use cases.
- SQLite as the initial persistence adapter.
- Embedded, versioned migrations.
- A pure-Go SQLite driver.
- Feature-focused services rather than generic CRUD.
- Incremental refactoring rather than a rewrite.


## Broader product and runtime recommendations

The items below extend the review beyond the storage boundary. They are architectural review points and design recommendations, **not claims that the current implementation is defective**. Inspect the production code against each item before implementation. Preserve the roadmap's standalone-first delivery order: distributed execution is planned before v1.0.0, but should not force premature distributed abstractions into the standalone release.

### P0 — Define job and execution-attempt semantics

A job is the requested unit of work; an execution attempt is one concrete effort to run it. Keep those concepts distinct so retries, recovery, and history remain understandable.

- Define an explicit job lifecycle and permitted state transitions.
- Model each attempt independently, including start/end time, worker, exit status, cancellation reason, and log/report references.
- Specify retry policy, exponential backoff, maximum attempts, execution timeout, cancellation, and concurrency limits.
- Make state transitions atomic and reject invalid transitions.
- Define what happens when the daemon or worker exits during execution, including how abandoned work is detected and recovered.
- Make execution handlers idempotent where retries or redelivery can occur.

**Acceptance criteria:** Lifecycle transitions and retry behavior are documented; tests cover success, failure, cancellation, timeout, retry, daemon restart, and invalid transitions. Job history clearly distinguishes the overall job from its individual attempts.

### P0 — Specify manager/worker contracts before distributed implementation

The plan already describes remote workers, heartbeats, drain mode, isolation, and gRPC communication. Turn those roadmap concepts into explicit contracts before implementing the distributed path.

- Define worker registration, identity, authentication, capabilities, labels/pools, and authorization.
- Specify heartbeat cadence, missed-heartbeat thresholds, health states, reconnect behavior, and stale-worker cleanup.
- Define worker availability and maintenance transitions (active, drain, paused, down, and isolated) and which component owns each transition.
- Define dispatch acknowledgement, execution start, progress, completion, failure, cancellation, and log-stream messages.
- Specify graceful drain behavior and what happens to in-flight work when a worker disconnects.
- Keep transport behind ports; use the planned authenticated mTLS gRPC adapter for remote communication.
- Start from at-least-once delivery plus idempotent processing and durable state transitions. Do not promise exactly-once execution merely because a lease exists; document the failure model and prove the guarantees with fencing and recovery tests.

**Acceptance criteria:** Manager/worker protocol and state machines are documented; contract tests exercise reconnects, duplicate delivery, lost acknowledgements, worker crashes, drain, cancellation, and stale leases.

### P1 — Separate authentication, authorization, and worker identity

- Treat authentication as establishing who or what is making a request; authorization decides whether that principal may perform a specific action on a resource.
- Define roles and permissions centrally, with consistent enforcement across HTTP, CLI, local socket, and worker RPC entry points.
- Distinguish human users, service accounts, personal access tokens, worker credentials, and controller/peer identities.
- Define token/session expiry, revocation, disabled-user behavior, scope enforcement, and credential rotation.
- Ensure secrets are shown only when created, stored as hashes or encrypted material as appropriate, and never written to logs.
- Audit security-sensitive actions through the planned audit subsystem.

**Acceptance criteria:** A permission matrix covers each principal type and transport; authorization tests prove both allowed and denied actions at application boundaries.

### P1 — Treat execution history, logs, and reports as first-class data

The plan already calls for disk-streamed logs, live streaming, and HTML reports. Keep the data model and access rules explicit.

- Associate logs and reports with an execution attempt, not just a job.
- Define retention, cleanup, maximum sizes, disk-full behavior, and partial-log behavior after crashes.
- Keep live streaming bounded and non-blocking so slow viewers cannot stall execution.
- Apply the same public/private visibility and authorization rules to run metadata, logs, reports, downloads, and live streams.
- Record artifact metadata and references in persistence; avoid storing large log bodies in the relational database.

**Acceptance criteria:** Tests cover large output, slow/disconnected viewers, process termination, disk exhaustion, retention, and private artifact access.

### P1 — Define API and transport contracts consistently

- Keep HTTP handlers thin: parse/validate requests, invoke application use cases, and map results to transport responses.
- Define a consistent error envelope and stable error codes for validation, not-found, conflict, authentication, authorization, and internal failures.
- Version public APIs and document request/response schemas.
- Apply request IDs/correlation IDs, authentication and authorization middleware, input limits, and appropriate rate limits.
- Ensure CLI and HTTP invoke the same application use cases rather than duplicating business rules.

**Acceptance criteria:** API contract tests cover success and error responses, auth/authz, validation, and version compatibility.

### P1 — Make reliability objectives measurable

The user has confirmed that SLOs are already in the TODO list and plan and are next to be added. Treat this as planned work, not a newly discovered omission. When the SLO work is added, connect objectives to observable service behavior.

- Define service-level indicators for API availability/latency, scheduler dispatch latency, execution start latency, worker availability, and completion/failure rates.
- Set service-level objectives and measurement windows only after establishing realistic workload and deployment assumptions.
- Define alerting and operational responses for error-budget burn, queue growth, missed heartbeats, and stuck executions.
- Keep health/readiness endpoints distinct from Prometheus metrics and from deeper diagnostic checks.
- Propagate request, job, attempt, and worker correlation identifiers through logs and metrics.

**Acceptance criteria:** Each SLO has a documented SLI, measurement source, target, window, and alert/runbook policy. Avoid inventing targets before collecting a baseline.

### P1 — Complete the planned audit ledger as a security control

The user has confirmed that audit logging is already in the TODO list and plan and is next to be added. The plan's Milestone 1-09 already specifies an append-only cryptographic audit ledger and verification command. This recommendation is therefore about implementation quality and integration, not adding a new roadmap item.

- Record actor/principal, action, resource, timestamp, outcome, and relevant before/after metadata without recording credentials or secret values.
- Make audit writes reliable with the business operation they describe; decide explicitly whether each action requires a shared transaction or a durable outbox/event mechanism.
- Restrict mutation/deletion paths and define retention/export policy.
- Verify the hash chain and make verification failures visible to operators.
- Cover authentication, token lifecycle, authorization-sensitive changes, configuration changes, job lifecycle, worker lifecycle, and administrative actions.
- Test concurrent writers, interrupted writes, tampering, missing entries, and verification from genesis to head.

**Acceptance criteria:** Security-sensitive actions produce attributable records; chain verification detects tampering and gaps; audit failures have explicit and tested behavior.

### P2 — Make distributed coordination and high availability failure-aware

The plan includes controller peers, replication, distributed leases, fencing, and failover. Before implementing those features:

- Define the authoritative owner for each piece of state and the consistency guarantees of replicated data.
- Specify lease acquisition, renewal, expiry, fencing-token monotonicity, and stale-owner rejection.
- Document quorum assumptions, split-brain behavior, network partition behavior, and recovery procedures.
- Distinguish failover of scheduling/control-plane state from recovery of an external process that may still be running on a worker.
- Add backup/restore, disaster-recovery, and upgrade/rollback procedures before describing the system as highly available.
- Test failure scenarios with process kills, partitions, delayed messages, clock skew, and restarts.

**Acceptance criteria:** The failure model and consistency guarantees are documented; chaos/integration tests demonstrate that stale leaders cannot commit work and that recovery does not silently duplicate external side effects.

### P2 — Build reusable tests around architectural contracts

- Keep fast unit tests for domain rules and application use cases.
- Add adapter integration tests for SQLite migrations, constraints, transactions, and concurrency.
- Define reusable behavioral contract tests for persistence ports and run them against each database adapter.
- Add manager/worker protocol contract tests before remote worker rollout.
- Add end-to-end tests for the critical operator workflows across CLI and HTTP.
- Keep race detection and concurrent-startup tests in CI; use containerized PostgreSQL/MySQL integration tests when those adapters are introduced.

**Acceptance criteria:** CI exercises the relevant contracts and concurrency paths; new adapters cannot be considered complete until they pass shared behavioral tests plus adapter-specific tests.

### Roadmap alignment

- **SLOs and audit logging:** Already present in the user's TODO/plan and confirmed as the next work to be added. Do not treat them as newly identified gaps or reorder them behind the storage refactor. The recommendations above specify quality and acceptance criteria for that planned work.
- **Multi-binary management model:** `crucibled` is the server/daemon and `cruciblectl` is its separate management client. The web UI and CLI use the same API operations. The CLI supports remote network access and local Unix-socket access; transport-specific authentication maps remote identities and the trusted local-admin identity into the shared daemon application rules.
- **Standalone first:** Keep the initial product useful as a standalone daemon. Avoid introducing distributed coordination abstractions until a real use case requires them.
- **Distributed execution before v1.0.0:** Use the existing worker-fleet and cluster milestones as the delivery path; settle contracts and failure semantics before implementation.
- **Storage adapters:** Preserve SQLite as the initial adapter. Add PostgreSQL/MySQL only after persistence boundaries and behavioral contracts are stable, as described in the storage recommendations above.

## Review limitations

This was a source-level review. Findings should be revalidated against the
current code before implementation; runtime guarantees require tests.

## Current Checkout Revalidation

Revalidated against `2739c81683db140900aac247c19c9c14ad6644da` on
2026-10-30. The storage-boundary follow-up is implemented in the working tree;
affected package tests and the build pass. Full project checks remain before
commit.

Status meanings: **Confirmed** means the described condition exists in source;
**Partial** means the capability exists but the stated contract is incomplete
or contradicted; **Planned** means it is not implemented and is already
roadmapped; **Open** means the recommendation needs a product decision before
implementation; **Resolved in working tree** means a change addresses the
finding but is not yet committed; **Deferred** means no current defect or
implementation task is warranted.

| Finding | Status | Current evidence and disposition |
|---|---|---|
| Storage ports expose `*sql.DB` and `*sql.Tx` | Resolved in working tree | `Engine` no longer exposes `DB`; the SQLite adapter no longer exports `DB()` or `WithinTransaction`. Store constructors receive `*SQLiteAdapter` and use its private connection inside `internal/storage`. Commit and rollback behavior remains tested in that package, and a boundary test checks the exported method set. |
| Auth/token SQLite adapters sit in feature packages | Resolved in working tree | Concrete stores now live in `internal/storage`; auth and token persistence ports remain feature-owned. Constructors accept the concrete SQLite adapter. |
| HTTP adapter constructs fallback application/infrastructure services | Resolved in working tree | `NewServer` accepts a named `Dependencies` struct and creates no services. Engine, storage query, and token services are required; auth services are required when authentication is configured. The daemon composition root builds and injects them. |
| Token persistence creates missing users | Resolved in working tree | Token creation checks the user-lookup port before persistence; the SQLite token store no longer creates placeholder users. A test verifies the unknown user and token remain absent after rejection. |
| First-admin setup is safe across processes and atomic | Confirmed gap | `InitialSetup` uses `setupMu`, checks `CountUsers`, then separately calls `CreateUser` and `CreateSession`. The mutex is instance-local; the inspected setup tests cover validation/single invocation, not concurrent service instances or transaction failure. |
| SQLite migration startup is serialized across connections | Confirmed gap | `migrateFS` reads applied versions once before its loop; each migration is separately transactional but there is no migration-wide lock or in-transaction recheck. Inspected storage tests cover normal migration and transaction rollback, not concurrent migration startup. |
| SQLite adapter depends on application configuration | Confirmed | `internal/storage/adapter_sqlite.go` accepts `config.StorageConfig` and imports `internal/config`; its fallback path also comes from config. Translate config at the composition root if the adapter boundary is changed. |
| Shared management API for web and CLI across both transports | Partial | Separate binaries, transport discovery, `/api/v1` routes, and `docs/api.md` exist. The API documentation is not a complete shared contract for stable errors, authorization and scope parity, request limits, or streaming lifecycle. No runner/log-stream implementation exists yet. |
| Local Unix socket grants local administrator access | Intentional by owner decision | The owner clarified that admins with SSH/local CLI access are trusted to manage the server through the protected socket without PAT/login. `internal/server/adapter_http.go:extractSession` implements this through a `local-operator` admin session, matching the intended behavior. This is not a finding. Keep socket ownership/mode restrictive and document/test the trust boundary. |
| PostgreSQL/MySQL adapters and generic dialect layer | Deferred | No such adapters or dialect package exist. The future adapter roadmap remains conditional on stable persistence contracts; do not implement the generic layer before a demonstrated need. |
| Job and execution-attempt lifecycle | Planned / incomplete | The initial schema has `runs` and `job_events`, but there is no execution, scheduling, or reports feature package in this checkout and no attempt entity. Define lifecycle and recovery contracts before runner implementation; do not treat the roadmap prose as implemented behavior. |
| Manager/worker protocol and failure model | Planned / incomplete; design open | There is no cluster package or proto source in this checkout. Existing plan prose describes worker behavior, while Phase 3 promises “exactly-once” scheduled execution. Reconcile that guarantee with the at-least-once/idempotent recommendation and explicit failure assumptions before implementing distributed coordination. |
| Authentication, authorization, and worker identity separation | Partial | Local users, roles, sessions, and PATs exist. The Unix socket intentionally maps authorized local administrators to a local-admin principal; remote clients use their authenticated identities. Worker identity and a complete permission matrix for future RPC operations remain to be designed. |
| Logs/reports as attempt-linked protected artifacts | Planned / incomplete | The schema contains run visibility and report-view-token tables, but there is no reports/engine implementation in this checkout. Retention, partial output, disk exhaustion, and attempt-level access behavior remain to be specified before implementation. |
| API errors, limits, versioning, and contract tests | Partial; request-size issue confirmed | `docs/api.md` documents `/api/v1` endpoints and status codes, but handlers use ad hoc `http.Error` responses; inspected JSON decoders read request bodies without `http.MaxBytesReader`. Bound inbound bodies to prevent oversized requests. No shared API contract test suite or streaming contract exists. |
| SLO roadmap claim | Incorrect in the reviewed baseline | The recommendation text says SLOs are already in PLAN/TODO, but neither file contained SLO work at `832bad8`. SLO definition is now tracked for Phase 5 production readiness; numeric targets remain unchosen pending workload baselines. |
| Audit ledger | Planned / incomplete | Migration `003_audit_ledger.sql` creates the table and anti-tamper triggers; `internal/audit` recorder/store code is absent and TODO Milestone 1-09 is open. Treat hash-chain recording, atomicity, tamper verification, and failure behavior as acceptance criteria for that existing milestone. |
| HA and distributed coordination failure handling | Planned / open | No cluster implementation exists. The plan's exactly-once promise is stronger than the evidence or failure model currently provided. Keep this as an owner decision before Phase 3; do not silently weaken or preserve the guarantee in implementation. |
| Reusable architectural contract tests | Partial | Feature and CLI unit tests exist, including token concurrency and transaction rollback. The inspected suite has no cross-adapter behavioral contracts, setup/migration multi-connection race tests, or manager/worker protocol contracts. Add these alongside the corresponding implementations. |

### Review notes

- The local branch and HEAD were inspected; HEAD matches the review baseline.
- Relevant implementation and test files were read. The affected package tests
  and build were run; full project checks remain to be rerun.
- The owner clarified that the socket's local-admin trust boundary is
  intentional. `PLAN.md` and `docs/api.md` now document it; no code correction
  is needed for this behavior.
- Resolved statuses above describe working-tree changes and are not committed
  until the full project checks pass and the changes are reviewed.
