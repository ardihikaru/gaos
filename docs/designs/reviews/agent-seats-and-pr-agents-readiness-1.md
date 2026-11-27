I checked the design's claims against the code, since whether a file or behaviour exists can only be settled by looking. D-099 and BR-59/BR-60 are the next free numbers, and the label count (10 today, 18 after) is correct. The problems are below.

### Review defects can never be auto-fixed or reach a pull request - Severity: Blocker
- Affected scope: §6.2 field table, §6.3, §10 #11
- Finding: The §6.2 table gives no value for `Defect.AutomationType`, but that field is required in the database (`schema/17-defects.sql:18`: mandatory, regexp `be|e2e`). Without it, writing a review defect to ArcadeDB fails. Even with a value, the code has two more gates:
  - `autoClaimLocked` (`autofix.go:164-166`) and `fixEligible` (`autofixpr.go:82`) both refuse any defect whose `AutomationType != "be"`.
  - `fixEligible` refuses a `BaseOverride` that is not in `AllowedBases` (`autofixpr.go:90`).
  
  §6.3 exempts only the dispatch-time check (`autofix.go:302-307`). So a review fix that gets dispatched still ends in Review with no pull request after the run ("the retarget base is no longer allowed"). Two engineers would also put the exemption in different places: §10 #11 lists both files and does not say which function changes.
- Recommendation: Set `AutomationType` for review defects explicitly (for example `be`), and say why that is acceptable for a frontend repository. Name both functions that change, `autoFixPass` and `fixEligible`, and write the exact exemption predicate shared by both. Add a test where the full run opens the draft, not only one that checks the base conditions.

### Staging "create" cards have no way to get their case code - Severity: Blocker
- Affected scope: §7.2 steps 1-2
- Finding: The runner gets the case code for a `create` job from `caseCodeFor` (`qajobs.go:232-255`). That function reads a `QA-case:` line from the pull request body, which it finds through `prKeyParts(c.DedupKey)`. The new key `<owner>/<repo>@<sha>:e2e-create:<caseCode>` has no `#n`, so `prKeyParts` fails (`prreview.go:197`). The function returns `""`, and `start` blocks the card with "no QA-case line" (`qajobs.go:284-288`). As designed, every create card ends up Blocked. The design also leaves open:
  - who calls the QA tool's `GET /cases` (the store planner cannot make network calls);
  - how the case list gets into `PlanPullRequestCards`;
  - what the staging callback answers if that call fails.
- Recommendation: Say where the case code is carried (parsed from the dedup key, or a new field on the card) and how `caseCodeFor` reads it. Name the function that queries the QA tool and how its result reaches the planner, for example a new `PREvent` field. State the behaviour when the QA tool fails: plan the run only, or answer 5xx.

### Spec-merge waiting and failure states are undefined and can start a duplicate create job - Severity: Blocker
- Affected scope: §7.3 steps 1-4, §7.2 step 3
- Finding: Today, when a job is final, `poll` removes it from `r.jobs` and moves the card to Done "qa job succeeded" right away (`qajobs.go:332-336`). The design does not say:
  - which status and reason the card holds while it waits up to `GAOS_E2E_MERGE_WAIT`;
  - where the wait clock is stored, and whether it survives a restart.
  
  On failure the card goes to `AWAITING_INPUT` with reason `merge-<class>`, which has no `qa-job:` prefix. On the next tick, `Tick` sees an `AWAITING_INPUT` card with no tracked job and calls `start` (`qajobs.go:221-224`). `jobIDFromReason` then returns `""`, so the runner starts a **second** `create` job. That breaks the invariant at `qajobs.go:276-278` ("a second create job could open a second pull request").

  A card in Needs you can only be answered or cancelled (`board.go:214-216`), and there is no question to answer. So the merge-refused card can only be cancelled. Its dependent `e2e:run` card waits for a final status (§7.2) and is held until someone cancels it.
- Recommendation: Write the state machine. For each of "waiting for checks", "merged" and "refused", give the status and the exact reason string, keeping the `qa-job:<id>` prefix. Say how a restart resumes the wait. Say how a person clears a refused card (for example: merge by hand, then a named move) and what happens to the run card when that is done.

### The seat gate's position skips the merge cancel and the Pull requests row - Severity: Major
- Affected scope: §3.4 first paragraph
- Finding: The design says to drop unseated specs "before the de-duplication loop" and to return `ErrPRSkipped/unseated` when every spec is dropped. In `PlanPullRequestCards`, that early return comes before two things (`prflow.go:238-255`):
  - the merge-time cancel of unstarted review and e2e cards;
  - `reviewRowLocked`, which writes the Pull requests row.
  
  With no unit-test seat, a `pr.merged` event would neither cancel the PR's pending review card nor write its row.
- Recommendation: Give the exact place to insert the gate (after the cancel loop and the row write, before the cap check), or say the opposite on purpose. Add both cases to `TestUnseatedRoleGetsNoCard`.

