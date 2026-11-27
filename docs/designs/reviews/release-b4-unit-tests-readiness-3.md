# Readiness review: `release-b4-unit-tests.md` (rev 3)

Most of the design is ready to build. One blocker remains: the external-tasks API mapping in §4.2 does not work as written. Five Majors and eight Nits follow. I checked every `file:line` claim below against the code in `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-unit-design`.

**Sections that hold up:**
- **Identifiers:** D-104, BR-63 and TC-839…TC-865 are the next free values. The registers end at D-103, BR-62 and TC-838. 27 cases matches the phase table (4+4+5+6+4+3+1).
- **Schema and labels:** `21-unit-defects.sql` is the next schema number. "Labels 19 → 20" is correct (`labels.go:119-140` lists 19 labels).
- **Files:** every path in §12 that is listed as modified exists.
- **Planner hook:** `withRepoKind` only touches `staging.deployed` events (`repokinds.go:31-34`), so replacing the merge spec's labels with `[unit:qa]` drops nothing.
- **Hold paths:** the board-move guard and the drift-release path in §7.3 are correctly anchored (`board.go:330`, `sendback.go:153-159`, `store_arcade_blueprint.go:173-177`).
- **Phases:** the order is buildable and each phase has a command and an observable exit.

---

### The API mapping cannot produce "the same `unit:qa` card under the same key" - Severity: Blocker
- Affected scope: §4.2 "The external-tasks API"; §12 row `dashboard/api_cards.go`; TC-845
- Finding: The design says only "`apiSpec` also passes through `withUnitQA`". That change is not enough, and on its own it plans nothing:
  1. `apiSpec` keeps only the spec whose `Job == apiJob(in.Kind)` (`api_cards.go:182-188`). `apiJob(APIKindUnitTest)` is `prJobUnitTest`, which is `"unit-test"` (`api_cards.go:133-134`). After `withUnitQA` the spec's `Job` is `"unit-qa"`, so nothing matches and `apiSpec` returns an empty spec.
  2. The key comes from `apiKey`, which is `apiEvent(in).Key(apiJob(in.Kind))` (`api_cards.go:167`). It stays `…:unit-test`. So `apiLookupLocked` (`api_cards.go:248-269`) never finds or adopts the Brain Town `…:unit-qa` card, and both cards can exist for one merge.
  3. Neither `apiSpec(productID, in)` nor `apiKey(productID, in)` receives a `Master`, so neither can evaluate `withUnitQA`'s conditions without a signature change.
  4. `apiModelKind(APIKindUnitTest)` is true (`api_cards.go:126`), so `AutoDispatch: in.Dispatch && apiModelKind(...)` (`api_cards.go:372`) would try to dispatch the `unit:qa` card. The new `dispatch` refusal in §4.2 forbids that.
- Recommendation: Specify all four pieces:
  - `apiJob` and `apiKey` take `m Master` (or a `unitQA bool`) and answer `prJobUnitQA` when `withUnitQA`'s conditions hold.
  - `apiSpec` matches on the mapped job.
  - `apiModelKind` is false for a mapped `unit-test` request; name the API answer when `dispatch:true` is sent for one.
  - `apiLookupLocked` and `APICardLookup` get the master they need.
  
  TC-845 should assert that an API request arriving after Brain Town adopts the Brain Town card, and the reverse.

### Closing the defect bypasses the admin-only hold - Severity: Major
- Affected scope: §7.3 "Every path out of BLOCKED, and the hold"; §10 BR-63 ("held until an auto-fix admin releases or cancels it")
- Finding: `POST /products/{id}/defects/{ref}/close` (`main.go:578`) only checks for a signed-in session (`handlers_autofix.go:158-165`). `MemStore.CloseDefect` then calls `cancelDefectCardsLocked` (`defects.go:606`), which cancels the held triage card. Any non-admin can cancel a held unit card this way. The table in §7.3 does not list this path, and TC-858 would not catch it.
- Recommendation: Add the path to the table and decide how it behaves. Either `handleCloseDefect` requires `autoFixAdmin` for a `DefectKindUnit` record (state the 403 text), or the store refuses to close a unit defect whose triage card is BLOCKED with `ReasonUnitHeld` unless an admin flag is passed. Extend TC-858 to cover it.

