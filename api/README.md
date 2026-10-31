# api — Go service

The backend component of the `shortlink` repo: serves public redirects
(`GET /{code}` on `afhs.my.id`) and the creation/management API (`/api/*` on
`app.afhs.my.id`). Owns the database migrations.

Repo-level context — the platform contract, hostname routing, deploy, and the
repository model — is in [`../README.md`](../README.md). The canonical product
spec is [`../docs/PRD-shortlink-v2.md`](../docs/PRD-shortlink-v2.md).

Module path: `github.com/azizhudaya/shortlink/api`.

## Layout

```
cmd/shortlink-api/    entrypoint: serve (config -> migrate -> serve) and operator subcommands
internal/config/      env-driven configuration
internal/httpserver/  router, handlers, middleware, sessions, CSRF
internal/auth/        session/CSRF tokens, OAuth flow state, Google OIDC client
internal/oidctest/    fake OIDC provider for tests
internal/safety/      Google Safe Browsing Lookup API v4 client (FR-18)
internal/rescan/      hourly Safe Browsing re-scan with auto-takedown (FR-22)
internal/alert/       optional webhook alerts for re-scan takedowns
internal/ratelimit/   keyed token-bucket limiter (FR-16)
internal/testdb/      migrated, truncated database for integration tests
internal/shortcode/   CSPRNG base62 code generation (FR-01)
internal/alias/       custom-alias validation + reserved-prefix list (FR-02, FR-04)
internal/urlvalidate/ destination validation: scheme, own-domain, private IPs (FR-03)
internal/store/       Postgres persistence, collision handling (FR-05, FR-06)
internal/model/       Link, User, Session
internal/migrations/  golang-migrate SQL, embedded and run at startup
Dockerfile            static binary in a distroless nonroot image
```

The binary and container are both named `shortlink-api` — that name is
registered in `afh-infra/docs/NETWORKS.md` (PLT-02) and Caddy routes to it, so
it is not free to change.

## Endpoints (M2)

| Method | Path | Auth | Behaviour |
|---|---|---|---|
| `GET` | `/{code}` | — | 302 · 404 unknown · 410 disabled (FR-07). No cookie, no session lookup. |
| `GET` | `/` | — | 302 to `APP_URL` (default `https://app.afhs.my.id`), FR-28 |
| `GET` | `/healthz` | — | 200 with process + database reachability (FR-17) |
| `GET` | `/api/auth/google/start` | — | 302 to Google + oauth cookie |
| `GET` | `/api/auth/google/callback` | oauth cookie | 302 `/links` + session cookie, or `/signin?error=…` |
| `POST` | `/api/auth/logout` | session + CSRF | 204 |
| `GET` | `/api/auth/me` | session | `{email, csrf_token}` |
| `POST` | `/api/links` | session + CSRF | 201 · 400 · 409 · 429 · 503 |
| `GET` | `/api/links?cursor=&limit=` | session | own links, newest first |
| `PATCH` | `/api/links/{code}` | session + CSRF | `{"disabled": bool}` → 204 · 404 · 409 |
| `DELETE` | `/api/links/{code}` | session + CSRF | 204 · 404 · 409 |

Full decisions (D3–D29) are in
[`../docs/M2-IMPLEMENTATION-PLAN.md`](../docs/M2-IMPLEMENTATION-PLAN.md).

## Operator subcommands

Run on the VPS with `docker exec shortlink-api shortlink-api …`. They need only
`DATABASE_URL`, never migrate, and refuse a schema older than version 2.

```
shortlink-api takedown -code CODE -reason TEXT -actor NAME
shortlink-api user disable -email EMAIL -reason TEXT -actor NAME   # ban: sessions ended, all links taken down
shortlink-api user enable -email EMAIL -actor NAME                 # lift a ban; links stay down
shortlink-api user whitelist -email EMAIL [-off]                   # no daily quota
shortlink-api user list-recent [-days 7]
shortlink-api links rescan                                         # one Safe Browsing re-scan pass now
```

## Local development

```bash
set -a; source ~/.config/shortlink/dev.env; set +a   # GOOGLE_* + SAFE_BROWSING_API_KEY
export DATABASE_URL='postgres://shortlink_api:...@localhost:5432/shortlink?sslmode=disable'
export PUBLIC_HOSTNAMES='afhs.my.id,app.afhs.my.id'
export INSECURE_DEV_COOKIES=1 GOOGLE_REDIRECT_URL=http://localhost:3000/api/auth/google/callback
go run ./cmd/shortlink-api
```

Migrations run automatically at startup (§7.4) — there is no separate migrate
step to forget. Full setup, including the database, is in
[`../LOCAL-DEV.md`](../LOCAL-DEV.md).

## Checks

```bash
export TEST_DATABASE_URL='postgres://shortlink_api:devpassword@localhost:5432/shortlink_test?sslmode=disable'
go test -p 1 ./... && go vet ./... && gofmt -l .
```

Integration tests share one database, hence `-p 1`. Without
`TEST_DATABASE_URL` they skip and the rest still run.

## Deploy

Built from `../docker-compose.yml` with `context: ./api`, and deployed on its
own without touching the web container:

```bash
cd /srv/shortlink && docker compose up -d --build shortlink-api
```

Requires `afh-infra` applied first (PLT-01): the `edge` and `data` networks are
declared `external: true` and created there. Automated CI/CD is Milestone 3
scope and not yet present.
