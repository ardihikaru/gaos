# Comprehension review: `docs/designs/top-bar-menu.md` (Draft v1)

## Paraphrase

### What this proposes

1. **Arrows.** Wrap each of the two back-office menus (the global one and the product one) in a new `.bo-nav` container. Put a left and a right arrow button on either side of the menu. CSS hides the arrows until the existing `data-more` attribute says there is more content that way. Clicking an arrow scrolls the menu by 70% of its width.
2. **Wheel.** A vertical mouse wheel over a menu that overflows scrolls it sideways instead.
3. **Groups.** Turn some links into sub-menus:
   - The global menu goes from 11 entries to 6: Inbox, Products, **Library** (Stories, Diagrams, Knowledge, Archive), Agents, **Automation** (Auto-fix, Pull requests), **System** (Integrations, Traffic).
   - The product menu goes from 9 entries to 8: seven flat tabs plus **Setup** (Blueprint, AI vendors).
   - Each group is a `<button popovertarget>` that opens a `popover="auto"` panel. The panel sits as a sibling of the `<nav>`, outside the scroller.
   - Inside the panel are the existing `<a>` elements, moved without changing a byte.
   - A group button gets the class `is-current` when one of its children is the current view.
4. **Panel script.** It positions the panel under its button and flips it to the right edge if it would overflow. It sets `aria-expanded`. It moves focus into the panel only when the panel was opened from the keyboard. Arrow, Home and End keys move between links. The panel closes when focus leaves it, when the menu scrolls, on resize, and on an htmx request from inside it.
5. **`syncMenu`** gains a fallback: it scrolls the current group's button into view when the current link is inside a panel.
6. **Panel styling.** Popover styling with a scale-from-0.95 enter animation.
7. **Release and registers.** Bump the asset version to v0.70.0. Add two Go tests (TC-904 and TC-905). Update the QA register and its counts, the changelog and `toc.js`. No decision, business rule (BR) or open question (OQ) is added.

### How the existing system works, per this document

- The back-office bar is a three-zone grid: context, menu, controls. Exactly one of two `<nav class="bo-menu">` blocks renders.
- The menu scrolls sideways with a hidden scrollbar. The only hint that there is more is a 36 px edge fade, driven by `data-more`, which `syncMenuFade` sets. A vertical wheel does not scroll it.
- Each menu item is `<a href=page hx-get=fragment>` (D-094). A capture-phase handler lets a modified click (Ctrl, Cmd, middle button) reach the browser.
- Live counts reach `#navNeedsCount` and `#navTimelineN` as out-of-band (oob) swaps.
- The document claims that only a navigation re-renders `#boSheet`.
- Two existing tests assert exact link markup.
- A test ties the `?v=` asset versions in `page.html` to `toc.js` `semver`.

All sections could be paraphrased. The proposal, ordering and arithmetic are clear and mostly well anchored.

## Findings

### Section 4.5 is wrong: background refreshes also re-render `#boSheet` and will close an open panel - Severity: Major
- Affected scope: §4.5; §4.4 item 6 ("When the response re-renders `#boSheet`, the panel is replaced anyway").
- Finding: §4.5 says a full re-render of `#boSheet` happens on "any navigation". `dashboard/static/live.js` says otherwise:
  - `:31-32` says the sheet head "every mutation re-renders".
  - `:56-59` says "Re-rendering the back-office sheet replaces it".
  - `keepPlace` (`:66-76`) re-fetches the pane through `htmx.ajax`, which re-renders the sheet whenever a server hint arrives.

  So a panel the operator is reading, or moving through with the keyboard, can vanish because an agent asked a question. `busy()` (`live.js:60-64`) only holds a refresh back for a focused form field or an open modal. It does not treat an open popover as busy. `keepPlace` restores focus by element `id`, and panel links have no id, so focus also falls to `body`.
