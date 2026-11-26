# Review 1 — `board-and-list-ux.md`

Four lenses ran in parallel, each with zero context: comprehension, zoom-out, critic and readiness. Each got the design path and the rubric path list (`pustaka/docs/product/brd.html`, `decision-log.html`, `open-questions.html`, `docs/rules/gaos-design-review.md`, `docs/designs/README.md`), and was told to open nothing under `.claude/`. The security lens is filed separately (`board-and-list-ux-security-review-1.md`).

- **Canaries.** One `EGM-MARKER-…` line was planted in each of the seven standing files and removed after the run. No canary value appears in any report. The critic reported that the harness attached three rule files (`agent-workspace.md`, `qa-test-plan.md`, `frontend.md`) when it read under `docs/` and `pustaka/`; that is the known path-scoped attach described in the runbook, not a `CLAUDE.md` leak. The readiness reviewer saw the canary edits as modified files in `git status`, and did not read them.
- **Positive control.** A `general-purpose` agent read `.claude/rules/frontend.md` and `.claude/CLAUDE.md` and returned both canaries.

## Comprehension — B=0 M=5 N=9

The paraphrase is accurate. §3.1, §3.3, §3.6, §6 and §7 were clear.

- **[M] The "off" help text describes behaviour the runner does not have.** With `GAOS_RUNNER=off`, the scheduler loop never starts (`main.go:306`, `runner.go:40`, D-080), so tasks stay in Backlog. The loop also needs an adapter. Step 2 is wrong under manual: a card parked in Ready claims no lease (`runner.go:503-510`). The fix is to derive the runner state from both the adapter and the mode, add a no-adapter variant, and test off and no-adapter too.
- **[M] The "Blocked" help conflates three different cases.** These are the design-gate hold (wait for the review, `board.go:359-363`), a review blocked by its own verdict (Revise design, `:366-370`), and Deferred (cannot be moved, `:212-213`). It also uses "blocked" in the scheduler's sense for Backlog cards. The fix is to separate them.
- **[M] A move from the Board table re-renders page 1 while the address keeps the old page.** Mutations carry `X-Board-State` (`live.js:169-174`) and are answered through `renderBoardAfter` (`board_view.go:262`). The fix is to send `page` on every request except a filter change and the reset links.
- **[M] PR and Auto-fix drawer and tab links handle the page inconsistently.** The PR row links (`prview.go:281,308`) and the close links (`prview.go:379`, `autofix_view.go:767`) carry no page. PR tabs copy the filter (`:324-326`); Auto-fix tabs use literals (`:564,570`). §4 row 6 says "if read here", which is undecided. The fix is to list every link with keep or drop.
- **[M] The relabelled Inject tooltip contradicts BRD A-6 and B-4 without saying so.** The in-memory `Inject` also runs `evaluateReadiness` (`store_mem.go:1993`).
- [N] The QA pane builder cannot see the request (`handlers_qa.go:92`).
- [N] It is unclear who builds the Board's Pager. `BoardQuery.clean` (`board_page.go:449`) must keep `Page`.
- [N] The live.js mechanism for "only on refresh" is not stated.
- [N] "Every data table at 10" conflicts with Archive's tables at 5.
- [N] "New task" opens a wizard titled "Create workflow" that creates several tasks.
- [N] TC-754 does not say which route produces the URL header (`urls.go:341-348`).
- [N] The claim that `TestEveryHtmxLinkHasAPageHref` proves the pager links is unverified, because the fixture may never reach page 2.
- [N] The change list omits the rule file, and the "four places" are misattributed.
- [N] Line anchors have drifted (`board_page.go` 36–37, 498–502, 194–195; `page.html` 48/51; Arcade Inject at 534 and 1275; Auto-fix Href at 559).
- [N] Undefined terms: `X-GAOS-Refresh`, `/break-ui`, "BR-level rules".

## Zoom-out — B=0 M=4 N=6

The design is mostly the right size. It extends the existing address and header machinery, and `AllCards` is the minimal correct fix.

- **[M] A row action in the Board table jumps back to page 1.** The fix inverts the rule: always send `page`, drop it only when `elt === boardForm() && !refreshing`, and give the sort, toggle and reset fragment URLs an empty `page=` (`board_view.go:117-119,170`). Add a mutation-on-page-2 case.
- **[M] The design reverses parts of D-095 without recording it.** D-095 says the window stays out of the address, the table is server-paged, and the table holds at most 1,000 rows (`decision-log.html:2046-2070`). Add D-097 and the decision-log counts to §4.
- **[M] "Planning does not depend on it" contradicts the BRD value chain** (`brd.html:368,399,773`). Use neutral text, and record the gap as an open question.
- **[M] PR and Auto-fix paging is inconsistent across filter and drawer links.** Use one rule: filter changes drop `Page`, drawer links keep it. List `prview.go:308`.
- [N] The pager carries `Key` and `maxPage`, which it does not need. The Board would build presentation links in the store; the fix is a pure `clampPage` in the store and `paginate` in `boardView`.
- [N] `Locked` belongs on `CWView` (`view.go:381`), not on `WorkflowDraft`, which the store receives.
- [N] One action has two names: "New task" and "Create workflow".
- [N] Agents is a compiled-in roster of 13 (`fixtures.go:35-49`), the same "fixed handful" case as Integrations. Confirm with the operator. The QA heading `{{len .Epics}}` will show the page size instead of the total.
- [N] The test and register change list is incomplete: the grep misses `prMaxRows`, `afMaxRows` and `tableStep`, the rule file is missing from §4, and the count locations are misattributed.
- [N] "if read here" in row 6 is undecided; anchors are stale; the rollup cut is better as an offset window over `RollupGroup`.

