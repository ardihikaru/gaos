# GAOS — project rules

Four invariants, and where to read the rest. Everything else is on demand.

## 1. A test or an automation is not done until the QA plan says so

**When you create, change or delete a test or an automation, update
`pustaka/docs/product/qa-test-plan.html` in the same change.** Not in a
follow-up, not "later" — the same commit. A test that exists in code and not in
the register is a test nobody knows to run, and this project has no CI to notice
the difference: `D-025` makes the product owner the runner for every check, and
that page is the only thing telling them what to run.

The mechanics — which rows, which counts, the retired rows, the forward
specifications — are in **`.claude/rules/qa-test-plan.md`**. Read it before you
touch a test, an automation or a CI workflow.

## 2. Three vocabularies are closed

- **The event catalog** (`BR-05`). A new event type lands in the data-model
  document, `dashboard/events.go` and the fold together, or not at all.
- **The task-status vocabulary.** `Task.status` is constrained by a DDL regexp
  and by `TestTheTaskStatusVocabularyMatchesTheDDL`.
- **The docs component vocabulary.** `pustaka check` rejects a page that
  invents a CSS class.

## 3. A feature-sized change starts as a design

Write `docs/designs/<feature>.md` before any file outside `docs/designs/`
changes, and have a zero-context reviewer read it before you implement. The
trigger list, the roles and the triage vocabulary are in
**`docs/rules/gaos-design-review.md`**; the workspace layout and the reviewer's
deny-list are in **`.claude/rules/agent-workspace.md`**. A bug fix, a typo, a
comment and routine maintenance do not need a design.

## 4. Where to read what

Start at **`.claude/rules/rule-index.md`**. It names the rule for the job in
front of you and the authoritative source for the product's own constraints —
the BRD, the decision log and the open-question register.

## Related, and worth knowing before a large edit

- `pustaka/ai/AUTHORING_SPEC.md` is the contract for every docs page. Do not
  invent CSS classes; the component vocabulary is closed.
- The BRD, the decision log and the open questions are registers, not prose.
  Answering a question moves a row from one page to another and adds a `D-`
  number — it never deletes the question.
- The Blueprint files in a product workspace are GAOS's. Never edit them; a
  change there is drift, and the drift gate refuses the next dispatch.


