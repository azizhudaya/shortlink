# Implementation Plan — Milestones 0 and 1

| | |
|---|---|
| **Product** | Shortlink (`afh.my.id`) |
| **Source PRD** | `PRD-shortlink-v2.md` v2.0 (2026-07-31) |
| **Scope of this document** | M0 (platform) and M1 (redirect core) only |
| **Date** | 2026-07-31 |
| **Status** | Scaffolded, not yet deployed |

---

## 1. What this document covers

The PRD defines four milestones. This plan covers the first two:

| Milestone | Deliverable | Gate |
|---|---|---|
| **M0 — Platform** | Caddy, Postgres, Docker networks, firewall, SSH hardening | External port scan shows only 22/80/443; TLS issued for both hostnames |
| **M1 — Redirect core** | Create + redirect, custom aliases, creation UI | Stories 1–3 acceptance criteria pass **on the live domain** |

M0 is included because M1's gate cannot be met without it: "on the live
domain" requires a host, TLS, routing, and a database, all of which M0 owns.

**M2** (accounts, ownership, link management) and **M3** (CI/CD pipeline,
encrypted backups, monitoring, the full hardening gate) are out of scope
here. Where an M1 decision had to be made with M2/M3 in mind, §8 records it
so nothing built now needs tearing up later.

---

## 2. Repository model

### 2.1 Two repositories

The service is built as **two independent repositories**, each with its own git
history and its own deploy lifecycle, exactly as PRD §7.2 specifies:

| Repo | Owns | Visibility | Change frequency |
|---|---|---|---|
| `afh-infra` | Caddy, Postgres, Docker networks, host provisioning, backup jobs | **private** | rare — months |
| `shortlink` | Go API (`api/`), Next.js UI (`web/`), migrations, canonical PRD | public | per feature |

`portfolio`, the unrelated co-tenant named in the PRD, is not part of this
work.

### 2.2 Why the API and the UI share a repository

The boundary between repositories is **deployment lifecycle and blast radius,
not programming language**. `api/` and `web/` are driven by the same feature
work and released together, so they share a repository. Caddy and Postgres are
not, so they do not.

Three things decide it, in order of weight:

**Visibility is a one-way door and no single setting satisfies both.** Public
application code buys unlimited CI runner minutes and unmetered image storage,
which CD-06 depends on. `afh-infra` documents host topology, firewall
assumptions, SSH configuration, and backup destinations — none of it secret in
the credential sense, none of it something to publish as a free reconnaissance
document either. One repository forces one setting: public-everything gives
away the recon document, private-everything forfeits the CI economics. There is
no configuration that gets both.

**Risk and cadence are inverted.** Application changes are frequent, cheap, and
reversible in two minutes (NFR-09). Infrastructure changes are rare, dangerous,
and can take down every co-tenant at once. Merging them means either every
application push carries the ability to touch Caddy and Postgres configuration,
or the repository accumulates path filters to prevent exactly that —
reconstructing the split without its benefits.

**`afh-infra` serves more than one product.** It is platform-level and hosts
`portfolio` too. Making the `shortlink` repository the owner of that platform
would be the wrong ownership, and would require anyone working on either
product to hold access to the whole host.

### 2.3 What sharing a repository does not cost

An earlier revision of this plan split `api/` and `web/` into two repositories
to keep their deploys independent. **That property is preserved without the
split**, which is why the split was dropped:

- **Independent deploys.** One compose file, but Compose takes a service
  argument: `docker compose up -d --build shortlink-api` recreates only the API
  container. A frontend release still cannot interrupt the redirect path.
- **Independent images.** Two Dockerfiles, two build contexts (`./api`,
  `./web`), two images. The PRD's convention
  `ghcr.io/<owner>/<repo>-<component>` now resolves literally:
  `ghcr.io/azizhudaya/shortlink-api` and `.../shortlink-web`.
- **Independent pipelines at M3.** One workflow file with path filters on
  `api/**` and `web/**`, rather than two repositories' worth of duplicated
  workflow, secret, and environment configuration.

