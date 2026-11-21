# design-iterate — round 2 triage

**Design reviewed:** `docs/designs/design-iterate.md` v2. **Result:** v3.
**Lenses:** critic (B2 M5 N6), readiness (B1 M5 N6), security (Critical 0 High 1 Medium 2 Low 5). Reports: `design-iterate-critic-2.md`, `design-iterate-readiness-2.md`, `design-iterate-security-review-2.md`. Comprehension and zoom-out raised no Blocker in round 1 and were not re-run.

## Isolation

- Canaries re-planted in the same seven standing files before the round and removed after it; `git status` shows those files unchanged.
- The validator's value pattern finds no canary in any of the three reports.
- Positive control: an agent with no exclusion quoted the new rule-index canary.
- Same stated limit as round 1: readiness reported the harness attached `.claude/CLAUDE.md` unasked; it was not used and no canary reached a report.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| `reviewVerdictOf` reads the wrong line (the verdict is line 3) | readiness, critic | Blocker | PROPOSE FIX | §0 and §3.3 specify heading / blank / `^verdict: (accept\|iterate)$`; TC-490 round-trips both verdicts and a drift line and asserts a cleared review still releases |
| A revision that delivers no design block can be approved; the review reruns on the old design | critic | Blocker | PROPOSE FIX | §3.6 `approve` refused for design and review tasks; §3.6a a re-review needs more design artifacts than send-backs, else `design-missing`; an identical revision refused; F2 corrected; TC-499 |
| SEC-1 Approving a revision that produced no design re-runs the review on the old design | security | High | PROPOSE FIX | as above |
| The fresh-session rule misses dispatches that never start a run | readiness | Major | PROPOSE FIX | §3.5 keyed on the newest `RUNNING` move, which only follows a real run; TC-493 adds the requeue case |
| The phases do not each leave a buildable, green tree | readiness | Major | PROPOSE FIX | `MoveSendBack` in P1; TC-498 from hand-made events; the `foldedBoard` edit in P2; every exit is `go vet ./... && go test ./...` |
| P3's exit condition is not a command | readiness | Major | PROPOSE FIX | a named manual check on a local memory-store instance with its assertions, reported in the PR |
| The sheet's view model and JS wiring are left to the implementer | readiness | Major | PROPOSE FIX | `View.SendBack`/`SendBackSheetView` in `view.go`; ids `sendBackModal`/`sendBackForm`/`sb-<id>`; the three `app.js` hooks named with lines |
| A behaviour change credited to a test row that does not assert it | readiness | Major | PROPOSE FIX | the drift-line assertion added to TC-493 |
| The drift-release skip list is keyed on stale block reasons | critic | Major | PROPOSE FIX | the list narrowed to `design-blocked`/`design-unreviewed`; `MoveCard` clears the reason on unblock; the board shows the current reason |
| OQ-16 is the wrong open question | critic | Major | PROPOSE FIX | OQ-11 |
| The actor claims contradict each other across merge orders | critic | Major | PROPOSE FIX | §13 rows for `Actor` and the BR-48/OQ wording under each order; N7 reworded to hold under both |
| A design refused for quoting the review gets no feedback | critic | Major | PROPOSE FIX | the refusal is a `FAILED` attempt with signature `design-quotes-review`, so the existing failure lesson carries the sentence and finding numbers; retries are operator-paced |
| The operator override in the brief is dropped without an explicit operator decision | critic | Major | PROPOSE FIX | §11 raises it as the operator's decision; BR-48 says the clearance is withdrawn on purpose |
| SEC-2 The verbatim check is narrower than the threat | security | Medium | PROPOSE FIX | every round's triage and findings, normalised (case, whitespace, punctuation) |
| SEC-3 The read boundary between workspaces now guards operator triage | security | Medium | PROPOSE FIX (docs, test) / ESCALATE (a real read boundary) | GAOS writes no packet file; the reviewer workspace reset runs before every review turn and TC-493 asserts it for the re-review; the read boundary is BR-47's inherited limit, a gateway change outside this design |
| SEC-4 The sheet handler drops `requireBO` | security | Low | PROPOSE FIX | added |
| SEC-5 Triage notes skip BR-53's stripping | security | Low | PROPOSE FIX | the exemption recorded in BR-57's text; notes rendered inside quotes |
| SEC-6 The POST's same-origin test covers one header case | security | Low | PROPOSE FIX | TC-497: `cross-site`, `same-site`, foreign `Origin` |
| SEC-7 The operator's email in event captions | security | Low | DECLINE | the caption is the BO timeline's audit view (BR-48); the retention is now stated in D-083's consequences |
| SEC-8 Full scans of the event stream on board reads | security | Low | DECLINE | the scan runs only for review cards that are `design-blocked` (a handful per product at most), on a send-back and on a design packet freeze; card timing already reads the stream per board render; an index is a cost without a measured need |
| `$card` used before it is defined | readiness, critic | Nit | PROPOSE FIX | placed after `:400`, with an id |
| Count wording in the ToC | readiness, critic | Nit | PROPOSE FIX | "Eighty-two decisions" for `toc/product.js:129` |
| Ambiguous scope of the 24-rune threshold | readiness, critic | Nit | PROPOSE FIX | "any note, triage or finding, of at least 24 runes" |
| One stale ToC sentence not in the docs list | readiness | Nit | PROPOSE FIX | `architecture.js:116` |
| `sendBacksLocked` has no stated signature | readiness | Nit | PROPOSE FIX | stated |
| Register timing across phases not stated | readiness | Nit | PROPOSE FIX | one commit |
| The route's body format undefined for JSON clients | critic | Nit | PROPOSE FIX | form only, JSON → 415 |
| The zero-findings worst case cannot be parsed | critic | Nit | PROPOSE FIX | the sheet's sentence for empty findings stated |
| Booking a left DONE segment as blocked mislabels time | critic | Nit | PROPOSE FIX | stated as a convention in §3.1 and D-083 |

No severity was changed. A third round runs on v3 for the three Blockers and the High.
