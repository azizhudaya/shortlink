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

## Endpoints (M2)

Redirects (`GET /{code}`) and `/healthz` are public. Everything under `/api/*`
other than the Google sign-in start/callback needs a session, and every
mutating call also needs the `X-CSRF-Token` from `GET /api/auth/me`. The full
table is in [`api/README.md`](api/README.md).

Sign-in is **Google only** and registration is **open**, so the abuse controls
in `docs/PRD-shortlink-v2.md` §13.2 carry the domain's reputation: Safe Browsing
on every create (fails closed), an hourly Safe Browsing re-scan with automatic
takedown, a new-account daily quota, a sign-up kill switch, and one-command
bans. M1's Caddy basic auth on `app.afh.my.id` stays on until the M2 gate passes
on the live domain (`docs/M2-IMPLEMENTATION-PLAN.md`, Task 13).

## Operator runbook

All commands run on the VPS in `/srv/shortlink`. `SL` below is
`docker exec shortlink-api shortlink-api`.

**Abuse wave**
1. Close sign-ups: set `SIGNUPS_OPEN=false` in `.env`, then `docker compose up -d shortlink-api` (~5 s). Existing accounts keep working.
2. Find the accounts: `$SL user list-recent -days 2`.
3. Ban each abuser: `$SL user disable -email x -reason "phishing wave" -actor aziz` (sessions ended, every link taken down).
4. Sweep now rather than waiting for the hour: `$SL links rescan`.
5. Check the Safe Browsing status of `afh.my.id` (Transparency Report / Search Console). Remove `SIGNUPS_OPEN` and recreate when clean.

**Single takedown**: `$SL takedown -code X -reason "…" -actor aziz`. If the
app is down, over the SSH tunnel:
`UPDATE links SET disabled_at = now(), disabled_by = 'operator:aziz', disable_reason = '…' WHERE short_code = 'X';`

**Lift a ban** (D28)
1. `$SL user enable -email x -actor aziz`. The user signs in again; their links stay down.
2. List what the ban took down: `SELECT short_code, long_url, disabled_by, disable_reason, disabled_at FROM links WHERE user_id = <id> AND disabled_at IS NOT NULL ORDER BY disabled_at;`
3. Check each destination and restore only the safe ones: `UPDATE links SET disabled_at = NULL, disabled_by = NULL, disable_reason = NULL WHERE user_id = <id> AND short_code IN ('…');`
   **Never restore** a link whose `disable_reason` starts with `safe browsing:`.

**Whitelist** (D29): the user signs in once, then `$SL user whitelist -email x`
(`-off` to remove). SQL fallback:
`UPDATE users SET is_whitelisted = <bool> WHERE lower(email) = '<email>';`.
Takes effect on the next create; no restart.

**Re-scan alerts** (D27): `docker logs shortlink-api 2>&1 | grep rescan_takedown`.
A "review owner" alert means one link of an established account was taken
down: look at that user's links and `user disable` them if abusive. Set
`ALERT_WEBHOOK_URL` in `.env` (e.g. an ntfy.sh topic) to get pushes. Persistent
`rescan stopped: safe browsing unavailable` warnings mean the key or its quota
needs attention.

**Restore a deleted link** (D31): owner deletes are soft, so the row is still there:
`UPDATE links SET deleted_at = NULL WHERE short_code = 'X';`

**Claim the M1 links** (D16, once): after the operator's first sign-in,
`UPDATE links SET user_id = <your id> WHERE user_id IS NULL;`

**Rotate a secret** (Google client secret or Safe Browsing key): create the new
value in Google Cloud Console, put it in `.env`, `docker compose up -d
shortlink-api`, then revoke the old one.

**Weekly** (mandatory since registration opened): `$SL user list-recent` and
the Safe Browsing status of `afh.my.id`.

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
set -a; source ~/.config/shortlink/dev.env; set +a    # Google + Safe Browsing dev credentials
cd api && INSECURE_DEV_COOKIES=1 GOOGLE_REDIRECT_URL=http://localhost:3000/api/auth/google/callback \
  go run ./cmd/shortlink-api                           # :8080, migrations run at startup
cd web && npm run dev                                  # :3000, proxies /api/* to :8080
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
