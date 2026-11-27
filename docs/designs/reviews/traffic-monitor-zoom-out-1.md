# Design review: traffic-monitor.md, rev 1 (Skeptical Architect lens)

**Overall:** the design cannot be approved yet. One finding is a Blocker: the design turns a D-106 security step into an optional one, and the operator's quoted request never asks for that. The Redis store is kept, as the operator asked; nothing below argues against it.

**Checked and solid:**
- **File count.** 7 new files plus items 8 to 43 (36 items, item 33 being two files, so 37 files) gives 44 files. That matches §17.
- **Test count.** `dashboard/` has 720 `func Test` today across 96 files, so 16 new tests makes 736. `deploy/check-workspace.sh:114-115` says 745 Go tests and `:129` says 883 cases, so 761 and 900 follow. TC-884 to TC-899 is 16 tests; adding TC-900 makes 17 register rows.
- **Page count.** `urls.go:31-35` has nine views and `urls_test.go:233-234` asserts eleven page patterns, as the design says.
- **Inbound hook points.** The anchors in `auth.go:541-590` and `main.go:666-669` (the gate and the bearer marking) are accurate. Reusing `jenkinsSource` (`staging.go:276-316`) for the caller's address is correct reuse.
- **Wrappers.** No existing `RoundTripper` or `ResponseWriter` wrapper exists to reuse. A search finds only `waTransport` at `wa.go:205`, so the new `recordingTransport` and `trafficWriter` are justified.
- **Hand-written Redis client (§6).** This follows from `dashboard/go.mod:1-7` (stdlib only, no dependencies). It is not reinventing the wheel.
- **Rejected in-memory alternative (§18.1).** Stated fairly. `compose.prod.yaml:18` fixes `name: gaos-prod` and `rebuild-restart.sh:365` runs `up -d`, so Redis really does survive dashboard redeploys.

---

### The firewall demotion and the inbound half rest on a decision the quoted request does not contain - Severity: Blocker
- **Affected scope:** §0 "The context", §13.7, §14 (the D-107 text, and "D-106 — unchanged"), §17 item 32, §18.7.
- **Simplicity rationale:**
  - The operator's quoted request is about outbound traffic only: "view traffic outbound to QA tool (and other external platform)". It says nothing about the firewall or inbound callers. §18.7 even calls outbound-only "the literal request".
  - Yet D-107 records "instead of requiring the host firewall", and item 32 changes `deploy/edge/README.md` §5 to "Optional since v0.69.0". Today that section says "The check is mandatory and recorded" (`deploy/edge/README.md:202`).
  - D-106 itself lists the firewall as the remaining operator step (`pustaka/docs/product/decision-log.html:2170`). Downgrading it changes D-106's residual risk, so "D-106 — unchanged" is not true.
  - The inbound half roughly doubles the design: §8 entirely, §8.5, edits to `apiGate` and `jenkinsGate`, and TC-893 to TC-895. It also changes a security posture. None of that can rest on an operator preference that the document never quotes.
- **Recommendation:**
  - Quote the operator's own words choosing visibility over the firewall (date and wording), or ask for that decision before review continues.
  - Without it, ship outbound only (the literal request) and leave the edge README and D-106 untouched. Inbound could then be a follow-up design.
  - Either way, record a D-106 amendment rather than calling D-106 "unchanged".

### The inbound recorder watches the wrong place to answer the firewall's question, and the success check fails as written - Severity: Major
- **Affected scope:** §0 "Success" (the conntrack cross-check), §8.1, §19 ("/healthz; static files"), §18.4.
- **Simplicity rationale:**
  - The firewall question is "who reaches Traefik on port 18301" (`deploy/edge/README.md:177-187`). Traefik serves the landing page, the docs, the back office, `/healthz` and the API (`compose.prod.yaml:31-258`).
  - The recorder sits in the dashboard's gate and records only `/v1/` paths. So a tunnel peer that only fetches `/api/healthz` (an uptime probe, say), `/docs/` or the back office appears in a `conntrack` sample but never on the page.
  - The success check also fails as worded. The design requires that "a `conntrack -L` sample of the tunnel port … names no peer that the page does not show as `direct`". But the main peer in any sample is the edge itself, `10.10.0.1`, and the page never shows it, because `X-Real-IP` replaces it with the public client address and labels the call `edge`.
  - The one existing component that sees every request at the right place is Traefik's access log, which already records the client address for every route (`deploy/edge/README.md:151-160`). §18.4 rejects it partly for lacking outbound data, which is a different half of the feature.
- **Recommendation:**
  - If inbound stays in scope, word the success check precisely: "excluding `10.10.0.1`, and only for `/v1/` paths".
  - State on the page and in the edge README note that `/healthz`, the docs, the landing page and the back office are not covered. Without that, the "optional since D-107" note claims a coverage the page does not give.
  - Alternatively, make Traefik's access log the inbound source.

