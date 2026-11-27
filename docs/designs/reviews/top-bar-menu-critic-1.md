# Goldfish critic review: `docs/designs/top-bar-menu.md`

The design's main idea holds up: fixed named groups shown with the `popover` attribute, with each child link moved unchanged. Most of its anchors also check out against the repository. Three things are wrong:

- **The change list is incomplete.** The design's own verification command, `./deploy/check-workspace.sh`, will fail after this change.
- **§4.5 is wrong about live updates.** A background refresh does replace an open panel.
- **The script has a focus bug.** The rule that closes a panel on `focusout` will reopen it, or swallow a click, in common sequences.

The strongest argument against the design: for the product menu, grouping two items saves one entry. That is a lot of new script and ARIA risk for little gain. Arrows plus wheel scrolling (Alternative 1) would fix the reachability problem (§1.1) on their own. §1.2 ("no room for the next page") is a forecast that one more template edit could absorb when a new page actually arrives.

---

### The change list leaves out `deploy/check-workspace.sh` and `README.md`, so §6.2 fails - Severity: Blocker
- Affected scope: §5 change list (rows 1–10), §5 arithmetic, §6.2.
- Finding and rationale: `deploy/check-workspace.sh` hard-codes the counts this change moves:
  - `:114` and `:118` hold "seven hundred and sixty-three", and `:115` holds `"763"`.
  - `:129` and `:130` hold "nine hundred and three".
  - `:147–149` compare `README.md` with what the script computes: `"$go_tests Go tests"`, `"$qa_rows registered test cases"` and `"Product version $semver"`.

  `README.md:12` says "Product version 0.69.0", `:143` says "763 Go tests: 738 in `dashboard/`" and `:145` says "903 registered test cases". Neither file is in §5. Once the two new `func Test…` exist and `toc.js` says 0.70.0, `./deploy/check-workspace.sh` (the last line of §6.2) fails on at least six checks. The release cannot pass its own verification as written.
- Recommendation: Add `deploy/check-workspace.sh` (lines 114, 115, 118, 129 and 130, moving to 765 / "seven hundred and sixty-five" / "nine hundred and five") and `README.md` (lines 12, 143 and 145) to §5. Restate the file count and the arithmetic. Then grep the repository for every other pinned "903", "763", "738" and "0.69.0" so §5 is shown to be complete, not assumed to be.

### §4.5 is wrong: a live refresh replaces the whole sheet, and with it an open panel - Severity: Major
- Affected scope: §4.5; §9 has no row for this.
- Finding and rationale: §4.5 says the bar's only live targets are two out-of-band (oob) count swaps, and that a full re-render happens only on "any navigation". That is false:
  - `live.js:104-110` re-fetches `ui/bo/inbox` on every question hint while the Inbox is open.
  - `live.js:251` re-fetches the product board pane.
  - Both go through `keepPlace` → `get()` (`live.js:43-47`, `66-76`), which swaps `#sink`. That re-renders `#boSheet`, the bar included, because the sheet is oob at `backoffice.html:46`.
  - The only guard is `busy()` (`live.js:60-64`). It defers the refresh for a dialog or a focused INPUT, TEXTAREA or SELECT, but not for an open popover.

  So an operator who opens Automation or System from the Inbox can have the panel disappear under the pointer when an agent asks a question. A keyboard user is worse off: `keepPlace` restores focus by `id` (`:69`, `:73`), the panel's links have no ids, and focus falls to `<body>`.
