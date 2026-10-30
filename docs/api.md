# Crucible REST API Specification

This document details the HTTP REST API exposed by the `crucibled` server
daemon. The API is consumed by the `cruciblectl` operator CLI, automated
CI/CD runners, and client scripts.

---

## 1. Architectural Overview

Crucible exposes a RESTful JSON API alongside its embedded web console:
- **Base URL**: `http://<host>:<port>` or `https://<host>:<port>`
- **Default Port**: `8080` (configurable via `server.http_port` or env)
- **UNIX Domain Socket**: `/var/run/crucible/crucibled.sock` (mode `0660`)
- **Content-Type**: `application/json; charset=utf-8`
- **Line Width Standard**: Documentation conforms to strict $\le 79$ columns.

### Authentication & Authorization Models
1. **Local UNIX Domain Socket (`/run/crucibled/crucible.sock`, mode `0660`)**:
   - Intentional local-admin trust boundary. Filesystem ownership and mode
     restrict which local administrators can connect; authorized local
     callers may administer the daemon without a PAT or browser login.
2. **Cookie Session (`crucible_session`)**:
   - `HttpOnly`, `SameSite=Strict`, secure cookie issued upon successful
     login or 2FA verification.
3. **Bearer Token (`Authorization: Bearer <token>`)**:
   - Required for remote TCP HTTP/HTTPS requests that do not carry an
     authenticated browser session. Local socket requests use the protected
     local-admin trust boundary described above.
   - Supports Personal Access Tokens (`cb_pat_...`) hashed with SHA-256
     at rest, with granular role enforcement and expiration telemetry.

---

## 2. Health & Diagnostics

### `GET /healthz`
Returns liveness and readiness status, database connectivity diagnostics,
active storage driver, WAL pragmas, and uptime telemetry.

- **Authentication**: None (publicly accessible for container orchestrators
  and load balancers).
- **Responses**:
  - `200 OK`: System healthy and database connection verified.
  - `503 Service Unavailable`: System degraded or database uncontactable.

#### Example Response (`200 OK`):
```json
{
  "status": "healthy",
  "version": "v0.1.0",
  "uptime": "2h14m12s",
  "storage": {
    "status": "connected",
    "driver": "sqlite",
    "path": "var/crucible.db",
    "wal_enabled": true,
    "user_version": 5,
    "table_count": 18
  }
}
```

---

## 3. First-Boot Setup & System Initialization

Crucible features an automated first-boot setup wizard. When the database has
zero registered accounts (`CountUsers == 0`), visitors to `/`, `/login`, or
`/operator` are redirected to `/setup`. Once an administrator account is
provisioned, setup is permanently locked and further attempts return
`403 Forbidden`.

### `GET /api/v1/setup`
Reports whether the installation currently requires first-boot setup.

- **Authentication**: None (public).
- **Responses**:
  - `200 OK`: `{"needs_setup": true|false}`

### `POST /api/v1/setup`
Initializes the system by creating the initial root administrator account and
active session. Can only be invoked when `needs_setup` is `true`.

- **Authentication**: None (first boot only).
- **Request Body**:
```json
{
  "username": "admin",
  "password": "InitialAdminPassword123!",
  "confirm_password": "InitialAdminPassword123!"
}
```
- **Responses**:
  - `201 Created`: Setup succeeded; returns admin details and session ID.
  - `400 Bad Request`: Validation error (mismatched or invalid password).
  - `403 Forbidden`: Setup has already been completed.

---

## 4. Authentication & Session Management

### `POST /api/v1/auth/login`
Authenticates user credentials against configured authentication backends
(e.g. Linux PAM or SQLite credentials).

- **Authentication**: None.
- **Request Body**:
```json
{
  "username": "operator",
  "password": "SecretPassword123!"
}
```

- **Responses**:
  - `200 OK` (Standard Login / 2FA Not Required):
    Sets `crucible_session` cookie and returns user session.
    ```json
    {
      "requires_2fa": false,
      "session": {
        "id": "ses_01j78abc...",
        "username": "operator",
        "role": "operator",
        "must_change_password": false,
        "created_at": "2026-10-29T19:00:00Z",
        "expires_at": "2026-10-30T19:00:00Z"
      },
      "user": {
        "username": "operator",
        "role": "operator",
        "must_change_password": false
      }
    }
    ```
  - `200 OK` (2FA Challenge Triggered):
    When the account has registered 2FA devices, returns challenge token:
    ```json
    {
      "requires_2fa": true,
      "challenge_token": "chl_01j78xyz..."
    }
    ```
  - `428 Precondition Required` (Mandatory 2FA Enrollment Required):
    When daemon enforces `require_2fa` but user has 0 enrolled devices:
    ```json
    {
      "error": "two-factor authentication is required; please enroll",
      "enrollment_required": true
    }
    ```
  - `401 Unauthorized`: Invalid credentials.
  - `403 Forbidden`: Account deactivated or access denied.