### Reusing `BaseOverride` for the merged base clashes with its retarget meaning - Severity: Major
- Affected scope: §7.1 `BaseOverride` row; §5.1 "the fix is opened where the tests failed"; §7.5 table rows `fixprpoll.go:399` and `fixprpoll.go:457`
- Finding: In today's code a non-empty `BaseOverride` means "a retarget is pending", and several sites read it that way:
  - `fixPRReplacedLocked` reports `pending` (`pullrequests.go:327-331`), which makes `AdvanceFixPR` leave the defect alone (`fixprpoll.go:448-449`).
  - `fixerPartsLocked` appends a "previous fix" section (`autofix.go:405-409`).
  - The claim loop treats the card as a retarget card (`autofix.go:350-361`).
  - The UI shows it as an override (`prview.go:465`), and `prview.go:533` and `retarget.go:87` read it too.
  
  Worse, the "QA path" for a fix pull request closed unmerged clears it: `d.Gen++` then `d.BaseOverride = ""` (`fixprpoll.go:464-465`). The person-only second attempt therefore opens against `AutoFix.BaseBranch`, not against the merged base. That contradicts §5.1.
- Recommendation: Store the merged base in its own property, for example `UnitBase` (schema 21). Have the base selection at `autofixpr.go:235-238` read `UnitBase` for a unit defect, and leave every retarget site untouched. If `BaseOverride` must be kept, list each of the sites above with the unit behaviour and amend `fixprpoll.go:465` for `DefectKindUnit`.

### The store cannot send the held-card notice, and the once-a-day limit has no home - Severity: Major
- Affected scope: §5.5 third `unit:failure` row ("the store … at most one per defect per UTC day"); §7.3 last sentence; TC-861
- Finding: The store never calls the notifier. `MemStore` has no notifier field (no match for `notifier` in `store_mem.go`), and every `Notify` call is in a runner, a handler or `main.go` (`qajobs.go:451,499`, `main.go:475`, `handlers_board.go:226`, `api_tasks.go:308`). Two engineers would put the call and the "once per defect per UTC day" state in different places: inside the store's lock, in the runner after `PlanUnitDefect`, or in memory versus persisted.
- Recommendation: Name the sender. A good option is the runner, after `PlanUnitDefect` returns a planned or reopened defect, calling `r.nt.Notify` on the triage card. Name where the per-day marker lives, for example a new defect property `UnitNoticedDay` written by `PlanUnitDefect`, or an in-memory map that a restart resets. State whether a reopen counts.

### The "every reason" grammar contradicts the outcome texts - Severity: Major
- Affected scope: §5.4 "Every reason of a unit card the runner writes is `qa-job:<id> case:* sha:… mode:… <text>`"; §5.1 table; §6 table; TC-855
- Finding: Several reasons in the design cannot or do not follow the grammar:
  - Bare classes go through `fail()`, which writes the class alone (`qajobs.go:496-497`): FAILED `qa-job-failed`, `qa-unit-unreadable`, `qa-unit-no-commit`.
  - Some reasons are written before any job id exists: DONE `unit tests switched off; nothing ran`, BLOCKED `ReasonUnitNoCommit`, BLOCKED `ReasonUnitAttest`.
  - DONE texts produced through `doneReason` carry no prefix today.
  
  Whether the grammar wraps final and pre-start reasons is undecided. TC-855's expected strings depend on it.
- Recommendation: Restrict the grammar to the RUNNING and AWAITING_INPUT reasons (the ones adoption reads). State that DONE, FAILED and BLOCKED reasons are exactly the texts in the §5.1 and §6 tables. Note that `ReasonUnitAttest` and `ReasonUnitNoCommit` are constants compared by equality.

### The verdict list in §8.2 does not cover every check - Severity: Major
- Affected scope: §8.2 checks 1-4 and the verdict list; TC-863
- Finding: The verdicts are `checked`, `not a draft`, `author is not the unit identity`, `head is not qa-unit/*`, `from a fork` and `files outside TestGlobs: <path>`. Four cases have no verdict:
  - `pullFiles` returns fewer than `changed_files` (the review path calls this "file list incomplete", `reviewdefect.go:370-371`);
  - a file under `.github/` that matches `TestGlobs`;
  - a path that fails `contextPathOK`;
  - a `pullFiles` read error.
  
  It is also unstated whether a transient error leaves the note unchanged or writes something.
