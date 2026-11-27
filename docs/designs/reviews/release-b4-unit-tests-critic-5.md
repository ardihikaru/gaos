# Goldfish critic review: `docs/designs/release-b4-unit-tests.md` (rev 5)

I checked the design against the rubric, the BRD business rules and the code at `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design`.

**The core objection.** The design says a unit run is a QA-tool job that the existing runner can drive by adding a "unit" branch. The code does not support that. The runner is one synchronous loop with no limit on starts. Its only store write, `ResolvePRCard`, refuses several moves the design needs. Its inbox relay is not the QA tool's only way to read an answer. Several of the design's guarantees are built on calls that the store refuses or that do nothing.

### Act-once marker, attestation retry and switch-off depend on moves `ResolvePRCard` refuses - Severity: Blocker
- Affected scope: §5.4 "Act once, across a restart"; §5.3 "Backoff"; §4.2 "Switch transitions"; §6 "Every end after `unitDone`"
- Finding and rationale: `resolveFrom` (`dashboard/prflow.go:600-606`) allows AWAITING_INPUT only from RUNNING. So the `unit:<h8|pass|refused>` marker cannot be written to an AWAITING_INPUT card. But §6 says the result comes "including while the job is `awaiting_approval`", and that is exactly the case TC-868 tests (an admin-closed defect that must not reopen after a restart). The design names no store method that does this write. The other two mechanisms have the same problem:
  - §5.3 moves a card "BLOCKED → READY (`ResolvePRCard`, rule `unit-attest`)". `ResolvePRCard` has no READY target (`prflow.go:613-614`) and no rule parameter; the rule is fixed to `RulePRResolved` (`prflow.go:626`).
  - §4.2 has `SetUnitQA` end BLOCKED cards DONE. `taskTransitions[BLOCKED]` has no DONE (`dashboard/taskstate.go:92`), so the move would pass through READY, DISPATCHED, RUNNING and REVIEW and emit `TaskMoved` events for a card that "ran nothing".
  - The reason is also rewritten at `qajobs.go:429`, `:448` and `:456` with `qaJobReason(id, case, text)`. §5.4 says `qaJobReason` gains "the two optional parts", but the grammar has three (`sha:`, `mode:`, `unit:`). Unless every rewrite carries all three, a later wait or resume erases the marker.
- Recommendation: Name one new store method, for example `NoteQAJobReason(ctx, product, task, reason)`, that rewrites the reason in place for RUNNING and AWAITING_INPUT cards and emits the existing `TaskMoved`/`RecordUpdated`. Make `qaJobReason` take a struct holding all the fields, so every rewrite keeps them. Name the store call that moves an attestation-blocked card back to READY. End switched-off BLOCKED cards CANCELED, not DONE.

### §8.5 contradicts BR-57 and does not stop the answer reaching the QA tool - Severity: Blocker
- Affected scope: §8.5; §2 row 8; §10 (BR-57 is not in the amendment list); §11 "Approval of generated tests"
- Finding and rationale:
  - BR-57 (`pustaka/docs/product/brd.html:1354`) says: "The QA tool receives every decision on its own items, whatever answered them; the webhook is a notification … Any other client polls." The webhook is not the only channel. `inbox_worker.go:26-27` says the item "stays readable by GET, which is the QA tool's fallback". Not sending the webhook therefore does not stop the QA tool reading the answer, and the design still breaks BR-57.
  - "Marked refused" cannot be recorded: `RecordExternalDelivery` accepts only `delivered`, `retry` and `gave-up` (`external_inbox.go:370-374`). `external_inbox.go` is not in §12.
  - The check relies on the QA tool naming its `externalId` `qa-job-<id>`. The QA tool chooses that format itself, and GAOS cannot verify it.
  - The worker holds only `store` and `out` (`inbox_worker.go:49-54`), so it cannot see "a tracked" job.
- Recommendation: Either amend BR-57 explicitly and also block the GET read of such items, or drop §8.5 and state that §11's rule binds only the QA tool. If you keep it, add the outcome constant and the fold change for it, plus `external_inbox.go`. Bind the check to data GAOS owns: the card's job id in its reason, not an id format the QA tool picks.

