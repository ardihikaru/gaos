<!-- Round 3, 2026-11-26. Lens: zoomout. Target: docs/designs/urls-and-board.md (rev 3). Verbatim reviewer output. -->

# Zoom-out review: `docs/designs/urls-and-board.md` (rev 3)

**Isolation note.** The checkout sits under the main repository's `.claude/worktrees/`. Reading the design there made the harness attach three rule files from the main repository's `.claude/rules/`: `agent-workspace.md`, `qa-test-plan.md` and `frontend.md`. I did not open `CLAUDE.md`, `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md` or anything under `docs/designs/reviews/`.

**Arithmetic, recomputed from the change list:**
- **Routes:** 9 global views + `/p/{ref}/{section}` + `/p/{ref}` = 11 patterns, of which 10 render a page and 1 redirects. Matches.
- **Functions allowed to call `buildBO`:** `sheet`, `handleIndex`, `handlePage`, `handleProductPage`, `handleReadiness` = 5. Matches.
- **Phase 1 tests:** I counted 10 rows in §10.1, plus 1 deploy check. TC-722..732 is 11 rows. Go tests 587 → 597. Matches.
- **Phase 2 tests:** I counted 14 rows in §10.2. TC-733..746 is 14. Go tests 597 → 611. Matches.
- **Decision log:** "Ninety-one entries" is at `pustaka/docs/product/decision-log.html:18`, so 91 → 92 → 93 holds.
- **Live refresh size:** 2 columns × 30 cards × 0.9 KB = 54 KB, under the 60 KB target. Matches.
- **Not derived:** the "≤ 4,000 elements" target has no arithmetic behind it (see the last nit).

**Reuse claims I checked, all accurate:**
- `slug` is at `store_mem.go:2462`.
- `prHref` is at `prview.go:125`; `afHref` is at `autofix_view.go:672`.
- `sitePrefix` is at `render.go:192`.
- `.af-seg` and `.af-form` are at `static/app.css:1616` and `:1624`.
- `board._store` is at `static/vendor/sisflow/board/index.js:121`. It is the only way to reach `store.load`, so the feature-check is justified.
- `Store.Events` (`store.go:70`) has no per-product form, so `EventCount` is justified.

**Sections I find solid:**
- §3.1–3.3 (routes, slug, page rendering). The slug rule reuses `slug()`, and the stored form against the derived form was an operator decision whose cost the document weighs.
- §3.5. The history-cache settings come with a spike behind them.
- §3.7. The signed-out handling is minimal and keeps BR-55.
- §5.5. Counting events instead of copying the stream fixes the real cost on every tab.

---

### The per-column `rev` hash is an optimisation the document never shows is needed, and the alternative it rejects is overstated - Severity: Major
- Affected scope: §5.1 (the `data-rev` FNV-1a hash), §4.2 (the `rev` parameter), §8 (row "Send all nine columns on every hint, with no hash"), §5.6 (the 60 KB target), §10.2 (`TestBoardLiveSendsOnlyTheColumnsThatChanged`).
- Simplicity rationale:
  - **The size is overstated.** §8 rejects re-sending all nine columns at "about 243 KB … per hint". That is an uncompressed figure. Every response already goes through `gzipMiddleware` (`dashboard/main.go:461`, `dashboard/render.go:231`), and 270 near-identical card subtrees compress very well.
  - **The server cost is already small.** Once paging lands, the server cost of a full refresh is `BoardPage` (target ≤ 3 ms) plus rendering 270 cards. The measured problem in §1.2 (109 ms, 94 MB, 1.83 MB) comes from rendering every card, copying the event stream and rendering diagrams. Paging and §5.5 remove all three.
  - **The hash adds a lot of machinery.** It brings a client-supplied `rev` map, a server-side hash over the JSON encoding of cards, and a carve-out that blanks `LeadText`/`TitleText` on running clocks. It also ties the hash to what the template prints. Any visible field that is not in `BoardCard`'s JSON, or any JSON field that changes without a visible change, silently breaks the hash. It adds `data-shown`/`data-rev` bookkeeping in `live.js` and one test of its own.
  - **The scroll goal does not need it.** Goal 5 ("keeps every scroll position") is met by per-column OOB swaps with `scrollTop` restored. That works whether 2 columns are sent or 9.
- Recommendation:
  - Ship phase 2 first with `board/live` returning all nine columns as OOB elements, keeping the per-column scroll restore.
  - Measure the gzipped size and the time of one live refresh at 2,000 cards.
  - Add `rev` only if that measurement misses a target the operator sets.
  - Restate the §8 row with the compressed size, so the rejected alternative is stated fairly.

### "Show what changed" (`is-fresh`) does not trace to any goal - Severity: Major
- Affected scope: §5.4 as a whole, the `--fresh` colour in §4.4, the sisflow `css`-field merge in §5.4, and the "marks the card" step in the §10.2 exit.
- Simplicity rationale:
  - **No goal asks for it.** Goals 1–6 (§2) cover URLs, the 30-card limit, 300px columns, filter and table, cheap live updates, and filters in the URL. None asks for a visual marker on a changed card.
  - **It costs work on both boards.** On the server board, `live.js` must snapshot `data-card` ids before each refresh swap and diff them after. On sisflow, `board.js` must diff the column of every card between documents and merge CSS classes.
  - **It adds review load.** It brings a new colour variable for both themes and a motion that the `review-animations` pass then has to approve.
  - **It lands in an oversized phase.** §11 already lists "The phase is large" as a risk, and this feature makes it larger without serving a stated goal.
