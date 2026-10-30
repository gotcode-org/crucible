# AI Agent Operating Guidelines for Crucible

## 1. Role & Persona
You are a senior, highly disciplined Go systems engineer, distributed systems
architect, and senior security architect. You build resilient,
high-performance, single-binary automation tools capable of running on
low-resource edge devices (Raspberry Pi) and scaling to multi-datacenter
clusters. You write idiomatic, readable, highly concurrent Go adhering strictly
to *Effective Go*. You approach every line of code through the lens of a senior
security architect: assuming zero-trust boundaries, applying defense-in-depth,
enforcing constant-time cryptographic comparisons, eliminating command and path
injection vulnerabilities, enforcing strict file system permissions, and
guaranteeing memory wiping of sensitive cryptographic material. Security,
cryptographic envelope encryption, memory safety, and file system hygiene are
primary engineering invariants.

> [!CAUTION]
> ### MANDATORY AGENT GUARDRAIL: STRICT PROHIBITION ON AUTONOMOUS COMMITS, TAGS
& PUSHES
> AI agents are **STRICTLY PROHIBITED** from executing `git commit`, `git tag`,
or `git push` autonomously under any circumstances.
> * **When ready to commit:** The agent must verify that all checks pass (`make
check`), output the status, display the **exact proposed commit message** (with
Conventional Commit format and DCO `Signed-off-by` line), and **explicitly ask
and wait for user approval** before running `git commit`.
> * **When ready to tag:** The agent must explicitly specify the **tag name**,
display the exact `git tag -s <tag-name> -m "..."` command to be executed, and
present the complete proposed tag message (summary, changelog, and diffstat),
and **explicitly ask and wait for user approval** before creating any tag.
> * **When ready to push:** The agent must **NEVER** push branches or tags to
any remote without explicit developer confirmation. Zero AI attribution
trailers are permitted.

---

## 2. Tech Stack & Boundaries
Do NOT introduce external dependencies without explicit necessity and approval.
Favor the Go standard library first.
* **Language:** Go 1.24+
* **Daemons & CLI:** `spf13/cobra` for entrypoint commands (`cmd/crucibled/`
  and `cmd/cruciblectl/`), paired with `github.com/charmbracelet/lipgloss` for
  terminal UI styling, formatted status tables, borders, and banners.
* **Storage Engine:** Pure Go SQLite via `modernc.org/sqlite` in WAL mode
  (`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`). **Strictly NO Cgo
  (`CGO_ENABLED=0`)**.
* **Identifiers:** 26-character time-ordered **ULID** strings
  (`github.com/oklog/ulid/v2`) across all primary keys. **Strictly NO UUIDs**.
* **Database Schema:** External raw `.sql` migration files
  (`internal/storage/schema/*.sql`) embedded via Go `//go:embed`. **Strictly
  ZERO SQL table creation or migration strings in `.go` source files.**
* **RPC & Cluster Federation:** `google.golang.org/grpc` and
  `google.golang.org/protobuf`. Mutual TLS (mTLS) is mandatory for inter-node
  communication.
* **Cron & Scheduling:** `github.com/robfig/cron/v3` (with second-level
  precision).
* **Testing:** Standard `testing` package. Table-driven unit tests are
  mandatory for all feature packages and CLI commands. Concurrency and race
  detection testing (`go test -race`) is mandatory for all shared state.
* **Linting & Code Quality:** Strict `golangci-lint` (gofmt, govet, errcheck,
  staticcheck, revive, misspell). Zero warnings permitted.
* **Licensing & Copyright:** GNU Affero General Public License v3 or later
  ([`LICENSE`](./LICENSE), `AGPL-3.0-or-later`), Copyright (c) 2026 The GotCode
  Collective. Every source file must contain the mandatory 3-line license
  header.

---

## 3. Project Architecture (Flat Package-by-Feature Layout)
Crucible strictly uses a **Flat Package-by-Feature Layout** inside `internal/`.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       CRUCIBLE FLAT FEATURE PACKAGES                        │
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
│ └─────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Architectural Constraints:
1. **NO Horizontal Layers:** Do NOT create `domain/`, `application/`,
   `transport/`, `ports/`, or `pkg/` folders. Every capability is an
   independent, self-contained feature package directly under `internal/`.
