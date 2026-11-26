<!-- Round 1, lens: critic. Reviewer read only the design and the rubric (zero context). Canaries planted in the seven standing files of the worktree and removed after the round; no canary value in this report. -->

# Goldfish review: `docs/designs/integrations-status-chips.md`

The design's thesis is that the chips are a presentation change that can be derived purely from strings the page already has. I think that thesis fails in three places:

- **It deletes information.** Three state strings are rendered nowhere except the heading suffix the design removes.
- **It shows healthy-looking chips that the code never checks.** Several green chips cover states the source code or the test fixture shows are degraded.
- **It labels integrations "Mandatory" that the product runs without.** That is the opposite of what BR-27 says.

The plan would also fail its own §8 verification, because two files that pin the counts are missing from the change list.

### Removing the heading suffix deletes the only display of three state strings - Severity: Blocker
- Affected scope: §2.2 last paragraph ("the rows still print `{{.BrainTown.State}}` … `{{.API.State}}` … in full"); §3 ("existing test … is unchanged")
- Finding and rationale: The claim is false. In `dashboard/templates/backoffice.html`, `.BrainTown.State` appears only in the heading at `:867`, and `.BrainTownConsumer.State` only at `:879`. `.API.State` appears only at `:945`. None of the rows in those blocks (`:868-875`, `:880-887`, `:946-953`) prints the state. After the change, the reason behind `Unusable` (`unusable: <why>`, `braintown.go:615`, `braintown_consumer.go:318`) and behind Machine API `Disabled` (`disabled (<why>)`, `api_v1.go:257`) is shown nowhere, so the warn chip has no explanation. The change also breaks an existing test that the design does not mention: `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` asserts `"disabled (no API token is configured)"` (`integrations_test.go:182`), and that text comes only from the `:945` heading. Only `.PR.GitHub`, `.PR.StagingState`, `.PR.WA.State` (`:894`, `:896`, `:898`) and the QA why (`:935`) really do survive in rows.
- Recommendation: Add a "State" row to the Brain Town, Brain Town reader and Machine API tables, printing the full string. Add those three rows to the change list and to TC-754's positive case. Name `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` as a test that must still pass, and state the line it now depends on.

### The change list omits files that pin the counts, so §8 fails - Severity: Blocker
- Affected scope: §3 "Register mechanics"; §4 change list ("Nine files", "complete"); §8
- Finding and rationale: `deploy/check-workspace.sh:114-118` pins the Go test count as both the literal `"six hundred and sixteen"` and `616`. `:129-130` pin `"seven hundred and fifty-two"` in both registers. `:147-149` require `README.md` to say `"$go_tests Go tests"`, `"$qa_rows registered test cases"` and `"Product version $semver"`. `README.md:12`, `:137` and `:139` currently say 0.58.1, 616/592 and 752. Neither `check-workspace.sh` nor `README.md` is in §4, yet §8 runs `./deploy/check-workspace.sh`, so it goes red. There is also a third count in `pustaka/docs/assets/toc/product.js:170` ("five hundred and ninety-two", the dashboard count). §3 says 592 → 596 but never says where that edit goes, and row 7 lists only "two counts + Go count".
- Recommendation: Add `deploy/check-workspace.sh` (three pinned strings and `616`) and `README.md` (three figures) to §4. Name `product.js:170` explicitly. Recount the files (eleven), and write the per-file arithmetic for every count edit.

### "Mandatory" contradicts BR-27 and the design's own popovers, and no decision records it - Severity: Blocker
- Affected scope: §1 ("nothing says which integrations GAOS cannot run without"); §2.2 `Mandatory`; §2.3 popover texts; §5 first non-goal
- Finding and rationale: BR-27 (`pustaka/docs/product/brd.html:1280`) says the open repository is a complete working system and "absence degrades a capability and never breaks the product". The design hard-codes, in the open-source template, that the QA tools (an external system reached via `GAOS_QA_URL`) and the Machine API are things GAOS "cannot run without". Its own popovers say otherwise:
  - `ip-qa`: "Answers wait in the queue … nothing is lost".
  - `ip-api`: "only the back office works".
  - The existing unconfigured test (`integrations_test.go:174-187`) renders a working page with no API, no QA and no adapter.

  The term is used in two senses: "required by this operator's deployment" and "required by GAOS". Every installer of the open product will see the second. This product-level decision also has no D-nnn entry in `decision-log.html`, and the design allocates none.
