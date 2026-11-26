<!-- Round 1, 2026-11-26. Lens: critic. Target: docs/designs/urls-and-board.md (first draft). Verbatim reviewer output, except that a private product name the first draft used as its example is redacted (D-085). -->

# Goldfish critic review: `urls-and-board.md`

**Isolation note.** The checkout sits under the parent repository's `.claude/worktrees/` folder. Because of that, reading it made the harness attach three of the parent's rule files: `.claude/rules/agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. No `CLAUDE.md`, `AGENTS.md` or `GEMINI.md` content was attached, and I opened nothing under the checkout's `.claude/`. Please take this into account when you plant the canaries.

**What I checked and found correct.** These claims match the code:
- The route lines in `main.go` (499, 519, 524, 526, 537).
- `board.go:242`, `store_mem.go:1815` and `:2462`, the no-op list at `fold.go:442`, and `cardLocked` / `taskByIDLocked` / `timingIdx`.
- `render.go:192`, the gate at `auth.go:534-563`, `readOnlyGET` at `auth_test.go:258`, and `main_test.go:755-762`.
- The CSS lines 812, 855 and 1216, and the template lines 46, 71-79, 87-96, 122, 219 and 401.
- htmx is not used for history anywhere today, and no template contains `href="#`.
- The TC and count arithmetic (721 → 729 → 742, 587 → 595 → 608) adds up.

The problem statement (§1), the slug algorithm (§3.2), the alternatives (§8) and the risks table (§11) are sound apart from the findings below.

### A signed-out deep URL answering 200 contradicts BR-55 - Severity: Blocker
- Affected scope: §3.7, §6 first bullet, §11 row 5
- Finding and rationale: BR-55 (`pustaka/docs/product/brd.html:1349`) says "Every route is refused without [a verified session] unless it is on a fixed public list or is a bearer route". The design creates `classPage`, says plainly that it "is not public", and answers it signed out with `200`. That is a third class of non-refused route, and the rule does not allow it. The code shows the same rule in force: `publicPatterns` (`auth.go:499-510`) and `TestEveryRouteIsClassified`, which requires 401 from every route that is not public (`auth_test.go:321-334`). The change list does not touch `brd.html`, so the BRD would go on stating a rule the code breaks.
- Recommendation: Keep BR-55 intact. Answer a signed-out `classPage` request with status **401** and the sign-in page as the HTML body. Browsers render a 401 HTML body normally, and the test keeps its 401 invariant. Update §3.7, §6, `TestASignedOutDeepURLShowsOnlyTheLogin` and the new `rebuild-restart.sh` check (which expects `200`) to match. If you prefer to keep 200, BR-55 has to be amended in `brd.html` through a decision entry, and `brd.html` has to join the change list.

### `pushURL` misses half the sheet renderers and fires on requests that do not carry the view - Severity: Blocker
- Affected scope: §3.4 bullets 2-3, §8 row 1, §9 phase 1
- Finding and rationale:
  - **Missing call sites.** §3.4 calls `pushURL` from four handlers plus `renderBOAfter`. These also build and render the sheet and are not covered:
    - `renderBOView` (`handlers_bo.go:1026`, used by `handlers_prs.go:125` and by the contacts handlers at `handlers_bo.go:1053` and `:1069`)
    - `handleLogin` (`handlers_bo.go:96`)
    - `handlers_cw.go:87`
    - `handlers_conversation.go:121`, `:123` and `:156`
    - `handlers_questions.go:152`
  - **The design's own reason fails.** §8 justifies the header approach because it can "cover a mutation that changes the view (creating a workflow opens the Board)". That is exactly `handlers_cw.go:87`, which is not on the list. After creating a workflow the address stays on `/brd`, and a reload leaves the Board.
  - **Wrong headers from mutations and refreshes.** `Href` is built from the request being answered. A `PUT …/move`, a POST, or the live refresh `products/{id}?tab=board` (`static/live.js:105`) carries no filter or view query. So on `/p/x/board?view=table` the "same path, different query" branch sends `HX-Replace-Url` and strips the query. This happens on every mutation and every live tick. §3.8 says phase 1 "passes through" Board parameters, but there is nothing to pass through from those requests.
