# Crucible Architecture

This document outlines the high-level architecture, subsystem boundaries, and
core engineering philosophies behind **Crucible** (`crucibled` server daemon
and `cruciblectl` operator CLI). Understanding these patterns is strictly
required to preserve Crucible's performance, memory safety, cryptographic
isolation, and edge-to-datacenter portability.

---

## 1. Upstream Git as Single Source of Truth & P2P Broadcast

Crucible rejects central proprietary database silos for playbook
configurations, inventories, and play variables. The upstream Git repository is
the **absolute, immutable Source of Truth**.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                UPSTREAM GIT & P2P COMMIT BROADCAST TOPOLOGY                 │
│                                                                             │
│                            ┌─────────────────────┐                          │
│                            │ Upstream Git Server │                          │
│                            │ (e.g. Git Service)  │                          │
│                            └──────────┬──────────┘                          │
│                                       │ git push / webhook                  │
│                                       ▼                                     │
│                           ┌───────────────────────┐                         │
│                           │   Crucible Manager    │                         │
│                           │     (Controller)      │                         │
│                           └───────────┬───────────┘                         │
│                                       │                                     │
│                   gRPC BroadcastCommit│(<500ms push signal)                 │
│             ┌─────────────────────────┼─────────────────────────┐           │
│             ▼                         ▼                         ▼           │
│  ┌─────────────────────┐   ┌─────────────────────┐   ┌─────────────────────┐│
│  │   Worker Node 01    │   │   Worker Node 02    │   │   Worker Node 03    ││
│  │ (git pull --ff-only)│   │ (git pull --ff-only)│   │ (git pull --ff-only)││
│  └──────────┬──────────┘   └──────────┬──────────┘   └──────────┬──────────┘│
│             ▼                         ▼                         ▼           │
│  ┌─────────────────────┐   ┌─────────────────────┐   ┌─────────────────────┐│
│  │  Local Worktree 01  │   │  Local Worktree 02  │   │  Local Worktree 03  ││
│  └─────────────────────┘   └─────────────────────┘   └─────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
```

### Core Tenets:
1. **P2P Commit Broadcasting:** When an operator commits changes via the
   UI/CLI, the controller immediately broadcasts the new commit SHA to all
   cluster workers over gRPC (`BroadcastCommit`). Workers execute an instant
   `git pull --ff-only` (<500ms propagation across the fleet).
2. **5-Minute Heartbeat Sync Fallback:** A background timer guarantees
   reconciliation even if temporary network partitions interrupt real-time gRPC
   broadcasts.
3. **Local Read Worktrees:** Every node executes playbooks against its local,
   verified Git worktree. Disconnected worker nodes can continue running
   scheduled jobs against the local repository copy even during upstream Git
   outages.

---

## 2. Two-Binary Topology (`crucibled` & `cruciblectl`)

Crucible compiles into exactly two statically-linked, zero-Cgo
(`CGO_ENABLED=0`) binaries:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             TWO-BINARY TOPOLOGY                             │
│                                                                             │
│  OPERATOR CLI TOOL                            SERVER DAEMON                 │
│  ┌───────────────────────┐                    ┌───────────────────────────┐ │
│  │      cruciblectl      │                    │         crucibled         │ │
│  │ (Alias: 'c' or 'cb')  │                    │                           │ │
│  └───────────┬───────────┘                    │ ┌───────────────────────┐ │ │
│              │                                │ │ cmux Port-Mux (8443)  │ │ │
│              ├──── UNIX Socket (Local) ──────►│ │ ├─ HTTP (Web UI)      │ │ │
│              │     (/run/crucibled/cb.sock)   │ │ ├─ REST (/api/v1/*)   │ │ │
│              │                                │ │ └─ gRPC (Cluster Mesh)│ │ │
│              └──── HTTPS/REST + PAT ─────────►│ └───────────────────────┘ │ │
│                                               │ ┌───────────────────────┐ │ │
│                                               │ │ Local Worker Agent    │ │ │
│                                               │ └───────────────────────┘ │ │
│                                               └───────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

1. **`crucibled` (The Daemon):**
   - The central orchestrator and worker agent in a single executable.
   - Embeds the Web UI templates and static assets via Go `//go:embed`.
   - Uses `cmux` on a single TCP port (default: `8443`) to multiplex HTTP/1.1
     (browser UI & REST API) and HTTP/2 gRPC (cluster mesh & remote CLI).
   - Listens on a local UNIX domain socket (`/run/crucibled/crucible.sock`,
     mode `0660`) for secure, local, zero-credential CLI administration.