## Critic — B=1 M=6 N=5

- **[B] "Prune all" over `AllCards` breaks D-082's "bound to the listed cards."** Prune cannot be undone. There is also no bound on the number of hidden `card` inputs (`handlers_conversation.go:139-150`). The fix is to choose between pruning the current page and listing every card inside the confirmation, record the choice as D-097, and assert it in TC-757.
- **[M] A move on the Board table sends the operator back to page 1.** This contradicts D-095 (4), "in the same view." The fix is to always send `page`, put an empty `page=` on view-changing links, and add a hidden `page` input to the filter form that a change clears.
- **[M] The help text is wrong for several of the states it explains.** Under off, tasks stay in Backlog. Under on, the runner needs an adapter. Step 1: a gated implementation task is BLOCKED at creation (`store_mem.go:2282-2309`). Step 5: the gate hold means waiting; Revise design is only for a review's own verdict; a BR-39 drift block clears on Blueprint sync (`workspace.go:384-405`). Step 6: a design or review task cannot be approved (`board.go:376-381`). The off case is untested.
- **[M] The design changes D-095 without a decision entry.** Update the comment at `board_page.go:12-17`.
- **[M] The PR and Auto-fix tables lose the page on their own links.** `at(n)` is undefined for them, and the drawer `CloseHref` is unspecified.
- **[M] The "Sync record to graph" tooltip is inaccurate.** Inject also writes requirements, diagrams and the Blueprint (`store_arcade.go:1282-1350`; `store_arcade_blueprint.go:277-305`). On the in-memory store it writes nothing. The button is hidden on `blueprint`, which injection writes, and the Graph projection panel still shows on other tabs.
- **[M] The change list is incomplete and one item is undecided.** The rule file is missing, row 6 says "if read here", and the `buildQAPane` signature is missing.
- [N] Several cited line numbers have drifted (`board_page.go:36-37,64-69,189-195,498-502`; `legalMove` at `board.go:209-229`; prune input at `:814`).
- [N] "The test proves it" depends on fixtures the design does not specify.
- [N] The Board pager links have no `hx-sync="#boardFilters:replace"`.
- [N] Accessibility and naming: the `aria-label` on `<summary>` hides the visible text; `title` and `aria-describedby` announce the same text twice; "New task" opens "Create workflow"; a one-page gap renders as "…".
- [N] Paging the Timeline still copies the whole event stream on every click (`handlers_bo.go:358-364`).

## Readiness — B=3 M=9 N=5

