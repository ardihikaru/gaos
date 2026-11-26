# Review 2 — `board-and-list-ux.md`

Round 2 re-ran three lenses, each with zero context: readiness, critic and comprehension. These are the lenses that raised round 1's Blockers, plus the lens that raised the help-text Majors. Each reviewer got the design path, the round-1 report paths and the rubric path list, and was told to open nothing under `.claude/`.

- **Canaries.** New canaries went into the seven standing files and were removed after the run. No canary value appears in any report.
- **Rule files attached by the harness.** Two lenses report that the harness attached three `.claude/rules/` files when they read under `docs/`/`pustaka/`. This is the known path-scoped attach.
- **`git status`.** The readiness lens saw the canary edits in `git status` and did not read them.
- **Positive control.** Round 1's control stands. The method has not changed.

Every round-1 Blocker is confirmed resolved by the lens that raised it: phases, the move-resets-page bug, the unlisted constant tests, and prune versus D-082.

## Readiness — B=1 M=6 N=8

- **[B] The final gate cannot pass.** `deploy/check-workspace.sh:114-118,129-131` pins "six hundred and sixteen"/`616`, "seven hundred and fifty-two" and "Ninety-five entries", and checks `README.md` (`:147-149`). `README.md:12,137,139` say 0.58.0, 616 and 752. Neither file is in §4.
- **[M] The `?v=` bump misses `app.js`** (`page.html:50`). `external_inbox_test.go:898-912` requires all three files to carry the version.
- **[M] `buildQAPane`'s new signature breaks `qaclient_test.go:309,321`.**
- **[M] The QA pager's `at(n)` is unspecified.** `buildQAPane` has no slug.
- **[M] Phases schedule shared files after the phases that need them.** This affects `view.go`, the `backoffice.html` slices for P4/P5, and the `QAPane`/`ArchivePage` file attribution.
- **[M] TC-759 expects the picker for an unknown product.** Today `buildCW` fails on one (`handlers_cw.go:128-132`).
- **[M] PR row hrefs are built before the cut** (`prview.go:281,308` vs `:340`), so they carry the unclamped page.
- [N] Archive's pagers are placed on `BOView` in one section and on `ArchivePage` in another.
- [N] The list pages do not write the clamped page back.
- [N] TC-761 matches the topbar's "Create workflow".
- [N] The locked wizard's name source and the step-1 hint are not pinned.
- [N] Anchors drift: `clean` `:349`; `urls.go:258-273,278-285`; `board_page_test.go:442`.
- [N] Row 21 misses the rule file's "Ninety-five entries".
- [N] TC-756's fixture is unstated.
- [N] The prune list has no bound.

## Critic — B=1 M=4 N=10

- **[B] The same `check-workspace.sh`/`README.md` finding.** Editing the script is itself a validation-script change.
- **[M] `?v=` misses `app.js`.**
- **[M] `buildQAPane` breaks `qaclient_test.go`, and its links need the slug.**
- **[M] TC-759 expects behaviour the code does not have.**
- **[M] The prune confirmation lists every archived card on every Archive render, with no limit.** Archive gets no payload benefit from paging, the response grows without bound, and consent becomes a wall of text. A cap or a lazy fetch is needed, recorded in D-097.
- [N] The Board refresh is driven by SSE hints, not a 30 s timer, so the manual step 2 needs a provoked refresh.
- [N] Help step 4 omits that an unanswered question expires to Blocked (`questions.go:245`).
- [N] The Sync tooltip still claims a graph write on the in-memory store.
- [N] Anchors and file attribution: `clean`, `board_view.go:107-112`, `urls.go`, `QAPane` and `ArchivePage`.
- [N] The clamped-page rule is broken by PR row hrefs and by the `BOView` raw page.
- [N] Offset paging over live data is not listed as a risk.
- [N] PR and Auto-fix pager clicks can race the 30 s poll.
- [N] Paging replaces history, so Back does not step through pages.
- [N] Two `<nav aria-label="Pages">` on Archive; no overflow rule on the locked heading.
- [N] TC-755 tests nothing beyond TC-754 and TC-746.

## Comprehension — B=0 M=5 N=9

The paraphrase holds, and every section could be restated.

- **[M] `qaclient_test.go` breaks.**
- **[M] The empty `page=` on view-changing links breaks assertions** at `board_page_test.go:798` (TC-746) and `:852` (TC-747), which the design says stay. The position of `page=` is not fixed.
- **[M] An unknown product under lock** (same as readiness).
- **[M] The Sync tooltip is untrue on the default (memory) store** (`main.go:160-161`).
- **[M] The help panel names controls that do not exist.** The move buttons render `→ working`, `→ done`, `→ ready` (`backoffice.html:509,540`). There is no Approve, Redo or Retry.
- [N] PR row links are built before the clamp, and `autofix_view.go:552` must move.
- [N] `From`/`To` mean two things in §3.1.
- [N] Where Archive's pagers live.
- [N] Anchors.
- [N] TC-756, TC-757 and TC-761 are under-specified: route, header and fixture.
- [N] Locked step 1: the hint and its `aria-describedby`, and the value source.
- [N] One action still has two names (the topbar, and the step-3 `NextLabel`).
- [N] Glosses needed for: design gate, Blueprint, drift, and the bare IDs.
- [N] The live.js exception depends on statement order; a search counts as a filter change.