What the merge removes is a real cost: a change spanning both — a new API field
the UI consumes — was two commits in two repositories with nothing verifying
they agreed. It is now one atomic commit, and CI sees both halves together.

The platform contract is unaffected either way. Container names, network
memberships, and Caddy routes are identical; only the deploy directory changes,
from two to the single `/srv/shortlink/` the PRD §7.1 layout already named.

### 2.4 The platform contract

`afh-infra` defines these interfaces; the `shortlink` repo consumes them, for
both of its containers. The registry lives at `afh-infra/docs/NETWORKS.md` and
is the single source of truth.

| Contract item | Symptom if violated |
|---|---|
| Networks `edge` and `data` exist before any app deploys (PLT-01) | `docker compose up` fails immediately — loud and harmless |
| Container names globally unique (PLT-02) | Caddy resolves to an arbitrary container: silent, intermittent, worst failure in this list |
| The app repo never publishes a host port or creates a network (PLT-03) | Silent firewall bypass — Docker's iptables rules precede UFW's |
| Postgres role + database provisioned per tenant | API fails at startup on migration |

Only `shortlink-api` joins both networks. `shortlink-web` joins `edge` only and
has no network path to Postgres at all — sharing a repository does not change
that, because the boundary is the network, not the repo.

**PLT-05 is the trap worth naming twice:** `afh-infra/postgres/initdb/` runs
*only* on first volume creation. On a fresh VPS it will run and create the
`shortlink` database. For any tenant added later, the SQL file sits in the
repo, looks applied, and never executes — provision those manually.

### 2.5 Hostname routing

| Hostname | Container | Purpose |
|---|---|---|
| `afh.my.id` | `shortlink-api` | Public redirects, `GET /{code}` |
| `app.afh.my.id` | `shortlink-web`, with `/api/*` → `shortlink-api` | Creation UI |

Proxying `/api/*` on the app hostname to the API container makes every browser
request **same-origin**. CORS never enters the system: no preflight, no origin
allowlist, and — once M2 adds session cookies — no cross-origin cookie
handling. This routing is configured at M0 even though M1 has no auth, because
retrofitting it later would mean the UI's fetch URLs change.

---

## 3. Current state of the scaffold

Both repos are scaffolded and `git init`-ed with **no commits** — review before
committing. Dependencies have been resolved, so `api/go.sum` and
`web/package-lock.json` exist; `go build`, `go vet`, `go test`, `tsc
--noEmit`, and `next build` all pass against this tree.

