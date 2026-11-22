# Review round 1 — brain-town-emitter (comprehension, zoom-out, critic, readiness)

- Canaries were planted in the seven standing files and removed afterwards; `grep -rn "EGM-MARKER-"` on the
  tree returns nothing outside the design-doc prose that discusses the probe.
- Positive control: a general-purpose session without the exclusion read `.claude/CLAUDE.md` and `AGENTS.md`
  and reported both canaries, so the probe can detect a leak.
- Isolation note: all four lenses reported that the harness placed `.claude/CLAUDE.md` in their context
  unasked; none opened it or used it as a source. Recorded as an ESCALATE below.
- Reports are condensed to finding, severity and the line the design cited; the reviewers' full text lived in
  their sessions. The design's private-instance domain is not repeated here (D-085).

## Findings

### Zoom-out (B=1 M=7 N=4)
| # | Finding | Severity |
|---|---|---|
| Z1 | Design names one operator's private domain in a tracked file (D-085) | Blocker |
| Z2 | The 1 s settle + "still in the store" check never filters anything | Major |
| Z3 | Post-commit alternative overstated: one enqueue after `commitStage()` in `persist` suffices | Major |
| Z4 | `review-cleared` maps to several `gate.decided` per decision | Major |
| Z5 | BRD egress inventory (§8.12.1, L-2) not in the change list | Major |
| Z6 | "Send nothing / redact hidden products" option never considered | Major |
| Z7 | Event id length does not add up (6+12+2 ≠ 26) | Major |
| Z8 | `BRAIN_TOWN_TOKEN` env fallback adds a second secret source | Major |
| Z9–Z12 | `task.qa` paragraph contradicts itself; duplicate product-upsert rows; 36 s+3×3 s arithmetic (48 s); anchor drift and unnamed count | Nit ×4 |

### Comprehension (B=0 M=10 N=9)
| # | Finding | Severity |
|---|---|---|
| C1 | Requirements come from a brief outside the repository (design must stand alone) | Major |
| C2 | Gate mapping misreads `review-cleared` and `review-failed` | Major |
| C3 | Lifecycle reversals (send-back, retry, redo) not covered | Major |
| C4 | "Still in Events()" cannot detect an uncommitted event | Major |
| C5 | BRD egress inventory contradiction; no-egress (L-6) interaction unstated | Major |
| C6 | Design names a personal deployment domain (D-085) | Major |
| C7 | `task.qa` paragraph contradicts itself | Major |
| C8 | `product.key` from a mutable, non-unique name | Major |
| C9 | Two queues with undefined overflow and ordering | Major |
| C10 | Enrichment reads state at send time, not event time | Major |
| C11–C19 | `task.failed` reason is "FROM→TO"; id arithmetic; 48 s; "one function" claim (also `appendPreparedLocked`); anchor drift; anchor cites forbidden file; token-reader ambiguity; undefined terms; change-list loose ends | Nit ×9 |

### Critic (B=1 M=12 N=6)
| # | Finding | Severity |
|---|---|---|
| K1 | Tracked design names a private instance's domain | Blocker |
| K2 | Settle check cannot detect uncommitted event; O(N) per event | Major |
| K3 | After restart the town never learns of existing tasks; boot events lost | Major |
| K4 | `review-cleared` also moves released implementation tasks (duplicate `gate.decided`) | Major |
| K5 | `review-failed` is not a verdict (D-068 cl. 7) | Major |
| K6 | Enrichment reads send-time state | Major |
| K7 | `product.key` collides and changes on rename | Major |
| K8 | New egress of private text to a public third party: no secret screen, no BR | Major |
| K9 | `agent` may leak the signed-in operator's account | Major |
| K10 | Token handling: GAOS credentials could go out; env fallback breaks file-only convention | Major |
| K11 | Outage: head-of-line blocking and drop-oldest break the lifecycle | Major |
| K12 | Send-back un-finishes a task the town was told is done | Major |
| K13 | `task.failed.reason` is the "FROM→TO" string | Major |
| K14–K19 | arithmetic; id length; header §; anchor drift; naming/change-list gaps; `task.qa` paragraph | Nit ×6 |

