# Goldfish review: `docs/designs/traffic-monitor.md` (rev 1)

**Paraphrase.** GAOS adds a Redis container with no disk, reachable only from the dashboard. A recording `RoundTripper` on the eight external HTTP clients writes outbound calls to one capped stream, and a recorder in `server.gate` writes machine-API calls to another, with source address and an `edge`/`direct` flag. A new back-office **Traffic** page reads both streams. D-107 then makes the D-106 host-firewall step optional, on the grounds that the page shows who calls GAOS directly.

**The strongest case against it.** The inbound half is there to replace a firewall on the *whole tunnel port*, but it only records `/v1/` paths. The `via` flag is guessed from the address class, not from which hop delivered the request. And both streams are first-in-first-out per direction, so the noisiest producer pushes every quieter platform or caller out of the "48-hour" window. The outbound half is sound engineering. The inbound half is what justifies D-107, and it is the weakest part.

Facts confirmed in the repository:
- Registers: D-106 is the last decision (`pustaka/docs/product/decision-log.html:2167`), TC-883 the highest case (`qa-test-plan.html:974`), semver is `0.68.0` (`pustaka/docs/assets/toc.js:22`).
- 720 `func Test` in `dashboard/`.
- Eight external clients plus Arcade: every `.Do(` call site in non-test code maps to §7.1.
- Gate structure (`dashboard/auth.go:541-590`), `jenkinsSource` (`dashboard/staging.go:276-316`), trust list (`deploy/stacks/traefik.yml:52-53`), edge rate limits (`deploy/edge/gaos.conf:77-78`).
- Page-pattern count `11` (`dashboard/urls_test.go:233-234`), `readOnlyGET` (`dashboard/auth_test.go:259-292`).

---

### D-107 makes the firewall optional based on a page that cannot see most direct callers - Severity: Blocker
- Affected scope: §0 "The context", §8.1, §8.2, §13.7, §14 (D-107 text: "The firewall step becomes optional hardening"), §17 item 32 (the edge README text)
- Finding and rationale: The firewall step is about **the port**. `deploy/edge/README.md:177-182`: Traefik "is still *reachable* from every WireGuard peer on the VM's published port, `18301`". Its sampling step is `conntrack … --orig-port-dst 18301` (`:187`), which covers every path. The recorder only records bearer patterns and unmatched `/v1/` paths (§8.1). It says outright: "Nothing else is recorded: not the back office, not `/healthz`, not static files" (§8.1), and §19 excludes back-office traffic. So a tunnel peer that goes direct to `/app/auth/login`, `/app/...`, `/docs`, the landing page, or any non-`/v1/` path never appears. The proposed README text still says the page "shows every caller of the machine API … Apply the rules below only if a direct caller there should not exist". An operator reads that as "no direct caller on the page means no firewall needed". D-107 records that premise as a decision, and the mechanism cannot support it.
- Recommendation: Pick one.
  - (a) Record **every** request whose `via` is `direct`, whatever the route, storing `(non-api)` in place of the path, as §8.2 already does for unmatched paths. Keep API-only recording for `edge` calls.
  - (b) Drop the "firewall becomes optional" clause from D-107 and from the README, and describe the page as visibility into the machine API only.

  Either way, rewrite the D-107 context sentence and §13.7 to say exactly which part of the port the page covers.

### One stream per direction lets the noisiest producer push everything else out; "48 hours" does not hold - Severity: Major
- Affected scope: §0 "Success", §1 (Window, Cap), §5.1, §5.3, §8.5, §10.2 item 4, §13.5
- Finding and rationale:
  - **Outbound.** Excluding ArcadeDB "because it would fill the outbound cap within minutes and push every external platform out" (§7.1) applies to GAOS's own pollers as well:
    - The AI-manager adapter polls every 2 s, for up to 660 s per turn (`dashboard/adapter_aimanager.go:44-45, 245-251`). That is up to 330 records per turn, so about 15 turns fill the default cap of 5,000.
    - The QA job runner ticks every 15 s (`dashboard/qajobs.go:193`).
    - The inbox worker ticks every 10 s (`dashboard/inbox_worker.go:31`).
    - The fix-PR poller runs every 30 s (`dashboard/fixprpoll.go:23`).
    
    A WhatsApp send or a GitHub write from yesterday morning is pushed out by AI-manager polls. The platform table then shows "No calls in the window" for a platform that was called, which is a false statement.
  - **Inbound.** The per-source limit is 300 a minute. One unauthenticated scanner held just under it (5 r/s; the edge does not limit bearer routes, `deploy/edge/gaos.conf:77`) replaces the whole inbound cap in about 17 minutes (about 67 minutes at the 20,000 maximum). It wipes the evidence of the direct callers the page exists to show. §13.5 says the flood is "bounded twice", but the bound limits memory, not what gets pushed out.
  
  The success sentence ("for the retained window (48 hours by default), sees …") is therefore not achievable under normal load. There is also no volume estimate anywhere in the design.
