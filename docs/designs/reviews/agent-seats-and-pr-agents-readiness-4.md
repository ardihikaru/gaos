# Readiness review: section 22, "Addendum: card-source labels and the docs links"

**Design reviewed:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos-agent-seats/docs/designs/agent-seats-and-pr-agents.md`, lines 724–747 only.

I checked every code claim against the branch:

- `dashboard/labels.go:41-60, 99-100, 118-119` matches. The label count is 17, and the `source` family sits after the severity family.
- `dashboard/api_cards.go:369-371`, `dashboard/bundles.go:191` and `dashboard/prflow.go:543-545` match the text.
- The ArcadeDB store passes both planners and `SetTaskLabel` straight to the in-memory store, at `store_arcade.go:422, 441, 481`. No schema statement limits labels (`schema/04-execution.sql:31-34`), so no DDL change is needed.
- `TestCardSourceLabels` is at `assign_test.go:306`, and the changed assertion in `TestPlanAPICardMatchesBrainTown` is at `api_tasks_test.go:217`.
- The four **i** links are at `templates/backoffice.html:464, 833, 863, 1157`. Their anchors (`#assignment`, `#severity`, `#review-fix`) exist in `pustaka/docs/integrations/agents.html`.

The label logic and the test both hold. The gaps are in how the change is written down, plus one change to a published contract.

### Section 22 has no change list, register entries, phase or verification - Severity: Major
- Affected scope: §22 as a whole, compared with §9, §10, §13 and §14
- Finding: §22 describes behaviour but has no file list, so it cannot serve as a write allowlist. The branch actually changes these files, and none of them is named as a change:
  - `dashboard/static/app.css:1983` adds the `a.info-btn.info-link` rule. The class is cited, but the file is never listed as changed.
  - `pustaka/docs/assets/toc/integrations.js:72-81` adds the new page and its `source-labels` entry.
  - `pustaka/docs/changelog.html:46-47`.
  - D-102 is extended in `pustaka/docs/product/decision-log.html:2144`. The added clause is never stated, so its text cannot be checked against the design.
  - `qa-test-plan.html:909` and `:4612`.
  - The count text in `toc/product.js:162,168`.
  - `README.md:140-142`.
  - `deploy/check-workspace.sh:114-118, 129-130`.
  - `pr-agents.html:54`, which now links to the new page.
  - `api_tasks_test.go`, which appears only as prose.

  §22 also gives no phase or exit condition, no verification commands, and no corrected §13 arithmetic (§13 still says 15 labels and 646 Go tests).
- Recommendation: Add a §22 change table in the §10 format, one row per file above. Add a register paragraph in the §9 format that gives the exact D-102 clause text. State the counts: 818 cases, written "eight hundred and eighteen"; 682 Go tests, written "six hundred and eighty-two"; decision and rule counts unchanged. Add an exit condition: §11 commands 1, 2 and 4 pass.

### The external-tasks API answer changes shape, but the published contract is not updated - Severity: Major
- Affected scope: §22 "Labels" table, row `source:api`
- Finding: Every card an API client creates now has `source:api` in its `labels`. That array is returned to outside clients, so this is a change to a published API answer.
  - `normalizeLabels` sorts `source:*` ahead of `e2e:*` and `unit:test` (`labels.go:109-127`). On those cards, `labels[0]` is now `source:api` rather than the job label.
  - The published examples still show `"labels":["pr-review:auto"]`, in `pustaka/docs/integrations/external-tasks-api.html:91` and `pustaka/docs/downloads/gaos-external-tasks-api.md:58`.
  - §22 neither notes the change nor lists those two files.

  An API client that matches the array exactly, or reads its first element, will break silently.
- Recommendation: State in §22 that the API answer now carries `source:api`, and give its position after normalisation. Add both docs files to the change list with the corrected example: `["pr-review:auto","source:api"]`. Add a line to the changelog's Changed list for API clients.

### The label vocabulary table in pr-agents.html leaves out the new family - Severity: Nit
- Affected scope: §22 "Labels"; `pustaka/docs/integrations/pr-agents.html:56-70`
- Finding: The `#labels` table presents itself as the closed vocabulary, and the severity family was added to it at `:68`. `source:api` and `source:bundle` are documented only on the new `agents.html#source-labels`.
- Recommendation: Either add one row for `source:api` and `source:bundle` that links to `agents.html#source-labels`, or state in §22 that the table stays as it is on purpose.

### Cards that already exist are not covered - Severity: Nit
- Affected scope: §22 "Labels", the "On" column
- Finding: "Every card an API client creates" does not say whether API and bundle cards created before v0.64.0 get the label. The code does not backfill them. Two implementers could reasonably choose differently, so this is an undecided choice. The practical effect is small: the `source:api` filter will not show older ungated cards that are still open.
- Recommendation: Add one sentence: "No backfill: only cards planned from v0.64.0 on carry a source label."

### Nothing checks the docs links - Severity: Nit
- Affected scope: §22 "Docs links"
- Finding: No test or command checks that the four **i** links render, or that their anchors exist in `agents.html`. A renamed anchor would go unnoticed. TC-818 covers only the labels.
- Recommendation: Either extend an existing render test (for example `main_test.go` for the Agents page, `board_page_test.go` for the board) to check each `href`, or name a manual check with its expected result.

### Wrong line anchor for the existing docs links - Severity: Nit
- Affected scope: §22 line 747, `templates/backoffice.html:971`
- Finding: The first relative docs link (`../docs/integrations/ai-manager-api.html`) is at line 972, not 971.
- Recommendation: Change the citation to `templates/backoffice.html:972`.

### The changelog lists three link places; §22 lists four - Severity: Nit
- Affected scope: §22 "Docs links" list, compared with `pustaka/docs/changelog.html:47`
- Finding: The changelog names the Agents page, the board's Severity filter and the Auto-fix page. It leaves out the Edit assignments summary (`#assignment`), which §22 lists and the code has (`backoffice.html:863`).
- Recommendation: Add "the Edit assignments summary" to the changelog line, or list it in §22's change table as a known difference.

Counts: B=0 M=2 N=5
