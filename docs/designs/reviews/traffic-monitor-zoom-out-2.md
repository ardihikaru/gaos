# Design review: traffic-monitor.md, rev 2 (Skeptical Architect lens)

**Overall:** rev 2 fixes all ten round-1 zoom-out findings. Specifically, it now:
- quotes the operator's firewall decision word for word;
- uses one stream per platform outbound;
- splits inbound into `in:anon`, `in:direct` and `in:auth` instead of using the per-source limiter;
- uses three RESP commands;
- has no `v` field;
- names the helpers it reuses;
- keeps one recorder handle;
- makes the caps and window constants.

The Redis stream with last-N retention is what the operator asked for, and nothing below argues against it. No Blockers remain. Two Majors are left: the caps are not sized against the stated volume, and the `via` fallback fails open and carries a setting it does not need.

**Checked and solid:**
- **File count.** 7 new files. The changed items run from 8 to 44, which is 37 items. Item 33 is two files, so 38 changed files. 7 + 38 = **45**, which matches §17.
- **Go test count.**
  - `dashboard/` has 720 `^func Test` today, across 96 files (confirmed by search).
  - `deploy/check-workspace.sh:114-115` pins 745 in total.
  - Adding the 17 new tests (TC-884 to TC-900) gives **737** and **762**.
- **Register count.** `check-workspace.sh:129` pins 883 cases. 883 + 18 (17 Go tests and TC-901) = **901**. All three numbers in the design are correct.
- **Reuse.**
  - `jenkinsSource` and `normSource` (`dashboard/staging.go:276-316`).
  - `prViewerIsAdmin` and `autoFixAdmin` (`dashboard/prview.go:502-505`, `dashboard/handlers_autofix.go:18`).
  - `agoAt` (`dashboard/integrations.go:62`).
  - `noStoreView` (`dashboard/integrations.go:27-31`).
  - The `has`, `hasnt`, `hasre` and `hasntre` helpers (`deploy/check-stacks.sh:24-30`).
  - All of these exist and do what the design says.
  - The only `ResponseWriter` wrapper in the code is `gzWriter` (`dashboard/render.go:256-265`), and it is not a general recorder. So `trafficWriter` is new code with good reason. `gzWriter` is also the warning case: its missing `Flush` once broke the stream (`render.go:269-272`). §8.6 handles `Flush` and `Unwrap` explicitly because of that.
- **No container healthcheck adds noise to `in:direct`.** `deploy/Dockerfile.dashboard:23-25` has no `HEALTHCHECK`, and `compose.prod.yaml` has none for the dashboard either. So the "Direct sources" tile is not warning all the time because of the deploy's own probes.
- **Alternatives (§18).** Alternative 2 (an in-memory buffer snapshotted to disk) is now stated in its strongest form and rejected for fair reasons.

---

### The caps are sized the same for every stream, although §5.1's own estimate shows the platform the operator named lasts only about 6 jobs - Severity: Major
- **Affected scope:** §5.1 (caps table and volume estimate), §4.1 "Memory" row, §10.2 "Kept since".
- **Simplicity rationale:**
  - The operator asked for this page "for periodic monitoring" of traffic "outbound to QA tool" (§0).
  - The design's own estimate says the `qa-tool` stream of 1 000 holds about 6 jobs, and `ai-manager` about 3 long turns. On a busy day that is hours, not the 48 h the page header advertises.
  - Meanwhile §4.1 says the total of 13 000 records needs at most 8 MB inside a 64 MB limit. That leaves about 56 MB of headroom, and the design leaves it unused.
  - So the "Kept since … *cap*" marker tells the truth, but the stream the operator named is the one cut shortest. The design makes the trade-off and never says why 1 000 was chosen.
- **Recommendation:**
  - Derive each cap from the estimate table, so every stream covers at least one review period (24 h, say).
  - For example: `qa-tool` and `ai-manager` 5 000 each, `github` 3 000, the others 1 000. That gives about 21 000 records, which is ≤ 13 MB at 600 bytes each, still under 64 MB.
  - The parser bound of 50 000 array elements (§6) and the 15 s cache still hold at those sizes.
  - Write the derivation down in §5.1, so the cap can be checked against the purpose. No new mechanism is needed; these are constant values.

### The `via` fallback fails open and keeps `GAOS_TUNNEL_IP` alive just for itself - Severity: Major
- **Affected scope:** §8.3 "Fallback", §4.3 (`GAOS_TUNNEL_IP` row and compose line), §4.4 "dashboard" assertion, TC-886, TC-893 (the two fallback cases).
- **Simplicity rationale:**
  - §8.3 says Traefik always appends the delivering hop to `X-Forwarded-For`. So a request with no `X-Forwarded-For` did not pass through Traefik, and therefore did not come through the edge. By definition it is `direct`.
  - The fallback instead guesses from the address class: loopback, private, CGNAT or the tunnel /24 count as direct, and anything else counts as **`edge`**.
  - Under §8.1, `edge` on a non-API route means **no record at all**. So the one case the fallback exists for (a misconfiguration, §8.3) hides a public caller on exactly the routes the firewall cross-check needs.
  - The fallback also brings the `GAOS_TUNNEL_IP` pass-through, its validation and log line, a `check-stacks.sh` assertion and two test cases.
  - The tunnel /24 also contains the edge itself (`10.10.0.1`, `deploy/check-stacks.sh:98-105`). That shows the address-class rule is not the right test here.