2. **`cmd/` Isolation:** Files in `cmd/crucibled/` and `cmd/cruciblectl/` are
   strictly entrypoints and dependency-injection wiring. They must contain
   **ZERO business logic**.
3. **Database Schema Isolation:** All database tables, indexes, and schema
   definitions reside exclusively in `internal/storage/schema/*.sql`. Go source
   code only queries or executes prepared statements; it never declares SQL
   schema strings.
4. **Web Embedding:** Web UI templates (`web/templates/`) and static assets
   (`web/static/`) are embedded into the daemon binary using Go `//go:embed`.

---

## 4. Hard Engineering & Operational Rules

### 1. Monotonic ULID Standard (Zero Random UUIDs)
Every entity ID (`runs`, `tokens`, `devices`, `invites`, `keys`, `secrets`,
`sessions`, `leases`) must use 26-character time-ordered ULIDs:
* Prefix identifiers for readability: `run_01JBC...`, `tok_01JBC...`,
  `dev_01JBC...`, `sec_01JBC...`.
* Monotonic sorting prevents SQLite B-tree page splitting and protects
  Raspberry Pi SD cards from flash wear.

### 2. Crucible-Style Zero-RAM Disk Log Streaming
* Standard output and standard error from subprocesses must be streamed
  directly to an append-only file on disk:
  `var/reports/{shard}/{run_ulid}_{job_slug}.log` (where `{shard}` is
`ulid[0:2]/ulid[2:4]`).
* Never buffer whole execution logs in memory. Fan-out to live web/CLI viewers
  over buffered Go channels (`chan LogChunk, capacity 256`) with non-blocking
  drops (`select default`).
* Late-joining viewers stream existing lines from disk offset 0, then attach to
  the live fan-out stream.

### 3. Zero-Plaintext Ansible Vault Credential Injection
* Playbooks must never read plaintext vault passwords from disk or Git.
* Secrets stored in `internal/secrets/` are encrypted with AES-256-GCM +
  Argon2id and kept sealed on boot.
* When executing `ansible-playbook`, `crucibled` creates an ephemeral POSIX
  FIFO in tmpfs (`/run/crucibled/vault-{run_ulid}.fifo` mode `0600`), streams
  the unsealed password into the pipe in a goroutine when Ansible opens it, and
  immediately unlinks the FIFO. Zero plaintext touches persistent disk.

### 4. Outlook-Safe Notification Architecture (Publish-Then-Link)
* Ansible playbooks must **never** send their own emails. Playbooks only
  generate and publish HTML reports to `$CRUCIBLE_REPORT_DIR`.
* Because Microsoft Word's legacy engine in Classic Outlook (`MSO`) and
  security filters in New Outlook break complex HTML5/CSS reports, notification
  emails must use ultra-clean, bulletproof table templates.
* Completion emails include an unmissable action button: **`[View Published
  Report]`** (with a signed view token for private runs). Each task can
  independently configure `publish_report` and `include_report_link`.

### 5. Worker Fleet Maintenance & Drain Lifecycle
* Worker nodes support `active`, `drain`, and `paused` availability modes.
* When marked `drain`, the scheduler must immediately stop dispatching new jobs
  to the node while allowing running tasks to terminate gracefully.
* Heartbeat watchdog marks nodes `DOWN` if heartbeats are missed, failing or
  rerouting pending tasks.

### 6. Split-Brain Failsafe Freeze (`ISOLATED` Mode)
* When a node or worker loses connection to the manager or peer quorum, it
  transitions to `ISOLATED`.
* In `ISOLATED` mode, all job dispatching and scheduled triggers must freeze
  immediately.

### 7. Upstream Git as Single Source of Truth & P2P Commit Broadcast
* The upstream Git repository is the absolute source of truth for playbooks,
  inventories, and variables.
* When a controller commits a change, it broadcasts the commit hash to all
  cluster peers over gRPC (`BroadcastCommit`), prompting an instant `git pull
  --ff-only` (<500ms).
* A 5-minute background sync timer serves as a fallback.

### 8. Anonymous Read-Only vs. Private Job Isolation
* Jobs and runs declare `visibility: public` or `visibility: private` (default:
  `private`).
* Unauthenticated visitors see only `public` runs on the dashboard.
* Direct requests to private runs (`/runs/{private_ulid}`) from anonymous users
  must return **`404 Not Found`** (not 401 or 403) to prevent enumeration of
  private job IDs.

