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

The new rule and the amendments this design makes, copied word for word from the
design's §10, so the operator can reverse any of them. (The design's §0 and §10
quote an excerpt of line 14 above; line 14 is the full text.)

- **BR-64** (new): "A unit test or build failure of a fix-target repository's existing tests, reported by the QA tool, files at most one open unit defect per repository, base and failing set, only while auto-fix is on, a fixer and a triager are assigned and the caps allow; its triage card is held until an auto-fix admin releases or cancels it or closes the defect. A failure of the QA job itself files nothing. When every failing test of a defect passes at a later merge on the same base, the defect closes — except that a defect whose triage card is still held is only marked, once, and an auto-fix admin closes it. A failure from a merge older than such a pass never reopens or files the defect."
- **BR-38** amended: "one live **model** turn per product: a card the QA-job runner is driving (a card whose planner-set key is a QA-runner key and whose reason carries the runner's job) holds no lease and is never dispatched to an agent."
- **BR-54** amended: "…and a unit job's result object, and the QA tool's capability answer, may set the final status and reason of that job's own `unit:qa` card and file a unit defect whose triage card is held; and a result in which every failing test of an open unit defect passed at a later merge cancels that defect's cards that hold no live turn — except a held triage card, which stays held for an admin. No other existing card is moved."
- **BR-61** amended: "…a card the QA-job runner drives (`unit:qa`) is not gated by assignment; the unit defect it files is (fixer and triager, BR-64)."
- **D-091 (8)** amended: a unit defect counts as backend by decision, as a review defect does.
- **OQ-38** amended: the missing QA routes now also block `unit:qa` cards, which need the `unit-run` mode, `watch`/`watched` and the `sandbox` object.
- **BR-44** clarification (not amended): a `unit:qa` card never enters Needs you.
- **BR-48** clarification: from this release every board move records the mover's alias.
- **Live from the release, whatever the switch:** the design's §2 changes (a–g) apply to every product from release day, even while unit runs stay dormant — among them, cards the QA-job runner drives (e2e cards included) leave the live-turn lease and take no agent seat, boot reconciliation no longer requeues them, `e2e:*` labels are refused on cards the QA tool does not run, an auto-fix admin may cancel a running QA-tool card, and every board move is attributed.
- **The residual risks** recorded in D-105.
