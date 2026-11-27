# Security review: `release-b4-unit-tests.md` rev 14 (§0–§14, §26)

## What this proposes

Rev 14 adds a second **result source** for the unit-test feature, D-105, which shipped in v0.67.0. The new setting `UnitChecks.Source` can be `github-actions` (the default, and what every existing record reads as) or `jenkins`. With `jenkins`, the organisation's Jenkins runs `go test -short` on each merge build of a unit base. It then sends the result to a new machine route, `POST /api/v1/products/{id}/unit-results` (§5.4).

The route reuses the staging callback's checks unchanged, moved into a shared `jenkinsGate`: the `X-Forwarded-Prefix` rule, a 503 when the credential file is not configured, and a per-source lockout. That lockout is shared with the staging route: 5 failed logins in 5 minutes block the source for 5 minutes. Authentication is HTTP Basic with one credential per product from `GAOS_STAGING_BASIC_AUTH_FILE`, and the staging callback uses the same credential.

The request body is limited to 64 KiB, parsed strictly and validated field by field. Test names, file paths and the summary are screened with the same rules as GitHub annotations. A result is accepted only for a **pending** watch of that exact merge commit, on its base, while the product has unit checks on with the `jenkins` source. It finishes the watch through the same store call the poller uses (`FinishUnitWatchFrom`), so defects, holds, pass-closes and notices work as before.

When no watch exists yet, the route answers 404 `no-watch` with `Retry-After: 60` and stores nothing; the Jenkins stage retries once a minute for up to 10 minutes. With no result, the poller ends a `jenkins` watch as `no-ci` after 6 hours, without reading GitHub.

Other changes:
- The posted `runUrl` is stored after screening. It is shown as a link only when it matches a new `GAOS_JENKINS_ORIGIN`.
- The Auto-fix page gets a source selector and a "Before you switch this on" checklist.
- §11.1 gives the repository owner a Jenkinsfile template. Its test stage runs in a container with no credential; its report stage runs on a `gaos-report` agent and binds the credential with `withCredentials`.
- Registers: D-106 (new), BR-65 and BR-52 amended, and schema 22 (`UnitWatch.runUrl`).

## How the existing system works, per this document

A merge into a unit base of a fix-target repository stores a `UnitWatch` instead of an agent card (§4.2). Under GitHub Actions, the poller reads the configured workflow's push run and that run's check-suite check (§5.1). A failure files or updates one open unit defect per repository and base. Its triage card is created BLOCKED (`ReasonUnitHeld`) and only an auto-fix admin can release it (§7.3). A later pass closes released defects; a still-held defect is only marked (§7.4).

The staging callback (D-088, `staging.go:195-288`) is today the only Basic-auth machine route. It counts failures per source, where the source is the peer address plus the first `X-Forwarded-For` hop (`staging.go:172-181`). Every other `/v1` route authenticates itself with a bearer token through `apiGate` (`api_v1.go:316-340`).

---

### SEC-1 The lockout is keyed on a value the caller supplies, so it can be dodged, and anyone can use it to block Jenkins - Severity: High
- **Component:** the authentication gate (`jenkinsGate`) and its lockout, at the public edge → VM boundary.
- **Evidence:**
  - `dashboard/staging.go:172-181` builds the lockout key from the peer address plus the **first** `X-Forwarded-For` entry.
  - `deploy/edge/gaos.conf:118-128` *appends* to `X-Forwarded-For`, so the caller's own value stays first. Its comment says "Nothing behind this reads only the first hop", which `stagingSource` contradicts.
  - `deploy/stacks/traefik.yml:40-41` trusts the tunnel, so Traefik passes the chain through, and the peer GAOS sees is the same Traefik address for every caller.
  - The design relies on this lock as the control: `docs/designs/release-b4-unit-tests.md:236` and `:241` ("failures on either route count toward the same lock").
- **Attack path:** an unauthenticated internet caller can do two things.
  - **(a) Avoid the lock.** They send a different first `X-Forwarded-For` value on each attempt. Every attempt then gets a fresh failure counter, so only nginx's per-address `limit_req` slows credential guessing. The file requires only a 12-character password and does not enforce randomness (`staging.go:86`).
  - **(b) Lock out Jenkins.** They put Jenkins' egress address as the first `X-Forwarded-For` value. Their key is then identical to Jenkins' key, so 5 bad requests every 5 minutes keep Jenkins locked out of both routes. Every unit result for every product on that Jenkins is refused (429). After its 10-minute retry the stage gives up, and those watches end `no-ci`, so failing merges are never filed. The staging callback is silenced too.

  Rev 14 makes this worse: it puts a defect-filing path behind this lock and deliberately shares the lock across both routes.