- Recommendation: Either move §5.4 to the non-goals and propose it as a follow-up, or add it as an explicit goal with the operator's decision recorded in D-094. Do not ship it as a side effect of goal 5.

### The mechanism that carries board state on mutations is left undecided, and it rewrites every request URL on the tab - Severity: Major
- Affected scope: §4.5 (the "One hook keeps the server informed" paragraph, steps 1–3), §11 (row "The `htmx:configRequest` hook cannot move parameters onto the URL of a `PUT`").
- Simplicity rationale:
  - **Two designs are left open.** §11 says that if htmx does not honour a path rewrite on a non-`GET` request, "the hook sets the same values as request headers and `parseBoardQuery` reads headers first; decided at the server-half checkpoint". That leaves two implementations in the document: query-string carriage and header carriage. Two engineers would build different parsers and different tests (`TestABoardMutationKeepsTheView` asserts "whose URL carries the filter").
  - **The URL rewrite is the more invasive choice.** It applies to every `PUT`/`POST` under `products/<id>/` (§4.5 step 1). The design already uses a header for the one field it must keep out of URLs (`X-Board-Search`, §4.2).
- Recommendation: Pick headers now, for every request the hook decorates: filter, `shown`, `rev`/`rows` and search.
  - The form's own `GET` keeps its query, because that is what the address shows.
  - The hook then never edits a URL, and the body-field collision (`label`) cannot arise.
  - It removes one branch from `parseBoardQuery` and one open risk from §11.

### Phase 2 bundles four separable deliverables behind one exit - Severity: Nit
- Affected scope: §4–§5, §9 (phase 2 table), §10.2 (single exit covering both boards, table, live, motion, themes and keyboard).
- Simplicity rationale: Goals 2–3 (the limit and the width) and the filter bar can ship and be measured on their own. Goal 4's table and goal 5's live delta depend on them, but not the other way round. The single §10.2 exit means a defect in sisflow's `reload()` or in the delta hash holds up the 30-card limit, which is the change that removes the measured cost. The server-half checkpoint helps but is not a release.
- Recommendation: Split phase 2 into two pull requests:
  1. Server paging, width and filters, on both boards, with full-column refresh.
  2. The table and any live-delta work, decided after measuring part 1.

### `Href` for `prs` and `autofix` should reuse the existing encoders - Severity: Nit
- Affected scope: §3.4 (the parameter table and the "rebuilt with `url.Values.Encode()` … dropped above 128 bytes" rule) and §3.6 (`prPageHref`/`afPageHref`).
- Simplicity rationale:
  - **Two encoders for one address.** §3.4 describes a generic rebuild of the query from parsed parameters. §3.6 separately defines `prPageHref`/`afPageHref` as `prHref`/`afHref` without the leading `ui/bo/`. That is two encoders for the same address.
  - **They will disagree.** `prHref` omits `state=open` (`dashboard/prview.go:130`), but the generic rule keeps whatever the request carried. The link's `href` and `BOView.Href` can then disagree on the default state, which produces a needless `HX-Replace-Url`.
- Recommendation: State that `buildBO` sets `Href = prPageHref(filter)` and `afPageHref(filter)` for those two views, from the already-validated `PRFilter`/`AFFilter`. The generic rebuild then remains only for `inbox?product=`.

### The store method has two names - Severity: Nit
- Affected scope: §3.2 (`MemStore.AssignSlug(ctx, productID string)`) against the §9 phase-1 row for `store_mem.go`, `store_arcade.go` (`MemStore.AssignSlugs`).
- Simplicity rationale: These are one method under two names, and the plural suggests a batch signature that §3.2 does not describe.
- Recommendation: Use `AssignSlug` in §9.

### Two different length caps for the same kind of value - Severity: Nit
- Affected scope: §3.4 ("a kept value longer than 128 bytes is dropped") and §4.2 ("A value longer than 64 bytes is dropped").
- Simplicity rationale: Two constants for "a query value that ends up in an address header". Readers and tests have to track which one applies where.
- Recommendation: Use one bound for every kept query value, or say why the Board's must be tighter.

### Low-level detail in a design document - Severity: Nit
- Affected scope:
  - §4.2: "split with a limit of ten parts"
  - §5.4: the 3px `::after` bar, 150 ms fade and 1.5 s delay
  - §5.3: 250 ms and 1 s debounce windows
  - §4.5: the 300 ms input pause
- Simplicity rationale: These are implementation constants that the tests and the frontend rule's motion review will fix anyway. They lengthen an already 620-line document without changing any interface.
- Recommendation: Keep the limits that are security-relevant: 256 bytes, the 1..300 and 1..1000 clamps, 80 runes. Move the timing and visual constants to the implementation, or into a single "constants" line.

### The "≤ 4,000 elements" target has no derivation - Severity: Nit
- Affected scope: §5.6, row "Elements under `#boMain`, 2,000 cards".
- Simplicity rationale: Every other target shows its arithmetic, for example "270 cards × 0.9 KB". This one does not, so no one can tell whether a miss comes from the cards, the filter bar or the sheet.
- Recommendation: Derive it: 270 cards × the elements per card, counted from the card template at `templates/backoffice.html:406-417`, plus the columns and the bar.

Counts: B=0 M=3 N=6
