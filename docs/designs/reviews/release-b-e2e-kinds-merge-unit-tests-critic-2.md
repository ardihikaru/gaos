# Goldfish critic review: `release-b-e2e-kinds-merge-unit-tests.md` (rev 2)

The strongest case against the thesis: rev 2 handles the merge question well. GAOS keeps no merge power, and BR-61, BR-08's GAOS side and the out-of-scope line all stay intact. But that work moved the risk into Part 4 and the store hooks, and both lean on "D-091 runs unchanged" and "the runner drives it like an e2e card". Neither holds against the code. The unit defect reaches its triager with no source code. The `unit:qa` card cannot pass through the runner as written. Several caps, dedup rules and register amendments are missing or undecided.

### A unit defect's triage gets no source code: "D-091 runs unchanged" is a wrong premise - Severity: Blocker
- Affected scope: §5.3 ("the triager reads the source slice (`buildDefectContext`)"), §10 row 4.
- Finding and rationale: `buildDefectContext` (`dashboard/defecttriage.go:294-418`) finds files only by searching `d.Operations` (`:314-337`). It fetches only those hits, at the default branch's head (`:308`), not at the merge SHA. §5.3 sets no `Operations` on a unit defect, only `Files`. So the context is empty (`Built: true`, no files), and the triager judges "easy" with nothing to read.
  - `contextClass` (`:278-288`) is Go-only: `Map.test.tsx` ranks with the "everything else" class.
  - The precedent the design should have followed is the review defect. It builds its own slice from `Files` at a pinned SHA (`buildReviewContext`, `dashboard/reviewdefect.go:319-345`) and stores it on the record (`:173`).
- Recommendation: give unit defects their own context builder. It should read the failing test files plus their source counterparts at the merge SHA, with the same caps as `buildReviewContext`, and store it in `PlanUnitDefect`'s `Context`. Add `defecttriage.go` and `reviewdefect.go` to the B4 change list, and add a test proving that a held unit triage, once released, has non-empty `Context.Files`.

### The capability is read at job start (§2.3) but must be known at planning (§5.2), and the planner has no QA client - Severity: Major
- Affected scope: §2.3 "read when a job starts"; §5.2 "When a `pr.merged` card is planned … and the tool lists `unit`".
- Finding and rationale:
  - `prCardsFor` runs under the store lock inside `PlanPullRequestCards` (`dashboard/prflow.go:218`).
  - `prFlow` holds only `st`, `gh` and `ctx` (`prflow.go:392-398`), with no `qaOutbound`.
  - §2.3 makes the capability cache a job-start concern.
  - Nothing says who reads `unit` before planning, how the result reaches `prCardsFor(ev, m)`, or what happens on a cold cache (an empty set at boot after an error, so `unit:test` is planned and that card keeps its driver for life).
  - §8 says GAOS "will not enable the switch without the `unit` capability", but `validateAutoFix` (§5.2) checks only the confirmation.
- Recommendation: give `prFlow` the capability reader, pass a `unitViaQA bool` into `PlanPullRequestCards`, and state the cold-cache behaviour. Make §8 and §5.2 say the same thing: the capability is a planning-time check, not a validation-time one.

### "Unit tests failed" and "the unit job failed" are not told apart - Severity: Major
- Affected scope: §5.2 Outcomes; §5.1 report shape.
- Finding and rationale:
  - The runner's only outcome split is `qaJob.final()` (`dashboard/qajobs.go:49-57`): a `failed` status leads to `r.fail(... "qa-job-failed")`, which marks the card Failed and sends an `e2e:failure` notice (`:337-339`, `:402-407`).
  - §5.2 wants a test failure to end Done and plan a defect, but never says whether the QA tool reports failing tests as `succeeded` with `failed>0` or as `failed`.
  - If it is the latter, sandbox crashes, timeouts and checkout errors would become defects. Or real failures would become Failed cards with no defect.
  - `qaJob` (`:32-42`) has no `tests`, `failed` or `failures` fields.