### The one-time upgrade seating has no home and misfires for new products - Severity: Major
- Affected scope: §3.4 "Upgrade", §10 #1-2
- Finding:
  - The design does not say which code runs the migration (ArcadeStore bootstrap, MemStore, or `main.go`), or what actor its `RecordUpdated` names.
  - `seatsMigrated` is described as a Product property "written in the same upsert". But §10 #1 adds no `Master.SeatsMigrated` field, and the upsert builds its properties from `Master` (`store_arcade.go:840-849`).
  - A product created after the upgrade has no `seatsMigrated`. If it has repositories, the next restart silently seats agents in it, overriding an operator who left it unseated on purpose.
- Recommendation: Add the field to the change list. Name the function and the point at startup where it runs. Have `CreateProduct` set `seatsMigrated: true` (or keep one tenant-level marker). Add a test: a product created after the migration stays unseated across a restart.

### RepoKinds has no store write path - Severity: Major
- Affected scope: §7.1 second paragraph, §10 #2
- Finding: `RepoKinds` is a `Master` field, but it is edited on the Auto-fix form. That form calls `SetAutoFix(ctx, id, a AutoFix, ...)` (`handlers_autofix.go:137`), which replaces only `AutoFix`. No store method in the change list writes `RepoKinds`, so implementers will choose between changing `SetAutoFix`'s signature, moving the map into `AutoFix`, or adding a new method.
- Recommendation: Pick one, and list the signature on all three stores and in the `Store` interface. Also give the form field names, which `autoFixFromForm` must read because the form replaces the whole record.

### The unit-test seed policy is in a different file, and existing products get no reply contract - Severity: Major
- Affected scope: §8.1, §10 #17
- Finding:
  - `blueprint.go:164` is only the generic renderer (`65-<key>.md`). The `unit-test` seed policy is in `deploy/seed/example/product.json:138`, so change #17 names the wrong file.
  - The reviewer's reply contract is injected by code (`prReviewContract`, `prreview.go:90`). The unit-test contract would exist only in policy text, which existing products do not receive (§8.1 says so). For every existing product, every unit-test run would then end Failed as `unit-unreadable` until a person edits the policy.
- Recommendation: Point change #17 at `deploy/seed/example/product.json` and give the exact new text. Inject a `unitTestContract` into the packet through the PR hooks, as the reviewer does, so behaviour does not depend on a manual policy edit.

### The unit-fix card has no store method, no stored context, and uses up the PR cap - Severity: Major
- Affected scope: §8.2 "The fix card", §10 #16
- Finding:
  - A new card must be planned through a store mutation on MemStore, ArcadeStore and readOnly, but none is named. Only `unittest.go` is listed.
  - "The reply's summary as its context part": no `TaskRecord` field holds a context part, and the packet hook only supplies `prPacketParts` for `pr-reviewer` cards (`main.go:443`).
  - Its dedup key is not `defect:`-prefixed, so a card that stays Blocked counts against `prMaxOpen` (`prflow.go:176-191`). Enough of them would block review cards.
- Recommendation: Name the store method and how it plans the card (PENDING then moved, as `planDefectCardLocked` does, so a replay rebuilds the same board). Say where the summary is stored and how the packet reads it. Decide whether `unit:fix` cards are excluded from the cap.

### §8.2's argument against using Needs you rests on a false premise - Severity: Major
- Affected scope: §8.2 "Where a person sees it", §7.3 step 3
- Finding: §8.2 says that "only an agent's question may put a card" in Needs you. `legalMove` only stops an operator from dropping a card there. The QA runner already moves cards to `AWAITING_INPUT` (`qajobs.go:353`), and §7.3 of this design does the same. The reason given for choosing Blocked over the operator's "Needs you" is therefore wrong, and the two sections contradict each other.
- Recommendation: Correct the premise. Make one consistent choice for "waits for a person" in both §7.3 and §8.2, give the move that clears it, and record that choice in D-099.

### The review defect key and the re-open path conflict with the defect model - Severity: Major
- Affected scope: §6.2 rows `Key`, `Kind`
- Finding:
  - Defect keys are `<productId>/<defectId or caseId>` (`schema/17-defects.sql:10`, `defects.go:436,472`). `CloseDefect` looks keys up as `productID+"/"+ref` (`defects.go:588`). The design's `review:<owner>/<repo>#<n>` breaks that convention in a map shared by all products.
  - `reopenDefectLocked` (`defects.go:536-553`) sets `Regression=true`, clears `Difficulty`, and plans a **triage** card. That contradicts "already triaged easy, no triage card", and a regression is never auto-fixed (`autofixpr.go:84`).
  - The design does not say what happens when a second low review arrives while the defect is still `fixing`.
- Recommendation: Give the exact key, for example `<productId>/review:<owner>/<repo>#<n>`. Either describe a review-specific re-open, or drop re-open (a pull request is reviewed only once on `pr.opened`) and say so. Define the non-final case.