---

### `POST /api/v1/auth/verify-2fa`
Verifies an RFC 6238 time-based one-time passcode (TOTP) against an active
login challenge token. On success, touches `last_used_at` telemetry on the
matched authenticator device.

- **Authentication**: None (requires valid `challenge_token`).
- **Request Body**:
```json
{
  "challenge_token": "chl_01j78xyz...",
  "passcode": "123456"
}
```

- **Responses**:
  - `200 OK`: Sets `crucible_session` cookie and returns session.
    ```json
    {
      "session": {
        "id": "ses_01j78abc...",
        "username": "operator",
        "role": "operator",
        "must_change_password": false,
        "created_at": "2026-10-29T19:00:00Z",
        "expires_at": "2026-10-30T19:00:00Z"
      },
      "user": {
        "username": "operator",
        "role": "operator",
        "must_change_password": false
      },
      "device_id": "dev_01j78dev..."
    }
    ```
  - `401 Unauthorized`: Invalid passcode or expired challenge token.

---

### `GET /api/v1/auth/session`
Returns details of the currently authenticated session.

- **Authentication**: Required (`crucible_session` cookie or Bearer token).
- **Responses**:
  - `200 OK`:
    ```json
    {
      "id": "ses_01j78abc...",
      "username": "operator",
      "role": "operator",
      "must_change_password": false,
      "created_at": "2026-10-29T19:00:00Z",
      "expires_at": "2026-10-30T19:00:00Z"
    }
    ```
  - `401 Unauthorized`: Missing or invalid session.

---

## 5. Multi-Device Two-Factor Authentication (2FA)

### `GET /api/v1/auth/devices`
Lists all active RFC 6238 TOTP authenticator devices enrolled for the
authenticated operator.

- **Authentication**: Required.
- **Responses**:
  - `200 OK`:
    ```json
    [
      {
        "id": "dev_01j78dev1...",
        "username": "operator",
        "name": "YubiKey 5C",
        "created_at": "2026-10-28T14:30:00Z",
        "last_used_at": "2026-10-29T15:08:22Z",
        "enabled": true
      },
      {
        "id": "dev_01j78dev2...",
        "username": "operator",
        "name": "Work Phone",
        "created_at": "2026-10-29T10:00:00Z",
        "last_used_at": null,
        "enabled": true
      }
    ]
    ```

---

### `POST /api/v1/auth/devices`
Enrolls a new TOTP authenticator device for the authenticated operator.
Requires validating a passcode before persisting the device. Device secrets
are envelope-encrypted with AES-256-GCM at rest.

- **Authentication**: Required.
- **Request Body**:
```json
{
  "device_name": "Work Laptop",
  "secret_base32": "JBSWY3DPEHPK3PXP",
  "passcode": "123456"
}
```

- **Responses**:
  - `200 OK`:
    ```json
    {
      "id": "dev_01j78new...",
      "username": "operator",
      "name": "Work Laptop",
      "created_at": "2026-10-29T19:15:00Z",
      "last_used_at": null,
      "enabled": true
    }
    ```
  - `400 Bad Request`: Invalid passcode or malformed secret key.

---

### `DELETE /api/v1/auth/devices?id=<device_id>`
Revokes an enrolled TOTP authenticator device owned by the authenticated
operator.

- **Authentication**: Required.
- **Query Parameters**:
  - `id`: The ULID identifier of the device to revoke.
- **Responses**:
  - `204 No Content`: Device successfully revoked.
  - `400 Bad Request`: Missing device ID parameter.
  - `404 Not Found`: Device not found or owned by a different user.

---

### `POST /api/v1/auth/invite`
Generates a single-use 24-hour onboarding invitation token for shared or
administrative accounts (e.g. `root`).

- **Authentication**: Required.
- **Request Body**:
```json
{
  "username": "root",
  "label": "Emergency Break-Glass Access"
}
```

- **Responses**:
  - `200 OK`:
    ```json
    {
      "id": "inv_01j78inv...",
      "token": "inv_sec_01j78tok...",
      "username": "root",
      "label": "Emergency Break-Glass Access",
      "created_by": "admin",
      "expires_at": "2026-10-30T19:15:00Z",
      "used": false
    }
    ```

---

## 6. Personal Access Tokens (PAT) Subsystem

Personal Access Tokens provide high-entropy API key credentials for remote
`cruciblectl` operations and external automation. Plaintext tokens conform
to the format `cb_pat_<64 hex chars>` and are stored exclusively as SHA-256
hashes.

