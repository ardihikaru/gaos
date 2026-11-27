# Goldfish review: `docs/designs/release-b4-unit-tests.md` (rev 1)

**Paraphrase.** The design adds a product switch, `UnitQA`. When it is on, a merged pull request gets a `unit:qa` card, and the QA-job runner sends that card to the QA tool to run the repository's unit tests at the merge commit, and optionally to write missing tests. A job that fails on infrastructure only fails the card. A test failure files a deduplicated, capped `unit` defect whose triage card starts BLOCKED, and only an auto-fix admin can release it into the D-091 path. Tests the QA tool wrote come back as a `qa-unit` pull-request row, which is checked against `TestGlobs`.

**Counter-thesis.** The design says the D-091 path, the QA-job runner and the pull-request row code can be reused unchanged with one new kind added. The code says otherwise in at least five places. The runner throws away rows whose case is `*`. The defect schema rejects the record the design describes. The triage packet is shaped for QA defects. The merge commit has nowhere to live on the card. The defect planner has no gate equal to BR-54's auto-fix condition. Most of the design is new behaviour presented as reuse.

---

### The merge commit has nowhere to live on the card - Severity: Blocker
- Affected scope: §3 "Start commit", §4.2, §2 step 1
- Finding and rationale: §4.2 says the request body "is built only from the card's key and the product record". The key is `<owner>/<repo>#<n>:unit-qa`, which holds no commit. `PREvent.Key` builds the PR-number form whenever `PRNumber != 0` (`dashboard/braintown_consumer.go:122-127`), and the card id uses the number too (`dashboard/prflow.go:144-149`). Setting `ev.Commit` on the event therefore reaches neither the key, the id nor any card field. Some later Tick (`dashboard/qajobs.go:241-265`, `350-397`) has to send a `sha` it cannot read back. One engineer would put it in `BlockReason`, another in the title, a third in the key. Each choice changes how a restarted runner adopts the job (`qajobs.go:355-361`).
- Recommendation: Name the field that stores the commit: a key suffix (for example `#<n>@<sha>:unit-qa`, with the parsing updated), or a new `TaskRecord` field that is persisted. Say how a runner restart reads it back, and add the change to §10.

### The unit defect has no BR-54 planning gate - Severity: Blocker
- Affected scope: §6.1, §8 (BR-54 amendment), §12 non-goal "outside `TargetRepos`"
- Finding and rationale: BR-54 as amended lets a QA token plan dispatchable defect cards "only for a product whose operator has recorded the auto-fix switch, and within its caps" (`pustaka/docs/product/brd.html:1351`). The precedent, `reviewPlanRefusalLocked`, refuses when no fixer is assigned, auto-fix is off, or the repository is not a fix target or is an automation repository (`dashboard/reviewdefect.go:97-118`). D-102 (4) records the same conditions (`pustaka/docs/product/decision-log.html:2144`). §6.1 lists no precondition at all. With auto-fix off, a defect would still be filed and its card held. A card released with auto-fix off sits in READY for ever, because `autoFixPass` skips the product (`dashboard/autofix.go:300-303`) and the scheduler refuses `defect:*` cards (`dashboard/labels.go:182-184`). The non-goal "repositories outside `TargetRepos`" is stated but nothing enforces it.
- Recommendation: Add a `unitPlanRefusalLocked` with the same conditions as the review planner: auto-fix enabled, triager and fixer assigned, `Repo` in `TargetRepos` and not in `AutomationRepos`. Make the card reason carry the refusal. Write those conditions into the BR-54 amendment and BR-63.

