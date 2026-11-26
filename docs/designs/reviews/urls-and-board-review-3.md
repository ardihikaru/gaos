# Round 3 — `docs/designs/urls-and-board.md` (rev 3)

2026-11-26. The same five lenses read rev 3 with no knowledge of the earlier rounds. Reports,
verbatim: `urls-and-board-comprehension-3.md`, `urls-and-board-zoomout-3.md`,
`urls-and-board-critic-3.md`, `urls-and-board-readiness-3.md`,
`urls-and-board-security-review-3.md`.

| Lens | Blocker | Major | Nit |
|---|---:|---:|---:|
| Comprehension | 0 | 4 | 5 |
| Zoom-out | 0 | 3 | 6 |
| Critic | 0 | 9 | 8 |
| Readiness | 0 | 4 | 11 |
| Security | Critical 0 · High 0 | Medium 1 | Low 4 |

**No Blocker, and no Critical or High.** The round-2 Blocker (the search text reaching the access
log) was not raised as one again; what remained of it is row 1 below. Every lens that checked
phase 1's routes, call sites, gate ordering and counts found them correct. The findings are almost
all in phase 2's client half.

## Isolation

Canaries planted in both checkouts before the run and removed after; no report contains a marker.

## Triage

| # | Finding | Lens · severity | Disposition | What changed in rev 4 |
|---|---|---|---|---|
| 1 | The search box is a named field of an `hx-get` form, so htmx (or a native submit, or a failed script) still puts it in the URL; the source-reading test cannot see that | Critic, Comprehension · Major; Security SEC-1 · Medium; Readiness · Nit | PROPOSE FIX | §4.5: the search input has no `name`, so nothing can serialise it; the hook reads it by id. §6 and TC `TestTheSearchTextNeverTravelsInAURL` assert the rendered form and a `?q=` request instead of reading source. sisflow's `reload()` sends headers, not `FormData`. |
| 2 | The hook re-adds the form's fields to the links meant to clear them ("Board", "Clear filters") | Critic, Comprehension, Readiness · Major | PROPOSE FIX | §4.2, §4.5: a parameter in the URL wins over the header even when empty, and a clearing link names what it clears (`view=`, `role=&…`). Asserted in `TestABoardMutationKeepsTheView`. |
| 3 | The carriage of board state on mutations was left open (URL rewrite, or headers "decided at the checkpoint") | Zoom-out · Major; Security SEC-5 · Low; Readiness · Nit | PROPOSE FIX | Decided: one header, `X-Board-State`, with §4.2's bounds; the hook never edits a URL or a body. The §11 row is gone; §8 records the rejected URL form. |
| 4 | `board/live` cites §3.4's rule and must never push | Critic · Major; Comprehension · Nit | PROPOSE FIX | §3.4: `pushURL(…, replaceOnly)`; §4.3 states the rule. |
| 5 | A hint during a filter change aborts the operator's request and the address never follows | Critic · Major | PROPOSE FIX | §4.3: `board/live` sends `HX-Replace-Url` for a refresh too, guarded by the path check. |
| 6 | The search box is never said to be re-rendered | Critic · Major | PROPOSE FIX | §4.1, §4.5: printed from `BoardWindow.Text`. |
| 7 | sisflow loses depth and scroll on every whole-sheet response | Critic · Major | PROPOSE FIX | §4.8: depth and scroll are module state keyed by product, restored on remount. |
| 8 | With lanes gone, sisflow labels its single lane "Plan" | Critic · Major | PROPOSE FIX | §4.7: the label is emptied and the lane header hidden; in the exit. |
| 9 | "The task's last event" is not what the timing index holds | Critic · Major; Readiness · Nit | PROPOSE FIX | §4.1 step 4: "last state change", defined on the index after `extendIndexLocked`; a label change does not move a card. |
| 10 | The paged JSON has no test; the checkpoint regex names a test that does not exist | Critic, Readiness · Major | PROPOSE FIX | `TestTheBoardJSONPagesOnlyWhenAsked` (TC-747); phase 2 is fifteen tests, 747 rows, 612 Go tests; the checkpoint names its eight tests. |
| 11 | The column hash still changes on every request for an unfinished card (`AsOf`, the `…Seconds` fields) | Readiness · Major | PROPOSE FIX | §5.1: the hash input is an explicit struct, `colRev`/`cardRev`, that names every field in it. |
| 12 | The 64-byte cap contradicts `shown` and `rev` | Readiness, Comprehension · Major; Zoom-out · Nit | PROPOSE FIX | §4.2: one 128-byte bound for `BoardQuery` values; `shown` and `rev` have their own, measured after decoding. |
| 13 | Goal 6 contradicts §4.1 step 2 | Comprehension · Major | PROPOSE FIX | Goal 6 and step 2 now say the same thing: ignored on screen at once, gone from the address at the next address header. |
| 14 | The per-column hash is unproven; send all nine columns | Zoom-out · Major (second time) | DECLINE — put to the operator | §8 restates the alternative with its real cost (the size argument is withdrawn) and the reason that remains: DOM churn during hint bursts. |
| 15 | The "fresh" mark traces to no goal | Zoom-out · Major | PROPOSE FIX | Goal 5 names it, with the operator's request it answers; D-094 records it. |
| 16 | `sheet` does not apply the no-store rule; `board/live` and the paged JSON say nothing | Security SEC-2 · Low | PROPOSE FIX | §3.4, §4.3, §6; asserted. |
| 17 | The log also holds the page address as `Referer` | Security SEC-3 · Low | PROPOSE FIX | §6 says so. The log's location and retention are the deployment's and are not changed. |
| 18 | A cross-site link can open a view with a drawer already open | Security SEC-4 · Low | PROPOSE FIX | §3.7: a deep link selects, it never acts. |
| 19 | Split phase 2 into two pull requests | Zoom-out · Nit | DECLINE | The server half has its own checkpoint; a release with paging but the old whole-sheet refresh would lose "Show more" depth on every hint. |
| 20 | Low-level constants in a design | Zoom-out · Nit | DECLINE | As in round 2. |
| 21 | Smaller: `AssignSlug`'s name; `PageTitle` introduced twice; undefined terms and the column ids; where the back-fill runs; negative `shown`; how the hook recognises a refresh; the PR/Auto-fix view-model fields and default values; the failed-write conformance case; `Matched` "true"; numeric id order; `EventCount`'s cost; story facets; an unknown `/p/{ref}`; which attributes global views carry; `sort=status`; the two meanings of "label"; a move overtaken by an older refresh; the element target's derivation | all · Nit | PROPOSE FIX | Each corrected where it stood. |

## For the operator

Three trade-offs the reviewers raised that are decisions, not defects:

1. **The product's name is now in addresses, and so in the edge's access log, the browser's
   history and the tab title** (§6). This follows from choosing `/p/<slug>/…`.
2. **A slug is not reserved after its product is removed by hand** (§12).
3. **Per-column replacement with a hash, against re-sending all nine columns on every hint** (§8,
   row 14 above). The hash is more code; the alternative is simpler and churns more DOM.
