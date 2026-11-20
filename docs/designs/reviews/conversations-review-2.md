# Review record 2: conversations (design v2 → v3)

Round 2 ran all five lenses on v2 (`conversations-{comprehension,zoomout,critic,readiness}-2.md`, `conversations-security-review-2.md`), each given the design path and the rubric path list only.

| Lens | Counts |
|---|---|
| comprehension | B=0 M=4 N=6 |
| zoom-out | B=0 M=1 N=6 |
| critic | B=1 M=7 N=4 |
| readiness | B=1 M=3 N=8 |
| security | Critical=0 High=0 Medium=3 Low=5 |

## Isolation

Canaries re-planted in the seven standing files for this round and removed after it (`git checkout`); no canary in any report; `grep -rn "EGM-MARKER-[A-Za-z]"` clean. Positive control: an unrestricted agent quoted the canary planted in `.claude/rules/agent-workspace.md`. Every lens again reported the harness attaching `.claude/CLAUDE.md` (and stale main-checkout rule copies) unasked; none used them. Isolation for that file remains instruction-only (escalated, as in round 1).

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| Prune deletes almost nothing unique but says it deletes the conversation's text | critic | Blocker | PROPOSE FIX | v3 §3.7 "What prune leaves readable", the exact form text (§3.6), `Kept` list (§3.5), D-082 and BR-41 wording say what is emptied (documents) and what survives and where. Scope chosen = the product owner's relayed brief (§11): artifact bodies and session transcripts deleted; events and metadata kept. Clearing event text or slices rejected (§6) |
| Prune certifies prompt/question as deleted while their content stays readable | zoom-out | Major | PROPOSE FIX | same; option (b) adopted |
| Prune says it deletes the conversation, most of it survives | comprehension | Major | PROPOSE FIX | same; L1 "full answer" |
| SEC-1 prune promises a deletion it does not perform; gateway copies | security | Medium | PROPOSE FIX | same; gateway question records and turn results named in §0 and `Kept` |
| Prune store split and digests for unloaded bodies undecided | readiness | Blocker | PROPOSE FIX | §3.7 steps 1–3 with `pruneSelectLocked`, the bound-list read, `pruneCommitLocked` and its `commit` callback; `CardRef` defined; digest from the read; TC-488 asserts it |
| ArcadeStore prune: no digest source; unbounded transaction under the lock | critic | Major | PROPOSE FIX | same; 30 s budget; sheet reads do not take ArcadeStore's lock |
| Commit atomicity holds in the database, not the projection | critic | Major | PROPOSE FIX | §3.7 step 3: events built with sequence numbers but appended, and bodies emptied, only after the database commit returns nil; TC-484 injects a failing commit |
| Prune commit step: how ArcadeStore gets old bodies | comprehension | Major | PROPOSE FIX | step 2 |
| Sessions deleted with no event; 404 counted as deleted | critic | Major | PROPOSE FIX | certificate first (step 3), sessions after (step 4), outcome event (step 5); `SessionsGone` separate; unsupported vendor and non-UUID ids not sent |
| SEC-3 sessions deleted before the re-check | security | Medium | PROPOSE FIX | same — "commit the tombstone first" adopted |
| "Every session a terminal card names is finished" not enforced | critic | Major | PROPOSE FIX | ids also named by a run outside the selection are kept (`SessionsShared`); TC-484 |
| SEC-7 terminal-never-runs enforced at the board only | security | Low | PROPOSE FIX | archive also refuses a card with a `STARTED`/`RUNNING` run; TC-482 |
| Markdown: fences inside list items | critic, readiness | Major | PROPOSE FIX | §3.3 rows for a fence at an item's text column, and mixed list types; TC-478's example includes a fence in an item |
| OQ-17 answered, not open; D-043; backups | critic, comprehension | Major | PROPOSE FIX | §3.7 "Storage" cites D-043 and `RECONCILIATION.md:241`; no claim on page reclamation; L-9 backups in `Kept`. No new OQ row: D-082 records the gap (DECLINE reopening a register row) |
| SEC-8 OQ-17 status disagrees with the register | security | Low | PROPOSE FIX | same |
| Approval by delegation unanchored | critic, comprehension | Major | PROPOSE FIX (label) | §11 quotes the relayed brief and labels it as a conversation message, not a repository file; the brief names prune's scope, the D-number and the delegation explicitly. Routing approval to the operator in person: DECLINE — the product owner's standing instruction is that agents approve among themselves |
| What each mutation answers is undecided | readiness | Major | PROPOSE FIX | §3.5 toasts, §3.7 toasts, §3.8 response per route |
| `Kept` and form text have no value | readiness | Major | PROPOSE FIX | §3.5 exact slice; §3.6 exact paragraph |
| SEC-2 no CSP / anti-framing | security | Medium | PROPOSE FIX (partial), ESCALATE (`script-src`) | §3.8 `frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'` on BO responses (`base-uri 'self'` because `page.html:25` sets `<base>`); TC-486 asserts it; `script-src` stays R5 |
| SEC-4 session id not validated | security | Low | PROPOSE FIX | UUID check before sending; sentinels, never wrapped |
| SEC-5 actor must be bound | security | Low | PROPOSE FIX | `actor = :actor`; TC-488 |
| SEC-6 link host display | security | Low | PROPOSE FIX | ASCII hosts only (no punycode in stdlib: a non-ASCII host stays text), bidi/zero-width stripped, `[` in text literal; TC-479 |
| Timing sheet as the conversation's home | zoom-out | Nit | PROPOSE FIX | §6, rejected with reason |
| Sessions vs 30-day expiry not weighed | zoom-out | Nit | PROPOSE FIX | §6 |
| Reuse `ErrGatewayTransient`; `admin()` leaks the path | zoom-out, readiness, critic | Nit | PROPOSE FIX | §3.7 sentinels |
| `Result` field omitted | zoom-out, critic | Nit | PROPOSE FIX | not needed in v3: the database write happens in the `commit` callback, so no `Result` field is added |
| One query per missing body | zoom-out | Nit | PROPOSE FIX | one bound-list `IN` query (measured to work) |
| Anchors drifted | zoom-out, readiness | Nit | PROPOSE FIX | corrected |
| 404 vs 422 in the refusal list | comprehension | Nit | PROPOSE FIX | §3.5 |
| Undefined names (`Talk`, `CardRef`, gate vs requireBO, sessionOf) | comprehension | Nit | PROPOSE FIX | defined / corrected |
| Pruned mode unreachable | comprehension | Nit | PROPOSE FIX | certificates table has **Open** |
| Skipped-cards toast unspecified | comprehension | Nit | PROPOSE FIX | §3.7 |
| Section numbering | comprehension, critic | Nit | PROPOSE FIX | §7/§8 removed; numbering kept for cross-references (§9–§11) |
| Change list leaves edits implied | critic, readiness | Nit | PROPOSE FIX | §3.10 lists every moving string |
| Glossary vs `onVendor` comment | critic | Nit | PROPOSE FIX | §0 says the fan-out comment is about workspace routes |
| Board JSON archived ambiguity | readiness | Nit | PROPOSE FIX | §3.4: field always set with a run; chip only when active |
| Certificate counting edge cases | readiness | Nit | PROPOSE FIX | one line per document; empty ids excluded |
| `drive` card unspecified | readiness | Nit | PROPOSE FIX | the cancelled board card, given one run |
| No checkpoints in one-commit phasing | readiness | Nit | PROPOSE FIX | §3.9 |
| Doc-only changes unverified | readiness | Nit | PROPOSE FIX | §3.9 greps |

No Blocker survives v3; no Critical or High security finding is open. No third round (policy step 7). Approved by delegation (design §11).