- Recommendation: Rewrite §4.5. Add `live.js` to §5 and make `busy()` (and `boardBusy()`, `:206-210`) return true while `document.querySelector(".bo-pop:popover-open")` matches. Also run `runDeferred` when a panel closes (the `toggle` listener's closed branch). Add a §9 row and a §6.3 step: open a group on the Inbox, trigger a question, and check that the panel stays open until it is dismissed.

### The `focusout` → `hidePopover()` rule reopens the panel or swallows a click - Severity: Major
- Affected scope: §4.4 step 5 (last clause) and step 4 (opening from the keyboard).
- Finding and rationale: Suppose a panel was opened from the keyboard, so focus is on a child link.
  - **Clicking the trigger to close it:** `mousedown` moves focus, `focusout` fires and the rule calls `hidePopover()`. The `click` then reaches a `popovertarget` button whose popover is now closed, so the button opens it again. The panel cannot be closed from its own trigger.
  - **In Safari, clicking a child link:** Safari does not focus a link or button on click, so `relatedTarget` is `null`. The `focusout` on `mousedown` closes the panel and removes it from the top layer before `click` fires. The navigation is lost.

  §6.3 does not exercise either sequence.
- Recommendation: Drop the `focusout` rule. Light dismiss already closes an `auto` popover on an outside click, and Esc already closes it. If Tab-out must close the panel, handle it in the panel's own `keydown` (Tab or Shift+Tab on the last or first link → `hidePopover()` and move focus on), not on `focusout`. Add both sequences to §6.3, and name Safari explicitly.

### The ARIA on the trigger contradicts its own rationale and is wrong with JavaScript off - Severity: Major
- Affected scope: §4.2 markup (`aria-haspopup="menu" aria-expanded="false"`) and the "No ARIA `role="menu"`" decision; §4.4 step 4.
- Finding and rationale:
  - **`aria-haspopup="menu"` without a `role="menu"` panel.** The design rejects `role="menu"` so that it does not take on the menu keyboard model. But `aria-haspopup="menu"` makes NVDA, JAWS and VoiceOver announce a menu button, which promises that model to the user. The design then implements half of it (ArrowUp, ArrowDown, Home and End inside the panel). Keeping the attribute as a "conventional hint" is the exact mismatch the ARIA Authoring Practices warn against.
  - **The static `aria-expanded="false"`.** A `popovertarget` invoker already exposes its expanded state to accessibility tools on its own. Writing `aria-expanded="false"` into the markup overrides that. With JavaScript off (§4.2 says the panel still opens), the attribute stays `"false"` while the panel is open. §4.3 also styles `.bo-grp[aria-expanded="true"]` and rotates the chevron from it, so with no script the visual state is wrong too.
- Recommendation: Use the disclosure pattern:
  - Remove `aria-haspopup`.
  - Do not write `aria-expanded` in the markup; let the browser derive it from `popovertarget`.
  - Drive the open-state style from `.bo-nav:has(#boGrpX:popover-open)`, or from a class that the `toggle` listener sets.
  - Give the panel a role that can carry its `aria-label`; see the Nit below.

### The security criterion matches, and the design waives it on its own authority - Severity: Major
- Affected scope: header line 8 ("Security lens: not required").
- Finding and rationale: `docs/rules/gaos-design-review.md:94-97` requires `gaos-goldfish-security` when a design "touches … the back-office surface", and says "the security lens is not optional" (`:63-64`). This design rewrites the back office's only navigation bar (`backoffice.html:52-112`). The author's reasoning (no new route, credential or outbound call) may well be correct. But the rubric gives the author no exemption based on reasoning, and the design does not ask the operator to grant one.
- Recommendation: Either run the security lens, or record the waiver as an operator decision with the rubric's wording quoted. Do not assert the waiver in the header.

### A capture-phase, non-passive `wheel` listener on `document` for a 42–56 px strip - Severity: Major
- Affected scope: §4.4 step 3; §9 row 3.
- Finding and rationale: A `{passive:false}` wheel listener on `document` makes the browser wait for the main thread before it scrolls anything in the app. That includes the board's column scrollers (`live.js:263-293`, the heaviest view) and `.sheet-body`. The cost lands everywhere, for a gain on one strip. §9 talks only about trapping a vertical scroll, not about scroll latency. The handler also:
  - ignores `deltaMode === 2` (page units);
  - keeps calling `preventDefault()` when the menu is already at its scroll limit, which is harmless here but nowhere stated.
- Recommendation: Attach the listener to the `.bo-nav` element itself after each `#boSheet` swap (there is one menu per page, so this is one listener). Or attach it to `#boSheet .bo-bar`. Either keeps the rest of the page on passive scrolling. Handle `deltaMode === 2` by multiplying by `menu.clientWidth`.

### Goal 2's "both fit at 1280 px" is not backed, and nothing is decided if it fails - Severity: Major
- Affected scope: §2 item 2; §1.1 assumption A1; §4.1 product menu; §6.3 step 1.
- Finding and rationale:
  - Goal 2 is a fixed outcome, but A1 is measured only after the change is built.
  - On the product menu, the only change is that "Blueprint (n)" and "AI vendors" become "Setup ▾". That saves about one item's width.
  - In product mode the context zone shows "Products / <name>" at up to `clamp(160px,20vw,300px)` (`app.css:763`). Five of the remaining tabs carry counts (`backoffice.html:74-79`).
  - With §1.1's own arithmetic (about 22 px of padding plus the label and the count per item), eight entries are not shown to fit in the roughly 650 px left at 1280 px.

  If step 1 of §6.3 finds that the product menu still overflows, the document does not say whether that blocks the release, regroups the menu, or is accepted.
- Recommendation: Either measure A1 now and put the numbers in §1.1, or restate Goal 2 so that the global menu must fit at 1280 px and the product menu may overflow, with the arrows covering it. Say what happens on failure. Also justify the Setup group on its own merits, or drop it: one saved entry may not be worth a second group pattern.

### Several anchors do not match the repository - Severity: Nit
- Affected scope: §0, §1.1, §4.3, §4.4, §5 row 4, §6.1.
- Finding and rationale:
  - The bar header is at `backoffice.html:52`, not `:53`.
  - The test is `TestStaticAssetsAreVersionedByTheProductVersion` (`external_inbox_test.go:893`), not `…WithTheRelease`.
  - `autofix_view_test.go:110` uses `newAFFixture(t)` and `f.srv`, not `newTestServer(t)`. `newTestServer` is at `main_test.go:27` and returns `(*server, *MemStore)`.
  - In `app.js`, `syncMenuFade` is at `:442-447` (the design says `:440-446`), `syncMenu` at `:448-456`, the scroll listener at `:457-459` and the bar block at `:437-460`.
  - `scrollbar-width:none` is at `app.css:781`, and the 300 px clamp at `:763`.
  - The git status at session start shows `dashboard/static/app.js` modified in the working tree. That may explain the drift. If the modification belongs to this feature, it is a file outside `docs/designs/` changing before approval.
- Recommendation: Re-anchor against `HEAD`. State what the `app.js` working-tree change is, or revert it until the design is approved.

### §4.3 says a long label both truncates and does not need truncating - Severity: Nit
- Affected scope: §4.3, the `.bo-pop` rows bullet.
- Finding and rationale: "A label longer than the panel truncates with an ellipsis (… is not needed …)" states both outcomes, so two implementers could build different rows.
- Recommendation: Pick one. Given fixed English labels and `max-width:min(320px,…)`, say "no truncation: the panel grows to its widest row, up to the cap" and drop the ellipsis clause.

### The panel's `aria-label` sits on a generic `div`, and the arrows are `aria-hidden` buttons - Severity: Nit
- Affected scope: §4.2 markup.
- Finding and rationale: ARIA 1.2 forbids naming an element with the generic role, so `aria-label="Library"` on a plain `div` is ignored. The arrows are `<button>` elements with `aria-hidden="true"`. Chromium focuses a button on click, which puts focus inside an `aria-hidden` subtree.
- Recommendation: Give the panel `role="group"`, or wrap its links in a labelled `<nav>` or `<ul>`. Render the arrows as `<span>` or `<div>` with `aria-hidden`, which needs no `tabindex`.

### Keyboard-open detection is specified loosely - Severity: Nit
- Affected scope: §4.4 step 4, third bullet.
- Finding and rationale: "The last `keydown` before it was Enter or Space on the trigger" leaves open how long that flag stays set, and whether a later mouse click clears it. Space also activates a button on `keyup`, not `keydown`.
- Recommendation: Use the trigger's `click` event: `e.detail === 0` means the click came from the keyboard. Record it on the panel, and read it in the `toggle` listener.

### The register prose cannot quote messages the design never fixes - Severity: Nit
- Affected scope: §5 row 6; §6.1.
- Finding and rationale: The QA register rule requires each scenario to quote the test's exact `t.Errorf` string. §6.1 gives the assertions but none of the messages, so the register text is left undecided.
- Recommendation: Write the `t.Errorf` and `t.Fatalf` strings for TC-904 and TC-905 into §6.1.

### The existing popover is not considered - Severity: Nit
- Affected scope: §4.3 (`.bo-pop`, Motion); §8.
- Finding and rationale: The dashboard already ships `popover` panels: `.info-pop` (`app.css:2016-2024`, `backoffice.html:996-1192`, D-098). They use `--r-s`, their own shadow and 150 ms timing. The new `.bo-pop` uses `--r-m`, `var(--shadow)` and 120/180 ms. The result is two popover styles in one console, and the design neither mentions nor reconciles them.
- Recommendation: Cite `.info-pop` as prior art and either align the two or say why they differ.

---

What holds up:
- **Byte-exact links:** the child links are byte-exact as claimed (`autofix_view_test.go:143`, `prview_test.go:80`, `backoffice.html:89-99`).
- **IDs and the D-094 handler:** the `navNeedsCount` and `navTimelineN` ids are at `:89` and `:79`, and the D-094 modified-click handler is at `app.js:470-478`.
- **Registers:** the `.Tab` values `blueprint` and `vendors` match `:80-81`. D-094's URL contract (`decision-log.html:2021-2044`) is respected, and no BRD rule governs the layout.
- **Version bump:** the `?v=` lines are at `page.html:48,50,51` and `semver` is at `toc.js:22`.
- **Alternatives and non-goals:** the alternatives are real, and the non-goals are clear.

Counts: B=1 M=6 N=6
