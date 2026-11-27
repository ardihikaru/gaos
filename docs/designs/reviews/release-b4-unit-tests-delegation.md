# release-b4 unit tests: the operator's delegation (record)

This file records, verbatim, the operator's instruction under which the design
`docs/designs/release-b4-unit-tests.md` is approved by the agents' review rather
than by the operator in person. It is committed before the design's status
changes, so the anchor exists before any code is written.

- **Date:** 2026-11-27
- **Channel:** Claude Code session, operator prompt (the operator's own message, not relayed by an agent)
- **Context:** the operator asked the session to finish the remaining Release B
  tasks. The list included "The unit-test design (part 4), still deferred."
- **Words, verbatim:**

  > dont ask for any of my further confirmation and decision. work and collaborate among agents yourselves. also update the docs accordingly. add data flow how it works so user can understand. anything about UI/UX, adopt this `https://emilkowal.ski/skill`.

## What it delegates

1. The approval of the unit-test design, after the design gate's rounds leave no
   Blocker and no Critical or High security finding
   (`docs/rules/gaos-design-review.md`). The delegation does not waive that gate.
2. The delivery decision to split the operator's "run + create" choice into two
   designs: this one (`unit-run`) and `docs/designs/release-b4b-unit-create.md`.

## What the operator is told in the final summary

The business-rule amendments this design makes, each by number, so the operator
can reverse any of them:

- **BR-38:** a card the QA-job runner drives holds no live model turn.
- **BR-54:** a unit job's result may set its own card's final status and reason,
  file a held unit defect, and close (or, for a held defect, mark) a unit defect.
- **BR-61:** a card the QA-job runner drives is not gated by assignment.
- **D-091 (8):** a unit defect's triage card is born BLOCKED and held for an
  auto-fix admin.
- **The residual risks** recorded in D-105.