- Recommendation:
  - Key outbound streams per platform (`gaos:traffic:out:<label>`, eight keys, each with its own `MAXLEN`).
  - Key inbound streams by `via` at least, so `direct` records cannot be pushed out by internet traffic through the edge.
  - Keep summaries that survive trimming, for example a per-platform, per-hour `HINCRBY` hash with `EXPIRE` equal to the window, or in-process counters.
  - Have the page say what the data really covers ("oldest record shown: <time>") rather than "the last 48 hours".
  - Add a volume estimate from the cadences above, and size the default cap from it.

### `check-stacks.sh` would fail on the design's own compose comment and block every deploy - Severity: Major
- Affected scope: §4.1 (the compose comment), §4.4 (assertion "no depends_on")
- Finding and rationale: The redis block's comment says "there is deliberately no depends_on in either direction" (design §4.1, the YAML comment). The assertion is `hasnt $PROD 'depends_on'`. `hasnt` is `grep -qF` over the whole file (`deploy/check-stacks.sh:25`), so it matches the comment and prints `FAIL`. `rebuild-restart.sh` runs `check-stacks.sh` as pre-flight and exits on any failure (`deploy/rebuild-restart.sh:272-277`), so no prod deploy or restart could run.
- Recommendation: Either reword the comment so it does not contain the literal `depends_on`, or make the assertion `hasntre $PROD '^[[:space:]]+depends_on:'`. Say which one, and add the comment case to TC-900's negative cases.

### `via` is guessed from the address class, not taken from the hop that delivered the request - Severity: Major
- Affected scope: §8.3, §1 (Via), §10.2 tile "Direct sources", §20
- Finding and rationale: §8.3 asserts "The edge is a public host whose clients are on the internet, so a call that came through it carries a public address". This is not anchored and not in §20's assumptions.
  - A caller that reaches the edge from a private address (a host on the edge's LAN, or a tunnel host routed to the edge's address) gets `X-Real-IP = $remote_addr` (`deploy/edge/gaos.conf:126`). That address is private, so the call is shown as `direct` even though it came through the edge.
  - The page's main question, "which of these did not come through the edge", is answered by inference when a deterministic signal is available. The dashboard's peer is always Traefik. Traefik keeps the forwarded chain only from `10.10.0.1/32` and appends its own peer, so the rightmost `X-Forwarded-For` hop *is* the hop that delivered the request. `via = edge` exactly when that hop is `10.10.0.1`. (Confirm in T2 that Traefik v3.3 appends the peer as described.)
