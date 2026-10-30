# ADR 0003: SQLite as the Initial Persistence Adapter

- **Status:** Accepted in recorded project context.
- **Date:** 2026-10-30
- **Decision owners:** Project owner

## Context

Crucible is intended to be straightforward to deploy on a standalone host, including resource-constrained systems. The project context identifies SQLite as the initial database and PostgreSQL/MySQL as possible future targets.

The architecture review identified SQL implementation leakage through the storage Engine abstraction and transaction callbacks. Adding more database drivers before application-facing persistence and transaction contracts are clear would multiply uncertainty.

## Decision

Use **SQLite as the initial persistence adapter**.

- Keep database-specific SQL, connection handling, and transaction mechanics inside the SQLite adapter.
- Keep application-facing ports independent of concrete SQL driver types.
- Keep schema migrations explicit, versioned, and reviewable.
- Define transaction behavior at the application boundary based on actual use cases.
- Do not add PostgreSQL or MySQL support until a concrete need exists and the relevant ports, transaction semantics, and adapter contract tests are stable.
- Do not introduce an ORM or generic SQL-dialect abstraction solely to anticipate future databases.

The existing project guidance specifies pure-Go SQLite and external SQL migration files. Those implementation constraints remain subject to validation against the current repository and should not be silently changed by this ADR.

## Consequences

- SQLite behavior, concurrency limits, migration safety, and backup/restore must be understood and tested.
- Application services should depend on feature-owned ports rather than raw `*sql.DB` or `*sql.Tx`.
- Future adapters may reveal differences in isolation, locking, and transaction behavior; those differences must be addressed explicitly rather than hidden behind a lowest-common-denominator abstraction.
- Alternate databases remain future possibilities, not current delivery commitments.

## Alternatives considered

1. **Support multiple databases immediately:** Deferred because it increases implementation and test surface before the application contracts are stable.
2. **Adopt an ORM or generic database framework:** Not selected without a demonstrated requirement.
3. **Use a remote database as the only initial option:** Not selected because it conflicts with the standalone-first deployment goal.

## Revisit when

A supported deployment requirement cannot be met reliably with SQLite, or a concrete operational need justifies an additional adapter.