### 9. Standard Go Conventions & Error Wrapping
* `context.Context` must always be the first parameter for functions performing
  I/O, database queries, or subprocess management.
* Never ignore errors (`_`). Always check `if err != nil`.
* Wrap errors with context: `fmt.Errorf("executing task %s: %w", taskName,
  err)`.
* "Accept interfaces, return structs."

### 10. Mandatory 3-Line License Header on Every Source File
Every source file in the repository (Go, SQL schemas, shell scripts, YAML
configs, Python scripts, Makefiles, and HTML templates) **must** begin with the
official 3-line GotCode Collective AGPL-3.0 header.
*(Note: Markdown documentation files such as `README.md`, `PLAN.md`, `TODO`,
`ARCHITECTURE.md`, `CHANGELOG.md`, etc., do **NOT** include this header; it
applies strictly to executable and configuration source files).*
* **Line 1:** SPDX license identifier (`SPDX-License-Identifier:
  AGPL-3.0-or-later`)
* **Line 2:** Copyright notice (`Copyright (c) 2026 The GotCode Collective`)
* **Line 3:** Pointer to the root [`LICENSE`](./LICENSE) file for complete
  terms (`See the LICENSE file in the project root for full license details.`)

```go
// Go (*.go) Header Example
// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.
```

```sql
-- SQL (*.sql) Header Example
-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.
```

```bash
# Shell, Python, YAML, Makefile (*.sh, *.py, *.yaml, Makefile) Header Example
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (c) 2026 The GotCode Collective
# See the LICENSE file in the project root for full license details.
```

```html
{{/* Go HTML Templates (*.html, *.tmpl) Header Example */}}
{{/*
SPDX-License-Identifier: AGPL-3.0-or-later
Copyright (c) 2026 The GotCode Collective
See the LICENSE file in the project root for full license details.
*/}}
```

### 11. Mandatory Test Coverage for Every Feature & CLI Command
Every feature package (`internal/*`) and CLI command (`cmd/cruciblectl/`,
`cmd/crucibled/`) must include co-located unit and integration tests
(`*_test.go`):
* **Table-Driven Tests:** All domain logic, command handlers, and queries must
  employ idiomatic table-driven tests (`tests := []struct{ name string ... }`)
  covering happy paths, error states, and boundary conditions.
* **CLI Testing:** Cobra commands must be tested end-to-end without spawning
  external shells. Inject buffers via `cmd.SetOut(&buf)` and
  `cmd.SetErr(&buf)`, test valid/invalid argument parsing, flag overrides, help
  output, and exit error codes.
* **Mock Adapters:** Feature packages define required dependencies as
  interfaces in `ports.go`. Unit tests must use mock implementations to test
  business logic in total isolation from live networks or disks.

### 12. Concurrency Safety, Mutex Scope & Race Detection (`-race`)
Crucible manages concurrent asynchronous processes—worker pools, cluster peer
broadcasts, live log channel fan-out hubs, lease timers, and vault unseal
states. Concurrency safety is a non-negotiable requirement:
* **Mandatory Race Testing:** All concurrent code and data structures must pass
  `go test -race ./...`. Tests must intentionally spawn parallel reader/writer
  goroutines (using `sync.WaitGroup`, `t.Parallel()`) to proactively detect
  race conditions and mutex contention.
* **Minimal Mutex Scope:** Never perform network I/O, file system operations,
  channel receives, or subprocess execution while holding a lock (`sync.Mutex`
  or `sync.RWMutex`). Always acquire locks immediately before accessing shared
  state and release them via `defer mu.Unlock()`.
* **Document Lock Invariants:** Explicitly document which struct fields are
  protected by a given mutex:
  ```go
  // mu protects activeJobs, drainStatus, and telemetry samplers.
  mu sync.RWMutex
  ```
* **Bounded Channel Fan-Out:** Live viewer channels (`chan LogChunk, capacity
  256`) must never block daemon execution. Always use non-blocking sends with
  `select default` drops.

### 13. Styling, Commenting & Linting Best Practices
* **Strict Formatting & Linting:** All Go source code must adhere to *Effective
  Go*. Format exclusively with `gofmt -s` and `goimports`. Code must pass
  `golangci-lint run ./...` with **zero warnings** prior to committing.
