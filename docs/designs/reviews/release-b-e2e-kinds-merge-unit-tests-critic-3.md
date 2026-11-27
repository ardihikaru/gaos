# Design review: `release-b-e2e-kinds-merge-unit-tests.md` (rev 3)

**Overall:** parts 1–3 cannot be built safely yet: one blocker and thirteen major findings. Most of the problems are in where the design meets the existing runner and store code (`qajobs.go`, `pullrequests.go`, `store_arcade.go`), and in what the contract with the QA tool still leaves unsaid. Moving part 4 out (§5) is clean, apart from a few loose ends listed as nits.

What holds: §1 matches the code. The 17-label count is right (`labels.go:87-105`). TC-819 onward is free in `pustaka/docs/product/qa-test-plan.html`, where TC-818 is the last. D-103 is free, since D-102 is the last entry (`decision-log.html:2141`).

### Fullstack API creates contradict the kind's folder - Severity: Blocker
- Affected scope: §2.1 table and §2.3, against §3.1 step 2.
- Finding and rationale: §2.1 says a `fullstack` create may write "`tests/web/` only (`WEB`, `E2E` cases)". §2.3 says that, for `create`, the QA tool "restricts the generator to the kind's folder". But §3.1 step 2 plans `API` creates for a `fullstack` repository "when the product has no repository of kind `be`", and those cards carry `e2e-kind:fullstack`. A QA tool built to §2.1/§2.3 must refuse them. Each refusal ends the card Failed, which starts a 7-day block, so a product with one fullstack repository fails its API creates every week, forever. This document's stated purpose is to fix the contract between GAOS and the QA tool (header, "Paths"), so a contradiction inside that contract is a wrong premise for the QA tool's own design.
- Recommendation: decide one of two things.
  - (a) A `fullstack` create may also write `tests/api/rest/` for `API` cases. Change §2.1 and BR-62 to say so.
  - (b) GAOS sends `kind: be` on an `API`-level create, whatever kind the repository has.

  Then add a test for the single-fullstack-repository product to TC-824.

### `case:` in the reason does not survive later reason writes - Severity: Major
- Affected scope: §3.2 "The case is recorded"; §3.1 step 3, first bullet.
- Finding and rationale:
  - Only `start` is said to write `qa-job:<id> case:<code>`.
  - Every later reason replaces the whole text: "finished", "resumed after approval" and the approval wait all go through `qaJobReason(t.jobID, text)` (`qajobs.go:335,353-354,362`), and `qaJobReason` has no case parameter (`qajobs.go:260`).
  - The final reasons have no prefix at all: `"qa job succeeded"` (`qajobs.go:336`) and the fail classes (`qajobs.go:403`).
  - So a card that waits for approval loses its `case:`. Waiting for approval is the normal path for a create.
  - Two things then break. The blocking scan stops seeing the case of an open PR-planned card. Adoption after a restart, which typically happens while the job is waiting, cannot recover the case. The Pull requests row then gets `QACase` = "", because adoption sets `caseCode` to "" (`qajobs.go:280`).
- Recommendation: give `qaTracked` the case and make `qaJobReason(id, case, text)` write `case:` on every intermediate reason. Say what the final reasons carry. Make TC-829 restart the runner while the job is waiting for approval.

### The blocking rules miss PR-planned creates; two jobs can push one branch - Severity: Major
- Affected scope: §3.1 step 3; §11 ("not changing the `QA-case:` path").
- Finding and rationale:
  - The block on an open or merged row (bullets 2–3) only covers rows of `staging-create/` cards. A PR-planned `e2e:automation` card whose spec PR is open or merged does not block a staging create of the same case.
  - A PR-planned card that is still Pending has no recorded case yet, so bullet 1 misses it too.
  - In the other direction, `pr.opened` never checks for staging creates.
  - Two create jobs for one case both push `qa-auto/<code-lower>`, and the QA tool never force-pushes (`QA:…/worktree.go:23-38`). One job fails, or both open duplicate specs.
- Recommendation: base the block on **rows** (any `qa` row with `QACase == case`, whatever card made it), plus open cards of either origin whose case is known or can be resolved. Write down what happens to a PR-planned create that collides with an open staging create.

