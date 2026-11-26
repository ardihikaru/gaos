<!-- Round 2, lens: readiness. Zero context; canaries planted and removed; no canary value in this report. -->

# Readiness review, round 2: `docs/designs/integrations-status-chips.md`

Worktree: `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips`

The design is not ready. One Blocker remains: the version bump breaks an existing dashboard test, and the step that fixes it is missing from the change list. There are also three Majors and four Nits.

## Round-1 Blockers

| Round-1 Blocker | Resolved? | Evidence |
|---|---|---|
| Removing the heading suffix deletes the only display of three states; TC-512 breaks (all four general lenses) | **Resolved** | §3.3b adds State rows. `{{.API.State}}` is still rendered, so `"disabled (no API token is configured)"` (`integrations_test.go:182`) still matches. One weakness in how the new rows are tested is below as Major M2. |
| The change list leaves out the files that pin the counts (critic) | **Resolved as stated, but the same kind of gap is back** | Every pin I checked is correct: `check-workspace.sh:114-118`, `:129-131`; `README.md:12`, `:137`, `:139`; `product.js:129`, `:162`, `:168`, `:170`; `toc.js:22`; `decision-log.html:18`. But the version bump also pins `dashboard/templates/page.html`, and that file is not listed (Blocker B1). |
| "Mandatory" contradicts BR-27, and no decision records it (critic) | **Resolved** | BR-27 (`brd.html:1280`) is about private submodules. D-097 defines "Mandatory" as needed by the workflow, not by start-up. The next free number is right (`d-096` is at `decision-log.html:2074`). |

## Findings

### The version bump breaks TC-690, and `page.html` is not in the change list - Severity: Blocker
- Affected scope: §5 ("Version: 0.58.1 → 0.59.0", the literals table), §6 (the change list, "Thirteen product files"), §7 phase 3.
- Finding: `TestStaticAssetsAreVersionedByTheProductVersion` (`dashboard/external_inbox_test.go:893-915`, TC-690) reads `semver` from `pustaka/docs/assets/toc.js`. It then requires each `?v=` on `app.css`, `app.js` and `live.js` in `dashboard/templates/page.html` (`:48`, `:50`, `:51`, all `0.58.1` today) to equal that value. Change #9 bumps `toc.js` to 0.59.0 and nothing updates `page.html`, so the dashboard suite fails. This change also edits `app.css`. Without the cache-busting `?v=`, browsers keep the old stylesheet for up to a week (`page.html:46`), and the phase-4 live check would show the chips with no styling. Phase 3 does not catch any of this: it runs only the pustaka tests and the shell checks, and the dashboard suite already passed in phase 2, before the bump.
- Recommendation: Add `dashboard/templates/page.html` to §6 (making fourteen product files), with the change `?v=0.58.1` → `?v=0.59.0` at `:48`, `:50` and `:51`. Add a row to the §5 literals table. Add `cd dashboard && go test ./...` to the phase-3 commands, with the expected result `ok`.

### `.block h4` overrides `.integ-h h4{margin:0}`, so the headings will not line up with the chips - Severity: Major
- Affected scope: §3.4 ("Appended after the existing `.chip` rules (`app.css:317-326`)"), rule `.integ-h h4{margin:0}`.
- Finding: `.block h4` (`app.css:860-863`) sets `margin:0 0 10px`. It has the same specificity (0,1,1) as `.integ-h h4`. The design places the new rules around line 327, which is before line 860, so the later `.block h4` wins. Every wrapped `h4` keeps a 10px bottom margin inside a flex row with `align-items:center`. The heading text then sits about 5px above the chips and the ⓘ button, and the 8px margin on the wrapper adds to the 10px, giving 18px.
- Recommendation: Either append the block at the end of `app.css` (after `:860`), or raise the selector's specificity to `.block .integ-h h4{margin:0}`. Name which one. Add "heading text and chips share a baseline" to the phase-4 live checklist.

### TC-754's State-row check cannot fail for two of the three rows - Severity: Major
- Affected scope: §4 TC-754 positive ("The three new State rows print their full state string"); §3.3b.
- Finding: `newIntegrationsFixture` (`integrations_test.go:48-102`) sets neither `srv.brain`/`srv.brainOff` nor `srv.btConsumer`/`srv.btConsumerOff`. `buildIntegrations` (`integrations.go:189-198`) therefore uses the zero `BrainTownRow`, whose `State` is `""`. Asserting that "the full state string" appears means asserting that an empty string appears, which always passes. These State rows are the round-1 Blocker fix, and for Brain Town and the Brain Town reader they would be untested. The fixture also gives Brain Town a `Disabled` chip from a state that production never produces (`main.go:271` always calls `btState`).
- Recommendation: Say what TC-754 sets and what it asserts. For example: set `f.srv.brainOff = BrainTownRow{State: "unusable: <sentinel>"}` and `f.srv.btConsumerOff = BrainTownRow{State: "disabled"}`. Then assert the exact cells `<td class="mono">unusable: <sentinel></td>` and `<td class="mono">disabled</td>`, plus `enabled (admin=true qa=true` for the Machine API row. This also covers the `Unusable · bad` chip in a rendered page.