- Recommendation:
  - Put the call in one place every sheet-rendering path goes through (for example the render step for `bo-sheet` / `bo-main-oob` / `sheets` / `cw-closed`), and list every file it touches.
  - Specify that only a `GET` whose URL encodes the whole view may emit `HX-Replace-Url`.
  - For a mutation or a background refresh, emit `HX-Push-Url` only when the path changes, and never `HX-Replace-Url`.
  - Add a test for "a mutation on a filtered URL keeps its query".

### Ctrl-click on the new links does not open a new tab - Severity: Major
- Affected scope: §3.6, the phase 1 exit ("Ctrl-click a tab")
- Finding and rationale: The vendored htmx skips Ctrl/Meta-click only on *boosted* anchors. Its function `dt` is `ie(e).boosted&&e instanceof HTMLAnchorElement&&…(t.ctrlKey||t.metaKey)` (`static/htmx.min.js`, one line). For an anchor with an `href`, its cancel check returns true, so a plain `<a href hx-get>` turns Ctrl-click into the htmx swap. Middle-click works; Ctrl-click and Cmd-click do not. Nothing in `static/app.js` handles modifier keys. Two related problems:
  - The "form the Pull requests rows already have" is not proof that this works. Those `href`s point at fragment routes (`templates/backoffice.html:904`, `:936`, `:945`, `:962`), so a copied link opens a bare fragment, or 401 JSON when signed out.
  - §3.6 does not convert them.
- Recommendation:
  - Add `hx-trigger="click[!ctrlKey&&!metaKey&&!shiftKey]"` to every converted link, or add one delegated handler in `app.js`.
  - Change the existing `href`s at `:904`, `:936`, `:945`, `:962` and `:980` (and the Auto-fix equivalents) to page URLs.
  - Add a template test that every `<a … hx-get>` has a page-route `href`.

### The slug back-fill writes state with no event (BR-02) - Severity: Major
- Affected scope: §3.2 "No event", "Existing products"
- Finding and rationale: BR-02 (`brd.html:1226`) says "Every change emits an event. An event that was never written is a state change that never happened." The back-fill writes `slug` to every existing `Product` vertex at boot with no event. The design's defence (the fold ignores `ProductCreated`/`RecordUpdated`) addresses BR-05 but not BR-02. `ProductCreated` (`store_mem.go:1845`) also does not record the assigned slug.
- Recommendation: In the back-fill transaction, emit the existing `RecordUpdated` once per back-filled product (text like "slug assigned: x"); the catalog does not change. Alternatively, state explicitly why a one-time back-fill is outside BR-02 and get the operator's ruling.

### "Count the Timeline without the copy" needs a store method nobody declared - Severity: Major
- Affected scope: §5.5 bullet 1; §10 `TestTheBoardTabSkipsTheEventCopyAndTheDiagrams`; §9 phase 2
- Finding and rationale: The only event read on the `Store` interface is `Events(ctx) ([]Event, error)` (`store.go:70`). The tab count is `len .Timeline` (`templates/backoffice.html:77`). The test asserts "no `Events` call", so the count has to come from a new method on both stores. The method is unnamed, unspecified, and missing from the change list, which names `store.go` only for `BoardPage`.
- Recommendation: Declare the method (for example `EventCount(ctx, deskID) (int, error)`), its MemStore and ArcadeStore implementations, and the view field (for example `ProductDetail.TimelineN`). Add `store_arcade*.go` to the phase 2 change list.

### sisflow paging contradicts the library, and `limit` cannot express per-column depth - Severity: Major
- Affected scope: §4.3 JSON rows, §4.8 "Show more", §5.2
- Finding and rationale:
  - **Who loads the next page.** `attachProvider` already handles `board:load-more` itself: it calls `provider.loadPage(req)`, then `store.ingest(page.cards)` (`static/vendor/sisflow/board/index.js:162-166`). §4.8 instead has `board.js` "merging the cards into its copy of the document, and reloading the document". Do both and the cards are ingested twice or the store is overwritten. Two engineers would build this differently.
  - **Reload depth.** §5.2's `reload()` fetches "the JSON with the current filter and depths". But `GET …/board` takes one scalar `limit=N`, so after "show more" in Done a reload either drops Done back to 30 or loads N cards into every column.
  - **Totals.** The column label "Done · 30 of 412" goes stale after a library-driven `ingest`.
