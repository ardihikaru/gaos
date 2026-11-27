# Security review: `docs/designs/traffic-monitor.md` (rev 2)

**Summary.** All ten round-1 findings are resolved in the design text. The round-1 High (SEC-1) is closed, so nothing blocks the design under `docs/rules/gaos-design-review.md:99-100`. This round raises two new Medium findings and six Low ones. The most important is SEC-11: the required review step can quietly lose the evidence it depends on.

**About the brief.** The brief says `signedIn()` "accepts a cookie from any request". That is not true of this code. `signedIn` → `sessionFrom` → `verify` (`dashboard/auth.go:329-396`) checks the HMAC, the expiry, the account and the credential fingerprint. The gate refuses a signed-out back-office route (`dashboard/auth.go:568-585`), and BR-55 says the same (`pustaka/docs/product/brd.html:1355`). So "every signed-in account", and "full addresses for auto-fix admins" (`dashboard/handlers_autofix.go:18-38`, `dashboard/prview.go:502-505`), can both be built as written.

## Paraphrase

### What this proposes
- **Recording.** GAOS records two kinds of call:
  - **Outbound:** every call made through its eight external HTTP clients (ArcadeDB excluded). Each client's transport is wrapped.
  - **Inbound:** every machine-API call, and every request that reached the dashboard *directly*, meaning not delivered to Traefik by the edge hop `10.10.0.1`.
- **What a record holds:**
  - Outbound: host, a path relative to the client's configured base (as `{base}`) with identifiers templated, status, timing, sizes, error class, and the gRPC code for TaskMgr.
  - Inbound API call: route pattern, source (`jenkinsSource`), `via`, principal and product id.
  - Any other direct request: a reduced record with no path.
  - Never a header, body, query string, error text or configured base path.
- **Storage.** Eleven capped Redis streams: eight outbound (1000 each), plus `in:direct` (2000), `in:auth` (2000) and `in:anon` (1000). Records are trimmed to 48 h.
- **The Redis service.** Pinned by digest, no persistence, `noeviction` at 64 MB. It runs non-root and read-only on its own `internal: true` network at the fixed address `172.30.14.2`. It publishes no port. The `default` user has no password and an ACL that allows `xadd`, `xtrim`, `xrevrange`, `ping` and `acl list` on `gaos:traffic:*` only.
- **Writing.** Records go through a non-blocking queue of 4096; anonymous records stop at ¾ full. One writer goroutine pipelines them, and drops are counted.
- **The client.** A stdlib RESP2 client with a bounded parser.
- **The page.** A read-only back-office **Traffic** page with filters and a 30 s poll, reading a snapshot cached for 15 s. Auto-fix admins see full source addresses; everyone else sees /24 (IPv4) or /48 (IPv6).
- **The firewall.** The D-106 host firewall becomes "recommended hardening". Reviewing the page becomes the required step.

### How the existing system works, per this document
- **The path in.** The edge (nginx, TLS) proxies over WireGuard to Traefik on the VM's tunnel address, port 18301. Traefik trusts forwarded headers from `10.10.0.1/32` only, routes `/app` and `/api` to the dashboard, and sends `/` and `/docs` to other containers.
- **The gate.** Every dashboard request passes `server.gate`. Bearer routes are authenticated in `apiGate`, and the two Basic routes in `jenkinsGate`, keyed on `jenkinsSource`.
- **Outbound.** Clients are built at eight sites. Most refuse redirects.
- **Exposure.** Every WireGuard peer can reach the tunnel port until the firewall is applied.

## Round-1 findings: resolution check

