# The back-office bar: scroll arrows you can see, and sub-menus that make room

**Status:** Approved (v2). The operator approved it on 2026-11-28 ("Approve, implement"), without a round 2. Round 1 reviewed v1 with five lenses: `reviews/top-bar-menu-{comprehension,zoomout,critic,readiness}-1.md` and `reviews/top-bar-menu-security-review-1.md`. The triage is in `reviews/top-bar-menu-review-1.md`. Every `PROPOSE FIX` is applied below.
**Feature:** `top-bar-menu`
**Version target:** v0.70.0, a minor release (main is v0.69.1: `pustaka/docs/assets/toc.js:22`, `dashboard/templates/page.html:48,50,51`).
**Numbers claimed:** **TC-904**, **TC-905**, **v0.70.0**. No D-, BR- or OQ- number is added, amended or answered (§7).
**BRD rules this must obey:** none of §9's rules govern the bar's layout. Every URL D-094 gave a view is kept (§4.1). The bar renders only inside `{{if .BO}}` (`dashboard/templates/backoffice.html:47`), so only a verified session sees it (BR-55); TC-904 checks that for the new markup.
**Security lens:** required (the change touches the back-office surface, `docs/rules/gaos-design-review.md`, "The security criterion"). Report: `reviews/top-bar-menu-security-review-1.md`, Critical 0, High 0, Medium 1, Low 2, all fixed in v2.

All `file:line` anchors are against `main` at `545294d` (v0.69.1).

## 0. Glossary

- **The bar**: the back office's top bar, `<header class="sheet-head bo-bar">` (`backoffice.html:52`). It is a three-zone grid: context on the left, the menu in the middle, the operator's controls on the right (`dashboard/static/app.css:756-762`). It sits inside `#boSheet` (`backoffice.html:46`), which is re-rendered as a whole by every navigation and by `live.js`'s background refreshes (§4.6).
- **The menu**: the `<nav class="bo-menu">` in the middle zone. There are two, and exactly one renders:
  - **the global menu** (`backoffice.html:88-100`), 11 links: Inbox, Products, Stories, Diagrams, Knowledge, Agents, Archive, Integrations, Traffic, Auto-fix, Pull requests;
  - **the product menu** (`backoffice.html:72-82`), 9 links, shown when a product is open: Requirements, Stories, Diagrams, Knowledge, Board, QA, Timeline, Blueprint, AI vendors.
- **A menu link**: `<a href="page" hx-get="fragment" hx-target="#sink">`. `href` is the page (new tab, copied link); `hx-get` is the fragment a plain click swaps in (D-094). `app.js:462-479` lets a modified click reach the browser.
- **A group**: new here. A button in the menu that opens a small panel of menu links (a sub-menu).
- **The popover attribute**: the HTML `popover` attribute. An element carrying it is hidden until shown; when shown it renders in the browser's *top layer*, above everything and not clipped by any ancestor's `overflow` or `mask`. A `<button popovertarget="id">` toggles it without script, and the browser exposes that button's expanded state to assistive technology itself. An `auto` popover closes on Esc and on a click outside it ("light dismiss"). The browser fires `beforetoggle` before it shows or hides the popover and `toggle` after. Every current engine has it (Chromium 114, Firefox 125, Safari 17). The dashboard already uses it for the D-098 info panels (`.info-pop`, `app.css:2016-2029`).
- **The design skills**: three vendored review skills the dashboard's frontend work is held to — `.claude/skills/emil-design-eng/` (taste and the Before / After / Why review table), `.claude/skills/review-animations/` (motion), `.claude/skills/break-ui/` (worst-case data).

## 1. Problem

### 1.1 The items past the edge cannot be reached with a mouse

The menu scrolls sideways when it does not fit (`app.css:779-783`, `overflow-x:auto`), but:

- its scrollbar is hidden (`app.css:781` `scrollbar-width:none`, `:784` `::-webkit-scrollbar{display:none}`);
- the only cue is a 36 px edge fade (`app.css:786-788`), driven by `data-more`, which `syncMenuFade` sets (`app.js:442-447`);
- a mouse wheel scrolls vertically, and a horizontal scroller ignores it. Only shift+wheel, a trackpad swipe or Tab reach the hidden items.

The middle zone is `minmax(0,1fr)` (`app.css:757`). Each item costs 22 px of padding plus its label and count (`app.css:791-794`). The context zone's text is capped at `clamp(160px,20vw,300px)` (`app.css:763`), 256 px at 1280 px. *Assumption A1* (an estimate from the CSS, measured in §6.3 step 1): at 1280 px about 715 px is left for the menu, and the 11 global items need about 1 000 px, so the last three or four (Integrations onward) are cut off.

