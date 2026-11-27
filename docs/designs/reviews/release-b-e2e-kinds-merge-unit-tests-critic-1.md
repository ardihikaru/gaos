# Design review: Release B (e2e kinds, create-then-run, guarded spec merges, unit tests through the QA tool)

Document: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-release-b/docs/designs/release-b-e2e-kinds-merge-unit-tests.md`

Paraphrase: the design adds four things, one per phase. Each repository gets a kind (`be` or `fullstack`), and that kind narrows what the QA tool creates and runs. A staging deploy plans capped "create" cards for catalog cases that have no automation yet, then runs the suite. GAOS gets a new token so it can merge a QA spec pull request behind guards. A merged product pull request can have its unit tests run or created by the QA tool, and a failure opens a held `unit:fix` card.

---

### The design contradicts BR-61 and leaves out the BR-08 amendment D-102 (5) requires - Severity: Blocker
- Affected scope: §4.1, §6 Registers
- Finding and rationale: BR-61 says GAOS "never merges and never closes a pull request" (`pustaka/docs/product/brd.html:1307`). The design never mentions BR-61, and §6 does not amend it, so Part 3 breaks a rule that is in force. D-102 (5) also says this release "takes effect only when its own design is approved and BR-08, D-091 (2) and the out-of-scope merge line are amended" (`decision-log.html:2144`). §4.1 instead claims to "honour" BR-08, and §6 has no BR-08 amendment. The design therefore does not meet the condition its own parent decision sets. One smaller gap: D-102 (5) only promised a refusal in the Minimum band, while §4.3 also refuses in Insufficient. That is stricter, but it is a different rule and not recorded as one.
- Recommendation: In §6, add a BR-61 amendment ("…never merges, except a QA spec pull request under BR-62") and an explicit BR-08 amendment ("auto-merge is allowed only in Working and Complete, and only for automation repositories under BR-62"). §4.1 should then quote D-102 (5)'s condition and show each part of it is met.

### The merge wait runs into the runner's 40-minute active timeout, so "every outcome is Done" is false - Severity: Major
- Affected scope: §4.4
- Finding and rationale: `poll` adds the time a job is not waiting to `t.active` and fails the card once `t.active > r.timeout` (`dashboard/qajobs.go:317,371-375`). `qaJobTimeout` is 40 minutes (`qajobs.go:157`). A create job that already used, say, 15 minutes and then sits in the 30-minute `GAOS_E2E_MERGE_WAIT` goes past 40 minutes. The runner then calls `cancel` on a job that has already succeeded, and the card ends FAILED with `qa-job-timeout`. That is not a Done outcome, and it also sends `e2e:failure`.
- Recommendation: Say that the merging state is tracked separately (for example `t.merging` with its own deadline), that it is left out of `active`, and that the approval and active limits are not applied to it.

### Re-adoption after a restart would end a "merging" card Done without merging it - Severity: Major
- Affected scope: §4.4 ("the `qa-job:` prefix survives a restart")
- Finding and rationale: `start` re-adopts a card using only the job id taken from its reason (`qajobs.go:279-281`). It does not parse `merging since <unix>`. On the next `poll`, `jobStatus` answers `succeeded`, and `final()` sends the card straight to Done with "qa job succeeded" (`qajobs.go:332-336`). The merge state does not survive a restart, and the PR is left open with a misleading reason. Nothing in the design says that `poll`'s success branch changes either: today it deletes the tracker on success.
- Recommendation: Specify the change to `poll`'s final branch: on success, for a create card with merge on, enter or keep the merging state instead of going to Done. Specify how `start` reads `merging since` back from `BlockReason`. Add a restart test to B3's exit.

### The run hold needs a "planned at" time that TaskRecord does not have - Severity: Major
- Affected scope: §3.2
- Finding and rationale: The rule is "younger than `GAOS_E2E_RUN_HOLD` … since the run card was planned". `TaskRecord` has no creation timestamp (`dashboard/model.go:833-881`; its only time fields are `AutoAt`, `APIAt`, `LeaseUntil` and `LastBeatAt`). `planCardsLocked`'s `cardItem` carries no `Deps` either (`prflow.go:304`). The design says neither where the time is stored, nor whether it survives a restart in ArcadeDB, nor how `Deps` are written for PR-workflow cards.
- Recommendation: Name the field (a new `PlannedAt`, or reuse `AutoAt` and say so), its DDL or document property, and the change to `planCardsLocked` and the ArcadeDB `PlanPullRequestCards` that writes `Deps` and DEPENDS_ON.

### The create dedup key is per repository and permanent: it allows duplicates across repositories and blocks retries for good - Severity: Major
- Affected scope: §3.1 steps 2-3
- Finding and rationale: `taskByDedupKeyLocked` matches a card in any status, Done, Failed and Canceled included (`prflow.go:168-175,234`). Two consequences follow.
  1. Once a create card for `repo:case` exists, even one that ended `qa-approval-timeout` or `qa-job-failed`, no later deploy will ever plan it again. A one-off failure becomes a permanent gap, and nothing tells anyone.
  2. Catalog cases belong to the product, not to a repository. Two repositories of the same kind each have their own key. Step 2 only skips while a create card is open, so once repo A's create card is Done (merge off, PR still open), a deploy of repo B plans a second create for the same case. The QA tool then pushes `qa-auto/<code>` again, and that push is "never forced" (§1).
- Recommendation: Key the create card per product and case (for example `staging-create/<product>/<caseID>`). State what retry policy applies after a Failed or Canceled card: re-plan after N days, or only on a person's release. State that a Done card blocks re-creation until the catalog flips `Implemented` or the PR is closed.

### Create-then-run does not run what it created, in B2 or in B3 - Severity: Major
- Affected scope: §0 (the goal: "create it first, merge it, and run it"), §3.2, §3.3, §7 B2 exit
- Finding and rationale: In B2 (merge off) a create card ends Done once its PR is *open*, so the run that is released next runs the automation repository's default branch without the new spec. In B3, the approval can take up to 24 hours, and the merge comes after the approval, but the run is held for only 30 minutes. In practice the run almost never includes the new spec, and no follow-up run is planned after a merge. The B2 exit ("plans two create cards and a run") checks how many cards were planned, not the outcome the design promises. The strongest counter-argument is that Part 2 is "plan creates next to a run" and has nothing to do with "create then run". If so, the `Deps` and hold machinery cost complexity and give nothing.
- Recommendation: Either say plainly that the run never waits for new specs, and drop `Deps` and the hold, or plan a run for the case itself (`run` with that `caseCode`) once the merge succeeds. Then make the exit criterion "the new case's spec ran on staging".

### A `fullstack` default silently stops API and GQL cases running on staging - Severity: Major
- Affected scope: §2.1 table and its default, §2.3
- Finding and rationale: Today a staging run asks for `*`, which is every automated case (`qajobs.go:238-240`). Under the new rule, a repository with no entry is `fullstack`, `fullstack` maps only to `WEB` and `E2E`, and `run *` with `kind` "runs only the cases whose level belongs to the kind". So as soon as the QA tool advertises `kind`, every product that has not set kinds stops running its API and GQL cases on deploy. That is a regression that nobody chose. A repository described as "frontend plus backend" should not exclude backend levels either.
- Recommendation: Make `fullstack` cover every level for `run` (and, if wanted, only `WEB` and `E2E` for `create`), or treat "no entry" as "omit `kind`", which is today's behaviour. Add a B1 test that pins the behaviour when no kind is configured.

### Kind is undefined for API-planned e2e cards - Severity: Major
- Affected scope: §2.2 ("`PlanAPICard`'s e2e kinds do the same")
- Finding and rationale: An API automation-run card has the key `qa-run/<product>/<client>/<code|ALL>/<externalId>:e2e-run` and is not tied to any repository (`dashboard/api_cards.go:159-166,173-179`). "The kind label of `ev.Repo`" therefore has nothing to resolve to for an `APIKindAutoRun` card. Two engineers would build this differently.
- Recommendation: Say what an API run gets: no kind label and no `kind` sent, a kind field in the request, or a product-level default.

### Part 4 widens BR-44 and BR-54 without amending them - Severity: Major
- Affected scope: §5.2 ("the existing BR-44 path"; "Run failed → … GAOS plans a `unit:fix` card")
- Finding and rationale: BR-44's one exception is that "the QA-job runner may put an **e2e** card" in AWAITING_INPUT (`brd.html:1304`). A `unit:test` card is not an e2e card, and §3.3 even promises "BR-44 is not widened". BR-54 says QA data may add only an epic card, or defect cards when auto-fix is on (`brd.html:1350`). A `unit:fix` card planned from a QA job result is a new card caused by QA data. §6 amends BR-54 only for staging creates.
- Recommendation: Add to §6 a BR-44 amendment ("…an e2e or a QA-driven `unit:test` card") and a second BR-54 amendment for `unit:fix`, born BLOCKED and released only by an operator, as `qa-epic-added` is.

### The `unit:fix` card has no route to code and no defined output - Severity: Major
- Affected scope: §1 (the unit-test workspace "holds no product code; GAOS runs no code"), §5.2 ("runs as an ordinary card and ends in Review")
- Finding and rationale: By the design's own §1, the `unit-test` role's workspace has no product code. The `unit:fix` card is sent to that same role and is meant to "fix the failing tests". The design does not say:
  - how the agent gets the code, or which files reach the packet (BR-32 egress applies);
  - where its change goes (a draft PR? by whom, with which token, in which repository?);
  - what "Review" means for this card;
  - what dedup key it gets, and what happens when a second failing merge comes in while one `unit:fix` is open.
  
  The citation is also wrong: BR-50 is about the design-gate reviewer (`brd.html:1331`), not about unit tests.
- Recommendation: Define the `unit:fix` contract end to end: packet contents, output channel (for example D-091's draft-PR path with `GAOS_GITHUB_WRITE_TOKEN_FILE`), key, cap, and the end state. Otherwise drop `unit:fix` from B4 and leave the failure as a notice.

### The QA tool's failure summary enters an agent prompt without screening - Severity: Major
- Affected scope: §5.2 (`unit:fix` "failure summary as its context part")
- Finding and rationale: `junit_summary` is free text produced by running product code on another host. It can carry secrets from test output and instructions planted by an attacker. BR-53 says every QA-sourced text field is "screened for secrets and stripped … before it can reach an agent's prompt" (`brd.html:1349`), and BR-59 bars an outside client from writing free text onto a card (`brd.html:1353`). The design gives no size cap, no screen, no fence and no nonce, although D-100 (4) uses a nonce fence for a similar context (`decision-log.html:2132`).
- Recommendation: Say that the summary is bounded (bytes and lines), screened and stripped as BR-53 requires, and fenced with a fresh nonce. Better still, store only structured fields (test names, counts) and leave out the raw output.

### Part 4's inputs are undecided: the merge SHA and the run-or-create rule - Severity: Major
- Affected scope: §5.1 (`sha`), §5.2 ("Which mode")
- Finding and rationale:
  - A `pr.merged` `PREvent` has no commit (`Commit` is only set for staging, `dashboard/braintown_consumer.go:105`), so the source of `sha` is not given.
  - "A test file beside it" is not defined: same directory? a name pattern?
  - A PR that mixes covered and uncovered files has no stated mode. The wording ("if a changed source file has") would pick `run`, and the uncovered files would get no tests.
  - `run` executes the repository's whole command, so a failure unrelated to the PR still opens `unit:fix` for that PR.
- Recommendation: Name the SHA source (`merge_commit_sha` from GitHub, after `MergeChecked`). Define "beside" as an exact rule. Decide the mixed case: create for the uncovered files and run for the rest, or always both.

### `labelsAllowDispatch` cannot see the product, and there is no change list - Severity: Major
- Affected scope: §2.2, §3.1, §5.2, the whole document (README rule 3)
- Finding and rationale: `labelsAllowDispatch(labels []string)` takes only labels (`dashboard/labels.go:167`), so "refuses `unit:test` on that product" means changing its signature and every caller, and the design lists none of them. More broadly, the document has no file-level change list. These changes are implied but never named:
  - `prCardsFor` gains a product argument (`prflow.go:80`), and `apiSpec` calls it too (`api_cards.go:182`);
  - `openPRCardsLocked` must exclude `staging-create/` keys for `GAOS_E2E_CREATE_OPEN` to be "not against `GAOS_PR_MAX_OPEN`" (`prflow.go:178-195`);
  - the ArcadeDB store, the Agents page template, `notifyKinds` and `isE2ECard` all need changes;
  - nothing says that `qaJobRunner.Tick` must recognise `unit:test` (`qajobs.go:193-201,216`).
- Recommendation: Add a change list per phase, one row per file and function, with the TC counts per phase worked out from it.

### No alternatives section - Severity: Major
- Affected scope: the whole document (README rule 5)
- Finding and rationale: No alternative is considered anywhere, and this design adds GAOS's first merge power. Obvious ones that would need a reasoned rejection include: GitHub auto-merge with branch protection on the automation repository, which keeps GAOS out of merging; the QA tool merging its own spec PR after approval; and a per-product kind instead of per-repository kinds.
- Recommendation: Add §"Alternatives considered" with at least one rejected option per part and the reason it was rejected.

### A GAOS merge leaves no durable audit record - Severity: Major
- Affected scope: §4.4, §6
- Finding and rationale: The only record of a merge is a card's block reason, which later moves overwrite, and a WhatsApp notice on refusal. The design adds no event (BR-05 keeps the catalog closed, `brd.html:1230`), says nothing about updating the Pull requests row the job wrote with `PRStateOpen` (`qajobs.go:421-423`), and gives no record of which token, SHA and guard results were used. For an irreversible write, BR-28 treats an audit record as a safety property (`brd.html:1281`).
- Recommendation: Either reuse an existing event type with a fixed text that names the PR, the SHA and "merged by GAOS", or add a new event type as a BR-05 schema change listed in §6. Say whether the Pull requests row moves to merged.

### Which readiness score gates the merge is unspecified - Severity: Nit
- Affected scope: §4.3 guard 1
- Finding and rationale: `ReadinessBand(score)` (`dashboard/model.go:729`) is given no score. BR-09 says the gate's score is the effective one, raw minus drift (`brd.html:1241`).
- Recommendation: Write `ReadinessBand(effective)`.

### New notice kind missing from the closed contact vocabulary - Severity: Nit
- Affected scope: §4.4 (`e2e:merge-refused`), §6
- Finding and rationale: `notifyKinds` is closed (`dashboard/contacts.go:66-69`), and D-101 (5) recorded its last extension. `e2e:merge-refused` appears in neither, and §6 does not list it.
- Recommendation: Add the kind to §6 and say whether it is matched exactly.

### Citation drift - Severity: Nit
- Affected scope: §1, §2.2, §3.2, §4.1
- Finding and rationale:
  - `poll` is at `qajobs.go:310`, not `:315`.
  - `prCardsFor` is at `prflow.go:80`, not `:78`.
  - `labels.go:110-114` is `labelOrder`; the dispatch refusal the text cites is `labels.go:167-176`.
  - The out-of-scope merge line is `brd.html:1459-1460`; lines 1456-1457 are the generic-edge line.
  - The boundary prose is `brd.html:1382,1385-1388`.
  - `qajobs.go:64` is `startAutomation`, not "`qaOutbound`".
  - BR-50 is cited for unit-test workspaces but is about the design-gate reviewer.
- Recommendation: Correct the anchors.

### "Kind" is used in several senses - Severity: Nit
- Affected scope: §2, §5.2
- Finding and rationale: "Kind" means a repository's kind (`be`/`fullstack`), `PlanAPICard`'s API kinds (`APIKind*`), the artifact "of kind `context`", and "the e2e builder in two kinds".
- Recommendation: Call the new one "repository kind" (`repoKind`) everywhere, and write "API card type" for `PlanAPICard`.

### The `unit:fix` reason is wrong under the manual runner - Severity: Nit
- Affected scope: §5.2 (`ReasonUnitFixHeld`)
- Finding and rationale: Under `manual`, moving a card to Ready dispatches nothing (BR-56, `brd.html:1306`), so "move to Ready to let an agent fix" misleads.
- Recommendation: Word the reason so it is correct for every runner mode, as D-097 (4) does for the Board panel.

### Related open question not cross-referenced, and operator choices left open - Severity: Nit
- Affected scope: §3.1, §10
- Finding and rationale: B2 creates automations from a case code alone, with no pull request. That is the mechanism OQ-42 leaves open (`open-questions.html:954-957`). §10.2 also decides whether B4 ships `create` at all, which is still undecided under README rule 6.
- Recommendation: Cite OQ-42 and say whether this design answers it. List §10.2's two outcomes as two variants of the B4 exit.

Solid: §1's GAOS facts that I checked match the code (`caseCodeFor`, the `start` re-adoption, the Done on success, the `QaCase` fields, `ghTokenKind`, `operatorHolds` at `sendback.go:153`, `waitsOnAPerson` at `board_page.go:324`, `POST /agents/assign`). The identifier allocations are free: D-103, D-104, BR-62 and TC-819 are unused. The label count 17 → 20 is consistent with `labels.go:87-105`.

Counts: B=1 M=15 N=6