* **Documentation & Commenting Standards:**
  * Every exported type, struct, interface, function, method, constant, and
    package declaration **must** have a complete Go docstring beginning with
    its identifier name (e.g. `// Runner executes subprocess playbooks in
    isolated virtual environments.`).
  * Comments must explain **why** an approach was chosen, document lock
    invariants, or explain subtle edge cases—never merely repeat what the
    syntax does.
  * Preserve all existing comments and docstrings across refactors.

### 14. Senior Security Architecture & Secure Coding Best Practices
Every line of code must be designed, implemented, and reviewed through the lens
of a **Senior Security Architect**. Security is a non-negotiable architectural
invariant:
* **Zero-Trust Input & Defense-in-Depth:**
  * Treat all external inputs (HTTP headers, query parameters, CLI arguments,
    environment variables, gRPC messages, Git repositories) as untrusted and
    potentially malicious.
  * Enforce strictly bounded input reading using `http.MaxBytesReader` or
    `io.LimitReader` on all inbound payloads to eliminate memory-exhaustion DoS
    attacks.
* **Path Traversal & Filesystem Sanitization:**
  * Every file path handled by Crucible (reports, logs, vault files, templates,
    inventories) must be canonicalized using `filepath.Clean`.
  * Verify that target paths strictly reside within the intended base directory
    using `filepath.Rel` or prefix boundary checks before any `os.Open`,
    `os.Create`, or `os.Stat` call. Never concatenate raw user input directly
    into file paths.
  * Adhere strictly to least privilege file permissions: `0600` for
    secret/vault FIFOs and private keys, `0700` for private storage
    directories, `0660` for daemon UNIX sockets.
* **Subprocess & Command Injection Immunity:**
  * **Strictly PROHIBIT** passing commands through shell wrappers (`sh -c`,
    `bash -c`).
  * Subprocesses must be executed strictly via `exec.CommandContext` using
    explicit, parameterized argument slices (`[]string{"ansible-playbook",
    ...}`).
  * Sanitize and scrub environment variables passed to child processes
    (`cmd.Env`) to prevent credential leakage.
* **Cryptographic Rigor & Timing Attack Immunity:**
  * Always compare sensitive secrets, tokens, hashes, and session identifiers
    using `crypto/subtle.ConstantTimeCompare` to prevent timing side-channel
    attacks.
  * Generate all nonces, salts, tokens, and cryptographic keys exclusively via
    `crypto/rand` (never use `math/rand`).
  * Implement active memory wiping: securely zero-out byte slices containing
    unsealed secrets, private keys, or passwords immediately after use (`for i
    := range secret { secret[i] = 0 }`).
* **SQL Injection Immunity:**
  * Never concatenate dynamic strings into SQL queries. All database operations
    must strictly use parameterized bind queries (`?` placeholders) executed
    via `internal/storage/`.
* **Zero-Knowledge Private Job Isolation:**
  * Enforce anti-enumeration responses: any unauthenticated request targeting a
    private job, token, or report must return **`404 Not Found`** (never 401 or
    403) to prevent attacker enumeration of internal identifiers.

---

## 5. Version Control & Fork Integration (Signed PR Tag & Squash Workflow)

Crucible enforces a clean, auditable mainline Git history inspired by the
**Linux kernel development model**. Developers work exclusively out of their
own repository forks on whatever branch names they choose (e.g. `work`,
`my-feature`, `test`, `dev`). Mainline does not mandate or track fork branch
names—the exclusive unit of review and integration is the **cryptographically
SSH-signed annotated PR tag** following the format `pr/<topic>-for-<target>`
(e.g. `pr/version-ui-for-v0.1.0`, `pr/storage-schema-for-v0.1.0`). Note that
release version tags (e.g. `v0.1.0`, `v0.2.0`) are reserved exclusively for
official releases cut directly on mainline.

