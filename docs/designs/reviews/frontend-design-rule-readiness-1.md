# Design review — M17.5a, the frontend design rule

**Target:** `docs/designs/frontend-design-rule.md` (revision 1)
**Lens:** Readiness (the zero-decision rule — if the implementer would have to ask "which one did
they mean?", the document is not ready)
**Round:** 1 · **Date:** 2026-11-04
**Read:** the design document, the rubric in the prescribed order (`.claude/rules/rule-index.md` →
`.claude/rules/qa-test-plan.md` → `.claude/rules/agent-workspace.md`) and the authoritative
registers the index names (`brd.html`, `decision-log.html`, `open-questions.html`).

---

The design defines an 18-file change list with a trimmed re-vendor and a path-scoped rule. The
rubric loaded, the claims were independently validated against the repository and the three
authoritative product registers.

**Claims confirmed as stated**: the skill is tracked at `HEAD` and deleted in the worktree;
`deploy/check-workspace.sh:213-231` scans skill dirs against `react|jsx|tailwind|sonner|shadcn`
and asserts the directory stays gone; `gaos-frontend.md:24-28` and `:15-28` contradict each other
exactly as described; the `break-ui` pointers are at `:3` and `:16`; the current counts are 379 /
264 / 70 / 50 / 32 and the four QA mirrors plus README, `toc.js` and the changelog are where the
design says.

---

### Trim boundary leaves upstream content unassigned and contradicts itself - Severity: Blocker
- Affected scope: `docs/designs/frontend-design-rule.md:115-134` (D2), `:92-93` (Non-goals), `:79-80` (Goals)
- Finding: D2's keep/drop/rewrite table does not cover the upstream file's last section. `HEAD:.claude/skills/emil-design-eng/SKILL.md` ends with `## Review Checklist` at line 660 (a 12-row table through line 676); that section appears in no verb of the table (keep ends at `## Accessibility` 527-557, drop ends at `## Debugging Animations` 639-658). Its content collides with two decisions in the same document: it is a second motion manual (rows at upstream:670-676 — "Animation on keyboard action", "Duration > 300ms", "Keyframes on rapidly-triggered element", "Motion `x`/`y` props under load", "Elements all appear at once → Add stagger delay"), while the Non-goals say "No new motion guidance" and delegate motion to `review-animations`; and row upstream:669 names "Base UI's `var(--transform-origin)`", a component library, while D2 claims exactly "the three kept passages that name a framework" and the Goals require "plain CSS, plain DOM, one hand-written stylesheet, no component library". The table also contradicts itself on `## Performance Rules`: the keep cell lists it whole (477-525) while the prose drops its `### Motion hardware acceleration caveat` (495-509). Two engineers ship materially different `SKILL.md` files.
- Recommendation: extend D2 to name `## Review Checklist` explicitly — keep-with-listed-rewrites (enumerating the Base UI reference and each motion row) or drop, stating the consequence for the skill's checklist — and rewrite the `## Performance Rules` keep cell as "477-494 + 511-525" so the table and prose agree.

### The three rewrites have no specified text, and check #1 cannot detect an over-trim - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:121-123`, `:136-142`, `:293-295`
- Finding: D2 says only that each rewrite is "the same claim in plain CSS or plain DOM"; D3 says the new section states the boundary "in the skill's own voice"; the frontmatter "gains one clause". No target text, no CSS/DOM, no wording. The upstream description also still advertises "animation decisions" (upstream:3), which the trim drops, so the clause must be more than an append. The only mechanical check (verification row 1, design:295) is "the trimmed `SKILL.md` scores zero on the framework vocabulary" — that passes for an empty file or a file whose guidance was deleted, so it cannot show "the trim is real". Two engineers write different guidance for the layer every frontend session loads.
- Recommendation: give the three rewrite targets verbatim (the exact CSS/DOM snippet that carries the upstream claim), give the `## GAOS edition` section and the frontmatter clause verbatim, and extend check #1 to assert the kept sections survive (e.g. `grep -q '^## Core Philosophy'` and `grep -c '^## '` equals the stated count).

### TC-380's register row, section, kind and the case count are undecided - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:8`, `:251`, `:269`, `:291`
- Finding: The header calls TC-380 a "Forward specification" pointing at `pustaka/docs/product/qa-test-plan.html#go-designgate`, but that anchor is the *implemented* design-gate h3 (`qa-test-plan.html:2640`, "dashboard — the design gate (TC-359…TC-375)"), while the assertion that would justify a forward spec is implemented in this same change (D7, change #8) — and a forward spec is by definition Status `Not implemented` for code that lands later (`.claude/rules/qa-test-plan.md:57-60`). The correct home is `#cw-workspace` (`qa-test-plan.html:2623`, "check-workspace.sh … (TC-357, TC-358)") under `#implemented`. The house also requires a positive **and** a negative case (`.claude/rules/qa-test-plan.md:65-66`); the existing `check-workspace.sh` pair is two rows, TC-357 Positive and TC-358 Negative (`qa-test-plan.html:448-449`), while the design supplies one check and one row — so the count may be 381, not the 380 asserted at design:269. The exact `ok`/`FAIL` string the new assertion prints is not stated either, and the house requires the register to quote it exactly (`.claude/rules/qa-test-plan.md:67-70`).
- Recommendation: state the row in full (ID, Automation, Kind, Scenario, Status), the h3 and anchor it lives under, whether a Negative row (TC-381) is added, and the exact string the validator prints; then derive the counts table and its five mirrors from that.

### The README and version mirrors break two validator assertions the change list omits - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:194-207` (D7), `:265-285`, `:301-309`
- Finding: The plan moves README to "380 cases" and "Product version 0.35.1" (design:274, 280-281, change #14) and requires `./deploy/check-workspace.sh` to pass (design:308). But `deploy/check-workspace.sh:141` asserts README says "379 cases" and `:142` asserts "Product version 0.35.0" — both fail after the change. D7 enumerates exactly two edits to that file (vocabulary-scan scope and the inverted assertion, design:196-207), and the counts section only notes that the validator's case-count prose strings move (design:280-281); the README and version assertions appear nowhere in the document.
- Recommendation: add both literal updates to D7 and to the change-list description of `deploy/check-workspace.sh`, or drop the README/version edits from the plan.

### The rule's path scope does not cover the frontend work the agent owns - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:77-78`, `:144-170` (D4)
- Finding: The goal is "Every frontend change loads it" (design:77-78), but the rule's frontmatter is `dashboard/templates/**` and `dashboard/static/**` (design:150-155). The frontend agent also owns "the view-model glue that feeds them (`dashboard/render.go`, `dashboard/handlers_bo.go`'s `buildBO`)" (`.claude/agents/gaos-frontend.md:9-13`); both files exist, and a change touching only `dashboard/render.go` loads no design rule, so the taste layer does not load. Whether view-model work counts as "frontend work" is exactly the case where two engineers differ.
- Recommendation: either add the view-model paths to the rule's `paths` list, or state that the scope is deliberately templates-and-static and amend the goal sentence to match.

### The cross-harness rule symlink is missing from the change list - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:144-170`, `:238-261`
- Finding: The house wiring is "`.agents/rules/*.md` are relative symlinks into `.claude/rules/`, so a rule is one file read by two harnesses" (`.claude/rules/agent-workspace.md:112-125`), and `.agents/rules/` currently holds exactly one symlink per rule file (`agent-workspace.md`, `qa-test-plan.md`, `rule-index.md` → `../../.claude/rules/…`). Creating `.claude/rules/frontend.md` without `.agents/rules/frontend.md` leaves the Antigravity harness without the rule, and the change list is required to be complete (`docs/designs/README.md:24-25`). The design never mentions `.agents/`.
- Recommendation: add `.agents/rules/frontend.md` to the change list with the exact symlink target `../../.claude/rules/frontend.md`.

### No phases, exit conditions or dependency order - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:238-309`
- Finding: The document has no phase partition, no per-phase exit condition and no dependency statement, although the rubric requires each phase to leave the tree buildable and testable with dependencies stated. The dependency is consequential: the new rule names a skill that does not exist until change #1 lands, so writing `.claude/rules/frontend.md` first puts a rule pointing at a missing skill into every frontend session — the defect this change exists to repair — and the inverted validator assertion fails until the skill is restored.
- Recommendation: add phases with one exit condition each and an explicit order (skill before rule; rule before the `rule-index` row; register row after the validator's exact assertion string is fixed), e.g. P1 restore-and-trim the skill, P2 rule + index row + `.agents` symlink, P3 agent file and workspace rule, P4 validator inversion, P5 register, mirrors, README, changelog.

### Most changes have no verification, and "the register's row for each" is false - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:289-299`
- Finding: "One row per check, and the register's row for each" (design:291) is wrong on both halves: the table has five checks but only one TC, the other four carrying "—"; and the rows for `rule-index.md` (246), `agent-workspace.md` (247), `gaos-frontend.md` (248), `NOTICE` (250), `toc.js` (253), `changelog.html` (254), `qa-test-plan.md` (256) and the three review reports (258-259) have no verification line naming them. A change with no verification line is a change nobody will notice failing.
- Recommendation: add one verification row per changed file with the command and the expected result (e.g. `grep -c "emil-design-eng" .claude/rules/frontend.md` = 1; `grep -n "emil-design-eng" NOTICE` = 1; `grep -n "0.35.1" pustaka/docs/assets/toc.js`; `grep -n "v0-35-1" pustaka/docs/changelog.html`), and either give every check a TC or delete the claim that each has one.

### Check #4 can never pass, and the canary inventory in the design is wrong - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:298`, `:311-316`
- Finding: Check #4 is `grep -rn "EGM-MARKER-" .` → nothing. The literal prefix appears in the design document itself (design:298), in `docs/designs/egm-design-review.md`, in four existing reports under `docs/designs/reviews/`, in `.claude/skills/gaos-design-review/SKILL.md`, in `deploy/check-workspace.sh`, in `AGENTS.md`, `GEMINI.md`, `.claude/CLAUDE.md` and the three rule files. The house validator deliberately matches a canary *value* (`EGM-MARKER-[A-Za-z0-9._-]*[0-9a-f]{8,}`, `deploy/check-workspace.sh:176`) and states that prose about the prefix is fine (`deploy/check-workspace.sh:169-176`; `docs/designs/reviews/README.md:34-39`), so check #4 as written fails on the design's own text. Separately, the isolation paragraph says this round's canary is planted in `.claude/rules/frontend.md`, `rule-index.md` and `agent-workspace.md` (design:314-316), but `frontend.md` does not exist; the planted values are in `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md` plus `AGENTS.md`, `GEMINI.md` and `.claude/CLAUDE.md` — `qa-test-plan.md`, which change #15 edits, is omitted, and three of the six are files no change in the plan touches. `deploy/check-workspace.sh` currently fails on exactly these six.
- Recommendation: replace check #4 with the house value pattern and the house file set (e.g. `grep -rnE 'EGM-MARKER-[A-Za-z0-9._-]*[0-9a-f]{8,}' .claude/ AGENTS.md GEMINI.md docs/designs/reviews/` → nothing), and state who removes the six planted canaries and when, naming the three rule files that changes #5, #6 and #15 edit.

### The review-report path is a glob that does not match the house report names - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:258`, `:361-364`
- Finding: Change #17 lists `docs/designs/reviews/frontend-design-rule-*-review-1.md`. The store's convention in practice is `<feature>-<lens>-<n>.md` — it already holds `egm-design-review-review-1.md`, `egm-design-review-critic-1.md`, `egm-design-review-zoomout-1.md`, `egm-design-review-security-1.md`, `egm-design-review-readiness-1.md` and `m17-design-gate-critic-1.md` — while `docs/designs/reviews/README.md:3-4` names `<feature>-review-<n>.md` and `<feature>-security-review-<n>.md`. The glob matches `frontend-design-rule-review-1.md` and `frontend-design-rule-security-review-1.md` but not `…-critic-1.md`, `…-zoomout-1.md`, `…-readiness-1.md`; the five lens names are known in advance, so a glob is not warranted (a glob is acceptable only for filenames that cannot be known).
- Recommendation: list the five exact report paths plus `frontend-design-rule-consolidated-1.md` in the change list.

### The edit to `.claude/rules/qa-test-plan.md` is not pinned to exact strings - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:256`, `:265-285`
- Finding: Change #15 says the file gets "the counts and the next free number". That file carries three different numbers for the same quantity: "The highest in use is `TC-379`; next free number is `TC-380`" (`.claude/rules/qa-test-plan.md:50-51`), "They are currently all 'three hundred and seventy-five'" (`:73-74` — already stale; the register says 379), and "379 rows in total" (`:127`). After this change the highest is TC-380, the next free number is TC-381, and the total is 380 or 381 depending on the TC-380 decision above. "The counts" does not say which literals move, so one engineer rewrites 375→380 and another leaves the stale 375. The Automation→Cases map at `:113-125` also lists no `check-workspace.sh` row even though TC-357/358 exist, so the new row would widen that pre-existing gap.
- Recommendation: spell out the exact before/after strings for lines 50-51, 73-74, 81 and 127 (including correcting 375), and state whether the map gains a `check-workspace.sh` row.

### "Sixteen files outside `docs/designs/`" is not the arithmetic of the change list - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:261`
- Finding: The change list has 18 rows; rows 16-18 are under `docs/designs/`, so 15 files are outside it, not sixteen. The house contract requires that the counts follow from the change list and that the arithmetic be written down (`docs/designs/README.md:26-27`), and this count is the scope statement for a write allowlist — an implementer reconciling "sixteen" against the table has to guess which file is missing.
- Recommendation: change "Sixteen" to "Fifteen" and write the arithmetic ("18 rows − 3 under `docs/designs/` = 15").

### Whether this ruling belongs in the product decision log is not decided - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:100-215` (D1-D8), `:271`
- Finding: The counts table asserts "decisions 70 → 70 | no new ruling" (design:271), but the document itself records eight numbered rulings and five rejected alternatives, one explicitly the operator's choice ("The operator chose a trimmed edition", design:104). The house precedent logs exactly this class of change: D-063 records a third-party vendoring/licensing ruling, and D-068/D-069 record the design-gate process rulings that were also accompanied by BRD rules. No house rule states that every design decision must be logged, so this is an open question rather than a confirmed defect — but the design asserts it away without saying on what basis. If the operator rules it belongs in the log, the count becomes 71 and at least five strings move (the "Seventy entries" prose at `pustaka/docs/product/decision-log.html:18`, `check-workspace.sh:127`, and the decision mirrors in `pustaka/docs/assets/toc/product.js:66,129,162,168`), none of which is in the change list.
- Recommendation: state explicitly whether a D-071 entry is created (and, if so, add `pustaka/docs/product/decision-log.html` and the `toc/product.js` mirrors to the change list and the counts table), or record why a workspace rule is not a product decision.

### The upstream vocabulary count in D1 is wrong - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:104-113`, `:322-325`
- Finding: D1 and Alternative A state the upstream file "scores **7**" under the validator's grep and list nine lines. The file at `HEAD` yields 11 matches on 10 lines — 162, 336, 338, 350, 499, 542, 559, 561, 567 (twice), 577 — and the directory scan the validator runs also yields 11 (`LICENSE` 0, `SKILL.md` 11, `SOURCE.md` 0); line 559 is missing from the design's list. The rejection of Alternative A still holds, and the acceptance condition ("must score zero", design:133-134) is unaffected.
- Recommendation: correct "7" to 11 matches on 10 lines in D1 and Alternative A, and add 559 to the cited line list.

### The prose deliverables have no specified text - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:169-170`, `:209-215`, `:253-255`
- Finding: Several text changes are described but not written: the `rule-index` table row (design:169-170), the `NOTICE` third-party bullet (design:209-215), and the changelog entry (design:254). The changelog entry in particular carries a choice the docs component vocabulary makes load-bearing — the release uses `<span class="bump patch">` for a patch (`.bump.patch` is defined at `pustaka/docs/assets/site.css:365`; the file currently uses 35 `bump minor` and 4 `bump patch`) — and the house convention is a tagline paragraph plus `<li>` entries with `chg add`/`chg change` spans (`pustaka/docs/changelog.html:39-45`).
- Recommendation: give the exact rule-index row, the NOTICE bullet and the changelog section (including the `bump patch` class and the entry list) as the text to write.

### `SOURCE.md`'s own "Do not edit in place" line survives a trim that edits it in place - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:209-215` (D8), `:172-183` (D5)
- Finding: The restored `SOURCE.md` at `HEAD` ends "Do not edit in place: GAOS-specific guidance lives in .claude/agents/gaos-frontend.md." The plan amends that file to record the trim (change #3, D8) while the workspace rule records an exception for exactly that edit (D5). Whether the existing sentence is replaced, qualified or left standing is not stated, and the three outcomes leave three different provenance files.
- Recommendation: state that the "Do not edit in place" sentence in `SOURCE.md` is replaced by the recorded-trim exception wording, and give that wording.

Counts: B=1 M=12 N=3
