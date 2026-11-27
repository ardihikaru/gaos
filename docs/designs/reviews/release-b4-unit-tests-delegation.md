# release-b4 unit tests: the operator's delegation (record)

This file records, verbatim, the operator's instructions under which the design
`docs/designs/release-b4-unit-tests.md` is approved by the agents' review rather
than by the operator in person. It is committed before the design's status
changes, so the anchor exists before any code is written.

- **Date:** 2026-11-27
- **Channel:** Claude Code session, operator prompt (the operator's own message, not relayed by an agent)
- **Context:** the operator asked the session to finish the remaining Release B
  tasks. The list included "The unit-test design (part 4), still deferred."
- **Words, verbatim (first message):**

  > dont ask for any of my further confirmation and decision. work and collaborate among agents yourselves. also update the docs accordingly. add data flow how it works so user can understand. anything about UI/UX, adopt this `https://emilkowal.ski/skill`.

- **Second message, the same day, same channel**, after the session reported that rulesets cannot be created on the private automation repository and that the unit-test design was approved but not built. Verbatim, with the private product name replaced by `[product]` (D-085) and the two pasted report blocks shown as `[…]`:

  > for this: ``` Partly done: the two auto-merge rulesets. They can't be created. GitHub answered "Upgrade to GitHub Pro or make this repository public" for the private [product] automation repository.``` . then, it is okay, set default auto merge as false then. then work among agents to decide the final decision. deep analysis. then apply it: ``` […] ``` . update docs as well. once done, make pr, merge to main. push. deploy latest

- **Third message, the same day, same channel**, after v0.67.0 was deployed and the
  repository's GitHub Actions run could not start (the organisation's billing). Verbatim,
  with the private product name replaced by `[product]` (D-085):

  > (1) `Fix the org's GitHub billing, then re-run the check on #4964.` - we dont care this. since we are using jenkins for the CI/CD. so we DONT need this PR `Merge #4964`. close it. (2) add this information properly in the platform `If tests that need a database fail, guard them with -short.`. explisitly tell with proper UI. also, work on this: `Switch Unit tests on in the [product] Auto-fix page`.

## What it delegates

1. The approval of the unit-test design, after the design gate's rounds leave no
   Blocker and no Critical or High security finding
   (`docs/rules/gaos-design-review.md`). The delegation does not waive that gate.
2. The delivery decision to split the operator's "run + create" choice into two
   designs: this one (`unit-run`) and `docs/designs/release-b4b-unit-create.md`.
3. (second message) The final decision on how unit tests run — decision C′ of rev 12:
   GAOS reads the repository's own GitHub Actions workflow run on the merge commit and
   executes nothing — and its implementation, pull request, merge and deployment. Spec
   auto-merge stays off by default (`QA_SPEC_AUTO_MERGE` unset, `autoMergeSpecs` false).
4. (third message, read with the first) Rev 14–17 of the design: Jenkins as a second
   result source (D-106), the "Before you switch this on" checklist with the `-short`
   rule on the Auto-fix page, their approval after the security rounds leave no Blocker
   and no Critical or High finding, the release v0.68.0 and its deployment, and turning
   Unit tests on for the product with the Jenkins source.

## What the operator is told in the final summary

The new rule and the amendments this design makes **as of rev 13** (copied from the design's §10).
The rev-11 amendments to BR-38, BR-54, BR-61 and OQ-38, the BR-44 clarification and the
release-day changes to e2e cards are **dropped**.

- **BR-64** (new): "A failing named CI check of a fix-target repository's merge commit, from the workflow the operator configured, files or updates at most one open unit defect per repository and base (and one for a failed build), only while auto-fix is on, a fixer and a triager are assigned and the caps allow; its triage card is held until an auto-fix admin releases or cancels it or closes the defect. A missing, unread or inconclusive check files and closes nothing. When the check passes at a later merge on the same base, the defect closes, except that a defect whose triage card is still held is only marked and an auto-fix admin closes it."
- **D-091 (8)** amended: a unit defect counts as backend by decision, as a review defect does; a reopened unit defect is fixable after an admin releases it.
- **OQ-40** amended: a unit fix adds test files on a same-repository `gaos/fix-*` branch; the §11 workflow's `pull_request` trigger runs them on the draft (read-only token, no secret in the test step) — a standing case of the question, guarded by `CIConfirmed`, the admin release and the draft-only pull request.
- **BR-48** clarification: a held unit card's release, cancel and close record the admin's alias; other moves unchanged.
- **Nothing is live before the switch:** with `UnitChecks.Enabled` false (the default) GAOS behaves as before for every product.
- **The residual risks** recorded in D-105.