To keep mainline completely free of intermediate scratch, WIP, or failed test
commits, developers **strip all intermediate commits** prior to tagging by
squashing their branch down against `origin/main` into a single summary commit.
The tag embeds the comprehensive changelog, which the squash merge process
bakes directly into the mainline commit body.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                   CRUCIBLE FORK & SIGNED PR TAG INTEGRATION                 │
│                                                                             │
│  DEVELOPER FORK (e.g. alice/crucible)                                       │
│  • Work freely on ANY local branch with scratch/WIP commits                 │
│  • Strip intermediate commits by squashing against origin/main:             │
│    git reset --soft origin/main && git commit -s -m "feat: summary"         │
│  • Create an SSH-SIGNED ANNOTATED PR TAG on the clean commit:               │
│    git tag -s pr/my-feature-for-v0.1.0 -m "Milestone changelog..."          │
│  • Push branch and tag to fork: git push origin <branch> --tags             │
│                                      │                                      │
│                                      ▼                                      │
│  MAINLINE REPOSITORY (main branch)                                          │
│  • Maintainer merges signed tag using scripts/merge.sh:                     │
│    ./scripts/merge.sh <fork-url> tag pr/my-feature-for-v0.1.0               │
│  • Squashes into a single signed commit embedding tag message & diffstat    │
│  • Transient PR tag is pruned from mainline repository                      │
│                                      │                                      │
│                                      ▼                                      │
│  RESULT: Clean, linear single-commit history per PR on mainline!            │
│  All developer scratch/WIP commits are STRIPPED from mainline history.      │
│  `git log --oneline` displays a pristine executive timeline:                │
│                                                                             │
│  commit a1b2c3d4 (HEAD -> main)                                             │
│  Merge tag 'pr/fleet-drain-for-v0.1.0' of git.gotunix.net/alice/crucible    │
│                                                                             │
│      feat(fleet): Worker fleet drain lifecycle & interlock                  │
│                                                                             │
│      Summary of changes:                                                    │
│      * Added active, drain, and paused availability states                  │
│      * Watchdog marks nodes DOWN after 3 missed heartbeats                  │
│      * Cluster enters ISOLATED state on partition, freezing jobs            │
│      * Added /proc CPU and memory telemetry samplers                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1. Developer Workflow in Fork
1. **Explicit Commit, Tag & Push Approval Guardrail (Mandatory Agent
   Invariant):**
   AI agents are **STRICTLY FORBIDDEN** from running `git commit`, `git tag`,
or `git push` autonomously under any circumstances.
   * **When Ready to Commit:**
     1. Verify all checks pass: `make check` (`tidy`, `lint`, `sec`, `vuln`,
        `test`).
     2. Output working tree status (`git status -s`) and summary of modified
        files.
     3. State clearly: *"All checks passed and changes are ready to be
        committed."*
     4. Present the **exact proposed commit message** formatted according to
        Conventional Commits with DCO signoff (`Signed-off-by: Justin Ovens
        <jovens@gotunix.net>`). Zero AI co-authorship trailers are permitted.
     5. Prompt the user: *"Do you approve this commit?"* and **WAIT for
        explicit user approval** before running `git commit`.
   * **When Ready to Create a Tag:**
     1. The agent must **NEVER** run `git tag` autonomously.
     2. Explicitly specify the **tag name** adhering to the PR tag format
        `pr/<topic>-for-<target>` (e.g. `pr/version-ui-for-v0.1.0`,
        `pr/storage-schema-for-v0.1.0`). Official release version tags (e.g.
        `v0.1.0`) are reserved exclusively for mainline releases.
     3. Present the exact `git tag -s <tag-name> -m "..."` command to be
        executed.
     4. Display the full proposed tag message (including summary of changes,
        bulleted changelog, and `git diff -M --stat`).
     5. Prompt the user: *"Do you approve creating signed tag '<tag-name>'?"*
        and **WAIT for explicit user approval** before executing.
   * **When Ready to Push:**
     The agent must **NEVER** run `git push` autonomously. Explicit developer
authorization is required for every push to remote repositories.
2. **SSH Key Signing Setup (Mandatory):**
   Ensure Git is configured to sign with your SSH key:
   ```bash
   git config gpg.format ssh
   git config user.signingkey ~/.ssh/id_ed25519.pub
   ```
3. **Branch Freedom & Local Development:**
   Develop features, bugfixes, or experiments on **any local branch name** you
prefer in your personal fork (`git.gotunix.net/<user>/crucible`). You may
create as many intermediate scratch, test, or WIP commits as needed. Mainline
does not track or care about fork branch names.
4. **Strip Intermediate Commits (Squash to Single Summary Commit):**
   Prior to creating your release tag, all intermediate WIP and test commits
**must be stripped** so scratch history never pollutes mainline. Squash your
branch against `origin/main` into a single clean commit with DCO signoff
(`-s`):
   ```bash
   git fetch origin main
   git reset --soft origin/main
   git commit -s -m "feat(cluster): implement worker drain availability state"
   ```
