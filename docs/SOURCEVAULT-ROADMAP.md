# SourceVault — Product Roadmap and Architecture Baseline

> Status: planning baseline  
> Last updated: 2026-10-31  
> Purpose: preserve the product vision, confirmed constraints, and implementation sequence in one durable handoff document. This is not a claim that every design question has been settled.

## 1. Product vision

SourceVault is a federated, self-hosted developer forge. It is intended to give individuals and organizations control over their source code, collaboration history, packages, build workflows, project publishing, identity, and federation relationships.

It is not merely a Gitolite-style hosting service with a dashboard, nor a feature-for-feature GitHub clone. Its distinguishing goals are:

- Git-first project collaboration: code and project collaboration records travel together.
- Self-hosting and data sovereignty without giving up cross-instance collaboration.
- Distributed Git storage with explicit control-plane and storage responsibilities.
- First-class namespaces, projects, repositories, packages, websites, and permissions.
- Integrated PKI, service identity, SSH certificates, and public TLS lifecycle.
- Auditable operations and recoverable authoritative system metadata.
- CI/CD, package hosting, and dynamic project publishing as integrated capabilities.
- Federation with discovery, identity verification, trust policy, and negotiated mirroring.

The product should be built incrementally. The full vision is not an MVP acceptance checklist.

## 2. Confirmed architectural constraints

### 2.1 Two binaries

The intended product has exactly two primary binaries:

1. **`sourcevault-server`** — the server process.
2. **`sourcevault`** — the remote administration and user CLI.

Do not introduce a separate worker/agent binary as a default architectural requirement. Background workers and job execution components initially run within the server process. Revisit process separation only when a demonstrated operational requirement justifies it.

### 2.2 Application architecture

- Modular monolith.
- Hexagonal Architecture: domain and application logic remain independent of transport, database, filesystem, and external service adapters.
- CQRS: commands and queries have distinct responsibilities and models where useful.
- No default microservices architecture.
- Same-process module communication uses application interfaces, not loopback RPC.
- REST/JSON is the default interface for web UI, public APIs, CLI, and integrations.
- gRPC is appropriate for typed internal/remote boundaries, streaming, storage operations, replication, and node coordination where justified.
- Native Git client access remains Git-over-SSH and Git-over-HTTP.
- The CLI manages the server through its supported API; it must not reach into the server's database or filesystem.

CQRS does not require multiple databases. SQLite may serve as a local query projection in the same database deployment.

### 2.3 Deployment shape

The initial implementation is one server process with capabilities/modules enabled or disabled by configuration. Possible deployment profiles (all-in-one, control-plane-focused, storage-focused, audit-focused, or mixed HA) are candidates for later evaluation, not fixed requirements.

## 3. Product and domain model

### 3.1 Namespaces and projects

Namespaces are first-class ownership and authorization boundaries. A namespace may represent a person or an organization.

Examples:

- `justin/backup` — a personal namespace and project.
- `gotunix/crucible` — an organization namespace and project.

Namespaces own projects and may own resources directly. Projects can contain repositories, packages, websites, and collaboration records.

Stable internal namespace identifiers are preferred; display names may change. Exact nesting, naming rules, membership semantics, uniqueness scope, project transfer, and federation identity mapping remain design questions.

### 3.2 Resource boundaries

Keep the logical model clear even if the eventual physical storage layout differs:

- Namespace: ownership, membership, policy defaults.
- Project: grouping and project-level policy.
- Repository: Git objects, refs, and Git-first collaboration records.
- Package/artifact: metadata and binary content.
- Website: project publishing source and rendering configuration.
- Node: runtime identity, capabilities, and storage placement.
- Federation peer: discovered identity, trust state, and negotiated relationships.

Do not prematurely lock directory names or on-disk layouts in the absence of a storage design.

## 4. Data ownership and persistence

### 4.1 Authoritative system metadata

System state is **not** stored in a special Git repository. Authoritative system metadata is a collection of configuration files.