- Recommendation:
  - State that `board.js`'s custom `loadPage` (it already overrides one, `static/board.js:211`) fetches `…/board/cards` with the filter and returns `{cards, exhausted}`, and that `board.js` does not reload on paging.
  - Give the JSON route the same `shown=col:n,…` parameter that `board/live` uses.
  - Say how labels update after `board:page-loaded`.

### A move cannot answer with "the same response", because the PUT carries no filter, depth or rev - Severity: Major
- Affected scope: §5.1 last bullet; §10 `TestAMoveAnswersWithTheBoardNotTheSheet`
- Finding and rationale: The move buttons send only `{"column": …}` (`templates/backoffice.html:417`). The `board/live` response needs the filter, `shown` and `rev`. Without them the server cannot tell which columns changed or how deep to render them, and with a filter active it would re-render unfiltered columns. Moves from the table view have the same problem.
- Recommendation: Specify `hx-include="#boardFilters"` on the move buttons, plus the `shown`/`rev` values, added by `live.js` in an `htmx:configRequest` hook. Say what happens when they are missing (for example, send all columns at the default depth).

### Hashing the rendered HTML makes every active column "changed" each minute - Severity: Major
- Affected scope: §5.1 bullet 1 and the last paragraph; §5.6 row "two columns"
- Finding and rationale: The server renders `LeadText` inside each open card's timing chip (`templates/backoffice.html:416`). Every column holding an open card therefore gets a new hash every minute. In practice a hint re-sends Working, Review, Blocked, Ready and the others, not two columns. The §5.6 target and `TestBoardLiveSendsOnlyTheColumnsThatChanged` only pass with a frozen clock. The design's comment that this "costs an extra column" understates the effect.
- Recommendation: Hash the column's *data* (card ids, status, rank, labels, decorations, `createdAt`), not its rendered relative text. `live.js` already re-ticks `time[data-elapsed-since]` (`static/live.js:183-186`). Or exclude the `<time>` text from the hash. State the hash input precisely.

### Duplicate cards on a stale offset have no defined handling on the server board - Severity: Major
- Affected scope: §7 row 4, §4.3 `board/col`
- Finding and rationale: §7 says "both boards ignore a card id they already hold". The server board appends with `hx-swap-oob="beforeend:…"`, and htmx has no id-dedupe. A new Done card (newest first) shifts every offset, so the appended page repeats a card. That gives duplicate element ids (`tm-<id>`, `cv-<id>`, `nb-<id>`), which `app.js` delegation and focus restore rely on.
- Recommendation: Name the mechanism. One option: an `htmx:oobBeforeSwap` hook in `live.js` that removes incoming cards whose `data-card` is already in the column. Another: send `after=<last card id>` instead of `offset`, so the server can seek. Add a test.

### The QA counts omit the new `rebuild-restart.sh` check - Severity: Major
- Affected scope: §9 phase 1 `deploy/rebuild-restart.sh`; §10 phase 1 counts
- Finding and rationale: The register's rule (the attached `qa-test-plan.md`) says a change to `deploy/rebuild-restart.sh` that asserts something is a check with its own TC row; every `rebuild-restart.sh` case has one (TC-017…TC-047, …, TC-688). Phase 1 adds "`/app/inbox` and `/app/p/x/board` answer 200", but §10 counts only the eight Go tests. "Register 721 → 729" is one short.
- Recommendation: Give the deploy check TC-730 (or fold it into an existing rebuild-restart row and say so). Recompute both phases: phase 1 → 730, phase 2 TC-731…743 → 743. Go-test counts are unchanged.

