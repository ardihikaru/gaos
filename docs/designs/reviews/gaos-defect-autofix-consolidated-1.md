# gaos-defect-autofix — review rounds 1 and 2 (consolidated)

- Document: `docs/designs/gaos-defect-autofix.md` (revisions 1, 2, 3)
- Round 1 (revision 1): critic (B3 M17 N6), readiness (B4 M11 N5), security (High 5, Medium 5, Low 2).
- Round 2 (revision 2): critic (B2 M14 N3), readiness (B2 M11 N5), security (High 3, Medium 6, Low 5).
- Round 3 was **not** run: revision 3 applies every round-2 finding and was approved by delegation (the operator's
  instruction to implement). Recorded as a deviation from `docs/rules/gaos-design-review.md`.
- Canaries were planted in the seven standing files before each round and removed after it (restored from copies);
  no canary value appears in any report. The reviewers' definitions were changed after round 1 (see below).

## What each round forced

**Round 1** — the document named a real customer, repositories and product in a tracked file (D-085); the triage
card could not run under `GAOS_RUNNER=manual`; BR-54's guarantee was broken in substance; the safety caps read
policy text GAOS never parses; "Needs you" breaks BR-44; defect states had no events; the cap counted outcomes, not
spend; the fix was computed against a moving base; restart recovery contradicted never-overwrite; whole-file base64
replies; an unverifiable `executed: true`; CI exposure from a same-repository branch; the write token widened without
a bound. → Neutral names; structured `AutoFix` configuration; `READY`-not-`BLOCKED` waiting; a state table with
events; dispatch counting; blob-SHA pinning; one recovery rule; no `executed` field; `CIConfirmed`; a separate token.

**Round 2** — `dispatchNow` cannot be called under the store lock and its `TaskMoved` is not persisted from
`RunOnce`; defect cards would consume the PR flow's open-card cap; `GAOS_RUNNER=on` would bypass the switch; the
dedup keys blocked regressions and second attempts; a relabelled card could become an automatic pull request; the
path gate was a denylist chosen from attacker-supplied search terms; BR-35 was claimed as a gate but is not wired; the
rate-limit, retention and GitHub-App claims did not match the code; search-error handling; draft support; card end
states; the change list and counts. → `AutoDispatch` as a store method wrapped in `mutate` with a shared
`dispatchNow` core and `MoveAutoFix`; defect keys excluded from the PR cap; the scheduler never dispatches `defect:*`
in any mode; generations in the dedup keys; eligibility decided from the record and `SetTaskLabel` refusing the
family; a required `AllowedGlobs`; a CI workflow check; separate write token and admin list; search-and-replace
edits; honest limits; a full change list and exact phase exits.

## Isolation findings

Round 1: all three lenses reported `.claude/CLAUDE.md` attached to their context. Cause (confirmed by round 2): the
reviewer definitions told them to read `.claude/rules/*`, and opening a file under `.claude/` makes the harness
attach `.claude/CLAUDE.md` (the nested-memory loader), which `omitClaudeMd` (session start only) does not stop. Round
2, told not to open anything under `.claude/`, saw only the path-scoped rule files that attach on reading `docs/`.
The five definitions and the design-review skill now say so. The positive control of 2026-11-25 (a normal agent saw
the `CLAUDE.md` sentence, a reviewer did not) still holds for the session-start load.

## Implementation

The design was implemented in v0.52.0 (D-091) as revision 3; a third review round was not run and the revision was
approved by delegation. Every round-2 finding landed as its fix above. The deviations of the build are listed in the
design's section 18, none of which reopens a finding.
