# Design review — M17.5a, the frontend design rule

**Target:** `docs/designs/frontend-design-rule.md` (revision 1)
**Lens:** Zoom-out (is this the smallest sufficient change? does existing code already solve it?)
**Round:** 1 · **Date:** 2026-11-04
**Read:** the design document, the rubric (`.claude/rules/rule-index.md`,
`.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`) and the domain sources the
index names. Every `file:line` claim was re-derived from the tree.

---

**Checks performed** — every `file:line` claim re-derived from the tree (not from the document):
the upstream skill at `HEAD` (line map, byte/line counts, the validator's own vocabulary
command), `deploy/check-workspace.sh` in full, the QA register and its four mirrors,
`README.md`/`toc.js`/`NOTICE`/`changelog.html`, `.claude/agents/gaos-frontend.md`, all three
`.claude/rules/` files, `.agents/rules/` symlink parity, `docs/designs/README.md` (the document
contract), and the sibling design `docs/designs/m17.5-reviewer-workspace.md`. No file was
created, edited or deleted.

**Verdict** — the plan is implementable and the operator's request is real, but as written it is
*not* the smallest sufficient change, its file-count target does not follow from its own change
list, and it is missing three files and two validator strings that must change. The move I would
insist on first is D2: the trim as specified re-imports ~274 lines of craft mechanics the repo
already holds and buys that with three hand-rewrites of vendored MIT text.

### The trim keeps 274 lines the repo already holds, and pays for it with three rewrites - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:119-131` (D2 keep/drop/rewrite table)
- Simplicity rationale: the keep list is `Component Building Principles` (197-344), `CSS Transform Mastery` (346-391), `Performance Rules` (477-525), `Accessibility` (527-557) — 274 of the 325 kept lines. These are the *same* mechanics `.claude/agents/gaos-frontend.md` already states: upstream "Only animate transform and opacity" (479) ≈ `gaos-frontend.md:74-75`; "CSS variables are inheritable" (483) ≈ `gaos-frontend.md:45-47`; "prefers-reduced-motion" (529) ≈ `gaos-frontend.md:79-81`; upstream Review Checklist rows `transition: all` / `scale(0)` / `ease-in` / `Duration > 300ms` / hover media query / keyboard animation ≈ `gaos-frontend.md:60,67,73,74,76,78`. The document's own reason for dropping the motion family — "two motion manuals is one too many" (`:122`, `:338`) — applies to its own keep list. The genuinely missing layer is `## Core Philosophy` (18-36) + `## Review Format (Required)` (38-60): 42 lines. And the three "rewrite" rows exist only because craft sections embed React examples; drop them and there is nothing to rewrite, no hand-editing of MIT prose, and the zero-score acceptance is trivially true.
- Recommendation: restore frontmatter + `## Initial Response` + `## Core Philosophy` + `## Review Format (Required)` only (≈50 lines), leave `gaos-frontend.md` as the single source of craft mechanics, and delete D2's rewrite row. The restored skill then carries no framework vocabulary to begin with.

### The trim boundary leaves `## Review Checklist` unassigned, and it re-imports the motion the document drops - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:117-123` (D2: "The upstream file is 676 lines. The trim is decided by three verbs")
- Simplicity rationale: the keep set spans 8-557 and the drop set 62-658; upstream lines **660-676 (`## Review Checklist`)** are in neither. That section's rows include "Motion `x`/`y` props under load (pre-14.2)" and "Elements all appear at once → Add stagger delay (30-80ms between items)" — motion guidance the drop rationale delegates to `review-animations/STANDARDS.md`. A boundary the document says a reviewer can "check each cut" against must assign every line; an unassigned 17-line section is an open decision (`docs/designs/README.md:30-31`).
- Recommendation: name660-676 explicitly in the keep or drop column, with the reason. If it is kept, the "one motion manual" premise in D2 is broken; if dropped, say so.

### The zero-score acceptance condition does not measure the stated goal - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:79-84` (Goals) and `:133-134` ("The result must score zero on the validator's vocabulary grep")
- Simplicity rationale: the goal is "plain CSS, plain DOM, one hand-written stylesheet, **no component library**" (`:79-80`), but the check is a five-word grep. The kept `Component Building Principles` still carries `/* Base UI */` at upstream 239, inside the kept `### Make popovers origin-aware` (234-246). That names a component library this dashboard does not have, in a code sample an agent is told to read as a principle — and it passes the grep, so the acceptance condition as written admits a file that fails the goal. The doc's rewrite list (three passages) is derived from the grep, not from the goal.
- Recommendation: state the goal as a checkable property of the kept text (no named library, no component model, no React/DOM-API idiom) rather than as a five-word count, and add `/* Base UI */` to the rewrite list — or, per the finding above, drop the section that contains it.