```
afh-infra/                                   private, infrastructure
├── docker-compose.yml                       Caddy + Postgres; CREATES edge/data
├── Caddyfile                                TLS + routing for both hostnames
├── .env.example                             placeholders only, no secrets
├── postgres/initdb/001_create_shortlink_db.sql   first-boot-only (PLT-05)
├── firewall/ufw-rules.sh                    default-deny, 22/80/443 (SEC-06)
├── ssh/sshd_config.hardened                 key-only, no root (SEC-09)
├── scripts/bootstrap-host.sh                Docker, fail2ban, unattended-upgrades
├── scripts/scan-ports.sh                    external scan — the M0 gate (SEC-07)
└── docs/NETWORKS.md                         container/network registry

shortlink/                                   public, both application containers
├── docker-compose.yml                       BOTH containers; networks external
├── .env.example                             placeholders only, no secrets
├── .editorconfig                            shared across Go and TypeScript
├── .gitignore                               one file, repo-wide (secret hygiene in one place)
├── README.md                                repo-level: contract, routing, deploy
├── LOCAL-DEV.md                             running the whole stack on a laptop
├── docs/
│   ├── PRD-shortlink-v2.md                  canonical product spec
│   └── M1-IMPLEMENTATION-PLAN.md            this document
│
├── api/                                     Go backend → ghcr.io/…/shortlink-api
│   ├── Dockerfile                           static binary → distroless nonroot
│   ├── go.mod                               module github.com/azizhudaya/shortlink/api
│   ├── cmd/shortlink-api/main.go            config → migrate → serve
│   └── internal/
│       ├── config/config.go                 env-driven config
│       ├── httpserver/router.go             ServeMux, 3 routes, middleware chain
│       ├── httpserver/handlers_create.go    POST /api/links (Stories 1, 3)
│       ├── httpserver/handlers_redirect.go  GET /{code} (Story 2) + 404/410 pages
│       ├── httpserver/handlers_health.go    GET /healthz (FR-17)
│       ├── httpserver/middleware.go         headers, logging, recover, body cap
│       ├── httpserver/errors.go             the single JSON error shape
│       ├── shortcode/shortcode.go           CSPRNG base62 (FR-01)  [+ test]
│       ├── alias/validate.go                format rules (FR-02)   [+ test]
│       ├── alias/reserved.go                reserved list (FR-04)
│       ├── urlvalidate/validate.go          scheme/own-domain/private IP (FR-03) [+ test]
│       ├── store/store.go                   Store interface + sentinel errors
│       ├── store/postgres.go                pgx impl, FR-05 and FR-06 collision paths
│       ├── model/link.go                    Link struct
│       └── migrations/                      embedded SQL + embed.go
│
└── web/                                     Next.js frontend → ghcr.io/…/shortlink-web
    ├── Dockerfile                           deps → build → standalone runner
    ├── next.config.js                       output: 'standalone', dev-only /api rewrite
    ├── package.json                         Next 15, React 19; sharp/postcss pinned forward
    └── app/{layout.tsx,page.tsx,globals.css}   the single creation form
```

Two layout notes:

**Migrations live at `api/internal/migrations/`** rather than the component root
because `go:embed` cannot reference parent directories — the directive must sit
in a package at or above the files it embeds.

**The Go module root is `api/`, not the repo root.** The module path is
`github.com/azizhudaya/shortlink/api`, which keeps `go build ./...` scoped to
the Go component and leaves the Node toolchain to `web/`. Neither component's
tooling has to ignore the other's tree.

---

## 4. Data model (M1 subset)

M1 needs one table. `users`, `sessions`, and `invites` arrive with M2.

```sql
CREATE TABLE IF NOT EXISTS links (
    short_code     VARCHAR(32)  PRIMARY KEY,
    long_url       TEXT         NOT NULL,
    is_custom      BOOLEAN      NOT NULL DEFAULT FALSE,
    user_id        BIGINT       NULL,   -- no FK yet: users doesn't exist until M2
    disabled_at    TIMESTAMPTZ  NULL,
    disabled_by    BIGINT       NULL,
    disable_reason TEXT         NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_links_user_id ON links (user_id) WHERE user_id IS NOT NULL;
```

Three decisions worth stating:

**`short_code` is the primary key.** The redirect is the hottest path and it
looks up by code, so the lookup is a single primary-key probe with no
surrogate-key indirection. Nothing in the product references a link by numeric
id, so a `bigserial` would add an index for no benefit.

**`user_id` is nullable and carries no foreign key.** Nullable because M1
links are created before accounts exist; no FK because `users` does not exist
to reference. The M2 migration that creates `users` also adds the FK on
`user_id` and `disabled_by`. Because the column is already nullable, existing
M1 rows satisfy the constraint unchanged — **no backfill, no breaking
change.**

**`disabled_at` / `disabled_by` / `disable_reason` exist but are never
written in M1.** Takedown is Story 8 (M2/M3). Creating the columns now means
that work needs no schema change, and it lets the M1 window use a manual SQL
takedown as a kill switch (§9). The redirect handler already returns 410 when
`disabled_at` is non-null, so the path is live even though nothing sets it.

---

## 5. API surface (M1)

| Method | Path | Hostname | Responses |
|---|---|---|---|
| `GET` | `/{code}` | `afh.my.id` | 302 + `Location` · 404 unknown · 410 disabled |
| `GET` | `/healthz` | both | 200 `{"status":"ok"}` · 503 `{"status":"degraded"}` |
| `POST` | `/api/links` | `app.afh.my.id` | 201 · 400 invalid · 409 alias taken |