- **Recommendation:**
  - When there is no `X-Forwarded-For`, set `via = direct`.
  - Drop `GAOS_TUNNEL_IP` from the dashboard's environment, §4.3, the §4.4 dashboard line, TC-886 and TC-893.
  - The fallback then fails closed (the request is recorded) and needs no setting.

### TaskMgr gets a special deferred mode in the generic transport when `do` is already its single choke point - Severity: Nit
- **Affected scope:** §7.6, §7.2 (the note on `TestTaskMgrConfig`), §17 item 19, TC-892 (the TaskMgr part).
- **Simplicity rationale:**
  - Every TaskMgr call goes through `taskmgrClient.do` (`dashboard/taskmgr.go:326-359`). It already has:
    - the start time;
    - the method (always `POST`);
    - `path`;
    - the raw `err` from `hc.Do` (`:339-341`);
    - `resp.StatusCode` (`:345`);
    - the gRPC code (`:348-351`).
  - Instead, §7.6 makes the shared `recordingTransport` read a context flag, build a record without sending it, and hand it back to `do` through a `defer`. That couples the generic transport to one client.
  - It also keeps the `TestTaskMgrConfig` type assertion (`taskmgr_test.go:409`) dependent on the wrap being a no-op.
- **Recommendation:**
  - Do not wrap TaskMgr's transport. Have `do` call `rec.out` once, in a `defer`, with `errClass(err)` and the code it read.
  - Same fields, same TC-889 assertions, no deferred mode in the transport, and `taskmgr.go:233` stays unchanged.

### Masking private and tunnel addresses to /24 hides the direct-peer list from non-admins without protecting anyone - Severity: Nit
- **Affected scope:** §10.1 "Who may view", §10.2 item 3 ("Direct sources" tile), §10.3 `src` filter, TC-900.
- **Simplicity rationale:**
  - Direct callers are tunnel peers or the VM itself, at private addresses such as `10.10.0.9` and `10.10.0.5`. A non-admin sees all of them as `10.10.0.0/24`. Since "the summary groups by what the viewer sees", the Direct sources tile shows 1 to a non-admin and 3 to an admin.
  - These are GAOS's own infrastructure addresses, not personal data. Masking them adds a viewer-dependent branch to the summaries and filters and protects nothing.
- **Recommendation:** Mask only public addresses: anything that is not loopback, private, link-local or in 100.64/10, using the same test `jenkinsSource` already applies (`staging.go:284`). Then every viewer sees the same direct-source list and count.

### `+acl|list` widens the ACL permanently for a one-time T4 readback - Severity: Nit
- **Affected scope:** §4.1 command line and "ACL" row, §4.4 ACL assertion, §13.4, §16 Deploy.
- **Simplicity rationale:**
  - `acl|list` has no caller in GAOS. Its only use is for T4 to read the ACL back once.
  - The deploy already proves the restriction by refusal: `redis-cli FLUSHALL → NOPERM`. And `check-stacks.sh` already asserts the exact ACL text in the compose file.
- **Recommendation:**
  - Drop `+acl|list`.
  - T4 records `FLUSHALL` and `CONFIG GET maxmemory`, both answering `NOPERM`, and `XADD gaos:other x 1 * a b`, which is refused because of the key pattern.
  - That proves the command limit and the key limit without granting a command nothing uses.

### Assumption A6 checks `X-Forwarded-For` but not `X-Real-IP`, which `src` for a direct peer depends on - Severity: Nit
- **Affected scope:** §8.3 (A6), §20 A6, §1 "Source".
- **Simplicity rationale:**
  - `src` is `jenkinsSource` unchanged. It takes `X-Real-IP` first. If that is absent, its `X-Forwarded-For` fallback **skips private hops** (`dashboard/staging.go:280-288`).
  - A direct tunnel peer is private (`10.10.0.x`). Without an `X-Real-IP` set by Traefik, it would fall through to `RemoteAddr`, which is Traefik's container. Every direct peer would then collapse into one address.
  - D-106 relies on Traefik setting `X-Real-IP` (`check-stacks.sh:96-97`), so this probably holds. But A6 is the test run for exactly this hop, and it records only one of the two headers.
- **Recommendation:** Have A6 record both `X-Real-IP` and `X-Forwarded-For` from a trusted peer and from an untrusted one.

### TC-901's recorded run says "eleven results" while the block has ten lines - Severity: Nit
- **Affected scope:** §4.4, last paragraph ("the eleven results") versus the §4.4 output block (10 `ok` lines) and the TC-901 row ("the ten `ok` lines").
- **Simplicity rationale:** The acceptance record of a hand-run negative check has to be countable. As written, it contradicts itself: if the eleventh result is the comment case from the critic triage, it is not listed.
- **Recommendation:** Either say "ten", or list the eleventh case (the `depends_on` comment) by name in §4.4.

---

The remaining parts are solid: §0 (the decision, now quoted), §6 (the client contract), §7.3–§7.5 (what is read and the error class), §9 (never blocking), §12 (not an event), §14 (registers) and §19 (non-goals). I found nothing to add on them.

Files relevant to this review:
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/docs/designs/traffic-monitor.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/dashboard/taskmgr.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/dashboard/staging.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/dashboard/render.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/deploy/check-stacks.sh
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-traffic/deploy/check-workspace.sh

Counts: B=0 M=2 N=5