### 1.2 There is no room for the next page

Recent releases each added a top-level item (Integrations, Auto-fix, Pull requests, and Traffic in v0.69.0). A flat list only grows sideways.

## 2. Goal

1. A mouse user can see that the menu has more, and reach it, without a keyboard modifier.
2. The global menu shows 6 entries instead of 11 and fits at 1280 px. The product menu shows 8 instead of 9; it may still overflow at 1280 px with a long product name, and the arrows cover that case. §6.3 step 1 measures both; a global menu that still overflows at 1280 px is reported in the pull request and does not block the release, because goal 1 still holds.
3. Every view keeps its URL, its link, its keyboard reach and its "current" mark.

## 3. Non-goals

- No route, handler, view or view-model field changes. `buildBO` is untouched.
- No change to the needs chip, the theme button, Sign out, the context zone or the login sheet.
- No change to any view's content.
- No new colour, font or CSS variable.
- No reordering of views beyond the grouping in §4.1.
- No change to the 900 px two-row layout (`app.css:1012-1023`) beyond moving one rule from `.bo-menu` to its new wrapper (§4.3).
- No arrow-key menu model inside a panel (§4.2 says why).
- No deploy. The release is tagged; deploying it is a separate step.

## 4. Design

### 4.1 The grouping

Entries are grouped by the job the operator is doing. The operator chose this grouping, including the product menu's Setup group, in the request that started this change (2026-11-28); the approval of this design confirms it. Top-level entries stay links; a group is a button.

**Global menu** (6 entries, was 11):

| Entry | Kind | Children (in this order) | Trigger is current when `.BO.View` is |
|---|---|---|---|
| Inbox | link | — | — |
| Products | link | — | — |
| **Library** | group | Stories, Diagrams, Knowledge, Archive | `stories`, `diagrams`, `knowledge`, `archive` |
| Agents | link | — | — |
| **Automation** | group | Auto-fix, Pull requests | `autofix`, `prs` |
| **System** | group | Integrations, Traffic | `integrations`, `traffic` |

**Product menu** (8 entries, was 9):

| Entry | Kind | Children | Trigger is current when `.Tab` is |
|---|---|---|---|
| Requirements, Stories, Diagrams, Knowledge, Board, QA, Timeline | links | — | — |
| **Setup** | group | Blueprint, AI vendors | `blueprint`, `vendors` |

Arithmetic: global 2 + 1 + 1 + 1 + 1 = 6 entries carrying 2 + 4 + 1 + 2 + 2 = 11 links. Product 7 + 1 = 8 entries carrying 7 + 2 = 9 links. No link is added or dropped.

Every child link is **the existing `<a>` element moved byte for byte**: same `href`, `hx-get`, `hx-target`, `hx-swap`, `aria-current` and the same whitespace between attributes. Two existing tests depend on that: `autofix_view_test.go:143` (`<a aria-current="page"   href="autofix" hx-get="ui/bo/autofix"`) and `prview_test.go:80` (`>Pull requests</a>`). The live ids stay where they are: `navNeedsCount` on the Inbox link (`backoffice.html:89`) and `navTimelineN` on the Timeline link (`:79`), both top-level. In product mode the hidden `<span id="navNeedsCount" hidden>` (`backoffice.html:86`) stays a direct child of the `<header>`, after the new `.bo-nav`, exactly where it is today; `chrome.html:95` targets it.

### 4.2 Markup (`dashboard/templates/backoffice.html`)

Both `<nav class="bo-menu">` blocks (`:72-82` and `:88-100`) are wrapped the same way. The global one becomes:

```html
<div class="bo-nav">
  <span class="bo-step" data-dir="-1" aria-hidden="true">‹</span>
  <nav class="bo-menu" aria-label="Back office">
    <a …Inbox, unchanged…</a>
    <a …Products, unchanged…</a>
    <button type="button" class="bo-grp{{if or (eq .BO.View "stories") (eq .BO.View "diagrams") (eq .BO.View "knowledge") (eq .BO.View "archive")}} is-current{{end}}" popovertarget="boGrpLibrary">Library<svg class="bo-chev" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 3.5 5 6.5 8 3.5"/></svg></button>
    <a …Agents, unchanged…</a>
    <button type="button" class="bo-grp{{if or (eq .BO.View "autofix") (eq .BO.View "prs")}} is-current{{end}}" popovertarget="boGrpAutomation">Automation<svg …same…/></button>
    <button type="button" class="bo-grp{{if or (eq .BO.View "integrations") (eq .BO.View "traffic")}} is-current{{end}}" popovertarget="boGrpSystem">System<svg …same…/></button>
  </nav>
  <span class="bo-step" data-dir="1" aria-hidden="true">›</span>
  <div class="bo-pop" id="boGrpLibrary" popover="auto" role="group" aria-label="Library">
    <a …Stories, unchanged…</a><a …Diagrams…</a><a …Knowledge…</a><a …Archive…</a>
  </div>
  <div class="bo-pop" id="boGrpAutomation" popover="auto" role="group" aria-label="Automation">
    <a …Auto-fix…</a><a …Pull requests…</a>
  </div>
  <div class="bo-pop" id="boGrpSystem" popover="auto" role="group" aria-label="System">
    <a …Integrations…</a><a …Traffic…</a>
  </div>
</div>
```

