# Crucible (`crucibled` / `cruciblectl`) — Architecture & Implementation Plan

Crucible is a lightweight, single-binary, self-hosted job orchestrator
engineered to run **Ansible playbooks** and arbitrary **CLI scripts** (Bash,
Python, binaries) across single servers, local networks, or **federated
multi-datacenter clusters**. It is built from the ground up for operational
simplicity on Linux and resource-constrained environments like the **Raspberry
Pi**, integrating cleanly with upstream Git repositories and notifications.

---

## 1. System Vision & Design Goals

* **Single-Daemon Multi-Mode (`crucibled`):** A single binary that operates in
  three modes:
  * `standalone` (Default): All-in-one controller, SQLite storage, scheduler,
    local process runner, and embedded Web UI.
  * `controller`: Central control plane, dashboard, scheduler, and
    multi-datacenter job dispatcher. Can run in **Converged** mode (manager +
    local runner) or **Dedicated Control Plane**
    (`--enable-local-runner=false`).
  * `worker`: Lightweight, zero-state execution daemon running in remote
    datacenters or on edge devices (Raspberry Pis) that dials home to the
    controller.
* **Worker Availability & Maintenance Lifecycle (Drain / Active / Pause):**
  * Inspired by Docker Swarm (`docker node update --availability drain`) and
    Kubernetes cordon/drain.
  * **`drain` Mode**: Operators can mark nodes for maintenance (MX), kernel
    upgrades, or hardware reboots. In `drain` mode, the scheduler **stops
    dispatching new jobs** to that node, while allowing in-flight jobs to
    complete gracefully before the machine is taken offline.
  * **Automated Health Watchdog**: The manager monitors heartbeat telemetry; if
    a node goes `DOWN` due to a hardware, network, or power failure, the
    scheduler immediately stops routing work to it and fails/reroutes pending
    jobs.
* **Split-Brain Prevention & Failsafe Execution Freeze:**
  * When peers or workers lose connection to the central manager or peer
    quorum, they enter an **`ISOLATED`** state.
  * In the `ISOLATED` state, **all new job executions and scheduled cron
    triggers are immediately frozen**, preventing dual-execution or conflicting
    infrastructure changes during network partitions.
  * The Web UI remains functional in read-only mode with clear visual
    indicators until connectivity is re-established.
* **Granular Multi-Token Authentication for CLI & Automation:**
  * Users and service accounts can generate **multiple independent Personal
    Access Tokens (PATs)** (e.g., `laptop-cli`, `workstation-cli`,
    `ci-cd-pipeline`, `backup-agent`).
  * High-entropy tokens (`cb_pat_<32-byte-hex>`) stored as SHA-256 hashes.
    Plaintext is shown only once at creation.
  * **Independent Single-Token Revocation**: Easily revoke a single compromised
    or retired token without disrupting other devices, sessions, or services.
  * Telemetry tracking for every token: `last_used_at`, `last_used_ip`, and
    scoped roles.
* **Peer-to-Peer State Replication & Multi-Server Redundancy:**
  * Multiple `crucibled` controllers (e.g. Server A in NYC, Server B in
    Frankfurt) peer with each other over gRPC.
  * Pipeline configurations, schedules, active runs, and execution history are
    replicated automatically between servers.
* **Strict "Exactly-Once" Distributed Execution (Distributed Leases &
  Fencing):**
  * When a scheduled job arrives, both servers know about it, but a
    **distributed lease mechanism with fencing tokens** guarantees the job
    executes **only once** across the cluster.
  * If the executing server crashes mid-job, the lease expires and the peer
    safely takes over without duplicate runs or split-brain.
* **Public Read-Only UI with Job-Level Visibility (Public vs. Private):**
  * **Public Runs & Reports**: Operators can mark jobs and pipelines as
    `visibility: public` or `visibility: private` (default). Anonymous,
    unauthenticated visitors can view the public dashboard, tail live execution
    logs, and inspect generated HTML reports for all `public` jobs without
    logging in.
  * **Private Runs Protection**: Private automation tasks, inventories,
    host/group variables, cluster node management, SSH keys, secrets vault, and
    audit logs are strictly invisible to anonymous visitors (returning 404 Not
    Found to prevent information disclosure).
  * **Authenticated Operators / Admins**: Secure login unlocking action buttons
    to trigger ad-hoc runs, configure recurring schedules, cancel active
    executions, edit inventories, and manage cluster nodes.
* **Crucible-Style Zero-RAM Log Streaming to Disk & Sharded HTML Reports:**
  * Subprocess stdout and stderr stream directly into append-only disk log
    files (`var/reports/{shard}/{ulid}_{slug}.log`) sharded chronologically by
    the first 4 characters of the run ULID.
  * Fan-out `LogBroadcaster` dispatches live chunks to web viewers
    (SSE/xterm.js) and disk simultaneously without retaining massive execution
    logs in memory, preventing daemon OOM crashes during heavy Ansible runs.
  * Automatic discovery and serving of pipeline-generated HTML test and
    compliance reports with visibility gatekeeping.
* **Hierarchical Email Notifications & Granular Report Dispatching:**
  * Multi-tier notification rules declared at the pipeline root, cluster/group,
    or individual task level.
  * Target specific teams selectively: e.g. Team A receives DB groups 1 & 2
    reports, Teams A & B receive Web groups 3, 4, 5 reports.
  * Flexible delivery: direct clickable links to web reports (with signed
    one-time view tokens for private runs), attached HTML/log artifacts, or
    both.
  * Automated dispatch via the event bus (`on_start`, `on_failure`,
    `on_success`, `always`) or manual on-demand trigger via Web UI ("Email
    Report" modal) and CLI (`cruciblectl notify send`).
* **Secrets Engine & HashiCorp Vault-Style Seal / Unseal Architecture:**
  * Integrated secrets vault (`internal/secrets/`) with envelope encryption
    (AES-256-GCM + Argon2id KDF).
  * Explicit **`SEALED`** and **`UNSEALED`** state machine. On daemon boot, the
    vault is sealed; operator unseals via CLI (`cruciblectl vault unseal`) or
    Web UI using a passphrase or Shamir's Secret Shares (or configurable
    auto-unseal keyfile/env for automated reboots).
  * In-memory master key safely injects Ansible Vault passwords into
    `ansible-playbook` via pure-Go ephemeral FIFOs (named pipes) or IPC socket
    password clients, guaranteeing ZERO plaintext passwords on non-volatile
    disk.
* **Two-Factor Linux PAM Authentication with Multi-Device TOTP & Invite
  Links:**
  * **Factor 1 (Identity)**: Linux PAM verification against `/etc/shadow` /
    `/etc/pam.d/login` using standard system accounts.
  * **Factor 2 (2FA / MFA)**: RFC 6238 Time-based One-Time Password (TOTP)
    supporting **multiple 2FA tokens per user** (e.g. shared accounts like
    `root` where multiple team members register their own authenticator app, or
    users with both phone and hardware keys).
  * **Secure TOTP Invite URLs**: Primary operator can generate an ephemeral,
    single-use invite URL (`/enroll/2fa?invite=<ulid>`) allowing additional
    sysadmins to enroll their own authenticator device for a shared account
    (like `root`) securely without sharing secrets.
  * Validates against any active registered device; displays device labels and
    last used timestamps.
  * **POSIX Group Mapping**: Automatically maps Linux/PAM groups (e.g. `wheel`,
    `sudo`, `operators`) directly to Crucible RBAC roles.
* **ULID (Universally Unique Lexicographically Sortable Identifier) Standard:**
  * Replaces legacy UUIDs with 26-character, time-ordered, URL-safe **ULID**
    strings across all identifiers (runs, tokens, devices, invites, keys,
    sessions).
  * Monotonically ordered inserts drastically improve SQLite B-tree write
    performance and reduce flash memory wear on Raspberry Pi SD cards.
* **Native HTTPS & Automated Let's Encrypt ACME:**
  * Built-in TLS server supporting custom certificates (`.crt`/`.key`).
  * Automated Let's Encrypt certificate issuance supporting **TLS-ALPN-01**
    (pure port 443, zero port 80 required) and **DNS-01** challenges (for
    internal homelab RFC 1918 networks, CGNAT, and wildcard domains).
* **Python Virtual Environment (`venv`) Execution Engine:**
  * Support running Ansible inside a dedicated Python `venv` (globally via
    `--ansible-venv` or per-pipeline `venv: path`).
  * Automatic environment injection (`PATH`, `VIRTUAL_ENV`) isolating Ansible
    collections and dependencies (`pynetbox`, `jmespath`) from host packages.
* **NetBox Dynamic Inventory Plugin & Local Inventory Dual Support:**
  * First-class integration with NetBox as an authoritative infrastructure
    inventory source using the official `netbox.netbox.nb_inventory` plugin.
  * Local SQLite caching provides instantaneous Web UI tree browsing and
    variable inspection for NetBox devices.
  * Seamless merging: local `group_vars/`, `host_vars/`, and custom `vars/**`
    layer directly on top of NetBox-discovered hosts.
* **Built-in SSH Key Management (Web UI & CLI):**
  * Generate, display, import, and test dedicated SSH keypairs (Ed25519 and RSA
    4096):
    * `git_sync`: Deploy key for pulling/pushing the Ansible Git repository.
    * `ansible_nodes`: Target nodes key for connecting to managed servers,
      switches, and Raspberry Pis.
* **Multi-Datacenter Job Orchestration:** The controller can dispatch pipeline
  tasks to remote workers based on datacenter tags, host labels, or environment
  requirements, while streaming all logs and status back to the central
  dashboard in real time.
* **Ergonomic Operator CLI (`cruciblectl`):** A fast, responsive command-line
  client built with Cobra and Lip Gloss (`github.com/charmbracelet/lipgloss`)
  for terminal UI styling, formatted status tables, borders, and banners for
  ad-hoc job triggers, log tailing, schedule administration, worker node
  monitoring, and token management.
* **Flat Feature Package Layout:** Modular, feature-by-feature layout directly
  under `internal/` (`internal/version/`, `internal/ui/`, `internal/ansible/`,
  `internal/git/`, `internal/cluster/`, `internal/engine/`, `internal/auth/`,
  `internal/tokens/`, `internal/ssh/`, `internal/tls/`, `internal/storage/`,
  `internal/schedule/`, `internal/metrics/`, `internal/notify/`,
  `internal/events/`). Zero layered nesting (`domain/`, `application/`,
  `transport/`, `ports/`). Each feature folder encapsulates its models,
  business logic, and store methods.
* **Automated Security & Vulnerability Pipeline:** Mandatory pre-build security
  auditing integrating `gosec` (AST source code security scanner) and
  `govulncheck` (Go vulnerability database checker) chained directly into `make
  all` before test and compilation.
* **External `.sql` Database Schema Files:** All SQLite table definitions,
  indexes, and migrations are kept strictly in external `.sql` files
  (`internal/storage/schema/*.sql`), never hardcoded as string literals in
  `.go` source files. Embedded into the binary via Go `//go:embed` for seamless
  auto-migration at startup.
* **Prometheus Observability (`/metrics` Endpoint):**
  * Native scraping endpoint exposing pipeline counters, job durations, active
    worker telemetry, cluster health, and Go runtime stats for Prometheus &
    Grafana.
* **Centralized Ansible Inventory Management (CLI & Web UI):**
  * Manage, edit, validate, and version Ansible inventories (YAML and INI
    formats) directly from the Web UI and `cruciblectl` CLI.
  * In-browser editor with syntax validation, group/host tree visualization,
    variable inspection, and instant error checking.
  * Dynamic inventory provider: Crucible automatically injects managed
    inventories into executing playbooks (`-i`) and exposes standard Ansible
    JSON dynamic inventory feeds.
  * Cluster-wide replication: Inventories synchronize automatically across peer
    nodes over the gRPC mesh.
* **Raspberry Pi Friendly:** Zero external runtime dependencies, 100% Cgo-free
  SQLite (`modernc.org/sqlite`), low idle memory footprint (<35 MB RAM), and
  minimal SD card write amplification using SQLite WAL mode with monotonic ULID
  keys.

---

## 2. Worker Node Lifecycle: Health, Availability & Drain Mode

To manage server maintenance, OS patching, hardware failures, and rolling
reboots without interrupting running automation, Crucible implements a
dual-dimensional state model for every worker node: **Health Status**
(automated) and **Availability** (operator-controlled).

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                              WORKER NODE AVAILABILITY & HEALTH MATRIX                       │
│                                                                                             │
│  HEALTH STATUS (Automated Watchdog):                                                        │
│    🟢 UP       — Heartbeat active within last 15s. Node reachable.                          │
│    🔴 DOWN     — Heartbeat missed (> 30s). Node unreachable (hardware/network failure).     │
│                                                                                             │
│  AVAILABILITY (Operator Controlled):                                                        │
│    🟢 ACTIVE   — Default. Node accepts all incoming pipeline tasks.                         │
│    🟡 DRAIN    — Maintenance mode. No new tasks scheduled. In-flight tasks finish cleanly.  │
│    ⚪ PAUSED   — Administrative freeze. No new tasks scheduled.                             │
│                                                                                             │
│  SCHEDULING ROUTING LOGIC:                                                                  │
│  Can the manager schedule a new job on this node?                                           │
│                                                                                             │
│                   HEALTH: UP                     HEALTH: DOWN                               │
│               ┌──────────────────────────────┬──────────────────────────────┐               │
│  AVAIL: ACTIVE│  ✅ YES — Dispatches task    │  ❌ NO (Node is unreachable) │               │
│  AVAIL: DRAIN │  ❌ NO (Node draining for MX)│  ❌ NO (Node down & draining)│               │
│  AVAIL: PAUSED│  ❌ NO (Node administratively│  ❌ NO (Node down & paused)  │               │
│               │         frozen)              │                              │               │
│               └──────────────────────────────┴──────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 1. The Drain Protocol (`cruciblectl worker drain <id>`)
When an operator needs to patch an OS, reboot a machine, or decommission a
Raspberry Pi:
1. Operator issues command: `cruciblectl worker drain rpi-lab-01` (or clicks
   **[ Drain ]** in the Web UI).
2. The controller dispatches `UpdateWorkerAvailabilityCommand{WorkerID:
   "rpi-lab-01", Availability: "drain"}`.
3. **Scheduler Routing**:
   * The scheduler instantly removes `rpi-lab-01` from the pool of eligible
     dispatch candidates.
   * If a pipeline arrives targeting `labels: [arm64]`, the manager routes the
     job to alternative active nodes. If none exist, the job queues or fails
     with an informative error:
     ```
     Error: no active worker available for target {labels: [arm64]} (node
'rpi-lab-01' is in DRAIN mode)
     ```
4. **Graceful Task Completion**:
   * Any Ansible playbooks or scripts already running on `rpi-lab-01` continue
     executing uninterrupted until completion.
5. **Safe to Reboot**:
   * Once `active_jobs` reaches `0`, the node status reflects `drain (idle)`.
     The operator can safely stop `crucibled`, reboot, upgrade kernel packages,
     or service hardware.
6. **Resuming Service**:
   * After reboot, the operator runs `cruciblectl worker active rpi-lab-01` (or
     clicks **[ Activate ]**). The node re-enters the active scheduling pool
     immediately.

### 2. Automated Health Watchdog (`UP` vs `DOWN`)
* Workers pulse heartbeats every 5 seconds over the gRPC stream.
* If a worker misses heartbeats for $> 30$ seconds (e.g. power supply failure,
  network switch outage):
  * The `ClusterMonitor` watchdog marks `health = DOWN`.
  * Emits `WorkerWentDownEvent`.
  * The scheduler stops assigning new jobs.
  * In-flight jobs on that dead node are transitioned to `failed` with status:
    `Node disconnected unexpectedly during execution`.
  * If notifications are configured, a webhook alert fires to **Dispatch**.

### 3. Real-Time CPU & Memory Telemetry (Sampling & Scheduling Impact)
To empower the Web UI's status/health cards and enable intelligent job
placement, workers continuously gather lightweight system telemetry:

1. **Lightweight Linux `/proc` Sampler**:
   * Pure Go, zero external dependencies, minimal CPU impact (< 0.1%).
   * Reads `/proc/stat` and `/proc/loadavg` for CPU utilization % and 1m/5m/15m
     load averages.
   * Reads `/proc/meminfo` for Total, Used, and Available RAM.
2. **Heartbeat Telemetry Payload**:
   * Every 5-second heartbeat bundles current metrics:
     ```json
     {
       "cpu_percent": 18.4,
       "load_avg": "0.35, 0.40, 0.28",
       "mem_used_mb": 1120,
       "mem_total_mb": 4096,
       "mem_percent": 27.3,
       "active_jobs": 2
     }
     ```
3. **Web Dashboard Health Cards**:
   * The Web UI renders live visual gauges (CPU load progress bars, RAM
     percentage bars, and active job counters) on each node card in
     `/cluster/workers` and `/status`.
   * Visual health indicators: 🟢 `< 70%` normal, 🟡 `70–90%` high load, 🔴 `>
     90%` critical.
4. **Intelligent "Least-Loaded" Scheduling**:
   * When multiple worker nodes match a pipeline's target labels, the scheduler
     picks the candidate with the lowest resource load (`min(cpu_percent,
     mem_percent)`).
   * **Overload Protection**: If a node exceeds configured limits (e.g.
     `max_cpu: 90%` or `min_free_mem: 256MB`), the scheduler pauses dispatching
     additional jobs to that node until load subsides.

---

## 3. Split-Brain Prevention & Failsafe Execution Interlock

In multi-datacenter setups or edge environments (like a remote Raspberry Pi),
**network partitions will happen**. If a peer server or runner is cut off from
the manager, running jobs locally on isolated assumptions leads to catastrophic
duplicate operations (e.g. two servers running database migrations or deploying
contrasting configurations simultaneously).

Crucible prevents split-brain using a **Two-Layer Safety Interlock**:

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                           CLUSTER CONNECTIVITY & SPLIT-BRAIN INTERLOCK                      │
│                                                                                             │
│                         Heartbeat Pulse to Manager / Peer Quorum                            │
│                                            │                                                │
│                      ┌─────────────────────┴─────────────────────┐                          │
│                      ▼                                           ▼                          │
│          Heartbeat OK (< 15s)                        Heartbeat Missed (> 15s)               │
│                      │                                           │                          │
│                      ▼                                           ▼                          │
│             State: HEALTHY                              State: ISOLATED                     │
│    ┌───────────────────────────────────┐       ┌───────────────────────────────────┐        │
│    │ • Full Execution Permitted        │       │ ⛔ EXECUTION SAFETY FREEZE        │        │
│    │ • Scheduler Fires On Time         │       │ • Cron Scheduler PAUSED           │        │
│    │ • Ad-hoc CLI/Web Runs Enabled     │       │ • Ad-hoc Job Triggers BLOCKED     │        │
│    │ • Lease Renewal Heartbeats Sent   │       │ • UI enters Read-Only Alert Mode  │        │
│    └───────────────────────────────────┘       └───────────────────────────────────┘        │
│                                                                  │                          │
│                                                       Connection Restored                   │
│                                                                  ▼                          │
│                                                     Sync State & Resumes HEALTHY            │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

## 4. Multi-Token Architecture for CLI & Automation

To support multiple workstations, CI/CD runners, and automated scripts without
sharing credentials, Crucible implements a GitHub-style **Personal Access Token
(PAT)** management engine:

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                            TOKEN LIFECYCLE & MULTI-DEVICE MANAGEMENT                        │
│                                                                                             │
│  User: jovens (Admin)                                                                       │
│  Active Personal Access Tokens (PATs):                                                      │
│                                                                                             │
│  [ID]      [NAME]            [PREFIX]           [ROLE]      [LAST USED]      [ACTION]       │
│  tok_01    laptop-cli        cb_pat_9f82...   admin       2 mins ago       [ Revoke ]       │
│  tok_02    workstation-cli   cb_pat_4a11...   admin       3 days ago       [ Revoke ]       │
│  tok_03    ansible-ci-runner cb_pat_c73e...   operator    1 hour ago       [ Revoke ]       │
│  tok_04    rpi-edge-agent    cb_pat_e55b...   operator    Just now         [ Revoke ]       │
│                                                                                             │
│  Scenario: "Laptop was lost or decommissioned"                                              │
│  Action: Operator clicks [ Revoke ] on tok_01 (laptop-cli)                                  │
│  Result: Only tok_01 is immediately blacklisted. Workstation, CI runner, and web sessions   │
│          continue operating with zero interruption!                                         │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Peer Replication & "Exactly-Once" Distributed Execution

