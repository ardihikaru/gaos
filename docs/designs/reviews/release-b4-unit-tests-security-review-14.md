# Security review: `docs/designs/release-b4-unit-tests.md` (rev 16)

Round 14 of the design gate: the rev-16 security re-check. Saved verbatim; HTML entities shown in the session are written back as literal characters.

**Scope:** §0–§14 and §26–§28. §15–§25 were read only for context.
**Rubric:** `docs/rules/gaos-design-review.md`, `docs/designs/README.md`.
**Code root:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins`.
I opened nothing under `.claude/` and no credential file.

## Paraphrase

### What this proposes

Version v0.67.0 already gets unit-test results by reading one named GitHub Actions check on a merge commit. Rev 14–16 add a second result source, `UnitChecks.Source = jenkins`, because the target organisation's GitHub Actions cannot run.

- **Who posts.** The organisation's Jenkins sends the result for each merge build to a new machine route, `POST /api/v1/products/{id}/unit-results`.
- **How it authenticates.** HTTP Basic, one credential per product. The credential has its own file, `GAOS_UNIT_RESULTS_BASIC_AUTH_FILE`. It uses the staging callback's mechanism (`jenkinsGate`), but the two routes never share a credential or a lock.
- **What a result does.** It finishes the pending watch for that merge commit through the same store call the GitHub poller uses. So the held defect, the pass-close and the notices stay the same.
- **Body rules.** The body is strict JSON, at most 64 KiB, and test names are screened. A `runUrl` is a link only when it matches `GAOS_JENKINS_ORIGIN`.
- **Answers.** A result for an unseen merge gets 409 `no-watch`. A result up to 24 hours after `no-ci` is accepted. A second, different result is refused and reported to the group once.
- **Changes to the lock (D-088's gate), for both routes:**
  - The lockout is keyed on the address the edge sets (`X-Real-IP`, else the rightmost public hop; IPv6 as its /64), plus the route, plus the credential's own product. It never uses the path id.
  - The failure map is capped at 4096 keys.
  - Traefik's `trustedIPs` narrows from the tunnel `/24` to the edge and the two GAOS VMs.
  - Each VM gets a `DOCKER-USER` firewall rule.
- **Jenkins side (§11.1).** A test stage in the multibranch Jenkinsfile runs `go test` in a container with no credential and no network, and archives four data files. A separate report job, defined by an administrator outside any repository, is the only holder of the credential. It:
  - takes the upstream build from the build cause;
  - checks the full job path;
  - reads the commit the upstream build recorded;
  - refuses Replays;
  - refuses symlinks and files over 64 KiB;
  - posts with curl, passing the credential on stdin.
- **The Auto-fix page** gains a source selector and a "Before you switch this on" checklist.
- **Registers:** D-106, BR-65 and BR-52 amended, schema 22, TC-871 … TC-883.

### How the existing system works, per this document

- A merge into a unit base of a fix-target repository plants a `UnitWatch`.
- Every 2 minutes the poller reads the push run of the configured workflow and the check run in that run's suite. The result files or updates one held unit defect per repository and base; only an auto-fix admin can release it.
- A pass closes the defect, or only marks it if it is still held.
- A missing or unread check is never a pass.
- The staging callback (`staging.go:195-230`) is today's only Basic-auth machine route. It keys its failure lock on the peer plus the caller-written first `X-Forwarded-For` hop.
- nginx on the edge (clino) sets `X-Real-IP` and appends `X-Forwarded-For` (`gaos.conf:117,128`).
- Traefik is published on the VM's tunnel address and trusts forwarded headers from the whole `/24` (`traefik.yml:40-41`, `compose.prod.yaml:42`).

## Trust boundaries the design touches

| From → To | Authenticates | Authorises | Validates | If the far side fails |
|---|---|---|---|---|
| Jenkins report job → edge → Traefik → `/unit-results` | Basic credential from its own file (§5.4) | the credential's product must equal the path id, else 404 | strict body, screening, watch state checks | no post means `no-ci` after 6 h; never a pass |
| tunnel peer → Traefik (`X-Real-IP`) | Traefik `trustedIPs` plus the `DOCKER-USER` rule | — | — | spoofed source: lock dodged, or Jenkins locked out (SEC-3, SEC-4) |
| branch pusher → Jenkins → credential | Jenkins item permissions and credential folder scope (owner conditions, unverifiable by GAOS) | report job cause and path checks | Replay refusal, recorded revision | forged results for pending merges (SEC-1, SEC-2) |
| admin browser → `POST /products/{id}/autofix/unit` | session (`auth.go:375-378` validates it; the rubric's "accepts any cookie" note is out of date) | `autoFixAdmin` | 64 KiB limit, form validation | — |
| repository → host (credential file) | file mode 0600 (`staging.go:71`) | — | `rebuild-restart.sh:233` loop | a missing file stops the deploy |

## Findings

### SEC-1 The report job's script approvals widen Groovy access for every pipeline on the Jenkins instance - Severity: High
- Component: §11.1 report job (Jenkins trust boundary)
- Evidence: `docs/designs/release-b4-unit-tests.md:675-680` (`Jenkins.get().getItemByFullName(...)`, `getBuildByNumber`, `getCause(ReplayCause)`, `getActions(BuildData)`, "need an administrator's script approval … once"); `:581`; `:504` (the D-106 owner conditions do not mention it).
- Attack path:
  - In-process Script Approval of method signatures applies to every sandboxed Groovy script on the instance, not just to `gaos-unit-report`. That includes every multibranch branch and pull-request build, which is the actor this design treats as untrusted (§11.1, "a branch pusher controls it").
  - Without the Authorize Project plugin, builds run as SYSTEM. A branch pusher's Jenkinsfile could then call the approved accessors to read any job and build by full name, regardless of item permissions, and read their recorded revisions and remote URLs.
  - Every further signature an administrator approves "once" to make the template work widens this for all pipelines.
  - The design asks the owner to weaken the very sandbox that keeps branch code away from privileged state, and never states the scope.
  - Rated High rather than Medium: this is a privileged control the design tells someone to switch off, the exposure crosses Jenkins folder authorisation, and the fix is cheap.
- Remediation:
  - State that the report job runs **with the Groovy sandbox off**. An administrator approves the whole script; whole-script approval is keyed to that exact script and grants no per-signature access to other pipelines. Alternatively, put the upstream inspection in a trusted (global) shared library step.
  - Forbid per-signature approvals in the template comment and in D-106's owner conditions.
  - Side effect: Replay of an unsandboxed main script then needs administrator rights, which also helps SEC-2.
- Effort: small

### SEC-2 Permissions on the credential's only holder (the report job and its folder) are not an owner condition - Severity: Medium
- Component: D-106 owner conditions, §11.1
- Evidence: `docs/designs/release-b4-unit-tests.md:504` (conditions name only "Run and Replay on the unit-base jobs"); `:581`, `:585` (the report job "is the only holder of the credential"); `:750`.
- Attack path:
  - A Jenkins user who has Item/Configure, Run/Replay or Item/Build on `gaos-unit-report`, or Credentials/View or Update on its folder, can do either of two things:
    - edit the script, or replay it with changed Groovy, which runs with `gaos-p-9` bound and skips every cause, path and Replay check;
    - read the credential directly.
  - The design's protections all apply to the upstream build. None applies to the report job itself.
  - Rated Medium, not High: the document already says "an administrator defines the report job" and "a writer of either can forge". The gap is that the permissions are not stated, not a design that grants them.
- Remediation:
  - Add to D-106's owner conditions: Configure, Replay and Build on `gaos-unit-report`, and every credential permission on its folder, are held only by Jenkins administrators. (The upstream trigger needs only the triggering build's authority.)
  - Add an item to the §0 first-run check: a non-administrator account cannot see Configure or Replay on the report job.
- Effort: quick-win

### SEC-3 The `DOCKER-USER` rule as described matches the published port, which Docker has already rewritten - Severity: Medium
- Component: host firewall (tunnel boundary), §5.4, §12.1
- Evidence: `docs/designs/release-b4-unit-tests.md:249` ("accepts TCP 18301 on the `wg0` interface only from `10.10.0.1` and the VM's own tunnel address"); `:828`; `deploy/stacks/compose.prod.yaml:42` (`<tunnel-ip>:18301:80`); `deploy/STACKS.md:103` (Docker bypasses ufw).
- Attack path:
  - Packets reach the `DOCKER-USER` chain after the destination NAT, with destination port 80 and the container's address. A rule written as "TCP port 18301" therefore matches nothing.
  - Depending on how the accept and drop are written, the rule either drops nothing (every tunnel peer still reaches Traefik) or drops the edge too.
  - Locally generated traffic from the VM to its own published port does not cross `DOCKER-USER` at all, so the "VM's own address" clause is moot.
  - The commands are left to `deploy/edge/README.md` and do not exist yet. The design treats this rule as the second layer behind `trustedIPs`, and in a multi-tenant tunnel it is the only thing stopping cross-VM spoofing (see SEC-4).
- Remediation:
  - Specify the match in the design itself: `-i wg0 -p tcp -m conntrack --ctorigdst <vm-tunnel-ip> --ctorigdstport 18301`, accept from `10.10.0.1`, then drop.
  - Make the "curl from a non-listed tunnel peer times out" check a mandatory deploy step, with the result recorded.
  - Name the known direct tunnel caller `deploy/seed/README.md:21` (`http://<tunnel-ip>:18301/api/v1`). A one-day `conntrack` sample will probably not see it.
