# Security review: release-b4-unit-tests, rev 15 (§0–§14, §26–§27)

Reviewer: gaos-goldfish-security, with no prior context. I read the design, `docs/rules/gaos-design-review.md`, `docs/designs/README.md`, and the code the design cites: `dashboard/staging.go`, `dashboard/api_v1.go`, `dashboard/main.go`, `dashboard/notify.go`, `dashboard/auth.go`, `deploy/edge/gaos.conf`, `deploy/stacks/traefik.yml`, `deploy/stacks/compose.prod.yaml`, `deploy/rebuild-restart.sh` and `.gitignore`. I opened nothing under `.claude/`, no credential file, and no file under `pustaka/` (so not the BRD, the decision log or the open-questions register).

## Paraphrase

### What this proposes

Each product gets a result source, `UnitChecks.Source`. It is either `github-actions` (the default, what v0.67.0 does today) or `jenkins`.

Under `jenkins`, GAOS reads nothing from GitHub's checks. Instead, the organisation's Jenkins posts each merge build's unit-test result to a new machine route, `POST /api/v1/products/{id}/unit-results`.

**Authentication.** The route uses the same mechanism as the staging callback, now shared through a function called `jenkinsGate`:
- HTTP Basic, one `productId:user:password` line per product, constant-time compare.
- A credential for another product answers 404.
- A failure lockout of 5 failures in 5 minutes.
- The route has its own credential file, `GAOS_UNIT_RESULTS_BASIC_AUTH_FILE`, and its own lock table. A credential for one route never opens the other.

**Lock key.** Rev 15 changes the lockout key for both routes. It becomes `X-Real-IP` (set by nginx), else the rightmost public `X-Forwarded-For` hop, else the peer. The product in the path is added to the key.

**Body.** At most 64 KiB, strict JSON. Test names and files go through the screening already used for GitHub annotations. Bad display-only fields are cut or emptied rather than refused.

**How a result is handled.** A result finishes the pending watch for that merge commit through the existing `FinishUnitWatch` path, so the defect, the hold for an auto-fix admin, the pass-close and the notices are the same as today. The edge cases:
- **No watch yet:** 409 `no-watch` with `Retry-After`. Jenkins retries for 10 minutes.
- **A watch already ended `no-ci`:** a result arriving within 24 hours is still accepted.
- **A second result:** the same result answers 200 (duplicate). A different one answers 409, writes a log line and sends a group notice.

**Attribution.** The caller's address, the posted result and a screened build URL are stored on the watch (schema 22). The build URL becomes a link only when it matches `GAOS_JENKINS_ORIGIN`.

**Jenkins side (§11.1).** The design gives templates:
- A multibranch test stage with no credential. It runs `go test -short` in Docker and archives four data files.
- An administrator-defined `gaos-unit-report` job, the only holder of the credential. It identifies the upstream build from the build cause, copies the four files and posts with `curl -K -`.

**UI and docs.** A "Before you switch this on" checklist explains the `-short` rule.

### How the existing system works, per this document

- A merged pull request reaches GAOS from Brain Town. GAOS re-reads it from GitHub to get the merge commit and stores a "watch" for that commit.
- A poller reads the configured workflow's check run every 2 minutes. That check run must belong to the push run of the named workflow.
- A failure files or updates one open unit defect per repository and base. Its triage card is held BLOCKED until an auto-fix admin releases it.
- A pass closes the defect, or only marks it if the card is still held. A missing or unreadable check is never a pass.
- Text that comes from the code under test is screened when it is taken in, fenced in the agent packet, and screened again at dispatch (BR-32).
- The staging callback (D-088) is today's only HTTP Basic machine route. It sits on the bearer mux behind an `X-Forwarded-Prefix` check.

## Trust boundaries touched

| From → To | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Jenkins report job → edge → Traefik → `unit-results` | Basic credential from its own file | the path product must equal the credential's product | strict decoder, screening, state checks | 409 `no-watch` and a retry; after 6 h the watch ends `no-ci` |
| Jenkins test stage → report job | the Jenkins build cause | the upstream job-name regex | none on the `COMMIT` parameter beyond 40 hex | the report job errors and the build is marked unstable |
| tunnel peers → Traefik → dashboard | WireGuard membership | none | `X-Real-IP` is trusted from all of 10.10.0.0/24 | not stated |
| Route → group notifier (WhatsApp) | — | — | fixed text | sending fails and is logged |
| Back office → `POST /products/{id}/autofix/unit` | a validated session (`auth.go:375-378`) | auto-fix admin | form fields | 422 |

Note on the rubric: the rubric says `signedIn()` "accepts a cookie from any request". `dashboard/auth.go:375-378` shows it reads a session through `sessionFrom`. This is not a design finding; the rubric's owner should correct that line (as §27 row 28 already says).

## Findings

