# Design review — M17, the platform design gate

**Target:** `docs/designs/m17-design-gate.md` (revision 2)
**Lens:** Tech Lead / the zero-decision rule (readiness)
**Round:** 2
**Date:** 2026-11-04
**Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`

**Validation performed** (read-only): `cd dashboard && go test ./...` → ok; `go build ./...` → clean;
`cd pustaka && go run . check ./docs` → 22 pages valid; `cd pustaka && go test ./...` → ok;
`./deploy/check-workspace.sh` → 6 failures, all of them the planted canaries (see finding 12).
Register cross-checks done with `grep` against `qa-test-plan.html`, `decision-log.html`,
`brd.html`, `open-questions.html`, `implementation-plan.html`, `toc/product.js`, `toc.js`,
`README.md` and `.claude/rules/qa-test-plan.md`. Off-limits files were not opened.

### The kind is not persisted in ArcadeDB — Severity: Blocker
- Affected scope: §"The phases" (phase 1) and §"What revision 2 changed" (item 5)
- Finding: The phase-1 table specifies for `dashboard/store_arcade.go`: "`kind` in the task upsert map
  and the task load path; the artifact load path widens to handoffs, designs and reviews". Only the
  artifact half landed. The task upsert map writes no `kind`, and the load query does not select it,
  so the `TaskRecord` it rebuilds has an empty `Kind`. After a restart every task's kind is `""`,
  the review branch never runs, and the gate is inert on a restored graph — precisely the class of
  bug revision 2 item 5 was raised to fix. The conformance suite passes because no case asserts the
  kind survives a reload.
- Recommendation: Write `"kind": t.Kind` into the upsert map, select it in the load query, rebuild
  with `Kind: taskKind(r.Kind)`, and add a store-conformance case that a design/review task is still
  one after a reopen.

### `validatePlan`'s stated rule rejects the document's own plan — Severity: Blocker
- Affected scope: D1.1 against D3 and D6
- Finding: D1.1 says "A plan with a task of kind `implement` that has no `review` prerequisite … is
  rejected before the first write". D3's plan has four implement tasks with no review prerequisite —
  `p1`, `arch`, `sec`, `dev`. Implemented literally, D1.1 refuses every plan the planner produces. The
  code implements a different rule: per story, *at least one* implementation task waits directly on a
  review, with a fallback for a plan with no story at all. That rule depends on `PlanTask.Story`, a
  field D1 never mentions, and it is nowhere in the document.
- Recommendation: State the rule the gate actually enforces, say which field carries the story, and
  strike the literal per-task reading.

### The isolation claim is stated at the packet level; the code isolates at the prompt level — Severity: Major
- Affected scope: §"The mechanism, in one page", verification row 1, D4
- Finding: The document says the review packet's "two parts are `.gaos/packet/design.md` and
  `.gaos/packet/rules.md`" and lists `.gaos/packet/handoff.md` among the six withheld paths. The code
  keeps the contract part in the review packet and `RenderPrompt` drops it and re-renders the contract
  text. The test asserts on the *prompt*, not the packet. D-070 clause 5 states the accurate version.
  The design document is the odd one out.
- Recommendation: Restate at the prompt level: the review *packet* holds contract, rules and design;
  the *prompt* holds exactly design and rules. Name which of the six paths are absent from the packet
  (five) and from the prompt (six).

### "A task with no kind reads as `implement`" is not what `taskKind` does — Severity: Major
- Affected scope: §"The data model"
- Finding: `taskKind` returns the empty string for an absent kind, while its own comment says an absent
  kind is an implement task. The effect then depends on the call site: the packet branch treats `""` as
  implement, while every gate site treats `""` as *not* implement. A legacy task gets the author's
  full packet but is never held, released or cleared by the gate.
- Recommendation: State the default as one rule — an absent kind is `implement` everywhere, an
  unrecognised kind is refused — and name the function that owns the normalisation.

### The regexp is not anchored, and the test does not check the "both ways" claim — Severity: Major
- Affected scope: §"The data model", verification row 14
- Finding: The document specifies `regexp '^(implement|design|review)$'` and asserts "**The regexp is
  anchored.**". The delivered file says `regexp 'implement|design|review'` — no anchors — and the file
  carries no note about it. The required test does not test it either: it only checks that each Go
  constant appears in the DDL alternation, so the "both ways" claim in the verification table is
  asserted nowhere.
- Recommendation: Write the SQL the document specifies, and add the reverse assertion the table
  promises.

### "The dispatch is refused" for an unknown kind is not implemented anywhere — Severity: Major
- Affected scope: §"The data model"
- Finding: `knownTaskKind` has no production caller — only the test uses it. `taskKind` normalises the
  unknown kind to `""`, and `CreateWorkflow` writes that normalised value onto the record, so the
  unknown kind is erased rather than refused. The task then takes the implement branch and receives
  the author's brief, handoff and operator answer.
- Recommendation: Specify the refusal point and make it a named test, or drop the claim.

### "A review always opens a new session, whatever its history" contradicts the next paragraph — Severity: Major
- Affected scope: §"The session"
- Finding: The document says the reason is selected in `sessionForLocked`, "which reads the task's kind:
  a task of kind `review` always opens a new session, whatever its history". The next paragraph says
  the opposite — a retry resumes the review's own session. The code implements the second.
- Recommendation: State the rule once, precisely.

### The scope validation rule in the document is not the rule in the code — Severity: Major
- Affected scope: D5
- Finding: The document states the scope "must not contain whitespace beyond the path, it is bounded
  at 200 runes, and the path part must not be absolute". The code checks no whitespace at all, adds two
  rules the document never states — the path must contain a `/` or a `.`, and the line must parse as a
  positive integer — and a scope carrying prose with a slash passes untouched.
- Recommendation: State the whole rule set as the code holds it, or make the code match the document.

### The note's bound is never stated — Severity: Major
- Affected scope: D5
- Finding: The scope has an exact bound in the document; the note is only "one line, bounded", with no
  number. The code bounds the note at 300 runes. A forward specification whose asserted value is not
  stated leaves the implementer to pick a number, and the number is user-visible in the persisted
  finding.
- Recommendation: State the note's bound as a number, and add it to the parser's rules.

### The phase tables and the test count cannot both be true — Severity: Major
- Affected scope: §"The phases", §"The change list", §"The counts that move"
- Finding: The counts table says "seventeen new top-level `func Test…`", and phase 1 creates the file
  with one test. The remaining sixteen belong to phases 2, 3 and 4, but the file appears in no
  phase-2, phase-3 or phase-4 file table. The phase tables are the stated write-allowlist source.
- Recommendation: Name the file in every phase that adds a test, with the tests it adds.

### Phase 4 enumerates "the counts" but not all of them; two register files carry stale counts — Severity: Major
- Affected scope: §"The phases" (phase 4), §"The registers"
- Finding: Two further numbers moved and are not named: the per-package split (221 → 238) and the QA
  rule's own derived paragraph. The delivered state shows the gap: `README.md` still reads "221 in
  `dashboard/`", and `.claude/rules/qa-test-plan.md` still reads "**356 rows in total. 221 in
  `dashboard/` and 22 in `pustaka/`** — 243 Go tests". The validator asserts the totals but not the
  split, so it passes over both.
- Recommendation: Enumerate every string that changes, not a category.

### Phase 4's exit condition does not hold in the delivered tree — Severity: Major
- Affected scope: §"The phases" (phase 4)
- Finding: The exit condition is "`./deploy/check-workspace.sh` passes". Run during the round it exits
  1 with 6 failures: "canary left behind" in the six standing files. The cause is that the planted
  marker matches the canary pattern. Five of those six files are outside M17's change list, so an
  implementer working inside the stated allowlist cannot make the stated exit condition pass.
- Recommendation: Restate the exit condition as a scoped command, with the planted canary excluded,
  and say which pre-existing failures are out of scope.

### "No new blocking reason" contradicts D6's three reasons — Severity: Major
- Affected scope: §"Goals", §"Non-goals", D6
- Finding: The Goals say the gate needs "no new task status and no new blocking reason", and the
  Non-goals repeat it. D6 then specifies three new block reasons, and the code adds them.
  `TaskRecord.BlockReason` is a free-form field, so "no new blocking reason" cannot mean "no new
  reason values"; as written the document contradicts itself.
- Recommendation: Say what is meant: no new task *status*; the block reason is free text and the gate
  writes three new values.

### The change list's numbering allocates "17" twice — Severity: Nit
- Affected scope: §"The change list"
- Finding: "The seventeen modified files" runs 1–17, then "The eleven register files" restarts at 17.
  An allowlist built from this list has two different files at index 17.
- Recommendation: Restart the register list at 18.

### The phase-1 exit condition carries a drafting artifact — Severity: Nit
- Affected scope: §"The phases" (phase 1)
- Finding: The exit reads "`cd dashboard && go run . schema-check` — hmm, there is no such command.
  *Exit:* `cd dashboard && go test -run TestTheClosedVocabulariesMatchTheDDL ./...` passes". An approved
  document carries the reasoning that produced the correction inline.
- Recommendation: Delete the false start.

### "The four review reports" is five files — Severity: Nit
- Affected scope: §"The change list"
- Finding: The change created five files under `docs/designs/reviews/`, and the count omits the
  consolidated triage.
- Recommendation: Count the consolidated triage explicitly.

### "4 + 4N" does not hold when the plan picks no story — Severity: Nit
- Affected scope: D3
- Finding: The planner has a fallback branch that emits a design/review/implement/verify chain with
  no story, giving 8 tasks where the formula gives 4.
- Recommendation: State the no-story case, or say the formula covers only plans with a story.

### Two file-boundary assignments do not match where the code landed — Severity: Nit
- Affected scope: §"The phases" (phases 2 and 3)
- Finding: Phase 2 assigns the generalised fence reader to `handoff_reply.go`, and phase 3 assigns the
  gate's rule strings to `board.go`; both landed in `designgate.go`. No write escapes the allowlist, but
  the phase table is not a reliable map.
- Recommendation: Put the scanner and the rule strings where the document says, or move the assignments.

### BR-47's amended text differs from the document's quote — Severity: Nit
- Affected scope: §"The registers"
- Finding: The document quotes the amended rule as "A reviewer's packet carries …"; the landed rule
  reads "A reviewer sees …".
- Recommendation: Quote the landed wording.

### One rule string is never named — Severity: Nit
- Affected scope: D6
- Finding: D6 names `design-delivered` and `review-cleared`, but not the rule carried by the initial
  `BLOCKED` move (`gate-unwired`) nor the two failure rules. These strings travel in the `TaskMoved`
  event and are visible in the timeline.
- Recommendation: Name all the rule strings in D6, in one row of the table.

Counts: B=2 M=11 N=7
