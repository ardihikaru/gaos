# The tunnelled stacks — GAOS on real infrastructure

`compose.yaml` at the repo root is the **local** stack: one published port, no
infrastructure, runs anywhere. This file describes the two that run on the
private VM and are reached from the internet —
`deploy/stacks/compose.{dev,prod}.yaml`.

## Topology

```
browser
  │  HTTPS
  ▼
clino — 103.127.134.25, wg 10.10.0.1 — PUBLIC edge VM, 1 vCPU / 957 MiB
  │  nginx 1.18 terminates TLS (certbot, HTTP-01, two cert lineages)
  │  and proxies PLAIN HTTP, setting Host, X-Forwarded-For, X-Forwarded-Proto
  │  NO Docker. NO application state. NO PROXY protocol.
  ▼  WireGuard
10.10.0.2:18301  (prod)   — the private VM (vm-rnd-ardi), 8 vCPU / 23 GB
10.10.0.2:19301  (dev)
  │
  ▼  per-stack Traefik, entrypoint `web` on :80, no ACME, no TLS
  /     → landing (nginx, site root, owns the origin's 404)
  /app/ → dashboard (Mission Control, Go + HTMX)
  /docs/→ docs (pustaka)
  /api/ → dashboard (JSON: /api/healthz)
```

The private VM hosts several products this way. Each owns a Traefik and one
tunnel port; nothing but the edge is ever publicly bound.

## The port slot

Ports are `1<env><product>1` — `18xx1` production, `19xx1` non-production. The
middle digits are the product. **GAOS is `03`.**

| Digits | Product | Prod | Dev |
|---|---|---|---|
| `00` | portal | 18001 | — |
| `01` | clino (laundry) | 18101 | 19101 |
| `02` | cubiq | 18201 | 19201 |
| **`03`** | **gaos** | **18301** | **19301** |
| `04` | pustaka | 18401 | — |
| `05` | jagara | 18501 | — |
| `06` | saas | 18601 | — |

Hostnames: `gaos.mfardiansyah.id` (prod), `dev.gaos.mfardiansyah.id` (dev).
**Both DNS records already exist** and point at the edge.

## The edge

`clino` terminates TLS and proxies plain HTTP over the tunnel. Certificates come
from certbot over **HTTP-01** — not DNS-01 wildcards, because both domains
delegate to `dns-parking.com` and there is no API to authenticate against. Exact
hostnames need no DNS credential at all.

GAOS uses **two certificate lineages**, `gaos` and `gaos-dev`, rather than one
certificate carrying both names. HTTP-01 fails all-or-nothing: one hostname that
stops resolving aborts the renewal of every SAN it shares. Splitting them means
a broken dev record cannot take production's certificate down with it.

The full runbook, including the four rules that will otherwise take every
product on that box offline, is in **`deploy/edge/README.md`**. The config itself
is `deploy/edge/gaos.conf`.

Do **not** reintroduce PROXY protocol. Traefik here is configured for
`forwardedHeaders`; an edge sending a PROXY preamble to a proxy not expecting one
is an HTTP 400 on every request, not a degradation.

## One origin, on purpose

One hostname per environment, routed by path. Not cosmetic:

- Same-origin means **no CORS**, and it is what lets Mission Control reference
  every asset, fragment and form action relatively.
- **Neither the dashboard nor pustaka has a base-path option.** `/app` and
  `/docs` work solely because Traefik strips the prefix — and because both read
  the `X-Forwarded-Prefix` that strip sets. The dashboard puts it into a single
  `<base href>`; pustaka puts it back onto its root-absolute login redirects.
  Replace `stripPrefix` with a rewrite that drops the header and both break
  silently, with a 200 on every document and a 404 on every asset.

All three stacks share one routing table, `deploy/traefik/dynamic.yml`, so those
rules exist in exactly one place. The service names `traefik`, `landing`,
`dashboard`, `docs` are its upstream DNS names and are load-bearing.

## The database

