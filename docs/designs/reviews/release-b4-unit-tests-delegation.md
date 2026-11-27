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

The new rule and the amendments this design makes **as of rev 12** (copied from the design's §10;
rev 12 replaced the QA-tool runner with a read of the repository's own CI check, decision C′,
after the operator's second message of 2026-11-27: "work among agents to decide the final
decision. deep analysis. then apply it"). The rev-11 amendments to BR-38, BR-54, BR-61 and
OQ-38, the BR-44 clarification and the release-day changes to e2e cards are **dropped**.

- **BR-64** (new): "A failing named CI check run of a fix-target repository's merge commit files at most one open unit defect per repository, base and failing set, only while auto-fix is on, a fixer and a triager are assigned and the caps allow; its triage card is held until an auto-fix admin releases or cancels it or closes the defect. A missing or inconclusive check files and closes nothing. When the check passes at a later merge on the same base, the defect closes, except that a defect whose triage card is still held is only marked and an auto-fix admin closes it."
- **D-091 (8)** amended: a unit defect counts as backend by decision, as a review defect does.
- **OQ-40** amended: a unit fix adds or edits test files on a same-repository `gaos/fix-*` branch, whose CI then runs them — a standing case of the question, guarded by `CIConfirmed`, the admin release and the draft-only pull request.
- **BR-48** clarification: a held unit card's release, cancel and close record the admin's alias; other moves unchanged.
- **Nothing is live before the switch:** with `UnitChecks.Enabled` false (the default) GAOS behaves as before for every product.
- **The residual risks** recorded in D-105.
