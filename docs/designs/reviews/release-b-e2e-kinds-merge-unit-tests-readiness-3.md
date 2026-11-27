I reviewed revision 3 of the design against the rubric, the three domain registers and the code it points at. It is not ready to build yet. There are no Blockers, but eight Major gaps would each lead two engineers to different results: several cases where §2.1 and §3.1 disagree, timestamps and fields the rules depend on that the data model doesn't have, and some reply and persistence behaviour left open. Moving part 4 out to its own design (§5) is mostly clean; the leftovers are all Nits.

All paths below are inside `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-release-b`.

### Staging creates of API cases on a `fullstack` repository contradict the kind's create folder - Severity: Major
- Affected scope: §2.1 table (`fullstack` → "`tests/web/` only (`WEB`, `E2E` cases)"), §3.1 step 2 ("`fullstack`: `WEB`, `E2E`, and also `API` when the product has no repository of kind `be`") and step 4 (labels `e2e-kind:K`), §7 TC-824.
- Finding: On a product with only a `fullstack` repository, step 2 selects `API` cases. Step 4 then labels them `e2e-kind:fullstack`, so `start` sends `kind: fullstack`. Under §2.1 the QA tool then limits the generator to `tests/web/`, which an `API` case can never be written to. So the plan makes cards the QA tool is told it may not fulfil. TC-824 only checks that they are planned, not that they can succeed. One engineer would label these cards `be`, another would leave the kind off, a third would ship it as written.
- Recommendation: Decide what an `API` create planned from a `fullstack` deploy carries: `e2e-kind:be`, or no kind label and no `kind` sent. Update §2.1's create column, and have TC-824 assert the label and the `kind` that is sent.

### The 7-, 14- and 30-day blocking windows have no timestamp to measure against - Severity: Major
- Affected scope: §3.1 step 3 (bullets 2–4), §7 B2 TC-825.
- Finding: `TaskRecord` (`dashboard/model.go:833-881`) has no field for when a card ended: no `EndedAt` or `UpdatedAt`, only `AutoAt` and `APIAt`. So "ended Failed or Canceled … less than 7 days ago" can't be computed. `PullRequest` (`dashboard/pullrequests.go:36-63`) has `OpenedAt` and `CheckedAt`, but `CheckedAt` is rewritten on every poll (`dashboard/fixprpoll.go:364`), so "unchanged for less than 30 days" has no source either. "Merged … at most 14 days" and "closed … less than 7 days ago" can only use `CheckedAt` of the final poll, and the design doesn't say so. Implementers would each invent a field, scan events, or pick `OpenedAt`.
- Recommendation: Name the clock for each rule. For example: `PullRequest.OpenedAt` for the 30-day rule; `CheckedAt` of a final row for the merged and closed rules; and either a new `TaskRecord.EndedAt` (with its schema and Arcade persistence in the change list) or the latest task-moved event's time for the Failed/Canceled rule.

### The capped-run answer loses the planned creates in ArcadeDB - Severity: Major
- Affected scope: §3.1 (`PlanStagingCards` signature; "409 … only when the run card itself is capped (creates are then still planned)"), §7 B2.
- Finding: `ArcadeStore.mutate` throws the `Result` away whenever the apply function returns an error (`dashboard/store_arcade.go:632-635`). Today a capped run returns `ErrPRSkipped` (`dashboard/prflow.go:291-293`). If `PlanStagingCards` keeps that convention, creates planned in the in-memory projection are never persisted: the memory and the database disagree, and a restart loses the cards. The design doesn't say whether a capped run is an `err` or a field in `res`.
- Recommendation: State that a capped run returns `err == nil` with a field such as `res.Skipped = "cap"` (or a `runCapped bool` return value), that `apiStaging` maps that to 409, and add an ArcadeDB case to TC-828 covering a capped run with planned creates.

### §4.2 writes a "reason" on the row and compares a head SHA that `AdvancePullRequest` never receives - Severity: Major
- Affected scope: §4.2 ("the row's reason says `merged head is not the approved commit`"; "`ghPRState` gains `HeadSHA`"), §7 B3 (`pullrequests.go`).
- Finding:
  - `PullRequest` has no reason field (`dashboard/pullrequests.go:36-63`).
  - `AdvancePullRequest` receives only `PRFields` (`dashboard/pullrequests.go:66-73`), and `pollRow` builds those from `ghPRState` without the SHA (`dashboard/fixprpoll.go:364`). Adding `HeadSHA` to `ghPRState` alone doesn't reach the store.
  - The planning happens inside a store method that must also call `gateUnassigned`, plan a task and emit events. None of that is listed for `pullrequests.go` or the ArcadeStore wrapper.
- Recommendation: Add `PRFields.HeadSHA`. Either add a row field for the refusal (`QANote`, with a schema statement in `schema/20-release-b.sql`) or replace "the row's reason" with a log line or event of named text. List the task-planning code path in `AdvancePullRequest` (Mem and Arcade) in §7 B3.