One `ServeMux` serves both hostnames; Caddy decides what reaches it.

Errors use one shape so the frontend branches on a stable code, never on
message text:

```json
{"error": {"code": "ALIAS_TAKEN", "message": "..."}}
```

### Redirect semantics

**302, not 301.** Browsers cache 301 aggressively and often indefinitely. A
cached 301 means the browser stops contacting the server, which would make
takedown unenforceable for exactly the population that matters during an abuse
incident — people who have already followed the link. 302 costs one indexed
read per click and keeps both takedown and future click-counting correct.

**Codes are case-sensitive.** `/Abc123` and `/abc123` are distinct links,
which falls out of the SQL comparison for free.

**Route literals and the reserved list must agree.** Go 1.22's `ServeMux`
prefers a literal pattern (`GET /healthz`) over an overlapping wildcard
(`GET /{code}`), so a link whose code is `healthz` would be permanently
unreachable. `internal/alias/reserved.go` is the single source of truth, it
already contains every literal the router registers plus the paths M2 will
add (`login`, `logout`, `register`, `auth`, ...), and
`alias/validate_test.go` asserts the router's literals are all reserved.

---

## 6. Task sequence

### 6.1 M0 — Platform

Order matters; each step gates the next.

1. Provision the VPS. Create the deploy user; install the SSH public key.
2. Apply `ssh/sshd_config.hardened` — key-only, no root login, no
   password/keyboard-interactive auth (SEC-09). **Confirm you can still log in
   from a second terminal before closing the first.**
3. Run `scripts/bootstrap-host.sh`: Docker + Compose plugin, `fail2ban`,
   `unattended-upgrades` (SEC-10), then `firewall/ufw-rules.sh` (SEC-06).
4. Clone `afh-infra` to `/srv/infra/`. Copy `.env.example` → `.env`, fill in
   real values, `chmod 0600` (SEC-05).
5. `docker compose up -d`. This creates the `edge` and `data` networks — the
   step every application deploy depends on (PLT-01).
6. Verify `initdb` ran: the `shortlink` database and `shortlink_api` role
   exist. Then set the role's password once, over an SSH tunnel:
   ```
   ssh -L 5432:postgres:5432 deploy@vps
   psql -h localhost -U postgres -c "ALTER ROLE shortlink_api WITH PASSWORD '<generated>';"
   ```
   Put that value in `/srv/shortlink/.env`, mode 0600.
7. Point DNS: A records for `afh.my.id` and `app.afh.my.id` at the VPS.
8. Confirm Caddy obtained certificates for both hostnames (ACME needs port 80
   reachable — see the runbook entry for certificate failures).
9. **Gate:** run `scripts/scan-ports.sh <host>` from a machine *outside* the
   VPS. Only 22/80/443 may be open. Reading the compose file is not
   sufficient — Docker writes iptables rules ahead of UFW's, so a published
   port bypasses the firewall silently.

### 6.2 M1 — Backend (`api/`)

Depends on M0 step 6 for a reachable database. Steps 1–2 can be done locally
against any Postgres 16. Run from `api/`.

1. `go mod tidy` — already run; `go.sum` exists and the tree builds.
2. Fill in the implementation behind the scaffolded skeletons. All the
   load-bearing logic is already sketched; what remains is completing and
   testing it:
   - `shortcode.Generate` — 7-char base62 from `crypto/rand` (FR-01)
   - `alias.Validate` — length, format, reserved list, each returning a
     distinct error so the 400 names the specific violation (FR-02, FR-04)
   - `urlvalidate.Validate` — scheme allowlist, own-hostname rejection,
     DNS resolution + private/loopback/link-local/CGNAT rejection (FR-03)
   - `store.Postgres.CreateWithGeneratedCode` — retry on unique violation up
     to 5 times, then `ErrCodeExhausted` (FR-06)
   - `store.Postgres.CreateWithAlias` — insert once; unique violation is
     `ErrAliasTaken` → 409, **never** a substituted code (FR-05)
