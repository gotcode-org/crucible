# Crucible

**A lightweight, self-hosted automation orchestrator for Ansible
playbooks and systems tasks. Engineered in pure Go for single-binary
simplicity, edge-to-datacenter scaling, and absolute operational
reliability.**

![Status](https://img.shields.io/badge/Status-In%20Development-yellow)
![Language](https://img.shields.io/badge/Language-Go%201.24+-blue)
![License](https://img.shields.io/badge/License-AGPL--3.0-green)

---

## What is Crucible?

Crucible is a sovereign, self-hosted automation orchestrator built to
execute Ansible playbooks, system scripts, and scheduled maintenance
tasks without the complexity, bloat, or heavy memory footprints of
traditional orchestration platforms (such as AWX/Tower or Jenkins).

Compiled into a pure Go binary with **zero Cgo dependencies**
(`CGO_ENABLED=0`), Crucible runs effortlessly on resource-constrained
edge hardware (e.g. Raspberry Pi) while seamlessly scaling to
distributed, multi-datacenter cluster topologies over mutual TLS
(mTLS). It embeds its own Web UI, provides a unified operator CLI,
manages isolated Python virtual environments, and directly streams
execution logs to disk without memory buffering.

> [!NOTE]
> Crucible is currently in active development. It is a clean-room
> implementation engineered specifically for sovereign infrastructure
> management, deterministic automation runs, and bulletproof security
> hygiene.

---

## Core Capabilities & Architectural Pillars

### ⚡ Two-Binary Topology (`crucibled` & `cruciblectl`)
* **`crucibled` (Server Daemon):** A single executable running the embedded
  Web UI, REST API, gRPC cluster mesh, task scheduler, and in-process
  execution engine. It multiplexes HTTP and gRPC traffic over a single port
  via `cmux` and binds a local UNIX domain socket for zero-credential
  administrative control.
* **`cruciblectl` (Operator CLI):** A scriptable command-line client
  (aliased as `c` or `cb`) for automation pipelines and administrators,
  communicating via local UNIX socket or over HTTPS with Personal Access
  Tokens.

### 🔄 Upstream Git as Single Source of Truth
* All playbooks, inventories, and variables reside in your upstream Git
  repository (e.g. Upstream Git Server).
* **P2P Commit Broadcasting:** When an operator commits changes, the
  controller immediately broadcasts the commit hash across the cluster
  over gRPC (`BroadcastCommit`), prompting workers to run an instant
  `git pull --ff-only` (<500ms propagation across the fleet).
* Workers execute tasks against local, verified worktrees, allowing
  scheduled jobs to run reliably even during upstream network outages.

### 💾 Zero-RAM Disk Log Streaming
* Playbook and subprocess output streams directly to append-only disk files:
  `var/reports/{shard}/{ulid}_{job_slug}.log`.
* Log directories are automatically sharded by monotonic ULID prefixes to
  protect filesystem inodes.
* A live broadcast hub fans out chunks to active Web and CLI viewers over
  bounded channels (`chan LogChunk, capacity 256`) with non-blocking drops,
  preventing memory bloat.
* Late-joining viewers read existing output from disk offset 0 and attach
  seamlessly to the live stream.

### 🔐 Ansible Vault Secrets Engine & Ephemeral FIFO Injection
* Specialized secrets engine dedicated to keeping Ansible Vault credentials
  safe without plaintext disk exposure.
* Passwords remain sealed at boot (encrypted at rest with AES-256-GCM +
  Argon2id) and are unsealed strictly into pinned RAM using passphrases or
  Shamir key shares.
* When executing `ansible-playbook`, Crucible creates an ephemeral POSIX FIFO
  in tmpfs (`/run/crucibled/vault-{run_ulid}.fifo` mode `0600`), streams the
  unsealed password directly through kernel pipe buffers in a goroutine,
  and immediately unlinks the FIFO. **Zero plaintext secrets touch
  persistent storage.**

### 📧 Outlook-Safe Notifications (Publish-Then-Link)
* Playbooks never send emails directly; they publish rich HTML reports to
  disk.
* Crucible sends clean, minimalist transactional notification emails
  engineered with bulletproof MSO table templates (`<!--[if mso]>`)
  tested for 100% rendering fidelity across Classic Outlook, New Outlook,
  Apple Mail, and mobile clients.
* Notifications feature a secure action button linking directly to the
  hosted report on Crucible, complete with HMAC-signed view tokens for
  private jobs.
* Employs a 3-tier recipient hierarchy: Task-level overrides -> Inventory
  group targets -> Global fallback.

### 🛠️ Worker Fleet Telemetry & Drain Lifecycle
* Worker nodes support explicit availability states: `active`, `drain`, and
  `paused`.
* In `drain` mode, nodes reject new task dispatches while allowing running
  automations to finish cleanly—enabling zero-downtime host reboots and
  maintenance.
* Workers sample live Linux `/proc/stat` and `/proc/meminfo` metrics every 5
  seconds for least-loaded scheduling.
* **Split-Brain Failsafe (`ISOLATED` Mode):** If a worker or node loses
  cluster quorum, it immediately freezes all scheduled triggers to prevent
  duplicate executions.

### 🛡️ Identity, Multi-Device 2FA & Privacy Isolation
* **PAM Host Authentication:** Authenticates system accounts directly
  against the host Linux PAM stack.
* **Multi-Device TOTP:** Supports registering multiple hardware tokens,
  authenticators, and emergency keys per account, complete with secure
  shareable onboarding invite URLs (`totp_invites`).
* **Personal Access Tokens (PATs):** Granular, revocable tokens for CI/CD
  and CLI automation.
* **Public vs. Private Visibility:** Unauthenticated users can view
  `visibility: public` runs on the public dashboard. Requests to private
  runs from unauthenticated users return **`404 Not Found`**
  (anti-enumeration protection).

### 🗄️ Pure Go Storage Engine & Monotonic ULIDs
* Embedded SQLite storage via `modernc.org/sqlite` in WAL mode
  (`PRAGMA journal_mode=WAL;`).
* **Strict SQL Separation:** Zero SQL schema or table creation strings in Go
  source code. All migrations and DDL statements reside exclusively in
  external raw `.sql` files (`internal/storage/schema/*.sql`).
* **Monotonic ULIDs:** 26-character time-ordered identifiers
  (`github.com/oklog/ulid/v2`) across all primary keys (`run_...`,
  `tok_...`, `sec_...`), preventing B-tree page fragmentation and
  protecting SD card flash storage on edge hardware.

---

## Architecture & Governance

Crucible is built with strict engineering discipline and open-source
governance. For detailed specifications, consult the project documentation:

* [`ARCHITECTURE.md`](./ARCHITECTURE.md) — Comprehensive architectural
  diagrams, subsystem boundaries, and data flows.
* [`PLAN.md`](./PLAN.md) — Full 21-section technical blueprints and systems
  specifications.
* [`TODO.md`](./TODO.md) — Phase-by-phase implementation checklist and
  roadmap.
* [`AGENTS.md`](./AGENTS.md) — Contributor guidelines, flat
  package-by-feature layout, and signed PR tag workflow.
* [`CONTRIBUTING.md`](./CONTRIBUTING.md) — DCO requirements, SSH signing
  setup, and contribution standards.
* [`SECURITY.md`](./SECURITY.md) — Vulnerability disclosure process and
  operational security recommendations.
* [`CHANGELOG.md`](./CHANGELOG.md) — Release notes and revision history.
* [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md) — Contributor Covenant v2.1
  standards.
* [`DCO`](./DCO) — Developer Certificate of Origin v1.1.

---

## License

Crucible is licensed under the
**GNU Affero General Public License v3 or later** ([`LICENSE`](./LICENSE)).

```
Copyright (c) 2026 The GotCode Collective
See the LICENSE file in the project root for full license details.
```
