# Review record 1: runner-manual (design v1 → v2)

Design: `docs/designs/runner-manual.md`. Round 1 ran five goldfish lenses on v1 in parallel, each given the design path and the rubric path list only: `runner-manual-comprehension-1.md`, `runner-manual-zoomout-1.md`, `runner-manual-critic-1.md`, `runner-manual-readiness-1.md`, `runner-manual-security-review-1.md`. The security lens ran because the change is the permission to spend.

| Lens | Counts |
|---|---|
| comprehension | B=0 M=3 N=7 |
| zoom-out | B=0 M=2 N=4 |
| critic | B=1 M=3 N=7 |
| readiness | B=0 M=6 N=7 |
| security | Critical=0 High=1 Medium=4 Low=3 |

## Isolation

- Canaries were planted in the seven standing files (`.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/rules/rule-index.md`, `qa-test-plan.md`, `agent-workspace.md`, `frontend.md`) before the run and removed after it (the files restored with `git checkout`); `grep -rn "EGM-MARKER-[A-Za-z]"` over the tree finds nothing outside the skill's own instructions.
- No canary appears in any of the five reports.
- Positive control: one general-purpose agent with no exclusion was told to read `.claude/rules/rule-index.md` and quote its trailing HTML comments; its report quoted the planted rule-index canary. The probe can detect a leak.
- **Limit, stated.** All five reviewers reported that the harness attached `.claude/CLAUDE.md` to their context unasked (the `omitClaudeMd` isolation did not hold in this harness). None opened it, and none of its canary reached a report — but the probe proves only "the canary did not reach a report", not "the file was not seen". Treat this round's isolation as instruction-only for that file (SEC-7).

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| The manual `RunOnce` can move a live turn back to READY | critic | Blocker | PROPOSE FIX | §3.2 `readyPending`: move only from PENDING, re-read under the write lock; TC-442 with a stale decision |
| The manual move can pull a just-dispatched task back to READY | readiness | Major | PROPOSE FIX | same fix as the Blocker |
| SEC-1 BR-38 lease in `dispatchNow` can be raced | security | High | PROPOSE FIX | §3.3 step 6: re-check and move under one lock, honour `moveTaskLocked`'s boolean; TC-445 |
| Board dispatch skips readiness, capability, budget | critic | Major | PROPOSE FIX (readiness), ESCALATE (budget), DECLINE (capability) | §3.3 step 3 re-checks BR-07; budget is a stub (`BudgetNormal`), D-080 says the guard must gate the board dispatch; capability is a desk-projection seat, not a resource a turn consumes (L3) |
| Readying and dispatching drift apart; only some gates at dispatch | comprehension | Major | PROPOSE FIX | same as above; G3 and BR-56 now list exactly the gates applied |
| L2 contradicts BR-40 and the adapter | critic | Major | PROPOSE FIX | L2 rewritten: expiry → rotation; dropped session → `session-lost` FAILED, retry opens a new session; cost accepted |
| A parked READY card cannot be demoted and blocks other products | critic | Major | PROPOSE FIX (blocking), DECLINE (demotion) | A3 adopted: a parked card claims nothing (§3.2). Demotion needs a new move and a hold flag (N5, L1); cancel remains |
| Compose pass-through makes `on` a shell variable | zoom-out | Major | PROPOSE FIX | R4 and D-080 consequences state it; deploy log prints the mode |
| SEC-2 ambient shell state; env_file and `.env` | security | Medium | PROPOSE FIX (document, log line), DECLINE (refuse `on`, A6) | §3.7 names the deploy line as the one mechanism and the env_file override; §9 path-only grep premise |
| Compose `environment:` overrides the env file | readiness | Major | PROPOSE FIX | §3.7 and §9: override is fail-closed, stated; redeploy-line toil stated |
| Version literals README:8, check-workspace:144 | zoom-out, comprehension | Major | PROPOSE FIX | §4 rows 18, 19 |
| TC-446 bundles two checks; "Positive + Negative" | comprehension, critic, readiness | Major / Nit | PROPOSE FIX | one Kind per row; check-stacks split into TC-448/TC-449; arithmetic redone |
| Change #7 file undecided / does not exist | readiness, critic | Major / Nit | PROPOSE FIX | the Arcade-only restart test is dropped (zoom-out: it re-tests an existing event path); the conformance case runs on both stores |
| TC-124/125 count undecided and stale | readiness, critic | Major / Nit | PROPOSE FIX | "forty-five" (44 + 1), correcting "forty-two" |
| No phases or per-change verification | readiness | Major | PROPOSE FIX | §4 split into P1/P2/P3 with exit commands and greps |
| D-080 and doc rows named, not written | readiness | Major | PROPOSE FIX | §4.1 writes D-080; rows 14-16 write the env rows and the changelog summary; `h3 id="go-runner-manual"` |
| SEC-3 no actor on an operator spend | security | Medium | ESCALATE | events carry no actor (D-079 follow-up, OQ-11); L4; BR-56 says "signed-in operator" |
| SEC-4 the desk route is a weaker spend path | security | Medium | ESCALATE | it changes `on`'s behaviour of an unused route; recorded in D-080 consequences |
| SEC-5 `/healthz` discloses the mode | security | Low | DECLINE | three-valued, next to `"adapter"` already public; stated in §3.6 |
| SEC-6 rollback to an older tag | security | Low | PROPOSE FIX | §9: v0.39.x only, never before D-079 |
| SEC-7 reviewer isolation not enforced by tooling | security | Medium | ESCALATE | harness/workflow, not this change; recorded in the isolation section above |
| SEC-8 / the `hasnt` check is narrow | security, critic, readiness | Low / Nit | PROPOSE FIX | exactly one `GAOS_RUNNER:` line plus the pinned pass-through |
| A3 rejected for a cost it doesn't have | zoom-out | Nit | PROPOSE FIX | adopted |
| Three tests check the same branch | zoom-out | Nit | PROPOSE FIX (drop restart test), DECLINE (merge TC-441) | TC-441 keeps the MemStore event-level assertions the conformance case cannot make through `Store` |
| G6 "byte-for-byte" | zoom-out, readiness | Nit | PROPOSE FIX | G6 reworded with the list of visible differences |
| Three constants and three accessors | zoom-out | Nit | PROPOSE FIX | `runnerDispatches` dropped |
| Decision count depends on D-078 order | zoom-out, comprehension, readiness | Nit | PROPOSE FIX | header and R3 |
| L1 wrong about which card holds the product | comprehension | Nit | PROPOSE FIX | superseded by A3; L1 rewritten |
| Answer exemption expires (L2) | comprehension | Nit | PROPOSE FIX | L2 |
| Terms undefined | comprehension | Nit | PROPOSE FIX | glossary |
| `/healthz` vs `/api/healthz` | comprehension | Nit | PROPOSE FIX | §3.6 |
| Refusal text without adapter | comprehension, critic | Nit | PROPOSE FIX | §3.3 step 1 own text; §3.6 describes the key |
| Live-instance facts unanchored | comprehension | Nit | PROPOSE FIX | cites `sample-product-qa-sync.md`, labels the ids as an assumption |
| Conformance case needs type assertions | critic | Nit | PROPOSE FIX | §5 |
| `brd.html:324`; D-079 line | critic | Nit | PROPOSE FIX | §4 row 13; header |
| TC-441 "≥1" | readiness | Nit | PROPOSE FIX | exactly one on the first pass |
| BR-56 position | readiness | Nit | PROPOSE FIX | appended after BR-45 in §9.6 |

No Blocker and no Critical/High security finding remains open in v2, so no second round was run (policy step 7). Approved by the orchestrating agent under the product owner's standing instruction of 2026-11-20 to proceed without confirmation.
