<!-- Round 2, 2026-11-26. Lens: critic. Target: docs/designs/urls-and-board.md (rev 2). Verbatim reviewer output. -->

# Goldfish critic review: `docs/designs/urls-and-board.md` (rev 2)

**What I checked.** I read the design, the rubric, BR-02, BR-05 and BR-55 in `brd.html`, the decision log and the QA register, and I checked the design's code claims against the `feat-urls` checkout.

**Claims that hold:**
- Every `buildBO(` call site in §3.4.
- The route table (`main.go:499-583`).
- The gate (`auth.go:527-565`).
- `slug()` (`store_mem.go:2462`).
- `writeProduct` (`store_arcade.go:763-770`) and the load query (`:1493-1496`).
- The fold's no-op list (`fold.go:442`).
- The eleven `<a … hx-get>` elements.
- No `pushState` or `HX-Push-Url` outside the vendored htmx.
- The CSS anchors.
- The TC and decision-log baselines: TC-721 is the highest ID, the log says "Ninety-one entries", and D-092 is the last entry.

**Counts.** The arithmetic holds: phase 1 adds 10 Go tests and 1 deploy check, and phase 2 adds 13 Go tests.

Several of the design's central claims do not survive contact with the code or the deployment.

---

### The search text `q` is written to the edge access log; §6's guarantee is false - Severity: Blocker
- **Affected scope:** §4.2 (`q` "request only"), §4.3, §4.5, §4.8, §6 ("What becomes visible outside the session"), §8 (last row).
- **Finding and rationale:**
  - §6 states as a requirement that `q` "must not reach a log", and §8 rejects keeping it in the URL for that reason. But every way the design sends `q` is a `GET` with `q` in the query string:
    - the filter form's `board/live` request (§4.5),
    - the sisflow `…/board?paged=1&<filter>` load and the `…/board/cards?…` pages (§4.8),
    - the Board/Table links "with the current filter" (§4.5).
  - Both Traefik configs enable the default access log: `deploy/traefik/traefik.yml:39` and `deploy/stacks/traefik.yml:54`, each `accessLog: {}`. That default (Common Log Format) records the full request line, query string included. So the mechanism contradicts the security property the design claims.
  - A related claim is also wrong. §6 says "the product id form of the URL still works for anyone who prefers it", implying the name stays out of logs. But `/p/p-6/…` answers `302` to the slug (§3.1 step 2), so the next request line in the log carries the name anyway.
- **Recommendation:** Do one of:
  - send the filter state of `board/live`, `board?paged=1` and `board/cards` in a request body or a request header, which the access log does not record (this needs a reason for a non-`GET` read, or a header-based design), or
  - configure `accessLog` to drop the query (Traefik `fields.names` / `RequestPath` handling) and add that to the change list and a `check-stacks.sh` assertion, or
  - drop the claim and record in D-093 that `q` reaches the edge log.

  Either way, remove the "id form" mitigation from §6 and §11, or say the redirect defeats it.

### Background, deferred and out-of-order responses push or replace the address - Severity: Major
- **Affected scope:** §3.4 (`pushURL` table, first row "any | differs | push"), §4.3 (`board/live` sets `HX-Replace-Url`), §5.3.
- **Finding and rationale:**
  - `live.js` decides to refresh when the hint arrives, then fires 200 ms later through `soon()` (`static/live.js:48-51`, `:97-107`) without re-checking which view is on screen.
  - A refresh held back by `busy()` is saved in `deferred` and replayed on the next `htmx:afterSwap` (`static/live.js:76-84`, `:160-162`). That swap is often the operator's own navigation away from the Board.
  - Today such a stale refresh flips the screen back. Under §3.4 it also carries the new `HX-Current-URL` (for example `/p/x/stories`) while its `Href` is `p/x/board`. The paths differ, so the refresh sends `HX-Push-Url`: a background refresh now adds a history entry and navigates the operator.
  - In phase 2, a late `board/live` response sets `HX-Replace-Url` to the Board while the screen shows another tab, because the OOB columns find no target.
  - A filter change and a live refresh are not ordered against each other: §5.3 serialises hints only. An old-filter response arriving last puts the old filter back on the columns and in the address while the form shows the new one.