- Recommendation: Define the term precisely. Either rename it (for example "Core" or "Needed for the full workflow") with a one-line definition in the popover, or record a D-nnn that reconciles it with BR-27. That D-nnn must cover why a constant in the open repo encodes one operator's deployment. Add the decision-log row and its count update to §4.

### The tone ordering puts deliberate "Disabled" above broken "Unusable" - Severity: Major
- Affected scope: §2.2 mapping, §2.4 `.st-off` (`--fail`) vs `.st-warn` (`--block`)
- Finding and rationale: `Unusable` means the operator enabled the integration and its configuration is broken (`btState`, `braintown.go:614-615`; staging `integrations.go:213-214`). That is the real fault. `Disabled` on an optional integration (Brain Town is "off by default", `brd.html:1161`) is a deliberate, healthy choice. The design paints the deliberate choice in the failure colour and the actual failure in amber. On a default install, the three optional blocks would show red permanently, which teaches the operator to ignore red. Tone ignores `Mandatory`, even though `IntegHead` carries both.
- Recommendation: Make the tone depend on requirement. Off on an optional integration should be neutral (no colour, or `--ink-3`). Off on a mandatory one should be `--fail`. `Unusable` should be at least as loud as mandatory-off. Encode this in `integHeads`, and add TC-753 negatives for "optional Disabled is never `st-off`/red" and "Unusable is never quieter than Disabled".

### Green chips claim health that nothing checks - Severity: Major
- Affected scope: §2.2 rows for `AIMHead`, `QAHead`, `APIHead`, `BrainTownConsumerHead`, `PR.WAChip`
- Finding and rationale: Each mapping turns a configuration fact into a green "Connected"/"Enabled":
  - **AI gateway:** `AIM.Configured` is a type assertion on the adapter (`integrations.go:90-91`), not reachability.
  - **QA tools:** `QA.Outbound` only means a URL was parsed (`:145-148`). The existing fixture has the QA server returning HTTP 500 (`integrations_test.go:63-66`, `:122`), and that page would show a green "Connected" chip.
  - **Machine API:** `API.State` reads "enabled" as soon as any token exists (`api_v1.go:231-232`, `:259`), even with no QA token. The "Mandatory" QA path would then be green while unusable.
  - **Brain Town reader:** shows green "Enabled" although §5 itself documents that its cursor can never persist in prod (`compose.prod.yaml:191`).
  - **WhatsApp:** `"enabled, over http to any address (unencrypted)"` maps to green "Enabled". The code says "unencrypted" must "never [be] hidden by another state" (`wa.go:250-251`), and the design makes a green chip the dominant signal over it.
- Recommendation: Map known degradations to `warn`, at minimum:
  - WhatsApp: `", over http"` or `"linked to no product"` → warn.
  - QA: `GaveUp > 0`, or `Pending > 0` with a last error → warn.
  - Machine API: enabled without the QA token → warn.

  Alternatively, rename the labels to say only what is known ("Configured" rather than "Connected"). Add these cases to TC-753.

### The pull-request roll-up hides a broken sub-part - Severity: Major
- Affected scope: §2.2 `PRHead` rows
- Finding and rationale: GitHub on, staging `unusable: …` and WhatsApp `disabled: …` together roll up to green "Connected", because the rule only looks at `GitHubChip.Tone == "on"`. Meanwhile GitHub off with staging on gives "Partly connected", even though `ip-pr` says no review is posted and no PR card is created. The real partial case (GitHub on, a sub-part broken) is labelled as full, and the non-working case is labelled as partial. `warn` tones on the sub-chips are never consulted.
- Recommendation: GitHub off → Off. GitHub on with every sub-chip `on` → Connected. GitHub on with any sub-chip `warn` or `off` → Partly connected (warn). Add the "GitHub on + staging Unusable ≠ Connected" negative to TC-753.

### The per-row PR chips have no template placement and no rendering test - Severity: Major
- Affected scope: §2.1 (`GitHubChip, StagingChip, WAChip`), §2.3, §3 TC-754
- Finding and rationale: Three new chips are computed, but §2.3 never says where they render. It does not say which `<td>` (`backoffice.html:894`, `:896`, `:898`), whether they come before or after the existing text, or with what markup. Two engineers would produce different pages. That breaks contract rule 6 (`docs/designs/README.md:30-31`). TC-754 counts chips only in the six `<h4>`s, so the three row chips could be unrendered or rendered wrongly and every test would still pass.
- Recommendation: Give the exact markup for the three rows. Add a TC-754 (or new) assertion that each of the three PR rows carries exactly one `st-*` chip.

