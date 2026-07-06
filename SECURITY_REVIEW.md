# Remainwith — Security & Code Review (Production-Grade)

This document is the top-level index for the security audit and code review findings.

## Issue Summary Table

| ID | Title | Severity | Category | File | Line | Impact |
|---:|---|---|---|---|---:|---|
| RW-SEC-001 | Authentication cookie missing `Secure` and `MaxAge` mismatch with `Expires` | **High** | Security | `internal/handler/login.go` | ~86 | JWT can be exposed over non-TLS; inconsistent expiry increases session risk |
| RW-SEC-002 | Client-readable `session_data` cookie stores sensitive identifiers; missing integrity protection | **High** | Security | `internal/handler/login.go`, `internal/handler/logout.go` | ~104 / ~18 | Session fixation / tampering risk; enables JS exfiltration and account/session correlation |
| RW-SEC-003 | JWT not bound to server-side session; `session_id` claim is not validated consistently | **Medium** | Security / Architecture | `internal/handler/middleware.go`, `internal/handler/logout.go` | ~12 / ~38 | Stolen JWT remains valid until expiry; logout not reliably invalidating |
| RW-SEC-004 | CSRF middleware base cookie is not `HttpOnly` and not `Secure` in production-safe way | **Medium** | Security | `internal/handler/middleware.go` | ~52 | CSRF token cookie accessible to JS; risk in real deployments if misconfigured |
| RW-QUAL-001 | Ignored errors when executing templates can lead to partial responses and hidden failures | **Low** | Maintainability / Correctness | `internal/handler/login.go`, `internal/handler/signup.go` | multiple | Silent failures; harder debugging; inconsistent UX |
| RW-QUAL-002 | Repeated template parsing per request (no caching) | **Low** | Performance / Maintainability | `internal/handler/*.go` | multiple | Increased CPU and latency under load |
| RW-BUG-001 | Signup password hash error ignored | **Medium** | Bug / Security | `internal/handler/signup.go` | ~86 | Potentially stores empty/invalid hash; unpredictable auth behavior |

## Individual Issue READMEs

Each issue has a dedicated README with analysis, proof-of-concept (where relevant), and a resolution path:

- `docs/issues/RW-SEC-001_cookie_secure_flags/README.md`
- `docs/issues/RW-SEC-002_session_data_cookie_tampering/README.md`
- `docs/issues/RW-SEC-003_jwt_session_invalidation_gap/README.md`
- `docs/issues/RW-SEC-004_csrf_cookie_flags/README.md`
- `docs/issues/RW-BUG-001_ignored_bcrypt_error/README.md`
- `docs/issues/RW-QUAL-001_template_execute_error_handling/README.md`
- `docs/issues/RW-QUAL-002_template_parse_caching/README.md`

## Security Risk Summary

- **Overall risk level:** **High**
- **Top 3 findings:**
  1. RW-SEC-001 — Cookies not `Secure` (JWT exposure over non-TLS).
  2. RW-SEC-002 — Client-readable cookie containing session identifiers; no integrity.
  3. RW-SEC-003 — Weak session invalidation model; stolen JWT remains valid.

### Immediate actions required

1. Make auth cookie `Secure` when behind HTTPS, and ensure consistent expiration (`MaxAge` + `Expires`).
2. Remove or harden `session_data` (avoid storing identifiers client-side; use server-side session store or sign/encrypt).
3. Introduce server-side session revocation/allow-list and validate `session_id` claim on every authenticated request.

### Long-term recommendations

- Add centralized template caching.
- Add structured logging, request IDs, and security headers.
- Add tests around auth, cookies, CSRF failure paths.

## Dependency & Environment Review

- JWT secret comes from `JWTKEY` env var (`internal/handler/jwt.go`) — good practice, but ensure:
  - Adequate entropy (32+ bytes random).
  - Separate secrets per environment.
- Add environment-driven cookie config:
  - `COOKIE_SECURE=true` in production.
  - `SameSite=Lax` for auth cookie unless strict is intended for all flows.

> Note: This audit focuses on files visible in the repository snapshot and key auth flow surfaces. Expand to other handlers and websocket paths for a full assessment.
