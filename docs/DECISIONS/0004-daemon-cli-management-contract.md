# ADR 0004: Daemon-Owned Management Capabilities Shared by UI and CLI

- **Status:** Accepted in recorded project context.
- **Date:** 2026-10-30
- **Decision owners:** Project owner

## Context

Crucible is intended to provide both a web UI and an operator CLI. Duplicating application workflows in each client would lead to inconsistent validation, authorization, and behavior. The project context records that `crucibled` is the server/daemon and `cruciblectl` is a separate CLI client.

The CLI is intended to manage Crucible remotely over a network connection or locally through a Unix-domain socket.

## Decision

The daemon owns application capabilities and the management API contract. The web UI and `cruciblectl` are clients of those capabilities.

- Business rules, authorization decisions, and state transitions belong in daemon-side application behavior.
- The web UI and CLI must not independently implement the same business workflows.
- Both clients use consistent operations, validation rules, and error semantics.
- Local Unix-socket access is a transport choice, not an implicit authorization bypass.
- Remote access uses the approved network transport and authentication policy.
- The composition root wires inbound transports to the same application capabilities.

This decision establishes the client/server responsibility boundary. It does not freeze the complete endpoint list, wire format, authentication scheme, or API versioning policy; those require their own specifications and compatibility decisions.

## Consequences

- Management operations should be defined independently of HTTP or CLI presentation.
- API contract tests should verify behavior relied on by both clients.
- Authorization must be enforced by the daemon for every transport.
- CLI output and web presentation may differ, while application semantics remain consistent.
- The daemon API becomes a public compatibility surface if external clients are supported; its stability policy must be decided before 1.0.

## Alternatives considered

1. **CLI contains direct business logic and database access:** Rejected because it duplicates behavior and bypasses the daemon boundary.
2. **Web UI and CLI have separate application services:** Rejected because they would drift in validation and policy.
3. **Unix socket automatically grants unrestricted administrator access:** Not selected; transport locality must not silently replace authorization.

## Revisit when

The product owner changes the deployment or client model, or a new interface requires a separately specified application contract.