### Create and post-merge run cards have no title or card id - Severity: Major
- Affected scope: §3.1 step 4, §4.2.
- Finding: A `staging-create/` card gets labels, a role and a key, but no title and no id rule. `prCardID` (`dashboard/prflow.go:144-149`) builds ids from a `PREvent` with a PR number or a commit, and a create has neither. Titles are screened and republished to Brain Town (`dashboard/prflow.go:118-122`), so the choice is visible to people. `qa-merged/` has a title but no id rule.
- Recommendation: Specify both. For example: title `E2E automation of <caseID> (staging)`; id `prWorkflowID`-based `e2e-create-<sanitized productId-caseID>-<n>` and `e2e-merged-<…>-<prNumber>`, passed through `freeCardID`.

### The Done reason keys on the capability, not on what the job reported - Severity: Major
- Affected scope: §3.2 ("`it merges when its checks pass` when the tool listed `auto-merge`"), §4.1 (job `auto_merge`: `enabled` | `refused:<why>` | `off`), §7 TC-830.
- Finding: A tool that lists `auto-merge` but answers `refused:<why>`, or `off` because GAOS sent `false` in the Minimum band, would still tell the person "it merges when its checks pass". That is false, and nobody would merge the spec. The design also doesn't say how the 10-minute capability cache is read at the end of a job, as opposed to its start.
- Recommendation: Drive the reason from the job's `auto_merge` value: `enabled` → "merges when its checks pass"; `refused:<why>` → "a person merges it (auto-merge refused: <why, bounded and screened>)"; `off` or absent → "a person merges it". Have TC-830 cover all three.

### `auto_merge` sent to a QA tool that does not know the field - Severity: Major
- Affected scope: §4.1 ("GAOS adds one field to a `create` request: `auto_merge: true|false`"), §9 ("a missing capability leaves today's behaviour"), §7 B3 `qajobs.go`/`qaclient.go`.
- Finding: The text sends `auto_merge` on every create, `false` included. If the QA tool's decoder is strict (GAOS's own staging decoder refuses unknown fields, `dashboard/staging.go:234`), every create breaks before QA-tool change (2) ships, which contradicts §9. `kind` is gated on its capability; `auto_merge` isn't. Also, `startAutomation` posts a `map[string]string` body (`dashboard/qajobs.go:64-66`) and the `qaJobClient` interface (`:112-116`) can't carry these values, but the new signature isn't stated.
- Recommendation: Send `auto_merge` only when `auto-merge` is in the capability set. Define the new `startAutomation` input (for example a `qaStartIn{Product, CaseCode, Mode, Kind string; AutoMerge *bool}`) and the matching `fakeQAJobs` change in `qajobs_test.go`.

### The `case:` reason prefix is overwritten, and pending automation cards carry no case - Severity: Major
- Affected scope: §3.2 ("`start` records the case code … in the card's reason prefix (`qa-job:<id> case:<code> …`)"), §2.3 ("the intermediate reasons are not touched"), §3.1 step 3 bullet 1.
- Finding:
  - Every later write goes through `qaJobReason(id, text)`: "waits for you to approve" (`dashboard/qajobs.go:353-354`), "resumed after approval" (`:362`), "finished" (`:335`). Unless each of these also carries `case:`, a restart during the approval wait adopts a job with no case, which is exactly the case §3.2 means to fix. §2.3's "intermediate reasons are not touched" invites leaving them as they are.
  - An `e2e:automation` card from a PR that is still Pending or Ready has no reason yet. The blocking scan therefore can't see its case, and staging plans a duplicate create.
- Recommendation: Change `qaJobReason` to `qaJobReason(id, case, text)` for every write, and say so. Decide whether the scan treats a not-yet-started PR-planned automation card as blocking. One option is resolving its case through `caseCodeFor` under the lock, which is a network call. The other is accepting the race explicitly, covered by a TC-825 case.

### Open or merged spec rows from PR-planned creates do not block a staging create - Severity: Major
- Affected scope: §3.1 step 3 bullets 2–4.
- Finding: Bullets 2 and 4 cover only rows of `staging-create/` cards, and bullet 3 says "its row" without saying whose. Take a PR-planned `e2e:automation` card that ended Done with its spec pull request open, where the catalog still shows `!Implemented`. Bullet 1 doesn't apply because the card is no longer open, so the next deploy plans a create for the same case. The QA tool pushes `qa-auto/<code-lower>` and never forces (§1). The second job collides on that branch, fails, and then retries every 7 days.
- Recommendation: Key bullets 2–4 on any `qa` Pull requests row of the product whose `QACase` is the case, whatever card produced it. Add that case to TC-825.

### `SetAssigned` signature change reaches files outside the change list, and its reset/absence semantics are open - Severity: Major
- Affected scope: §7 B1 (`store.go`, `store_arcade.go`, `assign.go`, `handlers_assign.go`), §2.1.
- Finding:
  - Adding `kinds map[string]string` to `SetAssigned` breaks callers in `dashboard/reviewdefect_test.go:37,93,160` and `dashboard/reviewseverity_test.go:51`. Neither file is in the change list, so a write allowlist would block the build.
  - The meaning of the argument isn't fixed. Does a `reset` (`dashboard/handlers_assign.go:116-117`) pass `nil` meaning "keep"? Does a save with no `kind.*` fields clear every kind or keep them? Does `kind.x=""` delete the entry? What happens to a kind whose repository was later removed from `Master.Repos`?
  - "Its text naming the kinds" gives no format for the `RecordUpdated` text.
