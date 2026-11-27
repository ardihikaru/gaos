# Goldfish review: `docs/designs/traffic-monitor.md` (rev 2), critic lens

Design: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/docs/designs/traffic-monitor.md`
Round-1 report checked against: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/docs/designs/reviews/traffic-monitor-critic-1.md` (plus the §21 triage of the other four reports)

**Paraphrase.** GAOS adds a Redis container. It has no disk and no port, sits on its own internal network at a pinned address, and is locked down by an ACL. It holds eleven capped streams. A recording `RoundTripper` on the eight external clients writes one stream per platform. A recorder in `server.gate` writes three inbound streams:
- `in:direct`: every request on any route that did not arrive through the edge;
- `in:auth`: API calls through the edge where a principal authenticated;
- `in:anon`: API calls through the edge with no principal.

`via` comes from the rightmost `X-Forwarded-For` hop. A back-office **Traffic** page reads the streams. D-107 makes reviewing that page the required step and moves the D-106 host firewall to "recommended hardening".

**The strongest case against it.** Rev 2 is honest about its residuals. But the required step it creates is still hard to carry out:
- It has no cadence.
- Its evidence stream, `in:direct`, can be filled by the legitimate direct callers the edge README already names.
- For any account that is not an auto-fix admin, every tunnel peer is masked to the same `/24`.
- Its oldest-record marker claims 48 hours after a store restart that emptied it.
- The store itself can fail to start after a reboot, and no deploy check would notice.

Each of these is cheap to fix, but together they decide whether "reviewing the page" means anything.

**Round-1 status.** The round-1 Blocker (direct callers of non-API routes were invisible) is resolved in principle by §8.1. One gap remains: the gate's own refusals (Major 1 below). The four round-1 Majors are resolved:
- per-platform and per-kind streams (§5.1);
- `hasntre` on the key only (§4.4);
- `via` from the hop that delivered the request, with A6 checked in T2 (§8.3);
- the success check restated with E1–E3 (§0).

Round-1 SEC-1 (High) is resolved for edge floods (§5.1, §8.5).

**Confirmed in the repository:**
- D-106 is the last decision row (`pustaka/docs/product/decision-log.html:2167`), and ":18" says "One hundred and five entries".
- BR-65 is at `brd.html:1310` and ":1218" says "Sixty-five rules".
- TC-883 is the highest row (`qa-test-plan.html:974`).
- semver is 0.68.0 (`toc.js:22`; `README.md:12`, `:143-145`).
- 720 `func Test` in `dashboard/`, 66 event types in `events.go`.
- The client inventory is complete: every non-test `.Do(` maps to §7.1 or Arcade (`aimanager.go:158`, `aimanager_admin.go:63`, `braintown.go:566`, `braintown_consumer.go:422`, `github.go:363`, `qaclient.go:227`, `taskmgr.go:339`, `wa.go:291,374`), and the J3 unit poller makes no HTTP call of its own.
- `gate` is at `auth.go:541-590`; the bearer registration is at `main.go:666-669`; `apiGate`'s `authorize` is at `api_v1.go:329`.
- `jenkinsGate`'s `a.ok` is at `staging.go:404`; the route constants are at `staging.go:71-72`.
- The trust list is `traefik.yml:52-53`; `check-stacks.sh:51` and `:98` work as described.
- TC-725's text and the count of eleven are at `qa-test-plan.html:816` and `urls_test.go:233-234`.
- TC-019 is at `qa-test-plan.html:1080-1081`.
- The `prs-live` attribute string at `backoffice.html:1528` matches §10.5.
- The arithmetic holds: 7 + 38 = 45 files; 745 + 17 = 762; 883 + 18 = 901.

---

### The gate's own refusals may not be recorded, and they are most of what a direct prober produces - Severity: Major
- Affected scope: §8.1 (last paragraph), §0 cross-check, TC-893
- Finding and rationale: §8.1 says the gate wraps `w`, "passes the tagged copy `r2` … to `mux.ServeHTTP` (for the back-office class, after its session value is added as today, `:586`)", and records in a `defer`. But `gate` answers five cases itself, before or instead of calling `mux.ServeHTTP`:
  - the unmatched early return (`auth.go:544-546`);
  - the cross-origin 403 (`:564-566`);
  - `requireBO` (`:571-573`);
  - `signedOutPage` (`:579-581`);
  - the JSON 401 (`:583-584`).

  A tunnel peer probing the back office directly is signed out by definition, so almost all of its requests end in one of these. If the wrap and the `defer` are built at the `mux.ServeHTTP` call, as the sentence reads, none of them is recorded, and the cross-check of §0 fails on exactly the peers it exists to catch. Two implementers could reasonably build it either way (README rule 6). TC-893's "a direct back-office request" does not say signed in or signed out.