- Effort: quick-win

### SEC-4 `trustedIPs` trusts the two GAOS VMs with no stated need, and each instance trusts the other VM - Severity: Medium
- Component: `deploy/stacks/traefik.yml`, D-106 residual
- Evidence: `docs/designs/release-b4-unit-tests.md:249`, `:504` (the residual "a process on either GAOS VM can set `X-Real-IP`"), `:827`; `deploy/stacks/traefik.yml:32-36` (the measurement shows only that VM-local requests arrive *as* `10.10.0.2`, not that they carry forwarded headers that must be believed).
- Attack path:
  - Trusting `10.10.0.2/32` and `10.10.0.9/32` on both instances means:
    - any host process on vm-rnd-ardi can set `X-Real-IP` for rnd-v3's GAOS, and the reverse, held back only by the SEC-3 rule;
    - any host process on the same VM can do the same for its own GAOS.
  - Such a process can lock Jenkins out of both Basic routes or dodge the brute-force lock.
  - The design accepts this as a residual, but it is self-inflicted. Trust is needed only for a proxy that forwards someone else's traffic, and nothing on either VM is said to do that.
- Remediation:
  - Set `trustedIPs` to `10.10.0.1/32` only. A VM-local request then gets its own address as `X-Real-IP`, the D-106 residual disappears, and `check-stacks.sh` (TC-883) asserts a single entry.
  - If a VM-local proxy does exist, name it, and list only that VM's own address in that instance's file rather than a shared list.
