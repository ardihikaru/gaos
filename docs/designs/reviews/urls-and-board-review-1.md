# Round 1 — `docs/designs/urls-and-board.md`

2026-11-26. Five zero-context reviewers read the first draft. Their reports, verbatim:
`urls-and-board-comprehension-1.md`, `urls-and-board-zoomout-1.md`, `urls-and-board-critic-1.md`,
`urls-and-board-readiness-1.md`, `urls-and-board-security-review-1.md`.

| Lens | Blocker | Major | Nit |
|---|---:|---:|---:|
| Comprehension | 0 | 9 | 9 |
| Zoom-out | 0 | 5 | 4 |
| Critic | 2 | 9 | 8 |
| Readiness | 0 | 12 | 5 |
| Security | Critical 0 · High 0 | Medium 3 | Low 3 |

## Isolation

- **Canaries.** One `EGM-MARKER-…` line was planted in each standing file of both checkouts (the
  main one and `feat-urls`) before the reviewers ran and removed after. No report contains a marker.
- **Probe.** A `general-purpose` agent, asked with no tool call whether its context held the
  sentence that opens the always-on memory file, answered YES (the positive control). A
  `gaos-goldfish-comprehension` agent answered NO, read `docs/designs/README.md` in the `feat-urls`
  checkout, and answered NO again.
- **A note four reviewers made themselves.** The checkout sits under the main repository's
  `.claude/worktrees/`. Reading it attached three rule files of the main checkout
  (`agent-workspace.md`, `qa-test-plan.md`, `frontend.md`) — the rubric — and no memory file.
- **Redaction.** The first draft used a real product's name as its example. D-085 forbids that
  name in a tracked file, and the critic flagged it. The design now uses an invented product, and
  the name is redacted in the three reports that quoted it. Nothing else in them was changed.

## Triage

Findings raised by more than one lens are one row. Severities are the reviewers'.

