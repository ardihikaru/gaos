<!-- Round 3, 2026-11-26. Lens: comprehension. Target: docs/designs/urls-and-board.md (rev 3). Verbatim reviewer output. -->

# Comprehension review: urls-and-board.md

Reviewed `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/feat-urls/docs/designs/urls-and-board.md` (rev 3, 623 lines) against `docs/rules/gaos-design-review.md` and `docs/designs/README.md` in the same checkout.

**Isolation note.** The checkout itself sits under the main repository's `.claude/worktrees/`. Reading files there made the harness attach three rule files from the main repository's `.claude/rules/`: `agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. It did not attach `CLAUDE.md`, and I opened nothing under `.claude/` myself. No finding below relies on those attached files. If isolation matters, the next round could be run from a checkout that is not under `.claude/`.

## What this proposes

**Phase 1 (v0.55.0, D-093): real URLs.**
1. Eleven new GET routes:
   - nine page routes for the global views (`/inbox`, `/stories`, `/diagrams`, `/knowledge`, `/agents`, `/archive`, `/integrations`, `/autofix`, `/prs`);
   - `/p/{ref}/{section}` for the nine product tabs;
   - `/p/{ref}`, which returns `302` to `/p/<slug>/brd`.

   These routes always return a full HTML page with `Cache-Control: no-store`. The existing fragment routes stay as they are and are still what htmx calls.
2. A stored `Product.Slug`:
   - It is assigned once in `CreateProduct` from the existing `slug(name)`, cut to 48 bytes, made unique with `-2`, `-3` and so on, and may never look like another product's id.
   - Existing products get one at boot through `backfillSlugs`. In ArcadeDB each backfill is an ordinary mutation that emits `RecordUpdated`.
   - The slug is stored as a `slug` property on the `Product` vertex, with no schema change. It is checked when loaded and reassigned if it is malformed or a duplicate.
   - If `{ref}` is an id, the server redirects to the slug.
3. A single helper, `sheet()`, builds the sheet and calls `pushURL`. `pushURL` compares `BOView.Href` with `HX-Current-URL`:
   - a different path gets `HX-Push-Url`;
   - a GET with the same path and a different query gets `HX-Replace-Url`;
   - otherwise nothing.

   Background refreshes are marked `X-GAOS-Refresh` and never change the address. Two timing faults in `live.js` are fixed. A `bo-title` template sets the tab title.
4. htmx history cache off, and a full reload on Back and Forward.
5. Navigation buttons become `<a href=page hx-get=fragment>`. A capture-phase listener lets Ctrl-click and middle-click open a new tab.
6. A signed-out request for a page route still gets `401`, but with the HTML sign-in page instead of JSON. After sign-in on a deep URL the server answers `204` with `HX-Refresh`. `Referrer-Policy: same-origin` is added.

**Phase 2 (v0.56.0, D-094): the Board.**
1. A paged read model:
   - `BoardQuery` is the part that lives in the URL. `BoardWindow` (search text, cards shown per column, table rows) never goes in the URL.
   - New store methods `BoardPage` and `BoardCard`. They decorate only the cards they return, and Done and Canceled are sorted newest first.
2. A new route, `GET /products/{id}/board/live`, sends only the columns whose hash changed. The JSON route gains a `paged=1` form.
3. Layout: 300px columns, full width, no aside on the Board tab, nine columns always present with stable ids, at most 30 cards per column at first, "Show more" up to 300.
4. A filter bar, a count line and a Board/Table switch. A `htmx:configRequest` hook copies the filter state onto every request this tab makes; the search text travels only in the `X-Board-Search` header.
5. A server-rendered table capped at 1,000 rows.
6. sisflow gets the same features by reloading the paged JSON into its store, and its story lanes are removed.
7. Live-update cost: per-column hashes, hints batched for 250 ms to 1 s, paused while the browser tab is hidden, an `is-fresh` marker on changed cards, a cheap Timeline count, and sequence diagrams rendered only on their tab.
8. Benchmarks with numeric targets.

## How the existing system works, per this document

- Every back-office click is an htmx GET into `#sink`, answered with the whole `#boSheet` out of band. Nothing pushes history. Only `GET /{$}` returns a full page.
- `Board()` returns every card. The quadratic decorators make it slower than linear: 361 ms and 182 MB at 10,000 cards.
- Each `task` hint re-fetches the whole product tab. `buildBO` copies the entire event stream and renders every sequence diagram. The whole sheet is replaced, and only the vertical scroll is kept.
- There are two boards: sisflow, which loads only from v0.54.1, and the server-rendered fallback.
- Signed out, a non-htmx request to a back-office route gets JSON `401`. Products are never renamed, but they are sometimes deleted by hand in the database.