| Finding | Resolved? | Evidence |
|---|---|---|
| SEC-1 anon flood wipes the trace | Yes | Separate streams, the ¾ reserve and kept-since per stream (`traffic-monitor.md:240-244`, `:395`, `:463`, TC-895 `:604`). Direct traffic has a related gap, raised as new SEC-11. |
| SEC-2 direct non-API callers invisible | Yes, mostly | Reduced records for every direct request (`:360`); E1 named (`:36`). The gate's own refusals are a gap: SEC-13. |
| SEC-3 README downgrade | Yes | `:665` (best-effort, "not proof", firewall recommended); D-107 residual (`:575`). |
| SEC-4 Redis takes every command | Yes | ACL in `:115`, asserted at `:219`; T4 `FLUSHALL → NOPERM` (`:552`). The residual when the host can connect is understated: SEC-12. |
| SEC-5 name resolves through outside DNS | Yes | Pinned subnet and IP (`:129`, `:161`); IP literal only (`:184`), TC-886. Hardening remains: SEC-17. |
| SEC-6 base-path secrets | Yes | `{base}` (`:334`); TC-888 (`:597`). Confirmed the WhatsApp paths after the base are fixed (`dashboard/wa.go:286,368`). A related Low on branch and file paths: SEC-18. |
| SEC-7 `via` from the address class | Yes | Delivering hop (`:375`), A6 (`:378`). The fallback direction is a Low: SEC-14. |
| SEC-8 deny-lists | Yes | `:554`, TC-896. |
| SEC-9 addresses to every account | Yes | `:428`, TC-900. Masking side channels: SEC-16. |
| SEC-10 published posture | Yes | `:571`, `:666`. |

## Trust boundaries touched

| From → To | Authn | Authz | Validation | Far side fails |
|---|---|---|---|---|
| Tunnel peer / edge → dashboard (now recorded) | unchanged | unchanged | `via` from the rightmost XFF hop; `src` from `X-Real-IP` | n/a |
| dashboard → Redis | none (`nopass`) | ACL, 5 commands, `gaos:traffic:*` | bounded RESP parser | drops counted; no DNS (pinned IP) |
| VM host / local processes → Redis | none | ACL | — | A7 unverified: SEC-12 |
| operator browser → Traffic page | verified session (BR-55) | any account; full sources only for auto-fix admins | closed-set and `netip` params, not echoed | Unavailable state |
| dashboard → eight platforms | unchanged | unchanged | `{base}` and template | unchanged; recording is a no-op when off |

**Abuse cases checked and found adequately handled:**
- **Injection into Redis or the page:** every inbound field is closed-set, `netip`-parsed or a fixed pattern. Outbound host and path come from configuration and templating. Output goes through `html/template` (TC-900).
- **Secrets in records:** headers, query, userinfo, body and `err.Error()` are never read (`:330`).
- **A forged `X-Real-IP` or `X-Forwarded-For` from a non-edge peer:** Traefik discards it (`deploy/stacks/traefik.yml:52-53`, `deploy/edge/README.md:158-164`). The rightmost hop is appended by Traefik (A6, tested in T2).
- **Oversized replies:** the parser is bounded. Total data is bounded by `maxmemory` 64 MB.
- **CSRF:** the new routes are GET-only and change nothing.
- **Redirects:** five clients refuse them (`dashboard/braintown.go:292`, `github.go:183`, `qaclient.go:147`, `taskmgr.go:233`, `wa.go:183`). A redirect on the AI-manager or Brain Town reader is only a templated host and path.

## New findings

### SEC-11 The required review step can lose its direct-caller evidence to ordinary direct traffic, and exclusion E3 lets the cross-check pass anyway - Severity: Medium
- Component: inbound stream layout, the firewall cross-check, D-107
- Evidence:
  - `docs/designs/traffic-monitor.md:240`: `in:direct` (cap 2000) holds *every* direct record: authenticated API calls, unauthenticated ones and reduced records.
  - `:255`: the volume estimate assumes the QA tool's calls arrive through the edge (A4).
  - `deploy/edge/README.md:199-200`: names a QA backend *on the VM* that calls `http://<vm-tunnel-ip>:18301/api/v1`. Such calls are `direct`, not `auth`.
  - `:38`: E3 says a peer seen before the oldest kept record "is explained by it".
  - `:559`: the direct-flood residual.
  - `:14`: the operator says "periodic monitoring".
  - `:697`: alerts are a non-goal.
- Attack path:
  - **Ordinary eviction.** A legitimate, frequent direct caller (a VM-local QA backend polling the API) fills `in:direct` within hours. A tunnel peer's one-off direct probe is then pushed out of the 2000-record cap long before 48 h. No flood and no attacker effort is needed.
  - **No alert, no record that survives.** There is no alert, and no per-source summary that eviction cannot remove. A "periodic" review misses the probe.
  - **The cross-check passes anyway.** Under E3, the conntrack cross-check passes: any peer older than the oldest kept direct record counts as "explained".
  - **Result.** The step that replaces the firewall as *required* can neither show nor be failed by the exact event it exists to catch.
