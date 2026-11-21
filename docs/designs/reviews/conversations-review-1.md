# Review record 1: conversations (design v1 → v2)

Design: `docs/designs/conversations.md`. Round 1 ran five goldfish lenses on v1 in parallel, each given the design path and the rubric path list only: `conversations-comprehension-1.md`, `conversations-zoomout-1.md`, `conversations-critic-1.md`, `conversations-readiness-1.md`, `conversations-security-review-1.md`. The security lens ran because the change adds destructive back-office routes, an outbound admin call and renders model text as HTML.

| Lens | Counts |
|---|---|
| comprehension | B=2 M=4 N=7 |
| zoom-out | B=1 M=5 N=4 |
| critic | B=1 M=10 N=5 |
| readiness | B=2 M=10 N=5 |
| security | Critical=0 High=1 Medium=4 Low=5 |

## Isolation

- Canaries planted in the seven standing files before the run and removed after it (`git checkout`); `grep -rn "EGM-MARKER-[A-Za-z]"` finds nothing outside the skill's own instructions. No canary appears in any report.
- Positive control: an agent with no exclusion read `.claude/rules/frontend.md` and quoted its planted canary. The probe can detect a leak.
- Limit, stated: every lens reported that the harness attached `.claude/CLAUDE.md` (and some rule files from the main checkout) unasked; none opened or used it. As in `card-timing-review-1.md` and `runner-manual-review-1.md`, isolation for that file is instruction-only.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| Prune contradicts BR-41 and D-012 while claiming to obey BR-41 | zoom-out | Blocker | PROPOSE FIX | v2 header and §3.11: BR-41 amended in place, D-012 and J-6 cross-referenced, L-7/L-8/OQ-17 cited; the product owner's explicit request recorded as the operator's ruling |
| Prune deletes records BR-41 and J-6 keep forever | comprehension | Blocker | PROPOSE FIX | same |
| Prune deletes what BR-41 says must stay readable | critic | Blocker | PROPOSE FIX | same; certificate lists what survived (L-8) |
| Prune contradicts BR-41 | readiness | Blocker | PROPOSE FIX | same; `brd.html` in the change list; rule count unchanged |
| SEC-1 Prune destroys the per-attempt record; keep digests | security | High | PROPOSE FIX | same, plus SHA-256 and byte length per deleted body in `PruneRecord`; the context slice (egress record, BR-32) is kept |
| The pruned state hides new attempts; no way to re-archive | comprehension | Blocker | PROPOSE FIX | §3.1: archive only terminal cards, so a pruned card can never gain attempts; pruned is final |
| The prune ordering contradicts itself and does not fit `mutate` | readiness | Blocker | PROPOSE FIX | §3.7: one numbered list (select → sessions → one `persist` transaction for bodies and certificates), locks named, failure per step stated, `pruning` mark removed |
| Prune order paragraph self-revising; failure claim wrong | comprehension | Major | PROPOSE FIX | same |
| Prune order can delete bodies without the tombstone | critic | Major | PROPOSE FIX | same |
| Prune ordering rewrites itself; half-done state | zoom-out | Major | PROPOSE FIX | same — the zoom-out's two-step order adopted |
| SEC-5 Prune order can delete data without a tombstone | security | Medium | PROPOSE FIX | same; sessions before the transaction (transport, expire anyway) — SEC-5's "tombstone before sessions" not adopted because it needs two events per card; a failure after sessions leaves the card prunable again |
| Archive allowed on tasks that will run again | comprehension | Major | PROPOSE FIX | terminal only |
| Archive any card costs a layer of states | zoom-out | Major | PROPOSE FIX | terminal only |
| What happens when an archived card runs again is undecided | readiness | Major | PROPOSE FIX | cannot happen: terminal only |
| Prune nulls by task id; attempts after archive | critic | Major | PROPOSE FIX | by artifact id, collected in step 1; terminal only |
| A pruned card that runs again loses its chip | zoom-out | Major | PROPOSE FIX | cannot happen: terminal only |
| Card states after a prune contradict L2 and G1 | critic | Major | PROPOSE FIX | same; L2 removed |
| "Pruned" semantics make new attempts invisible | readiness | Major | PROPOSE FIX | same |
| Archive page "Open" goes to an empty sheet | zoom-out | Major | PROPOSE FIX | §3.4 archived mode renders the full conversation read-only with Restore |
| Open link contradicts the sheet | comprehension | Major | PROPOSE FIX | same |
| Operator cannot read what they destroy; confirm not bound to the set | critic | Major | PROPOSE FIX | archived mode; the form carries the listed cards |
| "Open in read mode" undefined | readiness | Major | PROPOSE FIX | same |
| SEC-9 archived readability contradictory | security | Low | PROPOSE FIX | same; archive changes visibility, not access |
| SEC-2 prune deletes what is archived at POST time, not what was seen | security | Medium | PROPOSE FIX | bound card set; skipped cards counted. Password re-entry DECLINED: single trust domain, certificate names the account (L5) |
| Session deletion assumed rather than decided | zoom-out | Major | CONTEST | the product owner asked for prune to permanently delete, and the coordinator named the session transcript as deletable; the antigravity fan-out question is answered from the gateway source: one shared store, no pool fan-out (§0) |
| Gateway behaviour rests on a document outside the repo | comprehension | Major | PROPOSE FIX | §0 labels it an external contract with the gateway commit; TC-487 pins the request |
| SEC-10 gateway DELETE behaviour unverifiable | security | Low | PROPOSE FIX | same |
| Over-broad admin key; external assumptions | critic | Major | PROPOSE FIX | OQ-29 cited as R4; same |
| The prompt and answer survive in places §3.7 does not list | critic | Major | PROPOSE FIX | §3.7 "Survives" names event Text and the context slice; L1, L2 |
| Deleted-body states three ways; L4 contradicts OQ-17 | critic | Major | PROPOSE FIX | one representation `""`; L4 and D-082 state the reclamation gap against OQ-17 |
| "Permanently deletes" depends on unverified storage | readiness | Major | PROPOSE FIX | same; UI wording "deletes the stored text" |
| Per-card artifact query has no index | critic | Major | PROPOSE FIX | by id (`(tenantId, id) UNIQUE`) |
| Markdown linear-time claim unbacked | critic | Major | PROPOSE FIX | §3.3 one miss per delimiter per line; TC-479 500 KiB pathological input with a time bound |
| SEC-4 hand-rolled parser, no invariant, no CSP | security | Medium | PROPOSE FIX (invariant), ESCALATE (CSP) | TC-489 checks the allowlist/balance invariant over generated inputs; CSP needs an audit of every template (R5) |
| Safe-markdown forward spec has no expected output | readiness | Major | PROPOSE FIX | §3.3 fixed example and its exact output; every open rule stated |
| JS opener would request `undefined`; close mechanism unspecified | readiness | Major | PROPOSE FIX | §3.4 JS edits by line; close by an out-of-band hidden modal |
| P2 exit fails: unemitted-names test | readiness | Major | PROPOSE FIX | `drive` emits both names |
| `ArchivedConversations` has no enumeration strategy | readiness | Major | PROPOSE FIX | `convIdx` on the same high-water mark |
| Phases break same-commit rules | readiness | Major | PROPOSE FIX | §3.9 one commit; arcade URL required |
| SEC-3 gateway error carries the session id | security | Medium | PROPOSE FIX | sentinel errors without path; TC-484/487 assert |
| SEC-6 no `no-store` | security | Low | PROPOSE FIX | sheet and Archive view |
| SEC-7 model links open any http(s) host | security | Low | PROPOSE FIX | https only, host shown |
| SEC-8 CSRF relies on SameSite=Lax | security | Low | PROPOSE FIX | named in §3.8/R2; TC-486 asserts both headers |
| New opener attribute and helper not needed | zoom-out | Nit | PROPOSE FIX | `conversationURL` beside `timingURL`; one branch |
| Timing index name and comment wrong | zoom-out | Nit | PROPOSE FIX | renamed `taskEventsLocked` |
| `Rule` overloaded | zoom-out | Nit | PROPOSE FIX | `To` = `archived` \| `active` |
| Wrong citation for `renderBOAfter` | zoom-out | Nit | PROPOSE FIX | `handlers_bo.go:494` |
| "Turn" means two things | comprehension | Nit | PROPOSE FIX | "attempt" for records, "turn" for the live call; `Talk` only as the view field |
| Terms not defined | comprehension | Nit | PROPOSE FIX | projection and sisflow in §0 |
| Line anchors drifted | comprehension, critic, readiness | Nit | PROPOSE FIX | corrected |
| JS selector left out | comprehension | Nit | PROPOSE FIX | §3.4 |
| `html.EscapeString` vs the rule's function | comprehension, critic | Nit | PROPOSE FIX | `template.HTMLEscapeString` |
| ToC decision count missing | comprehension | Nit | PROPOSE FIX | change #16 |
| L5 vague | comprehension | Nit | PROPOSE FIX | §3.8: `""`, Text says "by operator" |
| "No session id in an event" rationale false | critic | Nit | PROPOSE FIX | reworded: the stream already holds ids; the certificate is about content |
| `Actor` duplicates `Event.actor` | critic | Nit | PROPOSE FIX | `writeEvent` writes the column from the payload; payload kept because the in-memory Event has no actor |
| Line refs wrong (incl. BR-25 meaning) | critic | Nit | PROPOSE FIX | BR-25 dropped from the header |
| `broker.go` missing | critic | Nit | PROPOSE FIX | `sseName` → `task` |
| Markdown rules ambiguous | critic | Nit | PROPOSE FIX | intraword `_`, indentation, tables as text |
| Counts check out | critic | Nit | CONTEST | no change needed; recounted for v2 (12 tests) |
| `Clone` not extended | readiness | Nit | PROPOSE FIX | change #3 |
| Event Kind, Text, actor unspecified | readiness | Nit | PROPOSE FIX | §3.5 table |
| `conversationStateOf` signature and filter | readiness | Nit | PROPOSE FIX | one signature; `DeskID` filter |
| Test-to-file mapping, ArcadeStore split | readiness | Nit | PROPOSE FIX | §3.10 file column; change #5a/#5b |
| Drifted anchors | readiness | Nit | PROPOSE FIX | corrected |
| Harness attached CLAUDE.md to every lens | all | — | ESCALATE | harness behaviour, recorded above |

Blockers were raised, so round 2 runs on v2 (policy step 7).