3. `go test ./...` — the three validator test files are scaffolded with the
   Story 1–3 edge cases already enumerated. The DNS-dependent tests use an
   injected resolver, so they run offline.
4. Verify migrations apply cleanly against a scratch database, including a
   second startup (should be a no-op via `ErrNoChange`).
5. `docker build .` — confirm the distroless image builds and the binary runs.

### 6.3 M1 — Frontend (`web/`)

Independent of the backend until integration; can be built in parallel. Run
from `web/`.

1. `npm install` — already run; `package-lock.json` exists and the build passes.
2. Complete the creation form: URL field, optional alias field, submit,
   result with one-action copy-to-clipboard, inline errors keyed off the
   API's error `code`.
3. `npm run build` — confirm the standalone output is produced.
4. Check the form against the M1 acceptance criteria locally, with the API
   running on `localhost:8080` (the dev-only rewrite in `next.config.js`
   makes `/api/*` resolve the same way it will in production).
5. Accessibility and viewport pass: keyboard-only operation, visible focus,
   320 px width without horizontal scroll (NFR-13, NFR-14).

### 6.4 M1 — Integration and the gate

Requires §6.1, §6.2, §6.3 complete.

1. Clone `shortlink` to `/srv/shortlink/`. Create `.env` from `.env.example`
   (mode 0600).
2. `docker compose up -d --build`. **This manual deploy stands in for the M3
   pipeline, which does not exist yet.** Building the Next.js image on the VPS
   peaks at 1–1.5 GB (CD-01) — acceptable only as a stopgap, only with the
   memory limits in place, and worth watching `docker stats` during. Bring the
   API up first so the UI never briefly points at a missing backend:
   ```
   docker compose up -d --build shortlink-api
   docker compose up -d --build shortlink-web
   ```
3. Confirm the API booted and migrated: `docker logs shortlink-api` shows
   `migrations up to date`, and `curl https://afh.my.id/healthz` returns
   `{"status":"ok"}`.
4. **Story 1** on the live domain — verify each acceptance criterion:
   valid URL → 7-char code; scheme-less input → `https://` assumed;
   `javascript:`, `data:`, `file:` → rejected; `afh.my.id` and
   `app.afh.my.id` targets → rejected; a host resolving to a private or
   loopback address → rejected; copy control puts the short URL on the
   clipboard in one action.
5. **Story 2** — active code → 302 with `Location`; unknown code → plain 404;
   `Referrer-Policy: no-referrer` present; no interstitial and no JavaScript
   on the redirect path; `/Abc` and `/abc` distinct. For 410, set
   `disabled_at` manually over the SSH tunnel (no takedown UI until M2):
   ```sql
   UPDATE links SET disabled_at = now() WHERE short_code = '<code>';
   ```
6. **Story 3** — valid alias used verbatim; taken alias → 409 with no
   substitute code created (check the response *and* the table); reserved
   alias → 400; malformed alias → 400 naming the specific violation.
7. Confirm `app.afh.my.id` serves the UI and `/api/*` is same-origin — the
   browser console should show no CORS errors and no preflight requests.
8. **Re-run the external port scan.** The application deploys are exactly when
   a stray `ports:` entry would appear.
9. **Gate:** Stories 1–3 pass on the live domain. M1 complete.

---

## 7. Decisions made here that the PRD left open

| Question (PRD §19) | Decision | Reasoning |
|---|---|---|
| Is `/healthz` public? | Yes, in M1, reporting only `ok`/`degraded` | External monitoring needs it without credentials; no version or dependency detail, so a scanner learns nothing about the stack |
| Does the 404 page explain itself? | Minimal branded page | Reassures a visitor who mistyped a printed code; no service detail, no link back to the app, no JavaScript |
| Include `/healthz` in M1 at all? | Yes | The PRD lists FR-17 as P0 without assigning a milestone. It is needed for step 6.4.3 and reused unchanged by M3's monitoring |