- Remediation:
  - **Split `in:direct` in two.** One stream for direct calls with a principal, one for unauthenticated and reduced direct records. Known direct clients then cannot evict unknown peers.
  - **Keep a small per-source summary that volume cannot evict.** One entry per distinct direct source: first seen, last seen and count, bounded to a few hundred sources, kept 48 h. Volume cannot evict it, only the number of distinct sources. It can live in memory since boot, or in Redis (which widens the ACL by the hash commands it needs).
  - **Restate E3 and the review cadence.**
    - E3 explains a peer only if the per-source summary covers the sampling period. Otherwise the cross-check is *inconclusive*, not passed.
    - D-107 and the README state the review cadence against the shortest `in:direct` reach the page shows.
  - **Test it.** Add a TC: a burst of authenticated direct calls does not evict a single unauthenticated direct record.
- Effort: small

### SEC-12 If the VM host can reach Redis, any local process or user can read every source address and principal; the residual mentions only forging and trimming - Severity: Medium
- Component: the traffic store's access control
- Evidence:
  - `docs/designs/traffic-monitor.md:115`: `nopass`, and the ACL grants `xrevrange`.
  - `:551`: A7 checks host reach in T4. If the host connects, "the ACL below is what stands".
  - `:552`: the residual names forged records and trims only, and says such a process "is past Docker's isolation".
  - `:543`: the personal-data claim ("never written to … shown in full only to auto-fix admins").
  - `:717`: A7 is unverified.
- Attack path:
  - **Host processes are not stopped by the `internal` flag.** A host process reaching a container on a bridge network goes through the host's own routing, not the inter-network isolation rules. That is why A7 is an open assumption, and not "past isolation".
  - **What a local user gets.** If T4 finds the host can connect, any unprivileged account or process on the VM can read the full trace with `XREVRANGE`: full source addresses (shown on the page only to auto-fix admins), client names and product ids. It can also `XTRIM` the streams to erase the evidence.
  - **The design has no stop condition for that result.** It records it and continues, so the masking control of SEC-9 is bypassed at the store.
- Remediation:
  - **Make A7 a gate.** If the host connects, T4 stops.
  - **Then add authentication.** Use `user default off`, plus a named user for the dashboard with the same command and key ACL and a password from a mounted secret file, as the existing secrets are mounted (`deploy/stacks/compose.prod.yaml:207`). Add a matching check-stacks assertion.
  - **In either case,** name the *read* residual in §13.4 and D-107.
- Effort: small

### SEC-13 The gate's own refusals may never reach the recorder - Severity: Low
- Component: inbound recorder in `server.gate`
- Evidence:
  - `docs/designs/traffic-monitor.md:365`: the tag, the wrapped writer and the deferred `rec.in` are described around the call to `mux.ServeHTTP`.
  - `dashboard/auth.go:564-566` (cross-origin 403) and `:569-584` (signed-out 401 and the sign-in page) answer *before* `mux.ServeHTTP`.
  - `dashboard/auth.go:544-546`: the `pat == ""` branch.
  - TC-893 (`:602`) does not say whether its "direct back-office request" is signed in or out.
- Attack path: a tunnel peer probes back-office routes signed out. If the defer is installed only around `mux.ServeHTTP`, every one of those 401s and 403s goes unrecorded. The peer is then invisible on the page, apart from `POST /auth/login`.
- Remediation:
  - Install the writer wrap and the deferred `rec.in` right after `src` and `via` are computed, before any early return in `gate`, including the `pat == ""` branch.
  - Add TC-893 cases: a signed-out direct back-office request (401), and a cross-origin direct POST (403).
- Effort: quick-win

