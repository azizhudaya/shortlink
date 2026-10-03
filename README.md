# shortlink

Public application repository for the `afh.my.id` URL shortener: the Go API
that serves redirects and the Next.js UI that creates links. Both ship
together, so both live here.

```
api/     Go service — redirects (GET /{code}), creation API (/api/*), migrations
web/     Next.js creation UI served at app.afh.my.id
docs/    canonical PRD and the M1 implementation plan
docker-compose.yml   both containers; networks come from afh-infra
LOCAL-DEV.md         running the whole stack on a laptop
```

## Related repositories

Two repositories, split on deployment lifecycle rather than language:

| Repo | Owns | Visibility | Change frequency |
|---|---|---|---|
| `shortlink` | this repo — Go API, Next.js UI, migrations, canonical PRD | public | per feature |
| `afh-infra` | Caddy, Postgres, Docker networks, host provisioning, backups | private | rare — months |

`docs/PRD-shortlink-v2.md` §7.2 carries the full rationale. In short: the API
and the UI are versioned by the same feature work and a change spanning both
should be one commit, while infrastructure has inverted risk and cadence — it
changes monthly at most and a bad change takes down every co-tenant on the
host. The two also require opposite repository visibility, which a single
repository cannot provide: public here buys the unlimited CI minutes and
unmetered image storage CD-06 depends on, while `afh-infra` documents host
topology, firewall assumptions, and backup destinations and stays private.

`afh-infra/docs/NETWORKS.md` is the registry for the platform contract
(network names, container names, deploy directories) and is the single source
of truth for it.

## Two components, still two independent deploys

Sharing a repository does not couple the releases. Compose takes a service
argument, so each container is still deployed on its own:

```bash
cd /srv/shortlink
docker compose up -d --build shortlink-api    # API only — web untouched
docker compose up -d --build shortlink-web    # web only — redirects never interrupted
```

That keeps the availability property worth having (a frontend release cannot
interrupt the redirect path) without paying for it in cross-repo coordination.
From M3, CI applies path filters on `api/**` and `web/**` so a change to one
does not rebuild or redeploy the other.

## Endpoints (M1)

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/{code}` | 302 to destination · 404 unknown · 410 disabled (FR-07) |
| `GET` | `/healthz` | 200 with process + database reachability (FR-17) |
| `POST` | `/api/links` | 201 created · 400 invalid · 409 alias taken · 415 not JSON |

Creation has no accounts in M1; accounts arrive with M2 (Stories 4–6). Until
then, `app.afh.my.id` (UI and `/api/*`) sits behind **Caddy basic auth**
configured in `afh-infra`, and `afh.my.id/api/*` returns 404 so the create
endpoint cannot be reached around it. `POST /api/links` also requires
`Content-Type: application/json` (415 otherwise), which stops other sites from
using the operator's cached credentials. Remove the basic auth when M2 lands.
See "Known gaps" in `docs/M1-IMPLEMENTATION-PLAN.md`.

## Hostname routing

| Hostname | Container | Purpose |
|---|---|---|
| `afh.my.id` | `shortlink-api` | Public redirects, `GET /{code}` |
| `app.afh.my.id` | `shortlink-web`, with `/api/*` → `shortlink-api` | Creation UI |

Caddy proxying `/api/*` on the app hostname to the API container makes every
browser request same-origin, so CORS never enters the system.

## Local development

See `LOCAL-DEV.md`. Short version:

```bash
cd api && go run ./cmd/shortlink-api      # :8080, migrations run at startup
cd web && npm run dev                     # :3000, proxies /api/* to :8080
```

## Deploy

`afh-infra` must be applied first (PLT-01): the `edge` and `data` networks are
declared `external: true` here and created there. Automated CI/CD is
Milestone 3 scope and not yet present.

## Platform contract

This repo consumes interfaces it does not own. Violating them fails at
runtime, not at build time:

| Rule | Meaning here |
|---|---|
| PLT-01 | `edge` and `data` are declared `external: true`, never created |
| PLT-02 | `container_name` values are globally unique and registered in `afh-infra` before first use |
| PLT-03 | This repo never publishes a host port, defines a network, or defines a database service |
