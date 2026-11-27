# Security review: `docs/designs/traffic-monitor.md` (rev 1)

This design is not ready for "Approved": it has one High finding, and under `docs/rules/gaos-design-review.md:99-100` a High is a Blocker. Five Medium and four Low findings follow it.

The brief said the back-office cookie "checks nothing". That is not true of this code. `signedIn` → `sessionFrom` → `verify` (`dashboard/auth.go:329-396`) checks the cookie's MAC first, then the expiry, the account and the account's credential fingerprint. The gate refuses a signed-out back-office route (`dashboard/auth.go:568-585`), and BR-55 (`pustaka/docs/product/brd.html:1355`) says the same. So "any signed-in account may view" can be implemented as written, and I raise no finding about a missing identity.

## Paraphrase

### What this proposes
- **Recording:** GAOS records two kinds of call:
  - every call it makes through its eight external HTTP clients (the QA tool, the AI manager, Brain Town and its reader, GitHub read and write, WhatsApp, TaskMgr; ArcadeDB is excluded);
  - every call made to its machine API (the bearer routes, the two Basic routes, and `/v1/` paths that match no route).
- **What a record holds:** a short, fixed set of fields. For an outbound call: the host and a templated path. For an inbound call: the route pattern, the source address from `jenkinsSource`, an `edge`/`direct` flag based on whether that address is public, and the label of the authenticated client. It never holds headers, bodies, queries or error text.
- **Storage:** a new Redis container that keeps nothing on disk. It sits on a new `internal: true` network shared only with the dashboard. It has no published port and no password, and is capped at 64 MB with `noeviction`. Records go into two capped streams, by default 5000 records per direction and nothing older than 48 h.
- **Writing:** recording never blocks a call. Records go to a bounded queue (4096) that one writer goroutine pipelines to Redis; a full queue or a dead Redis counts as a drop.
- **Flood control:** inbound records are limited to 300 per source per minute, tracked in memory.
- **The client:** a stdlib RESP2 client with a bounded parser.
- **The page:** a read-only back-office page, **Traffic**, open to every signed-in account. It refreshes every 30 s and reads a snapshot cached for 15 s.
- **The firewall step:** the D-106 host-firewall step in the edge README becomes "optional", with this page as the visibility that replaces it.

### How the existing system works, per this document
- **The edge:** it terminates TLS and proxies over WireGuard to Traefik on the VM's tunnel address (port 18301). Traefik trusts forwarded headers only from the edge (`10.10.0.1/32`), so `X-Real-IP` is the edge's `$remote_addr` for edge traffic and the peer's own address for any other tunnel peer.
- **The machine API:** every route passes `server.gate`. Bearer routes are authenticated in `apiGate`, and the two Basic routes in `jenkinsGate`, which keeps a per-source failure lock.
- **The back office:** it is gated by a verified session (BR-55).
- **The dashboard:** it uses only the standard library, and builds every external client in eight places. The edge rate-limits only the two Basic routes. The firewall step is manual, and it starts by sampling who calls the port.

## Trust boundaries touched

| From → To | Authn | Authz | Validation | If the far side fails |
|---|---|---|---|---|
| Internet / tunnel peer → machine API (now recorded) | bearer / Basic, unchanged | unchanged | source from `X-Real-IP` (the edge overwrites it, `deploy/edge/gaos.conf:126,175`; Traefik trusts only `10.10.0.1/32`, `deploy/stacks/traefik.yml:52-53`) | n/a: recording is fire-and-forget |
| dashboard → Redis (new) | **none** | **none** (all commands) | bounded RESP parser | records dropped; but see SEC-5 (the name falls through to outside DNS) |
| operator browser → Traffic page (new) | verified session (BR-55) | any account | query params checked against closed sets / `netip`, not echoed | "Unavailable" state |
| host / VM-local processes → Redis (new) | none | none | — | design claims unreachable; not verified (SEC-4) |
| dashboard → the eight platforms | unchanged | unchanged | path template (heuristic) | unchanged |

