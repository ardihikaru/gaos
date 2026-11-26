<!-- Round 1, lens: zoom-out. Reviewer read only the design and the rubric (zero context). Canaries planted in the seven standing files of the worktree and removed after the round; no canary value in this report. -->

# Goldfish review (Skeptical Architect, zoom-out lens): `docs/designs/integrations-status-chips.md`

The design is in proportion overall. It adds one pure Go function, one sub-template, a few CSS rules and uses the browser's built-in Popover API, so it needs no JavaScript. The `<dialog>` and `<details>` alternatives in §6 are described fairly. Two things must be fixed before implementation: one premise in §2.2 is false, and the change is under-specified in two places.

### Removing the heading suffix deletes the only place three state reasons are shown - Severity: Blocker
- Affected scope: §2.2 last paragraph ("the rows still print `{{.BrainTown.State}}` … `{{.API.State}}` … in full … Only the heading's `&middot; <state>` suffix is removed"); §3 ("existing … must still pass").
- Simplicity rationale: the premise is false. In `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips/dashboard/templates/backoffice.html`, `.BrainTown.State` (line 867), `.BrainTownConsumer.State` (line 879) and `.API.State` (line 945) appear **only** in the `<h4>` suffix. None of the three tables has a State row (lines 868-875, 880-887, 946-953). Removing the suffix removes the reason from the page, for example `unusable: BRAIN_TOWN_URL is not set` and `disabled (no API token is configured)`. A chip that says only "Unusable" then hides why. It also breaks an existing test: `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` (`dashboard/integrations_test.go:182`, TC-512) checks that the page contains `"disabled (no API token is configured)"`. The design says that test is untouched. Only the PR rows (lines 894, 896, 898) actually show their state in a row.
- Recommendation: the smallest fix is a template-only change. Add a first row `<tr><th scope="row">State</th><td class="mono">{{.X.State}}</td></tr>` to the Brain Town, Brain Town reader and Machine API tables. Alternatively, keep the reason text after the chip. Then correct §2.2's claim, and list TC-512 as a test that must still pass.

### The three `PRFlowRow` chip fields are either unused or their rendering is unspecified - Severity: Major
- Affected scope: §2.1 (`GitHubChip, StagingChip, WAChip StatusChip` "for the per-row chips of the pull-request block"); §2.3; §4 row 3.
- Simplicity rationale: §2.1 says these fields feed per-row chips. But §2.3 and the change list only change "six headings, six popovers, `integ-chips`", and no row markup is given. One engineer would add chips to the GitHub, Staging and WhatsApp rows; another would not. If they are not rendered, three exported view-model fields exist only to compute `PRHead`, which is a YAGNI addition to `view.go:231`. This breaks README rule 6 ("nothing is left undecided").
- Recommendation: make them local variables inside `integHeads` and drop them from `PRFlowRow`. If per-row chips are actually wanted, write out the row markup in §2.3 and add an assertion for it to TC-754.

### WhatsApp "enabled, … (unencrypted)" and "linked to no product" both turn green - Severity: Major
- Affected scope: §2.2 row `PR.WAChip` (`has prefix "enabled"` → Enabled · on).
- Simplicity rationale: `dashboard/wa.go:250-262` builds the state from parts on purpose, so that "an operator must always be able to see the link is clear". The proposed prefix rule gives the same green chip to `enabled, over http to any address (unencrypted)` and to `enabled, linked to no product (GAOS_WA_PRODUCTS)`, a notifier that sends nothing. That chip then pushes `PRHead` towards Partly connected. This is the risk of parsing free-text state strings written in other files (`wa.go`, `api_v1.go:255-261`, `braintown*.go`): the chip meaning drifts silently when someone edits a sentence.
- Recommendation: keep the string parsing, but add a rule to the table: `enabled` plus any qualifier (the state contains `,`) → `warn`. Add both qualifier strings to TC-753's negative cases. Do not add a status enum to the producers for this change.

### "Connected" overstates what the fields prove - Severity: Major
- Affected scope: §2.2 rows `AIMHead` (`AIM.Configured`) and `QAHead` (`QA.Outbound`).
- Simplicity rationale: `QA.Outbound` is true when a URL parsed (`integrations.go:145-148`). It says nothing about whether delivery works. The existing fixture (`integrations_test.go:122`) renders "the QA tools answered HTTP 500" and a retry, and under this mapping that block would get a green **Connected**. The operator's complaint was about seeing what works, and this chip would wrongly say it does.
- Recommendation: do not add health logic. Use the honest word for what is measured, "Configured" (or "Enabled"), for these two heads. If a health signal is wanted later, it needs its own decision.