The product one is the same shape: `aria-label="Product sections"` on the `<nav>`, the seven flat links, then `<button … class="bo-grp{{if or (eq .Tab "blueprint") (eq .Tab "vendors")}} is-current{{end}}" popovertarget="boGrpSetup">Setup<svg/></button>`, and one panel `id="boGrpSetup" … aria-label="Setup"` holding the Blueprint and AI vendors links.

Decisions, each one fixed:

- **The class attribute is written as `class="bo-grp"` or `class="bo-grp is-current"`, immediately followed by ` popovertarget="…"`.** TC-904/905 match on that exact shape.
- **The panels sit outside the `<nav>`**, as siblings in `.bo-nav`. In the top layer they are not clipped either way, but outside the scroller they do not count toward the menu's `scrollWidth`.
- **Panel ids**: `boGrpLibrary`, `boGrpAutomation`, `boGrpSystem`, `boGrpSetup`. Only one menu renders, so they are unique in the page.
- **Every panel stays inside the `{{if .BO}}` block of `bo-sheet`** (`backoffice.html:47`), never at page level. TC-904's signed-out case checks it.
- **The arrows are `<span aria-hidden="true">`**, not buttons. They are a mouse affordance: a keyboard or screen-reader user reaches every entry by Tab, and a span takes no focus, so no focus can land inside an `aria-hidden` subtree.
- **No `role="menu"`, no `aria-haspopup`, no `aria-expanded` in the markup.** The children are links that navigate, so the panel is a labelled `role="group"` of links (a disclosure), not an ARIA menu, which would oblige the arrow-key model and hide them from the links list. The browser derives the trigger's expanded state from `popovertarget`, so writing `aria-expanded` would only override it — and stay wrong with script off.
- **Keyboard**: the browser puts an invoked popover's contents right after its invoker in the Tab order. Tab reaches the trigger; Enter or Space opens the panel; the next Tab enters it; Esc closes it and returns focus to the trigger. The only key handling the script adds is closing the panel when Tab leaves it (§4.4 item 5).
- **The current child keeps `aria-current="page"`** (unchanged); its trigger gets the class `is-current`. A button does not get `aria-current`.
- **With JavaScript off**, `popovertarget` still opens and closes a panel. The script supplies the position (§4.4 item 4); without it the panel keeps the browser's default popover position, centred in the viewport, because §4.3 does not override `inset` or `margin` in CSS. The dashboard needs JavaScript for htmx anyway.

### 4.3 CSS (`dashboard/static/app.css`, the bar section `:749-812` and the 900 px block `:1012-1023`)

