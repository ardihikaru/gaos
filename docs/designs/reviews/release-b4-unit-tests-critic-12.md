# Goldfish review: release-b4-unit-tests.md, rev 14 (§0–§14, §26), critic, structure and style lens

**Thesis under attack.** Rev 14 says Jenkins is just "a second result source": the same finish, the same outcomes, the same trust model with a different anchor. The strongest counter-argument is this:

- **The trust anchor gets weaker, not just different.** D-105's anchor is a binding to one workflow's push run on a GitHub VM. D-106's anchor is a reusable password, shared with the staging callback, placed into every fix-target repository's multibranch pipeline. Any branch build there can bind it.
- **The template carries the correctness.** Most of the behaviour now depends on a Groovy and bash template that GAOS cannot check, built on labelled assumptions.
- **Most failures are silent.** They surface six hours later as `no-ci`, or as a `200 duplicate` that swallows a real result.

Not one finding is a Blocker, but six Majors would make the first real run (the operator's request (b)) fail or mislead.

**Anchors checked and confirmed** (against `7f63709`):
- `staging.go:51-55`, `:57-96`, `:172-181`, `:186`, `:196-230`
- `unitpoll.go:22`, `:25`, `:48-50`, `:74-96`, `:111-113`, `:126`, `:228-240`, `:276-288`, `:297-348`, `:415`, `:447-449`, `:463-470`
- `unitdefect.go:35`, `:38`, `:111-114`, `:443-491` (it fills `Repo/SHA/Base/MergedAt` from the watch, so `UnitPassIn{RunID:0}` is sufficient)
- `unitchecks.go:64`, `:88`, `:100`, `:123-141`, `:466`
- `api_v1.go:442-444`, `main.go:473-475`, `main.go:634-637` (bearer-mux registration)
- `compose.prod.yaml:176`
- `autofix_view.go:127`, `:660`, `:973-999`; `backoffice.html:1258-1287`; `app.css:1666-1675`, `:1777-1778`
- `brd.html:1218`, `:1310`, `:1352`, `:1355`, `:1524`; `decision-log.html:18` ("One hundred and four entries"); `qa-test-plan.html` ends at TC-870
- `README.md:143` (733/708); `check-workspace.sh:114-130`

The arithmetic in §9.1 (9 cases, 733 → 742, 708 → 717) and the 16-file count in §12.1 (11 + 2 + 3) are correct. The register numbering (D-106, TC-871…879, schema 22, no new BR) is consistent.

---

### Any branch build can take the shared credential; the "reviewed pull request" premise is wrong for multibranch - Severity: Major
- **Affected scope:** §11.1 (the rules, "GAOS never writes the Jenkinsfile… merges it through an ordinary reviewed pull request"), §10.1 D-106 residuals, §13 row "A separate credential file", §14 "Jenkins (rev 14)".
- **Finding and rationale:**
  - In a Jenkins multibranch pipeline, every branch and same-repository pull-request build runs the Jenkinsfile of that branch, not the merged one. The `when { branch … }` guard sits inside that file, so the branch author controls it.
  - A developer with push access to any branch can add `withCredentials([usernamePassword(credentialsId: 'gaos-p-9' …)])` and send the password anywhere. No review happens first.
  - The credential is also the staging callback's (`staging.go:19-30`). That callback plans staging run cards and create cards (`staging.go:259-261`, BR-54 amended), so the leak reaches more than unit results.
  - D-106 lists "a writer of the Jenkinsfile can forge" as a residual, but frames that writer as someone who merged a reviewed pull request. That understates the exposure: the credential moves from one staging job to every repository's pipeline.
  - §13 rejects a separate credential on "the operator asked for the staging callback's authentication exactly". That conflates the authentication *mechanism* (per-product HTTP Basic, the same gate) with the *same secret*.
  - D-088's rejected-alternatives line (`decision-log.html:1885`) also keeps "a bearer client for Jenkins" as the follow-up. The Jenkinsfile already uses curl, so it can send a bearer header. §13 does not consider this.
- **Recommendation:**
  - Keep `jenkinsGate` (the same mechanism), but read a second file, `GAOS_UNIT_RESULTS_BASIC_AUTH_FILE`, with the same format, or let the gate accept a line from either file. Keep the per-product lines separate from the staging ones.
  - Require the Jenkins credential to be scoped so that only the base-branch jobs can bind it: a folder-scoped credential plus a branch-restricted credentials provider, or a credential the PR and branch jobs cannot see. State this as an owner rule in §11.1.
  - Restate the D-106 residual: a branch pusher, not only a merger, can read the credential unless it is scoped.
  - Add the bearer-client alternative to §13 with a reason.

### Retrying on every 404 counts toward the shared lock and can lock out the staging callback - Severity: Major
- **Affected scope:** §5.4 (gate step 5, the "shared lock" paragraph, the race decision); §11.1 (retry on 404 and 429, the Jenkinsfile line `if (code != '404' && code != '429')`).
- **Finding and rationale:**
  - The gate answers 404 `not-found` for a credential valid for another product and counts it as a failure (`staging.go:224-227`). It answers 404 `no-watch` after `a.ok(src)` and counts nothing.
  - The template retries on any 404 without reading the code. Picture a Jenkinsfile copied into a repository of another product with the `p-9` path unchanged, or a credential ID pointing at the wrong product's line. Five retries lock the source for 5 minutes (`staging.go:51-55`, `:134-136`).
  - Because the lock is per source address and shared by both routes, the same Jenkins host's staging-deploy callback then gets 429. The design advertises "failures on either route count toward the same lock" as a property, but does not notice that its own retry loop produces those failures.
- **Recommendation:** Give "not seen yet" a status that cannot be confused with an authentication outcome, for example `425 Too Early` or `409 no-watch` with `Retry-After`. Alternatively, have the template parse `answer.json` and retry only when `error == "no-watch"`. Add a TC-873 or TC-876 assertion that a wrong-product credential is never retried into a lock.

### The report stage can fail or stall the organisation's base-branch pipeline - Severity: Major
- **Affected scope:** §11.1 ("it never fails the build: the merge already happened"); the Jenkinsfile `sh(returnStdout: true, …)` and the retry loop.
- **Finding and rationale:**
  - In the curl pipeline, `curl` is the last command. On a connection error (GAOS down, DNS, TLS) curl exits non-zero (7, 6 or 35), the `sh` step throws, and the stage and build go FAILURE, not UNSTABLE. That contradicts the stated rule.
  - In a CI/CD Jenkinsfile, a FAILURE also skips every later stage, including the organisation's deploy stages.
  - Even on the happy path, every non-merge push to a base (§5.4 admits these) holds the pipeline for up to 10 minutes of retries before any later stage runs. So does every Brain Town delay. The design never says where this stage sits relative to the organisation's existing stages.
  - Smaller point: `printf 'user = "%s:%s"'` breaks curl's config parsing if the password contains `"` or `\`.
- **Recommendation:**
  - Wrap the attempt in `catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE')` or try/catch. Treat a curl error as a retryable answer (`000`).
  - Require the stage to be the last one, or to run in `post { always { … } }`, or in a separate downstream job, so it never gates deploy.
  - Pass the credential with curl's `-u` read from a file, or escape `"` and `\` in the config line.

### The Docker test stage as written likely fails to build Go, so every merge would become a `~build` defect - Severity: Major
- **Affected scope:** §11.1, the template (`agent { docker { image 'golang:1.23-bookworm' } }`, no environment), and the rule "Go 1.24 and later report build errors in the JSON stream".
- **Finding and rationale:**
  - Jenkins' Docker agent runs the container as the Jenkins user's uid with `HOME` effectively `/`. The `golang` image's `GOPATH=/go` is root-owned. So `go test` usually fails with "mkdir /.cache/go-build: permission denied" or cannot write `/go/pkg/mod`.
  - The template then reports `build-failed` (status ≠ 0, no failing leaf test) on every merge: a held `~build` defect and a red "baseline".
  - The image pins Go 1.23 while the summary rule relies on Go 1.24+ `build-output` events. With `go.mod` at a newer `go` line, Go 1.23 tries a toolchain download.
  - §11 handled private modules with a separate `go mod download` step. §11.1 drops this entirely, so a repository with private modules also fails to build with no credential.
- **Recommendation:**
  - Set `GOCACHE`, `GOMODCACHE` and `HOME` under `$WORKSPACE` in the stage `environment`.
  - Derive the image tag from `go.mod`, or state that the owner must match it, and drop the 1.24 claim or pin `golang:1.24+`.
  - Add §11's private-module rule: download in a separate step with the token, and give `go test` no secret.
  - Label the whole template "verified on: (none yet)" until the first run.

### An http or over-long `runUrl` makes the whole result fail - Severity: Major
- **Affected scope:** §5.4 validation row `runUrl` ("scheme not `https`" → 422, "stores nothing"); the template's `--arg runUrl "$BUILD_URL"`.
- **Finding and rationale:**
  - `runUrl` is optional and only for display (§5.4: never put in a packet or a notice). Yet an `http://` `BUILD_URL` makes every result answer 422. Internal Jenkins instances very commonly run with an http root URL such as `http://jenkins:8080/`.
  - The template marks the build unstable, the watch ends `no-ci` six hours later, and a real failure is never filed.
  - The design already drops (not refuses) a `runUrl` hit by `screenBody`. The asymmetry has no stated reason.
- **Recommendation:** Treat every `runUrl` defect (scheme, length, parse, userinfo) as "dropped, stored empty", like the screening case. Keep 422 only for fields that carry the result. Update TC-874.

### A late or re-run result is silently accepted and discarded - Severity: Major
- **Affected scope:** §5.4 state check 6 (`done` → `200 {"accepted":true,"duplicate":true,…}`), §5.5 step 3.
- **Finding and rationale:**
  - Under `jenkins`, GAOS has the result in hand. The 6-hour cut-off was a GitHub polling budget, but it now also discards a pushed `failed` that arrives after the watch ended `no-ci` (a long Jenkins queue, an outage, a slow agent).
  - Jenkins is told `accepted: true`, so the build stays green, and nobody learns that a real failure was dropped.
  - The same applies to a "Replay" of the base build after a flake. The newer result is ignored with a 200. This is defensible for parity with GitHub's done watches, but it is undocumented.
- **Recommendation:** Answer `409 expired` (or `accepted:false`) when the stored outcome is `no-ci` or `inconclusive` and the posted result differs, so the stage marks the build unstable. Better, accept a result over a `no-ci` watch within, say, 24 h, through `FinishUnitWatchFrom`. Either way, state the replay rule in §11.1 and the docs.

### The poller sends a false "no result from Jenkins" line when the route wins the race - Severity: Nit
- **Affected scope:** §5.5 step 3.
- **Finding and rationale:** The existing pattern calls `unitFinish` and then `unitDaily` unconditionally (`unitpoll.go:224-225`). With a second writer (the route), the poller can read a pending watch, lose the race to a posted result (`FinishUnitWatch` returns an empty Result for a done watch, `unitdefect.go:450-451`), and still send "no unit-test result from Jenkins".
- **Recommendation:** Send the daily line only when the finish actually changed the watch (`len(res.UnitWatches) > 0`). Add this case to TC-877.

### The Jenkins branch of the poller is gated by the budget it claims not to spend - Severity: Nit
- **Affected scope:** §5.5 ("handled in the same per-product loop … before the budgeted ones").
- **Finding and rationale:** The per-product loop runs only `if budget > 0` (`unitpoll.go:75`). Once the GitHub products have spent the 10 reads, no due `jenkins` watch of a later product is handled in that pass. Their due watches also move `p.unitLast` (`:97-101`), which shifts the round-robin.
- **Recommendation:** Say that the `jenkins` due watches are handled outside the `budget > 0` guard and do not set `unitLast`.

### `kind-warn` has no colour outside `.kind-row`, so "app.css is not changed" is wrong - Severity: Nit
- **Affected scope:** §4.3 (the credential line "in the `kind-warn` colour"; "No new class"); §12.1 ("`static/app.css` is **not** changed").
- **Finding and rationale:** The only rule is `.kind-row .kind-warn{grid-column:1/-1;margin:-8px 0 0;color:var(--block)}` (`app.css:1724`). In the new `<div data-unit-src="jenkins">` the line renders as plain text.
- **Recommendation:** Wrap the line in a `kind-row`, use the `af-note` element, or add one rule. Correct §12.1 either way.

### `afUnit` cannot compute `RunLink` with the stated signature - Severity: Nit
- **Affected scope:** §4.3 ("`afUnit` gains the argument `jenkinsReady bool`"), §12.1.
- **Finding and rationale:** `afUnit` is a free function (`autofix_view.go:973`). `RunLink` needs `server.jenkinsOrigin`, which the change list never passes in.
- **Recommendation:** Give `afUnit` the origin as well (for example `afUnit(pr, m, ws, jenkinsReady bool, origin string)`), or compute `RunLink` at the caller (`:660`).

### Rev-14 anchors that drifted - Severity: Nit
- **Affected scope:** §5.4 Screening, §4.3.
- **Finding and rationale:** Against `7f63709`:
  - `stripBR53` is at `defects.go:270`, not `:251`.
  - `contextPathOK` is at `defecttriage.go:264`, not `:262`.
  - The delegated `change` listener is at `app.js:79-83`; `:84-91` is `clearSaved`.
  
  §0 says the rev-14 sections anchor at v0.67.0.
- **Recommendation:** Re-anchor them.

### The change list leaves items undecided or missing - Severity: Nit
- **Affected scope:** §12.1 last row ("`toc.js` (if a new anchor is listed)"); `deploy/stacks/compose.prod.yaml` row.
- **Finding and rationale:**
  - `pustaka/docs/assets/toc/integrations.js:89-90` describes `release-b` and `release-b-unit` as reading "the repository's own CI check … with the workflow template". That description becomes incomplete, and the file is not named in §12.1. "If" is a decision left open (README rule 6).
  - `deploy/check-stacks.sh:207-224` pins every prod environment pass-through. `GAOS_JENKINS_ORIGIN` gets none.
- **Recommendation:** Name `toc/integrations.js` with its new text, and add a `has $PROD 'GAOS_JENKINS_ORIGIN: "${GAOS_JENKINS_ORIGIN:-}"'` line to `check-stacks.sh`.

### The summary byte budget can exceed 2 KiB after JSON encoding - Severity: Nit
- **Affected scope:** §11.1 (`head -c 1800`, "assumption: mostly ASCII").
- **Finding and rationale:** `head -c` can split a multi-byte rune, and jq replaces invalid bytes with U+FFFD (3 bytes). A summary can then cross 2,048 bytes and be refused, which leaves the build unstable and the watch ending `no-ci` with nothing filed.
- **Recommendation:** Have the route cap the summary (`capBytes`, `unitSummaryMax`) instead of refusing it. Alternatively, cut on a rune boundary in the template (`iconv -c` or `jq` with `.[:1800]` on the string).

### Unlabelled assumption, and "step" versus "stage" - Severity: Nit
- **Affected scope:** §0 Rev 14 ("the staging callback it already calls"), §5.3 and §5.5 ("Jenkins step"), §4.3 and §11.1 ("Jenkins stage").
- **Finding and rationale:**
  - That the target organisation's Jenkins already calls the staging callback is not anchored. It is an assumption about production, and it should be labelled as one (README rule 2).
  - The notice text says "step" while the page and docs say "stage" for the same thing.
- **Recommendation:** Label the assumption, and use "stage" everywhere.

---

**Sections that hold up:**
- §5.4 state checks and outcome mapping: they reuse `FinishUnitWatch` correctly, and the poller-race re-read matches `unitdefect.go:450-451`.
- §10.1 registers: the next free numbers are correct; the BR-52 and BR-55 reasoning matches `brd.html:1352`, `:1355` and `main.go:634-637`.
- §9.1 counts are correct.
- §13 gives a real reason for rejecting the early-result store.

Counts: B=0 M=6 N=8