### A new tone vocabulary next to an existing one - Severity: Nit
- Affected scope: §2.1 (`Tone` is "on" | "off" | "warn"); §2.4 (`st-off` → `--fail` red).
- Simplicity rationale: the sibling Auto-fix view already uses `AFToken.Tone` `ok | warn | off` (`dashboard/autofix_view.go:107`, CSS `app.css:1663-1667`), where `off` is grey (`--ink-3`). With this design, `off` would mean red on one back-office view and grey on the next. Other ok/warn precedents exist too: `.stat.ok/.warn` (`app.css:216-217`) and `.vnd-dot.ok/.down` (`app.css:1457-1458`).
- Recommendation: reuse the `ok | warn | off` names, or give the red tone a different name. Either way, say in one sentence why "off" is red here.

### `IntegHead.Mandatory` stores a constant in the view model - Severity: Nit
- Affected scope: §2.1 `IntegHead`, §2.2 "Mandatory is … a constant".
- Simplicity rationale: each heading is already static template text, so the requirement chip can be static markup too. TC-755 already tests the rendered output. The bool and the wrapper struct add indirection without adding behaviour.
- Recommendation: put the `Mandatory`/`Optional` span directly in each heading and reduce `IntegHead` to `StatusChip`. Keeping it as it is is acceptable if the author prefers one sub-template.

### The count arithmetic misses the dashboard count - Severity: Nit
- Affected scope: §3 "Register mechanics"; §4 row 7.
- Simplicity rationale: I count four "seven hundred and fifty-two" strings: `qa-test-plan.html:19` and `:79`, and `assets/toc/product.js:162` and `:168`. That matches §4. But §3 also says "dashboard 592 → 596", and `product.js:170` contains "five hundred and ninety-two dashboard … unit tests". Neither the register mechanics nor change-list row 7 ("two counts + Go count") includes it, so that count would go stale.
- Recommendation: row 7 should read "two register counts, the Go count (616→620) and the dashboard count (592→596)".

### Wrong anchors and one false supporting claim - Severity: Nit
- Affected scope: §2.2 `PR.GitHubChip` (`integrations.go:189-192`) and `PR.StagingChip` (`integrations.go:193-200`); §2.5 "the docs links on each block"; §3 `integrations_test.go:103`.
- Simplicity rationale: the cited ranges are wrong:
  - Lines 189-198 of `integrations.go` are Brain Town and the reader.
  - `GitHub` is set at `integrations.go:206-209` and `StagingState` at `:210-217`.
  - The Machine API block (`backoffice.html:944-954`) has no docs link.
  - The test starts at line 104.
- Recommendation: fix the anchors, and change "each block" to "five of the six blocks".

### "Mandatory" is a product decision with no decision-log entry - Severity: Nit
- Affected scope: §1 ("The operator decided which integrations are mandatory"); §4.
- Simplicity rationale: nothing in `pustaka/docs/product/decision-log.html` or `brd.html` names these integrations as mandatory. The claim has no anchor (README rule 2). It is also not obvious: GAOS runs without the Machine API, which the `ip-api` text itself admits ("only the back office works").
- Recommendation: add a D-entry to the change list (making it 10 files), or label the claim as an operator assumption in the design.

### The TC-756 secret check can never fail - Severity: Nit
- Affected scope: §3 TC-756 negative ("no popover holds any string of the fixture's secret list").
- Simplicity rationale: the popovers are static text with nothing interpolated (§2.3). The existing whole-body leak check (`integrations_test.go:128-131`) already covers the page.
- Recommendation: drop that negative, or replace it with one that can fail, such as "no `{{` field appears inside an `info-pop`" or "no id appears twice", which is already listed.

### The cursor non-goal is unrelated to this design - Severity: Nit
- Affected scope: §5 second bullet (the read-only `prod.bt-consumer-cursor` mount).
- Simplicity rationale: this is a real deployment defect, but it has nothing to do with chips or popovers. It says "Escalated" without naming where, so it is likely to be lost inside a UI design.
- Recommendation: record it as an OQ or D entry and replace the bullet with a one-line pointer to it.

§2.3 (popover ids and static texts), §2.4 (CSS reuses existing colour variables; motion follows the house rules), §2.5 (native Popover API, no JS) and §6 (alternatives) are sound.

Counts: B=1 M=3 N=7