### Registers and identifiers are not allocated - Severity: Major
- Affected scope: §9, §10 #19-20, §14 "one release"
- Finding:
  - No QA case IDs are allocated. The next free is TC-767, since D-098 used TC-763 to TC-766 (`decision-log.html:2121`).
  - No release version is given. v0.60.0 is the current version (`changelog.html:39`, `README.md:12`).
  - The changelog entry and the README figures are not in the change list (`README.md:137` Go test count, `:139` "766 registered test cases"). The sibling design `docs/designs/integrations-status-chips.md:391-396` treats those as required.
  - "One release" conflicts with phase 5 waiting on a change in another repository.
- Recommendation: Allocate TC-767 onward, one per §11 test, and put the IDs in the tests table and in the D-099 evidence cell. Name the version and the changelog entry, and list the README lines that change. Say whether phase 5 can ship in a later release.

### Phases have no exit conditions, verification commands or stated dependencies - Severity: Major
- Affected scope: §14, §11
- Finding: The phases are only a list of section numbers. There is no command and expected result per phase, for example `go test ./dashboard/ -run '...'` or `deploy/schema/scratch.sh up` with a pass count. Dependencies between phases are not stated:
  - `labels.go`'s eight labels are needed across phases 2, 3 and 5;
  - `schema/19-agent-seats.sql` is needed by phase 4;
  - phase 4 needs phase 2's severity label and phase 1's seat gate.
  
  `TestLabelVocabulary` would fail or change in every phase.
- Recommendation: For each phase, list the files, the tests (with TC IDs), the exact command and its expected result, and the earlier phase it needs. Split the label additions by phase, or move all eight into phase 1.

### Wiring changes are missing from the change list - Severity: Major
- Affected scope: §6.2, §6.3, §7.3, §10 #5
- Finding:
  - `prReviewer` only has a `prPoster` (`prreview.go:212-219`), but §6.2 needs `pullRequest` for `head.ref` and `pullFiles` for the changed files.
  - `qaJobRunner` has no `ghWriter` (`qajobs.go:173`), but the merge needs one.
  - `autoFixPass(ctx, st, src codeSource)` has no GitHub pull-request reader for §6.3 check 2.
  
  These are constructor and signature changes in `main.go` and in those files, and they are not listed.
- Recommendation: List each new dependency, its type and where `main.go:431-436` injects it, including the nil behaviour when a token is missing.

### Line references are wrong in several places - Severity: Nit
- Affected scope: §3.4, §4.1, §5, §6.2, §3.3
- Finding:
  - The skip log is `prflow.go:385-386`, not 388-389.
  - The staging `capped` case is at `staging.go:262-263`, not "after 282".
  - The end-state branch is at `prreview.go:256-261`.
  - `listPageAt` is at `pager.go:128`.
  - The fix dedup key is `defect:<key>:<gen>:fix` (`defecttriage.go:45`).
  - Defect cards are born PENDING, not READY.
- Recommendation: Correct the references.

### `agentForStage` is not a per-role picker - Severity: Nit
- Affected scope: §3.4 "The gate does not choose…", formula
- Finding: `agentForStage` maps a stage to a role through `stageRole` (`fixtures.go:80-91`); it does not pick an agent for the card's role. Also, `agents[id]` treats a slice as a map.
- Recommendation: Write the predicate with `agentByID(id).Role`, and correct the description of how an agent is chosen.

### The 422 fallback trigger is too broad - Severity: Nit
- Affected scope: §5 last paragraph
- Finding: GitHub returns 422 for several reasons, not only for a review on the token owner's own pull request. It is also unclear whether `request-changes-refused` replaces the Done card's "review posted" reason.
- Recommendation: Match on the error message, or compare the pull request author with the token's login. Give the final reason string for each row of the table.

### The arithmetic leaves things out - Severity: Nit
- Affected scope: §13
- Finding: It does not count:
  - the new environment variables `GAOS_E2E_CREATE_MAX` and `GAOS_E2E_MERGE_WAIT`;
  - the new outbound endpoints (QA `GET /cases`, GitHub check-runs, merge);
  - the new store methods;
  - the new `Facets` field for severities (§4.2).
- Recommendation: Add these counts.

### The operator can set the new label families by hand - Severity: Nit
- Affected scope: §4.1, §7.1, §8.2
- Finding: `SetTaskLabel` refuses only the `defect` family (`prflow.go:434`). Through the existing label route, an operator could set `e2e-kind:*` (which changes the kind sent to the QA tool) or `unit:fix` on a `unit:test` card.
- Recommendation: State whether these families can be edited by hand, and refuse the ones that cannot.

### Label and request disagree when RepoKinds is empty - Severity: Nit
- Affected scope: §7.1 last sentence
- Finding: With `RepoKinds` empty, the card carries `e2e-kind:fullstack` but the `kind` field is not sent, so the label says something the QA tool was never told.
- Recommendation: Either leave the label off when the field is omitted, or say that the label is display only.

Sections that hold up: §4.1 (choosing a label over a new field is consistent with `labels.go`), §3.2's route and admin rule, and the §13 label count.

Counts: B=3 M=10 N=6
