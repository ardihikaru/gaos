# Traffic monitor — GAOS's outbound and inbound calls, on one back-office page

> **Status:** In Review (rev 2, 2026-11-27). Rev 1 was read by the four general lenses and the security lens (round 1, `docs/designs/reviews/traffic-monitor-{comprehension,zoom-out,critic,readiness}-1.md`, `traffic-monitor-security-review-1.md`: B=2, M=15, N=40; security High=1, Medium=5, Low=4). Rev 2 applies the author's decisions on every finding; the triage is §21. Nothing outside `docs/designs/` changes until the operator approves.
> **Policy:** `docs/rules/gaos-design-review.md` (the security lens is required: this touches the back-office surface, a new service and the machine API's gate, `:92-100`).
> **Related:** `docs/designs/release-b4-unit-tests.md` (D-106: the edge-set source address this design reuses, and the host-firewall step it re-ranks), `docs/designs/integrations-status-chips.md` (the Integrations page), `docs/designs/gaos-pr-overview.md` (the Pull requests page whose live list this page copies).
> **Paths.** A bare file name (`staging.go`) is in `dashboard/`; every other path is from the repository root. Anchors are against `main` at v0.68.0 (`fc0f9c3`).
> **Registers (verified on `main` at `fc0f9c3`):** next free **D-107** (the log ends at `d-106`, `pustaka/docs/product/decision-log.html:2167`), **BR-66** (the BRD ends at BR-65, `pustaka/docs/product/brd.html:1310`), **TC-884** (the register's highest row is TC-883, `pustaka/docs/product/qa-test-plan.html:974`; `deploy/check-workspace.sh:105-111` asserts rows = highest). Release **v0.69.0** (`pustaka/docs/assets/toc.js:22` is `0.68.0`).
> **Private names.** This document and every file it changes name the QA platform only as "the QA tool" and the public proxy only as "the edge" (D-085).

## 0. Why, and the scope

**The operator's words** (2026-11-27, verbatim). The operator answered an item of the v0.68.0 deploy report, which the reply quotes:

> "regarding this: ``` Still manual, not applied: a firewall rule on each VM that limits which tunnel machines can reach GAOS directly. It needs a day of watching who connects first, so it doesn't cut off the QA tool. The commands are in deploy/edge/README.md.``` . as long as we have dashboard / monitoring page in GAOS to view traffic outbound to QA tool (and other external platform), will be enough. add them if there is none yet. use `https://aiforui.dev/skills` for better UI/UX. you can use redis stream just to limit last N record only and do auto retention. we dont need a long period of traffic monitoring data anyway. since it will be used only for periodic monitoring."

**What that decides.** The quoted item is the D-106 host firewall (`deploy/edge/README.md:170-209`), still unapplied. The operator's answer — a monitoring page "will be enough" — chooses **visibility over the firewall as the required step**. The firewall is not dropped: it stays documented as **recommended hardening** (D-107, §14), and the README says honestly what the page can and cannot see (§17 item 32). The firewall's question is "which tunnel machines reach GAOS directly" (`deploy/edge/README.md:177-182`), so the page must show both directions:

- **Outbound** — every call GAOS makes to an external platform (§7), which is the operator's own wording.
- **Inbound** — who reaches GAOS, from where, and whether through the edge or directly over the tunnel (§8), because that is what the quoted item was about.

**Verified facts this design starts from.**

1. GAOS has no traffic page: the back-office views are the nine in `urls.go:31-35`.
2. GAOS runs no Redis: the prod stack is `traefik`, `landing`, `dashboard`, `docs` (`deploy/stacks/compose.prod.yaml:31-258`; `deploy/rebuild-restart.sh:26`).
3. The dashboard is stdlib-only by policy (`dashboard/go.mod:1-7`).
4. Every external HTTP client is built in one of eight places (§7.1), plus ArcadeDB's (`arcade.go:74-75`), excluded (§7.1).
5. Every request the dashboard receives passes `server.gate` (`auth.go:541-590`), which resolves the matched pattern (`:543`) and its class (`:548-554`); every `registerAPI` pattern is bearer (`main.go:666-669`). Traefik routes `/app/` and `/api/` to the dashboard and `/` and `/docs/` to two other containers (`deploy/STACKS.md`, "Topology").
6. The caller's address is computed already, for the Basic routes' lock: `jenkinsSource` (`staging.go:270-297`) takes `X-Real-IP`, which Traefik trusts from the edge alone (`deploy/stacks/traefik.yml:52-53`; `deploy/edge/README.md:158-164`), and reduces IPv6 to its /64 (`staging.go:307-316`).

**Success.** After v0.69.0 is deployed on both prod instances, a signed-in operator opens **Traffic** and sees, per platform and per inbound class, what was called, by whom, how it answered and how far back the kept records reach. Stopping Redis changes nothing anywhere else: every page, route and outbound call behaves as before (with the one residual of §8.6), and the page says the store is unavailable.

**The firewall cross-check.** The operator runs the edge README's own sampling step (`conntrack -L -p tcp --orig-port-dst 18301`, `deploy/edge/README.md:186-187`) over the same period the page covers. It passes when **every conntrack peer other than the edge (`10.10.0.1`) appears on the page as a `direct` source, or is explained by a named exclusion**:

| Exclusion | Why the page cannot see it | How the operator explains it |
|---|---|---|
| E1 — a peer that only fetched `/` or `/docs/` | Traefik sends those to the `landing` and `docs` containers; the dashboard never sees the request (`deploy/STACKS.md`, "Topology") | `docker compose -f deploy/stacks/compose.prod.yaml logs traefik`: the access log's first field is the client (`deploy/edge/README.md:158`) |
| E2 — a TCP connection that sent no complete HTTP request (a port scan) | No request reaches Traefik's router | conntrack shows the state (`SYN_SENT`, `CLOSE`) with no `ASSURED` flag |
| E3 — a record lost to a drop or to eviction | §9, §5.3 | the page shows the drop counters and each stream's oldest kept record (§10.2); a peer seen before the oldest kept record is explained by it |

Traefik's access log is **not** adopted as the page's inbound source (§18 alternative 4).

## 1. Glossary

| Term | Meaning |
|---|---|
| Record | One call, as the fields of §5.2. Not an event (§12). |
| Outbound | A call GAOS makes through one of the eight clients of §7.1. |
| API call | A request to a machine-API route (a bearer pattern, `main.go:666-669`) or to an unmatched `/v1/` path (§8.2). |
| Route class | For an inbound record: `api` (an API call), or, for a direct request elsewhere, `backoffice`, `public`, `healthz`, `static` or `unmatched` (§8.2). |
| Platform | The closed label of an outbound client: one of eight (§7.1). |
| Source | The caller's address as `jenkinsSource` computes it (`staging.go:276-297`): IPv4 whole, IPv6 as its /64. |
| Edge hop | The edge's tunnel address, `10.10.0.1`, the one entry of Traefik's trust list (`traefik.yml:53`). |
| Via | `edge` when the request was delivered to Traefik by the edge hop, else `direct` (§8.3). |
| Principal | The machine-API identity that authenticated: `admin`, a clients-file name (`api_v1.go:54-55`), `basic:staging` or `basic:unit-results`; none when nothing authenticated. |
| Stream | One capped Redis stream (§5.1): eight outbound, three inbound. |
| Retention window | 48 hours: records older are trimmed and never counted (§5.3). |
| Cap | A stream's maximum number of records (§5.1). |
| Drop | A record not stored: the queue was full (§9.1) or Redis did not answer or refused it (§9.2). Counted and shown. |
| Evicted | A record removed because its stream reached its cap. Visible as the stream's oldest kept record (§10.2). |

## 2. What changes

| Area | Change | Section |
|---|---|---|
| Stack | A `redis` service, pinned by version and digest, on a pinned `internal: true` network shared with the dashboard only, at a static address; no port, no disk, `maxmemory 64mb` `noeviction`, an ACL that admits three commands on the traffic keys, a healthcheck, non-root, read-only | §4 |
| Settings | `GAOS_REDIS_ADDR` (an IP and port, fixed in compose); `GAOS_TUNNEL_IP` passed to the dashboard | §4.3 |
| Go | `resp.go`: a minimal RESP2 client, no dependency | §6 |
| Go | `traffic.go`: the recorder, queue and writer; the recording transport; the path template; the inbound recorder in the gate | §7–§9 |
| Go | `traffic_view.go`: the page model, summaries, filters, poll | §10 |
| Go | The eight clients wrap their transport; TaskMgr reports its gRPC code; `apiGate` and `jenkinsGate` name the principal; the two outbound deny-lists gain the Redis host | §7, §8.4, §13.4 |
| UI | **Traffic** after Integrations; `bo-traffic`, `traffic-live`; one CSS selector widened | §10, §11 |
| Checks | `check-stacks.sh` gains "the traffic store" block | §4.4 |
| Deploy | `rebuild-restart.sh` and the deploy agent accept `redis` | §4.5 |
| Docs | `integrations/traffic.html` (new); `STACKS.md`, `infra/environments.html`, the edge README §5 | §17 |
| Registers | D-107, D-106 amended, BR-66, TC-884 … TC-901, v0.69.0; no event type, task status, schema statement or docs CSS class | §14 |

## 3. Data flow

```
 outbound                                      inbound
 a GAOS client (§7.1)                          caller ─► the edge (10.10.0.1) ─┐      tunnel peer / this VM ─┐
   │ http.Client.Do                                                            ▼                             ▼
 recordingTransport.RoundTrip                  Traefik ── appends the delivering hop to X-Forwarded-For ── /app, /api ─► dashboard
   │ base.RoundTrip ──► the platform           (/, /docs ─► landing, docs: not seen, exclusion E1)                 │
   │ (answer returned unchanged)                                                                   server.gate: via from the last hop
   ▼                                                                                               API call → full record
 rec.out ─────────────────┐                                                                         direct, any route → reduced record
                          ▼                                                                         edge, not API → nothing
          bounded queue 4096 (select … default: drop++; anonymous records stop at ¾)  ◄── rec.in ◄──┘
                          │
                          ▼  one writer: batch ≤128 or 250 ms, pipelined
          XADD gaos:traffic:<stream> MAXLEN ~ <cap> * …      every 60 s: XTRIM <each stream> MINID ~ <now − 48 h>
                          │
                          ▼  redis, 172.30.14.2 on gaos-prod-traffic (internal), ACL: xadd xtrim xrevrange
                          ▲
                          │  11 × XREVRANGE … COUNT <cap>, pipelined, at most once per 15 s (cached)
          traffic_view.go ── Traffic page, polled every 30 s
```

## 4. The traffic store

### 4.1 The service

Added to `deploy/stacks/compose.prod.yaml` after `dashboard` (`:59-238`), before `docs` (`:240`):

```yaml
  # D-107: the traffic store. Eleven capped Redis streams of recent calls in and out;
  # nothing reads them but the Traffic page. Its own internal network, shared with the
  # dashboard alone, at a fixed address: no port, no route out, no neighbour, no DNS name
  # in use. No disk: a Redis restart -- or a whole-stack restart -- empties it, by design.
  # GAOS starts and serves without it: there is no start-order dependency either way.
  redis:
    image: redis:8.10.3-alpine@sha256:7958cfbb9d152f87dadfbc89f1b0b15938f0b81ff8982feb327476f4911935c3
    restart: unless-stopped
    command: ["redis-server", "--save", "", "--appendonly", "no", "--maxmemory", "64mb", "--maxmemory-policy", "noeviction", "--user", "default", "on", "nopass", "resetkeys", "~gaos:traffic:*", "resetchannels", "-@all", "+ping", "+xadd", "+xtrim", "+xrevrange", "+acl|list"]
    user: "999:1000"
    read_only: true
    tmpfs: ["/data"]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 128m
    cpus: 0.25
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 30s
      timeout: 3s
      retries: 3
    networks:
      traffic: {ipv4_address: 172.30.14.2}
```

| Choice | Decision | Why |
|---|---|---|
| Image | `redis:8.10.3-alpine`, pinned by tag **and digest** | *Assumption A1 (external facts, re-checked in T4):* 8.10.3 was released 2026-10-26 (`redis/redis` GitHub releases), and the `8.10.3-alpine` manifest list was last pushed 2026-11-11 with the digest above (Docker Hub, read 2026-11-27). The tag is mutable; the digest is the supply-chain pin. The "released at least two weeks before use" rule is the instruction given for this design, not a repository policy. T4 re-reads the digest with `docker buildx imagetools inspect redis:8.10.3-alpine` before the compose change lands. |
| Persistence | None: `--save ""`, `--appendonly no`, `/data` a tmpfs | The operator wants no long history (§0). A Redis restart, and a whole-stack `rebuild-restart.sh <env> restart` (which restarts every service, `deploy/rebuild-restart.sh:368`), empty every stream. A dashboard redeploy does not: only that container is replaced. |
| Memory | `--maxmemory 64mb`; container `mem_limit: 128m` | The caps of §5.1 total 13 000 records; at the field bounds of §5.2 (≤ 600 bytes a record) that is ≤ 8 MB. 64 MB is headroom the streams cannot reach. The container limit bounds Redis' own overhead, as `docs` is bounded (`compose.prod.yaml:253-257`). |
| Eviction | `noeviction` | Eleven keys only; `allkeys-lru` at the limit would delete a whole stream. Under `noeviction` an `XADD` past the limit is refused (`OOM`) and the writer counts a drop (§9.2). The arithmetic above says the limit is not reached. |
| ACL | The `default` user: no password, keys `gaos:traffic:*` only, no channels, commands `ping`, `xadd`, `xtrim`, `xrevrange` and `acl list` only | The dashboard uses three commands (§6); `ping` is the healthcheck; `acl list` lets T4 read the ACL back. Everything else — `FLUSHALL`, `DEL`, `XDEL`, `EVAL`, `CONFIG`, `KEYS` on other names — is refused (`NOPERM`) to anything that reaches Redis, with no secret file to manage. |
| Network | `traffic` only, static `172.30.14.2` (§4.2) | Only the dashboard can reach Redis; Redis has no route out. A fixed address means the dashboard never resolves a name, so a stopped Redis can never send the writer to a host found through outside DNS (SEC-5). |
| Port | No `ports:`, no `expose:` | Nothing is published beyond Traefik's one mapping (`check-stacks.sh:50-54`). |
| Hardening | `user: "999:1000"`, `read_only`, `tmpfs /data`, `cap_drop: [ALL]`, `no-new-privileges` | The official entrypoint switches user only when started as root. *Assumption A2 (T4):* the alpine image's `redis` user is uid 999, gid 1000 (`docker run --rm <image> id redis`); if not, the `user:` line takes the printed values. |
| Healthcheck | `redis-cli ping`, 30 s | `docker compose ps` shows `healthy`. |
| Start order | No start-order key in either direction | GAOS must start, serve and call out with Redis absent (§9.4). |
| YAML shape | Every list in the block is a flow sequence (`[...]`) | `check-stacks.sh:51` counts lines starting with `- "` as port mappings. |

**Licence.** Redis 8 is RSALv2 / SSPLv1 / AGPLv3 at the user's choice. GAOS runs the unmodified upstream image as a separate process, does not link, modify or redistribute it (the host pulls it), so nothing reaches GAOS's Apache-2.0 code. Valkey (BSD-3) speaks the same protocol and commands (§18 alternative 6).

### 4.2 The network

`compose.prod.yaml` `networks:` (`:20-28`) gains:

```yaml
  # D-107: the dashboard and the traffic store, nothing else. internal: no route out.
  # PINNED, so Redis has a fixed address and the dashboard resolves no name for it.
  # 172.30.14.0/29 is GAOS's, beside .12 (prod) and .13 (dev).
  traffic:
    name: gaos-prod-traffic
    internal: true
    ipam:
      config:
        - subnet: 172.30.14.0/29
```

The subnet line is a block-sequence `- subnet:` entry like the existing one (`:27-28`); it does not start with `- "`, so `check-stacks.sh:51` does not count it. *Assumption A3 (T4):* `172.30.14.0/29` is free on both prod VMs (`docker network inspect $(docker network ls -q) --format '{{.IPAM.Config}}'` lists no overlap); if not, the next free /29 in `172.30.14.0/24` is taken and the address follows. The header comment's neighbour line (`:25`) gains ".14 is the traffic network".

The `dashboard` service's `networks: [internal]` (`:238`) becomes `networks: [internal, traffic]`.

The dev-parity comment (`:9-13`, "differing ONLY in …") gains: "— and the traffic store (D-107), which is prod's alone: dev is retired".

### 4.3 The settings

The dashboard's `environment:` gains, after `GAOS_TASKMGR_CA_FILE` (`:207`):

```yaml
      # D-107: the traffic store, by its fixed address on the traffic network (an IP, never
      # a name). The tunnel address is this VM's, as in ports: above; the recorder uses its
      # /24 to class an inbound caller that bypassed Traefik's forwarded chain.
      GAOS_REDIS_ADDR: "172.30.14.2:6379"
      GAOS_TUNNEL_IP: "${GAOS_TUNNEL_IP:-10.10.0.2}"
```

| Variable | Empty / unset | Valid | Invalid |
|---|---|---|---|
| `GAOS_REDIS_ADDR` | recording off; wrapping is a no-op (§7.2); the page says Off | an IP literal and a numeric port | a name or a malformed value: off, the reason logged once and shown on the page |
| `GAOS_TUNNEL_IP` | the fallback of §8.3 uses no tunnel prefix | an IPv4 literal | ignored, logged once |

**The caps and the retention window are constants**, not settings (§5.1, §5.3); a change is a code change. The boot log prints one line: `traffic: recording to 172.30.14.2:6379, 48 h` or `traffic: off (GAOS_REDIS_ADDR is not set)`; it never prints a recorded value.

The dev compose (retired, `deploy/STACKS.md:1-3`) and the local `compose.yaml` get no Redis; their dashboards run with recording off.

### 4.4 `check-stacks.sh`

A new block after "the proxy never holds the Docker socket" (`check-stacks.sh:91-105`), prod only like the credential checks (`:192-253`). Two helpers are added beside `has`/`hasnt` (`:24-30`):

- `svcblock <name>` prints the lines of `$PROD` after the line `  <name>:` up to (not including) the next line that matches `^ {0,2}[a-z]` — a service block, or a top-level network block.
- `inblock <name> <fixed string> <message>` is `svcblock <name> | grep -qF -- <fixed string> && ok <message> || bad <message>`; `inblockre` is the same with `grep -qE`; `notinblock`/`notinblockre` invert it.

A line whose message joins conditions with "and" is one `if` over all of them, printing one `ok` or one `bad`.

```
== the traffic store (D-107) ==
ok   prod pins the Redis image by version and digest
ok   prod's Redis persists nothing (no RDB, no AOF, no volume)
ok   prod's Redis has a memory limit and refuses writes rather than evicting a stream
ok   prod's Redis admits only the traffic keys and the commands GAOS uses
ok   prod's Redis publishes no port
ok   prod's Redis is on the internal traffic network only, at its pinned address
ok   prod's Redis has a healthcheck
ok   prod's dashboard joins the traffic network and reaches Redis by address, never by name
ok   the dashboard's edge hop is the one address Traefik trusts
ok   nothing in prod has a start-order dependency
```

| Message | Assertion |
|---|---|
| pins the image | `hasre $PROD '^[[:space:]]+image: redis:[0-9]+\.[0-9]+\.[0-9]+-alpine@sha256:[0-9a-f]{64}$'` |
| persists nothing | `inblock redis '"--save", ""'`, `inblock redis '"--appendonly", "no"'`, `notinblock redis 'volumes:'` |
| memory limit | `inblockre redis '"--maxmemory", "[0-9]+mb"'` and `inblock redis '"--maxmemory-policy", "noeviction"'` |
| ACL | `inblock redis '"--user", "default", "on", "nopass", "resetkeys", "~gaos:traffic:*", "resetchannels", "-@all", "+ping", "+xadd", "+xtrim", "+xrevrange", "+acl|list"]'` (the whole tail, so a widened list fails) |
| no port | `notinblockre redis '^[[:space:]]*(ports|expose):'` |
| traffic network | `inblock redis 'traffic: {ipv4_address: 172.30.14.2}'`; `inblock traffic 'name: gaos-prod-traffic'`; `inblock traffic 'internal: true'`; `inblock traffic 'subnet: 172.30.14.0/29'` |
| healthcheck | `inblock redis 'healthcheck:'` and `inblock redis '"redis-cli", "ping"'` |
| dashboard | `inblock dashboard 'networks: [internal, traffic]'`; `has $PROD 'GAOS_REDIS_ADDR: "172.30.14.2:6379"'`; `has $PROD 'GAOS_TUNNEL_IP: "${GAOS_TUNNEL_IP:-10.10.0.2}"'` |
| edge hop | the value of `edgeHop = "…"` in `dashboard/traffic.go` (`grep -oE 'edgeHop *= *"[0-9.]+"'`) followed by `/32` equals the single trust entry `check-stacks.sh:98` already finds in `deploy/stacks/traefik.yml` |
| no start order | `hasntre $PROD '^[[:space:]]*depends_on:'` (a key, never a comment) |

Failures print `FAIL` with the same message (`check-stacks.sh:22`). The block is one register row, TC-901. **Its negative half is run by hand**, like every check of this script (D-025): in a scratch checkout, break one condition at a time in `deploy/stacks/compose.prod.yaml` (or `edgeHop` in `traffic.go`), run the script, see that line print `FAIL`, restore with `git checkout -- <file>`; the run is recorded in the QA plan's prose for TC-901 by the date and the eleven results (§15).

### 4.5 `rebuild-restart.sh` and the deploy agent

`SERVICES="traefik landing dashboard docs"` (`deploy/rebuild-restart.sh:26`) and its usage comment (`:15`) gain `redis`. A plain `deploy` already starts every service (`:359-367`). `dev … redis` passes the list and then fails inside compose with compose's own "no such service" message; dev is retired (`deploy/STACKS.md:1-3`), so this is accepted rather than split per tier. The deploy agent's list of valid services (`.claude/agents/gaos-deploy.md:38`) gains `redis`. The verification steps (`:373-470`) are unchanged: Redis is not on the health path. TC-019's quoted message (`qa-test-plan.html:1080-1081`) gains ` redis`.

## 5. Streams, records and retention

### 5.1 Streams and caps

| Stream key | Holds | Cap (constant) |
|---|---|---|
| `gaos:traffic:out:<label>` × 8 | outbound calls of one platform (§7.1) | 1 000 each |
| `gaos:traffic:in:direct` | every inbound record with `via = direct`, any route class | 2 000 |
| `gaos:traffic:in:auth` | API calls through the edge where a principal was set | 2 000 |
| `gaos:traffic:in:anon` | API calls through the edge with no principal | 1 000 |

One stream per platform, so a busy platform cannot push a quiet one out; and three inbound streams, so an unauthenticated flood through the edge evicts only `in:anon` — never a direct caller and never an authenticated client (SEC-1, §13.5). Each prod instance deploys its own copy of the compose file and so its own Redis (`deploy/edge/README.md:181-182`, `:210-215`): the keys carry no instance prefix.

**Volume estimate** (the cadences that write outbound records; the page shows the real reach, §10.2):

| Platform | Source of calls | Rate | 1 000 records last |
|---|---|---|---|
| `ai-manager` | a turn polls its result every 2 s for up to 660 s (`adapter_aimanager.go:44-45`) | ≤ 330 per turn | ~3 long turns; on a busy day hours, not 48 h |
| `qa-tool` | a running job's status every 15 s (`qajobs.go:193`), ≤ 40 min a job; inbox answers on a 10 s tick when one is due (`inbox_worker.go:31`); sync buttons | ≤ 160 per job | ~6 jobs |
| `github` | each non-final fix PR every 5 min for 7 days (`fixprpoll.go:16-24`), plus flow reads | 288 per PR per day | ~3.5 PR-days |
| `taskmgr` | directory refresh every 15 min (`taskmgr.go:43`), plus lookups | ~100 a day | > 48 h |
| `github-write`, `whatsapp`, `brain-town`, `brain-town-reader` | per fix, notice, event, stream reconnect | tens a day | > 48 h |
| inbound `auth` | the QA tool's calls and Jenkins posts | seconds to minutes apart (*assumption A4*, visible on the page) | hours to 48 h |

So the busy platforms are bounded by their cap, not by the 48 h window, and the page says so per stream (§10.2) instead of claiming 48 hours.

### 5.2 Record fields

`XADD <key> MAXLEN ~ <cap> * <field> <value> …`, server-assigned id; the call's own time is `t`, because a batch is written up to 250 ms later (§9.2).

| Field | Out | In (API) | In (reduced) | Value | Bound |
|---|---|---|---|---|---|
| `t` | ✓ | ✓ | ✓ | call start, Unix ms | — |
| `m` | ✓ | ✓ | ✓ | `GET POST PUT PATCH DELETE HEAD OPTIONS`, else `OTHER` | closed |
| `s` | ✓ | ✓ | ✓ | HTTP status; `0` = no response | integer |
| `d` | ✓ | ✓ | ✓ | ms: out, to the response headers or the error; in, gate to handler return | integer |
| `qb` | ✓ | ✓ | | request `ContentLength`, `-1` unknown | integer |
| `rb` | ✓ | ✓ | | out: response `ContentLength` (`-1` unknown; the body is never read); in: bytes written | integer |
| `h` | ✓ | | | `req.URL.Host`, lower-cased | 100 bytes |
| `u` | ✓ | | | the path, relative to the client's base, templated (§7.4) | 160 bytes |
| `e` | ✓ | | | error class (§7.5); empty when a response arrived | closed |
| `g` | taskmgr | | | the gRPC status code, `-1` when none was read (§7.6) | integer |
| `k` | | ✓ | ✓ | route class (§1) | closed |
| `r` | | ✓ | | the route pattern without its method, or `(no route)` | registered patterns |
| `src` | | ✓ | ✓ | the source (§8.3) | 45 bytes |
| `via` | | ✓ | ✓ | `edge` or `direct` | closed |
| `c` | | ✓ | | the principal, empty when none | 64 bytes |
| `pid` | | ✓ | | `{id}` when it matches `^p-[0-9]+$` (`urls.go:90`) | 16 bytes |

The platform is the key, not a field. No record holds a header, a body, a query string, a fragment, userinfo, a cookie, a token, a password, an error message, or — for a reduced record — any path. A record the reader cannot parse is skipped (§10.3); there is no version field, because a shape change simply deploys and the old records age out within 48 h.

### 5.3 Retention

- **By count:** every `XADD` carries its stream's `MAXLEN ~ <cap>`. `~` trims whole internal nodes, so a stream may hold up to ~100 records over its cap; the page reads at most the cap.
- **By age:** every 60 s the writer pipelines `XTRIM <key> MINID ~ <now − 48 h>-0` for the eleven keys; the page also ignores any record whose `t` is older than 48 h.
- **On restart:** nothing is on disk (§4.1).

## 6. The Go client: a minimal RESP2 client

**Decision: `dashboard/resp.go`, written here; no dependency.** `dashboard/go.mod:1-7` keeps the module stdlib-only ("a service with no dependency graph has no dependency graph to audit, patch or wait on"), and the binary that would carry a library holds every GAOS credential. `go-redis` and its peers bring cluster, sentinel, pub/sub and pooling this design does not use.

The contract:

- **Three commands:** `XADD`, `XTRIM`, `XREVRANGE` — nothing else is sent (the healthcheck uses `redis-cli`, not Go). Commands may be pipelined: all written, then exactly as many replies read.
- **RESP2 only** (no `HELLO`), a dial timeout and a per-operation deadline; any I/O or parse error closes the connection and the caller redials.
- **The reply parser is bounded**, because the reply is input: a line ≤ 1 KiB, a bulk string ≤ 64 KiB, an array ≤ 50 000 elements, nesting ≤ 3; anything else is an error that closes the connection. Nothing is allocated from a declared length before the bound is checked. An error reply becomes a Go error holding at most its first 80 bytes.
- **Not safe for concurrent use:** the writer owns one connection; the page reader owns another under a mutex.

## 7. Recording outbound calls

### 7.1 The clients and their labels

| Label | Shown as | Built at | Base for §7.4 | Covers |
|---|---|---|---|---|
| `qa-tool` | QA tool | `qaclient.go:145-150` | `o.base` (`GAOS_QA_URL`) | capabilities, results, catalog (`qaclient.go:73, 273, 358`); automation, jobs, proposals (`qajobs.go:96, 112, 129, 592`); the inbox worker's answers (`inbox_worker.go:51`) |
| `ai-manager` | AI manager | `aimanager.go:131-136`, the default branch | `c.Base` (`GAOS_AIM_URL`) | chat, results, providers (`aimanager.go:170, 182, 246`), questions (`aimanager_questions.go:71`), admin (`aimanager_admin.go:63`) |
| `brain-town` | Brain Town | `braintown.go:292-294` | `BRAIN_TOWN_URL` | the feed's posts (`braintown.go:566`) |
| `brain-town-reader` | Brain Town reader | `braintown_consumer.go:321-328` | `BRAIN_TOWN_CONSUMER_URL` | the stream requests (`braintown_consumer.go:422`) |
| `github` | GitHub | `github.go:179-183` via `main.go:393` | `cfg.base` | the review token's calls |
| `github-write` | GitHub (auto-fix) | the same constructor via `newGHWriter` (`githubwrite.go:162-168`) | `cfg.base` | the write token's calls |
| `whatsapp` | WhatsApp gateway | `wa.go:180-186`, transport `waTransport` (`wa.go:205`) | `cfg.base` (`GAOS_WA_URL`) | group check and sends (`wa.go:291, 374`) |
| `taskmgr` | TaskMgr | `taskmgr.go:233-235` | `c.base` | the gRPC calls (`taskmgr.go:339`) |

**Excluded: ArcadeDB** (`arcade.go:74-75`), GAOS's own store, called on nearly every request; its reachability is on `/api/healthz` (`main.go:706`).

**`github` vs `github-write`.** `ghConfig` (`github.go:108-112`) gains `platform string`; `newGHWriter` (`githubwrite.go:162-168`) sets `cfg.platform = "github-write"` before calling `newGHClient(cfg)`; empty means `github`. The label is therefore right however the writer's config was built, and no signature changes.

**AI manager.** `AIMClient.client()` returns an injected `c.HTTP` as is (`aimanager.go:132-134`) and wraps only the client it builds (`:135`). Production injects none (`modelconfig.go:201`).

### 7.2 The wrap and the one handle

`trafficWrap(label, base string, rt http.RoundTripper) http.RoundTripper` returns `rt` unchanged when recording is off, else a `recordingTransport` around `rt` (`nil` meaning `http.DefaultTransport`, exactly what those clients use now). Each constructor of §7.1 passes its label, its configured base URL and the transport it sets today; client timeouts, `CheckRedirect` and TLS settings are untouched. A label outside the closed set panics at construction (a programming error TC-892 would catch).

**The recorder has one handle:** a package-level `atomic.Pointer[trafficRecorder]`, set in `serve()` right after the signal context (`main.go:195-196`) and before `ArcadeFromEnv` (`main.go:198`), so before any client exists. The gate and the view read the same pointer; `server` gets no field. Tests that turn recording on are not `t.Parallel()` and restore the pointer in `t.Cleanup`; Go runs a package's parallel top-level tests after its sequential ones, so no parallel test sees a test recorder. Because the wrap is a no-op when off, `TestTaskMgrConfig`'s `cl.hc.Transport.(*http.Transport)` (`taskmgr_test.go:409`) still holds. No production code type-asserts a client's transport (in non-test files the only assertion is `arcade.go:62`, on `http.DefaultTransport`). `recordingTransport` delegates `CloseIdleConnections()` when its base has it.

### 7.3 What is read — and what is not

The record is built from exactly: the start time and elapsed time; `req.Method`; `req.URL.Host`; `req.URL.EscapedPath()` (then §7.4); `req.ContentLength`; `resp.StatusCode` and `resp.ContentLength` when there is a response; `errClass(err)` when there is an error. It reads no `req.Header`, `req.Body`, `req.URL.User`, `req.URL.RawQuery`, `req.URL.Fragment`, `resp.Header`, `resp.Body`, `resp.Trailer` or `err.Error()`. The response and the error go back to the caller unchanged and the body is not wrapped, so a streaming response (the Brain Town reader, `braintown_consumer.go:321-323`) is one record, at its headers; latency is time to headers for every platform. `rec.out` never blocks (§9.1).

### 7.4 The path

1. **Relative to the base.** If the client's base URL has a non-empty path and the request's escaped path starts with it, that prefix becomes the literal `{base}`; if it has a path the request does not start with (not expected), the whole path is recorded as `{other}`. An operator-configured base path — which may hold an instance key, and which GAOS today shows only as its host (`main.go:422`, `wa.go:270`) — never reaches a record (SEC-6).
2. **Templated.** Each remaining segment is kept only if it starts with a letter, is ≤ 40 bytes of letters, digits, `.`, `_`, `-`, and holds ≤ 3 digits; an all-digit segment becomes `{n}`; anything else (more digits, a `%`-escape, `@`, `:`, `=`, longer) becomes `{id}`. At most 12 segments; the result is cut at 160 bytes.

Examples (TC-887): base `https://wa.example/inst-Kq7xTzLm` and path `/inst-Kq7xTzLm/send/message` → `{base}/send/message`; `/repos/acme/route-api/pulls/12/files` → `/repos/acme/route-api/pulls/{n}/files`; a 40-hex commit → `{id}`; `/v1/requests/req-7f3c9a2e/result` → `/v1/requests/{id}/result`.

**Why a templated path may be shown when the Integrations page shows only hosts** (`integrations.go:35-38`): Integrations shows configuration, where a path can be a secret; here the configured part is replaced by `{base}`, and what remains is the API's own route shape with identifiers replaced. Owner and repository names stay; the Pull requests page shows them to the same audience (`templates/backoffice.html:1568`). Credentials travel in headers (`aimanager.go:154`, `taskmgr.go:338`) and a WhatsApp number in the body (`wa.go:374`), never in a path, so BR-63 (`brd.html:1309`) holds twice over.

### 7.5 Error class

A closed set, in this order: `timeout` (`context.DeadlineExceeded` or a `net.Error` with `Timeout()`), `canceled`, `dns` (`*net.DNSError`), `refused` (`ECONNREFUSED`), `reset` (`ECONNRESET`, `io.EOF`, `io.ErrUnexpectedEOF`), `tls` (certificate and record errors), else `other`. The error's text is never stored: a `*url.Error`'s text holds the URL and its query. A refused redirect is recorded as the 3xx status the transport returned.

### 7.6 TaskMgr: the gRPC code

TaskMgr's outcome is the `grpc-status` trailer, or a trailers-only header, read after the body (`taskmgr.go:343-354`); an HTTP 200 says nothing. So the TaskMgr client reports the code (**option b**): `taskmgrClient.do` (`taskmgr.go:326`) marks its request context as deferred-recording; for such a request the transport fills the record but does not send it; `do` then sets the code it read (`:349-352`; `-1` when it returned before reading one) and a `defer` sends the record exactly once on every return path. On the page, a TaskMgr call with `g` not `0` and not `-1` counts as **Failed** and shows its code (`gRPC 16`). TC-889 asserts it.

## 8. Recording inbound calls

### 8.1 Where, and what

In `server.gate` (`auth.go:541-590`), for every request the dashboard receives. The gate computes `src` and `via` (§8.3) first, then:

| Request | Record | Stream |
|---|---|---|
| an API call (bearer pattern, or unmatched `/v1/…`), `via = direct` | full (§5.2 "In (API)") | `in:direct` |
| an API call, `via = edge`, a principal set | full | `in:auth` |
| an API call, `via = edge`, no principal | full | `in:anon` |
| any other request, `via = direct` | reduced: `t m s d k src via`, no path | `in:direct` |
| any other request, `via = edge` | none | — |

The route class `k` of a non-API request: `healthz` for `GET /healthz`, `static` for `GET /static/`, `public` for the other `publicPatterns` (`auth.go:516-524`), `unmatched` for an empty pattern outside `/v1/`, else `backoffice`.

For a recorded request the gate creates a tag in the request context, wraps `w` (§8.6), passes the tagged copy `r2 := r.WithContext(…)` to `mux.ServeHTTP` (for the back-office class, after its session value is added as today, `:586`), and in a `defer` calls `rec.in` with the writer's status and bytes, the tag's principal and `r2.PathValue("id")`. `http.ServeMux.ServeHTTP` sets the path values on the request it is given (Go 1.22+; *assumption A5*, asserted by TC-893).

### 8.2 Unmatched `/v1/` paths

An empty pattern is answered by the mux with 404, or 405 for a wrong method on a real path (`auth.go:534-536`, `:544-546`). Both are recorded as `r = (no route)` with their status — never the path, which is caller-controlled.

### 8.3 Source and via

`src` is `jenkinsSource(r)` unchanged (`staging.go:276-297`).

`via` comes from **the hop that delivered the request to Traefik**. Traefik keeps a forwarded chain only from its one trusted peer and appends the delivering peer's address to `X-Forwarded-For`; the dashboard's peer is always Traefik, so the **rightmost** `X-Forwarded-For` entry is the delivering hop. `via = edge` when that entry equals `edgeHop`, else `direct`.

- `edgeHop = "10.10.0.1"` is a constant in `traffic.go`. It is the one address Traefik trusts (`traefik.yml:52-53`); `check-stacks.sh:98` already asserts that entry is single, and the new block (§4.4) asserts the constant equals it, so the two cannot drift.
- *Assumption A6 (confirmed in T2):* Traefik v3.3 with `forwardedHeaders.trustedIPs` keeps a trusted peer's chain and appends the delivering peer, and replaces an untrusted peer's chain with that peer. T2 runs `traefik:v3.3` with `deploy/stacks/traefik.yml` and a one-route echo backend, sends a request from a trusted and from an untrusted address, and records the `X-Forwarded-For` the backend receives in the T2 commit message. If it differs, T2 stops and the design is revised.
- **Fallback, when there is no `X-Forwarded-For` at all** (no Traefik in the path — tests, or a misconfiguration): the address class of `src`. `direct` when it is loopback, private, link-local, unspecified, `unknown`, in `100.64.0.0/10` (the CGNAT range tunnels use, as `wa.go:189-199` already treats it) or in the /24 of `GAOS_TUNNEL_IP`; otherwise `edge`.
- *Stated limit:* the edge's own processes calling GAOS through the edge are `edge`; the edge box calling the VM's port directly (the deploy's health probe, `deploy/rebuild-restart.sh:399`) arrives without the edge's chain, has `10.10.0.1` as its delivering hop, and is therefore classed `edge` with source `10.10.0.1` — it is the edge.

### 8.4 The principal

Set only by the code that authenticates, after authentication:

- `apiGate` (`api_v1.go:316-340`), right after `authorize` (`:329`) when `got != scopeNone`: `admin` for `scopeAdmin`, else `cl.Name` (`api_v1.go:54-55`; the QA tools' principal is `qa-tools`, `answer_source.go:27`). A 403 (`:335-337`) is recorded with the principal that was refused, and in `in:auth`.
- `jenkinsGate` (`staging.go:331-406`), after `a.ok(prodKey)` (`:404`): `basic:` + `a.route` (`staging.go:71-72`); the product is `pid`.

Nothing else sets it. A clients-file name is the operator's own label, cut to 64 bytes. The recorder never reads `Authorization`, a Basic user name or a password.

### 8.5 Floods

There is **no per-source limiter.** An unauthenticated flood through the edge lands in `in:anon` and evicts only `in:anon` (§5.1); it shows up as a flood, which is what a monitoring page is for. Two more bounds keep it from reaching other records:

- **The queue reserve.** A record bound for `in:anon` is enqueued only while the queue is below ¾ full; above that it is dropped and counted as `droppedAnon`. So an anonymous flood cannot fill the queue that outbound, direct and authenticated records share.
- **Direct floods** come from tunnel peers or the VM itself and land in `in:direct`; such a flood is itself the finding the page exists for (residual, §13.5).

### 8.6 The response writer

`trafficWriter` records the first `WriteHeader` status (200 when `Write` comes first; **200 when the handler returns having called neither**, as net/http sends), counts bytes, implements `Unwrap()` so `http.NewResponseController` reaches the real writer (`apiDecodeLimit` sets a read deadline through it, `api_v1.go:398-401`), and `Flush()` when the real writer can. If the handler panics, the deferred `rec.in` records `s = 500` (the gate notes that `ServeHTTP` did not return) and the panic continues to net/http unchanged.

**Residual:** `http.MaxBytesReader(w, …)` (`api_v1.go:402`) tells the server to close the connection after a 413 through an unexported method on net/http's own writer, which a wrapper cannot forward. Behind `trafficWriter`, an over-limit body still gets its 413 and its error, but the connection may be kept open for the next request. The response is unchanged; TC-894 asserts the 413.

## 9. Never blocking, never failing

### 9.1 The hot path

`rec.out` and `rec.in` build one record and `select { case q <- r: default: dropped++ }` on a channel of 4096 (anonymous records: §8.5). No lock the writer holds, no I/O. With recording off they are not reached.

### 9.2 The writer

One goroutine on the signal context: collect ≤ 128 records or 250 ms; dial if needed (500 ms); pipeline the `XADD`s with a 2 s deadline. A dial failure drops the batch, then waits 1 s doubling to 30 s while still draining the queue and counting drops, so a dead Redis never fills the queue for long. An `OOM` or `NOPERM` reply drops that record; an I/O error closes the connection and drops the unacknowledged records. Every 60 s, the eleven `XTRIM`s. On shutdown, queued records are discarded (§12).

### 9.3 Counters and logs

In memory since process start: `written`, `dropped`, `droppedAnon`, and the time of the last write error; the page shows them. The log gets one line per transition: `traffic: the store is not answering (<error class>); records are dropped until it answers` and `traffic: the store answers again; <n> records were dropped meanwhile`. No log line carries a recorded value.

### 9.4 Redis down

Nothing outside this feature changes: no handler, gate or client waits on Redis; `/api/healthz` does not probe it; the dashboard starts without it. TC-890 asserts it.

## 10. The Traffic page

### 10.1 Place, address and access

- **Menu:** **Traffic** after **Integrations** (`templates/backoffice.html:96`) — what is configured, and what it is doing. `pageViews` (`urls.go:31-35`) gains `{"traffic", "Traffic"}`, which gives `GET /traffic` (the loop in `routes()`, `main.go:581-583`) and the pane `GET /ui/bo/traffic` (`main.go:605`).
- **Poll:** `GET /ui/bo/traffic/live`, registered beside `GET /ui/bo/prs/live` (`main.go:608`) and shaped like `handlePRsLive` (`prview.go:489-499`): `requireBO`, `no-store`, `traffic-live` with `OOB = true`.
- **Who may view:** every signed-in back-office account (the gate refuses a signed-out one, `auth.go:568-585`; BR-55). **Full source addresses only for auto-fix admins** (`autoFixAdmin`, as `prViewerIsAdmin` checks it, `prview.go:502-505`); every other account sees a source as its /24 (IPv4) or /48 (IPv6), and the summary groups by what the viewer sees (SEC-9). Nothing on the page can be edited.
- **Caching:** `noStoreView` (`integrations.go:27-31`) adds `"traffic"`; the page route already sends `no-store` (`urls.go:395`).
- `buildBO` (`handlers_bo.go:197`) gains `case "traffic"`; `BOView` (`view.go:247-250`) gains `Traffic *TrafficPage`; the view switch (`templates/backoffice.html:129-131`) gains `bo-traffic`.

### 10.2 What it shows

Header: eyebrow `monitoring`, title **Traffic**, sub "Recent calls GAOS made to other platforms, and calls made to GAOS — at most 48 hours, and each kind of call keeps only its newest records. Nothing here survives a restart of the traffic store."

1. **Status line** (`af-status`, `templates/backoffice.html:1529-1533`): the pulse, "Updated <time> · refreshes every 30 s", the busy text.
2. **Notes** (`af-note`, `role="note"`), only when true: "<n> calls were not recorded since <boot time> (the queue was full or the store did not answer); the figures are lower than the real traffic." and "<n> anonymous API calls were not recorded during a burst."
3. **Tiles** (`af-tiles`, `:1336-1342`): *Outbound calls*; *Outbound failed* (tone `fail` when > 0); *Inbound API calls*; *Direct sources* — distinct sources in `in:direct` (tone `warn` when > 0).
4. **Outbound by platform** — the eight platforms in §7.1's order:

   | Column | Value |
   |---|---|
   | Platform | display name |
   | Host | the most frequent `h`, `+n` with all in `title` |
   | Calls | kept records in the window |
   | 4xx | `400 ≤ s < 500` |
   | Failed | `s ≥ 500`, `s = 0`, or (TaskMgr) `g` not `0`/`-1` |
   | p50, p95 | nearest-rank over records that got a response |
   | Last call | relative, absolute in `title` |
   | Kept since | the oldest kept record's time; "48 h" when that is the window's start, else the time, marked *cap* — the honest reach of the row |

   A platform with no record shows one muted row: "No calls kept".
5. **Inbound by source and route** — rows per (source as the viewer sees it, route-or-class), newest last call first, at most 50, then "<n> more — filter the calls below by source".

   | Column | Value |
   |---|---|
   | Source | the address or prefix, mono; a link that filters the calls (§10.3) |
   | Via | `edge`, or chip **direct** (`chip st-warn`, `title` "Reached GAOS over the tunnel or from its own host, not through the edge") |
   | Client | up to two principals, else "unauthenticated"; "—" for a non-API class |
   | Route | `r`, or the class in brackets (`[backoffice]`, `[healthz]` …) |
   | Calls, Refused (`4xx`), Failed (`≥ 500`), p95, Last call | as above |

6. **Kept records** — one compact line per inbound stream: "Direct: 312 kept since <time> · Authenticated: 2 000 kept since <time> (cap) · Anonymous: …", so an evicted trace is visible.
7. **Recent calls** — newest first, filtered (§10.3), `tablePageSize` (10, `pager.go:15`; D-097) rows a page with the shared `pager` template (`templates/backoffice.html:596-604`). Columns: When; direction (`→` / `←` with sr-only "outbound"/"inbound"); Who (platform, or source + via); Call (method + host + path, or method + route-or-class and principal); Status (chip `st-ok` < 400, `st-warn` 4xx, `st-bad` ≥ 500; the error class for `s = 0`; `gRPC n` for TaskMgr); Time; Bytes (`af-md`, `—` for `-1`).

### 10.3 Filters, paging and the read

| Parameter | Values | Default | Rule |
|---|---|---|---|
| `dir` | `all`, `out`, `in` | `all` | — |
| `platform` | one of the eight labels | none | **a set platform forces `dir=out`** |
| `src` | an address or prefix `netip` parses, compared canonically after the viewer's masking | none | **a set source forces `dir=in`** |
| `status` | `all`, `ok` (`0 < s < 400` and no gRPC failure), `4xx`, `fail` | `all` | — |
| `page` | `pageParam` (`pager.go:40`) | 1 | — |

An invalid value is ignored, never echoed. If both `platform` and `src` are set, **`src` is dropped** and `dir=out` (a query string has no reliable order to say which was set last). The direction tabs (`af-seg`, as `templates/backoffice.html:1541-1543`) show the forced direction as current; platform and status are `select`s in an `af-form` with `hx-trigger="change"` and a `<noscript>` Apply (`:1544-1553`); a set filter shows **Clear filters**. Every link carries `href` and `hx-get` (`TestEveryHtmxLinkHasAPageHref`, `urls_test.go:441-447`). The poll URL carries the filters and the page.

**The read.** One pipelined `XREVRANGE <key> + - COUNT <cap>` per stream (eleven) over the reader connection (dial 500 ms, deadline 2 s, a mutex); each entry parsed strictly (an unknown method, class or via becomes `other`; a non-numeric number field skips the entry); entries older than 48 h dropped; the result cached 15 s for every viewer, and a failed read cached 15 s too. Summaries, masking and the filtered list are computed per request from the snapshot (≤ 13 000 records).

### 10.4 States

| State | When | Text |
|---|---|---|
| Off | `GAOS_REDIS_ADDR` empty or invalid | "Traffic recording is off: <reason>." then "This deployment has no traffic store configured. The docs say how it is set up." (link to `integrations/traffic.html`). No tables. |
| Unavailable | the read failed | `af-note af-note--fail`, `role="alert"`: "The traffic store is not answering. GAOS keeps working; calls made meanwhile are not recorded." and the drop counter. No tables. |
| Empty | read succeeded, no record | the platform table, all rows muted, and "No call recorded in the last 48 hours. Outbound calls appear when GAOS calls a platform; inbound ones when something calls GAOS." |
| Filtered empty | records, none match | "No call matches these filters." + **Clear them** |
| Normal | — | §10.2 |

### 10.5 Live refresh

The `traffic-live` element, exactly as the Pull requests list's (`templates/backoffice.html:1528`) with its own ids:

```html
<div id="trafficLive" class="af-main"{{if .OOB}} hx-swap-oob="true"{{end}} hx-get="{{.PollURL}}" hx-trigger="every 30s [!document.hidden &amp;&amp; !document.getElementById('trafficLive').matches(':focus-within')]" hx-target="#sink" hx-swap="innerHTML" hx-indicator="#trafficPulse">
```

No poll in a hidden tab, none while a filter has focus. A record reaches the page at most 250 ms (§9.2) plus 15 s (§10.3) after its call, well inside a 30 s refresh.

## 11. UI and UX

The operator pointed at `https://aiforui.dev/skills`, the index of Emil Kowalski's design-engineering skills (source `github.com/emilkowalski/skills`). The repository vendors them — `emil-design-eng` (GAOS edition), `review-animations`, `break-ui` — and `.claude/rules/frontend.md` requires loading all three for this work; the dashboard's own mechanics live in `.claude/agents/gaos-frontend.md`. Those are `.claude/` files; the published counterpart of the same rules is the docs authoring guide (`pustaka/docs/guide/authoring.html`). Phase T3 is built by the `gaos-frontend` agent under them. The rules this page follows:

1. **No new motion.** Seen many times a day and refreshed every 30 s: rows do not animate in, counts do not tick. The one moving thing is the existing refresh pulse (state indication). The reduced-motion kill-switch at the end of `static/app.css` applies unchanged.
2. **Numbers are tabular and right-aligned** with the existing rule: `.qa-tbl .r` (`static/app.css:1543`) is widened to `.qa-tbl .r, .af-table .r` — one selector, no copied declaration. Counts use `num` (`render.go:138`, `thousands`); relative times use `agoAt` (`integrations.go:62`) and `relSpan` (`handlers_bo.go:692`), with the absolute time in `title`.
3. **Long values are cut on the server** with `clipEllipsis` (`handlers_bo.go:431`) to 48 runes, full value in `title`; addresses, hosts and paths are mono. No new CSS class.
4. **Colour never carries meaning alone:** every chip shows its code or word; direction is an arrow plus sr-only text. Only existing chips and tile tones (`static/app.css:2000-2002`), so both themes hold.
5. **Empty states say what is true and what happens next** (§10.4).
6. **Hover:** only `af-table`'s existing row hover.
7. **Narrow screens:** Bytes and Host are `af-md`, hidden below 1000 px (`static/app.css:1886`); tables sit in `tm-scroll af-scroll` (`templates/backoffice.html:1557`).
8. **Keyboard:** every filter and link reachable by Tab with the existing focus ring.
9. **No chart:** it would need a new vendored library, which needs the operator's approval.
10. **A `break-ui` pass before T3 is done:** a 100-byte host, a 160-byte path, a 64-byte principal, an IPv6 /64 source and its /48 mask, 0 and 13 000 records, 1 234 567 as a count, every status and error class, `gRPC 16`, `qb = rb = -1`, a platform with nothing kept, a stream at its cap, every state. TC-900 renders the escaping and masking cases.

The T3 review is reported as a `| Before | After | Why |` table.

## 12. Monitoring data is not an event

BR-05 closes the event catalog (`brd.html:1230`); BR-02 says every change emits an event (`:1227`); BR-01 says state lives in the graph and anything in memory must be reconstructible (`:1226`); BR-04 makes events append-only (`:1229`). A traffic record is none of that:

- **It records no change.** A call is an observation; what the call changed is already recorded, as events, by the code that handled it.
- **Nothing reads it to decide anything.** No fold, scheduler, gate or handler reads Redis; the page is its only reader. Deleting every record changes no state and no behaviour.
- **It may be lost** — dropped, evicted, trimmed, erased on restart — which an event never may.
- **It lives outside the graph:** no schema statement, no event type (`dashboard/events.go` keeps its 66), and `check-stacks.sh`'s landing-page figures (`:327-348`) do not move.

BR-66 writes this down.

## 13. Security

### 13.1 Exposure

| Data | Where | Who sees it |
|---|---|---|
| Outbound: platform, host, `{base}`-relative templated path, method, status, timings, sizes, error class, gRPC code | Redis; the page | signed-in accounts |
| Inbound: route or class, method, status, timings, sizes, via, principal, product id | Redis; the page | signed-in accounts |
| Inbound source address | Redis; the page | full: auto-fix admins; /24 or /48: other accounts |
| Counters | dashboard memory; the page | signed-in accounts |

No new public surface: the page and its poll are back-office routes behind the session gate; no JSON export, no API route, nothing on `/healthz`. Redis has no port.

### 13.2 Personal data: source addresses

Kept at most 48 h and at most the stream's cap, nowhere on disk; never written to the dashboard log, the event log, Brain Town, a packet, a notice or the graph; shown in full only to auto-fix admins (§10.1); `no-store`. IPv6 is already a /64 when recorded.

### 13.3 Secrets never recorded

By construction (§7.3, §7.4, §8.4) and by test: TC-888 (outbound, including a secret in the configured base path) and TC-893 (inbound) put known strings in every forbidden place and assert no stored field contains any.

### 13.4 Redis itself

- **Reachability:** only the dashboard, over `gaos-prod-traffic` (`internal: true`, pinned subnet), at a fixed address; no port; no route out. T4 records, from the VM host, `nc -z -w 3 172.30.14.2 6379` and from another container on `gaos-prod-int` the same, both refused or timed out (*assumption A7*: Docker's bridge isolation; if the host can connect, the ACL below is what stands, and the deploy note says so).
- **ACL** (§4.1): even a process that reaches it can only add, trim and read the traffic streams, `PING` and `ACL LIST`; `FLUSHALL`, `DEL`, `XDEL`, `EVAL`, `CONFIG` are `NOPERM`. T4 records `redis-cli ACL LIST` and `redis-cli FLUSHALL` → `NOPERM`. *Residual:* such a process can still add forged records or trim a stream; it already runs beside every GAOS credential (`compose.prod.yaml:208-228`) if it is the dashboard, and it is past Docker's isolation if it is not.
- **No name resolution** (§4.3): `GAOS_REDIS_ADDR` must be an IP literal, so a stopped Redis cannot send records to a host found through outside DNS (SEC-5).
- **Deny-lists:** the host of `GAOS_REDIS_ADDR` joins the hosts the QA client and the Brain Town client refuse as destinations (`qaclient.go:174`, `braintown.go:87`), so a mistyped URL never sends a bearer token to Redis (SEC-8, TC-896).
- **Forged records:** parsed strictly, bounded, escaped by `html/template` (TC-900); no `template.HTML` is built from a record.

### 13.5 Availability and evidence

An anonymous flood evicts only `in:anon` and cannot fill the shared queue (§8.5); direct and authenticated records survive it (TC-895). Memory, queue, reads and the page's cache are bounded (§4.1, §9.1, §10.3). *Residual:* a direct peer that floods evicts older direct records; the flood itself is then the most visible thing on the page.

### 13.6 Supply chain

No Go dependency (§6). One image, pinned by version and digest (§4.1), asserted by `check-stacks.sh`.

### 13.7 What this does not do

It blocks nobody. **Detection is not prevention:** until the recommended firewall is applied, a direct peer keeps reach to every route on the tunnel port. The page is best-effort: it drops under load, evicts per stream, keeps 48 h, and does not see requests Traefik sends to the landing and docs containers (exclusion E1). An empty direct list is not proof that no direct caller exists. D-107 names this residual, and the edge README says it (§17 item 32).

### 13.8 What the published docs say

`integrations/traffic.html` describes what is recorded, `edge`/`direct`, the states, the masking and the exclusions. It does **not** describe Redis' authentication posture, its address or the network names (those stay in `deploy/STACKS.md`), and every example address is from the documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) (SEC-10).

## 14. Registers

- **D-107** (new, 2026-11-27): "GAOS records its outbound calls to external platforms and the calls made to it, and shows them on a back-office Traffic page; reviewing that page is the required step, and the host firewall on the tunnel port becomes recommended hardening. Context: D-106 left the firewall unapplied; the operator, answering that item of the v0.68.0 deploy report on 2026-11-27, wrote that a monitoring page of outbound traffic to the QA tool and the other platforms 'will be enough' (design `docs/designs/traffic-monitor.md` §0, verbatim). Decision: capped Redis streams — one per platform out, and inbound split into direct, authenticated and anonymous — in a Redis only the dashboard reaches, at a fixed address, behind an ACL, with no persistence, keeping at most 48 hours; a stdlib RESP client; a recording transport on the eight external clients (ArcadeDB excluded), TaskMgr reporting its gRPC code; every API call recorded, and every request that reached GAOS directly, with `via` taken from the hop that delivered it to Traefik; never a header, body, query, token, credential or configured base path; recording never blocks or fails a call; any signed-in account may view, full addresses for auto-fix admins only. **Residuals:** detection, not prevention — a direct peer keeps reach to every route until the firewall is applied; the trace is lossy (drops counted, evictions shown), keeps 48 h, and does not see requests served by the landing and docs containers, so an empty direct list is not proof; Redis has no password, its ACL admits three commands on the traffic keys; source addresses are kept for 48 hours."
- **D-106 amended** (one sentence appended to its row, `decision-log.html:2167-2172`): "Since D-107 the host firewall on the tunnel port (edge README §5) is recommended hardening; the required step is reviewing the Traffic page." Its evidence cell gains D-107.
- **BR-66** (new, §9.8 beside BR-52 and BR-55, `brd.html:1340-1358`): "GAOS keeps a short trace of the calls it makes to external platforms and of the calls made to it: every machine-API call, and every request that reached it without passing through the edge. A record holds the time, direction, platform or route, method, the host and a path relative to the platform's configured base with identifiers replaced, status, duration, sizes and an error class, and for an inbound call the source address, whether it came through the edge, the authenticated client's name and the product id — and never a header, a body, a query string, a token, a credential or a configured base path. The trace lives only in the deployment's own Redis, in streams capped per platform and per inbound kind, none older than 48 hours, never persisted. It is not an event and nothing in GAOS reads it to decide anything. Recording never delays or fails a call; a call that cannot be recorded is counted. Any signed-in back-office account may view the trace, with full source addresses for auto-fix admins only; nobody may change it. *(D-107)*"
- **Unchanged:** BR-01, BR-02, BR-04, BR-05 (§12); BR-55 (the new routes are back-office routes); BR-52 (no route, credential or answer changes); BR-63 (§7.4). No open question changes ("All forty-five in one place").
- **QA test plan:** **TC-884 … TC-901** (18 rows: 17 Go tests, one `check-stacks.sh`), section `#go-traffic` under `#implemented`. Existing rows that change:
  - **TC-019** — the quoted message (`qa-test-plan.html:1080-1081`) becomes `unknown service 'bogus-service' -- valid: traefik landing dashboard docs redis`.
  - **TC-725** — the row (`qa-test-plan.html:816`) becomes "Each of the eleven page routes returns the whole document for its view, with its title and no-store, and a product section's pane carries the product and the tab; there are twelve page patterns."; its prose's quoted message becomes `there are %d page patterns, want twelve: ten views, a product section and the redirect`, and the section intro (`:4558`) "the eleven page routes" → "the twelve page routes".
  - **TC-431** — its table gains two entries; its prose (`:3749-3758`) quotes no count and names no route, so it is unchanged.
- **Counts:** cases 883 → **901** ("nine hundred and one"); Go tests 745 → **762** ("seven hundred and sixty-two"), `dashboard/` 720 → **737**; "Sixty-five rules" → "**Sixty-six rules**" (`brd.html:1218`) and "64 business rules" → "**66 business rules**" in the BRD's ToC entry (`pustaka/docs/assets/toc/product.js:36`, already one behind); "One hundred and five entries" → "**One hundred and six entries**" (`decision-log.html:18`; 105 distinct `D-` rows today) and its mirror "One hundred and five decisions" (`toc/product.js:129`).
- **Release v0.69.0**, minor: the changelog and `product.semver` (`toc.js:22`) in one change; `README.md:12` follows.
- **Not added:** an event type, task status, notice kind, label, schema statement, packet kind, docs CSS class or open question.

## 15. Tests

One `func Test…` per register row (`.claude/rules/qa-test-plan.md:129-130`). The fake Redis is a `net.Listen("tcp", "127.0.0.1:0")` server in `resp_test.go` that parses RESP2, records commands and answers from a script; `go test ./...` needs no real Redis.

| TC | Test (file) | Kind | Asserts |
|---|---|---|---|
| TC-884 | `TestRESPRoundTripsTheTrafficCommands` (`resp_test.go`) | Positive | `XADD … MAXLEN ~ n *`, `XTRIM … MINID ~`, `XREVRANGE … COUNT` encode byte-exact; simple, error, integer, bulk, null-bulk and nested-array replies parse; a pipeline of 128 returns 128 replies in order |
| TC-885 | `TestRESPRefusesMalformedOrOversizedReplies` (`resp_test.go`) | Negative | a line > 1 KiB, a bulk > 64 KiB, an array > 50 000, nesting > 3, a negative or non-numeric length, a truncated reply, an unknown type byte: each an error that closes the connection, with nothing allocated from the declared length |
| TC-886 | `TestTrafficSettings` (`traffic_test.go`) | Positive, negative | `GAOS_REDIS_ADDR` empty → off; `172.30.14.2:6379` → on; `redis:6379`, `192.0.2.4`, `x:y` → off with the reason; `GAOS_TUNNEL_IP` valid and invalid; the caps and window are the §5 constants |
| TC-887 | `TestOutboundPathIsTemplated` (`traffic_test.go`) | Positive, negative | the §7.4 examples; `{base}` for a base path and `{other}` for a mismatch; 13 segments; 300 bytes; `%2F`; an e-mail; a phone-number segment; an empty path; never a query or fragment |
| TC-888 | `TestRecordingTransportKeepsNoSecret` (`traffic_test.go`) | Negative | base `https://h/inst-Kq7xTzLm`; a request with `Authorization: Bearer <s1>`, userinfo `<u>:<s2>`, `?token=<s3>`, `#<s4>`, body `<s5>`, response header and body `<s6>`; no stored field contains any of `s1`–`s6` or `inst-Kq7xTzLm`; the fields are exactly the outbound set; the caller's response is unchanged |
| TC-889 | `TestRecordingTransportClassifiesErrors` (`traffic_test.go`) | Positive, negative | timeout, canceled, refused, unknown-CA TLS, reset-mid-headers → their classes with `s = 0` and no error text; a fake gRPC server answering `grpc-status: 16` in the trailer → one TaskMgr record with `s = 200`, `g = 16`, counted Failed; a trailers-only `grpc-status: 5` → `g = 5`; a transport error → `g = -1`; exactly one record per call |
| TC-890 | `TestTrafficRecordingNeverBlocks` (`traffic_test.go`) | Negative | with a store that accepts and never answers, `4096 + 1000` records enqueue from one goroutine without blocking (the loop ends before a 5 s guard) and `dropped` rises by exactly 1000; with a store that refuses, 300 wrapped calls to an `httptest` server all succeed and `dropped` counts every record |
| TC-891 | `TestTrafficWriterTrimsByLengthAndAge` (`traffic_test.go`) | Positive | each `XADD` goes to its stream with that stream's `MAXLEN ~ <cap>`; a trim tick sends eleven `XTRIM … MINID ~ <now − 48 h>-0`; an `OOM` or `NOPERM` reply counts one drop; a lost connection is redialled with backoff and the two transition lines are logged once each |
| TC-892 | `TestEveryExternalClientIsRecorded` (`traffic_test.go`) | Positive | with recording on, the clients of `newQaOutbound`, `AIMClient.client` (default branch; an injected `HTTP` returned as is), `newBrainTown`, `newBTConsumer`, `newGHClient`, `newGHWriter` (built from a bare `ghConfig`), `newWANotifier`, `newTaskMgrClient` carry a recording transport with the §7.1 label and base; the Arcade clients do not; with recording off, each has today's transport |
| TC-893 | `TestInboundRecordsRouteSourceAndClient` (`traffic_test.go`) | Positive, negative | through `routes()`: a QA-token call with `X-Forwarded-For: 203.0.113.9, 10.10.0.1` → `in:auth`, pattern not path, `c = qa-tools`, `pid`, `via = edge`; the same from `X-Forwarded-For: 10.10.0.9` → `in:direct`; no XFF and source `100.64.1.2` → direct; no XFF and a source in the `GAOS_TUNNEL_IP` /24 → direct; a wrong token → `in:anon`, `s = 401`; a Basic unit-results call → `basic:unit-results`; `/v1/nope?x=<secret>` → `(no route)`, 404, and `DELETE /v1/products` → `(no route)`, 405, neither with a path; a direct `GET /healthz` and a direct back-office request → reduced records `k = healthz` / `backoffice`; the same through the edge → nothing; no field holds the bearer token, the Basic password or the query |
| TC-894 | `TestInboundRecorderKeepsTheResponseController` (`traffic_test.go`) | Positive | through the wrapped writer `apiDecodeLimit`'s `SetReadDeadline` succeeds; an over-limit body still answers 413; `Flush` reaches the recorder; a handler that writes nothing records 200; a handler that panics records 500 and the panic reaches net/http |
| TC-895 | `TestAnAnonymousFloodEvictsNoDirectOrAuthenticatedRecord` (`traffic_test.go`) | Negative | after one direct and one authenticated record, 10 000 anonymous API calls from 5 000 sources: `in:direct` and `in:auth` still hold their records; `in:anon` holds its cap; with the writer stalled the anonymous records stop at ¾ of the queue and an outbound record still enqueues |
| TC-896 | `TestOutboundURLsRefuseTheTrafficStore` (`traffic_test.go`) | Negative | with `GAOS_REDIS_ADDR=172.30.14.2:6379`, `GAOS_QA_URL=http://172.30.14.2:6379` and a Brain Town URL on that host are refused with the existing "names a host GAOS already uses" reason |
| TC-897 | `TestTrafficSummaries` (`traffic_view_test.go`) | Positive | per-platform calls, 4xx, failed (incl. TaskMgr `g`), nearest-rank p50/p95 over answered calls, last call, kept-since and *cap*; the eight platforms in order; inbound pairs newest first, capped at 50 with the "more" count; records older than 48 h not counted; the direct-sources tile; the kept-records line |
| TC-898 | `TestTrafficFiltersAndPages` (`traffic_view_test.go`) | Positive, negative | each filter as §10.3; `platform` forces `out`, `src` forces `in`, both set drops `src`; the tabs show the forced direction; invalid values ignored and not echoed; 10 a page; the poll URL keeps the filters |
| TC-899 | `TestTrafficPageStates` (`traffic_view_test.go`) | Positive, negative | Off, Unavailable, Empty, Filtered empty, Normal render their texts; page and poll answer `no-store`; signed out both are refused (401) and touch no store; a failed read is not retried within 15 s |
| TC-900 | `TestTrafficPageEscapesAndMasksAddresses` (`traffic_view_test.go`) | Negative | host, path, route, principal and source holding `<script>`, `"><img onerror=…>` and a 2 KiB string render escaped and clipped; a non-admin sees `203.0.113.0/24` and `2001:db8:1::/48`, an auto-fix admin the full source; a non-admin `src` filter on a full address matches nothing |
| TC-901 | `check-stacks.sh` | Positive, negative | the ten `ok` lines of §4.4; each prints `FAIL` with the same message when its condition is broken by hand in a scratch checkout (§4.4) |

Existing tests whose assertions move: `TestADeepURLRendersThePage` (`urls_test.go:233-234`, eleven → twelve) and `TestEveryRouteIsClassified` (`auth_test.go:259-292`, two `readOnlyGET` entries). `TestEveryHtmxLinkHasAPageHref` (`urls_test.go:441-447`) iterates `pageViews` and needs no edit.

## 16. Phases

Each phase ends with `(cd dashboard && go vet ./... && go test ./... && go test -race ./...)`, `(cd pustaka && go run . check ./docs && go test ./...)`, `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` green, and writes its own register rows and every count in the same commit: `qa-test-plan.html`, `pustaka/docs/assets/toc/product.js`, `deploy/check-workspace.sh`, `README.md`, `.claude/rules/qa-test-plan.md`.

| Phase | Builds | TC |
|---|---|---|
| T1 Client and recorder | `resp.go`; `traffic.go` without wiring: settings, streams, queue, writer, trims, `recordingTransport`, `{base}` and template, error class, the deferred TaskMgr record, the inbound writer and classes | TC-884 … TC-891 |
| T2 Wiring | assumption A6 run first; the pointer set in `serve()`; the eight wraps, `ghConfig.platform` set in `newGHWriter`, the TaskMgr code report; the gate; the principals; the two deny-lists | TC-892 … TC-896 |
| T3 Page (`gaos-frontend` agent) | `traffic_view.go`; `pageViews`, `noStoreView`, `buildBO`, `BOView`, the poll route; the templates and the nav item; the widened selector; `readOnlyGET`; the `urls_test.go` count; TC-725 text | TC-897 … TC-900 |
| T4 Deploy, docs, release | assumptions A1–A3 checked first; the compose service, network, address and settings and the header comments; `check-stacks.sh`; `rebuild-restart.sh` and the deploy agent's list; `STACKS.md`; `infra/environments.html` and `toc/infra.js`; the edge README; `integrations/traffic.html` and `toc/integrations.js`; D-107, the D-106 amendment, BR-66 and their counts; the changelog, `semver`, `README.md:12` | TC-901; TC-019 text |

**Deploy** (after approval and merge): tag `v0.69.0`; `./deploy/rebuild-restart.sh prod deploy --tag v0.69.0` on each prod instance with its usual environment line. Per instance, the deploy note records: the image digest; `docker compose -f deploy/stacks/compose.prod.yaml ps` shows `redis … (healthy)` and no port; `redis-cli ACL LIST` and `redis-cli FLUSHALL` → `NOPERM`; the two reachability checks of §13.4; Traefik's `X-Forwarded-For` as A6 found it; a QA sync appears under QA tool within a minute; `docker compose … stop redis`, a QA sync and an API call still succeed and the page says Unavailable; `start redis`. After 24 hours the firewall cross-check of §0, with each peer and its explanation.

## 17. Change list

New files (7):

1. `dashboard/resp.go` — §6.
2. `dashboard/resp_test.go` — TC-884, TC-885, the fake server.
3. `dashboard/traffic.go` — §5, §7, §8, §9.
4. `dashboard/traffic_test.go` — TC-886 … TC-896.
5. `dashboard/traffic_view.go` — §10.
6. `dashboard/traffic_view_test.go` — TC-897 … TC-900.
7. `pustaka/docs/integrations/traffic.html` — the page, what is and is not recorded, `edge`/`direct`, masking, the exclusions, the states, that a Redis or whole-stack restart empties it; documentation address ranges only, no Redis posture or network names (§13.8); built from `pustaka/ai/AUTHORING_SPEC.md`'s existing components.

Changed files (items 8–44; item 33 is two files, so 38 files):

8. `dashboard/main.go` — the pointer and the writer set after `:195-196`, before `:198`; `GET /ui/bo/traffic/live` beside `:608`.
9. `dashboard/auth.go` — the recorder in `gate` (`:541-590`).
10. `dashboard/api_v1.go` — the principal in `apiGate` (`:329-339`).
11. `dashboard/staging.go` — the principal in `jenkinsGate` (`:404`).
12. `dashboard/qaclient.go` — wrap at `:145`; the Redis host in the deny-list at `:174`.
13. `dashboard/aimanager.go` — wrap the default branch of `client()` (`:135`).
14. `dashboard/braintown.go` — wrap at `:292`; the Redis host in the deny-list at `:87`.
15. `dashboard/braintown_consumer.go` — wrap at `:323`.
16. `dashboard/github.go` — `ghConfig.platform` (`:108-112`); wrap at `:180`.
17. `dashboard/githubwrite.go` — `newGHWriter` sets the platform (`:162-168`).
18. `dashboard/wa.go` — wrap at `:182`.
19. `dashboard/taskmgr.go` — wrap at `:233`; the deferred record and the code report in `do` (`:326-354`).
20. `dashboard/urls.go` — `pageViews` (`:31-35`).
21. `dashboard/integrations.go` — `noStoreView` (`:28`).
22. `dashboard/handlers_bo.go` — `buildBO` case (`:254-265`).
23. `dashboard/view.go` — `BOView.Traffic` (`:247-250`).
24. `dashboard/templates/backoffice.html` — nav item (`:96`), view switch (`:129`), `bo-traffic`, `traffic-live`.
25. `dashboard/static/app.css` — the selector at `:1543`.
26. `dashboard/auth_test.go` — `readOnlyGET` (`:259-292`).
27. `dashboard/urls_test.go` — the count (`:233-234`).
28. `deploy/stacks/compose.prod.yaml` — §4.1–§4.3, the header comments (`:9-13`, `:25`).
29. `deploy/check-stacks.sh` — §4.4.
30. `deploy/rebuild-restart.sh` — `:15`, `:26`.
31. `deploy/STACKS.md` — the service, the network, the address, the ACL and the no-password posture.
32. `deploy/edge/README.md` — §5 (`:170-209`): the heading's "an operator step" becomes "recommended hardening (D-107)", and under it: "Since v0.69.0 the required step is reviewing the back office's **Traffic** page, which records every request that reaches GAOS's dashboard without passing through this box, and every machine-API call. It is best-effort detection, not a control: it drops records under load, keeps each kind of call only up to its cap and at most 48 hours, and does not see requests Traefik sends to the landing and docs containers. **An empty direct list is not proof that no direct caller exists.** Until the rules below are applied, a direct peer can reach every route on the port; applying them remains recommended." The "mandatory and recorded" check (`:202`) applies when the rules are applied.
33. `pustaka/docs/infra/environments.html` (`:17`, `:30`) and `pustaka/docs/assets/toc/infra.js` (`:41`) — prod's five services, Redis named as the traffic store, no address or posture.
34. `pustaka/docs/assets/toc/integrations.js` — the page's entry after `pull-requests` (`:96-107`).
35. `pustaka/docs/product/qa-test-plan.html` — 18 rows, `#go-traffic`, TC-019 and TC-725 texts, the counts.
36. `pustaka/docs/product/brd.html` — BR-66; "Sixty-six rules" (`:1218`).
37. `pustaka/docs/product/decision-log.html` — D-107; the D-106 amendment (`:2167-2172`); "One hundred and six entries" (`:18`).
38. `pustaka/docs/assets/toc/product.js` — "66 business rules" (`:36`); "One hundred and six decisions" (`:129`); the case and Go-test counts (`:162`, `:168`).
39. `pustaka/docs/changelog.html` — v0.69.0.
40. `pustaka/docs/assets/toc.js` — `semver: "0.69.0"` (`:22`).
41. `deploy/check-workspace.sh` — `762`, "seven hundred and sixty-two", "nine hundred and one", "One hundred and six entries", "Sixty-six rules" (`:113-133`).
42. `README.md` — "Product version 0.69.0" (`:12`); 762 / 737 / 901 (`:143-145`).
43. `.claude/rules/qa-test-plan.md` — highest TC-901, next TC-902, the counts, the range map (`check-stacks.sh` gains TC-901; `dashboard (go test)` gains TC-884 … TC-900).
44. `.claude/agents/gaos-deploy.md` — the service list (`:38`) gains `redis`.

**Arithmetic.** 7 new + 38 changed (37 items, item 33 being two files) = **45 files**. Go tests: 17 new `func Test…` (TC-884 … TC-900), all in `dashboard/`: 745 + 17 = **762**, 720 + 17 = **737**. Register rows: 17 + 1 (TC-901) = 18: 883 + 18 = **901**.

The files the problem names: `compose.prod.yaml` (28), `check-stacks.sh` (29), `STACKS.md` (31), `api_v1.go` (10), the unit-results gate (11), the eight clients (12–19), `arcade.go` (unchanged, §7.1), the edge README (32).

## 18. Alternatives

1. **An in-memory ring buffer in the dashboard.** No new service. Rejected: the dashboard is replaced on every release, and an empty page after each deploy cannot answer "who called yesterday".
2. **That ring buffer, snapshotted to a mounted directory on SIGTERM and loaded at boot** (the pattern `prod.bt-state` uses, `compose.prod.yaml:219`). No image, network, ACL or RESP parser. Rejected for two reasons: the operator named Redis streams for exactly this ("you can use redis stream just to limit last N record only and do auto retention", §0), and a snapshot taken on SIGTERM loses everything since the last one on a SIGKILL, an OOM kill or a crash — the moments an operator most wants the trace — while Redis, a separate process, keeps it through any dashboard death.
3. **ArcadeDB.** Rejected: it is the shared store of record reached over the tunnel; a write per call adds load and latency to every call path, retention needs a pruning job and schema statements, and records would drift toward events, which BR-04 and BR-05 forbid (§12).
4. **Traefik's access log as the inbound source.** It sees every request, including landing and docs (exclusion E1). Rejected as the page's source: Traefik logs to its container's stdout (`deploy/stacks/traefik.yml:66`), so the dashboard could read it only through the Docker socket — which `check-stacks.sh:92-94` forbids in this stack — or through a new shared log volume and a parser; it has no principal and no outbound half. It stays the operator's manual cross-check for E1 (§0).
5. **Prometheus and Grafana.** Rejected: metrics aggregate, while the page needs per-call rows with addresses (high-cardinality labels); two more services and a second UI with its own sign-in; the Go client is a dependency.
6. **`go-redis`.** Rejected by the module's policy (§6).
7. **Valkey.** Equivalent here (same protocol and commands), BSD-licensed. Not chosen because the operator asked for Redis and its licence imposes nothing on an unmodified, unlinked service; switching is an image line.
8. **A per-source limiter on inbound** (rev 1). Removed: at 300 a minute one source still evicted a 5 000 cap in 17 minutes, and many sources made it moot (round 1); separate streams protect what matters with less machinery (§8.5).

## 19. Non-goals

- Blocking or rate-limiting anyone (§13.7).
- Alerts or notices about traffic.
- History beyond 48 hours, persistence, export.
- ArcadeDB calls; requests Traefik serves from the landing and docs containers; requests through the edge to anything but the API.
- Bodies, headers, query strings, configured base paths — ever (BR-66).
- Per-product access scoping; editing or clearing the trace.
- Redis on the retired dev stack or the local `compose.yaml`.
- Settings for the caps and the window.
- A chart or a new vendored library.
- Changing any client's timeout, retry, redirect or TLS behaviour.

## 20. Assumptions, labelled

| # | Assumption | Checked |
|---|---|---|
| A1 | Redis 8.10.3's release date and the `8.10.3-alpine` digest are as §4.1 states | T4: `docker buildx imagetools inspect` |
| A2 | The alpine image's `redis` user is uid 999, gid 1000 | T4: `docker run --rm <image> id redis` |
| A3 | `172.30.14.0/29` is free on both prod VMs | T4: the network inspection of §4.2 |
| A4 | Inbound authenticated calls are seconds to minutes apart | after deploy, on the page (`in:auth` kept-since) |
| A5 | `ServeMux.ServeHTTP` sets `PathValue` on the request it is given | TC-893 |
| A6 | Traefik v3.3 appends the delivering peer to `X-Forwarded-For` and drops an untrusted peer's chain | T2, before any wiring (§8.3) |
| A7 | Docker's bridge isolation keeps the VM host and other containers off the traffic network | T4: §13.4 checks; the ACL stands either way |

## 21. Round-1 triage

Severities are the reviewers'. Every finding is triaged; there are no contests.

### Comprehension (`traffic-monitor-comprehension-1.md`, B=0 M=4 N=16)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The compose comment trips the design's own "no depends_on" check | Major | PROPOSE FIX | Comment reworded ("no start-order dependency") and the assertion is `hasntre '^[[:space:]]*depends_on:'` (§4.1, §4.4) |
| The success criterion's conntrack check cannot pass as written | Major | PROPOSE FIX | Restated: every peer but `10.10.0.1` is a `direct` source or a named exclusion E1–E3; direct requests on every route are recorded (§0, §8.1); README note reworded (§17 item 32) |
| The 5000 cap binds long before 48 h, and the page does not say so | Major | PROPOSE FIX | A stream per platform; volume estimate (§5.1); "Kept since" per row and a kept-records line (§10.2) |
| TaskMgr failures recorded as HTTP 200 successes | Major | PROPOSE FIX | Option (b): the client reports the gRPC code (§7.6); TC-889 case |
| Glossary "Drop" contradicts `suppressed` | Nit | PROPOSE FIX | The limiter and `suppressed` are gone; "Drop" and "Evicted" defined (§1) |
| "Window" used in three senses | Nit | PROPOSE FIX | "Retention window" only; the minute bucket is gone; §10.5 reworded |
| Edge/direct rule differs between glossary and §8.3; one-sided limit | Nit | PROPOSE FIX | `via` from the delivering hop; one definition; CGNAT and the tunnel /24 in the fallback; the limit stated (§1, §8.3) |
| Unmatched `/v1/` paths are not only 404s | Nit | PROPOSE FIX | 405 recorded under `(no route)` (§8.2); TC-893 case |
| Several anchors off by one or overstated | Nit | PROPOSE FIX | `main.go:605`, `:608`; `taskmgr.go:339`; the grep claim scoped to non-test files; `staging.go:276-297` |
| README version line missing from the change list | Nit | PROPOSE FIX | §17 item 42 includes `:12` |
| check-stacks assertions cannot be built from the helpers named | Nit | PROPOSE FIX | `svcblock`, `inblock`, `inblockre`, `notinblock(re)` defined; the network check is a block check; joined conditions are one `if` (§4.4) |
| Some claims cannot be checked; two unanchored | Nit | PROPOSE FIX | Image facts are A1 with a T4 re-read; the two-week rule is labelled as this design's instruction; a published anchor added for the UI rules (§11) |
| A whole-stack restart also empties Redis | Nit | PROPOSE FIX | §4.1 Persistence row and the docs page (§17 item 7) |
| The Off state tells the operator to set a fixed value | Nit | PROPOSE FIX | The text points at the docs and the deployment (§10.4) |
| Unstated whether an injected AI-manager client is wrapped | Nit | PROPOSE FIX | Only the default branch (§7.1); TC-892 |
| `github-write` label from the config function, not the constructor | Nit | PROPOSE FIX | Set in `newGHWriter` (§7.1); TC-892 builds from a bare config |
| TC-890's timing assertion likely flaky | Nit | PROPOSE FIX | Counter-based (§15) |
| The page renders paths, which Integrations never does | Nit | PROPOSE FIX | Reason stated, and the configured base path is never recorded (§7.4) |
| The live fragment omits the OOB swap | Nit | PROPOSE FIX | Exact attribute string (§10.5) |
| What the shared `other` bucket stores is undefined | Nit | PROPOSE FIX | The limiter and its bucket are removed (§8.5) |

### Zoom-out (`traffic-monitor-zoom-out-1.md`, B=1 M=3 N=6)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The firewall demotion rests on a decision the quoted request does not contain | Blocker | PROPOSE FIX | The operator's full reply, including the firewall item it answers, quoted verbatim with its date (§0); the firewall is recommended hardening, not optional (D-107, §17 item 32); D-106 amended, not "unchanged" (§14) |
| The inbound recorder watches the wrong place; the success check fails | Major | PROPOSE FIX | Every direct request recorded on any route (§8.1); E1 (landing, docs) named and explained by the Traefik log; success check restated (§0) |
| One shared outbound stream lets busy platforms push quiet ones out | Major | PROPOSE FIX | `gaos:traffic:out:<label>`, 1 000 each; the call-rate estimate (§5.1) |
| The per-source limit does not protect what it is meant to | Major | PROPOSE FIX | Deleted; `in:anon` separated from `in:direct` and `in:auth` (§5.1, §8.5); TC-895 is the flood test |
| Two of five Redis commands have no caller | Nit | PROPOSE FIX | `PING` and `XLEN` removed from the client; three commands (§6) |
| A record-format version on short-lived data | Nit | PROPOSE FIX | `v` dropped (§5.2) |
| Existing helpers and CSS not named for reuse | Nit | PROPOSE FIX | `agoAt`, `relSpan`, `clipEllipsis`, `num`; `.qa-tbl .r` widened instead of copied (§11) |
| Two handles to the same recorder | Nit | PROPOSE FIX | The package pointer only; no `server` field (§7.2) |
| Cap and window are settings while the flood limit is a constant | Nit | PROPOSE FIX | Both constants; only `GAOS_REDIS_ADDR` (and the passed-through `GAOS_TUNNEL_IP`) remain (§4.3) |
| Too much low-level detail | Nit | PROPOSE FIX | §6 is a contract, not signatures; §7.4 prose, no regex; CSS declarations removed. The check-stacks one-liners stay, because two readiness findings asked for them |

### Critic (`traffic-monitor-critic-1.md`, B=1 M=4 N=9)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| D-107 makes the firewall optional based on a page that cannot see most direct callers | Blocker | PROPOSE FIX | Option (a): every direct request on any route, reduced record (§8.1); D-107 names the coverage and the residual (§14, §13.7) |
| One stream per direction lets the noisiest producer push everything out | Major | PROPOSE FIX | Per-platform outbound; inbound by direct, auth, anon; "Kept since" (§5.1, §10.2). Per-hour counters not adopted: the per-stream reach shown on the page answers the honesty problem without a second store |
| check-stacks would fail on the design's own compose comment | Major | PROPOSE FIX | As above (§4.1, §4.4); the comment case is in TC-901's negative procedure |
| `via` guessed from the address class | Major | PROPOSE FIX | From the rightmost `X-Forwarded-For` hop vs `edgeHop`, asserted equal to Traefik's trust entry (§8.3, §4.4); A6 in T2; TC-893 cases |
| The success criterion fails as written | Major | PROPOSE FIX | Restated with E1–E3 (§0) |
| `noeviction` mitigation claim wrong | Nit | PROPOSE FIX | The claim is removed (§4.1) |
| Anchors and boot-order wording | Nit | PROPOSE FIX | `main.go:608`; "before `ArcadeFromEnv` (`main.go:198`)" (§7.2) |
| `traffic-live` attributes leave out OOB and `&amp;&amp;` | Nit | PROPOSE FIX | Quoted exactly (§10.5) |
| Leftover undecided or unused items (`XLEN`, TC-431) | Nit | PROPOSE FIX | `XLEN` removed; TC-431's prose checked: unchanged (§14) |
| The dev parity comment and the shared service list | Nit | PROPOSE FIX | Comment amended (§4.2); `dev … redis` failing in compose accepted because dev is retired (§4.5) |
| The network assertion needs an unspecified helper | Nit | PROPOSE FIX | Block helpers (§4.4) |
| The wrapped writer loses `MaxBytesReader`'s close-after-reply | Nit | PROPOSE FIX | Stated as a residual (§8.6); TC-894 asserts the 413 |
| TC-890 wall-clock under `-race` | Nit | PROPOSE FIX | Counter-based, a few hundred calls (§15) |
| Alternative 1 skips its strongest form | Nit | PROPOSE FIX | The snapshot variant with its reasons: the operator named Redis, and SIGKILL loss (§18 alternative 2) |

### Readiness (`traffic-monitor-readiness-1.md`, B=0 M=4 N=9)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The Redis comment fails its own "no depends_on" check | Major | PROPOSE FIX | Both: comment reworded and `hasntre` key-only assertion (§4.1, §4.4) |
| The conntrack success check fails by construction | Major | PROPOSE FIX | §0 restated; README and D-107 carry the limit |
| TaskMgr failures recorded as successes | Major | PROPOSE FIX | Option (b), client-reported gRPC code (§7.6); TC-889 |
| Platform and source filters undefined with `dir=all` | Major | PROPOSE FIX | `platform` forces `out`, `src` forces `in`, both set drops `src`, tabs reflect it (§10.3); TC-898 |
| `server.traffic` appears only in the change list | Nit | PROPOSE FIX | Dropped; one handle (§7.2) |
| `AIMClient.client()` has two branches | Nit | PROPOSE FIX | Default branch only (§7.1) |
| `github-write` label depends on the config path | Nit | PROPOSE FIX | Set in `newGHWriter` (§7.1) |
| A handler that writes nothing gets status 0 | Nit | PROPOSE FIX | 200; `rec.in` deferred; a panic records 500 (§8.6); TC-894 |
| TC-725's new text stated wrongly | Nit | PROPOSE FIX | Exact row and message quoted (§14) |
| One count mirror and one service list missing | Nit | PROPOSE FIX | `toc/product.js:36` and `.claude/agents/gaos-deploy.md:38` added (§17 items 38, 44) |
| Line anchors drifted | Nit | PROPOSE FIX | `main.go:605/608`; `braintown.go:566`, `braintown_consumer.go:422`, `taskmgr.go:339`, `wa.go:291/374`, `wa.go:182` (§7.1, §17) |
| TC-900's negative half has no stated way to run | Nit | PROPOSE FIX | By hand in a scratch checkout, recorded in TC-901's prose (§4.4) — the row is TC-901 in rev 2 |
| A plain `restart` now wipes the trace | Nit | PROPOSE FIX | §4.1 and the docs page |

### Security (`traffic-monitor-security-review-1.md`, Critical=0 High=1 Medium=5 Low=4)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| SEC-1 An unauthenticated caller can wipe the inbound trace | High | PROPOSE FIX | `in:anon` is its own stream; direct and authenticated records cannot be evicted by it; anonymous records stop at ¾ of the queue; oldest kept shown per stream (§5.1, §8.5, §10.2); TC-895 |
| SEC-2 Direct callers of non-API routes are invisible | Medium | PROPOSE FIX | Reduced record for every direct request on any route class (§8.1); E1 named (§0) |
| SEC-3 The edge README is downgraded on a contradicted claim | Medium | PROPOSE FIX | Firewall stays recommended; the note says best-effort, lossy, E1, "an empty direct list is not proof" (§17 item 32); D-107 residual (§14) |
| SEC-4 Redis takes every command; host reach unverified | Medium | PROPOSE FIX | ACL on `default`, no password, three commands plus `ping` and `acl list` on `gaos:traffic:*`, asserted by check-stacks; T4 records `ACL LIST`, `FLUSHALL → NOPERM` and the host reach (§4.1, §13.4) |
| SEC-5 `redis:6379` can resolve through outside DNS | Medium | PROPOSE FIX | Pinned subnet, static `172.30.14.2`, `GAOS_REDIS_ADDR` must be an IP literal; asserted (§4.2, §4.3, §4.4); TC-886 |
| SEC-6 The path template keeps configured base-path segments | Medium | PROPOSE FIX | Path relative to the client's base, as `{base}`, then templated (§7.4); TC-888 extended |
| SEC-7 `via` inferred from the address class | Low | PROPOSE FIX | Delivering hop first; fallback includes 100.64/10 and the `GAOS_TUNNEL_IP` /24 (§8.3); TC-893 |
| SEC-8 The Redis host is not in the outbound deny-lists | Low | PROPOSE FIX | Added to both (§13.4); TC-896 |
| SEC-9 Source addresses shown to every account | Low | PROPOSE FIX | Full for auto-fix admins, /24 and /48 for others (§10.1); TC-900 |
| SEC-10 Published docs would describe Redis' posture and topology | Low | PROPOSE FIX | Posture, address and network names kept to `deploy/STACKS.md`; documentation ranges in examples (§13.8) |