- Recommendation: Correct §4.5 to say that background refreshes re-render the sheet too. Then decide one of these:
  - add `.bo-pop:popover-open` to `busy()` in `live.js`, and add `live.js` to the change list; or
  - accept the behaviour and state that as a risk in §9.

  Add a manual check for it in §6.3.

### The security lens is declared "not required" although the change touches the back-office surface - Severity: Major
- Affected scope: header line "Security lens: not required".
- Finding: `docs/rules/gaos-design-review.md` ("The security criterion") makes the security lens mandatory when a design "touches ... the back-office surface". This design changes the back office's only navigation bar, its template, and the script that runs on every back-office page. The document opts out using its own narrower test (no route, credential or origin). The rubric does not offer that test.
- Recommendation: Either run `gaos-goldfish-security`, or cite where the rule lets an author waive the lens for a presentation-only back-office change and have the operator confirm the waiver. As written, the opt-out contradicts the policy.

### Positioning on the `toggle` event can show the panel in the wrong place for a frame - Severity: Major
- Affected scope: §4.4 item 4; §4.3 `.bo-pop` (`position:fixed;inset:auto;margin:0`).
- Finding: The `toggle` event is queued as a task after the popover is shown. The panel is also styled with `inset:auto`, which places it at its static position rather than under its button. So the browser can paint it before the handler sets `top` and `left`. The result can be a visible jump during the 180 ms scale-in. §6.3 item 3 checks the final position but not the first frame. The document does not say why it chose `toggle` over `beforetoggle` plus a measurement after showing.
- Recommendation: Say which event positions the panel and why the first painted frame is already correct. One option: hide it with `visibility:hidden` until positioned, or keep it at `opacity:0` until the script sets `top` and `left`. Add "no jump on open" to §6.3.

### Wrong test name and line anchor for the version check - Severity: Nit
- Affected scope: §5 row 4.
- Finding: The test cited as `TestStaticAssetsAreVersionedWithTheRelease` does not exist. The real test is `TestStaticAssetsAreVersionedByTheProductVersion` at `dashboard/external_inbox_test.go:893` (its comment starts at `:890`, it ends at `:915`). It checks all three `?v=` values in `page.html` against `toc.js` `semver`, so the plan itself is right.
- Recommendation: Correct the name and the line range.

### Several `file:line` anchors are off against the current tree - Severity: Nit
- Affected scope: §0, §1.1, §4.2, §4.4.
- Finding: Each anchor below is followed by what the file actually shows.
  - `backoffice.html:53` for the `<header>`: actual `:52`.
  - `backoffice.html:45` for `hx-swap-oob`: actual `:46`.
  - `app.js:440-446` for `syncMenuFade`: actual `:442-447`.
  - `:448-454` for `syncMenu`: actual `:448-456`.
  - `:451-453` for `scrollIntoView`: actual `:454`.
  - `:456-458` for the scroll listener: actual `:457-459`.
  - `:437-438` for the "no smooth scroll" comment: actual `:440-441`.
  - `:466-478` for the modified-click handler: actual `:470-478`.
  - `app.css:764` for the 300 px context width: actual `:763`, and it is `clamp(160px,20vw,300px)` on `.bo-where`, not the whole zone.
  - `app.css:782` for `scrollbar-width`: actual `:781`.

  Every other anchor I checked is correct: the `app.css` bar rules, the 900 px block at `:1012-1023`, `backoffice.html:72-100`, `:79`, `:86`, `:89`, `autofix_view_test.go:110` and `:143`, `prview_test.go:80`, `page.html:48-51` and `toc.js:22`.
- Recommendation: Re-anchor against the tree the design will be implemented on.

### Contradiction on label truncation - Severity: Nit
- Affected scope: §4.3, `.bo-pop` rows.
- Finding: The sentence first says "A label longer than the panel truncates with an ellipsis". It then says the ellipsis CSS "is not needed: every label is a fixed English word, see §6.3". §6.3 says nothing about labels being fixed words. One engineer would add the ellipsis CSS and another would not.
- Recommendation: Pick one. For example: "Labels are fixed and the longest, 'Pull requests 12 345', fits in 200 px; no truncation is styled."

