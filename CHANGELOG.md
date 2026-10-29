# Changelog

All notable changes to the **Crucible** project (`crucibled` server daemon
and `cruciblectl` operator CLI) will be documented in this file.

The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Subsystems**:
  - Hexagonal CQRS Configuration Subsystem
    ([`internal/config/`](./internal/config/)) providing 4-tier precedence
    resolution (flags, environment variables, YAML configuration files, and
    embedded defaults), domain models for `ServerConfig`, `ClientConfig`,
    `StorageConfig`, and `SystemdOptions`, and filesystem/environment adapters.
  - Hexagonal Relational Persistence Subsystem
    ([`internal/storage/`](./internal/storage/)) with outbound `Engine` and
    `Transactor` ports, inbound CQRS `CommandHandler` and `QueryHandler`
    services, and pure Go SQLite adapter
    ([`adapter_sqlite.go`](./internal/storage/adapter_sqlite.go))
    utilizing `modernc.org/sqlite` in WAL mode with foreign keys.
  - Concurrency-Safe Monotonic ULID Identity Subsystem
    ([`internal/id/`](./internal/id/)) providing domain entity identifier
    generation with standard category prefixes (`run_`, `tok_`, `dev_`,
    `inv_`, `sec_`, `ses_`, `aud_`, `sch_`, `ssh_`, `wrk_`, `nbc_`).
  - Hexagonal CQRS Version Subsystem
    ([`internal/version/`](./internal/version/)) providing runtime build
    metadata extraction, dependency introspection, and tabular/JSON
    presentation formats.
  - Terminal UI Subsystem ([`internal/ui/`](./internal/ui/)) with Charm Lip
    Gloss styling, adaptive colors, status badges, ASCII banner, formatted
    tables, and Cobra help/usage formatters.
  - Standalone Server & IPC Listener Subsystem
    ([`internal/server/`](./internal/server/)) implementing dual TCP listener
    and UNIX domain socket bindings (`0660` mode with automatic cleanup),
    JSON `/healthz` liveness/readiness verification, and graceful shutdown
    context draining.
  - Embedded Web Shell Subsystem ([`web/`](./web/)) utilizing Go `//go:embed`
    to package HTML5 templates, Tailwind CSS styles, JetBrains Mono font
    stacks, custom scrollbars, and client scripts into the binary.
  - Hexagonal CQRS Authentication & Multi-Device TOTP Subsystem
    ([`internal/auth/`](./internal/auth/)) providing outbound SPI ports
    (`Authenticator`, `DeviceStore`, `SessionStore`, `ChallengeStore`,
    `InviteStore`, and `SecretCrypter`), inbound CQRS ports (`CommandHandler`
    and `QueryHandler`), pure Go Linux PAM adapter via `/sbin/unix_chkpwd`
    with zero Cgo (`CGO_ENABLED=0`), RFC 6238 TOTP engine with drift tolerance,
    SVG vector and ANSI terminal QR generators, AES-256-GCM envelope
    encryption, and SQLite/in-memory persistence adapters.
- **Database Schemas & Migrations**:
  - `001_initial.sql`: Initial relational schema defining 18 core tables
    with monotonic ULID primary keys and foreign key constraints.
  - `002_indexes.sql`: 14 composite covering indexes optimizing high-frequency
    lookups and status filtering across runs, tokens, and workers.
  - `003_audit_ledger.sql`: Append-only SHA-256 hash-chained immutable audit
    ledger protected by SQLite `BEFORE UPDATE` and `BEFORE DELETE` anti-tamper
    triggers.
