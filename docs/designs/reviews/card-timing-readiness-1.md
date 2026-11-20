# Readiness review: card-timing (`docs/designs/card-timing.md`, v1)

**Isolation note:** the harness put the contents of `.claude/CLAUDE.md` into my context as a system reminder. I did not open that file, and none of the findings below rely on it.

**Claims I checked and found correct:**
- Event, emit, write and load paths: `model.go:193-201`, `store_mem.go:444`, `store_arcade.go:1058-1071` and `:1738-1767`, `parseStamp` at `:1889-1899`.
- Every status change goes through `moveTaskLocked` (`runner.go:430-444`). The QA-catalog card gets a `TaskPlanned` before its move (`qacatalog_create.go:221-223`).
- DONE and CANCELED are terminal (`taskstate.go:87,89`).
- Run status values (`model.go:732-737`) and `runsFor` (`store_mem.go:1199`).
- The route patterns don't collide with `main.go:375-379`.
- sisflow drag skips buttons (`vendor/sisflow/board/dnd/pointer.js:7`).
- Counts: the register is 465, and 465 + 10 = 475. Go tests go 341 → 351 and dashboard tests 319 → 329. The decision log goes from seventy-nine to eighty, and D-081 is the next free number (`decision-log.html:18,1625,1632`).
- §3.1 (the new event-time field) and §3.2 (the timing algorithm) are clear, and the JSON examples add up.

---

