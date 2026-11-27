# Goldfish comprehension review: `docs/designs/traffic-monitor.md` (rev 1)

I read the design, the rubric (`docs/rules/gaos-design-review.md`, `docs/designs/README.md`) and the domain sources the design's claims touch (the BRD §9.8 and invariants, the decision-log head and D-085/D-106, open questions). I checked the code anchors against the repository. I opened nothing under `.claude/`.

## Paraphrase

### What this proposes

1. **A Redis service in the prod compose file.** Its image is pinned by tag and digest. It sits on a new `internal: true` network that only the dashboard joins. It publishes no port, writes nothing to disk, has a 64 MB `noeviction` memory limit, a hardened container and a healthcheck. Nothing depends on it in either direction.
2. **Three new settings.** `GAOS_REDIS_ADDR` is fixed in compose. `GAOS_TRAFFIC_MAX` (default 5000, clamped to 500–20 000) and `GAOS_TRAFFIC_HOURS` (default 48, clamped to 1–168) come from the deploy shell.
3. **`resp.go`, a small RESP2 client.** It uses only the standard library and five commands, and its reply parser is bounded.
4. **Outbound recording.** Each of eight external HTTP client constructors wraps its transport in `recordingTransport`. The wrapper records the platform label, host, a templated path, status, latency to headers, sizes and an error class. It never reads headers, bodies, query strings or error text. ArcadeDB is excluded.
5. **Inbound recording.** In `server.gate`, every bearer-class `/v1` route and every unmatched `/v1/` path is recorded with:
   - the route pattern;
   - the source (from `jenkinsSource`);
   - `edge` or `direct`, inferred from whether that address is public or private;
   - the authenticated client label, set by `apiGate` or `jenkinsGate`;
   - the product id.

   No source may store more than 300 records a minute.
6. **A non-blocking pipeline.** Records go into a 4096-slot channel that drops when full. One writer goroutine batches `XADD … MAXLEN ~ cap` into two streams and runs `XTRIM MINID` every 60 s. Drops and suppressed records are counted, and the log gets one line per state change.
7. **A back-office "Traffic" page** for any signed-in account, polled every 30 s. It shows tiles, a table by platform, a table by inbound source and route, and a filtered, paged list of recent calls, built from a 15-second cached `XREVRANGE` snapshot. It has Off, Unavailable, Empty and Filtered-empty states.
8. **Supporting changes:**
   - `check-stacks.sh` gains a block of nine assertions, and `rebuild-restart.sh` accepts `redis` as a service.
   - The docs, the registers (D-107, BR-66, TC-884–900) and the v0.69.0 release are updated.
   - The edge README's firewall step becomes optional.

### How the existing system works, per this document

- GAOS has no traffic page and no Redis. The prod stack is traefik, landing, dashboard and docs.
- The dashboard is stdlib-only by policy (`go.mod`).
- Outbound HTTP is built in eight constructors, plus ArcadeDB.
- Every route passes `server.gate`. The gate resolves the pattern, sends unmatched paths straight to the mux, and treats every `registerAPI` pattern as bearer, including the two Basic routes.
- `jenkinsSource` derives the caller's address from `X-Real-IP`, which Traefik trusts only from the edge (10.10.0.1), and reduces IPv6 to its /64.
- D-106 left a manual host-firewall step whose first instruction is to sample callers with `conntrack`.

I could paraphrase every section, so there is no comprehension Blocker.

## Clarity

The document flows well: a data-flow diagram, then store, client, outbound, inbound, failure modes, UI, security, registers, tests, phases and change list. Most anchors check out (`auth.go:541-590`, `staging.go:276-316`, `api_v1.go:316-340, 429-456`, `urls.go:31-35, 90`, `compose.prod.yaml`, `check-stacks.sh:24-30, 50-54`, `rebuild-restart.sh:26, 359-368`, `traefik.yml:52-53`, the edge README §4/§5, the BRD rows and the register heads). The findings below are where the document disagrees with itself or with the code.

## Findings