- Effort: quick-win

### SEC-5 A credential holder can reset the guessing lock, and nothing guarantees enough password entropy - Severity: Medium
- Component: `jenkinsGate` (staging and unit-results), credential file format
- Evidence: `docs/designs/release-b4-unit-tests.md:241` (step 6 `a.ok(src|route)` and `a.ok(src|route|<product>)`); `dashboard/staging.go:147-151` (`ok` deletes the key), `:230`; `dashboard/staging.go:86` (minimum 12 characters, no entropy rule); `deploy/edge/gaos.conf:47-48` (no edge limit on `/api`); `docs/designs/release-b4-unit-tests.md:243`.
- Attack path:
  - Under D-106's own residual, anyone who can push a branch where the credential is not scoped holds a valid credential for product A.
  - From one source they can send 4 guesses at another product's credential, then 1 valid post for A. That resets `src|route`, so the lock never engages and guessing continues without limit, against both routes.
  - Separately, a caller with many addresses (an IPv6 /48 gives 65,536 /64 buckets) gets 5 guesses per bucket until the map holds 4096 keys. The cap bounds memory, not the total guess rate.
  - The only remaining bound is password strength, and the format accepts any 12 characters.
- Remediation:
  - On success, reset only `src|route|<product>`. Let `src|route` expire on its own window.
  - Require a generated secret of at least 32 characters for the new file, and document one generation command (for example `openssl rand -base64 32`).
  - Optionally add a per-route global ceiling on unauthenticated failures.
  - Extend TC-877 with an "interleaved success" case.
