# Security review: `docs/designs/top-bar-menu.md` (Draft v1)

## Paraphrase

### What this proposes

The back office's top bar has a sideways-scrolling menu that hides its scrollbar. Items past the edge can't be reached with a plain mouse wheel. The design fixes this in three ways:

1. **Arrow buttons.** It adds two arrow buttons (`.bo-step`) on each side of the menu. They are hidden from assistive technology and from Tab. They show only when the existing `data-more` attribute says there is more to scroll. Clicking one pages the menu by 70 %.
2. **Wheel scrolling.** It adds a document-level, capture-phase, non-passive `wheel` listener. It turns vertical wheel movement into sideways scrolling, but only while the pointer is over a menu that overflows.
3. **Groups.** It cuts the global menu from 11 entries to 6 by grouping Stories, Diagrams, Knowledge and Archive under Library, Auto-fix and Pull requests under Automation, and Integrations and Traffic under System. It cuts the product menu from 9 to 8 by putting Blueprint and AI vendors under Setup.
   - Each group is a `<button popovertarget>` that opens a `popover="auto"` panel. The panel is a sibling of the `<nav>`, so it renders in the browser's top layer.
   - The child links move into the panels byte for byte.
   - A script positions each panel under its trigger. It also keeps `aria-expanded` in step, adds arrow, Home and End keys inside a panel, and closes the panel on focus-out, scroll, resize and on any htmx request from inside it.

There are no route, handler, view-model or store changes. Two new Go tests (TC-904, TC-905) check the markup. The change list also bumps the asset version, the QA register counts, the changelog and the semver to v0.70.0. The design declares the security lens "not required" (design:8).

### How the existing system works, per this document

- The bar is part of `#boSheet`, which every navigation re-renders as an out-of-band swap.
- The bar holds either the global menu or the product menu. Each item is an `<a href hx-get hx-target="#sink">` (D-094).
- A capture-phase click handler lets modified clicks through as normal browser navigation.
- `syncMenu` and `syncMenuFade` keep the current item in view and set `data-more`.
- `live.js` updates two counts in the bar by id (`navNeedsCount`, `navTimelineN`).
- The bar is shown only to a signed-in session (BR-55).

## Trust boundary walk

The only boundary this design touches is **operator browser → back-office dashboard**. None of the others is affected: there is no new outbound call, no database access, no edge change and no change to deploy or the host.

- **What authenticates it:** the back-office cookie.
  - The rubric's description of `signedIn()` as "a cookie that currently checks nothing" is out of date. `dashboard/auth.go:375-396` verifies the cookie's MAC and expiry through `a.verify`, which matches BR-55 (`pustaka/docs/product/brd.html:1356`).
  - The new markup sits inside `{{if .BO}}` (`dashboard/templates/backoffice.html:46-47`), so it inherits that gate.
- **What authorises it:** nothing changes. Every child link targets a route that already exists, including `GET /ui/bo/{view}` (`dashboard/main.go:612`). No new route is added.
- **What validates it:** the template outputs no new store value.
  - The `is-current` class comes from `eq` against string literals.
  - The panel ids and `aria-label`s are literals.
  - I confirmed both byte-exact test anchors exist (`dashboard/autofix_view_test.go:143`, `dashboard/prview_test.go:80`).
- **What if the far side fails:** without `popover` support or JavaScript, the menu falls back to today's behaviour, and every view can still be reached by URL (design:110-113, 234).

### Abuse cases

- **Replay, forged request, IDOR, path traversal:** none new. No request shape changes.
- **Oversized payload, DoS against the tunnel:** none new. Paging and wheel handling run only in the browser.
- **Injection through a free-text field:** the template adds no free text. The new script does select page elements by class name across the whole document (see SEC-2).
- **Secret leaking into a log or reply:** none. No value from the store is added.
- **Privilege escalation:** none. Grouping hides nothing from the server's point of view, and server-side checks are unchanged.
- **Credential committed:** none. No credential file is in the change list.

## Findings

