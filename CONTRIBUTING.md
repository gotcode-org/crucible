# Contributing to Crucible

Thank you for contributing to **Crucible** (`crucibled` and `cruciblectl`). Crucible is a lightweight, single-binary automation orchestrator built for high performance, memory safety, and edge-to-datacenter reliability.

To maintain code quality, security, and a clean Git history, all contributors must follow the guidelines detailed below.

---

## 1. Developer Certificate of Origin (DCO)

We enforce the **Developer Certificate of Origin (DCO v1.1)** on all contributions. Every commit **must** include a signed-off-by trailer certifying that you authored the code or have the right to submit it under the project's license:

```bash
git commit -s -m "feat(cluster): implement worker drain availability state"
```

By adding `--signoff` (`-s`), you certify compliance with the terms stated in the project [`DCO`](./DCO) file.

### Human-Only Attribution Policy
Crucible enforces strict human accountability:
- **No AI Attribution Trailers:** Do **NOT** include `Co-authored-by:` or `Reviewed-by:` trailers in Git commits, tags, or merge requests. The human submitting the signed-off commit is solely responsible for the integrity of the code.

---

## 2. Mandatory 3-Line License Header

Crucible is licensed under the **GNU Affero General Public License v3 or later** ([`LICENSE`](./LICENSE)).

Every newly created or modified **source code and configuration file** (`.go`, `.sql`, `.sh`, `.py`, `.yaml`, `.yml`, Makefile, and HTML templates) **must** begin with the official 3-line GotCode Collective AGPL-3.0 header:

```go
// Go (*.go) Source Header:
// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.
```

```sql
-- SQL (*.sql) Schema Header:
-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (c) 2026 The GotCode Collective
-- See the LICENSE file in the project root for full license details.
```

```bash
# Shell, Python, YAML, Makefile (*.sh, *.py, *.yaml, *.yml, Makefile) Header:
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (c) 2026 The GotCode Collective
# See the LICENSE file in the project root for full license details.
```