- Recommendation: fix the contract. Test failures are `status: succeeded` plus `result.failed>0`, and only `status: failed` means the job broke. Extend `qaJob` with a strictly bounded `result`, and plan a defect only from a succeeded job with `failed>0`.

### The runner cannot drive a `unit:qa` card as specified - Severity: Major
- Affected scope: §5.2 ("`isE2ECard` becomes `isQACard`"); §7 B4.
- Finding and rationale: `start` routes every card through `caseCodeFor` and `startAutomation` (`qajobs.go:283-290`).
  - For a unit card `caseCodeFor` returns `""`, so the card goes Blocked with "no QA-case line" (`:284-288`).
  - `recordQARow` writes only `PRKindQA` rows, which `prRepos` limits to `AutoFix.AutomationRepos` (`dashboard/pullrequests.go:90-95`). The `qa-unit/*` draft on the product repository is therefore refused and never shown.
  - The card's role is unstated. Kept as `unit-test`, the gate demands a unit-test agent nobody uses (`prflow.go:352-371`). Also, `main.go:473` sends `unit:failure` for a Failed `unit-test` card while `r.fail` sends `e2e:failure`: two notices, one of them the wrong kind.
  - §5.3 says the `unit:failure` notice "still goes out", but a failed run ends **Done**, and the existing hook fires only on Failed.
- Recommendation: specify a unit branch in `start`/`poll` (`startUnit`, its own result handling and row kind), the card's role and gating, and exactly which notices go out on Done-with-failures and on job failure. Add `main.go`, `notify.go` and `pullrequests.go` to B4.

### The 40-minute active timeout is shorter than one unit job's own budget - Severity: Major
- Affected scope: §5.1 (10-minute run limit, "up to 3 repairs"); `qaJobTimeout`.
- Finding and rationale: `qaJobTimeout = 40 min` of active time (`qajobs.go:157`, enforced at `:371-375`). A unit job with `create_for` runs the existing tests (≤10 min), generates tests, runs them (≤10 min) and repairs up to 3 times (≤30 min). That can reach 50 minutes plus model time before it waits for approval, so GAOS would cancel healthy jobs as `qa-job-timeout`.
- Recommendation: give `unit:qa` its own active timeout derived from the stated limits (for example 75 minutes), or have the QA tool count repairs against one total, and state it.

### A unit card planned through the machine API breaks once the switch is on - Severity: Major
- Affected scope: §5.2; §7 B4 (no `api_cards.go`).
- Finding and rationale: `apiSpec` calls `prCardsFor` for `APIKindUnitTest` with a synthesised `prTypeMerged` event that has no commit (`dashboard/api_cards.go:143-149`, `:181-189`). Under §5.2 that card becomes `unit:qa`. It has no `merge_commit_sha`, because `prFlow.handle` never sees it, so it fails `unit-no-merge-sha` every time.
- Recommendation: decide one of two options and list `api_cards.go` in B4:
  - API unit cards stay `unit:test`.
  - The API path looks up the merge SHA itself.

### A person can flip the driver of a `unit:qa` card by setting `unit:test` - Severity: Major
- Affected scope: §5.2 ("family `unit`, planner-only"; "keeps one driver for its life").
- Finding and rationale: `SetTaskLabel` replaces the label in the same family (`prflow.go:555-568`). `unit:qa` and `unit:test` share the family `unit`.
  - If only `unit:qa` is refused, setting `unit:test` on a `unit:qa` card swaps it. The scheduler then dispatches it to an agent, because `unit:test` is not in `labelsAllowDispatch` (`dashboard/labels.go:167-176`).
  - If the whole family is refused, `unit:test` stops being hand-settable, which is a behaviour change the design does not state.
- Recommendation: refuse any `unit:*` change on a card that carries `unit:qa` (and the reverse), and test it.

