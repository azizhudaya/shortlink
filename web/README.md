# web — Next.js UI

The frontend component of the `shortlink` repo: the link-creation UI served at
`app.afh.my.id`.

Repo-level context — the platform contract, hostname routing, deploy, and the
repository model — is in [`../README.md`](../README.md). The canonical product
spec is [`../docs/PRD-shortlink-v2.md`](../docs/PRD-shortlink-v2.md).

## How it talks to the API

It doesn't, directly. Caddy serves this app at `app.afh.my.id` and proxies
`app.afh.my.id/api/*` to the `shortlink-api` container (PRD §7.3), so the
browser's `fetch('/api/links')` is **same-origin**. There is no API base URL to
configure, no CORS preflight, no origin allowlist, and — once M2 adds session
cookies — no cross-origin cookie handling.

Consequence: this container has no `DATABASE_URL` and no network path to
Postgres. It joins only the `edge` network. Sharing a repository with `api/`
does not change that — the boundary is the network, not the repo.

## Scope (M1)

A single page: paste a URL, optionally choose an alias, submit, get a short URL
back with one-action copy-to-clipboard (Stories 1 and 3). Link listing,
disable, and delete arrive with M2 once accounts exist.

Targets WCAG 2.1 AA (NFR-13) and viewports from 320 px (NFR-14).

## Local development

```bash
npm install
npm run dev
```

Serves on `localhost:3000`. The `rewrites` in `next.config.js` proxy `/api/*`
to `localhost:8080` in development, standing in for what Caddy does in
production — so the fetch URLs are identical in both. Run the API alongside:

```bash
cd ../api && go run ./cmd/shortlink-api
```

Full setup is in [`../LOCAL-DEV.md`](../LOCAL-DEV.md).

## Checks

```bash
npx tsc --noEmit && npm run build && npm audit
```

## Deploy

Built from `../docker-compose.yml` with `context: ./web`, and deployed on its
own — a frontend release does not recreate the API container, so redirects are
never interrupted by a UI change:

```bash
cd /srv/shortlink && docker compose up -d --build shortlink-web
```

Requires `afh-infra` applied first (PLT-01) — `edge` is declared
`external: true` and created there.

Note (CD-01): a Next.js build peaks at 1–1.5 GB, which would trigger the OOM
killer against Postgres on the 2 GB host. Building on the VPS with `--build`
is acceptable only as the M1 manual-deploy stopgap and only with the API and
Postgres memory limits in place; M3 moves the build to GitHub-hosted runners,
which is the real fix.