### The defect record breaks the Defect schema and the key convention - Severity: Major
- Affected scope: §6.1
- Finding and rationale: §6.1 sets `State new`. The only allowed states are `open|triaged|fixing|fixed|closed` (`dashboard/defects.go:28-32`, `dashboard/schema/17-defects.sql:36`), so ArcadeDB rejects the write. `Defect.caseId` is `mandatory true, notnull true` (`schema/17-defects.sql:15`), and §6.1 never sets `CaseID`. The review defect sets one through `reviewCaseID` (`dashboard/reviewdefect.go:81-83,165`). Without it `Defect.Ref()` (`dashboard/defects.go:126`) has nothing to print in the card title (`dashboard/defecttriage.go:103`). The proposed key `unit:<productId>:…` also breaks the documented vertex-id form `<productId>/<caseId>` (`schema/17-defects.sql:10`, `reviewdefect.go:77-79`).
- Recommendation: Use `State: DefectStateOpen`. Define `CaseID = "unit:<owner>/<repo>@<h>"` and `Key = productID + "/" + CaseID`. Add a test that persists the record on ArcadeDB.

### "BR-54 still never moves an existing card" contradicts §4.3 and §5 - Severity: Major
- Affected scope: §4.3, §5 table, §8 BR-54 amendment
- Finding and rationale: BR-54 says no QA "result, case or sync changes the status or block reason of any existing task". Today the runner gets away with this because only the job's own status closes the card (`dashboard/qajobs.go:26-28`). This design uses QA data to set status:
  - `unit.result` absent or unknown makes the card FAILED `qa-unit-unreadable`.
  - `unit.result` `failed` writes a reason containing a count taken from QA data.
  - The capabilities answer moves the card to BLOCKED (§4.3).

  The amendment text in §8 denies all three.
- Recommendation: Amend BR-54 to say what is actually done: "a unit job's result object and the tool's capability answer may set the final status and reason of that job's own `unit:qa` card". Otherwise, key FAILED/BLOCKED on job status alone.

### `qa-unit` rows are dropped by today's code and collide with the PR flow - Severity: Major
- Affected scope: §7, §4.2, §10 row for `qajobs.go`
- Finding and rationale:
  - **Rows are dropped.** `recordQARow` marks the row recorded and returns without writing whenever `t.caseCode == "*"` (`dashboard/qajobs.go:519-521`), and §4.2 sets `CaseCode` to `"*"` for unit modes. It also hard-codes `Kind: PRKindQA` (`qajobs.go:523`).
  - **The kind is refused.** `upsertPullRequestLocked` rejects any kind other than review or qa (`dashboard/pullrequests.go:111-113`).
  - **Kinds collide.** The upsert never changes an existing row's kind (`pullrequests.go:104-106`). The QA tool's pull request is in the product's own repository, which the Brain Town flow follows. A `pr.opened` event, or the draft being marked ready, writes a `review` row with the same key first (`dashboard/prflow.go:290`, `pullrequests.go:351`). The same pull request also gets a reviewer card and an e2e card, and when it is merged, another `unit:qa` card.
  - **The head check has no data.** The "head branch must start `qa-unit/`" check runs before `HeadRef` exists. `recordQARow` never sets it; only the poller fills it (`pullrequests.go:70-80`).
- Recommendation:
  - Name the change to the `caseCode == "*"` guard and to the kind check at `pullrequests.go:111`.
  - Decide what the PR flow does with `qa-unit/*` heads: skip them, or plan them. Whichever, write it down.
  - Decide which kind wins when both writers reach the same row.
  - Run the branch check when the poller fills `HeadRef`.

### The `TestGlobs` check trusts the QA tool's own file list - Severity: Major
- Affected scope: §5 `qaUnitResult.Files`, §7
- Finding and rationale: GAOS checks `unit.files`, which is the QA tool's statement of what its pull request adds. The GitHub reader for the real list already exists (`pullFiles`, `dashboard/prflow.go:394-397`), and the review path compares it with `changed_files` to refuse a truncated list (`dashboard/reviewdefect.go:237-238`). A buggy or compromised QA tool can report test paths while the pull request changes source.
- Recommendation: Read the pull request's files from GitHub and require `len(files) == changed_files`. Apply `TestGlobs` and the `.github/` refusal to that list. Treat `unit.files` as advisory only.