- **Remediation:**
  - Key the source on an address the edge sets, not one the caller sends: the address nginx appended (the last entry before the trusted proxies), or `X-Real-IP`, which nginx sets from `$remote_addr` at `gaos.conf:117`. Add a test that a spoofed first hop neither escapes nor causes a lockout.
  - Also count failures per product, so a lockout does not span products.
  - Rethink whether one lock should cover both routes.
  - Record this gap against D-088 and D-106.
- **Effort:** small.

### SEC-2 The credential can be reached by unreviewed code in multibranch Jenkins, and the shared staging credential is now exposed in every merge build - Severity: High
- **Component:** the repository → Jenkins → GAOS trust anchor (D-106, §11.1).
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:563-565`: the controls are a `when { branch … }` guard, a `gaos-report` agent label and `withCredentials`, and all three live in the **repository's** Jenkinsfile.
  - `:580`, `:593`, `:598`, `:622-623`: the template's own guard, label and credential binding.
  - `:487`: the D-106 residual describes the risk as "a writer of the Jenkinsfile". `:652` and `:767` name review of the merged pull request as the guard.
  - `:758`: a separate credential was rejected.
- **Attack path:** in a multibranch pipeline, each branch builds from its own Jenkinsfile. Anyone who can push a branch, not merge one, can do any of the following:
  - Remove the `when` guard, bind credential `gaos-p-9` in any stage, and read it.
  - Post `passed` for any pending watch of the product. That closes released unit defects and pre-empts the real result, because the first result for a watch wins (`unitdefect.go:450-452`).
  - Post invented failures up to the daily cap.
  - Call the staging callback with the same credential.

  The "has never run the code under test" property of `gaos-report` is only a naming convention. Any Jenkinsfile in the organisation can ask for `node('gaos-report')`. Review of the merged pull request cannot catch any of this, because it happens in builds that are never merged.

  Today the staging credential sits in a deploy job. Rev 14 puts it into every merge build of every fix-target repository.
- **Remediation:** §11.1 and the published docs should require all of the following. GAOS cannot check them, so D-106 should state them as conditions, not leave them as a residual.
  - **Bind the credential only from Jenkins configuration outside the repository.** For example, an administrator-defined report job that copies the artifacts of base-branch builds and posts them, or a credential scoped to a folder that branch builds cannot reach.
  - **Restrict the `gaos-report` label to that job.**
- Also:
  - Issue a **separate per-product unit-results credential**. The route can accept a second file with the same gate.
  - Reword the D-106 residual to "anyone who can push a branch the pipeline builds".
- **Effort:** structural.

### SEC-3 The Jenkins stage retries every 404, including the one that counts as a failed login, so a misconfigured stage locks out its own Jenkins - Severity: Medium
- **Component:** the §11.1 retry loop and the shared lockout.
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:238`: a credential valid for another product answers 404 **and counts as a failure**.
  - `:285`: `no-watch` is also 404, and does not count.
  - `:570` and `:631`: the stage retries on any 404 or 429, up to 10 times, once a minute.
  - `dashboard/staging.go:224-227` (counts the failure) and `:51-55` (5 failures in 5 minutes).
- **Attack path:** a stage with the wrong product id in its URL, or one copied to a repository of another product, gets 404 `not-found` and retries. It reaches 5 counted failures within 5 minutes, which locks the shared Jenkins source. Combined with SEC-1, every product's staging callbacks and unit results from that Jenkins are then refused. The design gives the stage no way to tell "not yet" from "wrong product".
- **Remediation:** make `no-watch` distinguishable, for example with a different status such as 409 or 425 `no-watch`. Alternatively, have the template read `error` from `answer.json` and retry only `no-watch` and `throttled`. State in §5.4 which answers count toward the lock.
- **Effort:** quick-win.

### SEC-4 A posted result is not attributed to a caller, and a conflicting later result is thrown away silently - Severity: Medium
- **Component:** audit trail for `apiUnitResults`.
- **Evidence:**
  - `docs/designs/release-b4-unit-tests.md:304`: the log line has no caller address.
  - `:286`: a second result for a done watch answers 200 `duplicate` and is discarded.
  - `:300`: the only provenance is `RunRef` ending `@jenkins`.
  - `dashboard/unitdefect.go:429-430`: the pass-close records the rule `unit-passed` and no actor.