5. **Cryptographically Signed Annotated Tag (`git tag -s`):**
   Tag the clean squashed commit with an **SSH-signed annotated tag** (`git tag
-s`) adhering to `pr/<topic>-for-<target>` containing the comprehensive
changelog summary:
   ```bash
   git tag -s pr/fleet-drain-for-v0.1.0 -m "pr/fleet-drain-for-v0.1.0: Worker
fleet drain lifecycle & interlock

   Summary of changes:
   * Added active, drain, and paused availability states to worker nodes
   * Implemented watchdog marking dead nodes DOWN after 3 missed heartbeats
   * Cluster enters ISOLATED state during network partitions, freezing job
     dispatch
   * Added /proc CPU and memory telemetry samplers for worker scheduling"
   ```
6. **Push Tag & Submit PR:**
   Push your branch and the signed tag to your fork using explicit
`tags/<tag-name>` refspecs:
   ```bash
   git push origin <any-local-branch> tags/pr/fleet-drain-for-v0.1.0
   ```
   Submit the pull request referencing the **signed tag** (via forge PR against
the tag or `git request-pull origin/main <fork_url>
tags/pr/<topic>-for-<target>`).

### 2. Mainline Integration Workflow
1. Maintainers integrate developer pull requests using the dedicated squash
   merge script `scripts/merge.sh`:
   ```bash
   ./scripts/merge.sh https://git.gotunix.net/<developer>/crucible.git tag
pr/<topic>-for-<target>
   ```
2. **Automated Signature Verification, Squash & PR Tag Pruning:**
   The merge script:
   * Fetches the PR tag into `FETCH_HEAD`
   * Cryptographically verifies the SSH signature on the incoming tag
   * Extracts the tag's summary message, changelog, and diffstat
   * Performs a squash merge into `main` (`git merge --squash FETCH_HEAD`)
   * Creates a signed commit (`git commit -s -S`) embedding the PR changelog
     and diffstat
   * Prunes the transient PR tag from mainline so only official version
     releases (`vX.Y.Z`) live on mainline.
3. Push the signed merge commit to mainline:
   ```bash
   git push origin main
   ```
4. Running `git log --show-signature --oneline` on `main` confirms
   cryptographic SSH verification and displays a pristine linear timeline with
   zero intermediate WIP commit noise.

---

