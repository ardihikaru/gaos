# Traffic monitor — GAOS's outbound and inbound calls, on one back-office page

> **Status:** In Review (rev 1, 2026-11-27). Not reviewed yet; nothing outside `docs/designs/` changes until the five lenses (and the security lens, which this design triggers: it touches the back-office surface, a new service on the deployment and the machine API's gate — `docs/rules/gaos-design-review.md:92-100`) have read it and the operator has approved it.
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/release-b4-unit-tests.md` (D-106: the edge-set source address this design reuses, and the host-firewall step it replaces), `docs/designs/integrations-status-chips.md` (the Integrations page, the nearest existing read-only operations view), `docs/designs/gaos-pr-overview.md` (the Pull requests page whose live list this page copies).
> **Paths.** A bare file name (`staging.go`) is in `dashboard/`; every other path is from the repository root. Anchors are against `main` at v0.68.0 (`fc0f9c3`).
> **Registers (verified on `main` at `fc0f9c3`):** next free **D-107** (the decision log ends at `d-106`, `pustaka/docs/product/decision-log.html:2167`), **BR-66** (the BRD ends at BR-65, `pustaka/docs/product/brd.html:1310`), **TC-884** (`.claude/rules/qa-test-plan.md:50-51`; the register's highest row is TC-883, `pustaka/docs/product/qa-test-plan.html:974`). Release **v0.69.0** (`pustaka/docs/assets/toc.js:22` is `0.68.0`).
> **Private names.** This document and every file it changes name the QA platform only as "the QA tool" and the public proxy only as "the edge" (D-085).

## 0. Why, and the scope

**The operator's request** (2026-11-27, verbatim):

> "as long as we have dashboard / monitoring page in GAOS to view traffic outbound to QA tool (and other external platform), will be enough. add them if there is none yet. use `https://aiforui.dev/skills` for better UI/UX. you can use redis stream just to limit last N record only and do auto retention. we dont need a long period of traffic monitoring data anyway. since it will be used only for periodic monitoring."

**The context.** D-106 left one manual step: a host firewall on each GAOS VM's tunnel port, so that only the edge and the known direct callers reach Traefik (`deploy/edge/README.md:170-209`). It is "an operator step … not part of any automated deploy" (`deploy/edge/README.md:172`), its first instruction is to *sample who calls the port* with `conntrack` (`:186-187`), and nothing in GAOS shows that today. The operator prefers visibility to the firewall: a page that shows who calls GAOS, and whom GAOS calls, is "enough". So the page must show both directions:

- **Outbound** — every call GAOS makes to an external platform (the QA tool and the others in §7.1).
- **Inbound** — every call made to GAOS's machine API (`registerAPI`, `api_v1.go:429-456`), including the two Basic routes (`api_v1.go:442-448`), with **the source address** and whether it came **through the edge or directly** over the tunnel — the exact question the firewall step was about.

**Verified facts this design starts from.**

1. GAOS has no traffic or telemetry page: the back-office views are the nine in `urls.go:31-35`, none of them about traffic.
2. GAOS runs no Redis: the prod stack is `traefik`, `landing`, `dashboard` and `docs` (`deploy/stacks/compose.prod.yaml:31-258`; `deploy/rebuild-restart.sh:26`).
3. The dashboard is stdlib-only, deliberately: "There is no `require` block and there is no go.sum, and that is the point" (`dashboard/go.mod:1-7`).
4. Every external HTTP client is built in one of eight places, listed in §7.1, plus the ArcadeDB client (`arcade.go:74-75`), which is GAOS's own store and is excluded (§7.1).
5. Every machine-API route passes one gate, `server.gate` (`auth.go:541-590`), which already knows the matched pattern (`auth.go:543`) and whether it is a bearer route (`auth.go:552-553`); `routes()` marks every pattern `registerAPI` registers as bearer (`main.go:666-669`).
6. The source address of an inbound call is already computed, for the Basic routes' failure lock: `jenkinsSource` (`staging.go:270-297`) takes `X-Real-IP`, which Traefik trusts from the edge alone (`deploy/stacks/traefik.yml:52-53`; `deploy/edge/README.md:158-164`), and normalises an IPv6 address to its /64 (`staging.go:307-316`).

**Success.** After the v0.69.0 deploy on both prod instances, an operator signed into the back office opens **Traffic** and, for the retained window (48 hours by default), sees: one row per external platform with its call count, client errors, failures, p50 and p95 latency and last call; one row per inbound source address and route with its client, whether it came through the edge, its refusals and last call; and the newest calls, filterable and paged. Stopping the Redis container changes nothing anywhere else in GAOS: every page, every route and every outbound call behaves and times exactly as before, and the Traffic page says the store is unavailable. The operator verifies the inbound half once against the edge README's own sampling step: a `conntrack -L` sample of the tunnel port (`deploy/edge/README.md:186-187`) names no peer that the page does not show as `direct`.

## 1. Glossary

| Term | Meaning |
|---|---|
| Record | One call, as the fields of §5.2. Not an event (§12). |
| Outbound | A call GAOS makes to an external platform through one of the clients of §7.1. |
| Inbound | A call to a machine-API route (a bearer pattern of `routes()`, `main.go:666-669`), or an unmatched `/v1/` path (§8.2). |
| Platform | The closed label of an outbound client: one of the eight in §7.1. |
| Source | The caller's address as `jenkinsSource` computes it (`staging.go:276-297`): IPv4 whole, IPv6 as its /64. |
| Via | `edge` when the source is a public address, `direct` when it is loopback, private or link-local (§8.3). |
| Client label | The machine-API principal that authenticated: `admin`, a clients-file name (`apiClient.Name`, `api_v1.go:54-63`), `basic:staging` or `basic:unit-results`; empty when nothing authenticated. |
| Window | `GAOS_TRAFFIC_HOURS` (default 48): records older than this are trimmed and never counted. |
| Cap | `GAOS_TRAFFIC_MAX` (default 5000): the most records kept per direction. |
| Drop | A record not stored because the queue was full, Redis did not answer, or the per-source limit was reached. Always counted (§9). |

## 2. What changes, in one table

| Area | Change | Section |
|---|---|---|
| Stack | A `redis` service, pinned by version and digest, on a new `internal: true` network shared with the dashboard only; no port, no persistence, `maxmemory 64mb`, `noeviction`, a healthcheck, read-only root, no capabilities | §4 |
| Settings | `GAOS_REDIS_ADDR`, `GAOS_TRAFFIC_MAX`, `GAOS_TRAFFIC_HOURS` | §4.3 |
| Go | `resp.go`: a minimal RESP2 client (no dependency) | §6 |
| Go | `traffic.go`: the recorder, the queue and writer, the recording `RoundTripper`, the path template, the inbound recorder in the gate | §7, §8, §9 |
| Go | `traffic_view.go`: the page model, the summaries, the filters, the poll handler | §10 |
| Go | The eight client constructors wrap their transport; `apiGate` and `jenkinsGate` name the principal | §7.1, §8.4 |
| UI | **Traffic** in the back-office menu after Integrations; `bo-traffic` and `traffic-live` templates; two CSS classes | §10, §11 |
| Checks | `check-stacks.sh` gains a "traffic store" block | §4.4 |
| Deploy | `rebuild-restart.sh` accepts `redis` as a service | §4.5 |
| Docs | A new page `integrations/traffic.html`; `STACKS.md`, `infra/environments.html` and the edge README's §5 updated | §17 |
| Registers | D-107, BR-66, TC-884 … TC-900, v0.69.0; no event type, task status, schema statement or docs CSS class | §14 |

## 3. Data flow

```
 outbound                                   inbound
 ─────────                                  ───────
 a GAOS client (§7.1)                       a caller ── the edge ──┐   (or a tunnel peer / this VM, directly)
   │ http.Client.Do                                                ▼
   ▼                                        Traefik ── X-Real-IP ──► dashboard: server.gate (auth.go:541)
 recordingTransport.RoundTrip                                        │ bearer pattern, or unmatched /v1/...
   │ base.RoundTrip(req) ── the platform                             │ wraps w, puts a tag in the context
   │ (answer returned to the caller unchanged)                       ▼
   ▼                                                               the handler (apiGate / jenkinsGate set the tag's client)
 rec.out(record)                                                     │
   │                                                                 ▼
   │                                                               rec.in(record)   ── per-source limit (§8.5)
   ▼                                                                 │
 ┌──────────── bounded queue, 4096 (select … default: drop++) ◄──────┘
 │
 ▼  one writer goroutine: batch ≤128 or 250 ms, pipelined
 XADD gaos:traffic:out|in MAXLEN ~ <cap> * <fields>        every 60 s: XTRIM … MINID ~ <now − window>
 │
 ▼  redis (own network, no port, no disk)
 ▲
 │  XREVRANGE … COUNT <cap>, one read per 15 s at most (cached)
 traffic_view.go ── Traffic page, polled every 30 s
```

## 4. The traffic store

### 4.1 The service

Added to `deploy/stacks/compose.prod.yaml` after `dashboard` (`:59-238`) and before `docs` (`:240`):

```yaml
  # D-107: the traffic store. Two capped Redis streams, the newest calls in and out and
  # nothing older than the window; nothing reads them but the Traffic page. Its own
  # internal network, shared with the dashboard alone: no port, no route out, no
  # neighbour. No disk: a restart empties it, by design. GAOS never waits for it --
  # there is deliberately no depends_on in either direction.
  redis:
    image: redis:8.10.3-alpine@sha256:7958cfbb9d152f87dadfbc89f1b0b15938f0b81ff8982feb327476f4911935c3
    restart: unless-stopped
    command: ["redis-server", "--save", "", "--appendonly", "no", "--maxmemory", "64mb", "--maxmemory-policy", "noeviction"]
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
    networks: [traffic]
```

Each choice, decided:

| Choice | Decision | Why |
|---|---|---|
| Image | `redis:8.10.3-alpine` pinned **by tag and digest** | 8.10.3 was released 2026-10-26 (32 days before this design; GitHub `redis/redis` releases); the `8.10.3-alpine` manifest list was last pushed 2026-11-11 (16 days) with digest `sha256:7958cfbb…35c3`, read from Docker Hub on 2026-11-27. The tag alone is mutable (Docker Hub rebuilds it); the digest makes the deploy reproducible and is the supply-chain pin. Updating it is a deliberate edit that `check-stacks.sh` keeps well-formed (§4.4). |
| Persistence | **None**: `--save ""`, `--appendonly no`, `/data` a tmpfs | The operator asked for no long history (§0). A Redis restart empties both streams; a **dashboard** redeploy — the common restart, `deploy/rebuild-restart.sh` rebuilds the dashboard image on every release — does not, because only the dashboard container is replaced. |
| Memory | `--maxmemory 64mb`, container `mem_limit: 128m` | The worst case is bounded by the clamps of §4.3 and the field bounds of §5.2: 2 directions × 20 000 records × ≤ 600 bytes ≈ 24 MB, so 64 MB is headroom, not a limit the streams reach. The container limit bounds Redis' own overhead, as `docs` is bounded (`compose.prod.yaml:253-257`). |
| Eviction | `noeviction` | There are exactly two keys. Under `allkeys-lru` a full Redis would delete a whole stream, silently erasing one direction. Under `noeviction` an `XADD` past the limit is refused with an OOM error, which the writer counts as a drop (§9) — visible on the page — while `XTRIM` keeps freeing memory. |
| Network | `networks: [traffic]` only; the network is `internal: true` (§4.2) | Nothing but the dashboard can reach Redis: not Traefik, not the public docs container, not the landing page, not another tunnel peer; and Redis itself has no route out. |
| Port | **No `ports:` and no `expose:`** | Nothing is published beyond Traefik's one mapping (`check-stacks.sh:50-54`). |
| Authentication | **None** (no `requirepass`, no ACL file) | The network admits only the dashboard; a password would be one more host-side secret file whose only reader is the process that already holds every GAOS credential (`compose.prod.yaml:208-228`). The residual is stated in §13.4. |
| Hardening | `user: "999:1000"`, `read_only`, `tmpfs /data`, `cap_drop: [ALL]`, `no-new-privileges` | The official image's entrypoint changes user only when started as root; started as the image's `redis` user it needs no capability. *Assumption, verified in phase T4 before the compose change lands:* the alpine image's `redis` user is uid 999, gid 1000 (`docker run --rm <image> id redis`); if it differs, the `user:` line takes the printed values and nothing else changes. Redis ≥ 7 ships `enable-protected-configs no`, `enable-debug-command no` and `enable-module-command no` by default, so `CONFIG SET dir`, `DEBUG` and `MODULE LOAD` are refused over the network; T4 records `redis-cli CONFIG GET enable-protected-configs` → `no` in the deploy note. |
| Healthcheck | `redis-cli ping` every 30 s | `docker compose ps` then shows `healthy`; the dashboard does not depend on it (next row). |
| Start order | **No `depends_on`**, either way | GAOS must start, serve and call out with Redis absent (§9). |
| YAML shape | Every list in the block is a **flow sequence** (`[...]`) | `check-stacks.sh:51` counts lines that start with `- "` as port mappings; a block-sequence `command:` would turn "exactly one ports: mapping" into a false failure. |

**Licence.** Redis 8 is distributed under RSALv2, SSPLv1 or AGPLv3 at the user's choice. GAOS runs the unmodified upstream image as a separate process, does not link it, does not modify it and does not redistribute it (the host pulls it from Docker Hub), so no obligation reaches GAOS's Apache-2.0 code. Valkey (BSD-3) speaks the same protocol and is the drop-in replacement if that ever changes: the client of §6 uses only commands both implement (§18).

### 4.2 The network

`compose.prod.yaml` `networks:` (`:20-28`) gains:

```yaml
  # D-107: the dashboard and the traffic store, and nothing else. internal: no route out.
  traffic:
    name: gaos-prod-traffic
    internal: true
```

Its subnet is **not pinned**, unlike `internal` (`:23-28`): `internal` is pinned because Traefik's trust list is reasoned about in addresses (`:23-24`); nothing reasons about the traffic network's addresses, and Docker allocates a subnet no existing network uses. The `dashboard` service's `networks: [internal]` (`:238`) becomes `networks: [internal, traffic]`.

### 4.3 The settings

Added to the dashboard's `environment:` in `compose.prod.yaml` (after `GAOS_TASKMGR_CA_FILE`, `:207`):

```yaml
      # D-107: the traffic store (the service below). Fixed, not from the deploy shell: it is
      # this stack's own service name. The cap and the window come from the deploy shell,
      # empty = the defaults (5000 records per direction, 48 hours); out-of-range values are clamped.
      GAOS_REDIS_ADDR: "redis:6379"
      GAOS_TRAFFIC_MAX: "${GAOS_TRAFFIC_MAX:-}"
      GAOS_TRAFFIC_HOURS: "${GAOS_TRAFFIC_HOURS:-}"
```

| Variable | Empty / unset | Valid | Out of range | Not a number |
|---|---|---|---|---|
| `GAOS_REDIS_ADDR` | recording **off**; wrapping is a no-op (§7.2); the page says "off" | `host:port` that `net.SplitHostPort` accepts with a numeric port | — | anything else: off, with the reason logged once and shown on the page |
| `GAOS_TRAFFIC_MAX` | 5000 | 500 … 20 000 | clamped to the nearer bound, logged once | 5000, logged once |
| `GAOS_TRAFFIC_HOURS` | 48 | 1 … 168 | clamped, logged once | 48, logged once |

The boot log prints one line: `traffic: recording to redis:6379, newest 5000 per direction, 48 h` or `traffic: off (GAOS_REDIS_ADDR is not set)`. It never prints an address recorded from a call.

The dev compose (`compose.dev.yaml`, retired since v0.33.1, `deploy/STACKS.md:1-3`) and the local `compose.yaml` get no Redis: their dashboards run with recording off.

### 4.4 `check-stacks.sh`

A new block, after "the proxy never holds the Docker socket" (`check-stacks.sh:91-105`), prod only like the credential checks (`:192-253`), using the existing `has`/`hasnt`/`hasre`/`hasntre` helpers (`:24-30`) and one new helper that extracts a service's block (the lines from `^  <name>:` up to the next line matching `^  [a-z]`):

```
== the traffic store (D-107) ==
ok   prod pins the Redis image by version and digest
ok   prod's Redis persists nothing (no RDB, no AOF)
ok   prod's Redis has a memory limit and refuses writes rather than evicting a stream
ok   prod's Redis publishes no port
ok   prod's Redis is on the traffic network only, and that network is internal
ok   prod's Redis has a healthcheck
ok   prod's dashboard joins the traffic network
ok   prod points the dashboard at its own Redis and passes the cap and the window from the deploy shell
ok   nothing in prod depends on Redis
```

Each line's assertion, exactly:

| Message | Assertion |
|---|---|
| pins the image | `hasre $PROD 'image: redis:[0-9]+\.[0-9]+\.[0-9]+-alpine@sha256:[0-9a-f]{64}$'` |
| persists nothing | the redis block contains `"--save", ""` and `"--appendonly", "no"` and no `volumes:` line |
| memory limit | the redis block matches `"--maxmemory", "[0-9]+mb"` and contains `"--maxmemory-policy", "noeviction"` |
| publishes no port | the redis block contains neither `ports:` nor `expose:` |
| traffic network only | the redis block contains `networks: [traffic]`; the file contains `name: gaos-prod-traffic` followed on the next line by `internal: true` |
| healthcheck | the redis block contains `healthcheck:` and `redis-cli` |
| dashboard joins | the dashboard block contains `networks: [internal, traffic]` |
| address and settings | `has $PROD 'GAOS_REDIS_ADDR: "redis:6379"'`, `has $PROD 'GAOS_TRAFFIC_MAX: "${GAOS_TRAFFIC_MAX:-}"'`, `has $PROD 'GAOS_TRAFFIC_HOURS: "${GAOS_TRAFFIC_HOURS:-}"'` |
| no depends_on | `hasnt $PROD 'depends_on'` |

A failing assertion prints `FAIL` with the same message (`check-stacks.sh:22`). The block is one register row, TC-900 (§15). The existing "exactly one ports: mapping" check (`:50-54`) is unchanged and still passes because of the flow-sequence rule of §4.1.

### 4.5 `rebuild-restart.sh`

`SERVICES="traefik landing dashboard docs"` (`deploy/rebuild-restart.sh:26`) becomes `"traefik landing dashboard docs redis"`, and the usage comment (`:15`) with it, so `rebuild-restart.sh prod restart redis --tag …` works. A plain `deploy` already builds and starts every service (`:359-367`), Redis included. The verification steps (`:373-470`) are unchanged: Redis is not on the health path (§9). TC-019's quoted message (`pustaka/docs/product/qa-test-plan.html:1080-1081`, `-- valid: traefik landing dashboard docs`) gains ` redis`; no new row.

## 5. Streams, records and retention

### 5.1 Keys

| Key | Holds |
|---|---|
| `gaos:traffic:out` | outbound records |
| `gaos:traffic:in` | inbound records |

One key per direction, so a burst in one direction cannot evict the other's history. Each prod instance deploys its own copy of the compose file, so its own Redis (`deploy/edge/README.md:181-182`, `:210-215`), so the keys carry no instance prefix.

### 5.2 Record fields

Every record is `XADD <key> MAXLEN ~ <cap> * <field> <value> …` with the server-assigned id (`*`); the call's own time is the field `t`, because a batch is written up to 250 ms after its calls (§9.2) and explicit ids would have to be strictly increasing across concurrent callers.

| Field | Out | In | Value | Bound |
|---|---|---|---|---|
| `v` | ✓ | ✓ | record shape version, `1` | — |
| `t` | ✓ | ✓ | the call's start, Unix milliseconds | — |
| `m` | ✓ | ✓ | method: `GET POST PUT PATCH DELETE HEAD OPTIONS`, anything else `OTHER` | closed |
| `s` | ✓ | ✓ | HTTP status; `0` = no response (outbound transport error) | integer |
| `d` | ✓ | ✓ | milliseconds: out, from `RoundTrip` start to the response headers or the error; in, from the gate to the handler's return | integer |
| `qb` | ✓ | ✓ | request `ContentLength`; `-1` unknown | integer |
| `rb` | ✓ | ✓ | out: the response's `ContentLength` (`-1` unknown — the body is never read or wrapped, §7.3); in: bytes the handler wrote | integer |
| `p` | ✓ | | platform label (§7.1) | closed |
| `h` | ✓ | | `req.URL.Host` lower-cased (host and port; `URL.Host` never holds userinfo) | 100 bytes, longer cut with `…` |
| `u` | ✓ | | the templated path (§7.4) | 160 bytes |
| `e` | ✓ | | error class (§7.5); empty when a response arrived | closed |
| `r` | | ✓ | the matched route pattern without its method, e.g. `/v1/products/{id}/staging`; `(no route)` for an unmatched `/v1/` path | the registered patterns |
| `src` | | ✓ | the source (`jenkinsSource`, `staging.go:276-297`) | 45 bytes |
| `via` | | ✓ | `edge` or `direct` (§8.3) | closed |
| `c` | | ✓ | the client label (§8.4) | 64 bytes |
| `pid` | | ✓ | the path's `{id}` when it matches `^p-[0-9]+$` (`productIDShape`, `urls.go:90`), else empty | 16 bytes |

No other field exists. In particular no record has a header, a body, a query string, a fragment, `URL.User`, a cookie, a token, a password, an `Authorization` value, an error message or a request id from a body. §7.3 says how the code guarantees it; TC-888 and TC-893 assert it.

### 5.3 Retention

- **By count**: every `XADD` carries `MAXLEN ~ <cap>`. The `~` lets Redis trim whole internal nodes, so a stream can hold up to about 100 records more than the cap (Redis' default `stream-node-max-entries`); the page reads at most the cap (§10.3), so the excess is never shown.
- **By age**: `XADD` takes one trimming strategy, so age is a second command. Every 60 s the writer sends `XTRIM gaos:traffic:out MINID ~ <ms>-0` and the same for `in`, where `<ms>` is now minus the window. The page also ignores any record whose `t` is older than the window, so a record that survives a missed trim is never counted.
- **On restart**: Redis keeps nothing on disk (§4.1).

## 6. The Go client: a minimal RESP2 client

**Decision: a minimal client written in this repository, `dashboard/resp.go`; no dependency.**

| | Minimal RESP client | `go-redis` (or another library) |
|---|---|---|
| Policy | Keeps the module stdlib-only, as `dashboard/go.mod:1-7` requires and explains | The first `require` block and `go.sum` in the module: the policy's stated reason ("a service with no dependency graph has no dependency graph to audit, patch or wait on") is lost |
| Supply chain | Nothing new to trust | A library plus its transitive modules on the build path of a binary that holds every GAOS credential |
| Size | ~250 lines: one encoder, one bounded reply parser, five commands | Thousands of lines for features this design does not use (cluster, sentinel, pub/sub, scripting, pooling) |
| Risk | Protocol bugs are ours; mitigated by the fake-server tests TC-884, TC-885 | Well tested upstream |

RESP2 is small: a command is an array of bulk strings; a reply is one of five types. This design uses five commands only: `PING`, `XADD`, `XTRIM`, `XREVRANGE`, `XLEN`.

**The client's contract.**

- `dialRESP(addr string, timeout time.Duration) (*respConn, error)` — `net.DialTimeout("tcp", …)`; no `HELLO`, so the server speaks RESP2.
- `(*respConn).do(deadline time.Time, args ...string) (respValue, error)` — one command; `(*respConn).pipeline(deadline, cmds [][]string) ([]respValue, error)` — writes all, then reads exactly `len(cmds)` replies. Both set the connection deadline; any I/O or parse error closes the connection, and the caller redials.
- `respValue` is `{kind byte; str string; n int64; arr []respValue}`; an error reply (`-…`) is returned as a Go error `resp: <first 80 bytes>`.
- **The parser is bounded**, because the reply is input: a line of at most 1 KiB (`bufio.Reader.ReadSlice` and a length check), a bulk string of at most 64 KiB, an array of at most 50 000 elements, nesting at most 3 deep (an `XREVRANGE` reply is array → entry → field list). Anything outside is an error and closes the connection (TC-885). It never allocates from a declared length before checking it.
- Not safe for concurrent use: the writer owns one connection, the page reader owns another under a mutex (§9.2, §10.3).

## 7. Recording outbound calls

### 7.1 The clients and their labels

| Platform label | Shown as | Built at | Covers |
|---|---|---|---|
| `qa-tool` | QA tool | `qaclient.go:145-150` (`newQaOutbound`) | every call to the QA tool: capabilities, results, catalog (`qaclient.go:73, 273, 358`), automation, jobs, case proposals (`qajobs.go:96, 112, 129, 592`), the inbox worker's answers (`inbox_worker.go:51`) |
| `ai-manager` | AI manager | `aimanager.go:131-136` (`AIMClient.client`) | chat, results, providers (`aimanager.go:170, 182, 246`), questions (`aimanager_questions.go:71`), the admin calls (`aimanager_admin.go:63`) |
| `brain-town` | Brain Town | `braintown.go:292-294` (`newBrainTown`) | the feed's posts (`braintown.go:559`) |
| `brain-town-reader` | Brain Town reader | `braintown_consumer.go:321-328` (`newBTConsumer`) | the reader's stream requests (`braintown_consumer.go:413`) |
| `github` | GitHub | `github.go:179-183` (`newGHClient`) via `main.go:393` | the review token's calls |
| `github-write` | GitHub (auto-fix) | the same constructor via `newGHWriter` (`githubwrite.go:162-168`) | the write token's calls |
| `whatsapp` | WhatsApp gateway | `wa.go:180-186` (`newWANotifier`, transport from `waTransport`, `wa.go:205`) | group check and sends (`wa.go:286, 368`) |
| `taskmgr` | TaskMgr | `taskmgr.go:233-235` (`newTaskMgrClient`) | the gRPC-over-HTTP/2 calls (`taskmgr.go:332`) |

**Excluded: ArcadeDB** (`arcade.go:74-75`). It is GAOS's own store, called on nearly every request; recording it would fill the outbound cap within minutes and push every external platform out of the window. Its reachability is already on `/api/healthz` (`main.go:706`). Listed as a non-goal (§19).

**`github` versus `github-write`.** Both clients come from `newGHClient(cfg ghConfig)` (`github.go:173`). `ghConfig` (`github.go:108-112`) gains one field, `platform string`; `ghWriteConfigFromEnv` (`githubwrite.go:135`) sets it to `github-write`; empty means `github`. No constructor signature changes, so no test call site changes.

**The closed set.** The eight labels are constants in `traffic.go`, in the table's order, which is also the page's row order (§10.2). A label not in the set is never written (`trafficWrap` with an unknown label panics at construction — a programming error that TC-892 would catch first).

### 7.2 The wrap

```go
// trafficWrap returns rt wrapped in a recordingTransport for platform, or rt itself
// when recording is off. A nil rt means http.DefaultTransport, as http.Client reads it.
func trafficWrap(platform string, rt http.RoundTripper) http.RoundTripper
```

Each constructor of §7.1 sets its client's `Transport` to `trafficWrap(<label>, <the transport it sets today>)`; where a constructor sets no transport today (`braintown.go:292`, `qaclient.go:145`, `aimanager.go:135`) the base is `nil`, i.e. `http.DefaultTransport`, exactly the transport it uses now. Timeouts, `CheckRedirect` and TLS settings stay on the client and the base transport, untouched.

The recorder is process-wide: `var trafficRec atomic.Pointer[trafficRecorder]`, set once in `serve()` right after the signal context (`main.go:195-196`) and before the first client is built (`newBrainTown`, `main.go:293`). `trafficWrap` reads it at construction; `AIMClient.client()` builds a client per call (`aimanager.go:131-136`) and so wraps per call. **When recording is off, `trafficWrap` returns `rt` unchanged**, so every test that does not turn recording on sees today's transports — including `TestTaskMgrConfig`'s assertion `cl.hc.Transport.(*http.Transport)` (`taskmgr_test.go:409`). The tests that turn it on (§15) are not `t.Parallel()` and restore the pointer with `t.Cleanup`; Go runs a package's top-level parallel tests only after its sequential ones finish, so no parallel test sees a test recorder.

`recordingTransport` also implements `CloseIdleConnections()` by delegating to the base when the base has it, so `http.Client.CloseIdleConnections` keeps working. Nothing in the dashboard type-asserts a production client's transport (`grep -n 'Transport.(' dashboard/*.go` finds only `arcade.go:62`, on `http.DefaultTransport`, which is not wrapped).

### 7.3 What `RoundTrip` reads — and does not

```go
func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	t.rec.out(outRecord{ /* the fields below, and only these */ })
	return resp, err
}
```

The record is built from exactly: `start`, `time.Since(start)`, `req.Method`, `req.URL.Host`, `req.URL.EscapedPath()` (then templated, §7.4), `req.ContentLength`, `resp.StatusCode` and `resp.ContentLength` when `resp != nil`, and `errClass(err)` when `err != nil`. The function does not read `req.Header`, `req.Body`, `req.URL.User`, `req.URL.RawQuery`, `req.URL.Fragment`, `resp.Header`, `resp.Body` or `err.Error()`. The response and the error are returned unchanged, and the body is not wrapped, so a streaming response (the Brain Town reader's stream, `braintown_consumer.go:321-323`) is recorded once, at its headers, and the latency is time to headers for every platform. TC-888 sends a request carrying a bearer header, userinfo, a token in the query and a body, and asserts no stored field contains any of them.

`rec.out` never blocks (§9.1).

### 7.4 The path template

The escaped path is split on `/`; each segment is kept or replaced:

| Segment | Becomes |
|---|---|
| empty (leading or doubled `/`) | kept empty |
| matches `^[A-Za-z][A-Za-z0-9._-]{0,39}$` **and** holds at most 3 digits | kept |
| only digits | `{n}` |
| anything else — more than 3 digits, a `%`-escape, `@`, `:`, `=`, `+`, `~`, longer than 40 bytes | `{id}` |

At most 12 segments are kept; a longer path ends in `/…`. The result is cut at 160 bytes. Examples (TC-887): `/repos/acme/route-api/pulls/12/files` → `/repos/acme/route-api/pulls/{n}/files`; `/repos/acme/route-api/commits/3f2a…(40 hex)` → `/repos/acme/route-api/commits/{id}`; `/api/v1/gaos/jobs/job_01JD9X…` → `/api/v1/gaos/jobs/{id}`; `/v1/requests/req-7f3c9a2e/result` → `/v1/requests/{id}/result`; `/send/message` → `/send/message`; `/x/628123456789@s.whatsapp.net` → `/x/{id}`. Owner and repository names are kept: the Pull requests page already shows them to the same audience (`templates/backoffice.html:1568`).

This is a defence in depth, not the only one: the clients of §7.1 put identifiers in paths but put credentials in headers (e.g. `AIMClient.do`'s Basic login, `aimanager.go:154`) and a WhatsApp number in the body (`wa.go:368`), never in a path — so BR-63's rule that a number is never stored (`pustaka/docs/product/brd.html:1309`) holds twice over.

### 7.5 Error class

`errClass(err)` returns one of a closed set, checked in this order: `timeout` (`errors.Is(err, context.DeadlineExceeded)` or a `net.Error` with `Timeout()`), `canceled` (`context.Canceled`), `dns` (`*net.DNSError`), `refused` (`syscall.ECONNREFUSED`), `reset` (`syscall.ECONNRESET`, `io.EOF`, `io.ErrUnexpectedEOF`), `tls` (`*tls.CertificateVerificationError`, `x509.UnknownAuthorityError`, `x509.HostnameError`, `tls.RecordHeaderError`), else `other`. The error's text is never stored: a `*url.Error`'s text holds the URL, query included. A refused redirect is not a transport error — the client's `CheckRedirect` sees a 3xx the transport already returned — so it is recorded as that 3xx status.

## 8. Recording inbound calls

### 8.1 Where

In `server.gate` (`auth.go:541-590`), which already resolves the pattern (`auth.go:543`) and its class (`auth.go:548-554`). Recording applies when **the class is bearer** (every `registerAPI` pattern, `main.go:666-669`, the two Basic routes included, `api_v1.go:442-448`) **or** the pattern is empty and the path starts with `/v1/` (§8.2). Nothing else is recorded: not the back office, not `/healthz`, not static files.

For a recorded request the gate: notes the start time; computes `src := jenkinsSource(r)` (`staging.go:276`); creates a `*trafficTag` and puts it in the request context under an unexported key; wraps `w` in a `trafficWriter`; calls `mux.ServeHTTP` (`auth.go:588`) as today; then calls `rec.in(…)` with the status and byte count from the writer, the client label from the tag, and `pid` from `r.PathValue("id")`. The gate passes the tagged copy, `r2 := r.WithContext(…)`, to `mux.ServeHTTP` (today it passes `r` itself for the bearer class, `auth.go:588`; the copy at `:586` is the back-office class's) and reads `r2.PathValue("id")` after it returns: `http.ServeMux.ServeHTTP` sets the matched path values on the request it is given (Go 1.22+; §20 assumption 4, asserted by TC-893).

### 8.2 Unmatched `/v1/` paths

A request for `/v1/…` that matches no pattern is answered 404 by the mux today (`auth.go:544-546`). It is recorded with `r = (no route)`, its method, status, source and via — **never its path**, which is caller-controlled. This is the caller who is probing the API, which is what the firewall step wanted to see.

### 8.3 Source and via

`src` is `jenkinsSource(r)` unchanged (`staging.go:276-297`): `X-Real-IP` when it parses; else the rightmost public `X-Forwarded-For` hop; else the peer. Traefik sets and trusts these headers from the edge alone (`deploy/stacks/traefik.yml:52-53`), and a request from any other tunnel peer carries that peer's own address (`deploy/edge/README.md:160-164`).

`via` is `direct` when the source address (for an IPv6 /64, its prefix address) `IsLoopback`, `IsPrivate`, `IsLinkLocalUnicast` or `IsUnspecified`, or is `unknown`; otherwise `edge`. The edge is a public host whose clients are on the internet, so a call that came through it carries a public address; a call that reached Traefik over the tunnel from another host, or from a process on the VM itself (`deploy/edge/README.md:198-200`), carries a private one. *Limit, stated on the page's docs:* a caller that reaches the edge from the VM's own public address is shown as `edge` with that address — it did come through the edge.

### 8.4 The client label

The tag holds one string, set by the code that authenticates:

- `apiGate` (`api_v1.go:316-340`): right after `s.api.authorize(r)` (`:329`), when `got != scopeNone`, it sets `admin` for `scopeAdmin` and `cl.Name` otherwise (`api_v1.go:54-55`; the QA tools' principal is `qa-tools`, `answer_source.go:27`, `api_v1.go:177`). A 403 (`:335-337`) is therefore recorded with the client that was refused.
- `jenkinsGate` (`staging.go:331-406`): after `a.ok(prodKey)` (`:404`) it sets `basic:` + `a.route` (`staging`, `unit-results`, `staging.go:71-72`); the product is the record's `pid`.
- Nothing else sets it; an unauthenticated or refused-before-authentication call has an empty label, shown as "unauthenticated".

A clients-file name is the operator's own label for a client (`api_v1.go:54-55`), not a secret; it is cut to 64 bytes. The `Authorization` header, the Basic user name and the password are never read by the recorder: the label is set only from the values above, after authentication.

### 8.5 The per-source limit

An unauthenticated caller can send as many requests to a bearer route as the edge passes (the edge limits only the two Basic routes, `deploy/edge/README.md:163-166`), and every one would be a record pushing legitimate ones out of the cap. So `rec.in` keeps, in memory, a count per source for the current minute (a fixed window; the map is cleared at each minute boundary and holds at most 4096 sources, beyond which new sources share one bucket named `other`). **The 301st record from one source in one minute and every later one in that minute are not stored**; each is counted as `suppressed` (§9.3). 300 a minute is five a second, above any legitimate caller's rate today (the QA tool's polling and the Jenkins posts are seconds to minutes apart — *assumption*, verifiable on the page itself after deploy). Outbound records have no such limit: their volume is GAOS's own.

### 8.6 The response writer

`trafficWriter` records the first `WriteHeader` status (200 if `Write` comes first) and counts bytes written. It implements `Unwrap() http.ResponseWriter`, so `http.NewResponseController(w)` reaches the real writer — `apiDecodeLimit` sets a read deadline that way (`api_v1.go:398-401`) and must keep working (TC-894) — and `Flush()` when the underlying writer is an `http.Flusher`. It changes no header and no status.

## 9. Never blocking, never failing

### 9.1 The hot path

`rec.out` and `rec.in` build a small struct and do `select { case rec.q <- r: default: rec.dropped.Add(1) }` on a channel of capacity 4096. They take no lock that the writer holds, do no I/O and allocate one record. With recording off they are not reached (`trafficWrap` returns the base; the gate checks `trafficRec.Load() == nil` first).

### 9.2 The writer

One goroutine, started by `serve()` with the signal context:

1. Collect up to 128 records, or whatever arrived within 250 ms.
2. If there is no connection, dial with a 500 ms timeout. On failure: count the batch as dropped, wait the backoff (1 s, doubling to 30 s), and keep draining the queue meanwhile, counting each record dropped — so a dead Redis never fills the queue for long and never costs a caller anything.
3. Pipeline one `XADD` per record (§5.2) with a 2 s deadline. A reply that is an error (`OOM` under `noeviction`) counts that record dropped. An I/O error closes the connection, counts the unacknowledged records dropped and goes to step 2.
4. Every 60 s, pipeline the two `XTRIM … MINID ~` of §5.3.

On context cancel the writer stops; records still queued are discarded (they are monitoring data, §12).

### 9.3 Counters and logs

The recorder holds three counters since process start — `dropped` (queue full, Redis down, Redis refused), `suppressed` (§8.5), `written` — and the time of the last write error. The page shows them (§10.4). The log gets one line on each **transition**, never one per record: `traffic: the store is not answering (<error class>); records are dropped until it answers` and `traffic: the store answers again; <n> records were dropped meanwhile`. No log line carries an address, a path or a host from a record.

### 9.4 What Redis being down does

Nothing outside this feature: no handler, route, gate or client waits on Redis; `/api/healthz` does not probe it (`main.go:706-…` unchanged); the dashboard starts without it (§4.1, no `depends_on`). TC-890 asserts it with a Redis address that accepts connections and never answers, and one that refuses them.

## 10. The Traffic page

### 10.1 Place, address and access

- **Navigation:** a new back-office menu item **Traffic**, after **Integrations** (`templates/backoffice.html:96`) — the two are the operations pair: what is configured, and what it is doing. `pageViews` (`urls.go:31-35`) gains `{"traffic", "Traffic"}` after `{"integrations", "Integrations"}`, which gives the page route `GET /traffic` (the `pageViews` loop in `routes()`, `main.go:581-583`; `urls.go:78-84`) and the pane `GET /ui/bo/traffic` (`main.go:604`).
- **The poll:** `GET /ui/bo/traffic/live` → `handleTrafficLive`, registered beside `GET /ui/bo/prs/live` (`main.go:607`), shaped like `handlePRsLive` (`prview.go:489-499`): `requireBO`, `Cache-Control: no-store`, render `traffic-live` with `OOB = true`.
- **Who may view: every signed-in back-office account.** The same audience as Integrations (`handlers_bo.go:254-257`) and Pull requests (`handlers_bo.go:263-265`): the gate refuses a signed-out request (`auth.go:568-585`; BR-55, `brd.html:1355`). The page has nothing to edit and no admin-only part. Restricting it to auto-fix admins, as BR-63 does for the per-person directory, was considered and rejected: the addresses are machine callers' (CI, the QA tool, the tunnel peers, scanners), knowing them is the page's whole purpose for whoever runs GAOS, and their retention is bounded by §5.3 (§13.2).
- **Caching:** `noStoreView` (`integrations.go:27-31`) adds `"traffic"`, so the pane, like Integrations, is `no-store`; the page route already sends `no-store` (`urls.go:395`).
- `buildBO` (`handlers_bo.go:197`) gains `case "traffic": s.buildTraffic(r, bo); return bo`; `BOView` (`view.go:206-251`) gains `Traffic *TrafficPage`; the view switch (`templates/backoffice.html:129-131`) gains `{{else if eq .View "traffic"}}{{template "bo-traffic" .}}`.

### 10.2 What the page shows

Header: eyebrow `monitoring`, title **Traffic**, sub "Calls GAOS made to other platforms, and calls made to GAOS's machine API, over the last 48 hours — at most the newest 5,000 each way. Nothing here is kept after the traffic store restarts." (the numbers are the live settings).

1. **Status line** (`af-status`, as `templates/backoffice.html:1529-1533`): the pulse, "Recorded up to <time> · window 48 h · refreshes every 30 s", and the `Updating…` busy text.
2. **Notes** (`af-note`, `role="note"`), only when true: *"<n> calls were not recorded since <boot time>: the queue was full or the store did not answer. The figures below are lower than the real traffic."* (dropped > 0); *"<n> inbound calls were not recorded: a source sent more than 300 a minute."* (suppressed > 0).
3. **Tiles** (`af-tiles`, `templates/backoffice.html:1336-1342`): *Outbound calls*; *Outbound failed* (tone `fail` when > 0); *Inbound calls*; *Direct sources* — the number of distinct sources with `via = direct` (tone `warn` when > 0, because that is the population the firewall step would have closed).
4. **Outbound by platform** — always the eight platforms in §7.1's order; a platform with no call in the window is one muted row "No calls in the window".

   | Column | Value |
   |---|---|
   | Platform | the display name |
   | Host | the most frequent `h`; up to two more as `+n` with all in `title` |
   | Calls | records in the window |
   | 4xx | `400 ≤ s < 500` |
   | Failed | `s ≥ 500` or `s = 0` |
   | p50, p95 | nearest-rank percentiles of `d` over records with `s > 0` (a call with no answer has no latency; it is in Failed) |
   | Last call | relative time, absolute in `title` |

5. **Inbound by source and route** — one row per (`src`, `r`) pair, sorted by the pair's last call, newest first; at most 50 rows, then *"<n> more pairs — filter the calls below by source to see them."*

   | Column | Value |
   |---|---|
   | Source | the address, mono; a link that filters the calls table to it (§10.3) |
   | Via | `edge`, or the chip **direct** (`chip st-warn`, `title`: "Reached GAOS over the tunnel or from this host, not through the edge") |
   | Client | the distinct labels of the pair, up to two, else "unauthenticated" |
   | Route | `r`, mono |
   | Calls, Refused (`400 ≤ s < 500`), Failed (`s ≥ 500`), p95, Last call | as above |

6. **Recent calls** — newest first, filtered (§10.3), paged by `tablePageSize` rows (10, `pager.go:15`; D-097) with the shared `pager` template (`templates/backoffice.html:596-604`).

   | Column | Out | In |
   |---|---|---|
   | When | relative, absolute in `title` | same |
   | Direction | `→` + sr-only "outbound" | `←` + sr-only "inbound" |
   | Who | platform display name | source + via chip |
   | Call | method + host + path, mono | method + route, mono; client below |
   | Status | the code as a chip (`st-ok` < 400, `st-warn` 4xx, `st-bad` ≥ 500), or the error class for `s = 0` | the code as a chip |
   | Time | `d` as `84 ms` / `1.2 s` | same |
   | Bytes (`af-md`) | `qb` / `rb`, `—` for `-1` | same |

### 10.3 Filters, paging and the read

Query parameters, each validated; an invalid value is ignored (treated as its default), never echoed:

| Parameter | Values | Default |
|---|---|---|
| `dir` | `all`, `out`, `in` | `all` |
| `platform` | one of the eight labels (applies to outbound records) | none |
| `src` | an address or /64 prefix that `netip` parses, compared in canonical form (applies to inbound) | none |
| `status` | `all`, `ok` (`0 < s < 400`), `4xx`, `fail` (`s ≥ 500` or `s = 0`) | `all` |
| `page` | `pageParam` (`pager.go:40`) | 1 |

The direction is an `af-seg` tab row (`templates/backoffice.html:1541-1543`); platform and status are `select`s in an `af-form` with `hx-trigger="change"` and a `<noscript>` Apply button (`:1544-1553`); a set filter shows **Clear filters**. Every link carries `href` and `hx-get` (`TestEveryHtmxLinkHasAPageHref`, `urls_test.go:441`). The poll URL carries the filters and the page, as the Pull requests page's does.

**The read.** `trafficSnapshot()` sends `XREVRANGE <key> + - COUNT <cap>` for both keys over the reader connection (dial 500 ms, deadline 2 s, a mutex), parses each entry strictly (an unknown platform label, method or via becomes `other`; a non-numeric number field drops the entry), drops entries older than the window, and caches the result for 15 s, shared by every viewer; a failed read is cached for the same 15 s, so a dead Redis costs each poll nothing. Summaries and the filtered list are computed from the snapshot per request (at most 2 × 20 000 records; no sort beyond one pass and the per-row percentile sorts).

### 10.4 States

| State | When | What the page says |
|---|---|---|
| Off | `GAOS_REDIS_ADDR` empty or invalid | *"Traffic recording is off."* and the reason, then *"Set `GAOS_REDIS_ADDR` to the traffic store's address and restart. See the docs."* (link to `integrations/traffic.html`). No tables. |
| Unavailable | the read failed | `af-note af-note--fail`, `role="alert"`: *"The traffic store is not answering. GAOS keeps working; calls made meanwhile are not recorded."* plus the drop counter; no tables. |
| Empty | the read succeeded, no record in the window | the platform table (all rows muted) and *"No call recorded in the last 48 hours. Outbound calls appear when GAOS calls a platform; inbound ones when a client calls the machine API."* |
| Filtered empty | records exist, none match | *"No call matches these filters."* + **Clear them** |
| Normal | — | §10.2 |

### 10.5 Live refresh

`traffic-live` is one `div#trafficLive.af-main` with `hx-get="{{.PollURL}}" hx-trigger="every 30s [!document.hidden && !document.getElementById('trafficLive').matches(':focus-within')]" hx-target="#sink" hx-swap="innerHTML" hx-indicator="#trafficPulse"` — the attributes of the Pull requests list (`templates/backoffice.html:1528`) and the Auto-fix list (`:1326`): no poll in a hidden tab, none while a filter has focus. Every 30 s matches the window's granularity: a record is at most 250 ms (§9.2) plus 15 s (§10.3) old when it reaches the page.

## 11. UI and UX

The operator pointed at `https://aiforui.dev/skills`, the index of Emil Kowalski's design-engineering skills (source `github.com/emilkowalski/skills`). This repository vendors them: `emil-design-eng` (the GAOS edition, `.claude/skills/emil-design-eng/SKILL.md`, upstream commit in `SOURCE.md:1-2`), `review-animations` (`STANDARDS.md`) and `break-ui` (`CATALOG.md`), and `.claude/rules/frontend.md` says to load all three, in that order, for frontend work; the dashboard's own mechanics are `.claude/agents/gaos-frontend.md`, which wins where they differ (`gaos-frontend.md:27-31`). Phase T3 is built by the `gaos-frontend` agent under those skills. The concrete rules this page follows:

1. **No new motion.** The page is seen tens of times a day and refreshed every 30 s: by the frequency table (`.claude/skills/review-animations/STANDARDS.md:5-14`) rows do not animate in, counts do not tick, nothing slides. The one moving thing is the existing refresh pulse (`af-pulse`), which is state indication. The global reduced-motion kill-switch (`gaos-frontend.md:85-87`) applies unchanged.
2. **Numbers are tabular and right-aligned.** A new class `.tf-num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap }`, as the QA table does for its own (`static/app.css:1543`), so a column of counts does not jitter between polls. Counts use the `num` template function (thousands separators); durations are `84 ms` under one second, `1.2 s` above; unknown is `—`.
3. **Relative times carry the absolute time in `title`** (`gaos-frontend.md:109`).
4. **Colour never carries meaning alone.** Every status chip shows its code or class as text; the **direct** chip says "direct"; direction is an arrow plus sr-only text. Only existing colours and chips are used (`chip st-ok/st-warn/st-bad`, `static/app.css:2000-2002`; tile tones): no new colour variable, so both themes hold (`gaos-frontend.md:54-55`).
5. **Long values are cut, never wrapped into the next column.** A new class `.tf-cut { max-width: 32ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap }` on hosts, paths, routes and client labels, with the full value in `title`. Addresses are mono.
6. **Empty states say what is true and what happens next** (`gaos-frontend.md:107-108`; §10.4).
7. **Hover effects stay behind `(hover: hover) and (pointer: fine)`** (`gaos-frontend.md:84`): the tables reuse `af-table`'s existing row hover, nothing new.
8. **Narrow screens:** the Bytes column and the Host column of the outbound table are `af-md`, hidden below 1000 px (`static/app.css:1886`); every table sits in `tm-scroll af-scroll` (`templates/backoffice.html:1557`).
9. **Keyboard:** every filter and link is reachable by Tab with the existing focus ring; no custom keys.
10. **No chart.** A chart needs a vendored library the dashboard does not have, and adding one needs the operator's approval (`gaos-frontend.md:45-46`); percentiles and counts in a table answer the periodic-monitoring question.
11. **`break-ui` pass before T3 is done** (`.claude/rules/frontend.md:22-25`), with the catalog's cases mapped to this page: a 100-byte host, a 160-byte path, a 64-byte client label, an IPv6 /64 source, 0 and 20 000 records, a count of 1 234 567, every status class, every error class, a record with `qb = rb = -1`, a platform with no calls, the Off/Unavailable/Empty states. TC-899 renders the escaping cases.

The review of T3 is reported as the `| Before | After | Why |` table (`.claude/rules/frontend.md:66-77`).

## 12. Monitoring data is not an event

BR-05 closes the **event catalog**: "A new event type is a schema change to the fold, not a free addition" (`pustaka/docs/product/brd.html:1230`); BR-02 says every change emits an event (`:1227`); BR-01 says state lives in the graph and anything in memory must be reconstructible (`:1226`). A traffic record is none of those things:

- **It records no change.** A call to a platform or to the API is an observation; whatever the call changed (a task, a defect, a watch) is already recorded by the code that handled it, through the events it already emits. A record is a second, lossy view of the wire, never the authority.
- **Nothing reads it to decide anything.** No fold, scheduler, gate or handler reads Redis; the Traffic page is its only reader. Deleting every record changes no state and no behaviour.
- **It is allowed to be lost** — dropped under load (§9), trimmed after the window (§5.3), erased on a Redis restart (§4.1) — which an event, append-only and never updated (BR-04, `:1229`), must never be.
- **It lives outside the graph**, in a store with no schema statement and no event type: the catalog (`dashboard/events.go`, 66 types) and the DDL are unchanged, and `check-stacks.sh`'s landing-page figures (`check-stacks.sh:327-348`) do not move.

BR-66 (§14) writes this down, so a later change cannot turn the trace into a source of state without changing a rule.

## 13. Security

### 13.1 What is exposed, and to whom

| Data | Where | Who sees it |
|---|---|---|
| Outbound: platform, host, templated path, method, status, timings, sizes, error class | Redis; the Traffic page | signed-in back-office accounts |
| Inbound: route pattern, method, status, timings, sizes, source address, via, client label, product id | Redis; the Traffic page | signed-in back-office accounts |
| Counters | dashboard memory; the page | the same |

No new public surface: the page and its poll are back-office routes behind the session gate (BR-55); there is no JSON export, no API route and nothing on `/healthz`. Redis has no port (§4.1).

### 13.2 Personal data: source addresses

A source address can identify a person (someone calling the API from home). Controls: retention of at most the window (default 48 h, at most 168 h) and the cap; no persistence, so a Redis restart erases all of it; never written to the dashboard log (§9.3), the event log, Brain Town, a packet, a notice or the graph; shown only to signed-in accounts, `no-store` (§10.1). An IPv6 source is already reduced to its /64 (`staging.go:307-316`). Client labels and product ids are operator-chosen identifiers, not personal data.

### 13.3 Secrets never recorded

By construction (§7.3, §8.4): the recorder reads a fixed list of request and response members, none of them a header, a body, userinfo, a query, a fragment or an error text; the inbound label comes from the authenticated principal's configured name, never from the `Authorization` header. By test: TC-888 (outbound) and TC-893 (inbound) put known secret strings in every forbidden place and assert that no stored field contains them.

### 13.4 Redis itself

- **Reachability:** only the dashboard container, over `gaos-prod-traffic` (`internal: true`); no published port; Redis has no route out (§4.2).
- **No authentication** — residual: any code running inside the dashboard container can read and write the streams. That code already holds every GAOS credential (`compose.prod.yaml:208-228`), so Redis gives it nothing new; a password file would only add a secret.
- **Dangerous commands:** Redis ≥ 7 refuses `CONFIG SET` of protected settings, `DEBUG` and `MODULE` by default (§4.1); the container runs as a non-root user with no capabilities, a read-only root and no disk.
- **Forged records** (if the container boundary were crossed): the page parses every field strictly, bounds every string and escapes everything through `html/template` (TC-899); a forged record can mislead the page, not execute in it. No `template.HTML` value is built from a record (`.claude/rules/frontend.md:52-58`).

### 13.5 Availability

An inbound flood is bounded twice: the per-source limit (§8.5) and the cap. The queue is bounded (§9.1), Redis memory is bounded (§4.1), the page's read is bounded (`COUNT <cap>`) and cached (§10.3). Nothing in GAOS waits on Redis (§9.4).

### 13.6 Supply chain

No Go dependency (§6). One new image, pinned by version and digest, released at least two weeks before it is used (§4.1), its pin asserted by `check-stacks.sh` (§4.4).

### 13.7 What this does not do

It does not block anyone: visibility replaces the firewall step at the operator's choice (D-107), it does not implement it. The firewall procedure stays in the edge README as optional hardening (§17).

## 14. Registers

Verified on `main` at `fc0f9c3` (see the header).

- **D-107** (new, 2026-11-27): "GAOS records its outbound calls to external platforms and the calls made to its machine API, and shows them on a back-office Traffic page, instead of requiring the host firewall on the tunnel port. Context: D-106 left a manual firewall step (edge README §5) whose first instruction is to sample who calls the port; the operator prefers to see the callers (operator instruction of 2026-11-27, design `docs/designs/traffic-monitor.md` §0). Decision: two capped Redis streams in a Redis that only the dashboard can reach, no persistence, the newest `GAOS_TRAFFIC_MAX` (default 5000) per direction and nothing older than `GAOS_TRAFFIC_HOURS` (default 48); a stdlib RESP client, no dependency; a recording transport on the eight external clients (ArcadeDB excluded) and an inbound recorder in the gate for the machine API, recording the source the edge sets and whether it came through the edge; never a header, body, query, token or credential; recording never blocks or fails a call; any signed-in account may view the page. The firewall step becomes optional hardening. Residuals: the trace is lossy by design (drops are counted and shown); Redis has no password, its network admits only the dashboard; source addresses are kept for the window."
- **BR-66** (new, in §9.8 beside BR-52 and BR-55, `brd.html:1340-1358`): "GAOS keeps a short trace of the calls it makes to external platforms and of the calls made to its machine API — time, direction, platform or route, method, host, a templated path, status, duration, sizes and an error class, and for an inbound call the source address, whether it came through the edge, the authenticated client's name and the product id — and never a header, a body, a query string, a token or a credential. The trace lives only in the deployment's own Redis, holds at most the newest configured number of calls per direction and none older than the configured window, and is never persisted. It is not an event and nothing in GAOS reads it to decide anything. Recording never delays or fails a call: a call that cannot be recorded is counted as dropped. Any signed-in back-office account may view the trace; nobody may change it. *(D-107)*"
- **D-106** — unchanged. The edge README's §5 gains a note that the step is optional since D-107 (§17).
- **BR-05, BR-01, BR-02, BR-04** — unchanged (§12). **BR-55** — unchanged: the two new routes are back-office routes, refused signed out. **BR-52** — unchanged: no route, credential or answer of the machine API changes; recording observes it. **BR-63** — unchanged and still held: a WhatsApp number is never in a path, and a path segment with digits is templated anyway (§7.4).
- **QA test plan:** **TC-884 … TC-900** (17 rows: 16 Go tests and one `check-stacks.sh` row), in a new section `#go-traffic` under `#implemented`; **TC-019**'s quoted message gains ` redis` (§4.5); **TC-725**'s row and prose say twelve page routes and ten views (`urls_test.go:233-234` asserts the count, §17); **TC-431**'s table (`readOnlyGET`, `auth_test.go:259-292`) gains `GET /traffic` and `GET /ui/bo/traffic/live` — its prose names no route, so it changes only if it quotes the count.
- **Counts:** cases 883 → **900** ("nine hundred"); Go tests 745 → **761** ("seven hundred and sixty-one"), `dashboard/` 720 → **736**; BRD "Sixty-five rules" → "**Sixty-six rules**"; decision log "One hundred and five entries" → "**One hundred and six entries**" (105 distinct `D-` rows today, `grep -oE '<td>D-[0-9]{3}</td>' … | sort -u | wc -l`), and its mirror in `pustaka/docs/assets/toc/product.js:129`. Open questions unchanged ("All forty-five in one place").
- **Release v0.69.0**, minor: `pustaka/docs/changelog.html` and `product.semver` in `pustaka/docs/assets/toc.js:22`, one change.
- **Not added:** an event type, a task status, a notice kind, a label, a schema statement, a packet kind, a docs CSS class (the new page uses the authoring spec's existing components), an open question.

## 15. Tests

Every Go test is one `func Test…` and one register row (`.claude/rules/qa-test-plan.md:132-135`). A fake Redis for the tests is a `net.Listen("tcp", "127.0.0.1:0")` server in `resp_test.go` that parses RESP2 commands, records them and answers from a script; no real Redis is needed for `go test ./...`.

| TC | Test (file) | Kind | Asserts |
|---|---|---|---|
| TC-884 | `TestRESPRoundTripsTheTrafficCommands` (`resp_test.go`) | Positive | `PING`, `XADD … MAXLEN ~ n *`, `XTRIM … MINID ~`, `XREVRANGE … COUNT`, `XLEN` are encoded byte-exact and each reply type (simple, error, integer, bulk, null bulk, nested array) is parsed; a pipeline of 128 returns 128 replies in order |
| TC-885 | `TestRESPRefusesMalformedOrOversizedReplies` (`resp_test.go`) | Negative | a line over 1 KiB, a bulk over 64 KiB, an array over 50 000, nesting over 3, a negative or non-numeric length, a truncated reply and an unknown type byte are each an error, close the connection, and allocate nothing from the declared length |
| TC-886 | `TestTrafficConfigDefaultsAndClamps` (`traffic_test.go`) | Positive, negative | empty → off / 5000 / 48; `abc` → defaults; `10` → 500; `999999` → 20000; `0` hours → 1; `500` hours → 168; an address without a numeric port → off with its reason |
| TC-887 | `TestOutboundPathIsTemplated` (`traffic_test.go`) | Positive, negative | the examples of §7.4, a 13-segment path, a 300-byte path, `%2F`, an e-mail, a phone-number segment, an empty path; the template never contains a query or a fragment |
| TC-888 | `TestRecordingTransportKeepsNoSecret` (`traffic_test.go`) | Negative | a request with `Authorization: Bearer <s1>`, userinfo `<u>:<s2>`, query `?token=<s3>`, fragment `#<s4>`, body `<s5>` and a response header and body holding `<s6>` is recorded; every stored field is compared with the six strings and none contains any; the fields are exactly the outbound set of §5.2; the caller receives the response unchanged |
| TC-889 | `TestRecordingTransportClassifiesErrors` (`traffic_test.go`) | Positive, negative | a client timeout → `timeout`, a canceled context → `canceled`, a refused port → `refused`, a TLS server with an unknown CA → `tls`, a server that closes mid-headers → `reset`; `s = 0` and the stored record holds no error text |
| TC-890 | `TestTrafficRecordingNeverBlocks` (`traffic_test.go`) | Negative | with a store that accepts and never answers, and with one that refuses: 20 000 recorded outbound calls through a wrapped client to an `httptest` server each finish within the same bound as through the bare transport (the test asserts the wrapped run is within 2× the bare run plus 100 ms), the queue never blocks, and `dropped` equals the records not written |
| TC-891 | `TestTrafficWriterTrimsByLengthAndAge` (`traffic_test.go`) | Positive | against the fake server, every `XADD` carries `MAXLEN ~ <cap>`; after a trim tick both keys get `XTRIM … MINID ~ <now − window>-0`; an `OOM` reply counts one drop; a dropped connection is redialled with backoff and the transition is logged once |
| TC-892 | `TestEveryExternalClientIsRecorded` (`traffic_test.go`) | Positive | with recording on, the clients built by `newQaOutbound`, `AIMClient.client`, `newBrainTown`, `newBTConsumer`, `newGHClient`, `newGHWriter`, `newWANotifier` and `newTaskMgrClient` each have a `*recordingTransport` with the label of §7.1; the Arcade clients do not; with recording off each has today's transport |
| TC-893 | `TestInboundRecordsRouteSourceAndClient` (`traffic_test.go`) | Positive, negative | through `routes()`: a QA-token call records the pattern (not the path), `c = qa-tools`, `pid`, `src` from `X-Real-IP`, `via = edge` for `203.0.113.9` and `direct` for `10.10.0.9`; a wrong token records `s = 401`, empty `c`; a Basic unit-results call records `basic:unit-results`; an unmatched `/v1/nope?x=<secret>` records `(no route)` and no path; no field holds the bearer token, the Basic password or the query; a back-office route and `/healthz` record nothing |
| TC-894 | `TestInboundRecorderKeepsTheResponseController` (`traffic_test.go`) | Positive | through the wrapped writer, `apiDecodeLimit`'s `SetReadDeadline` succeeds (no `http.ErrNotSupported`), `Flush` reaches the recorder, status and byte count are those the handler wrote |
| TC-895 | `TestInboundRecordsArePerSourceLimited` (`traffic_test.go`) | Negative | 400 calls from one source in one minute store 300 and count 100 `suppressed`; a second source is unaffected; the next minute stores again; the 4097th distinct source shares the `other` bucket |
| TC-896 | `TestTrafficSummaries` (`traffic_view_test.go`) | Positive | from a fixed snapshot: per-platform calls, 4xx, failed, nearest-rank p50/p95 over answered calls only, last call; all eight platforms present in order; inbound pairs sorted by last call, capped at 50 with the "more" count; records older than the window are not counted; the direct-sources tile |
| TC-897 | `TestTrafficFiltersAndPages` (`traffic_view_test.go`) | Positive, negative | each `dir`, `platform`, `src`, `status` value filters as §10.3 says; an invalid value of each is ignored and not echoed; paging by 10; the poll URL keeps the filters |
| TC-898 | `TestTrafficPageStates` (`traffic_view_test.go`) | Positive, negative | Off, Unavailable, Empty, Filtered empty and Normal render their texts; the page and the poll answer `no-store`; signed out, `GET /traffic` and `GET /ui/bo/traffic/live` are refused (401) and touch no store; a failed read is not retried within 15 s |
| TC-899 | `TestTrafficPageEscapesRecordedText` (`traffic_view_test.go`) | Negative | a snapshot whose host, path, route, client and source hold `<script>`, `"><img onerror=…>` and a 2 KiB string renders them escaped and cut to their bounds; an unknown platform renders as `other` |
| TC-900 | `check-stacks.sh` | Positive, negative | the nine `ok` lines of §4.4; each prints `FAIL` with the same message when its condition is broken |

Every existing test keeps passing unchanged except the three whose assertions move with the new view: `TestADeepURLRendersThePage` (`urls_test.go:233-234`: eleven page patterns → twelve), `TestEveryRouteIsClassified` (`auth_test.go:259-292`: two `readOnlyGET` entries) and, through its list, `TestEveryHtmxLinkHasAPageHref` (`urls_test.go:441-447`, which iterates `pageViews` and needs no edit).

## 16. Phases

Each phase ends with `(cd dashboard && go vet ./... && go test ./... && go test -race ./...)`, `(cd pustaka && go run . check ./docs && go test ./...)`, `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` green, and **writes its own register rows and every count in the same commit** (`.claude/CLAUDE.md` invariant 1): `qa-test-plan.html` (rows, prose, lede, intro), `pustaka/docs/assets/toc/product.js`, `deploy/check-workspace.sh` (the Go-test number and the case words), `README.md` and `.claude/rules/qa-test-plan.md` (highest TC, totals, range map).

| Phase | Builds | TC |
|---|---|---|
| T1 Client and recorder | `resp.go`; `traffic.go` without wiring: config, queue, writer, trim, `recordingTransport`, path template, error class, the inbound recorder and writer types, the per-source limit | TC-884 … TC-891 |
| T2 Wiring | `trafficRec` set in `serve()`; the eight constructors wrap (§7.1), `ghConfig.platform`; the gate records (§8.1); `apiGate` and `jenkinsGate` set the label | TC-892 … TC-895 |
| T3 Page (the `gaos-frontend` agent, with the three skills) | `traffic_view.go`; `pageViews`, `noStoreView`, `buildBO`, `BOView`, the poll route; `bo-traffic`, `traffic-live`, the nav item; `.tf-num`, `.tf-cut`; `readOnlyGET`, `urls_test.go` count | TC-896 … TC-899; TC-725, TC-431 updated |
| T4 Deploy, docs, release | the compose service and network and settings (after the uid check of §4.1); `check-stacks.sh`; `rebuild-restart.sh`; `STACKS.md`; `infra/environments.html` and `toc/infra.js`; the edge README note; `integrations/traffic.html` and `toc/integrations.js`; D-107, BR-66 and their counts; the changelog and `semver` 0.69.0 | TC-900; TC-019 updated |

**Deploy (after approval and merge):** tag `v0.69.0`; `./deploy/rebuild-restart.sh prod deploy --tag v0.69.0` on each prod instance with its usual environment line (the new variables may stay unset); then, per instance: `docker compose -f deploy/stacks/compose.prod.yaml ps` shows `redis … (healthy)` and no port; `docker compose … exec redis redis-cli CONFIG GET enable-protected-configs` prints `no`; open **Traffic**, see a QA-tool call within a minute of a QA sync, and the instance's own inbound callers; `docker compose … stop redis`, check that a QA sync and an API call still succeed and the page says Unavailable; `start redis`. The deploy note records the date, the image digest and the conntrack cross-check of §0.

## 17. Change list

New files (7):

1. `dashboard/resp.go` — §6.
2. `dashboard/resp_test.go` — TC-884, TC-885, the fake server.
3. `dashboard/traffic.go` — §5, §7, §8, §9.
4. `dashboard/traffic_test.go` — TC-886 … TC-895.
5. `dashboard/traffic_view.go` — §10.
6. `dashboard/traffic_view_test.go` — TC-896 … TC-899.
7. `pustaka/docs/integrations/traffic.html` — the page, the settings, what is and is not recorded, `edge`/`direct`, the states, the redeploy line; built from `pustaka/ai/AUTHORING_SPEC.md`'s existing components only.

Changed files (items 8–43; item 33 is two files, so 37 files):

8. `dashboard/main.go` — `trafficRec` and the writer started after `main.go:195-196`; `server.traffic`; `GET /ui/bo/traffic/live` beside `main.go:607`.
9. `dashboard/auth.go` — the inbound recorder in `gate` (`:541-590`).
10. `dashboard/api_v1.go` — the label in `apiGate` (`:329-339`).
11. `dashboard/staging.go` — the label in `jenkinsGate` (`:404`).
12. `dashboard/qaclient.go` — wrap at `:145`.
13. `dashboard/aimanager.go` — wrap in `client()` (`:131-136`).
14. `dashboard/braintown.go` — wrap at `:292`.
15. `dashboard/braintown_consumer.go` — wrap at `:321-328`.
16. `dashboard/github.go` — `ghConfig.platform` (`:108-112`); wrap at `:179-180`.
17. `dashboard/githubwrite.go` — `platform: "github-write"` in `ghWriteConfigFromEnv` (`:135`).
18. `dashboard/wa.go` — wrap at `:180-181`.
19. `dashboard/taskmgr.go` — wrap at `:233`.
20. `dashboard/urls.go` — `pageViews` (`:31-35`).
21. `dashboard/integrations.go` — `noStoreView` (`:28`).
22. `dashboard/handlers_bo.go` — `buildBO` case (`:254-265`).
23. `dashboard/view.go` — `BOView.Traffic` (`:247-250`).
24. `dashboard/templates/backoffice.html` — the nav item (`:96`), the view switch (`:129`), `bo-traffic` and `traffic-live`.
25. `dashboard/static/app.css` — `.tf-num`, `.tf-cut`.
26. `dashboard/auth_test.go` — `readOnlyGET` (`:259-292`).
27. `dashboard/urls_test.go` — the page-pattern count (`:233-234`).
28. `deploy/stacks/compose.prod.yaml` — §4.1 – §4.3.
29. `deploy/check-stacks.sh` — §4.4.
30. `deploy/rebuild-restart.sh` — §4.5 (`:15`, `:26`).
31. `deploy/STACKS.md` — the service and the network in the topology; a "The traffic store" paragraph.
32. `deploy/edge/README.md` — §5 (`:170-209`) gains, under its heading: "Optional since v0.69.0 (D-107): the operator chose visibility over this filter. The back office's Traffic page shows every caller of the machine API, marked `direct` when it did not come through this box. Apply the rules below only if a direct caller there should not exist."
33. `pustaka/docs/infra/environments.html` and `pustaka/docs/assets/toc/infra.js` — "four services" (`environments.html:17, :30`; `infra.js:41`) → prod's five, Redis named as the traffic store.
34. `pustaka/docs/assets/toc/integrations.js` — the page's entry, after `pull-requests` (`:96-107`).
35. `pustaka/docs/product/qa-test-plan.html` — 17 rows, `#go-traffic`, TC-019 / TC-725 / (TC-431) prose, the counts.
36. `pustaka/docs/product/brd.html` — BR-66, "Sixty-six rules" (`:1218`).
37. `pustaka/docs/product/decision-log.html` — D-107, "One hundred and six entries" (`:18`).
38. `pustaka/docs/assets/toc/product.js` — case, Go-test and decision counts (`:129`, `:162`, `:168`).

The release and count files:

39. `pustaka/docs/changelog.html` — v0.69.0.
40. `pustaka/docs/assets/toc.js` — `semver: "0.69.0"` (`:22`).
41. `deploy/check-workspace.sh` — `761`, "seven hundred and sixty-one", "nine hundred", "One hundred and six entries", "Sixty-six rules" (`:113-133`).
42. `README.md` — 761 / 736 / 900 (`:143-145`).
43. `.claude/rules/qa-test-plan.md` — highest TC-900, next TC-901, the counts, the range map (`check-stacks.sh` gains TC-900; `dashboard (go test)` gains TC-884…TC-899).

**Arithmetic.** 7 new files + 37 changed files (36 items, item 33 being two files) = **44 files**. Go tests: 16 new `func Test…` (TC-884 … TC-899) → 745 + 16 = 761, all in `dashboard/` → 720 + 16 = 736. Register rows: 16 + 1 (TC-900) = 17 → 883 + 17 = 900.

Files the problem names and where they appear: `compose.prod.yaml` (28), `check-stacks.sh` (29), `STACKS.md` (31), `api_v1.go` (10), the unit-results gate (11), the eight client files (12–19), `arcade.go` (deliberately unchanged, §7.1), `deploy/edge/README.md` (32).

## 18. Alternatives

1. **An in-memory ring buffer in the dashboard process.** No new service at all, and the smallest change. Rejected: the dashboard is the container every release replaces (`rebuild-restart.sh` builds it from each tag), and several releases a day are normal for this project (v0.66.x … v0.68.0 landed within days); a monitoring page that is empty after each deploy cannot answer "who called in the last day". It also could not survive the dashboard's own crash. Redis outlives the dashboard's restarts at the cost of one small, isolated service, and its `MAXLEN` / `MINID` give the retention the operator described without code. The operator also named Redis streams.
2. **ArcadeDB.** Already deployed. Rejected: it is the store of record, shared and reached over the tunnel (`deploy/STACKS.md`, "The database"); a write per call adds load and latency to every external call path, retention needs a pruning job and schema statements, and the temptation to treat records as events is exactly what BR-05 and BR-04 forbid (§12).
3. **Prometheus (and a dashboard such as Grafana).** Rejected: metrics aggregate, and the page needs per-call rows with source addresses — a high-cardinality label set Prometheus advises against; it adds two services and a second UI with its own authentication outside the back office, and the Go client library is a dependency (§6).
4. **Traefik's access log.** Already records inbound `ClientHost` (`deploy/edge/README.md:151-160`). Rejected as the page's source: inbound only, no client label, container logs not in the back office, and no outbound. It remains the cross-check for §8.3.
5. **`go-redis`.** Rejected by the module's dependency policy (§6).
6. **Valkey instead of Redis.** Equivalent for this design (same protocol and commands), BSD-licensed. Not chosen because the operator asked for Redis and its licence imposes nothing on an unmodified, unlinked service (§4.1); switching later is an image line.
7. **Recording only outbound** (the literal request). Rejected: the request replaces a firewall step that is about inbound callers (§0).

## 19. Non-goals

- Blocking or rate-limiting anyone; the firewall stays optional (§13.7).
- Alerts, notices or WhatsApp messages about traffic.
- History beyond the window, persistence, export or download.
- ArcadeDB calls; back-office (browser) traffic; `/healthz`; static files; calls the edge refuses before they reach Traefik.
- Request or response bodies, headers, query strings — ever (BR-66).
- Per-product access scoping of the page, or editing or clearing the trace.
- Redis on the retired dev stack or the local `compose.yaml`.
- A chart or a new vendored library (§11.10).
- Changing any client's timeout, retry, redirect or TLS behaviour.

## 20. Assumptions, labelled

1. The alpine image's `redis` user is uid 999, gid 1000 — checked in T4 before the compose change (§4.1).
2. Redis 8.10.3 defaults `enable-protected-configs`, `enable-debug-command` and `enable-module-command` to `no` — checked on the deployed container (§16).
3. 300 records per source per minute exceeds every legitimate caller — visible on the page after deploy as `suppressed = 0`; if not, the constant changes in a fix (§8.5).
4. Go's `ServeMux` sets `PathValue` on the request passed to `ServeHTTP` — asserted by TC-893 (§8.1).