### `GET /api/v1/tokens`
Lists active and revoked personal access tokens.
- **Authentication**: Required (Bearer PAT, cookie, or UNIX socket).
- **Query Parameters**:
  - `username` (Admin only): Filter tokens by specific operator username.
  - `all=true` (Admin only): Return all tokens across all operators.
- **Responses**:
  - `200 OK`:
    ```json
    [
      {
        "id": "tok_01j78tok1...",
        "name": "workstation-cli",
        "token_prefix": "cb_pat_9f82ab12",
        "username": "alice",
        "role": "admin",
        "scopes": ["*"],
        "created_at": "2026-10-29T14:30:00Z",
        "last_used_at": "2026-10-29T15:20:00Z",
        "last_used_ip": "192.168.1.50:54321",
        "expires_at": "2026-11-28T14:30:00Z",
        "revoked": false
      }
    ]
    ```

---

### `POST /api/v1/tokens`
Generates and persists a new Personal Access Token. The plaintext token
(`raw_token`) is returned exactly once in the response payload.

- **Authentication**: Required (Bearer PAT, cookie, or UNIX socket).
- **Request Body**:
```json
{
  "name": "ci-deployer",
  "role": "operator",
  "expires_in": "720h",
  "username": "alice"
}
```
- **Responses**:
  - `201 Created`:
    ```json
    {
      "token": {
        "id": "tok_01j78new...",
        "name": "ci-deployer",
        "token_prefix": "cb_pat_11223344",
        "username": "alice",
        "role": "operator",
        "scopes": ["*"],
        "created_at": "2026-10-29T15:30:00Z",
        "expires_at": "2026-11-28T15:30:00Z",
        "revoked": false
      },
      "raw_token": "cb_pat_11223344556677889900aabbccddeeff..."
    }
    ```
  - `400 Bad Request`: Missing name or invalid expiration duration.
  - `403 Forbidden`: Non-admin operator attempted to grant admin role.

---

### `DELETE /api/v1/tokens?id=<token_id>`
Revokes an active Personal Access Token. Subsequent requests presenting
this token are immediately rejected with `401 Unauthorized`.

- **Authentication**: Required (Bearer PAT, cookie, or UNIX socket).
- **Query Parameters**:
  - `id`: The ULID identifier of the token to revoke.
- **Responses**:
  - `204 No Content`: Token successfully marked revoked.
  - `404 Not Found`: Token not found or not owned by authenticated user.

---

## 7. Web Console Form Actions

The web console provides form-based endpoints for operator interactions:

| Method | Endpoint | Description | Role |
|:-------|:---------|:------------|:-----|
| `GET`  | `/setup` | First-boot setup wizard | Guest (uninitialized) |
| `POST` | `/setup` | Initialize root admin | Guest (uninitialized) |
| `POST` | `/login` | Form login submission | Guest |
| `POST` | `/auth/2fa` | Form 2FA challenge response | Guest |
| `GET`  | `/logout` | Invalidate current session | Auth |
| `GET`  | `/settings/tokens` | Render personal tokens view | Auth |
| `POST` | `/settings/tokens/create` | Form token generation | Auth |
| `POST` | `/settings/tokens/revoke` | Form token revocation | Auth |
| `POST` | `/settings/password` | Self-service password rotation | Auth |
| `POST` | `/settings/2fa/enroll` | Enroll authenticator device | Auth |
| `POST` | `/settings/2fa/revoke` | Revoke authenticator device | Auth |
| `POST` | `/settings/2fa/invite` | Issue onboarding invite | Auth |
| `POST` | `/settings/users/create` | Provision operator account | Admin |
| `POST` | `/settings/users/update` | Update role/active/must-change | Admin |
| `POST` | `/settings/users/reset-password` | Admin reset credentials | Admin |
| `POST` | `/settings/users/status` | Toggle user active status | Admin |
| `POST` | `/settings/users/revoke-device` | Revoke user 2FA device | Admin |
| `POST` | `/settings/users/delete` | Delete operator account | Admin |

---

## 8. Standard Error Codes & Status Summary

| Status | Meaning | Typical Scenario |
|:-------|:--------|:-----------------|
| `200`  | OK | Successful query or mutation. |
| `201`  | Created | Entity created successfully with payload. |
| `204`  | No Content | Succeeded with empty body (e.g. deletion). |
| `400`  | Bad Request | Malformed JSON or missing required fields. |
| `401`  | Unauthorized | Missing session cookie or invalid credentials. |
| `403`  | Forbidden | Access denied or self-action restriction. |
| `404`  | Not Found | Requested entity (device/user) does not exist. |
| `405`  | Method Not Allowed | Invalid HTTP method for endpoint. |
| `428`  | Precondition Required | Mandatory 2FA enrollment is required. |
| `500`  | Internal Error | Server, database, or cryptographic failure. |
| `503`  | Service Unavailable | Database unreachable in `/healthz` check. |
