# top-bar-menu — review round 1, consolidated triage

**Design reviewed:** `docs/designs/top-bar-menu.md` v1 (Draft). **Applied in:** v2.
**Lenses:** comprehension, zoom-out, critic, readiness (`top-bar-menu-{comprehension,zoomout,critic,readiness}-1.md`) and security (`top-bar-menu-security-review-1.md`). v1 declared the security lens not required; the comprehension lens, the critic and the security lens itself found that the back-office criterion applies, so it was run in the same round, with the canaries still planted.

## Isolation

- **Canaries.** One `EGM-MARKER-…` line was planted in each of the seven standing files (`.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/rules/rule-index.md`, `qa-test-plan.md`, `agent-workspace.md`, `frontend.md`) before the run and removed after it. The value pattern from `deploy/check-workspace.sh` finds no canary value in any report or standing file.
- **The CLAUDE.md probe.** Positive control: a `general-purpose` agent on the session's model, asked whether its context holds the sentence starting "Four invariants, and where to read the rest", answered **YES**. (A first attempt on a smaller model answered NO and was discarded as a control that cannot see the file.) The test: a `gaos-goldfish-comprehension` agent answered **NO** before any tool call and **NO** after reading `docs/designs/README.md`.
- The readiness lens reported the harness attaching the three path-scoped rule files when it read under `docs/`; that is the known attach described in the runbook, and it used them only as rubric.
- The zoom-out and critic lenses saw `dashboard/static/app.js` modified in the working tree. That change belonged to another session's v0.69.1 fix (merged as PR #72, `545294d`), not to this feature. v2 is anchored against `545294d` and is implemented in its own worktree.

## Counts

| Lens | Blocker | Major | Nit / Low |
|---|---|---|---|
| comprehension | 0 | 3 | 10 |
| zoom-out | 0 | 3 | 4 |
| critic | 1 | 6 | 6 |
| readiness | 1 | 5 | 5 |
| security | — | Medium 1 | Low 2 |

## Triage

Duplicates across lenses are merged into one row; the lenses that raised it are named.

