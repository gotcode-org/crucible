# ADR 0001: Modular Monolith as the Initial Product Architecture

- **Status:** Accepted in recorded project context; reconfirm against the primary repository before treating this mirror copy as authoritative.
- **Date:** 2026-10-30
- **Decision owners:** Project owner

## Context

Crucible needs a coherent server-side application that can run simply on a standalone host while leaving room for future distributed execution. Splitting the initial product into independently deployed services would add deployment, networking, consistency, and operational complexity before those boundaries have been demonstrated.

The project context records an explicit preference for a Go monolith with internal modular boundaries, rather than a collection of microservices.

## Decision

Build Crucible initially as a **modular monolith**.

- Keep business capabilities in cohesive, independently understandable feature areas.
- Keep runtime composition and process entrypoints separate from business behavior.
- Use explicit interfaces at meaningful boundaries.
- Do not create independently deployed services merely to mirror internal package boundaries.
- Introduce a separately deployed process or service only when an approved product or operational requirement justifies it.

This decision does not, by itself, decide the exact Go package tree or whether a future worker uses the same executable.

## Consequences

### Positive

- One primary application deployment and a simpler initial operational model.
- Easier local development, testing, and debugging.
- Internal boundaries can be tested before any process or network boundary is introduced.
- Distributed execution can be added through explicit contracts rather than premature service decomposition.

### Trade-offs

- Package boundaries and dependency direction must be actively maintained.
- A modular monolith can become tightly coupled if features share concrete infrastructure or reach into one another's internals.
- Future process separation may require explicit protocol and lifecycle work.

## Alternatives considered

1. **Microservices from the beginning:** Rejected for the initial product because the operational cost is not justified by a demonstrated need.
2. **Unstructured monolith:** Rejected because it would make feature ownership and future execution boundaries harder to preserve.

## Implementation and verification notes

This ADR records intended architecture, not implementation status. Verify the current repository's package dependencies, composition root, and process topology independently.

## Revisit when

A concrete scaling, isolation, deployment, or reliability requirement demonstrates that an independently deployed service is necessary.
