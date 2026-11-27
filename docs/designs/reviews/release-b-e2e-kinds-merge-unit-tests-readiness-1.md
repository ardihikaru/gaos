# Readiness review: release-b-e2e-kinds-merge-unit-tests.md (rev 1)

Reviewed: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-release-b/docs/designs/release-b-e2e-kinds-merge-unit-tests.md`

I checked the anchors against the tree and they mostly hold. The functions it cites exist: `qajobs.go:64/204/235/274`, `staging.go:196`, `sendback.go:153`, `githubwrite.go:123`, `model.go:729`, `scheduler.go:379` and `board_page.go:324`. TC-819 is the next free test number (TC-818 is the highest in `qa-test-plan.html`). D-103 is the next free decision (D-102 is the last row in `decision-log.html:2142`). BR-62 is the next free rule (BR-61 at `brd.html:1307` is the highest). The problems are in the change list, a few undecided choices, and the register amendments.

### No file change list - Severity: Blocker
- Affected scope: the whole document; §6, §7
- Finding: The design names only one file it creates (`githubmerge.go`). It never says which files are modified, and nothing could serve as a write allowlist. From the text, the implementer must find and change at least these:
  - `model.go`: `RepoKinds`, `AutoFix.AutoMergeSpecs`, `AutoFix.UnitTestsViaQA`
  - `labels.go`: three labels plus `labelOrder` placement
  - `prflow.go`: the `prCardsFor` signature, `openPRCardsLocked`, `setTaskLabelLocked`
  - `api_cards.go`: `cardItem` needs `Deps`, plus `apiSpec`
  - `staging.go`
  - `qajobs.go`: `startAutomation` signature, the `qaJobClient` interface, `isE2ECard`, `Tick`, `poll`
  - `qaclient.go`: capability read
  - `handlers_assign.go`
  - `handlers_autofix.go` and `defects.go` (`SetAutoFix`/`AutoFixConfigured`)
  - `sendback.go`, `board_page.go`, `store.go`, `store_arcade.go`
  - the notifier
  - a schema file (see the Artifact finding)
  - the register pages: `brd.html`, `decision-log.html`, `qa-test-plan.html`, `implementation-plan.html`, `agents.html`, `pr-agents.html`, `changelog.html`

  Two engineers would touch different sets.
- Recommendation: Add a per-phase change table (B1 to B4). Each row: repository-relative path, create/modify, and the exact symbols changed. Include the docs, register files and the new test file names.

### No named tests and no per-change verification - Severity: Major
- Affected scope: §6 "Tests TC-819 onward … allocated per phase"; §7 exit column and line 164
- Finding: There are no test function names, no TC→test mapping and no count. Each exit condition describes behaviour ("each guard refused alone leaves it open"). None gives a command with an expected result. The only command listed is the generic suite. B1's exit ("runs only API/GQL cases") happens on the QA tool's side, so GAOS cannot observe it. The checkable GAOS-side fact is "the request body carries `kind: be`".
- Recommendation: For each phase, list `Test…` names with their TC IDs and a total count. B3 needs one test per guard 1–6. Restate each exit as `go test -run '<regexp>' ./...` → PASS, with an observable GAOS-side assertion.

### D-102 (5) requires amending BR-08; the design says it "honours" BR-08 instead - Severity: Major
- Affected scope: §4.1, §6
- Finding: `decision-log.html:2144` says D-102 (5) "takes effect only when … BR-08, D-091 (2) and the out-of-scope merge line are amended". BR-08 (`brd.html:1240`) says only that Minimum "forbids auto-merge". It does not say that Working or Complete permit it. §6 lists no BR-08 amendment, so the precondition D-102 set is unmet.
- Recommendation: Add an explicit BR-08 amendment to §6, with its exact text, for example: "Working/Complete may auto-merge a QA spec pull request under BR-62; Minimum and Insufficient never".

### BR-61 "never merges" and BR-44 "e2e card" are not amended - Severity: Major
- Affected scope: §4.1, §5.2, §6
- Finding:
  - BR-61 (`brd.html:1307`) ends "GAOS … never merges and never closes a pull request". §6 does not list it.
  - BR-44 (`brd.html:1304`) allows the QA-job runner to put only "an e2e card" in AWAITING_INPUT. §5.2 puts a `unit:test` card there ("the existing BR-44 path"), and §3.3 claims BR-44 "is not widened". That is a contradiction.
- Recommendation: Add BR-61 and BR-44 amendments, with their wording, to §6, and allocate them to D-103 or D-104.

### BR-54 is not amended for part 4 - Severity: Major
- Affected scope: §5.2 "Run failed → … GAOS plans a `unit:fix` card", §6
- Finding: A QA-tool job result causes a new card. BR-54 (`brd.html:1350`) allows QA data only the epic card and the defect-route cards. §3.1 amends BR-54 for the staging callback only.
- Recommendation: Add a second BR-54 amendment under D-104, or argue in the document why the runner, and not QA data, is the source.

### Create cards include GQL cases that cannot be generated - Severity: Major
- Affected scope: §3.1 step 1 vs §2.1 table
- Finding: Step 1 selects uncovered cases with "`Level` in *K*'s levels", and for `be` that is `API, GQL`. §2.1 says "a GQL case is run, never generated". Every planned GQL create card would therefore fail.
- Recommendation: Say that create selection uses the kind's generatable levels only (`be` → `API`; `fullstack` → `WEB`, `E2E`), and keep `GQL` for run filtering.

### Create dedup key: per repository or per case, and is a failed create ever retried - Severity: Major
- Affected scope: §3.1 steps 2–3
- Finding: The key `staging-create/<owner>/<repo>:<caseID>` is permanent. `planCardsLocked` matches any card with that key whatever its status (`api_cards.go:54`), so a Failed or merge-off Done create blocks that case on that repository forever. Step 2 only skips "still open" cards across repositories. Once the first card closes, a second repository of the same kind plans a second create for the same case. Two engineers would build different retry and cross-repository behaviour.
- Recommendation: Choose one:
  - key per product and case (`staging-create/<product>:<caseID>`), or
  - keep the per-repository key and state the cross-repository rule.

  Also state whether a Failed create is replanned, and under what rule.

### Run hold has no stored "planned at" time - Severity: Major
- Affected scope: §3.2
- Finding: The hold is "30 minutes since the run card was planned". `TaskRecord` (`model.go:833-881`) has no planned timestamp. PR cards also get no `Deps`: `cardItem` (`api_cards.go:21-28`) and `planCardsLocked` set none. The sentence "a dependency is open and younger than" is also ambiguous: younger refers either to the dependency or to the run card.
- Recommendation: Name the field, for example a new `TaskRecord.PlannedAt` carried in `PlannedTask` so a replay keeps it, or name the event the time is read from. State that `cardItem` gains `Deps`, and how they ride `PlannedTask.Deps`. Rewrite the rule as one predicate.

### Create cards' cap and `openPRCardsLocked` exclusion are not specified - Severity: Major
- Affected scope: §3.1 cap paragraph
- Finding:
  - "Not against `GAOS_PR_MAX_OPEN`" requires `openPRCardsLocked` (`prflow.go:178-195`) to skip `staging-create/` keys. The design does not say so.
  - "Above it, step 3 plans nothing" leaves a partial fill undecided: with 3 open of 4 and 2 uncovered cases, does it plan 1 or 0?
  - It is not stated whether the create cards are planned inside `PlanPullRequestCards` (same lock, same Result) or by a new store method.
  - It is not stated how `"creates": n` appears on the 200 duplicate path (`staging.go:278-279`).
- Recommendation: Specify:
  - the exclusion in `openPRCardsLocked`;
  - `min(remaining cap, CREATE_MAX)` or all-or-nothing;
  - the store method and its `Store`/`ArcadeStore` signature;
  - the response body on both 202 and 200.

### Kind label for an API auto-run card has no repository - Severity: Major
- Affected scope: §2.2 "`PlanAPICard`'s e2e kinds do the same"
- Finding: An `APIKindAutoRun` card is keyed `qa-run/<product>/<client>/<code>/…` (`api_cards.go:160-166`). It carries no repository, so `ev.Repo`'s kind cannot be looked up. The design also does not say whether `kind` is sent for such a run.
- Recommendation: State the rule. Examples: no kind label and no `kind` sent, or the product's single kind when all its repositories agree.

### `labelsAllowDispatch` has no product parameter - Severity: Major
- Affected scope: §5.2 "`labelsAllowDispatch` refuses `unit:test` on that product"
- Finding: `labelsAllowDispatch(labels []string)` (`labels.go:167`) is called from `scheduler.go:286` and `apidispatch.go:47`. It cannot see the product's `UnitTestsViaQA`. Changing the signature, adding a wrapper, or checking in the callers are three different implementations.
- Recommendation: Name the new signature, for example `labelsAllowDispatchFor(m Master, labels)`, and every caller that switches to it.

### Unit mode choice ("a test file beside it") is undecidable - Severity: Major
- Affected scope: §5.2 "Which mode"
- Finding: Three points are open:
  - "Beside" is undefined: same directory, a name convention (`x.go` → `x_test.go`), or any file matching `TestGlobs` in that directory.
  - "A changed source file" can mean any one or all of them.
  - "Source file" itself is undefined against `TestGlobs`/`AllowedGlobs`.

  The `sha` sent to `/gaos/unit` (merge commit or head) and the source of `files[]` (all changed files, or source files only) are also unstated.
- Recommendation: Give the exact predicate, the sha source on the `pr.merged` `PREvent`, and the `files[]` filter.

### `unit:fix` artifact kind `context` is refused by the DDL - Severity: Major
- Affected scope: §5.2 "stored as an `Artifact` of kind `context`"
- Finding: The current `Artifact.kind` regexp (`schema/14-design-gate.sql:36`) is `sequence|erd|flow|diff|commit|report|log|upload|handoff|prompt|reply|question|design|review`, which does not include `context`. The ArcadeDB commit would refuse the row. No schema change is listed, and the "packet hook" that reads the artifact is not named.
- Recommendation: Either reuse an admitted kind (for example `report`), or add a new `schema/NN-*.sql` widening the regexp plus the `designgate_test.go` set update. Name the packet-builder function that reads it.

### `unit:fix` dispatched to an agent with no product code - Severity: Major
- Affected scope: §5.2, §1 last GAOS bullet
- Finding: §1 says the `unit-test` role's workspace "holds no product code; GAOS runs no code". §5.2 then dispatches `unit:fix` (role `unit-test`) "as an ordinary card" to fix failing tests. The design does not say what the agent receives or produces (a patch, a PR, a handoff), nor how it reaches Review with any meaningful output.
- Recommendation: State the agent's input, its output contract, and where the fix lands. If it cannot fix code, rename the card to what it actually does.

### `unit` label family is shared and person-settable - Severity: Major
- Affected scope: §5.2 "family `unit`"; `prflow.go:540-568`
- Finding: `setTaskLabelLocked` replaces the label within its family and refuses only `defect` and `source` (and, per the design, `e2e-kind`). A person could relabel a QA-driven `unit:test` card to `unit:fix`, which makes it dispatchable and skips the BLOCKED hold.
- Recommendation: Make `unit:fix` planner-only (refused in `SetTaskLabel`), or give it its own family. Say which.

### Merging state: how the runner keeps tracking after the job is final - Severity: Major
- Affected scope: §4.4
- Finding: `poll` deletes a job from `r.jobs` as soon as `j.final()` returns done (`qajobs.go:332-336`) and marks the card Done. The design keeps the card Running "every poll" for up to 30 minutes, which is about 120 GitHub rounds at the 15 s interval (`qajobs.go:156`). Three things are left open:
  - whether the merger is a branch inside `poll`, a separate tracked state, or its own loop;
  - the GitHub poll cadence;
  - how `start`'s re-adoption parses `merging since <unix>`.
- Recommendation: Specify the state machine: the tracked field, the cadence (for example every 60 s), the reason grammar, and the restart path.

### Operator open item 2 changes B4's scope - Severity: Major
- Affected scope: §10 item 2, §5.1 `create`
- Finding: If the answer is no, part 4 ships `run` only, and `create` "attaches the proposed tests to the card". That attach mechanism is not designed. It is an "A, or B" with no selection made, and it is not registered as an OQ (the register ends at OQ-43).
- Recommendation: Resolve it before approval. Otherwise register it as OQ-44 and design both branches, or explicitly defer `create` to a later phase.

### No milestone, version or label-count pages named - Severity: Nit
- Affected scope: §6
- Finding: The design allocates no implementation-plan milestone and no release version per phase. It also does not name the pages that state the label vocabulary (`pustaka/docs/integrations/pr-agents.html`, `agents.html`).
- Recommendation: Add the milestone ID, the versions per phase, and the doc pages to the change list.

### Line anchors have drifted - Severity: Nit
- Affected scope: §1, §2.2, §4.1
- Finding:
  - `prCardsFor` is at `prflow.go:80`, not `:78`.
  - `poll` is at `qajobs.go:310`, not `:315`.
  - The BRD out-of-scope merge line is at `brd.html:1459`, not `1456-1457` (which is the edge-type line).
  - The boundary prose is `brd.html:1385-1387`.
- Recommendation: Correct the anchors.

### Guard 4 admits `api/graphql` though nothing generates it - Severity: Nit
- Affected scope: §4.3 guard 4
- Finding: The regexp allows `tests/api/graphql/`, but §1 and §9 say GraphQL specs are never generated and the QA tool's own path regexp is `web|api/rest`.
- Recommendation: Drop `api/graphql` from the merge regexp, or say why it is kept.

Sections that are solid: §0 part ordering and the §1 facts I could check; §2.3 sends `kind` only when the tool lists the capability; the §4.3 guard list is concrete and ordered.

Counts: B=1 M=18 N=3