### Readiness (B=4 M=8 N=8)
| # | Finding | Severity |
|---|---|---|
| R1 | No phases, exit conditions or dependency order | Blocker |
| R2 | `check-workspace.sh` pins counts the plan changes and is not in the change list | Blocker |
| R3 | Event id formula does not produce 26 characters | Blocker |
| R4 | `gate.decided` sent more than once and labels non-verdicts as rejections | Blocker |
| R5 | No verification line per change; tests unnamed; TC-519 has no measurable bound | Major |
| R6 | Count targets not stated (364→, ranges, decision-log string) | Major |
| R7 | Settle check can never fail | Major |
| R8 | `gate.requested` needs a lookup the interface does not provide | Major |
| R9 | Queue overflow policy undecided | Major |
| R10 | `task.failed` reason and `agent` underspecified | Major |
| R11 | Deploy: secret mount, env pass-through, check-stacks case undecided | Major |
| R12 | Receiver domain hard-coded in a tracked file | Major |
| R13–R20 | product.upserted duplicates; token reader unnamed; `validOutURL` deny list; Integrations row data shape; retry arithmetic; abbreviated paths and missing `overview.html`; status line §; D-086 undrafted; design task can send `task.done` twice | Nit ×8 (R13 Nit, others Nit) |

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| Z1, C6, K1, R12 | Blocker/Major | PROPOSE FIX | placeholder `town.example.org`; real URL only in the deploy shell (§1) |
| Z2, Z3, C4, K2, R7 | Major | PROPOSE FIX | broker/settle dropped; post-commit tap in `persist` and in `MemStore.emit` (§3.1) |
| Z4, C2, K4, K5, R4 | Major/Blocker | PROPOSE FIX | gate events only for `Kind == review`; `review-failed` → `task.moved` only (§3.2) |
| Z5, C5, K8 | Major | PROPOSE FIX | BRD §8.12.1 row added (change 12); redaction and secret detectors (§3.3). L-6 no-egress products: **DECLINE** a per-product flag — the feed is off unless the operator lists a product as public, and hidden products are redacted |
| Z6 | Major | PROPOSE FIX | hidden products redacted at source (§1 deviation, §3.3) |
| Z7, R3, K15 | Major/Blocker | PROPOSE FIX | ULID-shaped id fully specified with a worked example (§3.3) |
| Z8, K10, R11 | Major | PROPOSE FIX | env token removed; file-only; `gaos_` and equal-token refusal; mount + assertions (§3.5) |
| C1 | Major | PROPOSE FIX | receiver contract copied into §1; `column` assumption labelled |
| C3, K12 | Major | PROPOSE FIX | send-back yields `task.moved`; second `task.done` stated intended (§3.2) |
| C7, K19, Z9 | Major/Nit | PROPOSE FIX | one decision sentence; `task.qa` declared unmet (R3) |
| C8, K7 | Major | PROPOSE FIX | key = product id (§3.3) |
| C9, R9, K11 | Major | PROPOSE FIX | one channel, drop + un-announce, degraded mode (§3.4) |
| C10, K6 | Major | PROPOSE FIX | column from the event; lookup only for role/kind/title (§3.3) |
| K3 | Major | PROPOSE FIX | tap before reconcile; synthetic `task.created` on first sight (§3.1, §3.2) |
| K9 | Major | PROPOSE FIX | closed field list; `Actor`/`Text` never read; `@` assertion (§3.2, TC-515) |
| K13, R10, C11 | Major/Nit | PROPOSE FIX | fixed reason vocabulary; `agent` = role (§3.2) |
| R1, R5, R6, R2 | Blocker/Major | PROPOSE FIX | phases, test table, count arithmetic, `check-workspace.sh` in change list (§4) |
| R8 | Major | CONTEST | no `ReviewOf`: `gate.requested` now fires on the review task's own move, so `TaskInfo(id)` suffices |
| Nits (anchors, arithmetic, header, terms, paths, D-086 draft) | Nit | PROPOSE FIX | all applied; D-086 drafted in §4 |
| Harness loaded `.claude/CLAUDE.md` into reviewer context | — | ESCALATE | harness behaviour, outside this change |
