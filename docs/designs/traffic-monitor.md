# Traffic monitor — GAOS's outbound and inbound calls, on one back-office page

> **Status:** Approved (rev 3; 2026-11-27, under the operator's standing instruction 'dont ask for any of my further confirmation and decision. work and collaborate among agents yourselves' and the request 'once done, make pr, merge to main. push. deploy latest'; round 1 five lenses + security, round 2 zoom-out, critic, security; no Blocker, no Critical/High open). The delegation is recorded in `docs/designs/reviews/traffic-monitor-delegation.md`. Round 1 (rev 1): `docs/designs/reviews/traffic-monitor-{comprehension,zoom-out,critic,readiness}-1.md` and `traffic-monitor-security-review-1.md`, B=2 M=15 N=40, security High=1 Medium=5 Low=4; triage §21. Round 2 (rev 2): `traffic-monitor-{zoom-out,critic}-2.md` and `traffic-monitor-security-review-2.md`, B=0 M=8 N=17, security Medium=2 Low=6; triage §22.
> **Policy:** `docs/rules/gaos-design-review.md` (the security lens is required here, `:92-100`).
> **Related:** `docs/designs/release-b4-unit-tests.md` (D-106: the edge-set source address reused here, and the host-firewall step re-ranked here), `docs/designs/integrations-status-chips.md`, `docs/designs/gaos-pr-overview.md` (the live list this page copies).
> **Paths.** A bare file name (`staging.go`) is in `dashboard/`; every other path is from the repository root. Anchors are against `main` at v0.68.0 (`fc0f9c3`).
> **Registers (verified on `main` at `fc0f9c3`):** next free **D-107** (the log ends at `d-106`, `pustaka/docs/product/decision-log.html:2167`), **BR-66** (the BRD ends at BR-65, `brd.html:1310`), **TC-884** (highest row TC-883, `qa-test-plan.html:974`; `deploy/check-workspace.sh:105-111` asserts rows = highest). Release **v0.69.0** (`pustaka/docs/assets/toc.js:22` is `0.68.0`).
> **Private names.** The QA platform is "the QA tool" and the public proxy "the edge" (D-085).

## 0. Why, and the scope

**The operator's words** (2026-11-27, verbatim), answering an item of the v0.68.0 deploy report, which the reply quotes:

> "regarding this: ``` Still manual, not applied: a firewall rule on each VM that limits which tunnel machines can reach GAOS directly. It needs a day of watching who connects first, so it doesn't cut off the QA tool. The commands are in deploy/edge/README.md.``` . as long as we have dashboard / monitoring page in GAOS to view traffic outbound to QA tool (and other external platform), will be enough. add them if there is none yet. use `https://aiforui.dev/skills` for better UI/UX. you can use redis stream just to limit last N record only and do auto retention. we dont need a long period of traffic monitoring data anyway. since it will be used only for periodic monitoring."

**What that decides.** The quoted item is the D-106 host firewall (`deploy/edge/README.md:170-209`), still unapplied. A monitoring page "will be enough": the operator chooses **visibility over the firewall as the required step**. The firewall stays documented as **recommended hardening** (D-107), and the README says what the page can and cannot see (§17 item 33). The firewall's question is "which tunnel machines reach GAOS directly" (`deploy/edge/README.md:177-182`), so the page shows both directions:

- **Outbound** — every call GAOS makes to an external platform (§7), the operator's own words.
- **Inbound** — who reaches GAOS, from where, and whether through the edge or directly over the tunnel (§8), which is what the quoted item was about.

**The required step's cadence.** "Periodic monitoring" becomes: **review the Traffic page at least daily, and always within the shortest "kept since" of the `in:direct:other` stream the page shows** (§10.2). A missed day loses per-call detail but not the fact that an unknown peer called: the since-boot direct-source summary keeps it (§8.7). No formal review record is kept (§22, the declined part).

**Verified facts this design starts from.**

1. GAOS has no traffic page: the back-office views are the nine in `urls.go:31-35`.
2. GAOS runs no Redis: the prod stack is `traefik`, `landing`, `dashboard`, `docs` (`deploy/stacks/compose.prod.yaml:31-258`; `deploy/rebuild-restart.sh:26`).
3. The dashboard is stdlib-only by policy (`dashboard/go.mod:1-7`).
4. Every external HTTP client is built in one of eight places (§7.1), plus ArcadeDB's (`arcade.go:74-75`), excluded.
5. Every request the dashboard receives passes `server.gate` (`auth.go:541-590`). Traefik routes only `/app/` and `/api/` to the dashboard; everything else goes to the landing container, and the `/app` and `/docs` slash redirects are answered by Traefik itself (`deploy/traefik/dynamic.yml:51-55`, `:78-93`).
6. The caller's address is computed already: `jenkinsSource` (`staging.go:276-297`) takes `X-Real-IP`, which Traefik trusts from the edge alone (`deploy/stacks/traefik.yml:52-53`; `deploy/edge/README.md:158-164`), and reduces IPv6 to its /64 (`staging.go:307-316`).

**Success.** After v0.69.0 is deployed on both prod instances, a signed-in operator opens **Traffic** and sees, per platform and per inbound kind, what was called, by whom, how it answered and how far back the kept records reach, plus every direct source since the dashboard started. Stopping Redis changes nothing anywhere else (with the one residual of §8.6), and the page says the store is unavailable.

**The firewall cross-check.** For the sampling period (at least 24 hours), the operator logs new connections to the tunnel port with `sudo conntrack -E -e NEW -p tcp --orig-port-dst 18301 -o timestamp > ~/gaos-conntrack-<date>.log` (stopped at the end of the period; `conntrack -L` lists only the current table and cannot cover a period). The check **passes** when every peer in the log other than the edge (`10.10.0.1`) appears on the page's direct-source summary, or is explained by a named exclusion:

| Exclusion | Why the page cannot see it | How the operator explains it |
|---|---|---|
| E1 — a peer whose requests were all outside `/app/` and `/api/`, or the two slash redirects `/app` and `/docs` | Traefik sends them to the landing container or answers them itself (`deploy/traefik/dynamic.yml:51-55`, `:78-93`); the dashboard never sees them | `docker compose -f deploy/stacks/compose.prod.yaml logs traefik`: the access log's first field is the client (`deploy/edge/README.md:158`) |
| E2 — a TCP connection that sent no complete HTTP request (a port scan) | No request reaches Traefik's router | the conntrack log shows the connection with no matching Traefik log line |
| E3 — a peer not in the summary because the summary did not cover the whole period | the summary is held since the dashboard started (§8.7) | **only if** the dashboard started before the sampling period began is the summary complete; if it restarted during the period, a peer missing from it is **inconclusive**, not explained, and the period is repeated |

The check is **inconclusive** (not passed) whenever E3 applies or the summary shows untracked sources (§8.7). Traefik's access log is not the page's inbound source (§18 alternative 4).

## 1. Glossary

| Term | Meaning |
|---|---|
| Record | One call, as the fields of §5.2. Not an event (§12). |
| Outbound | A call GAOS makes through one of the clients of §7.1. |
| API call | A request to a machine-API route (a bearer pattern, `main.go:666-669`) or to an unmatched `/v1/` path (§8.2). |
| Route class | For an inbound record: `api`, or, for a direct request elsewhere, `backoffice`, `public`, `healthz`, `static` or `unmatched` (§8.1). |
| Platform | The closed label of an outbound client (§7.1). |
| Source | The caller's address as `jenkinsSource` computes it (`staging.go:276-297`): IPv4 whole, IPv6 as its /64. |
| Edge hop | `10.10.0.1`, the one entry of Traefik's trust list (`traefik.yml:53`). |
| Via | `edge` when the last `X-Forwarded-For` entry is the edge hop, else `direct` (§8.3). |
| Principal | The machine-API identity that authenticated: `admin`, a clients-file name (`api_v1.go:54-55`), `basic:staging`, `basic:unit-results`; none when nothing authenticated. |
| Stream | One capped Redis stream (§5.1): eight outbound, four inbound. |
| Retention window | 48 hours: older records are trimmed and never counted (§5.3). |
| Cap | A stream's maximum number of records (§5.1). |
| Drop | A record not stored: the queue was full (§9.1) or Redis did not answer or refused it (§9.2). Counted and shown. |
| Evicted | A record removed because its stream reached its cap. Visible through the stream's "kept since" (§10.2). |
| Direct-source summary | The in-memory, since-boot table of direct sources that eviction never touches (§8.7). |
| Public address | One that is not loopback, private, link-local, unspecified or in `100.64.0.0/10`. Only public addresses are masked (§10.1). |

## 2. What changes

| Area | Change | Section |
|---|---|---|
| Stack | A `redis` service pinned by version and digest, on a pinned `internal: true` network shared with the dashboard only, at a static address outside the dynamic range; no port, no disk, `maxmemory 64mb` `noeviction`; `default` user off, a `gaos` user with a password from a mounted secret file and an ACL of `XADD`, `XTRIM`, `XREVRANGE` on `gaos:traffic:*` plus `PING`; healthcheck; non-root, read-only | §4 |
| Secret | `deploy/stacks/prod.traffic-redis-auth`, mode 600, generated by `rebuild-restart.sh` when absent; `.gitignore` gains `deploy/stacks/*.traffic-*` | §4.1, §4.5 |
| Settings | `GAOS_REDIS_ADDR` (an IP inside the traffic subnet), `GAOS_REDIS_AUTH_FILE` | §4.3 |
| Go | `resp.go`: a minimal RESP2 client, no dependency | §6 |
| Go | `traffic.go`: the recorder, queue, writer, recording transport, path template, the gate recorder, the direct-source summary | §7–§9 |
| Go | `traffic_view.go`: the page | §10 |
| Go | Seven clients wrap their transport; TaskMgr records in `do`; `apiGate`, `jenkinsGate` name the principal; two deny-lists gain the Redis host | §7, §8.4, §13.4 |
| UI | **Traffic** after Integrations | §10, §11 |
| Checks | `check-stacks.sh` "the traffic store" block and the secret file | §4.4 |
| Deploy | `rebuild-restart.sh`: `redis` service, secret generation and linking, a health warning; the deploy agent's service list | §4.5 |
| Docs | `integrations/traffic.html` (new); `STACKS.md`; `infra/environments.html`; edge README §5 | §17 |
| Registers | D-107, D-106 amended, BR-66, TC-884 … TC-903, v0.69.0; no event type, task status, schema statement or docs CSS class | §14 |

## 3. Data flow

```
 outbound                                     inbound
 a GAOS client (§7.1)                         caller ─► the edge (10.10.0.1) ─┐      tunnel peer / this VM ─┐
   │ http.Client.Do                                                           ▼                             ▼
 recordingTransport.RoundTrip                 Traefik ── appends the delivering hop to X-Forwarded-For ── /app, /api ─► dashboard
   │ (TaskMgr: taskmgrClient.do records)      (everything else ─► landing, or Traefik's redirects: E1)                     │
   ▼                                                                                          server.gate, at its top: src, via, tag, wrap, defer
 rec.out ────────────────┐                                                                     API call → full record
                         │                                                                     direct, any route, any exit → reduced record
                         │                                                                     direct → since-boot summary (512 sources)
                         ▼                                                                     edge, not API → nothing
          bounded queue 4096 (select … default: drop++; anonymous records stop at ¾)  ◄── rec.in ◄──┘
                         │
                         ▼  one writer: AUTH once per connection; batch ≤128 or 250 ms, pipelined
          XADD gaos:traffic:<stream> MAXLEN ~ <cap> * …     every 60 s: XTRIM <each stream> MINID ~ <now − 48 h>
                         │
                         ▼  redis 172.30.14.2 on gaos-prod-traffic (internal); user gaos: xadd xtrim xrevrange ping
                         ▲
                         │  12 × XREVRANGE … COUNT <cap>, pipelined, at most once per 15 s (cached)
          traffic_view.go ── Traffic page, polled every 30 s
```

## 4. The traffic store

### 4.1 The service

Added to `deploy/stacks/compose.prod.yaml` after `dashboard` (`:59-238`), before `docs` (`:240`):

```yaml
  # D-107: the traffic store. Twelve capped Redis streams of recent calls in and out;
  # nothing reads them but the Traffic page. Its own internal network, shared with the
  # dashboard alone, at a fixed address: no port, no route out, no DNS name in use.
  # No disk: a Redis restart -- or a whole-stack restart -- empties it, by design.
  # The default user is off; the dashboard signs in as "gaos" with the password in
  # prod.traffic-redis-auth (rebuild-restart.sh creates it). traffic-redis.sh writes the
  # ACL file into the tmpfs at start, holding only the password's SHA-256.
  # GAOS starts and serves without it: there is no start-order dependency either way.
  redis:
    image: redis:8.10.3-alpine@sha256:7958cfbb9d152f87dadfbc89f1b0b15938f0b81ff8982feb327476f4911935c3
    restart: unless-stopped
    entrypoint: ["/bin/sh", "/usr/local/bin/traffic-redis.sh"]
    volumes: ["./traffic-redis.sh:/usr/local/bin/traffic-redis.sh:ro", "./prod.traffic-redis-auth:/run/secrets/traffic-redis-auth:ro"]
    user: "${GAOS_UID:-1001}:${GAOS_GID:-1001}"
    read_only: true
    tmpfs: ["/data"]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 128m
    cpus: 0.25
    healthcheck:
      test: ["CMD", "/bin/sh", "/usr/local/bin/traffic-redis.sh", "health"]
      interval: 30s
      timeout: 3s
      retries: 3
    networks:
      traffic: {ipv4_address: 172.30.14.2}
```

**`deploy/stacks/traffic-redis.sh`** (new, tracked, read-only mount), in full:

```sh
#!/bin/sh
# D-107: start the traffic store, or check its health. The password never appears in
# an argument, an environment variable of the server, or `docker inspect`: the ACL file
# in the tmpfs holds only its SHA-256, and redis-cli reads it from REDISCLI_AUTH.
set -eu
secret=/run/secrets/traffic-redis-auth
if [ "${1:-}" = health ]; then
  REDISCLI_AUTH="$(tr -d '\n' < "$secret")" redis-cli --no-auth-warning --user gaos ping | grep -qx PONG
  exit $?
fi
[ -s "$secret" ] || { echo "traffic-redis: $secret is empty or missing" >&2; exit 1; }
umask 077
hash="$(tr -d '\n' < "$secret" | sha256sum | cut -d' ' -f1)"
printf 'user default off\nuser gaos on #%s ~gaos:traffic:* resetchannels -@all +ping +xadd +xtrim +xrevrange\n' "$hash" > /data/users.acl
exec redis-server --save "" --appendonly no --maxmemory 64mb --maxmemory-policy noeviction --aclfile /data/users.acl
```

| Choice | Decision | Why |
|---|---|---|
| Image | `redis:8.10.3-alpine`, pinned by tag **and digest** | *A1 (re-checked in T4):* 8.10.3 was released 2026-10-26 and the `8.10.3-alpine` manifest list was last pushed 2026-11-11 with this digest (Docker Hub, read 2026-11-27). "At least two weeks old" is this design's instruction, not a repository policy. T4 re-reads the digest with `docker buildx imagetools inspect redis:8.10.3-alpine`. |
| Start | The image's own entrypoint is replaced by `traffic-redis.sh` | The script writes the ACL file and execs `redis-server`; started as a non-root uid, the official entrypoint would only exec it anyway. |
| Persistence | None: `--save ""`, `--appendonly no`, `/data` a tmpfs | A Redis restart and a whole-stack `rebuild-restart.sh <env> restart` (`deploy/rebuild-restart.sh:368`) empty every stream; a dashboard redeploy does not. The page says so (§10.2). |
| Memory | `--maxmemory 64mb`; `mem_limit: 128m` | The caps of §5.1 total 25 000 records; at ≤ 600 bytes a record (§5.2) that is ≤ 15 MB. |
| Eviction | `noeviction` | Twelve keys; `allkeys-lru` at the limit would delete a whole stream. An `XADD` past the limit is refused (`OOM`) and counted as a drop. The arithmetic says the limit is not reached. |
| Authentication | `default` user **off**; user `gaos`, password from `prod.traffic-redis-auth` (64 hex characters, `openssl rand -hex 32`), ACL `~gaos:traffic:* resetchannels -@all +ping +xadd +xtrim +xrevrange` | Anything that reaches Redis without the password gets nothing; with it, it can add to, trim and read the traffic streams and `PING`, and nothing else (`FLUSHALL`, `DEL`, `XDEL`, `EVAL`, `CONFIG`, `ACL`, another key: `NOPERM`). The password is never an argument (the ACL file holds its SHA-256, `#<hex>` form) and never in the server's environment, so `docker inspect` shows neither. |
| Secret file | Mounted read-only into `redis` and `dashboard`; mode 600, owned by the deploy user | Both containers run as `${GAOS_UID:-1001}:${GAOS_GID:-1001}`, the uid the dashboard already uses for its secrets (`compose.prod.yaml:229-234`), so both can read it and nobody else on the host can. |
| Network | `traffic` only, static `172.30.14.2` | Only the dashboard can reach Redis; Redis has no route out; the dashboard resolves no name. |
| Port | No `ports:`, no `expose:` | Nothing is published beyond Traefik's mapping (`check-stacks.sh:50-54`). |
| Hardening | `read_only`, `tmpfs /data`, `cap_drop: [ALL]`, `no-new-privileges` | — |
| Healthcheck | `traffic-redis.sh health`: an authenticated `PING` | `docker compose ps` shows `healthy`; the password reaches `redis-cli` through `REDISCLI_AUTH` inside the check, never in the healthcheck's arguments. |
| Start order | No start-order key in either direction | GAOS must start, serve and call out with Redis absent (§9.4). |
| YAML shape | Every list in the block is a flow sequence (`[...]`) | `check-stacks.sh:51` counts lines starting with `- "` as port mappings. |

**Licence.** Redis 8 is RSALv2 / SSPLv1 / AGPLv3 at the user's choice; GAOS runs it unmodified as a separate process and neither links nor redistributes it. Valkey (BSD-3) is the drop-in alternative (§18).

### 4.2 The network

`compose.prod.yaml` `networks:` (`:20-28`) gains:

```yaml
  # D-107: the dashboard and the traffic store, nothing else. internal: no route out.
  # PINNED: Redis has the fixed address .2; dynamic endpoints (the dashboard) get .4-.7
  # only, so they can never take it. 172.30.14.0/29 is GAOS's, beside .12 and .13.
  traffic:
    name: gaos-prod-traffic
    internal: true
    ipam:
      config:
        - subnet: 172.30.14.0/29
          ip_range: 172.30.14.4/30
```

*A3 (T4):* `172.30.14.0/29` is free on both prod VMs (`docker network inspect $(docker network ls -q) --format '{{.IPAM.Config}}'`); if not, the next free /29 in `172.30.14.0/24` is used, with the address, range and assertions following. The header's neighbour comment (`:25`) gains ".14 is the traffic network"; the dev-parity comment (`:9-13`) gains "— and the traffic store (D-107), which is prod's alone: dev is retired". The `dashboard` service's `networks: [internal]` (`:238`) becomes `networks: [internal, traffic]`, and its `volumes:` gains `./prod.traffic-redis-auth:/run/secrets/traffic-redis-auth:ro`.

### 4.3 The settings

The dashboard's `environment:` gains, after `GAOS_TASKMGR_CA_FILE` (`:207`):

```yaml
      # D-107: the traffic store, by its fixed address on the traffic network (an IP inside
      # 172.30.14.0/29, never a name), and the password file it shares with the store.
      GAOS_REDIS_ADDR: "172.30.14.2:6379"
      GAOS_REDIS_AUTH_FILE: "/run/secrets/traffic-redis-auth"
```

| Variable | Off when | Valid |
|---|---|---|
| `GAOS_REDIS_ADDR` | empty; a name; an IP outside `172.30.14.0/29` (a constant in `traffic.go`); no numeric port | `172.30.14.x:port` |
| `GAOS_REDIS_AUTH_FILE` | empty; the file missing, empty, not a regular file or readable by group or others | a mode-600 file; its content, trimmed, is the password |

An off reason is logged once and shown on the page. **The caps and the window are constants** (§5.1, §5.3). The boot log prints `traffic: recording to 172.30.14.2:6379, 48 h` or `traffic: off (<reason>)`, never a recorded value or the password. The dev compose (retired) and the local `compose.yaml` get no Redis.

### 4.4 `check-stacks.sh`

A new block after "the proxy never holds the Docker socket" (`check-stacks.sh:91-105`), prod only. Helpers beside `has`/`hasnt` (`:24-30`):

- `svcblock <name>` prints the lines of `$PROD` after the line `  <name>:` up to (not including) the next line matching `^ {0,2}[a-z]` — a service block or a top-level network block.
- `inblock`, `inblockre`, `notinblock`, `notinblockre <name> <pattern>` test `svcblock <name>` with `grep -qF`/`grep -qE`, positive or negative, returning a status; the block below combines them, and each output line is one `if` over its conditions printing one `ok` or one `bad`.

The conditions, numbered (**N = 30**):

| Output line | Conditions |
|---|---|
| `prod pins the Redis image by version and digest` | C1 `hasre $PROD '^[[:space:]]+image: redis:[0-9]+\.[0-9]+\.[0-9]+-alpine@sha256:[0-9a-f]{64}$'` |
| `prod's Redis persists nothing` | C2 `traffic-redis.sh` contains `--save "" --appendonly no`; C3 the redis block's only `volumes:` entries are the script and the secret (`inblock redis 'volumes: ["./traffic-redis.sh:/usr/local/bin/traffic-redis.sh:ro", "./prod.traffic-redis-auth:/run/secrets/traffic-redis-auth:ro"]'`); C4 `inblock redis 'tmpfs: ["/data"]'` |
| `prod's Redis has a memory limit and refuses writes rather than evicting a stream` | C5 the script contains `--maxmemory 64mb`; C6 it contains `--maxmemory-policy noeviction` |
| `prod's Redis turns the default user off and admits the gaos user to the traffic keys and three commands` | C7 the script contains `user default off`; C8 it contains `user gaos on #%s ~gaos:traffic:* resetchannels -@all +ping +xadd +xtrim +xrevrange\n'`; C9 it contains `--aclfile /data/users.acl`; C10 `inblock redis 'entrypoint: ["/bin/sh", "/usr/local/bin/traffic-redis.sh"]'`; C11 `hasntre $PROD '(requirepass|REDIS_PASSWORD|nopass)'` |
| `prod's Redis publishes no port` | C12 `notinblockre redis '^[[:space:]]*(ports|expose):'` |
| `prod's Redis is on the internal traffic network only, at its pinned address outside the dynamic range` | C13 `inblock redis 'traffic: {ipv4_address: 172.30.14.2}'`; C14 `notinblockre redis '^[[:space:]]+internal'`; C15 `notinblock redis 'network_mode'`; C16 `inblock traffic 'name: gaos-prod-traffic'`; C17 `inblock traffic 'internal: true'`; C18 `inblock traffic 'subnet: 172.30.14.0/29'`; C19 `inblock traffic 'ip_range: 172.30.14.4/30'`; C20 exactly two service blocks mention `traffic` (dashboard and redis), counted over `svcblock` of every service name |
| `prod's Redis has an authenticated healthcheck` | C21 `inblock redis '"traffic-redis.sh", "health"'` |
| `prod's dashboard joins the traffic network and reaches Redis by address, with the shared password file` | C22 `inblock dashboard 'networks: [internal, traffic]'`; C23 `has $PROD 'GAOS_REDIS_ADDR: "172.30.14.2:6379"'`; C24 `has $PROD 'GAOS_REDIS_AUTH_FILE: "/run/secrets/traffic-redis-auth"'`; C25 `inblock dashboard './prod.traffic-redis-auth:/run/secrets/traffic-redis-auth:ro'` |
| `the traffic store's password file is untracked, a regular file, mode 600` (in "no secret is committed", `:107-117`, a loop over `deploy/stacks/*.traffic-*` like the others) | C26 untracked; C27 a regular file; C28 mode 600 — each only when the file exists, as the existing loops do |
| `the dashboard's edge hop is the one address Traefik trusts` | C29 `edgeHop = "…"` in `dashboard/traffic.go`, followed by `/32`, equals the single entry `check-stacks.sh:98` finds in `traefik.yml` |
| `nothing in prod has a start-order dependency` | C30 `hasntre $PROD '^[[:space:]]*depends_on:'` (a key, never a comment) |

That is **eleven output lines over thirty conditions**. Failures print `FAIL` with the same message (`check-stacks.sh:22`). The block is register row TC-902. Its negative half is run by hand (D-025): in a scratch checkout, break one condition at a time (C1 … C30, including a comment containing `depends_on`, which must still pass C30), run the script, see that line print `FAIL` (or, for the comment, `ok`), restore with `git checkout -- <file>`; TC-902's prose records the date and the thirty results.

### 4.5 `rebuild-restart.sh` and the deploy agent

- `SERVICES` (`deploy/rebuild-restart.sh:26`) and the usage comment (`:15`) gain `redis`. `dev … redis` fails inside compose with its own message; dev is retired, so this is accepted. TC-019's quoted message becomes `unknown service 'bogus-service' -- valid: traefik landing dashboard docs redis`.
- **The secret.** In the prod pre-flight block beside the credential-file check (`:228-241`), when the tagged compose file mentions `prod.traffic-redis-auth`: if `deploy/stacks/prod.traffic-redis-auth` does not exist, create it with `(umask 077; openssl rand -hex 32 > deploy/stacks/prod.traffic-redis-auth)` and print `ok "created deploy/stacks/prod.traffic-redis-auth (mode 600)"` — the value is never printed; if it exists, leave it unchanged. The link loop (`:243`) gains `deploy/stacks/*.traffic-*`. *Verified:* the existing `.gitignore` rules are per kind (`.gitignore:4-17`: `*.env`, `*.aim-*`, `*.api-*`, `*.bo-*`, `*.bt-*`, `*.pr-*`) and do **not** cover the new name (`git check-ignore` reports nothing), so `.gitignore` gains `deploy/stacks/*.traffic-*` with a comment. The reviewers' deny-list already covers it ("every `deploy/stacks/prod.*` except `*.example`").
- **The health warning.** After the containers listing (`:373-374`), a line: when `dc ps --format '{{.Service}} {{.Status}}'` has no `redis … (healthy)` line, print `info "WARN redis is not healthy: the Traffic page will say Unavailable"`; it is not counted in `vfails` — Redis stays off the critical path.
- The deploy agent's list of valid services (`.claude/agents/gaos-deploy.md:38`) gains `redis`.

These are a new assertion-bearing behaviour of the script: register row TC-903.

## 5. Streams, records and retention

### 5.1 Streams and caps

| Stream key | Holds | Cap |
|---|---|---|
| `gaos:traffic:out:qa-tool` | QA tool calls | 5 000 |
| `gaos:traffic:out:ai-manager` | AI manager calls | 5 000 |
| `gaos:traffic:out:github` | GitHub (review token) calls | 3 000 |
| `gaos:traffic:out:<label>` for `github-write`, `whatsapp`, `brain-town`, `brain-town-reader`, `taskmgr` | each platform | 1 000 each |
| `gaos:traffic:in:direct:auth` | direct requests where a principal was set | 2 000 |
| `gaos:traffic:in:direct:other` | every other direct request: unauthenticated API calls and every non-API direct request | 2 000 |
| `gaos:traffic:in:auth` | API calls through the edge with a principal | 2 000 |
| `gaos:traffic:in:anon` | API calls through the edge with no principal | 1 000 |

A busy platform cannot push a quiet one out; an anonymous flood through the edge evicts only `in:anon`; and known direct clients (a QA backend on the VM calling the tunnel port, `deploy/edge/README.md:198-200`, A8) land in `in:direct:auth` and cannot evict an unknown peer in `in:direct:other`. Each prod instance deploys its own compose and so its own Redis (`deploy/edge/README.md:181-182`, `:210-215`).

**Derivation** — each cap is sized to cover about one review period (a day, §0) of that platform's expected volume:

| Platform | Rate (anchored) | Day's estimate | Cap | Covers |
|---|---|---|---|---|
| `qa-tool` | a running job's status every 15 s (`qajobs.go:193`) for ≤ 40 min: ≤ 160 a job; inbox answers on a 10 s tick when due (`inbox_worker.go:31`); syncs | ~30 jobs a working day ≈ 4 800 (*A4*) | 5 000 | ~1 day |
| `ai-manager` | a turn polls every 2 s for ≤ 660 s (`adapter_aimanager.go:44-45`): ≤ 330 a turn | ~15 long turns ≈ 5 000 (*A4*) | 5 000 | ~1 day; less on a heavier day, which the page shows |
| `github` | each non-final fix PR every 5 min for 7 days (`fixprpoll.go:16-24`): 288 a PR-day, plus flow reads | ~10 open fix PRs ≈ 2 900 | 3 000 | ~1 day |
| `taskmgr` | directory refresh every 15 min (`taskmgr.go:43`) plus lookups | ~100 | 1 000 | > 48 h |
| `github-write`, `whatsapp`, `brain-town`, `brain-town-reader` | per fix, notice, event, stream reconnect | tens | 1 000 each | > 48 h |
| inbound streams | the QA tool's calls and Jenkins posts, seconds to minutes apart (*A4*) | — | 2 000 / 2 000 / 2 000 / 1 000 | hours to 48 h; shown per stream |

**Memory sum:** 5 000 + 5 000 + 3 000 + 5 × 1 000 = 18 000 outbound; 2 000 × 3 + 1 000 = 7 000 inbound; **25 000 records × ≤ 600 bytes ≤ 15 MB**, under `maxmemory 64mb`. The read bound (12 streams, at most 5 000 each) stays under the parser's 50 000-element array bound (§6).

### 5.2 Record fields

`XADD <key> MAXLEN ~ <cap> * <field> <value> …`, server-assigned id; the call's time is `t`.

| Field | Out | In (API) | In (reduced) | Value | Bound |
|---|---|---|---|---|---|
| `t` | ✓ | ✓ | ✓ | call start, Unix ms | — |
| `m` | ✓ | ✓ | ✓ | `GET POST PUT PATCH DELETE HEAD OPTIONS`, else `OTHER` | closed |
| `s` | ✓ | ✓ | ✓ | HTTP status; `0` = no response | integer |
| `d` | ✓ | ✓ | ✓ | ms: out, to the response headers or the error (TaskMgr: to the end of `do`); in, gate entry to exit | integer |
| `qb` | ✓ | ✓ | | request `ContentLength`, `-1` unknown | integer |
| `rb` | ✓ | ✓ | | out: response `ContentLength` (`-1` unknown); in: bytes written | integer |
| `h` | ✓ | | | `req.URL.Host`, lower-cased | 100 bytes |
| `u` | ✓ | | | the path, relative to the client's base, templated (§7.4) | 160 bytes |
| `e` | ✓ | | | error class (§7.5); empty when a response arrived | closed |
| `g` | taskmgr | | | the raw gRPC status read, `-1` when none | integer |
| `x` | taskmgr | | | the client's outcome: `ok`, `notfound`, `invalid`, `auth`, `unavailable` (§7.6) | closed |
| `k` | | ✓ | ✓ | route class | closed |
| `r` | | ✓ | | the route pattern without its method, or `(no route)` | registered patterns |
| `src` | | ✓ | ✓ | the source (§8.3) | 45 bytes |
| `via` | | ✓ | ✓ | `edge` or `direct` | closed |
| `c` | | ✓ | | the principal, empty when none | 64 bytes |
| `pid` | | ✓ | | `{id}` when it matches `^p-[0-9]+$` (`urls.go:90`) | 16 bytes |

The platform and the inbound kind are the key, not fields. No record holds a header, a body, a query string, a fragment, userinfo, a cookie, a token, a password, an error message, or — for a reduced record — any path. A record the reader cannot parse is skipped; there is no version field.

### 5.3 Retention

- **By count:** each `XADD` carries its stream's `MAXLEN ~ <cap>`; `~` may leave up to ~100 records over the cap; the page reads at most the cap.
- **By age:** every 60 s the writer pipelines `XTRIM <key> MINID ~ <now − 48 h>-0` for the twelve keys; the page ignores any record older than 48 h.
- **On restart:** nothing is on disk.

## 6. The Go client: a minimal RESP2 client

**Decision: `dashboard/resp.go`, written here; no dependency** — `dashboard/go.mod:1-7` keeps the module stdlib-only, and the binary that would carry a library holds every GAOS credential.

- **Four commands:** `AUTH gaos <password>` once per connection, then `XADD`, `XTRIM`, `XREVRANGE`. Commands may be pipelined. Nothing else is sent; the healthcheck uses `redis-cli`.
- **RESP2 only**, a dial timeout, a per-operation deadline; any I/O, parse or `AUTH` error closes the connection and the caller redials. The password is never logged or put in an error; an `AUTH` failure is logged as `traffic: the store refused the credential`.
- **Bounded parser:** a line ≤ 1 KiB, a bulk string ≤ 64 KiB, an array ≤ 50 000 elements, nesting ≤ 3; anything else closes the connection; nothing is allocated from a declared length before the bound is checked; an error reply becomes a Go error of at most 80 bytes.
- **Not safe for concurrent use:** the writer owns one connection; the page reader owns another under a mutex.

## 7. Recording outbound calls

### 7.1 The clients and their labels

| Label | Shown as | Recorded at | Base for §7.4 | Covers |
|---|---|---|---|---|
| `qa-tool` | QA tool | transport, `qaclient.go:145-150` | `o.base` | `qaclient.go:73, 273, 358`; `qajobs.go:96, 112, 129, 592`; the inbox worker (`inbox_worker.go:51`) |
| `ai-manager` | AI manager | transport, `aimanager.go:135` (default branch only; an injected `c.HTTP` is used as is, `:132-134`) | `c.Base` | `aimanager.go:170, 182, 246`; `aimanager_questions.go:71`; `aimanager_admin.go:63` |
| `brain-town` | Brain Town | transport, `braintown.go:292-294` | `BRAIN_TOWN_URL` | `braintown.go:566` |
| `brain-town-reader` | Brain Town reader | transport, `braintown_consumer.go:321-328` | `BRAIN_TOWN_CONSUMER_URL` | `braintown_consumer.go:422` |
| `github` | GitHub | transport, `github.go:179-183` | `cfg.base` | the review token's calls |
| `github-write` | GitHub (auto-fix) | the same, via `newGHWriter` | `cfg.base` | the write token's calls |
| `whatsapp` | WhatsApp gateway | transport, `wa.go:180-186` (`waTransport`, `:205`) | `cfg.base` | `wa.go:291, 374` |
| `taskmgr` | TaskMgr | **`taskmgrClient.do`** (`taskmgr.go:326-359`), not the transport (§7.6) | `c.base` | `taskmgr.go:339` |

**Excluded: ArcadeDB** (`arcade.go:74-75`), GAOS's own store; its reachability is on `/api/healthz` (`main.go:706`).

**`github` vs `github-write`:** `ghConfig` (`github.go:108-112`) gains `platform string`; `newGHWriter` (`githubwrite.go:162-168`) sets `cfg.platform = "github-write"` before `newGHClient(cfg)`; empty means `github`.

### 7.2 The wrap and the one handle

`trafficWrap(label, base string, rt http.RoundTripper)` returns `rt` unchanged when recording is off, else a `recordingTransport` around it (`nil` meaning `http.DefaultTransport`, what those clients use now). Seven constructors call it; timeouts, `CheckRedirect` and TLS stay as they are. A label outside the closed set panics at construction (TC-892).

**One handle:** a package-level `atomic.Pointer[trafficRecorder]`, set in `serve()` right after the signal context (`main.go:195-196`) and before `ArcadeFromEnv` (`:198`). The gate, `taskmgrClient.do` and the view read it; `server` gets no field. Tests that turn recording on are not `t.Parallel()` and restore the pointer in `t.Cleanup`. TaskMgr's transport is never wrapped, so `TestTaskMgrConfig`'s type assertion (`taskmgr_test.go:409`) is untouched. No production code type-asserts a wrapped client's transport (the only non-test assertion is `arcade.go:62`). `recordingTransport` delegates `CloseIdleConnections()`.

### 7.3 What is read — and what is not

Exactly: start and elapsed time; `req.Method`; `req.URL.Host`; `req.URL.EscapedPath()` (then §7.4); `req.ContentLength`; `resp.StatusCode` and `resp.ContentLength`; `errClass(err)`. Never `req.Header`, `req.Body`, `req.URL.User`, `req.URL.RawQuery`, `req.URL.Fragment`, `resp.Header`, `resp.Body`, `resp.Trailer` or `err.Error()`. The response and error go back unchanged; the body is not wrapped; a streaming response is one record at its headers. `rec.out` never blocks (§9.1).

### 7.4 The path

1. **Relative to the base.** A non-empty base path that prefixes the request path becomes `{base}`; a base path the request does not start with records the whole path as `{other}`. An operator-configured base path — which GAOS today shows only as its host (`main.go:422`, `wa.go:270`) — never reaches a record.
2. **Free-form tails.** Everything after a `branches` segment becomes `{ref}` (`github.go:207, 433`), and everything after a `contents` segment becomes `{path}` (`github.go:587`, `githubwrite.go:84`, `:323`): branch names and file paths of private repositories are not shown.
3. **Templated.** Each remaining segment is kept only if it starts with a letter, is ≤ 40 bytes of letters, digits, `.`, `_`, `-`, and holds ≤ 3 digits; all digits become `{n}`; anything else `{id}`. At most 12 segments; cut at 160 bytes.

Examples (TC-887): base `https://wa.example/inst-Kq7xTzLm` and `/inst-Kq7xTzLm/send/message` → `{base}/send/message`; `/repos/acme/route-api/pulls/12/files` → `/repos/acme/route-api/pulls/{n}/files`; `/repos/acme/route-api/branches/feature/jane-ticket-12` → `/repos/acme/route-api/branches/{ref}`; `/repos/acme/route-api/contents/internal/billing/rates.go` → `/repos/acme/route-api/contents/{path}`; a 40-hex commit → `{id}`.

**Why a templated path may be shown when Integrations shows only hosts** (`integrations.go:35-38`): the configured part is `{base}`, free-form tails are `{ref}`/`{path}`, and what remains is the API's route shape with identifiers replaced. Owner and repository names stay (the Pull requests page shows them, `templates/backoffice.html:1568`). Credentials travel in headers (`aimanager.go:154`, `taskmgr.go:338`), a WhatsApp number in the body (`wa.go:374`); BR-63 holds.

### 7.5 Error class

Closed set, in order: `timeout`, `canceled`, `dns`, `refused`, `reset`, `tls`, `other`. The error's text is never stored. A refused redirect is recorded as the 3xx the transport returned.

### 7.6 TaskMgr

TaskMgr's outcome is the `grpc-status` trailer, or a trailers-only header, read after the body (`taskmgr.go:343-354`). Every call goes through `taskmgrClient.do` (`:326-359`), so `do` records it: a `defer` at its top sends **one** record on every return path, with `POST`, the host and `{base}`-relative templated path of `c.base+path`, `resp.StatusCode` (or `0`), `errClass` of the transport error, the raw code read (`g`, `-1` when none) and the outcome `x` mapped from the error `do` returns: `ok` (`nil`), `notfound` (`errTMNotFound`, code 5), `invalid` (`errTMInvalid`, code 3), `auth` (`errTMAuth`, codes 7, 8, 16), `unavailable` (`errTMUnavailable`: a transport error, a non-200, an over-limit body, any other code). **Failed = `x` is `auth` or `unavailable`** — the codes the client itself treats as failures (`taskmgr.go:272-278`); `notfound` and `invalid` are answers, not failures. The raw code is always shown (`gRPC 16`).

## 8. Recording inbound calls

### 8.1 Where, and what

At the **top of `server.gate`** (`auth.go:541-590`), before its first return: the gate computes `src` and `via` (§8.3), creates the tag in the request context, wraps `w` (§8.6) and installs the deferred `rec.in` — so **every exit is recorded**: the unmatched early return (`:544-546`), the cross-origin 403 (`:564-566`), `requireBO` (`:571-573`), `signedOutPage` (`:579-581`), the JSON 401 (`:583-584`), and `mux.ServeHTTP` (`:588`). All later code in `gate` uses the wrapped writer and the tagged request; the back-office class still adds its session value (`:586`) to that request.

| Request | Record | Stream |
|---|---|---|
| an API call, `via = direct`, a principal set | full | `in:direct:auth` |
| an API call, `via = direct`, no principal | full | `in:direct:other` |
| any other request, `via = direct` | reduced: `t m s d k src via` | `in:direct:other` |
| an API call, `via = edge`, a principal set | full | `in:auth` |
| an API call, `via = edge`, no principal | full | `in:anon` |
| any other request, `via = edge` | none | — |

Every direct request also updates the direct-source summary (§8.7). The route class `k`: `api` for an API call; `healthz` for `GET /healthz`; `static` for `GET /static/`; `public` for the other `publicPatterns` (`auth.go:516-524`); `unmatched` for an empty pattern outside `/v1/`; else `backoffice`. `pid` is `r2.PathValue("id")` read after `mux.ServeHTTP` returns (*A5*, TC-893).

### 8.2 Unmatched `/v1/` paths

An empty pattern is answered 404, or 405 for a wrong method on a real path (`auth.go:534-536`). Both are recorded as `r = (no route)` with their status, never the path.

### 8.3 Source and via

`src` is `jenkinsSource(r)` unchanged (`staging.go:276-297`).

`via` comes from **the hop that delivered the request to Traefik**: the **last entry of the comma-joined `r.Header.Values("X-Forwarded-For")`** (as `jenkinsSource` joins them, `staging.go:280-281`), trimmed and parsed as an address. `via = edge` when it equals `edgeHop`; otherwise `direct`. **A request with no `X-Forwarded-For` is `direct`**: it did not pass through Traefik, so it did not come through the edge. No address-class fallback and no tunnel setting are used.

- `edgeHop = "10.10.0.1"` is a constant in `traffic.go`, asserted equal to Traefik's one trusted entry (§4.4 C29).
- *A6 (confirmed in T2, before any wiring):* Traefik v3.3 with `forwardedHeaders.trustedIPs` keeps a trusted peer's chain and appends the delivering peer, replaces an untrusted peer's chain with that peer, and sets `X-Real-IP` to the client (trusted) or the peer (untrusted). T2 runs `traefik:v3.3` with `deploy/stacks/traefik.yml` and a one-route echo backend, sends a request from a trusted and from an untrusted address, and records **both** `X-Forwarded-For` and `X-Real-IP` as the backend receives them in the T2 commit message. If either differs, T2 stops and the design is revised.
- *Stated limit:* the edge box calling the VM's port directly (the deploy's health probe, `deploy/rebuild-restart.sh:399`) has `10.10.0.1` as its delivering hop and is classed `edge` — it is the edge.