## Triage

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| `check-workspace.sh` / `README.md` pin the counts | readiness, critic | Blocker | PROPOSE FIX | §4 adds rows 24 and 25 in P7. The script's pinned literals change the way every count change has changed them. Its register rows (TC-357/358/380/381/437/510) quote no count, so their prose does not move. |
| `?v=` misses `app.js` | readiness, critic | Major | PROPOSE FIX | Row 17: `page.html:48,50,51` → 0.59.0. `external_inbox_test.go:898-912` is named. |
| `qaclient_test.go` breaks; QA `at(n)` | all three | Major | PROPOSE FIX | `buildQAPane`'s signature is **unchanged**. The call site at `handlers_bo.go:348` slices `d.QA.Epics` and builds `d.QA.Pager`, using `d.Slug`. `qaclient_test.go` is not touched. |
| Phases out of order | readiness | Major | PROPOSE FIX | §9 gives each phase its slice of `view.go`, `handlers_qa.go`, `handlers_conversation.go` and `backoffice.html`. Row 11 is corrected. |
| TC-759 versus an unknown product | all three | Major | PROPOSE FIX | `handleCWOpen` and `handleCWStep` clear an unknown `product` and the lock before `buildCW`, so the unlocked picker renders. Listed in row 12. |
| PR row hrefs before the cut | readiness, critic, comprehension | Major / Nit | PROPOSE FIX | PR row hrefs are assigned in a loop over the page's rows after the cut. `autofix_view.go:552` moves after the clamp. TC-758 adds `page=99`. |
| Prune list unbounded | critic, readiness | Major / Nit | PROPOSE FIX | One prune takes at most `pruneBatch = 100` cards, the oldest first, and lists exactly those. With more archived, the button reads "Prune the 100 oldest of N". Recorded in D-097. |
| Sync tooltip untrue on the memory store | comprehension, critic | Major / Nit | PROPOSE FIX | The text is now true for both stores: "Projects this record … into the graph database (ArcadeDB); without one, it records the injection and rescores readiness." |
| Help names controls that do not exist | comprehension | Major | PROPOSE FIX | The steps quote the rendered labels: `→ working`, `→ done`, `→ ready`, `Revise design`, the `Needs you` badge. |
| Empty `page=` breaks `:798`, `:852` | comprehension | Major | PROPOSE FIX | `page=` is appended last. `:798` is listed under TC-746 and `:852` under TC-747 as changed assertions. |
| Archive pager placement | all | Nit | PROPOSE FIX | `ArchivePage.ArchivedPager` and `PrunedPager`. `BOView.Pager` is for rollup and agents only. `Pager2` is dropped. |
| Clamped page write-back on list pages | readiness, critic | Nit | PROPOSE FIX | `bo.Page = pager.Page` (0 when 1); likewise `APage` and `PPage`. TC-756 adds `page=99`. |
| TC-761 / TC-756 / TC-757 specification | all | Nit | PROPOSE FIX | Routes, headers and fixtures are named. |
| Locked name source and hint | readiness, comprehension | Nit | PROPOSE FIX | `CWView.LockedName` comes from the selected `Pick`. When locked, the new sentence replaces `.Hint` in `#cwProductHelp`. `NextLabel` becomes "Create tasks". |
| Anchors | all | Nit | PROPOSE FIX | Re-anchored. |
| Row 21 "Ninety-five entries" | readiness | Nit | PROPOSE FIX | Added. |
| Refresh is hint-driven | critic | Nit | PROPOSE FIX | Gloss corrected. The manual check provokes a refresh from a second tab. |
| Question expiry | critic | Nit | PROPOSE FIX | Added to step 4. |
| Offset paging over live data | critic | Nit | PROPOSE FIX | Accepted, with a row added to §8. |
| PR / Auto-fix race with the poll | critic | Nit | PROPOSE FIX | `Sync` is `#prsLive:replace` / `#afLive:replace`. |
| Back does not step pages | critic | Nit | PROPOSE FIX | Stated as intended in §6. Pager links replace the address, the same as filters. |
| Two `nav` landmarks; heading overflow | critic | Nit | PROPOSE FIX | `Pager.Label`; `#cwTitle` wraps. |
| TC-755 redundant | critic | Nit | PROPOSE FIX | Re-scoped to `TestTheBoardPageClampsAfterAMove`: a move that empties the last page answers the clamped page. |
| `From`/`To` two meanings | comprehension | Nit | PROPOSE FIX | `clampPage` returns `lo, hi`. `sliceFor` is `xs[p.From-1:p.To]`. |
| Glosses | comprehension | Nit | PROPOSE FIX | Terms block extended. |
| live.js order; search counts | comprehension | Nit | PROPOSE FIX | Stated. |
| Topbar still says Create workflow | comprehension | Nit | DECLINE | The topbar entry is the tenant-wide, unlocked wizard, and "Create workflow" is accurate there. The Board's own entry is "New task", as the operator asked. |
