# Readiness review: `docs/designs/top-bar-menu.md`

I read the design, the rubric, the three domain registers and the files the design cites. I did not open anything under `.claude/`. The harness attached `.claude/rules/agent-workspace.md`, `qa-test-plan.md` and `frontend.md` by itself, and I used them only as rubric.

### The validator the plan runs will fail, because `deploy/check-workspace.sh` and `README.md` are missing from the change list - Severity: Blocker
- Affected scope: §5 Change list; §6.2 Commands
- Finding: §6.2 makes `./deploy/check-workspace.sh` a pass condition. That script has the current counts typed into it as fixed values:
  - `deploy/check-workspace.sh:114-118` checks for the phrase "seven hundred and sixty-three" and for `go_tests` = `763`.
  - `deploy/check-workspace.sh:129-130` checks for "nine hundred and three" in `qa-test-plan.html` and in `toc/product.js`.
  
  After changes #5 to #7 there are 765 Go tests and the prose says "nine hundred and five", so all three checks fail. The script also compares `README.md` with values it computes itself:
  - `README.md:12` says "Product version 0.69.0".
  - `README.md:143` says "763 Go tests: 738 in `dashboard/`".
  - `README.md:145` says "903 registered test cases".
  
  Those three checks (`check-workspace.sh:147-149`) also fail. Neither file is in the change list, so either the implementer edits files that are not on the allowlist, or the release fails its own gate.
- Recommendation: Add two rows to §5.
  - `deploy/check-workspace.sh`: on `:114`, `:115` and `:118`, change "seven hundred and sixty-three" and `763` to "seven hundred and sixty-five" and `765`. On `:129-130`, change "nine hundred and three" to "nine hundred and five".
  - `README.md`: on `:12`, `0.69.0` → `0.70.0`. On `:143`, `763`/`738` → `765`/`740`. On `:145`, `903` → `905`.
  
  Update the §5 arithmetic to match.

### No phases, no exit conditions, no ordering - Severity: Major
- Affected scope: §5, §6
- Finding: The ten changes form one flat list. Some of them only pass together:
  - #4 (`page.html` `?v=`) and #10 (`toc.js` semver) must land together, or `TestStaticAssetsAreVersionedByTheProductVersion` fails.
  - #5 to #8, plus the validator and README edits above, must land together, or `check-workspace.sh` fails.
  - #1 must come before #5, because the tests assert markup that #1 creates.
  
  No intermediate state has an exit command. §6.3 (by-hand checks) is not tied to any point in the work.
- Recommendation: Define phases, each with an exit command.
  - **P1, markup plus tests:** changes #1 and #5. Exit when `cd dashboard && go test ./...` is green and TC-904/905 pass.
  - **P2, CSS plus JS:** changes #2 and #3. Exit with the §6.3 by-hand checklist.
  - **P3, registers plus release:** #4, #6 to #10, the validator and the README. Exit when every command in §6.2 is green.

### TC-904/905 failure messages are not stated, so the register prose (#6) cannot be written from the design - Severity: Major
- Affected scope: §6.1; §5 #6
- Finding: The register rule says the scenario `h3` must quote the test's exact `t.Errorf` and `t.Fatalf` strings. §6.1 lists what each test asserts but gives no message for any of them. The register content is also left to the implementer:
  - the Scenario sentence for each row;
  - the `data-tags`;
  - the `h3` id/anchor and where it goes under `#implemented`.
  
  Two engineers will write different register entries. This is the audit vector "a forward specification whose asserted value is not actually stated".
- Recommendation: In §6.1, give one exact `t.Errorf`/`t.Fatalf` string per assertion. In §5 #6, give the two `<tr>` rows verbatim and the `h3` id (for example `go-bo-menu`), and say where it goes (after `#go-traffic`).

### Keyboard-open detection has no reset rule - Severity: Major
- Affected scope: §4.4 item 4
- Finding: The rule "the last `keydown` before it was Enter or Space on the trigger" keeps state but never says when that state clears. If a user opens a panel with Enter, closes it, and later clicks the trigger with the mouse, the last keydown is still Enter-on-trigger. The child then gets focus and shows a ring on a mouse open, which is exactly what the design says must not happen. Engineers will clear it differently: on `pointerdown`, after the toggle, or with a timeout.
- Recommendation: Specify one mechanism. For example, record `kbd = (e.detail === 0)` in a capture `click` listener on `.bo-grp` (a click from the keyboard has `detail` 0), and read it in the `toggle` handler. Or clear the flag on every `pointerdown`.