### SEC-1 The design exempts itself from the security lens that the policy requires - Severity: Medium
- Component: the design-review process, back-office surface
- Evidence: `docs/designs/top-bar-menu.md:8`; `docs/rules/gaos-design-review.md:94-97`; `docs/rules/gaos-design-review.md:63-64`
- Attack path: The policy requires the security lens whenever a design "touches … the back-office surface". This design rewrites the back office's navigation markup (`backoffice.html:72-100`) and adds document-wide event handlers in `app.js`. A design that can declare itself exempt sets a precedent where the author decides whether security gets reviewed. That is the same shared-context blind spot the method exists to remove (`docs/rules/gaos-design-review.md:16-19`). No exploit follows from this design itself, but the control was skipped on a path the policy names as privileged. I rated it Medium rather than Low because a skipped control on a path the policy names is more than a wording problem.
- Remediation: Change design:8 to say "Security lens: required (back-office surface); report `top-bar-menu-security-review-1`". Keep the reasoning for why the risk is low as context, not as an exemption.
- Effort: quick-win

### SEC-2 The new handlers select by class name across the whole document and rely on an invariant the design never states - Severity: Low
- Component: `dashboard/static/app.js`, design §4.4 steps 2-6
- Evidence: `docs/designs/top-bar-menu.md:135`, `:139`, `:140-141`, `:146`; `dashboard/markdown.go:12-21`; `dashboard/static/app.js:457-459`
- Attack path: Every new listener is attached to `document` and matches on `.bo-step`, `.bo-menu` and `.bo-pop` anywhere in the page.
  - Step 3 calls `preventDefault()` on wheel events inside any `.bo-menu`.
  - Step 5 takes over arrow keys inside any `.bo-pop`.
  - Step 4 builds a selector by joining a panel's `id` into an attribute string (`'[popovertarget="' + id + '"]'`).
  - Today this is safe only because rendered user or model content can't carry a `class`, `id` or `popover` attribute: `markdown.go` escapes first and emits tags from a fixed list (`dashboard/markdown.go:12-21`). The design doesn't rely on that fact in writing.
  - If a later renderer or a `template.HTML` value ever lets those attributes through, view content could hijack the bar's wheel and keyboard handling. An id containing a quote would make `querySelector` throw inside the toggle listener. That breaks the bar on the page, which is a nuisance-level denial of service, not code execution.
- Remediation:
  - Limit each listener to the bar: check `e.target.closest("#boSheet .bo-bar")` before acting.
  - Find the trigger without building a selector from a string. Search inside the panel's own `.bo-nav` parent and compare `getAttribute("popovertarget") === panel.id`, or keep the four ids in a fixed table.
  - Add one sentence to §4.4 saying the handlers trust only bar markup, and cite `markdown.go:12-21` as the reason view content can't imitate it.
- Effort: quick-win

### SEC-3 TC-904/905 test only signed-in requests, so the "only shown to a signed-in session" claim is not checked for the new markup - Severity: Low
- Component: `dashboard/bo_menu_test.go` (new), design §6.1
- Evidence: `docs/designs/top-bar-menu.md:7`, `:177-189`; `dashboard/templates/backoffice.html:46-47`
- Attack path: Both new tests send `boCookies()`. The panels list every back-office view URL. They are safe only if they stay inside `{{if .BO}}`. A future edit could move a panel to page level, for example to escape a stacking context, which is a plausible step for top-layer markup. That would send the menu structure to a signed-out visitor, and no test would fail. The exposure would be small, since the URLs are not secret and the routes stay gated by BR-55. But the design names BR-55 as the guarantee and never tests it for the new elements.
- Remediation:
  - Add a negative case to TC-904: `GET /stories` with no cookie must contain no `boGrp` and no `popovertarget`.
  - State in §4.2 that every `.bo-pop` must stay inside the `{{if .BO}}` block of `bo-sheet`.
- Effort: quick-win

## Notes for the author (not findings)

- **Out-of-date line numbers.** Several `app.js` references have drifted:
  - "`:440-446`" for `syncMenuFade` is actually 442-447;
  - "`:448-454`" for `syncMenu` is 448-456;
  - "`:456-458`" for the scroll listener is 457-459;
  - "`:466-478`" for the D-094 handlers is 470-478.

  This is a readiness issue, not a security one.
- **The rubric's own statement about `signedIn()` is out of date** (see the trust boundary walk). It should be corrected in the agent's instructions, not in this design.
- **The reviewer deny-list is enforced only by instruction** outside Claude Code (`docs/rules/gaos-design-review.md:142-146`). That is a property of the review framework, not of this design, so it is not raised as a finding here.

Counts: Critical=0 High=0 Medium=1 Low=2