- **Recommendation:**
  - Mark background refreshes, for example with a request header that `live.js` sets, and have `pushURL` and `board/live` never send a push or replace header for them.
  - Have `live.js` re-check `#boMain`'s view and tab when the timer fires, and drop `deferred` on navigation.
  - Serialise every `board/live` request, filters included: one in flight, or `hx-sync` with `replace`, or a generation number that drops stale responses.
  - Add a case to `TestNavigationPushesReplacesOrLeavesTheAddress`.

### Every Board mutation except a move still replaces the whole sheet, so the filter, depth and scroll are lost while the URL keeps them - Severity: Major
- **Affected scope:** §5.1 (only `handleMoveCard` is changed), §9 phase-2 change list, Goal 5.
- **Finding and rationale:** Six more Board-tab actions answer with `renderBOAfter(…, "board")` or `buildBO(…, "board")`:
  - send-back (`handlers_board.go:149`),
  - set label (`handlers_board.go:226`),
  - archive or restore a conversation (`handlers_conversation.go:121`),
  - answer a question from the Board (`handlers_questions.go:86`),
  - close a defect (`handlers_autofix.go:143`, `:164`).
  
  Each re-renders `#boSheet` through `buildBO`. The design never says whether `buildBO` reads `BoardQuery` or `shown` from a non-`GET` body, and two of those routes (`products/{id}/tasks/…/answer`, `products/{id}/defects/…`) are outside the hook's `products/<id>/board` prefix (§4.5).
  
  The result: the board resets to an unfiltered, default-depth state and both scroll positions are lost. Meanwhile `pushURL` leaves the address alone ("not GET, same path → none"), so the address says `?role=x&view=table` and the screen shows something else. This breaks Goal 5 and Goal 6 for the commonest Board actions.
- **Recommendation:** List every Board-tab mutation and give each the move's treatment: answer with `board/live`, or the table body, plus the toast. Or state that `buildBO` builds the Board from the request's `BoardQuery`/`BoardWindow` whatever the method, and widen the hook to those routes. Add the handlers to §9 and a case to `TestAMoveAnswersWithTheBoardNotTheSheet`.

### The sisflow paging premise is wrong: the library never records `exhausted`, and a column can stall - Severity: Major
- **Affected scope:** §4.8 ("Show more" row), §7 (stale offset row).
- **Finding and rationale:**
  - `index.js:162-167` only emits `board:page-loaded`. Nothing in the vendored library listens for it, and `state.exhausted` is set only once, to `false` (`virtual-list.js:25`).
  - Loading stops only through `state.loading`, which resets only when the cell's card count changes (`virtual-list.js:29`, `:46-48`). So when `loadPage` drops every card as a duplicate (§4.8, §7), or the fetch fails (`index.js:167-169`), the column's `loading` stays `true`. That column never asks again until a `reload()` changes its length.
  - The first case is exactly the "stale offset" case §7 says is handled.
  - With `virtual: {force: true}`, every short column whose content is within 600 px of its end asks for a page on each mount and each `reload()` (`virtual-math.js:75`). That is up to nine empty `board/cards` requests per hint, which works against §5.3.
- **Recommendation:**
  - Correct §4.8.
  - Put the per-column `counts` from the paged document to use: `loadPage` answers `{cards: [], exhausted: true}` without a request when the board already holds `counts[col]`.
  - Specify what happens on an all-duplicate page (fetch the next offset) and on an error (the stall, and how it is cleared).
  - Add both to §7.