### No phases, no exit conditions, no verification per change - Severity: Major
- Affected scope: §4 Change list, §5, §9 Rollout
- Finding: The 23 changes are one flat list. Nothing says what order to make them in, where the build has to be green before moving on, or which command checks each change. Some changes depend on others, and the plan doesn't say so:
  - The new `Store` method (#5) won't compile until #2 and #4 exist.
  - The templates (#12–#14) need `View.Timing` (#11) and the handler (#9).
  - The docs counts (#20, #21) must change in the same commit as the tests (#18).

  None of the project's own pre-done commands appear anywhere in the document (`qa-test-plan.md:162-168`, plus `go vet ./...` from `frontend.md:83-84`). The docs edits (#20–#23) have no check at all: neither `go run . check ./docs` nor `deploy/check-workspace.sh`.
- Recommendation: Split the work into phases, each with a command and an expected result. For example:
  1. P1: event time (#1–#3) plus TC-475. Exit when `go test ./... -run 'TestEventTime|TestStoreConformance'` passes.
  2. P2: the pure function and the store seam (#4–#7, #18, #19). Exit when `go test ./...` passes.
  3. P3: the routes and the UI (#8–#17). Exit when `go vet ./... && go test ./...` passes.
  4. P4: the registers and release (#20–#23). Exit when `pustaka check ./docs`, `check-workspace.sh` and the `grep "four hundred and"` all agree.

### Change list misses `dashboard/auth_test.go`, so the build goes red - Severity: Major
- Affected scope: §4 #10, §3.6, §3.7
- Finding: `TestEveryRouteIsClassified` requires every GET pattern to be on the `readOnlyGET` map, and fails with "is not on the read-only list" otherwise (`dashboard/auth_test.go:258`, `:310-313`). The two new routes, `GET /products/{id}/board/cards/{taskId}/timing` and `GET /ui/timing/{id}/{taskId}`, are not on that map. `auth_test.go` is not in the change list, so an implementer who follows the list exactly gets a failing test suite.
- Recommendation: Add `dashboard/auth_test.go` to §4 with the two exact `readOnlyGET` entries. Also say whether that test's register row or scenario prose needs updating, since the qa rule counts a change to what a test asserts.

### Chip duration format is undecided for both renderers - Severity: Major
- Affected scope: §3.5, §3.7 "A lead-time chip", §4 #14 and #15
- Finding: The chip shows a formatted duration such as `1h 55m`, but the card's `timing` JSON only carries integer seconds.
  - **Server board (`backoffice.html:395-405`).** The template has no duration helper; `render.go:128-144` has no `fmtDur`. The implementer has to pick between registering `fmtDur` as a template func (which edits `render.go`, not in the change list) and adding a pre-formatted string field to `CardTimingSummary`.
  - **sisflow board (`board.js`).** There is no JS formatter. The implementer has to pick between writing a JS copy of `fmtDur`, which can drift from the Go version, and using a server-formatted field.

  The same gap applies to the chip's `title` and `aria-label` text ("Lead 1h 55m · AI 22m · idle 1h 33m"). Two engineers would ship different shapes, and could end up with different strings on the two boards.
- Recommendation: Make one choice. For example, add `leadText` and `titleText` strings to `CardTimingSummary` (JSON `leadText`, `title`), filled by `fmtDur` in `decorateBoardTiming` and used by both renderers. Then list the exact fields. If a template func is chosen instead, add `dashboard/render.go` to §4.

### QA register update is under-specified - Severity: Major
- Affected scope: §4 #20, §5
- Finding: Change #20 says only "register rows and every count". The QA rule asks for more, and the design doesn't mention any of it:
  - an `h3` scenario section for each new test, under `#implemented`, quoting each test's exact `t.Fatalf` messages (`qa-test-plan.md:62-70`);
  - an update to the TC-124/TC-125 prose, which describes the `storeContract` table that now gains `CardTimingOfAPlannedTask` (`qa-test-plan.md:131-135`);
  - an update to the range map row in `.claude/rules/qa-test-plan.md:121`, from `TC-453…TC-465` to `TC-453…TC-475`. The same rule file also still says "currently all four hundred and sixty" at lines 73-74, which the update should correct.

  In addition:
  - TC-473 and TC-474 each test several things at once. TC-473 says "404 for an unknown card" without naming which of the two routes.
  - The register `data-tags` and the Positive/Negative split per row aren't given.
- Recommendation: In §5:
  - list every place in the register that must change;
  - say that each new TC gets an `Implemented` row and an `h3` quoting its `t.Fatalf`;
  - name the TC-124/125 prose edit and the rule-file range edit;
  - make TC-473 say which route answers 404 and with what body.

### Timing sheet: view-model, hidden state and error path not specified - Severity: Major
- Affected scope: §3.7 "The timing sheet", §4 #9, #11, #12
- Finding:
  - **`TimingView` has no field list.** §4 #11 adds `View.Timing *TimingView` but never defines it. The six content sections are prose, so the implementer has to invent the struct and the formatted fields: the abs-time strings, `<time datetime>` ISO values, bar widths, and the dimmed-zero flags.
  - **The hidden state is missing from the markup.** The modal snippet omits the `{{if not .Timing}}hidden{{end}}` condition that `answer-modal` has (`needs.html:70`), yet the text says an empty, hidden sheet is always in the page.
  - **The HTML route's error path is undefined.** For an unknown product or task, nothing says whether it should call `s.fail` (toast), return a 404, or render an empty sheet. Compare `handleAnswerSheet` in `handlers_bo.go:773-776`.
  - **The bar widths have no named mechanism.** The existing safe helper is `styleWidth` (`render.go:143`), and the design doesn't name it.
- Recommendation:
  - Write `TimingView` out field by field.
  - Show the full `timing-modal` opening tag including the `hidden` condition.
  - State the unknown-card behaviour of `GET /ui/timing/...`, for example `s.fail(w, r, ErrNotFound)`, which gives a toast.
  - Require `styleWidth` for the bar segments.

### `decorateBoardTiming` and `workflowTiming` signatures cannot work as written - Severity: Nit
- Affected scope: §3.3, §3.4, §4 #6
- Finding:
  - `decorateBoardTiming(&b, productID, now)` is listed in `timing.go` beside the pure functions, but it has to read `s.events`, `s.runs` and `s.tasks`. So it must be a `*MemStore` method, like `decorateBoardQa` (`qacatalog_store.go:383`).
  - `workflowTiming(id, cards)` says "lead runs to `now`", but `now` is not a parameter.
- Recommendation: Declare it as `func (s *MemStore) decorateBoardTiming(b *Board, productID string, now int64)`. Either add `now int64` to `workflowTiming`, or state that it uses `cards[0].AsOf`.

### Segment `bucket` vocabulary and JSON tags not fully enumerated - Severity: Nit
- Affected scope: §3.2 step 6, §3.5, §3.6
- Finding:
  - The segment `bucket` values are only shown by example (`backlog`, `waitingOperator`). The value for AI segments (`ai`?) and for terminal DONE/CANCELED segments (`""`?) isn't stated.
  - Which summary fields carry `omitempty` is only implied by "`finishedAt` is 0 (omitted)".
- Recommendation: Give the Go struct definitions with their JSON tags, and the closed list of `bucket` strings.

### D-081, changelog and graph.html content not stated - Severity: Nit
- Affected scope: §4 #21–#23
- Finding: The design claims D-081 but never gives the decision row's title or text. The v0.41.0 changelog note and the `graph.html` edit are also undescribed. Note that `graph.html:169` already lists the `Event (tenantId, ts)` index.
- Recommendation: Write the D-081 row text and the changelog sentence into the design.

### Line anchors drift slightly - Severity: Nit
- Affected scope: §3.1, §3.7
- Finding: `eventPayloadWire` is at `store_arcade.go:1038-1043`, not 1037-1042. `openAnswer` is at `app.js:153-164`, not 155-164.
- Recommendation: Correct the anchors.

Counts: B=0 M=5 N=4