- **[B] No phases, exit conditions or verification commands.**
- **[B] A move on table page 3 is answered with page 1.** The current header carries `rows=` (`board_page_test.go:762`, `TestABoardMutationKeepsTheView`). Breaks D-095(4).
- **[B] Deleting the constants breaks tests the design's grep misses.** These are `prview_test.go:124,133,138`, `autofix_view_test.go:400-401`, `board_page_test.go:373-374,430`, and `TestTheTableSortsAndCaps` (`:813`). Say for each whether it is rewritten or retired.
- **[M] Parsing gives `Page=1`, encoding drops it, so the round-trip breaks** (`TestTheBoardQueryRoundTrips`, `board_page_test.go:417-428`). Pick one normal form.
- **[M] Who builds the Board's `Pager` is not stated.**
- **[M] PR and Auto-fix drawer links drop the page, and the tabs keep a stale page.** Parsing belongs in `prPage` and `buildAutoFix`.
- **[M] The QA pane's builder cannot see the page.**
- **[M] D-095's limits are reversed with no new decision.**
- **[M] The QA rule file is edited but not listed in the change list** (highest-in-use line, the range table, the totals line).
- **[M] The branch already changes files the design does not account for** (`AGENTS.md`, `GEMINI.md`, `.claude/**`).
- **[M] The new test files are not named.**
- **[M] "`TestEveryHtmxLinkHasAPageHref` proves it" is not true as written.**
- [N] The pager window is ambiguous when exactly one page is skipped.
- [N] TC-755 cannot test the filter reset, which happens in `live.js`.
- [N] Archive Restore and Prune go back to page 1.
- [N] Pages deliberately not paginated (Products, Inbox) are missing from the non-goals.
- [N] Some line anchors are off by one, and the help panel's position is vague.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| No phases or exit commands | readiness | Blocker | PROPOSE FIX | §9 adds phases P1–P7, each with its `go test -run` selector, and a final gate. |
| A move on page N answers page 1 | readiness / critic / comprehension / zoom-out | Blocker / Major | PROPOSE FIX | `page` now goes on every Board request. It is dropped only on a user filter change (`elt === boardForm() && !refreshing`), and view-changing links carry an empty `page=`. TC-746 is extended to cover a move from page 2. |
| Constants break unlisted tests | readiness | Blocker | PROPOSE FIX | §5 now lists TC-630, TC-710, TC-739, TC-741, TC-746 and TC-747, with the change to each. TC-747 is renamed and rewritten in place. |
| Prune breaks D-082 | critic | Blocker | PROPOSE FIX | The confirmation lists every card it will prune, outside the pager. D-097 amends D-082. TC-757 asserts it. |
| Page normal form | readiness | Major | PROPOSE FIX | 0 means "no page". `pageParam` returns 0 for absent or ≤ 1, and the clamp treats 0 as 1. Round-trip preserved. |
| Who builds the Pager | readiness / comprehension / zoom-out | Major / Nit | PROPOSE FIX | The store calls `clampPage`; `boardView` calls `paginate`. `clean()` keeps `Page`. |
| PR / Auto-fix links | all four | Major | PROPOSE FIX | §3.2.2 lists every link and whether it keeps or drops the page. Parsing is in `prPage` and `afPage`. |
| QA pane signature | readiness / comprehension | Major / Nit | PROPOSE FIX | `buildQAPane(ctx, m, page int)`. |
| D-095 reversed silently | readiness / critic / zoom-out | Major | PROPOSE FIX | D-097 amends D-095 (2) and its accepted limits. The decision-log page and its counts are added to §4. |
| Help text wrong (runner, blocked, gate) | comprehension / critic | Major | PROPOSE FIX | `BoardView.Runner` is off, no-adapter, manual or on. The steps are rewritten against the cited code. TC-760 covers all four states. |
| Inject tooltip contradicts A-6 / inaccurate | comprehension / zoom-out / critic | Major | PROPOSE FIX | The tooltip now says what is written, without claiming anything about planning. `blueprint` counts as a record tab. A-6 and B-4 are cited, and the gap is left open in §6. |
| Rule file missing from change list | readiness / critic / zoom-out / comprehension | Major / Nit | PROPOSE FIX | §4 row 21. |
| Branch changes `.claude/**`, `AGENTS.md`, `GEMINI.md` | readiness | Major | CONTEST | Those were this review's own canaries. They have since been removed, and `git status` shows none of those files. |
| Test files not named | readiness | Major | PROPOSE FIX | §5 gives the file for each test. |
| HtmxLink test proves nothing | readiness / critic / comprehension | Major / Nit | PROPOSE FIX | TC-754 and TC-756 assert the pager `href` shape on a fixture past page 1. |
| `Locked` on `WorkflowDraft` | zoom-out | Nit | PROPOSE FIX | Moved to `CWView`; `model.go` leaves the change list. |
| Two names, one action | zoom-out / critic / comprehension | Nit | PROPOSE FIX | The Board button is "New task", as the operator asked. When the wizard opens locked, its heading reads "New tasks for <product>", with a lede saying the tasks are planned from stories. |
| Agents is a fixed roster | zoom-out | Nit | CONTEST | The operator named `/app/agents` explicitly in the request. |
| QA heading count | zoom-out | Nit | PROPOSE FIX | Uses `.Pager.Total`. |
| Pager `Key`, `maxPage` | zoom-out | Nit | PROPOSE FIX | `Key` is dropped. The `pageParam` cap stays as an input bound (SEC-3). |
| One-page gap | readiness / critic | Nit | PROPOSE FIX | A gap of one page shows the number. TC-753 covers it. |
| TC-755 cannot test live.js | readiness | Nit | PROPOSE FIX | Stated. The JS path is a manual check in §9. |
| Restore resets apage | readiness | Nit | DECLINE | Accepted: a mutation re-renders page 1 of Archive. Carrying two page numbers through restore costs more than it saves. Stated in §6. |
| Products / Inbox not listed | readiness | Nit | PROPOSE FIX | Added to §6. |
| Anchors drift | all | Nit | PROPOSE FIX | Re-anchored. |
| No `hx-sync` on pager | critic | Nit | PROPOSE FIX | Board pager links carry `hx-sync="#boardFilters:replace"`. |
| Accessibility on summary / tooltip | critic | Nit | PROPOSE FIX | No `aria-label` on `<summary>`; `title` only on Inject. |
| Timeline copies every event | critic | Nit | PROPOSE FIX | The cost is stated and accepted in §6. |
| Rollup flatten | zoom-out | Nit | PROPOSE FIX | Now an offset window over `RollupGroup`. |
| Undefined terms | comprehension | Nit | PROPOSE FIX | Glossed. |
| "data table" vs Archive | comprehension | Nit | PROPOSE FIX | §3 now defines which views take 10 and which take 5. |
| TC-754 route | comprehension | Nit | PROPOSE FIX | Route and `HX-Current-URL` named. |
