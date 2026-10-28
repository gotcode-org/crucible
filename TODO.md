# Crucible (`crucibled` / `cruciblectl`) — Master Implementation Checklist

Format: `[state] (Priority) [domain/binary] Task description @assignee +tag`

| Element      | Type     | Description                                       |
|:-------------|:---------|:--------------------------------------------------|
| `[ ]`, `[x]` | State    | `[ ]` = Open, `[-]` = In-Progress, `[x]` = Done   |
| `(A)`, `(B)` | Priority | Task priority: High (A), Medium (B), Low (C)     |
| `[binary]`   | Target   | Binary target (`crucibled`, `cruciblectl`)        |
| `[domain]`   | Domain   | Subsystem (`storage`, `secrets`, `reports`, etc.) |
| `@name`      | Assignee | Primary context (e.g. `@jovens@gotcode.org`)     |
| `+tag`       | Tag      | Milestone tag (e.g. `+phase1`, `+config`)         |

---

## Phase 1: Standalone Sovereign Runner (v0.1.0)
- [x] (A) [shared] Initialize go.mod module gotcode.org/crucible Go 1.24+
      @jovens@gotcode.org +phase1 +scaffolding
- [x] (A) [shared] Author base Makefile enforcing pre-build pipeline all: tidy
      lint sec vuln test build @jovens@gotcode.org +phase1 +tooling
- [x] (A) [shared] Configure make tidy target (go mod tidy && go mod download)
      @jovens@gotcode.org +phase1 +tooling
- [x] (A) [shared] Configure make lint target (golangci-lint with empty-repo
      safety guard) @jovens@gotcode.org +phase1 +tooling +lint
- [x] (A) [shared] Configure make sec target (gosec@v2.22.2 AST scanner pinned
      for Go 1.24 with empty-repo guard) @jovens@gotcode.org +phase1 +tooling
      +security
- [x] (A) [shared] Configure make vuln target (govulncheck@v1.1.4 vulnerability
      check pinned for Go 1.24 with empty-repo guard) @jovens@gotcode.org
      +phase1 +tooling +security
- [x] (B) [shared] Configure make check pre-commit verification target (tidy
      lint sec vuln test) @jovens@gotcode.org +phase1 +tooling
- [x] (B) [shared] Add root-anchored .gitignore ignoring binaries, SQLite
      db/WAL files, logs, FIFOs, and reports @jovens@gotcode.org +phase1
      +tooling
- [x] (C) [shared] Configure .golangci.yml for project linting standards
      @jovens@gotcode.org +phase1 +tooling
- [x] (B) [shared] Implement version command and metadata reporter (AppName,
      AppVersion, GitBranch, GitCommit, Architecture, Go version, Dependencies)
      with Makefile ldflags injection @jovens@gotcode.org +phase1 +tooling
- [x] (B) [shared] Author Lip Gloss terminal styles, ASCII banner, formatted
      tables, and status badges in internal/ui @jovens@gotcode.org +phase1
      +tooling +cli
- [x] (A) [config] Implement internal/config package with ServerConfig and
      ClientConfig models, defaults, and env overrides @jovens@gotcode.org
      +phase1 +config
- [x] (B) [crucibled] Implement crucibled config init and validate commands
      for YAML configuration lifecycle @jovens@gotcode.org +phase1 +crucibled
      +config
- [x] (B) [cruciblectl] Implement cruciblectl config init and validate commands
      with profile and token management @jovens@gotcode.org +phase1
      +cruciblectl +config
- [x] (B) [crucibled] Implement crucibled systemd unit generator command with
      ambient capabilities (CAP_NET_BIND_SERVICE) @jovens@gotcode.org +phase1
      +crucibled +systemd
- [x] (A) [id] Implement domain entity monotonic ULID generator with standard
      prefixes in internal/id @jovens@gotcode.org +phase1 +domain
- [x] (A) [storage] Author external raw SQL migration 001_initial.sql with
      monotonic ULIDs @jovens@gotcode.org +phase1 +storage
- [x] (A) [storage] Author external raw SQL index migration 002_indexes.sql
      @jovens@gotcode.org +phase1 +storage
