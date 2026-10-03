# api — Go service

The backend component of the `shortlink` repo: serves public redirects
(`GET /{code}` on `afh.my.id`) and the creation/management API (`/api/*` on
`app.afh.my.id`). Owns the database migrations.

Repo-level context — the platform contract, hostname routing, deploy, and the
repository model — is in [`../README.md`](../README.md). The canonical product
spec is [`../docs/PRD-shortlink-v2.md`](../docs/PRD-shortlink-v2.md).

Module path: `github.com/azizhudaya/shortlink/api`.

## Layout

```
cmd/shortlink-api/    entrypoint: config -> migrate -> serve
internal/config/      env-driven configuration
internal/httpserver/  router, handlers, middleware
internal/shortcode/   CSPRNG base62 code generation (FR-01)
internal/alias/       custom-alias validation + reserved-prefix list (FR-02, FR-04)
internal/urlvalidate/ destination validation: scheme, own-domain, private IPs (FR-03)
internal/store/       Postgres persistence, collision handling (FR-05, FR-06)
internal/model/       Link struct mirroring the links table
internal/migrations/  golang-migrate SQL, embedded and run at startup
Dockerfile            static binary in a distroless nonroot image
```

The binary and container are both named `shortlink-api` — that name is
registered in `afh-infra/docs/NETWORKS.md` (PLT-02) and Caddy routes to it, so
it is not free to change.

## Endpoints (M1)

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/{code}` | 302 to destination · 404 unknown · 410 disabled (FR-07) |
| `GET` | `/healthz` | 200 with process + database reachability (FR-17) |
| `POST` | `/api/links` | 201 created · 400 invalid · 409 alias taken |

Creation is **unauthenticated in M1** — authentication arrives with M2
(Stories 4–6). See "Known gaps" in
[`../docs/M1-IMPLEMENTATION-PLAN.md`](../docs/M1-IMPLEMENTATION-PLAN.md)
before exposing this publicly.

## Local development

```bash
export DATABASE_URL='postgres://shortlink_api:...@localhost:5432/shortlink?sslmode=disable'
export PUBLIC_HOSTNAMES='afh.my.id,app.afh.my.id'
go run ./cmd/shortlink-api
```

Migrations run automatically at startup (§7.4) — there is no separate migrate
step to forget. Full setup, including the database, is in
[`../LOCAL-DEV.md`](../LOCAL-DEV.md).

## Checks

```bash
go test ./... && go vet ./... && gofmt -l .
```

## Deploy

Built from `../docker-compose.yml` with `context: ./api`, and deployed on its
own without touching the web container:

```bash
cd /srv/shortlink && docker compose up -d --build shortlink-api
```

Requires `afh-infra` applied first (PLT-01): the `edge` and `data` networks are
declared `external: true` and created there. Automated CI/CD is Milestone 3
scope and not yet present.
