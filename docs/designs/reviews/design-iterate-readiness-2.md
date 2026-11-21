# Readiness review: `docs/designs/design-iterate.md` (v2)

**Isolation note.** The harness loaded `.claude/CLAUDE.md` into my context automatically. I did not open it, and none of the findings below use it. You should know about this when you check the canary.

Sections that hold up:
- **Change list (§7):** every listed file exists. The one missing file is covered under Finding 5.
- **Counts (§13):** the numbers follow under both merge orders. 353 Go tests + 12 + 10 = 375, of which 331 + 12 + 10 = 353 are in `dashboard/`. The register goes 477 + 12 + 10 = 499, and the decision log goes "Eighty" → "Eighty-two". BR-56 exists (`brd.html:1305`), so BR-57 is the next free rule.
- **TC-208:** removing one pair takes the list from fifteen undeclared moves to fourteen, which matches `taskstate_test.go:36-52`.
- **Line references:** the guard placement, the drift release and the board code match the cited lines.

### 1. `reviewVerdictOf` reads the wrong line, which breaks every gate - Severity: Blocker
- Affected scope: §0 Glossary ("line 2 `verdict: …`"); §3.3 `reviewVerdictOf`; §3.3 check step 5; the switch of `gateStateLocked` to it; §3.11 Consequences.
- Finding: `renderReviewArtifact` (`dashboard/designgate.go:596-598`) writes `"# Design review — %s\n\n"` and then `"verdict: %s\n"`. Line 1 is the heading, line 2 is **empty**, and the verdict is on line 3. Line 4 is `open blockers`, not line 3. A `reviewVerdictOf` built to the stated spec ("reads the second line") returns `""` for every artifact. The consequences are:
  - `gateStateLocked` is switched to it in the same change (`designgate.go:503-504`). Every accepted review would then report `GateFailed`, and no implementation task would ever be released.
  - Check step 5 would refuse every send-back.
  - TC-490 only asserts that a note saying `verdict: accept` is ignored. That passes even when the function always returns `""`, so no planned test catches this.
- Recommendation: Define the anchor exactly: "line 3, i.e. the first line after the heading's blank line, must be `verdict: <v>`." Better, anchor it to the line right after the blank line that follows the heading. Correct the glossary's line numbers. Add an assertion to TC-490 or TC-492 that `gateStateLocked` returns `GateCleared` for an unmodified `renderReviewArtifact` output with zero blockers, and that `reviewVerdictOf` returns `iterate` for one with a blocker.

### 2. The fresh-session rule misses dispatches that never start a run - Severity: Major
- Affected scope: §3.5, `reviewSentBackSinceDispatchLocked`; TC-493.
- Finding: The rule is "fresh iff the newest send-back Seq is greater than the newest `To == DISPATCHED` Seq." A dispatch move can be written without a new run following it:
  - `Reconcile` puts a DISPATCHED task with no run back to READY with rule `requeued` (`dashboard/runner.go:668-679`).
  - The `not-started` path does the same (`runner.go:533`, `board.go:442`).
  - A restart between the move at `runner.go:525` and `startRun` (`runner.go:139`) does the same.

  In each case `dp > sb` while the newest run is still the **pre-send-back** review run. `sessionForLocked` then resumes the session that holds the old design and the reviewer's own earlier findings. That is exactly the isolation breach §3.5 exists to prevent. The argument in §3.5 ("for a retry of the re-review the newest dispatch move is the re-review's own") assumes that every dispatch move produces a run.
- Recommendation: Decide the rule from runs, not from dispatch moves. For example: fresh iff no `TaskRunRecord` of this review has `Attempt` greater than the attempt count at the newest send-back (or a `RunStarted` Seq after it). This can still be derived from the event stream. Add a TC-493 case: send back, dispatch, requeue the task without a run, dispatch again → `("", "reviewer")`.

### 3. The phases do not each leave a buildable, green tree - Severity: Major
- Affected scope: §12 P1–P3; §7 rows 2 and 10; TC-498; the TC-308 edit.
- Finding:
  - P1 (rows 1, 2, 11) puts the `moveTaskLocked` guard `rule == MoveSendBack` in `runner.go`. But `MoveSendBack` is defined in `board.go` (row 10, P2). P1 does not compile.
  - TC-498 (P1) needs "a design sent back once", and `SendBackDesign` does not exist until P2. The plan does not say whether the test should hand-write the events.
  - `decorateBoardGate` lands in P2 (row 10), but the `foldedBoard` fix to TC-308 is scheduled for P3. `boardHistory` builds a planned product (`taskfold_test.go:41-111`), so a gate-blocked card in that history would make TC-308 fail in P2. P2's exit runs only a `-run` subset, so this would go unnoticed until P3.
- Recommendation:
  - Move `MoveSendBack` (or the whole row 10 constant block) into P1.
  - State that TC-498 builds its history from hand-made `TaskMoved` events.
  - Move the `foldedBoard` edit into P2.
  - Make every phase's exit include `cd dashboard && go vet ./... && go test ./...`, in addition to the targeted `-run`.