- Recommendation: Derive `via` from the delivering hop (the rightmost XFF entry compared with the trusted edge address, kept beside `traefik.yml`'s single entry, which `check-stacks.sh:98` already asserts). Keep the address class as a fallback only. Add the tested behaviour to TC-893, and list whatever remains as an assumption in §20.

### The success criterion fails as written - Severity: Major
- Affected scope: §0 "Success", last sentence; §16 "Deploy" ("the conntrack cross-check of §0")
- Finding and rationale: "A `conntrack -L` sample of the tunnel port names no peer that the page does not show as `direct`" cannot pass, for three reasons:
  1. The busiest conntrack peer is the edge itself, `10.10.0.1`. The page never shows it as a source, because it shows the client address from `X-Real-IP`.
  2. Every peer hitting non-API paths is absent by design (Blocker above).
  3. The deploy's own health probe is `ssh clino curl …/api/healthz` (`deploy/rebuild-restart.sh:399`; `deploy/edge/README.md:205`). That puts the edge on `/healthz`, which is excluded.

  A required success metric that fails on day one either gets ignored or gets "passed" by reading it loosely.
- Recommendation: Restate it, for example: "every conntrack peer other than `10.10.0.1` appears on the page as a `direct` source, or is explained by a recorded exclusion". Name the exclusions (`/healthz`, plus any path the fix to the Blocker still leaves out).

### `noeviction` mitigation claim is wrong when the count cap is the binding limit - Severity: Nit
- Affected scope: §4.1 table, "Eviction" row
- Finding and rationale: The row says that under OOM "`XTRIM` keeps freeing memory". `XTRIM MINID` frees only records older than the window. If memory fills while every record is inside the window, nothing is freed. `XADD` is refused, including its own `MAXLEN` trim, so recording stays off until records age out. The §4.1 arithmetic says this state cannot be reached, so the claim is unneeded rather than dangerous.
- Recommendation: Drop the claim, or add a fallback `XTRIM … MAXLEN ~ <cap/2>` when an OOM reply is seen.

### Anchors and boot-order wording - Severity: Nit
- Affected scope: §7.2, §10.1, §17 item 8
- Finding and rationale:
  - `GET /ui/bo/prs/live` is at `dashboard/main.go:608`; `:607` is the retarget POST.
  - "Before the first client is built (`newBrainTown`, `main.go:293`)" is wrong. `ArcadeFromEnv()` (`:198`) and `selectAdapter(...)` with `adapter.Smoke` (`:239-245`) come first.
  
  Setting `trafficRec` right after `:195-196` still precedes all of them, so the plan works and only the stated reason is wrong.
- Recommendation: Fix both anchors, and say "before `ArcadeFromEnv` (`main.go:198`)".

### The `traffic-live` attribute list leaves out OOB and the escaped `&&` - Severity: Nit
- Affected scope: §10.5
- Finding and rationale: The existing lists carry `{{if .OOB}} hx-swap-oob="true"{{end}}` and write `&amp;&amp;` inside the attribute (`dashboard/templates/backoffice.html:1326, 1528`). §10.5 claims "the attributes of the Pull requests list" but omits the first and writes a raw `&&`. Two implementers could produce different markup from it.
- Recommendation: Quote the attribute string exactly, including the OOB conditional.

### Leftover undecided or unused items - Severity: Nit
- Affected scope: §6 (`XLEN`), §14 (TC-431)
- Finding and rationale:
  - `XLEN` is in the client's command set and TC-884, but no flow in §5, §9 or §10 uses it.
  - TC-431 "changes only if it quotes the count" is left undecided, which breaks README rule 6.
- Recommendation: Remove `XLEN` or name its use. Check TC-431's prose now and state the edit, or state that there is none.

### The dev compose parity comment and the shared service list - Severity: Nit
- Affected scope: §4.3 (dev and local compose get no Redis), §4.5
- Finding and rationale:
  - `compose.prod.yaml:9-13` says dev differs "ONLY in" a fixed list, and that "a difference outside that list is a bug". Adding Redis to prod only contradicts that comment, and the change list does not edit it.
  - `SERVICES` is shared by both tiers (`deploy/rebuild-restart.sh:26, 82-85`). `dev restart redis` passes validation, then fails inside compose.
- Recommendation: Update the header comment in item 28. Either accept `redis` for prod only, or say why the dev failure is acceptable (dev is retired).

### The check-stacks network assertion needs an unspecified multi-line helper - Severity: Nit
- Affected scope: §4.4, row "traffic network only"
- Finding and rationale: "`name: gaos-prod-traffic` followed on the next line by `internal: true`" cannot be checked with the existing `has`/`hasre` helpers, which are single-line `grep` (`deploy/check-stacks.sh:24-30`). Only the block extractor is specified.
- Recommendation: Specify the second helper (for example `grep -A1 -F 'name: gaos-prod-traffic' | grep -qF 'internal: true'`) or extract the `traffic:` network block the same way as a service block.

### The wrapped writer loses `MaxBytesReader`'s close-after-reply - Severity: Nit
- Affected scope: §8.6, §0 "Success" ("every route behaves … exactly as before")
- Finding and rationale: `http.MaxBytesReader(w, …)` (`dashboard/api_v1.go:402`) signals the server through an unexported `requestTooLarge` method on the concrete writer. It does not follow `Unwrap`, and a wrapper outside `net/http` cannot implement an unexported method. Behind `trafficWriter`, a 413 no longer forces the connection closed. The effect is small, but it breaks the "exactly as before" claim.
- Recommendation: State this as a residual in §8.6, or wrap only after body decoding. TC-894 then asserts the 413 is unchanged.

### TC-890 is a wall-clock test with 20,000 HTTP calls, run under `-race` - Severity: Nit
- Affected scope: §15 TC-890, §16 (each phase runs `go test -race`)
- Finding and rationale: "Within 2× the bare run plus 100 ms" over 20,000 `httptest` round trips is slow under `-race` and likely to be flaky on a loaded CI host.
- Recommendation: Assert that `rec.out` does not block, with a store that never answers and a queue filled past capacity (counter-based), and use a few hundred calls for any timing comparison.

### Alternative 1 skips its strongest form - Severity: Nit
- Affected scope: §18.1
- Finding and rationale: The in-memory option is rejected because a redeploy empties it. A ring buffer saved to a mounted writable directory on SIGTERM and loaded at boot (the pattern `prod.bt-state` already uses, `compose.prod.yaml:219`) survives redeploys. It needs no new image, network, licence review, uid assumption, RESP parser or check-stacks block. It loses data on a crash, which §12 already accepts for monitoring data. The operator wrote "you *can* use redis stream", which permits Redis but does not require it.
- Recommendation: Add the snapshot variant and give a real reason against it, such as the operator's preference or SIGKILL loss, or adopt it.

---

Solid, in one line each:
- §7.3 (what the recorder reads) is complete and strict.
- §7.5 (error class without error text) is right.
- §12 (not an event) argues BR-01, BR-02, BR-04 and BR-05 correctly.
- §6 (bounded parser) is well specified.
- §17's arithmetic (7 + 37 = 44; 720 + 16 = 736; 883 + 17 = 900) checks out against the repository.

Not verified, because the sources are off limits to this review: the `.claude/` anchors in §11, §14 and §16.

Counts: B=1 M=4 N=9