2. **`cruciblectl` (The Operator CLI):**
   - Fast, scriptable command-line client for automation pipelines, cron
     triggers, and administrators.
   - Connects seamlessly via local UNIX socket or over HTTPS with Personal
     Access Tokens (PATs).
   - Strictly contains **zero business logic**—functions exclusively as an
     adapter translating CLI input into daemon commands and queries.

---

## 3. Flat Package-by-Feature Architecture & CQRS

Crucible avoids deep horizontal layer soup (`domain/`, `application/`,
`ports/`, `infrastructure/`). Every capability is organized as a
self-contained, flat feature package located directly within `internal/`:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       FLAT FEATURE PACKAGE STRUCTURE                        │
│                                                                             │
│  cmd/crucibled/             cmd/cruciblectl/           web/                 │
│  (Daemon & DI Wiring)       (Operator CLI)             (Embedded Web UI)    │
│           │                        │                          │             │
│           └────────────────────────┼──────────────────────────┘             │
│                                    ▼                                        │
│ ┌─────────────────────────────────────────────────────────────────────────┐ │
│ │                     SELF-CONTAINED FEATURE PACKAGES                     │ │
│ │                                                                         │ │
│ │  internal/ansible/   internal/git/       internal/cluster/              │ │
│ │  internal/engine/    internal/reports/   internal/secrets/              │ │
│ │  internal/notify/    internal/auth/      internal/tokens/               │ │
│ │  internal/ssh/       internal/tls/       internal/schedule/             │ │
│ │  internal/storage/   internal/metrics/   internal/events/               │ │
│ │  internal/id/        internal/version/   internal/config/               │ │
│ └─────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Standard Internal Feature Layout
Every feature directory adheres strictly to this internal file separation:

```
internal/<feature>/
├── models.go       # Structs, state definitions, validation methods
├── ports.go        # Go interfaces for storage & service dependencies
├── commands.go     # CQRS command handlers (writes, state mutations)
├── queries.go      # CQRS query handlers (reads, non-mutating)
├── adapter_*.go    # Port adapters (e.g. SQLite queries, external APIs)
└── <feature>_test.go # Table-driven unit and integration tests
```

### Architectural Constraints:
- **Zero Raw SQL in Go Code:** All database tables, indexes, and migrations
  reside exclusively in external raw `.sql` files
  (`internal/storage/schema/*.sql`). Go files only execute prepared statements
  through `internal/storage/`.
- **Interface Segregation:** Feature packages declare the dependencies they
  need via `ports.go`. Outer layers satisfy those interfaces at startup.
- **CQRS Separation:** Handlers mutating state (`commands.go`) never return
  large read datasets; handlers querying state (`queries.go`) are strictly
  read-only and never trigger side effects.

---

## 4. Zero-RAM Disk Log Streaming Subsystem

Automation runs (especially large Ansible runs) produce tens of thousands of
log lines. Storing complete logs in daemon RAM causes memory exhaustion, GC
pressure, and crashes on edge hardware (e.g., Raspberry Pi nodes).