- [x] (A) [audit] Author raw SQL schema 003_audit_ledger.sql with monotonic
      ULIDs, SHA-256 hash chaining, and SQLite anti-tamper triggers
      @jovens@gotcode.org +phase1 +audit +storage
- [x] (A) [storage] Implement hexagonal Engine outbound port, CQRS Command &
      Query services, and pure Go SQLite adapter in internal/storage
      @jovens@gotcode.org +phase1 +storage
- [x] (A) [crucibled] Implement crucibled db migrate command with Lip Gloss
      applied migrations report table @jovens@gotcode.org +phase1 +storage +cli
- [x] (B) [crucibled] Implement crucibled db status command with integrity
      check, WAL mode, and table counts @jovens@gotcode.org +phase1 +storage
      +cli
- [ ] (A) [crucibled] Implement crucibled start standalone HTTP daemon with
      UNIX socket, /healthz, and graceful shutdown @jovens@gotcode.org +phase1
      +daemon
- [ ] (B) [web] Embed web/templates/ and web/static/ assets into daemon binary
      via Go //go:embed @jovens@gotcode.org +phase1 +web
- [ ] (B) [web] Implement base HTML5 layout, CSS stylesheets, navigation bar,
      and dark mode styling @jovens@gotcode.org +phase1 +web
- [ ] (B) [auth] Implement Linux PAM authentication adapter for system accounts
      in internal/auth @jovens@gotcode.org +phase1 +auth +security
- [ ] (B) [auth] Implement multi-device RFC 6238 TOTP 2FA engine with encrypted
      keys and QR generator @jovens@gotcode.org +phase1 +auth +security
- [ ] (B) [auth] Implement cryptographically signed shared-account onboarding
      invite URLs @jovens@gotcode.org +phase1 +auth +security
- [ ] (B) [cruciblectl] Implement cruciblectl auth login, 2fa devices, and 2fa
      invite commands @jovens@gotcode.org +phase1 +cruciblectl +auth
- [ ] (B) [web] Implement Web UI /login, /auth/2fa challenge, and /settings/2fa
      device management views @jovens@gotcode.org +phase1 +web +auth
- [ ] (A) [tokens] Implement internal/tokens package with high-entropy token
      generator, SHA-256 hashing, and scope validator @jovens@gotcode.org
      +phase1 +tokens +security
- [ ] (A) [cruciblectl] Implement cruciblectl token create, list, and revoke
      commands with Lip Gloss table formatting @jovens@gotcode.org +phase1
      +cruciblectl +tokens
- [ ] (B) [web] Implement Web UI /settings/tokens view for token creation,
      one-click copy, and revocation @jovens@gotcode.org +phase1 +web +tokens
- [ ] (A) [audit] Implement internal/audit package with append-only SHA-256
      hash chain recorder and genesis block linking @jovens@gotcode.org +phase1
      +audit +security
- [ ] (A) [cruciblectl] Implement cruciblectl audit list and verify commands
      with cryptographic chain validation @jovens@gotcode.org +phase1
      +cruciblectl +audit
- [ ] (B) [web] Implement Web UI /audit view with timeline filters, metadata
      diff drawer, and chain validation badge @jovens@gotcode.org +phase1 +web
      +audit
- [ ] (A) [secrets] Implement internal/secrets package with AES-256-GCM +
      Argon2id envelope encryption at rest @jovens@gotcode.org +phase1 +secrets
      +security
- [ ] (A) [secrets] Implement seal/unseal state machine with passphrases and
      Shamir key shares @jovens@gotcode.org +phase1 +secrets +security
- [ ] (A) [secrets] Implement ephemeral tmpfs POSIX FIFO password streamer
      unlinked on open @jovens@gotcode.org +phase1 +secrets +security
- [ ] (A) [crucibled] Implement crucibled vault init, status, and unseal
      commands @jovens@gotcode.org +phase1 +crucibled +secrets
- [ ] (B) [cruciblectl] Implement cruciblectl secret set and get commands
      @jovens@gotcode.org +phase1 +cruciblectl +secrets