I checked a sample of the anchors and they hold, including:
- `main.go:499`, `:519`, `:524`, `:526`, `:537`
- `handlers_bo.go:96`, `:137`, `:148`, `:158`, `:170`, `:188`, `:474`/`:508`, `:518`, `:1028`, `:911`
- `auth.go:493`, `:502-510`, `:541-560`, `:570-573`
- `store_arcade.go:154-166`, `:533`, `:582-592`, `:763-765`, `:1493-1496`
- `fold.go:442`, `store_mem.go:325`, `:417`, `:1815`, `:2462`
- `board.go:40-44`, `:137`, `:156`
- `live.js:40-84`, `:97`, `:160-162`, `:183-186`, `:198`
- `board.js:174`, `:184`, `:203`, `:211`, `:217`, `:233-234`
- `templates/page.html:6`, `:12`, `:25`
- `auth_test.go:258`, `:290`, `:332`
- BR-02 at `brd.html:1226`
- TC-721 is present.

## Findings

### The hook puts stale form values back on the Board link and on "Clear filters" - Severity: Major
- Affected scope: §4.5 (hook step 1, `nav.af-seg`, `a.af-reset`), §4.6
- Finding: Hook step 1 adds "each field of `#boardFilters` that the request does not itself carry as a query parameter". The form has a hidden `view` input holding the current view. The **Board** link "carries only `view`" and, for the board, has no `view` at all. So from the table, clicking Board sends a request with no `view`, the hook adds the hidden `view=table`, and the operator stays on the table. "Clear filters" (`a.af-reset`) has the same problem: it carries no filter parameters, so the hook puts every current filter back and nothing is cleared. The document never says how a request asks for a field to be reset, and the exit test ("switch to the table and back") would fail. Two engineers would solve this differently.
- Recommendation: State the rule. Options include:
  - links that reset a field carry it explicitly with an empty value (`view=`, `role=`, and so on), and the hook treats a key that is present as authoritative even when empty;
  - the hook skips elements marked `data-board-reset`.

  Add a case to `TestABoardMutationKeepsTheView`, or a browser exit step, for table → board and for Clear filters.

### sisflow's `reload()` can put the search text in the URL, and the planned test cannot see it - Severity: Major
- Affected scope: §4.8 "Loading" row, §6 "What becomes visible outside the session", §10.2 `TestTheSearchTextNeverTravelsInAURL`
- Finding: `reload()` reads `#boardFilters` with `FormData` and fetches `…/board?paged=1&<filter>&shown=…`. The form contains the search box `q` (§4.5). The text never defines `<filter>`. Serialising the `FormData` straight into the URL puts `q` in the request line, which is the access-log leak §4.2 exists to prevent. The test reads `live.js` and `board.js` and asserts "the only place `q` is written is the `X-Board-Search` header". A `new URLSearchParams(formData)` writes `q` without the literal `q` ever appearing in the source, so the test would pass with the leak in place. The same `FormData` would also carry the hidden `surface=sisflow` input into the JSON request.
- Recommendation: Define `<filter>` as an explicit list of keys (`view`, `role`, `story`, `label`, `wf`, `waiting`, `empty`, `sort`, `dir`), and say that `q` and `surface` are removed before the URL is built. Make the test behavioural: send a JSON or `board/live` request with `q=` and assert the response ignores it. Better still, assert on a captured request URL in the browser exit step, which §10.2 already partly does.

### The 64-byte value cap contradicts `shown` and `rev` - Severity: Major
- Affected scope: §4.2 opening paragraph vs the `shown` and `rev` rows
- Finding: §4.2 says "A value longer than 64 bytes is dropped." It then lets `shown` and `rev` be up to 256 bytes. A full `rev` for nine columns is about 220 bytes:
  - column ids (`board.go:119-127`): 59 bytes
  - nine colons: 9 bytes
  - nine 16-digit hashes: 144 bytes
  - eight commas: 8 bytes

  If the 64-byte rule applies first, every `rev` is dropped and every refresh resends all nine columns, which defeats §5.1 silently. The 256-byte limit also does not say whether it is measured before or after percent-decoding. Encoded, the commas and colons push a full `rev` to about 254 bytes, almost exactly at the limit.