```
┌──────────────────────────────────────┐                      ┌──────────────────────────────────────┐
│      crucibled (Server A - NYC)      │                      │      crucibled (Server B - FRA)      │
│                                      │                      │                                      │
│  • Local Scheduler (Cron)            │                      │  • Local Scheduler (Cron)            │
│  • SQLite Storage                    │                      │  • SQLite Storage                    │
│  • Web Dashboard                     │                      │  • Web Dashboard                     │
└──────────────────┬───────────────────┘                      └──────────────────┬───────────────────┘
                   │                                                             │
                   │           Peer-to-Peer Mesh (gRPC / TLS)                    │
                   │ ◄─────────────────────────────────────────────────────────► │
                   │                                                             │
                   │   1. Replicate State: SyncSchedules, SyncRuns, SyncTokens   │
                   │   2. Distributed Lock: AcquireLease(SlotKey, FencingToken)  │
                   ▼                                                             ▼
 ┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
 │                                 AT THE SCHEDULED TRIGGER TIME (e.g. 00:00:00)                    │
 │                                                                                                  │
 │  Deterministic Execution Slot Key: hash(ScheduleID, 2026-10-23T00:00:00Z)                        │
 │                                                                                                  │
 │  Server A wins the race:                                  Server B:                              │
 │  1. AcquireLease(SlotKey, TTL=30s) -> SUCCESS             1. AcquireLease(SlotKey) -> REJECTED   │
 │  2. Spawns Subprocess (Ansible/Bash)                      2. Observes Server A holds lease       │
 │  3. Pulses Heartbeat every 10s to extend lease            3. Yields execution (Does NOT run)     │
 │  4. Streams logs locally & replicates events              4. Receives replicated stream/events   │
 │  5. Releases lease upon completion                        5. Marks local DB record as Completed  │
 └──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 5.3 Cluster Ansible Git Repository Replication Protocol

When running Crucible across a multi-controller or manager-worker cluster,
keeping the local Ansible repository synchronized across all machines without
race conditions or merge conflicts is essential:

1. **Upstream Remote Git as Single Source of Truth**:
   * The authoritative repository lives on an upstream Git server (GitLab,
     GitHub, Gitea, or generic Git server).
   * All state-changing edits (inventories, variables, playbooks) are committed
     and pushed directly to this remote repository.
2. **P2P Real-Time Commit Broadcast (`PeerMeshService.BroadcastCommit`)**:
   * When an operator saves an inventory or variable file (or a pipeline
     updates the repository), the controller commits and executes `git push
     origin <branch>`.
   * Immediately upon successful push, the controller broadcasts a
     `BroadcastCommit(repo, branch, commit_hash)` message across the gRPC peer
     mesh to all connected controllers and workers.
   * Connected peers receive this notification and immediately invoke `git
     fetch origin && git pull --ff-only` to advance their local working tree to
     that exact commit in real time (< 500ms).
3. **Periodic Background Re-Sync (5m Timer)**:
   * If a node was temporarily offline, network partitioned, or rebooting when
     a broadcast occurred, its background sync ticker (`internal/git/sync.go`,
     default: 5m) automatically executes `git pull --ff-only` upon reconnection
     to catch up with any missed commits.
4. **Edge & Air-Gapped Worker Proxy Mode**:
   * For remote edge devices (e.g. Raspberry Pis in remote facilities) or
     workers isolated from the Internet without direct access to the Git
     upstream, `crucibled` controllers act as an internal Git mirror over the
     gRPC mesh (`FetchGitBundleStream` RPC), streaming git packfile bundles
     directly to the worker so edge devices always execute against the exact
     commit without needing independent Git credentials.

---

## 6. Authentication, 2FA & Role-Based Access Control (RBAC)

### Access Model: Anonymous vs. Authenticated & Public/Private Visibility

Crucible introduces a fine-grained visibility model that balances public
transparency with infrastructure confidentiality:
* **Job & Run Visibility (`visibility: public` vs `visibility: private`)**:
  * Every pipeline config (`tasks.yaml`) or ad-hoc run can declare its
    visibility. The default is `private`.
  * **`public`**: Accessible by anyone without authentication. Perfect for
    community-facing status pages, open-source build logs, public homelab
    status dashboards, and non-sensitive maintenance reports.
  * **`private`**: Strictly restricted to authenticated operators and admins.
    Private runs, logs, and HTML artifacts are completely hidden from anonymous
    visitors.
* **Anonymous Visitor Experience (Unauthenticated Web UI)**:
  * Visiting the root dashboard (`/`) renders the **Public Dashboard**. Only
    runs marked `visibility: public` are displayed.
  * Direct requests to private runs (`/runs/{private_ulid}`) or their log
    streams return `404 Not Found` (preventing attackers from probing the
    existence of private job IDs).
  * Management interfaces (`/inventory`, `/vars`, `/nodes`, `/ssh`, `/vault`,
    `/settings`) are completely omitted from the navigation bar. If accessed
    directly, they redirect to `/login` or return `401 Unauthorized`.
  * Top navigation bar displays: `[Crucible Logo]` `Public Runs` `[Sign In
    Button]`.
* **Authenticated User Experience (Logged in via PAM / Session Cookie / PAT)**:
  * Authenticated users see ALL runs (`public` and `private`).
  * Table rows feature prominent badges: `[Public]` (blue) or `[Private]`
    (neutral gray).
  * Authenticated operators can toggle a run's visibility post-execution
    (`cruciblectl run set-visibility <id> public|private` or in the Web UI).

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                             INCOMING WEB / API REQUEST ROUTING                              │
│                                                                                             │
│                         Has Valid Session Cookie / Bearer Token?                            │
│                                   │                       │                                 │
│                                   ▼ NO                    ▼ YES                             │
│                         ┌──────────────────┐    ┌───────────────────────────────────┐       │
│                         │   Role: Viewer   │    │ User: <username>                  │       │
│                         │ (Unauthenticated)│    │ Role: Operator / Admin (from PAM) │       │
│                         └─────────┬────────┘    └─────────────────┬─────────────────┘       │
│                                   │                               │                         │
│                    Is Target Resource Public?                     │                         │
│                         │                  │                      │                         │
│                         ▼ YES              ▼ NO                   │                         │
│            ┌────────────────────────┐  ┌──────────────────┐       ▼                         │
│            │ Public Read Actions    │  │ Block Access     │  ┌────────────────────────────┐ │
│            │ • Public runs list     │  │ • 404 for runs   │  │ Full Control (Per RBAC)    │ │
│            │ • Public logs (SSE)    │  │ • 401 for admin  │  │ • All runs (public+private)│ │
│            │ • Public HTML reports  │  │ • Redirect /login│  │ • Trigger / cancel runs    │ │
│            │ • Public node status   │  └──────────────────┘  │ • Inventory & variables    │ │
│            │ • Prom /metrics (opt)  │                        │ • SSH keys & Secrets Vault │ │
│            └────────────────────────┘                        │ • Cluster node maintenance │ │
│                                                              └────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

### RBAC Permissions Matrix

| Resource / Capability                  | Viewer *(Anonymous)* | Operator      | Maintainer     | Admin                 |
| -------------------------------------- | :------------------: | :-----------: | :------------: | :-----------------:   |
| **Public Runs & Logs**                 | ✅ View               | ✅ View        | ✅ View         | ✅ View            |
| **Private Runs & Logs**                | ❌ Blocked (404)      | ✅ View        | ✅ View         | ✅ View            |
| **Public HTML Reports**                | ✅ View               | ✅ View        | ✅ View         | ✅ View            |
| **Private HTML Reports**               | ❌ Blocked (404)      | ✅ View        | ✅ View         | ✅ View            |
| **Trigger Runs (Ad-hoc / Manual)**     | ❌ Blocked (401)      | ✅ Execute     | ✅ Execute      | ✅ Execute         |
| **Cancel Active Runs**                 | ❌ Blocked (401)      | ✅ Cancel      | ✅ Cancel       | ✅ Cancel          |
| **Toggle Run Visibility (`pub/priv`)** | ❌ Blocked (401)      | ✅ Update      | ✅ Update       | ✅ Update          |
| **View / Edit Inventories & Vars**     | ❌ Blocked (401)      | 👁️ View Only  | ✅ Full Edit    | ✅ Full Edit         |
| **Manage Recurring Schedules**         | ❌ Blocked (401)      | 👁️ View Only  | ✅ Full Edit    | ✅ Full Edit         |
| **SSH Key Management**                 | ❌ Blocked (401)      | ❌ Blocked     | 👁️ View Public | ✅ Generate / Import |
| **Secrets Engine (Seal/Unseal/KVs)**   | ❌ Blocked (401)      | ❌ Blocked     | ❌ Blocked      | ✅ Full Admin      |
| **Worker Drain & Maintenance**         | ❌ Blocked (401)      | ❌ Blocked     | ✅ Drain Node   | ✅ Full Admin      |
| **User & PAT Token Administration**    | ❌ Blocked (401)      | Own PATs Only | Own PATs Only  | ✅ Full Admin        |

### Multi-Device Two-Factor Authentication (Multiple TOTP Tokens per User)

In production homelabs and enterprise environments, users often require
**multiple 2FA authenticators for a single account**:
1. **Shared Administrative Accounts (e.g. `root`, `deployer`)**: Multiple
   authorized engineers each enroll their own individual authenticator app
   (Google Authenticator, 1Password, YubiKey) under the shared account with
   distinct labels (e.g. "Alice Phone", "Bob YubiKey").
2. **Device Redundancy**: An individual operator can register both a primary
   mobile app and a hardware key or password manager.

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                       MULTI-DEVICE 2FA ENROLLMENT & VERIFICATION                            │
│                                                                                             │
│  User Account: root (Shared Sysadmin Account)                                               │
│  Enrolled 2FA Devices (user_totp_devices table):                                            │
│                                                                                             │
│  [DEVICE ID]   [LABEL]             [CREATED]       [LAST USED]     [STATUS]    [ACTION]     │
│  dev_9a1f      Alice Phone         2 months ago    10 mins ago     🟢 ACTIVE   [ Revoke ]   │
│  dev_4b8c      Bob YubiKey 5C      3 weeks ago     Yesterday       🟢 ACTIVE   [ Revoke ]   │
│  dev_7e20      Ops Vault Backup    6 months ago    Never           🟢 ACTIVE   [ Revoke ]   │
│                                                                                             │
│  AUTHENTICATION VERIFICATION FLOW:                                                          │
│  1. User authenticates Factor 1 via Linux PAM (username + PAM password).                    │
│  2. If 2FA devices are enrolled for that account:                                           │
│     • An ephemeral pre-auth challenge token is created (5-minute TTL).                      │
│     • The user enters the 6-digit TOTP passcode from ANY of their enrolled devices.         │
│  3. The auth engine checks the code against ALL active secrets for that username.           │
│  4. If a match is found within ±1 time step (30s drift tolerance):                          │
│     • Authentication succeeds.                                                              │
│     • Session cookie is issued.                                                             │
│     • The specific matched device's last_used_at is updated.                                │
│  5. Single Device Revocation: If Alice leaves the team or Bob loses his key, clicking       │
│     [ Revoke ] deletes only that device's secret. All other users/devices keep working!     │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

### Shared Account TOTP Invite URL Protocol

When onboarding multiple operators to a shared system account (such as `root`
or `deployer`):
1. **Initial Enrollment**:
   * The first administrator logs in via Linux PAM with the system account
     password.
   * Because zero TOTP devices are registered, Crucible prompts for initial
     setup. The administrator scans the QR code, enters the 6-digit TOTP code,
     and the device is saved with a label (e.g. `"Alice Primary Phone"`).
2. **Generating a Secure Invite Link**:
   * To add a second sysadmin (e.g. Bob) to `root` without sharing Alice's
     secret key:
   * Alice navigates to `/settings/2fa` in the Web UI (or runs `cruciblectl
     auth 2fa invite --username root --label "Bob Phone"`).
   * Crucible generates an ephemeral, cryptographically signed invite URL
     backed by a single-use record in `totp_invites` with a 24-hour TTL:
`https://crucible.example.com/enroll/2fa?invite=inv_01JBCDEFGH1234567890ABCDEF`
3. **Redeeming the Invite**:
   * Bob opens the invite URL in his browser.
   * **Identity Verification**: To prevent unauthorized enrollment if the URL
     is intercepted, Bob must enter the account's Linux PAM password to verify
     authorization.
   * Crucible generates a brand-new, unique Base32 TOTP secret exclusively for
     Bob and displays the QR code.
   * Bob scans the QR code in Google Authenticator or 1Password and enters the
     6-digit confirmation code.
   * Crucible records the new device in `user_totp_devices` with label `"Bob
     Phone"`, marks the invite token as consumed, and issues a session cookie.
   * Both Alice and Bob can now independently log in as `root` using their
     respective authenticator apps!

---

---

## 7. Multi-Server & Cluster Topology Modes

Because `crucibled` is packaged as a single binary, it supports any cluster
topology via CLI flags:

1. **Converged (Default / Raspberry Pi / Home Lab)**:
   * `crucibled`
   * Runs the Controller (Web UI, Scheduler, SQLite) **and** executes jobs
     locally.
2. **Dedicated Controller (Production Isolation)**:
   * `crucibled --mode=controller --enable-local-runner=false`
   * Dedicated management plane: zero job execution on this host. All work is
     dispatched to remote workers.
3. **Pure Worker Node (Agent)**:
   * `crucibled --mode=worker --controller=https://hub:50051 --token=cb_pat_...
     --datacenter=us-east`
   * Zero-state execution agent with no Web UI or DB. Dials home to the
     controller.
4. **Multi-Master Active-Active Mesh**:
   * `crucibled --mode=controller --peers=https://peer-b:50051`
   * Replicates state and shares jobs with distributed lease locking.

---

## 8. Ansible Inventory & Variables Management (Git-Backed, CLI & Web UI)

Managing hosts, groups, and variable scopes across environments (production,
staging, homelab, edge devices) is fundamental to running production Ansible
automation. Crucible provides first-class, centralized **Ansible Inventory and
Variables Management** directly accessible from both the web dashboard and the
`cruciblectl` operator CLI.

Rather than maintaining detached databases, **Crucible uses Git as the
authoritative Single Source of Truth**, while using SQLite as an
in-memory/on-disk indexed cache for blazing-fast search, tree visualization,
and execution resolution.

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│                     GIT-BACKED ANSIBLE INVENTORY & VARIABLES PIPELINE                            │
│                                                                                                  │
│       Web Dashboard (/inventory, /vars)               Operator CLI (cruciblectl inventory/vars)  │
│    ┌──────────────────────────────────────┐      ┌────────────────────────────────────────────┐  │
│    │ • Host/Group/Custom Vars Tree View   │      │ • cruciblectl inventory [list|get|set|edit]│  │
│    │ • In-Browser YAML/INI Code Editor    │      │ • cruciblectl vars [list|get|set|edit]     │  │
│    │ • Instant Pre-Commit Syntax Check    │      │ • cruciblectl vars resolve <host>          │  │
│    │ • Variable Precedence Inspector      │      │ • cruciblectl inventory export (json)      │  │
│    └──────────────────┬───────────────────┘      └────────────────────┬───────────────────────┘  │
│                       │ HTTP / REST / SSE                               │ gRPC                   │
│                       └─────────────────────────┬───────────────────────┘                        │
│                                                 ▼                                                │
│                               ┌───────────────────────────────────┐                              │
│                               │      Crucible Server Core Engine      │                          │
│                               │   (internal/ansible & git)        │                              │
│                               └─────────────────┬─────────────────┘                              │
│                                                 │                                                │
│              ┌──────────────────────────────────┼──────────────────────────────────┐             │
│              ▼                                  ▼                                  ▼             │
│  ┌───────────────────────────────┐  ┌───────────────────────┐          ┌───────────────────────┐ │
│  │ Git Synchronization Engine    │  │ Local Working Tree    │          │ Persistence Cache     │ │
│  │ (internal/git)                │  │ (Cloned Repository)   │          │ (internal/storage)    │ │
│  │ • Background Pull (Timer/5m)  │  │ • inventory/          │          │ • crucible.db         │ │
│  │ • Pre-Edit Pull Hook          │  │ • host_vars/          │          │   - inventories       │ │
│  │ • Pre-Commit Rebase Pull      │  │ • group_vars/         │          │   - var_scopes        │ │
│  │ • Atomic Commit & Push        │  │ • vars/accounting/    │          │   - hosts_cache       │ │
│  └───────────────┬───────────────┘  └───────────┬───────────┘          └───────────────────────┘ │
│                  │                              │                                                │
│                  ▼                              ▼                                                │
│       Remote Git Repository          Execution Sandbox (Materialize -i)                          │
│       (GitHub/GitLab/Gitea)          ansible-playbook -i ...                                     │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 8.1 Scope of Managed Assets (Inventories, Vars & Custom Vars)

Crucible manages the full breadth of Ansible's configuration ecosystem:
1. **Inventories (`inventory/`)**:
   * Hierarchical YAML (`inventory/hosts.yaml`, `production.yaml`) and classic
     INI (`staging.ini`).
2. **Group Variables (`group_vars/`)**:
   * Variables mapped to host groupings: `group_vars/all.yml`,
     `group_vars/webservers.yml`, `group_vars/databases.yml`.
3. **Host Variables (`host_vars/`)**:
   * Host-specific overrides: `host_vars/web-01.yml`, `host_vars/db-01.yml`.
4. **Custom Role & Application Variables (`vars/**`)**:
   * Custom scoped paths defined by roles or organizational conventions:
     * `vars/accounting/users.yml`
     * `vars/accounting/groups.yml`
     * `vars/network/vlans.yml`
   * Administrators can configure arbitrary tracked vars paths via
     `inventory.custom_vars_paths: ["vars/accounting/**", "vars/network/**"]`.

### 8.2 Git-Backed Synchronization Protocol (Always Fresh, Always Tracked)

To ensure inventories and variables are never stale and that team members
collaborating via Git, the CLI, and the Web UI never overwrite one another,
Crucible enforces a strict **4-Stage Git Synchronization Protocol**:

#### 1. Periodic Background Pull (Scheduled Timer)
* A background ticker in `internal/git/` executes `git fetch && git pull
  --ff-only` on a configurable interval (default: `5m`).
* Keeps the local working copy continuously synchronized with remote commits
  pushed by external team members or CI/CD pipelines.

#### 2. Pre-Edit Pull Hook (Freshness Guarantee)
* When an operator clicks **"Edit"** on any inventory or variable file in the
  Web UI, or runs `cruciblectl inventory edit <id>` / `cruciblectl vars edit
  <path>`:
* The server automatically executes an immediate `git pull --ff-only` **before
  serving the file content** to the browser or terminal editor.
* Prevents operators from making edits against stale local files.

#### 3. Atomic Save Pipeline (Pre-Commit Pull -> Commit -> Push)
When an operator saves an edited file in the Web UI or closes `$EDITOR` in the
CLI:
1. **Syntax Validation**: Before touching disk or Git, the parser
   (`internal/ansible/validator.go`) validates YAML/INI syntax. If invalid, the
   save is rejected immediately with exact line numbers and syntax error
   messages.
2. **Working Tree Mutex Lock**: An exclusive in-memory read/write mutex
   (`internal/git/lock.go`) is acquired to prevent concurrent edits from
   racing.
3. **Pre-Commit Rebase/Pull**: The server executes `git pull --rebase` to
   integrate any upstream commits created while the operator was editing.
4. **File Write**: The validated file is written to the local working
   directory.
5. **Git Commit**: Executes:
   ```bash
   git add <path>
   git commit -m "Update <path> via Crucible [operator: <username>]"
--author="<username> <<email>>"
   ```
6. **Git Push**: Executes `git push origin <branch>`. If the push fails (e.g.
   branch protection or non-fast-forward), the rebase is aborted, the user is
   alerted, and local changes remain safely buffered.
7. **Cache & Index Refresh**: The SQLite search index in `crucible.db` is
   refreshed, updating resolved host counts, groups, and variable values.
8. **Cluster Peer Notification**: An `InventoryUpdatedEvent` /
   `VarsUpdatedEvent` is broadcast across the gRPC peer mesh, instructing peer
   controllers to pull the latest commit into their local clones.

### 8.3 Variable Precedence & Effective Variable Inspector

