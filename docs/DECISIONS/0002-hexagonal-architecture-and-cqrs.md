# ADR 0002: Hexagonal Architecture and Pragmatic CQRS

- **Status:** Accepted in recorded project context; package topology remains unresolved.
- **Date:** 2026-10-30
- **Decision owners:** Project owner

## Context

Crucible needs to keep business behavior independent of transport and persistence choices. Its CLI and web UI are clients of daemon-owned capabilities, and SQLite is the first persistence adapter. The project context explicitly records Hexagonal Architecture (Ports & Adapters) and CQRS as architectural direction.

The repository's instructions also require a flat package-by-feature layout and prohibit generic horizontal folders such as `domain/`, `application/`, `transport/`, and `ports/`. Hexagonal Architecture is a dependency model, not a requirement to create those folders.

## Decision

Use **Hexagonal Architecture (Ports & Adapters)** and **CQRS where it clarifies responsibilities**.

- Application/business capabilities own the interfaces (ports) they require.
- Infrastructure provides concrete adapters for those ports.
- Inbound adapters translate external requests into application operations; they do not construct persistence infrastructure or contain business rules.
- The composition root creates concrete implementations and wires dependencies.
- Commands represent state-changing operations; queries retrieve information without changing business state, except for explicitly documented telemetry or access-recording behavior.
- Do not add CQRS ceremony, separate databases, event sourcing, or generic mediator frameworks without a demonstrated need.

The architecture is defined by dependency direction and ownership, not by mandatory directory names.

## Package topology: explicit unresolved constraint

The project instructions currently prohibit horizontal layer directories and require feature packages directly under `internal/`. This can coexist with ports-and-adapters concepts, but the exact placement of concrete adapters and shared infrastructure must be specified.

A candidate approach for discussion—not an accepted package-tree decision—is:

- Feature packages own their application-facing ports and use cases.
- Concrete SQLite implementations live in a clearly identified infrastructure package.
- The daemon composition root wires feature ports to concrete adapters.
- Cross-feature dependencies are explicit and minimized.

Do not reorganize packages based on this candidate until the owner approves the concrete package layout. Resolve any conflict between `AGENTS.md`, `ARCHITECTURE.md`, and this ADR before implementation.

## Consequences

- Business logic should be testable without a live database, HTTP server, or gRPC connection.
- Concrete SQL types should not leak through application-facing ports.
- The composition root is responsible for dependency selection.
- Tests should verify both feature behavior and important architectural boundaries.
- The team must agree on a package topology that satisfies the dependency model without violating the repository's layout constraints.

## Alternatives considered

1. **Database-first or transport-first layering:** Rejected because it makes business behavior depend on infrastructure.
2. **CQRS everywhere:** Rejected because unnecessary command/query ceremony adds complexity without value.
3. **Generic repository/ORM framework:** Not selected; introduce only if a concrete need is demonstrated.

## Revisit when

The owner changes the product architecture, the package topology proves unable to preserve the intended dependency direction, or CQRS introduces more complexity than clarity.