- Effort: small

### SEC-6 Reviewer deny-list does not cover the new credential file - Severity: Medium
- Component: reviewer workspace, stored review output
- Evidence: `docs/designs/release-b4-unit-tests.md:825`, `:829` (new host file `deploy/stacks/prod.pr-unit-results-auth`); `.gitignore:13-17` (`*.bt-*` and `*.pr-*` are secret-bearing). The deny-list this reviewer was given names `*.aim-*`, `*.api-*`, `*.bo-*`, `.env*`, `*.env`, `*.pem`, `*.key` and `**/credentials*`, but not `*.pr-*` or `*.bt-*`. I cannot open `.claude/rules/agent-workspace.md`, so whether the canonical list matches is **unverifiable** by me.
- Attack path:
  - The goldfish reviewers hold Read/Grep/Glob over the repository, and their reports are committed and append-only.
  - In a checkout where the deploy files exist (they are absent from this worktree), a reviewer, or one steered by injected text in source or test names, can read `prod.pr-unit-results-auth`. The same is already true of `prod.pr-staging-auth`, `prod.pr-github-token` and `prod.bt-token`.
  - The control is instructional only.
  - Rated Medium, not High: the gap is older than this design, the files are gitignored and absent here, and the "path and line, never the text" rule still applies. But this design adds another member to the uncovered pattern.
- Remediation:
  - Add `*.pr-*` and `*.bt-*` (better: `deploy/stacks/prod.*` with a `!*.example` exception) to the deny-list in `.claude/rules/agent-workspace.md` and the reviewer prompts.
  - Enforce it with a tool-level deny rule rather than instruction alone.
  - Record it in §12.1.
- Effort: quick-win

### SEC-7 Gate step 3 checks a product-keyed lock before the product is known - Severity: Low
- Component: §5.4 gate order
- Evidence: `docs/designs/release-b4-unit-tests.md:238-241`.
- Attack path:
  - Step 3 looks up "`src|route|<product>`" before step 4 has read the credential, so no product is known yet.
  - An implementer is likely to reach for the path id, the one value at hand. That brings back round-13 SEC-1 for that key form: a wrong-product caller rotates the path and is never locked.
- Remediation:
  - Step 3 looks up only `src|route`.
  - After a match, look up `src|route|<p>` for each matched product before answering step 5 or step 6.
  - Name this in TC-877.
- Effort: quick-win

### SEC-8 The report job picks the commit by substring match on the remote URL - Severity: Low
- Component: §11.1 report job
- Evidence: `docs/designs/release-b4-unit-tests.md:680` (`remoteUrls.any { u -> u.contains('acme/route-api') }`, `.find` keeps the first).
- Attack path:
  - A unit-base build that records more than one `BuildData` can make the report job post another repository's revision. Examples: a second `checkout`, or a shared library whose remote is `acme/route-api-lib`.
  - This weakens the "commit the upstream recorded" guarantee of round-13 SEC-5.
- Remediation:
  - Compare the normalised remote URL for exact equality (strip `.git` and the scheme).
  - Refuse when anything other than exactly one `BuildData` matches.
- Effort: quick-win

### SEC-9 One log line per answer, unauthenticated ones included, on a path with no edge rate limit - Severity: Low
- Component: §5.4 logging, availability
- Evidence: `docs/designs/release-b4-unit-tests.md:312` ("One line per answer"); `deploy/edge/gaos.conf:47-48`, `:63-69` (the limiter keys are empty outside `/docs`); `docs/designs/release-b4-unit-tests.md:243` (each full-map 429 is also answered and logged).
- Attack path: an unauthenticated caller grows the dashboard log at its request rate (401, 429 and map-full 429 all log), with no edge limit on `/api`. The host's disk fills, and real conflict lines get buried.
- Remediation:
  - Log the first failure per key and the transition into a lock, and count the rest.
  - Or add a modest `limit_req` zone for `/api/v1/products/*/(staging|unit-results)` at the edge.
- Effort: quick-win

Counts: Critical=0 High=1 Medium=5 Low=3