| # | Finding | Lens · severity | Disposition | What changed in rev 2 |
|---|---|---|---|---|
| 1 | A signed-out page route answering `200` contradicts BR-55 | Critic · Blocker; Security SEC-2 · Medium | PROPOSE FIX | §3.7: the route is refused with `401`; only the body changes (the sign-in page instead of JSON). No new route class. §6 spells out the four assertions `TestEveryRouteIsClassified` gains. |
| 2 | `pushURL` misses half the handlers that render the sheet, and a refresh or a mutation strips the query from the address | Critic · Blocker; Readiness · Major ×2; Comprehension · Major | PROPOSE FIX | §3.4: one method, `sheet`, replaces `buildBO` at every call that renders the sheet (table of all eleven); a source test holds the list. A mutation never rewrites a query; a background refresh sends the page's own query. `Href`'s query is rebuilt from parsed parameters, per view. |
| 3 | Ctrl-click on `<a href hx-get>` does not open a tab; existing `href`s point at fragments | Critic · Major | PROPOSE FIX | §3.6: a capture-phase listener for modified clicks; `prPageHref`/`afPageHref`; `TestEveryHtmxLinkHasAPageHref`. |
| 4 | The slug back-fill writes state with no event (BR-02) | Critic · Major | PROPOSE FIX | §3.2: the back-fill emits the existing `RecordUpdated` per product. |
| 5 | A failed back-fill write stops the boot | Security SEC-6 · Low | PROPOSE FIX | §3.2: logged; the slug is served from memory and written at the next boot. |
| 6 | Store the slug at all? No code deletes a product | Zoom-out · Major; Critic · Nit | CONTEST | Products are removed from the database by hand; `nextProductID` (`store_mem.go:417`) and TC-699 exist because of it. A derived slug of the surviving namesake would change. §3.2 and §8 now say this instead of "when the older one is deleted". |
| 7 | "Count the Timeline without the copy" names no mechanism | Critic, Readiness, Comprehension · Major | PROPOSE FIX | §5.5: `Store.EventCount`, `ProductDetail.TimelineN`; both in §9. |
| 8 | sisflow "Show more" contradicts the library, which already ingests a page; one `limit` cannot hold per-column depth; labels go stale | Critic, Readiness, Comprehension, Zoom-out · Major | PROPOSE FIX | §4.8: `loadPage` returns the page for the library to ingest and drops known ids; the paged JSON takes `shown`; totals move to the server's count line. |
| 9 | A move cannot answer "the same response": the `PUT` carries no filter, depth or `rev` | Critic, Comprehension · Major | PROPOSE FIX | §4.5: one `htmx:configRequest` hook adds them to every request under `products/<id>/board`. §5.1 states the answer, including the table view. |
| 10 | Hashing rendered HTML re-sends every active column each minute; the 60 KB target cannot be met | Critic, Zoom-out · Major | PROPOSE FIX | §5.1: the hash input is the column's data with the two ticking strings blanked. §5.6 shows the arithmetic of each target. |
| 11 | Separate "next page" routes are unnecessary and append duplicates on a stale offset | Zoom-out, Critic, Readiness · Major | PROPOSE FIX | §4.3: `board/col` and `board/rows` are gone; "Show more" is `board/live` with `more=`, and the column is replaced whole. sisflow's page route de-duplicates in `loadPage`. |
| 12 | "Hide empty columns" leaves an OOB swap with no target | Readiness, Comprehension · Major | PROPOSE FIX | §4.4: all nine columns are always rendered; an empty one is `hidden`. |
| 13 | `BoardQuery`: validation needs the product, the round-trip test cannot compile, request state is mixed with URL state | Readiness · Major; Critic · Nit; Comprehension · Major | PROPOSE FIX | §4.1, §4.2: `BoardQuery` (URL, comparable) and `BoardWindow` (request only); validation in `BoardPage` step 2, which returns the cleaned query. |
| 14 | `BoardCard` lacks story, workflow and updated | Readiness · Major | PROPOSE FIX | §4.1: three `omitempty` fields set only by `BoardPage`. |
| 15 | Table grouping does not fit paging, and no goal asks for it | Zoom-out, Readiness · Major | PROPOSE FIX | §4.6: no grouping (now a non-goal); one `<tbody>`, replaced whole. |
| 16 | The `needs` filter needs send-back state before the cut | Comprehension · Major; Readiness · Nit | PROPOSE FIX | §4.1 step 3. |
| 17 | Who marks a card "fresh" | Readiness · Major; Comprehension, Critic · Nit | PROPOSE FIX | §5.4: the browser, on both boards; the two classes are joined. |
| 18 | The fresh marker is polish no goal asks for | Zoom-out · Nit | DECLINE | The operator asked for smoother live changes; a mark on the card that moved is the smallest signal, and it is client-side only. |
| 19 | The new `rebuild-restart.sh` check has no register row | Critic, Readiness, Comprehension · Major | PROPOSE FIX | §10.1: TC-732; the totals are recomputed (732, then 745). |
| 20 | The phase-2 exit contradicts §5.6; benchmarks and browser measures are unnamed | Readiness · Major | PROPOSE FIX | §5.6 names four benchmarks and the browser steps; a missed target blocks the exit unless accepted. §10.2 adds the server-half checkpoint. |
| 21 | The example names a private product (D-085) | Critic · Major | PROPOSE FIX | Replaced throughout; `check-workspace.sh` passes on the branch. |
| 22 | Product names and search text reach the access log and history | Security SEC-1 · Medium | PROPOSE FIX (search), ESCALATE (slug) | `q` is request-only and never in the address (§4.2, §6). The slug in the URL is the operator's decision; §6 states the consequence and D-093 records it — raised with the operator at approval. |
| 23 | Bounds for `offset`, `limit`, `shown`, `rev`, `{col}` | Security SEC-3 · Medium | PROPOSE FIX | §4.2, §4.3, §6; negative cases in two tests. |
| 24 | "Query string kept" contradicts "no client-supplied URL reaches a response" | Security SEC-4 · Low; Critic · Nit | PROPOSE FIX | §3.1, §3.4: rebuilt from parsed parameters; the redirect test sends an unknown one. |
| 25 | The cross-origin check does nothing for `GET` | Security SEC-5 · Low | PROPOSE FIX | §3.7 says so. |
| 26 | `<title>` must be a top-level node; other sheet templates would keep a stale title | Critic · Nit; Readiness (within Major 1) | PROPOSE FIX | §3.4: `bo-title`, first line of the four templates. |
| 27 | Split into two designs | Zoom-out · Nit | DECLINE | One review, two approvals: §2 states the phases can be approved separately. |
| 28 | Land the decorator fix first as a plain fix | Zoom-out · Nit | DECLINE | The id map is built by `BoardPage`'s first pass; a separate fix would build it twice and be rewritten a release later. Only the two decorators that look up by id change (§4.1). |
| 29 | `mountBoard` already skips a mounted host | Zoom-out · Nit | PROPOSE FIX | The bullet is removed. |
| 30 | Global views keep `?product=<id>` | Critic · Nit | DECLINE | It is a filter value the fragment routes and `prHref` already use; §3.1 states the exception. |
| 31 | Smaller: eleven patterns include a redirect; drifted anchors; unanchored operator decisions; `BoardFacets` undefined; "three closed vocabularies"; "five routes"; PR #45 dependency; login with no `HX-Current-URL`; workflow totals; the `prHref` example; page titles; section numbering | Comprehension, Readiness, Critic · Nit | PROPOSE FIX | Each corrected where it stood. |

No finding was contested on comprehension. Two Blockers were raised, so a second round follows on rev 2.