### 4. P3's exit condition is not a command - Severity: Major
- Affected scope: §12 P3, "a headless render of the board and the sheet"; §3.9 client board and `app.js`.
- Finding: No tool, script, URL or expected result is named. §12 itself says `board.js` has no Go test, so the client board's reason line, the Revise button, its key-stopping, and the sheet's open/close behaviour have no stated check. This is an exit condition of the "it looks right" kind.
- Recommendation: Name the command, for example a stated headless-browser invocation against a seeded `GAOS_RUNNER=manual` instance. List the assertions: the button is present when `sendBack.open`, the limit line when it is not, the sheet opens and focus is trapped, the sheet closes on 2xx, and focus returns to the button. Alternatively, record it as a named manual checklist in the QA register.

### 5. The sheet's view model and JS wiring are left to the implementer - Severity: Major
- Affected scope: §3.8 `handleSendBackSheet`; §3.9 "The sheet", `app.js`; §7 rows 12–14.
- Finding:
  - **Missing file.** Modals here are `{{if not .Timing}}hidden{{end}}` out-of-band swap units driven by a `View` field (`dashboard/templates/needs.html:141`, `dashboard/view.go:71`). The design names no `View` field and no wrapper struct, and `dashboard/view.go` is not in §7.
  - **No product name.** The header needs the product name, which `SendBackView` does not carry. The timing handler looks it up separately (`dashboard/handlers_bo.go:794-801`), and the design says nothing about how this handler gets it.
  - **No ids.** No modal element id and no form id are named.
  - **Hard-coded app.js hooks.** `app.js` keys three behaviours to specific ids:
    - returning focus on close (`app.js:127`);
    - running the enter animation after a swap (`app.js:324`);
    - closing on a 2xx only for `answerForm` (`app.js:308-315`).

    The design names only the open selector. `renderBOAfter` renders `bo-main-oob` plus `oob-chrome`, and `oob-chrome` (`templates/chrome.html:63-67`) does not re-render any modal. So after a successful send-back the sheet stays open. A second submit is then refused by check step 3.
- Recommendation: Specify each piece:
  - the field, for example `View.SendBack *SendBackSheetView`, holding the product name and the `SendBackView`, and add `dashboard/view.go` to §7;
  - the ids, for example modal `sendBackModal` and form `sendBackForm`;
  - the edits at `app.js:127`, `:324` and a 2xx close hook alongside `:308-315`.

### 6. A behaviour change is credited to a test row that does not assert it - Severity: Major
- Affected scope: §3.3a ("TC-493 asserts a re-review under a newer Blueprint names the new pins"); §5 TC-493 row.
- Finding: TC-493's row in §5 covers the session, the packet and retry resume. It says nothing about the `drift:` line or the pins. The `blueprintPinsLocked(reviewTaskID, runID)` signature change therefore has no registered assertion, and its prose under the `go-send-back` section would have nothing to quote.
- Recommendation: Add the assertion to the TC-493 row: after a send-back and a Blueprint republish, the re-review artifact's `drift:` line names the revision's pin and the re-review run's pin. Alternatively, assign it to another row explicitly.

### 7. Unclear whether the 24-rune threshold covers triage notes - Severity: Nit
- Affected scope: §3.5 item 2; §8 R2.
- Finding: "any triage note of `last` or any finding note … that is at least 24 runes long" can be read with the threshold applying to finding notes only. R2 implies it applies to both.
- Recommendation: Say "any note, triage or finding, of at least 24 runes."

### 8. The server-board snippet's placement breaks the template - Severity: Nit
- Affected scope: §3.9 "Server board".
- Finding: The snippet uses `{{$card}}` and is placed "under the meta line". `$card` is declared at `templates/backoffice.html:400`, after the meta line (`:398`) and the QA badge (`:399`), so placing it there is a template parse error. The button also has no `id`, and `openTiming`-style trigger bookkeeping (`app.js:173`) returns focus by `t.id`.
- Recommendation: Place it after `:400` and give it `id="sb-{{$card}}"`.

### 9. The count string for `toc/product.js` is wrong - Severity: Nit
- Affected scope: §13, Decision row.
- Finding: §13 lists `toc/product.js:129` under the "Eighty-two entries" string, but that line says "Eighty decisions …", not "entries".
- Recommendation: Give the exact replacement string for each file: "Eighty-two entries" in `decision-log.html:18` and `check-workspace.sh:129`, and "Eighty-two decisions" in `toc/product.js:129`.

### 10. One stale ToC sentence is not in the docs list - Severity: Nit
- Affected scope: §3.10.
- Finding: `pustaka/docs/assets/toc/architecture.js:116` also says the clearance is refused "until the back office has a real identity". §3.10 edits only `:104`.
- Recommendation: Add `:116` to §3.10 and §7 row 16.

### 11. `sendBacksLocked` has no stated signature - Severity: Nit
- Affected scope: §3.3 step 6; §3.4 snippet (`last.Payload.Triage`); §3.5 item 2.
- Finding: "Returning the count and the newest such event" does not say whether the event is `Event` or `*Event`, or what comes back when the count is 0.
- Recommendation: State it, for example `func (s *MemStore) sendBacksLocked(designID string) (int, Event)`, with a zero `Event` when the count is 0.

### 12. Register timing across phases is not stated - Severity: Nit
- Affected scope: §12 P1–P4; `.claude/rules/qa-test-plan.md` "The one rule".
- Finding: The tests land in P1–P3, but the register rows, prose and counts land in P4. The QA rule requires them in the same change. The plan does not say whether the phases are separate commits.
- Recommendation: Either state that P1–P4 are one commit or PR, or move each phase's register rows into that phase.

Counts: B=1 M=5 N=6