- **`.bo-nav`** takes the grid area: `grid-area:menu;position:relative;display:flex;align-items:stretch;min-width:0`. `.bo-menu` loses `grid-area:menu` (`:780`) and gains `flex:1 1 auto`. In the 900 px block, `.bo-menu{height:42px;padding:0 6px;box-shadow:inset 0 1px 0 var(--line-soft)}` (`:1021`) becomes `.bo-nav{…}` with the same declarations.
- **`.bo-step`**: `position:absolute;top:0;bottom:0;z-index:1;width:28px;display:none;place-items:center;cursor:pointer;user-select:none;background:var(--panel);color:var(--ink-3);font-size:16px`; `[data-dir="-1"]{left:0}`, `[data-dir="1"]{right:0}`. Shown with `display:grid` by `.bo-nav[data-more="start"] .bo-step[data-dir="-1"]`, `.bo-nav[data-more="end"] .bo-step[data-dir="1"]`, and both under `.bo-nav[data-more="both"]`. It covers the faded 36 px, which the existing mask already empties, so it hides no readable label. Hover `color:var(--ink)` inside the existing `@media (hover:hover) and (pointer:fine)` block (`:806-810`). No transition: it appears with the content it points at.
- **`.bo-grp`** copies `.bo-menu a` (`:791-795`) — `flex:0 0 auto;display:inline-flex;align-items:center;gap:6px;padding:0 11px;font-size:13px;color:var(--ink-3);white-space:nowrap;cursor:pointer;transition:color 150ms ease` — plus `background:none;border:0;font-family:inherit`. Focus: the same inset ring rule as `:797`. `.bo-grp.is-current{color:var(--ink);box-shadow:inset 0 -2px 0 var(--brass)}` (as `:798`). `.bo-grp.is-open{color:var(--ink)}`. Hover `color:var(--ink-2)` in the hover block.
- **`.bo-chev`**: `width:10px;height:10px;fill:none;stroke:currentColor;stroke-width:1.5;transition:transform 150ms var(--ease-out)`; `.bo-grp.is-open .bo-chev{transform:rotate(180deg)}`. `is-open` is set by the script (§4.4 item 4), so with script off the chevron does not turn; nothing else depends on it.
- **`.bo-pop`** uses the `.info-pop` surface and motion (`app.css:2016-2024`, D-098), so the console has one popover look:
  ```css
  .bo-pop{
    min-width:200px;max-width:min(320px,calc(100vw - 16px));padding:4px;
    background:var(--panel);color:var(--ink);border:1px solid var(--line);border-radius:var(--r-s);
    box-shadow:0 12px 32px rgba(0,0,0,.35);
    opacity:0;transform:scale(.95);
    transition:opacity 150ms ease-out,transform 150ms ease-out;
  }
  .bo-pop:popover-open{opacity:1;transform:scale(1)}
  @starting-style{.bo-pop:popover-open{opacity:0;transform:scale(.95)}}
  @media (prefers-reduced-motion:reduce){.bo-pop{transition:none}}
  ```
  Like `.info-pop`, it closes instantly (no exit transition): closing is the faster half, and the bar is used tens of times a day. `transform-origin` is set by the script to the trigger's corner (§4.4 item 4).
- **The rows**: `.bo-pop a{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:var(--r-s);font-size:13px;color:var(--ink-2);text-decoration:none;white-space:nowrap}`; `.bo-pop a .n{margin-left:auto}` plus the menu's mono count style (`:800-803`, extended to `.bo-pop .n`); `.bo-pop a[aria-current="page"]{color:var(--ink);box-shadow:inset 2px 0 0 var(--brass)}`; `.bo-pop a:focus-visible{outline-offset:-2px}`; hover `background:var(--panel-2);color:var(--ink)` in the hover block. **No truncation**: the labels are fixed English words, and the widest row, "Pull requests" with a five-digit count, is under 200 px, so the panel grows to its widest row within the 320 px cap.

### 4.4 Script (`dashboard/static/app.js`, the bar block `:437-460`)

**Scope.** Nothing is matched by class across the whole document. One function, `bindBar(nav)`, attaches every listener to the elements of the one `.bo-nav` on the page, and runs from `syncMenu` (which already runs after every swap, `app.js:501-502`). It marks the element with `nav.dataset.bound = "1"` and returns early when the mark is there, so a re-run does not double-bind; a re-rendered bar is a new element, so it is bound afresh. View content cannot imitate the bar anyway: rendered markdown is escaped and emits tags from a fixed list with no `class`, `id` or `popover` (`dashboard/markdown.go:12-21`).

1. **`syncMenuFade(menu)`** (`:442-447`) writes the same `data-more` value to `menu.parentNode` when that has the class `bo-nav`, and removes it there when it removes it from the menu.
2. **Arrows**: a `click` listener on each `.bo-step`: `menu.scrollBy({left: dir * Math.round(menu.clientWidth * 0.7), behavior: reduce ? "instant" : "smooth"})`, where `dir` is the span's `data-dir` and `reduce` is `matchMedia("(prefers-reduced-motion: reduce)").matches`. The existing scroll listener (`:457-459`) updates the fade and the arrows. The "no smooth scroll" note at `:440-441` is about switching tabs, which stays instant; an arrow is a deliberate paging gesture.
3. **Wheel**: one `wheel` listener on the `.bo-nav` element, `{passive:false}` — so only this 42–64 px strip opts out of passive scrolling; the rest of the page keeps it. It acts when the menu overflows (`menu.scrollWidth > menu.clientWidth + 1`), `!e.ctrlKey` (pinch zoom), and `Math.abs(e.deltaY) > Math.abs(e.deltaX)`: `e.preventDefault()`, then `menu.scrollLeft += e.deltaY * k` with `k` = 1 for `deltaMode` 0 (pixels), 16 for 1 (lines), `menu.clientWidth` for 2 (pages). Because it sits on `.bo-nav`, wheeling over an arrow scrolls too. At either end of the scroll it still prevents the default; the bar never scrolls vertically, so nothing is lost.
4. **Opening a panel**: a `beforetoggle` listener on each `.bo-pop`. On `e.newState === "open"`, before the panel is shown and so before its first frame:
   - its trigger is the `.bo-grp` in the same `.bo-nav` whose `getAttribute("popovertarget") === panel.id` (compared, never built into a selector);
   - `r = trigger.getBoundingClientRect()`; `W = innerWidth`. The panel's style gets `inset:auto;margin:0;top:(r.bottom + 4)px`, then:
     - if `r.left + r.width / 2 <= W / 2`: `left = max(8, r.left)`, `right:auto`, `maxWidth = min(320, W - left - 8)`, `transformOrigin = "top left"`;
     - else: `right = max(8, W - r.right)`, `left:auto`, `maxWidth = min(320, W - right - 8)`, `transformOrigin = "top right"`;
   - the trigger gets the class `is-open`.

   Nothing is measured on the panel, so nothing waits for it to be shown, and the first painted frame is already in place. On `newState === "closed"`: the trigger loses `is-open`, and `document.dispatchEvent(new Event("gaos:menu-closed"))` (§4.6).