- Recommendation: State that `src`, `via`, the tag, the `trafficWriter` wrap and the `defer` are set up at the top of `gate`, before the first return, so every exit path is recorded. Add TC-893 cases: a direct signed-out `GET /inbox` gives a reduced record with `s = 401`; a direct HX request gives the `requireBO` status; a direct cross-origin POST gives `s = 403`.

### Legitimate direct callers share `in:direct` with the unknown peers it exists to catch - Severity: Major
- Affected scope: §5.1 (stream table, volume estimate "inbound `auth`: the QA tool's calls and Jenkins posts"), §8.1, §10.2 tile "Direct sources", §20 A4
- Finding and rationale:
  - The repository says the QA tool is on the tunnel. `GAOS_QA_URL` "is a tunnel IP" (`deploy/check-stacks.sh:244-245`; `compose.prod.yaml:146-149`). The edge README names "a QA backend that calls `http://<vm-tunnel-ip>:18301/api/v1`" as an expected direct caller (`deploy/edge/README.md:198-200`).
  - The volume estimate assumes the QA tool's calls arrive through the edge and land in `in:auth`. If they are direct, every QA call lands in `in:direct`, which has a cap of 2 000. At A4's "seconds to minutes apart", that cap evicts the one stray tunnel peer within hours. This is the round-1 eviction defect again, moved into the stream that was meant to be protected (§5.1: "never a direct caller").
  - The "Direct sources" tile turns `warn` permanently, which trains the operator to ignore it.
  - A4 states only a cadence. It does not say which path each known machine client takes.
- Recommendation: Add an assumption (A8, checked in T4 from conntrack and the Traefik log) naming which known clients (QA tool, Jenkins) call directly. Then either:
  - split `in:direct` by principal (`in:direct:auth` and `in:direct:other`, so authenticated direct traffic cannot evict unauthenticated direct traffic); or
  - keep a small per-source summary of direct sources (first seen, last seen, count, last class) that eviction does not touch.

  Base the tile on unauthenticated direct sources only.

### Masking collapses every tunnel peer into one source for non-admins, so the required review cannot be done - Severity: Major
- Affected scope: §10.1 ("every other account sees a source as its /24"), §10.2 items 3 and 5, §0 cross-check, D-107 text in §14
- Finding and rationale:
  - All tunnel peers are `10.10.0.x` (`traefik.yml:32-35`; `edge/README.md:181`). A non-admin therefore sees every direct peer as `10.10.0.0/24`.
  - "Direct sources", which counts distinct sources as the viewer sees them, shows 1.
  - The cross-check ("every conntrack peer … appears on the page as a `direct` source") cannot be performed.
  - Full addresses are shown only to `autoFixAdmin` (`prview.go:502-505`), a list in a separate file (`compose.prod.yaml:199`) that can be empty.
  - D-107 makes "reviewing that page" the required step, with no condition on who reviews it.
  - The privacy reason (SEC-9) applies to internet clients, not to the operator's own tunnel and VM addresses.
- Recommendation: Mask only public addresses. Show loopback, private, CGNAT and link-local sources in full to every signed-in account. Alternatively, state in D-107 and the README that the review and the cross-check are done by an auto-fix admin, and add "the reviewing operator is on the admin list" to the deploy note. Add a TC-900 case for a private source seen by a non-admin.

### "Kept since … 48 h" claims coverage the store may not have - Severity: Major
- Affected scope: §10.2 item 4 ("Kept since"), item 6, §0 exclusion E3, §4.1 Persistence row
- Finding and rationale:
  - The rule is: "'48 h' when that is the window's start, else the time, marked *cap*". The oldest record is almost never exactly at the window's start, so this is undecided: two engineers will produce different rules.
  - The natural reading (below cap, show "48 h") is wrong after any event that empties Redis while the dashboard runs: a Redis restart, an OOM kill, a host reboot, a whole-stack `restart` (`rebuild-restart.sh:368`). A stream holding three hours of records then claims 48.
  - E3 says "a peer seen before the oldest kept record is explained by it". With "48 h" displayed, the operator believes there is a gap in the evidence where there is really a gap in the store.
  - The page has no signal for "the store was emptied": the counters are dashboard-process counters (§9.3), and the ACL forbids `INFO`.
- Recommendation: Always show the oldest kept record's time per stream, and add *cap* when the kept count is at or above the cap. Have the writer record the time of its first successful write after any reconnect. Show "the store may have been emptied at <time>" when that time is later than the dashboard's start. Add the cases to TC-897.