- Recommendation: List both test files. State that `nil` means unchanged and a non-nil map replaces the whole set, with `""` deleting an entry and only repositories in `Master.Repos` kept. State that reset passes `nil`. Give the exact event text, for example `…: kinds acme/route-api=be acme/route-web=fullstack`.

### `caseCodeFor` order for `qa-merged/` is not stated - Severity: Nit
- Affected scope: §4.2, §7 B3.
- Finding: `caseCodeFor` returns `*` for any `run` card before it reads the PR body (`dashboard/qajobs.go:238-240`). A `qa-merged/` branch placed after that line would run every case.
- Recommendation: Say that the `staging-create/` and `qa-merged/` branches come before the `mode == "run"` shortcut, beside `qaRunPrefix`.

### Key parsing of case IDs - Severity: Nit
- Affected scope: §3.1 step 4 and §4.2 key formats.
- Finding: `validCursor` accepts printable text (`dashboard/qacatalog.go:418`), so a case ID may contain `#`, `@` or `:`. The design doesn't name the split rule. The `qa-merged/` key also leaves out the automation repository, so two of the product's up to four automation repositories with the same PR number and case collapse into one card.
- Recommendation: Specify the split (first `:` after the prefix, last `#` or `@`) and include `owner/repo` in the `qa-merged/` key.

### "Effective band" names no function - Severity: Nit
- Affected scope: §4.1.
- Finding: There is no drift function. `PlanFloor`'s comment (`dashboard/model.go:603-608`) says effective is `ReadinessScore()` while the penalty is zero.
- Recommendation: Write `ReadinessBand(m.ReadinessScore())`, with a pointer to that comment.

### Inconsistent anchors and ambiguous paths - Severity: Nit
- Affected scope: §1 (`recordQARow` `qajobs.go:~395`) against §4.2 (`:412`); §7 B1 (`api_cards.go:369`, the actual call is at `:368`); §2.2 (`prflow.go:206` is the function, the call is at `:218`); §7 B3 `artifacts/v1/gaos-arcadedb-model.html` and §6 `README.md`, `deploy/check-workspace.sh`.
- Finding: Under the "bare name is in `dashboard/`" convention, `artifacts/v1/…` points at `dashboard/artifacts/…`, which doesn't exist. The real file is at the repository root. `README.md` is ambiguous.
- Recommendation: Use the root-relative path wherever a file isn't in `dashboard/`, and correct the line anchors.

### Kind suffix and the new Done reasons are not composed - Severity: Nit
- Affected scope: §2.3 against §3.2.
- Finding: §2.3 appends `(kind not sent: …)` to the "final reason". §3.2 replaces a create's final reason with `spec PR open (<url>); …`. Their order isn't stated, and neither is whether the suffix is added to `fail` class strings such as `qa-job-failed`.
- Recommendation: Give one combined example, and say whether the suffix applies to `r.fail` classes.

### Which phase writes which register text - Severity: Nit
- Affected scope: §6, §7 ("Every phase also updates the §6 pages").
- Finding: D-103, BR-62 and the BR-54 amendment cover all three parts, but the design doesn't say whether they land in B1 or grow phase by phase. The change to `.claude/rules/qa-test-plan.md` isn't described. The new qa-test-plan section's anchor and the TC range D-103 cites (TC-819…TC-835) aren't stated.
- Recommendation: Add a per-phase register line, for example "B1: D-103 (1) and BR-62's kind sentence; B2: the BR-54 create clause; B3: the rest". Name the anchor (for example `#go-release-b`) and the rule-file edit.

### Leftovers of the part-4 deferral - Severity: Nit
- Affected scope: the title, §0 table row 4, §2.3 (`"unit"` in the capabilities list), §6 ("D-104 is left for part 4"), §10 rows for part 4, §12 B4 checkbox, §14 notes citing §5.1/§5.2/§5.3.
- Finding: Moving part 4 to its own design (§5) is clean in substance: its requirements are listed, and "`unit:test` behaves as today" is stated. What remains:
  - The title still promises unit tests.
  - §14 points at subsections that no longer exist.
  - D-104 is reserved for a design that doesn't exist yet. If another decision lands first, the numbering breaks.
  - `unit` is defined in a contract this design doesn't build.
  - §12 has a Done-when item this design can never tick.
- Recommendation: Retitle. Point the §14 notes at §5 items 1–5. Leave D-104 unreserved ("the next free D-number"). Drop `unit` from §2.3 or mark it as defined by the part-4 design. Move B4 and the part-4 rows of §10 into §5.

The rest holds up: B1's label family and count (17 → 19, `dashboard/labels.go:87-105`), the `SetTaskLabel` refusal precedent (`dashboard/prflow.go:543-545`), and the D-103, BR-62 and TC-819…TC-835 numbering (next free after D-102, BR-61 and TC-818) are all correct.

Counts: B=0 M=10 N=7