5. **Tab out**: a `keydown` listener on each `.bo-pop`: on `Tab` without Shift from the last link, or Shift+Tab from the first link, call `panel.hidePopover()` and let the key's default focus move happen. No `focusout` rule: light dismiss covers an outside click, Esc covers the keyboard, and a `focusout` close would fight the trigger's own toggle (clicking the trigger would close then reopen) and drop a Safari click on a child.
6. **A stale position**: an open panel is closed with `hidePopover()` on a `scroll` of its menu (a listener on the menu in `bindBar`), on `resize` (added to the existing `resize` handler, `:460`), and on `htmx:beforeRequest` whose `e.target` is inside the panel (a listener on the panel). The last one closes the panel the moment a child is clicked, before the response arrives.
7. **`syncMenu`** (`:448-456`) looks for `menu.querySelector('[aria-current="page"], .bo-grp.is-current')`: when the current view is a grouped child, the child is in a panel outside the `<nav>`, so the menu scrolls its trigger into view instead. It then calls `bindBar(menu.parentNode)` when that is a `.bo-nav`.

Not changed, and noted so it is not counted as new: the page shortcut handler (`app.js:106-113`) toggles the theme on `t` and ignores inputs, not links, so `t` on a focused panel link toggles the theme, as it does on any focused menu link today.

### 4.5 What is reused

- `syncMenuFade`, `syncMenu` and the `resize` handler (`app.js:442-460`) — extended, not replaced.
- The D-094 modified-click handler (`app.js:462-479`) works unchanged on the moved links: it matches `a[href][hx-get]` wherever they are.
- The `.info-pop` surface and motion (`app.css:2016-2029`).
- The `gaos:modal-closed` pattern in `live.js` (`:356`) — the new `gaos:menu-closed` event is the same idea.

### 4.6 Live refreshes and an open panel (`dashboard/static/live.js`)

`live.js` re-renders the whole sheet in the background: `keepPlace` (`live.js:66-76`) re-fetches the current pane through `htmx.ajax`, the response re-renders `#boSheet` (oob, `backoffice.html:46`), and the bar with it. It does so for an Inbox hint and for the Board (`live.js:104-110`, `:251`). An open panel would vanish under the pointer, and focus inside it would fall to `<body>` (`keepPlace` restores focus by `id`, and panel links have none). The guards that hold a refresh back are `busy()` (`live.js:60-64`) and `boardBusy()` (`:206-210`). So:

- `busy()` and `boardBusy()` each return `true` when `document.querySelector("#boSheet .bo-pop:popover-open")` matches;
- `live.js` adds `document.addEventListener("gaos:menu-closed", function () { runDeferred(); boardCatchUp(); })`, so a refresh held back while the panel was open runs as soon as it closes.

The bar's two oob count targets (`navNeedsCount`, `navTimelineN`) are on top-level links and do not re-render a panel.

## 5. Change list

Three phases. Each ends on a green command; a phase does not start until the previous one exits.

### P1 — markup and tests. Exit: `(cd dashboard && go vet ./... && go test ./...)` green, TC-904 and TC-905 included.

| # | File | Change |
|---|---|---|
| 1 | `dashboard/templates/backoffice.html` | §4.2. |
| 2 | `dashboard/bo_menu_test.go` (new) | `TestBoMenuGroupsGlobal` (TC-904), `TestBoMenuGroupsProduct` (TC-905), exactly as §6.1. |