- [ ] (B) [web] Implement Web UI /settings/vault view with seal status banner
      and interactive unseal modal @jovens@gotcode.org +phase1 +web +secrets
- [ ] (A) [engine] Implement internal/engine package with Python venv manager
      and subprocess playbook runner @jovens@gotcode.org +phase1 +engine
- [ ] (A) [reports] Implement internal/reports package with zero-RAM disk log
      streaming in var/reports/{shard}/ @jovens@gotcode.org +phase1 +reports
- [ ] (A) [reports] Implement non-blocking live log fan-out hub with bounded
      channels (capacity 256) @jovens@gotcode.org +phase1 +reports
- [ ] (A) [crucibled] Implement crucibled run command executing playbooks
      with FIFO password injection @jovens@gotcode.org +phase1 +crucibled
      +engine
- [ ] (B) [cruciblectl] Implement cruciblectl run command with live log tailing
      @jovens@gotcode.org +phase1 +cruciblectl +engine
- [ ] (B) [cruciblectl] Implement cruciblectl logs command with -f streaming
      and disk catch-up @jovens@gotcode.org +phase1 +cruciblectl +reports
- [ ] (A) [web] Implement Web UI dashboard (/), live xterm.js execution console
      (/runs/{ulid}), and report viewer @jovens@gotcode.org +phase1 +web
- [ ] (A) [web] Enforce anti-enumeration 404 Not Found for anonymous access to
      private runs @jovens@gotcode.org +phase1 +web +security
- [ ] (B) [notify] Implement internal/notify package with 3-tier recipient
      resolver and Outlook MSO table templates @jovens@gotcode.org +phase1
      +notify
- [ ] (B) [notify] Implement pure Go SMTP client with STARTTLS support and
      signed report view tokens @jovens@gotcode.org +phase1 +notify +security
- [ ] (B) [cruciblectl] Implement cruciblectl notify send and test commands
      @jovens@gotcode.org +phase1 +cruciblectl +notify
- [ ] (B) [web] Implement Web UI Email Report action modal on completed run
      view @jovens@gotcode.org +phase1 +web +notify
- [ ] (A) [shared] Execute standalone single-node end-to-end verification
      suite across CLI and Web UI and cut release v0.1.0 tag
      @jovens@gotcode.org +phase1 +release

---

## Phase 2: Worker Fleet & Maintenance Lifecycle (v0.2.0)
- [ ] (A) [cluster] Author proto/crucible/cluster.proto with WorkerMeshService
      and mTLS transport definitions @jovens@gotcode.org +phase2 +cluster
      +proto
- [ ] (A) [crucibled] Implement crucibled worker lightweight zero-state remote
      daemon with dial-home mTLS gRPC @jovens@gotcode.org +phase2 +crucibled
      +worker
- [ ] (A) [cluster] Implement worker availability state machine (active, drain,
      paused) and drain interlock @jovens@gotcode.org +phase2 +cluster
- [ ] (A) [cluster] Implement 10-second heartbeat watchdog marking dead nodes
      DOWN with job failover @jovens@gotcode.org +phase2 +cluster
- [ ] (B) [cluster] Implement pure Go /proc/stat and /proc/meminfo telemetry
      samplers for load-aware routing @jovens@gotcode.org +phase2 +cluster
- [ ] (A) [cluster] Implement split-brain failsafe freezing triggers in
      ISOLATED state @jovens@gotcode.org +phase2 +cluster
- [ ] (A) [cruciblectl] Implement cruciblectl node list, drain,
      activate, pause, and status commands @jovens@gotcode.org +phase2
      +cruciblectl +cluster
- [ ] (A) [shared] Execute worker drain and failover verification suite and
      cut release v0.2.0 tag @jovens@gotcode.org +phase2 +release

---

## Phase 3: Cluster Peer HA & Distributed Leases (v0.3.0)
- [ ] (A) [cluster] Implement multi-controller peer-to-peer mesh replicator
      over mTLS gRPC @jovens@gotcode.org +phase3 +cluster
- [ ] (A) [cluster] Implement distributed lease manager with monotonic fencing
      tokens for exactly-once scheduling @jovens@gotcode.org +phase3 +cluster