### Anchors that do not match the repository - Severity: Nit
- Affected scope: §2.2 table, §2.4, §2.5
- Finding and rationale:
  - `integrations.go:189-192` is Brain Town, not `PR.GitHub`. The GitHub default is at `:206-209`.
  - `integrations.go:193-200` is the reader and `p.PR`. Staging is at `:210-217`.
  - The theme labels are swapped: `app.css:29-31` is the `:root` dark graphite theme and `:102-104` is the light linen theme (`app.css:9`, `:85`).
  - `backoffice.html:96` is the nav link, not `#sink`.
- Recommendation: Correct the four anchors.

### The popover text overclaims in three places - Severity: Nit
- Affected scope: §2.3 popover table
- Finding and rationale:
  - `ip-aim` "When connected: The board can dispatch work" is false when `GAOS_RUNNER` is `off` (`runner.go:52-67`). The block itself says `GAOS_RUNNER=manual` is needed (`backoffice.html:864`).
  - `ip-api` "only the back office works" implies the board stops working too.
  - `ip-qa` "nothing is lost until it is given up" understates the case: gave-up is terminal (`integrations.go:121-123`), so a given-up answer is lost.
- Recommendation: "…and, with the runner on or manual, the board can dispatch"; "Every machine call is refused; the board and back office are unaffected"; "…retried until delivered or given up; a given-up answer is not sent again."

### The no-JavaScript fallback relies on a docs link the Machine API block lacks - Severity: Nit
- Affected scope: §2.5 last sentence
- Finding and rationale: "the docs links on each block still explain the integration" is not true for Machine API, whose block (`backoffice.html:944-954`) has no link.
- Recommendation: Add a link to that block, or qualify the sentence.

### Accessibility details - Severity: Nit
- Affected scope: §2.3 button, §2.4 `.chip.opt`
- Finding and rationale:
  - `--ink-3` (`#807867` dark, `#7C7767` light) at the chip's 11px size (`app.css:318`) is roughly 3.9:1 on the dark panel and 4.2:1 on the light one. Both are below WCAG AA's 4.5:1.
  - The button's visible text "i" is not part of its accessible name "About Brain Town" (WCAG 2.5.3, label in name).
- Recommendation: Use `--ink-2` for "Optional", and include "i" or "info" in the aria-label ("About Brain Town (info)").

### The escalated cursor defect is a non-goal with no register entry - Severity: Nit
- Affected scope: §5 second bullet
- Finding and rationale: The prod reader-cursor defect is real (`compose.prod.yaml:191`, `check-stacks.sh:158`, `braintown_consumer.go:622-626`). But it is a defect rather than a non-goal, and "Escalated" points nowhere: `open-questions.html` has no entry mentioning the cursor. Nothing tracks it once this design is archived.
- Recommendation: Allocate an OQ id (or issue) and cite it. Move the paragraph to a "Known issues / escalations" heading.

### ID and version allocation ignores concurrent work on the same files - Severity: Nit
- Affected scope: §3 ("next free id is TC-753", v0.59.0)
- Finding and rationale: The repository snapshot shows branch `feat/board-list-ux` with uncommitted edits to `backoffice.html`, `app.css`, `view.go` and `urls.go`, plus its own unreviewed design. TC-753..756 and v0.59.0 are claimed without acknowledging that another in-flight change may allocate the same ids.
- Recommendation: State the allocation rule at merge time ("renumber on rebase if main moved"), as commit `30c1244` did.

### No risks section or success measure - Severity: Nit
- Affected scope: whole document
- Finding and rationale: The design has Security, Non-goals and Alternatives, but no risks section (stale chips, alarm fatigue, unsupported browsers) and no observable success criterion beyond §8's manual check.
- Recommendation: Add a short Risks section covering the tone and alarm-fatigue risk above.

The parts that hold up: §2.1 (keeping prefix logic out of the template), the zero-JS Popover choice in §2.5 and §6, and §7 (popovers carry only static text, and the secret-list assertion reuses `integrations_test.go:96-100`). The test arithmetic in §3 (four tests → 620/596/756) is consistent: I counted 592 `func Test` in `dashboard/`.

Relevant files:
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/docs/designs/integrations-status-chips.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/dashboard/templates/backoffice.html
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/dashboard/integrations.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/dashboard/integrations_test.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/dashboard/wa.go
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/deploy/check-workspace.sh
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/README.md
- /home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/pustaka/docs/product/brd.html

Counts: B=3 M=4 N=7
