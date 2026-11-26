# Round 2 — `docs/designs/urls-and-board.md` (rev 2)

2026-11-26. The same five lenses read rev 2 with no knowledge of round 1 (they were told not to open
`docs/designs/reviews/`). Reports, verbatim: `urls-and-board-comprehension-2.md`,
`urls-and-board-zoomout-2.md`, `urls-and-board-critic-2.md`, `urls-and-board-readiness-2.md`,
`urls-and-board-security-review-2.md`.

| Lens | Blocker | Major | Nit |
|---|---:|---:|---:|
| Comprehension | 0 | 8 | 14 |
| Zoom-out | 0 | 4 | 2 |
| Critic | 1 | 6 | 8 |
| Readiness | 0 | 7 | 7 |
| Security | Critical 0 · High 1 | Medium 1 | Low 3 |

Both round-1 Blockers are closed: no lens raised BR-55 or the missing `pushURL` call sites again, and
the critic and the security lens each confirmed the gate ordering and the call-site table.

## Isolation

Canaries planted in both checkouts before the run and removed after; no report contains a marker.
The probe of round 1 was not repeated (same session, same agent definitions). Three reviewers again
noted the three rule files the harness attaches for a checkout under `.claude/worktrees/`.

## Triage

| # | Finding | Lens · severity | Disposition | What changed in rev 3 |
|---|---|---|---|---|
| 1 | The search text is sent as a `GET` query parameter, so it reaches the edge's access log — the opposite of what §6 claimed. The "id form" mitigation is false too: the id redirects to the slug. | Critic · Blocker; Security SEC-1 · High | PROPOSE FIX | §4.2: the search text travels in the request header `X-Board-Search`, never in a URL, on both boards; the hook removes `q` from the URL. `TestTheSearchTextNeverTravelsInAURL`. §6 drops the id-form claim and lists everything that is now in the address. |
| 2 | A background, deferred or out-of-order response pushes or replaces the address | Critic · Major | PROPOSE FIX | §3.4: `live.js` marks its requests `X-GAOS-Refresh` and `pushURL` sends nothing for them; the timer re-checks the screen; `deferred` is scoped to its view. §4.5: every `board/live` request is synchronised on the form with `hx-sync … replace`. §4.3: `board/live` never pushes. |
| 3 | Every Board mutation except a move still replaces the sheet and loses filter, depth and scroll | Critic, Readiness · Major | PROPOSE FIX | §4.5: the hook covers every request made from the Board tab and puts the filter and window on the URL; `buildBO` reads them whatever the method (never the body, which also removes the `label` name clash). §5.1 lists the six mutations and keeps the scroll across a sheet swap. `TestABoardMutationKeepsTheView`. |
| 4 | sisflow's paging premise is wrong: the library never records `exhausted` and a column can stall | Critic, Readiness · Major; Zoom-out (round 1) | PROPOSE FIX | §4.8: the library's paging is not used. "Show more" on sisflow is a button in the server's count line and a deeper `reload()`. The `board/cards` route and its offsets are gone: one new route instead of two. |
| 5 | A removed product's slug can be issued again — the hazard §3.2 cited | Critic, Comprehension · Major | DECLINE, stated | §3.2 now claims only what holds (a slug does not move while its product exists); §12 records the reuse as an accepted limit, with the reason: there is no delete operation to hang a tombstone on. |
| 6 | The ArcadeDB back-fill has no named mechanism; a failed write leaves an unpersisted event | Critic · Major; Readiness, Comprehension · Nit | PROPOSE FIX | §3.2: `MemStore.AssignSlug` through the ordinary `mutate`/`persist`; §7 says it fails exactly as any mutation does today. A conformance case covers the retry. |
| 7 | The page title has no owner; sign-out blanks it; `GET /ui/bo` never sets it | Critic, Readiness · Major; Comprehension · Nit | PROPOSE FIX | §3.4: `PageTitle` is on `BOView`, set in `buildBO`; `bo-title` has a fallback; `handleBOSheet` renders `bo-main-oob`. |
| 8 | Phase 1 ships phase 2's URL parameters; `BoardQuery` has two homes; phase 1 has no test for it | Zoom-out, Comprehension, Readiness · Major; Critic · Nit | PROPOSE FIX | Phase 1 keeps no parameter for a product section. `BoardQuery`, its parser and its test are all phase 2, in `board_page.go`. §2 says which phase needs which. |
| 9 | Client-supplied values do reach response headers; no length bound; `{section}` not validated before the redirect | Comprehension · Major | PROPOSE FIX | §6 says what is true (allow-listed keys, bounded, escaped, same-origin); §3.4 bounds every kept value; §3.1 validates the section first. |
| 10 | The Board/Table and sort links go stale after a filter change | Comprehension, Readiness · Major | PROPOSE FIX | §4.5: those links carry only their own parameter; the hook supplies the filter from the form; they re-render the sheet. |
| 11 | "Needs you" means two different sets | Comprehension · Major; Critic · Nit | PROPOSE FIX | §4.2: the filter is "Waiting on a person" (`waiting=1`): `AWAITING_INPUT` or `REVIEW`. The send-back clause is dropped, and with it the only predicate that had to be computed before the cut. |
| 12 | Column-only answers leave the Timeline count in the bar stale | Comprehension · Major | PROPOSE FIX | §4.3, §5.5: `board/live` re-sends `#navTimelineN`; the move answer is rendered with the chrome. |
| 13 | A move's answer would set `HX-Replace-Url` on a `PUT` | Comprehension · Major; Readiness · Nit | PROPOSE FIX | §5.1: no address header on a non-`GET`. |
| 14 | A filter can be active with no control showing it | Comprehension · Major | PROPOSE FIX | §4.1 step 2: every value not in the facets is cleared, labels included; a select shows whenever its facet has a value. |
| 15 | "Hide empty columns" never takes effect: the hash ignores it | Readiness · Major | PROPOSE FIX | §5.1: `hidden` is part of the hashed input. |
| 16 | Tests that must change are missing from the change list | Readiness · Major | PROPOSE FIX | §9: `autofix_view_test.go` with its four assertions; the five Board-tab tests are named with the reason they are expected to pass unchanged. |
| 17 | sisflow's hot path still builds the whole board to answer one drag | Zoom-out · Major | PROPOSE FIX | §4.1: `Store.BoardCard` at the three single-card reads; `BenchmarkBoardCard` and a target. |
| 18 | The live diff is spent on the fallback board; send all nine columns instead | Zoom-out · Major | DECLINE | §8 states the alternative fairly and why not: 270 card subtrees re-created per hint per window, during exactly the bursts the operator watches. The server board is also what a browser shows whenever the module fails — which it did for ten releases. |
| 19 | An id-bearing slug was never considered | Zoom-out · Major | CONTEST | It was one of the three forms put to the operator, who chose the bare name; §8 now records that and the cost that was weighed. |
| 20 | A stored slug is trusted, not validated, before it reaches a header | Security SEC-2 · Medium | PROPOSE FIX | §6: validated on load, re-assigned if malformed or duplicate; `url.PathEscape` in `Href`. |
| 21 | Window parameters: parse rules, the clamp after `more`, oversized pair lists | Security SEC-3 · Low | PROPOSE FIX | §4.2's second table. Offsets no longer exist. |
| 22 | No `Referrer-Policy` | Security SEC-4 · Low | PROPOSE FIX | §3.7: `same-origin` on every non-bearer route, asserted. |
| 23 | §6 omits the filters and `open=` from what the log now holds | Security SEC-5 · Low | PROPOSE FIX | §6 lists them. |
| 24 | `more` and `surface` are mode flags | Zoom-out · Nit | DECLINE | `more` lets a plain button deepen one column without client arithmetic; `surface` keeps the count line server-rendered on both boards. Each is one line in the handler. |
| 25 | Too much tuning detail (250 ms, 3px, 1.5 s) | Zoom-out · Nit | DECLINE | The readiness lens asks for exactly these numbers; the motion review can still change them. |
| 26 | Smaller: the test's JSON assertion must be narrowed; the allowed `buildBO` callers; the Auto-fix product link; no header for a missing product; how the comparison normalises; `busy()`; two visible removals; `Total` naming and the example; zero-task boards; `Row`'s `omitempty`; the 300 cap's behaviour; sorting without copying events; wrong anchors; undefined terms; the missing `?`; the branch point of `feat/board` | Comprehension, Readiness, Critic · Nit | PROPOSE FIX | Each corrected where it stood (§3.4, §3.6, §4.1, §4.3, §4.4, §4.7, §5.3, §6, §12, Terms). |

One Blocker (and the same finding as a security High) was raised, so a third round follows on rev 3.
