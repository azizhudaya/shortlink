# M2 — Accounts and Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

| | |
|---|---|
| **Product** | Shortlink (`afh.my.id`) |
| **Source PRD** | `PRD-shortlink-v2.md` v2.0 — Stories 4–8, FR-08…FR-16, §8, §9, §10.6, §13, §14. **Amended to v2.1 by this plan (§0).** |
| **Predecessor** | `M1-IMPLEMENTATION-PLAN.md` (M0 + M1) |
| **Date** | 2026-10-14 |
| **Status** | Planned, not started |

**Goal:** Open **Sign in with Google** as the only way to register and authenticate. Add server-side sessions, owner-stamped link creation, and per-owner list/disable/delete. Replace closed registration with compensating abuse controls: Safe Browsing destination checks, new-account quotas, a sign-up kill switch, and one-command account bans. Add an auditable operator takedown, so the M1 basic-auth stopgap on `app.afh.my.id` can be removed.

**Architecture:** The Go API runs the OpenID Connect authorization-code flow with Google (PKCE + `state` + `nonce`) under the existing same-origin `/api/*` route. The first sign-in creates the account; later sign-ins match it by Google `sub`. The API then issues its own server-side session cookie. Link creation gains a Safe Browsing lookup and a DB-backed daily quota. Operator actions (takedown, account ban) are subcommands of the API binary run with `docker exec`.