Two more small ones: HSTS and the other security response headers are set by
the application as well as Caddy, so they survive a proxy config regression;
and request bodies are capped at 1 MB in the application as a backstop to the
proxy cap.

---

## 8. Forward compatibility with M2 and M3

Every item here is a choice made now specifically so M2/M3 does not have to
undo it.

| Decision | Why it holds later |
|---|---|
| `links.user_id` nullable, no FK | M2's migration creates `users` and adds the FK. Existing M1 rows already satisfy it — no backfill |
| `disabled_*` columns created, unwritten | M2/M3 takedown starts writing them with no schema change. The 410 path is already live |
| Reserved list includes M2's route names | `login`, `logout`, `register`, `auth`, `admin` are reserved before those routes exist, so no alias can shadow them. A test asserts router literals are reserved |
| All routes under `/api/` | M2 adds `/api/auth/*`, `/api/links/{code}` with zero Caddyfile changes |
| `/api/*` proxied same-origin from M0 | The UI's fetch URLs never change, and M2's session cookies are same-origin by construction |
| Migrations run in-process at startup | Ordering is unconditional. A rollback to an image that predates a migration fails visibly at boot instead of serving against a schema it does not expect |
| Container hardening applied from M0 | `no-new-privileges`, `cap_drop: [ALL]`, read-only rootfs + tmpfs, `pids_limit`, memory limits are in place now. Retrofitting onto a live service risks breaking things that happened to work under looser constraints. One deliberate exception: Postgres is not `read_only` — it needs writable paths beyond its data volume |
| Image tags `:${GIT_SHA:-local}` | M3's pipeline populates `GIT_SHA` and pushes; no compose restructuring, and rollback by SHA works the moment tagged images exist |
| Memory limits on every container | Without them one leaking process OOM-kills Postgres and takes down every co-tenant |
| `edge` not `internal`; `data` `internal` | The API needs outbound DNS to resolve destination hostnames for the SSRF check. Postgres traffic never needs egress |
| Dedicated `shortlink_api` role, not superuser | Blast radius stays scoped regardless of what M3's backup jobs add |
| Structured JSON logs to stdout, size-capped driver | M3's retention story is already the mechanism in use; no log aggregation stack, which would not fit NFR-06 |

---

## 9. Known gaps in the M1 window

M1 ships a working service that is deliberately incomplete. These are the
gaps, with a recommended stance for each.

### 9.1 Creation is unauthenticated — the significant one

FR-11 requires authentication on creation, and §13.2 requires it "never
anonymous, at any milestone." Both are satisfied by M2, which is the milestone
that introduces accounts. M1 has no accounts, so `POST /api/links` is open.

An open shortener on a public domain is found by bots and used to wrap
phishing destinations. The consequence is not just bad links: if Safe Browsing
blocks `afh.my.id`, Chrome and Firefox show a full-page interstitial for the
**entire domain, including `app.afh.my.id`**, and delisting is slow. A single
successful campaign can take the whole product offline for days.

The PRD's own pre-launch checklist says authentication must be enforced on
create *before the create endpoint accepts traffic*. Taken literally, that
means M1's create endpoint should not be publicly reachable at all.

**Recommended stance — pick one before step 6.4.2:**

- **Restrict the endpoint at the proxy** for the M1 window: a Caddy matcher
  on `/api/links` limited to your own source IP, or basic auth in front of it.
  A few lines in the Caddyfile, removed when M2 lands. This keeps the M1 gate
  fully testable on the live domain while the endpoint stays effectively
  closed. **This is the recommendation.**
- **Or keep the M1 window short and monitored** — deploy, verify the gate,
  then take the create route out of the Caddyfile until M2. Cheapest, but
  leaves a window.

Either way: check Safe Browsing status for `afh.my.id` before and after the M1
window, and keep the manual SQL kill switch to hand (`UPDATE links SET
disabled_at = now() ...`), which works because the columns already exist.

