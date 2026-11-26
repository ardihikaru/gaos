<!-- Round 1, lens: readiness. Reviewer read only the design and the rubric (zero context). Canaries planted in the seven standing files of the worktree and removed after the round; no canary value in this report. -->

# Goldfish readiness review: `docs/designs/integrations-status-chips.md`

Reviewed in `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-integ-chips`. The design has one Blocker: three integrations print their full state only in the heading, so removing the heading suffix loses that text, and an existing test that the design does not mention then fails. There are also five Majors and two Nits.

### Removing the heading suffix deletes the only copy of three state strings, and an existing test then fails - Severity: Blocker
- Affected scope: §2.2 last paragraph ("the rows still print `{{.BrainTown.State}}`, `{{.PR.GitHub}}`, `{{.API.State}}` … in full"); §3 ("The existing `TestTheIntegrationsViewShowsEachIntegrationAndNoSecret` … is unchanged and must still pass").
- Finding: The premise is wrong for three of the six blocks. In `dashboard/templates/backoffice.html`, `{{.BrainTown.State}}` is printed only at :867, `{{.BrainTownConsumer.State}}` only at :879, and `{{.API.State}}` only at :945. All three are `<h4>` headings, and none of the three tables has a state row. Only `{{.PR.GitHub}}`, `{{.PR.StagingState}}` and `{{.PR.WA.State}}` (:894, :896, :898) and `QA.OutboundWhy` (:935) are in rows. So once the `&middot; <state>` suffix goes, the reason behind `Unusable` (`"unusable: " + why`, `braintown.go:615`, `braintown_consumer.go:318`, `main.go:281/353`) and the Machine API's `disabled (<why>)` / `enabled (admin=… qa=… clients=…)` (`api_v1.go:255-261`) disappear from the page. That is the outcome §2.2 promises will not happen.

  An existing test also breaks. `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` (`dashboard/integrations_test.go:182`) requires `"disabled (no API token is configured)"` in the section, and today that text comes only from the heading at :945. The design does not mention this test, so the implementer has to decide alone whether to add rows, where to put them, what to call them, or whether to rewrite the test.
- Recommendation: Say exactly where the full state text goes. For example, add a first row `<tr><th scope="row">State</th><td class="mono">{{.BrainTown.State}}</td></tr>` to the Brain Town, Brain Town reader and Machine API tables. Add `backoffice.html` row edits to change #3, name `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` as a test that must still pass unchanged, and correct the §2.2 sentence.

### The WhatsApp chip is green when the link is unencrypted or tied to no product - Severity: Major
- Affected scope: §2.2 row `PR.WAChip` (has prefix `"enabled"` → Enabled · on).
- Finding: `waNotifier.Status` (`dashboard/wa.go:250-263`) builds states such as `enabled, over http to any address (unencrypted)` and `enabled, linked to no product (GAOS_WA_PRODUCTS)`. The comment at `wa.go:250-251` says the unencrypted state must never be hidden by another state. The mapping makes both of these a green `Enabled` chip. They also count as `on` inside the `PRHead` rule ("Partly connected"). This is the very situation the operator complained about ("a disabled integration does not stand out"), just turned around: a warning state does not stand out.
- Recommendation: Add a rule: when the state is exactly `"enabled"`, the chip is Enabled · on. When it has prefix `"enabled,"`, it is `Degraded · warn` (or another named label). Add the unencrypted case to TC-753's negative column.

### The CSS for the info button and popover is placeholder comments - Severity: Major
- Affected scope: §2.4, the `.info-btn`, `.info-pop` and `.info-pop:popover-open` rules.
- Finding: Three of the rules are only comments (`/* panel colours, … padding, shadow */`, `/* 22px round button … */`, `/* opacity 1, scale 1 */`). Two engineers would choose different background and border variables, padding, shadow, the button's border and size, and where the popover sits. A `[popover]` is centred in the viewport by the browser's default style unless it is positioned, and the design never says whether the panel is centred or anchored to the ⓘ.

  The transition rule is not written either. §2.4 states "150 ms, ease-out", but no rule carries a `transition` property. An exit animation would also need `transition-behavior: allow-discrete` on `display` and `overlay`, and the design neither decides that nor rules out an exit animation.