### `qaSha` may never be recorded - Severity: Major
- Affected scope: §4.2 first paragraph.
- Finding and rationale:
  - `recordQARow` writes the row once, on the first poll that has `pr_url`, and then sets `t.recorded = true` (`qajobs.go:413-428`).
  - `upsertPullRequestLocked` only fills empty fields, and only on the fields it lists (`pullrequests.go:130-145`).
  - The contract never says `pushed_sha` is present whenever `pr_url` is. If `pr_url` arrives one poll earlier, `qaSha` is never set. Every merge then reads "not the approved commit" and the case never runs.
- Recommendation: make it part of the contract that `pushed_sha` comes no later than `pr_url`, or keep recording until both are present. Add `qaSha` to the fill list. Test the case where `pr_url` arrives first.

### A capped run card would roll back the creates - Severity: Major
- Affected scope: §3.1 answers ("409 … creates are then still planned"); the `PlanStagingCards` signature.
- Finding and rationale: today the cap is signalled as an `error` (`ErrPRSkipped`, `prflow.go:291-293`; `staging.go:266`). `ArcadeStore.mutate` throws the Result away when there is an error (`store_arcade.go:632-635`). If the implementer follows the existing pattern, the creates are planned in the MemStore projection but never persisted, so the projection and the database disagree (BR-01).
- Recommendation: state that a capped run returns `err == nil` with a `Result.Capped` (or `RunCapped`) flag carrying the creates, and that `apiStaging` maps that flag to 409. TC-827 should restart ArcadeDB after a 409 and check that the creates survived.

### Auto-merge has no operator switch and ignores a later band drop - Severity: Major
- Affected scope: §4.1 first paragraph; BR-62 (§6).
- Finding and rationale:
  - D-102 (5) proposed the merge as "refused in the Minimum band, **off by default**" (`decision-log.html:2144`). Rev 3 drops the default-off: every product in the Working or Complete band auto-merges as soon as the QA tool lists the capability, with no per-product opt-in.
  - The band is read once, when the create job starts. The person approves, and checks pass, hours or up to 720 h later (`qajobs.go:166`). BR-10 says a downward band crossing "applies the stricter band immediately" (`brd.html:1242`), yet nothing revokes auto-merge that is already enabled.
  - BR-62 binds the QA tool, but GAOS has no means to check it.
  - §14 claims "the QA tool's auto-merge applies to every approved spec". That leaves undefined what happens to a spec approved through the QA tool's own interface, with no `auto_merge` from GAOS.
- Recommendation:
  - Add a per-product switch, off by default, recorded with the actor.
  - Specify a revoke: when the band drops, GAOS asks the QA tool to disable auto-merge on open `qa` rows. Failing that, state the residual risk in D-103.
  - State that the QA tool's default is off when no `auto_merge: true` arrives.
  - GAOS should alert when a job reports `auto_merge: enabled` but GAOS sent `false`.

### "GAOS never merges" understates GAOS's role - Severity: Major
- Affected scope: §0 item 1, §6 ("BRD scope line — unchanged"), §8.
- Finding and rationale: the BRD's out-of-scope line reads "GAOS indexes and proposes; the gate is where its authority stops" (`brd.html:1459-1460`). Under this design, GAOS's `auto_merge: true` is the deciding input: without it, nothing merges without a person. GAOS's authority therefore reaches past the gate, by instruction rather than by token. Calling the line "unchanged" hides that. D-102 (5) itself required the scope line to be amended (`decision-log.html:2144`).
- Recommendation: amend the scope line (or annotate it under D-103) to say "GAOS may ask the QA tool to enable auto-merge on its own spec pull requests, in the Working and Complete bands only". Count that amendment honestly in §6.

### `auto_merge` is sent without checking the capability, and the Done reason trusts the capability - Severity: Major
- Affected scope: §4.1, §3.2 Done reasons.
- Finding and rationale:
  - `kind` is sent only when the QA tool lists it (§2.3). `auto_merge` has no such condition. A QA tool that decodes requests strictly would reject every create, and the card would fail with `qa-start-failed` (`qajobs.go:296`).
  - The Done reason "it merges when its checks pass" depends on whether the tool **listed** `auto-merge`, not on the job's own `auto_merge` (`enabled | refused:<why> | off`). A refused or off job would tell the person something false.