### A stored slug is still reused after a manual delete, which is the hazard §3.2 cites - Severity: Major
- **Affected scope:** §3.2 step 3 ("taken" = held by another *existing* product), "Why stored and not derived", §8 row 4.
- **Finding and rationale:**
  - The design argues for a stored slug because products are removed from the database by hand, and a bookmark must not open the wrong product.
  - But `newSlug` treats a slug as taken only if a product that still exists holds it. Remove "Route Planner" by hand, create a new product with the same name, and it gets `route-planner`: the old bookmark opens the new product.
  - That is the very failure the section claims a stored slug prevents.
  - `nextProductID` has the same weakness. It is "one past the highest" id (`store_mem.go:412-434`), so deleting the highest product frees its id. TC-699's wording overstates the guarantee the design leans on.
- **Recommendation:** Either keep a tombstone or high-water set of slugs ever issued (stored on the Tenant vertex) and never reissue one, or narrow the claim to "a slug does not move while its product exists" and record the reuse case as an accepted limit in §12.

### The ArcadeDB back-fill has no named mechanism, and its failure path leaves an event that was never persisted - Severity: Major
- **Affected scope:** §3.2 (`NewArcadeStoreFor` bullet), §7 row 5.
- **Finding and rationale:**
  - The back-fill runs after `load` and before `installHook` (`store_arcade.go:154-166`), outside any handler's `mutate()`. The design says "one mutation writes the product … and emits `RecordUpdated`" but never names the transaction, the persist path, or whether the event reaches the graph's event store or only the projection.
  - On a write failure, the slug "stays in memory" and the `RecordUpdated` is already in the projection's stream. The Timeline shows a change the database never recorded, which is BR-02 inverted (`brd.html:1226`).
  - The next boot then emits a second "slug assigned" event for the same product.
- **Recommendation:** Name the call: a `persist` of a `Result` that holds the product and the event in one transaction. Only append the event to the projection after the commit, or say that a failed write rolls back both the in-memory slug and the event. Add the failure case to the conformance table.

### The page title has no single owner, and sign-out blanks it - Severity: Major
- **Affected scope:** §3.3 (`View.PageTitle`), §3.4 ("One call site", `bo-title`).
- **Finding and rationale:**
  - `PageTitle` is on `View`, but `sheet(w, r, …)` returns a `*BOView`. Every one of the 13 call sites in §3.4 would have to set `v.PageTitle` by hand, and the design does not say who does. Two implementers will diverge, and a missed site sends `<title></title>`.
  - `bo-title` goes first in `sheets`, which `handleLogout` renders with `v.BO = nil` (`handlers_bo.go:109-114`). So signing out sets an empty title.
  - `handleBOSheet` renders `bo-sheet` directly (`handlers_bo.go:139`), not one of the four templates listed, so opening the back office from the topbar (`chrome.html:33`) never updates the title.
- **Recommendation:** Put the title on `BOView`, set it in `buildBO` beside `Href`, and have `bo-title` print `.BO.PageTitle`, falling back to `GAOS · Products` when `.BO` is nil. Include `bo-sheet` in the template list. Assert the title in `TestNavigationPushesReplacesOrLeavesTheAddress`.

### `TestEveryRouteIsClassified` does not "keep its invariant" - Severity: Nit
- **Affected scope:** §6, second bullet; §10.1 "Changed, keeping their ids".
- **Finding and rationale:** The test asserts today that every non-public route, signed out and without htmx, answers 401 JSON (`auth_test.go:332-333`). §3.7 makes page routes answer 401 HTML, so that assertion must gain an exception for `pagePatterns`.
- **Recommendation:** Say that the JSON assertion is narrowed to patterns outside `pagePatterns`.

### The Auto-fix product link is not covered by `afPageHref` - Severity: Nit
- **Affected scope:** §3.6, second bullet.
- **Finding and rationale:** `templates/backoffice.html:904` is `<a href="products/{{.ID}}" hx-get=…>`, a product link, not a filter link. `afPageHref` does not change it, so `TestEveryHtmxLinkHasAPageHref` fails on it. Its row model also needs `Slug`.
- **Recommendation:** Name this link, and the `Slug` field its row type gains, in §3.6 and §9.