### "Unchanged: `syncMenu`" introduces a change - Severity: Nit
- Affected scope: §4.4, the last paragraph.
- Finding: The paragraph opens with "Unchanged" and then changes the selector `syncMenu` uses.
- Recommendation: Make it item 8 of the numbered list, "`syncMenu` also matches `.bo-grp.is-current`". Also state whether `syncMenuFade` removes `data-more` from the `.bo-nav` when it removes it from the menu. Item 1 only says "writes the same value".

### Unclear rule for detecting a keyboard open - Severity: Nit
- Affected scope: §4.4 item 4, third bullet.
- Finding: The rule is "the last `keydown` before it was Enter or Space on the trigger". It leaves these open:
  - Where that state is stored.
  - When it is cleared: by a later pointer event, or after the toggle.
  - Space activates a button on `keyup`, not `keydown`.

  Without these answers, a mouse open that follows an earlier keyboard Enter could still be treated as a keyboard open.
- Recommendation: Specify it, for example: "a flag set on `keydown` Enter or Space whose target is a `.bo-grp`, cleared on `pointerdown` and after each `toggle`".

### `aria-haspopup="menu"` contradicts the "no `role=menu`" decision - Severity: Nit
- Affected scope: §4.2, the "No ARIA `role="menu"`" bullet.
- Finding: The document rejects `role="menu"` for the panel but keeps `aria-haspopup="menu"` on the button. Screen readers will announce a menu that never appears.
- Recommendation: Either drop `aria-haspopup`, or justify the mismatch. Also note that browsers already expose an expanded state for `popovertarget` buttons, and say whether the manual `aria-expanded` is needed.

### References to context the reader cannot see - Severity: Nit
- Affected scope: §4.2 ("Correction to the plan this grew from"); §8 item 4 ("the operator chose grouping by job").
- Finding: `docs/designs/README.md` rule 1 says a design stands alone. The "plan this grew from" is not linked anywhere. The operator's choice is not recorded in `decision-log.html`, and §7 says there is no decision. A reader cannot check either claim.
- Recommendation: Remove the reference to the plan. Either cite where the operator's choice is recorded, or label it as an author assumption that the operator still has to confirm.

### Skills and rules named without links - Severity: Nit
- Affected scope: §3 (`frontend.md`); §4.3 (`/emil-design-eng`, `/review-animations`); §6.3 (`/break-ui`, "the frontend rule's Before / After / Why table").
- Finding: These are project-specific skills and rules, used without a path or a one-line definition.
- Recommendation: Give the path of each, or a short gloss in §0.

### Where the product-mode `navNeedsCount` span goes is not stated - Severity: Nit
- Affected scope: §4.1, the last sentences; §4.2.
- Finding: In product mode, `#navNeedsCount` is a hidden `<span>` that sits as a sibling after the `<nav>` (`backoffice.html:86`), not on an Inbox link. The design says the ids "stay on top-level links". It does not say whether this span goes inside the new `.bo-nav` wrapper or stays a grid child.
- Recommendation: State where it goes. It must stay in the bar, because `chrome.html:95` targets it.

### The wheel and arrow behaviour at the edges is not fully defined - Severity: Nit
- Affected scope: §4.4 items 2 and 3.
- Finding: The wheel handler acts only when the target is inside `.bo-menu`. The arrows sit outside the menu and overlap its edges, so wheeling over an arrow does nothing, and that is not stated. `deltaMode === 2` (page units) is not handled.
- Recommendation: Say whether the target test should be `.bo-nav`, and say what page-mode deltas do.

Counts: B=0 M=3 N=10

Files referenced:
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/docs/designs/top-bar-menu.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/static/live.js
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/static/app.js
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/static/app.css
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/templates/backoffice.html
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/templates/chrome.html
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/dashboard/external_inbox_test.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos/docs/rules/gaos-design-review.md
