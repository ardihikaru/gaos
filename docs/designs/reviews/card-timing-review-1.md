# Review record 1: card-timing (design v1 → v2)

Design: `docs/designs/card-timing.md`. Round 1 ran five goldfish lenses on v1 in parallel, each given the design path and the rubric path list only: `card-timing-comprehension-1.md`, `card-timing-zoomout-1.md`, `card-timing-critic-1.md`, `card-timing-readiness-1.md`, `card-timing-security-review-1.md`. The security lens ran because the change adds two back-office routes.

| Lens | Counts |
|---|---|
| comprehension | B=0 M=5 N=7 |
| zoom-out | B=0 M=3 N=5 |
| critic | B=0 M=5 N=8 |
| readiness | B=0 M=5 N=4 |
| security | Critical=0 High=1 Medium=3 Low=3 |

## Isolation

- Canaries were planted in the seven standing files (`.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/rules/rule-index.md`, `qa-test-plan.md`, `agent-workspace.md`, `frontend.md`) before the run and removed after it (the files restored with `git checkout`); `grep -rn "EGM-MARKER-[A-Za-z]"` over the tree finds nothing outside the skill's own instructions.
- No canary appears in any of the five reports.
- Positive control: one agent with no exclusion was told to read `.claude/rules/rule-index.md` and quote its trailing HTML comments; it quoted the planted rule-index canary. The probe can detect a leak.
- **Limit, stated.** All five reviewers reported that the harness attached `.claude/CLAUDE.md` to their context unasked, and the security lens also saw `agent-workspace.md` from the main checkout. None opened them and no canary reached a report — but the probe proves only "the canary did not reach a report", not "the file was not seen" (SEC-7). Same limit as `runner-manual-review-1.md`.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| SEC-1 Per-attempt run data is new HTTP exposure; the record carries session and request ids | security | High | PROPOSE FIX | §3.4 `AttemptTiming` allowlist; §3.6 names the never-copied fields; TC-472 and TC-475 assert no seeded session/request id |
| SEC-2 Read-only GET classifier not in the change list | security | Medium | PROPOSE FIX | §3.6, §4 #24: two `readOnlyGET` entries; `TestEveryRouteIsClassified` named as the control |
| SEC-3 Unbounded per-read work under the read lock | security | Medium | PROPOSE FIX | §3.5 incremental per-task index with a high-water mark; runs grouped per read |
| SEC-4 No per-product authorisation | security | Low | PROPOSE FIX | §3.6 states the single-trust-domain assumption; N7 |
| SEC-5 500 path echoes internal error text | security | Low | PROPOSE FIX | §3.6 fixed body `timing unavailable`, cause logged |
| SEC-6 Chip URL encoding unspecified | security | Low | PROPOSE FIX | §3.7 `url.PathEscape` / `encodeURIComponent` |
| SEC-7 Deny-list enforced by instruction only | security | Medium | ESCALATE | Harness isolation, not this change; recorded under Isolation above, as in runner-manual |
| The per-card JSON route has no consumer | zoom-out | Major | CONTEST | The brief asks for the board JSON "plus a per-card JSON route if a detail route is cheaper"; segments on every board read are not cheaper (§7) |
| Workflow totals go beyond the request | zoom-out | Major | CONTEST | The brief asks for "workflow-level totals too if cheap" (§2 G4, §7) |
| An open card's chip freezes; a ticker already exists | zoom-out | Major | PROPOSE FIX | §3.7 `data-elapsed-since` on live.js's existing `tick()`; chip text is `relSpan` |
| `fmtDur` duplicates `relSpan` | zoom-out | Nit | PROPOSE FIX | Chip uses `relSpan`/`span`; the sheet keeps finer `fmtDur`, reason stated (§3.7) |
| The design forbids charts but adds one | zoom-out | Nit | PROPOSE FIX | Proportion bar dropped |
| `byStatus` has no reader | zoom-out | Nit | PROPOSE FIX | Removed |
| Renaming `openAnswer`/`showAnswer` is churn | zoom-out | Nit | PROPOSE FIX | §3.7: names kept, two id checks widened, one new delegate branch |
| Two stored times for one fact | zoom-out | Nit | DECLINE | §7: overwriting `ts` changes an existing column's meaning and old rows still need the offset |
| Flooring makes the bucket sum exceed the lead; idle can go negative | comprehension | Major | PROPOSE FIX | §3.2 step 1 running maximum; invariant exact; idle clamped; TC-469 |
| Summed `costUsd` drops the basis (BR-31) | comprehension | Major | PROPOSE FIX | §3.2 step 8 cost per basis; BR-31 named in the header |
| History and current status can disagree | comprehension | Major | PROPOSE FIX | §3.2 step 5 `complete=false` with a reason; status from the card; G7 narrowed; TC-471 |
| `queued` mislabels READY under `off` | comprehension | Major | PROPOSE FIX | Mode passed as a string; READY under `off` is `operator` (§3.2 step 7) |
| D-081 content never stated | comprehension | Major | PROPOSE FIX | §3.9 |
| Fallback opening segment underspecified | comprehension | Nit | PROPOSE FIX | §3.2 step 2 |
| `workflowTiming` has no `now` | comprehension | Nit | PROPOSE FIX | §3.3 signature |
| Types and view-models not defined | comprehension | Nit | PROPOSE FIX | §3.4, §3.7 `TimingView` |
| Bucket vocabulary not listed | comprehension | Nit | PROPOSE FIX | §3.2 step 7 table, closed list |
| Modal markup omits its hidden condition | comprehension | Nit | PROPOSE FIX | §3.7 template snippet |
| BLOCKED meaning narrower than its transitions | comprehension | Nit | PROPOSE FIX | §3.2 table and L7 |
| Cross-product filtering relies on unstated uniqueness | comprehension | Nit | PROPOSE FIX | Function takes the `TaskRecord` and filters `DeskID`; glossary states uniqueness |
| No phases, no exit conditions | readiness | Major | PROPOSE FIX | §3.8 |
| Change list misses `auth_test.go` | readiness | Major | PROPOSE FIX | Same as SEC-2 |
| Chip duration format undecided for both renderers | readiness | Major | PROPOSE FIX | Server-formatted `leadText` and `title` on the summary (§3.4) |
| QA register update under-specified | readiness | Major | PROPOSE FIX | §4 #20 and §5: rows, `h3`s, TC-124/125 prose, rule file range map and stale sentence |
| Timing sheet view-model, hidden state and error path | readiness | Major | PROPOSE FIX | §3.7 `TimingView`, hidden condition, `s.fail(ErrNotFound)`; bar dropped so no width helper |
| `decorateBoardTiming`/`workflowTiming` signatures | readiness | Nit | PROPOSE FIX | §3.5 method; §3.3 `now` |
| Segment bucket vocabulary and JSON tags | readiness | Nit | PROPOSE FIX | §3.4 |
| D-081, changelog and graph.html content | readiness | Nit | PROPOSE FIX | §3.9; changelog sentence in §4 |
| Line anchors drift | readiness | Nit | PROPOSE FIX | Corrected |
| Old card histories are incomplete yet reported as known | critic | Major | PROPOSE FIX | Same as the comprehension finding; L3 restated (events lost, not delayed) |
| A clock step makes the bucket total exceed the lead | critic | Major | PROPOSE FIX | Same as the comprehension finding |
| Old and new rows use two clocks | critic | Major | PROPOSE FIX | §3.1 `dbOffset` from a `sysdate()` probe, rounded to 900 s; the `TaskRun.startedAt` sentence corrected; TC-476 |
| The open chip goes stale while a live dot says it is current | critic | Major | PROPOSE FIX | Ticks; no live dot |
| Renaming the answer-sheet JS risks answering | critic | Major | PROPOSE FIX | Same as zoom-out; R4 names the manual check |
| READY mislabelled under `off` and under BR-38 | critic | Nit | PROPOSE FIX | Mode string; the BR-38 case is L2 |
| Wrong citations | critic | Nit | PROPOSE FIX | Corrected (`board.go:380-383,412-416`, `runner.go:157`) |
| `GET /events` changes shape silently | critic | Nit | PROPOSE FIX | §3.1 states the additive `"At"` key |
| D-081 content | critic | Nit | PROPOSE FIX | §3.9 |
| Sheet error and empty paths | critic | Nit | PROPOSE FIX | §3.7 |
| Security lens not triggered; stale rule sentence | critic | Nit | PROPOSE FIX | Header names the lens; §4 #20 fixes the sentence |
| Performance claim not measured | critic | Nit | PROPOSE FIX | §3.5 labelled an assumption; index; runs grouped |
| Two accepted limits missing | critic | Nit | PROPOSE FIX | L6, L8 |

No Blocker was raised and the High (SEC-1) is fixed in v2, so no second round was run (policy step 7). Approval by delegation is recorded in the design's §11.
