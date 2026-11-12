# Rule index — read the matching doc BEFORE starting work

This is the always-on entry point for GAOS's rules. It is short on purpose: it
names the other rules and the authoritative domain sources, and says which to
read for which question. Everything it points at loads on demand, not at every
session start.

## The rules in this directory

| Before doing… | Read first |
|---|---|
| **Creating, changing or deleting a test, an automation or a CI workflow** | `.claude/rules/qa-test-plan.md` — the register, the counts and the release hygiene |
| **Working in `.claude/` or `docs/`** — an agent, a skill, a rule, a design doc | `.claude/rules/agent-workspace.md` — the workspace layout, the design gate and the reviewer's deny-list |
| **Working on the dashboard's frontend** — `dashboard/templates/`, `dashboard/static/`, `dashboard/render.go`, `dashboard/view.go`, `dashboard/seq.go` | `.claude/rules/frontend.md` — the three skills to load and the stack they fit |
| **Anything else** | `.claude/CLAUDE.md` — the always-on invariants |

## The authoritative domain sources

These are the product's registers of record. They are not rules in the
`.claude/rules/` sense; they are where the product's own constraints live, and
a design review must read them.

| Question | Read |
|---|---|
| What must this change obey? What is the score, the budget, the egress rule, the session rule? | `pustaka/docs/product/brd.html` — the business rules (§9) and the requirement domains (§8) |
| What has already been decided, and why? | `pustaka/docs/product/decision-log.html` |
| What is still undecided, and what does it block? | `pustaka/docs/product/open-questions.html` |
| How is a docs page written? What CSS classes exist? | `pustaka/ai/AUTHORING_SPEC.md` |
| How do I write a docs page at all? | `pustaka/docs/guide/authoring.html` |
| Where is the topology, and which tier is alive? | `deploy/STACKS.md`, and `deploy/edge/README.md` before touching nginx on `clino` |
| How do I contribute, and what licence applies? | `CONTRIBUTING.md` |

## What is closed, and must not be opened by a change

Three vocabularies are closed. Opening one is a change to the closed set, not a
change to a document that lists it.

- **The event catalog** (`BR-05`). A new event type lands in the data-model
  document, `dashboard/events.go` and the fold together, or not at all.
- **The task-status vocabulary.** `Task.status` is constrained by a DDL regexp
  (`dashboard/schema/04-execution.sql`) and by
  `TestTheTaskStatusVocabularyMatchesTheDDL`.
- **The docs component vocabulary.** `pustaka check` rejects a page that
  invents a CSS class.

## Related

- `docs/rules/gaos-design-review.md` — the design gate's policy. The source of
  truth for the procedure.
- `docs/designs/README.md` — the design-document contract.


