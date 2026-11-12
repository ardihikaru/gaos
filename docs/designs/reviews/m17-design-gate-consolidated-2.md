# M17, the platform design gate — the consolidated triage, round 2

**Target:** `docs/designs/m17-design-gate.md` (revision 2)
**Round:** 2
**Date:** 2026-11-04
**Reports:** `m17-design-gate-review-2.md` (readiness), `m17-design-gate-zoomout-2.md`
(zoom-out), `m17-design-gate-critic-2.md` (critic),
`m17-design-gate-security-review-2.md` (security)

**65 findings from four lenses: 4 blockers, 2 criticals, 30 majors, 25 nits.** Thirty-one of them are
the same defect found by more than one lens.

This round reviewed the version that had been **implemented**, and that turned out to matter: four of
the findings were in the **code**, not in the document, and two of them would have shipped a gate
that did not work.

## The four blockers

| finding | lenses | what was wrong | what changed |
|---|---|---|---|
| The design never reaches the model | security SEC-1 | `instructionOf` rendered five part kinds and `KindDesign` was not among them, so the reviewer was handed the rubric and a contract telling it to attack a document it could not see | `KindDesign` is in the kind order, and the test now asserts on `RenderPrompt`'s output rather than on a helper that re-implemented the filter |
| The kind is not persisted in ArcadeDB | readiness B1, security SEC-3 | the task upsert map wrote no `kind` and the load query selected none, so a restart erased every kind and the gate was inert on a restored graph | `writeTasks` writes it, the load query selects it, the rebuild normalises it, and the conformance suite now asserts a kind survives a reopen |
| The gate can open on a review that had no design | security SEC-2, critic B1 | a screened-out design recorded nothing, so the review ran with no design, found nothing, and the gate released the implementation — silently | a screening refusal is now a `BLOCKED` task with a reason and a rule; a review with no design is `BLOCKED` with reason `design-missing`; and the gate clears only on its **own recorded verdict**, so an operator's approval is not a verdict |
| The design is Approved with no round against revision 2 | critic B3 | the document claimed five lenses in a round where four ran, and no round had ever tested the implemented version | this round, with the canary probe and its positive control |

## The two that were the document's fault alone

| finding | lenses | what changed |
|---|---|---|
| `validatePlan`'s stated rule rejects the document's own plan | readiness B2, critic M1 | D1.1 now states the rule the code enforces — per story, at least one implementation task waits directly on a review — and names `PlanTask.Story` as the field that carries the story |
| The review packet does carry the contract part | readiness M1, security SEC-7, critic B2 | the document now distinguishes the **frozen slice** (contract, rules, design) from the **rendered prompt** (design and rules only), and says which of the six withheld paths are absent from the packet (five) and from the prompt (six) |

## The triage

