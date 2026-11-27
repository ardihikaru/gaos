# Readiness review: `docs/designs/release-b4-unit-tests.md` rev 14 (§0–§14, §26)

**Paraphrase.** Rev 14 adds a product setting, `UnitChecks.Source`, that is either `github-actions` or `jenkins`. With `jenkins`, the organisation's Jenkins posts each merge build's result to a new route, `POST /api/v1/products/{id}/unit-results`. That route uses the staging callback's Basic credential gate, moved into a shared `jenkinsGate`. The result ends the pending watch for that merge commit through the existing `FinishUnitWatch` path, so the outcomes, defects, hold and notices from rev 13 do not change. The poller does not read GitHub for a `jenkins` watch: it wakes the watch once at `MergedAt + 6 h` and ends it `no-ci`. The Auto-fix page gets a source selector, a block for each source, a "Before you switch this on" checklist and a link to the Jenkins build. The work ships in four phases, J1 to J4, with TC-871 … TC-879, schema 22 and D-106, and amends BR-65 and BR-52.

**What checks out.** The code anchors for rev 14 are mostly correct. I checked these against the code: `staging.go:197-230`, `api_v1.go:442-444`, `unitchecks.go:64/88/100/123-141/466`, `unitdefect.go:35/38/111-114/443-491`, `unitpoll.go:22/25/48-50/74-96/109/111-113/126/228-240/276-288/297-348/415/447-449/463-470`, `autofix_view.go:127/660/973-999`, `backoffice.html:1258-1288`, `compose.prod.yaml:176` and `brd.html:1218/1310/1352/1355/1524`. The next free identifiers are correct too: D-106, TC-871, schema 22, with `schema/22-*` absent today. The TC count works out: 2 + 4 + 1 + 2 = 9, the test files hold 5 + 3 + 1, and the totals go 733 → 742 and 708 → 717. The state-check table in §5.4 and the outcome mapping can be decided as written. The `FinishUnitWatchFrom` approach leaves the poller's existing calls unchanged.

---