Those files define canonical system facts such as resource identity, existence, ownership, location, and relevant configuration. They replicate between nodes. Exact file format, schema versioning, atomic update strategy, snapshots/deltas, conflict handling, and HA/quorum behavior require design.

Durability rules:

- An authoritative update must be durably written before success is acknowledged.
- Updates must be atomic or recoverable after interruption.
- Replaying an update must be idempotent.
- Recovery must handle a crash between the authoritative file update and query projection update.
- Cluster-wide configuration must be distinguished from node-local paths and runtime state.

### 4.2 SQLite projections

SQLite is a local, rebuildable query projection/cache of authoritative system metadata and other indexed data. It is not the authority for system resource existence or ownership.

- The projection can be deleted and rebuilt from authoritative sources.
- Rebuilds must be deterministic and testable.
- Projection lag or corruption must not silently change canonical state.
- Query models may be optimized independently of canonical file representation.

### 4.3 Git repository contents

A project repository is a portable project container. Issues, pull requests, bugs, discussions, and related collaboration records are Markdown files **inside the same Git repository as the code**. A clone/export therefore carries code and collaboration history together.

SQLite may separately index these records for fast queries, but is not their canonical store.

The exact directory/schema conventions are not approved yet. Design work must address metadata/frontmatter, comments and reviews, concurrent edits, attachments, and update workflows. Pull-request records should refer to stable repository identity and immutable commit identifiers, not only mutable branch names.

This Git-first rule applies to project collaboration records. It does not mean all SourceVault system configuration, operational state, or audit data belongs in project repositories.

## 5. Distributed Git storage

SourceVault intends to build its own Gitaly-inspired distributed Git storage capability.

Responsibilities should be separated:

- **Control plane:** identity, authorization, repository registry, placement decisions, and policy.
- **Storage service/module:** direct repository filesystem and Git manipulation.
- **Git client transports:** native SSH and Git-over-HTTP.
- **Storage RPC:** gRPC for remote storage operations where a process or node boundary requires it.

Storage design must explicitly address repository placement, concurrent pushes, ref consistency, replication, integrity verification, failover, recovery, and operational observability. Git repository storage is distinct from authoritative system metadata and package/blob storage.

## 6. Security, identity, PKI, and audit

### 6.1 PKI subsystem

The integrated PKI is a Vault-like seal/unseal subsystem responsible for:

- CA hierarchy and key custody.
- Node and service identity.
- Scoped node/service enrollment using bootstrap join tokens.
- mTLS certificate issuance, rotation, and revocation.
- SSH certificate authority and SSH certificate signing.
- Public TLS lifecycle, including ACME/Let's Encrypt integration.
- Identity verification during node federation and service communication.

Join tokens should be short-lived, scoped, revocable, and ideally single-use. Bootstrap tokens are not ongoing identities. Node identity and service identity should be distinct. Internal mTLS, SSH CA, and public TLS should use separate issuing authorities or policies as appropriate.

Audit encryption/signing keys must remain separate from node identity, SSH CA, and mTLS CA keys. Seal/unseal topology (cluster-wide versus per-node) remains open.

**Security invariant:** private keys must never be written into a Git registry or ordinary replicated metadata. A prior prototype mishandled an Ed25519 private key in this way; this must not recur.

### 6.2 Audit subsystem

Go `slog` is the logging integration point. Events may route to stdout/stderr or the audit subsystem according to explicit typed event classification. Do not infer audit classification from log severity.

Audit requirements:

- Encrypt audit payloads at rest in the authoritative source and SQLite projection.
- SQLite stores ciphertext, not plaintext payloads.
- Keep searchable metadata visible where policy permits.
- Use signed, tamper-evident hash chaining.
- Tampering with an event invalidates that event and subsequent chain entries.
- Use independently replicated signed checkpoints to detect truncation.
- Define durable buffering, backpressure, and failure policy; never silently drop required audit events.

Per-node versus global chain topology remains open. Payload encryption and chain signing keys are separate key roles.

