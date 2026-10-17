# ADR-004: Authentication, Server-Managed Sessions, and Argon2id Hashing

## Status
Accepted

## Context
ClaimBridge orchestrates real-time financial disbursements via the PayPal Payouts API. Financial platforms face targeted credential stuffing, token theft, account takeover, and CSRF attacks.

We evaluated stateless JWT tokens in browser storage versus server-managed sessions using opaque tokens in HTTP-only cookies, and bcrypt versus Argon2id for password hashing.

## Decision
1. **Server-Managed Opaque Sessions over Stateless JWTs**:
   - JWTs cannot be instantly revoked without a distributed blacklist (which negates their stateless benefit).
   - In ClaimBridge, password resets, account lockouts, and emergency administrative freezes must revoke access **immediately**.
   - Sessions are represented by a 32-byte cryptographically secure random token given to the client in an `HttpOnly`, `SameSite=Lax` cookie.
   - The database stores only the SHA-256 hash of the session token.
2. **Argon2id for Password Hashing**:
   - Argon2id is the winner of the Password Hashing Competition (PHC) and provides superior resistance against GPU and side-channel attacks compared to bcrypt and PBKDF2.
   - Passwords are salted with 32 cryptographically random bytes and hashed using Argon2id (`v=19`, `m=64MB`, `t=3`, `p=4`).
   - Constant-time dummy evaluation is performed on non-existent accounts to eliminate timing-based user enumeration.
3. **Cryptographic Token Hashing for Email Verification & Password Reset**:
   - Single-use tokens sent via email are hashed with SHA-256 before storage in PostgreSQL.
   - Consumption is enforced with atomic transactional updates (`UPDATE ... SET consumed_at = NOW() WHERE token_hash = $1 AND consumed_at IS NULL`).
4. **Local Mailpit for Zero-Cost Local Email Testing**:
   - Developers inspect real email verification and password-reset messages via Mailpit's local SMTP server (`localhost:1025`) and web UI (`localhost:8025`) without external API keys or paid third parties.

## Consequences
### Positive
- Instant session revocation on password change or logout.
- Zero credential exposure in JavaScript, eliminating XSS token harvesting.
- Timing attack resilience and brute-force resistance.
- Zero external paid SaaS dependency for local development.

### Negative
- Requires a PostgreSQL query to validate the active session on authenticated requests (mitigated by indexed token lookups and connection pooling).