### A pinned address with dynamic neighbours can stop Redis from starting, and no deploy check would notice - Severity: Major
- Affected scope: §4.1 (`ipv4_address: 172.30.14.2`), §4.2 (`/29`, no `ip_range`), §4.5 ("Redis is not on the health path"), §16 Deploy
- Finding and rationale:
  - The dashboard joins `gaos-prod-traffic` with a dynamic address, and there is deliberately no start order (§4.1).
  - Docker's IPAM hands out dynamic addresses from the bottom of the subnet. If the dashboard's endpoint is created first, on first `up`, after `down`/`up`, or when the daemon restarts containers after a reboot, it can take `.2`. Redis then fails with "address already in use".
  - `rebuild-restart.sh` verifies only the bind address, `healthz` and the routes (`:380-437`). The deploy passes, the page says "Unavailable", and nothing tells anyone.
- Recommendation: Add `ip_range: 172.30.14.4/30` under the network's `ipam.config` (dynamic addresses can then never be `.2`), or give the dashboard a static `172.30.14.3`. Assert it in the §4.4 block. Add one line to the verification in `rebuild-restart.sh`: `dc ps redis` shows `healthy`, a warning and not a failure, so Redis stays off the critical path.

### "Reviewing the page is the required step" has no cadence and no record, so nothing enforces it - Severity: Major
- Affected scope: §14 D-107 and the D-106 amendment, §17 item 32, §16 Deploy (one cross-check after 24 h), §21 critic row 2 ("Per-hour counters not adopted")
- Finding and rationale:
  - The firewall step it replaces had a recorded, mandatory check (`edge/README.md:202-205`). The new required step has neither a cadence nor a place where its being done is written down. There is one cross-check, once, at deploy time.
  - The evidence lasts at most 48 h, and less on busy streams (§5.1). A review every few days sees only the last hours.
  - The operator said "periodic monitoring" (§0). The design does not turn that into a period that fits inside the retention.
  - As written, the rule cannot be checked anywhere, so "required" means nothing.
- Recommendation: State the cadence in D-107 and the README, at most the shortest "kept since" of `in:direct`. Say where each review is recorded, for example the deploy note or an operator log. Alternatively, back the review with the direct-source summary proposed above, so that a missed day loses detail but not the fact that an unknown peer called.

### The `fail` and `warn` tile tones do not exist - Severity: Nit
- Affected scope: §10.2 item 3, §11 rules 3–4
- Finding and rationale: Tiles render `af-tone-{{.Tone}}` (`backoffice.html:1338`), and the CSS defines only `run`, `ask`, `done` and `block` (`static/app.css:1790-1793`). Tone `fail` or `warn` therefore either renders uncoloured or needs a new rule, which §11 ("No new CSS class", "Only existing … tile tones") forbids. The chip lines cited at `:2000-2002` also leave out `st-bad` at `:2003`.
- Recommendation: Use `block` for "Outbound failed" and `ask` (or `block`) for "Direct sources", or name the new tone rule in the change list. Fix the anchor to `:2000-2003`.

### TC-901 says ten lines in one place and eleven results in another - Severity: Nit
- Affected scope: §4.4 (last paragraph: "the eleven results"), §15 TC-901 ("the ten `ok` lines")
- Finding and rationale: The block prints ten lines, while the negative procedure says eleven results. Several lines join more than one condition, so the number of hand-broken conditions is neither ten nor eleven.
- Recommendation: Say "each of the N conditions in the Assertion column", give N, and use it in both places.

### "On the internal traffic network only" asserts presence, not exclusivity - Severity: Nit
- Affected scope: §4.4 row "traffic network"
- Finding and rationale: `inblock redis 'traffic: {ipv4_address: 172.30.14.2}'` still passes if `internal: {}` is added under Redis's `networks:`, which would put Redis beside Traefik, landing and docs.
- Recommendation: Add `notinblockre redis '^[[:space:]]+internal:'`, or assert that the Redis block holds exactly one network key.

### `GAOS_TUNNEL_IP`'s /24 adds nothing in this deployment - Severity: Nit
- Affected scope: §4.3, §8.3 fallback, §4.4 dashboard row, TC-886, TC-893
- Finding and rationale:
  - The tunnel is `10.10.0.0/24` (`traefik.yml:32`), which is already inside 10/8, so "private" catches it first.
  - The /24 also contains the edge, `10.10.0.1`, so the fallback would class the edge as `direct`. Harmless only because the fallback never runs behind Traefik.
  - The setting costs one compose line, one check-stacks assertion and two test cases for no change in behaviour.
- Recommendation: Drop `GAOS_TUNNEL_IP` from the dashboard and keep loopback, private, link-local, CGNAT and `unknown` as the fallback. Or keep it and state the one deployment it serves (a tunnel outside private space).