### SEC-1 The new lock key adds the product id the caller picks, which reopens both the per-attempt bypass and unbounded growth of the failure map - Severity: High
- Component: `jenkinsGate` / `jenkinsSource` lock key (staging and unit-results)
- Evidence: `docs/designs/release-b4-unit-tests.md:238`, `:245` ("The key also carries the product of the path"), `:471` (TC-877 "product A's lock does not block product B"); `dashboard/staging.go:102-111` (`match` compares the credential against every product, whatever the path says); `dashboard/staging.go:125-145` (`fail` adds one map entry per key, and only removes entries that have expired once the map is over 4096); `deploy/edge/gaos.conf:45-48,63-70` (the edge rate-limits only `/docs`, so `/api` has no limit).
- Attack path: An unauthenticated caller from one address sends each guess to a different `{id}`: a real product, or any string such as `x1`, `x2`, …. Step 4 (a credential that matches no product, a 401) is counted against `source|{id}`. So the key is fresh on every attempt. Meanwhile `match` still tests the guess against every product's credential. This gives unlimited online guessing against every unit-results and staging credential, which is exactly the bypass SEC-1 of round 12 was meant to close.
  Every new key also adds an entry to `a.fails`. `PathValue` places no length limit on the id (only the edge's header-buffer limit applies), and nothing is removed while entries are inside their 5-minute window. A sustained stream of requests to `/api/v1/products/<random-long-id>/staging` therefore grows the dashboard's memory without bound. That is a denial of service against the single process that serves the board, the API and the poller.
  The design applies this key to staging as well, so this is a regression on a route that is live today.
- Remediation:
  - Count a step-4 failure (the credential matches no product) against the source alone: `jenkinsSource(r) + "|" + route`, never the path id.
  - Count a step-5 failure (valid credential, wrong product) against the source plus the credential's own product, not the path's.
  - If a product is wanted in the key at all, use it only when `a.sums` holds that product. Any other value becomes a fixed `?`.
  - Bound the map itself: a hard cap with eviction of the oldest entries, or refuse new keys while it is full.
  - Extend TC-877: rotating `{id}` must not escape the lock, and 10,000 distinct ids must not grow `fails` past the cap.
- Effort: small

### SEC-2 The lockout keys on the full IPv6 address, so a caller with a /64 gets a fresh bucket for every attempt - Severity: Medium
- Component: `jenkinsSource`
- Evidence: `docs/designs/release-b4-unit-tests.md:245` (the key is the `X-Real-IP` address); `deploy/edge/gaos.conf:84,140` (`listen [::]:443`); `deploy/edge/gaos.conf:50-52,59-62` (the edge's own docs limiter already keys IPv6 on the /64, "so keying by address would let it rotate freely").
- Attack path: An internet caller with an IPv6 /64, which is common, sends each attempt from a new address. Each one gets its own lock bucket, so the 5-in-5-minutes lockout never fires. This is the per-attempt bypass that round 12's SEC-1 described, now through address rotation instead of `X-Forwarded-For`.
- Remediation: Normalise the source the same way the edge does: IPv4 as the full address, IPv6 as its /64 prefix (`netip.Prefix` from `netip.ParseAddr(x).Prefix(64)`). Add an IPv6 rotation case to TC-877.
- Effort: quick-win

### SEC-3 `X-Real-IP` is trusted from every WireGuard peer, not just the edge, and the "reached only through the edge" assumption does not hold - Severity: Medium
- Component: the edge → Traefik → dashboard trust chain
- Evidence: `docs/designs/release-b4-unit-tests.md:245` (assumption: "GAOS is reached only through the edge (its port is not exposed beside it)"; Traefik "trusts the tunnel"); `deploy/stacks/compose.prod.yaml:42` (Traefik is published on the tunnel address); `deploy/stacks/traefik.yml:32-41` (`trustedIPs: 10.10.0.0/24`, which by its own comment includes the private VM itself and other tunnel hosts); `dashboard/main.go:473-475` / `compose.prod.yaml:67-76` (other hosts sit on the same tunnel, such as the database bridge and the second instance).
- Attack path: Any process on any WireGuard peer can talk to Traefik on 10.10.0.2:18301 directly. That includes a compromised or agent-driven process on another host in the /24. Because the peer is trusted, its own `X-Real-IP` value passes through. It can set `X-Real-IP` to Jenkins' public address and fail 5 times, locking Jenkins out of unit results and staging. Or it can send a fresh `X-Real-IP` on every attempt to escape the lock. The J2 deploy check (one call's log line) cannot detect this, because it only exercises the legitimate path.
- Remediation:
  - State the assumption accurately: GAOS is reachable from every tunnel peer.
  - Then pick one of two fixes:
    - (a) Firewall port 18301 on the VM so that only the edge's tunnel address (10.10.0.1) can reach it, and narrow `trustedIPs` to match. Note the measured 10.10.0.2 self-traffic in `traefik.yml:35`.
    - (b) Have the edge add a header that only it can set, for example a shared secret held in an edge-only file, and trust `X-Real-IP` only when that header is present.
  - Record the remaining risk in D-106.
- Effort: small

### SEC-4 The "conflicting unit result" group notice has no rate limit, and the design's claim that the lockout bounds it is contradicted by the design itself - Severity: Medium
- Component: `apiUnitResults` state 6c / the group notifier
- Evidence: `docs/designs/release-b4-unit-tests.md:226` ("one per conflicting post; the route's lockout bounds the rate"); `:243` ("every answer after authentication (… 409 …) count nothing"); `:290` (6c sends the notice on every conflicting post); `dashboard/notify.go:145-178` (`NotifyPR` sends to WhatsApp and writes a `RecordNotification` on every call, with no throttle).
- Attack path: An authenticated caller repeatedly posts a result that differs from the one stored on any finished watch of its product. That caller is the credential holder, or, by the D-106 residual, anyone who can push a branch when the credential is not scoped. Each post triggers one WhatsApp message to the product group and one store write. The lockout never fires, because a 409 is not counted. A legitimate Jenkins Replay loop or a misconfigured retry does the same thing by accident. The result is spam to the people who must react to real unit failures (alert fatigue) and steady growth of the store.
- Remediation: Send the conflict notice at most once per watch, using a `ConflictNoticeSent` flag on `UnitWatch` (schema 22 is already changing). Or share the once-a-day slot with the other daily lines. Keep the per-post log line. Correct `:226`, and add a case to TC-879: 20 conflicting posts produce one notice.
- Effort: quick-win

### SEC-5 The report job takes the commit from a parameter the upstream controls, and its upstream-job check accepts any folder prefix - Severity: Medium
- Component: §11.1 `gaos-unit-report` template (the trust anchor's last step)
- Evidence: `docs/designs/release-b4-unit-tests.md:577` (the upstream is taken "from the build cause … never from a parameter"); `:607` and `:641` (the commit is `env.UNIT_COMMIT`, set by the branch's own Groovy and passed as parameter `COMMIT`); `:655`, `:666` (only a 40-hex shape check); `:665` (`(.*\/)?route-api\/(develop|staging|main)`, any parent folder).
- Attack path: The upstream check proves which job triggered the report, but not which commit that job built.
  - A Jenkins user who can Replay a unit-base build can edit its Groovy without any pull request. They set `env.UNIT_COMMIT` to a *different* pending merge commit of the repository and write `status.txt` = 0. The report job then posts a forged `passed` for that commit. The commit is still pending, or within 24 h of `no-ci` (state 6a), and its branch matches. The forged pass closes a released unit defect.
  - A user who can create a job named `route-api/main` in any other folder can do the same, because the regex allows any prefix. Examples: an organisation folder that scans a fork, or a personal folder.
  - Neither path needs the credential, and review of the merged pull request guards neither. Yet D-106 states the residual only as "a writer of the Jenkinsfile".
- Remediation:
  - Read the commit in the report job from the upstream build itself (the SCM revision the upstream build records), not from a parameter. Or, at minimum, require the parameter to equal it.
  - Refuse an upstream build whose causes include a Replay cause.
  - Anchor the job check to the full path (`^<org-folder>/route-api/(develop|staging|main)$`).
  - List "Run/Replay on unit-base jobs is restricted" next to the folder-scoped credential among D-106's owner conditions.
  - Add Replay to the residuals.
- Effort: small

### SEC-6 The per-answer log line writes the unauthenticated, URL-decoded path id - Severity: Low
- Component: `apiUnitResults` logging
- Evidence: `docs/designs/release-b4-unit-tests.md:308` ("One line per answer: `unit-results: <product> …`"); `dashboard/staging.go:223` (`r.PathValue("id")`, which the Go mux returns unescaped).
- Attack path: An unauthenticated caller puts a percent-encoded line break in `{id}`. The 401 answer's log line then carries text the caller chose, which can forge an "accepted" line or split entries in the log the operator reads to investigate conflicts and attribution (SEC-4 of round 12).
- Remediation: Log the path id with `%q`, or log `-` for every answer before step 6. Log `<repo>@<sha12>` only after the body has been validated.
- Effort: quick-win

### SEC-7 The test stage's artifacts and network are not hardened against the code under test - Severity: Low
- Component: §11.1 test stage and report job
- Evidence: `docs/designs/release-b4-unit-tests.md:600` (the Docker agent runs with the default network); `:631` (`archiveArtifacts` with no `followSymlinks: false`); `:677`, `:683` (the report job reads `total.txt` and `summary.txt` with no size check before `jq`).
- Attack path:
  - Merged test code can leave a background process, or replace `unit/*.txt` with symlinks, before `archiveArtifacts` runs. Jenkins' artifact archiver follows symlinks by default (to be confirmed against the organisation's Jenkins version). That can pull a file from the agent host into an artifact, which then becomes the `build-failed` summary sent to GAOS and, after screening, into an agent packet.
  - The container's open network also lets test code reach internal services during the run. The `-short` rule exists precisely because tests should not touch the network.
- Remediation:
  - Set `archiveArtifacts … followSymlinks: false`.
  - Run the test container with `args '--network none'` after a separate `go mod download` that does have network.
  - In the report job, refuse any artifact over a fixed size, for example 64 KiB, before reading it.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=4 Low=2