Ansible's variable precedence is complex (spanning 22 precedence levels).
Crucible's variable resolver (`internal/ansible/resolver.go`) calculates the
**effective merged variables** for any selected host and presents a visual
inheritance breakdown in the Web UI:

$$\text{Inventory Host Vars} \longrightarrow \text{group\_vars/all}
\longrightarrow \text{group\_vars/<group>} \longrightarrow
\text{host\_vars/<host>} \longrightarrow \text{Custom Vars (vars/accounting/*)}
\longrightarrow \text{Extra Vars}$$

* In the Web UI **Variable Inspector**, clicking on any variable reveals its
  resolution trace:
  ```
  app_env: "production"  (Defined in: group_vars/webservers.yml:4)
  db_port: 5432          (Defined in: vars/accounting/db.yml:12, Overrode:
group_vars/all.yml:8)
  admin_user: "deploy"   (Defined in: host_vars/web-01.yml:2)
  ```

### 8.4 Web Interface Studio (`/inventory` & `/vars`)

The web dashboard provides an integrated infrastructure management environment:

* **Inventory & Variables Catalog (`/inventory` & `/vars`)**:
  * Tabbed navigation: **[ Inventories ]**, **[ Group Vars ]**, **[ Host Vars
    ]**, **[ Custom Vars (`vars/**`) ]**.
  * File tree view showing all tracked files with format badges (`YAML` /
    `INI`), file size, last Git commit author, and relative commit timestamp
    (`2 mins ago`).
  * Search by filename, variable key, or host IP.
* **Interactive Editor & Visualizer (`/inventory/{id}` & `/vars/{path}`)**:
  * **Split-Pane Layout**:
    * **Left Pane (Tree & Explorer)**: Interactive tree of groups, member
      hosts, and custom directory folders. Clicking a host opens its effective
      merged variables drawer.
    * **Right Pane (Code Editor)**: Syntax-highlighted YAML/INI editor with
      line numbers, dark/light theme, and find/replace.
  * **Pre-Commit Syntax Validator**: Live "Validate" button tests formatting
    against Ansible rules with line-numbered error callouts.
  * **"Save & Push" Button**: Executes the atomic Git commit and push pipeline
    with real-time status spinner.
  * **Role Guards**: Anonymous viewers get read-only visualization; editing and
    saving require `Operator` or higher.

### 8.5 Operator CLI Commands (`cruciblectl inventory` & `cruciblectl vars`)

Operators can inspect and manipulate inventories and variables directly from
the command line:

#### Inventory Commands
| CLI Command                                                 | Description                                                                             |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `cruciblectl inventory list`                                | Tabular display of managed inventories (ID, NAME, FORMAT, HOSTS, GROUPS, UPDATED)       |
| `cruciblectl inventory get <id> [--format yaml\|ini\|json]` | Dumps inventory content to stdout                                                       |
| `cruciblectl inventory create <id> --name <name> -f <file>` | Creates a new inventory file, commits, and pushes to Git                                |
| `cruciblectl inventory edit <id>`                           | Pulls latest, opens in `$EDITOR` (vim/nano), validates syntax on save, commits & pushes |
| `cruciblectl inventory hosts <id> [--group <name>]`         | Lists all resolved hosts in the inventory, optionally filtered by group                 |
| `cruciblectl inventory validate -f <file>`                  | Validates inventory syntax before upload                                                |
| `cruciblectl inventory export <id> --format json`           | Emits Ansible dynamic inventory JSON output (usable with `ansible-inventory -i`)        |
| `cruciblectl inventory delete <id>`                         | Deletes inventory file from Git and pushes commit                                       |

#### Variables Commands
| CLI Command                                          | Description                                                                             |
| ---------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `cruciblectl vars list [--type group\|host\|custom]` | Lists all tracked variable files (`group_vars/`, `host_vars/`, `vars/**`)               |
| `cruciblectl vars get <path>`                        | Prints content of variable file (e.g. `cruciblectl vars get vars/accounting/users.yml`) |
| `cruciblectl vars set <path> -f <file.yml>`          | Overwrites variable file, validates YAML, commits, and pushes                           |
| `cruciblectl vars edit <path>`                       | Pulls latest, opens `$EDITOR`, validates YAML syntax on save, commits & pushes          |
| `cruciblectl vars resolve <hostname>`                | Displays the complete, effective merged variables for a host with origin traces         |

### 8.6 Pipeline Execution Integration

When pipelines run Ansible playbooks:
```yaml
name: accounting-payroll-deploy
inventory: production        # References managed inventory from git working copy

tasks:
  - name: Run Payroll Setup
    ansible:
      playbook: playbooks/payroll.yml
      limit: accounting-hosts
      extra_vars:
        vars_file: vars/accounting/users.yml
```
* **Direct Working Copy Invocation**: Subprocess runners execute directly
  against the synchronized local Git working copy, ensuring `ansible-playbook
  -i inventory/hosts.yaml` automatically discovers `group_vars/`, `host_vars/`,
  and referenced `vars/accounting/` paths without requiring manual file copying
  or clunky mounts.

---

### 8.7 NetBox Dynamic Inventory Plugin & Dual-Source Architecture

In modern enterprise and homelab infrastructure, **NetBox** frequently serves
as the authoritative Single Source of Truth for network devices, IPAM, and
server infrastructure. Crucible provides native, first-class support for NetBox
dynamic inventories alongside traditional Git-backed local inventory files:

1. **Dual Inventory Support**:
   * **Local File Mode**: Flat YAML/INI files (`inventory/hosts.yaml`,
     `staging.ini`) managed directly in the Web UI and CLI.
   * **NetBox Dynamic Plugin Mode**: Configured via standard Ansible plugin
     YAML (`inventory/netbox.yaml`):
     ```yaml
     plugin: netbox.netbox.nb_inventory
     api_endpoint: https://netbox.internal.example.com
     token: 0123456789abcdef...
     validate_certs: true
     config_context: true
     group_by:
       - site
       - role
       - platform
       - manufacturer
     ```
2. **Execution Inside Python Virtualenv**:
   * Crucible executes `ansible-inventory -i inventory/netbox.yaml --list`
     inside the configured Python virtual environment (`venv`) where the
     `pynetbox` library and `netbox.netbox` collection are installed.
3. **SQLite Caching & Web UI Visualization**:
   * Crucible ingests the JSON output from NetBox and caches the device
     hierarchy (Sites, Racks, Device Roles, Platforms, IP addresses) in
     `netbox_inventory_cache`.
   * In the Web UI **Inventory Studio**, operators can toggle between **[ Local
     Files ]** and **[ NetBox Infrastructure ]**.
   * NetBox devices appear in the interactive tree explorer, complete with
     their NetBox config context and custom fields.
4. **Layered Variable Inheritance**:
   * Local `group_vars/` and `host_vars/` seamlessly layer on top of NetBox
     dynamic hosts! If NetBox provides `ansible_host: 10.0.0.5`, an operator
     can still define `host_vars/switch-core-01.yml` or
     `vars/accounting/users.yml` in the Git repo to inject role-specific
     credentials and configurations.

---

## 9. Native HTTPS, TLS & Automated Let's Encrypt (ACME)

Crucible requires HTTPS in production to protect passwords, TOTP challenge
codes, session cookies, and API tokens. The daemon includes a built-in pure Go
TLS engine supporting both manual certificates and automated Let's Encrypt
certificate issuance.

### 9.1 Custom Certificate & Key Files
* Operators can provide custom certificates:
  ```bash
  crucibled --tls-cert=/etc/ssl/certs/crucible.crt
--tls-key=/etc/ssl/private/crucible.key
  ```
* Supports intermediate certificate chains and automatic in-memory reload on
  SIGHUP or file modification without dropping active connections.

### 9.2 Automated Let's Encrypt ACME Integration
For zero-touch TLS, `crucibled` integrates ACME certificate lifecycle
management:
1. **TLS-ALPN-01 Challenge (`--acme-challenge=tls-alpn-01`)**:
   * Operates strictly on **port 443** using the TLS Application-Layer Protocol
     Negotiation (`acme-tls/1`) extension.
   * **Zero Port 80 Requirement**: Perfect for firewalls, edge gateways, or
     locked-down environments where HTTP port 80 is strictly blocked.
2. **DNS-01 Challenge (`--acme-challenge=dns-01`)**:
   * Solves challenges via automated DNS `_acme-challenge` TXT records.
   * **Homelab & Private Network Essential**: Works for internal RFC 1918 LAN
     addresses (e.g. `crucible.home.arpa`, `crucible.lab.internal`), machines
     behind Carrier-Grade NAT (CGNAT), and wildcard certificates
     (`*.lab.example.com`).
   * Pluggable provider support via environment variables / flags: Cloudflare,
     AWS Route53, RFC 2136 BIND `nsupdate`, DigitalOcean, etc.
3. **Automatic Renewal**:
   * Background daemon checks certificate validity daily and automatically
     initiates ACME renewal 30 days before expiration.
   * Renewed certificates are applied in memory and persisted to the SQLite
     database or filesystem cache.

---

## 10. Python Virtual Environment (`venv`) Execution Engine

Ansible requires a Python interpreter with specific modules (e.g., `pynetbox`,
`jmespath`, `ansible-core`, `cryptography`). To prevent polluting the host
system Python or conflicting with system packages:
* **Global Venv Configuration**: Set via `--ansible-venv=/opt/ansible-venv` or
  daemon config `engine.ansible_venv: "/opt/ansible-venv"`.
* **Per-Pipeline Venv Override**:
  ```yaml
  name: network-switch-audit
  venv: /opt/venvs/netbox-ansible   # or relative path inside repo: .venv
  inventory: inventory/netbox.yaml
  tasks:
    - name: Audit Interfaces
      ansible:
        playbook: playbooks/audit.yml
  ```
* **Execution Environment Injection (`internal/engine/exec_local.go`)**:
  * The subprocess runner inspects the resolved `venv` directory.
  * Automatically injects:
    * `PATH=<venv>/bin:$PATH`
    * `VIRTUAL_ENV=<venv>`
    * `PYTHONHOME=""` (cleared to avoid host library conflicts)
  * Executes `<venv>/bin/ansible-playbook` directly, guaranteeing that all
    required Python dependencies and Ansible collections are available.

---

## 11. Built-in SSH Key Management (Web UI & CLI)

Ansible and Git rely on SSH key authentication. Rather than forcing operators
to manually navigate Linux dotfiles (`~/.ssh`), Crucible provides native SSH
keypair management:

### 11.1 Dedicated Key Purposes
Crucible manages two distinct SSH key identities:
1. **`git_sync` (Git Deploy Key)**:
   * Used by `internal/git/` to authenticate `git fetch`, `git clone`, and `git
     push` against upstream repositories (GitLab, GitHub, Gitea, or generic Git
     server).
2. **`ansible_nodes` (Ansible Host Key)**:
   * Used by `internal/engine/` to connect to managed target machines, network
     switches, and Raspberry Pis.
   * Automatically passed to `ansible-playbook` via `--private-key` or an
     ephemeral in-memory SSH agent.

### 11.2 Keypair Operations (Web UI & CLI)
* **Generate Keypair**:
  * Pure Go Ed25519 (recommended default: fast, high security, compact) or RSA
    4096-bit.
  * Web UI: One-click "Generate SSH Key" button in `/settings/ssh`.
  * CLI: `cruciblectl ssh generate --purpose git_sync --type ed25519`
* **View Public Key**:
  * Displays public key (`ssh-ed25519 AAAA... crucible@controller`) with
    one-click copy button for adding to Git repository "Deploy Keys" or remote
    `~/.ssh/authorized_keys`.
  * CLI: `cruciblectl ssh show --purpose git_sync --public`
* **Import Existing Key**:
  * Operators can paste existing private keys if infrastructure already uses an
    established identity.
  * CLI: `cruciblectl ssh import --purpose ansible_nodes -f ~/.ssh/id_ed25519`
* **Connection Diagnostics**:
  * Web UI & CLI provide instant connection testing:
    `cruciblectl ssh test --purpose git_sync --target git@github.com`
    `cruciblectl ssh test --purpose ansible_nodes --target 192.168.1.50`
* **Secure Persistence**:
  * Keys are stored in the `ssh_keys` database table, encrypted at rest using a
    machine-derived key or configurable encryption passphrase, and set with
    strict `0600` permissions when materialized to temporary sandboxes.

---

## 12. ULID (Universally Unique Lexicographically Sortable Identifier) Standard

Rather than using standard random UUIDv4 strings or auto-incrementing integers,
Crucible adopts **ULID** across the entire platform:

### 12.1 Format & Properties
$$\underbrace{\text{01JBCDEFGH}}_{\text{48-bit timestamp (ms)}} \quad
\underbrace{\text{JKMNPQRSTVWXYZ0123}}_{\text{80-bit cryptographic entropy}}$$
* **128-bit Compatibility**: Matches standard 128-bit UUID size, stored as a
  clean 26-character Crockford's Base32 string (URL-safe, case-insensitive, no
  ambiguous characters like `I`, `L`, `O`, `U`).
* **Lexicographically Sortable**: Because the first 48 bits encode UNIX
  millisecond epoch time, sorting ULID strings alphabetically sorts them in
  exact chronological order!
* **SQLite Performance & SD Card Longevity**:
  * Random UUIDv4 causes severe B-Tree index fragmentation because keys are
    inserted randomly throughout the tree, leading to high disk write
    amplification.
  * ULID inserts are sequentially monotonic: new rows append cleanly to the end
    of index leaves. This significantly accelerates SQLite write throughput and
    minimizes wear on Raspberry Pi SD cards.

### 12.2 Cluster-Wide ULID Scope
All primary keys in Crucible use ULID:
* Runs: `run_01JBC...`
* Tokens: `tok_01JBC...`
* 2FA Devices: `dev_01JBC...`
* 2FA Invites: `inv_01JBC...`
* SSH Keys: `ssh_01JBC...`
* Sessions: `ses_01JBC...`
* Secrets: `sec_01JBC...`
* Leases: `lse_01JBC...`

---

## 13. Crucible-Style Zero-RAM Log Streaming to Disk & Sharded HTML Reports

To execute massive Ansible playbooks and long-running pipelines without
exhausting server RAM, Crucible implements an append-only disk streaming engine
modeled directly after **Crucible / Go-Runner**:

```
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                       CRUCIBLE-STYLE DISK LOG STREAMING & REPORT ENGINE                         │
│                                                                                                 │
│   ansible-playbook Subprocess (stdout / stderr)                                                 │
│                        │                                                                        │
│                        ▼                                                                        │
│             ┌─────────────────────────┐                                                         │
│             │ LogBroadcaster (Engine) │                                                         │
│             └──────┬───────────┬──────┘                                                         │
│                    │           │ Non-blocking fan-out (chan LogChunk, buf: 256)                 │
│       Disk Write   │           ├────────────────────────────┬────────────────────────────┐      │
│       Direct Append│           ▼                            ▼                            ▼      │
│                    │   ┌───────────────┐            ┌───────────────┐        ┌───────────────┐  │
│                    │   │ Live Viewer 1 │            │ Live Viewer 2 │        │  cruciblectl  │  │
│                    │   │ (SSE Stream)  │            │ (SSE Stream)  │        │  (gRPC)       │  │
│                    ▼   └───────────────┘            └───────────────┘        └───────────────┘  │
│     ┌──────────────────────────────────────────────────┐                                        │
│     │ Sharded Disk File:                               │                                        │
│     │ var/reports/01/JB/01JBCDEF..._deploy_web.log     │                                        │
│     └──────────────────────────────────────────────────┘                                        │
│                    ▲                                                                            │
│                    │ Read from offset 0 to EOF, then attach to live channel                     │
│             ┌──────┴──────────────┐                                                             │
│             │ Late-Joining Viewer │                                                             │
│             └─────────────────────┘                                                             │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 13.1 Zero-RAM Disk Log Streaming Architecture
1. **Append-Only File Sinks (`var/reports/`)**:
   * When a job executes, the subprocess runner does **not** store the entire
     stdout/stderr buffer in daemon memory.
   * Instead, the `LogBroadcaster` appends each output line directly to disk:
     `var/reports/{shard}/{run_ulid}_{job_slug}.log`.
   * Standard error is colorized with ANSI red escape sequences upon write,
     while Ansible's native ANSI color formatting is preserved verbatim.
2. **Non-Blocking Fan-Out to Live Viewers**:
   * Active browser sessions (via Server-Sent Events
     `/runs/{ulid}/logs/stream`) and CLI clients (`cruciblectl run --follow`)
     receive chunks over buffered Go channels (`chan LogChunk`, capacity 256).
   * If a slow network client fills its channel buffer, `publish()` drops the
     chunk via non-blocking `select default`, ensuring a slow viewer **never
     blocks or stalls the execution engine**.
3. **Seamless Late-Joining Crucibleback**:
   * When a user opens a run that is currently in progress (or inspects a past
     completed run):
     * The stream handler opens
       `var/reports/{shard}/{run_ulid}_{job_slug}.log`.
     * It reads and streams all pre-existing lines from byte offset 0.
     * If the job is still active (`IsDone() == false`), the handler seamlessly
       attaches to the live `LogBroadcaster` channel, streaming new real-time
       output until EOF.

### 13.2 ULID 2-Level Directory Sharding
Placing tens of thousands of log files in a single flat directory causes severe
file system inode lookup degradation. Crucible solves this by sharding using
the first 4 characters of the 26-character ULID:
$$\text{ULID: } \underbrace{\mathbf{01}}_{\text{Shard Level 1}}
\overbrace{\mathbf{JB}}^{\text{Shard Level 2}}
\text{CDEFGHJKMNPQRSTVWXYZ0123}$$
* Path structure:
  `var/reports/01/JB/01JBCDEFGHJKMNPQRSTVWXYZ0123_setup_database.log`.
* **Chronological Clustering**: Because the first 10 characters of a ULID
  represent the millisecond timestamp, the two shard levels naturally partition
  runs chronologically by time windows!
* Pre-creation: The daemon automatically executes `os.MkdirAll(shardDir, 0755)`
  prior to spawning the task.

### 13.3 HTML Report Publishing & Visibility Gatekeeping
Ansible playbooks, pytest suites, and audit scripts frequently generate rich
HTML reports (e.g. ARA records, JUnit HTML, security compliance summaries).
1. **Report Injection Environment Variable**:
   * Crucible automatically injects
     `CRUCIBLE_REPORT_DIR=/absolute/path/to/var/reports/{shard}` into every
     executing job.
   * Playbooks can output their final HTML report directly to:
     `$CRUCIBLE_REPORT_DIR/${CRUCIBLE_RUN_ID}_report.html` (or
`${CRUCIBLE_RUN_ID}_${JOB_SLUG}.html`).
2. **Automatic Detection in Web UI**:
   * When rendering run details, the Web dashboard performs a `HEAD` probe
     against `/reports/{shard}/{run_ulid}_{job_slug}.html`.
   * If found, the UI renders an eye-catching **"View HTML Report"** button
     that opens the artifact in a secure sandboxed tab.
3. **Public vs. Private Report Gatekeeping**:
   * Requests to `/reports/{shard}/*` are intercepted by authorization
     middleware:
     * If the parent run's `visibility == 'public'`, the report is served
       immediately without authentication.
     * If `visibility == 'private'`, anonymous requests receive `404 Not
       Found`, while authenticated users must possess at least `viewer`
       permissions.

---

## 14. Secrets Engine & HashiCorp Vault-Style Seal / Unseal Architecture

Automation workflows invariably handle sensitive credentials: cloud API keys,
SSH passphrases, database credentials, and **Ansible Vault Passwords**. Storing
plaintext secrets in Git repositories or on disk is an extreme operational
vulnerability.

Crucible provides a built-in, lightweight **Secrets Engine**
(`internal/secrets/`) featuring envelope encryption (AES-256-GCM + Argon2id)
and a strict **Seal / Unseal** operational lifecycle inspired by HashiCorp
Vault:

```
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                       SECRETS ENGINE: SEAL / UNSEAL & ANSIVOULT INJECTION                       │
│                                                                                                 │
│  DAEMON BOOT                                                                                    │
│       │                                                                                         │
│       ▼                                                                                         │
│  ┌───────────┐    Unseal Key / Shares Submitted     ┌─────────────┐                             │
│  │  SEALED   │ ───────────────────────────────────> │  UNSEALED   │                             │
│  │  STATE    │ <─────────────────────────────────── │  STATE      │                             │
│  └─────┬─────┘    Manual Re-Seal / cruciblectl seal     └──────┬──────┘                         │
│        │                                                   │                                    │
│        │ Ciphertext in SQLite                              │ Master KEK Held in Guarded RAM     │
│        │ (AES-256-GCM)                                     │ (Zeroized on Seal / Exit)          │
│        │                                                   │                                    │
│        ▼                                                   ▼                                    │
│  Jobs Requiring Secrets BLOCKED                 ansible-playbook Execution                      │
│  Web UI displays [SEALED] badge                            │                                    │
│                                                            ▼                                    │
│                                         ┌──────────────────────────────────────────┐            │
│                                         │ Ephemeral Pure-Go FIFO (Named Pipe)      │            │
│                                         │ /run/crucibled/vault-01JBC...fifo (0600) │            │
│                                         │ • crucibled writes secret to pipe        │            │
│                                         │ • ansible reads --vault-password-file    │            │
│                                         │ • FIFO unlinked immediately              │            │
│                                         │ • ZERO PLAINTEXT SAVED TO DISK           │            │
│                                         └──────────────────────────────────────────┘            │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 14.1 Seal / Unseal Lifecycle
1. **Boot in Sealed State**:
   * Whenever `crucibled` boots, restarts, or crashes, it defaults to the
     **`SEALED`** state.
   * In `SEALED` mode:
     * Master Key Encryption Key (KEK) does not exist in memory.
     * All entries in the `vault_secrets` SQLite table remain encrypted blobs.
     * Any pipeline requiring vault secrets is held in `WAITING_UNSEAL` or
       rejected.
     * Web UI displays a prominent warning badge: `[SEALED]` with an
       interactive unseal prompt.
     * CLI reports `cruciblectl vault status -> Status: Sealed`.
2. **Unsealing the Engine**:
   * Unsealing can be performed via CLI (`cruciblectl vault unseal`) or through
     the Web UI modal.
   * **Unseal Modes**:
     * **Mode A: Single Master Passphrase / Recovery Token**: Ideal for simple
       homelabs and single-operator environments.
     * **Mode B: Shamir's Secret Sharing ($K$-of-$N$)**: Splits the Master Key
       into $N$ shares where a minimum threshold $K$ is required to reconstruct
       the key (e.g. 2 of 3 sysadmins must submit their shares).
     * **Mode C: Headless Auto-Unseal (Optional for Lights-Out Reboots)**: For
       automated homelab reboot recovery without human intervention, operators
       can pass `--vault-unseal-file=/etc/crucible/vault.key` or set
       `CRUCIBLE_VAULT_UNSEAL_KEY=...` or use `systemd-creds`. If set, the
       daemon auto-unseals at boot, but operators can still trigger an
       immediate emergency re-seal (`cruciblectl vault seal`).
3. **Emergency Manual Seal**:
   * In the event of a suspected intrusion or maintenance window, operators can
     execute:
     `cruciblectl vault seal`
   * The daemon instantly wipes the KEK from volatile memory (using memory
     zeroing).

### 14.2 Zero-Plaintext-on-Disk Ansible Vault Injection
Ansible Vault uses AES-256 to encrypt sensitive files (`vars/vault.yml`) and
inline variables (`!vault | ...`). To decrypt during a run, `ansible-playbook`
requires a password. Persisting plaintext password files to disk is insecure.
Crucible solves this using two zero-plaintext mechanisms:

#### Mechanism 1: Ephemeral Pure-Go Named Pipe (FIFO)
1. Before launching `ansible-playbook`, `crucibled` creates a POSIX FIFO in a
   protected tmpfs directory:
   `/run/crucibled/vault-{run_ulid}.fifo` (file mode `0600` owned by the runner
UID).
2. `crucibled` launches a short-lived internal goroutine that opens the FIFO
   for writing.
3. The runner executes `ansible-playbook ... --vault-password-file
   /run/crucibled/vault-{run_ulid}.fifo`.
4. As soon as Ansible opens the FIFO to read the password, the goroutine writes
   the decrypted password and closes the pipe.
5. The runner immediately unlinks (`os.Remove`) the FIFO file.
6. **Security Guarantee**: The password only exists in kernel pipe memory
   buffers for microseconds; zero bytes of plaintext are written to disk.

#### Mechanism 2: Executable Vault Password Script (IPC Socket)
1. Ansible natively supports executable vault password files
   (`--vault-password-file /usr/local/bin/crucible-vault-pass`).
2. When invoked, the helper script contacts `crucibled`'s local Unix domain
   socket (`/run/crucibled/crucibled.sock`) passing the environment token
   `CRUCIBLE_RUN_ID`.
3. `crucibled` verifies the run ID, extracts the required vault password from
   protected memory, and writes it directly to stdout.

### 14.3 Cryptographic Specification & Storage Model
* **Encryption Algorithm**: AES-256-GCM with authenticated 128-bit
  authentication tags and unique 96-bit random nonces per secret record.
* **Key Derivation Function (KDF)**: Argon2id (`time=3`, `memory=64MB`,
  `parallelism=4`, salt=16 cryptographically random bytes).
* **Storage Schema (`vault_secrets`)**:
  * `id`: ULID primary key (`sec_01JBC...`)
  * `name`: Unique human-readable identifier (e.g. `default`,
    `prod_network_vault`)
  * `secret_type`: `ansible_vault_pass`, `kv`, `token`
  * `ciphertext`: AES-256-GCM encrypted payload
  * `nonce`: 12-byte initialization vector

### 14.4 Vault CLI Commands (`cruciblectl vault`)
* `cruciblectl vault init [--shares N] [--threshold K]`: Initializes master key
  and outputs unseal keys.
* `cruciblectl vault status`: Displays seal status, initialization state, and
  unseal progress.
* `cruciblectl vault unseal [key]`: Submits an unseal key share or passphrase.
* `cruciblectl vault seal`: Immediately purges keys from RAM and locks the
  secrets engine.
* `cruciblectl vault set <name> --type ansible_vault_pass [--value secret |
  --prompt]`: Securely encrypts a new secret.
* `cruciblectl vault list`: Lists secret names, types, and timestamps (values
  are never displayed).
* `cruciblectl vault delete <name>`: Permanently removes a secret.

---

## 15. Prometheus Observability & Metrics Architecture (`/metrics`)

Crucible provides native, first-class Prometheus observability by exposing a
standard `GET /metrics` scraping endpoint on the HTTP server daemon
(`crucibled`). This enables real-time telemetry ingestion into Prometheus,
Grafana, Alertmanager, or VictoriaMetrics without requiring external exporters
or daemon sidecars.

### 15.1 Metric Categories & Definitions

| Metric Name                                    | Type      | Labels                                                                                          | Description                                                |
| ---------------------------------------------- | --------- | ----------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| **Pipeline & Job Execution**                   |           |                                                                                                 |                                                            |
| `crucible_runs_total`                          | Counter   | `pipeline`, `trigger` (`adhoc`\|`cron`\|`webhook`), `status` (`success`\|`failed`\|`cancelled`) | Total pipeline runs executed                               |
| `crucible_run_duration_seconds`                | Histogram | `pipeline`, `status`                                                                            | End-to-end execution latency of pipelines                  |
| `crucible_active_runs`                         | Gauge     | `pipeline`                                                                                      | Number of currently running pipelines                      |
| `crucible_job_duration_seconds`                | Histogram | `job`, `runner_type` (`local`\|`remote`)                                                        | Execution time for individual tasks/playbooks              |
| **Worker Fleet & Node Telemetry**              |           |                                                                                                 |                                                            |
| `crucible_workers_total`                       | Gauge     | `datacenter`, `availability` (`active`\|`drain`\|`paused`), `health` (`up`\|`down`)             | Count of registered workers by status                      |
| `crucible_worker_cpu_utilization_ratio`        | Gauge     | `node_id`, `hostname`, `datacenter`                                                             | Worker CPU usage ratio (0.00 to 1.00) from `/proc` sampler |
| `crucible_worker_memory_used_bytes`            | Gauge     | `node_id`, `hostname`, `datacenter`                                                             | Worker RAM consumed in bytes                               |
| `crucible_worker_memory_total_bytes`           | Gauge     | `node_id`, `hostname`, `datacenter`                                                             | Total worker host RAM in bytes                             |
| `crucible_worker_active_jobs`                  | Gauge     | `node_id`, `hostname`                                                                           | Active tasks running concurrently on worker                |
| **Cluster Federation & Split-Brain Interlock** |           |                                                                                                 |                                                            |
| `crucible_cluster_state`                       | Gauge     | `state` (`healthy`\|`isolated`\|`standalone`)                                                   | Current interlock status (1 for active state)              |
| `crucible_cluster_peers_total`                 | Gauge     | `sync_status` (`synced`\|`lagging`\|`offline`)                                                  | Count of connected controller peers                        |
| `crucible_cluster_leases_active`               | Gauge     | (none)                                                                                          | Count of active distributed execution leases               |
| `crucible_cluster_lease_fencing_token`         | Gauge     | `slot_key`                                                                                      | Current fencing token for active leases                    |
| **Scheduler & Webhooks**                       |           |                                                                                                 |                                                            |
| `crucible_schedules_total`                     | Gauge     | `enabled` (`true`\|`false`)                                                                     | Total registered cron and one-time schedules               |
| `crucible_schedule_ticks_total`                | Counter   | `schedule_id`, `status` (`triggered`\|`missed`\|`skipped_isolated`)                             | Ticks evaluated by scheduler                               |
| **Security & Authentication**                  |           |                                                                                                 |                                                            |
| **Secrets Engine & Vault**                     |           |                                                                                                 |                                                            |
| `crucible_vault_sealed`                        | Gauge     | `state` (`sealed`\|`unsealed`)                                                                  | Seal status of secrets engine (1 for active state)         |
| `crucible_vault_secrets_total`                 | Gauge     | `secret_type` (`ansible_vault_pass`\|`kv`\|`token`)                                             | Total encrypted secrets stored in vault                    |
| `crucible_auth_attempts_total`                 | Counter   | `method` (`pam`\|`pat`\|`totp`), `result` (`success`\|`failure`)                                | Authentication attempts                                    |
| `crucible_active_sessions`                     | Gauge     | `role` (`admin`\|`maintainer`\|`operator`\|`viewer`)                                            | Active web browser sessions                                |
| `crucible_api_tokens_active`                   | Gauge     | `role`                                                                                          | Active unrevoked personal access tokens                    |
| **Process & Go Runtime**                       |           |                                                                                                 |                                                            |
| Standard Go runtime metrics                    | Built-in  | `go_goroutines`, `go_memstats_alloc_bytes`, `process_cpu_seconds_total`, etc.                   | Core runtime health & resource usage                       |

### 15.2 Event Bus Integration (Zero Business Logic Intrusion)

Prometheus metric recording is cleanly decoupled from domain logic using
Hexagonal Ports and the Domain Event Bus:

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                 PROMETHEUS INTEGRATION                                 │
│                                                                                        │
│   Domain Action / Event               Event Bus                 Driven Adapter         │
│  ┌──────────────────────┐      ┌────────────────────┐      ┌─────────────────────────┐ │
│  │ RunCompletedEvent    │ ────▶│ In-Memory Bus      │ ────▶│ adapter_metrics.go      │ │
│  │ WorkerWentDownEvent  │      │ (events.go)        │      │ (prometheus/client_go)  │ │
│  │ LeaseAcquiredEvent   │      └────────────────────┘      └────────────┬────────────┘ │
│  └──────────────────────┘                                               │              │
│                                                                         ▼              │
│                                                           ┌──────────────────────────┐ │
│   Scrape Request (Prometheus / Grafana)                   │ Prometheus Registry      │ │
│   GET /metrics ───────────────────────────────────────────▶│ (DefaultRegisterer)     │ │
│                                                           └──────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

1. **Feature Package (`internal/metrics/recorder.go`)**: Telemetry recording
   functions (`RecordRunStarted`, `RecordRunCompleted`,
   `UpdateWorkerTelemetry`, `RecordAuthAttempt`, etc.) register Prom metrics
   with `prometheus/client_golang`.
2. **Event Bus Integration**: Subscribes to the in-memory event bus
   (`internal/events/bus.go`) so lifecycle events instantly update metric
   counters/gauges without coupling business logic.
3. **HTTP Server Route (`cmd/crucibled/server_http.go`)**: Serves Prometheus
   exposition at `/metrics` (with optional `--metrics-allow-unauth=true` or
   bearer token guard).

### 15.3 Cross-Cluster Metric Replication & Federated `/metrics` Aggregation

In a multi-datacenter active-active cluster or manager-worker topology,
requiring Prometheus to discover and scrape every individual node independently
creates firewall and NAT headaches. To provide a single unified pane of glass,
**hitting `GET /metrics` on the manager (or any controller node) automatically
replicates and aggregates all metrics from every connected peer**.

#### 1. Zero-Latency Background gRPC Streaming

Rather than issuing slow, blocking HTTP fan-out requests when Prometheus
scrapes the manager, peer metrics replicate continuously over the existing gRPC
mesh (`PeerMeshService`):

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        CROSS-CLUSTER PEER METRICS REPLICATION                          │
│                                                                                        │
│      Peer Server B (FRA)                           Manager / Leader Server A (NYC)     │
│   ┌─────────────────────────┐                   ┌───────────────────────────────────┐  │
│   │  Local Prometheus       │                   │  PeerMetricsRegistry (In-Memory)  │  │
│   │  Registry               │                   │  ┌─────────────────────────────┐  │  │
│   └───────────┬─────────────┘                   │  │ Peer B (FRA): Snapshot      │  │  │
│               │                                 │  │ Peer C (SGP): Snapshot      │  │  │
│               ▼ (Heartbeat / 10s)               │  └──────────────┬──────────────┘  │  │
│   ┌─────────────────────────┐   gRPC PeerMesh   │                 │                 │  │
│   │ PeerMeshClient          │ ─────────────────▶│ PeerMeshServer  │                 │  │
│   │ SyncMetricsPayload      │   TLS Stream      │ (peer_manager)  │                 │  │
│   └─────────────────────────┘                   └─────────────────┼─────────────────┘  │
│                                                                   │                    │
│                                                                   ▼                    │
│                                                 ┌───────────────────────────────────┐  │
│         Scrape: GET /metrics                    │ ClusterMetricsHandler             │  │
│         (Prometheus / Grafana) ────────────────▶│ 1. Local NYC metrics              │  │
│                                                 │ 2. Tagged FRA & SGP metrics       │  │
│                                                 │ 3. Cluster Rollup Gauges          │  │
│                                                 └───────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

* **Continuous Replication**: Every peer pushes a `SyncMetricsPayload` over its
  persistent gRPC connection to the manager during heartbeat cycles (default:
  every 10 seconds).
* **In-Memory `PeerMetricsRegistry`**: The manager caches the latest metric
  families and snapshots in memory.
* **Instantaneous Scrape Response**: When Prometheus calls `GET /metrics`, the
  response is formatted and streamed directly from memory in < 5ms—immune to
  remote network latency spikes or transient WAN drops.
* **Partition & Staleness Watchdog**: If a peer drops off for > 30 seconds, its
  cached metrics are pruned and
  `crucible_cluster_peer_sync_status{peer="peer-b"} 0` alerts operators of the
  network partition.

#### 2. Label Disambiguation & Metric Namespacing

To prevent Prometheus scrape collisions (duplicate time series with identical
label sets), all replicated metrics are injected with distinguishing identity
labels (`node` and `datacenter`):

```prometheus
# --- Local Manager Metrics (NYC) ---
crucible_runs_total{node="manager-nyc",datacenter="dc-nyc",pipeline="deploy-db",status="success"} 42
go_goroutines{node="manager-nyc"} 34

# --- Replicated Peer Metrics (FRA) ---
crucible_runs_total{node="peer-fra",datacenter="dc-fra",pipeline="deploy-db",status="success"} 19
go_goroutines{node="peer-fra"} 28

# --- Cluster-Wide Rollup Metrics ---
crucible_cluster_nodes_total{role="controller",health="up"} 2
crucible_cluster_nodes_total{role="worker",health="up"} 8
crucible_cluster_active_jobs_total 5
crucible_cluster_peer_metrics_age_seconds{peer="peer-fra"} 3.2
```

* **Label Injection**: The manager's `ClusterMetricsHandler` ensures every
  series has `node="<hostname_or_id>"` and `datacenter="<dc>"` attached.
* **Grafana Ready**: Dashboards can display a single multi-select template
  variable: `node: [ All | manager-nyc | peer-fra ]`, enabling operators to
  drill down into a single node or view aggregate fleet capacity in one
  dashboard.

#### 3. Scrape Scoping & Direct Access

* **Unified Cluster Scrape (Default)**: `GET /metrics` outputs local controller
  metrics + all connected peer metrics + cluster rollups.
* **Local-Only Scrape**: `GET /metrics?scope=local` outputs only the local
  node's metrics. This allows standard Prometheus service discovery to scrape
  each node directly at its internal IP when desired.

---

## 16. Email Notifications & Report Dispatch Engine (`internal/notify/`)

Crucible includes an enterprise-grade notification and report distribution
engine (`internal/notify/`) designed for multi-team infrastructure operations.
It implements a decoupled, **publish-then-notify** architecture that eliminates
the notorious rendering inconsistencies of Microsoft Outlook while enabling
granular per-job and per-task notification routing.

```
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                      CRUCIBLE-STYLE REPORT PUBLISHING & NOTIFICATION DISPATCH                   │
│                                                                                                 │
│  ANSIBLE EXECUTION ENGINE                                                                       │
│  • Executes playbook inside Python venv                                                         │
│  • Generates rich HTML report (ARA / pytest / custom HTML)                                      │
│  • Publishes to $CRUCIBLE_REPORT_DIR/01/JB/01JBC..._patch_db.html (ZERO EMAIL FROM ANSIBLE)     │
│                        │                                                                        │
│                        ▼                                                                        │
│  EVENT BUS / DOMAIN LIFECYCLE (RunStarted, JobSuccess, JobFailed)                               │
│                        │                                                                        │
│                        ▼                                                                        │
│  ┌─────────────────────────────────────────────────────────┐                                    │
│  │           internal/notify Hierarchy Resolver            │ ◀─── Manual Trigger (Web / CLI)    │
│  │                                                         │      • "Email Report" Modal        │
│  │   Pipeline Default (Root)                               │      • cruciblectl notify send     │
│  │      ▼                                                  │                                    │
│  │   Cluster / Group Level (e.g. Database-Group-1)         │                                    │
│  │      ▼                                                  │                                    │
│  │   Individual Task Level (e.g. patch-db-01)              │                                    │
│  │   • on_start  • on_finish  • on_failed                  │                                    │
│  │   • include_report_link: true | false                   │                                    │
│  └───────────────────────────┬─────────────────────────────┘                                    │
│                              │                                                                  │
│             ┌────────────────┴────────────────┐                                                 │
│             ▼                                 ▼                                                 │
│   Delivery Mode: "link" (Default)   Delivery Mode: "attach" or "both"                           │
│   • Outlook-Safe MSO Table Email    • Attaches published HTML report artifact as MIME attachment│
│   • Status Badge & Duration         • Avoids Outlook body rendering breakage                    │
│   • [View Published Report] Button  • Ideal for offline archiving & compliance audits           │
│     (with signed view token)                                                                    │
│             │                                 │                                                 │
│             └────────────────┬────────────────┘                                                 │
│                              │ SMTP Relay (TLS / STARTTLS)                                      │
│                              ▼                                                                  │
│    ┌───────────────────┬───────────────────┬───────────────────┐                                │
│    ▼                   ▼                   ▼                   ▼                                │
│  Team A (DBAs)       Team B (Web Ops)    On-Call SRE         Security / Audit                   │
│  (DB Groups 1 & 2)   (Web Groups 3,4,5)  (Pipeline Failures) (Compliance HTML)                  │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 16.1 The Architectural Philosophy: Ansible Publishes, Crucible Delivers

In traditional Ansible automation, administrators frequently attempt to make
playbooks send their own emails using `community.general.mail`. This approach
suffers from critical operational flaws:
1. **Coupling Execution with Delivery**: Hardcoding SMTP servers, recipient
   addresses, and email logic directly into Ansible playbooks polutes
   automation code with transport concerns and exposes email credentials across
   Git repositories.
2. **Failure Blindspots**: If an Ansible playbook fails mid-execution or
   crashes during pre-flight tasks, downstream email tasks never execute,
   leaving operators blind to outages.
3. **The Microsoft Outlook Rendering Barrier**:
   * **Classic Outlook (Windows Desktop)**: Uses Microsoft Word's legacy layout
     engine (`MSO`), which does not support HTML5, CSS Grid, Flexbox, media
     queries, CSS variables, or JavaScript.
   * **New Outlook (Webview / Chromium)**: Strips embedded scripts, interactive
     components, and external stylesheets for security.
   * Attempting to render a rich Ansible HTML report (e.g. ARA summaries, JUnit
     tables, or custom styled metrics) directly inside an email body results in
     a broken, unreadable mess across different Outlook clients.

#### Solution: Publish Report + Send Outlook-Safe Notification Link
* **Ansible Publishes**: Ansible tasks generate their rich HTML reports and
  write them directly to the pre-created reports directory:
  `$CRUCIBLE_REPORT_DIR/${CRUCIBLE_RUN_ID}_${JOB_SLUG}.html`
* **Crucible Hosts**: Crucible stores and serves the published report natively
  on its web server with full modern CSS, JavaScript charts, and interactive
  filtering completely intact.
* **Crucible Notifies**: Crucible's notification engine generates a rock-solid,
  minimalist, table-based transactional notification email tested for 100%
  rendering fidelity across Classic Outlook (`<!--[if mso]>`), New Outlook,
  Apple Mail, and mobile clients.
* **Interactive Button**: If report linking is enabled for that task
  (`include_report_link: true`), the notification includes an unmissable,
  high-contrast action button: **`[View Published Report]`** directing
  recipients directly to the hosted artifact.

### 16.2 Standardized Event Triggers & Per-Task Report Control
Notifications can be configured with granular event triggers:
* **`on_start` (or `start`)**: Dispatched immediately when the task or pipeline
  begins execution (blue status badge).
* **`on_finish` (or `finish` / `on_success`)**: Dispatched when the task or
  pipeline finishes successfully with exit code 0 (green success badge,
  duration).
* **`on_failed` (or `failed` / `on_failure`)**: Dispatched immediately if a
  task fails or times out (red critical badge, last 20 lines of log/stderr).
* **`always`**: Dispatched unconditionally upon completion regardless of exit
  code.

#### Per-Task Report Link Configuration (`include_report_link`)
Every task within a pipeline can independently declare whether a report link
should be generated and included:
* **`publish_report: true | false`**: Informs Crucible that this task writes an
  HTML report to `$CRUCIBLE_REPORT_DIR`.
* **`include_report_link: true | false`**: When `true`, Crucible generates a
  direct URL (or signed token link for private jobs) to that task's published
  report and embeds the action button in the completion email.
* If `include_report_link: false` (e.g. for quick maintenance steps like
  restarting a service), the email functions purely as a status notification
  without cluttering the recipient with empty report buttons.

### 16.3 Real-World Multi-Team Fleet Patching Specification
Consider a rolling security update patching 100+ servers split into multiple
groups across a single job pipeline. Different teams require different
visibility:

```yaml
name: "Rolling Fleet Security Patching"
visibility: private

# Pipeline root fallback: SRE on-call gets an immediate alert if the overall pipeline fails
notify:
  on_failed:
    to: ["oncall-sre@company.com"]
    include_report_link: true

jobs:
  # Cluster 1 & 2: Database Tier -> Managed exclusively by Team A (DBAs)
  - cluster: Database-Primary-Group1
    tasks:
      - name: patch-db-primary-01
        command: "ansible-playbook -i inventory/db.yml playbooks/patch_db.yml --limit db-01"
        publish_report: true # Generates HTML report in $CRUCIBLE_REPORT_DIR/patch-db-primary-01.html
        notify:
          on_start:
            to: ["dba-ops@company.com"]
          on_finish:
            to: ["dba-team@company.com"]
            include_report_link: true # Emails Team A with direct link to the DB 1 report
          on_failed:
            to: ["dba-critical@company.com", "oncall-sre@company.com"]
            include_report_link: true

      - name: patch-db-primary-02
        command: "ansible-playbook -i inventory/db.yml playbooks/patch_db.yml --limit db-02"
        publish_report: true
        notify:
          on_finish:
            to: ["dba-team@company.com"]
            include_report_link: true

  - cluster: Database-Replica-Group2
    tasks:
      - name: patch-db-replica-01
        command: "ansible-playbook -i inventory/db.yml playbooks/patch_db.yml --limit db-03"
        publish_report: true
        notify:
          on_finish:
            to: ["dba-team@company.com"]
            include_report_link: true

      - name: verify-replication-state
        command: "ansible-playbook -i inventory/db.yml playbooks/verify_repl.yml"
        publish_report: false # Quick check; no HTML report published
        notify:
          on_failed:
            to: ["dba-critical@company.com"]
            include_report_link: false # Plain status alert; no report link

  # Cluster 3, 4, 5: Web Tier -> Joint report sent to both Team A and Team B
  - cluster: Web-Frontend-Group3
    tasks:
      - name: patch-web-nodes
        command: "ansible-playbook -i inventory/web.yml playbooks/patch_web.yml"
        publish_report: true
        notify:
          on_finish:
            to: ["dba-team@company.com", "web-team@company.com"]
            include_report_link: true # Joint notification to Team A and Team B with web report link
          on_failed:
            to: ["web-critical@company.com", "oncall-sre@company.com"]
            include_report_link: true
```

### 16.4 Delivery Modes: Link vs. Attachment vs. Both
Configurable via `mode: link | attach | both`:

| Delivery Mode                    | Description                                                                                                                   | Ideal Use Case                                                                                          |
| -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| **`mode: link`** *(Recommended)* | Sends an Outlook-safe HTML notification with an execution summary table and a prominent **`[View Published Report]`** button. | Standard production workflows. Avoids Outlook HTML rendering breakage and keeps mailboxes lightweight.  |
| **`mode: attach`**               | Attaches the published HTML report artifact (`var/reports/{shard}/{ulid}_{slug}.html`) directly as a MIME attachment.         | Compliance audits, offline archiving, or external stakeholders who cannot access internal web networks. |
| **`mode: both`**                 | Includes the interactive dashboard link in the email body **and** attaches the published HTML report file.                    | Critical production reviews or multi-team sign-offs.                                                    |

#### Signed View Tokens for Private Runs
When a run is marked `visibility: private`, anonymous users cannot access
`/runs/{ulid}` or `/reports/{shard}/*`.
To allow external stakeholders, team leads, or executives to view a specific
report from an email link without needing a Linux PAM account, Crucible
generates an ephemeral, cryptographically signed **Report View Token**:
`https://crucible.internal/reports/01/JB/01JBCDEF...html?token=tok_...`
* Stored in SQLite (`report_view_tokens`) with a configurable TTL (default: 7
  days).
* Grants strictly scoped read-only access to that single report/run without
  exposing other private jobs, inventories, or admin settings.

### 16.5 Manual On-Demand Report Dispatching
In addition to automated event-driven emails, operators can manually dispatch
reports on demand:

#### Web UI Interactive Modal
1. On any Run details page or Cluster/Task row, operators can click the
   **"Email Report"** button.
2. An interactive modal opens with:
   * **Scope Selector**: Entire Run, or a specific Cluster/Task.
   * **Recipients Input**: Pre-populated with the configured YAML recipients;
     operator can add/remove email addresses.
   * **Mode Selector**: `Link`, `Attachment`, or `Both`.
   * **Optional Personal Note**: Text area for operator annotations (e.g.
     *"Reviewed and verified clean after maintenance window"*).
3. Clicking **"Send Email"** dispatches the message via SMTP in the background.

#### Operator CLI (`cruciblectl notify send`)
```bash
# Email entire run report as a link to team leads
cruciblectl notify send run_01JBCDEFGHJKMNPQRSTVWXYZ01 \
  --to "lead-a@company.com,lead-b@company.com" \
  --mode link \
  --note "Routine maintenance completed successfully"

# Email a specific task's report with attached HTML artifact
cruciblectl notify send run_01JBCDEFGHJKMNPQRSTVWXYZ01 \
  --task "patch-db-primary-01" \
  --to "dba-team@company.com" \
  --mode attach
```

### 16.6 SMTP Configuration & Secrets Engine Integration
SMTP relay settings are configured in `crucibled.yaml` or via CLI flags:
```yaml
smtp:
  host: "smtp.mailgun.org"
  port: 587
  user: "postmaster@myorg.com"
  password_secret: "smtp_relay_password" # Fetched directly from internal/secrets/
  from: "Crucible Orchestrator <crucible-notifications@myorg.com>"
  tls_mode: "starttls" # "starttls" | "tls" | "none"
  web_base_url: "https://crucible.myorg.internal:8443"
```
* **Zero Plaintext Credentials**: The SMTP password can reference a secret
  stored in Crucible's built-in secrets engine (`password_secret:
  "smtp_relay_password"`), keeping production credentials safely encrypted
  under the Master Key.
* **Asynchronous Dispatch**: The notifier runs asynchronously in background
  worker goroutines; slow or unresponsive SMTP servers will never stall running
  pipelines.

---

## 17. Modular Flat Feature Package Architecture

To ensure operational maintainability, avoid circular dependencies, and prevent
monolithic "dumping ground" packages, Crucible strictly uses a **Flat
Package-by-Feature Layout** inside `internal/`.

There are **NO** artificial horizontal layers (`domain/`, `application/`,
`transport/`, `ports/`). Instead, every business capability is an independent,
self-contained feature package directly under `internal/`:

```
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                 FLAT FEATURE PACKAGE ARCHITECTURE                               │
│                                                                                                 │
│    Web UI Dashboard              Operator CLI                Prometheus / Grafana               │
│    (cmd/crucibled/server_http)       (cmd/cruciblectl)               (GET /metrics)             │
│               │                         │                               │                       │
│               └─────────────────────────┼───────────────────────────────┘                       │
│                                         ▼                                                       │
│               ┌───────────────────────────────────────────────────┐                             │
│               │   Daemon Core Entrypoint (cmd/crucibled/main.go)  │                             │
│               │     • HTTP/SSE Router  • gRPC Service Listener    │                             │
│               │     • TLS & ACME Engine (TLS-ALPN-01 & DNS-01)    │                             │
│               │     • Personal Access Token & RBAC Interceptors   │                             │
│               └─────────────────────────┬─────────────────────────┘                             │
│                                         │ Direct Function Calls & Feature APIs                  │
│                                         ▼                                                       │
│ ┌─────────────────────────────────────────────────────────────────────────────────────────────┐ │
│ │                                  SELF-CONTAINED FEATURE PACKAGES                            │ │
│ │                                                                                             │ │
│ │  ┌───────────────────────┐  ┌───────────────────────┐  ┌─────────────────────────────────┐  │ │
│ │  │ internal/ansible      │  │ internal/git          │  │ internal/cluster                │  │ │
│ │  │ • Inventory Models    │  │ • Working Tree Client │  │ • Worker Pool & Telemetry       │  │ │
│ │  │ • YAML / INI Parsers  │  │ • 5m Sync Timer       │  │ • Drain Lifecycle (Active/Drain)│  │ │
│ │  │ • NetBox Plugin Engine│  │ • Pre-Edit Pull Hook  │  │ • Health Watchdog (UP/DOWN)     │  │ │
│ │  │ • Host/Group Tree     │  │ • Working Tree Mutex  │  │ • Distributed Leases & Fencing  │  │ │
│ │  │ • Precedence Resolver │  │ • Pre-Commit Rebase   │  │ • Split-Brain Freeze (ISOLATED) │  │ │
│ │  │ • Dynamic JSON Export │  │ • Atomic Commit & Push│  │ • P2P Mesh & Commit Broadcast   │  │ │
│ │  └───────────────────────┘  └───────────────────────┘  └─────────────────────────────────┘  │ │
│ │                                                                                             │ │
│ │  ┌───────────────────────┐  ┌───────────────────────┐  ┌─────────────────────────────────┐  │ │
│ │  │ internal/auth         │  │ internal/tokens       │  │ internal/engine                 │  │ │
│ │  │ • Linux PAM Engine    │  │ • PAT Generator       │  │ • Subprocess Engine (os/exec)   │  │ │
│ │  │ • Multi-Device TOTP   │  │ • SHA-256 Hash Verify │  │ • Python venv Execution Engine  │  │ │
│ │  │ • TOTP Invite URLs    │  │ • Single-Token Revoke │  │ • Direct Git Copy Execution     │  │ │
│ │  │ • Session Cookies     │  │ • Multi-Token for CLI │  │ • Remote Worker gRPC Runner     │  │ │
│ │  │ • POSIX Group RBAC    │  │ • ULID Identifiers    │  │ • Fan-Out Log Broadcaster       │  │ │
│ │  └───────────────────────┘  └───────────────────────┘  └─────────────────────────────────┘  │ │
│ │                                                                                             │ │
│ │  ┌───────────────────────┐  ┌───────────────────────┐  ┌─────────────────────────────────┐  │ │
│ │  │ internal/ssh          │  │ internal/tls          │  │ internal/storage                │  │ │
│ │  │ • Ed25519 & RSA Keys  │  │ • Pure Go TLS Server  │  │ • SQLite WAL Engine             │  │ │
│ │  │ • Git Deploy Key      │  │ • Custom .crt/.key    │  │ • schema/*.sql (Raw SQL Only)   │  │ │
│ │  │ • Ansible Nodes Key   │  │ • ACME TLS-ALPN-01    │  │ • ULID Primary Keys             │  │ │
│ │  │ • Handshake Tester    │  │ • ACME DNS-01         │  │ • Store Repositories            │  │ │
│ │  └───────────────────────┘  └───────────────────────┘  └─────────────────────────────────┘  │ │
│ │                                                                                             │ │
│ │  ┌───────────────────────┐  ┌───────────────────────┐  ┌─────────────────────────────────┐  │ │
│ │  │ internal/schedule     │  │ internal/metrics      │  │ internal/notify & events        │  │ │
│ │  │ • Cron (Seconds Prec) │  │ • Prometheus Gauges   │  │ • internal/notify (Dispatch)    │  │ │
│ │  │ • One-time Timers     │  │ • Peer Aggregation    │  │ • internal/events (Event Bus)   │  │ │
│ │  └───────────────────────┘  └───────────────────────┘  └─────────────────────────────────┘  │ │
│ │                                                                                             │ │
│ │  ┌───────────────────────────────────────────────────┐ ┌─────────────────────────────────┐  │ │
│ │  │ internal/secrets                                  │ │ internal/reports                │  │ │
│ │  │ • Vault Seal/Unseal Engine (AES-256-GCM + Argon2) │ │ • Zero-RAM Log Broadcaster      │  │ │
│ │  │ • Shamir Secret Sharing (k-of-n) & Auto-Unseal    │ │ • ULID 2-Level Sharding         │  │ │
│ │  │ • Pure-Go Ephemeral FIFO Named Pipe for Ansible   │ │ • Late-Join Tailing Reader      │  │ │
│ │  │ • Zero Plaintext Passwords Saved to Disk          │ │ • Public/Private Report Gate    │  │ │
│ │  └───────────────────────────────────────────────────┘ └─────────────────────────────────┘  │ │
│ └─────────────────────────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 18. CQRS Specification

### Commands (Write Operations — State Altering)

| Command                        | Payload                                                                              | Side Effects / Domain Events                                                                                                                               |
| ------------------------------ | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `UpdateWorkerAvailabilityCmd`  | `WorkerID` (ULID), `Availability` (`active`\|`drain`\|`pause`)                       | Changes node scheduling eligibility, allows in-flight jobs to finish, emits `WorkerAvailabilityChangedEvent`                                               |
| `EvaluateWorkerHealthCmd`      | `WorkerID` (ULID), `MissedHeartbeats`                                                | Marks dead nodes `DOWN`, fails or reroutes in-flight jobs, emits `WorkerWentDownEvent`                                                                     |
| `UpdateClusterStateCommand`    | `NodeID`, `NewState`, `Reason`                                                       | Updates cluster connectivity state (`HEALTHY` vs `ISOLATED`), engages/disengages execution interlock                                                       |
| `CreateTokenCommand`           | `Name`, `Username`, `Role`, `ExpiresAt`                                              | Generates `cb_pat_<hex>` with ULID ID, computes SHA-256 hash, stores in DB (allows multiple PATs per user), emits `TokenCreatedEvent`                      |
| `RevokeTokenCommand`           | `TokenID` (ULID), `Username`                                                         | Marks single token revoked in DB without affecting other tokens, invalidates active CLI connections, emits `TokenRevokedEvent`                             |
| `AuthenticatePAMCommand`       | `Username`, `Password`                                                               | Verifies PAM credentials against Linux system accounts, creates pre-auth challenge token                                                                   |
| `Verify2FACommand`             | `ChallengeToken`, `Passcode`                                                         | Validates 6-digit TOTP code against ALL active registered devices for that user, creates session, updates device `last_used_at`, emits `UserLoggedInEvent` |
| `Enroll2FADeviceCommand`       | `Username`, `DeviceName`, `Secret`, `Code`                                           | Validates initial TOTP setup, persists secret with ULID in `user_totp_devices` (supports multiple 2FA apps per user e.g. shared `root`), marks enabled     |
| `CreateTOTPInviteCommand`      | `Username`, `DeviceLabel`, `ExpiresIn`                                               | Generates 24h cryptographically signed invite URL (`/enroll/2fa?invite=<ulid>`) stored in `totp_invites`                                                   |
| `AcceptTOTPInviteCommand`      | `InviteToken`, `PAMPassword`, `Secret`, `Code`                                       | Verifies PAM password, registers new device for account, marks invite consumed, emits `TOTPDeviceRegisteredEvent`                                          |
| `Revoke2FADeviceCommand`       | `DeviceID` (ULID), `Username`                                                        | Revokes single 2FA device without locking out other devices for that user                                                                                  |
| `RevokeSessionCommand`         | `SessionID` (ULID)                                                                   | Invalidates active user session, clears auth cookies                                                                                                       |
| `GenerateSSHKeyCommand`        | `Purpose` (`git_sync`\|`ansible_nodes`), `KeyType` (`ed25519`\|`rsa`), `Name`        | Generates keypair with ULID ID, stores private key encrypted in DB                                                                                         |
| `ImportSSHKeyCommand`          | `Purpose`, `PrivateKeyPEM`, `Name`                                                   | Validates and encrypts private key in `ssh_keys`                                                                                                           |
| `AcquireJobLeaseCommand`       | `SlotKey`, `NodeID`, `TTL`                                                           | Atomic lease reservation, fences peer nodes, guarantees single execution                                                                                   |
| `HeartbeatLeaseCommand`        | `SlotKey`, `NodeID`, `FencingToken`                                                  | Extends active execution lease during job execution                                                                                                        |
| `SyncPeerStateCommand`         | `EventPayload`, `SourceNodeID`                                                       | Applies replicated schedule, run, or status update from cluster peer                                                                                       |
| `BroadcastCommitCommand`       | `Repo`, `Branch`, `CommitHash`                                                       | Broadcasts commit to cluster peers over gRPC, triggering instant `git pull --ff-only`                                                                      |
| `RunPipelineCommand`           | `ConfigPath`, `Env`, `Target`, `VenvOverride`                                        | Verifies `HEALTHY` state & available active worker, acquires lease, spawns run with ULID                                                                   |
| `CancelRunCommand`             | `RunID` (ULID), `Reason`                                                             | Validates role, terminates local/remote subprocesses, marks run `cancelled`                                                                                |
| `RegisterWorkerCommand`        | `WorkerID`, `Hostname`, `Datacenter`, `Labels`                                       | Registers remote worker in memory and DB, defaults availability to `active`                                                                                |
| `HeartbeatWorkerCommand`       | `WorkerID`, `Telemetry`                                                              | Updates worker telemetry and last-seen timestamp                                                                                                           |
| `CreateScheduleCommand`        | `Name`, `ConfigPath`, `CronExpr`, `RunAt`                                            | Validates role, saves schedule with ULID to DB, registers timer with `Scheduler`                                                                           |
| `DeleteScheduleCommand`        | `ScheduleID` (ULID)                                                                  | Removes timer from `Scheduler`, deletes record from DB                                                                                                     |
| `CreateInventoryCommand`       | `ID`, `Name`, `Format`, `Content`, `Variables`                                       | Validates syntax, computes host/group counts, saves to git & DB, emits `InventoryCreatedEvent`, replicates to peers                                        |
| `UpdateInventoryCommand`       | `ID`, `Content`, `Variables`                                                         | Re-validates syntax, executes atomic git rebase-commit-push, updates DB, emits `InventoryUpdatedEvent`, replicates to peers                                |
| `DeleteInventoryCommand`       | `ID`                                                                                 | Removes file from git & DB, emits `InventoryDeletedEvent`, replicates deletion to peers                                                                    |
| `SaveVarsFileCommand`          | `RelPath`, `Content`, `Author`                                                       | Validates YAML syntax, acquires git mutex, rebases, commits & pushes to git, emits `VarsUpdatedEvent`                                                      |
| `DeleteVarsFileCommand`        | `RelPath`, `Author`                                                                  | Removes vars file from git, commits & pushes, emits `VarsUpdatedEvent`                                                                                     |
| `SyncNetBoxInventoryCommand`   | `ConfigFile`                                                                         | Executes `ansible-inventory` inside Python venv, caches parsed NetBox topology in SQLite                                                                   |
| `InitializeVaultCommand`       | `Passphrase`, `SharesTotal`, `SharesThreshold`                                       | Derives Master KEK via Argon2id, generates Shamir shares, initializes singleton metadata, transitions to `UNSEALED`                                        |
| `UnsealVaultCommand`           | `KeyShare` or `Passphrase`                                                           | Validates and accumulates share, reconstructs Master KEK in guarded memory, unlocks secrets, emits `VaultUnsealedEvent`                                    |
| `SealVaultCommand`             | (None)                                                                               | Zeroizes Master KEK in volatile RAM, transitions to `SEALED`, emits `VaultSealedEvent`                                                                     |
| `SetVaultSecretCommand`        | `Name`, `SecretType`, `PlaintextValue`, `Description`, `Author`                      | Encrypts value with AES-256-GCM using in-memory KEK, stores in `vault_secrets` with ULID                                                                   |
| `DeleteVaultSecretCommand`     | `SecretID` (ULID)                                                                    | Deletes encrypted record from `vault_secrets`, emits `VaultSecretDeletedEvent`                                                                             |
| `SetRunVisibilityCommand`      | `RunID` (ULID), `Visibility` (`public` or `private`)                                 | Toggles run visibility state in DB, controls anonymous access to logs and reports                                                                          |
| `SendEmailNotificationCommand` | `RunID` (ULID), `ClusterName`, `JobName`, `Recipients`, `Mode`, `Note`, `SenderUser` | Resolves delivery mode, attaches HTML reports if requested, dispatches email via SMTP                                                                      |
| `CreateReportViewTokenCommand` | `RunID` (ULID), `TTL`                                                                | Generates cryptographically signed view token for external sharing of private reports                                                                      |

### Queries (Read Operations — Projection & Side-Effect Free)

| Query                          | Parameters                                               | Output Model                                                                                |
| ------------------------------ | -------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `GetWorkerQuery`               | `WorkerID` (ULID)                                        | `WorkerDetails` (ID, datacenter, labels, availability, health, active_jobs, telemetry)      |
| `ListWorkersQuery`             | `DatacenterFilter`, `AvailabilityFilter`, `HealthFilter` | `[]WorkerSummary` (table of nodes, health, drain state, active tasks)                       |
| `GetClusterHealthQuery`        | (None)                                                   | `ClusterHealth` (state: `HEALTHY`\|`ISOLATED`, peers_connected, active_leases)              |
| `ValidateTokenQuery`           | `PlaintextToken`                                         | `TokenVerificationResult` (valid, username, role, scopes)                                   |
| `ListTokensQuery`              | `UsernameFilter`                                         | `[]TokenMetadata` (ULID, name, prefix, role, created, last_used, expires, revoked)          |
| `List2FADevicesQuery`          | `Username`                                               | `[]TOTPDeviceSummary` (DeviceID ULID, Name, CreatedAt, LastUsedAt, Enabled)                 |
| `ValidateTOTPInviteQuery`      | `InviteToken`                                            | `TOTPInviteInfo` (valid, username, label, expires_at)                                       |
| `GetSSHKeyQuery`               | `Purpose` (`git_sync`\|`ansible_nodes`)                  | `SSHKeyDetails` (ULID, purpose, name, key_type, public_key, fingerprint, created_at)        |
| `TestSSHConnectionQuery`       | `Purpose`, `TargetHost`                                  | `SSHTestResult` (success, banner, latency_ms, error)                                        |
| `GetCurrentUserQuery`          | `SessionToken`                                           | `UserProfile` (username, role, permissions, 2fa_enabled, 2fa_device_count) or `Anonymous`   |
| `ValidateSessionQuery`         | `SessionToken`                                           | `SessionState` (valid, role, expires_at)                                                    |
| `GetRunQuery`                  | `RunID` (ULID)                                           | `RunDetails` (status, duration, target worker, individual job breakdown, exit codes)        |
| `ListRunsQuery`                | `Limit`, `Offset`, `StatusFilter`, `WorkerFilter`        | `PagedRuns` (total count, run summaries)                                                    |
| `StreamLogsQuery`              | `RunID` (ULID), `JobName`, `Follow`                      | Stream of `LogChunk` (stdout/stderr lines, timestamp, ANSI escape sequences)                |
| `GetScheduleQuery`             | `ScheduleID` (ULID)                                      | `ScheduleDetails` (cron expression, next run time, last run status)                         |
| `ListSchedulesQuery`           | `IncludeDisabled`                                        | `[]ScheduleSummary`                                                                         |
| `ListClusterPeersQuery`        | (None)                                                   | `[]PeerSummary` (NodeID, Address, Latency, SyncStatus, LastSeen)                            |
| `ListClusterLocksQuery`        | (None)                                                   | `[]ActiveLease` (SlotKey, HolderNodeID, FencingToken, ExpiresAt)                            |
| `GetServerMetricsQuery`        | (None)                                                   | `ServerStatus` (connected workers, peer replication health, active jobs, DB size)           |
| `GetInventoryQuery`            | `ID`, `Format`                                           | `InventoryDetails` (ID, Name, Format, Content, Variables, HostCount, GroupCount, UpdatedAt) |
| `ListInventoriesQuery`         | (None)                                                   | `[]InventorySummary` (ID, Name, Format, HostCount, GroupCount, UpdatedAt, Author)           |
| `ValidateInventoryQuery`       | `Content`, `Format`                                      | `ValidationResult` (Valid, Errors with LineNumbers, ResolvedHostCount, GroupNames)          |
| `GetVarsFileQuery`             | `RelPath`                                                | `VarsFileDetails` (RelPath, Scope, Content, ParsedVars, LastModified, Author)               |
| `ListVarsFilesQuery`           | `ScopeFilter`                                            | `[]VarsFileSummary` (RelPath, Scope, Size, Author, UpdatedAt)                               |
| `ResolveHostVarsQuery`         | `Hostname`                                               | `ResolvedHostVars` (Merged key-values with provenance source traces)                        |
| `GetNetBoxInventoryQuery`      | (None)                                                   | `NetBoxTopology` (Sites, Racks, Roles, Platforms, Devices list)                             |
| `GetVaultStatusQuery`          | (None)                                                   | `VaultStatus` (IsInitialized, IsSealed, SharesTotal, SharesThreshold, Progress)             |
| `ListVaultSecretsQuery`        | `TypeFilter`                                             | `[]VaultSecretSummary` (ID ULID, Name, SecretType, Description, CreatedAt, UpdatedAt)       |
| `GetHTMLReportQuery`           | `RunID` (ULID), `JobSlug`, `RequesterRole`               | `ReportFile` (evaluates `visibility` vs role, returns 404 if unauthorized)                  |
| `StreamDiskLogsQuery`          | `RunID` (ULID), `JobSlug`, `RequesterRole`, `Follow`     | Stream of `LogChunk` (evaluates `visibility`, tails `var/reports/...`)                      |
| `ValidateReportViewTokenQuery` | `Token`, `RunID` (ULID)                                  | `ViewTokenStatus` (valid, run_id, expires_at)                                               |

---

## 19. Directory Structure (Flat Feature Package Layout)

```
crucible/
├── Makefile                     # Build, test, cross-compilation, and demo targets
├── README.md                    # User and operator documentation
├── PLAN.md                      # Comprehensive architecture and design specifications
├── TODO                         # Step-by-step master implementation checklist
├── TODO.md                      # Markdown implementation checklist (reference)
├── go.mod
├── go.sum
│
├── cmd/
│   ├── crucibled/                   # Unified Server Daemon
│   │   ├── main.go              # Entrypoint (CLI flags: --mode=standalone|controller|worker)
│   │   ├── main_test.go         # Daemon CLI entrypoint and flag parsing tests
│   │   ├── root.go              # Root server Cobra command
│   │   ├── version.go           # `crucibled version` Cobra command
│   │   ├── server_grpc.go       # gRPC server (Token Auth interceptor, JobService, TokenService, etc.)
│   │   ├── server_http.go       # HTTP/SSE Web UI router, /metrics scraper & reverse proxy
│   │   ├── peer_manager.go      # Outbound P2P mesh connection manager & commit broadcaster
│   │   ├── worker_client.go     # Outbound connection manager for --mode=worker
│   │   ├── cluster_monitor.go   # Heartbeat monitor & split-brain watchdog
│   │   │
│   │   ├── handlers_auth.go     # Web handlers: /login, /login/2fa, /logout, /settings/2fa, /enroll/2fa
│   │   ├── handlers_tokens.go   # Web handlers: /settings/tokens (Multiple PATs create/revoke)
│   │   ├── handlers_ssh.go      # Web handlers: /settings/ssh (Generate, Show public key, Import, Test)
│   │   ├── handlers_workers.go  # Web handlers: /cluster/workers (Drain, Active, Pause nodes)
│   │   ├── handlers_inventory.go# Web handlers: /inventory (YAML/INI view, edit, validate, export)
│   │   ├── handlers_vars.go     # Web handlers: /vars (host_vars, group_vars, custom vars & precedence)
│   │   ├── handlers_dashboard.go# Web handlers: /, /runs, /runs/{id}, /schedules, /cluster
│   │   ├── middleware_rbac.go   # Transparent session resolution & RBAC permission gate
│   │   │
│   │   └── web/                 # Embedded Web UI assets
│   │       ├── embed.go         # //go:embed static/* templates/*
│   │       ├── templates/       # HTML templates
│   │       │   ├── layout.html  # Base shell (Login/Logout, cluster alert banner)
│   │       │   ├── login.html   # PAM credentials form
│   │       │   ├── 2fa.html     # TOTP 6-digit challenge form
│   │       │   ├── 2fa_devices.html # Multi-device 2FA management (List, Invite URL, QR, Revoke)
│   │       │   ├── 2fa_enroll.html  # Invite enrollment view
│   │       │   ├── tokens.html  # Personal Access Tokens view (Multiple PATs create/revoke)
│   │       │   ├── ssh_keys.html# SSH Key management view (Git deploy key & Ansible host key)
│   │       │   ├── dashboard.html  # Public read-only runs list
│   │       │   ├── run_detail.html # Real-time log view (xterm.js)
│   │       │   ├── schedules.html  # Schedules list & create modal
│   │       │   ├── inventory.html  # Inventories catalog and NetBox infrastructure toggle
│   │       │   ├── inventory_detail.html # Split-pane editor (tree view + code editor + validator)
│   │       │   ├── vars.html       # Variables Studio catalog (group_vars, host_vars, custom vars)
│   │       │   ├── vars_detail.html# Variable editor & precedence inspector
│   │       │   ├── cluster.html    # Peers mesh, health status, active leases & workers
│   │       │   └── workers.html    # Worker nodes table with Drain/Active buttons & health badges
│   │       └── static/          # CSS, JS (Tailwind, xterm.js, SVG icons)
│   │
│   └── cruciblectl/                 # Operator CLI Client
│       ├── main.go              # Entrypoint
│       ├── main_test.go         # CLI entrypoint, flag parsing, and buffer injection tests
│       ├── root.go              # Root Cobra command & global flags (--server, --token)
│       ├── version.go           # `cruciblectl version` Cobra command
│       ├── auth.go              # `cruciblectl auth login` & `cruciblectl auth 2fa [invite|devices|revoke]`
│       ├── token.go             # `cruciblectl token [create|list|revoke]` (Multiple PAT tokens)
│       ├── ssh.go               # `cruciblectl ssh [generate|show|import|test]` (Deploy and host keys)
│       ├── worker.go            # `cruciblectl worker [list|drain|active|pause|status]`
│       ├── inventory.go         # `cruciblectl inventory [list|get|set|create|edit|hosts|validate|delete|export]`
│       ├── vars.go              # `cruciblectl vars [list|get|set|edit|resolve]`
│       ├── run.go               # Dispatches run pipeline, follows logs (--venv override)
│       ├── logs.go              # Dispatches log streaming
│       ├── status.go            # Dispatches status query
│       ├── cluster.go           # Dispatches cluster query
│       ├── schedule.go          # Dispatches schedule commands
│       └── cancel.go            # Dispatches run cancellation
│
├── proto/
│   └── crucible/
│       └── crucible.proto           # Protobuf definitions for client, worker mesh & peer mesh RPCs
│
├── gen/                         # Generated protobuf Go code
│   └── crucible/
│       ├── crucible.pb.go
│       └── crucible_grpc.pb.go
│
└── internal/                    # Flat Package-by-Feature Architecture (Zero Layer Nesting)
    ├── version/                 # Hexagonal CQRS Application Version & Build Metadata Subsystem
    │   ├── models.go            # BuildMetadata, DependencyInfo, Info domain types and validation
    │   ├── ports.go             # MetadataProvider outbound port, QueryHandler inbound port
    │   ├── queries.go           # GetVersionQuery and QueryService CQRS coordinator
    │   ├── adapter_runtime.go   # Concrete adapter querying linker ldflags, runtime, and debug.BuildInfo
    │   └── version_test.go      # Table-driven unit tests isolating query logic
    │
    ├── ui/                      # Lip Gloss Terminal UI Styling & Command Views
    │   ├── styles.go            # Adaptive color palettes, bold headers, Success/Error message helpers
    │   ├── views.go             # ASCII banner logo, custom Cobra HandleUsage & HandleHelp formatters
    │   ├── version.go           # Formatted tabular version output (PrintVersion) using tabwriter
    │   └── ui_test.go           # Unit tests for message styling, usage rendering, and version output
    │
    ├── ansible/                 # Ansible Inventory, NetBox & Variables Feature
    │   ├── models.go            # Inventory, HostVars, GroupVars, CustomVars, Group, Host
    │   ├── parser_yaml.go       # YAML inventory & vars parser
    │   ├── parser_ini.go        # Classic INI inventory parser
    │   ├── netbox.go            # NetBox dynamic inventory plugin runner & topology parser
    │   ├── tree.go              # Host & group hierarchy tree extraction
    │   ├── resolver.go          # Effective merged variable precedence engine
    │   ├── validator.go         # Real-time pre-commit syntax validator
    │   └── dynamic.go           # Dynamic JSON exporter for ansible-playbook
    │
    ├── git/                     # Git Repository Synchronization Feature
    │   ├── client.go            # Git CLI / pure Go client (clone, pull, rebase, commit, push)
    │   ├── sync.go              # Background timer pull (5m) & on-demand pre-edit pull hook
    │   ├── lock.go              # Working tree mutex lock (prevents concurrent edit races)
    │   └── p2p_sync.go          # Commit broadcast listener and instant pull trigger
    │
    ├── cluster/                 # Worker Fleet, Leases & Mesh Replication Feature
    │   ├── models.go            # WorkerNode, WorkerTelemetry, ClusterPeer, ClusterLease
    │   ├── worker_pool.go       # Worker registry, drain mode & least-loaded routing
    │   ├── telemetry.go         # Pure Go /proc sampler (CPU %, Load, RAM)
    │   ├── monitor.go           # Heartbeat watchdog & split-brain execution freeze
    │   ├── lease.go             # Monotonic fencing token & distributed lock manager
    │   └── peer_manager.go      # P2P gRPC mesh connection & state replicator
    │
    ├── engine/                  # Job Execution & Subprocess Feature
    │   ├── models.go            # Pipeline, Job, Run, JobEvent, LogChunk
    │   ├── venv.go              # Python venv detector, path resolution & environment injection
    │   ├── exec_local.go        # Local subprocess engine with direct working-copy execution
    │   ├── exec_remote.go       # Remote worker gRPC job runner
    │   ├── broadcaster.go       # Non-blocking fan-out log broadcaster & disk logger
    │   └── pipeline.go          # Pipeline YAML parser & DAG runner
    │
    ├── auth/                    # Linux PAM, Multi-Device 2FA & Session Feature
    │   ├── models.go            # User, UserSession, TOTPDevice, TOTPInvite, Role, Permission
    │   ├── pam.go               # Linux PAM (libpam.so.0) authenticator
    │   ├── totp.go              # Multi-device RFC 6238 TOTP (multiple tokens per user, e.g. root)
    │   ├── invite.go            # Secure TOTP invite URL generator & validator for shared accounts
    │   ├── session.go           # Cryptographic web sessions & challenge tokens
    │   └── rbac.go              # Role resolution (POSIX groups -> Viewer/Operator/Admin)
    │
    ├── tokens/                  # Personal Access Tokens (PATs) Feature
    │   ├── models.go            # APIToken, TokenMetadata (using ULID identifiers)
    │   └── token.go             # Token generator (cb_pat_...), SHA-256 hasher, validator & revoker
    │
    ├── ssh/                     # Built-in SSH Key Management Feature
    │   ├── models.go            # SSHKey (git_sync, ansible_nodes), KeyPair
    │   ├── generator.go         # Pure Go Ed25519 & RSA 4096-bit keypair generator
    │   └── tester.go            # TCP SSH handshake & banner diagnostic tester
    │
    ├── tls/                     # Native HTTPS & Let's Encrypt (ACME) Feature
    │   ├── certs.go             # Custom .crt/.key loader with hot-reload support
    │   ├── acme_alpn.go         # Let's Encrypt TLS-ALPN-01 challenge handler (port 443 only)
    │   ├── acme_dns.go          # Let's Encrypt DNS-01 challenge handler (Cloudflare, Route53, RFC2136)
    │   └── manager.go           # Automatic 30-day renewal manager
    │
    ├── schedule/                # Automation Cron & Timer Feature
    │   ├── models.go            # Schedule, TriggerType
    │   └── scheduler.go         # Cron scheduler (robfig/cron/v3) & one-time timer scheduler
    │
    ├── reports/                 # Crucible-Style Disk Log Streaming & Sharded Reports
    │   ├── models.go            # Report metadata, log chunk models
    │   ├── broadcaster.go     # Direct disk append & non-blocking subscriber fan-out
    │   ├── sharding.go        # ULID 2-level directory sharder (var/reports/01/JB/...)
    │   ├── reader.go          # Late-joining tailing disk reader
    │   └── handler.go         # HTTP static handler with public/private gatekeeper
    │
    ├── secrets/                 # Secrets Engine & HashiCorp Vault-Style Seal/Unseal
    │   ├── models.go          # Secret models, vault metadata, seal status
    │   ├── engine.go          # Sealed/Unsealed state machine, RAM key custodian, lock
    │   ├── crypto.go          # AES-256-GCM cipher, Argon2id KDF, Shamir shares
    │   ├── fifo.go            # Pure-Go ephemeral FIFO (named pipe) runner injector
    │   └── store.go           # Encrypted SQLite repository for vault_secrets & metadata
    │
    ├── storage/                 # Pure Go SQLite Storage Engine
    │   ├── db.go                # modernc.org/sqlite connection in WAL mode
    │   ├── migrate.go           # Migration runner executing external .sql files via //go:embed
    │   ├── ulid.go              # Monotonic ULID generator helper
    │   ├── schema/              # Raw .sql Schema Files (NO SQL in Go files for schema)
    │   │   ├── 001_initial.sql  # Users, user_totp_devices, totp_invites, ssh_keys, api_tokens, etc.
    │   │   ├── 002_indexes.sql  # Performance indexes
    │   │   └── 003_audit_ledger.sql # Audit ledger table, indexes, and anti-tamper triggers
    │   ├── store_auth.go        # User, sessions, multi-device TOTP & invite queries
    │   ├── store_tokens.go      # Multi-token PAT queries
    │   ├── store_ssh.go         # Git deploy key & Ansible host key queries
    │   ├── store_workers.go     # Worker nodes, health & availability queries
    │   ├── store_leases.go      # Distributed execution leases
    │   ├── store_runs.go        # Runs, job events, logs
    │   ├── store_schedules.go   # Schedules queries
    │   ├── store_vault.go       # Encrypted secrets & vault metadata queries
    │   └── store_inventory.go   # Inventory, vars & NetBox index cache
    │
    ├── metrics/                 # Prometheus Observability Feature
    │   ├── recorder.go          # Prometheus counters, histograms & gauges
    │   ├── federation.go        # PeerMetricsRegistry & cross-cluster scrape aggregator
    │   └── handler.go           # HTTP GET /metrics scraper (cluster-wide vs ?scope=local)
    │
    ├── notify/                  # Hierarchical Email & Alerts Engine
    │   ├── models.go            # NotificationConfig, ResolvedNotification, EmailMessage
    │   ├── resolver.go          # 3-tier hierarchy resolver (task -> cluster -> pipeline)
    │   ├── sender.go            # SMTP client (TLS/STARTTLS, MIME attachment builder)
    │   ├── tokens.go            # Signed report view token generator & validator
    │   └── dispatch.go          # Webhook dispatcher for external systems
    │
    ├── logging/                 # Operational Structured Logging Feature (slog)
    │   ├── logger.go            # slog initialization, stdout/stderr streaming, dynamic LevelVar
    │   └── context.go           # Contextual tracing attributes (request_id, run_ulid, actor_id)
    │
    ├── audit/                   # Cryptographic Immutable Audit Ledger Feature
    │   ├── models.go            # AuditEvent domain models, action slugs, actor types
    │   ├── hasher.go            # Canonical SHA-256 record hash calculation & genesis seeding
    │   ├── recorder.go          # Append-only ledger recorder linking previous_hash to HEAD
    │   ├── verifier.go          # Full chain validation engine verifying sequence and hashes
    │   └── store.go             # SQLite store queries for audit_ledger table
    │
    └── events/                  # In-Memory Event Bus
        ├── bus.go               # Pub/sub event broker
        └── events.go            # Lifecycle domain event definitions
```

---

## 20. Database Schema Architecture (External `.sql` Files)

> [!IMPORTANT]
> **Strict Separation Guarantee**: All SQL statements for table creation,
> schema versions, and indexes reside exclusively in external `.sql` files
> (`internal/storage/schema/*.sql`). Zero SQL schema definition statements
> exist inside Go `.go` source files.
> The storage engine embeds these files via Go `//go:embed schema/*.sql` and
> executes them automatically upon startup in `internal/storage/migrate.go`.
> **ULID Standard**: All entity primary keys use 26-character, time-ordered,
> URL-safe ULID identifiers.

### `internal/storage/schema/001_initial.sql`

```sql
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

-- Centralized Ansible Inventories (YAML & INI)
CREATE TABLE IF NOT EXISTS inventories (
    id             TEXT PRIMARY KEY,        -- slug, e.g. "production", "homelab"
    name           TEXT NOT NULL,           -- Display name
    format         TEXT NOT NULL DEFAULT 'yaml', -- yaml | ini | netbox
    content        TEXT NOT NULL,           -- Raw YAML or INI text
    variables_json TEXT NOT NULL DEFAULT '{}', -- Global extra vars
    host_count     INTEGER NOT NULL DEFAULT 0,
    group_count    INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by     TEXT NOT NULL            -- PAM user or token name
);

-- Cached NetBox Dynamic Inventory Topology
CREATE TABLE IF NOT EXISTS netbox_inventory_cache (
    id            TEXT PRIMARY KEY,         -- ULID
    config_file   TEXT NOT NULL,            -- e.g. "inventory/netbox.yaml"
    topology_json TEXT NOT NULL,            -- Full parsed device & group JSON
    device_count  INTEGER NOT NULL DEFAULT 0,
    synced_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Federated Cluster Worker Nodes with Availability, Health & Telemetry Tracking
CREATE TABLE IF NOT EXISTS worker_nodes (
    id            TEXT PRIMARY KEY,         -- ULID or hostname
    hostname      TEXT NOT NULL,
    datacenter    TEXT NOT NULL,
    labels_json   TEXT NOT NULL DEFAULT '[]',
    health        TEXT NOT NULL DEFAULT 'up',     -- up | down
    availability  TEXT NOT NULL DEFAULT 'active', -- active | drain | paused
    active_jobs   INTEGER NOT NULL DEFAULT 0,
    cpu_percent   REAL,                           -- e.g. 18.4
    mem_used_mb   INTEGER,                        -- e.g. 1120
    mem_total_mb  INTEGER,                        -- e.g. 4096
    mem_percent   REAL,                           -- e.g. 27.3
    load_avg      TEXT,                           -- e.g. "0.35, 0.40, 0.28"
    last_ping     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Users (Identity & Role Overrides)
CREATE TABLE IF NOT EXISTS users (
    username      TEXT PRIMARY KEY,
    role_override TEXT,                    -- Optional manual role override (e.g. 'admin')
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login    DATETIME
);

-- Multi-Device Two-Factor Authentication (Multiple TOTP Tokens per User)
-- Supports shared administrative accounts (e.g. root) where multiple operators
-- register their own authenticator apps, as well as multi-device redundancy.
CREATE TABLE IF NOT EXISTS user_totp_devices (
    id            TEXT PRIMARY KEY,        -- ULID
    username      TEXT NOT NULL,           -- Owner (e.g. 'root', 'jovens')
    name          TEXT NOT NULL,           -- e.g. "Alice Phone", "Bob YubiKey", "Work Laptop (1Password)"
    secret_key    TEXT NOT NULL,           -- Base32 encoded TOTP secret (encrypted at rest)
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  DATETIME,
    enabled       INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Secure One-Time TOTP Invite URLs (For Shared Accounts e.g. root)
CREATE TABLE IF NOT EXISTS totp_invites (
    id            TEXT PRIMARY KEY,        -- ULID
    token         TEXT NOT NULL UNIQUE,    -- High-entropy signed invite token
    username      TEXT NOT NULL,           -- Target account (e.g. 'root')
    label         TEXT NOT NULL,           -- e.g. "Bob Workstation"
    created_by    TEXT NOT NULL,           -- Operator who issued invite
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,       -- 24-hour TTL
    used_at       DATETIME,                -- NULL until consumed
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Built-in SSH Keypairs (Git Deploy Key & Ansible Managed Nodes Key)
CREATE TABLE IF NOT EXISTS ssh_keys (
    id            TEXT PRIMARY KEY,        -- ULID
    purpose       TEXT NOT NULL UNIQUE,    -- 'git_sync' | 'ansible_nodes'
    name          TEXT NOT NULL,           -- e.g. "Default Git Deploy Key"
    key_type      TEXT NOT NULL DEFAULT 'ed25519', -- 'ed25519' | 'rsa'
    public_key    TEXT NOT NULL,
    private_key   TEXT NOT NULL,           -- Encrypted at rest
    fingerprint   TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by    TEXT NOT NULL
);

-- Personal Access Tokens (CLI & Automation Multi-Tokens with Single Revocation)
CREATE TABLE IF NOT EXISTS api_tokens (
    id            TEXT PRIMARY KEY,        -- ULID
    name          TEXT NOT NULL,           -- e.g. "laptop-cli", "ci-cd-runner", "workstation"
    token_prefix  TEXT NOT NULL,           -- e.g. "cb_pat_9f82..." (first 12 chars for display)
    token_hash    TEXT NOT NULL UNIQUE,    -- SHA-256 hash of plaintext token
    username      TEXT NOT NULL,           -- Owner PAM username
    role          TEXT NOT NULL,           -- viewer | operator | maintainer | admin
    scopes_json   TEXT NOT NULL DEFAULT '["*"]',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  DATETIME,
    last_used_ip  TEXT,
    expires_at    DATETIME,                -- NULL = never expires
    revoked       INTEGER NOT NULL DEFAULT 0,
    revoked_at    DATETIME,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Distributed Cluster Leases (Guarantees Exactly-Once Job Execution)
CREATE TABLE IF NOT EXISTS cluster_leases (
    slot_key      TEXT PRIMARY KEY,        -- SHA256(schedule_id + scheduled_timestamp)
    holder_node   TEXT NOT NULL,           -- Node ID holding the lock
    fencing_token INTEGER NOT NULL,        -- Monotonic fencing token
    acquired_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,       -- Heartbeat lease expiration
    status        TEXT NOT NULL DEFAULT 'active' -- active | completed | failed | expired
);

-- Peered Crucible Controllers (Multi-Server Redundancy)
CREATE TABLE IF NOT EXISTS cluster_peers (
    node_id       TEXT PRIMARY KEY,
    address       TEXT NOT NULL,           -- host:port
    status        TEXT NOT NULL DEFAULT 'connected', -- connected | disconnected | degraded
    last_seen     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_sync_seq INTEGER NOT NULL DEFAULT 0
);

-- Active User Web Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,        -- ULID
    username      TEXT NOT NULL,
    role          TEXT NOT NULL,           -- viewer | operator | maintainer | admin
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);

-- Ephemeral Pre-Auth 2FA Challenges (5-minute TTL)
CREATE TABLE IF NOT EXISTS auth_challenges (
    token         TEXT PRIMARY KEY,        -- Cryptographic challenge token
    username      TEXT NOT NULL,
    expires_at    DATETIME NOT NULL
);

-- Core Runs, Events, and Schedules
CREATE TABLE IF NOT EXISTS runs (
    id            TEXT PRIMARY KEY,        -- ULID (e.g. 01JBCDEFGH...)
    schedule_id   TEXT,
    config_path   TEXT NOT NULL,
    visibility    TEXT NOT NULL DEFAULT 'private', -- 'public' | 'private'
    worker_id     TEXT,
    executing_node TEXT NOT NULL,          -- Controller node that managed execution
    status        TEXT NOT NULL DEFAULT 'running',
    started_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at   DATETIME,
    FOREIGN KEY(worker_id) REFERENCES worker_nodes(id)
);

CREATE TABLE IF NOT EXISTS job_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        TEXT NOT NULL,           -- ULID
    job_name      TEXT NOT NULL,
    group_name    TEXT NOT NULL,
    state         TEXT NOT NULL,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    recorded_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS schedules (
    id            TEXT PRIMARY KEY,        -- ULID
    name          TEXT NOT NULL,
    config_path   TEXT NOT NULL,
    trigger_type  INTEGER NOT NULL,
    cron_expr     TEXT,
    run_at        DATETIME,
    env_json      TEXT NOT NULL DEFAULT '{}',
    visibility    TEXT NOT NULL DEFAULT 'private', -- 'public' | 'private'
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Vault Secrets Engine Metadata & Verification (Singleton)
CREATE TABLE IF NOT EXISTS vault_metadata (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    is_initialized       INTEGER NOT NULL DEFAULT 0,
    kdf_salt             BLOB NOT NULL,          -- 16-byte Argon2id salt
    verification_hash    BLOB NOT NULL,          -- Hash to verify unseal passphrase / shares
    key_shares_total     INTEGER NOT NULL DEFAULT 1,
    key_shares_threshold INTEGER NOT NULL DEFAULT 1,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Ephemeral Signed View Tokens for Sharing Private Run Reports
CREATE TABLE IF NOT EXISTS report_view_tokens (
    token         TEXT PRIMARY KEY,        -- High-entropy crypto token
    run_id        TEXT NOT NULL,           -- ULID
    created_by    TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    DATETIME NOT NULL,
    FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
);



-- Encrypted Secrets at Rest (AES-256-GCM)
CREATE TABLE IF NOT EXISTS vault_secrets (
    id                   TEXT PRIMARY KEY,       -- ULID
    name                 TEXT UNIQUE NOT NULL,   -- e.g. "default", "prod_vault", "db_pass"
    secret_type          TEXT NOT NULL,          -- "ansible_vault_pass" | "kv" | "token"
    ciphertext           BLOB NOT NULL,          -- AES-256-GCM encrypted payload
    nonce                BLOB NOT NULL,          -- 12-byte initialization vector
    description          TEXT,
    created_by           TEXT NOT NULL,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### `internal/storage/schema/002_indexes.sql`

```sql
CREATE INDEX IF NOT EXISTS idx_inventories_format ON inventories(format);
CREATE INDEX IF NOT EXISTS idx_workers_routing ON worker_nodes(health, availability);
CREATE INDEX IF NOT EXISTS idx_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_tokens_user ON api_tokens(username);
CREATE INDEX IF NOT EXISTS idx_totp_devices_user ON user_totp_devices(username);
CREATE INDEX IF NOT EXISTS idx_totp_invites_token ON totp_invites(token);
CREATE INDEX IF NOT EXISTS idx_ssh_keys_purpose ON ssh_keys(purpose);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(username);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_visibility ON runs(visibility, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_schedules_visibility ON schedules(visibility);
CREATE INDEX IF NOT EXISTS idx_job_events_run ON job_events(run_id);
CREATE INDEX IF NOT EXISTS idx_vault_secrets_name ON vault_secrets(name);
CREATE INDEX IF NOT EXISTS idx_report_view_tokens_run ON report_view_tokens(run_id);
```

---

## 21. Build System, Security Scanning & Verification Pipeline (`Makefile`)

The build pipeline enforces automated quality, security auditing, vulnerability
detection, and testing **before** any binary compilation occurs. Running `make`
or `make all` guarantees that no unformatted, insecure, vulnerable, or failing
code can produce a binary.

### 21.1 Pre-Build Pipeline Execution Order

```
┌───────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                 AUTOMATED MAKE PRE-BUILD PIPELINE                                     │
│                                                                                                       │
│   make all: tidy ───────► lint ───────► sec ───────► vuln ───────► test ───────► build                │
│                                                                                                       │
│   1. make tidy:  Prunes unused module dependencies & downloads checksum requirements.                 │
│   2. make lint:  Executes strict golangci-lint (style, cyclomatic complexity, unhandled errs).        │
│   3. make sec:   Runs gosec AST security analysis across all packages for CWE security risks.         │
│   4. make vuln:  Queries Go Vuln DB via govulncheck for CVEs in transitive dependencies.              │
│   5. make test:  Runs standard unit tests (CGO_ENABLED=0) followed by race detection (-race).         │
│   6. make build: Compiles crucibled and cruciblectl static binaries with linker -ldflags injection.   │
└───────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 21.2 Targets Specification & Invariants

1. **`make tidy` (`go mod tidy && go mod download`):**
   * Prunes unused dependencies from `go.mod` and `go.sum`.
   * Pre-downloads all vendor modules to ensure subsequent linting, security,
     and build phases operate with complete cached dependencies.
   * **Rule:** Must execute prior to all linting, vulnerability checking, or
     compilation steps.

2. **`make lint` (`golangci-lint`):**
   * Executes `golangci-lint run ./...` configured via
     [`.golangci.yml`](./.golangci.yml).
   * Verifies strict formatting (`gofmt -s`), cognitive and cyclomatic
     complexity limits (`gocyclo` max 15), unhandled errors (`errcheck`),
     export docstrings, and dead code elimination.
   * **Empty-Repository Safety Guard:**
     ```makefile
     @if find . -name '*.go' -not -path './vendor/*' | grep -q .; then \
         golangci-lint run ./...; \
     else \
         echo "No Go files found to analyze; skipping lint."; \
     fi
     ```
     *Prevents build failures when running `make` immediately following module
initialization before initial Go files are committed.*

3. **`make sec` (`gosec` AST Security Scanner):**
   * Inspects Go source code AST against common security weaknesses (CWEs): SQL
     injection, path traversal, command injection, unvalidated deserialization,
     insecure cryptographic primitives, unpinned cipher suites, world-writable
     file permissions, and hardcoded credentials.
   * **Toolchain Version Pinning:** To prevent Go's automatic toolchain
     switching (e.g. attempting to pull `go1.26` tools and failing under Go
     1.24), `gosec` is pinned to `v2.22.2`:
     ```makefile
     @if find . -name '*.go' -not -path './vendor/*' | grep -q .; then \
         if which gosec > /dev/null 2>&1; then \
             gosec ./...; \
         else \
             go run github.com/securego/gosec/v2/cmd/gosec@v2.22.2 ./...; \
         fi \
     else \
         echo "No Go files found to analyze; skipping sec."; \
     fi
     ```
   * Zero security issues or warnings are tolerated. Any security violation
     aborts the build pipeline immediately.

4. **`make vuln` (`govulncheck` Vulnerability Database):**
   * Queries the official Go vulnerability database (`vuln.go.dev`) for known
     Common Vulnerabilities and Exposures (CVEs) affecting the Go standard
     library and all third-party dependencies.
   * Only flags vulnerabilities that are actually reachable in your call graph.
   * **Toolchain Version Pinning:** Pinned to `govulncheck@v1.1.4` to maintain
     strict Go 1.24 compatibility:
     ```makefile
     @if find . -name '*.go' -not -path './vendor/*' | grep -q .; then \
         if which govulncheck > /dev/null 2>&1; then \
             govulncheck ./...; \
         else \
             go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...; \
         fi \
     else \
         echo "No Go files found to analyze; skipping vuln."; \
     fi
     ```

5. **`make test` (`test-unit` & `test-race`):**
   * `test-unit`: Runs table-driven unit tests with `CGO_ENABLED=0 go test -v
     ./...`.
   * `test-race`: Runs all concurrent test suites under the Go race detector
     (`go test -v -race ./...`) to ensure lock invariants, channel
     synchronizations, and atomic state transitions are completely free of data
     races.

6. **`make build` (`build-daemon` & `build-cli`):**
   * Pure Go static compilation with `CGO_ENABLED=0` and linker flags:
     ```makefile
     LDFLAGS := -s -w \
                -X $(MODULE)/internal/version.AppVersion=$(VERSION) \
                -X $(MODULE)/internal/version.Version=$(VERSION) \
                -X $(MODULE)/internal/version.GitBranch=$(BRANCH) \
                -X $(MODULE)/internal/version.GitCommit=$(COMMIT) \
                -X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE)
     ```
   * Outputs self-contained binaries to `bin/crucibled` and `bin/cruciblectl`.

7. **`make check`:**
   * Ergonomic pre-commit target running `tidy lint sec vuln test` without
     compiling binaries.

---

## 22. Operational Logging & Immutable Audit Ledger (`internal/audit/`)

Crucible enforces a strict separation between **operational daemon logging**
(runtime diagnostics, debug output, info traces) and **security/operational
audit logging** (tamper-evident, compliance-grade action trails).

### 22.1 Operational Daemon Logging with `log/slog`

Normal operational logging streams directly to standard outputs (`stdout` and
`stderr`) without touching database storage. This keeps database write IOPS low
and delegates log aggregation to standard infrastructure tools (systemd
journald, container logging drivers, syslog, vector/promtail).

* **Pure Standard Library**: Built strictly on Go's `log/slog` (zero external
  dependencies).
* **Dual Output Formats**:
  * **Interactive / Terminal TTY Mode**: Colorized, human-friendly
    `slog.NewTextHandler` with high-contrast level badges and timestamp
    formatting via `internal/ui`.
  * **Daemon / Production Mode**: Structured `slog.NewJSONHandler` outputting
    RFC 3339 timestamps, severity levels, source caller location (`file:line`),
    and structured key-value pairs.
* **Contextual Tracing**:
  * Request-scoped and run-scoped handlers automatically inject correlation
    context: `request_id`, `run_ulid`, `node_id`, `actor_id`, and `subsystem`.
* **Dynamic Log Level Controller**:
  * Thread-safe `slog.LevelVar` supporting runtime switching (`DEBUG`, `INFO`,
    `WARN`, `ERROR`) via `--log-level` flag, configuration files, or UNIX
    `SIGHUP` signals.

---

### 22.2 Cryptographically Immutable Audit Ledger in SQLite (`internal/audit/`)

Unlike transient operational logs that stream to `stdout`/`stderr`, all
security-sensitive actions and operational state mutations are stored in an
append-only **SQLite audit table** (`audit_ledger`). This enables operators and
auditors to query, inspect, and mathematically verify the action history from
both the CLI (`cruciblectl audit`) and the Web UI (`/audit`).

#### Cryptographic SHA-256 Hash Chaining
Inspired by Git commit DAGs and blockchain tamper-evident ledgers, every audit
record cryptographically seals the previous record's hash:

```
┌─────────────────────────────────┐       ┌─────────────────────────────────┐
│ Audit Record #1 (Genesis)       │       │ Audit Record #2                 │
│ seq:           1                │       │ seq:           2                │
│ previous_hash: 0000000000...    │◄──────┼ previous_hash: SHA256(Record #1)│
│ record_hash:   SHA256(Block #1) │       │ record_hash:   SHA256(Block #2) │
└─────────────────────────────────┘       └─────────────────────────────────┘
```

* **Genesis Block (`seq = 1`)**: Uses a canonical seed of 64 zero characters
  (`0000000000000000000000000000000000000000000000000000000000000000`) for
  `previous_hash`.
* **Subsequent Blocks (`seq > 1`)**: Must reference the exact `record_hash` of
  the record with sequence `seq - 1`.
* **Canonical Record Hash Formula**:
  $$\text{record\_hash} = \text{SHA-256}(seq \parallel ":" \parallel
previous\_hash \parallel ":" \parallel timestamp \parallel ":" \parallel
actor\_id \parallel ":" \parallel action \parallel ":" \parallel resource\_type
\parallel ":" \parallel resource\_id \parallel ":" \parallel status \parallel
":" \parallel details)$$

#### Audit Event Schema (`internal/storage/schema/003_audit_ledger.sql`)

```sql
CREATE TABLE IF NOT EXISTS audit_ledger (
    seq            INTEGER PRIMARY KEY AUTOINCREMENT, -- Strictly monotonic sequence
    id             TEXT NOT NULL UNIQUE,              -- Monotonic ULID (audit_01JBC...)
    timestamp      TEXT NOT NULL,                     -- ISO 8601 UTC with nanosecond precision
    actor_id       TEXT NOT NULL,                     -- User ULID, PAT token ULID, or system/peer node
    actor_type     TEXT NOT NULL,                     -- user | token | system | cluster_peer
    ip_address     TEXT NOT NULL,                     -- Client IP or "127.0.0.1" / "unix"
    action         TEXT NOT NULL,                     -- Domain slug (e.g., auth.login.success)
    resource_type  TEXT NOT NULL,                     -- run | secret | node | token | inventory | ssh_key
    resource_id    TEXT NOT NULL,                     -- Target ULID or slug
    status         TEXT NOT NULL,                     -- SUCCESS | FAILURE | DENIED
    details        TEXT NOT NULL DEFAULT '{}',        -- Canonical JSON payload (scrubbed secrets)
    previous_hash  TEXT NOT NULL,                     -- SHA-256 of record seq-1 (or 64 zeros)
    record_hash    TEXT NOT NULL                      -- SHA-256 of canonical record tuple
);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_timestamp ON audit_ledger (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_ledger_actor ON audit_ledger (actor_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_ledger_action ON audit_ledger (action, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_ledger_resource ON audit_ledger (resource_type, resource_id);
```

#### Database Immutability Enforcers (Anti-Tamper Triggers)
SQLite triggers prevent any attempt by rogue queries or compromised sessions to
update or delete audit records:

```sql
CREATE TRIGGER IF NOT EXISTS prevent_audit_update
BEFORE UPDATE ON audit_ledger
BEGIN
    SELECT RAISE(ABORT, 'security violation: audit_ledger records are cryptographically immutable and cannot be updated');
END;

CREATE TRIGGER IF NOT EXISTS prevent_audit_delete
BEFORE DELETE ON audit_ledger
BEGIN
    SELECT RAISE(ABORT, 'security violation: audit_ledger records are cryptographically immutable and cannot be deleted');
END;
```

---

### 22.3 Operator CLI Interface (`cruciblectl audit`)

The operator CLI provides native tools for security auditing and chain
verification:

* **`cruciblectl audit list [--actor <id>] [--action <slug>] [--since <time>]
  [--limit <n>]`**: Formatted tabular view of recent audit actions with
  colorized status badges.
* **`cruciblectl audit show <ulid>`**: Detailed breakdown of a single audit
  event, formatted JSON parameters, actor attribution, and hash chain linkage.
* **`cruciblectl audit verify`**: Walks the entire hash chain from record 1 to
  HEAD. Recomputes all SHA-256 hashes and verifies backward linkage. Outputs `✓
  Cryptographic Audit Chain Valid (N records verified, 0 anomalies)` or
  pinpoints exact tampered sequence numbers.
* **`cruciblectl audit export [--format json|csv]`**: Exports audit records for
  ingestion into SIEMs, Splunk, Elasticsearch, or compliance cold storage.

---

### 22.4 Administrative Web UI Interface (`/audit`)

The embedded Web dashboard provides an administrative audit view for security
teams and compliance reviewers:
* **Interactive Filter & Search**: Filter by actor, action slug, target
  resource, status, or date range.
* **Integrity Status Banner**: Prominently displays the real-time cryptographic
  integrity check result (`[✓ AUDIT CHAIN VALID: 1,420 RECORDS VERIFIED]`).
* **Audit Drawer**: Click any entry to inspect raw metadata, client
  user-agents, IP addresses, parameter diffs, and cryptographic hash proofs.

---

## 23. Implementation Roadmap

### Phase 1: Modular Flat Scaffolding & Feature Models
1. Initialize Go module with vanity import path `gotcode.org/crucible` (Go
   1.24+).
2. Scaffold flat feature directory structure:
   * `internal/version/`, `internal/ui/`, `internal/logging/`,
     `internal/audit/`, `internal/ansible/`, `internal/git/`,
     `internal/cluster/`, `internal/engine/`, `internal/auth/`,
     `internal/tokens/`, `internal/ssh/`, `internal/tls/`,
     `internal/schedule/`, `internal/storage/schema/`, `internal/metrics/`,
     `internal/notify/`, `internal/events/`.
   * Zero layered nesting (`domain/`, `application/`, `transport/`, `ports/`).
3. Define self-contained feature models with ULID identifiers:
   * `internal/logging/`: `Logger`, `LogLevel`, `LogFormat`.
   * `internal/audit/models.go`: `AuditEvent`, `ActorType`, `AuditAction`,
     `AuditStatus`.
   * `internal/ansible/models.go`: `Inventory`, `HostVars`, `GroupVars`,
     `CustomVars`, `InventoryGroup`, `InventoryHost`.
   * `internal/cluster/models.go`: `WorkerNode`, `WorkerAvailability`,
     `NodeHealth`, `ClusterState`, `ClusterPeer`, `ClusterLease`.
   * `internal/auth/models.go`: `User`, `UserSession`, `TOTPDevice`,
     `TOTPInvite`, `Role`, `Permission`.
   * `internal/tokens/models.go`: `APIToken`, `TokenMetadata` (multiple PATs
     with ULIDs).
   * `internal/ssh/models.go`: `SSHKey`, `KeyPair`.
   * `internal/engine/models.go`: `Pipeline`, `Job`, `Run`, `JobEvent`,
     `LogChunk`.
   * `internal/schedule/models.go`: `Schedule`, `TriggerType`.
   * `internal/events/events.go`: Lifecycle domain event definitions
     (`WorkerAvailabilityChangedEvent`, `InventoryUpdatedEvent`,
     `VarsUpdatedEvent`, `GitCommitBroadcastEvent`, etc.).
4. Author schema `.sql` files:
   * `internal/storage/schema/001_initial.sql` (ULID primary keys,
     `user_totp_devices`, `totp_invites`, `ssh_keys`,
     `netbox_inventory_cache`).
   * `internal/storage/schema/002_indexes.sql`.
   * `internal/storage/schema/003_audit_ledger.sql` (Immutable audit ledger
     table, indexes, and anti-tamper triggers).
5. Implement Hexagonal CQRS Version Subsystem (`internal/version/`):
   * Domain models (`BuildMetadata`, `DependencyInfo`, `Info`), validation
     (`Validate()`), human-readable and short formatters.
   * Outbound port (`MetadataProvider`) and inbound port (`QueryHandler`).
   * CQRS read query (`GetVersionQuery`) and query service coordinator
     (`QueryService`).
   * Concrete runtime adapter (`RuntimeMetadataProvider`) querying compiler
     linker `-ldflags` (`AppVersion`, `GitBranch`, `GitCommit`, `BuildDate`),
     `runtime.GOOS/GOARCH`, and embedded build info
     (`runtime/debug.ReadBuildInfo`).
   * Comprehensive table-driven unit tests isolating query business logic with
     mock providers.
6. Implement Terminal UI Subsystem (`internal/ui/`):
   * Lip Gloss adaptive color palettes, bold/header styles, `SuccessMsg`,
     `ErrorMsg`, and dynamic terminal width detection.
   * Tabular version renderer (`PrintVersion`) using `text/tabwriter` and Lip
     Gloss styling.
   * ASCII banner logo and custom Cobra `HandleUsage` / `HandleHelp` formatters
     with low cyclomatic complexity.
   * Unit tests verifying message rendering, usage output, and version table
     formatting.
7. Implement Cobra Entrypoints (`cmd/crucibled/` and `cmd/cruciblectl/`):
   * Entrypoint `main.go`, flags, and `version.go` subcommands wired to the
     Hexagonal version service and Lip Gloss UI.
   * Unit tests verifying command execution and flag handling via buffer
     injection.
8. Establish Pre-Build Security & Testing Pipeline in `Makefile`:
   * Enforce build sequence: `tidy` -> `lint` (`golangci-lint`) -> `sec`
     (`gosec` AST scanner) -> `vuln` (`govulncheck`) -> `test` (`go test
     -race`) -> `build` (`CGO_ENABLED=0`).
   * Linker `-ldflags` version metadata injection (`-X
     gotcode.org/crucible/internal/version.AppVersion=...`).
   * Root-anchored `.gitignore` (`/crucibled`, `/cruciblectl`, SQLite WAL,
     reports).
   * Pinned tooling (`gosec@v2.22.2`, `govulncheck@v1.1.4`) to prevent
     unexpected toolchain switching on Go 1.24.

### Phase 2: Core Feature Packages Implementation
1. **SQLite Storage Engine (`internal/storage/`):**
   * Pure Go with `modernc.org/sqlite` in WAL mode (`db.go`).
   * ULID generator utility (`ulid.go` using `github.com/oklog/ulid/v2`).
   * Schema migration loader in `migrate.go` executing `schema/*.sql` via
     `//go:embed`.
   * Repositories: `store_auth.go` (multi-device TOTP & invites),
     `store_tokens.go` (multiple PATs), `store_ssh.go` (deploy and host keys),
     `store_workers.go`, `store_leases.go`, `store_runs.go`,
     `store_schedules.go`, and `store_inventory.go`.
2. **Native HTTPS & Let's Encrypt (`internal/tls/`):**
   * Custom certificate loader (`certs.go`).
   * ACME TLS-ALPN-01 challenge engine for port 443 (`acme_alpn.go`).
   * ACME DNS-01 challenge engine with pluggable DNS providers (`acme_dns.go`).
   * Automated 30-day certificate renewal manager (`manager.go`).
3. **Built-in SSH Key Management (`internal/ssh/`):**
   * Pure Go Ed25519 & RSA 4096-bit keypair generator (`generator.go`).
   * Public key formatters and fingerprint calculators.
   * Handshake connection tester (`tester.go`).
4. **Linux PAM, Multi-Device 2FA & RBAC (`internal/auth/`):**
   * `pam.go`: Linux PAM authentication with POSIX group lookup.
   * `totp.go`: Multi-device RFC 6238 TOTP verification (matches against any
     active registered device for user, QR code generation).
   * `invite.go`: Cryptographically signed TOTP invite URL generator &
     validator for shared accounts (e.g. `root`).
   * `rbac.go`: POSIX groups -> Crucible roles (`Viewer`, `Operator`,
     `Maintainer`, `Admin`).
   * `session.go`: Web session cookies and 5-minute pre-auth challenge tokens.
5. **Personal Access Tokens (`internal/tokens/`):**
   * High-entropy `cb_pat_<hex>` generator with ULID ID, SHA-256 hasher,
     validator, and independent single-token revoker.
6. **Git Synchronization & P2P Replication Engine (`internal/git/`):**
   * Git working-tree client (`client.go`) managing clones, fast-forward pulls,
     rebases, commits, and pushes using configured SSH deploy key.
   * Background periodic sync ticker (`sync.go`) with 5m polling interval.
   * On-demand pre-edit pull hook (`git pull --ff-only`).
   * Working-tree mutex lock (`lock.go`) and atomic save pipeline (rebase ->
     write -> commit -> push).
   * P2P real-time commit broadcast listener (`p2p_sync.go`): triggers
     immediate `git pull --ff-only` upon receiving gRPC commit broadcast from
     peers.
7. **Ansible Inventory, NetBox & Variables Engine (`internal/ansible/`):**
   * Bidirectional parsing for YAML inventories and classic INI
     (`parser_yaml.go`, `parser_ini.go`).
   * NetBox dynamic inventory plugin runner (`netbox.go`): executes
     `ansible-inventory` inside Python venv, caches parsed topology in SQLite.
   * Group/host hierarchy tree extractor (`tree.go`).
   * Variable precedence engine (`resolver.go`) calculating effective merged
     variables with provenance source traces.
   * Syntax validator (`validator.go`) returning exact line numbers on errors.
   * Dynamic JSON inventory generator (`dynamic.go`).
8. **Cluster Orchestration & Worker Pool (`internal/cluster/`):**
   * Worker registry and pool managing connected gRPC streams
     (`worker_pool.go`).
   * Pure Go `/proc` sampler in `telemetry.go` reading CPU %, Load, and RAM.
   * Routing filter (health + availability), least-loaded routing, overload
     guardrails, and drain watcher.
   * Heartbeat monitor & split-brain watchdog (`monitor.go`).
   * Distributed lease manager & monotonic fencing tokens (`lease.go`).
   * P2P mesh state replicator (`peer_manager.go`) broadcasting schedules,
     runs, and git commit heads.
9. **Execution Engine with Python Venv (`internal/engine/`):**
   * Python venv detector and environment injector (`venv.go`): sets `PATH`,
     `VIRTUAL_ENV`, executes `<venv>/bin/ansible-playbook`.
   * SSH private key injector: injects `ansible_nodes` key via ephemeral agent
     or `--private-key`.
   * Local subprocess runner (`exec_local.go`) executing directly against
     synchronized Git working copy.
   * Remote gRPC worker executor (`exec_remote.go`).
   * Non-blocking fan-out log broadcaster (`broadcaster.go`).
   * Pipeline YAML validator & DAG runner (`pipeline.go`).
10. **Schedule, Metrics & Notifications:**
    * `internal/schedule/`: Cron and one-time timer scheduler.
    * `internal/metrics/`: Prometheus recorder, peer federation registry, and
      scrape handler.
    * `internal/notify/`: Dispatch webhook alert client.
    * `internal/events/`: In-memory pub/sub event bus.

### Phase 3: Daemon Core CQRS Wiring & Integration Tests
1. Implement command and query handlers wiring the flat feature packages with
   ULID support.
2. Feature integration tests verifying:
   * Multi-device TOTP & invite URL redemption for shared `root` account.
   * Multiple CLI PAT tokens with independent single-token revocation.
   * SSH keypair generation, public key rendering, and connection testing.
   * NetBox dynamic inventory parsing inside Python venv.
   * Cluster P2P Git commit broadcast and immediate peer pull.
   * External `.sql` file migration execution via `//go:embed`.
   * Worker drain mode and split-brain interlock freeze.

### Phase 4: Protobuf Contract & gRPC Services
1. Create `proto/crucible/crucible.proto` covering:
   * Client API (`JobService`, `TokenService`, `WorkerService`,
     `InventoryService`, `VarsService`, `SSHService`).
   * Worker Mesh API (`WorkerMeshService`: Register, Heartbeat, ExecuteStream,
     DrainNotice).
   * Peer Cluster API (`PeerMeshService`: SyncState, AcquireLease, RenewLease,
     SyncMetrics, BroadcastCommit).
2. Generate Go protobuf bindings into `gen/crucible/`.
3. Implement `cmd/crucibled/server_grpc.go` with interceptors for token auth,
   RBAC, and worker handling.

### Phase 5: Embedded Web Dashboard, HTTPS, 2FA & Variables Studio
1. Import and adapt web interface layout into `cmd/crucibled/web/` templates:
   * Public dashboard with live runs and SSE streaming logs (`xterm.js`).
   * Worker nodes table (`/cluster/workers`): live telemetry, health badges,
     drain mode controls.
   * Inventory & Variables Studio (`/inventory` & `/vars`): split-pane editor,
     NetBox toggle, syntax validator, effective variable precedence inspector,
     atomic save & push button.
   * Multi-Device 2FA Management (`/settings/2fa`): list enrolled devices,
     generate invite link, add device with QR, revoke single device.
   * Invite Enrollment Page (`/enroll/2fa`): verify PAM password, scan QR code,
     activate device.
   * Personal Access Tokens view (`/settings/tokens`): create and revoke named
     PATs for CLI.
   * SSH Key Management (`/settings/ssh`): Git deploy key & Ansible host key
     generation, one-click copy, import, connection tester.
   * Login, 2FA challenge, and QR enrollment views.
2. Implement HTTP router with RBAC middleware and native TLS/HTTPS in
   `cmd/crucibled/server_http.go`.
3. Expose federated `GET /metrics` via `promhttp.Handler()`.

### Phase 6: Operator CLI (`cruciblectl`)
1. Implement Cobra commands and Lip Gloss (`github.com/charmbracelet/lipgloss`)
   terminal UI styling in `cmd/cruciblectl/`:
   * Render stylized terminal tables, status badges (`ACTIVE`, `DRAIN`,
     `DOWN`), execution banners, and progress indicators via `internal/ui`.
   * `cruciblectl version` (Formatted tabular metadata output with Go runtime
     and dependency graph)
   * `cruciblectl worker list|drain|active|status`
   * `cruciblectl inventory list|get|create|edit|hosts|validate|export|delete`
   * `cruciblectl vars list|get|set|edit|resolve`
   * `cruciblectl auth login` (Interactive PAM + TOTP login creating named
     token)
   * `cruciblectl auth 2fa invite --username root --label <name>` (Generate
     invite URL)
   * `cruciblectl auth 2fa devices` & `cruciblectl auth 2fa revoke <device-id>`
   * `cruciblectl token [create|list|revoke]` (Multiple PAT tokens)
   * `cruciblectl ssh generate|show|import|test` (Manage deploy and host keys)
   * `cruciblectl run <file.yaml> [--follow] [--target <dc>] [--venv <path>]`
   * `cruciblectl logs <run-id> [-f] [--job <name>]`
   * `cruciblectl status [run-id]`
   * `cruciblectl cluster status`
   * `cruciblectl schedule [list|create|delete]`
   * `cruciblectl cancel <run-id>`

### Phase 7: Packaging, Systemd & Verification
1. Create unified `Makefile` (`make all`: `tidy`, `lint`, `sec`, `vuln`,
   `test`, `build`, `make package`).
2. Provide systemd service units for Controller (`crucibled.service`) and
   Worker (`crucibled-worker.service`).
3. Verification Suite:
   * **Verification 1: Standalone Single-Node Run**: Run Ansible playbook
     inside Python venv; verify live logs.
   * **Verification 2: Worker Drain & Failover**: Verify draining worker
     rejects new jobs; verify watchdog flags unreachable nodes `DOWN`.
   * **Verification 3: Split-Brain Interlock**: Verify isolated node freezes
     executions.
   * **Verification 4: Multiple CLI PAT Tokens**: Verify creating multiple
     tokens under one user and revoking a single token leaves others
     functional.
   * **Verification 5: Multi-Device 2FA & Invite URL**: Enroll Device 1 for
     `root`, generate invite link for Device 2, redeem invite via PAM
     verification, verify both devices authenticate, and verify revoking Device
     1 preserves Device 2.
   * **Verification 6: Native HTTPS & ACME**: Verify HTTPS server starts with
     custom certs and test Let's Encrypt TLS-ALPN-01 & DNS-01 challenge paths.
   * **Verification 7: SSH Key Management**: Generate `git_sync` and
     `ansible_nodes` keys; test SSH connection handshakes.
   * **Verification 8: NetBox Dynamic Inventory**: Verify parsing
     `inventory/netbox.yaml` inside Python venv and caching device topology in
     SQLite.
   * **Verification 9: Cluster Git Replication**: Trigger commit on Manager;
     verify `BroadcastCommit` triggers instant `git pull --ff-only` on peer
     nodes.
   * **Verification 10: External `.sql` Schema & ULIDs**: Verify schema
     initializes strictly from `schema/*.sql` and all primary keys are valid
     monotonic ULIDs.
   * **Verification 11: Prometheus Observability**: Verify `GET /metrics`
     outputs local and peer metrics with `node` and `datacenter` labels.


