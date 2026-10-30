# Crucible — Product and Engineering Roadmap

> **Status:** Draft for project-owner review  
> **Last updated:** 2026-10-30  
> **Scope:** Strategic sequencing and milestone outcomes; not a sprint backlog or delivery-date commitment.

## 1. Purpose and authority

This roadmap describes a proposed order for delivering Crucible as usable, verifiable product capabilities. It is intentionally higher-level than the root `TODO.md`.

Use the project documents as follows:

- [`CONTEXT.md`](../CONTEXT.md) records the project direction and context confirmed during the architecture review.
- [`ARCHITECTURE.md`](../ARCHITECTURE.md) describes the architecture; it requires reconciliation with the current context and decisions before being treated as fully authoritative.
- [`PLAN.md`](../PLAN.md) is the detailed technical blueprint. Its proposals must be checked against current owner decisions.
- [`TODO.md`](../TODO.md) is the task-level implementation checklist.
- [`RECOMMENDATIONS.md`](../RECOMMENDATIONS.md) contains review findings and proposals, not approved implementation tasks.
- [`DECISIONS/`](./DECISIONS/README.md) records decisions, rationale, consequences, and unresolved questions.
- [`AI-HANDOFF.md`](./AI-HANDOFF.md) records the verified state of work between sessions.

This roadmap does not override an owner-approved decision, add a feature requirement, or authorize implementation by itself. When documents disagree, stop and resolve the conflict with the project owner; do not silently choose one.

## 2. Product direction

The currently recorded direction is:

- Crucible is a Go modular monolith, not a collection of independently deployed microservices.
- The architecture uses Hexagonal Architecture (Ports & Adapters) and CQRS where the separation adds value.
- `crucibled` is the daemon/server and `cruciblectl` is a separate operator CLI client.
- The web UI and CLI use the same daemon-owned application capabilities and management API; clients must not duplicate business rules.
- SQLite is the initial persistence adapter. PostgreSQL and MySQL are possible future adapters, not current implementation commitments.
- Distributed execution is part of the broader product direction, while delivery should establish useful standalone operation first.

These are product-direction statements, not proof that a capability is implemented. The current implementation status must be established from the primary repository and verified tests.

## 3. Roadmap principles

1. **Deliver usable vertical slices.** A milestone should result in an operator-visible capability, not merely a collection of packages or abstractions.
2. **Standalone value before distributed complexity.** Establish a useful single-node experience before depending on cluster coordination.
3. **Define contracts before multiple implementations.** Stabilize application-facing persistence and execution contracts before adding alternate databases or remote executors.
4. **Security and operability are acceptance criteria.** Authentication, authorization, auditability, recovery, observability, and safe failure behavior are part of features—not cleanup for the end.
5. **Preserve the existing implementation.** Prefer focused changes and tests over broad rewrites.
6. **No speculative abstraction.** Add an interface or framework when a real boundary or second implementation justifies it.
7. **No dates or release numbers by implication.** Milestones are capability gates. Release versions are assigned through the release process after scope and compatibility are reviewed.

## 4. Proposed delivery milestones

The milestones below are a planning proposal. Their ordering and acceptance criteria require owner review, and individual items must be reconciled with `TODO.md` and the detailed blueprint before being treated as committed scope.

### M0 — Establish an authoritative engineering baseline

**Outcome:** Contributors can determine the intended architecture, current implementation state, approved scope, and verification standard without guessing.

Candidate exit criteria:

- Resolve conflicts among `AGENTS.md`, `CONTEXT.md`, `ARCHITECTURE.md`, and `PLAN.md`.
- Identify which architecture statements are implemented, intended, proposed, or unknown.
- Confirm package ownership and dependency direction for the chosen Hexagonal/CQRS implementation.
- Establish an ADR process and a roadmap/backlog relationship.
- Establish repeatable local checks and a definition of done.
- Revalidate the existing architecture recommendations against the current primary-repository HEAD.

**Not complete merely because:** documentation files exist. The owner must review and accept the decisions, and the code baseline must be inspected.

### M1 — Deliver a coherent standalone operator workflow

**Outcome:** An operator can configure and operate a single Crucible instance through supported interfaces.

Candidate capability areas to validate against the existing implementation and backlog:

- Reliable daemon startup, shutdown, configuration, and health reporting.
- A clearly defined management API and consistent error semantics.
- CLI operations that call daemon capabilities rather than reimplementing business logic.
- Authentication, authorization, and first-time setup invariants.
- Job definition/submission and a well-defined execution lifecycle.
- Local execution with observable status, output, and results.
- Durable run history and report/log access.
- Scheduling and operator control of jobs.

**Candidate end-to-end acceptance scenario:** Install and configure a single instance, authenticate, submit or trigger a job using a supported interface, observe its lifecycle and output, and retrieve its result after completion or daemon restart.