### A staging create and a pull-request create can target the same case - Severity: Major
- Affected scope: §3.1 step 3 ("a create card for it … blocks it").
- Finding and rationale:
  - A `pr.opened` `e2e:automation` card does not carry its case in the key. The case is read from the body at start (`qajobs.go:241-254`).
  - The blocking scan therefore cannot see it, and a deploy can plan `staging-create/…:<case>#1` while a PR-driven create for the same case is open.
  - The QA tool pushes `qa-auto/<code-lower>` and never forces (§1, `QA:` assumption), so the second job fails or collides. Each one also costs a person an approval.
- Recommendation: let the scan count any open `e2e:automation` card whose tracked or recorded case (the `QACase` on its Pull requests row, or the case code recorded at start) equals the case. Or record the case on the card at start and scan that.

### The merge-to-catalog window re-plans a create for a case that was just automated - Severity: Major
- Affected scope: §3.1 steps 2–3.
- Finding and rationale: once the spec merges, step 3 stops blocking, because the Done card's row is no longer open. Step 2 relies on `QaCase.Implemented` (`dashboard/qacatalog.go:97`), which only the QA tool's sync writes (BR-51). Nothing states when `Implemented` flips (at PR open, at merge, or at the next pull). A deploy in that window plans a duplicate create, a duplicate approval and a duplicate PR.
- Recommendation: also block on a Done create whose row is **merged**, for a stated period or until the catalog shows `Implemented`. Or state, as a `QA:` assumption, that the QA tool sets `implemented` before the job succeeds.

### An open spec pull request blocks its case for ever, without telling anyone - Severity: Major
- Affected scope: §3.1 step 3 ("Done while its Pull requests row is still open"); §4.1.
- Finding and rationale:
  - With auto-merge, a failing required check leaves the PR open indefinitely (§4.1, "never when they fail").
  - `pollRow` stops reading a row after 90 days (`dashboard/fixprpoll.go:327-330`), and skips it once the repository leaves `AutomationRepos` (`:324`), so the row stays `open` in GAOS for good.
  - Either way the case is never created again, and no card or notice says so.
- Recommendation: bound the block (for example, a row unchanged for N days no longer blocks), and surface a spec PR whose checks failed. At minimum, add a Pull requests filter and document the behaviour.

### Create levels are per product, not per repository, so a single full-stack repository never gets API creates - Severity: Major
- Affected scope: §2.1 table; §3.1 step 2; the BR-54 amendment text ("the deployed repository's uncovered cases").
- Finding and rationale:
  - `QaCase` has no repository field (`qacatalog.go:81-108`), so a `be` deploy plans creates for every API case of the product, whichever service it tests. The amended BR-54 wording is therefore false.
  - `fullstack` creates only `WEB`/`E2E` (§2.1). A product whose one repository holds both frontend and backend never gets an API case created, although its runs include API cases.
  - The Route Planner example hides both problems.
- Recommendation:
  - Say plainly that the selection is product-wide by level.
  - Decide whether `fullstack` creates `API` too, at least when the product has no `be` repository.
  - Correct the BR-54 text to "the product's uncovered cases of the deployed repository's kind".

### Unit defects have no cap and no dedup, and they blame the merged pull request for failures that were already there - Severity: Major
- Affected scope: §5.3 (key `…@<sha12>`, title "Unit tests failed after <owner>/<repo>#<n>").
- Finding and rationale:
  - The run executes all existing tests at the merge SHA, so a suite already red on the branch produces one new defect, one held triage card and one notice per merged PR.
  - BR-53's 50-new-keys-a-day limit applies to the defect route (`brd.html:1349`), which this path bypasses. The design states no cap of its own.
  - The title attributes failures that may predate the PR.
- Recommendation: dedup by repository plus a stable hash of the failing test names (or reopen the open unit defect of that repository), add a daily cap, and word the title as "Unit tests failing at <sha12> (after #n)".

