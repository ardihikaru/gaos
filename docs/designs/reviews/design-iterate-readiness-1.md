# Readiness review: design-iterate

Document: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/agent-ac38e26c8cafe21d1/docs/designs/design-iterate.md`

**Isolation note for the author.** I did not open any off-limits file. But during this session the harness inserted the contents of `.claude/CLAUDE.md` into my context as a system reminder, without my asking. I used none of it, and this report quotes nothing from it or from any marker comment. Account for this when you check the canaries.

**What holds up.** I checked the code claims behind the state machine (§3.1), the gate names (§3.2), the session rule (§3.5) and the reroll closure (§3.6). They match the code:
- `taskstate.go:79-95`
- `runner.go:430-444`
- `designgate.go:454-469, 594-612, 762-796`
- `turns.go:102-143`
- `board.go:162-191`

The new `sessionForLocked` case is nil-safe where it is placed. The register arithmetic is right if conversations merges first: 477+12+10 = 499 rows, and 353+12+10 = 375 Go tests.

### The iterate sheet has nowhere to get its data - Severity: Blocker
- Affected scope: §3.8 `GET /ui/iterate/{id}/{taskId}`; §3.3; §7 rows 5 and 12
- Finding: The sheet needs four things: the review artifact's findings, the design id, the send-back count, and the step 1–6 eligibility check. The handler only holds the `Store` interface. The only new interface method is `IterateDesign`, which writes. `BoardIterate` gives the count and the design id, but not the findings. No `Store` read method returns a review artifact's body. The design does not say:
  - which new read method exists, or its signature;
  - how `ArcadeStore` delegates it to `proj`;
  - whether `readOnly` serves it;
  - which function runs steps 1–6 without writing, so the sheet can show "the refusal sentence and no form".

  Two engineers would build two different read APIs and two different ways of checking eligibility.
- Recommendation: Specify a read method, for example `IterateSheet(ctx, productID, reviewTaskID) (IterateView, error)`, with the full `IterateView` struct. Factor steps 1–6 into a pure `iterateRefusalLocked(review) (designID string, n int, findings []ReviewFinding, err error)`, and have both `IterateDesign` and the sheet call it. Add the method to `store.go` (interface and `readOnly`), `store_arcade_blueprint.go` and the change list.

### Filling `BoardCard.Reason` in `boardOf` breaks TC-308 - Severity: Major
- Affected scope: §3.9 ("`Reason` is filled there"); §5; §7 row 10
- Finding: `boardOf` is shared by the live board and by the task-fold test (`taskfold_test.go:114-126`, `TestTheTaskFoldReproducesTheBoard`, TC-308). That test compares the two with `reflect.DeepEqual`. `foldTasks` does not rebuild `BlockReason` (`taskfold.go` never writes it). Every planned product has implementation cards that are `BLOCKED` with `design-unreviewed` (`store_mem.go:2165`). So the live board will carry `Reason` and the folded board will not, and TC-308 fails. The design also never says whether `Reason` is filtered to `BLOCKED`/`DEFERRED` cards, although its own comment suggests it should be.
- Recommendation: Pick one fix and write it down:
  - (a) fill `Reason` in a decorator, beside `decorateBoardIterate`, and clear it in `foldedBoard` the same way `Timing` is cleared (amend TC-308's prose); or
  - (b) keep it in `boardOf` and strip it in `foldedBoard`.

  Either way, state the status filter, and add `taskfold_test.go` to the change list.

### TC-497's file and the route-classification change are missing - Severity: Major
- Affected scope: §3.8 last paragraph; §5 TC-497 ("`handlers` test"); §7 row 15
- Finding: The design says `TestEveryRouteIsClassified` needs "no change". That is wrong. `auth_test.go:312-315` fails any GET pattern that is not in `readOnlyGET` (`auth_test.go:258`; see the timing entry at :273). So `auth_test.go` has to change, and it is not in the change list. TC-497 names no file, and no `handlers*_test.go` file exists in `dashboard/`. TC-497 also expects `Sec-Fetch-Site: cross-site → 403` without naming the route. `crossOriginOK` (`auth.go:560-564`) always passes GET, so that assertion only holds for the POST.
- Recommendation: Add `dashboard/auth_test.go` (the `readOnlyGET` entry) to the change list. Name TC-497's file. State that the 403 is asserted on `POST …/iterate` only.

### No phases, no dependencies between them, no per-phase exit conditions - Severity: Major
- Affected scope: §5 "Exit", §7
- Finding: The work is a flat list of 19 rows with one exit at the end. Some things must land before others:
  - the two guards (§3.1) before `IterateDesign`;
  - `EventPayload.Actor`/`Triage` before the store method;
  - the store method before the routes and the UI.

  None of this ordering is written down. Several changes have no check at all: `board.js`, `app.js`, `app.css`, and the `writeEvent` actor column. Nothing reads the column back, because `load()` reads only `payload` (`store_arcade.go:1753-1771`), so the conformance case cannot catch a wrong binding.
- Recommendation: Split the work into phases, each with a command and its expected result. For example:
  - P1: state machine, guards and timing (TC-494, TC-498, the TC-208 edit);
  - P2: payload, store method and packet (TC-490–493, 495, 496);
  - P3: routes and UI (TC-497, TC-499);
  - P4: docs and counts (`check-workspace.sh`, `pustaka check`).

  Give the client board and the actor column a check, or say explicitly that they are unverified.

### Identifiers and counts depend on another branch's merge order - Severity: Major
- Affected scope: header lines 5-6; §3.3 step 6; §5 Counts; §3.10
- Finding: Today the next free numbers are D-082, BR-57 and TC-478. The design claims D-083 and TC-490…499 on the assumption that a branch not present in this tree (`docs/designs/conversations.md` does not exist here) merges first. Two things in the design are inconsistent with that plan:
  - "Every number shifts… the assertions do not change" is false. The cap refusal sentence, which TC-496 asserts, contains the literal `(D-083)`.
  - "each number is 12 lower" covers only the TCs. The D-number shifts by 1, BR-57 does not shift, and the counts are different (487 rows, 363 Go tests, `dashboard/` 341).

  How `Actor` and `writeEvent` are reconciled with the other branch is also left to "whichever merges second".
- Recommendation: Add a two-column table with every identifier and count under both merge orders, including the cap sentence's text and the semver. Alternatively, take the actor sentence's D-number out of the Go string. State `Actor`'s exact semantics here, rather than pointing at a file that is not on this branch.

### The revision's packet does not carry the design being revised - Severity: Major
- Affected scope: §3.4, §3.5 "The designer"
- Finding: The design's packet gets `findings.md` (review body plus triage) but not the previous design artifact. The design relies on the resumed session to supply it. §3.5 itself says that rotation by age and a lost session open a new session (`turns.go:118-141`). In that case the designer gets findings pointing at sections of a document it cannot see. Implementers will differ on whether to add `.gaos/packet/design.md` to a design task's packet.
- Recommendation: Decide it. Either add the newest design artifact of the design task as `.gaos/packet/design.md`, with a citation, whenever `lastIterateLocked` is ok, and assert it in TC-491. Or state that it is withheld on purpose and what the designer does without it.

### Published counts and statements this change makes false are not in the change list - Severity: Major
- Affected scope: §3.10; §7 rows 16-19
- Finding: Adding BR-57 and an event actor leaves these wrong, and nothing in `check-workspace.sh` catches them:
  - "carries five rules" at `pustaka/docs/architecture/design-gate.html:170` and ":212";
  - "the five rules" at `pustaka/docs/assets/toc/architecture.js:104`;
  - "events carry no actor yet" at `pustaka/docs/product/open-questions.html:811-813`, OQ-34, and at line 519.

  `architecture.js` and `open-questions.html` are not in the change list.
- Recommendation: Add both files. Give the new wording for each line: six rules, and an actor recorded on the iterate move only. Whether OQ-34 stays open is your call to make and record.

### `moveTaskWithLocked`'s caption is ambiguous - Severity: Nit
- Affected scope: §3.3 step 8
- Finding: "The event's Text is `…by <actor>`; with an empty actor, `by operator`" sits next to "`moveTaskLocked` becomes `moveTaskWithLocked(id, to, rule, EventPayload{})`". One reading appends "by operator" to every scheduler and runner move.
- Recommendation: Say that the `by …` suffix is added only when `rule == MoveIterate`, and that every other caption stays the same byte for byte.

### `sessionOf` returns two values - Severity: Nit
- Affected scope: §0 Session, §3.8
- Finding: `sessionOf(r).Email` will not compile, because the function is `sessionOf(r) (session, bool)` (`auth.go:355`).
- Recommendation: Write `ss, _ := sessionOf(r); actor := ss.Email` and state that an empty actor is the fallback.

### Conditional doc edits left unresolved - Severity: Nit
- Affected scope: §3.10 (`graph.html` "if it lists payload keys"; the brd ToC "if it carries it"; the decision log "+1")
- Finding: `graph.html` lists no payload keys; it has the `at` paragraph at :130-135. `toc/product.js` does not contain "Fifty-six". The decision log's resulting word ("Eighty-…") is never stated.
- Recommendation: Say exactly what to do. For `graph.html`, either add a sentence after :135 about `actor` and `Triage`, or drop the row. State the exact decision-log word under each merge order.

### `check-workspace.sh` lines not named - Severity: Nit
- Affected scope: §3.10, §5 Counts
- Finding: `deploy/check-workspace.sh:112-116` (the hard-coded `353` and "three hundred and fifty-three") and `:127-128` (the QA count) also change. Step 3 of `qa-test-plan.md`, which §5 points to, does not list them.
- Recommendation: List every line and its new value.

### TC-494 asserts something a test cannot check - Severity: Nit
- Affected scope: §5 TC-494
- Finding: "the existing illegal-list row is gone" is an edit to TC-208, not something TC-494's test can assert.
- Recommendation: Move it to the TC-208 amendment line.

### QA scenario prose not planned - Severity: Nit
- Affected scope: §5
- Finding: `qa-test-plan.md` step 2 requires an `h3` with the positive and negative pair and the quoted `t.Fatalf` messages. The design plans only the register rows. TC-491, 492, 493, 495 and 496 also name no file.
- Recommendation: Name the new `h3` id and its place under `#implemented`, and name a file for each TC.

### Line references drifted - Severity: Nit
- Affected scope: §3.4 ("inside the existing `if kind != TaskKindReview` block (`store_mem.go:1437-1462`)")
- Finding: The block is at `store_mem.go:1443-1463`.
- Recommendation: Correct the reference.

Counts: B=1 M=6 N=7