Rate limiting (FR-16) is P1 in the PRD and not part of the M1 gate. It is a
reasonable cheap addition here, but it is not a substitute for the above — a
rate limit slows abuse, it does not prevent it.

### 9.2 Smaller gaps

**The SSRF check is creation-time only.** A hostname validated as public can
be repointed at a private address afterwards (DNS rebinding). Re-validating on
every redirect would put a DNS lookup on the hottest path and break NFR-01.
The exposure is limited because the service never fetches destinations
itself — only the visitor's browser does, after the 302. Documented, not
solved.

**No CI/CD.** Deploys are manual `docker compose up -d --build`, so config
drift between a local checkout and the VPS is possible, and the Next.js build
runs on the memory-constrained host (CD-01). Treat the repos as the only
source of truth — edit and redeploy, never hand-edit files on the VPS.

**No backups.** OPS-01 through OPS-05 are M3. Take an ad-hoc `pg_dump` after
creating any link worth keeping during the M1 window.

**No monitoring or alerting.** Failures surface only via `docker logs`.
Acceptable for a short, attended M1 window; not acceptable indefinitely.

**Brief downtime per deploy.** Compose stops the old container before the new
one is ready. The PRD accepts this (NFR-10, ≤5 s). Naming the service on the
deploy command (`docker compose up -d --build shortlink-web`) keeps it scoped:
a frontend deploy never touches the redirect path. Deploying without a service
argument recreates both containers, so don't.

---

## 10. Verification checklist

Consolidated from §6, in the order you will actually run them.

**M0**
- [ ] SSH: key-only, root login refused, second session confirmed before closing the first
- [ ] `fail2ban` and `unattended-upgrades` active
- [ ] `docker network ls` shows `edge` and `data`
- [ ] `shortlink` database and `shortlink_api` role exist; password set via tunnel
- [ ] TLS certificates issued for `afh.my.id` and `app.afh.my.id`
- [ ] **External** port scan: only 22/80/443

**M1 — backend** (from `api/`)
- [ ] `go test ./... && go vet ./... && gofmt -l .` passes clean
- [ ] Migrations apply to a scratch database; second run is a no-op
- [ ] `docker build .` produces a running distroless image

**M1 — frontend** (from `web/`)
- [ ] `npx tsc --noEmit && npm run build` succeeds
- [ ] Keyboard-only creation works; focus is visible
- [ ] No horizontal scroll at 320 px

**M1 — live gate**
- [ ] `/healthz` returns `{"status":"ok"}`; logs show migrations applied
- [ ] Story 1: all seven acceptance criteria
- [ ] Story 2: all six acceptance criteria (410 via manual SQL)
- [ ] Story 3: all four acceptance criteria
- [ ] `app.afh.my.id` UI works; no CORS errors or preflights in the console
- [ ] External port scan re-run after app deploys
- [ ] Create endpoint access decision from §9.1 applied: `app.afh.my.id` → 401
      without credentials; `POST afh.my.id/api/links` → 404
- [ ] `docker compose up -d --build shortlink-web` leaves the `shortlink-api`
      container's uptime untouched (`docker ps`) — the independent-deploy
      property §2.3 claims

---

## 11. Open items to settle before deploying

1. **GitHub owner name.** The scaffold assumes `azizhudaya` in the Go module
   path and image names. If the actual GitHub account differs, update
   `api/go.mod`, the import paths across `api/`, and both `image:` lines in
   `docker-compose.yml`.
2. **§9.1 stance** — *decided:* Caddy basic auth on all of `app.afh.my.id`
   (single operator credential, bcrypt hash in `afh-infra/.env`), `afh.my.id/api/*`
   answered with 404 at the proxy, and `POST /api/links` requires
   `Content-Type: application/json`. Removed when M2 enforces sessions on create.
3. **Canary code** (§12.2 / glossary): M3's monitoring probes a permanent
   short code. Worth creating one during M1 and never deleting it.