*(Note: Markdown documentation files such as `README.md`, `PLAN.md`, `TODO.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, etc., do **NOT** include this header).*

---

## 3. Contribution Workflow (Linux Kernel Tag Integration)

Crucible enforces a clean, auditable mainline Git history inspired by the **Linux kernel development model**. Contributors work out of personal repository forks on **any local branch name** they choose (e.g. `work`, `my-feature`, `dev`, `sandbox`). Mainline does not mandate or track fork branch names—the exclusive unit of review and integration is the **cryptographically SSH-signed annotated tag**.

To keep mainline completely free of intermediate scratch, WIP, or failed test commits, contributors **strip all intermediate commits** prior to tagging by squashing their branch down against `origin/main` into a single summary commit. The tag embeds the comprehensive changelog, which Git natively bakes into the mainline merge commit.

```
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                           CONTRIBUTION & TAG INTEGRATION WORKFLOW                               │
│                                                                                                 │
│  1. FORK & DEVELOP ON ANY BRANCH                                                                │
│     Fork git.gotunix.net/gotcode/crucible to your account (e.g. alice/crucible).                │
│     Work on ANY branch name you prefer with as many scratch/test commits as needed.             │
│                                                                                                 │
│  2. STRIP INTERMEDIATE COMMITS (SQUASH)                                                         │
│     Before submitting, squash your branch against origin/main into a single summary commit:     │
│     git fetch origin main                                                                       │
│     git reset --soft origin/main                                                                │
│     git commit -s -m "feat(subsystem): milestone summary"                                       │
│                                                                                                 │
│  3. CREATE SIGNED ANNOTATED TAG (-s)                                                            │
│     Tag the clean commit with an SSH-signed annotated tag containing the full changelog:        │
│     git tag -s v0.2.0 -m "Release summary and changelog..."                                     │
│                                                                                                 │
│  4. SUBMIT PULL REQUEST FOR THE TAG                                                             │
│     Push your branch and tag: git push origin <branch> --tags                                   │
│     Send a pull request for the tag via forge PR or email dev@gotcode.org                       │
│                                                                                                 │
│  5. MAINLINE MERGE (COMMITS STRIPPED)                                                           │
│     Maintainers merge your signed tag with:                                                     │
│     git merge --no-ff --signoff -S <tag-name>                                                   │
│     Git natively bakes the tag's summary message into the merge commit body!                   │
│     Zero intermediate WIP commits ever enter mainline history.                                  │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### SSH Signing Configuration
Configure Git to sign with your SSH key:
```bash
git config gpg.format ssh
git config user.signingkey ~/.ssh/id_ed25519.pub
```

---

## 4. Architectural & Engineering Rules

Before submitting code, ensure your changes adhere strictly to the engineering rules outlined in [`AGENTS.md`](./AGENTS.md) and [`ARCHITECTURE.md`](./ARCHITECTURE.md):

1. **Flat Package-by-Feature Layout:** Every capability lives in an isolated package directly under `internal/<feature>/`. Do **not** create horizontal `domain/`, `application/`, `ports/`, or `transport/` directories.
2. **Zero SQL Schema in Go Source:** All database tables and migrations reside exclusively in `internal/storage/schema/*.sql`. Go code only executes prepared queries; it never declares SQL DDL strings.
3. **Monotonic ULID Standard:** All entity primary keys (`runs`, `tokens`, `secrets`, `devices`) must use 26-character time-ordered ULIDs (`run_01JBC...`, `tok_01JBC...`). UUIDs are strictly prohibited.
4. **Zero-RAM Disk Log Streaming:** Automation stdout/stderr streams directly to append-only disk files (`var/reports/{shard}/{ulid}_{slug}.log`). Broadcasters fan out to viewers over bounded channels (`capacity 256`) with non-blocking drops. Never buffer whole logs in memory.
5. **Ansible Vault Secrets Hygiene:** Passwords to decrypt Ansible Vaults are kept sealed at rest (AES-256-GCM + Argon2id). When executing playbooks, passwords are injected via ephemeral tmpfs FIFOs (`/run/crucibled/vault-{ulid}.fifo` mode `0600`) unlinked immediately. Zero plaintext touches persistent disk.
6. **Outlook-Safe Notifications:** Playbooks never send emails directly; they write HTML reports to `$CRUCIBLE_REPORT_DIR`. Crucible publishes the reports and sends minimalist, bulletproof MSO table emails (`<!--[if mso]>`) containing deep links and HMAC-signed view tokens for private runs.
7. **Zero Cgo:** Crucible builds with `CGO_ENABLED=0` using `modernc.org/sqlite` in WAL mode.
8. **Mandatory Test Coverage (Features & CLI):** Every internal feature package and CLI command must include co-located table-driven tests (`*_test.go`). Cobra commands in `cmd/cruciblectl/` and `cmd/crucibled/` must be tested using buffer injection (`cmd.SetOut(&buf)`, `cmd.SetErr(&buf)`), flag parsing validation, and exit code assertions.
9. **Concurrency Safety & Race Detection (`-race`):** All concurrent routines (mutex-guarded state, worker pools, cluster peer sync, live log fan-out channels) must be verified with `go test -race ./...`. Never hold a mutex during network I/O or subprocess execution. Document all lock invariants.
10. **Styling, Commenting & Linting:** Code must be formatted with `gofmt -s` and pass `golangci-lint run ./...` with zero warnings. Every exported symbol must have a complete Go docstring explaining why it exists and its behavioral invariants.
11. **Security Architecture & Secure Coding Best Practices:** Contributors must approach all code as Senior Security Architects. Treat all external inputs as untrusted, bound all inbound readers (`http.MaxBytesReader`), enforce strict path canonicalization and base directory verification (`filepath.Clean`), eliminate command injection (never execute via `sh -c`), use constant-time comparisons (`crypto/subtle.ConstantTimeCompare`), securely zero-out memory buffers holding sensitive keys or unsealed secrets, and enforce anti-enumeration `404 Not Found` responses for private resources.

---

## 5. Verification Checklist

Before creating your signed release tag, run:

```bash
# 1. Format and tidy code
gofmt -s -w .
go mod tidy

# 2. Run static analysis and linting (zero warnings permitted)
golangci-lint run ./...

# 3. Verify zero-Cgo compilation
CGO_ENABLED=0 go build ./cmd/crucibled
CGO_ENABLED=0 go build ./cmd/cruciblectl

# 4. Execute all table-driven unit and CLI tests with race detection
go test -v -race -timeout 60s ./...
```

---

## 6. Code of Conduct

Participation in the Crucible project is governed by the Contributor Covenant Code of Conduct. Please read [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md) for details on expected behavior and enforcement procedures.