## 7. Federation, discovery, trust, and mirroring

### 7.1 Discovery mechanisms

- Global discovery is intended to use a BitTorrent DHT.
- Local-network discovery should use established mDNS/DNS-SD standards.
- Discovery may be automatic or feed a pending-review list according to operator policy.
- Discovery is not identity verification, trust, authorization, federation, or mirroring.

DHT records are discovery hints, not a trust authority. Discovery records should be signed and include stable identity, verified endpoint information, capabilities/version, and expiry. Threat modeling must include Sybil attacks, stale or malicious advertisements, spoofing, denial of service, and privacy leakage.

Whether the DHT is SourceVault-specific or supports generic compatible-service discovery remains open. Local advertisement must have appropriate opt-in and privacy controls.

### 7.2 Trust lifecycle

A candidate trust lifecycle is:

`discovered → identity verified → quarantined/pending approval → trusted → suspended/revoked`

This is a proposal to refine, not a finalized state machine. Trust policy may be human-approved or voter/policy-based. Never equate discovery with trust.

### 7.3 Repository mirroring

Per-namespace and per-repository opt-in mirroring is a confirmed product requirement. Both sides negotiate and agree to a mirror relationship.

Policy should be expressible at node, namespace, and repository levels, with namespace defaults and repository-level allow/deny overrides to be designed.

Mirroring must be a first-class storage feature, not a collection of external clone scripts. It needs incremental transfer, integrity checking, ref synchronization, recovery, and clear ownership/authorization rules. One-way read-only mirrors are a sensible initial design candidate, not yet a permanent requirement. Behavior after revocation or source deletion remains open.

## 8. Packages and artifacts

Initial registry targets:

- PyPI-compatible Python packages (wheels and source distributions).
- OCI/Docker images and compatible manifests/layers.
- Generic compiled/build artifacts.

Required workflows include pip publish/install, Docker/OCI login/push/pull, and generic artifact upload/download. Namespace ownership and permissions apply.

Keep metadata separate from bytes. Metadata follows authoritative metadata plus SQLite projection principles; binary content belongs in blob/object storage, potentially content-addressed and deduplicated. Integrate checksums, signing/provenance, retention, replication, and pipeline artifacts. npm, Maven, NuGet, and other ecosystems are later candidates, not initial commitments.

## 9. CI/CD and execution

Pipelines initially use Ansible as the execution backend.

Crucible's job-engine concepts may be reusable: scheduling, queues, workers, dependency graphs, lifecycle, output streaming, retries, timeouts, cancellation, secrets, and artifacts. Inspect the existing code and establish a narrow generic reuse boundary; do not hard-depend on or copy Crucible wholesale without an explicit design decision.

Workers are components within `sourcevault-server` initially, not a third binary. Use short-lived PKI identities and scoped credentials. Pipeline definitions may live in source repositories; execution state is operational state; logs need an explicit operational/audit split; artifacts belong in blob storage.

The precise reuse boundary and isolation model remain open.

## 10. Dynamic project publishing (Loom)

Projects may expose a website resource with URLs such as:

- `domain/namespace/project`
- `project.namespace.domain`
- `project.domain` where uniqueness or explicit routing makes this safe
- Custom domains as a later capability

The goal includes dynamic Markdown rendering, not merely static HTML hosting. Rendered HTML may be cached in SQLite.

Existing prototype: `gotunix-org/prototype-loom`, previously deployed at `https://gotcode.org/projects/loom`.

Initial inspection found a Go Cobra CLI, SQLite page cache keyed by domain/path, YAML-based site mapping, a Git-backed VFS, periodic remote-hash polling, and cache invalidation on content refresh. The full rendering/template/VFS/security behavior has not yet been audited; do not assume the prototype is production-ready or approve wholesale reuse.

Required archaeology before a reuse decision:

- Markdown and template/rendering pipeline.
- VFS and embedded Git behavior.
- Authentication and authorization boundaries.
- Cache invalidation and consistency.
- Host/domain routing and alias behavior.
- TLS/ACME lifecycle and security controls.
- Tests, operational behavior, and failure handling.