- Recommendation: Add explicit verdicts (for example `file list incomplete`, `workflow file: <path>`, `unsafe path`). State that a read error writes no note and retries on the poller's backoff (`fixprpoll.go:345-359`).

### `SetDefectContext` after a refusal - Severity: Nit
- Affected scope: §6 "Poll order" step (3)
- Finding: Step 3 runs "when (1) succeeded". On a §7.2 refusal `PlanUnitDefect` returns `Defect{}` (§7.1), so there is no key to write to. For a dedup hit it is unstated whether the newer merge's context replaces the stored one.
- Recommendation: Say "when (1) succeeded and (2) returned a non-empty `Defect.Key`". State whether a dedup hit overwrites the context.

### `buildUnitContext` has no stated source for the pull request's files - Severity: Nit
- Affected scope: §7.5 signature `buildUnitContext(ctx, code, d, sha, failingFiles, prFiles)`
- Finding: The design does not say who calls `pullFiles` for `prFiles`: the runner through `r.src` (which `prSource` provides, `prflow.go:394-397`) or the code source. It also does not say what happens when that read fails.
- Recommendation: Name `r.src.pullFiles` and state that on error `prFiles` is empty and context is still built from the failing files.

### Who sends `unit:files` - Severity: Nit
- Affected scope: §5.5 `unit:files` row versus §8.1 "Any fails: … `unit:files` goes out"
- Finding: §8.1's notice comes from the runner (`recordQARow`), not from the pull-request poller through `NotifyPR` as the table says.
- Recommendation: Add a second `unit:files` row (sender: the runner, `Notify` on the `unit:qa` card, with its text).

### The handler table misses the case with no QA tool configured - Severity: Nit
- Affected scope: §4.1 handler refusal table
- Finding: `s.qaOut` may be nil (`main.go:97`, `main.go:424`). The table has no row for that case. It also does not say whether the `unit-create` check uses the same fresh read or the cached `capabilities()`.
- Recommendation: Add the row `enabled` with `s.qaOut == nil` or `s.qaOut.why != ""` → the attestation text. State "the same fresh read" for the `create` row.

### The phase 7 exit does not run `check-stacks.sh` - Severity: Nit
- Affected scope: §9 exit commands; phase 7 changes `deploy/check-stacks.sh` and `compose.prod.yaml`
- Finding: The shared exit commands do not include `./deploy/check-stacks.sh`, so the new assertion is never shown to pass.
- Recommendation: Add `./deploy/check-stacks.sh` to phase 7's exit command.

### One change-list row and some anchors are not exact - Severity: Nit
- Affected scope: §12 row "`dashboard/store_arcade_defects.go` (or where `writeDefect` lives)"; §4.1 anchors
- Finding: `writeDefect` is at `store_arcade_defects.go:36`, so the parenthetical can go. Two anchors are off: `autoFixFromForm` is at `handlers_autofix.go:62`, not 60; `autoFixError` is at `:102`, not 111. §5.4 says "`shaRe` 40 hex", but `shaRe` also accepts 64 (`staging.go:186`).
- Recommendation: Drop the parenthetical and fix the anchors. For `reasonField`, name a 40-hex-only check, or say that `shaRe` is used as is.

### OQ-38 will become wrong and is not in the change list - Severity: Nit
- Affected scope: §12 registers; `pustaka/docs/product/open-questions.html:921-929`
- Finding: OQ-38 says the missing QA routes block "the e2e cards, and nothing else in the pull-request flow". After this design, `unit:qa` cards depend on the same routes plus `unit`/`unit-create` and the sandbox object (§11).
- Recommendation: Add `open-questions.html` to §12 with an OQ-38 amendment naming the unit modes. Alternatively, state why it is left unchanged.

### Turning `UnitQA` on can plan two unit cards for one merge - Severity: Nit
- Affected scope: §4.2 "Switch transitions"
- Finding: The keys differ (`…:unit-test` versus `…:unit-qa`, `prflow.go:241`). A replayed `pr.merged` after the switch is turned on therefore plans a `unit:qa` card next to the existing `unit:test` card.
- Recommendation: State whether that is accepted. If not, make `withUnitQA` skip when a `:unit-test` card exists for the same pull request.

Counts: B=1 M=5 N=8