- **Attack path:** someone holding the shared credential (see SEC-2) posts a forged `passed` that closes a released defect. When the real Jenkins posts `failed` a minute later, it gets 200 `duplicate` and nothing is recorded. The operator cannot see that two different results arrived, or where either came from. Because the credential is shared with staging, a leak cannot be traced to a route either.
- **Remediation:**
  - Log the caller's source address, using the corrected key from SEC-1.
  - Store `PostedFrom` and the posted `result` on the watch.
  - When a duplicate result's `result` differs from the stored outcome, log it and send the group a "conflicting unit result" notice. This uses the existing `unit:failure` kind, so no new notice kind is needed.
  - Record the actor as `jenkins:<product>` in the events the posted result causes (BR-48).
- **Effort:** small.

### SEC-5 The credential-holding report agent unpacks and processes files the code under test wrote, without limiting paths - Severity: Low
- **Component:** the §11.1 template's report stage.
- **Evidence:** `docs/designs/release-b4-unit-tests.md:589` stashes `**/*_test.go` and `test.json` from the code under test. `:599-608` unpacks them in the workspace where the credential is later bound (`:622`). `$dir` comes from `test.json`, which code running in the test container can overwrite, and is used as a path with no check for `..` or absolute paths (`:607-608`).
- **Attack path:** the code under test controls `test.json`. It can steer the report agent's `grep` to `*_test.go` files elsewhere on that agent, and the matched names go into the posted `file` field. GAOS's `contextPathOK` probably drops such paths, so the effect is small. The underlying pattern, though, is untrusted repository content landing in the credential-bearing workspace.
- **Remediation:**
  - Resolve each failing test to its file **in the test stage**, and stash only `test.status`, `module.txt` and the finished `failing.json` and summary.
  - In the template, refuse a `$dir` that is absolute or contains `..`.
  - Run `deleteDir()` before `unstash` on the report agent.
- **Effort:** quick-win.

### SEC-6 Nothing checks centrally that every machine-API route authenticates; rev 14 adds a second route that skips the bearer check - Severity: Low
- **Component:** BR-55 enforcement.
- **Evidence:**
  - `dashboard/main.go:634-637`: every `registerAPI` route is classed "bearer" and mounted with no shared authentication step. Each handler authenticates itself.
  - `dashboard/auth_test.go:332-336`: the test checks only that the route classes don't overlap.
  - `docs/designs/release-b4-unit-tests.md:491`: the BR-55 compliance argument rests on "registered on the bearer mux".
- **Attack path:** none today, since TC-873 covers the new route. The risk is that a later refactor of `jenkinsGate`, or a third Basic route, ships open while classification still reports "bearer".
- **Remediation:** add a table test over `registerAPI` that sends every route a request with no `Authorization` header and expects 401, or 503 when that route's authentication is not configured. List the Basic-auth routes (staging and unit-results) explicitly, so adding another one requires editing the list. Name this test in §9.1 J2.
- **Effort:** quick-win.

---

**Checked and found sound (no finding):**
- Authentication runs before the body is read.
- A result can only finish a pending watch, once (`unitdefect.go:446-452`). The watch key includes the product id (`unitchecks.go:88-90`), so one product's credential cannot reach another product's watches.
- The `runUrl` link is shown only for the configured `https` origin, with no user-info and `rel=noopener noreferrer`, and it is never put into a packet.
- Test names, files and the summary go through the same screening, then the fenced packet and screening again at dispatch (BR-32).
- No new outbound origin, egress entry, event type (BR-05) or notice kind is added.
- The design carries no private hostname or credential text: a search for the instance names and password-shaped strings found nothing.

**Rubric note:** the rubric says `signedIn()` accepts any cookie. That is out of date. `dashboard/auth.go:375-378` validates a session, and `dashboard/auth_test.go:354-369` expects a forged cookie to get 401. So the design's "auto-fix admin only" controls (`autoFixAdmin` via `sessionOf`) can be implemented as written. The rubric file should be updated.

**Files referenced:**
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/docs/designs/release-b4-unit-tests.md`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/staging.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/api_v1.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/main.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/unitdefect.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/unitchecks.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/auth.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/dashboard/auth_test.go`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/deploy/edge/gaos.conf`
- `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-jenkins/deploy/stacks/traefik.yml`

Counts: Critical=0 High=2 Medium=2 Low=2