### D7's rescope silently drops guidance files from the guard - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:196-207` (D7) and `:353` (Risks: "the scan still covers every skill's guidance")
- Simplicity rationale: the scan becomes "each skill's `SKILL.md`" (`:198`), and the document claims "break-ui and review-animations have no such file split" (`:201-202`). The tree says otherwise: `review-animations/` holds `SKILL.md` **and `STANDARDS.md`** — the file `gaos-frontend.md:18-20` calls "the bar an agent is held to" — and `STANDARDS.md` carries4 framework-vocabulary matches today (2 `jsx`, 2 `Sonner`); `break-ui/` holds `SKILL.md` **and `CATALOG.md`**. Both are guidance, not provenance. After the change they are no longer scanned, and the Risks row's claim that guidance is still covered is false. The stated need is narrower than the change: only `SOURCE.md` must be excluded (it must name what was cut).
- Recommendation: keep scanning every file in the skill directory **except the provenance files** (`SOURCE.md`, `LICENSE`) rather than reducing the scan to one filename; then the guard loses only what it must, and `STANDARDS.md`/`CATALOG.md` stay covered.

### The file-count target does not follow from the change list - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:240-261` (change list and "**Sixteen files** outside `docs/designs/`")
- Simplicity rationale: rows 1-15 are the files outside `docs/designs/`; rows 16-18 are inside it. The count outside is **15**, not 16 — and the contract demands exactly this arithmetic (`docs/designs/README.md:26-27`). Row 17's wildcard also compresses five lens reports into one row, so "the review reports" is six files, not two.
- Recommendation: write "Fifteen files outside `docs/designs/`, plus this document and six review files — 22 in all", or drop the sentence and let the table be the count.

### The vocabulary score is wrong three ways - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:106-110` (D1) and `:322-324` (Alternative A)
- Simplicity rationale: the document says the upstream file "scores **7**" under the guard and lists nine line numbers for it. The validator's own command (`deploy/check-workspace.sh:217`, `grep -rioE "react|jsx|tailwind|sonner|shadcn" <dir> | wc -l`) returns **11** for the upstream directory (11 in `SKILL.md`, 0 in `LICENSE`, 0 in `SOURCE.md`); the distinct matching lines are **ten** — 162, 336, 338, 350, 499, 542, **559**, 561, 567, 577 — and the document's list omits 559 (`## The Sonner Principles`). The conclusion (upstream violates the guard) survives; the number does not, and it is the number the trim is justified against.
- Recommendation: quote the validator's command and its output (11), or drop the count and keep the line list complete.

### The change list omits two validator strings that must move - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:249` (row 8) and `:276-281` ("Where each count lives")
- Simplicity rationale: the document enumerates the mirrors and names `deploy/check-workspace.sh:125-126` — but `deploy/check-workspace.sh:141` asserts `README.md` says `379 cases` and `:142` asserts `Product version 0.35.0`. Both change under this design (case count → 380, version → 0.35.1), so `check-workspace.sh` fails with "README version" at the end of implementation unless `:141` and `:142` are updated. Row 8 describes that file only as "the vocabulary scan's scope and the inverted assertion", so the change list is incomplete (`docs/designs/README.md:24-25`).
- Recommendation: add the two strings to row 8 and to the mirror list, and name `:141`/`:142` in "Where each count lives".

### TC-380 is called a forward specification but the check lands in the same change - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:8` (header) vs `:251` (row 10) and `:295` (verification row 1)
- Simplicity rationale: the header lists TC-380 under "**Forward specifications**", which `.claude/rules/qa-test-plan.md:57-60` defines as a row with Status `Not implemented` — an asserted value written *before* the code that produces it. But D7 implements the check that TC-380 describes in the same change, so the row must be `Implemented` with the script's exact quoted string, filed with the other workspace checks under `#cw-workspace` (where TC-357 lives), not under `#go-designgate` — the anchor the header points at. As written, the register would say the check does not exist, and the operator's run sheet would omit it.
- Recommendation: make TC-380 an `Implemented` row in the workspace section with the exact `ok`/`FAIL` string, and drop the header's "Forward specifications" line (this design has no forward specification).

### The new rule needs a cross-harness symlink the change list does not contain - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:76-78` (Goal 2) and `:240-259` (change list)
- Simplicity rationale: `.agents/rules/` holds a relative symlink for **every** rule file — `agent-workspace.md`, `qa-test-plan.md`, `rule-index.md` — because `.claude/rules/agent-workspace.md:118-120` states "`.agents/rules/*.md` are relative symlinks into `.claude/rules/`, so a rule is one file read by two harnesses". A new `.claude/rules/frontend.md` with no `.agents/rules/frontend.md` reaches one harness only, so "Every frontend change loads it" (`:77-78`) is not true for the second harness — and the change list has no row for it.
- Recommendation: add `.agents/rules/frontend.md` (relative symlink, `../../.claude/rules/frontend.md`) to the change list, as a nineteenth entry.