Abuse cases checked and found adequately handled:
- **Injection through records:** RESP bulk strings, and `html/template` output (TC-899).
- **Secrets in headers, query or userinfo:** none stored (TC-888, TC-893).
- **Oversized Redis replies:** the parser is bounded.
- **A forged `X-Real-IP` from a non-edge peer:** Traefik discards it, per `deploy/edge/README.md:160-164`.
- **CSRF:** the new routes are GET-only and change nothing.
- **An unclassified route:** default-deny in the gate, `dashboard/auth.go:568`.
- **An unknown outbound client:** the outbound client list is complete. `http.Client{` is built only at the eight sites plus `arcade.go:74-75`; nothing uses `http.DefaultClient`.

## Findings

### SEC-1 An unauthenticated caller can wipe the inbound trace within minutes - Severity: High
- Component: inbound recorder, per-source limit, retention
- Evidence: `docs/designs/traffic-monitor.md:374` (300 per source per minute; 4096 sources, then one shared `other` bucket), `:44` and `:168` (cap 5000, at most 20 000), `:544` ("an inbound flood is bounded twice"); `deploy/edge/README.md:163-166` and `deploy/edge/gaos.conf:120` (the edge limits only the two Basic routes); `dashboard/staging.go:307-316` (IPv6 is reduced to a /64)
- Attack path:
  - **One address:** an unauthenticated internet caller sends 5 requests a second to any bearer route. Each one is stored as a record. In about 17 minutes those records push every older inbound record out of the 5000-record cap.
  - **Many addresses:** with N IPv4 addresses, or N IPv6 /64s (one /48 holds 65 536), the limit allows N × 300 records a minute, and the 4096-source map then adds a shared `other` bucket. So the whole inbound history, including `direct` rows, can be wiped in under a minute. Under the same flood the 4096-slot queue fills, which also drops outbound records.
  - **Why it matters:** the page is offered as the replacement for a network control (D-106 §5). A tunnel peer that probes directly, or an outsider who wants to hide earlier probing, can bury the evidence. The two "bounds" in §13.5 limit memory, not loss of evidence.
- Remediation:
  - **Separate stores:** keep records that must survive in their own stream or stream budget, which unauthenticated public traffic cannot evict. That means `via = direct` records, and records where an authenticated principal was set.
  - **A global budget:** cap unauthenticated public-source records globally per minute (for example 600 a minute in total across all sources). Count anything past that budget, without storing it, in a per-source summary held in memory (source, count, first and last seen), and show that summary on the page.
  - **Show eviction:** show the time of the oldest retained record per direction, so the operator can see when the trace has been pushed out.
  - **Tests:** extend TC-895 with a many-source flood, and assert that a `direct` record survives it.
- Effort: small

### SEC-2 Direct callers of non-API routes are invisible, so the success test cannot hold - Severity: Medium
- Component: inbound recorder scope
- Evidence: `docs/designs/traffic-monitor.md:348` ("Nothing else is recorded: not the back office, not `/healthz`, not static files"), `:30` (the success test: a `conntrack` sample "names no peer that the page does not show as `direct`"); `deploy/edge/README.md:177-180,186-187` (the firewall question is about the port, not the API)
- Attack path:
  - A tunnel peer that reaches port 18301 directly and calls only the back-office sign-in, `/healthz`, the landing page or the docs never appears on the page. That includes sign-in guessing against the back office.
  - The firewall step was meant to catch exactly these peers. `conntrack` will show such a peer and the page will not, so the stated check either fails or gets explained away.
- Remediation:
  - Record a reduced record for every request, on any route class, whose source is not public: no path, only the route class (`backoffice`, `public`, `unmatched`) and the status.
  - Alternatively, narrow both the success test and the README wording to "machine-API callers" only, and keep the firewall as the control for the rest of the port.
- Effort: small

### SEC-3 The edge README is downgraded on a claim the design itself contradicts - Severity: Medium
- Component: deployment documentation and security posture
- Evidence: `docs/designs/traffic-monitor.md:644` (new README text: the page "shows every caller of the machine API"); `deploy/edge/README.md:202` ("The check is mandatory and recorded"); the design's own losses at `:45` (drops), `:374` (suppression), `:251` (the window), and SEC-1, SEC-2 above
- Attack path:
  - An operator reading the README skips the firewall because the page "shows every caller".
  - The page is lossy by design and can be evicted (SEC-1). Its view covers only the API (SEC-2) and only the last 48 h. So a direct peer keeps unrestricted reach to every route on the port, including the bearer routes, which have no rate limit at the edge.
  - Detection is being sold as prevention.
