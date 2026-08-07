# deploy/

Three stacks, one routing table, one set of Dockerfiles.

| | file | published | reached from |
|---|---|---|---|
| local | `compose.yaml` (repo root) | `localhost:8083` | your machine |
| dev | `deploy/stacks/compose.dev.yaml` | `10.10.0.2:19301` | `dev.gaos.mfardiansyah.id` |
| prod | `deploy/stacks/compose.prod.yaml` | `10.10.0.2:18301` | `gaos.mfardiansyah.id` |

**Read `STACKS.md` before touching either deployed tier.** This file covers the
local one only.

## The local stack

No tunnel, no infrastructure, no database. Runs anywhere Docker does.

```bash
docker compose up -d
open http://localhost:8083/
```

| path | service |
|---|---|
| `/` | landing-page (nginx, site root, owns the 404) |
| `/app/` | Mission Control (Go + HTMX) |
| `/docs/` | pustaka |
| `/api/healthz` | the dashboard's health probe |

`GAOS_EDGE_PORT` moves the published port. It defaults to **8083** rather than
8080 because pustaka already holds 8080 on the development box, and a default
that collides is a default nobody uses.

To exercise the ArcadeDB probe against the host-published dev instance:

```bash
GAOS_ARCADE_URL=http://host.docker.internal:9580 docker compose up -d dashboard
curl -sS http://localhost:8083/api/healthz     # {"ok":true,"database":"gaos",...}
```

With it unset the probe answers `{"database":"unconfigured","ok":true}` — that is
the correct answer for this stack, not a degraded one.

## Files

```
deploy/
├── STACKS.md              the tunnelled tiers — topology, secrets, verification
├── check-stacks.sh        asserts what a running stack will never tell you
├── Dockerfile.dashboard   go 1.24 -> distroless; templates and assets EMBEDDED
├── Dockerfile.landing     nginx:alpine + COPY
├── Dockerfile.pustaka     go 1.24 -> distroless; docs COPIED (they are content)
├── nginx/landing.conf     static serving + cache policy, bind-mounted everywhere
├── traefik/
│   ├── traefik.yml        static config, LOCAL stack
│   └── dynamic.yml        the routing table — SHARED BY ALL THREE STACKS
├── stacks/
│   ├── traefik.yml        static config, tunnelled tiers (adds trustedIPs)
│   ├── compose.dev.yaml
│   ├── compose.prod.yaml
│   ├── dashboard.env.example
│   └── docs.env.example
└── edge/
    ├── gaos.conf          the nginx server blocks for clino
    └── README.md          the runbook — certificates, order of operations
```

## Before every deploy

```bash
./deploy/check-stacks.sh
cd dashboard && go test ./... && cd ..
cd pustaka   && go test ./... && go run . check ./docs && cd ..
```

`check-stacks.sh` exists because the two failures that matter most here are
invisible at runtime: a dev stack wired to the production database, and a Traefik
published on every interface instead of the tunnel. Both start cleanly. Both pass
a smoke test. Neither shows up in `docker compose ps`.

## Why the proxy holds no Docker socket

Every other stack on the private VM mounts `/var/run/docker.sock` into Traefik
and keeps its routes in container labels, then relies on a provider constraint to
stop one product's proxy adopting another product's containers. A proxy that
never opens that socket cannot make that mistake, needs no constraint, and does
not hold what is effectively root on the host. The routing table is a file you
can read in a diff.

It is also why `traefik:v3.3` still works here. The Docker Engine 29 API break
that forces neighbouring stacks onto v3.6+ is in the **docker provider** — it
negotiates API 1.24 and the daemon refuses — and that code path never runs when
`providers` names only `file`.