- Recommendation: send `auto_merge` only when the tool lists the capability. Build the Done reason from the job's `auto_merge` value, with the refusal reason screened and bounded.

### Saving a kind forces assignment gating on - Severity: Major
- Affected scope: §2.1 editor paragraph; the B1 change list.
- Finding and rationale: kinds are posted with the assignments to `POST /agents/assign`. A post with `action` "save" sets `set = true` (`handlers_assign.go:114-122`), and `SetAssigned` then sets `AssignedSet` (`assign.go:78-79`). So an operator who only wants to set a kind on an unconfigured product also turns on gating. That changes which cards are planned and switches on `REQUEST_CHANGES` reviews (`assign.go:92`). Decision 4 ("no kind = today") does not cover this side effect.
- Recommendation: give kinds their own action (`action=kinds`) or their own route, which leaves `AssignedSet` untouched. Test this in TC-819.

### The kind label on PR-planned creates is unaccounted for - Severity: Major
- Affected scope: §2.2 (the kind appended to all `e2e:*` specs); §11 non-goal.
- Finding and rationale:
  - The kind label goes on the `pr.opened` `e2e:automation` card too, so its create is folder-restricted. Suppose a `QA-case:` line names a `WEB` case on a `be` repository: the QA tool must refuse it.
  - The design does not say what the card becomes, what reason it shows, or whether the 7-day rule applies.
  - §11 says the `QA-case:` path is unchanged, which this contradicts.
- Recommendation: either leave the kind off `pr.opened` creates, or define the mismatch outcome (Blocked with a named reason, never a silent Failed) and fix §11.

### Case IDs can contain the new keys' delimiters - Severity: Major
- Affected scope: §3.1 step 4 key; §4.2 key.
- Finding and rationale: `validCursor` accepts any printable ASCII, including space, `#`, `:`, `@` and `/`, and also the empty string (`qacatalog.go:49,418`). A case ID with `#` or `@` makes `staging-create/<p>:<caseID>#<n>` and `qa-merged/<p>:<caseID>@<pr>` ambiguous for `caseCodeFor` and for the retry counter. BR-59 requires that external values reaching the store "match a pattern that has no key delimiter" (`brd.html:1353`).
- Recommendation: require a strict case-code pattern (no delimiters, not empty) before planning. Skip non-matching cases and log the skip. Parse the keys from the last delimiter, and test a hostile case ID.

### The post-merge run assumes things about the QA tool - Severity: Major
- Affected scope: §4.2 ("so the operator's flow … completes").
- Finding and rationale:
  - The `qa-merged/` run starts within one poll of the merge: 5 minutes (`fixprpoll.go:24`).
  - Nothing says the QA tool's `run` fetches the default branch first, or that it runs a case whose catalog row does not yet show `Implemented`. §3.1 step 3 itself allows up to 14 days for that sync to catch up.
  - If either assumption is false, the run fails, and a person sees a failed e2e right after a successful merge.
- Recommendation: add both as explicit `QA:` assumptions in the contract (§9 item 2). Otherwise plan the run only once the catalog shows `Implemented`, with a stated window.

### Structural gaps in the row data the merge path needs - Severity: Major
- Affected scope: §3.1 step 3 windows; §4.2; §7 B3 change list.
- Finding and rationale:
  - **No reason field:** "the row's reason says `merged head is not the approved commit`", but `PullRequest` has no reason property (`schema/18-pull-requests.sql:9-31`).
  - **Unclear time bases:** "unchanged for less than 30 days" has no defined clock. `checkedAt` moves on every poll; `openedAt` is the only fixed time. The 14-day and 7-day windows are not anchored either: the row stores no merged or closed time.
  - **Missing from the change list:** `PRFields.HeadSHA`, which `AdvancePullRequest` needs (`pullrequests.go:206`, `fixprpoll.go:364`), and the `PullRequest.QASha` Go field.
  - **Undocumented contract change:** `AdvancePullRequest` is today a row write that emits no events. It would now plan cards. That changes its contract and is not stated.