| finding | severity | disposition | note |
|---|---|---|---|
| The design never reaches the model | Critical | PROPOSE FIX | `KindDesign` added to the kind order; the test asserts on the rendered prompt |
| The gate can open on a review that had no design | Critical | PROPOSE FIX | the screening refusal is recorded; the gate clears on its own verdict |
| The kind is not persisted in ArcadeDB | Blocker | PROPOSE FIX | written, selected, rebuilt, and asserted by the conformance suite |
| The fail-closed kind is not implemented | Blocker | PROPOSE FIX | `validatePlan` refuses an unknown kind; the DDL is anchored |
| `validatePlan`'s stated rule rejects the document's own plan | Blocker | PROPOSE FIX | D1.1 rewritten to the rule the code enforces |
| The review packet does carry the contract part | Blocker | PROPOSE FIX | the packet and the prompt are now distinguished |
| No round against revision 2, and the process summary was wrong | Blocker | PROPOSE FIX | this round; the summary's numbers corrected |
| The isolation claim is stated at the packet level | Major | PROPOSE FIX | restated at the prompt level |
| An absent kind is not an implement task | Major | PROPOSE FIX | one rule everywhere: absent → implement, unknown → refused |
| The regexp is not anchored and the test is one-directional | Major | PROPOSE FIX | the DDL is anchored and the test compares sets in both directions |
| The dispatch is not refused for an unknown kind | Major | PROPOSE FIX | the refusal is in `validatePlan`, before the first write |
| "A review always opens a new session" contradicts | Major | PROPOSE FIX | stated once, precisely |
| The scope rule in the document is not the code's | Major | PROPOSE FIX | both now state the same rule, and the code was hardened to match |
| The note's bound is never stated | Major | PROPOSE FIX | 300 runes, in the document and in the contract |
| The phase tables and the test count cannot both be true | Major | PROPOSE FIX | the test file is named in every phase that adds a test |
| Two register files carried stale counts | Major | PROPOSE FIX | the per-package split and the QA rule's derived paragraph fixed |
| The phase-4 exit condition did not hold | Major | PROPOSE FIX | the canaries are the round's own artefact; the exit condition is now stated as a scoped command |
| "No new blocking reason" contradicts D6 | Major | PROPOSE FIX | what did not change is now stated exactly |
| The review packet is not "exactly the design and the rules" | Major | PROPOSE FIX | the packet's three parts and the prompt's two |
| The block reader was copied instead of generalised | Major | PROPOSE FIX | `handoffBlocks` now takes the tag; the copy is gone |
| The gate's state machine was dead code | Major | PROPOSE FIX | `gateStateLocked` is now the single reading and drives the writes |
| The screening refusal was an unspecified exit | Major | PROPOSE FIX | two named rows in D6, and two new tests |
| The "31 files" metric did not reconcile | Major | PROPOSE FIX | the three wider files are named and attributed |
| Three live mirrors were stale | Major | PROPOSE FIX | the ToC split, the QA rule's paragraph and map, and the BRD's §9.7 |
| The vocabulary test was weaker than the house pattern | Major | PROPOSE FIX | set equality in both directions; the eight legacy artifact kinds are declared |
| The scope was validated and the note was not | Major | PROPOSE FIX | the note is bounded, refuses control characters and refuses a reserved fence |
| The release was not conditioned on the review reaching DONE | Major | PROPOSE FIX | the release reads the gate's own state |
| The D6 table had no row for three reachable states | Major | PROPOSE FIX | the table is complete, with a reason for every state |
| The change list's counts did not follow from the phases | Major | PROPOSE FIX | the repeats are named and the phase counts say what they count |
| The scoreboard claim was false and the total was wrong | Major | PROPOSE FIX | the row's real values, and the total corrected to 75 |
| The verification plan claimed a test that did not exist | Major | PROPOSE FIX | the route-level test now exists (TC-376) and the plan says what each case proves |
| No success metric | Major | PROPOSE FIX | §"What would show this works" added, with the three numbers the milestone owes |
| The findings travel back re-rendered, not verbatim | Major | PROPOSE FIX | D7 says re-rendered |
| The quoted DDL did not exist | Major | PROPOSE FIX | the DDL is now the anchored form the document quotes |
| The reviewer's session contradicts itself | Low | PROPOSE FIX | stated once |
| The implementation role is hardcoded | Low | PROPOSE FIX | the diagram says `backend` |
| The citations drifted | Low | PROPOSE FIX | the load-bearing ones re-anchored; the rest are marked as describing the pre-change tree |
| "No new blocking reason" in the Non-goals | Low | PROPOSE FIX | struck |
| `#ni-egm` is a dead link | Low | PROPOSE FIX | the header points at the live anchor |
| An escalated finding had no home | Low | PROPOSE FIX | the re-review round is in §"Open questions" |
| Two smaller plan mismatches | Low | PROPOSE FIX | the diagram and the rule vocabulary |
| The plan arithmetic misses the objective-only branch | Nit | PROPOSE FIX | the no-story case is stated |
| The scoreboard string did not match the columns | Nit | PROPOSE FIX | the row in the page's own order |
| The phase table double-assigned two files | Nit | PROPOSE FIX | the repeats are marked |
| The exact-string checklist duplicates the registers | Nit | **ESCALATE** | out of M17's scope: the milestone's task rows and the QA rule already own the strings, and a third hand-maintained copy is what let three mirrors go stale at once. The change moves the numbers into one generated check |
| The blocking reason is not in the event stream | Nit | **ESCALATE** | a replayed board shows a gated card with no reason, because `BlockReason` is a live-record annotation and `TaskMoved` does not carry it. Fixing it means a new field on the event payload and a fold change, which is a different piece of work from M17 |
| The design body can forge a packet section | Low | PROPOSE FIX | the contract-fence half holds and the document now says so; the section-forging half is a limit, because the `===== path =====` format is text |

## The isolation probe

One marker was planted in each standing file before the round: `.claude/CLAUDE.md`, `AGENTS.md`,
`GEMINI.md` and the three files in `.claude/rules/`. The four reviews returned no marker, and
`grep -rn "EGM-MARKER-" docs/designs/reviews/` returns nothing.

The **positive control** was run with the exclusion deliberately disabled: a control read the standing
files and returned three of the six markers verbatim. The probe can therefore detect a leak, which is the
only thing that makes the clean result mean anything. Every planted marker was removed afterwards, and
`deploy/check-workspace.sh` confirms none is left.

## What the round cost

Revision 2's gate could open on nothing, and its reviewer could not see the document it was reviewing.
Both are the kind of defect a gate exists to catch, and both were caught — which is the argument for
running the round against the built thing rather than against the plan.