### 8.4 The principal

Set only after authentication: `apiGate` (`api_v1.go:316-340`) right after `authorize` (`:329`) when `got != scopeNone` — `admin`, or `cl.Name` (`api_v1.go:54-55`; the QA tools' principal is `qa-tools`, `answer_source.go:27`); a 403 (`:335-337`) keeps the refused principal. `jenkinsGate` (`staging.go:331-406`) after `a.ok(prodKey)` (`:404`) — `basic:` + `a.route` (`staging.go:71-72`). A clients-file name is cut to 64 bytes. The recorder never reads `Authorization`, a Basic user name or a password.

### 8.5 Floods

No per-source limiter. An anonymous flood through the edge evicts only `in:anon`; a record bound for `in:anon` is enqueued only while the queue is below ¾ full, else dropped and counted as `droppedAnon`, so it cannot crowd out other records. An authenticated direct burst evicts only `in:direct:auth` (TC-896). An unauthenticated direct flood evicts older `in:direct:other` records, but every source in it stays in the summary (§8.7) — the flood is itself the finding.

### 8.6 The response writer

`trafficWriter` records the first `WriteHeader` status (200 when `Write` comes first, and **200 when the handler returns having called neither**), counts bytes, implements `Unwrap()` (so `http.NewResponseController` reaches the real writer, as `apiDecodeLimit` needs, `api_v1.go:398-401`) and `Flush()` when the real writer can. If the handler panics, the deferred `rec.in` records `s = 500` and the panic continues to net/http unchanged.

**Residual:** `http.MaxBytesReader(w, …)` (`api_v1.go:402`) signals close-after-413 through an unexported method on net/http's own writer, which a wrapper cannot forward; behind `trafficWriter` the 413 and its error are unchanged, but the connection may stay open. TC-894 asserts the 413.

### 8.7 The direct-source summary

In memory, since the dashboard started: a map of at most **512** direct sources, each with first seen, last seen, count, last route class and whether a principal was ever set. **Eviction never touches it**: Redis trimming, caps and restarts of Redis do not change it. When it is full, a new source is not added; it increments `untracked` (shown). It is lost only when the dashboard restarts — which the page states ("since <boot time>") and which makes the cross-check inconclusive (E3). It is the evidence the daily review rests on.

## 9. Never blocking, never failing

### 9.1 The hot path

`rec.out` and `rec.in` build one record and `select { case q <- r: default: dropped++ }` on a channel of 4096 (anonymous records: §8.5); the summary update is one short mutex-guarded map write. No I/O. With recording off they are not reached.

### 9.2 The writer

One goroutine on the signal context: collect ≤ 128 records or 250 ms; dial (500 ms) and `AUTH` if needed; pipeline the `XADD`s with a 2 s deadline. A dial or `AUTH` failure drops the batch, then backs off 1 s doubling to 30 s while draining and counting. `OOM` or `NOPERM` drops that record; an I/O error closes the connection and drops the unacknowledged records. **The writer records the time of its first successful write after every (re)connect** (`firstWriteAt`); the page uses it (§10.2). Every 60 s, the twelve `XTRIM`s. On shutdown, queued records are discarded. A test hook (an unexported option that leaves the writer unstarted) lets TC-890 and TC-895 make the queue the only consumer.

### 9.3 Counters and logs

In memory: `written`, `dropped`, `droppedAnon`, `untracked`, `firstWriteAt`, the last write error's time. One log line per transition (`traffic: the store is not answering (<class>); records are dropped until it answers` / `traffic: the store answers again; <n> records were dropped meanwhile`); no line carries a recorded value.

### 9.4 Redis down

Nothing outside this feature changes; `/api/healthz` does not probe it; the dashboard starts without it (TC-890).

## 10. The Traffic page

### 10.1 Place, address and access

- **Menu:** **Traffic** after **Integrations** (`templates/backoffice.html:96`); `pageViews` (`urls.go:31-35`) gains `{"traffic", "Traffic"}` → `GET /traffic` (`main.go:581-583`) and `GET /ui/bo/traffic` (`main.go:605`).
- **Poll:** `GET /ui/bo/traffic/live` beside `GET /ui/bo/prs/live` (`main.go:608`), shaped like `handlePRsLive` (`prview.go:489-499`).
- **Who may view:** every signed-in back-office account (`auth.go:568-585`; BR-55). **Masking:** a **public** source address is shown in full only to auto-fix admins (`prViewerIsAdmin`, `prview.go:502-505`) and as its /24 (IPv4) or /48 (IPv6) to everyone else; loopback, private, link-local and `100.64.0.0/10` sources — GAOS's own tunnel peers and VM — are shown in full to everyone (the test `jenkinsSource` applies, `staging.go:284`). **Masking happens in the page model before rendering**, so no `title`, `href`, `hx-get`, poll URL or filter value carries a masked-away address, and every count of sources (tiles included) counts the masked values. Nothing on the page can be edited.
- **Caching:** `noStoreView` (`integrations.go:27-31`) adds `"traffic"`; the page route already sends `no-store` (`urls.go:395`).
- `buildBO` (`handlers_bo.go:197`) gains `case "traffic"`; `BOView` (`view.go:247-250`) gains `Traffic *TrafficPage`; the view switch (`templates/backoffice.html:129-131`) gains `bo-traffic`.

### 10.2 What it shows

Header: eyebrow `monitoring`, title **Traffic**, sub "Recent calls GAOS made to other platforms, and calls made to GAOS — at most 48 hours, each kind keeping only its newest records — and every direct caller since <boot time>."

1. **Status line** (`af-status`, `templates/backoffice.html:1529-1533`).
2. **Notes** (`af-note`), only when true: drops since boot; anonymous drops; **"The traffic store may have been emptied at <time>: records before it are gone."** when `firstWriteAt` of the current connection is later than the process start plus one minute; "<n> direct sources are not tracked: the summary is full."
3. **Tiles** (`af-tiles`, `:1336-1342`; existing tones only, `static/app.css:1790-1793`): *Outbound calls*; *Outbound failed* (tone `block` when > 0); *Inbound API calls*; *Unknown direct sources* — summary entries with no principal ever set, counted on masked values (tone `ask` when > 0).
4. **Direct sources since <boot time>** — the summary (§8.7): source, first seen, last seen, count, last class, principal yes/no; newest first; all rows (≤ 512).
5. **Outbound by platform** — the eight platforms in order: Platform; Host; Calls; 4xx; Failed (`s ≥ 500`, `s = 0`, or TaskMgr `x` ∈ {`auth`, `unavailable`}); p50, p95; Last call; **Kept since** — always the oldest kept record's time, marked ***cap*** when the stream holds at least its cap. A platform with nothing kept: one muted row "No calls kept".
6. **Inbound by source and route** — rows per (masked source, route or class), newest first, ≤ 50, then "<n> more — filter the calls below by source": Source (link to the filter); Via (`edge` or chip **direct**); Client (≤ 2 principals, "unauthenticated", or "—" for a non-API class); Route (`r` or `[class]`); Calls; Refused (`4xx`); Failed (`≥ 500`); p95; Last call.
7. **Kept records** — one line per inbound stream: "Direct, authenticated: 312 kept since <time> · Direct, other: 2 000 kept since <time> (*cap*) · Authenticated: … · Anonymous: …". The shortest `in:direct:other` kept-since is the review deadline (§0).
8. **Recent calls** — newest first, filtered (§10.3), `tablePageSize` (10, `pager.go:15`; D-097) a page with the shared `pager` (`templates/backoffice.html:596-604`). Columns: When; direction (`→`/`←` with sr-only text); Who; Call; Status (chip `st-ok` < 400, `st-warn` 4xx, `st-bad` ≥ 500, `static/app.css:2000-2003`; the error class for `s = 0`; `gRPC n` for TaskMgr); Time; Bytes (`af-md`).

### 10.3 Filters, paging and the read

| Parameter | Values | Default | Rule |
|---|---|---|---|
| `dir` | `all`, `out`, `in` | `all` | — |
| `platform` | one of the eight labels | none | a set platform forces `dir=out` |
| `src` | an address or prefix `netip` parses, compared canonically **after the viewer's masking** | none | a set source forces `dir=in` |
| `status` | `all`, `ok` (`0 < s < 400`, not a TaskMgr failure), `4xx`, `fail` | `all` | — |
| `page` | `pageParam` (`pager.go:40`) | 1 | — |

An invalid value is ignored, never echoed. If both `platform` and `src` are set, `src` is dropped and `dir=out`. The direction tabs (`af-seg`, `templates/backoffice.html:1541-1543`) show the forced direction; platform and status are `select`s in an `af-form` with `hx-trigger="change"` and a `<noscript>` Apply (`:1544-1553`). Every link carries `href` and `hx-get` (`urls_test.go:441-447`).

**The read:** one pipelined `XREVRANGE <key> + - COUNT <cap>` per stream (twelve) on the reader connection; strict parsing; records older than 48 h dropped; cached 15 s for every viewer, a failed read cached 15 s too. Masking, summaries and the filtered list are computed per request (≤ 25 000 records).

### 10.4 States

| State | When | Text |
|---|---|---|
| Off | a setting makes recording off | "Traffic recording is off: <reason>." then "This deployment has no traffic store configured. The docs say how it is set up." (link). The direct-source summary is still shown (it does not need Redis). |
| Unavailable | the read failed | `af-note af-note--fail`, `role="alert"`: "The traffic store is not answering. GAOS keeps working; calls made meanwhile are not recorded." plus the drop counter and the direct-source summary. |
| Empty | read succeeded, no record | the platform table muted and "No call recorded in the last 48 hours. Outbound calls appear when GAOS calls a platform; inbound ones when something calls GAOS." |
| Filtered empty | records, none match | "No call matches these filters." + **Clear them** |
| Normal | — | §10.2 |

### 10.5 Live refresh

```html
<div id="trafficLive" class="af-main"{{if .OOB}} hx-swap-oob="true"{{end}} hx-get="{{.PollURL}}" hx-trigger="every 30s [!document.hidden &amp;&amp; !document.getElementById('trafficLive').matches(':focus-within')]" hx-target="#sink" hx-swap="innerHTML" hx-indicator="#trafficPulse">
```

As the Pull requests list (`templates/backoffice.html:1528`). A record reaches the page at most 250 ms plus 15 s after its call.

## 11. UI and UX

The operator pointed at `https://aiforui.dev/skills`, the index of Emil Kowalski's skills (`github.com/emilkowalski/skills`), vendored here as `emil-design-eng`, `review-animations` and `break-ui`; `.claude/rules/frontend.md` requires loading them; the dashboard's mechanics are `.claude/agents/gaos-frontend.md`; the published counterpart is `pustaka/docs/guide/authoring.html`. T3 is built by the `gaos-frontend` agent. Rules:

1. **No new motion.** Only the existing refresh pulse moves; the reduced-motion kill-switch applies.
2. **Numbers right-aligned and tabular** by widening `.qa-tbl .r` (`static/app.css:1543`) to `.qa-tbl .r, .af-table .r`; counts via `num` (`render.go:138`); times via `agoAt` (`integrations.go:62`) and `relSpan` (`handlers_bo.go:692`), absolute in `title`.
3. **Long values cut on the server** with `clipEllipsis` (`handlers_bo.go:431`) to 48 runes, full (masked) value in `title`; addresses, hosts and paths mono. No new CSS class.
4. **Colour never alone:** chips show their code or word; direction has an arrow and sr-only text. Existing chips (`static/app.css:2000-2003`) and tile tones `block` and `ask` (`:1790-1793`) only.
5. **Empty states** say what is true and what happens next (§10.4).
6. **Hover:** `af-table`'s existing row hover only.
7. **Narrow screens:** Bytes and Host are `af-md` (`static/app.css:1886`); tables in `tm-scroll af-scroll`.
8. **Keyboard:** every filter and link reachable by Tab.
9. **No chart** (a new vendored library needs the operator).
10. **`break-ui` pass:** a 100-byte host, a 160-byte path, a 64-byte principal, an IPv6 /64 and its /48 mask, a private source shown whole, 0 and 25 000 records, 512 summary rows, `gRPC 16`, `qb = rb = -1`, a stream at its cap, every state. TC-901 renders the escaping and masking cases.

## 12. Monitoring data is not an event

BR-05 closes the event catalog (`brd.html:1230`); BR-02, BR-01 and BR-04 (`:1227`, `:1226`, `:1229`). A traffic record records no change (what a call changed is already an event of the code that handled it); nothing reads it to decide anything; it may be dropped, evicted, trimmed or erased; it lives outside the graph — no schema statement, no event type (`dashboard/events.go` keeps 66), and `check-stacks.sh`'s landing figures (`:327-348`) do not move. BR-66 writes this down.

## 13. Security

### 13.1 Exposure

| Data | Where | Who sees it |
|---|---|---|
| Outbound: platform, host, `{base}`-relative templated path, method, status, timings, sizes, error class, gRPC code and outcome | Redis; the page | signed-in accounts |
| Inbound: route or class, method, status, timings, sizes, via, principal, product id | Redis; the page | signed-in accounts |
| A public source address | Redis; the summary; the page | full: auto-fix admins; /24 or /48: others |
| A private, CGNAT, loopback or link-local source | Redis; the summary; the page | signed-in accounts, in full |
| Counters, the summary | dashboard memory; the page | signed-in accounts |

No new public surface: back-office routes behind the session gate; no JSON export; nothing on `/healthz`; Redis has no port.

### 13.2 Personal data

Public source addresses: at most 48 h and the stream's cap in Redis, since boot (≤ 512) in the summary; never on disk, in a log, the event log, Brain Town, a packet, a notice or the graph; full only to auto-fix admins; `no-store`. IPv6 is a /64 when recorded.

### 13.3 Secrets never recorded

By construction (§7.3, §7.4, §7.6, §8.4) and by test (TC-888, TC-893). The Redis password is read from its file by the dashboard and by the store's start script only; it is never logged, never in a record, never in an argument or in `docker inspect` (§4.1).

### 13.4 Redis itself

- **Reachability:** only the dashboard, over `gaos-prod-traffic` (`internal: true`, pinned subnet, Redis at `.2` outside the dynamic range `.4-.7`); no port; no route out; only an IP literal inside the subnet is accepted (§4.3), so no name is resolved and a mistyped value cannot point the writer outside it.
- **Authentication:** `default` off; user `gaos` with a 256-bit password, admitted to `PING`, `XADD`, `XTRIM`, `XREVRANGE` on `gaos:traffic:*` only (§4.1). T4 records, with the password: `FLUSHALL` → `NOPERM`, `CONFIG GET maxmemory` → `NOPERM`, `XADD gaos:other * a b` → `NOPERM`; and without it: `PING` → `NOAUTH`.
- *A7, recorded as information (T4):* from the VM host, `nc -z -w 3 172.30.14.2 6379`, and from a container on `gaos-prod-int`. Whatever the result, a reader needs the password, which only the deploy uid can read on the host.
- **Read residual:** anyone holding the password — the deploy uid on the host, or code running in the dashboard or Redis container — can read the full trace and trim it. Both already hold every GAOS credential.
- **Deny-lists:** the host of `GAOS_REDIS_ADDR` joins the hosts the QA client and the Brain Town client refuse (`qaclient.go:174`, `braintown.go:87`) (TC-897).
- **Forged records:** need the password; even then they are parsed strictly, bounded and escaped (TC-901).

### 13.5 Availability and evidence

Anonymous floods evict only `in:anon` and stop at ¾ of the queue; authenticated direct bursts evict only `in:direct:auth`; every direct source stays in the summary (TC-895, TC-896). Memory, queue, reads and cache are bounded. *Residual:* an unauthenticated direct flood evicts older `in:direct:other` records, not their sources.

### 13.6 Supply chain

No Go dependency. One image pinned by version and digest, asserted by `check-stacks.sh`.

### 13.7 What this does not do

It blocks nobody. **Detection is not prevention:** until the recommended firewall is applied, a direct peer keeps reach to every route on the tunnel port. The page is best-effort: it drops under load, evicts per stream, keeps 48 h, and does not see what Traefik serves without the dashboard (E1); the summary is lost when the dashboard restarts. An empty direct list is not proof that no direct caller exists. D-107 names this residual and the edge README says it.

### 13.8 Published docs

`integrations/traffic.html` describes what is recorded, `edge`/`direct`, masking, the summary, the exclusions, the review cadence and the states. It does **not** describe Redis' authentication, address or network names (those are in `deploy/STACKS.md`), and every example address is from `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`.

## 14. Registers

- **D-107** (new, 2026-11-27): "GAOS records its outbound calls to external platforms and the calls made to it, and shows them on a back-office Traffic page; reviewing that page at least daily is the required step, and the host firewall on the tunnel port becomes recommended hardening. Context: D-106 left the firewall unapplied; answering that item of the v0.68.0 deploy report on 2026-11-27, the operator wrote that a monitoring page of outbound traffic to the QA tool and the other platforms 'will be enough' (design `docs/designs/traffic-monitor.md` §0, verbatim). Decision: capped Redis streams — one per platform out (sized to about a day each), and inbound split into direct with a principal, other direct, authenticated and anonymous — in a Redis only the dashboard reaches, at a fixed address, with the default user off and a password-protected user admitted to three data commands and `PING` on the traffic keys, with no persistence, keeping at most 48 hours; an in-memory summary of every direct source since the dashboard started, which eviction never touches; a stdlib RESP client; recording on the eight external clients (ArcadeDB excluded), TaskMgr with its gRPC outcome; every API call and every request that reached GAOS directly recorded, with `via` taken from the hop that delivered it to Traefik; never a header, body, query, token, credential, configured base path, branch name or file path; recording never blocks or fails a call; any signed-in account may view, public addresses in full for auto-fix admins only. Review cadence: at least daily, within the shortest kept span of the unknown-direct stream. **Residuals:** detection, not prevention — a direct peer keeps reach to every route until the firewall is applied; the trace is lossy (drops counted, evictions shown), keeps 48 h, does not see requests served without the dashboard, and the summary is lost on a dashboard restart, so an empty direct list is not proof; no formal record of each review is kept; anyone holding the store's password can read and trim the trace; public source addresses are kept for up to 48 hours."
- **D-106 amended** (one sentence appended, `decision-log.html:2167-2172`): "Since D-107 the host firewall on the tunnel port (edge README §5) is recommended hardening; the required step is reviewing the Traffic page at least daily." Its evidence cell gains D-107.
- **BR-66** (new, in **§9.5 "Commercial, execution and data handling"**, after BR-37, `brd.html:1276-1291`): "GAOS keeps a short trace of the calls it makes to external platforms and of the calls made to it: every machine-API call, and every request that reached it without passing through the edge. A record holds the time, direction, platform or route, method, the host and a path relative to the platform's configured base with identifiers, branch names and file paths replaced, status, duration, sizes and an error class, and for an inbound call the source address, whether it came through the edge, the authenticated client's name and the product id — and never a header, a body, a query string, a token, a credential or a configured base path. The trace lives only in the deployment's own password-protected Redis, in streams capped per platform and per inbound kind, none older than 48 hours, never persisted; a summary of every direct source since the dashboard started is kept in memory. It is not an event and nothing in GAOS reads it to decide anything. Recording never delays or fails a call; a call that cannot be recorded is counted. Any signed-in back-office account may view the trace, with public source addresses in full for auto-fix admins only; nobody may change it. *(D-107)*"
- **Unchanged:** BR-01, BR-02, BR-04, BR-05; BR-55; BR-52; BR-63. No open question changes.
- **QA test plan:** **TC-884 … TC-903** (20 rows: 18 Go tests, `check-stacks.sh`, `rebuild-restart.sh`), section `#go-traffic`. Existing rows that change:
  - **TC-019** — quoted message `unknown service 'bogus-service' -- valid: traefik landing dashboard docs redis` (`qa-test-plan.html:1080-1081`).
  - **TC-725** — the row (`:816`) becomes "Each of the eleven page routes returns the whole document for its view, with its title and no-store, and a product section's pane carries the product and the tab; there are twelve page patterns."; its quoted message becomes `there are %d page patterns, want twelve: ten views, a product section and the redirect`; the section intro (`:4558`) "the eleven page routes" → "the twelve page routes".
  - **TC-431** — its table gains two entries; its prose (`:3749-3758`) names no route or count: unchanged.
- **Counts:** cases 883 → **903** ("nine hundred and three"); Go tests 745 → **763** ("seven hundred and sixty-three"), `dashboard/` 720 → **738**; "Sixty-five rules" → "**Sixty-six rules**" (`brd.html:1218`) and "64 business rules" → "**66 business rules**" (`pustaka/docs/assets/toc/product.js:36`); "One hundred and five entries" → "**One hundred and six entries**" (`decision-log.html:18`) and "One hundred and five decisions" → "**One hundred and six decisions**" (`toc/product.js:129`).
- **Release v0.69.0**, minor: changelog, `product.semver` (`toc.js:22`), `README.md:12`.
- **Not added:** an event type, task status, notice kind, label, schema statement, packet kind, docs CSS class or open question.

## 15. Tests

One `func Test…` per register row (`.claude/rules/qa-test-plan.md:129-130`). The fake Redis is a `net.Listen("tcp", "127.0.0.1:0")` server in `resp_test.go` that parses RESP2, records commands and answers from a script.

| TC | Test (file) | Kind | Asserts |
|---|---|---|---|
| TC-884 | `TestRESPRoundTripsTheTrafficCommands` (`resp_test.go`) | Positive | `AUTH`, `XADD … MAXLEN ~ n *`, `XTRIM … MINID ~`, `XREVRANGE … COUNT` encode byte-exact; every reply type parses; a pipeline of 128 returns 128 replies in order |
| TC-885 | `TestRESPRefusesMalformedOrOversizedReplies` (`resp_test.go`) | Negative | each bound of §6 exceeded, a negative or non-numeric length, a truncated reply, an unknown type byte, a refused `AUTH`: an error that closes the connection, nothing allocated from the declared length, no password in the error |
| TC-886 | `TestTrafficSettings` (`traffic_test.go`) | Positive, negative | `172.30.14.2:6379` with a mode-600 file → on; empty, `redis:6379`, `192.0.2.4`, `203.0.113.5:6379`, `10.0.0.5:6379` (outside the subnet), `172.30.14.2:x` → off with the reason; a missing, empty or mode-644 password file → off; the caps and window are the §5 constants |
| TC-887 | `TestOutboundPathIsTemplated` (`traffic_test.go`) | Positive, negative | the §7.4 examples; `{base}`, `{other}`, `{ref}` (a multi-segment branch), `{path}` (a nested file, and `contents/.github/workflows`); 13 segments; 300 bytes; `%2F`; an e-mail; a phone number; an empty path; never a query or fragment |
| TC-888 | `TestRecordingTransportKeepsNoSecret` (`traffic_test.go`) | Negative | base `https://h/inst-Kq7xTzLm`; `Authorization: Bearer <s1>`, userinfo `<u>:<s2>`, `?token=<s3>`, `#<s4>`, body `<s5>`, response header and body `<s6>`: no stored field contains any of them or `inst-Kq7xTzLm`; the fields are exactly the outbound set; the caller's response is unchanged |
| TC-889 | `TestRecordingTransportClassifiesErrors` (`traffic_test.go`) | Positive, negative | timeout, canceled, refused, unknown-CA TLS, reset → their classes with `s = 0` and no error text; through `taskmgrClient.do` against a fake gRPC server: trailer `grpc-status: 16` → one record `s = 200, g = 16, x = auth`, Failed; trailers-only `5` → `g = 5, x = notfound`, not Failed; `3` → `invalid`, not Failed; an over-limit body → `g = -1, x = unavailable`, Failed; a transport error → `s = 0, x = unavailable`; exactly one record per call |
| TC-890 | `TestTrafficRecordingNeverBlocks` (`traffic_test.go`) | Negative | with the writer unstarted (the test hook), `4096 + 1000` records enqueue without blocking (the loop ends before a 5 s guard) and `dropped` rises by exactly 1000; with a started writer and a store that refuses connections, 300 wrapped calls to an `httptest` server all succeed |
| TC-891 | `TestTrafficWriterTrimsByLengthAndAge` (`traffic_test.go`) | Positive | `AUTH` first on each connection; each `XADD` to its stream with that stream's cap; a trim tick sends twelve `XTRIM … MINID ~ <now − 48 h>-0`; `OOM` or `NOPERM` counts one drop; a lost connection is redialled with backoff, `firstWriteAt` moves to the first write after it, and the two transition lines are logged once each |
| TC-892 | `TestEveryExternalClientIsRecorded` (`traffic_test.go`) | Positive | with recording on, `newQaOutbound`, `AIMClient.client` (default branch; an injected `HTTP` returned as is), `newBrainTown`, `newBTConsumer`, `newGHClient`, `newGHWriter` (from a bare `ghConfig`), `newWANotifier` carry a recording transport with the §7.1 label and base; `newTaskMgrClient`'s transport is the plain `*http.Transport`; the Arcade clients are not wrapped; with recording off each has today's transport |
| TC-893 | `TestInboundRecordsRouteSourceAndClient` (`traffic_test.go`) | Positive, negative | through `routes()`: a QA-token call with `X-Forwarded-For: 203.0.113.9, 10.10.0.1` → `in:auth`, pattern not path, `c = qa-tools`, `pid`, `via = edge`; the same headers split over two `X-Forwarded-For` lines → the same; `X-Forwarded-For: 10.10.0.9` with the token → `in:direct:auth`; no `X-Forwarded-For` → direct; a wrong token through the edge → `in:anon`, 401; a Basic unit-results call → `basic:unit-results`; `/v1/nope?x=<secret>` → `(no route)` 404 and `DELETE /v1/products` → `(no route)` 405, no path; a direct signed-out `GET /inbox` → reduced `in:direct:other` `k = backoffice`, `s = 401`; a direct signed-out HX request → the `requireBO` status; a direct cross-origin `POST` → `s = 403`; a direct `GET /healthz` → `k = healthz`; each direct request in the summary; the same back-office requests through the edge → nothing; no field holds the token, the Basic password or the query |
| TC-894 | `TestInboundRecorderKeepsTheResponseController` (`traffic_test.go`) | Positive | `apiDecodeLimit`'s `SetReadDeadline` succeeds through the wrapper; an over-limit body answers 413; `Flush` reaches the recorder; a handler that writes nothing records 200; a panic records 500 and reaches net/http |
| TC-895 | `TestAnAnonymousFloodEvictsNoDirectOrAuthenticatedRecord` (`traffic_test.go`) | Negative | after one direct and one authenticated record, 10 000 anonymous API calls from 5 000 sources: `in:direct:*` and `in:auth` keep their records, `in:anon` holds its cap; with the writer unstarted, anonymous records stop at ¾ of the queue and an outbound record still enqueues |
| TC-896 | `TestAnAuthenticatedDirectBurstEvictsNoOtherDirectRecord` (`traffic_test.go`) | Negative | one unauthenticated direct record, then 10 000 authenticated direct API calls: `in:direct:other` keeps it, `in:direct:auth` holds its cap; the summary holds both sources, the tile counts one unknown source |
| TC-897 | `TestOutboundURLsRefuseTheTrafficStore` (`traffic_test.go`) | Negative | with `GAOS_REDIS_ADDR=172.30.14.2:6379`, a QA URL and a Brain Town URL on that host are refused with the existing reason |
| TC-898 | `TestTrafficSummaries` (`traffic_view_test.go`) | Positive | per-platform calls, 4xx, failed (TaskMgr by outcome), nearest-rank p50/p95 over answered calls, last call; **kept since** is the oldest kept record's time in every case, marked *cap* at and above the cap and not below it; the "store may have been emptied" note when `firstWriteAt` is later than start + 1 min and not otherwise; the eight platforms in order; inbound pairs newest first, ≤ 50; records older than 48 h not counted; the summary table with its "since" and the `untracked` note at 513 sources; the tiles |
| TC-899 | `TestTrafficFiltersAndPages` (`traffic_view_test.go`) | Positive, negative | each filter as §10.3; `platform` forces `out`, `src` forces `in`, both set drops `src`; tabs show the forced direction; invalid values ignored, not echoed; 10 a page; the poll URL keeps the filters |
| TC-900 | `TestTrafficPageStates` (`traffic_view_test.go`) | Positive, negative | Off, Unavailable, Empty, Filtered empty, Normal render their texts, Off and Unavailable with the summary; `no-store`; signed out both routes are refused (401) and touch no store; a failed read is not retried within 15 s |
| TC-901 | `TestTrafficPageEscapesAndMasksAddresses` (`traffic_view_test.go`) | Negative | host, path, route, principal and source holding `<script>`, `"><img onerror=…>` and a 2 KiB string render escaped and clipped; for a non-admin the public sources `203.0.113.9` and `2001:db8:1:2::/64` appear as `203.0.113.0/24` and `2001:db8:1::/48` and **the full address appears nowhere in the HTML** (text, `title`, `href`, `hx-get`, the poll URL); `10.10.0.9` and `100.64.1.2` appear in full to a non-admin; two public sources in one /24 count as one unknown direct source for a non-admin and two for an admin; an admin sees full addresses |
| TC-902 | `check-stacks.sh` | Positive, negative | the eleven output lines over the thirty conditions of §4.4; each condition broken by hand prints its line's `FAIL`; a comment containing `depends_on` still passes |
| TC-903 | `rebuild-restart.sh` | Positive, negative | a prod deploy with no `deploy/stacks/prod.traffic-redis-auth` creates it mode 600 with 64 hex characters and prints `ok   created deploy/stacks/prod.traffic-redis-auth (mode 600)` without the value; with one present, it is unchanged; the file is linked into the tag worktree; with Redis not healthy the run prints `WARN redis is not healthy: the Traffic page will say Unavailable` and still ends `verification passed` |

Existing tests whose assertions move: `TestADeepURLRendersThePage` (`urls_test.go:233-234`) and `TestEveryRouteIsClassified` (`auth_test.go:259-292`). `TestEveryHtmxLinkHasAPageHref` (`urls_test.go:441-447`) needs no edit.

## 16. Phases

Each phase ends with `(cd dashboard && go vet ./... && go test ./... && go test -race ./...)`, `(cd pustaka && go run . check ./docs && go test ./...)`, `./deploy/check-stacks.sh` and `./deploy/check-workspace.sh` green, and writes its rows and every count in the same commit (`qa-test-plan.html`, `toc/product.js`, `check-workspace.sh`, `README.md`, `.claude/rules/qa-test-plan.md`).

| Phase | Builds | TC |
|---|---|---|
| T1 Client and recorder | `resp.go`; `traffic.go` without wiring: settings, streams, queue, writer (`AUTH`, trims, `firstWriteAt`, test hook), transport, `{base}`/`{ref}`/`{path}` and template, error class, inbound writer and classes, the summary | TC-884 … TC-891 |
| T2 Wiring | A6 first; the pointer in `serve()`; seven wraps; `newGHWriter`'s platform; TaskMgr's record in `do`; the gate at its top; the principals; the deny-lists | TC-892 … TC-897 |
| T3 Page (`gaos-frontend`) | `traffic_view.go`; `pageViews`, `noStoreView`, `buildBO`, `BOView`, the poll route; templates and nav; the widened selector; `readOnlyGET`; the `urls_test.go` count; TC-725 text | TC-898 … TC-901 |
| T4 Deploy, docs, release | A1 and A3 first; `traffic-redis.sh`; the compose service, network, settings, mounts and comments; `.gitignore`; `check-stacks.sh`; `rebuild-restart.sh` and the deploy agent's list; `STACKS.md`; `infra/environments.html`, `toc/infra.js`; the edge README; `integrations/traffic.html`, `toc/integrations.js`; D-107, D-106, BR-66 and counts; changelog, `semver`, `README.md:12` | TC-902, TC-903; TC-019 text |

**Deploy** (after merge): tag `v0.69.0`; `./deploy/rebuild-restart.sh prod deploy --tag v0.69.0` on each prod instance with its usual environment line (the password file is created on first run). The deploy note records per instance: the image digest; `redis … (healthy)` and no port; the `NOPERM` and `NOAUTH` results of §13.4; A7; A6's headers; **A8: which known clients (the QA tool, Jenkins) call directly**, from the page's summary and the Traefik log after a QA sync and a Jenkins post; a QA sync appears under QA tool; with Redis stopped a QA sync and an API call still succeed and the page says Unavailable; Redis started again. After at least 24 hours, the firewall cross-check of §0 with each peer and its explanation, or "inconclusive" and why.

## 17. Change list

New files (8):

1. `dashboard/resp.go` — §6.
2. `dashboard/resp_test.go` — TC-884, TC-885, the fake server.
3. `dashboard/traffic.go` — §5, §7, §8, §9.
4. `dashboard/traffic_test.go` — TC-886 … TC-897.
5. `dashboard/traffic_view.go` — §10.
6. `dashboard/traffic_view_test.go` — TC-898 … TC-901.
7. `pustaka/docs/integrations/traffic.html` — §13.8; that a Redis or whole-stack restart empties the streams and a dashboard restart empties the summary; the daily review; built from `pustaka/ai/AUTHORING_SPEC.md`'s existing components.
8. `deploy/stacks/traffic-redis.sh` — §4.1.

Changed files (items 9–46; item 34 is two files, so 39 files):

9. `dashboard/main.go` — the pointer and writer after `:195-196`, before `:198`; `GET /ui/bo/traffic/live` beside `:608`.
10. `dashboard/auth.go` — the recorder at the top of `gate` (`:541-590`).
11. `dashboard/api_v1.go` — the principal in `apiGate` (`:329-339`).
12. `dashboard/staging.go` — the principal in `jenkinsGate` (`:404`).
13. `dashboard/qaclient.go` — wrap at `:145`; deny-list at `:174`.
14. `dashboard/aimanager.go` — wrap the default branch (`:135`).
15. `dashboard/braintown.go` — wrap at `:292`; deny-list at `:87`.
16. `dashboard/braintown_consumer.go` — wrap at `:323`.
17. `dashboard/github.go` — `ghConfig.platform` (`:108-112`); wrap at `:180`.
18. `dashboard/githubwrite.go` — `newGHWriter` sets the platform (`:162-168`).
19. `dashboard/wa.go` — wrap at `:182`.
20. `dashboard/taskmgr.go` — the deferred record in `do` (`:326-359`); the transport (`:233`) unchanged.
21. `dashboard/urls.go` — `pageViews` (`:31-35`).
22. `dashboard/integrations.go` — `noStoreView` (`:28`).
23. `dashboard/handlers_bo.go` — `buildBO` (`:254-265`).
24. `dashboard/view.go` — `BOView.Traffic` (`:247-250`).
25. `dashboard/templates/backoffice.html` — nav (`:96`), view switch (`:129`), `bo-traffic`, `traffic-live`.
26. `dashboard/static/app.css` — the selector at `:1543`.
27. `dashboard/auth_test.go` — `readOnlyGET` (`:259-292`).
28. `dashboard/urls_test.go` — the count (`:233-234`).
29. `deploy/stacks/compose.prod.yaml` — §4.1–§4.3 and the header comments (`:9-13`, `:25`).
30. `deploy/check-stacks.sh` — §4.4.
31. `deploy/rebuild-restart.sh` — `:15`, `:26`, the secret (`:228-243`), the health warning (`:373-374`).
32. `deploy/STACKS.md` — the service, network, address, the `gaos` user and ACL, the password file.
33. `deploy/edge/README.md` — §5 (`:170-209`): heading "recommended hardening (D-107)", and under it: "Since v0.69.0 the required step is reviewing the back office's **Traffic** page **at least daily** (within the shortest "kept since" of its unknown-direct stream). It records every request that reaches GAOS's dashboard without passing through this box, every machine-API call, and every direct source since the dashboard started. It is best-effort detection, not a control: it drops records under load, keeps each kind of call only up to its cap and at most 48 hours, loses its direct-source summary when the dashboard restarts, and does not see requests Traefik serves without the dashboard. **An empty direct list is not proof that no direct caller exists.** Until the rules below are applied, a direct peer can reach every route on the port; applying them remains recommended." The sampling step (`:186-187`) gains the `conntrack -E` form of §0; the "mandatory and recorded" check (`:202`) applies when the rules are applied.
34. `pustaka/docs/infra/environments.html` (`:17`, `:30`) and `pustaka/docs/assets/toc/infra.js` (`:41`) — prod's five services, Redis named as the traffic store, no address or posture.
35. `pustaka/docs/assets/toc/integrations.js` — the page's entry after `pull-requests` (`:96-107`).
36. `pustaka/docs/product/qa-test-plan.html` — 20 rows, `#go-traffic`, TC-019 and TC-725 texts, the counts.
37. `pustaka/docs/product/brd.html` — BR-66 in §9.5 (`:1276-1291`); "Sixty-six rules" (`:1218`).
38. `pustaka/docs/product/decision-log.html` — D-107; D-106 amended (`:2167-2172`); "One hundred and six entries" (`:18`).
39. `pustaka/docs/assets/toc/product.js` — `:36`, `:129`, `:162`, `:168`.
40. `pustaka/docs/changelog.html` — v0.69.0.
41. `pustaka/docs/assets/toc.js` — `semver: "0.69.0"` (`:22`).
42. `deploy/check-workspace.sh` — `763`, "seven hundred and sixty-three", "nine hundred and three", "One hundred and six entries", "Sixty-six rules" (`:113-133`).
43. `README.md` — "Product version 0.69.0" (`:12`); 763 / 738 / 903 (`:143-145`).
44. `.claude/rules/qa-test-plan.md` — highest TC-903, next TC-904, counts, range map (`check-stacks.sh` gains TC-902, `rebuild-restart.sh` TC-903, `dashboard (go test)` TC-884 … TC-901).
45. `.claude/agents/gaos-deploy.md` — the service list (`:38`).
46. `.gitignore` — `deploy/stacks/*.traffic-*` (after `:17`).

**Arithmetic.** 8 new + 39 changed (38 items, item 34 being two files) = **47 files**. Go tests: 18 new (TC-884 … TC-901), all in `dashboard/`: 745 + 18 = **763**, 720 + 18 = **738**. Register rows: 18 + 2 (TC-902, TC-903) = 20: 883 + 20 = **903**.

## 18. Alternatives

1. **An in-memory ring buffer in the dashboard.** Rejected: the dashboard is replaced on every release; an empty page after each deploy cannot answer "who called yesterday". (The small since-boot direct-source summary does live in memory, because it must survive Redis eviction; it is a complement, not the trace.)
2. **That ring buffer, snapshotted on SIGTERM** (the `prod.bt-state` pattern, `compose.prod.yaml:219`). Rejected: the operator named Redis streams for exactly this ("you can use redis stream just to limit last N record only and do auto retention", §0), and a SIGTERM snapshot loses everything since the last one on SIGKILL, an OOM kill or a crash, while a separate Redis keeps it through any dashboard death.
3. **ArcadeDB.** Rejected: the shared store of record over the tunnel; a write per call adds load to every call path; retention needs schema and pruning; records would drift toward events (BR-04, BR-05).
4. **Traefik's access log as the inbound source.** It sees everything, including E1. Rejected as the page's source: Traefik logs to its container's stdout (`deploy/stacks/traefik.yml:66`), readable by the dashboard only through the Docker socket — forbidden in this stack (`check-stacks.sh:92-94`) — or a new log volume and parser; no principal; no outbound half. It is the operator's manual cross-check for E1.
5. **Prometheus and Grafana.** Rejected: aggregates, not per-call rows with addresses; two services and a second sign-in; a Go dependency.
6. **`go-redis`.** Rejected by the module policy.
7. **Valkey.** Equivalent; not chosen because the operator asked for Redis.
8. **A per-source limiter** (rev 1). Removed in rev 2: separate streams protect what matters with less machinery.
9. **`nopass` with an ACL on the default user** (rev 2). Replaced: if the VM host can reach the bridge, any local process could read every address (round-2 SEC-12); a password from a mode-600 file costs one generated secret.

## 19. Non-goals

- Blocking or rate-limiting anyone.
- Alerts or notices about traffic; a formal record of each review.
- History beyond 48 hours, persistence, export.
- ArcadeDB calls; requests Traefik serves without the dashboard; edge requests to anything but the API.
- Bodies, headers, query strings, configured base paths, branch names, file paths.
- Per-product access scoping; editing or clearing the trace.
- Redis on the retired dev stack or the local `compose.yaml`.
- Settings for the caps and the window.
- A chart or a new vendored library.
- Changing any client's timeout, retry, redirect or TLS behaviour.

## 20. Assumptions, labelled

| # | Assumption | Checked |
|---|---|---|
| A1 | Redis 8.10.3's release date and the `8.10.3-alpine` digest are as §4.1 states | T4: `docker buildx imagetools inspect` |
| A3 | `172.30.14.0/29` is free on both prod VMs | T4: §4.2 |
| A4 | The day's volumes of §5.1 (≈ 30 QA jobs, ≈ 15 long AI turns, ≈ 10 open fix PRs; inbound seconds to minutes apart) | after deploy, on the page (kept-since per stream) |
| A5 | `ServeMux.ServeHTTP` sets `PathValue` on the request it is given | TC-893 |
| A6 | Traefik v3.3 appends the delivering peer to `X-Forwarded-For`, drops an untrusted peer's chain, and sets `X-Real-IP` as §8.3 says | T2, before wiring |
| A7 | Whether the VM host and other containers can reach `172.30.14.2:6379` | T4, recorded as information (§13.4); the password stands either way |
| A8 | Which known clients (the QA tool, Jenkins) call GAOS directly | T4, from the summary and the Traefik log |

(A2 of rev 2 — the image's uid — is gone: Redis now runs as the deploy uid.)

## 21. Round-1 triage

Severities are the reviewers'. No contests.

### Comprehension (`traffic-monitor-comprehension-1.md`, B=0 M=4 N=16)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The compose comment trips the design's own "no depends_on" check | Major | PROPOSE FIX | Comment reworded; `hasntre '^[[:space:]]*depends_on:'` (§4.1, §4.4 C30) |
| The success criterion's conntrack check cannot pass as written | Major | PROPOSE FIX | Restated with E1–E3 (§0); direct requests on every route recorded (§8.1) |
| The 5000 cap binds long before 48 h | Major | PROPOSE FIX | Per-platform streams, derived caps, kept-since (§5.1, §10.2) |
| TaskMgr failures recorded as HTTP 200 successes | Major | PROPOSE FIX | TaskMgr's outcome recorded (§7.6); TC-889 |
| Glossary "Drop" contradicts `suppressed` | Nit | PROPOSE FIX | §1 |
| "Window" used in three senses | Nit | PROPOSE FIX | "Retention window" only |
| Edge/direct rule differs; one-sided limit | Nit | PROPOSE FIX | `via` from the delivering hop (§8.3) |
| Unmatched `/v1/` paths are not only 404s | Nit | PROPOSE FIX | 405 too (§8.2) |
| Several anchors off by one or overstated | Nit | PROPOSE FIX | Re-anchored |
| README version line missing | Nit | PROPOSE FIX | §17 item 43 |
| check-stacks assertions cannot be built from the helpers | Nit | PROPOSE FIX | Block helpers (§4.4) |
| Some claims cannot be checked; two unanchored | Nit | PROPOSE FIX | A1, a T4 re-read; the two-week rule labelled; a published anchor in §11 |
| A whole-stack restart also empties Redis | Nit | PROPOSE FIX | §4.1, the docs page |
| The Off state tells the operator to set a fixed value | Nit | PROPOSE FIX | §10.4 |
| Injected AI-manager client | Nit | PROPOSE FIX | Default branch only (§7.1) |
| `github-write` label source | Nit | PROPOSE FIX | Set in `newGHWriter` |
| TC-890 timing assertion flaky | Nit | PROPOSE FIX | Counter-based, test hook |
| The page renders paths | Nit | PROPOSE FIX | Reason stated (§7.4) |
| The live fragment omits OOB | Nit | PROPOSE FIX | §10.5 |
| The `other` bucket undefined | Nit | PROPOSE FIX | Limiter removed |

### Zoom-out (`traffic-monitor-zoom-out-1.md`, B=1 M=3 N=6)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The firewall demotion rests on a decision the quoted request does not contain | Blocker | PROPOSE FIX | The full reply quoted with the item it answered (§0); firewall recommended; D-106 amended |
| The inbound recorder watches the wrong place | Major | PROPOSE FIX | Every direct request recorded; E1 named |
| One shared outbound stream | Major | PROPOSE FIX | Per-platform streams; estimate |
| The per-source limit does not protect | Major | PROPOSE FIX | Deleted; separate inbound streams |
| Two Redis commands have no caller | Nit | PROPOSE FIX | `PING`, `XLEN` removed from the client |
| A record-format version | Nit | PROPOSE FIX | Dropped |
| Existing helpers and CSS | Nit | PROPOSE FIX | §11 |
| Two handles to the recorder | Nit | PROPOSE FIX | One pointer (§7.2) |
| Cap and window are settings | Nit | PROPOSE FIX | Constants |
| Too much low-level detail | Nit | PROPOSE FIX | Contracts only; check-stacks lines kept on request |

### Critic (`traffic-monitor-critic-1.md`, B=1 M=4 N=9)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| D-107 makes the firewall optional on a page that cannot see most direct callers | Blocker | PROPOSE FIX | Every direct request recorded; D-107 residuals |
| One stream per direction (the streams and kept-since part) | Major | PROPOSE FIX | §5.1, §10.2 |
| One stream per direction (the per-hour counters part) | Major | DECLINE | Per-hour Redis counters would need more ACL commands and a second retention; kept-since per stream and the since-boot summary answer the honesty problem (§22, critic-2 last row) |
| check-stacks fails on its own comment | Major | PROPOSE FIX | §4.4 C30 |
| `via` guessed from the address class | Major | PROPOSE FIX | Delivering hop (§8.3) |
| The success criterion fails as written | Major | PROPOSE FIX | §0 |
| `noeviction` mitigation claim | Nit | PROPOSE FIX | Removed |
| Anchors and boot order | Nit | PROPOSE FIX | §7.2 |
| `traffic-live` attributes | Nit | PROPOSE FIX | §10.5 |
| Leftover undecided items | Nit | PROPOSE FIX | `XLEN` gone; TC-431 unchanged |
| Dev parity comment and service list | Nit | PROPOSE FIX | §4.2, §4.5 |
| Network assertion helper | Nit | PROPOSE FIX | §4.4 |
| `MaxBytesReader` close-after-reply | Nit | PROPOSE FIX | Residual (§8.6) |
| TC-890 wall-clock | Nit | PROPOSE FIX | §15 |
| Alternative 1's strongest form | Nit | PROPOSE FIX | §18 alternative 2 |

(Rev 2 recorded the per-hour-counter decline inside the PROPOSE FIX row; round-2 critic flagged it, and the row is split here.)

### Readiness (`traffic-monitor-readiness-1.md`, B=0 M=4 N=9)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The Redis comment fails its own check | Major | PROPOSE FIX | §4.4 C30 |
| The conntrack check fails by construction | Major | PROPOSE FIX | §0 |
| TaskMgr failures as successes | Major | PROPOSE FIX | §7.6 |
| Filters undefined with `dir=all` | Major | PROPOSE FIX | §10.3 |
| `server.traffic` only in the change list | Nit | PROPOSE FIX | Dropped |
| `AIMClient.client()` branches | Nit | PROPOSE FIX | §7.1 |
| `github-write` label | Nit | PROPOSE FIX | §7.1 |
| A handler that writes nothing | Nit | PROPOSE FIX | §8.6 |
| TC-725's new text | Nit | PROPOSE FIX | §14 |
| A count mirror and a service list missing | Nit | PROPOSE FIX | §17 items 39, 45 |
| Anchors drifted | Nit | PROPOSE FIX | Re-anchored |
| TC-900's negative half | Nit | PROPOSE FIX | §4.4 (now TC-902) |
| A plain `restart` wipes the trace | Nit | PROPOSE FIX | §4.1 |

### Security (`traffic-monitor-security-review-1.md`, High=1 Medium=5 Low=4)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| SEC-1 Unauthenticated caller wipes the inbound trace | High | PROPOSE FIX | `in:anon`, the ¾ reserve, kept-since; TC-895 |
| SEC-2 Direct non-API callers invisible | Medium | PROPOSE FIX | §8.1 |
| SEC-3 README downgraded on a contradicted claim | Medium | PROPOSE FIX | Firewall recommended; honest note; D-107 residual |
| SEC-4 Redis takes every command | Medium | PROPOSE FIX | ACL (rev 3: a password-protected user, §4.1) |
| SEC-5 Outside DNS | Medium | PROPOSE FIX | Pinned subnet, static IP, IP literal only |
| SEC-6 Base-path secrets | Medium | PROPOSE FIX | `{base}` |
| SEC-7 `via` from address class | Low | PROPOSE FIX | Delivering hop |
| SEC-8 Deny-lists | Low | PROPOSE FIX | TC-897 |
| SEC-9 Addresses to every account | Low | PROPOSE FIX | Public addresses masked (rev 3 scope, §10.1) |
| SEC-10 Published posture | Low | PROPOSE FIX | §13.8 |

## 22. Round-2 triage

Severities are the reviewers'. No contests.

### Zoom-out (`traffic-monitor-zoom-out-2.md`, B=0 M=2 N=5)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| Caps sized the same for every stream | Major | PROPOSE FIX | qa-tool and ai-manager 5 000, github 3 000, others 1 000; derivation and memory sum (§5.1) |
| The `via` fallback fails open and keeps `GAOS_TUNNEL_IP` | Major | PROPOSE FIX | No `X-Forwarded-For` → direct; `GAOS_TUNNEL_IP` dropped from the dashboard (§8.3, §4.3) |
| TaskMgr's deferred mode in the generic transport | Nit | PROPOSE FIX | TaskMgr not wrapped; `do` records once in a `defer` (§7.6) |
| Masking private and tunnel addresses | Nit | PROPOSE FIX | Only public addresses are masked (§10.1) |
| `+acl|list` widens the ACL | Nit | PROPOSE FIX | Dropped; T4 proves the ACL by `NOPERM` (§13.4) |
| A6 checks `X-Forwarded-For` but not `X-Real-IP` | Nit | PROPOSE FIX | A6 records both (§8.3) |
| TC-901 "eleven results" vs ten lines | Nit | PROPOSE FIX | Eleven lines over N = 30 conditions, stated once (§4.4, TC-902) |

### Critic (`traffic-monitor-critic-2.md`, B=0 M=6 N=12)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| The gate's own refusals may not be recorded | Major | PROPOSE FIX | `src`, `via`, tag, wrap and `defer` at the top of `gate` (§8.1); TC-893 adds the signed-out 401, `requireBO` and the cross-origin 403 |
| Legitimate direct callers share `in:direct` with unknown peers | Major | PROPOSE FIX | `in:direct:auth` and `in:direct:other`; the since-boot summary of 512 sources (§8.7); the tile counts unknown direct sources; TC-896; A8 |
| Masking collapses every tunnel peer for non-admins | Major | PROPOSE FIX | Only public addresses masked; masking in the page model; TC-901 |
| "Kept since … 48 h" claims coverage the store may not have | Major | PROPOSE FIX | Always the oldest record's time, *cap* at or above the cap; `firstWriteAt` and the "may have been emptied" note (§9.2, §10.2); TC-898 |
| A pinned address with dynamic neighbours can stop Redis | Major | PROPOSE FIX | `ip_range: 172.30.14.4/30` (§4.2, C19); `rebuild-restart.sh` warns unless Redis is healthy (§4.5, TC-903) |
| "Reviewing the page" has no cadence or record (cadence part) | Major | PROPOSE FIX | At least daily, within the shortest `in:direct:other` kept-since, in D-107 and the README; the summary keeps a missed day's unknown peers (§0, §14, §17 item 33) |
| "Reviewing the page" has no cadence or record (record part) | Major | DECLINE | The operator chose periodic monitoring (§0); the summary covers a missed review |
| The `fail` and `warn` tile tones do not exist | Nit | PROPOSE FIX | `block` and `ask`; chips anchored `:2000-2003` (§10.2, §11) |
| TC-901 says ten lines and eleven results | Nit | PROPOSE FIX | N = 30 conditions, eleven lines, stated once |
| "Traffic network only" asserts presence, not exclusivity | Nit | PROPOSE FIX | C14, C15, C20 (§4.4) |
| `GAOS_TUNNEL_IP`'s /24 adds nothing | Nit | PROPOSE FIX | Dropped |
| BR-66 sits in the QA sync section | Nit | PROPOSE FIX | §9.5 (§14) |
| TC-890 and TC-895 need a writer stall | Nit | PROPOSE FIX | The writer-not-started test hook (§9.2) |
| E1 names only `/` and `/docs/` | Nit | PROPOSE FIX | "outside `/app/` and `/api/`, or the two slash redirects" (§0) |
| `conntrack -L` is a point-in-time listing | Nit | PROPOSE FIX | `conntrack -E … --orig-port-dst 18301` logged to a file (§0, §17 item 33) |
| The ACL described as "three commands" while it admits five | Nit | PROPOSE FIX | Stated exactly: three data commands and `PING` for user `gaos`, default user off (§2, §4.1, §14) |
| TaskMgr "Failed" disagrees with the client | Nit | PROPOSE FIX | Failed = `auth` or `unavailable`; raw code shown (§7.6) |
| Small anchor drift | Nit | PROPOSE FIX | `staging.go:276-297` throughout |
| One triage row is a partial decline | Nit | PROPOSE FIX | The row is split in §21 |

### Security (`traffic-monitor-security-review-2.md`, Medium=2 Low=6)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| SEC-11 The review step can lose direct-caller evidence; E3 passes anyway | Medium | PROPOSE FIX | Split direct streams; the summary eviction never touches; E3 explains only if the summary covers the period, else inconclusive; the cadence; TC-896 |
| SEC-12 If the host reaches Redis, any local process reads everything | Medium | PROPOSE FIX | `default` off; user `gaos` with a password from `prod.traffic-redis-auth` (mode 600, generated, never printed) through an ACL file of its SHA-256 written by `traffic-redis.sh`; the read residual named in §13.4 and D-107; A7 recorded as information, because the password stands either way |
| SEC-13 The gate's refusals may never reach the recorder | Low | PROPOSE FIX | Top of `gate` (§8.1); TC-893 |
| SEC-14 The `via` fallback fails open; the rightmost parse unspecified | Low | PROPOSE FIX | No XFF → direct; the last entry of the joined `Header.Values` (§8.3) |
| SEC-15 check-stacks asserts presence, not exclusivity | Low | PROPOSE FIX | C14 no `internal`, C15 no `network_mode`, C20 exactly two services on `traffic` |
| SEC-16 Masking side channels | Low | PROPOSE FIX | Masked in the page model; tiles count masked values; TC-901 asserts no full public address anywhere in a non-admin's HTML |
| SEC-17 `GAOS_REDIS_ADDR` accepts any IP | Low | PROPOSE FIX | Must be inside `172.30.14.0/29` (§4.3); TC-886 |
| SEC-18 The template keeps branch names and file paths | Low | PROPOSE FIX | `{ref}` and `{path}` (§7.4); TC-887 |

## 23. Implementation notes

Deviations from rev 3, each the smallest faithful one, recorded as they were taken.

### T1

1. **`errClass` is `trafficErrClass`.** `braintown.go:575` already declares an `errClass` (a different order, used by the Brain Town row). The traffic one keeps §7.5's closed set and order under its own name.
2. **TaskMgr's record in `do` lands in T1, not T2.** TC-889 (a T1 row) asserts it through `taskmgrClient.do`, so the deferred record (§7.6) is written with it. `taskmgr.go`'s transport is untouched; `do` gains named results so the `defer` reads the error it returns.
3. **A successful trim counts as a write for `firstWriteAt`.** The writer trims once when it starts and then every 60 s (§9.2 says every 60 s). Without this, a dashboard that made no call in its first minute would show "the traffic store may have been emptied" on a store nothing emptied. The trim is a write command, so "the first successful write after every (re)connect" still holds.
4. **The anonymous-drop counter.** A record bound for `in:anon` that is refused at ¾ of the queue, or by a full queue, counts in `droppedAnon` only; every other refusal counts in `dropped`. The page shows both (§10.2 item 2).

### T2

5. **A6, confirmed before any wiring** (2026-11-27, `traefik:v3.3` = v3.3.7, `deploy/stacks/traefik.yml` unchanged, a one-route echo backend, a docker network `10.10.0.0/24` with clients at `10.10.0.1` (trusted) and `10.10.0.9` (untrusted)). What the backend received:

   | From | Sent | `X-Forwarded-For` | `X-Real-IP` |
   |---|---|---|---|
   | `10.10.0.1` | nothing | `10.10.0.1` | `10.10.0.1` |
   | `10.10.0.1` | XFF `198.51.100.7` | `198.51.100.7, 10.10.0.1` | `10.10.0.1` |
   | `10.10.0.1` | XFF `198.51.100.7`, X-Real-IP `192.0.2.66` | `198.51.100.7, 10.10.0.1` | `192.0.2.66` |
   | `10.10.0.9` | nothing | `10.10.0.9` | `10.10.0.9` |
   | `10.10.0.9` | XFF `198.51.100.7` | `10.10.0.9` | `10.10.0.9` |
   | `10.10.0.9` | XFF `198.51.100.7`, X-Real-IP `192.0.2.66` | `10.10.0.9` | `10.10.0.9` |

   A trusted peer's chain is kept and the delivering peer appended; an untrusted peer's chain is replaced by the peer. `X-Real-IP` is kept from a trusted peer (the edge sets it to `$remote_addr`, so the backend receives the client) and is the peer for an untrusted one. One nuance the design did not state: a trusted peer that sets **no** `X-Real-IP` gets the peer itself; the edge always sets it, so `via` (last XFF entry) and `src` (`jenkinsSource`) are as §8.3 says. The design stands.
6. **The gate's body moved into `gateServe`.** `gate` installs the source, the via, the tag, the wrapped writer and the deferred record, then calls `gateServe`, which is the old body unchanged except that it reports the matched pattern and the request handed to the mux (for `PathValue("id")`) through a small `gateTrace`. With no recorder installed (tests, and any process before `serve()`), `gate` calls `gateServe` directly.
7. **The admin principal.** `apiGate` names `admin` for the admin token and `cl.Name` for a client; the clients file's names are already bounded, and the recorder cuts at 64 bytes anyway.
8. **TC-893's tunnel-peer call carries `X-Real-IP: 10.10.0.9`.** `jenkinsSource` skips a private `X-Forwarded-For` hop, so the source of a request from a tunnel peer is the `X-Real-IP` Traefik sets to that peer (A6, row 4). The test sends what Traefik sends.

### T3

9. **Built in the implementing session, not by a separate `gaos-frontend` agent.** The session loaded `emil-design-eng` and the agent's mechanics (`.claude/agents/gaos-frontend.md`) and followed §11: no new motion (only the existing pulse), no new CSS class (one selector widened, `.qa-tbl .r, .af-table .r`), existing tile tones and chips, `tm-scroll af-scroll` tables, `af-md` on Host and Bytes. It was rendered against the real Redis image at 1440 and 390 px wide and read for overflow.
10. **A non-admin's `src` filter is masked before it is compared or echoed.** §10.3 says "compared canonically after the viewer's masking"; the value from the query is masked too, so `?src=203.0.113.9` from a non-admin becomes `203.0.113.0/24` in the filter, the tabs, the poll URL and the pager (TC-899), and the full address never reaches the HTML (TC-901).
11. **The reader lives on the recorder** (`trafficRecorder.rd`), its own connection under its own mutex, with the 15 s cache for every viewer (§10.3). With no recorder installed (`trafficPtr` nil, tests only) the page shows Off with "no traffic recorder is installed".
12. **The Off note's docs link** points at `../docs/integrations/traffic.html`, which T4 writes.