Likely integration directions to evaluate include replacing standalone YAML mappings with namespace/project registry lookups, using SourceVault PKI rather than independent key files, and replacing periodic polling with commit/ref notifications where practical. These are proposals pending inspection.

## 11. Engineering rules for contributors and coding agents

These rules exist to prevent speculative implementation and repeated architectural drift.

1. Inspect the current repository, relevant code, tests, and documentation before proposing or changing behavior.
2. Treat this roadmap's confirmed constraints as binding unless the owner explicitly changes them.
3. Label statements as **confirmed**, **proposal**, or **open question**. Never silently promote a proposal into a requirement.
4. Do not invent product requirements, endpoints, schemas, storage layouts, deployment modes, or user intent.
5. Do not replace a requested architecture with a familiar template merely because it is easier to generate.
6. Keep domain/application logic independent of adapters.
7. Keep the two-binary constraint; do not add worker binaries by default.
8. Do not introduce microservices or multiple databases without a demonstrated requirement and explicit decision.
9. Preserve authoritative-source versus projection boundaries. Never make SQLite the canonical authority for system metadata.
10. Keep project collaboration records in the same Git repository as project code; do not move them into a shared external database.
11. Treat secrets and private keys as sensitive material. Never place private keys in Git, logs, ordinary config, or replicated metadata.
12. Make changes narrowly. Avoid unrelated refactors and speculative abstractions.
13. Add or update tests for behavior and failure cases, including crash/recovery and authorization boundaries where relevant.
14. Run the relevant tests and report the exact commands and outcomes. Never claim a test ran if it did not.
15. Update documentation and handoff notes when behavior, interfaces, or decisions change.
16. Do not commit, merge, push, or alter branches unless explicitly requested.
17. If repository evidence conflicts with this roadmap, stop and explain the discrepancy rather than guessing.
18. When uncertain, ask one focused question or record the uncertainty as an open decision.

## 12. Phased roadmap

Phases are ordered by dependency, not calendar date. Work may overlap only when interfaces and prerequisites are stable.

### Phase 0 — Repository archaeology and baseline

- Inventory repositories, branches, existing prototypes, docs, build/test workflows, and current implementation status.
- Identify existing code that is reusable, obsolete, or merely illustrative.
- Record architecture and documentation inconsistencies.
- Establish reproducible build, test, lint, and security-check commands.
- Create an evidence-backed baseline; do not start a rewrite based on assumptions.

**Exit gate:** current state and gaps are documented with references to actual repository evidence.

### Phase 1 — Requirements baseline

- Separate confirmed requirements, proposals, and unresolved questions.
- Capture core user journeys: create namespace/project/repository, clone/push, collaborate, administer, enroll node, and configure federation.
- Define initial target users and a deliberately small MVP boundary.
- Identify security, portability, recovery, and interoperability acceptance criteria.

**Exit gate:** owner-reviewed requirements baseline and explicit MVP scope.

### Phase 2 — Architecture and domain design

- Define bounded modules and dependency direction for the modular monolith.
- Define namespace/project/resource identity and authorization model.
- Define command/query boundaries and application ports.
- Decide initial REST API conventions and where gRPC is justified.
- Define server configuration, module enablement, and lifecycle.
- Document the two-binary packaging and deployment model.

**Exit gate:** architecture decision records for foundational choices, with unresolved decisions visible rather than hidden.

### Phase 3 — Canonical data, Git collaboration, and projections

- Specify authoritative metadata file formats and schemas.
- Design atomic/durable update and recovery behavior.
- Specify deterministic SQLite projection rebuild and reconciliation.
- Design Git-first collaboration record format and repository lifecycle.
- Define immutable commit references, metadata/frontmatter, concurrent updates, and attachment strategy.
- Build failure-injection tests for crash windows and projection rebuilds.

**Exit gate:** data ownership and recovery invariants are implemented and tested before higher-level features depend on them.