- Remediation:
  - Reword the README note: the page is best-effort detection; it can drop or evict records; it sees only machine-API calls; and an empty `direct` list is not proof that no direct caller exists.
  - Keep the firewall as the recommended control.
  - Record that downgrade as a named residual in D-107.
- Effort: quick-win

### SEC-4 Redis takes every command from anything that reaches it, and host reachability is not verified - Severity: Medium
- Component: the traffic store
- Evidence: `docs/designs/traffic-monitor.md:129,131,537-538` (claims: "Nothing but the dashboard can reach Redis"; no `requirepass`, no ACL); `deploy/stacks/compose.prod.yaml:25` (a neighbouring product on the same host); `deploy/edge/README.md:199-200` (other VM-local workloads, such as a QA backend)
- Attack path:
  - The design proves isolation only between containers. It does not check whether the VM host, or another workload on it, can reach the Redis container's bridge address.
  - Any process that can reach that address gets every command, with no authentication: it can read source addresses (personal data, §13.2), forge records, or `FLUSHALL` to erase the trace. That is the same evidence-erasure outcome as SEC-1, from a different side.
  - Protected configs only block `CONFIG SET dir`, `DEBUG` and `MODULE`. `FLUSHALL`, `EVAL`, `DEL` and `XDEL` all remain available.
- Remediation:
  - **Restrict the default user with no password.** Use something like `--user default on nopass resetkeys ~gaos:traffic:* -@all +ping +xadd +xtrim +xrevrange +xlen`. That admits only the five commands this design uses, on the two keys, and adds no secret file.
  - **Test it.** Assert it in `check-stacks.sh` (TC-900).
  - **Adjust T4.** The T4 step `CONFIG GET enable-protected-configs` will be refused under that ACL, so run it with an override or replace it with `ACL LIST`.
  - **Verify from the host.** Add a T4 check that a connection from the VM host to the Redis container's address is refused or times out, and record the result in the deploy note.
- Effort: quick-win