- Recommendation:
  - Name the property that carries the skip reason, or say it goes to the log only.
  - Define each window against a named field, adding `finalAt` if needed.
  - Add the missing fields to the B3 change list.
  - Consider a separate store method for planning the post-merge run.

### The Done reason stores a URL the QA tool supplied - Severity: Major
- Affected scope: §3.2 Done reasons.
- Finding and rationale: the schema's invariant for these rows is "No url is stored: every link is rebuilt from the validated owner/repo and number" (`schema/18-pull-requests.sql:5-6`). Card reasons are shown on the board and in outbound notifications. Writing `<url>` from `pr_url` into the reason is free text from outside reaching a card.
- Recommendation: rebuild the URL from `parseGitHubPRURL` plus the product-owned check (`qajobs.go:416`, `pullrequests.go:108`). When the check fails, say "spec PR open" without a link.

### A dangling §3.3 reference - Severity: Nit
- Affected scope: §3.1 step 3, first bullet.
- Finding and rationale: it cites "recorded case (§3.3)". There is no §3.3; the text is in §3.2.
- Recommendation: point it at §3.2.

### Inconsistent anchors - Severity: Nit
- Affected scope: §1, §2.2, §7 B1.
- Finding and rationale:
  - `recordQARow` is cited as `qajobs.go:~395` in §1 and as `:412` in §4.2; the real line is 412.
  - `qacatalog.go:81-105` and `81-108` are both used.
  - The call inside `PlanAPICard` is at `api_cards.go:368`, not 369.
  - `apiSpec` today takes `productID` (`api_cards.go:171`); the move to `apiSpec(m, in)` is not called out as a signature change.
- Recommendation: re-anchor these.

### "Score after drift" names a penalty that does not exist - Severity: Nit
- Affected scope: §4.1.
- Finding and rationale: the drift penalty is not built, and effective = raw today (`model.go:603-608`).
- Recommendation: say so, and name the line that must change when drift lands.

### B2 lists a B3 key - Severity: Nit
- Affected scope: §7 B2 change list.
- Finding and rationale: the `prflow.go`/`prreview.go` row excludes `qa-merged/`, which first exists in B3. §3.1 mentions only `staging-create/`.
- Recommendation: move the `qa-merged/` exclusion to B3.

### The events are not named - Severity: Nit
- Affected scope: §3.1, §4.2.
- Finding and rationale: it is not stated whether `StagingDeployed` still fires once `PlanStagingCards` replaces `PlanPullRequestCards` (`prflow.go:317-319`), or which events announce the creates and the `qa-merged/` card. BR-05 says the event catalog is closed.
- Recommendation: state "`TaskPlanned` only, no new event type" and keep `StagingDeployed` as it is.

### Done-when B1 cannot be observed by GAOS - Severity: Nit
- Affected scope: §12 B1, against §7 exits.
- Finding and rationale: "runs only API/GQL cases" happens inside the QA tool, so GAOS cannot see it.
- Recommendation: use §7's observable exit, "`"kind":"be"` is sent".

### Part 4 leaks into this contract - Severity: Nit
- Affected scope: §2.3, §6, §14.
- Finding and rationale:
  - The capability list includes `"unit"`, which part 4 would introduce.
  - "D-104 is left for part 4" reserves an ID that another decision may take first.
  - The operator's decisions §0 items 2–3 are recorded in no register until that design exists.
  - The §14 notes cite §5.1–§5.3, which are gone.
- Recommendation:
  - Drop `unit` from the B1 capability list.
  - Don't reserve D-104.
  - Record decisions 2–3 in D-103 as "decided, deferred".
  - Mark the stale §14 pointers as historical.

### §14 overstates the merge scope - Severity: Nit
- Affected scope: the §14 row "Merge scope".
- Finding and rationale: "GAOS runs the case after any spec merge" is not what §4.2 does. Only rows GAOS recorded from its own jobs are polled (`fixprpoll.go:319-325`, `qajobs.go:409-411`).
- Recommendation: reword it to "after the merge of a spec pull request a GAOS job recorded".

Counts: B=1 M=13 N=8