### The compose comment trips the design's own "no depends_on" check - Severity: Major
- Affected scope: §4.1 YAML comment ("there is deliberately no depends_on in either direction"); §4.4 table row "no depends_on"; TC-900
- Finding: The assertion is `hasnt $PROD 'depends_on'`. `hasnt` is `grep -qF` over the whole file, comments included (`deploy/check-stacks.sh:25`). The comment the design writes into `compose.prod.yaml` contains the literal string `depends_on`. Built as written, TC-900's "nothing in prod depends on Redis" line prints `FAIL` against the design's own compose block.
- Recommendation: Do one of two things. Reword the comment (for example "no start-order dependency in either direction"). Or make the assertion a regex that ignores comments, such as `hasntre $PROD '^[[:space:]]*depends_on:'`. State which.

### The success criterion's conntrack check cannot pass as written - Severity: Major
- Affected scope: §0 "Success" (the conntrack cross-check); §17 item 32 (the edge README note); §16 deploy note
- Finding: The check is that a `conntrack -L` sample of port 18301 "names no peer that the page does not show as `direct`". It fails structurally for three reasons:
  - **The edge itself is a conntrack peer.** conntrack's peer for every edge-forwarded call is the edge's tunnel address, 10.10.0.1 (`traefik.yml:52-53`). The page never shows that address: it shows `X-Real-IP`, the public client.
  - **Direct callers of other paths never reach the page.** conntrack records every connection to the port. The page records only `/v1` traffic (§8.1, §19 excludes back office, `/healthz` and static files). A tunnel peer that only hits `/api/healthz` (the README's own check, `deploy/edge/README.md:204-205`) or the back office is a conntrack peer that will never appear.
  - **Some records are never stored.** Per-source suppression (§8.5) and the cap (§5.3) both drop records.

  The README note in item 32 makes a similar overclaim: the page "shows every caller of the machine API".
- Recommendation: Restate the criterion as "every conntrack peer other than the edge (10.10.0.1) that sent a `/v1` request appears as `direct`". Name the excluded classes. Reword the README note to "shows the callers of the machine API within the retained window".

### The 5000 cap will bind long before the 48-hour window, and the page does not say so - Severity: Major
- Affected scope: §0 Success; §4.3 defaults; §5.3; §7.1 (the ArcadeDB rationale); §10.2 header and tiles; §10.4 Empty text
- Finding: The page heading says "over the last 48 hours — at most the newest 5,000 each way", and per-platform "Calls" are "records in the window". But one wrapped client polls fast:
  - The AI manager adapter polls results every 2 s per running turn, for up to 660 s (`adapter_aimanager.go:44-45, 245-272`). That is up to about 330 outbound records per model turn, so roughly 15 turns fill the default cap.
  - Other periodic callers add to it: the QA jobs every 15 s (`qajobs.go:193`) and the fix-PR poll every 30 s (`fixprpoll.go:23`).

  On a working day the outbound cap can cover a few hours, not 48. Rare platforms (WhatsApp, TaskMgr) then drop out of the stream, and their rows show "No calls in the window" when calls did happen. This is the same reason the design gives for excluding ArcadeDB (§7.1), and it is not applied here. The design has no volume estimate, and the page shows only "Recorded up to <time>", never the oldest record kept.
- Recommendation:
  - Add a per-platform volume estimate with these anchors.
  - Show the effective span ("oldest record shown: <time>") and label counts as "in the retained records" when the cap, not the window, is the bound.
  - Then decide one of:
    - raise the default cap;
    - keep a stream per platform;
    - leave AI-manager result polls out of the recording, or sample them.
  - Treat it as a labelled assumption in §20.

### TaskMgr failures will be recorded as HTTP 200 successes - Severity: Major
- Affected scope: §7.1 `taskmgr` row; §7.3; §10.2 "Failed" column and "Outbound failed" tile
- Finding: TaskMgr is gRPC over HTTP/2. Its result is in the `grpc-status` trailer, or in a trailers-only header (`taskmgr.go:9, 310, 348-350`). Per §7.3 the recorder reads only `resp.StatusCode` at headers and never touches the body or trailers. Every failed TaskMgr call that the server answers (auth refusals, unavailable) is therefore stored as `s = 200`, so TaskMgr shows zero failures. The design does not mention this.
- Recommendation: Either state the limitation on the page and in the docs page (a "status is transport-level only" note on the TaskMgr row), or define how a gRPC status is recorded without reading the body. The `RoundTrip` wrapper cannot see trailers, so that would mean, for example, having the TaskMgr client report the gRPC code to the recorder after `resp.Trailer` is read. Choose one.

### The glossary "Drop" contradicts the `suppressed` counter - Severity: Nit
- Affected scope: §1 Glossary "Drop" vs §8.5 and §9.3
- Finding: The glossary counts "the per-source limit was reached" as a drop. §8.5 and §9.3 count it separately as `suppressed`, with its own page note (§10.2 item 2).
- Recommendation: Remove the per-source case from "Drop" and add a "Suppressed" entry.

### "Window" is used in three senses - Severity: Nit
- Affected scope: §1 "Window"; §8.5 ("a fixed window"); §10.5 ("matches the window's granularity")
- Finding: The glossary defines Window as `GAOS_TRAFFIC_HOURS`. §8.5 uses it for the per-minute rate-limit bucket. §10.5 uses it in a sentence that has nothing to do with the 48-hour retention.
- Recommendation: Use "minute bucket" in §8.5, and rephrase §10.5 (for example "matches the data's freshness").

### The edge/direct rule differs between glossary and §8.3, and the stated limit is one-sided - Severity: Nit
- Affected scope: §1 "Via"; §8.3
- Finding: There are three problems:
  - **The two definitions differ.** The glossary says direct means loopback, private or link-local. §8.3 adds `IsUnspecified` and `unknown`.
  - **CGNAT is misclassified.** 100.64.0.0/10 is not `IsPrivate`, so a tailnet or CGNAT direct caller would show as `edge`. The repository already treats that range as tunnel space (`wa.go:189-199`).
  - **Only one misclassification is stated.** A client that reaches the edge from a private address, so that `X-Real-IP` is private, is shown as `direct` although it came through the edge. §8.3 states only the reverse case.
- Recommendation: Make the glossary match §8.3. Decide CGNAT explicitly, for example by reusing `privateAddr`. Add the private-through-edge misclassification to the stated limits.

### Unmatched `/v1/` paths are not only 404s - Severity: Nit
- Affected scope: §8.2
- Finding: An empty pattern covers both 404 and 405 (`auth.go:534-536`). A wrong-method call to a real route, such as `DELETE /v1/products`, will be recorded as `(no route)` with status 405. The design describes only 404.
- Recommendation: Say that 405s land under `(no route)` as well, or record them differently.

### Several anchors are off by one or overstated - Severity: Nit
- Affected scope: §10.1, §17 item 8, §7.1, §7.2, §0
- Finding:
  - The `GET /ui/bo/{view}` pattern is at `main.go:605`, not `:604`.
  - `GET /ui/bo/prs/live` is at `main.go:608`, not `:607`.
  - TaskMgr's `hc.Do` is at `taskmgr.go:339`, not `:332`.
  - §7.2's claim that `grep -n 'Transport.(' dashboard/*.go` finds only `arcade.go:62` is false. It also finds `arcade_test.go:367, 378` and `taskmgr_test.go:409`. The point the claim supports, that no production code asserts on a transport type, still holds.
  - §0 cites `staging.go:270-297` while the glossary and §8 cite `:276-297`.
- Recommendation: Correct the line numbers, and scope the grep claim to non-test files.

### The README version line is missing from the change list - Severity: Nit
- Affected scope: §17 item 42; §14 "Release"
- Finding: `README.md:12` says "Product version 0.68.0", and `check-workspace.sh:149` asserts it equals `toc.js` semver. Item 42 changes only `:143-145`. The phase gate would catch it, but the change list is meant to be complete (README rule 3).
- Recommendation: Add `:12` → "Product version 0.69.0" to item 42.

### The check-stacks block assertions cannot be built from the helpers named - Severity: Nit
- Affected scope: §4.4
- Finding: `has`/`hasnt`/`hasre` take a file argument and grep one line at a time (`check-stacks.sh:24-30`). The design asks for three things they cannot do directly:
  - "the redis block contains …" (block-scoped);
  - "`name: gaos-prod-traffic` followed on the next line by `internal: true`" (two lines);
  - two conditions joined by "and" in one `ok` line.

  Only one new helper, the block extractor, is named. How its output reaches `has` and how the two-line check works are left open, so two engineers would build different checks (README rule 6).
- Recommendation: Specify the helper's output (a temp file or process substitution) and the exact command for the two-line check.

### Some claims cannot be checked by a reviewer, and two are unanchored - Severity: Nit
- Affected scope: header "Registers" (`.claude/rules/qa-test-plan.md:50-51`); §11 (skills and `gaos-frontend.md` anchors); §15; §16 (`.claude/CLAUDE.md` invariant 1); §4.1 image date and digest; §13.6 "released at least two weeks before"
- Finding:
  - The `.claude/` anchors cannot be verified by a reviewer working under this rubric.
  - The Redis 8.10.3 release date and digest are external facts that the repository cannot confirm.
  - The "two weeks" rule in §13.6 cites no policy.
  - T4 records `CONFIG GET enable-protected-configs` but does not re-confirm the digest.
- Recommendation: Where possible, add an anchor outside `.claude/`. Label the image facts as assumptions in §20. Add a T4 step that re-reads the digest (for example `docker buildx imagetools inspect`). Cite the source of the two-week rule, or drop it.

### A whole-stack restart also empties Redis - Severity: Nit
- Affected scope: §4.1 "Persistence" row
- Finding: The row says only a Redis restart empties the streams and that a dashboard redeploy does not. `rebuild-restart.sh prod restart` with no service runs `dc restart` on every service (`rebuild-restart.sh:368`), and with Redis in the stack that now includes Redis, which empties both streams.
- Recommendation: Add this case to the row and to the docs page's "redeploy line".

### The Off state tells the operator to do something they cannot do in prod - Severity: Nit
- Affected scope: §10.4 Off; §4.3
- Finding: The message says "Set `GAOS_REDIS_ADDR` … and restart". §4.3 makes that variable a fixed compose value, deliberately "not from the deploy shell".
- Recommendation: Point the message at the docs and at the compose service rather than at a shell variable.

### It is unstated whether an injected AI-manager client is wrapped - Severity: Nit
- Affected scope: §7.2; TC-892
- Finding: `AIMClient.client()` returns `c.HTTP` unchanged when it is set (`aimanager.go:132-134`). The design says `client()` "wraps per call" but does not say whether an injected client is wrapped. No production code sets `HTTP` today, so this only affects tests.
- Recommendation: State that only the fallback branch wraps, and that an injected client is used as-is.

### The `github-write` label comes from the config function, not the constructor - Severity: Nit
- Affected scope: §7.1; TC-892
- Finding: `ghWriteConfigFromEnv` sets the label (`githubwrite.go:135-143`), not `newGHWriter` (`:162-168`). A test that calls `newGHWriter(ghConfig{…})` directly would get `github`.
- Recommendation: Have TC-892 build the writer config through `ghWriteConfigFromEnv`, or move the label into `newGHWriter`. Say which.

### TC-890's timing assertion is likely to be flaky - Severity: Nit
- Affected scope: §15 TC-890
- Finding: The test requires 20 000 wrapped calls to finish within 2× the bare run plus 100 ms. Phases also run `go test -race` (§16), so the bound is fragile on a loaded CI runner.
- Recommendation: Assert that the recorder never blocks (for example, that every enqueue returns within a small fixed bound), rather than comparing wall-clock run times.

### The page renders URL paths, which the Integrations page deliberately never does - Severity: Nit
- Affected scope: §10.1 ("same audience as Integrations"); §7.4
- Finding: The nearest precedent never renders a URL path (`integrations.go:18-21`; `templates/backoffice.html:978-982`). The Traffic page shows templated outbound paths and hosts, including internal IP literals. The design does not acknowledge the difference.
- Recommendation: Add one sentence that says why paths are acceptable here (templating, same audience), so the security lens can weigh it.

### The live fragment's markup omits the out-of-band swap it needs - Severity: Nit
- Affected scope: §10.5 vs §10.1
- Finding: §10.1 renders `traffic-live` with `OOB = true`. The attribute list in §10.5 omits the `{{if .OOB}} hx-swap-oob="true"{{end}}` that the copied `prsLive` element carries (`templates/backoffice.html:1528`).
- Recommendation: Include it in the list.

### What the shared `other` bucket stores is undefined - Severity: Nit
- Affected scope: §8.5
- Finding: Beyond 4096 sources, new sources share an `other` bucket. The design does not say whether a record counted there keeps its real `src`, or whether `other`'s own 300-a-minute limit suppresses legitimate new sources during a spread-out flood.
- Recommendation: State both.

Counts: B=0 M=4 N=16