### BR-66 sits in the QA sync section - Severity: Nit
- Affected scope: §14 BR-66 ("§9.8 beside BR-52 and BR-55")
- Finding and rationale: §9.8 is "The QA catalog and the QA sync API", and its introduction bounds "what a machine caller may write" (`brd.html:1340-1347`). A trace rule about outbound calls and source addresses does not belong under that introduction.
- Recommendation: Put it in §9.1 beside BR-01/BR-05, which §12 argues against, or in §9.5 "Commercial, execution and data handling".

### TC-890 and TC-895 need a writer stall the design does not provide - Severity: Nit
- Affected scope: §15 TC-890 ("`dropped` rises by exactly 1000"), TC-895 ("with the writer stalled")
- Finding and rationale: The writer takes up to 128 records off the queue before it blocks on a store that never answers (§9.2). During backoff it keeps draining and counting drops. The drop count from "4096 + 1000" therefore varies between runs, and these tests run in the deploy gate (`rebuild-restart.sh:348`).
- Recommendation: Specify a test hook (the writer not started, or a gate channel) so the queue is the only consumer. Assert "exactly 1000" only under that hook.

### E1 names `/` and `/docs/`, but Traefik routes far more away from the dashboard - Severity: Nit
- Affected scope: §0 exclusion table E1, §13.7
- Finding and rationale: Everything outside `/app` and `/api` goes to the landing container (`deploy/traefik/dynamic.yml:51-55`), including scanner paths such as `/.env` and `/wp-login.php`. The `/app` and `/docs` redirects are answered by Traefik itself (`:78-93`). A peer that only probed those will be counted as unexplained under E1 as worded.
- Recommendation: E1 should say "a peer whose requests were all outside `/app/` and `/api/`, or the two slash redirects".

### `conntrack -L` is a point-in-time listing, not a sample over a period - Severity: Nit
- Affected scope: §0 cross-check ("over the same period the page covers"), §16 Deploy
- Finding and rationale: `conntrack -L` lists the current table only. A closed HTTP connection leaves it within minutes, so a single listing cannot cover 24 h.
- Recommendation: Specify `conntrack -E -p tcp --orig-port-dst 18301` logged to a file for the period, or repeated sampling at a stated interval, and compare against the page at the end.

### The ACL is described as "three commands" while it admits five - Severity: Nit
- Affected scope: §2 Stack row, §14 D-107 residual ("its ACL admits three commands on the traffic keys")
- Finding and rationale: §4.1 admits `ping`, `xadd`, `xtrim`, `xrevrange` and `acl|list`. Register text that states a security posture should be exact.
- Recommendation: Say "three data commands on the traffic keys, plus `PING` and `ACL LIST`" in §2 and in D-107.

### TaskMgr "Failed" disagrees with the client's own idea of failure - Severity: Nit
- Affected scope: §7.6, §10.2 item 4 "Failed"
- Finding and rationale:
  - The page counts any `g` other than `0` and `-1` as Failed, but the client treats `5` (NotFound) and `3` (Invalid) as healthy answers (`taskmgr.go:273`). Routine lookups of unknown codes will show as failures.
  - In the other direction, an HTTP 200 whose body exceeds `tmMaxAnswer` is unavailable to the client (`:345-346`) but records `s = 200, g = -1`, which the page shows as OK.
- Recommendation: Count as Failed the codes the client treats as failures: `g ∉ {0, 3, 5, -1}`, or `-1` with `s = 200` when `do` returned `errTMUnavailable`. Show the raw code either way.

### Small anchor drift - Severity: Nit
- Affected scope: §0 fact 6, §11 rule 4
- Finding and rationale: §0 cites `staging.go:270-297` for `jenkinsSource`, while §1 and §8.3 cite `:276-297`; the function starts at `:276`. The `app.css:2000-2002` issue is noted above.
- Recommendation: Use `staging.go:276-297` throughout.

### One triage row is a partial decline recorded as PROPOSE FIX - Severity: Nit
- Affected scope: §21 Critic table, row 2
- Finding and rationale: The note says per-hour counters were "not adopted", which is a decline of part of the recommendation. The rubric requires exactly one disposition per finding (`docs/rules/gaos-design-review.md:74-81`).
- Recommendation: Record it as PROPOSE FIX for the streams and "Kept since", and write the declined part out explicitly with its reason, or split the row.

---

Solid, one line each:
- §6, the bounded RESP parser contract, is complete.
- §7.3, what the outbound recorder reads, and §7.5, error classes with no error text, are strict and correct.
- §7.4's `{base}` handling closes SEC-6.
- §12 argues BR-01/02/04/05 correctly.
- §17's arithmetic and the register identifiers check out against the repository.

Not verified, because the files are off limits to this review: the `.claude/` anchors in §4.5, §11, §15, §16 and §17 items 43–44.

Counts: B=0 M=6 N=12
