# Running Shortlink locally

Verified working on macOS (darwin/arm64) on 2026-07-31 with Go 1.26.5,
PostgreSQL 18.4 (Homebrew), Node 24.14.1.

Caddy is not involved locally. Its jobs are TLS and hostname routing, and
locally the Next dev server's `/api/*` rewrite stands in for the same-origin
proxy it provides in production.

```
localhost:3000   web/  (Next dev)     /api/* ──┐
localhost:8080   api/  (Go)    ◄───────────────┘
localhost:5432   postgres, database `shortlink`
```

All paths below are relative to this file, at the repo root.

---

## One-time setup

### 1. Database

Local Postgres gets a published port on 5432. That does not violate SEC-07 —
that rule governs containers on the VPS, not your laptop.

```bash
brew services start postgresql@18

psql -d postgres -c "CREATE ROLE shortlink_api WITH LOGIN NOSUPERUSER PASSWORD 'devpassword';"
psql -d postgres -c "CREATE DATABASE shortlink OWNER shortlink_api;"
```

This mirrors what `../afh-infra/postgres/initdb/001_create_shortlink_db.sql`
does on first boot on the VPS. Local Postgres is 18 while production targets 16
(PRD §7.4); nothing in this schema is version-dependent, but keep the gap in
mind if you ever hit behaviour that differs.

### 2. Dependencies

Both are already resolved in this checkout (`api/go.sum` and
`web/package-lock.json` are committed). Re-run only if they go missing:

```bash
cd api && go mod tidy      # generates go.sum
cd ../web && npm install   # generates package-lock.json
```

---

## Running (two terminals)

### Terminal 1 — API

```bash
cd api
export DATABASE_URL='postgres://shortlink_api:devpassword@localhost:5432/shortlink?sslmode=disable'
export PUBLIC_HOSTNAMES='afh.my.id,app.afh.my.id'
go run ./cmd/shortlink-api
```

Expect:

```json
{"level":"INFO","msg":"migrations up to date"}
{"level":"INFO","msg":"listening","port":"8080"}
```

Migrations run in-process at startup, so there is no separate migrate step.
`DATABASE_URL` is the only required variable — the process exits immediately
without it.

### Terminal 2 — Web

```bash
cd web
npm run dev
```

Open `http://localhost:3000`.

---

## Where to test what

| What | URL |
|---|---|
| Creation UI | `localhost:3000` |
| Create API (via the dev proxy) | `localhost:3000/api/links` |
| Create API (direct) | `localhost:8080/api/links` |
| **Redirects** | `localhost:8080/{code}` |
| Health | `localhost:8080/healthz` |

**Redirects are only on :8080.** The web app proxies `/api/*` and nothing
else, so `localhost:3000/{code}` will 404 from Next, not from the API.

---

## Two things that will surprise you

**You cannot shorten a localhost URL.** `urlvalidate` rejects loopback and
private addresses (FR-03), so `http://localhost:3000/foo` returns 400. This is
correct. Use real public URLs when testing the happy path.

**Created links display as `https://afh.my.id/{code}`.** `shortURL()` builds
from the first entry of `PUBLIC_HOSTNAMES`, so the returned link is not
clickable locally — copy the `code` and hit `localhost:8080/{code}` instead.
Setting `PUBLIC_HOSTNAMES='localhost:8080,...'` makes links clickable but then
own-domain loop prevention targets localhost, so you can no longer verify that
acceptance criterion.

---

## Verifying the M1 acceptance criteria

All of the following were run against the live local stack and pass.

```bash
B=http://localhost:8080

# Story 1 — create
curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/a/long/path?with=query"}'      # 7-char base62 code
curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
  -d '{"url":"example.com/page"}'                                # https:// assumed

# Story 1 — rejections
for u in 'javascript:alert(1)' 'data:text/html,<x>' 'file:///etc/passwd' \
         'https://afh.my.id/abc' 'http://127.0.0.1:3000' \
         'http://169.254.169.254/latest/meta-data'; do
  curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
    -d "{\"url\":\"$u\"}"; echo
done

# Story 2 — redirect semantics
curl -i $B/<code>          # 302 + Location + Referrer-Policy: no-referrer
curl -i $B/nosuch          # 404, plain branded page
curl -i $B/<CODE-CASED>    # 404 — codes are case-sensitive

# Story 2 — 410 (no takedown UI until M2, so set the column by hand)
psql -d shortlink -c "UPDATE links SET disabled_at = now() WHERE short_code = '<code>';"
curl -i $B/<code>          # 410 Gone
psql -d shortlink -c "UPDATE links SET disabled_at = NULL WHERE short_code = '<code>';"

# Story 3 — aliases
curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/resume","custom_alias":"resume"}'   # 201, verbatim
curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/other","custom_alias":"resume"}'    # 409, no substitute
curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","custom_alias":"api"}'             # 400, reserved
curl -s -X POST $B/api/links -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","custom_alias":"my link"}'         # 400, names the violation
```

Tests and build:

```bash
cd api  && go test ./... && go vet ./... && gofmt -l .
cd ../web && npx tsc --noEmit && npm run build && npm audit
```

---

## Resetting

```bash
# wipe data, keep the database
psql -d shortlink -c "TRUNCATE links;"

# or start completely fresh (migrations re-run at next API startup)
psql -d postgres -c "DROP DATABASE shortlink;"
psql -d postgres -c "CREATE DATABASE shortlink OWNER shortlink_api;"

# stop postgres
brew services stop postgresql@18
```

---

## Docker path (alternative)

Docker 29.7.0 is installed but its daemon was not running, so this route is
untested here. Note that `../afh-infra/docker-compose.yml` is
production-shaped: Postgres sits on an `internal` network with no published
port, so an API run from your host cannot reach it. This repo's own
`docker-compose.yml` is production-shaped too — it declares `edge` and `data`
as `external: true` and creates neither (PLT-01), so `docker compose up` here
fails outright until `afh-infra` has been applied. Running the full stack in
Docker locally would need a dev override that creates the networks, publishes
5432, and skips Caddy's real-domain TLS. The native path above is simpler and
is the one that has been verified.