- [ ] (B) [git] Implement internal/git package managing clone, fetch, rebase,
      commit, and push via deploy keys @jovens@gotcode.org +phase3 +git
- [ ] (A) [git] Implement sub-500ms P2P commit broadcaster over gRPC
      BroadcastCommit with 5m fallback ticker @jovens@gotcode.org +phase3 +git
- [ ] (B) [ansible] Implement YAML and INI inventory parser with group/host
      hierarchies in internal/ansible @jovens@gotcode.org +phase3 +ansible
- [ ] (B) [ansible] Implement NetBox dynamic inventory plugin and SQLite
      topology cache @jovens@gotcode.org +phase3 +ansible
- [ ] (B) [ansible] Implement variable precedence engine calculating merged
      effective vars with provenance traces @jovens@gotcode.org +phase3
      +ansible
- [ ] (B) [cruciblectl] Implement cruciblectl inventory and vars management
      commands @jovens@gotcode.org +phase3 +cruciblectl +ansible
- [ ] (C) [schedule] Implement second-precision cron scheduler and one-time
      timer models in internal/schedule @jovens@gotcode.org +phase3 +schedule
- [ ] (A) [shared] Execute cluster peer HA and Git broadcast verification
      suite and cut release v0.3.0 tag @jovens@gotcode.org +phase3 +release

---

## Phase 4: Embedded Web UI, Native TLS & Live Streaming (v0.4.0)
- [ ] (B) [tls] Implement native ACME Let's Encrypt TLS-ALPN-01 and custom
      cert loader in internal/tls @jovens@gotcode.org +phase4 +tls +security
- [ ] (B) [auth] Implement Linux PAM authentication adapter for system accounts
      in internal/auth @jovens@gotcode.org +phase4 +auth +security
- [ ] (B) [auth] Implement multi-device RFC 6238 TOTP 2FA engine with encrypted
      keys and QR generator @jovens@gotcode.org +phase4 +auth +security
- [ ] (B) [auth] Implement cryptographically signed shared-account onboarding
      invite URLs @jovens@gotcode.org +phase4 +auth +security
- [ ] (B) [web] Embed web/templates/ and web/static/ assets into daemon via
      Go //go:embed @jovens@gotcode.org +phase4 +web
- [ ] (B) [web] Implement public read-only dashboard with anti-enumeration
      404 Not Found for private jobs @jovens@gotcode.org +phase4 +web +security
- [ ] (A) [web] Implement real-time log viewer streaming SSE to xterm.js
      with late-joining disk reader @jovens@gotcode.org +phase4 +web
- [ ] (B) [web] Implement web Inventory and Variables Studio with split-pane
      editor and precedence inspector @jovens@gotcode.org +phase4 +web
- [ ] (B) [web] Implement operator Web UI for secrets vault unseal and 2FA
      device enrollment @jovens@gotcode.org +phase4 +web +secrets
- [ ] (C) [metrics] Implement Prometheus /metrics scrape endpoint with node
      and datacenter labels @jovens@gotcode.org +phase4 +metrics
- [ ] (A) [shared] Execute Web UI, PAM/TOTP, and live streaming verification
      suite and cut release v0.4.0 tag @jovens@gotcode.org +phase4 +release

---

## Phase 5: Production Hardening, Packaging & Enterprise Release (v1.0.0)
- [ ] (B) [shared] Package hardened systemd service units for crucibled and
      crucibled-worker @jovens@gotcode.org +phase5 +systemd
- [ ] (B) [shared] Build multi-distro distribution packages (RPM, DEB, AUR,
      and scratch container images) @jovens@gotcode.org +phase5 +packaging
- [ ] (A) [shared] Execute end-to-end chaos testing: network partitions,
      worker crashes, and quorum failover @jovens@gotcode.org +phase5 +testing
- [ ] (A) [security] Complete cryptographic memory wiping audit: ensure all
      unsealed secrets zero out on exit @jovens@gotcode.org +phase5 +security
- [ ] (A) [shared] Execute full performance benchmarking under 100k log line
      bursts and cut release v1.0.0 GA tag @jovens@gotcode.org +phase5 +release

