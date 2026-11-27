# Goldfish readiness review: `docs/designs/traffic-monitor.md` (rev 1)

**Lens:** Tech Lead, zero-decision rule. **Reviewed against:** `main` at `fc0f9c3`, clean tree.

**Anchors I confirmed:**
- **Gate:** `auth.go:541-590`, including the `pat == ""` early return at `:544-546`, the back-office copy at `:586` and the bearer `mux.ServeHTTP(w, r)` at `:588`.
- **Routes:** `main.go:666-669` marks every `registerAPI` pattern bearer. `api_v1.go:429-456` holds the two Basic routes at `:444` and `:448`.
- **Source and auth:** `staging.go:276-316` (`jenkinsSource` and `normSource`), `staging.go:71-72`, `staging.go:404`, `api_v1.go:329-339`.
- **Views:** `urls.go:31-35` lists nine views. `urls_test.go:233-234` expects eleven page patterns. `readOnlyGET` is at `auth_test.go:259-292`.
- **Clients:** the transport sites at `qaclient.go:145`, `aimanager.go:135`, `braintown.go:292`, `braintown_consumer.go:321-328`, `github.go:179-180`, `wa.go:182`/`:205`, `taskmgr.go:233`. The only production type assertion is `arcade.go:62`; the test one is `taskmgr_test.go:409`. Nothing else calls `http.DefaultClient`, `http.Get` or `http.Post`.
- **Deploy scripts:** the `check-stacks.sh` helpers (`:24-30`) and port count (`:51`), and `rebuild-restart.sh:15`/`:26`.
- **Registers:** D-106 is the last row (`decision-log.html:2167`), BR-65 is at `brd.html:1310`, TC-883 at `qa-test-plan.html:974`, semver `0.68.0` at `toc.js:22`. The Go test count is 745 (`check-workspace.sh:113-119`), and the README figures are at `:143-145`.

**Sections that are solid:**
- §6 (the RESP client contract)
- §7.3 and §7.5 (what is read, and the error classes)
- §9 (non-blocking)
- §12 (records are not events)
- The arithmetic in §17: 7 new files plus 37 changed files (items 8–43, with item 33 counted as two) makes 44. Go tests go 745 + 16 = 761, with 720 + 16 = 736 in `dashboard/`. Cases go 883 + 17 = 900.

---

### The design's own Redis comment fails its own "no depends_on" check - Severity: Major
- Affected scope: §4.1 (the compose comment, "there is deliberately no depends_on in either direction"), §4.4 (row "no depends_on": `hasnt $PROD 'depends_on'`), §16 (every phase must leave `check-stacks.sh` green).
- Finding: `hasnt` is `grep -qF` over the whole file (`deploy/check-stacks.sh:25`), so it matches comments too. The YAML block that §4.1 tells the implementer to paste contains the string `depends_on` in its comment. T4's exit condition therefore fails as written. The implementer has to choose between rewording the comment and narrowing the assertion, for example to `^\s*depends_on:`. That is an undecided choice.
- Recommendation: Pick one in the design. Either change the assertion to `hasntre $PROD '^[[:space:]]*depends_on:'`, or reword the comment (for example "no start-order dependency"). Then state the exact line.

### The success check against conntrack fails by construction - Severity: Major
- Affected scope: §0 "Success" (the `conntrack -L` cross-check), §16 Deploy ("the conntrack cross-check of §0"), §8.1 and §19 (only `/v1/` is recorded).
- Finding: `conntrack -L -p tcp --orig-port-dst 18301` (`deploy/edge/README.md:187`) lists every TCP peer of the tunnel port, whatever the path. Two problems follow:
  - **The edge itself, `10.10.0.1`**, carries every browser, docs and landing request. The page shows those calls under the callers' public `X-Real-IP` addresses, never as a `direct` row for `10.10.0.1`.
  - **Any direct peer that only calls `/api/healthz`, `/docs/`, `/app/` or the back office** never appears on the page, because §8.1 and §19 record `/v1/` only. The edge's own health curl (`README.md:205`) and the deploy verification curls are examples.

  So the acceptance check as worded ("names no peer that the page does not show as `direct`") cannot pass. Its result also depends on who happened to call non-API paths. Relatedly, the firewall step closed the port for every path, so the claim that visibility replaces it (§13.7, D-107) covers less than the step did.
- Recommendation: Restate the check precisely. Exclude `10.10.0.1`, and compare only peers whose conntrack entries coincide with `/v1/` calls. Or say plainly that a non-API direct peer is invisible and the operator must reconcile it by hand. Carry that limit into D-107's residuals and the edge README note (item 32).

### TaskMgr failures are recorded as successes - Severity: Major
- Affected scope: §7.1 (`taskmgr`), §7.3 ("recorded once, at its headers"), §10.2 (the Failed column and the *Outbound failed* tile).
- Finding: TaskMgr is gRPC over HTTP/2. The HTTP status is 200 and the real outcome is in the `grpc-status` trailer, which the client reads after the body (`taskmgr.go:345-349`). The recorder deliberately stops at the response headers. Every TaskMgr auth failure, not-found or invalid call therefore shows as `200` / ok. The page will report "0 failed" for a platform whose breaker is open on auth (`taskmgr.go:275-278`). The design does not mention this limit anywhere.
- Recommendation: Decide one of two options:
  - (a) State the limit on the page and in the docs, and leave TaskMgr's status column as "HTTP only".
  - (b) Wrap the body for `taskmgr` only and record at EOF from `resp.Trailer`.

  Either way, add the decision to §7.3 and a TC-889 case.

