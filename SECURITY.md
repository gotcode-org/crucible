# Security Policy

## Supported Versions

Only the latest release and the active development branch (`main`) receive security patches and updates:

| Version | Supported          |
| ------- | ------------------ |
| `main`  | :white_check_mark: |
| < 0.1.0 | :x:                |

---

## No Warranty

Per the terms of the GNU Affero General Public License v3 ([`LICENSE`](./LICENSE)), **Crucible** is provided "as is", without warranty of any kind, express or implied. While every reasonable effort is made by maintainers to ensure the software remains resilient and secure, operators are ultimately responsible for evaluating and securing their own deployments.

---

## Operational Security Recommendations

Crucible coordinates automation and executes privileged operations across server fleets. Administrators should strictly enforce the following security practices:

1. **Enforce Mutual TLS (mTLS):** Always require mTLS for gRPC communication between the Crucible controller daemon and remote worker nodes.
2. **Secrets Vault Protection:** Keep the Ansible Vault credentials engine sealed at boot. Restrict unseal passphrases and Shamir key shares to authorized personnel.
3. **Tmpfs Isolation for Ephemeral FIFOs:** Ensure `/run/crucibled/` is backed by `tmpfs` (mode `0700` or `0750`) so ephemeral POSIX FIFOs used for credential injection never write plaintext secrets to persistent flash or disk.
4. **UNIX Domain Socket Permissions:** Ensure the local socket `/run/crucibled/crucible.sock` is restricted to root or the dedicated `crucible` system group (mode `0660`).
5. **Multi-Device 2FA & Token Rotation:** Mandate TOTP two-factor authentication for all interactive Web UI logins, and regularly audit and revoke inactive Personal Access Tokens (PATs).
6. **Filesystem Hygiene:** Protect SQLite database files (`*.db`, `*.db-wal`) and report storage directories (`var/reports/`) from unauthorized read access on the host operating system.

---

## Reporting a Suspected Vulnerability

If you discover a security vulnerability in Crucible, please report it **confidentially**. 

**DO NOT open a public issue, discussion, or pull request.**

### How to Report
Send an email to **`security@gotcode.org`** with the following information:
* A detailed description of the suspected vulnerability.
* Clear, step-by-step instructions or a proof-of-concept script to reproduce the issue.
* The specific commit SHA or version of Crucible affected.
* Any potential impact or attack vector you have identified.

### Report Conditions
Please ensure your report meets the following criteria:
* Affects the `main` branch or the latest stable release of Crucible.
* Affects a Crucible instance installed and configured according to official documentation.
* Is reproducible and verified.

> [!NOTE]
> We **DO NOT** accept unsolicited reports generated purely by automated vulnerability scanners that have not been manually verified with a working exploit or proof-of-concept.

---

## Response & Disclosure Process

1. **Acknowledgment:** We will acknowledge receipt of your vulnerability report within **48 hours**.
2. **Investigation:** Maintainers will evaluate the report, verify the issue in an isolated environment, and determine the severity.
3. **Remediation:** We will develop and test a fix across supported platforms.
4. **Coordinated Disclosure:** Once a security patch is ready, we will publish a release tag, update the [`CHANGELOG.md`](./CHANGELOG.md), and credit the reporter.

---

## Bug Bounties & Recognition

As Crucible is free and open-source software maintained by The GotCode Collective, we do not offer monetary bug bounties at this time. However, valid vulnerability reports will be publicly acknowledged and credited in our security advisories and changelog.