GAOS runs **no** database of its own. Each stack joins the shared infra network
as `external` and reaches ArcadeDB by container DNS — no host ports, no
`host.docker.internal`.

| | Network | ArcadeDB | Infra stack |
|---|---|---|---|
| dev | `master-compose-ai-dev-net` | `http://arcadedb-master-dev:2480` | `projects/infra` |
| prod | `master-compose-ai-net` | `http://arcadedb-master:2480` | `projects/infra-prod` |

Only `dashboard` joins it. The proxy, the landing page and the docs cannot reach
the other projects' MySQL, RabbitMQ or Qdrant that share that network.

If compose reports *"network … declared as external, but could not be found"*,
the infra stack is down. Start it there — **never `docker network create` it
here**, which produces an empty network with the right name and a stack that
starts cleanly and can reach nothing.

**In this pass the dashboard does not query the graph.** It renders fixtures and
probes reachability. `GET /api/healthz` answers:

```json
{"ok":true,"database":"gaos","store":"memory","service":"gaos-dashboard"}
```

and **503** with `{"ok":false,"database":"unreachable"}` when the probe fails.
That probe is `GET {url}/api/v1/ready`, which answers **204 No Content**, not
200 — measured against both live instances on 2026-08-07. A check written
against 200 reports every healthy database as unreachable.

Unlike the neighbouring product's API server, **an unreachable database does not
stop the dashboard.** It serves fixtures either way. A crash loop would take the
whole UI away for a database it does not read yet.

## Secrets

Four gitignored files, per service and per environment:

```
deploy/stacks/{dev,prod}.dashboard.env    <- dashboard.env.example documents the names
deploy/stacks/{dev,prod}.docs.env         <- docs.env.example (the docs login)
```

`env_file:`, not `${…}` interpolation: no flag to forget, and a missing file is a
hard error rather than a silently empty value.

**Fill by name, one line at a time.** Never `cat` an infra `.env` into these — it
injects every line, handing GAOS's containers every other project's credentials.
`chmod 600` all four.

Generate `PUSTAKA_AUTH_SECRET` on the host (`openssl rand -hex 32`). Blank means
the server invents a random one per process, so every deploy signs everyone out.

## Bring-up

Dev first, always. There is no bootstrap step yet — no schema is applied in this
pass — so the sequence is short:

```bash
cd /home/ardi/devel/projects/gaos
./deploy/check-stacks.sh                    # the invariants a running stack hides

S=deploy/stacks/compose.dev.yaml            # dev only -- prod goes through --tag
docker compose -f $S build
docker compose -f $S up -d
docker compose -f $S ps --format '{{.Service}}\t{{.Status}}\t{{.Ports}}'
```

In practice use `deploy/rebuild-restart.sh`, which wraps all of the above plus
the test gate and the post-deploy verification. Prod additionally needs a tag —
see the next section.

## Building from a tag

**Prod builds from a tag, not from this checkout.** `rebuild-restart.sh --tag
<ref>` checks the ref out into a detached worktree under `.worktrees/` and
re-runs itself inside it, so the invariant check, the test gate and
`docker compose build` all read the tagged tree. What ran in production is then
a ref you can name a month later, rather than whatever happened to be saved.

```bash
git tag -a v0.13.0 -m "…"                                  # tag first
./deploy/rebuild-restart.sh prod deploy --tag v0.13.0
```

Without `--tag`, a prod `deploy` or `restart` refuses and lists the tags it
could use. `--from-tree` is the deliberate escape hatch and keeps the old
behaviour, dirty-tree refusal included.

Three things worth knowing:

- **The env files are symlinked in, not copied.** They are secrets, created by
  hand and never committed, so a fresh worktree has none and compose would stop
  at *env file not found*. Linking keeps one copy on this disk, so rotating a
  password cannot leave a stale duplicate in a worktree nobody removed.
- **Worktrees are reused, not deleted.** A second deploy of the same tag skips
  straight to pre-flight. `git worktree list` shows them; `git worktree remove
  .worktrees/<tag>` takes one back.
