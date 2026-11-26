<!-- Round 2, lens: critic. Zero context; canaries planted and removed; no canary value in this report. -->

# Goldfish review, round 2: `docs/designs/integrations-status-chips.md`

The design's thesis is that the chips are a pure presentation layer over state the page already has, so they cost nothing and cannot mislead. Round 2 fixes the round-1 information loss. Three problems remain:

- **The release plan still breaks a pinned test and the static cache.** The same kind of omission caused round-1 Blocker 2.
- **The rules still show green in places the code knows are failing.** The pull-request roll-up is green while the flow's only event source is off.
- **One amber rule can never clear.** The QA chip stays amber permanently after a single historical give-up.

## Round-1 Blockers

| Round-1 Blocker | Status |
|---|---|
| Removing the heading suffix deletes the only display of three states; TC-512 breaks (all four lenses) | **Resolved.** §3.3b adds State rows. `p.API.State = "disabled (" + whyNoToken + ")"` (`dashboard/integrations.go:172`) is printed by the new Machine API row, so `integrations_test.go:182` still matches. The QA reason stays at `backoffice.html:935` and the ai-manager adapter at `:856`. |
| Change list omits `check-workspace.sh` and `README.md` pins (critic) | **Resolved for the files it named.** I checked every literal in §5 against the repository (`check-workspace.sh:114-115,118,129-131`, `README.md:12,137,139`, `product.js:129,162,168,170`, `decision-log.html:18`, `qa-test-plan.html:19,79`, `toc.js:22`). **The same defect comes back** with `page.html` (see the Blocker below). |
| "Mandatory" contradicts BR-27, no decision recorded (critic) | **Partly resolved.** D-097 defines the term, and the BR-27 conflict is gone. The second half of the round-1 recommendation is unanswered: one operator's classification is still hard-coded in the open repository. Carried forward as a Major below. |

---