### The unit cap does not limit concurrent runs, throws merges away, and its notice has no sender - Severity: Major
- Affected scope: §4.2 "Own open cap"; §5.5 row 4; §14 "The caps' interaction"
- Finding and rationale:
  - The cap is checked when a card is planned and counts only RUNNING and AWAITING_INPUT cards. `Tick` starts every PENDING or READY card on every pass, with no limit (`qajobs.go:247-263`). Ten merges arriving while nothing runs plan ten cards, and the next tick starts ten sandbox jobs.
  - When the cap does bite, it drops the merge permanently: `prFlow.handle` returns `flowDone` (`prflow.go:483-508`), so that merge's tests never run. Since PENDING cards cost nothing under the cap's own rule, queueing them would cost nothing either.
  - The "one notice per product per hour" is said to be sent "by the runner, on the `unit:qa` card", but a dropped merge has no card. `prFlow` has no notifier (`prflow.go:401-407`), and the store cannot send notices (round 3 triage #6).
- Recommendation: Always plan the card PENDING, and enforce `GAOS_UNIT_QA_OPEN` in `Tick` before `start`. Count the RUNNING and AWAITING_INPUT `unit:qa` cards and leave the rest PENDING. That removes the drop, the hourly notice, `qaUnitCapNoticeAt` and the 409 `unit-qa-cap`. If you keep the drop instead, name a sender that exists and a card id the message can end with.

### Synchronous GitHub and QA calls in the single runner loop, and a 2.5-minute retry budget against hour-long rate limits - Severity: Major
- Affected scope: §5.1 outcome table; §5.3; §6 poll order; §8.1; §14 "QA-tool outage"
- Finding and rationale:
  - `Tick` runs `poll` and then each `start` in sequence (`qajobs.go:241-264`). Each unit start adds a GitHub PR read, possibly a `repoDefault` read, and a fresh capabilities read. Each poll that files a defect adds `buildUnitContext` (`pullFiles` plus up to 6 file reads). Each create adds a PR read in `recordQARow`.
  - `ghClient.do` sleeps up to 10 s twice per rate-limited call (`github.go:375-386`).
  - Active time is wall-clock time between polls (`qajobs.go:406-413`). A slow tick is therefore charged to every in-flight e2e job, and e2e jobs can hit `qa-job-timeout` (`qajobs.go:465-469`) because of unit work.
  - §5.1 treats `ghError.Limited` as transient but gives up after about 2.5 minutes (10 tries). GitHub's primary rate-limit window is up to an hour. After that the card goes BLOCKED `ReasonUnitNoCommit`, which has no automatic retry (unlike `ReasonUnitAttest`). So one rate-limit event leaves every merge in that window waiting for a person.
- Recommendation: Give `ReasonUnitNoCommit` the same 15-minute / 24-hour backoff as `ReasonUnitAttest`, or honour `Retry-After` and the reset time. Move unit start reads and context building off the poll loop (a worker per job, or a budget per tick), or at least state the latency cost to e2e jobs in §14 and exclude it from their active time.

### `ownUnitPRLocked` removes an e2e spec that a merge never has - Severity: Major
- Affected scope: §2 row 6; §8.3; §3 step 8; TC-857; `PREvent.HeadSHA` in §12
- Finding and rationale:
  - `prCardsFor` gives a `pr.merged` event exactly one spec, `unit-test` (`prflow.go:88-91`). The design cites this same line in §8.3.
  - Merged-spec e2e runs are planned only for `PRKindQA` rows (`mergedrun.go:52-55`), not from a merge event.
  - A draft `pr.opened` already returns at `prflow.go:232-233`.
  - So on a merge, "removes the e2e spec only" has nothing to remove. The "checked at the merged head" condition, the new `PREvent.HeadSHA` field and TC-857 protect nothing.
  - If the intent is a non-draft `pr.opened` (a qa-unit PR marked ready and then reopened), the function keeps the reviewer spec. That contradicts "GAOS does not review them" only by accident.
- Recommendation: Say which event can carry an e2e spec for a qa-unit PR. If none can, delete `ownUnitPRLocked`, `PREvent.HeadSHA` and TC-857, and fix row 6 of §2. If it is `pr.opened`, say so and say what happens to the reviewer spec.

### `jr.unit = ghc` puts a typed-nil `*ghClient` into the interface, which panics when GitHub is off - Severity: Major
- Affected scope: §5.1 ("`jr.unit = ghc`, `main.go:425`"); §5.1 table row "`r.unit == nil`"
- Finding and rationale:
  - `newGHClient` returns nil when there is no token (`github.go:169-171`). `main.go` guards every other use with `if ghc != nil` (`main.go:387-393`, `461-474`).
  - Assigning a nil `*ghClient` to the `unitSource` interface makes `r.unit != nil` true. `pullRequest` then dereferences `c.base` (`github.go:216`) and crashes the process.
  - The handler refuses `Enabled` without `codeSrc` (§4.1), but a later restart with the token removed leaves `UnitQA.Enabled` on, and GAOS then crash-loops.
- Recommendation: Write `if ghc != nil { jr.unit = ghc }`. Say so, and add the "no GitHub client" case to TC-864.

### The `Passed` list is unbounded but the reader is capped at 2 MiB, and it is re-sent on every poll - Severity: Major
- Affected scope: §6 (`Passed`, "the full list as received"); §11 "Status"
- Finding and rationale:
  - The QA body is read up to `qaOutMaxBody = 2 << 20` (`qaclient.go:40`). At up to 200 characters per name, that is about 10,000 test names. A larger suite cannot be read at all: the poll errors every time, the job ends `qa-job-timeout`, and no defect is filed or closed.
  - Because `unit` stays on the status for the rest of the job, a create waiting up to 24 h for approval (`qajobs.go:197-207`) re-sends the whole list every 15 s.
- Recommendation: Have the QA tool send `Passed` only on the first status that carries `unit` (or behind a separate route), and limit it to the names GAOS needs. GAOS could send the open defects' `UnitTests` in the start body and ask "did these pass?". State the largest suite size supported.

### Defect identity churns, and a stale superset defect closes only on a fully green run - Severity: Major
- Affected scope: §7.1 `h`; §7.4 "Passed"; §6 poll order; D-105 residuals
- Finding and rationale:
  - The hash includes `#<total>`, and `Total` also counts withheld and over-50 entries. So the same visible failing set with a different count files a new defect. With more than 50 failures, each change in the count files another one, up to the daily cap.
  - `PassUnitDefects` runs only on `result == passed`. Take a defect `{A,B}` where B is later fixed: the next run fails `{A}` and files a new defect, while `{A,B}` stays open, held, and can be triaged and fixed even though half of it is stale. It closes only on a fully green run.
  - The residual "overlapping failing sets are separate defects" does not cover a defect that never closes while anything else fails.
- Recommendation: Drop `Total` from the hash. On a `failed` result, also call the pass check with the `Passed` list, so a defect whose every test passed closes even when other tests fail (§11 already says `passed` lists every test that passed). Say in D-105 what happens to a superset defect.

### Check 0 proves only that a rule exists, and §11's identity cannot be enforced as written - Severity: Major
- Affected scope: §8.2 check 0 ("the control is check 0"); §11 "Identity"
- Finding and rationale:
  - `GET /repos/{o}/{r}/rules/branches/{ref}` lists the rules that apply to a branch. It does not list who may bypass them. An `update` rule also does not restrict creation or deletion. A writer can delete a `qa-unit/*` branch, recreate it with their own commits and reopen the pull request. Check 2 (author) still passes, and check 4 is advisory.
  - To my knowledge, fine-grained tokens cannot be limited to branches, and ruleset bypass lists name roles, teams, apps or deploy keys rather than single users. If so, "a ruleset lets it update only `qa-unit/*`" means everyone in that team or role can push. The identity can also write any unprotected branch.
  - So the one control the design calls effective is not one GAOS can verify.
- Recommendation: Have check 0 also require `creation` and `deletion` rules. Restate check 0 as an existence check in D-105's residuals, not "the control". Make §11 say what is actually enforceable: a GitHub App with a dedicated bypass, or an accepted residual.

### `SetUnitQA` is a store method that would call GitHub under the store lock - Severity: Major
- Affected scope: §4.1 "Store method" ("The store's three refusals"); §8.2 check 0; TC-877
- Finding and rationale:
  - §8.2 says "`SetUnitQA` with `Create` on reads the same for each target repository and refuses …". `SetUnitQA` is the store method (`MemStore`, and `ArcadeStore` via `mutate` under `s.mu`, `store_arcade.go:648-652`). That puts network reads under the store lock.
  - The design itself keeps `autoFixAdmin` out of the store lock for that reason (§7.3).
  - §4.1 lists three store refusals. This is a fourth, and TC-877 tests it at the handler.
- Recommendation: Move the branch-rule read to the handler's refusal table in §4.1, next to `unitSandbox`, and leave `SetUnitQA` network-free.

### `UnitBase` is never re-checked when an allowed base is revoked - Severity: Major
- Affected scope: §5.1 base rule; §7.5 site table; §13 "nine places"
- Finding and rationale:
  - The pre-open gate refuses a retarget base the operator removed from `AllowedBases` (`autofixpr.go:90`), and the claim loop does the same (`autofix.go:350-355`). The site table omits `autofixpr.go:90`, and nothing re-checks `UnitBase`. A unit defect filed while `release/x` was allowed still opens a fix into it after the operator removes it.
  - `AllowedBases` is documented as "the branches an open fix pull request may be retargeted to" (`model.go:462`). The design reuses it as an admission gate for unit runs while also refusing retargets for unit defects.
  - The "nine places" claim is off. Non-test sites also include `autofixpr.go:90`, `autofixpr.go:510` and `defecttriage.go:433-441`.
- Recommendation: Add a row: `autofixpr.go:90` refuses a unit fix when `UnitBase` is neither the effective base nor in `AllowedBases`. Either document the widened meaning in `model.go`/D-105 or add a field of its own. Correct the count.

### Exempting `unit:qa` from `gateUnassigned` contradicts BR-61, and BR-61 is not amended - Severity: Major
- Affected scope: §4.2 ("A `unit:qa` spec is exempt from `gateUnassigned`"); §10; §0 delegation list
- Finding and rationale:
  - BR-61 (`brd.html:1307`): "A product whose agent assignments are configured plans a pull-request flow card only for a role that has an agent assigned to it." The `unit:qa` spec has `Role: "unit-test"` and is planned on Brain Town's path without that role assigned.
  - API cards have an exemption in D-102 §22, but BR-61's text does not mention it.
  - The delegated-amendment list in §0/§10 (BR-38, BR-44, BR-54, D-091 (8)) is therefore incomplete, and so is the summary the operator will see.
- Recommendation: Amend BR-61 ("…a card the QA-job runner drives is not gated by assignment"). Alternatively give `unit:qa` a role that is not an agent role. Add the amendment to §0, §10 and the operator summary.

### Anchors drifted after the TaskMgr merge - Severity: Nit
- Affected scope: §0, §4.1, §5.1, §5.5, §7.5, §8.2, §10
- Finding and rationale: The code has moved, so these anchors are wrong:

  | Design anchor | Actual location |
  |---|---|
  | `main.go:474-475` | `main.go:490-491` |
  | `:388` | `:390` |
  | `:424-425` | `:439-440` |
  | `:444-456` | `:459-475` |
  | `:554` | `:570` |
  | `:577` | `:594` |
  | `:578` | `:595` |
  | `NotifyPR` `notify.go:97` | `notify.go:145` |
  | OQ-38 `open-questions.html:921-929` | `open-questions.html:923-931` |

  The other anchors I checked hold, for example `labels.go:179-188`, `board.go:452/479`, `scheduler.go:226-238`, `qajobs.go:350/429/465`, `defects.go:59/61/347/590` and `reviewdefect.go:106`.
- Recommendation: Re-anchor these against `main`.

### TC range stated three different ways - Severity: Nit
- Affected scope: §9 last line; §10 "TC-850…TC-877"; §18 row "TC-853"
- Finding and rationale:
  - §9 lists TC-850…TC-879, 30 cases, and the 30 is correct (4+6+5+6+4+3+2).
  - §10 says TC-850…TC-877.
  - §18 credits the `gateUnassigned` exemption to TC-853, but §9 puts it under TC-854.
- Recommendation: Use TC-850…TC-879 everywhere and fix the §18 pointer.

### The change list is missing files the mechanisms need - Severity: Nit
- Affected scope: §12
- Finding and rationale:
  - `decorateCardsGate(cards, byID)` takes no viewer and is called inside store board builders (`board.go:270`, `board_page.go:431`). Passing "is admin" in changes those methods' signatures, and `board_page.go` is not listed.
  - The §8.5 outcome needs `external_inbox.go`.
  - The inbox worker needs runner or store wiring in `main.go`, and the `main.go` row does not mention it.
- Recommendation: Add these files, or trim the moves in the handler after the store answers.

**What holds up:** The renumbering is clean. D-105, BR-64 and TC-850 are unused (`decision-log.html:2153` ends at D-104, and the QA plan ends at TC-849), schema 21 is free, and labels 19→20 is right. The §4.4 lease key (`qaToolDriven` on label plus `qa-job:` prefix) matches `qajobs.go:299-305` and fixes the rev-4 problem. The held-card guard and the drift-release exclusion (`workspace.go:395`, `sendback.go:153-159`) cover the paths out of BLOCKED that I could find.

Counts: B=2 M=10 N=3