### SEC-14 The `via` fallback fails open to `edge`, and the rightmost-hop parse is unspecified - Severity: Low
- Component: `via` classification
- Evidence:
  - `docs/designs/traffic-monitor.md:379`: with no `X-Forwarded-For`, any public `src` becomes `edge`, and a non-API `edge` request records nothing (`:361`).
  - `:375`: "the rightmost `X-Forwarded-For` entry", with no statement on multiple header lines. Compare `dashboard/staging.go:280-281`, which joins `Header.Values`.
  - `src` trusts `X-Real-IP` (`dashboard/staging.go:277`).
- Attack path:
  - A request that reaches `:8080` without Traefik (from a container on `gaos-prod-int` or the new traffic network) carries no XFF and a chosen `X-Real-IP`.
  - It is classed `edge` with a forged public source, and any non-API request it sends is not recorded.
  - An implementation using `Header.Get` would read the first header line, not the last.
- Remediation:
  - With no `X-Forwarded-For`, classify the request as `direct`; tests set the header explicitly.
  - Define "rightmost" as the last entry of the joined `Header.Values`.
  - Optionally, honour the chain only when `RemoteAddr` is in `172.30.12.0/24`.
- Effort: quick-win

### SEC-15 check-stacks asserts that Redis is on the traffic network, not that it is on that network *only* - Severity: Low
- Component: `deploy/check-stacks.sh` traffic block
- Evidence: `docs/designs/traffic-monitor.md:207` (the message says "on the internal traffic network only") and `:221` (only positive `inblock` checks).
- Attack path: a later edit can put the password-less Redis somewhere it was meant never to be, and the check still passes. Examples:
  - adding `internal` to Redis's `networks:`, which puts it beside Traefik, landing and docs on a routable bridge;
  - another service joining `traffic`;
  - `network_mode: host`.
- Remediation:
  - Assert that the redis block names no other network (`notinblock redis 'internal'`) and has no `network_mode`.
  - Assert that exactly two service blocks (dashboard, redis) reference `traffic`.
  - Add each of these to TC-901's by-hand negative run.
- Effort: quick-win

### SEC-16 Masking for non-admins has unspecified side channels - Severity: Low
- Component: Traffic page model and templates
- Evidence:
  - `docs/designs/traffic-monitor.md:506`: "full value in `title`".
  - `:438`: the *Direct sources* tile counts distinct sources, without saying whether masked or full.
  - `:457` and `:476`: source links, `hx-get` and the poll URL carry `src`.
  - TC-900 (`:609`) asserts only the visible text.
- Attack path: a non-admin account reads full addresses from a `title` or an `href`. The tile count can also tell apart callers inside one /24.
- Remediation:
  - Mask in the page model before any rendering.
  - The tile counts distinct *masked* values for non-admins.
  - TC-900 asserts that the full address appears nowhere in a non-admin's HTML, attributes and URLs included.
- Effort: quick-win

### SEC-17 `GAOS_REDIS_ADDR` accepts any IP literal - Severity: Low
- Component: settings
- Evidence: `docs/designs/traffic-monitor.md:184` (an IP and a numeric port is valid); TC-886 (`:595`) refuses names but not a public IP. The dashboard is also on the routable `gaos-prod-int` (`deploy/stacks/compose.prod.yaml:238`).
- Attack path: a mistyped or overridden value sends source addresses and principals, in cleartext, to an arbitrary routable host.
- Remediation:
  - Accept only private addresses, or only an address inside the pinned traffic subnet.
  - Add a TC-886 case: `203.0.113.5:6379` → off.
- Effort: quick-win

### SEC-18 The path template keeps branch names and repository file paths - Severity: Low
- Component: outbound path template
- Evidence:
  - `dashboard/github.go:207,433` (`/branches/<name>`), `dashboard/github.go:587` and `dashboard/githubwrite.go:84` (`/contents/<path>`).
  - `docs/designs/traffic-monitor.md:335`: letter-first segments are kept.
  - `:339`: the reasoning covers owner and repository names only.
- Attack path:
  - Every signed-in account sees branch names, which can carry people's names or ticket text.
  - It also sees file paths inside private repositories, which no existing page shows to that audience.
- Remediation:
  - Template everything after `branches/` as `{ref}` and after `contents/` as `{path}`.
  - Add TC-887 cases for both.
- Effort: quick-win

Counts: Critical=0 High=0 Medium=2 Low=6