### The version bump leaves `page.html` behind, so a test fails and prod keeps serving the old CSS - Severity: Blocker
- Affected scope: §5 literal table, §6 change list (#9 bumps `toc.js` semver), §7 phases 2–3
- Finding and rationale:
  - `dashboard/templates/page.html:48,50,51` load `app.css`, `app.js` and `live.js` with `?v=0.58.1`. The comment at `:46-47` says: "Bump it with product.semver whenever app.css, app.js or live.js change."
  - `TestStaticAssetsAreVersionedByTheProductVersion` (`dashboard/external_inbox_test.go:893-915`) fails when any `?v=` differs from `toc.js`'s semver.
  - Static files are cached for a week (`dashboard/main.go:605-614`). Without the bump, browsers keep the old `app.css`. The new `st-*`, `.info-btn` and `.info-pop` rules then do not apply in prod until the cache expires.
  - §6 calls itself "complete" and does not list `page.html`.
  - §7 cannot catch this. Phase 2 runs `go test` before phase 3 bumps the semver, and phase 3 runs only pustaka's tests and the shell checks.
- Recommendation:
  - Add `dashboard/templates/page.html` (three `?v=0.58.1` → `0.59.0`) to §5 and §6, making fourteen product files.
  - Make phase 3 re-run `cd dashboard && go test ./...` after the register edits.
  - Before calling the list complete, grep the repository for the old version string (`0.58.1`) and the old count words.

### The pull-request roll-up ignores the reader, which is the flow's only source of PR events - Severity: Major
- Affected scope: §3.2 `PRChip` rules; §3.3e `ip-btc` and `ip-pr` texts
- Finding and rationale:
  - `flow.handle` is fed only by `btCons.Drain` (`dashboard/main.go:462`).
  - `github.go:21` says "the GitHub webhook belongs to Brain Town", and `braintown_consumer.go:23-24` says the same. GAOS has no GitHub webhook of its own.
  - So with the reader disabled, unusable or failing, no `pr.opened` or `pr.merged` event ever reaches the flow. Yet GitHub on, staging enabled and WhatsApp enabled roll up to green "Connected".
  - The `ip-btc` text says the reader starts the flow "without a GitHub webhook". That implies a webhook alternative exists, and none does.
  - Its "when disconnected" text ("GAOS does not hear about pull requests from the town") understates the effect: GAOS hears about no pull request at all, and only the staging callback still works.
- Recommendation:
  - Make the reader chip an input to `PRChip`: reader not `ok` and GitHub on → Partly connected · warn, at least.
  - Rewrite `ip-btc`: "GAOS's only source of pull-request events. When disconnected, no review starts; only the staging callback still creates end-to-end cards."
  - Add a TC-753 negative: reader Disabled + GitHub on is never `Connected`.

### The QA "Degraded" signal counts gave-ups over all history, so it never clears - Severity: Major
- Affected scope: §3.2 `QAChip` (`GaveUp > 0` → Degraded · warn)
- Finding and rationale:
  - `p.QA.GaveUp` counts every `gave-up` delivery event in the whole stream (`dashboard/integrations.go:124-141`).
  - Events are kept indefinitely (BR-04, cited in BR-57 at `brd.html:1351`).
  - So one give-up, ever, turns the Mandatory QA chip amber for the life of the deployment, and no operator action can return it to green.
  - This produces the alarm fatigue that §10 says §3.2 handles: amber stops meaning "working with a known problem" (§2) and comes to mean "something once failed".
- Recommendation: base the amber on something that can clear, for example a gave-up within a stated window (such as the last 24 h, compared with `e.At`), or `Failing` alone. State the choice in §3.2 and add a TC-753 case for an old gave-up.

### "Degraded" is applied to some known failures and not to others; the triage overstates the fix - Severity: Major
- Affected scope: §3.2; §10 row "Green chips claim unchecked health … PROPOSE FIX"
- Finding and rationale: The design's own rule is that "a green chip does not claim a delivery path that is failing" (§3.2). It applies that rule to QA only. Signals the code already has are ignored:
  - **Brain Town reader:** it records `lastErr` ("unreachable", "the stream answers 401") at `braintown_consumer.go:285,386,391`, but `Status()` (`:308-330`) does not expose it. A reader whose token was revoked shows green "Enabled".
  - **Brain Town emitter:** `Failed`, `Dropped` and `Last` (`braintown.go:605-606`, `:547-548`) are ignored.
  - **Machine API:** enabled with a QA token but an empty `GAOS_API_QA_PRODUCTS` is green, although BR-52 says "an unset list reaches nothing" (`brd.html:1347`). The rule only checks `API.QA`.
  - **ai-manager:** green "Connected" even when every provider reads "not available" (`integrations.go:107-113`).

  The round-1 finding named ai-manager and the reader explicitly. §10 marks the finding PROPOSE FIX, but neither bullet was addressed or contested. That is a silent partial disposition, which the rubric does not allow (`docs/rules/gaos-design-review.md:74-84`).
- Recommendation: Either extend the rules (reader `lastErr` → warn, which needs a `BrainTownRow` field and `braintown_consumer.go` in the change list and the scope line; API with `len(QAProducts)==0` → warn), or split the §10 row and give each un-fixed bullet its own CONTEST or DECLINE with a reason.

### D-097 still encodes one deployment's classification in the open product - Severity: Major
- Affected scope: §2 D-097; §3.3e `info-req` texts
- Finding and rationale:
  - The decision log describes the QA tools as "a separate repository" (`decision-log.html:1443`). Their buttons "stay inert until the QA tools implement their side" (`:1527`).
  - D-097 tells every installer of the open repository that the QA tools and the Machine API are "Mandatory: the workflow this product runs needs it". That is false for an install with no QA tools, and BR-27's spirit (`brd.html:1280`, "absence degrades a capability") still argues against it.
  - Round 1 asked for the decision to "cover why a constant in the open repo encodes one operator's deployment". D-097 does not, and it does not cite or reconcile D-072 or D-075.
- Recommendation: Write into D-097 that the classification is this deployment's, citing D-072 and D-075. Alternatively, reword the chip and the sentence to "Needed for the QA loop" or "Core workflow", which is true for every installer.

### `.integ-h h4{margin:0}` loses the cascade to `.block h4` - Severity: Nit
- Affected scope: §3.4 (rules "appended after `app.css:317-326`")
- Finding and rationale: `.block h4{margin:0 0 10px}` (`app.css:860-863`) has the same specificity as `.integ-h h4` and comes later in the file. It wins, so the heading keeps a 10 px bottom margin inside a centred flex row and sits off-line from its chips.
- Recommendation: Use `.block .integ-h h4{margin:0}`, or append the rules after `:864`.

### The red chip misses the design's own contrast bar in the dark theme - Severity: Nit
- Affected scope: §3.4 "Contrast"
- Finding and rationale: `--fail` `#C86458` on the block's `--panel` `#1C1A16` (`app.css:11,31,859`) is about 4.48:1 for 11 px text (`.chip`, `:318`). That is just under the 4.5:1 the design applies to `--ink-3`.
- Recommendation: State the measured ratios for `st-ok`, `st-warn` and `st-bad` in both themes, and lighten or bolden `st-bad` in the dark theme.

### §3.5 describes the swap wrongly - Severity: Nit
- Affected scope: §3.5 "htmx swaps the whole view into the `#sink` element (`page.html:83`)"
- Finding and rationale: `page.html:79-83` says `#sink` is hidden and "returns nothing for it: all real updates arrive as hx-swap-oob elements". The view arrives as the out-of-band `#boSheet` (`backoffice.html:46`). Round 1's anchor fix replaced one wrong claim with another. The conclusion, that no re-initialisation is needed, still holds.
- Recommendation: Say "the view arrives as the out-of-band `#boSheet` (`backoffice.html:46`)".

### TC-754's State-row check cannot fail for two of the three rows - Severity: Nit
- Affected scope: §4 TC-754
- Finding and rationale:
  - In `newIntegrationsFixture`, `srv.brain` and `srv.btConsumer` are nil and `brainOff`/`btConsumerOff` are zero values (`integrations.go:189-198`, `main.go:105-108`). Both states are `""`, so "prints the full state string" passes vacuously.
  - The negative "no `<h4>` contains `&middot;` followed by a state word" does not define "state word".
- Recommendation: Set `brainOff` to `{State:"unusable: x"}` and `btConsumerOff` to `{State:"disabled"}` in the test, and assert those exact strings. Replace "state word" with the explicit list.

### The register mechanics are under-specified and diverge from the register's convention - Severity: Nit
- Affected scope: §4 last paragraph; §5 "one `h3` per test"
- Finding and rationale:
  - The register groups cases under one `h3`, with one `<p><strong>TC-n…</strong>` per case, and quotes format strings that include verbs (`qa-test-plan.html:4425-4442`).
  - "One `h3` per test" breaks that convention, and the design gives neither the `h3` ids nor where they go.
  - Requiring every `t.Errorf` to be a fixed string makes the table-driven TC-753 unable to report which row failed.
  - The D-097 row's text and evidence cells, and the changelog entry, are not written out (contract rule 6).
- Recommendation: Use one `h3` (for example `id="go-integ-chips"`, after `#go-board-page`) with four `<p>` entries. Allow `%q` and `%s` in messages. Draft the D-097 row.

### The `ip-aim` text is narrower than the code - Severity: Nit
- Affected scope: §3.3e `ip-aim`
- Finding and rationale: The board may dispatch with the runner `on` or `manual` (`runner.go:64-67`), not only `manual`. "Agents can take turns" is false with the runner `off`.
- Recommendation: "With the runner on or manual, agents take turns and the board can dispatch."

### An unconfigured optional sub-part reads as a "known problem" - Severity: Nit
- Affected scope: §2 tone definitions vs §3.2 `PRChip`
- Finding and rationale: WhatsApp or staging left unconfigured on purpose makes the Pull-request flow amber "Partly connected" permanently. §2 defines amber as "working with a known problem", and a deliberate choice is not a problem.
- Recommendation: Either amend §2's definition of amber, or treat a plain `disabled` sub-part as neutral in the roll-up.

### One §8 anchor points outside this repository - Severity: Nit
- Affected scope: §8, brain-town `backend/internal/admin/consumer.go:121-142`
- Finding and rationale: This anchor is in another repository and cannot be checked here. Contract rule 2 says an unanchored claim must be labelled as an assumption.
- Recommendation: Label it as external and name the brain-town commit it was read at.

**Confirmed:**
- The §1 anchors (`backoffice.html:853,867,879,891,932,945`), the `view.go` and `integrations.go` line ranges, and `api_v1.go:255-261` / `:415-417`, `staging.go:196`, `braintown.go:370-372` and `runner.go:52-67` all match.
- The app.css variables and lines are correct.
- The test counts are correct: 592 in `dashboard`, 24 in `pustaka`, highest register case TC-752.
- The §9 security claims hold, and the §8 cursor description matches `braintown_consumer.go:381-383,622-631` and `compose.prod.yaml:191`.

**Not checked:** change #13 (`.claude/rules/qa-test-plan.md`) is on my deny-list, so I did not verify it.

Counts: B=1 M=4 N=8