### P2 — CSS and script. Exit: P1's command still green, and every step of §6.3 passes.

| # | File | Change |
|---|---|---|
| 3 | `dashboard/static/app.css` | §4.3. |
| 4 | `dashboard/static/app.js` | §4.4. |
| 5 | `dashboard/static/live.js` | §4.6. |

### P3 — registers and release. Exit: every command in §6.2 green.

| # | File | Change |
|---|---|---|
| 6 | `dashboard/templates/page.html` | `?v=0.69.1` → `?v=0.70.0` on lines 48, 50, 51. `TestStaticAssetsAreVersionedByTheProductVersion` (`external_inbox_test.go:893`) ties these to row 7. |
| 7 | `pustaka/docs/assets/toc.js` | `semver: "0.69.1"` → `"0.70.0"` (`:22`). |
| 8 | `pustaka/docs/changelog.html` | a new first `<section class="rel rv">` in `.clog`, text in §5.1. |
| 9 | `pustaka/docs/product/qa-test-plan.html` | the two register rows and the `h3` in §5.2; "nine hundred and three" → "nine hundred and five" at `:19` and `:79`. |
| 10 | `pustaka/docs/assets/toc/product.js` | `:162` and `:168`: "nine hundred and three" → "nine hundred and five"; `:168`: "seven hundred and sixty-three" → "seven hundred and sixty-five". |
| 11 | `deploy/check-workspace.sh` | `:114`, `:118`: "seven hundred and sixty-three" → "seven hundred and sixty-five"; `:115`: `"763"` → `"765"`; `:129`, `:130`: "nine hundred and three" → "nine hundred and five". |
| 12 | `README.md` | `:12` "Product version 0.69.1" → "0.70.0"; `:143` "763 Go tests: 738 in `dashboard/`" → "765 Go tests: 740 in `dashboard/`"; `:145` "903 registered test cases" → "905". (`check-workspace.sh:147-149` compares these with what it computes.) |
| 13 | `.claude/rules/qa-test-plan.md` | highest `TC-905`, next free `TC-906`; the `dashboard (go test)` Implemented range gains `TC-904, TC-905`; "nine hundred and three" → "nine hundred and five"; "903 rows in total. 738 in `dashboard/` and 25 in `pustaka/`" → "905 … 740 …"; "763 Go tests" → "765"; "seven hundred and sixty-three" → "seven hundred and sixty-five". |

Arithmetic: 903 + 2 = 905 register rows; 738 + 2 = 740 dashboard tests; 763 + 2 = 765 Go tests. Thirteen files: 12 changed, 1 new. Completeness check, run in P3 before the exit command: `git grep -nE "nine hundred and three|seven hundred and sixty-three|\b(903|763|738)\b|0\.69\.1"` must show only `pustaka/docs/changelog.html` (older release notes stay as written), `docs/designs/` and `docs/designs/reviews/`.

### 5.1 The changelog entry

```html
<section class="rel rv">
  <h3 id="v0-70-0">v0.70.0 <time datetime="YYYY-MM-DD">YYYY-MM-DD</time> <span class="bump minor">minor</span></h3>
  <ul>
    <li><span class="chg add">Added</span> <strong>Sub-menus in the back-office bar.</strong> Stories, Diagrams, Knowledge and Archive are under <strong>Library</strong>; Auto-fix and Pull requests under <strong>Automation</strong>; Integrations and Traffic under <strong>System</strong>; in a product, Blueprint and AI vendors under <strong>Setup</strong>. Every page keeps its address, and the group of the page you are on is underlined.</li>
    <li><span class="chg add">Added</span> <strong>Scroll arrows</strong> on the bar when it does not fit, and the mouse wheel scrolls it sideways.</li>
    <li><span class="chg change">Changed</span> A background refresh waits while a sub-menu is open. Two new test cases, TC-904 and TC-905.</li>
  </ul>
</section>
```

`YYYY-MM-DD` is the release date, filled in when P3 is committed.

### 5.2 The register rows and the scenario

Rows, appended after TC-903 in `<tbody id="tc-index">`:

```html
<tr data-tags="dashboard go test back office bar menu sub-menu group popover library automation system current signed out positive negative implemented"><td>TC-904</td><td>dashboard (go test)</td><td>Positive</td><td>The global back-office bar groups Stories, Diagrams, Knowledge and Archive under Library, Auto-fix and Pull requests under Automation, and Integrations and Traffic under System; the group of the current view is marked, every view&rsquo;s link is present once, and a signed-out page carries no group.</td><td>Implemented</td></tr>
<tr data-tags="dashboard go test back office bar menu sub-menu group popover setup blueprint vendors product current positive negative implemented"><td>TC-905</td><td>dashboard (go test)</td><td>Positive</td><td>A product&rsquo;s bar keeps seven sections flat and puts Blueprint and AI vendors under Setup, which is marked only when one of the two is open; the global groups are absent.</td><td>Implemented</td></tr>
```

