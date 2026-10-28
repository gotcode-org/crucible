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
  - Pure Go SQLite Persistence Subsystem
    ([`internal/storage/`](./internal/storage/)) utilizing `modernc.org/sqlite`
    in WAL mode with foreign key enforcement and busy timeout configuration.
  - Embedded SQL Migration Runner
    ([`internal/storage/migrate.go`](./internal/storage/migrate.go)) using Go
    `//go:embed` for external SQL scripts, transactional migrations, and
    schema version tracking in `schema_migrations`.
  - Concurrency-Safe Monotonic ULID Generator
    ([`internal/storage/ulid.go`](./internal/storage/ulid.go)) using
    `github.com/oklog/ulid/v2` with domain entity prefixes (`run_`, `tok_`,
    `dev_`, `inv_`, `sec_`, `ses_`, `aud_`, `sch_`, `ssh_`, `wrk_`, `nbc_`).
  - Database Status and Diagnostics Port
    ([`internal/storage/status.go`](./internal/storage/status.go)) providing
    integrity checking (`PRAGMA integrity_check`), WAL mode verification,
    busy timeout reporting, table counts, and migration version metadata.
  - Hexagonal CQRS Version Subsystem
    ([`internal/version/`](./internal/version/)) providing runtime build
    metadata extraction, dependency introspection, and tabular/JSON
    presentation formats.
  - Terminal UI Subsystem ([`internal/ui/`](./internal/ui/)) with Charm Lip
    Gloss styling, adaptive colors, status badges, ASCII banner, formatted
    tables, and Cobra help/usage formatters.
- **Database Schemas & Migrations**:
  - `001_initial.sql`: Initial relational schema defining 18 core tables
    with monotonic ULID primary keys and foreign key constraints.
  - `002_indexes.sql`: 14 composite covering indexes optimizing high-frequency
    lookups and status filtering across runs, tokens, and workers.
  - `003_audit_ledger.sql`: Append-only SHA-256 hash-chained immutable audit
    ledger protected by SQLite `BEFORE UPDATE` and `BEFORE DELETE` anti-tamper
    triggers.
- **Commands**:
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

### Deprecated

### Removed

### Fixed

### Security