- Recommendation: Say that the 64-byte cap applies only to the `BoardQuery` keys, and that `shown` and `rev` have their own limit measured after decoding. Leave room for future column ids, or derive the limit from `len(boardColumns)`.

### Goal 6 contradicts §4.1 step 2, and the address can disagree with the screen - Severity: Major
- Affected scope: §2 goal 6; §4.1 step 2; §4.5 ("the address … keeps matching the screen"); §3.4 (a mutation never rewrites the query)
- Finding: Goal 6 says "nothing the dashboard does on its own removes [filters] from [the URL]". §4.1 step 2 says a value the product no longer offers "disappears from the address". Neither statement holds in all cases:
  - A refresh carries `X-GAOS-Refresh`, so it sends no address header.
  - A mutation (for example `POST …/label` removing the last use of a label) re-renders the sheet with the filter cleared, but by §3.4 it cannot rewrite the query.

  In both cases the address keeps `label=x` while the board shows no label filter. A reader cannot tell which of goal 6 and step 2 is the rule.
- Recommendation: Pick one behaviour and state it. Either the address keeps the stale value and a reload clears it from the screen only (and say so), or a cleared value is an allowed exception to goal 6 and the "address matches the screen" claim is qualified.

### `board/live` "follows §3.4's rule" but must never push - Severity: Nit
- Affected scope: §4.3 `board/live` row; §3.4 `pushURL` table; §10.2 `TestBoardLiveNeverPushesTheAddress`
- Finding: Under §3.4's table, a GET whose path differs from `HX-Current-URL` gets `HX-Push-Url`. §4.3 says `board/live` uses "the rule of §3.4's table" and then concludes it never pushes. The test confirms that a different tab gets no header. That requires a variant of `pushURL`, and the document does not describe one.
- Recommendation: Say that `board/live` calls `pushURL` in a replace-only mode: same path and a different query gives `HX-Replace-Url`, anything else gives nothing.

### "Sets `X-GAOS-Refresh` in the hook for that one request" has no stated mechanism - Severity: Nit
- Affected scope: §4.5 last paragraph; §5.4; §5.2
- Finding: The hook runs for every request from the form. The document does not say how it tells that one request came from a hint-triggered `refresh`, for example a flag set before `htmx.trigger` and cleared in `configRequest`, or `evt.detail.triggeringEvent.type`. sisflow's `reload()` has the same gap in §5.4: it needs to know whether it was hint-driven before it can mark `is-fresh`.
- Recommendation: Name the mechanism in one sentence, for both the hook and `board.js`.

### `AssignSlug` vs `AssignSlugs` - Severity: Nit
- Affected scope: §3.2 (`MemStore.AssignSlug(ctx, productID string)`) vs §9 phase 1 table (`MemStore.AssignSlugs`)
- Finding: The new method has two different names, one singular and one plural.
- Recommendation: Use one name in both places.

### `BOView.PageTitle` is introduced twice - Severity: Nit
- Affected scope: §3.3 ("`BOView` gains `PageTitle string`") and §3.4 ("`BOView` gains two fields … `PageTitle`")
- Finding: The field is announced twice, in two sections.
- Recommendation: Introduce it once, in §3.4, and refer back to it from §3.3.

### Project terms used without a definition - Severity: Nit
- Affected scope: §3.2 ("desk = the product id"); §5.1 ("writes it with the toast and the chrome"); §6 and §4.2 ("the edge")
- Finding: These project terms are never defined:
  - *desk*: the event's `DeskID`;
  - *the chrome*: what `RenderWithChrome` adds (`render.go:102-107`);
  - *the edge*: Traefik, which the **Terms** block only implies through "Prefix".

  The nine column ids that `shown`, `rev` and `more=` use are also never listed. `more=done` is the only example.
- Recommendation: Add these to the Terms block, with the column ids taken from `board.go:119-127`.

Sections that hold up: §1 (its claims match the code I checked), §3.1, §3.2's storage and fold claims, §3.5, §3.7, the §10 counts (10 + 1 and 14 tests, with matching TC ranges), and §12.

Counts: B=0 M=4 N=5