### The platform and source filters are undefined when the direction is "all" - Severity: Major
- Affected scope: §10.3 (the filter table: `platform` "applies to outbound records", `src` "applies to inbound").
- Finding: With `dir=all&platform=github`, one engineer will show the GitHub calls plus every inbound call, because the filter does not apply to inbound. Another will show only the GitHub calls, because a platform filter implies outbound. The same split applies to `src` with outbound records. TC-897's expected results depend on which is chosen, and "Filtered empty" (§10.4) does too.
- Recommendation: State the rule. For example: a set `platform` excludes inbound records and forces `dir=out`, and a set `src` excludes outbound records and forces `dir=in`, both reflected in the tab row. Or: setting one clears the other.

### `server.traffic` appears only in the change list - Severity: Nit
- Affected scope: §17 item 8 ("`server.traffic`") against §7.2 (a process-wide `var trafficRec atomic.Pointer[trafficRecorder]`).
- Finding: §7.2 makes the recorder a package global, which the gate and `trafficWrap` read. Item 8 also adds a `server.traffic` field that no other section defines (type, who sets it, who reads it).
- Recommendation: Either drop `server.traffic`, or define it (for example "a copy of the pointer for `buildTraffic`") and state which of the two the view reads.

### `AIMClient.client()` has two branches, and the design does not say which one is wrapped - Severity: Nit
- Affected scope: §7.2, §17 item 13.
- Finding: `aimanager.go:131-136` returns `c.HTTP` when it is set, and a new client otherwise. "Wrap in `client()`" leaves open whether an injected `c.HTTP` is wrapped. Production does not set it today (`modelconfig.go:201`), but TC-892's "with recording on … `AIMClient.client`" depends on the answer.
- Recommendation: State "only the default branch is wrapped; an injected `HTTP` is returned as is" (or the opposite).

### The `github-write` label depends on how the config was built - Severity: Nit
- Affected scope: §7.1 "`github` versus `github-write`", TC-892.
- Finding: The label is set in `ghWriteConfigFromEnv` (`githubwrite.go:135`), not in `newGHWriter` (`:162`). A `newGHWriter(ghConfig{token: …})` call, which is how a test builds one, is therefore labelled `github`, while TC-892 expects `github-write` from `newGHWriter`. Setting `cfg.platform = "github-write"` inside `newGHWriter` before `newGHClient(cfg)` changes no signature either.
- Recommendation: Set the label in `newGHWriter`, or make TC-892 build the writer from `ghWriteConfigFromEnv` with a mode-0600 token file, and say which.

### A handler that writes nothing gets inbound status 0 - Severity: Nit
- Affected scope: §8.6.
- Finding: `trafficWriter` records "the first `WriteHeader` status (200 if `Write` comes first)". When a handler returns without calling either, net/http sends 200, but the record would carry `s = 0`, which §10.2 counts as Failed. A handler panic would also skip `rec.in`.
- Recommendation: Add "neither called → 200", and say whether `rec.in` runs in a `defer`.

### TC-725's new text is stated wrongly - Severity: Nit
- Affected scope: §14, QA bullet ("TC-725's row and prose say twelve page routes and ten views").
- Finding: The row today reads "Each of the ten page routes … there are eleven page patterns" (`qa-test-plan.html:816`). In that wording, "page routes" means views plus the product section, and "patterns" adds the redirect. After the change it should read eleven page routes and twelve page patterns. The design's wording conflates the two counts.
- Recommendation: Quote the exact new row text.

### One count mirror and one service list are missing from the change list - Severity: Nit
- Affected scope: §17.
- Finding:
  - `pustaka/docs/assets/toc/product.js:36` says "with 64 business rules". It is already stale against BR-65, and BR-66 leaves it two behind. §14 updates the BRD count but not this mirror.
  - The deploy agent's definition under `.claude/agents/` (`gaos-deploy.md:38`, found by grep, not opened) enumerates the valid `service` values for `rebuild-restart.sh`, and `redis` is not added to it.
- Recommendation: Add both files (or state why each is left alone), and update the 44-file arithmetic.

### Line anchors have drifted on `main` at fc0f9c3 - Severity: Nit
- Affected scope: §10.1, §17 item 8, the "Covers" column of §7.1.
- Finding:
  - The `GET /ui/bo/{view}` route is at `main.go:605`, not `:604`. `GET /ui/bo/prs/live` is at `:608`, not `:607`.
  - The `.Do` call sites are `braintown.go:566` (not `:559`), `braintown_consumer.go:422` (not `:413`), `taskmgr.go:339` (not `:332`), and `wa.go:291`/`:374` (not `:286`/`:368`).
  - `wa.go`'s `Transport:` line is `:182`; item 18 says `:180-181`.
- Recommendation: Re-anchor these lines.

### TC-900's negative half has no stated way to run - Severity: Nit
- Affected scope: §4.4, §15 TC-900 ("each prints `FAIL` … when its condition is broken").
- Finding: `check-stacks.sh` has no test harness. Nothing says how each of the nine negative cases is exercised (for example a temporary copy of the compose file with one line broken), so the "negative" kind has no stated verification.
- Recommendation: Name the procedure: who breaks what, and where the result is recorded.

### A plain `restart` now wipes the traffic store - Severity: Nit
- Affected scope: §4.1 (Persistence row), §4.5.
- Finding: `rebuild-restart.sh <env> restart` with no service runs `dc restart` on every service (`deploy/rebuild-restart.sh:368`). Once Redis is in the stack, that empties both streams. The design only discusses the dashboard redeploy, which correctly leaves Redis alone.
- Recommendation: Add one line to §4.1 and to the docs page saying that a whole-stack `restart` clears the trace.

Counts: B=0 M=4 N=9
