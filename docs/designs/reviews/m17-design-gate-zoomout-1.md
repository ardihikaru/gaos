# Design review — M17, the platform design gate

**Target:** `docs/designs/m17-design-gate.md` (revision 1)
**Lens:** Skeptical Architect (zoom-out)
**Date:** 2026-11-04
**Rubric:** `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`

### The gate chain stalls on the operator before the review can run - Severity: Blocker
- Affected scope: D3, §"The verdict, and what the gate does with it"
- Finding: A dependency is satisfied only by `DONE`, and the only write of `StatusDone` in the
  codebase is the operator's approve action. The design grants an automatic `REVIEW → DONE` to the
  review task only, so the design task still ends its run in REVIEW and waits for a human; the
  review depends on the design, so the review is never dispatched until the operator approves the
  design card. The document's claim that "the operator is not in that loop" is true of one hop of
  three and false of the other two, and the Risks table does not name it.
- Recommendation: State the full `REVIEW → DONE` path for each new task kind and which hops are
  operator-gated, or make the gate's dependency satisfiable by a status other than DONE, or say
  plainly in Risks what the operator's approval now gates and what it costs.

### The change list is incomplete, so its file count and its acceptance metrics cannot be computed - Severity: Blocker
- Affected scope: §"The change list", §"The verification plan"
- Finding: At least four files the change must touch are absent from a list that claims to be
  complete: `dashboard/taskfold.go` and `PlannedTask` (a replayed graph loses the kind otherwise),
  `pustaka/docs/assets/toc/product.js` (it carries four of the counts the QA rule requires to
  move), `deploy/check-workspace.sh` (it hard-codes the Go test count and six prose strings, and
  the sixteen new test functions alone break it), and `artifacts/v1/gaos-arcadedb-model.html` (the
  source of truth for the schema file whose header forbids editing it without changing the
  document). The counts do not follow either, and the document states none of them.
- Recommendation: Add the four files, derive the new numbers, and state them.

### BR-48's blocking precondition is absent from a design that claims to implement BR-46…BR-50 - Severity: Blocker
- Affected scope: the header, §"The verdict", the Risks table
- Finding: BR-48 says an open Blocker stops the dispatch, the task goes to the operator's queue,
  only the operator clears it, and the clearance is refused until the back office has a real
  identity — a blocking precondition on M17. The document never mentions the queue, the clearance,
  `signedIn()` or the identity gap, and its change list touches no back-office file. The design
  therefore ships a gate whose clearance is unauthenticated, against a rule that calls that a
  blocking precondition.
- Recommendation: Implement the refusal or record it as a named gap with its consequence — an open
  Blocker then has no authenticated exit — and put it in Risks.

### The task-count arithmetic contradicts the plan shape the document specifies - Severity: Major
- Affected scope: D3, the Risks table
- Finding: `PlanWorkflow` emits `4 + 2N` tasks; D3's shape adds two per story, so it is `4 + 4N`.
  Two stories: 8 → 12, not 10. Ten stories: 24 → 44, not "eighteen to thirty". The cost argument
  the operator is asked to accept is mispriced in both places it appears.
- Recommendation: Recompute from the change list and quote the sizes the operator will see.

### The reply contract cannot be chosen by kind: nothing carries the kind to the renderer - Severity: Major
- Affected scope: §"The prompt", the change list
- Finding: `RenderPrompt` chooses the contract from the packet alone and hard-codes the handoff
  contract; `Packet` has no kind field and the adapter has no store access. The same missing carrier
  applies to `freezePacketFor`, whose review branch must learn the task's kind.
- Recommendation: Say that `Packet` gains a `Kind` field, name the file, and say whether it is
  inside or outside the digest.

### The workflow-wide design artifact is written and never read - Severity: Major
- Affected scope: D3's diagram, §"The findings travel back"
- Finding: `arch` becomes kind=design, so it owes a design block recorded as a design artifact — but
  the only stated consumer is "the review task depends on the design task", and the review depends
  on `des`, not on `arch`. The artifact is dead weight: a task, a turn, an artifact row and an edge
  no reader consumes. Risks names only that it is not reviewed, not that it is not read.
- Recommendation: Carry the workflow-wide design to the per-story design task's packet, or drop the
  separate workflow-wide design task. If it is kept, name the reader.

### The drift report has neither storage for the pin nor a reader - Severity: Major
- Affected scope: D6, the change list
- Finding: D6 says each design artifact records the Blueprint version pinned when it was written,
  but `ArtifactRecord` has no version field and the change list adds none. The pin does exist — on
  the run, which the artifact references — but the document says neither. Separately nothing consumes
  the report: no handler, no store method, no event, so it would be a function only its own unit test
  can see.