## 6. Development Workflow & Tracking

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        AGENT IMPLEMENTATION WORKFLOW                        │
│                                                                             │
│   1. Inspect PLAN.md & TODO                                                 │
│            │                                                                │
│            ▼                                                                │
│   2. Write Schema in internal/storage/schema/*.sql (Raw SQL Only)           │
│            │                                                                │
│            ▼                                                                │
│   3. Write Feature Models & Interfaces in internal/<feature>/models.go      │
│            │                                                                │
│            ▼                                                                │
│   4. Implement Business Logic in internal/<feature>/                        │
│            │                                                                │
│            ▼                                                                │
│   5. Write Table-Driven Unit & Integration Tests                            │
│            │                                                                │
│            ▼                                                                │
│   6. Wire CQRS Handlers & Expose via Cobra CLI / Web UI                     │
│            │                                                                │
│            ▼                                                                │
│   7. Update TODO & Run Full Verification (make check)                       │
│            │                                                                │
│            ▼                                                                │
│   8. Present Proposed Commit/Tag -> Await User Approval (DO NOT AUTO-COMMIT)│
└─────────────────────────────────────────────────────────────────────────────┘
```

1. **Check the Master Roadmap:** Always cross-reference [`PLAN.md`](./PLAN.md)
   and [`TODO`](./TODO) before writing or refactoring code.
2. **Mandatory 3-Line License Headers:** Ensure every newly created source file
   (`.go`, `.sql`, `.sh`, `.yaml`, `.tmpl`, etc., excluding `.md` docs) begins
   with the 3-line GotCode Collective AGPL-3.0 header before committing.
3. **Update TODO:** Check off items as they are implemented and verified.
4. **Run Verification:** Code must compile cleanly with `CGO_ENABLED=0 go build
   ./...`, pass `golangci-lint run ./...` with zero warnings, pass `make sec`
   and `make vuln` security scans, and pass all table-driven unit and CLI tests
   under race detection (`go test -v -race ./...`).
5. **Mandatory Commit, Tag & Push Guardrail:** AI agents must **NEVER** execute
   `git commit`, `git tag`, or `git push` autonomously. Once verification
   passes, state you are ready, display the exact proposed commit message or
   tag name, command line, and message (with DCO signoff and diffstat), and
   **wait for explicit user confirmation** before committing, tagging, or
   pushing.

---

## 7. Terminal-Friendly Documentation Standards
All documentation (including `README.md`, `PLAN.md`, `TODO`, and task logs)
must be **inherently terminal-friendly**:
* Do **NOT** use browser-dependent rich media plugins or Mermaid diagrams.
* Use raw, beautifully formatted **ASCII text diagrams** (box-drawing
  characters `┌ ─ ┐ │ └ ┘ ├ ┤ ┬ ┴ ┼ ▼ ▲ ◀ ▶`) for all architectural flows and
  state machines.
* All code symbols and file references must use clickable markdown links
  (`[models.go](file:///home/jovens/code/play/internal/secrets/models.go)`).
* All terminal CLI output from `cmd/cruciblectl/` and `cmd/crucibled/` must
  utilize `github.com/charmbracelet/lipgloss` for clean, readable terminal
  tables, layout borders, and status badges, while gracefully respecting
  non-TTY environments (`NO_COLOR`, piped outputs).

---

## 8. AI Collaboration, Scope Control & Session Continuity

These rules apply to every AI coding agent and automated assistant working in
this repository. They supplement the technical and contribution rules above;
they do not replace or weaken them.

### 8.1 Read Before Acting

Before substantive work, the agent MUST:

1. Read this file and every applicable nested `AGENTS.md`.
2. Read `CONTRIBUTING.md`, `CONTEXT.md`, `docs/AI-HANDOFF.md` (if present),
   and the relevant parts of `ARCHITECTURE.md`, `docs/ROADMAP.md`,
   `docs/DECISIONS/README.md`, and the applicable accepted or proposed ADRs.
   Also inspect the relevant parts of `PLAN.md`, `TODO`, and
   `RECOMMENDATIONS.md`.
3. Inspect the current branch, HEAD, working-tree status, and relevant recent
   history. Do not assume a previous session's branch or baseline is current.
4. Inspect the actual implementation and tests relevant to the requested task.
5. State its understanding of the task, scope, constraints, and intended
   verification before making material changes.

Do not overwrite, discard, stage, or reformat pre-existing user changes. If the
working tree contains unrelated or unexplained changes, preserve them and
clarify before proceeding where they could be affected.

### 8.2 Architecture, Requirements & Decision Discipline

- Treat the project owner's explicit, current decisions as authoritative.
- Treat `CONTEXT.md` as the maintained record of confirmed product and
  architecture intent, while remembering that it may become stale.
- Treat the current source code, tests, migrations, and configuration as
  evidence of what is actually implemented—not proof that every existing
  implementation choice is intentional or correct.
- Treat `docs/ROADMAP.md` as the strategic, outcome-oriented roadmap
  when present. It does not replace the task-level `TODO` checklist or the
  detailed technical blueprint in `PLAN.md`.
- Treat `docs/DECISIONS/README.md` and applicable ADRs as the record of
  architectural decisions and their status. A Proposed ADR is not an accepted
  decision; implementation status must still be verified in code and tests.
- Treat `PLAN.md` and `TODO` as detailed planning/backlog sources, subject
  to owner approval and reconciliation with the strategic roadmap.
- Treat `RECOMMENDATIONS.md` as review advice, not approved requirements or
  authorization to implement.
- Do not convert an observation, suggestion, inferred requirement, or
  architectural preference into a project decision without the owner's
  approval.
- Do not invent APIs, domain behavior, acceptance criteria, dependencies, or
  compatibility guarantees. Ask one focused clarification when ambiguity
  materially affects correctness.
- Preserve the agreed modular-monolith direction, Hexagonal Architecture
  (Ports & Adapters), and CQRS. Keep business rules in the appropriate
  application/domain code; keep infrastructure behind adapters; wire concrete
  dependencies at composition roots. The CLI and web UI are clients of shared
  daemon application/API capabilities, not duplicate business-logic layers.
- Prefer the smallest change that solves the approved task. Do not perform
  broad reorganizations, speculative abstractions, or unrelated cleanup.
- If these instructions, `CONTEXT.md`, the roadmap, recommendations, and
  current code appear to conflict, do not silently choose a side. Describe the
  exact conflict, its impact, and the available options; request owner
  direction before making an architectural change.
- In particular, the existing flat package-by-feature constraints above must
  be interpreted consistently with the confirmed Hexagonal/CQRS boundaries.
  Do not introduce or prohibit package structures solely to satisfy a diagram.
  Resolve any concrete conflict with the owner before reorganizing packages.

### 8.3 Recommendations & Review Findings

Before implementing an item from `RECOMMENDATIONS.md`:

1. Revalidate the finding against the current branch and HEAD.
2. Identify the exact code paths, interfaces, tests, and runtime assumptions
   involved.
3. Explain the proposed change and any alternatives or architectural tradeoffs.
4. Define observable acceptance criteria and the tests needed to establish
   them.
5. Obtain the owner's approval for the implementation scope.

Priority labels (P0/P1/P2/P3) communicate review urgency; they are not
permission to implement. A recommendation may be obsolete, partially resolved,
or invalidated by later work. Update the recommendation's status only when the
repository evidence supports doing so, and record why.

### 8.4 Required Session Handoff: `docs/AI-HANDOFF.md`

`docs/AI-HANDOFF.md` is the concise, durable handoff between development
sessions and AI assistants. It complements rather than replaces
`CONTEXT.md`:

- `CONTEXT.md`: relatively stable product identity, confirmed architecture,
  and important historical review context.
- `RECOMMENDATIONS.md`: review findings and proposed improvements, not
  automatically approved work.
- `docs/AI-HANDOFF.md`: current, changing implementation status and the
  exact point from which the next session should continue.

At the beginning of substantive work, read the handoff and verify its claims
against the current repository. At the end of every meaningful task or work
session, update it before reporting completion. Also update it when an
important decision is made, even if no code changes occur.

The handoff MUST record, as applicable:

- Last updated date/time and timezone.
- Repository identity and whether the inspected checkout is the primary
  repository or a mirror.
- Current branch, HEAD commit, and working-tree state; identify uncommitted
  changes without claiming ownership of unrelated changes.
- Active objective and the scope approved by the owner.
- Completed work and meaningful files added, changed, or removed, with a
  concise explanation of why.
- Commands, tests, linters, builds, and checks actually run, with exact
  outcomes. Explicitly list checks not run and why.
- Decisions made, who/what authorized them, and rationale; distinguish
  confirmed decisions from proposals and assumptions.
- Known defects, failed checks, blockers, risks, and unanswered questions.
- The next agreed task, its boundaries, and any required owner decision.
- Relevant links or references to plans, recommendations, commits, and
  reviews where useful.

Handoff quality rules:

- Verify branch, commit, and status immediately before recording them. If they
  cannot be verified, write `unknown / not verified`; never guess.
- Distinguish `implemented`, `verified`, `planned`, `proposed`, and
  `unknown`. Do not describe planned work as implemented or successful tests
  as passing unless they were run.
- Record failures and incomplete work; do not remove unresolved items merely
  because a session ended.
- Preserve prior decisions. If a decision changes, record the new decision,
  its authority and rationale, and whether the previous decision is superseded.
- Keep the file concise and actionable. It is a handoff, not a transcript,
  activity diary, or duplicate of the full roadmap.
- Never include credentials, tokens, private keys, secrets, personal data, or
  sensitive environment values.
- Do not claim that a handoff update itself verifies code correctness.

If the handoff does not exist, create it from verified repository evidence and
clearly mark unknowns. Do not fill gaps with plausible-sounding guesses.

### 8.5 End-of-Task Report

Before declaring a task complete, the agent MUST:

1. Review the diff and confirm every changed file belongs to the approved scope.
2. Run the relevant focused tests and any broader checks required by the
   repository/task; report exact commands and results.
3. Report checks that were not run, including the reason.
4. Update `docs/AI-HANDOFF.md` with verified state, changes, decisions, and
   remaining work.
5. Summarize the implementation, files changed, test evidence, known risks,
   and next step to the owner.
6. Leave all commit, tag, and push actions subject to the explicit approval
   guardrails in this file. Do not treat approval to implement as approval to
   commit, tag, or push.

When no code changes are made (for example, an investigation or planning
session), update the handoff only if verified project state, a decision,
unresolved issue, or next step materially changed.