**Tech Stack:** Go 1.26 stdlib `net/http`, pgx v5, golang-migrate (embedded), `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, `golang.org/x/time/rate`, Google Safe Browsing Lookup API v4; Next.js 15 / React 19.

**Spec:** `shortlink/docs/PRD-shortlink-v2.md`

**M2 gate (PRD §15):** Stories 4–8 (as amended in §0) pass on the live domain; authorization verified against a second Google account.

---

## 0. PRD amendment — open registration via Google only

**Change 1 — Google is the only identity method.** There is no email/password. Registering and signing in are the same action: "Sign in with Google". PRD §4.2 listed OAuth as out of scope; this reverses that.

**Change 2 — registration is open.** Any Google account with a verified email can create an account. This reverses PRD §2.3 ("Registration is closed"), §13.2 ("Invite-code only"), FR-08, and the pre-launch item "Registration closed behind invites". Reason (product owner decision): users won't create yet another password-based account for security reasons, and invites add friction the product doesn't want.

**Why this needs compensating controls.** The PRD calls closed registration "the single most effective abuse control available". Its critical risk is phishers wrapping links on `afh.my.id`, which gets the whole domain blocklisted by Safe Browsing, including `app.afh.my.id`. That risk is now live from the first public sign-in. Google accounts are free and are routinely used by phishers, so "has a Google account" isn't a trust signal. These controls replace the invite:

| Control | What it stops |
|---|---|
| **Safe Browsing check on every destination at creation** (D23) | Wrapping URLs Google already knows are phishing, malware, or unwanted software. These are exactly the links that would get the domain blocklisted. |
| **New-account daily quota** (D24): 10 links / 24 h for accounts younger than 7 days, then 100 / 24 h | Throwaway accounts used to mass-produce links |
| **Sign-up kill switch** `SIGNUPS_OPEN=false` (D25) | Ends an abuse wave in seconds: new accounts are refused, existing ones keep working |
| **One-command ban** `user disable` (D26): deactivate + kill sessions + take down all of the account's links | Cleans up an abusive account in ≤60 s instead of code-by-code takedowns |
| **Hourly re-scan** (D27): recheck the last 30 days of active links against Safe Browsing; auto-takedown, auto-ban new or repeat-offender owners, alert | Phishing URLs that were unknown at creation and listed by Google later, which D23 can't see |
| Existing: own-domain/private-IP rejection, CSPRNG codes, per-IP/per-user rate limits, 302 + takedown | unchanged |

Weekly Safe Browsing status checks of `afh.my.id` (PRD §13.3) become **mandatory**, not advisory.

**Accepted new dependencies.** Google sign-in: if it's down, nobody can start a new session. Redirects never need auth (FR-15), sessions last 30 days, and takedown is an SSH command. Safe Browsing: if it's down, **creation fails closed** (D23); redirects are unaffected.

**Amended acceptance criteria** (Stories 6–8 unchanged):

| Story | Criterion (v2.1) |
|---|---|
| 4.1 | Given a Google account with a verified email and no existing account, when Google sign-in completes and sign-ups are open, then an account bound to that Google `sub` is created and a session starts |
| 4.2 | Given sign-ups are closed (`SIGNUPS_OPEN=false`), when a Google account with no existing account signs in, then no account is created and the user sees a sign-ups-closed message. Existing accounts still sign in. |
| 4.3 | Given a Google account whose email is not verified by Google, when sign-in completes, then it is refused |
| 4.4 | Given a destination flagged by Safe Browsing, when creation is attempted, then it is rejected (400 `UNSAFE_URL`). If Safe Browsing can't be reached, creation fails (503) rather than skipping the check. |
| 4.5 | Given an account younger than 7 days that has created 10 links in the last 24 h, when an 11th is attempted, then 429 `QUOTA_EXCEEDED` is returned |
| 5.1 | Given a successful Google sign-in for an existing account, then a session cookie is set with `HttpOnly`, `Secure`, `SameSite=Lax` |
| 5.2 | Given a callback with a missing or mismatched `state`, a bad `nonce`, or an ID token failing signature/issuer/audience/expiry checks, then no session is created |
| 5.3 | Given a deactivated account, when Google sign-in completes, then no session is created |
| 5.4 | Logout deletes the session row and clears the cookie (unchanged) |
| 5.5 | A session older than 30 days is rejected and its row purged (unchanged) |

FR-08 (invite) and FR-09 (argon2id) are **void**. FR-10, FR-11–FR-16 stand. New: **FR-18** Safe Browsing check on create (P0); **FR-19** new-account quota (P0); **FR-20** sign-up kill switch (P0); **FR-21** operator account ban with cascade takedown (P0); **FR-22** hourly Safe Browsing re-scan with auto-takedown (P0); **FR-23** operator lifts a ban (P1, D28); **FR-24** whitelisted users have no daily link quota (P1, D29); **FR-25** QR code per short link with PNG/SVG download (P1, D30); **FR-26** daily click counts per link in the database (P1, D32); **FR-27** light/dark theme toggle (P2, D33).

---

## 1. Decisions this plan makes

| # | Question | Decision | Why |
|---|---|---|---|
| D3 | User identity key | `users.google_sub` (unique, not null). `email` is a display copy refreshed from the ID token on each sign-in. | Google documents `sub` as the stable identifier; emails change and get recycled. |
| D4 | Operator takedown (Story 8) | `shortlink-api takedown -code X -reason "…" -actor aziz` writes `disabled_at`, `disabled_by='operator:<actor>'`, `disable_reason`. SQL is the fallback. | ≤60 s over SSH (NFR-12), independent of Google and the UI. `admin` stays reserved and unused. |
| D5 | `links.disabled_by` type | `BIGINT` → `TEXT` per PRD §8.1. `user:<id>` = owner disable; `operator:<name>` = takedown; NULL with `disabled_at` set = an M1-era SQL takedown, treated as operator. | One column, two kinds of actor. |
| D6 | Can an owner undo or delete a takedown? | **No.** Re-enable only if `disabled_by LIKE 'user:%'`. DELETE of a taken-down link → `409 LINK_TAKEN_DOWN`. | PRD §8.2: a taken-down code stays reserved. With open sign-up this matters more: otherwise a banned abuser's codes could be freed and re-registered. |
| D7 | Session cookie | `__Host-sl_session` = 32 random bytes base64url; DB stores its SHA-256. `HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=2592000`. Absolute 30-day expiry. | `__Host-` forbids `Domain`, so the redirect apex never sees it (PRD §14). `Lax` is needed for the top-level GET callback from Google. |
| D8 | CSRF (PRD §10.6) | Stateless `csrf = base64url(SHA-256("csrf:" ‖ session_token))`, returned by `GET /api/auth/me`, required as `X-CSRF-Token` on authenticated POST/PATCH/DELETE, constant-time compare. Login CSRF is covered by `state` (D9). | No storage; unreadable cross-origin. |
| D9 | OAuth flow state | `GET /api/auth/google/start` sets `__Host-sl_oauth` (`HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=600`) holding `state`, `nonce`, and the PKCE `code_verifier`, base64url and dot-joined. The callback requires `state` to match (constant time) and the ID token `nonce` to match, and **always clears** the cookie. | No DB table for a 10-minute value. |
| D10 | ID token verification | `go-oidc` `Provider` for `https://accounts.google.com` (discovery + JWKS cache), `Verifier` with `ClientID` audience. Then require `email_verified == true` and a non-empty `sub`. Scopes `openid email` only; no refresh token; no Google tokens stored. | A mature verifier; a minimal UU PDP footprint of email + sub. |
| D11 | Callback outcomes | Always a 302 to the UI. Success → `/links?from=signin` (the marker lets My links send a user with no links on to Create; it is dropped from the URL once read). Otherwise `/signin?error=<code>`, `code ∈ {signups_closed, email_unverified, account_disabled, failed}`. Causes are logged, never put in the URL. Google `error=access_denied` (cancel) → `/signin` without error. | Browser navigation; coarse codes leak nothing. |
| D12 | Rate limits (FR-16) | In-memory keyed `rate.Limiter`, idle eviction, cap 10 000 keys, fail closed when full. `/api/auth/google/*` per IP `rate.Every(6s)` burst 10. Create per user `rate.Every(time.Minute)` burst 20; per IP `rate.Every(30*time.Second)` burst 30. | Burst control. Daily volume is D24's job. |
| D13 | Client IP | Rightmost `X-Forwarded-For`, else `RemoteAddr`. | Caddy overwrites client XFF and is the only peer (PLT-03). |
| D14 | Expired-row purge | Hourly goroutine in `serve` deletes expired `sessions`; expired sessions are also deleted on use. | PRD §12.4, no cron. |
| D15 | Pagination | Keyset `(created_at DESC, short_code DESC)`; cursor base64url(`<unix_micros>:<code>`); limit default 50, max 100; index `links (user_id, created_at DESC, short_code DESC)`. | Stable under inserts; the same index serves D24's count. |
| D16 | M1 links with `user_id IS NULL` | Runbook SQL after the operator's first sign-in: `UPDATE links SET user_id = <id> WHERE user_id IS NULL;` | One-time. |
| D17 | Secrets | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `SAFE_BROWSING_API_KEY` in `/srv/shortlink/.env` (0600), passed to `shortlink-api` only. `GOOGLE_REDIRECT_URL` defaults to `https://app.afh.my.id/api/auth/google/callback`. The API refuses to start if any is empty. | Public repo (SEC-01); rotate anything ever committed (SEC-02). |
| D18 | Testability | The issuer URL and Safe Browsing endpoint are **constructor parameters**, not env vars; `main` passes the Google constants. Tests use `internal/oidctest` (fake OIDC provider signing with a throwaway RSA key via `go-jose`) and a fake `SafetyChecker`. | Offline, deterministic tests. A misconfigured env can't point production at a fake. |
| D19 | Web auth gate | Client-side `SessionProvider` calls `GET /api/auth/me`; a 401 routes to `/signin`. | The API is the only enforcement point. |
| D20 | Delete confirmation | `window.confirm()` naming the short URL. | Native and accessible. |
| D21 | Web tests | None added. `tsc --noEmit` + `next build` + the gate checklist. | Thin client; same as M1. |
| D22 | Go version | `go.mod` → `go 1.26`; Dockerfile builder → `golang:1.26-alpine`. | `go-oidc` v3.21 needs ≥1.25 and `x/oauth2` v0.37 needs ≥1.26. Go 1.22 is out of security support. |
| D23 | Destination reputation (FR-18) | On create, after the existing URL validation, POST the normalised URL to Safe Browsing Lookup API v4 `threatMatches:find` with threat types `MALWARE`, `SOCIAL_ENGINEERING`, `UNWANTED_SOFTWARE`, `POTENTIALLY_HARMFUL_APPLICATION`, platform `ANY_PLATFORM`, entry type `URL`, 3 s timeout. Any match → 400 `UNSAFE_URL`. Error, timeout, or non-200 → **503 `SAFETY_CHECK_UNAVAILABLE`, fail closed**, logged. Results are not cached. | This is the control that most directly prevents the domain-blocklisting scenario. Creation isn't availability-critical; redirects are, and redirects never call it. Lookup API v4 is free for non-commercial use, which this personal service is. Revisit (Web Risk API) if that changes. |
| D24 | New-account quota (FR-19) | Every link insert also appends a `link_creations(user_id, created_at)` row in the same transaction. In the create handler, after rate limits, count that user's `link_creations` rows from the last 24 h. The cap is **10** if `users.created_at > now() - 7 days`, else **100**. At the cap → 429 `QUOTA_EXCEEDED` with `Retry-After` until the oldest of those creations ages out. | It's durable across deploys (unlike D12). It counts a separate ledger rather than `links`, so deleting links doesn't refund quota; otherwise create-delete cycles would bypass the cap. |
| D25 | Sign-up kill switch (FR-20) | Env `SIGNUPS_OPEN` (default `true`). When `false`, the callback creates no new users (→ `signups_closed`); existing users still sign in. Flip by editing `.env` + `docker compose up -d shortlink-api` (~5 s). | Incident response without a deploy of new code. |
| D26 | Account ban (FR-21) | `shortlink-api user disable -email x -reason "…" -actor aziz`. In one transaction: `is_active=false`, delete all of that user's sessions, and take down every non-taken-down link they own (`disabled_by='operator:<actor>'`, `disable_reason` = the given reason). Prints the counts. Also `user list-recent [-days 7]` prints new accounts with their link counts, for the weekly review. | One command per abuser, ≤60 s. Takedown (not delete) keeps the codes reserved (D6). |
| D27 | Hourly destination re-scan (FR-22) | A goroutine in `serve`, like D14 (no cron). It runs 5 min after start, then every 1 h ± up to 5 min jitter, and stops on shutdown. Each run takes the links with `disabled_at IS NULL` created in the last **30 days**, in pages by `short_code` (the table's key), and checks them with `threatMatches:find` in batches of ≤500 URLs (the same request body as D23, 10 s timeout). For each match, it takes the link down with `disabled_by='system:safebrowsing'` and `disable_reason='safe browsing: <threat type>'`; a ban cascade's links get `banned after safe browsing match on <code>` instead, so only directly flagged links count toward the threshold and the runbook's never-restore rule. **Owner policy:** unless whitelisted (D29), the owner is auto-banned (the D26 cascade, `disabled_by='system:safebrowsing'`) if the account is under 7 days old (`newAccountDays`) **or** now has ≥2 system takedowns. Otherwise only the link is taken down, and the alert asks for review. M1 links (`user_id NULL`) are only taken down. If Safe Browsing errors, the run stops, logs `WARN`, and the next tick retries; nothing is undone. Every takedown/ban logs `ERROR rescan_takedown` (code, URL host, user_id, action). It also POSTs a one-line plain-text alert to `ALERT_WEBHOOK_URL` if set (ntfy-compatible, 5 s timeout; a failed alert is logged and never blocks a takedown). `shortlink-api links rescan` runs the same pass once and prints the report. | Closes the gap D23 leaves open: a URL that was clean at creation but is listed by Google hours later. Exposure drops from "until the weekly review" to ≤~1 h after Google lists it. In-process because it ships and is tested with the binary, needs no host setup, and stops only when redirects are down too. Auto-ban is limited to the phisher pattern (new account or repeat offender), so an established user whose destination was hacked later isn't banned without a human looking. `system:` isn't `user:`, so the owner can't undo it (D6). |
| D28 | Revoke a ban (FR-23) | `shortlink-api user enable -email x -actor aziz` sets `is_active=true` and prints `enabled user <id> (<email>)`. The user then signs in again, because their sessions were deleted by the ban. **The links taken down by the ban stay down.** The operator reviews them and restores the ones that are safe with the runbook SQL. A ban's cascade takedowns share its `disable_reason`, so that SQL can filter on it. The D27 system-takedown count isn't reset, so a lifted user who gets one more flagged link is auto-banned again. No UI. | Unbanning is trivial; automatically restoring links isn't safe, because the cascade can't tell innocent links from the phishing one. Keeping link restores manual (DB) avoids a "restore" state machine in M2. |
| D34 | Bare domain → app (FR-28, PRD v2.2) | `GET /{$}` on the API (only `afh.my.id` reaches it; Caddy sends `app.afh.my.id/` to the web container) answers 302 to `APP_URL` + `/` (default `https://app.afh.my.id`). GET and HEAD, no cookie, no DB. | In the app rather than Caddy: versioned and tested with the code, no Caddy recreate. 302 so browsers don't cache it forever if a landing page comes later. |
| D33 | Theme (FR-27) | Default follows `prefers-color-scheme`. `ThemeToggle` (top right of every page, signed in or not) is a 36 px icon button: sun in light, moon in dark. A click flips the theme by setting `data-theme` on `<html>` and storing it in `localStorage` (`theme`); flipping back to the device's theme removes both. An inline `<head>` script applies the stored value before first paint, and which icon shows is pure CSS, so nothing flashes or shifts. Dark tokens apply under `@media (prefers-color-scheme: dark) :root:not([data-theme=light])` and under `:root[data-theme=dark]`. | A per-browser preference is a convenience, so `localStorage` is enough; no account setting or cookie. If a CSP is added later, the inline theme script needs its hash in `script-src` (and `img-src` must allow `data:` for D30). |
| D32 | Click counts (FR-26) | Migration `000004` adds `link_clicks_daily(short_code, day, clicks)`, PK `(short_code, day)`, FK to `links` with cascade. A served 302 for a GET calls `clicks.Counter.Record(code)`: an in-memory increment keyed by code and UTC day, no I/O. `Counter.Run` flushes once a minute with one `INSERT … SELECT FROM unnest(…) JOIN links … ON CONFLICT DO UPDATE SET clicks = clicks + EXCLUDED.clicks` (unknown codes skipped), and once more on shutdown after the HTTP server drains. A failed flush keeps the counts for the next one. Above 100 000 pending (code, day) keys, clicks on new keys are dropped and logged. 404, 410 and HEAD are not counted. No UI; read with SQL (README runbook). | Keeps NFR-01: the redirect stays one PK read. Counts only, so nothing identifies a visitor (§14). Daily rows keep history without an event log. Accepted: bots/previews are counted, and a crash loses up to a minute. |
| D31 | Owner delete is a soft delete | Migration `000003` adds `links.deleted_at`. `DELETE /api/links/{code}` sets it (still 204). Deleted rows are invisible to the redirect (404), the owner's list, PATCH/DELETE (404), the ban cascade and the re-scan. The code stays reserved: re-creating it is 409 `ALIAS_TAKEN` for everyone, the former owner included. Operator restore: `UPDATE links SET deleted_at = NULL WHERE short_code = '…';`. | Printed links and QR codes must never start pointing at someone else's destination after a delete; keeping the row also preserves evidence for abuse review. Cost: a deleted alias can't be re-created without the operator. Account erasure (UU PDP) still removes everything via `ON DELETE CASCADE`. |
| D30 | QR codes (FR-25) | Generated in the browser with `qrcode` 1.5.4 (lazy-loaded on first open), from the `short_url` the API returned. A "Show QR"/"Hide QR" toggle on the create result and on every My links row except `taken_down` reveals a 192 px preview and two downloads: PNG (1024 px) and SVG, named `afh.my.id-<code>.png`/`.svg`. Always black on white, error correction M, 2-module margin. | No API endpoint, nothing on the redirect path, no server load, and a QR can only encode one of this service's own short links, so it can't be used to dress up an arbitrary URL. A taken-down link would only lead to a 410. If a CSP is added later, `img-src` must allow `data:`. |
| D29 | Whitelisted users (FR-24) | `users.is_whitelisted` (default false). Set with `shortlink-api user whitelist -email x [-off]` or `UPDATE users SET is_whitelisted = true WHERE lower(email) = '…'`. Whitelisted users **skip the D24 daily quota** (unlimited links). The per-minute D12 rate limit, the D23 check, and the D27 re-scan still apply. The re-scan **never auto-bans** a whitelisted user: it takes the link down and alerts with "review owner". The `link_creations` row is still written, so removing the whitelist restores normal quota accounting immediately. The account must exist (sign in once) before it can be whitelisted. `user list-recent` shows `whitelisted=<bool>`. | A trusted user needs volume, not a bypass of safety checks: one compromised trusted account must still not be able to wrap known-phishing URLs or run away with a script. A DB flag keyed to the account (Google `sub`) beats an env email list, since emails can change. |

**Out of M2:** session list/revoke (P1), account deletion UI, link editing, analytics, other identity providers, automated abuse detection beyond D23, D24 and D27.

### API surface after M2

| Method | Path | Auth | Responses |
|---|---|---|---|
| `GET` | `/api/auth/google/start` | — | 302 to Google + `__Host-sl_oauth` · 429 |
| `GET` | `/api/auth/google/callback` | oauth cookie | 302 `/links` + session cookie · 302 `/signin?error=…` · 429 |
| `POST` | `/api/auth/logout` | session + CSRF | 204, cookie cleared |
| `GET` | `/api/auth/me` | session | 200 `{"email","csrf_token"}` · 401 |
| `POST` | `/api/links` | session + CSRF | 201 · 400 (`INVALID_URL`/`INVALID_ALIAS`/**`UNSAFE_URL`**) · 409 · 429 (`RATE_LIMITED`/**`QUOTA_EXCEEDED`**) · **401** · **503 `SAFETY_CHECK_UNAVAILABLE`** |
| `GET` | `/api/links?cursor=&limit=` | session | 200 `{"links":[…],"next_cursor":string\|null}` |
| `PATCH` | `/api/links/{code}` | session + CSRF | `{"disabled":bool}` → 204 · 404 · 409 `LINK_TAKEN_DOWN` |
| `DELETE` | `/api/links/{code}` | session + CSRF | 204 · 404 · 409 `LINK_TAKEN_DOWN` |

List item: `{"code","short_url","long_url","is_custom","created_at","status"}`, where `status ∈ {"active","disabled","taken_down"}`.

New error codes in `errors.go`: `UNAUTHORIZED`, `CSRF_INVALID` (403), `RATE_LIMITED`, `QUOTA_EXCEEDED`, `UNSAFE_URL`, `SAFETY_CHECK_UNAVAILABLE`, `LINK_TAKEN_DOWN`. Every 429 sets `Retry-After` in whole seconds.

---

## Global Constraints

- Go `1.26` (D22); stdlib `ServeMux` only.
- New Go deps limited to `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, `golang.org/x/time`, and `github.com/go-jose/go-jose/v4` (transitive; tests only). Safe Browsing is called with stdlib `net/http`; no Google client library.
- All SQL parameterized.
- Session tokens, OAuth `code`/`state`/`nonce`/verifier, ID tokens, the client secret, and the Safe Browsing API key never appear in logs, generated URLs, or error bodies. Request logs carry user IDs, never emails.
- The redirect path gains no I/O: one PK read, no session lookup, no cookie, **no Safe Browsing call** (NFR-01, §14). Its only addition is an in-memory click increment (D32).
- Ownership misses return **404, never 403** (FR-12).
- JSON-body endpoints keep the M1 `Content-Type: application/json` gate (415).
- Every new route stays under `/api/`; `alias.Reserved` + the router literal test still pass.
- `000002` applies cleanly over a live M1 database.
- No new published ports, networks, or containers (PLT-03). Outbound HTTPS to Google uses the existing non-internal `edge` network.

## Review Focus

1. **Owner undoes a takedown or ban** (PATCH enable / DELETE on an operator-disabled code, including codes taken down by `user disable`) → 409, row unchanged. Task 5, Task 8, Task 9.
2. **Safe Browsing unreachable or slow** → create returns 503 within ~3 s, no row inserted, and redirects keep working. Task 8.
3. **Quota gamed by deleting links**: a new account creates 10, deletes them all, and tries an 11th → still 429. Task 5 and Task 8.
4. **Callback replay or forgery:** no oauth cookie, mismatched `state`, a token with a different `nonce`, another `aud` → no session, no user row, `/signin?error=failed`. Task 7.
5. **The same Google account changes its email** → same user (matched by `sub`), email updated. A **disabled** account that signs in again is not recreated. Task 4 and Task 7.
6. **Re-scan acts on the wrong thing, or not at all:** a flagged link from a new account → link + all the owner's links 410, sessions gone. A single hit on a mature account → only that link 410, account active. A Safe Browsing error mid-run → no false takedowns, and the next run retries. The owner can't re-enable a `system:` takedown. Task 9a.

---

## File structure

```
api/
├── go.mod, Dockerfile                   MODIFY  Go 1.26 (D22)
├── cmd/shortlink-api/main.go            MODIFY  subcommand dispatch; build Google + Safe Browsing clients
├── cmd/shortlink-api/operator.go        CREATE  takedown, user disable/enable/whitelist/list-recent, links rescan
├── internal/
│   ├── rescan/rescan.go (+_test)        CREATE  hourly Safe Browsing re-scan + owner policy (D27)
│   ├── clicks/counter.go (+_test)       CREATE  in-memory click counter, batched flush (D32)
│   ├── alert/webhook.go (+_test)        CREATE  optional plain-text webhook alert (D27)
│   ├── auth/token.go (+_test)           CREATE  session token, CSRF, OAuth flow-state codec
│   ├── auth/google.go (+_test)          CREATE  OIDC: AuthURL + Exchange→Identity
│   ├── oidctest/provider.go             CREATE  fake OIDC provider for tests
│   ├── safety/safebrowsing.go (+_test)  CREATE  Safe Browsing v4 lookup client (single + batch)
│   ├── ratelimit/ratelimit.go (+_test)  CREATE
│   ├── migrations/000002_accounts.{up,down}.sql  CREATE
│   ├── migrations/run.go                CREATE  Up(databaseURL), moved from main.go
│   ├── model/link.go                    MODIFY  DisabledBy *string, Status()
│   ├── model/user.go                    CREATE  User, Session
│   ├── store/store.go                   MODIFY  link-management + quota methods, errors
│   ├── store/auth.go                    CREATE  AuthStore interface
│   ├── store/postgres.go                MODIFY  owner-scoped link ops, takedown, quota, rescan candidates
│   ├── store/postgres_auth.go           CREATE  users, sessions, ban (by email and by ID)
│   ├── store/postgres_test.go, postgres_auth_test.go   CREATE  integration
│   ├── testdb/testdb.go (+_test)        CREATE
│   ├── config/config.go                 MODIFY  Google creds, redirect URL, SB key, SignupsOpen, InsecureCookies, AlertWebhookURL
│   └── httpserver/
│       ├── router.go, errors.go, middleware.go     MODIFY
│       ├── session.go                   CREATE  cookies, requireSession, requireCSRF
│       ├── clientip.go (+_test)         CREATE
│       ├── handlers_auth.go (+_test)    CREATE  google start/callback, logout, me
│       ├── handlers_create.go           MODIFY  owner stamp, limits, quota, safety check
│       └── handlers_links.go (+_test)   CREATE  list, patch, delete
web/
├── lib/api.ts                           MODIFY  me/logout, CSRF header, links calls, new error copy
├── lib/session.tsx                      CREATE  SessionProvider + useSession
├── app/layout.tsx                       MODIFY  provider + Nav
├── app/components/{Nav,RequireAuth}.tsx CREATE
├── app/page.tsx                         MODIFY  wrapped in RequireAuth
├── app/signin/page.tsx                  CREATE  "Continue with Google" + error messages + privacy line
├── app/links/page.tsx                   CREATE
├── app/components/QrCode.tsx            CREATE  QR toggle, preview, PNG/SVG download (D30)
├── app/components/ThemeToggle.tsx       CREATE  sun/moon theme toggle + pre-paint script (D33)
├── lib/qr.ts                            CREATE  lazy QR generation + file names (D30)
└── app/globals.css                      MODIFY
docker-compose.yml, .env.example         MODIFY  GOOGLE_*, SAFE_BROWSING_API_KEY, SIGNUPS_OPEN, ALERT_WEBHOOK_URL
LOCAL-DEV.md, README.md, api/README.md   MODIFY
docs/PRD-shortlink-v2.md                 MODIFY  v2.1 amendment (§0)
../afh-infra/Caddyfile, .env.example     MODIFY  remove basic_auth (LAST, after gate)
```

### Integration test convention

`testdb.New(t)` reads `TEST_DATABASE_URL` and calls `t.Skip` when it's unset. Otherwise it migrates, truncates `links, link_creations, sessions, users RESTART IDENTITY CASCADE`, and returns a `*pgxpool.Pool`. Run with `go test -p 1 ./...`.

```bash
psql -d postgres -c "CREATE DATABASE shortlink_test OWNER shortlink_api;"
export TEST_DATABASE_URL='postgres://shortlink_api:devpassword@localhost:5432/shortlink_test?sslmode=disable'
```

---

## 2. Tasks

Branch: `git -C shortlink switch -c m2-accounts`.

### Task 0: Google Cloud setup (operator, manual — blocks Task 10 Step 3 and Task 13)

- [ ] Create Google Cloud project `shortlink`. Configure the OAuth consent screen: **External**, app name "Shortlink", scopes `openid` + `email` only, authorized domain `afh.my.id` (verify it in Google Search Console via DNS TXT), privacy-policy URL (Task 10 adds `/privacy`), then **Publish to production**. These scopes are non-sensitive, so there's no Google review. "Testing" mode would cap the app at 100 listed users, which contradicts open sign-up. **Don't upload a logo, and defer brand verification to Task 13 Step 7a.** Reviewers can't reach `app.afh.my.id` while M1 basic auth is on, and `/privacy` doesn't exist until Task 10, so an early submission fails. Unverified, the app still works for any Google account; only the app name/logo may be missing from the consent screen.
- [ ] Create an OAuth client, type **Web application**, with redirect URIs `https://app.afh.my.id/api/auth/google/callback` and `http://localhost:3000/api/auth/google/callback`.
- [ ] Enable the **Safe Browsing API**. Create an API key restricted to that API only (API restrictions) and, once known, to the VPS's IP (application restrictions).
- [ ] Store all three values in your password manager, then in `/srv/shortlink/.env` (0600) and your local shell. **Never in the repo.**

### Task 1: Go 1.26, migration 000002, test-DB helper

**Files:** Modify `api/go.mod`, `api/Dockerfile`, `api/cmd/shortlink-api/main.go`. Create `api/internal/migrations/000002_accounts.{up,down}.sql`, `migrations/run.go`, `api/internal/testdb/testdb.go`, `testdb_test.go`.

**Interfaces — Produces:** `migrations.Up(databaseURL string) error`; `testdb.New(t *testing.T) *pgxpool.Pool`.

```sql
CREATE TABLE users (
    id            BIGSERIAL   PRIMARY KEY,
    google_sub    TEXT        NOT NULL UNIQUE,                 -- D3
    email         TEXT        NOT NULL,
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    is_whitelisted BOOLEAN    NOT NULL DEFAULT FALSE,          -- D29
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);
CREATE INDEX users_created_at_idx ON users (created_at);      -- D26 list-recent

CREATE TABLE sessions (
    token_hash BYTEA       PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent TEXT,
    ip         INET
);
CREATE INDEX sessions_user_id_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE link_creations (                                  -- D24: survives deletes
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX link_creations_user_time_idx ON link_creations (user_id, created_at);

ALTER TABLE links ALTER COLUMN disabled_by TYPE TEXT USING disabled_by::text;   -- D5
ALTER TABLE links ADD CONSTRAINT links_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
DROP INDEX IF EXISTS idx_links_user_id;
CREATE INDEX links_user_id_created_at_idx ON links (user_id, created_at DESC, short_code DESC);  -- D15
```

`down` reverses this in the opposite order. `link_creations` rows older than 24 h are purged by the D14 loop.

- [ ] **Step 1:** Bump `go.mod` to `go 1.26` and the Dockerfile to `golang:1.26-alpine`. Run `go build ./... && go test ./... && docker build .` → all pass.
- [ ] **Step 2: Write the failing test** `TestMigrationsApplyOverM1Data`: migrate to version 1, insert an M1 row (`user_id NULL`, `disabled_at` set), run `migrations.Up`. Assert the row survives with `disabled_by` NULL, `user_id = 999` fails with `23503`, and a second `Up` returns nil.
- [ ] **Step 3: Run** `TEST_DATABASE_URL=… go test -p 1 ./internal/testdb/ -v`. Expected: FAIL.
- [ ] **Step 4: Implement** the SQL, `migrations.Up`, and `testdb.New`.
- [ ] **Step 5: Run.** Expected: PASS. Without `TEST_DATABASE_URL`: PASS with SKIP.
- [ ] **Step 6: Commit** `feat(api): Go 1.26, accounts schema migration (M2)`

### Task 2: Tokens and the OAuth-state cookie codec

**Files:** Create `api/internal/auth/token.go`, `token_test.go`.

**Interfaces — Produces:**
- `NewSessionToken() (token string, hash []byte, err error)`: 32 bytes, base64url without padding; hash = SHA-256 of the token string.
- `HashToken(token string) []byte`
- `CSRFToken(sessionToken string) string`; `CSRFValid(sessionToken, presented string) bool` (D8)
- `type FlowState struct{ State, Nonce, Verifier string }`; `NewFlowState() (FlowState, error)` (State/Nonce 32 bytes; Verifier from `oauth2.GenerateVerifier()`); `(FlowState) Encode() string`; `DecodeFlowState(s string) (FlowState, error)` (D9)

- [ ] **Step 1: Write failing tests:** session token is 43 chars, URL-safe, unique; `HashToken` matches. `CSRFValid` round-trips, and is false for another token and for `""`. `FlowState` round-trips. `DecodeFlowState` rejects `""`, `"a.b"`, `"a.b.c.d"`, and a part containing `+`.
- [ ] **Step 2: Run** `go test ./internal/auth/ -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** `go get golang.org/x/oauth2`.
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): session tokens, CSRF derivation, OAuth flow-state codec`

### Task 3: Google OIDC client and fake provider

**Files:** Create `api/internal/auth/google.go`, `google_test.go`, `api/internal/oidctest/provider.go`.

**Interfaces — Produces:**
- `type Identity struct{ Sub, Email string }`; `ErrEmailUnverified`, `ErrInvalidToken`
- `func NewGoogle(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string) (*Google, error)`: `oidc.NewProvider` discovery. `main` passes `"https://accounts.google.com"` (D18).
- `func (g *Google) AuthURL(fs FlowState) string`: scopes `openid email`; `state`; `oidc.Nonce`; `oauth2.S256ChallengeOption(fs.Verifier)`; `prompt=select_account`.
- `func (g *Google) Exchange(ctx context.Context, code string, fs FlowState) (Identity, error)`: `oauth2.VerifierOption`, extract `id_token`, `Verify`, constant-time nonce check, `email_verified == true`, non-empty `sub`. Other failures → `ErrInvalidToken` (cause wrapped for logs).
- `oidctest.New(t) *Provider`: `URL()`, `ClientID`, `ClientSecret`, `Next(claims map[string]any)`. Serves discovery, `/jwks`, `/authorize` (records `state`, `nonce`, and `code_challenge`; 302s back with `code` + `state`), and `/token` (checks `code_verifier` against the recorded challenge). RS256 with a per-test key.

- [ ] **Step 1: Write failing tests** against `oidctest`: `TestExchangeHappyPath`. `TestExchangeRejects` (table: wrong nonce, wrong aud, wrong iss, expired, missing sub, foreign signing key → `ErrInvalidToken`; `email_verified:false` → `ErrEmailUnverified`). `TestExchangeWrongVerifier` → `ErrInvalidToken`. `TestAuthURLParams` (`scope=openid+email`, `code_challenge_method=S256`, state, nonce, `prompt=select_account`, no `access_type=offline`).
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.** `go get github.com/coreos/go-oidc/v3`.
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): Google OIDC client with PKCE/nonce, fake provider for tests`

### Task 4: Auth store

**Files:** Create `api/internal/model/user.go`, `api/internal/store/auth.go`, `postgres_auth.go`, `postgres_auth_test.go`.

**Interfaces — Produces:**
- `model.User{ID int64; GoogleSub, Email string; IsActive, IsWhitelisted bool; CreatedAt time.Time}`; `model.Session{UserID int64; Email string; ExpiresAt time.Time}`
- `store.ErrSessionInvalid`, `store.ErrUserDisabled`, `store.ErrSignupsClosed`

```go
type AuthStore interface {
    // Existing sub → refresh email + last_login_at (ErrUserDisabled if inactive).
    // Unknown sub → insert when allowNew, else ErrSignupsClosed. Race-safe:
    // INSERT ... ON CONFLICT (google_sub) DO NOTHING, then re-select.
    UpsertGoogleUser(ctx context.Context, sub, email string, allowNew bool) (u model.User, created bool, err error)
    CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time, userAgent, ip string) error
    // Joins users; inactive → ErrSessionInvalid; expired row deleted, then ErrSessionInvalid.
    GetSession(ctx context.Context, tokenHash []byte) (model.Session, error)
    DeleteSession(ctx context.Context, tokenHash []byte) error
    PurgeExpired(ctx context.Context) error // sessions past expiry + link_creations older than 24h
    // D26. One tx: is_active=false, delete sessions, take down all non-taken-down links.
    BanUser(ctx context.Context, emailLower, actor, reason string) (linksTakenDown int, sessionsDeleted int, err error) // ErrNotFound
    ListRecentUsers(ctx context.Context, since time.Time) ([]RecentUser, error) // RecentUser{User model.User; Links int}
    // D28. is_active=true; links untouched. ErrNotFound; same ambiguous-email rule as BanUser.
    EnableUser(ctx context.Context, emailLower string) (model.User, error)
    // D29. ErrNotFound; same ambiguous-email rule as BanUser.
    SetWhitelisted(ctx context.Context, emailLower string, on bool) (model.User, error)
}
```

`BanUser` matches `lower(email)`. If more than one active user has that email (possible after recycling), it returns an error listing their IDs and changes nothing.

- [ ] **Step 1: Write failing tests:** `TestUpsertCreatesThenMatchesBySub` (created=true, then false with the same ID). `TestUpsertUpdatesEmail` (Review Focus 5). `TestUpsertSignupsClosed` (unknown sub with allowNew=false → `ErrSignupsClosed`, 0 users; a known sub still succeeds). `TestUpsertDisabledUserNotRecreated` (Review Focus 5) → `ErrUserDisabled`, user count unchanged. `TestSessionLifecycle`. `TestExpiredSessionRejectedAndPurged`. `TestInactiveUserSessionRejected`. `TestPurgeExpired`. `TestBanUserCascades`: 3 links (1 owner-disabled) + 2 sessions → takedown count 3, sessions deleted 2, every link `disabled_by='operator:aziz'`, `is_active=false`. `TestBanUnknownEmail` → `ErrNotFound`. `TestListRecentUsers`. `TestEnableUserKeepsLinksDown` (ban, enable → active, `UpsertGoogleUser` succeeds, the 3 links still taken down). `TestSetWhitelisted` (on/off; unknown email → `ErrNotFound`).
- [ ] **Step 2: Run** `TEST_DATABASE_URL=… go test -p 1 ./internal/store/ -v`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): Google-bound users, sessions, account ban`

### Task 5: Link ownership store and quota

**Files:** Modify `api/internal/model/link.go`, `api/internal/store/store.go`, `postgres.go`. Create `postgres_test.go`.

**Interfaces — Produces:**
- `model.Link.DisabledBy *string`; `func (l Link) Status() string` → `"active"` | `"disabled"` (prefix `user:`) | `"taken_down"`.
- `store.ErrTakenDown`, `store.ErrBadCursor`.

```go
// Both insert the link AND a link_creations row in one transaction (D24).
CreateWithGeneratedCode(ctx context.Context, userID int64, longURL string) (model.Link, error)
CreateWithAlias(ctx context.Context, userID int64, alias, longURL string) (model.Link, error)
// D24: creations in the last 24h, the oldest one's time, the account's created_at, and D29's whitelist flag.
CreationQuota(ctx context.Context, userID int64) (count int, oldest time.Time, accountCreated time.Time, whitelisted bool, err error)
ListByOwner(ctx context.Context, userID int64, cursor string, limit int) (links []model.Link, nextCursor string, err error)
SetDisabledByOwner(ctx context.Context, userID int64, code string, disabled bool) error // ErrNotFound, ErrTakenDown; idempotent
DeleteByOwner(ctx context.Context, userID int64, code string) error                     // ErrNotFound, ErrTakenDown
Takedown(ctx context.Context, code, actor, reason string) error                         // any owner; ErrNotFound
```

Fetch `limit+1` rows to decide `nextCursor`. For disable/enable/delete, use one conditional statement; on 0 rows, a follow-up `SELECT` separates `ErrNotFound`, `ErrTakenDown`, and an idempotent no-op.

- [ ] **Step 1: Write failing tests** (users U1/U2 via `UpsertGoogleUser`): `TestCreateStampsOwner`, `TestCreateRecordsCreation`, `TestQuotaSurvivesDelete` (Review Focus 3: create 3, delete 3 → count 3), `TestListOwnerScopedNewestFirst`, `TestListPaginates` (120 → 50/50/20), `TestListBadCursor`, `TestCrossOwnerIsNotFound`, `TestOwnerDisableEnable`, `TestTakedownBlocksOwner` (Review Focus 1), `TestM1NullDisabledByIsTakenDown`, `TestDeleteFreesCode`, `TestTakedownUnknown`.
- [ ] **Step 2: Run.** Expected: FAIL. Patch the `handlers_create.go` compile break with a placeholder `0`, which Task 8 replaces.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): owner-scoped links, takedown, creation quota ledger`

### Task 6: Rate limiter, client IP, Safe Browsing client

**Files:** Create `api/internal/ratelimit/ratelimit.go` (+ test), `api/internal/httpserver/clientip.go` (+ test), `api/internal/safety/safebrowsing.go` (+ test). `go get golang.org/x/time`.

**Interfaces — Produces:**
- `ratelimit.New(every time.Duration, burst int, idleTTL time.Duration, maxKeys int) *Limiter`; `(*Limiter).Allow(key string) (ok bool, retryAfter time.Duration)`, using `Reserve()` + `Cancel()`, failing closed when full, with an injectable `now`.
- `clientIP(r *http.Request) string` (D13)
- `safety.ErrUnsafe`, `safety.ErrUnavailable`; `type Checker interface { Check(ctx context.Context, url string) error }`; `safety.NewSafeBrowsing(endpoint, apiKey string, client *http.Client) *SafeBrowsing`. `main` passes `https://safebrowsing.googleapis.com/v4/threatMatches:find`, `&http.Client{Timeout: 3 * time.Second}`. The API key goes in the `key` query param; request body per D23; `clientId: "shortlink"`, `clientVersion: "2"`. `{}` → nil; any `matches` → `ErrUnsafe`; anything else → `ErrUnavailable` (wrapping the cause, **never** including the key in the error string).

- [ ] **Step 1: Write failing tests.** Limiter: burst allowed, burst+1 refused with `retryAfter > 0`; keys independent; idle eviction; full map refused. `clientIP`: `"1.1.1.1, 2.2.2.2"` → `2.2.2.2`; `"3.3.3.3:5"` → `3.3.3.3`; `"[::1]:5"` → `::1`. Safe Browsing against `httptest`: `{}` → nil; a body with `matches` → `ErrUnsafe`; 500 → `ErrUnavailable`; a handler sleeping past a 100 ms client timeout → `ErrUnavailable`; the request JSON contains the four threat types and the URL; the error string never contains the test API key.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): rate limiter, client IP, Safe Browsing lookup client`

### Task 7: Sign-in endpoints and session middleware

**Files:** Create `httpserver/session.go`, `handlers_auth.go`, `handlers_auth_test.go`. Modify `router.go`, `errors.go`, `middleware.go`, `config/config.go`, `cmd/shortlink-api/main.go`.

**Interfaces:**
- Consumes: Tasks 2, 3, 4, 6.
- Produces:
  - `type IdentityProvider interface { AuthURL(auth.FlowState) string; Exchange(ctx context.Context, code string, fs auth.FlowState) (auth.Identity, error) }`
  - `NewServer(cfg config.Config, st store.Store, as store.AuthStore, idp IdentityProvider, checker safety.Checker, logger *slog.Logger) *Server`
  - `requireSession`, `requireCSRF`, `sessionFrom(ctx)`, `requireJSON` (the M1 gate, extracted), `writeRateLimited`
  - `config.Config` gains `GoogleClientID`, `GoogleClientSecret`, `GoogleRedirectURL` (default per D17), `SafeBrowsingAPIKey`, `SignupsOpen` (env `SIGNUPS_OPEN`, default true; only the literal `false` closes), and `InsecureCookies` (`INSECURE_DEV_COOKIES=1`: drops `__Host-`/`Secure` for http://localhost only). `main` exits if any credential is empty.

**Start:** per-IP limit → `NewFlowState()` → set the oauth cookie → 302 to `idp.AuthURL(fs)`.

**Callback:** per-IP limit → read and **clear** the oauth cookie → `error` param → `/signin` → no cookie or `state` mismatch → `failed` → `Exchange` (`ErrEmailUnverified` → `email_unverified`; else `failed`) → `UpsertGoogleUser(sub, email, cfg.SignupsOpen)` (`ErrSignupsClosed` → `signups_closed`; `ErrUserDisabled` → `account_disabled`) → `CreateSession` → session cookie → 302 `/links`. Log `user_id` and `created` on success.

- [ ] **Step 1: Write failing integration tests** (real DB, `auth.NewGoogle` on `oidctest`; helper `signIn(t, sub, email) *http.Response` drives start → authorize → callback with a cookie jar, without following the final redirect):
  - `TestFirstSignInCreatesAccount`: `/links`; cookie `__Host-sl_session` with `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, `Max-Age=2592000`; `/me` returns the email + CSRF token; 1 user row.
  - `TestSecondSignInSameAccount`: still 1 user row, 2 sessions.
  - `TestSignupsClosed`: server built with `SignupsOpen=false` → a new sub gets `signups_closed` with 0 rows; a pre-existing user signs in OK.
  - `TestUnverifiedEmail` → `email_unverified`, 0 rows.
  - `TestDisabledAccount` → `account_disabled`, no session.
  - `TestCallbackForgery` (Review Focus 4, table: no oauth cookie / altered state / different nonce / different aud) → `failed`, 0 sessions, 0 users.
  - `TestCallbackClearsOAuthCookie` (success and failure).
  - `TestUserCancelled` → `/signin`.
  - `TestMeRequiresSession` → 401.
  - `TestLogoutRequiresCSRF` → 403 without the header; 204 with it; the old cookie then → 401.
  - `TestSecretsNeverLogged`: the session token, oauth `code`/`state`/`nonce`/verifier, and client secret never appear in captured `slog` output.
- [ ] **Step 2: Run** `TEST_DATABASE_URL=… go test -p 1 ./internal/httpserver/ -v`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run.** Expected: PASS. M1 tests still pass.
- [ ] **Step 5: Commit** `feat(api): open Google sign-in with kill switch, sessions, CSRF`

### Task 8: Authenticated link endpoints with abuse controls

**Files:** Modify `httpserver/handlers_create.go`, `router.go`. Create `handlers_links.go`, `handlers_links_test.go`.

**Interfaces:** Consumes Task 7 middleware, the Task 5 store, and `safety.Checker`. Routes: `POST /api/links` = session→CSRF→JSON; `GET /api/links` = session; `PATCH /api/links/{code}` = session→CSRF→JSON; `DELETE /api/links/{code}` = session→CSRF.

**Create order** (cheapest rejection first, and never call Google for a request that would fail anyway): per-user limit → per-IP limit → `CreationQuota` (D24; cap not enforced when `whitelisted`, D29) → JSON decode + `urlvalidate` → alias validation → `checker.Check(longURL)` (`ErrUnsafe` → 400 `UNSAFE_URL` "This destination is flagged as unsafe by Google Safe Browsing."; `ErrUnavailable` → 503 `SAFETY_CHECK_UNAVAILABLE` "We couldn't verify this link's safety right now. Try again in a minute.") → store insert. Log flagged URLs at `WARN` with `user_id` and the URL's host only. Remove the M1 "UNAUTHENTICATED" note.

List `limit` 1–100 (default 50), otherwise 400. `ErrBadCursor` → 400; `ErrNotFound` → 404; `ErrTakenDown` → 409 `LINK_TAKEN_DOWN` "This link was disabled by the operator and can't be changed." Limiter params and the D24 caps (`newAccountDays=7`, `newAccountCap=10`, `cap=100`) are `Server` fields, so tests can override them.

- [ ] **Step 1: Write failing tests** (accounts A and B via `signIn`; fake checker):
  - Basics: `TestCreateRequiresSession` (401), `TestCreateRequiresCSRF` (403), `TestCreateStampsOwnerViaHTTP`.
  - `TestCreateUnsafeURL` → 400 `UNSAFE_URL`, no row.
  - `TestCreateSafetyUnavailable` (Review Focus 2) → 503, no row; and a pre-existing link still 302s during the outage.
  - `TestSafetyNotCalledWhenInvalid`: a `javascript:` URL → 400, and the fake checker's call count is 0.
  - `TestNewAccountQuota` (Review Focus 3): 10 creates → 201; delete all 10; 11th → 429 `QUOTA_EXCEEDED` with `Retry-After`.
  - `TestWhitelistedNoQuota` (D29): a new account with `is_whitelisted=true` (cap overridden to 3) → the 4th create is 201; an unsafe URL is still 400 `UNSAFE_URL`; the per-user rate limit still returns 429 `RATE_LIMITED`.
  - `TestMatureAccountQuota`: `users.created_at` set to 8 days ago → the cap is 100 (override to 12 for test speed and assert the 13th is 429).
  - `TestListOnlyOwn`, `TestListPagination` (51 → 50 + cursor, then 1 + `null`), `TestListBadLimitAndCursor`, `TestCrossAccountPatchAndDelete404` (A's link still 302), `TestDisableThenRedirect410ThenEnable302`, `TestDeleteThenRedirect404AndAliasReusable`, `TestTakenDownLinkOwnerCannotEnableOrDelete`, `TestCreateRateLimitedPerUser`, `TestRedirectSetsNoCookie`.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -p 1 ./... && go vet ./... && gofmt -l .`. Expected: PASS, and `gofmt` prints nothing.
- [ ] **Step 5: Commit** `feat(api): authenticated create with Safe Browsing + quota; owner-scoped list/disable/delete`

### Task 9: Operator subcommands, purge loop, compose

**Files:** Modify `cmd/shortlink-api/main.go`, `docker-compose.yml`, `.env.example`. Create `cmd/shortlink-api/operator.go`, `operator_test.go`.

**Interfaces:** `os.Args[1]` ∈ {absent, `serve`} → serve; `takedown` → `runTakedown(args []string, out io.Writer) error`; `user` → `runUser(args []string, out io.Writer) error` with `disable`, `enable`, `whitelist`, and `list-recent`; otherwise usage, exit 2. Subcommands need only `DATABASE_URL`, never migrate, and refuse if the schema version is below 2.
- `takedown -code -reason -actor` (all required) → `disabled <code> by operator:<actor> at <time>`.
- `user disable -email -reason -actor` (all required) → `disabled user <id> (<email>): <n> links taken down, <m> sessions ended`.
- `user enable -email -actor` (both required; D28) → `enabled user <id> (<email>); its taken-down links were not restored`. Logs the actor.
- `user whitelist -email [-off]` (D29) → `whitelisted user <id> (<email>)` / `removed whitelist for user <id> (<email>)`.
- `user list-recent [-days 7]` → one line per user: `id  created_at  email  links=<n>  active=<bool>  whitelisted=<bool>`.
- Serve starts the hourly `PurgeExpired` loop (D14), stopped on shutdown.
- Compose `shortlink-api.environment` gains `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `SAFE_BROWSING_API_KEY` (each `${VAR}`), and `SIGNUPS_OPEN: ${SIGNUPS_OPEN:-true}`. `.env.example` gets placeholders and a comment on the kill switch.

- [ ] **Step 1: Write failing tests:** `TestTakedownRequiresReasonAndActor`, `TestTakedownSetsColumns`, `TestUserDisableCascades` (Review Focus 1: after it, the owner's PATCH enable → `ErrTakenDown`), `TestUserDisableRequiresFlags`, `TestUserEnableRequiresFlags`, `TestUserEnableThenSignInWorks`, `TestUserWhitelistOnOff`, `TestUserListRecent` (includes `whitelisted=`).
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement** with `flag.NewFlagSet` per subcommand.
- [ ] **Step 4: Run** the tests; `docker build api/`; `docker run --rm <img> user` → usage error. Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): takedown, user ban/unban/whitelist/list operator subcommands, hourly purge`

### Task 9a: Hourly Safe Browsing re-scan (D27)

**Files:** Create `api/internal/rescan/rescan.go` (+ test), `api/internal/alert/webhook.go` (+ test). Modify `safety/safebrowsing.go` (+ test), `store/store.go`, `store/postgres.go`, `store/postgres_auth.go` (+ tests), `config/config.go`, `cmd/shortlink-api/main.go`, `operator.go` (+ test), `docker-compose.yml`, `.env.example`.

**Interfaces:**
- Consumes: Task 4 `BanUser` cascade, Task 5 takedown, the Task 6 Safe Browsing client, the Task 9 serve loop and subcommand dispatch.
- Produces:
  - `safety`: `type BatchChecker interface { CheckBatch(ctx context.Context, urls []string) (flagged map[string]string, err error) }` maps URL → threat type. `*SafeBrowsing` implements it; >500 URLs → error (the caller pages). Matches are mapped back by the exact URL string sent. Errors → `ErrUnavailable`, with the same key-redaction rule as `Check`.
  - Store:
    ```go
    // D27: active links (disabled_at IS NULL) with created_at >= since and short_code > afterCode, ordered by short_code.
    RescanCandidates(ctx context.Context, since time.Time, afterCode string, limit int) ([]RescanLink, error)
    // RescanLink{Code, LongURL string; UserID *int64; UserCreatedAt time.Time; UserActive, UserWhitelisted bool}
    // Sets disabled_by='system:safebrowsing'. No-op (changed=false) if already disabled. ownerFlagged counts the
    // owner's links with a 'safe browsing:' reason, this one included (0 for a NULL owner).
    SystemTakedown(ctx context.Context, code, reason string) (changed bool, ownerFlagged int, err error)
    // D26's cascade by ID with a full disabled_by value. BanUser(email) is refactored to call the same tx with "operator:"+actor.
    BanUserByID(ctx context.Context, userID int64, disabledBy, reason string) (linksTakenDown, sessionsDeleted int, err error)
    ```
  - `rescan.New(st Store, checker safety.BatchChecker, notify alert.Notifier, logger *slog.Logger) *Rescanner`, with fields `Window=30*24h`, `Page=500`, `NewAccountDays=7`, `RepeatThreshold=2`, and `now` (injectable). `(*Rescanner).RunOnce(ctx) (Report, error)`, where `Report{Scanned, TakenDown, Banned int; Actions []Action}`. `(*Rescanner).Loop(ctx, first, every, jitter time.Duration)`.
  - `alert.Notifier` interface `Notify(ctx, msg string) error`; `alert.NewWebhook(url string, client *http.Client)`; `alert.Nop{}` when the URL is empty. The message is `"[shortlink] <action> code=<code> host=<host> user=<id>"`, never the full URL or an email.
  - `config.Config.AlertWebhookURL` (env `ALERT_WEBHOOK_URL`, optional).
  - Subcommand `links rescan` → `runLinks(args, out)`: runs `RunOnce` and prints `scanned=<n> taken_down=<n> banned=<n>`, then one line per action. Exit 1 on `ErrUnavailable`.
  - Serve starts `Loop(ctx, 5*time.Minute, time.Hour, 5*time.Minute)` next to the D14 purge loop, stopped on shutdown.
  - Compose gains `ALERT_WEBHOOK_URL: ${ALERT_WEBHOOK_URL:-}`.

- [ ] **Step 1: Write failing tests.**
  - `safety`: `TestCheckBatchMapsMatches` (3 URLs, 1 match → map with 1 entry and its threat type); `TestCheckBatchRejectsOver500`; `TestCheckBatchErrorIsUnavailable` (500 / timeout); request JSON lists every URL; the key is never in the error.
  - Store (real DB): `TestRescanCandidatesWindowAndPaging` (skips disabled links and links older than the window; pages by `id`); `TestSystemTakedownCountsAndIsIdempotent`; `TestSystemTakedownOwnerCannotUndo` (PATCH enable → `ErrTakenDown`; Review Focus 6); `TestBanUserByIDCascades`; the existing `TestBanUserCascades` still passes.
  - `rescan` (fake store + fake checker + recording notifier, fixed `now`): `TestNewAccountFlaggedIsBanned`; `TestMatureAccountSingleHitLinkOnly` (account active, notifier says "review owner"); `TestMatureAccountSecondHitBanned`; `TestM1LinkTakenDownNoBan`; `TestWhitelistedNeverAutoBanned` (new account, 2 hits → both links down, account active, "review owner" alerts; D29); `TestAlreadyInactiveOwnerNotRebanned`; `TestCheckerErrorStopsRunNoTakedowns` (Review Focus 6); `TestPagesOver500`; `TestNotifierFailureDoesNotBlockTakedown`; `TestLoopStopsOnCancel`.
  - `alert`: posts a `text/plain` body; a 500 response → error; `Nop` does nothing.
  - Operator: `TestLinksRescanPrintsReport`.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.** Order per flagged link: `SystemTakedown`, then (per policy) `BanUserByID(userID, "system:safebrowsing", reason)`, then log, then notify. Notify each owner once per run.
- [ ] **Step 4: Run** `go test -p 1 ./... && go vet ./... && gofmt -l .`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(api): hourly Safe Browsing re-scan with auto-takedown and owner policy`

### Task 10: Web — session plumbing, sign-in, privacy

**Files:** Modify `web/lib/api.ts`, `web/app/layout.tsx`, `web/app/page.tsx`. Create `web/lib/session.tsx`, `web/app/components/Nav.tsx`, `RequireAuth.tsx`, `web/app/signin/page.tsx`, `web/app/privacy/page.tsx`.

**Interfaces — Produces (`lib/api.ts`):** `type Me = { email: string; csrf_token: string }`; `me(): Promise<ApiResult<Me>>`; `logout(): Promise<void>`; `setCsrfToken(t: string | null)` attached as `X-CSRF-Token` on every mutating call (including `createLink`); `ApiResult` failure gains `status: number`. `messageFor` adds:
- `RATE_LIMITED` → "Too many requests. Try again in N minutes."
- `QUOTA_EXCEEDED` → "You've reached today's link limit. New accounts can create 10 links a day for their first week."
- `UNSAFE_URL` → API message
- `SAFETY_CHECK_UNAVAILABLE` → API message

Pages:
- `SessionProvider` exposes `{ status: 'loading'|'authed'|'anon', me, refresh, signOut }`. `RequireAuth` → `router.replace('/signin')`.
- `/signin`: a plain `<a href="/api/auth/google/start">Continue with Google</a>` styled as a button (full navigation, works without JS), plus the line "Shortlink stores only your Google email and account ID. We never see your Google password." and a link to `/privacy`. `?error=` copy, rendered `role="alert"`:
  - `signups_closed` → "New sign-ups are temporarily paused. Please try again later."
  - `email_unverified` → "Your Google account's email isn't verified."
  - `account_disabled` → "This account has been disabled."
  - `failed` → "Sign-in didn't complete. Please try again."
  
  Authed → `/links`. Use `<Suspense>` around the `useSearchParams` reader.
- `/privacy`: static. States what is stored (Google `sub`, email, session IP/user-agent for 30 days), that visitors to short links aren't tracked, the UU PDP basis, and how to request deletion. Required by the Google consent screen (Task 0).
- `Nav`: Create · My links · Sign out, hidden when anon.

- [ ] **Step 1:** Implement `lib/api.ts` + `lib/session.tsx`; run `npx tsc --noEmit`.
- [ ] **Step 2:** Implement the pages, Nav, and the `RequireAuth` wrap on `/`.
- [ ] **Step 3: Verify locally** (needs Task 0; API run with `INSECURE_DEV_COOKIES=1 GOOGLE_REDIRECT_URL=http://localhost:3000/api/auth/google/callback`): sign in with Google → `/links`. Sign out → `/` redirects to `/signin`. Restart the API with `SIGNUPS_OPEN=false` → a second Google account gets the paused message, while the first still signs in. Create `http://testsafebrowsing.appspot.com/s/phishing.html` (Google's official test URL) → "flagged as unsafe". Keyboard-only with visible focus.
- [ ] **Step 4: Run** `npx tsc --noEmit && npm run build`. Expected: success.
- [ ] **Step 5: Commit** `feat(web): Google sign-in, privacy page, session-gated creation`

### Task 11: Web — My links page

**Files:** Create `web/app/links/page.tsx`. Modify `web/lib/api.ts`, `web/app/globals.css`.

**Interfaces:** `type LinkItem = CreateResponse & { status: 'active'|'disabled'|'taken_down' }`; `listLinks(cursor?: string): Promise<ApiResult<{ links: LinkItem[]; next_cursor: string | null }>>`; `setDisabled(code: string, disabled: boolean): Promise<ApiResult<null>>`; `deleteLink(code: string): Promise<ApiResult<null>>`.

Behaviour: semantic `<ul>` (fits 320 px). Each row shows the short URL + copy, destination host + truncated path (full URL in `title`), created date, and status badge. Actions: Copy and Show QR on the left; Disable/Enable (pause/play icon) and Delete (trash icon) as labelled round icon buttons on the right, all on one row down to ~360 px. `taken_down` rows show "Disabled by operator" and no actions. "Load more" uses `next_cursor`. Arriving from sign-in (`?from=signin`) with no links redirects to `/` (Create); opened any other way, an empty list shows "No links yet. Create one". Delete → `window.confirm(\`Delete ${short_url}? It will stop working, and this code can't be used again.\`)`. Any 401 → `signOut()` → `/signin`. Action results go to `aria-live="polite"`.

- [ ] **Step 1:** Implement the API functions; run `npx tsc --noEmit`.
- [ ] **Step 2:** Implement the page and styles.
- [ ] **Step 3: Verify locally:** "Load more" above 50 with no duplicates. Disable → `:8080/<code>` 410; enable → 302. Cancelling Delete keeps the row; confirming → 404. A takedown row shows the operator badge and no actions. No horizontal scroll at 320 px.
- [ ] **Step 4: Run** `npx tsc --noEmit && npm run build`. Expected: success.
- [ ] **Step 5: Commit** `feat(web): my-links page with pagination, disable/enable, delete`

### Task 12: Documentation and PRD v2.1

**Files:** Modify `docs/PRD-shortlink-v2.md`, `README.md`, `LOCAL-DEV.md`, `api/README.md`.

- PRD → v2.1, applying §0 of this plan:
  - §2.3: drop "Registration is closed".
  - §4.1 M2 bullets: Google sign-in + abuse controls.
  - §4.2: remove the OAuth and open-registration exclusions.
  - Stories 4–5 → v2.1 criteria. FR-08/FR-09 void; add FR-18–FR-22 (FR-22 = hourly re-scan, D27).
  - §8.1 schema → Task 1's. §9.1 auth endpoints.
  - §10.6: drop the password and login-throttle rows; add "OIDC: PKCE, state, nonce, `email_verified` required, identity = `sub`".
  - §13.2: Registration row → "Open via Google; kill switch `SIGNUPS_OPEN`". Add rows for Safe Browsing (on create + hourly re-scan), quota, and ban.
  - §13.3: weekly reputation check and `user list-recent` review are mandatory.
  - §14: add Google `sub` and the scopes. §16: add "Abuse via open sign-up" (Likelihood: **High**) and "Google sign-in / Safe Browsing outage".
  - §17: replace "Closed registration" with "Open registration" and its rationale. §18: Google Identity + Safe Browsing dependencies.
  - Pre-launch checklist: replace "Registration closed behind invites" with "Safe Browsing check, quota, kill switch, and ban command verified".
  - Change-log row.
- LOCAL-DEV: test DB + `go test -p 1`; Google + Safe Browsing env vars, `INSECURE_DEV_COOKIES=1`, local redirect URL.
- Runbook (in README):
  - **Abuse wave:** 1) `SIGNUPS_OPEN=false` → `docker compose up -d shortlink-api`; 2) `user list-recent -days 2`; 3) `user disable` each abuser; 4) check Safe Browsing status; reopen when clean.
  - Single takedown (CLI, then SQL fallback).
  - Claim M1 links (D16).
  - Rotate the Google client secret / Safe Browsing key (new value in Console → `.env` → `up -d shortlink-api` → revoke the old one).
  - Weekly: `user list-recent` + Safe Browsing status for `afh.my.id`.
  - **Lift a ban** (D28): 1) `user enable -email x -actor aziz`. 2) List what the ban took down: `SELECT short_code, long_url, disabled_by, disable_reason, disabled_at FROM links WHERE user_id = <id> AND disabled_at IS NOT NULL ORDER BY disabled_at;`. 3) Check each destination (Safe Browsing Transparency Report) and restore only safe ones: `UPDATE links SET disabled_at = NULL, disabled_by = NULL, disable_reason = NULL WHERE user_id = <id> AND short_code IN ('…');`. **Never restore** a link whose `disable_reason` starts with `safe browsing:`.
  - **Whitelist** (D29): the user signs in once, then `user whitelist -email x` (or `-off`). SQL fallback: `UPDATE users SET is_whitelisted = <bool> WHERE lower(email) = '<email>';`. Effective on the next create; no restart.
  - **Re-scan alert** (D27): `docker logs shortlink-api 2>&1 | grep rescan_takedown`. For a "review owner" alert, inspect that user's links (`user list-recent`, SQL) and `user disable` them if abusive. Run `links rescan` by hand during an abuse wave. If `rescan` WARNs persist for hours, check the Safe Browsing key and quota.

- [ ] **Step 1:** Write it.
- [ ] **Step 2:** Follow LOCAL-DEV.md from a clean shell end to end. Expected: works as written.
- [ ] **Step 3: Commit** `docs: PRD v2.1 (open Google sign-in + abuse controls), M2 local dev and runbook`

### Task 13: Deploy and gate

**Basic auth stays on until the gate passes.** The browser re-sends basic credentials on the same-host OAuth callback, so both coexist during testing.

- [ ] **Step 1:** Over the SSH tunnel: `SELECT count(*) FROM links WHERE user_id IS NOT NULL;` must be 0. Take a `pg_dump`.
- [ ] **Step 2:** Add `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `SAFE_BROWSING_API_KEY` to `/srv/shortlink/.env` (0600), and leave `SIGNUPS_OPEN` unset (open). `git pull`, then `docker compose up -d --build shortlink-api`. Logs show `migrations up to date`; `/healthz` ok; an M1 link still 302s.
- [ ] **Step 3:** `docker compose up -d --build shortlink-web`; `shortlink-api` uptime unchanged.
- [ ] **Step 4:** Sign in with your Google account → claim M1 links (D16). Sign in with a **second Google account** in a private window.
- [ ] **Step 5: Gate, on the live domain:**
  - **Story 4:** the first sign-in creates an account. `SIGNUPS_OPEN=false` → a third Google account gets `signups_closed` with no `users` row, while account 1 still signs in; then set it back to open. `testsafebrowsing.appspot.com/s/phishing.html` → `UNSAFE_URL`. Account 2 (new): 10 links OK, delete them, 11th → `QUOTA_EXCEEDED`.
  - **Story 5:** cookie flags in devtools. Tampered `state` → `failed`, no session row. Sign out → row gone. Expired session → 401, row gone.
  - **Story 6:** `POST /api/links` with no session → 401. `user_id` stamped. Account 2 PATCH/DELETE of an account-1 code → 404.
  - **Story 7:** only own links, newest first; paginated above 50; disable → 410 and the alias isn't re-creatable; delete confirm → 404, the link leaves the list, and re-creating the alias → 409 (soft delete, D31).
  - **Story 8:** `takedown` → 410 on the next request; columns populated; the owner can't undo it; ≤60 s from the SSH prompt. `user disable -email <account 2>` → all of account 2's links 410, its session dead, and its next sign-in → `account_disabled`. Then `user enable -email <account 2> -actor aziz` → it signs in again, its links are still 410, and the runbook SQL restores one → 302.
  - **Click counts (D32):** open a short link a few times, wait a minute, then over the SSH tunnel `SELECT * FROM link_clicks_daily WHERE short_code = '<code>';` shows today's UTC date with that count; a 404 or disabled code adds nothing.
  - **QR codes (D30):** on a new link and on a My links row, open "Show QR", scan the preview with a phone camera (it opens the short link), and both downloads save `afh.my.id-<code>.png`/`.svg`; a taken-down row has no QR button.
  - **Whitelist (D29):** `user whitelist -email <account 2>` → 11+ creates in a day succeed; `testsafebrowsing.appspot.com/s/phishing.html` is still `UNSAFE_URL`. `-off` → the next create past the cap is `QUOTA_EXCEEDED`.
  - **Re-scan (D27):** with a fresh Google account (account 3), create one normal link. Then, over the SSH tunnel, insert a second link for account 3 whose `long_url` is `http://testsafebrowsing.appspot.com/s/phishing.html` (this bypasses the create-time check, simulating a URL listed after creation). Run `docker exec shortlink-api shortlink-api links rescan` → it reports `taken_down=1 banned=1`, both of account 3's links 410, `disabled_by='system:safebrowsing'`, and the alert arrives (if `ALERT_WEBHOOK_URL` is set). Logs show the hourly loop started.
  - **Privacy:** `users` holds only id, sub, email, flags, and timestamps. `docker logs shortlink-api 2>&1 | grep -E 'code=|state=|id_token|key=|<client secret>'` returns nothing.
- [ ] **Step 6:** Only now, in `afh-infra`: remove `basic_auth` from `app.afh.my.id` and `SHORTLINK_BASIC_AUTH_HASH` from `.env.example`, commit `feat: remove M1 basic auth now that M2 enforces sessions`, reload Caddy. Keep `afh.my.id { handle /api/* { respond 404 } }`.
- [ ] **Step 7:** Unauthenticated `POST /api/links` → 401 from the API. Sign-in works without basic auth. External port scan → only 22/80/443. Record the Safe Browsing status for `afh.my.id` as the launch baseline. Put the weekly check in your calendar.
- [ ] **Step 7a: Google brand verification.** In a private window with no basic auth, check that `https://app.afh.my.id` loads, says what Shortlink is, and links to `/privacy`, and that `/privacy` loads. Then Google Auth Platform → **Branding** → submit for verification. Approval takes a few business days; sign-in keeps working meanwhile. Not a gate blocker.
- [ ] **Step 8:** Set this plan's status to "M2 complete"; merge the branch.

---

## 3. Risks specific to M2

| Risk | Likelihood | Mitigation |
|---|---|---|
| **Abuse via open sign-up → `afh.my.id` blocklisted** | **High** — the PRD's critical risk, no longer prevented, only detected and limited | Safe Browsing on create (D23), new-account quota (D24), kill switch (D25), one-command ban (D26), hourly re-scan with auto-takedown/ban (D27), weekly review. Residual risk: a brand-new phishing URL stays live until Google lists it, plus ≤~1 h for the re-scan to catch it. If one incident happens, the response is the abuse-wave runbook. If it recurs, re-introduce a gate (allowlist or invite) — the schema doesn't block that. |
| Safe Browsing outage blocks creation | Low | Fail closed by design; redirects unaffected. |
| Google sign-in outage or a ban on the operator's Google account | Low | Redirects, takedown, and ban don't depend on Google. Sessions last 30 days. |
| Client secret / API key leaked via the public repo | Medium | Only in `.env`; push protection (SEC-03); rotation runbook. The Safe Browsing key is API- and IP-restricted. |
| Migration 000002 fails on the live DB | Low | Step 1 pre-check + `pg_dump`. A failed migration leaves version 1 (check `schema_migrations.dirty`), and the M1 image can be redeployed. |
| Rollback to the M1 image after 000002 applied | — | The M1 binary refuses schema version 2, which takes redirects down. Roll **forward**. If unavoidable, restore the Step 1 dump; accounts made since are lost. |
| Removing basic auth before auth works | — | Task 13 orders it last, behind the full gate. |

## 4. Verification checklist

**Per task (API):** `go test -p 1 ./... && go vet ./... && gofmt -l .` with `TEST_DATABASE_URL` set. Integration tests must run, not skip.
**Per task (web):** `npx tsc --noEmit && npm run build`.
**Gate:** Task 13 Step 5 in full, plus Step 7.