### Model-written tests reach the product's CI on a same-repository branch, which `CIConfirmed` was built to guard - Severity: Major
- Affected scope: §5.1 (draft PR on `qa-unit/<sha12>`); §8.
- Finding and rationale:
  - A branch in the product repository triggers `push` and `pull_request` workflows with that repository's secrets. This is the OQ-40 concern (`pustaka/docs/product/open-questions.html:940-944`), which GAOS's own fix branches answer with `CIConfirmed`.
  - The tests are model-written from product code, an injection surface, and CI will execute them.
  - Rulesets limit the branch, not the paths written. Nothing in §5.1 forbids `.github/workflows/**` beyond "test files only", which is asserted but not enforced.
  - `UnitSandboxConfirmed` covers the QA tool's sandbox, not the product's CI.
- Recommendation: require `CIConfirmed` (or an equivalent) for `UnitTestsViaQA`, and require the QA tool's identity to lack the `workflows` permission. Make "test files only" a checked precondition (paths must match `TestGlobs`), not prose.

### BR-54 is not amended for the post-merge run card - Severity: Major
- Affected scope: §4.2; §6 (D-103's BR-54 text covers staging creates only).
- Finding and rationale: BR-54 says QA data adds at most one card, the epic card born Blocked (`brd.html:1350`). The `qa-merged/` card is planned because a pull request the QA tool opened merged, and it is planned runnable. D-103's amendment text names only the staging callback's creates.
- Recommendation: extend the D-103 amendment of BR-54 to "…and a QA spec pull request's merge may plan one single-case run card".

### BR-08's Minimum-band ban on auto-merge is dropped without being addressed - Severity: Major
- Affected scope: §0 item 1; §6 ("BR-08 … unchanged").
- Finding and rationale:
  - BR-08: "*Minimum* … forbids auto-merge" (`brd.html:1240`).
  - D-102 (5) itself listed "refused in the Minimum band" as a required guard and made BR-08's amendment a precondition (`decision-log.html:2144`).
  - Rev 2 has the QA tool enable GitHub auto-merge for every product's automation repository, whatever its band, and declares BR-08 unchanged on the ground that GAOS does not press the button.
  - The work is planned by a GAOS card (the create), so the band's intent is bypassed by moving the merge one hop away.
- Recommendation: either state, in D-103, why BR-08 does not apply to spec PRs in the automation repository (an amendment, not "unchanged"), or have GAOS pass the band in the create request and have the QA tool refuse `--auto` for Minimum.

### A unit defect carries `be` without amending D-091 (8) - Severity: Major
- Affected scope: §5.3 (`AutomationType` `be`).
- Finding and rationale:
  - Auto-dispatch of a fix requires `AutomationType == "be"` (`dashboard/autofix.go:175-177`, `defecttriage.go:745`).
  - D-102 had to amend D-091 (8) explicitly to let review defects carry `be` (`decision-log.html:2144`, point 4).
  - A unit defect on `acme/route-web` (`Map.test.tsx`) labelled `be` gets around the backend-only gate the same way, and D-104 does not amend D-091 (8).
- Recommendation: add "amends D-091 (8)" to D-104 with the stated bound (`AllowedGlobs` and `Files`), or use `e2e` for frontend repositories so the existing gate applies.

### The cap for `qa-merged/` run cards is undecided - Severity: Major
- Affected scope: §4.2 ("outside `GAOS_PR_MAX_OPEN` … counted with the creates' cap"); §3.1 step 4.
- Finding and rationale:
  - If `qa-merged/` cards count towards `GAOS_E2E_CREATE_OPEN`, nothing says what happens when the cap is full at merge time: is the run dropped, deferred or planned anyway?
  - Planning happens inside `AdvancePullRequest`, which runs once per state change (`dashboard/pullrequests.go:206-231`; later polls see `merged`, which `prFinal` stops polling at `fixprpoll.go:320`).
  - A dropped run therefore never comes back. The same applies when the `qa` role is unassigned.
- Recommendation: decide this. For example, merged runs are never capped, or a refused merged run is planned Blocked with a reason. Add a test for each branch.

### OQ-42 is answered for a surface it does not ask about - Severity: Nit
- Affected scope: §3.1 last paragraph; §6.
- Finding and rationale: OQ-42 asks whether an **outside client** may create an automation from a case code alone (`open-questions.html:954-957`). Staging creates are internal planning, so "moving" OQ-42 would mark the API question as answered when it is not.
- Recommendation: leave OQ-42 open and cite it as related.

### `RepoKinds` lookup key vs `ev.Repo` - Severity: Nit
- Affected scope: §2.2 ("the kind label of `ev.Repo`").
- Finding and rationale: `PREvent.Repo` is the bare name, and the owner is `ev.Org` (`staging.go:259`). `RepoKinds` is keyed by lower-cased `owner/repo`.
- Recommendation: say "of `lower(ev.Org+"/"+ev.Repo)`".

### Anchor drift - Severity: Nit
- Affected scope: §1.
- Finding and rationale: `recordQARow` is at `qajobs.go:412`, not `~395`. The `QA-case:` read is at `:241-254`, not `:241-260`. `qaRunPrefix` is defined at `api_cards.go:153`; `qajobs.go:235` only uses it.
- Recommendation: re-anchor without the tilde.

### The label count is phrased as the end state in §2.2 - Severity: Nit
- Affected scope: §2.2 "(labels 17 → 19)" vs §6 "17 → 20".
- Finding and rationale: both are right per phase (`labels.go:87-105` has 17), but §2.2 reads as the end state.
- Recommendation: write "17 → 19 in B1; 20 after B4".

### The waiting filter also starts showing design-gate blocks - Severity: Nit
- Affected scope: §5.3 last sentence.
- Finding and rationale: `operatorHolds` also returns true for `ReasonDesignBlocked` and `ReasonDesignUnreviewed` (`dashboard/sendback.go:153-159`). Today's `waitsOnAPerson` shows only Awaiting input and Review (`dashboard/board_page.go:324-326`). The change pulls every design-gate block into the filter, which is probably right but is not stated.
- Recommendation: say so, and add it to the test.

### The new key prefixes are not excluded in `prKeyParts` - Severity: Nit
- Affected scope: §3.1 key `staging-create/<productId>:<caseID>#<n>`.
- Finding and rationale: `prKeyParts` excludes `qa-run/` and `bundle/` explicitly (`dashboard/prreview.go:198`). The new key contains `#` and is safe only because its colon comes before the hash.
- Recommendation: add `staging-create/` and `qa-merged/` to the exclusion, as D-099 and D-100 did.

### "It" is ambiguous in the fix-target sentence - Severity: Nit
- Affected scope: §5.3 "It is planned only when the repository is in `AutoFix.TargetRepos`; otherwise the card's reason says…".
- Finding and rationale: it is unclear whether the defect, the triage card or both are skipped, and which card carries `not a fix target`.
- Recommendation: name the defect and the `unit:qa` card explicitly.

### The kind label on a `qa-merged/` card does nothing - Severity: Nit
- Affected scope: §4.2.
- Finding and rationale: a single-case `run` ignores `kind` (§2.3), so copying the create's kind label onto the run changes nothing but the board.
- Recommendation: drop it, or say it is for filtering only.

### Solid sections
- **Part 1, kinds:** checked against `labels.go`, `prflow.go:80`, `api_cards.go:159-182` and `SetTaskLabel`. The design is coherent.
- **Identifiers:** TC-819…TC-833 follow TC-818 (`qa-test-plan.html:909`). D-103/D-104 follow D-102, BR-62 follows BR-61, `schema/20-…` follows `19-review-defects.sql`, and v0.65.0 follows v0.64.0 (`changelog.html:39`).
- **Test arithmetic:** 4+4+2+5 = 15 tests, which matches.

Counts: B=1 M=17 N=8