### `GaveUp > 0` counts every give-up ever recorded, so the QA chip can never return to green - Severity: Major
- Affected scope: §3.2 `QAChip` row 2 ("`GaveUp > 0` or `Failing` → Degraded · warn"); §3.2 "Why these rules", QA bullet.
- Finding: `p.QA.GaveUp` is counted across the whole event stream (`integrations.go:124-140`), and a give-up is terminal (`:121-123`). After one historical give-up, the Mandatory QA tools chip stays amber for the life of the store, even when every later delivery succeeds. The operator has no action that clears it. This is the alarm-fatigue risk that §10 says §3.2 handles, and the design neither selects this behaviour nor excludes it. Two implementers could reasonably differ: one counts all time as written, another limits the count to a recent window.
- Recommendation: Decide it explicitly. Either state that a permanent amber is intended and say so in the `ip-qa` popover, or narrow the rule. Possible narrower rules: a give-up within a stated window, such as the last 24 h measured by `e.At`; or a give-up newer than the last successful delivery. Add the chosen boundary to TC-753's cases.

### The phase-2 test count runs from the wrong directory - Severity: Nit
- Affected scope: §7 phase 2.
- Finding: The step runs `cd dashboard && go vet ./... && go test ./...` and then `grep -hoE '^func Test[A-Za-z0-9_]+' dashboard/*_test.go | wc -l`. If both run in the same shell, the grep looks for `dashboard/dashboard/*_test.go` and prints `0`. Round 1 raised this same class of problem.
- Recommendation: Write it as `(cd dashboard && go vet ./... && go test ./...)` followed by the grep from the root, or as `grep … *_test.go` inside `dashboard/`.

### TC-754's negative check uses an undefined "state word" - Severity: Nit
- Affected scope: §4 TC-754 negative ("No `<h4>` contains `&middot;` followed by a state word").
- Finding: The set of "state words" is not given, so two implementers would write different checks.
- Recommendation: Assert instead that no `<h4>` inside `#boIntegrations` contains `&middot;` at all. No remaining `h4` in the section uses it (`backoffice.html:906`, `:956`).

### The D-097 row, the four `h3` anchors and the changelog text are not specified - Severity: Nit
- Affected scope: §5, the paragraph "Register content added…".
- Finding: The `d-096` row (`decision-log.html:2074-2092`) links its evidence to `qa-test-plan.html#<h3-id>`. The design names neither the four new `h3` ids nor the D-097 row's statement text or evidence cell, nor the changelog entry's text or bump class. Each implementer would invent them.
- Recommendation: Give the four `h3` ids, for example `go-integ-chips-model`. Give the D-097 row's full cells: the date 2026-11-27, the statement (§2's paragraph), and the evidence `TC-753…TC-756; docs/designs/integrations-status-chips.md`. Give the changelog's `id="v0-59-0"`, its `minor` bump class and a one-line summary.

### The `ip-aim` popover still overclaims about the runner - Severity: Nit
- Affected scope: §3.3e `ip-aim` "When connected".
- Finding: The text says "Agents can take turns; with the runner set to manual, the board can dispatch work". `runnerMaySpend` (`runner.go:64-67`), which the design itself cites, allows board dispatch for both `on` and `manual`. With `GAOS_RUNNER` set to `off` (the default, `:53`), no turn runs even though ai-manager is connected.
- Recommendation: Use: "With the runner on or manual, agents take turns and the board can dispatch work; with it off, no turn runs."

## What holds

- The model, the mapping and the template markup (§3.1-§3.3) can be implemented without further decisions. Every source string matches the code: `wa.go:244-271`, `braintown.go:599-617`, `braintown_consumer.go:308-330`, `api_v1.go:255-261`, `main.go:384-387`, `integrations.go:206-217`.
- The counts are correct: 592 `func Test` in `dashboard/` today, so 592 + 4 = 596 and 616 + 4 = 620. TC-752 is the highest case in use (`qa-test-plan.html:843`).
- The identifiers are each allocated once: D-097 and TC-753…756.
- Every CSS variable the design uses exists, and the theme anchors are correct.
- The §8 cursor claims hold: `compose.prod.yaml:191`, `check-stacks.sh:158`, `braintown_consumer.go:381-383`, `:622-631`.
- Not checked: change #13 edits `.claude/rules/qa-test-plan.md`, which is on this reviewer's deny-list, so its current literals were not verified.

Counts: B=1 M=3 N=4