| # | Finding | Severity | Lenses | Disposition | Note |
|---|---|---|---|---|---|
| 1 | The change list leaves out `deploy/check-workspace.sh` and `README.md`, so the release fails its own validator | Blocker | critic, readiness | PROPOSE FIX | §5 rows 11–12 with the exact lines; a `git grep` completeness check in P3 |
| 2 | §4.5 is wrong: `live.js` background refreshes re-render `#boSheet` and would close an open panel and drop focus | Major | comprehension, critic | PROPOSE FIX | new §4.6: `busy()` and `boardBusy()` hold back while a panel is open; `gaos:menu-closed` runs the held refresh; `live.js` added to §5; §6.3 step 7 |
| 3 | The design waived the security lens on its own authority | Major / Medium | comprehension, critic, security (SEC-1) | PROPOSE FIX | the lens was run; header says required and names the report |
| 4 | The panel is positioned in `toggle`, after its first frame; the JS-off sentence was wrong | Major | comprehension, readiness | PROPOSE FIX | §4.4 item 4 positions in `beforetoggle` from the trigger's rect alone (no panel measurement); the CSS no longer overrides `inset`/`margin`, so JS-off really is centred |
| 5 | The `focusout` close fights the trigger's toggle and drops a Safari click | Major | critic, readiness | PROPOSE FIX | `focusout` rule removed; Tab-out handled in the panel's `keydown`; §6.3 steps 3, 6 |
| 6 | The design rejects `role="menu"` but builds the menu keyboard model; keyboard-open detection has no reset | Major | zoom-out, readiness, comprehension, critic | PROPOSE FIX | arrow/Home/End and auto-focus removed; the native popover Tab order is the keyboard model |
| 7 | `aria-haspopup="menu"` contradicts the decision; static `aria-expanded` overrides the browser and is wrong with JS off | Major | critic, zoom-out, comprehension | PROPOSE FIX | both removed; open-state styling via an `is-open` class the script sets |
| 8 | A non-passive capture `wheel` listener on `document` | Major | critic, zoom-out | PROPOSE FIX | bound to the bar's own `.bo-nav` element; `deltaMode` 2 handled. Zoom-out's "drop the wheel handler" is **DECLINE**: the request was to scroll left-right, and the wheel is the gesture a mouse user tries first (§8 alt. 7) |
| 9 | Goal 2 says both menus fit at 1280 px with no evidence and no failure rule; Setup saves one slot | Major | zoom-out, critic | PROPOSE FIX (goal) / DECLINE (drop Setup) | goal 2 now requires only the global menu to fit, measured in §6.3 step 1, and says what a miss means. Setup stays: the operator chose the grouping including it; recorded in §4.1 for the approval to confirm |
| 10 | No phases, exit conditions or ordering | Major | readiness | PROPOSE FIX | §5 split into P1–P3, each with an exit command |
| 11 | The TC-904/905 messages, register rows and `h3` are not given | Major | readiness, critic | PROPOSE FIX | §6.1 gives every `t.Errorf`/`t.Fatalf`; §5.2 the rows verbatim and the `h3` id and place |
| 12 | Handlers match by class across the whole document; a selector is built from an id string | Low | security (SEC-2) | PROPOSE FIX | `bindBar` binds to the one `.bo-nav`; the trigger is found by comparing attributes; `markdown.go:12-21` cited |
| 13 | No signed-out check for the new markup | Low | security (SEC-3) | PROPOSE FIX | TC-904 step 6; §4.2 requires every panel to stay inside `{{if .BO}}` |
| 14 | Line anchors off by 1–4 lines; wrong test name | Nit | all four | PROPOSE FIX | re-anchored against `545294d`; `TestStaticAssetsAreVersionedByTheProductVersion` |
| 15 | The ellipsis sentence contradicts itself | Nit | zoom-out, comprehension, critic | PROPOSE FIX | "No truncation", with the widest row's width |
| 16 | `.info-pop` not reused or mentioned | Nit | zoom-out, critic | PROPOSE FIX | `.bo-pop` takes the `.info-pop` surface and motion (§4.3, §4.5) |
| 17 | "Unchanged: `syncMenu`" introduces a change; `data-more` removal on the wrapper unstated | Nit | comprehension | PROPOSE FIX | §4.4 items 1 and 7 |
| 18 | `aria-label` on a generic `div`; `aria-hidden` arrow buttons can take focus | Nit | critic | PROPOSE FIX | panel `role="group"`; arrows are spans |
| 19 | Reference to a plan the reader cannot see; skills named without paths | Nit | comprehension | PROPOSE FIX | reference removed; skills glossed in §0 with paths |
| 20 | Where the product-mode hidden `navNeedsCount` span goes | Nit | comprehension | PROPOSE FIX | stays a direct child of the header (§4.1) |
| 21 | Wheel over an arrow; page-mode deltas | Nit | comprehension | PROPOSE FIX | listener on `.bo-nav`, so arrows are covered; `deltaMode` 2 → `clientWidth` |
| 22 | Test assertions imprecise: panel slice, `./` href, the needs chip's `hx-get` | Nit | readiness | PROPOSE FIX | the `panel()` helper defined; 11 literal `href … hx-get` pairs |
| 23 | `toc/product.js` phrases and changelog text not given | Nit | readiness | PROPOSE FIX | §5 row 10 and §5.1 |
| 24 | The D-108 question left open | Nit | readiness | PROPOSE FIX | settled: no decision (§7) |
| 25 | §6.2 commands assume the wrong directory | Nit | readiness | PROPOSE FIX | subshells |

No finding was contested. No severity was changed.

## Next

No Blocker survives v2. Round 2 is not required by the policy; the operator decides whether to run one before approving.
