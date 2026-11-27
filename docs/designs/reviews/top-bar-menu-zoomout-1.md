# Zoom-out review: `docs/designs/top-bar-menu.md`

**What the design does:** the back-office top bar has more menu items than fit in its width. The scrollbar is hidden and a mouse wheel does not scroll it sideways. The design adds scroll arrows and a wheel handler, and puts 7 of the 20 links into 4 `popover` groups (Library, Automation, System, Setup). It adds no route, handler or view-model change.

**What holds up:**
- **Root problem:** the design is aimed at the cause, not a symptom. §1.2 (each release adds a top-level page) is real.
- **The rejected option "a More menu filled at runtime"** (§8.3) is stated fairly.
- **Arithmetic:** 6 entries carry 11 links, and 8 entries carry 9. The counts 903+2=905, 738+2=740 and 763+2=765 match the current figures in the QA rule file. One `func Test…` per TC is respected.
- **Tests:** the byte-exact assertions exist as claimed, at `dashboard/autofix_view_test.go:143` and `dashboard/prview_test.go:80`. `navTimelineN` is also asserted at `dashboard/board_page_test.go:636,1054`, and the design keeps it on a top-level link.
- **Choice of `popover`:** using the native top layer instead of a custom dropdown (§8.5) is the right primitive.

### A wheel handler on the whole document, for a goal the arrows already meet - Severity: Major
- Affected scope: §4.4 item 3; §2 goal 1; §9 row 3.
- Simplicity rationale:
  - Goal 1 is "reach it without a keyboard modifier". The arrows (§4.4 item 2) already meet it, so the wheel handler is a third mechanism for the same goal.
  - As written, it is a `{passive:false}` capture `wheel` listener on `document`. A non-passive wheel listener at the document level makes the browser wait for the main thread on every wheel scroll anywhere in the page, including the long `.sheet-body` lists. §9 only looks at whether the handler calls `preventDefault()`, not this cost.
  - The stated reason for delegating, that the bar is re-rendered, is already handled. `syncMenu()` runs after every swap (`dashboard/static/app.js:501-502,534`), so the bar can be re-bound there.
- Recommendation: drop the wheel handler and ship the arrows. If the operator still wants wheel-scrolling after A1 is measured, bind a listener to the `.bo-menu` element itself inside `syncMenu()`, not on `document`.

### The design rejects menu semantics but still builds the menu keyboard model - Severity: Major
- Affected scope: §4.4 item 4 (the third bullet), item 5; §4.2 "No ARIA `role="menu"`".
- Simplicity rationale:
  - §4.2 says the panel is "a labelled group of links", not a menu. §4.4 then adds the menu keyboard behaviour anyway: ArrowUp/Down with wrapping, Home/End, and auto-focus into the panel.
  - The auto-focus needs to guess whether a keyboard opened the panel, by remembering the last `keydown` before a `toggle` event. That is fragile: the `toggle` event fires after the click that `popovertarget` handles, and the result depends on event order.
  - A disclosure of links only needs Tab. The HTML popover focus order already places an invoked popover's contents right after its invoker in the Tab sequence. The `focusout` → `hidePopover()` close is the only keyboard code this pattern needs.
- Recommendation: delete item 5 and the keyboard-open focus bullet. Keep Tab, Esc (built in), and the `focusout` close. Change §6.3 step 4 to match: Tab into the panel, Tab out closes it.

### Goal 2 says the product menu fits at 1280 px, but nothing checks it - Severity: Major
- Affected scope: §2 goal 2; §4.1 product table (the Setup group); §6.3 step 1.
- Simplicity rationale:
  - My estimate from the CSS (`app.css:757,763,791-794`; about 7 px per character at 13 px, 22 px padding, about 26 px per count):
    - **Product menu after the change:** about 700 px for 8 entries.
    - **Middle zone at 1280 px:** about 715 px, after a context zone of up to `clamp(160px,20vw,300px)` ≈ 256 px plus the brand mark, the controls, two 18 px column gaps and 32 px of padding.
  - So the Setup group saves only about 120 px, and whether the menu fits at 1280 px depends on the product's name length and on the counts. §6.3 step 1 only measures "whether the global menu fits", so half of goal 2 cannot be signed off.
  - Setup also costs two of the eight product sections an extra click and a fourth popover, all to save one slot. The arrows already make every product tab reachable.
- Recommendation: either extend §6.3 step 1 to measure the product menu at 1280 px with the longest name (with a stated pass/fail), or drop the Setup group and narrow goal 2 to the global menu. Dropping it removes TC-905's popover half, one panel and one id.

### The existing info popover is not reused or even mentioned - Severity: Nit
- Affected scope: §4.3 `.bo-pop` and Motion; §0 "The popover attribute".
- Simplicity rationale:
  - The dashboard already ships a popover component: `.info-pop` (D-098), CSS at `dashboard/static/app.css:2016-2029`, markup at `dashboard/templates/backoffice.html:996-1192`.
  - It uses the same scale(.95) → 1 plus `@starting-style` recipe, but with different values: 150 ms `ease-out`, `--r-s`, and its own shadow. `.bo-pop` would bring 120/180 ms `var(--ease-out)`, `--r-m` and `var(--shadow)`.
  - The result would be two popover styles that differ for no stated reason.
- Recommendation: name `.info-pop` in §4.3. Either share one base popover rule (surface, border, shadow, motion) or say why the bar's panel must differ.

### `aria-expanded` is managed by hand and `aria-haspopup="menu"` is wrong - Severity: Nit
- Affected scope: §4.2 (the trigger markup and the `role="menu"` bullet); §4.4 item 4 (first bullet and the `closed` branch).
- Simplicity rationale:
  - **`aria-expanded`:** browsers already expose a `popovertarget` invoker's expanded state to assistive technology, so the script's `aria-expanded` writes only serve the CSS hook.
  - **`aria-haspopup="menu"`:** it announces a menu that, by the design's own decision, is not there.
- Recommendation: drop `aria-haspopup`. Either keep `aria-expanded` and state that it exists for styling only, or style with `:has(:popover-open)` and drop the script writes.

### The ellipsis sentence contradicts itself - Severity: Nit
- Affected scope: §4.3 `.bo-pop` rows.
- Simplicity rationale: the sentence first says a long label "truncates with an ellipsis", then says the truncation styling "is not needed". With `white-space:nowrap`, `max-width:320px` and no overflow rule, a long label overflows the panel. It does not get an ellipsis.
- Recommendation: pick one. Either say "labels are fixed and short; no truncation" and drop the ellipsis claim, or add the truncation rule.

### Several line references are off by one to four lines - Severity: Nit
- Affected scope: §0, §1.1, §4.4.
- Simplicity rationale: the design's line references do not match the current files:

  | Design cites | Actual |
  |---|---|
  | `syncMenuFade` `app.js:440-446` | `:442-447` |
  | scroll listener `:456-458` | `:457-459` |
  | modified-click handler `:466-478` | `:470-478` |
  | `syncMenu` `:448-454` | `:448-456` |
  | `backoffice.html:53` (bar) | `:52` |
  | `backoffice.html:45` (`#boSheet`) | `:46` |
  | `app.css:782` `scrollbar-width` | `:781` |

  `dashboard/static/app.js` also shows as modified in the working tree while this design is still a Draft. If that change is unrelated, the references should be re-anchored once it lands.
- Recommendation: re-anchor the references against the committed files, and confirm that no file outside `docs/designs/` was changed for this feature before approval.

Counts: B=0 M=3 N=4

Files checked:
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/docs/designs/top-bar-menu.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/templates/backoffice.html
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/static/app.js
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/static/app.css