Scenario: a new `<h3 id="go-bo-menu">dashboard &mdash; the back-office bar&rsquo;s groups (TC-904, TC-905)</h3>` after the `#go-traffic` section and before `<h2 id="not-implemented">` (`qa-test-plan.html:4793`). One intro paragraph naming this design, then one `<p><strong>TC-90n, positive and negative</strong> (<code>dashboard/bo_menu_test.go</code>, <code>TestBoMenu…</code>): …</p>` per case, quoting each message of §6.1 verbatim.

## 6. Verification

### 6.1 The tests (`dashboard/bo_menu_test.go`)

Both use `srv, _ := newTestServer(t)` (`main_test.go:27`) and `do(t, srv, "GET", path, nil, boCookies()...)` (`main_test.go:64`, `:85`). Helpers in the file:

- `trigger(id) string` = `popovertarget="` + id + `"`;
- `isCurrent(body, id) bool` = `strings.Contains(body, `class="bo-grp is-current" popovertarget="`+id+`"`)`;
- `panel(body, id) string` = the substring from `id="`+id+`" popover="auto"` to the first `</div>` after it, or `""`.

**TC-904 `TestBoMenuGroupsGlobal`.**

1. `GET /ui/bo/stories` with cookies. If the code is not 200: `t.Fatalf("GET %s answered %d", path, code)`.
2. For each id of `boGrpLibrary`, `boGrpAutomation`, `boGrpSystem`:
   - `strings.Count(body, trigger(id)) != 1` → `t.Errorf("the bar has %d triggers for %s, want 1", n, id)`;
   - `panel(body, id) == ""` → `t.Errorf("the bar has no %s panel", id)`;
   - `isCurrent(body, id) != (id == "boGrpLibrary")` → `t.Errorf("on %s the %s trigger is current=%v, want %v", path, id, got, want)`.
3. `!strings.Contains(panel(body, "boGrpLibrary"), `<a aria-current="page"   href="stories"`)` → `t.Errorf("the Library panel does not hold the current Stories link")`.
4. For each of these 11 literals, `strings.Count(body, lit) != 1` → `t.Errorf("the bar carries %q %d times, want once", lit, n)`:
   `href="inbox" hx-get="ui/bo/inbox"`, `href="./" hx-get="ui/bo/products"`, `href="stories" hx-get="ui/bo/stories"`, `href="diagrams" hx-get="ui/bo/diagrams"`, `href="knowledge" hx-get="ui/bo/knowledge"`, `href="agents" hx-get="ui/bo/agents"`, `href="archive" hx-get="ui/bo/archive"`, `href="integrations" hx-get="ui/bo/integrations"`, `href="traffic" hx-get="ui/bo/traffic"`, `href="autofix" hx-get="ui/bo/autofix"`, `href="prs" hx-get="ui/bo/prs"`.
5. `GET /ui/bo/inbox` with cookies (same `Fatalf` on a non-200): for each of the three ids, `isCurrent` must be false, with the step-2 message.
6. The negative: `GET /stories` **without** cookies, whatever it answers (a signed-out request is refused with 401, BR-55): `strings.Contains(body, "boGrp")` → `t.Errorf("a signed-out page carries the bar's groups")`.

**TC-905 `TestBoMenuGroupsProduct`.**

1. `GET /products/p-1?tab=blueprint` with cookies; same `Fatalf`.
2. For `boGrpSetup`: the step-2 checks of TC-904 with the same messages; it must be current.
3. `p := panel(body, "boGrpSetup")`. `!(strings.Contains(p, `aria-current="page"`) && strings.Contains(p, `hx-get="products/p-1?tab=blueprint"`))` → `t.Errorf("the Setup panel does not hold the current Blueprint link")`. `!strings.Contains(p, `hx-get="products/p-1?tab=vendors"`)` → `t.Errorf("the Setup panel does not hold the AI vendors link")`.
4. For each tab of `brd`, `stories`, `diagrams`, `knowledge`, `board`, `qa`, `timeline`, with `lit` = `hx-get="products/p-1?tab=` + tab + `"`: `strings.Count(body, lit) != 1` → `t.Errorf("the bar carries %q %d times, want once", lit, n)`; `strings.Contains(p, lit)` → `t.Errorf("the flat tab %q is inside the Setup panel", tab)`.
5. `strings.Contains(body, trigger("boGrpLibrary"))` → `t.Errorf("a product's bar carries the global groups")`.
6. `GET /products/p-1?tab=board` with cookies: `isCurrent(body, "boGrpSetup")` must be false, step-2 message.

