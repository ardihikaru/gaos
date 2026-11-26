<!-- Round 1, lens: comprehension. Reviewer read only the design and the rubric (zero context). Canaries planted in the seven standing files of the worktree and removed after the round; no canary value in this report. -->

# Goldfish comprehension review: `docs/designs/integrations-status-chips.md`

## What this proposes

1. **Go types:** add `StatusChip{Label, Tone}` and `IntegHead{Status, Mandatory}` to `dashboard/view.go`.
   - `IntegrationsPage` gets six `IntegHead` fields, one per integration.
   - `PRFlowRow` gets three `StatusChip` fields: GitHub, staging and WhatsApp.
2. **Go function:** add a pure function, `integHeads(p)`, called at the end of `buildIntegrations`. It turns each integration's existing state (a bool or a string) into a fixed label and a tone of `on`, `off` or `warn`, using a fixed table.
   - The pull-request heading is built from its three row chips.
   - "Mandatory" is a fixed constant: ai-manager, QA tools and Machine API are mandatory; Brain Town, Brain Town reader and Pull-request flow are optional.
3. **Template:** in `backoffice.html`, each of the six headings loses its `· <state>` text. In its place come a coloured status chip, a Mandatory/Optional chip and an "i" button.
   - The button opens a popover using the browser's native Popover API.
   - Each popover has three fixed English sections: what it does, when connected, when disconnected.
4. **CSS:** tone classes that reuse the existing colour variables, plus popover styling with a short opacity/scale transition that respects reduced motion. No JavaScript is added.
5. **Tests and records:** four tests (TC-753 to TC-756), register and count updates in the pustaka docs, and a v0.59.0 changelog entry with a version bump. Nine files in total.
6. **Non-goals:** making "mandatory" configurable, and the read-only Brain Town reader cursor problem (escalated).

## How the existing system works, per this document

- The Integrations view prints each integration's state as plain text after a middle dot in its `<h4>`. Pull-request flow has no state at all.
- The state strings come from `Status()`/`Describe()` methods. Some carry free-text reasons: `unusable: <why>`, `disabled (<why>)`, `off (<why>)`.
- The document says the rows below each heading already print those state strings in full, so removing the heading suffix loses nothing.
- The page sits behind the back-office session. htmx swaps views into `#sink`.

---

## Findings

### Reason strings are only in the headings, so removing the suffix deletes them - Severity: Blocker
- Affected scope: §2.2 last paragraph ("the rows still print `{{.BrainTown.State}}`, … `{{.API.State}}` … in full"). Also §5 bullet 3 and §7.
- Finding: This premise is wrong for three of the six blocks.
  - In `dashboard/templates/backoffice.html`, `{{.BrainTown.State}}` appears only in the heading (line 867). The rows (870-873) show Receiver, Queue, Last and Sent.
  - `{{.BrainTownConsumer.State}}` appears only at line 879.
  - `{{.API.State}}` appears only at line 945. The Machine API rows (948-951) show only the token flags and QA products.
  - Removing the `&middot; <state>` suffix therefore removes `unusable: <why>` for both Brain Town feeds and `disabled (<why>)` / `enabled (admin=… clients=…)` for the Machine API. An operator would see "Unusable" or "Disabled" with no reason anywhere on the page.
  - This also contradicts §5's non-goal "Changing any state string, any row": either rows must be added, or information is lost.
  - (The ai-manager, QA and PR rows do print their reasons; QA via `OutboundWhy` at line 935.)
- Recommendation: Decide where the reason goes. One option is a "State" row (`<th scope="row">State</th><td class="mono">{{.X.State}}</td>`) in the Brain Town, Brain Town reader and Machine API blocks. Then fix the §2.2 claim, add those rows to §4 #3, and drop "any row" from §5.

### An existing test asserts the heading text this design removes - Severity: Major
- Affected scope: §3, "The existing `TestTheIntegrationsViewShowsEachIntegrationAndNoSecret` … is unchanged and must still pass". Also §4.
- Finding:
  - `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` (`dashboard/integrations_test.go:151-188`) requires the view to contain `"disabled (no API token is configured)"` (line 182). That text is rendered only by the Machine API heading (`backoffice.html:945`), so the change as written makes this test fail.
  - The document never mentions this test, and §4 lists `integrations_test.go` only for "four tests".
  - Separately, TC-754's negative check ("the old `&middot; disabled` suffixes are gone") is easy to satisfy while the reason is lost entirely. No test checks that the reason is still visible.
- Recommendation: Name this test in §3. Say whether it stays unchanged (which needs the State row from the Blocker above) or is edited. Add to TC-754 a positive check that `unusable: <why>` / `disabled (<why>)` still appear somewhere in each block.

### The pull-request row chips are specified but never placed - Severity: Major
- Affected scope: §2.1 (`GitHubChip, StagingChip, WAChip` "for the per-row chips of the pull-request block"), §2.3, §4 #3.
- Finding:
  - §2.3 shows only the heading pattern. §4 #3 lists "six headings, six popovers, `integ-chips` sub-template" and no row chips.
  - Nothing says which `<td>` gets a chip, whether it replaces the text or sits beside it, or what markup it uses (`integ-chips` takes an `IntegHead`, not a `StatusChip`).
  - No test checks that they render: TC-754 counts chips only in the six `<h4 class="integ-h">`.
  - Two implementers would produce different pages, which the design contract says must not happen (README rule 6).
- Recommendation: Give the exact row markup, for example `<td class="mono"><span class="chip st-{{.PR.GitHubChip.Tone}}">{{.PR.GitHubChip.Label}}</span> {{.PR.GitHub}}</td>`, for each of the three rows. Add it to §4 #3 and to a TC. Or drop the three fields and use them only internally for `PRHead`.