### This document and `m17.5-reviewer-workspace.md` cannot both be "halves of this change" - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:372` (Related: "the other half of this change")
- Simplicity rationale: the sibling document's own numbers assume this one has already landed: `docs/designs/m17.5-reviewer-workspace.md:471` reads "QA cases | 380 | **389**", `:557` bumps `Product version 0.36.0`, and its phase-3 table (`:405-413`) touches the same files this change touches (`qa-test-plan.html`, `toc/product.js`, `toc.js`, `changelog.html`, `README.md`, `deploy/check-workspace.sh`, `.claude/rules/qa-test-plan.md`). Two "halves" cannot each own the version bump and the case count; both are In Review at the same time and neither states the order.
- Recommendation: state the ordering in the Related line — this change lands first (379→380, 0.35.0→0.35.1) and M17.5 follows from 380/0.35.1 — or drop the "other half" framing.

### The canary check in the verification plan can never pass - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:298` (verification check 4)
- Simplicity rationale: the check is a plain grep for the marker prefix over `.` — and the design document itself contains that literal at line 298, so the grep finds its own rule file and the check fails by construction. It also contradicts the house semantics: `deploy/check-workspace.sh:172-175` says the prefix may legitimately appear in prose ("A report may discuss the prefix") and only a real *value* left in a standing file is a leak; the precise detector is the validator's own regex at `:176-185`, which is already in the house-check list at `:308`.
- Recommendation: delete check 4 and rely on `./deploy/check-workspace.sh`; if a local probe is wanted, use the validator's regex against the standing files, scoped so it cannot match this document.

### The NOTICE change may be one file too many - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:209-215` (D8) and `:250` (row 9)
- Simplicity rationale: the document's premise is "the notice is what the house requires next to every vendored file", but the house NOTICE lists the EGM method, the fonts and ECharts (`NOTICE:37`, `:46-49`) and lists **neither** of the two sibling vendored MIT skills — those are attributed by their own `SOURCE.md` + `LICENSE`, which is exactly the mechanism this design already amends (row 3). Adding an entry for the third skill only would establish a new convention for one of three identical files.
- Recommendation: either list all three vendored skills in NOTICE or drop row 9 and let `SOURCE.md` + `LICENSE` carry the attribution, as they do for `break-ui` and `review-animations`.

### The rule's path scope omits files the frontend agent claims - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:150-155` and `:157-158`
- Simplicity rationale: `gaos-frontend.md:11-13` gives the agent "the view-model glue that feeds them (`dashboard/render.go`, `dashboard/handlers_bo.go`'s `buildBO`)" — but the rule's `paths` cover only `dashboard/templates/**` and `dashboard/static/**`, so a view-model edit loads no design rule and the Goal "Every frontend change loads it" is literally untrue for two of the agent's own files.
- Recommendation: add `dashboard/render.go` to the `paths` list, or say in the rule that view-model changes are covered by the agent file alone.

### The published record of the removal is never acknowledged - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:16-22` and `:253` (row 13)
- Simplicity rationale: `pustaka/docs/changelog.html:99-101` publicly records "**Removed** … the `emil-design-eng` skill: 27 KB of React, Sonner and Tailwind guidance … The two stack-agnostic skills stay." This change restores a third skill under the same name, so the published v0.35.0 entry becomes false unless the v0.35.1 entry says plainly that the skill comes back trimmed. The design mentions the changelog only as a row to edit.
- Recommendation: say in the document that the v0.35.1 entry reverses the v0.35.0 removal, and name the trimming.

### Citation drift around the agent file, the rule frontmatter and NOTICE - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:186-189` (D6), `:146-147` (D4), `:212-213` (D8)
- Simplicity rationale: D6 says the "was removed on 2026-11-04" sentence comes "two lines later" than "Your taste comes from…" — it is nine lines later (`gaos-frontend.md:15-16` vs `:25-26`); D4 cites the frontmatter shape as `agent-workspace.md:1-4` (it is1-5, the closing `---` on line 5); D8 cites NOTICE's third-party section as `:30-44`, which cuts off the font and ECharts entries at `:46-49`; the Problem section cites `gaos-frontend.md:5-8` for the ownership list, which is at `:9-13`. The document contract requires every claim to carry a file and a line (`docs/designs/README.md:22-23`).
- Recommendation: re-derive the four ranges and correct them before sign-off.

---

**Solid as written** — the restore rationale (the two dangling pointers at `.claude/skills/break-ui/SKILL.md:3` and `:16` are real, and the tree confirms the sweep: the three files are deleted in the worktree and still tracked at `HEAD`, and `deploy/check-workspace.sh:229-231` does assert the directory stays gone); the QA/Go/decision/BRD/question count table (379 rows and TC-379 confirmed in `pustaka/docs/product/qa-test-plan.html`, 264 Go tests confirmed by the validator's own probe, and the four count locations confirmed at `qa-test-plan.html:19,79` and `toc/product.js:162,168`); and Alternatives A-E, which are real options with reasons rather than strawmen — the only one missing is the baseline variant noted above.

Counts: B=0 M=13 N=4