### The machine API's `unit-test` kind is not addressed - Severity: Major
- Affected scope: §1, §3, §12
- Finding and rationale: D-099 clients can ask for unit tests. They plan through the same routing table (`dashboard/api_cards.go:126-148`; BR-59, `brd.html:1354`) using a synthetic `pr.merged` event with no commit, and that path does no GitHub re-read. With `UnitQA` on, the design's rule "no merge commit means no card, and `unit:test` is not planned either" (§3) silently empties every API unit request. `apiModelKind` still calls it a model turn. If the API keeps `unit:test`, an API request and a Brain Town merge for the same pull request get two different keys (`:unit-test` and `:unit-qa`), so the work runs twice.
- Recommendation: State what an API `unit-test` request does under `UnitQA`: refuse it with a code, keep the agent card, or plan `unit:qa` after reading the commit. Add `api_cards.go` to §10.

### A GitHub blip on the merge re-read drops the unit run with no retry - Severity: Major
- Affected scope: §3 "Start commit"
- Finding and rationale: The re-read at `dashboard/prflow.go:456-461` throws away its error and never returns `flowRetry`. Under the design, a transient 5xx or rate limit means no commit, so no card, and only a log line records it. For the opened path, the code already separates transient errors from permanent ones (`prflow.go:466-472`, `lookupTransient` at `430-436`).
- Recommendation: When `UnitQA` is on, return `flowRetry` on a transient lookup error and skip the event only on a permanent one. Test both cases.

### Nobody is told about a held card, and anyone can cancel it - Severity: Major
- Affected scope: §6.3
- Finding and rationale:
  - **No notice.** §6.3 sends no notice when a held card is born, so it waits until someone happens to look at the Board.
  - **Cancel is not guarded.** The 403 covers release only. `legalMove` lets any session move BLOCKED to Canceled (`dashboard/board.go:238-239`), and `handleMoveCard` is the only entry point (`dashboard/handlers_board.go:57-72`).
  - **A cancel silences the failure set for good.** Once cancelled, dedup means "the same failing set seen again … plans nothing" (§6.1), so a non-admin cancel hides that failing set permanently.
- Recommendation: Send a notice (reuse `unit:failure`, `dashboard/main.go:474-475`) when the held card is planned. Extend the admin-only guard to every move out of BLOCKED, cancel included, or define a cancelled hold as closing the defect so a later sighting files a new generation.

### "The D-091 path runs unchanged" is not true for a `unit` kind - Severity: Major
- Affected scope: §2 step 6, §6.2, §6.3 last line
- Finding and rationale:
  - **The packet is shaped for QA defects.** The triager's packet is `renderDefectPacket` (`dashboard/defecttriage.go:456-470`, `487-515`). It prints Case, Spec, Backend, Level and Operations, which a unit defect leaves empty. It has no slot for the failing test names beyond the excerpt.
  - **A failed context build has no defined path.** If `buildUnitContext` fails in the runner, `autoFixPass` later calls `buildDefectContext` with no `Operations` (`dashboard/autofix.go:318-322`, `defecttriage.go:314`). That produces `Built:true` with zero files, and every "easy" verdict is then downgraded to medium (`defecttriage.go:507`). The defect can never be auto-fixed.
  - **Kind checks everywhere.** Code paths branch on `Kind == DefectKindReview` at `autofix.go:194,329,397`, `autofixpr.go:239`, `retarget.go:81` and `fixprpoll.go:399,457`. A `unit` defect falls into the QA branches, retarget included, and the design never says that is intended.
- Recommendation: Add a unit packet renderer, or extend `renderDefectPacket` with a `unit` case. Define what happens when the runner's context build fails: block the card, or retry before planning. State for each `Kind` branch whether `unit` follows the QA path or the review path, with retarget the first to decide.