### Phase 4 — Minimal runnable platform foundation

- Build `sourcevault-server` and `sourcevault`.
- Implement configuration, startup/shutdown, structured logging, health/readiness, and module registration.
- Implement the first vertical slice through API, application, domain, and persistence adapters.
- Establish migration/versioning policy for projections and authoritative metadata.
- Provide local development and repeatable test environments.

**Exit gate:** clean install/start, basic authenticated API/CLI interaction, and repeatable tests.

### Phase 5 — Core Git hosting and collaboration

- Implement namespace/project/repository registration and authorization.
- Support native Git-over-SSH and Git-over-HTTP.
- Implement repository lifecycle, permissions, and safe concurrent ref updates.
- Store issues, pull requests, bugs, discussions, and related records inside the same project repository as Markdown.
- Add indexes/projections for collaboration queries.
- Test clone/export portability and reconstruction of indexes.

**Exit gate:** users can create, clone, push, and collaborate while project records remain portable with the repository.

### Phase 6 — Distributed Git storage

- Establish storage API and control-plane placement responsibilities.
- Implement local storage adapter first.
- Add gRPC storage boundary only where the deployment topology requires it.
- Implement integrity checks, replication, ref consistency, concurrent operations, and recovery.
- Test node loss and interrupted transfers.

**Exit gate:** storage behavior is observable, integrity-checked, and recoverable; local single-node mode remains supported.

### Phase 7 — PKI, node identity, and audit

- Define seal/unseal and key-custody threat model.
- Implement bootstrap enrollment and node/service identities.
- Add mTLS issuance, rotation, and revocation.
- Add SSH CA and certificate lifecycle.
- Integrate public TLS/ACME.
- Implement typed audit classification, encrypted payloads, signed hash chains, and checkpoints.
- Test key separation, rotation, revocation, tampering, truncation, and audit backpressure.

**Exit gate:** identity lifecycle and audit integrity are demonstrated through failure and adversarial tests.

### Phase 8 — Federation discovery, trust, and mirroring

- Implement mDNS/DNS-SD local discovery.
- Define signed peer identity and discovery record format.
- Implement DHT discovery with expiry and privacy controls.
- Implement quarantine, approval, trust, suspension, and revocation policy.
- Implement negotiated namespace/repository mirroring.
- Add incremental transfer, integrity verification, ref synchronization, and revocation handling.

**Exit gate:** discovery never grants trust implicitly; mirror relationships require explicit bilateral agreement and have tested recovery semantics.

### Phase 9 — Package and artifact registry

- Implement blob/object storage abstraction.
- Add generic artifact upload/download and metadata.
- Add PyPI-compatible workflows.
- Add OCI/Docker manifests, layers, and registry API compatibility.
- Add namespace/project permissions, checksums, retention, and replication hooks.
- Add signing/provenance integration.

**Exit gate:** supported clients can publish and retrieve artifacts with authorization and integrity guarantees.

### Phase 10 — Pipeline engine and Ansible integration

- Inspect Crucible and define the reusable generic job-engine boundary.
- Implement pipeline definition and validation.
- Add scheduling, dependency graphs, lifecycle, logs/streaming, retries, timeouts, cancellation, secrets, and artifacts.
- Integrate Ansible execution.
- Use scoped, short-lived credentials and explicit isolation controls.
- Keep execution components inside the server process initially.

**Exit gate:** representative pipelines run reproducibly with auditable outcomes and bounded credentials.

### Phase 11 — Dynamic publishing and Loom integration

- Complete Loom prototype archaeology.
- Define website resource, routing, domain ownership, and authorization.
- Integrate Markdown rendering and safe template behavior.
- Add cache invalidation based on repository/ref changes.
- Integrate PKI and custom-domain lifecycle where approved.
- Test untrusted content, cache isolation, and domain verification.

**Exit gate:** project websites publish from repository content with safe rendering, correct authorization, and reliable cache refresh.