### An unencrypted WhatsApp link gets a green "Enabled" chip - Severity: Major
- Affected scope: §2.2, row `PR.WAChip` (has prefix `"enabled"` → Enabled · on).
- Finding:
  - `waNotifier.Status()` (`dashboard/wa.go:250-263`) deliberately builds `enabled, over http to any address (unencrypted)` and `enabled, … linked to no product (GAOS_WA_PRODUCTS)`.
  - The comment there says "an operator must always be able to see the link is clear". The proposed rule shows both as green "Enabled".
  - The same green also counts toward `PRHead` "Partly connected".
  - The row still prints the full text, but the chip, the new at-a-glance signal, tells the opposite story. This is the kind of state the operator asked to "stand out".
- Recommendation: Add rules for these states. For example, `== "enabled"` → Enabled · on, and any other `enabled…` → "Enabled (check)" · warn. Or justify in the document why green is correct, and add the case to TC-753's negatives.

### The "mandatory" classification has no recorded decision - Severity: Major
- Affected scope: §1 ("The operator decided which integrations are **mandatory**…"), §2.2, §5.
- Finding:
  - This is a product decision that will be hard-coded and shown to every operator. The document gives no anchor for it.
  - `pustaka/docs/product/decision-log.html` contains nothing after D-096 and no entry about mandatory integrations. §4 adds no decision-log entry.
  - Per README rule 2, an unanchored claim must be labelled as an assumption.
- Recommendation: Cite or add a D-number in `decision-log.html` that records the mandatory/optional split and the PR-heading roll-up rule, and add that file to §4 (making it ten files).

### Wrong line anchors for the GitHub and staging state sources - Severity: Nit
- Affected scope: §2.2, `PR.GitHubChip` (`integrations.go:189-192`) and `PR.StagingChip` (`integrations.go:193-200`).
- Finding:
  - Those lines are the Brain Town assignments.
  - The GitHub default is at `integrations.go:206-209`; the staging states are at `:210-217`.
  - (`main.go:384-387` is correct.)
- Recommendation: Correct the anchors.

### Light and dark theme anchors are swapped - Severity: Nit
- Affected scope: §2.4 ("light `app.css:29-31`, dark `app.css:102-104`").
- Finding: Lines 29-31 are in `:root` (line 7), the dark default. Lines 102-104 are in `[data-theme="light"]` (line 83). The same applies to `--ink` at `:19-21` / `:94-96`.
- Recommendation: Swap the labels.

### Off-by-one test anchor - Severity: Nit
- Affected scope: §3, `integrations_test.go:103`.
- Finding: `TestTheIntegrationsViewShowsEachIntegrationAndNoSecret` is at line 104.
- Recommendation: Correct it.

### "The docs links on each block" is not true for Machine API - Severity: Nit
- Affected scope: §2.5, last sentence.
- Finding: The Machine API block (`backoffice.html:944-954`) has no docs link. In a browser without the Popover API, nothing on that block explains it.
- Recommendation: Qualify the sentence, or add a link to that block.

### The `ip-pr` popover text conflicts with "Partly connected" - Severity: Nit
- Affected scope: §2.3 popover table, row `ip-pr`, together with §2.2 `PRHead`.
- Finding:
  - "When connected" says staging adds the test cards. "When disconnected" says "no pull-request card is created".
  - The design has a "Partly connected" state where GitHub is off and staging is on. The reader cannot tell whether cards are created in that state.
- Recommendation: Add one sentence on what works when only staging or WhatsApp is on.

### A claim rests on a file reviewers may not open - Severity: Nit
- Affected scope: §2.5 (`.claude/rules/frontend.md`) and §2.4 ("the existing motion rules in the frontend agent").
- Finding: A reviewer cannot verify these claims. The "frontend agent" is not defined or linked.
- Recommendation: Quote the relevant rule inline, or label it as an assumption.

### The verification commands run from the wrong directory - Severity: Nit
- Affected scope: §8.
- Finding: After `cd ../pustaka`, `./deploy/check-stacks.sh` resolves to `pustaka/deploy/…`, which does not exist. The scripts are at the repository root.
- Recommendation: Use `cd ..` first, or root-relative paths.

### "One h3" for four tests is ambiguous - Severity: Nit
- Affected scope: §3, register mechanics ("one `h3` under `#implemented` quoting each test's exact `t.Errorf` strings").
- Finding: It is unclear whether this means one heading for all four TCs or one per TC.
- Recommendation: State it explicitly.

### The escalated non-goal has no destination - Severity: Nit
- Affected scope: §5, Brain Town reader cursor bullet.
- Finding:
  - The facts check out: `compose.prod.yaml:191` mounts the cursor `:ro`, `check-stacks.sh:158` asserts it, and `braintown_consumer.go:622-631` writes to `.tmp` then renames.
  - But "Escalated" names no open-question id. It is also unrelated to this view, so a reader has to work out why it appears here.
- Recommendation: Cite the OQ number it was filed under, or move it out of this design.

Sections that hold up:
- The §2.4 colour variables and `.chip` / `:focus-visible` anchors check out.
- The register counts are correct: two "seven hundred and fifty-two" in `qa-test-plan.html` plus two in `product.js`, Go count 616, `semver` 0.58.1, next free TC-753.
- The other §2.2 source rules (`braintown.go:599-617`, `braintown_consumer.go:308-330`, `api_v1.go:255-261`) match the code.
- §6 alternatives are real.
- §7 is consistent, apart from the missing-reason issue above.

Counts: B=1 M=4 N=9