Crucible implements an append-only, disk-first streaming pipeline:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    ZERO-RAM DISK LOG STREAMING PIPELINE                     │
│                                                                             │
│   ┌─────────────────────────────────────────────┐                           │
│   │ Subprocess Runner (ansible-playbook / bash) │                           │
│   │ Stdout & Stderr Pipes                       │                           │
│   └──────────────────────┬──────────────────────┘                           │
│                          │ io.Copy Chunk Stream                             │
│                          ▼                                                  │
│   ┌─────────────────────────────────────────────┐                           │
│   │              Crucible Log Tee               │                           │
│   └──────────────┬──────────────────────────────┘                           │
│                  │                                                          │
│       1. Sync Write to Disk                 2. Live Non-Blocking Fan-Out    │
│                  │                                       │                  │
│                  ▼                                       ▼                  │
│   ┌─────────────────────────────┐         ┌─────────────────────────────┐   │
│   │   Append-Only Log on Disk   │         │     Log Broadcaster Hub     │   │
│   │   var/reports/{shard}/      │         │  (chan LogChunk, cap 256)   │   │
│   │   {ulid}_{job_slug}.log     │         └──────────────┬──────────────┘   │
│   └──────────────┬──────────────┘                        │                  │
│                  │ Read Offset 0 to EOF                  ▼                  │
│                  │ (Catch-Up Stream)       ┌─────────────┬─────────────┐    │
│                  └────────────────────────►│ Web Viewer  │ CLI Viewer  │    │
│                                            │  (SSE/WS)   │ (gRPC/Stream│    │
│                                            └─────────────┴─────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

1. **ULID Prefix Sharding:** Logs are written directly to
   `var/reports/{shard}/{run_ulid}_{job_slug}.log`, where `{shard}` is derived
   from the first 4 characters of the monotonic ULID (`ulid[0:2]/ulid[2:4]`).
   This prevents directories from exceeding filesystem inode thresholds.
2. **Bounded In-Memory Broadcast Channels:** The broadcaster fans out to
   connected live viewers over channels with capacity 256. If a client stalls,
   chunks are dropped (`select default`) to guarantee zero memory bloat.
3. **Seamless Catch-Up for Late Joiners:** Viewers joining mid-run read
   existing logs sequentially from disk offset 0 up to current file length,
   then seamlessly attach to the live channel for real-time updates.

---

## 5. Ansible Vault Secrets Engine & Ephemeral FIFO Injection

Crucible includes an integrated secrets engine specifically tailored for
Ansible Vault credential management. It is not an arbitrary cloud KMS—it solves
the exact challenge of keeping Ansible Vault passwords encrypted at rest while
preventing plaintext leakage to persistent storage.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    EPHEMERAL POSIX FIFO VAULT INJECTION                     │
│                                                                             │
│  1. SYSTEM STARTUP & UNSEAL                                                 │
│  ┌──────────────────────────┐                                               │
│  │  Disk Storage (SQLite)   │ ──(AES-256-GCM + Argon2id)──► Sealed at Boot  │
│  └──────────────────────────┘                                     │         │
│                                    Operator Passphrase            ▼         │
│                                    ──────────────────────► RAM (Unsealed)   │
│                                                                             │
│  2. JOB EXECUTION LIFECYCLE                                                 │
│                                                                             │
│  Crucible Engine:                                                           │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │ 1. Create POSIX FIFO in tmpfs (Mode 0600):                             │ │
│  │    /run/crucibled/vault-{run_ulid}.fifo                                │ │
│  │ 2. Launch goroutine waiting on FIFO open()                             │ │
│  │ 3. Execute: ansible-playbook --vault-password-file                     │ │
│  │    /run/crucibled/vault-{run_ulid}.fifo                                │ │
│  └───────────────────────────────────┬────────────────────────────────────┘ │
│                                      │                                      │
│                                      ▼                                      │
│  Child Subprocess (Ansible):                                                │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │ Opens /run/crucibled/vault-{run_ulid}.fifo for read                    │ │
│  └───────────────────────────────────┬────────────────────────────────────┘ │
│                                      │                                      │
│                                      ▼                                      │
│  Injector Goroutine:                                                        │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │ Writes unsealed password into FIFO, closes write descriptor,           │ │
│  │ and immediately unlinks FIFO.                                          │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                             │
│  RESULT: Password passes via pipe buffers. ZERO plaintext touches disk.     │
└─────────────────────────────────────────────────────────────────────────────┘
```

- **Sealed State at Boot:** Passwords cannot be decrypted without an explicit
  operator unseal operation (single passphrase or Shamir key shares).
- **RAM-Only Decryption:** Unsealed keys reside solely in pinned memory.
- **POSIX FIFO Streaming:** The FIFO is created in `tmpfs` mode `0600` and
  unlinked immediately after Ansible reads the byte stream. If the process is
  killed or panics, no plaintext remains on disk.

---

## 6. Outlook-Safe Hierarchical Notifications (Publish-Then-Link)

Because Microsoft Outlook (both the legacy Word rendering engine in Classic
Outlook and modern sandboxing in New Outlook) strips complex CSS, JavaScript,
and dynamic HTML, attempting to embed rich HTML reports directly into
notification emails results in broken, unreadable formatting.

Crucible enforces a **Publish-Then-Link** notification architecture:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      PUBLISH-THEN-LINK NOTIFICATION FLOW                    │
│                                                                             │
│  1. ANSIBLE PLAYBOOK RUNS                                                   │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │ Playbook generates clean, rich HTML report:                            │ │
│  │ $CRUCIBLE_REPORT_DIR/${CRUCIBLE_RUN_ID}_${JOB_SLUG}.html               │ │
│  └───────────────────────────────────┬────────────────────────────────────┘ │
│                                      │                                      │
│                                      ▼                                      │
│  2. CRUCIBLE PUBLISHES & CHECKS RECIPIENTS                                  │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │ • Verifies published report on disk                                    │ │
│  │ • Resolves 3-Tier Recipient Hierarchy:                                 │ │
│  │     1. Task-Level Config (notify: { to: [...] })                       │ │
│  │     2. Inventory Group Config (group_vars/db.yml -> notify_team)       │ │
│  │     3. Global Server Config (crucibled.yml fallback)                   │ │
│  └───────────────────────────────────┬────────────────────────────────────┘ │
│                                      │                                      │
│                                      ▼                                      │
│  3. BULLETPROOF MSO TABLE NOTIFICATION DISPATCHED                           │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │ • Clean MSO table (<!--[if mso]>) rendered to SMTP recipients          │ │
│  │ • Includes summary metrics (Tasks, Changed, Failed, Runtime)           │ │
│  │ • Generates secure deep-link button:                                   │ │
│  │   [View Report] -> https://crucible.internal/reports/01JBC...          │ │
│  │   (Includes HMAC-signed view token for private job runs)               │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Multi-Team Routing Matrix
Crucible routes reports based on inventory groups and task triggers:
- **Database Teams:** Receive notifications exclusively for database group
  plays.
- **SRE & Infrastructure Teams:** Receive immediate failure alerts across all
  jobs.
- **Private Run Tokens:** When a run has `visibility: private`, Crucible
  generates an expiring HMAC-SHA256 URL token embedded directly into the
  notification button so authorized recipients can access the report without
  logging in first.

---

## 7. Cluster Federation, Worker Telemetry & Drain Lifecycle

Crucible clusters operate over an encrypted, authenticated gRPC mesh using
mutual TLS (mTLS). Nodes can be designated as **Managers** (controllers) or
**Workers** (execution agents).

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      WORKER NODE AVAILABILITY LIFECYCLE                     │
│                                                                             │
│                  ┌───────────────────────────────────────┐                  │
│                  │                ACTIVE                 │                  │
│                  │  (Receives scheduled & ad-hoc jobs)   │                  │
│                  └───────┬───────────────────────▲───────┘                  │
│                          │                       │                          │
│       Operator Sets Drain│                       │ Operator Resumes         │
│                          ▼                       │                          │
│                  ┌───────────────────────────────┴───────┐                  │
│                  │                 DRAIN                 │                  │
│                  │ • Refuses all new job dispatches      │                  │
│                  │ • Running jobs finish gracefully      │                  │
│                  │ • Ready for maintenance/reboot        │                  │
│                  └───────┬───────────────────────────────┘                  │
│                          │                                                  │
│                          │ All running jobs complete                        │
│                          ▼                                                  │
│                  ┌───────────────────────────────────────┐                  │
│                  │                PAUSED                 │                  │
│                  │ (Idle: safe for upgrades and reboots) │                  │
│                  └───────────────────────────────────────┘                  │
│                                                                             │
│  Watchdog & Failsafe States:                                                │
│  • DOWN:     Node missed 3 heartbeats (tasks failed over).                  │
│  • ISOLATED: Node detects network partition from quorum; freezes all        │
│              scheduled triggers to prevent split-brain runs.                │
└─────────────────────────────────────────────────────────────────────────────┘
```

- **Native Linux Telemetry:** Workers collect live CPU utilization,
  1/5/15-minute load averages, and free memory directly from `/proc/stat` and
  `/proc/meminfo` every 5 seconds. Managers use these metrics for real-time,
  least-loaded worker scheduling.
- **Graceful Drain Mode:** Operators trigger `cruciblectl worker drain
  <node_id>` to service hardware without aborting running production
  automation.

---

## 8. Identity, Access & Public/Private Job Isolation

Crucible balances single-sign-on enterprise security with zero-maintenance edge
operation.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          IDENTITY & ACCESS CONTROL                          │
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Local PAM Host Authentication                    │   │
│   │ (Authenticates system accounts via PAM: root, wheel, or ops group)  │   │
│   └──────────────────────────────────┬──────────────────────────────────┘   │
│                                      │                                      │
│                                      ▼                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Multi-Device TOTP 2FA per User                   │   │
│   │ • Users register multiple hardware tokens, phones, or emergency keys│   │
│   │ • Secure shareable onboarding URLs (totp_invites) for team access   │   │
│   └──────────────────────────────────┬──────────────────────────────────┘   │
│                                      │                                      │
│                                      ▼                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │             Personal Access Tokens (PATs) for Automation            │   │
│   │ • Time-ordered ULID IDs (tok_01JBC...)                              │   │
│   │ • Scoped permissions: runs:read, runs:trigger, workers:manage,      │   │
│   │   secrets:unseal                                                    │   │
│   │ • Instant multi-token revocation per user                           │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Public vs. Private Visibility Isolation
To enable safe hosting across internal teams and external viewers:
- **`visibility: public`:** Runs, reports, and real-time logs can be viewed by
  unauthenticated anonymous dashboard visitors.
- **`visibility: private` (Default):** Runs and reports require authenticated
  sessions or a signed view token.
- **Anti-Enumeration 404:** Requests to private run endpoints
  (`/runs/{private_ulid}`) from anonymous users return **`404 Not Found`**
  rather than `401 Unauthorized` or `403 Forbidden`. This completely prevents
  attackers or unauthorized users from enumerating valid job IDs.

---

## 9. Hexagonal Storage Engine & Monotonic ULID Identity

Crucible maintains a strict separation between domain identity (`internal/id`)
and persistence adapters (`internal/storage`):

1. **Domain Entity Identity (`internal/id`):**
   Generates concurrency-safe, monotonic 26-character ULIDs with domain prefixes
   (`run_`, `tok_`, `dev_`, `aud_`, `sec_`, etc.). Entity identity is minted
   in the domain layer, keeping domain packages completely decoupled from
   database drivers.

2. **Hexagonal Persistence Subsystem (`internal/storage`):**
   - **Outbound `Engine` Port:** Provides a database-agnostic interface (`Ping`,
     `Migrate`, `Status`, `AppliedMigrations`, `WithinTransaction`, `Close`).
   - **Inbound CQRS Ports:** `CommandHandler` (`RunMigrations`) and
     `QueryHandler` (`GetDatabaseStatus`, `GetAppliedMigrations`).
   - **Driven Adapters:** `adapter_sqlite.go` (pure Go SQLite via
     `modernc.org/sqlite` with `CGO_ENABLED=0` in WAL mode), with architecture
     ready for future `adapter_postgres.go` and `adapter_mysql.go` backends.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        PURE GO SQLITE STORAGE & ULIDS                       │
│                                                                             │
│  • Write-Ahead Logging (WAL) Mode: Concurrent reads while writing           │
│  • Foreign Keys Enforced: PRAGMA foreign_keys = ON;                         │
│  • Zero SQL In Go Source: Migrations in internal/storage/schema/*.sql       │
│                                                                             │
│  Monotonic 26-Character ULIDs (Zero UUIDs):                                 │
│                                                                             │
│      10 Bytes (48 bits) Timestamp        16 Bytes (80 bits) Random          │
│    ┌───────────────────────────────┐   ┌─────────────────────────────────┐  │
│    │      0 1 J B C X Y Z 8 9      │   │   A B C D E F G H J K M N P     │  │
│    └───────────────────────────────┘   └─────────────────────────────────┘  │
│                                                                             │
│    Advantages:                                                              │
│    1. Naturally time-ordered -> Zero B-tree page splits on insert           │
│    2. Low disk write amplification -> Protects flash storage & SD cards     │
│    3. URL-safe Crockford Base32 -> Clean, human-readable CLI arguments      │
│    4. Subsystem prefixes for immediate recognition:                         │
│       • run_01JBC... (Task Run)                                             │
│       • tok_01JBC... (Personal Access Token)                                │
│       • sec_01JBC... (Vault Secret)                                         │
│       • dev_01JBC... (TOTP Device)                                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 10. Summary Checklist for Contributors

When writing code or submitting patches:
1. **Source Headers:** Ensure the mandatory 3-line GotCode Collective AGPL-3.0
   header is present on every source file.
2. **Flat Layout:** Keep all feature code inside `internal/<feature>/`. Do not
   introduce horizontal layers.
3. **Database Schema:** Put all SQL queries in `internal/storage/` and all
   table schemas in `internal/storage/schema/*.sql`.
4. **Log Streaming:** Never buffer full subprocess outputs in memory. Stream
   directly to disk and fan out via bounded channels.
5. **Vault Hygiene:** Decrypt secrets exclusively into RAM; inject into child
   processes strictly via ephemeral tmpfs FIFOs.
6. **No AI Trailers:** Ensure no `Co-authored-by:` or `Reviewed-by:` trailers
   appear in commits or pull requests.
7. **Signoff, Squashed History & Signed Tags:** Work on any local branch name
   in your fork, strip intermediate commits by squashing against origin/main,
   and merge features into `main` strictly via SSH-signed annotated tags (`git
   tag -s` and squash integration via `scripts/merge.sh`). Zero scratch commits
   enter mainline.
8. **Comprehensive Testing (Feature & CLI):** Write table-driven unit tests for
   all feature packages and buffer tests for all Cobra CLI commands.
9. **Concurrency & Race Testing:** Test all shared state, worker pools, and
   channel fan-outs with `go test -race ./...`. Document mutex scope and lock
   invariants.
10. **Style, Comments & Linting:** Format with `gofmt -s`, pass `golangci-lint
    run ./...` with zero warnings, and provide clear Go docstrings on all
    exported symbols explaining design rationale.
11. **Security Architecture & Secure Coding:** Code strictly as a Senior
    Security Architect: enforce zero-trust inputs, bound all inbound readers
    (`http.MaxBytesReader`), validate canonicalized paths (`filepath.Clean`),
    eliminate command injection (never execute via `sh -c`), use constant-time
    cryptographic comparisons (`crypto/subtle.ConstantTimeCompare`), securely
    zero-out memory buffers holding sensitive keys, and enforce
    anti-enumeration 404 responses for private runs.

