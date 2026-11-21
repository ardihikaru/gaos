# design-iterate — round 1 triage

**Design reviewed:** `docs/designs/design-iterate.md` v1. **Result:** v2.
**Lenses:** comprehension (B0 M4 N10), zoom-out (B0 M3 N7), critic (B1 M3 N10), readiness (B1 M6 N7), security (Critical 0 High 1 Medium 3 Low 5). Reports: `design-iterate-{comprehension,zoomout,critic,readiness}-1.md`, `design-iterate-security-review-1.md`.

## Isolation

- Canaries were planted in the seven standing files (`.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/rules/rule-index.md`, `qa-test-plan.md`, `agent-workspace.md`, `frontend.md`) before the run and removed after it; `git status` shows those files unchanged.
- The validator's value pattern finds no canary in any of the five reports.
- Positive control: one agent with no exclusion was told to read `.claude/rules/rule-index.md` and quote its trailing HTML comments; it quoted the planted rule-index canary. The probe can detect a leak.
- **Limit, stated.** The readiness and critic reviewers reported that the harness attached `.claude/CLAUDE.md` to their context unasked; neither used it and no canary reached a report. The probe proves "the canary did not reach a report", not "the file was not seen" — the same limit as `card-timing-review-1.md`.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| The iterate sheet has nowhere to get its data | readiness | Blocker | PROPOSE FIX | §3.3 "The read": `SendBackSheet` + `SendBackView`; the eligibility check factored into `sendBackCheckLocked`, shared by the write and the read |
| The board reason and the reroll guard break the fold-equals-board invariant (TC-308) | critic | Blocker | PROPOSE FIX | §3.9: `Reason` and `SendBack` are read-time decoration in `decorateBoardGate`, `legalMove` and `boardOf` untouched, the refusal lives in `MoveCard`; `foldedBoard` clears the decoration; TC-308 prose amended |
| Same (fold invariant) | comprehension, readiness | Major | PROPOSE FIX | as above |
| SEC-1 Triage and earlier findings reach the re-reviewer through the revised design | security | High | PROPOSE FIX | §3.5: an instruction in `renderTriage`; a verbatim check (notes ≥ 24 runes) refusing the revision with `design-quotes-review`; the paraphrase limit stated in N3, R2 and BR-57; TC-499 asserts the refusal |
| The re-reviewer is lobbied through the revised design itself | critic | Major | PROPOSE FIX | as SEC-1 |
| The re-review hook rebuilds what the scheduler already does | zoom-out | Major | PROPOSE FIX | §3.3 write step 5: the review goes `BLOCKED → PENDING`; the dependency edge and the scheduler promote it; `design-revising`, `design-revised`, `reReviewLocked` and the `finishRun` hook are gone (A7) |
| Per-finding structured triage is more machinery than the problem needs | zoom-out | Major | DECLINE | the brief asks for a per-finding disposition in the four-word vocabulary; A8 records the choice |
| The revision packet leaves out the design being revised | zoom-out, readiness | Major | PROPOSE FIX | §3.4 adds `.gaos/packet/design.md`; G3 reworded "where possible"; TC-491 |
| The reroll is not closed: drift release unblocks every BLOCKED task | comprehension | Major | PROPOSE FIX | §3.6: `releaseDriftLocked` skips the gate's reasons (`gateReason`); F7; TC-499 |
| `Actor` defined as "every operator move" but set only by iterate; OQs not addressed | comprehension, critic | Major | PROPOSE FIX | §3.2: the field's contract is the send-back only; N7; OQ-16 and OQ-34 narrowed in §3.10 |
| Key specifications made by reference to a document not in this repository | comprehension | Major | PROPOSE FIX | §3.2 states the `Actor` contract in full and what the second merge reconciles; `writeEvent` is no longer changed |
| Identifiers and counts depend on another branch's merge order | readiness | Major | PROPOSE FIX | §13 tabulates both orders; no Go string carries a D-number |
| TC-497's file and the route-classification change are missing | readiness | Major | PROPOSE FIX | `auth_test.go` `readOnlyGET` entry (§3.8, change 15); every TC names its file; the 403 asserted on the POST only |
| No phases, no dependencies, no per-phase exit conditions | readiness | Major | PROPOSE FIX | §12 phases with commands; the client board's check stated |
| Published counts and statements made false are not in the change list | readiness | Major | PROPOSE FIX | §3.10: `design-gate.html:170,212`, `toc/architecture.js:104`, OQ-16, OQ-34 |
| A re-review records the wrong Blueprint drift line | critic | Major | PROPOSE FIX | §3.3a: `blueprintPinsLocked` takes the review pin from the run being judged |
| SEC-2 The fresh-reviewer decision depends on second-resolution clocks and fails open | security | Medium | PROPOSE FIX | §3.5: decided from event `Seq` (send-back after the last dispatch move), fail closed; TC-493 asserts the same-second case |
| SEC-3 The author's session carries the triage; no read boundary to the reviewer's sibling folder | security | Medium | PROPOSE FIX | stated as the inherited BR-47 limit (§3.5, R4) |
| SEC-4 The actor is not guaranteed; machine moves stay labelled operator | security | Medium | PROPOSE FIX (part) / DECLINE (part) | an empty actor is refused and the route answers 401 without a session (§3.3, §3.8); the column's `'operator'` literal for other moves is not changed here (N7) — that is OQ-16's work and the conversations branch's binding |
| SEC-5 The note validation allows other control and invisible characters | security | Low | PROPOSE FIX | §3.3 step 3 refuses Cc, Cf, Zl, Zp; `ParseReview`'s own rule is ESCALATE (a reviewer-note change outside this design) |
| SEC-6 The verdict is read by substring search over model-written notes | security | Low | PROPOSE FIX | `reviewVerdictOf` reads the anchored line 2, used by the new check and by `gateStateLocked` |
| SEC-7 A rollback re-opens the reroll | security | Low | PROPOSE FIX | §10 rollback step |
| SEC-8 The operator's email goes permanently into event text | security | Low | DECLINE (text) / PROPOSE FIX (registers, tests) | the caption is the audit view the BO timeline prints and BR-48 asks for, visible only to signed-in accounts (R3); OQ registers updated; TC-491 extends the no-email assertion to `RenderPrompt` |
| SEC-9 The sheet route does not say it checks the product | security | Low | PROPOSE FIX | `sendBackCheckLocked` step 1 checks `DeskID`; the sheet uses it; TC-497 asserts 404 |
| "iterate" means two things | comprehension | Nit | PROPOSE FIX | the move rule is `send-back`; `VerdictIterate` in the glossary |
| Line references drifted | comprehension, critic, readiness | Nit | PROPOSE FIX | re-anchored |
| `sessionOf(r).Email` does not compile; the 401 claim | comprehension, readiness | Nit | PROPOSE FIX | `ss, ok := sessionOf(r)`; JSON 401 vs HTMX login sheet split in TC-497 |
| The counter `n` used for three counts | comprehension | Nit | PROPOSE FIX | `sent` and `revision` defined in §3.3 |
| "the reviewer's handoff" not guaranteed | zoom-out, comprehension, critic | Nit | PROPOSE FIX | §3.4 says "the product's newest handoff" |
| Re-review triggered by the gate's intention | comprehension | Nit | PROPOSE FIX | moot: the hook is gone; the scheduler reads the design's actual status |
| Undefined "the brief" | comprehension | Nit | PROPOSE FIX | glossary entry |
| The removed test row's reason argues against the design | comprehension | Nit | PROPOSE FIX | §3.1 says it reverses that argument and why |
| Test files and count targets not fully specified | comprehension, readiness | Nit | PROPOSE FIX | §5 names files; §13 gives every count |
| Operator text standard differs from BR-53 | comprehension, critic | Nit | PROPOSE FIX | R2 says why BR-53's stripping does not apply |
| Two guards where one has all the information | zoom-out | Nit | PROPOSE FIX | `moveTaskLocked` is the guard; `moveTask`'s check stated as defensive |
| `lastIterateLocked` and `iterationsLocked` do the same scan | zoom-out | Nit | PROPOSE FIX | one `sendBacksLocked` |
| `BoardIterate` carries derived and constant fields | zoom-out | Nit | PROPOSE FIX | `BoardSendBack{Design, Sent, Open}`, no `Cap` |
| `writeEvent` writes a column never read back | zoom-out | Nit | PROPOSE FIX | `writeEvent` unchanged |
| `BoardCard.Reason` on every blocked card is separate | zoom-out | Nit | PROPOSE FIX | limited to the gate's reasons, which is what the send-back needs and is never stale (§3.9) |
| TC-497's file missing | zoom-out | Nit | PROPOSE FIX | `design_iterate_test.go` |
| `moveTaskWithLocked`'s caption ambiguous | readiness | Nit | PROPOSE FIX | the suffix only when `Actor` is set |
| Conditional doc edits left unresolved | readiness, critic | Nit | PROPOSE FIX | `graph.html` explicitly unchanged; the ToC has no BRD count |
| `check-workspace.sh` lines not named | readiness | Nit | PROPOSE FIX | §3.10 names `:112-116, :127-131` |
| TC-494 asserts something a test cannot | readiness | Nit | PROPOSE FIX | moved to the TC-208 amendment |
| QA scenario prose not planned | readiness | Nit | PROPOSE FIX | `h3 id="go-send-back"` |
| The form limits do not match the data | critic | Nit | PROPOSE FIX | N read up to the first gap (≤1000), validated against the findings; a 256 KiB cap with a stated 400 |
| Stale reasons on the card | critic | Nit | PROPOSE FIX | only the gate's reasons shown |
| Precedent for payload keys mis-cited | critic | Nit | PROPOSE FIX | `Question`, `Genesis` |
| Counting send-backs scans the whole stream | critic | Nit | PROPOSE FIX | scanned only for design-blocked review cards, a send-back and a design packet (§3.7) |
| The frontend rule is not addressed | critic | Nit | PROPOSE FIX | §3.9 worst cases, rendered by TC-497 |
| Hedged `graph.html` item | critic | Nit | PROPOSE FIX | removed |

No severity was changed. Two Blockers and the one High are fixed in v2; a second round runs on v2.
