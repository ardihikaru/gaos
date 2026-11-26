# gaos-pr-overview — review round 2 (consolidated)

- Document: `docs/designs/gaos-pr-overview.md` (revision 2 reviewed; revision 3 is the result).
- Reviewers: comprehension (B0 M7 N6), readiness (B0 M13 N9), critic (B0 M9 N10), security (High 1, Medium 5, Low 3). The zoom-out lens was not re-run: round 1's findings from it were adopted whole.
- **No Blocker survived.** Revision 3 applies every Major, the High and the Mediums and was approved by delegation (the operator approved the plan and asked for the work to be done); a third round was not run. Recorded as a deviation from `docs/rules/gaos-design-review.md`, as round 3 of the auto-fix design was.
- Canaries were planted once for both rounds and removed after round 2; none appears in a report.

## What the round forced

- **A revoked base stayed usable (High).** `Defect.BaseOverride` was never cleared and not re-checked, so an admin who removed a base from the allowlist after a failed retarget still let any signed-in account unblock the card and open a pull request against it. Now: checked at use, cleared when the defect is fixed or closed and on every other generation change.
- **The old record was marked replaced before the new one existed**, so a failed retarget plus a merge of the old pull request stranded the defect. Now `RetargetedTo` is set by `OpenFixPR`; until then a "pending" rule protects the record, and a merge while pending moves the defect to `fixed` and blocks the card.
- **QA rows needed the automation repository in `Master.Repos`**, which enrols it in the review flow and plans agent turns over the QA tool's own output. Now a separate `AutomationRepos` setting.
- **BR-02**: the retarget moved the defect with no event. Now `DefectStateChanged` plus one `TaskPlanned` (actor, rule, reason); the cache fills are justified under D-092.
- Mechanics the implementers could not have guessed: `AdvanceFixPR`'s signature and the two early returns; the store's write path (`Result.PullRequests`, persist loop, boot load, `baseOverride`); `RetargetFixPR` needs `tokenKind`; the `qaCase` lookup would have read GitHub every 15 seconds; review rows for pull requests already open at release; a status and a sentence per refusal; the phase partition of counts and releases; D-092's content; BRD and landing-page counts; the poller's ownership re-check and a bound on polling.

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| A revoked retarget base remains usable | High | PROPOSE FIX | §6.4 checked at use, cleared by lifetime rules; `TestARevokedRetargetBaseOpensNothing` |
| Failed retarget then merge of the old PR strands the defect | Major (x3) | PROPOSE FIX | §4.2 pending rule; §6.3 step 3; §6.5 |
| `BaseOverride` never expires | Major | PROPOSE FIX | §6.4 lifetime |
| `RetargetFixPR` cannot run `autoFixPreconditions` | Major | PROPOSE FIX | `tokenKind` added |
| `OpenFixPR` writes `PullRequest` rows (contradiction) | Major (x3) | PROPOSE FIX | sentence removed; locked helper named |
| No event for `PullRequest` changes (BR-02) | Major | PROPOSE FIX | §3.4, D-092 |
| Review cards open at release get no rows | Major (x2) | PROPOSE FIX | §4.3 boot back-fill, row before the early return |
| QA repository must be a product repository; feeds the review flow | Major/Medium | PROPOSE FIX | `AutomationRepos` (§4.4), alternative recorded |
| Case code recomputed every 15 s, may call GitHub | Major (x2) | PROPOSE FIX | one write per job, `recorded`, no lookup |
| `AdvanceFixPR` signature and early returns | Major (x3) | PROPOSE FIX | `f.Base`; both guards given exactly |
| Persistence path of the vertex and `baseOverride` | Major | PROPOSE FIX | §4.1, §3.2 |
| Refusal statuses and sentences unspecified | Major | PROPOSE FIX | §6.1, §6.2 tables |
| `FixPR` older than 30 days never back-filled | Major | PROPOSE FIX | one extra read |
| Phases do not partition counts or releases | Major | PROPOSE FIX | §14 |
| D-092 content not stated; BRD "32 vertex types" missing | Major | PROPOSE FIX | §13 |
| No settings form; a post without the field clears it | Major | PROPOSE FIX | §6.1 note, §6.6 |
| OQ-37 called accepted | Major/Nit | PROPOSE FIX | "open" |
| Poller ownership re-check asserted, absent; no bound | Medium | PROPOSE FIX | §4.2, 90 days |
| Base name from GitHub unvalidated reaches a comment and a packet | Medium | PROPOSE FIX | validated; inside the untrusted section |
| `qaLink` passes with an empty origin | Medium | PROPOSE FIX | §4.4 |
| Defect move without `DefectStateChanged` | Medium | PROPOSE FIX | `setDefectStateLocked` |
| Route GitHub call before the ownership check | Low | PROPOSE FIX | §6.2 order |
| `BaseBranch` changes unaudited | Low | PROPOSE FIX | `base=`, `targets=`, `automation=` in the event |
| Anchors, "six" vs four properties, GET-only test table, round-1 report missing, naming, retention, cap counting | Nit | PROPOSE FIX | corrected in revision 3 |