### The release's version bump breaks J4's exit gate, and `toc.js` is listed only conditionally - Severity: Major
- Affected scope: §12.1, last row (`pustaka/docs/assets/toc.js (if a new anchor is listed)`); §9.1 J4 ("changelog v0.68.0"); the change list as a whole.
- Finding: Releasing v0.68.0 requires bumping `semver` in `pustaka/docs/assets/toc.js:22`, because `deploy/check-workspace.sh:138,149` holds `README.md` to "Product version $semver". Once `toc.js` says 0.68.0, `TestStaticAssetsAreVersionedByTheProductVersion` (`dashboard/external_inbox_test.go:893-915`) fails unless `dashboard/templates/page.html:48,50,51` (`?v=0.67.0` on `app.css`, `app.js` and `live.js`) is bumped too. `page.html` is not in §12.1. J4 changes `static/app.js`, so without the bump browsers keep the cached `app.js` (a week's cache) and the source toggle never arrives. The implementer must either edit a file outside the allowlist or ship with a failing exit gate. Separately, "(if a new anchor is listed)" leaves open whether `toc.js` is in scope at all.
- Recommendation: In §12.1, list `pustaka/docs/assets/toc.js` (`semver: "0.68.0"`) unconditionally. Add `dashboard/templates/page.html` (three `?v=0.68.0`) and `README.md` "Product version 0.68.0". Name all three in J4's Builds column.

### §5.5 does not say whether a `jenkins` product's due watches run when the pass budget is spent - Severity: Major
- Affected scope: §5.5, first paragraph ("handled in the same per-product loop (`unitpoll.go:74-96`) before the budgeted ones"); TC-877.
- Finding: In `unitPass` (`unitpoll.go:75`), the whole per-product read sits inside `if budget > 0 { … }`. The budget is per pass and shared across products, and each product has a single source, so "before the budgeted ones" has no clear meaning inside one product. Two implementers could reasonably:
  - keep the `budget > 0` guard, so a `jenkins` product after budget-heavy GitHub products waits for later passes; or
  - bypass the guard for `jenkins`.

  It is also unstated whether handling `jenkins` watches moves `p.unitLast`, which shifts the round-robin, and whether they count toward the returned `read`. TC-877 asserts "no budget" but gives no observable to test it against.
- Recommendation: State it exactly. For example: "for a product whose `SourceName()` is `jenkins`, the due watches are handled whatever `budget` is left, do not decrement it, do not count in `read`, and do not set `unitLast`." Make TC-877 seed more than 10 due GitHub watches on an earlier product and assert that the `jenkins` watch still ends in the same pass.

### The Jenkinsfile assumes `GIT_COMMIT` is set in a stage that never checks out code - Severity: Major
- Affected scope: §11.1 rules 1 and 7, and the Groovy template (`stage('report to GAOS')`, `--arg commit "$GIT_COMMIT"`).
- Finding: Under top-level `agent none`, the report stage has no agent and allocates `node('gaos-report')` inside `script` without `checkout scm`. Declarative sets the `GIT_*` variables from a stage's own automatic checkout. Whether `GIT_COMMIT` is present in a stage that never checked out is exactly the case the design's own "agent none" assumption flags as unverified, but this dependency is not listed. If it is empty, every post fails validation (`commit` not 40 hex → 422), the stage marks the build unstable and does not retry, and every merge ends `no-ci`. That is a silent total failure of the feature.
- Recommendation: Capture the commit in Groovy, outside any `sh` the code under test can touch, in the test stage: `script { env.UNIT_COMMIT = env.GIT_COMMIT }`, or a pipeline-level `def`. Use that value in the report stage. Add the first-run check "the posted commit equals the merge commit" to §0 *Success*.

### The template fails the build when GAOS is unreachable, which contradicts "it never fails the build" - Severity: Major
- Affected scope: §11.1, last rule, and the template (`code = sh(returnStdout: true, script: '''… curl -sS …''')`).
- Finding: When curl cannot connect (GAOS down, DNS, TLS), `-w '%{http_code}'` prints `000` and curl exits non-zero. Without `returnStatus` or a guard, that `sh` step throws, so the stage and the merge build go to FAILURE and the retry loop is abandoned. A non-zero exit from `jq` in the body-building step does the same. The rule states the opposite: any other answer marks the build unstable and never fails it.
- Recommendation: End the curl script with `|| true` (the code is then `000`), or wrap each attempt in `catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE')`. State explicitly whether `000` (and a 502/503 from the edge during a GAOS deploy) is retried like 404/429 or ends unstable.

### A non-HTTPS `BUILD_URL` or an over-long test name rejects the whole result - Severity: Major
- Affected scope: §5.4, validation table rows `runUrl` and `failing`; §11.1 template (`--arg runUrl "$BUILD_URL"`, no length cap on `test` or `file`).
- Finding: `runUrl` is documented as optional and used only for display. Yet "scheme not `https`" returns 422 for the whole body, and the template always sends `$BUILD_URL`. An internal Jenkins whose configured root URL is `http://…` is common, and it would have every result refused, the build marked unstable, no retry, and every merge ending `no-ci`. The design already treats a screening hit on `runUrl` as "dropped (stored empty), not refused", so the two rules are inconsistent. The same whole-body refusal applies to any `test` or `file` over 512 bytes, which the template never truncates.
- Recommendation: Treat a `runUrl` that fails the scheme, host, userinfo or length checks the same as a screening hit: store it empty and accept the result. Have the template drop entries whose `test` or `file` exceeds 512 bytes, or cut them with `.[0:512]`, so one long name cannot lose the result. Add both cases to TC-874/TC-875.

### `toc/integrations.js` is dropped from the rev-14 change list - Severity: Nit
- Affected scope: §12.1, last row.
- Finding: `pustaka/docs/assets/toc/integrations.js:89-90` describes `#release-b-unit` as "the check GAOS reads … and the workflow template". After rev 14 that text is incomplete. Rev 13's §12 listed `toc/{product,integrations}.js`; rev 14 lists only `toc/product.js`.
- Recommendation: Add `toc/integrations.js` with the new text, including whether `#release-b-unit-jenkins` and `#release-b-unit-short` get their own entries.

### The `kind-warn` colour has no style outside `.kind-row` - Severity: Nit
- Affected scope: §4.3, "Source-specific fields" (the credential line "in the `kind-warn` colour"); §12.1 ("`static/app.css` is **not** changed").
- Finding: The only rule is `.kind-row .kind-warn` (`app.css:1724`). A `kind-warn` line inside `<div data-unit-src="jenkins">` renders in the plain colour, so the two statements cannot both hold.
- Recommendation: Either wrap the line in a `kind-row`, or allow one `app.css` rule and say which.

### `afUnit` gains only `jenkinsReady`, but `RunLink` needs the origin - Severity: Nit
- Affected scope: §4.3, "The last result's build link"; §12.1 `afUnit(…, jenkinsReady)`.
- Finding: `afUnit` is a free function (`autofix_view.go:973`) and cannot reach `server.jenkinsOrigin`, yet `RunLink` must compare against it.
- Recommendation: Give the signature as `afUnit(pr, m, ws, jenkinsReady bool, jenkinsOrigin string)`, or compute `RunLink` at the caller (`autofix_view.go:660`).

### How the handler detects an absent `source` is unspecified - Severity: Nit
- Affected scope: §4.1, Route ("absent reads as the stored source"); §12.1 `handlers_autofix.go`.
- Finding: `PostFormValue` returns `""` both for an absent field and for an empty one, and `""` is itself a valid source that means `github-actions`. The credential refusal also needs the resolved source. It is not said whether the handler tests `r.PostForm["source"]` for presence and reads `s.store.Master` itself, or whether `SetUnitChecks` resolves it.
- Recommendation: Say "presence is `_, ok := r.PostForm["source"]`; when absent the handler reads `s.store.Master(id).UnitChecks.Source` before the credential check."

### J4 must also edit `check-workspace.sh:131` - Severity: Nit
- Affected scope: §9.1, count paragraph.
- Finding: `deploy/check-workspace.sh:131` pins "One hundred and four entries". The paragraph lists `check-workspace.sh` only for the Go-test number and the case words, so J4's exit fails until line 131 also says "One hundred and five entries".
- Recommendation: Add it to the count paragraph. Also give the intermediate after-counts for J1, J2 and J3 (for example 735 / "eight hundred and seventy-two" after J1), since every phase exit runs the check.

### Some rev-14 anchors are stale against v0.67.0 - Severity: Nit
- Affected scope: §5.4, "Screening".
- Finding: `stripBR53` is at `defects.go:270`, not `:251`. `contextPathOK` is at `defecttriage.go:264`, not `:262`. The header says the rev-14 sections anchor against `7f63709`.
- Recommendation: Update the line numbers.

### TC-879 does not say how the sample body is extracted - Severity: Nit
- Affected scope: §9.1 TC-879; §12.1 `agents.html` row.
- Finding: "Extracted from the page" does not name a marker. Two implementers would use different selectors, and the docs author does not know which element the test depends on.
- Recommendation: Name the element, for example `<pre id="unit-results-sample">` under `#release-b-unit-jenkins`, read from `../pustaka/docs/integrations/agents.html`. State the fixture the test seeds: p-9, a pending watch at that SHA on `develop`, `jenkins`, and a credential.

### Template details - Severity: Nit
- Affected scope: §11.1 template.
- Finding:
  - The image is `golang:1.23-bookworm`, but the build-failure summary relies on Go 1.24+ `build-output` events, and §11 uses `go-version-file: go.mod`.
  - Neither `when` has `beforeAgent true`, so the Docker agent starts for every branch before being skipped.
- Recommendation: Either derive the image from `go.mod` or note that the owner must match it. Add `beforeAgent true`.

### Two names for the Jenkins piece in the user-facing text - Severity: Nit
- Affected scope: §5.3 table row and §5.5 step 3 ("Jenkins step") against §4.3 and the docs ("Jenkins stage").
- Recommendation: Use one term.

### The checklist wording and the field order are left open - Severity: Nit
- Affected scope: §4.3, `{{.BaseList}}` and "Source-specific fields".
- Finding:
  - The `BaseList` string is described, not given. It is unclear whether a set `AutoFix.BaseBranch` is named, and how the literal joins with the configured `Bases`.
  - The position of the `jenkins` block relative to the switch is not stated, while §4.3 says gating state goes "before the switch it gates".
- Recommendation: Give the exact `BaseList` output for the three cases (base set; base empty; plus extra bases). Give the field order.

### `Total`'s `kept` is ambiguous - Severity: Nit
- Affected scope: §5.4, "`Total = min(max(total, kept), 100000)`".
- Finding: Up to 50 entries may be kept before the 10-entry cut, so `kept` could mean the count before the cut (≤ 50) or after it (≤ 10).
- Recommendation: Say which.

### Register text not named in the change list - Severity: Nit
- Affected scope: §12.1 docs rows.
- Finding: Two existing lines go stale and are not named: the `#go-unit-checks` heading "(TC-853 … TC-870)" at `qa-test-plan.html:4705`, and the status row at `agents.html:449` ("Nothing: GitHub Actions runs the repository's own workflow").
- Recommendation: List both.

Counts: B=0 M=5 N=12