### The attestation rule cannot be enforced as written - Severity: Major
- Affected scope: §4.3, §3 (`AttestedAt`)
- Finding and rationale:
  - **The client cannot carry it.** `capabilities` returns `map[string]bool`, is cached for 10 minutes, and "an error keeps the last known set" (`dashboard/qaclient.go:57-95`). It cannot carry the `sandbox` object. "Re-reads at each start" and "the handler has just read" both contradict the cache, and a QA tool that is down keeps passing on stale data.
  - **The planner's use is undefined.** §4.3 says the planner "knows the capability by the product record", but no age check is given for `UnitQA.AttestedAt`, and nothing ever refreshes it after the admin saves.
  - **Clearing the block takes two actions.** The BLOCKED card's message says "an admin re-saves". Re-saving does not unblock the card; someone must also move it to Ready.
  - **Switch transitions are undefined.** Nothing says whether the runner keeps driving pending `unit:qa` cards after `UnitQA` is turned off, or what happens to `unit:test` cards already planned when it is turned on.
- Recommendation: Specify a new client method returning `(caps, sandbox, fresh bool)`, bypassing the cache for the admin save and for each unit start, and failing closed on error. Define the planner's check on `AttestedAt`, if it has one. Fix the BLOCKED message. Add the switch transitions to §3.

### Build failures, truncated lists and fixed-then-regressed sets are undecided - Severity: Major
- Affected scope: §5, §6.1
- Finding and rationale:
  - **Build failures have no row.** A merge that does not compile is the most common breakage. The design does not say whether that is a job failure (no defect) or `result: failed` with an empty `Failing` list. In the second case every build break hashes to one key.
  - **Capping at 50 breaks the hash and the count.** `Failing` is capped at 50, so any larger set hashes on whichever 50 the tool sent, and "n tests failed" undercounts.
  - **"Seen again" ignores state.** It does not say what happens when the existing defect is `fixed` or `closed`. The QA path reopens with a new generation (`dashboard/retarget_test.go:493`, `reopenDefectLocked`); this design plans nothing, so a regression is never filed.
- Recommendation: Add a `total` field and a `result: build-failed` row to the table. Hash a set the QA tool sorts and caps the same way every time, or hash only when `total <= 50`. Define reopening for a final defect.

### The admin release cannot be attributed with today's `MoveCard` - Severity: Major
- Affected scope: §6.3, §8 event list
- Finding and rationale: §6.3 says the release is "attributed to the admin in its event". `MoveCard(ctx, productID, taskID, mv)` takes no actor (`dashboard/board.go:330`), and `moveTaskLocked` writes `rule = action` with an empty payload (`dashboard/runner.go:445-446`). D-103 itself records a board release as "an unattributed board move" (`decision-log.html:2150`). Separately, §8 says `TaskPlanned`, `TaskMoved` and `RecordUpdated` carry every change, but the precedent planner emits `DefectReported` and `DefectTriaged` (`dashboard/reviewdefect.go:176-181`), and the Auto-fix view reads them (`dashboard/autofix_view.go:844,882`).
- Recommendation: Add an actor to `CardMove`, or add a `ReleaseHeldDefect(ctx, pid, taskID, actor)` store method using `moveTaskWithLocked` with an actor payload, and list it in §10. Correct §8 to name `DefectReported`.

### The change list is incomplete - Severity: Major
- Affected scope: §10
- Finding and rationale: Files the design depends on but does not list:
  - `pullrequests.go`: the kind check at `111` and `prKindLabels`.
  - `prview.go`: kind labels and filter at `27,30`.
  - `api_cards.go`.
  - `main.go`: the runner needs a `codeSource` for `fileAt`. Today it holds only `prSource` (`dashboard/qajobs.go:173`, `prflow.go:394-397`).
  - `schema/21` also needs the `Defect.state` and `caseId` decisions from the schema finding above.
  - Registers: `brd.html`, `decision-log.html`, `qa-test-plan.html` (TC-839 onward).
  - Published docs: `pustaka/docs/integrations/pr-agents.html`, which documents `unit:test`, and the changelog. A changed published page is itself a trigger under `docs/rules/gaos-design-review.md:50`.