- **Commands**:
  - Added `start` daemon subcommand to `crucibled`
    ([`cmd/crucibled/start.go`](./cmd/crucibled/start.go)) for launching the
    standalone server daemon with auto-migration, status telemetry banners,
    and SIGINT/SIGTERM graceful shutdown handling.
  - Added `db migrate` and `db status` subcommands to `crucibled`
    ([`cmd/crucibled/db.go`](./cmd/crucibled/db.go)) for applying embedded
    schema migrations, displaying migration records, and inspecting database
    integrity, WAL pragmas, schema version, and table counts.
  - Added `config init` and `config validate` subcommands to `crucibled`
    ([`cmd/crucibled/config.go`](./cmd/crucibled/config.go)) and `cruciblectl`
    ([`cmd/cruciblectl/config.go`](./cmd/cruciblectl/config.go)), along with
    `config use-context` for profile switching.
  - Added `systemd unit` generator subcommand to `crucibled`
    ([`cmd/crucibled/systemd.go`](./cmd/crucibled/systemd.go)) for producing
    hardened systemd service units with `CAP_NET_BIND_SERVICE`.
  - Added `version` subcommand to `crucibled` daemon
    ([`cmd/crucibled/version.go`](./cmd/crucibled/version.go)) and
    `cruciblectl` CLI
    ([`cmd/cruciblectl/version.go`](./cmd/cruciblectl/version.go)) with
    optional dependency tree flag (`-d`, `--dependencies`).
  - Added `auth login`, `auth 2fa devices`, `auth 2fa revoke`, and `auth 2fa
    invite` subcommands to `cruciblectl`
    ([`cmd/cruciblectl/auth.go`](./cmd/cruciblectl/auth.go)) for operator
    credential authentication, multi-device management, and single-use
    onboarding invite generation with terminal QR codes.
- **Web Console Views & Templates**:
  - Added standalone Public Execution Monitor at `/`
    ([`public_dashboard.html`](./web/templates/public_dashboard.html))
    providing clean, centered pipeline visibility for unauthenticated
    visitors without sidebar controls or engine diagnostics.
  - Added redirect destination preservation via `next` parameter across
    `/login` and `/auth/2fa` views with open-redirect validation.
  - Added standalone `/login` authentication form and `/auth/2fa` challenge
    verification page with branded responsive layout.
  - Added `/settings/2fa` multi-device security management dashboard with
    device revocation, registration modals, and invite issuance.
- **Repository Architecture & Governance**:
  - Technical blueprint specifications ([`PLAN.md`](./PLAN.md),
    [`ARCHITECTURE.md`](./ARCHITECTURE.md)).
  - Contributor and AI agent guidelines with strict commit and signing
    guardrails ([`AGENTS.md`](./AGENTS.md),
    [`CONTRIBUTING.md`](./CONTRIBUTING.md)).
  - Open source governance policies
    ([`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md),
    [`SECURITY.md`](./SECURITY.md), [`MAINTAINERS`](./MAINTAINERS),
    [`DCO`](./DCO), [`LICENSE`](./LICENSE)).
  - Implementation roadmap and status tracker ([`TODO`](./TODO)).
- **Build Infrastructure & Tooling**:
  - Root [`Makefile`](./Makefile) with targets for `tidy`, `lint`, `sec`,
    `vuln`, `test`, `build`, and pre-commit `check`.
  - Go module initialized with `gotcode.org/crucible` targeting Go 1.24.4
    ([`go.mod`](./go.mod)).
  - Hardened static analysis configuration
    ([`.golangci.yml`](./.golangci.yml)).
  - Root-anchored [`.gitignore`](./.gitignore) protecting against build
    artifacts, logs, and temporary files.
- **Entrypoints**:
  - Base `main.go` entrypoints for `crucibled` server daemon
    ([`cmd/crucibled/main.go`](./cmd/crucibled/main.go)) and `cruciblectl`
    operator CLI ([`cmd/cruciblectl/main.go`](./cmd/cruciblectl/main.go)).

### Changed
- Decoupled public execution portal from operational workspace: moved the
  authenticated Operations Control Plane with command sidebar and database
  engine diagnostics from `/` to `/operator`.
- Updated navigation brand header and account dropdown links in
  [`base.html`](./web/templates/base.html) to target `/operator`.
- Updated technical blueprint in [`PLAN.md`](./PLAN.md) and
  [`TODO.md`](./TODO.md) establishing `internal/storage/` as centralized
  persistence authority, detailing multi-database dialect abstraction
  (SQLite, PostgreSQL, MySQL), and defining first-login password
  enforcement and expiry rotation policies.

### Deprecated

### Removed

### Fixed

### Security