### The worked example may name a private product (D-085) - Severity: Major
- Affected scope: §1.1, §2 Goal 1, §3.1, §3.3, §3.4, §3.7 ("[private product name, redacted]", `[private product name, redacted]`)
- Finding and rationale: D-085 (`decision-log.html:1786-1798`) forbids naming an operator's sample product in any tracked file, and `check-workspace.sh` fails on the forbidden-strings list. "[private product name, redacted]" appears nowhere else in the repository; this design is the only file. It is described as "the product [private product name, redacted]" that an operator opens, which reads like a real instance's product. I cannot check the list (it lives in `deploy/local/`, which I am not allowed to read).
- Recommendation: Replace it with a neutral invented name (for example `route-planner`, which the templates already use as a placeholder, `templates/backoffice.html:184`). Run `check-workspace.sh` on the design before committing.

### `<title>` must be a top-level node of the response - Severity: Nit
- Affected scope: §3.4 last bullet
- Finding and rationale: htmx applies a `<title>` only when its `parentNode` is the fragment root (`htmx.min.js`: `if(o&&o.parentNode===r){o.remove();r.title=o.innerText}`). `bo-main-oob` is `{{template "bo-sheet"}}` (`templates/backoffice.html:140`). A title placed inside `#boSheet` would be inserted into the DOM as content.
- Recommendation: State that the `<title>` is emitted before the `#boSheet` element, at the top level of the response.

### §6 says no client-supplied value reaches a response, but the 302 echoes the query - Severity: Nit
- Affected scope: §3.1 "Resolving {ref}" step 2; §6 bullet 3
- Finding and rationale: "query string kept" copies the client's raw query into `Location`. That is not an open redirect, but the §6 sentence is false as written.
- Recommendation: Rebuild the query from known parameters (`parseBoardQuery(...).Encode()` and the view's own filters), or reword §6.

### `TestTheBoardQueryRoundTrips` will not compile as written - Severity: Nit
- Affected scope: §4.1 `BoardQuery`, §10 phase 2
- Finding and rationale: `BoardQuery` has a `map[string]int` field, so `==` is a compile error. `Shown`/`Offset`/`Limit` are also deliberately left out of `Encode()` (§4.2), so they cannot round-trip.
- Recommendation: Compare only the URL fields, or move `Shown`/`Offset`/`Limit` out of `BoardQuery`.

### Product deletion does not exist - Severity: Nit
- Affected scope: §7 last row, §8 row 4
- Finding and rationale: There is no `DeleteProduct` anywhere in `dashboard/`. The failure row and the rejected-alternative reason describe a case that cannot happen.
- Recommendation: Drop the row, or reword it as "should deletion ever be added".

### The `Href` example contradicts `prHref` - Severity: Nit
- Affected scope: §3.4 bullet 1 (`prs?state=open&open=fix%3A12`)
- Finding and rationale: `prHref` leaves out `state=open`, because it is the default (`prview.go:130`).
- Recommendation: Use `prs?open=fix%3A12`.

### `board/live`'s `HX-Replace-Url` has no owner - Severity: Nit
- Affected scope: §4.5 bullet 3
- Finding and rationale: `board/live` "does not call `buildBO`", so it has no `BOView.Href`. It is also not on §3.4's list of `pushURL` call sites, so where its header is built is undefined.
- Recommendation: Say that `board/live` builds `p/<slug>/board?` + `q.Encode()` and calls `pushURL` with it.

### `is-fresh` and `is-needs-you` compete for one `css` field - Severity: Nit
- Affected scope: §5.4 last sentence
- Finding and rationale: `board.js` already sets `c.css = "is-needs-you"` (`static/board.js:203`).
- Recommendation: Say the two classes are joined with a space.

### Global views keep product ids while products get slugs - Severity: Nit
- Affected scope: §3.1 row 2, §3.4 (`inbox?product=p-6`)
- Finding and rationale: The design's premise is that a URL should say which product it is (§8 row 3), but `?product=` keeps the id.
- Recommendation: Either accept a slug there too, or note the exception and why.

### Line and section references are slightly off - Severity: Nit
- Affected scope: §4.4 (`sisflow-board.css:89`), §10.3
- Finding and rationale: `width:280px` is at line 90. "10.3 Commands" has no 10.1 or 10.2, and §5.6 refers to §10.3.
- Recommendation: Fix the line number, and number §10's subsections or rename §10.3.

Counts: B=2 M=9 N=8