- **The inner run is the tag's own copy of the script**, which is why `--tag` is
  not forwarded to it. That is deliberate: it means `--tag` can build a ref
  older than the flag itself.

Dev is unchanged — it builds from the working tree unless you ask otherwise,
because dev is where a change is looked at before it is worth tagging.

## Verify

**Nothing is bound publicly.** One row with a port, reading
`10.10.0.2:19301->80/tcp`:

```bash
ss -ltnp | grep -E ':(18301|19301)'          # 10.10.0.2 only, never 0.0.0.0
curl -sS --connect-timeout 3 http://192.168.30.102:19301/   # must be REFUSED
```

**Through the tunnel, from the edge** — the real test:

```bash
ssh clino "curl -sS -H 'Host: dev.gaos.mfardiansyah.id' http://10.10.0.2:19301/api/healthz"
ssh clino "curl -sS -H 'Host: gaos.mfardiansyah.id'     http://10.10.0.2:18301/api/healthz"
# {"ok":true,"database":"gaos",...}
```

`{"ok":false,"database":"unreachable"}` (503) means the ArcadeDB leg is wrong —
network name, alias or credentials. A bare **502** means `dashboard` is not
running at all. Then the other routes:

```bash
ssh clino "curl -sS -o /dev/null -w '%{http_code}\n' -H 'Host: gaos.mfardiansyah.id' http://10.10.0.2:18301/"      # 200, landing
ssh clino "curl -sS -o /dev/null -w '%{http_code}\n' -H 'Host: gaos.mfardiansyah.id' http://10.10.0.2:18301/app"   # 301
ssh clino "curl -sS -o /dev/null -w '%{http_code}\n' -H 'Host: gaos.mfardiansyah.id' http://10.10.0.2:18301/docs"  # 301
```

**And check `<base href>` survived the strip.** This is the one that fails
silently — a 200 on the document with a 404 on every asset:

```bash
ssh clino "curl -sS -H 'Host: gaos.mfardiansyah.id' http://10.10.0.2:18301/app/" | grep -o '<base href=\"[^\"]*\">'
# must read <base href="/app/">, NOT <base href="/">
```

**Real client IP actually survives — do not skip this.** Traefik believes
`X-Forwarded-For` only from a peer in `trustedIPs`; from anyone else it discards
the header and substitutes the peer's own address. Wrong, and every caller on
earth shares one identity while everything looks healthy:

```bash
ssh clino "curl -sS -H 'Host: gaos.mfardiansyah.id' -H 'X-Forwarded-For: 203.0.113.9' \
  http://10.10.0.2:18301/api/healthz > /dev/null"
docker compose -f deploy/stacks/compose.prod.yaml logs --tail 1 traefik
```

The first access-log field is `ClientHost`. It **must** read `203.0.113.9`. If it
shows `10.10.0.1`, the header was discarded and the trust list is wrong.

## Known gaps

- **Graph queries are opt-in, not the default.** The DDL is applied to dev and
  prod and `store_arcade.go` implements the `store.go` seam against it — one
  conformance suite covers both stores. But `selectStore` still defaults to
  `memory`, seeded from the prototype's fixtures; ArcadeDB is reached only with
  `GAOS_STORE=arcadedb`, and a bad connection fails the boot rather than falling
  back.
- **The back-office login checks nothing.** It sets a cookie, exactly as the
  prototype did. Real auth is pustaka's HMAC-signed cookie, credential
  fingerprint and per-IP throttle, lifted from `pustaka/main.go`.
- **The floor is simulated.** `dashboard/sim.go` is a goroutine standing in for
  the scheduler and the runner. It emits only names from the closed catalog in
  `events.go`, so replacing it changes no consumer.
- **Dev builds from this working tree.** That is deliberate — dev is where you
  look at a change before it is worth tagging. Prod no longer does; see
  *Building from a tag* above.
- The local `compose.yaml` defaults to host port **8083**, because 8080 is
  already held on this box. Override with `GAOS_EDGE_PORT`.
