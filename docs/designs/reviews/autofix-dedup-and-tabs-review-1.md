# autofix-dedup-and-tabs — review round 1 (v1)

- **Target:** `docs/designs/autofix-dedup-and-tabs.md` v1, on `main` at `cf78d6c`.
- **Lenses:** comprehension, zoom-out, critic and readiness, each a zero-context `gaos-goldfish-*` agent given only the
  document path and the rubric paths. The security report is `autofix-dedup-and-tabs-security-review-1.md`.
- **Canaries:** one `EGM-MARKER-…` line was planted in each of the seven standing files (`.claude/CLAUDE.md`,
  `AGENTS.md`, `GEMINI.md`, `.claude/rules/rule-index.md`, `qa-test-plan.md`, `agent-workspace.md`, `frontend.md`)
  before the run. They were removed after it with `git checkout`. No report quotes a canary value. Every lens said the
  harness attached the path-scoped rule files when it read under `docs/` or `pustaka/`, which is the known attach;
  none opened anything under `.claude/`.
- **Positive control:** not re-run this round. The probe verified on 2026-11-25 still stands (`omitClaudeMd`, the
  runbook's section 4).
- The reports are transcribed by the author from the agents' final messages. They are condensed to the finding,
  its evidence and its recommendation. Severities are unchanged.

## Comprehension (B=1 M=8 N=9)

| # | Finding | Severity |
|---|---|---|
| C1 | D-108 contradicts BR-61 (`brd.html:1308`, "never closes a pull request"), and the header says no BR is touched | Blocker |
| C2 | The resume path does not re-enter `run` after `OpenFixPR`: `resumeFixPRs` re-runs only `preparing` (`autofixpr.go:469-472`) | Major |
| C3 | §4.3 says a close never touches the live record; §5.3 says rule X does; the X event list is incomplete | Major |
| C4 | The poller's fallback for D and X is described wrongly: the closed path (`fixprpoll.go:487-508`) checks `fixing`, not `d.FixPR == cur.ID` | Major |
| C5 | A failed retarget close then blocks the next retarget in a chain, through the §4.4 guard | Major |
| C6 | Many `file:line` anchors do not match `cf78d6c` (`autofixpr.go` 379/384-388/410-413/558; `backoffice.html` 1351, 1417-1430, 1424, 1437, 1483, 1566; `cancelDefectCardsLocked` at `defects.go:382`) | Major |
| C7 | What the close route renders is undecided (`handleRetarget` renders `prs`, `handlers_prs.go:125`) | Major |
| C8 | The `.af-filters` selector also reaches the Board (`backoffice.html:472`) and Traffic (`:1732`) | Major |
| C9 | Where the 50-finding cap applies is ambiguous; step 5 could 409 a real duplicate | Major |
| C10 | "Appears in at most one finding" is unclear for the kept side | Nit |
| C11 | `closeSuperseded` inputs (`master`, `old.Repo`, `ctx`, placement) are unspecified | Nit |
| C12 | `CloseFixPR` cannot build its "superseded by" text from its arguments | Nit |
| C13 | The Auto-fix docs page is "if it exists", but it does (`pustaka/docs/integrations/autofix.html`) | Nit |
| C14 | CSRF handling is asserted, not anchored | Nit |
| C15 | `BlockDuplicate` has no home file | Nit |
| C16 | The guard runs after the Git writes, and the reason is not given | Nit |
| C17 | The product count is ambiguous with the cap of 30 (`autofix_view.go:609-611`) | Nit |
| C18 | Rule X's "oldest" has no ordering key | Nit |

## Zoom-out (B=0 M=3 N=7)

| # | Finding | Severity |
|---|---|---|
| Z1 | Rule D and the §4.4 guard both defend a path the document says does not exist, and D's fallback is harmful | Major |
| Z2 | Rule X is speculative, and it is the only reason `CloseFixPR` has to close a defect (it re-implements `CloseDefect`) | Major |
| Z3 | The claim that resume makes `closeSuperseded` idempotent is false | Major |
| Z4 | The `prCloser` interface and `closableFix` duplicate what already exists: use `*ghWriter`, and let retarget reuse the guard | Nit |
| Z5 | The `dups=1` toggle threads a fourth piece of state through every link; the cap interacts with step 5 | Nit |
| Z6 | The superseded comment says GAOS closes the pull request before it has tried | Nit |
| Z7 | The scroll selector reaches more pages than §3 says | Nit |
| Z8 | The scroll fix is a one-line bug fix bundled into a feature design | Nit |
| Z9 | Several anchors are wrong at `cf78d6c` | Nit |
| Z10 | The decision-log count target is not stated ("One hundred and seven") | Nit |

## Critic (B=1 M=10 N=6)

| # | Finding | Severity |
|---|---|---|
| K1 | BR-61 is neither named nor amended. The promise also appears in `pull-requests.html:91`, `reviewdefect.go:22`, `githubwrite.go:155-159` and `autofixpr.go:16-20` | Blocker |
| K2 | Rule X can keep a dead defect's pull request and close the live defect (a superseding `xfail`, `defects.go:472-480`) | Major |
| K3 | Closing a `fail` defect by rule X makes it reopen as a held regression on every QA run (`defects.go:488-497`) | Major |
| K4 | "Only a retarget makes a second draft" is false: a reopened unit defect (`unitdefect.go:381-396`), an operator close and a chained retarget all do. The guard reverses D-105 (5) | Major |
| K5 | The resume path does not reach `closeSuperseded` after a crash | Major |
| K6 | After a close, the sheet loses the query (tab, `dups`, filters) | Major |
| K7 | The filter form sits inside the PR block, which the other tabs hide | Major |
| K8 | The active tab would not be styled: `.af-seg` selects `aria-current="true"` (`app.css:1902`) | Major |
| K9 | Records the poller no longer reads (`fixPRPollLifetime`, `fixprpoll.go:89-92`) give findings that cannot be cleared; GitHub's 4xx is reported as "cannot be reached" | Major |
| K10 | The change list misses the register counts in `.claude/rules/qa-test-plan.md` and the docs page `autofix.html` | Major |
| K11 | Many anchors are wrong; the §4.4 guard runs after the blob, tree, commit and ref | Major |
| K12 | The scroll fix reaches the Board and Traffic | Nit |
| K13 | "Tab" already means a state link (`AFTab`, `StateTabs`) | Nit |
| K14 | The superseded comment precedes the close | Nit |
| K15 | `closeSuperseded` details are open (`old.Repo`, `Master`, the text source, `Result` contents) | Nit |
| K16 | Rule-table determinism: lexical ID tie-break, kept versus closed, the cap, the orphan successor | Nit |
| K17 | `prCloser` is wired inside the `SetPRHooks` branch (`main.go:489-494`) | Nit |
| K18 | There are no `WaitingMore` test assertions; name the tests that really break | Nit |

## Readiness (B=1 M=9 N=9)

| # | Finding | Severity |
|---|---|---|
| R1 | BR-61 is not amended | Blocker |
| R2 | There is no exact file list. Missing: `store.go`, the store file, the constants, the template, CSS, `page.html`, `main.go`, the fake's PATCH (`fixpr_fake_test.go:244-245` answers 404), the test files, `toc.js`, the registers, `brd.html` | Major |
| R3 | There are no phases, exit conditions or verification commands | Major |
| R4 | Where the filter form lives once one tab renders is undecided | Major |
| R5 | The close route's success and late-failure answers are not specified. `retargetFail` renders `empty` with a toast and does not write JSON (`handlers_prs.go:20-25`). `s.mut(r)` is not named | Major |
| R6 | Rule D's "newest by `CreatedAt`" is not necessarily `d.FixPR`; the poller fallback breaks the defect | Major |
| R7 | `closeSuperseded` lacks its inputs and its order; the comment precedes the close | Major |
| R8 | The active tab gets no highlight (`aria-current` value) | Major |
| R9 | The changes to existing tests and register rows are not listed (TC-716, `retarget_test.go:353`, `qa-test-plan.html:807`) | Major |
| R10 | The duplicate count's scope, cap and persistence (`dups` on the poll) are undefined | Major |
| R11 | Line anchors have drifted | Nit |
| R12 | Wait paging duplicates `listPageSize` (`pager.go:16`) and omits the clamp write-back | Nit |
| R13 | `afTrigger`'s inputs reach `afRow` by an unstated route | Nit |
| R14 | The product count is ambiguous with the cap of 30 | Nit |
| R15 | A conditional docs target that is already resolvable | Nit |
| R16 | The name sits close to the existing `AFTab` type | Nit |
| R17 | "Looks only at open records" contradicts rule S's merged successor | Nit |
| R18 | The guard runs after the Git writes | Nit |
| R19 | The `prCloser` placement ignores the store-hooks block | Nit |

## Triage (the author)

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| C1, K1, R1 (BR-61) | Blocker | PROPOSE FIX | BR-61 is amended through D-108, which also amends D-102. Every repeated promise is in the §8 change list. v2 header, §9 |
| C2, Z3, K5 (resume) | Major | PROPOSE FIX | The false claim is removed. A boot sweep over replaced open records is added (§4.4) |
| C3, Z2, K2, K3 (rule X) | Major | PROPOSE FIX | Rule X is removed. `CloseFixPR` never touches the defect. Escalated as a future design (§12) |
| C4, R6 (poller fallback) | Major | PROPOSE FIX | The poller's closed path requires `d.FixPR == cur.ID` (§4.5). D keeps `d.FixPR` when open (§5.1). TC-910 |
| C5, Z1, K4, C15, C16, R18 (the §4.4 guard) | Major | PROPOSE FIX | The guard is removed. Its cases are shown by rule D instead (§3) |
| Z1 (rule D) | Major | DECLINE (keep D) | D costs one map pass and shows the unit-reopen and operator-close cases K4 found. After §4.5 its close cannot move the defect |
| C6, Z9, K11, R11 (anchors) | Major | PROPOSE FIX | Re-anchored against `cf78d6c` |
| C7, K6, R5 (the route's answer) | Major | PROPOSE FIX | `renderBOView(…, "autofix")` with the posted filter. A step for every outcome. `s.mut(r)`. The JSON claim is corrected (§5.3) |
| C8, Z7, K12 (selector reach) | Major | PROPOSE FIX | Scoped by id: `#afFilters, #prsFilters, .af-sections, .af-dups` (§7.3) |
| C9, Z5, R10, K16 (cap, `dups`) | Major | PROPOSE FIX | No cap and no toggle. The panel renders whenever there is a finding. The full list is used everywhere (§5.1, §5.2) |
| K7, R4 (the form's place) | Major | PROPOSE FIX | The form moves above the section nav; the state links stay in the PR section (§7.2) |
| K8, R8 (active style) | Major | PROPOSE FIX | `aria-current="true"` (§7.2) |
| K9 (stale records) | Major | PROPOSE FIX | Read the state first, then record "already merged/closed" without a PATCH. The errors are mapped by class (§4.3, §5.3) |
| K10, R2 (file list, counts) | Major | PROPOSE FIX | §8 Files, with `.claude/rules/qa-test-plan.md` and `autofix.html` named |
| R3 (phases) | Major | PROPOSE FIX | §11 |
| R7, C11, K15 (inputs) | Major | PROPOSE FIX | `closeSuperseded` is fully specified (§4.2) |
| R9, K18 (existing tests) | Major | PROPOSE FIX | TC-716 and the grep for the rendering tests (§10) |
| Z4, K17, R19 (`prCloser`) | Nit | PROPOSE FIX | `server.fixWriter *ghWriter`, set in the `ghw != nil` block. `closableFix` is shared with the retarget |
| Z6, K14 (the comment) | Nit | PROPOSE FIX | The comment is posted after the close, with the sentence chosen by the outcome |
| Z8 (bundled scroll fix) | Nit | DECLINE | One line, the same release, and the operator asked for all of it together |
| Z10 (count target) | Nit | PROPOSE FIX | "One hundred and seven" |
| C10, C18, R17 (rule wording) | Nit | PROPOSE FIX | The input is every record, the Close side is open only, and the ordering key is stated |
| C12 | Nit | PROPOSE FIX | `RetargetedTo` is read under the lock |
| C13, R15 | Nit | PROPOSE FIX | `autofix.html` is named |
| C14 | Nit | PROPOSE FIX | `auth.go:612-660` and SameSite are cited |
| C17, R14 | Nit | PROPOSE FIX | `len(Products)+ProductsMore` |
| K13, R16 (naming) | Nit | PROPOSE FIX | The term is "section": `AFFilter.Section`, `afSection`, `.af-sections` |
| R12 | Nit | PROPOSE FIX | `listPageSize` and the write-back |
| R13 | Nit | PROPOSE FIX | The trigger is set at the call site (`autofix_view.go:634`) |