### The focusout close and the trigger toggle conflict - Severity: Major
- Affected scope: §4.4 item 5
- Finding: Suppose focus is inside an open panel (opened from the keyboard) and the user clicks the trigger to close it. Two things happen in order:
  1. `focusout` fires with `relatedTarget` = trigger, and the handler calls `hidePopover()`.
  2. The click then reaches `popovertarget` and toggles the now-closed panel open again.
  
  The panel cannot be closed by clicking its own trigger. The design does not handle the case where `relatedTarget` is the panel's own trigger.
- Recommendation: Add "skip `hidePopover()` when `e.relatedTarget` is the panel's own `[popovertarget]` trigger", and add this case to §6.3 item 4.

### The panel is positioned after it has already painted - Severity: Major
- Affected scope: §4.3 `.bo-pop`; §4.4 item 4
- Finding: The design sets `top`, `left` and `transform-origin` in the `toggle` handler. That event is queued and fires after the popover is shown, so the first frame can paint the panel at its `inset:auto` static position, with its entry transition already running from the wrong origin. The design does not say whether to use `beforetoggle` (runs before showing, but the panel cannot be measured yet) or `toggle` plus a correction. Separately, §4.2's claim that "with JavaScript off the panel shows … the centre of the viewport" is wrong: §4.3's `inset:auto;margin:0` is plain CSS and removes the centring whether or not JS runs.
- Recommendation: Position in `beforetoggle`: set `top` and the left-aligned `left` from the trigger's rect, then do the flip and clamp in `toggle`. Alternatively, state that the flash is accepted and why. Correct the §4.2 JS-off sentence to describe the real fallback position.

### Several cited line numbers and a test name are wrong - Severity: Nit
- Affected scope: §0, §4.4, §5 #4
- Finding: Each citation and what the file actually has:
  - `TestStaticAssetsAreVersionedWithTheRelease`: the test is `TestStaticAssetsAreVersionedByTheProductVersion` (`dashboard/external_inbox_test.go:893`).
  - In `app.js`, the citations are off by about two lines against the current working tree:

    | Design cites | Actual |
    |---|---|
    | bar block `:435-460` | `:437-460` |
    | `syncMenuFade` `:440-446` | `:442-447` |
    | `syncMenu` `:448-454` | `:448-456` |
    | `scrollIntoView` `:451-453` | `:454` |
    | scroll listener `:456-458` | `:457-459` |
    | modified-click handler `:466-478` | `:470-478` |
  - `backoffice.html:53` for the header: it is `:52`. `backoffice.html:45` for `#boSheet`: it is `:46`.
- Recommendation: Re-anchor these against the committed tree.

### Test assertions are not precise enough to implement the same way twice - Severity: Nit
- Affected scope: §6.1
- Finding: Three assertions are open to different implementations:
  - "Inside the `boGrpLibrary` panel's markup" does not define where the panel's markup starts and ends.
  - "each href appears exactly once as `href="…" hx-get="ui/bo/…"`" does not work for `./`, whose fragment is `ui/bo/products`.
  - The literal (substring or regexp) is not given. The needs chip (`chrome.html:82`) also carries `hx-get="ui/bo/inbox"`, so a looser pattern counts two.
- Recommendation: Give the 11 literal substrings. Define the panel slice as running from `id="boGrpLibrary"` to the next `</div>`.

### `toc/product.js` wording and the changelog text are not given - Severity: Nit
- Affected scope: §5 #7, #9
- Finding:
  - #7 says "903 → 905" and "763 → 765", but `toc/product.js:162` and `:168` spell the numbers out in words.
  - #9 says "a v0.70.0 entry" but gives no bump class (minor), no date and no list items.
- Recommendation: Write the exact replacement phrases ("nine hundred and five", "seven hundred and sixty-five") and the changelog `<section>` text.

### The D-108 question is left open - Severity: Nit
- Affected scope: §7
- Finding: "If the operator wants it on record, it becomes D-108" is a conditional. If it is taken, it adds `decision-log.html`, its count, the ToC and a `check-workspace.sh:131` edit, none of which are in §5.
- Recommendation: Settle it before approval. If the answer is yes, list those changes.

### The §6.2 commands assume the wrong working directory - Severity: Nit
- Affected scope: §6.2 line 3
- Finding: After `cd ../pustaka`, `./deploy/check-stacks.sh` is resolved from inside `pustaka/` and will not be found.
- Recommendation: Prefix it with `cd ..`, or use subshells: `(cd dashboard && …)`.

### Sections that hold up
- §4.1 grouping and arithmetic: consistent with `backoffice.html:72-100`. The byte-exact link constraint is correctly tied to `autofix_view_test.go:143` and `prview_test.go:80`.
- Identifiers: TC-904 is the next free number, which matches the register's highest, TC-903. TC-905 is used once. The counts 903, 738 and 763 match the current registers.
- Design tokens: every CSS variable the design uses exists in `app.css`.

Counts: B=1 M=5 N=5
