# Architecture Decision Records

This directory records decisions that materially shape Crucible's product, architecture, interfaces, or operations.

## Status meanings

- **Proposed:** A decision is suggested but requires project-owner approval.
- **Accepted:** The owner has approved the decision.
- **Superseded:** A later accepted ADR replaces it.
- **Deprecated:** The decision is no longer recommended but has not necessarily been replaced.

An ADR records a decision and its rationale; it is not an implementation task. Acceptance does not prove that the implementation conforms. Verify code and tests separately.

## Decision records

| ADR | Subject | Status |
|---|---|---|
| [0001](./0001-modular-monolith.md) | Modular monolith as the initial product architecture | Accepted in project context; confirm against primary repository |
| [0002](./0002-hexagonal-architecture-and-cqrs.md) | Hexagonal Architecture and pragmatic CQRS | Accepted in project context; package topology remains open |
| [0003](./0003-sqlite-first-persistence.md) | SQLite as the initial persistence adapter | Accepted in project context |
| [0004](./0004-daemon-cli-management-contract.md) | Daemon-owned management capabilities shared by UI and CLI | Accepted in project context |
| — | Worker process/binary topology | Open; do not infer a decision |

## Creating or changing a decision

1. Search existing ADRs before proposing a new one.
2. Describe the context and constraints, not just the preferred solution.
3. Record realistic alternatives and consequences.
4. Mark a new proposal **Proposed** until the owner explicitly accepts it.
5. Do not rewrite history when a decision changes. Create a superseding ADR and link both records.
6. Update the architecture and roadmap documents when an accepted decision changes them.
7. Keep implementation status separate from decision status.

Use the next available four-digit number. Keep each ADR focused on one decision.