The precise minimum feature set for this milestone is an owner decision. Do not infer it from this illustrative scenario.

### M2 — Make execution and operations dependable

**Outcome:** The standalone product behaves predictably under failure and provides enough operational information to diagnose and recover.

Candidate areas:

- Explicit job and execution-attempt semantics, including retries and cancellation.
- Defined timeout, termination, and shutdown behavior.
- Durable logs/reports with bounded resource use and access controls.
- Audit events for security-sensitive and operator actions.
- Measurable reliability objectives and health/readiness semantics.
- Backup, restore, and data-integrity procedures.
- Concurrency and multi-process safety for setup and database migrations.

These items overlap existing recommendations and blueprint material. Revalidate them before assigning priority.

### M3 — Define and validate the worker contract

**Outcome:** Local and remote execution can be expressed through a stable application-facing contract.

Candidate areas:

- Worker identity and authentication.
- Registration, capabilities, and availability.
- Assignment, acknowledgement, execution, and result reporting.
- Heartbeats, health, drain, pause, and recovery semantics.
- Cancellation, timeout, and loss-of-connectivity behavior.
- Version and protocol compatibility policy.

**Gate:** Do not implement distributed dispatch until the owner approves the worker deployment model and the lifecycle/failure semantics are documented and tested. The distinction between daemon modes and worker process/binary topology remains an open architecture question.

### M4 — Introduce distributed execution incrementally

**Outcome:** Remote workers execute approved work through authenticated, observable, recoverable coordination.

Candidate areas:

- Secure manager/worker communication (the existing project guidance specifies gRPC and mTLS; confirm these remain approved).
- Job dispatch and acknowledgement.
- Duplicate-delivery and idempotency handling.
- Worker disconnection, reconnection, and recovery.
- Drain and maintenance behavior.
- Cluster membership and coordination failure behavior.
- Operational visibility across nodes.

Do not promise exactly-once execution unless the system can define and prove what that means across process crashes, network partitions, and external side effects. Prefer explicit delivery and idempotency semantics.

### M5 — Expand persistence and production operations

**Outcome:** Crucible can support additional deployment and operational requirements without weakening its application boundaries.

Candidate areas:

- PostgreSQL adapter, if justified by a concrete deployment need.
- MySQL adapter, if justified independently.
- Migration and upgrade compatibility.
- High-availability design, only after consistency and failure semantics are specified.
- Performance, capacity, and resource-bound testing.
- Packaging, installation, upgrade, backup, and recovery workflows.
- Security review and operational documentation.

Alternate database adapters are gated on stable application-owned ports, transaction semantics, and adapter contract tests. They are not a reason to introduce a generic SQL dialect framework prematurely.

### M6 — 1.0 readiness and compatibility commitment

**Outcome:** Crucible is ready to make explicit public compatibility and support promises.

Candidate exit criteria:

- Owner-approved product scope and supported deployment model.
- Public CLI, API, configuration, and extension contracts documented.
- Compatibility and deprecation policy established.
- Upgrade, migration, backup, and recovery procedures tested.
- Security and operational acceptance criteria satisfied.
- Supported platforms and resource expectations documented.
- Release artifacts and reproducible build/version metadata verified.
- User-facing documentation reflects actual behavior.
- Known limitations and support policy published.

A 1.0 release is a product-readiness decision, not simply completion of a numbered checklist.

## 5. How to use this roadmap with TODO.md

- Keep `ROADMAP.md` focused on outcomes, dependencies, and milestone gates.
- Keep `TODO.md` focused on actionable tasks and their verified status.
- A task in `TODO.md` does not become approved merely because it appears in `PLAN.md`.
- Do not mark work complete based only on code presence; require the relevant acceptance criteria and verification evidence.
- When priorities or milestone boundaries change, update this roadmap and record material architectural/product changes as ADRs.

## 6. Open decisions before baselining the roadmap

The following must be resolved or explicitly accepted as deferred:

1. **Package topology:** How should feature-owned ports, application/domain logic, and concrete infrastructure adapters be arranged while satisfying the flat package-by-feature rule in `AGENTS.md`?
2. **Worker topology:** Is a worker a mode of `crucibled`, a separate executable, or a separately deployable role using a shared binary? Existing documents describe these differently.
3. **Product MVP:** Which operator workflows are essential for the first demonstrable standalone release?
4. **Existing plan reconciliation:** Which sections of `PLAN.md` remain approved requirements, which are design options, and which are obsolete?
5. **Release policy:** Confirm the proposed SemVer policy and whether any supported maintenance/LTS branches are actually needed.

Until these are resolved, treat the milestone structure as a draft planning aid, not an implementation mandate.

## 7. Review history

- 2026-10-30: Initial draft created from the recorded project context, existing architecture review, and repository planning documents. No source-code status was inferred and no milestone was marked complete.