### SEC-5 When Redis is down, `redis:6379` can resolve through outside DNS - Severity: Medium
- Component: dashboard → Redis address resolution
- Evidence: `docs/designs/traffic-monitor.md:160` (`GAOS_REDIS_ADDR: "redis:6379"`, a single-label name), `:150` (the traffic subnet is deliberately not pinned), `:604` (the deploy procedure stops Redis on purpose), `:126` (Redis stopped or replaced is an expected state); `deploy/stacks/compose.prod.yaml:20-28,238` (the dashboard is also on the normal bridge `internal`, which has a route out)
- Attack path:
  - While the Redis container is stopped or not yet created, Docker's embedded DNS has no `redis` entry. It forwards the query to the host's resolvers, which add the host's search domains.
  - If that resolves anywhere (a search-domain record, wildcard DNS, or a hostile resolver on the VM's network), the writer redials every 1–30 s over the dashboard's routable network. It would then pipeline source addresses, client labels and outbound hosts to a host outside GAOS.
  - This depends on the host's DNS setup, which the design does not examine; it is a far-side-fails case that the design does not cover.
- Remediation:
  - Pin the `traffic` subnet and give `redis` a static `ipv4_address`. Set `GAOS_REDIS_ADDR` to that IP, and assert both in `check-stacks.sh`.
  - Or: have `dialRESP` refuse a resolved peer outside the private ranges, or outside the configured traffic subnet, and add a TC for it.
- Effort: quick-win

### SEC-6 The path template keeps operator-configured base-path segments, which can hold secrets - Severity: Medium
- Component: outbound path template
- Evidence:
  - `docs/designs/traffic-monitor.md:332` keeps any segment that starts with a letter, is 40 bytes or less, and has at most 3 digits; `:338` claims credentials are "never in a path".
  - `dashboard/wa.go:92-115` validates `GAOS_WA_URL` for scheme and host only; any path is accepted and becomes `cfg.base`.
  - `dashboard/wa.go:286,368` add their paths to `cfg.base`.
  - `dashboard/qaclient.go:110-119` (`validOutURL`) refuses userinfo, a query and a fragment, but not a path.
  - `dashboard/main.go:422` and `dashboard/wa.go:270`: today GAOS deliberately shows only `urlHost(base)`.
- Attack path:
  - A gateway URL that carries an instance key or session name in its path is a common pattern. A letter-first alphanumeric token with few digits would be stored verbatim. A random 32-character base62 token has roughly one chance in six of passing the filter.
  - The token would then be shown to every signed-in account. That weakens the existing control, which shows only the host for this exact URL.
- Remediation:
  - In `trafficWrap`, give the recorder each client's configured base URL. Record only the path *relative to* that base, replacing the base path with a constant such as `{base}`, then apply the template.
  - Extend TC-888 with a base URL whose path segment is a secret.
- Effort: small

### SEC-7 `via` is inferred from the address class, not from the trust boundary - Severity: Low
- Component: inbound `via` classification
- Evidence: `docs/designs/traffic-monitor.md:360`; `deploy/stacks/compose.prod.yaml:40-42` (`GAOS_TUNNEL_IP` is set per instance)
- Attack path:
  - The check `netip.Addr.IsPrivate` does not cover 100.64.0.0/10 (CGNAT, the range used by some overlay VPNs). An instance whose tunnel uses such a range would show every direct peer as `edge`, a silent false negative.
  - The reverse error is safe but noisy: an edge-side client whose `$remote_addr` is private or loopback (a service on the edge box itself) shows as `direct`.
- Remediation:
  - State as a labelled assumption that every tunnel address is RFC 1918 or ULA.
  - Also classify as `direct` any address inside 100.64.0.0/10 or inside the prefix of `GAOS_TUNNEL_IP`, and add a TC-893 case for it.
- Effort: quick-win

### SEC-8 The Redis host is not added to the outbound deny-list - Severity: Low
- Component: outbound URL validation
- Evidence: `dashboard/qaclient.go:121-124,174` and `dashboard/braintown.go:87` (deny-lists hold the hosts of `GAOS_ARCADE_URL`, `GAOS_AIM_URL` and `GAOS_QA_URL`, so a bearer is never sent to an internal service); the design adds a new internal service at `:160` and does not extend them
- Attack path: if `GAOS_QA_URL` or the Brain Town URL is mistyped as the Redis address, GAOS sends a bearer token in cleartext to Redis. Redis's own HTTP guard limits the damage, but the existing control's stated purpose is broken.
- Remediation: add the host of `GAOS_REDIS_ADDR` to both deny-lists in T2, with a test.
- Effort: quick-win

### SEC-9 Source addresses are shown to every account, with no least-privilege reasoning - Severity: Low
- Component: Traffic page access
- Evidence: `docs/designs/traffic-monitor.md:411`, `:529` (it accepts that a source address can identify a person); BR-63 already restricts per-person data to auto-fix admins (cited at `:411`)
- Attack path: any back-office account can see where people call the API from, which can be a home address, for up to 168 h. The page's purpose (spotting `direct` callers) needs the full addresses only for whoever runs the deployment.
- Remediation: show full addresses to auto-fix admins only, and a truncated form (/24 for IPv4, /48 for IPv6) to everyone else. Or record the decision to show them to all accounts explicitly as a residual in D-107 and BR-66.
- Effort: small

### SEC-10 Published docs would describe Redis's lack of authentication and more topology - Severity: Low
- Component: published docs
- Evidence: `docs/designs/traffic-monitor.md:616` (the new public page), `:645` (`infra/environments.html` gains the service); `:131`, `:538` (the "no authentication" posture)
- Attack path: the public docs site already exposes hosts and topology. Adding "an unauthenticated Redis on the dashboard's network" plus the network name tells an attacker who gains code execution in any neighbour exactly what to target.
- Remediation:
  - Keep the authentication posture and the network names out of the published pages; keep them in `deploy/STACKS.md`.
  - Use documentation address ranges (203.0.113.0/24, 192.0.2.0/24) in every example on the new page.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=5 Low=4