- Recommendation: Write the three rules in full, using existing variables only (name them). State the position: centred (the browser default) or anchored, and if anchored, how. Give the exact `transition` declaration, and say whether the close is animated.

### No phases, no per-change checks, and the verification block runs from the wrong directory - Severity: Major
- Affected scope: §4, §8.
- Finding: The nine changes are not split into phases. There are no exit conditions and no order of work: for example, the Go struct and `integHeads` plus TC-753, then the template plus TC-754 to TC-756, then the registers. The only check is one global block, which gives no expected result. That block is also broken as written. After `cd ../pustaka`, the line `./deploy/check-stacks.sh` resolves to `pustaka/deploy/check-stacks.sh`, which does not exist; the scripts are at `deploy/check-stacks.sh` in the repository root.
- Recommendation: Define two or three phases, each with a command and its expected result (for example `go test -run 'TestIntegHeads|TestTheIntegrationsView|TestEveryIntegrationInfo' ./...` passes and the total is 596). Fix the paths to `cd .. && ./deploy/check-stacks.sh …`, or use paths from the root on every line.

### The dashboard test count is not in the register edits - Severity: Major
- Affected scope: §3 "dashboard 592 → 596"; §3 register mechanics; §4 row 7 ("two counts + Go count").
- Finding: `pustaka/docs/assets/toc/product.js:170` says "five hundred and ninety-two dashboard … unit tests". I counted 592 `^func Test` in `dashboard/`, so the number is correct today. The register mechanics update 752 (four places: `qa-test-plan.html:19`, `:79`, `product.js:162`, `:168`) and 616 (`product.js:168`), but never 592. After this change that text will be wrong.
- Recommendation: Add "five hundred and ninety-two" → "five hundred and ninety-six" at `product.js:170` to §3 and to change #7, and confirm by grep that it appears nowhere else.

### Neither the operator's decision nor the escalation gets an identifier - Severity: Major
- Affected scope: §1 ("The operator decided which integrations are mandatory"); §5 second bullet ("Escalated, not fixed here"); §3 changelog entry.
- Finding: The rule that ai-manager, QA tools and Machine API are mandatory is a product decision. It is written into code as a constant, but no decision-log row is allocated (the next free is D-097, after `decision-log.html:2074` `d-096`), and §1 has no source for the operator's words. The cursor-persistence defect is marked "Escalated", but no open question is allocated (the next free is OQ-40, after `open-questions.html:928`), and no file in the change list records it. Without a register entry the escalation is lost when this branch merges. The v0.59.0 changelog entry also has no D-number to cite, unlike recent entries.
- Recommendation: Either allocate D-097 (the mandatory set, plus the chip model) and OQ-40 (the read-only cursor), add `decision-log.html` and `open-questions.html` to the change list (11 files), and cite D-097 in the changelog and in the TC rows; or state explicitly why no D-number is needed and where the escalation is recorded.

### Several line anchors and paths are wrong - Severity: Nit
- Affected scope: §2.2, §2.4, §3, §5.
- Finding:
  - The GitHub default is at `integrations.go:206-209`, not 189-192.
  - The staging state is at `integrations.go:210-217`, not 193-200.
  - The existing test starts at `integrations_test.go:104`, not 103.
  - The file is `deploy/stacks/compose.prod.yaml:191`, not `compose.prod.yaml:191`.
  - `app.css:19-31` is the default/dark palette (`--ink:#EFE8DA`) and `:94-104` is the light one, so §2.4 swaps "light" and "dark".
- Recommendation: Correct the anchors.

### The info button's text and accessibility are not settled - Severity: Nit
- Affected scope: §2.3 markup, §2.5.
- Finding: The prose calls the button "ⓘ", but the markup's button text is a plain `i`. Because the button sits inside the `<h4>`, the heading's text becomes "Brain Town Enabled Optional i". The design should choose the glyph and state whether the button goes inside or outside the heading.
- Recommendation: Fix the glyph, and consider placing the button after `</h4>` inside a wrapping flex container.

The CSS colour variables, the `.chip` base rule, the next-free TC-753, the 752/616/592 counts, semver 0.58.1, the secret list at `integrations_test.go:96-100`, `buildIntegrations` being the only builder of `IntegrationsPage`, and the §5 cursor claims (`check-stacks.sh:158`, `writeCursor` at `braintown_consumer.go:622`) all check out.

Counts: B=1 M=5 N=2