- Recommendation: List each of these files with its change.

### Code anchors that do not match the repository - Severity: Nit
- Affected scope: §2 step 2, §5, §6.3, §7
- Finding and rationale:
  - `startAutomation` is at `qajobs.go:95`, not `:63` (that line is `waiting`).
  - `qaJob` is at `qajobs.go:32-46`, not `32-43`.
  - `board.go:330` is `MoveCard`; `MoveUnblock` is at `board.go:185`.
  - `matchAny` is defined at `egress.go:438`; `autofixcheck.go:276` is a call site.
  - `reviewdefect.go:143-196` should be `143-194`.
- Recommendation: Fix the anchors.

### The "family" wording contradicts itself - Severity: Nit
- Affected scope: §3 "The label"
- Finding and rationale: "A new family `unit` already holds `unit:test`" says new and existing in one sentence. No `labelFamilyUnit` constant exists (`dashboard/labels.go:61-71`). The requirement in `release-b-e2e-kinds-merge-unit-tests.md:138` asks for "its own family".
- Recommendation: Say whether a `labelFamilyUnit` constant is added, and whether `unit:qa` is planner-only.

### Runner notices say "e2e" for unit cards - Severity: Nit
- Affected scope: §2 step 4, §5
- Finding and rationale: `fail` sends `e2e:failure` / "e2e …" (`dashboard/qajobs.go:496-500`). The approval wait says "approve the spec (open the QA Inbox)" (`qajobs.go:447-452`).
- Recommendation: Give unit cards their own notice kinds and wording.

### The `autoClaimLocked` guard does nothing - Severity: Nit
- Affected scope: §6.3 first bullet
- Finding and rationale: Auto-claim only sees READY cards (`dashboard/autofix.go:311`), and an unblock clears `BlockReason` (`dashboard/board.go:397-400`). A card can never be READY and still carry `ReasonUnitHeld`.
- Recommendation: Drop the guard, or call it defence in depth and say what case it covers.

### Attestation wording is ambiguous, and the timeout outruns the sandbox - Severity: Nit
- Affected scope: §4.3, §3 "Timeout"
- Finding and rationale: The design says "exactly these values (a stricter one is fine…)" but does not say whether `network: none` is "stricter". The GAOS timeout can be up to 120 minutes while the attested sandbox limit is at most 1800 seconds, so any timeout above 30 minutes can never fire.
- Recommendation: Name the accepted values for each field, and cap `GAOS_QA_UNIT_TIMEOUT_MIN` at the attested `timeLimitSec` plus a margin.

### §0 credits D-103 (5) with more than it says - Severity: Nit
- Affected scope: §0
- Finding and rationale: D-103 (5) says only "decided and deferred to their own design" (`decision-log.html:2150`). The separate `qa-unit/*` identity is in `release-b-e2e-kinds-merge-unit-tests.md:138`.
- Recommendation: Cite the sibling design §5 for those details.

### The QA branch name collides on a re-run - Severity: Nit
- Affected scope: §9 "Pull request"
- Finding and rationale: A FAILED card that is retried (`board.go:232-233`) starts a second job for the same commit, which pushes to the same `qa-unit/<sha12>` branch.
- Recommendation: Add the job id to the branch name, or say that the QA tool reuses or refuses the existing branch.

Sections I found sound: §11 Alternatives, which gives a real reason for each rejection, and §12 Non-goals. The register IDs are free and in sequence: D-104 is next after `decision-log.html:2147`, BR-63 after `brd.html:1308`, TC-839 after TC-838, schema 21 after `schema/20-release-b.sql`, and labels go from 19 to 20 (`labels.go:95-115`).

Counts: B=2 M=12 N=7