### One shared outbound stream lets busy platforms push quiet ones out, and the 48-hour window cannot be derived - Severity: Major
- **Affected scope:** §5.1, §5.3, §4.3 (the default of 5000), §10.2 (the header and "always the eight platforms").
- **Simplicity rationale:**
  - The design splits by direction "so a burst in one direction cannot evict the other's history" (§5.1). It excludes ArcadeDB because it "would fill the outbound cap … and push every external platform out of the window" (§7.1).
  - The same logic applies between platforms, and the design ignores it. `fixprpoll.go:16,24` reads every open fix PR from GitHub every 5 minutes for 7 days. That is 12 × 48 = 576 calls per PR in 48 hours, so about nine tracked PRs alone fill the 5000 cap.
  - At that point the "Outbound by platform" table shows "No calls in the window" for the QA tool, which is the platform the operator named.
  - The document gives no expected call volume, so the "48 hours" in the header (§10.2) cannot be checked against the cap.
- **Recommendation:**
  - Key the outbound streams by platform (`gaos:traffic:out:<label>`, eight keys). Each gets its own `MAXLEN`, for example 1000 each.
  - The label set is already closed and fixed (§7.1), so this adds no new mechanism: the page does eight bounded reads instead of one. The 64 MB memory figure in §4.1 still holds.
  - Also write down a call-rate estimate per platform, so the window/cap trade-off can be computed.

### The per-source limit does not protect what it is meant to protect - Severity: Major
- **Affected scope:** §8.5, §9.3 (`suppressed`), §10.2 note 2, TC-895.
- **Simplicity rationale:**
  - The limit is meant to stop unauthenticated floods from "pushing legitimate ones out of the cap". At 300 records per minute per source, one source still fills a 5000 cap in 5000 / 300 ≈ 17 minutes, so legitimate records leave a 48-hour window anyway.
  - Two sources take about 8 minutes, and the 4096-entry map with its shared `other` bucket makes no difference to that.
  - So it adds an in-memory map, a minute clock, an overflow bucket, a counter, a page note and a test, and the outcome barely changes.
- **Recommendation:**
  - Delete §8.5 and TC-895.
  - If unauthenticated floods matter, use the separation the design already uses for directions: write records with an empty client label to their own capped key (`gaos:traffic:in:anon`). Authenticated callers can then never be pushed out by scanners, with no in-memory limiter. A flood still shows up as a flood, which is what a monitoring page is for.

### Two of the five Redis commands have no caller - Severity: Nit
- **Affected scope:** §6 ("five commands only: `PING`, `XADD`, `XTRIM`, `XREVRANGE`, `XLEN`"), TC-884.
- **Simplicity rationale:** The writer (§9.2) uses `XADD` and `XTRIM`, and the reader (§10.3) uses `XREVRANGE`. Nothing in the design calls `PING` or `XLEN`; the healthcheck uses `redis-cli`, not the Go client.
- **Recommendation:** Support only the three commands that are used, and test only those.

### A record-format version field on data that lives at most 168 hours - Severity: Nit
- **Affected scope:** §5.2 field `v`.
- **Simplicity rationale:** Records are wiped on every Redis restart and trimmed after at most a week. A format change can simply be deployed and old records ignored, since the parser already drops entries it cannot parse (§10.3). The field plans for a migration that will never be needed.
- **Recommendation:** Drop `v`.

### Existing helpers and CSS are not named for reuse - Severity: Nit
- **Affected scope:** §11.2, §11.5, §10.2 ("relative time", "cut with …").
- **Simplicity rationale:**
  - `.tf-num` repeats `.qa-tbl .r` declaration for declaration (`dashboard/static/app.css:1543`).
  - Helpers already exist for relative time (`agoAt` at `dashboard/integrations.go:62`, `relSpan` at `dashboard/handlers_bo.go:692`), for cutting text with an ellipsis (`clipEllipsis` at `dashboard/handlers_bo.go:431`) and for thousands separators (`num` → `thousands` at `dashboard/render.go:138`). Only `num` is named in the design.
- **Recommendation:** Make the right-aligned number class general instead of adding a near-copy, and name the three helpers so the page does not grow its own versions.

### Two handles to the same recorder - Severity: Nit
- **Affected scope:** §7.2 (the global `trafficRec atomic.Pointer`), §17 item 8 (`server.traffic`).
- **Simplicity rationale:** The design chooses a process-wide global so that no constructor signature changes, then also adds a field on `server`. Two places hold the same state.
- **Recommendation:** Use one handle. The global is enough, since the gate already checks `trafficRec.Load()`.

### Cap and window are environment settings while the flood limit is a constant - Severity: Nit
- **Affected scope:** §4.3, TC-886, §20 assumption 3.
- **Simplicity rationale:** `GAOS_TRAFFIC_MAX` and `GAOS_TRAFFIC_HOURS` bring parsing, clamping, log lines, compose plumbing, two `check-stacks.sh` assertions and a test. Meanwhile the per-source threshold is changed "in a fix". The operator asked for "last N", not for a tunable N.
- **Recommendation:** Make the cap and window constants for v1, like the other thresholds. Add settings only when an operator actually needs to tune them.

### Too much low-level detail for a design document - Severity: Nit
- **Affected scope:** §6 (function signatures, `respValue` layout), §7.4 (the segment regex), §10.5 (the full `hx-trigger` string), §11.2 and §11.5 (CSS declarations), §4.4 (assertion one-liners).
- **Simplicity rationale:** The parts that carry weight are the contracts: the field list, the bounds, what is never read, and the fact that recording never blocks. Exact helper signatures and CSS lines are implementation detail. They make the document long (692 lines) without adding decisions to judge.
- **Recommendation:** Keep the contracts and bounds, and move signatures, regexes and CSS into the code.

Counts: B=1 M=3 N=6