The existing assertions at `autofix_view_test.go:143` and `prview_test.go:80` pass unchanged; that is the check that the child links moved byte for byte.

### 6.2 The commands

```bash
(cd dashboard && go vet ./... && go test ./...)
(cd pustaka && go test ./... && go run . check ./docs)
./deploy/check-stacks.sh && ./deploy/check-workspace.sh
```

### 6.3 By hand, in a browser

Run the dashboard locally; check at 1440, 1280, 1024, 900 and 390 px, light and dark, in Chromium and Firefox; Safari where available.

1. **A1**: on `main`, at 1280 px, which global items are cut off; on this branch, whether the global menu fits, and whether the product menu fits with a 40-character product name. Record the numbers in the pull request (goal 2).
2. **Arrows and wheel**: at a width where the menu overflows, an arrow appears only on the side with more; clicking it pages by 70 %; the wheel over the menu or an arrow scrolls it sideways; with the menu not overflowing, the wheel does nothing to it.
3. **Panels**: each opens under its trigger with no jump on its first frame; it is not clipped by the fade; a trigger in the right half opens right-aligned; at 390 px it stays inside the viewport; Esc, an outside click and choosing a child close it; Esc returns focus to the trigger; clicking the open panel's trigger closes it and it stays closed.
4. **Keyboard**: Tab reaches each trigger; Enter opens it; Tab enters the panel; Tab past the last link (and Shift+Tab before the first) closes it.
5. **Current**: the trigger is underlined and the child marked, on a page load (`/stories`, `/p/<slug>/blueprint`) and after an htmx click.
6. **D-094**: Ctrl/Cmd-click and middle-click on a child open a new tab; a plain click on a child in Safari navigates.
7. **Live**: the Inbox and Timeline counts still update. With the Inbox open, open a group, then make an agent ask a question: the panel stays open; when it is closed, the Inbox refreshes.
8. **Edge cases** (the break-ui skill): a count of 12 345 on a child; a 40-character tenant and product name in the context zone.

The findings of the break-ui pass and of a review-animations pass on the panel are reported as the Before / After / Why table the emil-design-eng skill defines.

## 7. Registers

- **No decision.** The grouping is a presentation choice inside D-094's contract (every view keeps its URL) and reversible in one template edit. Settled: no D-number.
- **No BR.** Nothing in the BRD's §9 governs navigation layout.
- **No OQ** is opened or answered.

## 8. Alternatives

1. **Arrows only, no groups.** Fixes §1.1 but not §1.2: each new page lengthens a strip the operator pages through. Rejected; the operator asked for both.
2. **Wrap the menu onto two lines.** The bar has a fixed height (`--topbar-h`, `app.css:77`), and the current item's underline sits on the bar's bottom rule (`app.css:750-755`). Rejected.
3. **A "More ▾" filled at runtime** (measure, move what does not fit). Which items show changes with the window, so muscle memory never forms, and the server-rendered bar would rearrange after load. Rejected for fixed, named groups.
4. **One "More ▾" with every secondary item.** Becomes a junk drawer. Rejected.
5. **A custom dropdown inside the scroller.** The scroller's `overflow-x:auto` and `mask-image` clip it; it would need moving to `body` by script and its own Esc and outside-click handling. `popover` gives the top layer, Esc and light dismiss natively. Rejected.
6. **CSS anchor positioning.** Not in Firefox's stable release as of this writing (*assumption A2*). Rejected for now; §4.4 item 4 can be replaced by it later.
7. **Drop the wheel handler and ship arrows only** (zoom-out round 1). The request was "able to scroll left-right"; on a mouse, the wheel is the gesture people try first. Kept, scoped to the bar's own element (§4.4 item 3).

## 9. Risks

| Risk | Mitigation |
|---|---|
| A browser without `popover` (pre-2023) | The trigger does nothing; every child stays reachable by URL. The console targets current browsers. Accepted. |
| A child link loses a byte and the htmx click or a test breaks | The links are moved, not retyped; TC-904/905 and the two existing tests catch drift. |
| The wheel handler traps a vertical scroll the page wanted | It is bound only to the bar, which never scrolls vertically. |
| A grouped view the operator uses daily costs one more click | Inbox, Products and Agents stay top-level; the grouping is one template edit to change. |
| A background refresh is delayed while a panel is open | Only until the panel closes (§4.6); the panel closes on any click outside it. |
