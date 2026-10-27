# Changelog

All notable changes to the **Crucible** project (`crucibled` server daemon and `cruciblectl` operator CLI) will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Repository Architecture & Governance**:
  - Technical blueprint specifications ([`PLAN.md`](./PLAN.md), [`ARCHITECTURE.md`](./ARCHITECTURE.md)).
  - Contributor and AI agent guidelines with strict commit and signing guardrails ([`AGENTS.md`](./AGENTS.md), [`CONTRIBUTING.md`](./CONTRIBUTING.md)).
  - Open source governance policies ([`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md), [`SECURITY.md`](./SECURITY.md), [`MAINTAINERS`](./MAINTAINERS), [`DCO`](./DCO), [`LICENSE`](./LICENSE)).
  - Implementation roadmap and status tracker ([`TODO`](./TODO)).
- **Build Infrastructure & Tooling**:
  - Root [`Makefile`](./Makefile) with targets for `tidy`, `lint`, `sec`, `vuln`, `test`, `build`, and pre-commit `check`.
  - Go module initialized with `gotcode.org/crucible` targeting Go 1.24.4 ([`go.mod`](./go.mod)).
  - Hardened static analysis configuration ([`.golangci.yml`](./.golangci.yml)).
  - Root-anchored [`.gitignore`](./.gitignore) protecting against build artifacts, logs, and temporary files.
- **Entrypoints**:
  - Base `main.go` entrypoints for `crucibled` server daemon ([`cmd/crucibled/main.go`](./cmd/crucibled/main.go)) and `cruciblectl` operator CLI ([`cmd/cruciblectl/main.go`](./cmd/cruciblectl/main.go)).

### Changed

### Deprecated

### Removed

### Fixed

### Security