- Recommendation: Name the derivation, and name the surface the report is read from — or say plainly
  that the first version is a query rather than a report.

### `Task.kind` must travel in the event payload and the fold - Severity: Major
- Affected scope: §"The data model", the change list
- Finding: The plan's record is the event stream, and `PlannedTask` is documented as carrying what
  the task fold needs to rebuild a card. It has no kind and the fold rebuilds from exactly that
  payload, so a replayed graph loses the kind and the review branch would take the implement path
  on a rebuilt board. `dashboard/taskfold.go` does not appear in the change list.
- Recommendation: Add the file and `PlannedTask.Kind`, and state the reading of a missing kind.

### Reusing the `gate` flag for the model override conflates "approval gate" with "model tier" - Severity: Major
- Affected scope: D2, D3's diagram
- Finding: The only existing route to `claude-opus-5` in the plan is the gate flag, which is
  persisted as the task's `approvalRequired` and counted by the wizard. If the review task takes
  the flag to get the model, a two-story plan reports "12 tasks · 3 approval gates" and the review
  task is stored as an approval-gated task — a different promise from the one the document makes.
- Recommendation: Name the mechanism that sets the review task's model, and use a separate field.

### Two of the forward specifications the design claims to implement contradict its mechanism - Severity: Major
- Affected scope: §"The verification plan", the register item
- Finding: TC-364 asserts the implementer's next packet carries "exactly one new kind, `findings`",
  and the design rides the findings as a `KindContext` part. TC-362 asserts the review's `verdict`
  is `ITERATE`, and D5 rejects a verdict field because the verdict is derived. The change says all
  seven move to implemented "with their exact quoted strings" — impossible for these two.
- Recommendation: State which value wins for TC-362 and TC-364 and why, and change the register item
  to "with the values this design fixes", naming the two that differ.

### Three copies of one vocabulary, with no statement of which is authoritative - Severity: Major
- Affected scope: §"The data model", the change list
- Finding: `Artifact.kind`'s list would live in three places, and widening the 12 ALTER is
  redundant work: a later ALTER supersedes it, and the precedent is to add a new file and leave the
  old one alone.
- Recommendation: Name the authoritative pair, say that 12-questions.sql is left as it is because
  the new file supersedes it, and drop the change-list item.

### Citation drift in a document whose rule is that every claim carries a file and a line - Severity: Nit
- Affected scope: §"The events", §"The data model", D4
- Finding: Three cited line numbers are off — `TaskMoved` is at `events.go:65` not `:59`, the
  `CREATE PROPERTY … IF NOT EXISTS` precedent is at `13-vendors.sql:11` not `:15`, and
  `Handoff.MustKnow` is at `handoff.go:78` not `:75`. The other ~45 citations resolve correctly.
- Recommendation: Correct the three and re-run the sweep the document's own promise implies.

### Two new fences, one hard-coded parser - Severity: Nit
- Affected scope: D4, the change list
- Finding: `handoffBlocks` is written for exactly one tag and the document says "two more fences
  follow the same shape" without saying who owns the shared shape.
- Recommendation: Say that the fence reader takes the tag as a parameter.

### `disposition` has no defined meaning in the gate - Severity: Nit
- Affected scope: D5
- Finding: D5 borrows the policy's four dispositions and calls them "the policy's four dispositions",
  but in the policy those are the *author's* triage labels for a reviewer's finding — a different
  thing from a field a platform reviewer emits. Nothing says what each disposition asks the
  implementer to do, nor what the gate does differently for each.
- Recommendation: Give each disposition a stated consumer and meaning, or drop the field.

### `Task.kind`'s default does not cover rows that predate the property - Severity: Nit
- Affected scope: §"The data model"
- Finding: A property added to a type that already exists in deployed databases does not backfill the
  rows written before the migration; the Go side has to read an absent kind as `implement`.
- Recommendation: State that a missing kind reads as `implement`.

### The change list describes a file that already exists, with different DDL - Severity: Nit
- Affected scope: §"The data model", the change list
- Finding: The change list calls `dashboard/schema/14-design-gate.sql` a created file and calls
  `13-vendors.sql` the newest, but the file is already in the working tree and its DDL differs from
  the document's — no default, with the reason written out.
- Recommendation: Reconcile the document with the tree.

Counts: B=3 M=8 N=5