### Phase 12 — Administration, UI, CLI, and integrations

- Complete essential namespace/project/repository administration workflows.
- Provide CLI coverage for remote management and common developer operations.
- Add web UI workflows without making the UI the only way to operate the system.
- Document APIs, authentication, permissions, and automation interfaces.
- Add operational status, diagnostics, and safe configuration validation.

**Exit gate:** core workflows are manageable through documented interfaces and the CLI.

### Phase 13 — Reliability, HA, backup, and release readiness

- Define supported deployment profiles from measured needs.
- Implement backup/restore for authoritative metadata, Git storage, blobs, PKI material, and audit data.
- Test disaster recovery and node replacement.
- Validate replication lag, failover, consistency, and operational recovery.
- Complete threat modeling, security review, upgrade/rollback tests, and performance testing.
- Publish installation, upgrade, backup, restore, and operator documentation.

**Exit gate:** a clean deployment can be installed, upgraded, backed up, restored, monitored, and operated with documented guarantees.

## 13. Cross-cutting quality gates

Every phase should consider:

- **Correctness:** domain invariants, authorization, concurrency, idempotency.
- **Durability:** crash windows, recovery, backups, and restore verification.
- **Security:** least privilege, secret handling, key separation, input validation, auditability.
- **Portability:** project clone/export, documented formats, no hidden database dependency for project collaboration history.
- **Observability:** structured logs, metrics, traces where useful, health and actionable diagnostics.
- **Testing:** unit, integration, end-to-end, race, fuzz/property, failure-injection, and security tests as appropriate.
- **Documentation:** architecture decisions, operator procedures, API contracts, and agent handoff notes.
- **Scope discipline:** no speculative features or unrelated refactors.

A phase is not complete because code compiles. Its exit gate must be demonstrated with tests, operational evidence, and documentation.

## 14. Open decisions register

The following are intentionally unresolved and must not be silently decided by implementation:

1. Exact namespace/project nesting, naming, uniqueness, membership, transfer, and federation identity rules.
2. Authoritative metadata file format, schema evolution, atomic update mechanism, snapshots/deltas, and conflict resolution.
3. Cluster consistency, quorum, and HA model for replicated canonical metadata.
4. Physical on-disk layout for repositories, metadata, blobs, and caches.
5. Which gRPC boundaries are required in the first deployment topology.
6. Storage placement, replication factor, failover, and ref consistency guarantees.
7. Collaboration Markdown schema, comment/review update model, conflict handling, and attachment storage.
8. Federation DHT record format, SourceVault-specific versus generic discovery, privacy defaults, and trust policy.
9. Exact lifecycle and data-retention behavior for revoked/deleted mirror relationships.
10. Seal/unseal topology and key custody model.
11. Per-node versus global audit chain and checkpoint distribution.
12. Crucible job-engine reuse boundary and execution isolation model.
13. Package blob storage implementation and initial retention/replication policies.
14. Loom reuse versus rewrite decision after complete prototype inspection.
15. Exact MVP feature set and release criteria.
16. Deployment profiles and HA scope based on real operational requirements.

## 15. MVP guidance

Do not attempt to deliver the entire vision in the first release. A credible MVP should prove the architectural foundations and one coherent developer workflow:

- Two binaries and modular-monolith structure.
- Namespace/project/repository identity and authorization.
- Durable authoritative metadata plus rebuildable SQLite projections.
- Native Git hosting.
- Git-first collaboration records in the same repository.
- A secure, documented authentication baseline.
- CLI-driven administration and a minimal usable web interface.
- Backup/restore and operational diagnostics for the supported single-node deployment.

Distributed storage, federation, full PKI lifecycle, package ecosystems, pipelines, and dynamic publishing should be sequenced after the foundations they depend on. Their designs should be preserved, but they should not destabilize the first usable release.

---

**Maintainer note:** This roadmap is a living requirements and sequencing document. Update it when the owner makes a decision or repository evidence changes. Preserve the distinction between confirmed requirements, proposals, and open questions.