### Anchors that point at the wrong declaration - Severity: Nit
- **Affected scope:** §3.4 table; Terms.
- **Finding and rationale:**
  - `prview.go:125` is `prHref`; `PRFilter` is at `prview.go:36`.
  - `autofix_view.go:672` is `afHref`; `AFFilter` is at `:157`.
  - `deploy/traefik/dynamic.yml:63` is a comment; the prefix is at `:69`.
- **Recommendation:** Fix the three anchors.

### Where `BoardQuery` lives, and when it is tested - Severity: Nit
- **Affected scope:** §4.1 ("Two types, in the new file `board_page.go`"), §9, §10.
- **Finding and rationale:**
  - §4.1 puts `BoardQuery` in `board_page.go`. §9 puts it in `urls.go` in phase 1 and moves only `parseBoardQuery`.
  - The parser and `Encode` ship in phase 1, but their round-trip test (`TestTheBoardQueryRoundTrips`) is phase 2.
  - "The two phases can be approved separately" is true only in one direction: phase 2 needs phase 1.
- **Recommendation:** Pick one file, move the round-trip test (or a phase-1 subset) into phase 1, and say that phase 2 depends on phase 1.

### "Needs you" and "open send-back" are each used in two senses - Severity: Nit
- **Affected scope:** §4.1 step 3, §4.2 `needs`, §4.6 Status column.
- **Finding and rationale:**
  - The filter's "Needs you" includes `REVIEW` and blocked send-backs. The table's badge covers `AWAITING_INPUT` only.
  - `BoardSendBack.Open` means "another send-back is still allowed" (`sendback.go:389`, `sent < maxDesignSendBacks`), not "a send-back is open". Its condition also requires a review-kind task, which the design omits.
- **Recommendation:** Define the term once, give the exact predicate (kind, status, reason, `sent < cap`), and use the same predicate for the badge and the filter.

### "No `row` on any card" is not what `BoardCard` serialises - Severity: Nit
- **Affected scope:** §4.3 (`paged=1`), §4.7.
- **Finding and rationale:** `BoardCard.Row` is `json:"row"` with no `omitempty` (`board.go:44`). Clearing it emits `"row":""`, which sisflow may treat as an unknown lane distinct from `null`. `board.js:202` maps rows to `null` for that reason.
- **Recommendation:** Specify `omitempty` on `Row` (safe, because `laneOf` never returns empty for the unpaged board) or `row: null`, and say which.

### The 300-card clamp silently caps "Show more" and depth across a reload - Severity: Nit
- **Affected scope:** §4.2 (`shown` clamped to 1..300), §4.8 "Depth across a reload", §12 ("None open").
- **Finding and rationale:**
  - On the server board, "Show more" stops at 300 while the header can say "300 of 412".
  - A sisflow column scrolled past 300 comes back with 300 after a reload.
- **Recommendation:** State the server board's behaviour at the cap (hide the button, or say "first 300"), and record the limit in §12.

### Sorting Done newest-first does work before the cut, which step 3 says does not happen - Severity: Nit
- **Affected scope:** §4.1 steps 3-4, §4.6 `sort=updated`.
- **Finding and rationale:** Sorting Done, Canceled and the table by last-event `Seq` needs that number for every matching card before the cut. `taskEventsLocked` (`timing.go:404-414`) copies the task's events on every call, which adds allocation to the ≤ 3 ms / ≤ 8 MB targets.
- **Recommendation:** Read the tail of `s.timingIdx[id]` directly (one int, no copy) and drop "Nothing else is computed before the cut".

---

Sections that hold up on inspection:
- §3.1's routing and its count.
- §3.5's history configuration, which matches the vendored htmx keys.
- §3.7's gate ordering, which keeps BR-55.
- §10's count arithmetic.

Counts: B=1 M=6 N=8
