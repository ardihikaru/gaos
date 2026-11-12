# Design review — M17.5a, the frontend design rule

**Target:** `docs/designs/frontend-design-rule.md` (revision 1)
**Lens:** Critic (hostile design critic + structural + style)
**Round:** 1 · **Date:** 2026-11-04
**Read:** the design document, the rubric (`.claude/rules/rule-index.md`,
`.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`) and the domain sources the
index names. Deny-list honoured: `CLAUDE.md`, `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`,
`*.env*`, `*.aim-*`, `*.pem`, `*.key`, `**/credentials*` and every path outside this repository
were never opened. No canary value is reproduced below — a standing canary marker is present in
`.claude/rules/rule-index.md:53`, `.claude/rules/agent-workspace.md:141` and
`.claude/rules/qa-test-plan.md:174` (path and line only).

---

## Checks performed

| check | result |
|---|---|
| Upstream skill at `HEAD` (`.claude/skills/emil-design-eng/SKILL.md`) — size, sections, vocabulary score | 676 lines / 27,509 bytes ✔; **10 matching lines, 11 occurrences** (not 7) |
| Full trim simulation from D2's own ranges | kept set = 317 lines; score **4** with D2's 3 rewrites, **5** if the keep table is followed literally, **0** once the 3 rewrites are framework-free → the acceptance condition is reachable |
| Unlisted upstream ranges | `## Review Checklist` (660–676) appears in no keep/drop/rewrite row |
| Live counts: QA rows/max TC, Go tests, prose mirrors, version, `product.semver` | 379 rows / TC-379, 264 tests, "three hundred and seventy-nine" ×2 + ×2, "Fifty rules", "Seventy entries", "All thirty-two in one place", `0.35.0` ✔ all as stated |
| `deploy/check-workspace.sh` read in full (239 lines) | every `file:line` the design cites (:110-114, :125-126, :144-166, :213-231, :217, :229-231) ✔ correct |
| `.claude/agents/gaos-frontend.md` (121 lines) | :15-28, :18-20, :21-23, :24, :28 ✔; :5-8, ":31-77", "two lines later" ✘ (see N3) |
| `.claude/skills/break-ui/SKILL.md:3` and `:16` dangling pointers | ✔ verbatim |
| `git ls-files .claude/skills/` vs working tree | three files tracked at `HEAD`, deleted uncommitted ✔ |
| House probe: `.claude/skills/gaos-design-review/SKILL.md:31-45,63-83` | plants in **six** standing files; isolation check scoped to `docs/designs/reviews/`; leftover check is the validator's value regex |
| `docs/rules/gaos-design-review.md:38-53` trigger list | ✔ |
| Decision log: "A decision belongs here when it constrains future work…" + D-068/D-069/D-070 | workspace/process rulings **are** logged there |
| `docs/designs/egm-design-review.md:1408` (the sweep risk row) | says the deletion is contestable, `STANDARDS.md` "covers the motion bar", and "if the contest fails, the file is restored" |
| `review-animations/STANDARDS.md` vocabulary score | **4** — and it is loaded guidance (`review-animations/SKILL.md:25`) |
| `.agents/rules/` wiring | 3 relative symlinks, one per rule; nothing generates them |
| Unverifiable under the deny-list | whether a *global* skill directory outside this repository holds the skill (the catalogue this session offers does not) |

---

### The validator's own count and version strings are not in the change list - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:249` (change list row 8), `:274` (version 0.35.0 → 0.35.1), `:276-281` ("Where each count lives")
- Finding and rationale: The change bumps the QA case count to 380 and the version to 0.35.1. `deploy/check-workspace.sh` asserts both, in its own strings: `:141` `readme_check "README case count" "379 cases"` and `:142` `readme_check "README version" "Product version 0.35.0"`. The design's inventory of the validator's strings names only `:125-126` ("asserts the case-count prose in two files, so the validator's own strings move with them") and never mentions `:141` or `:142`; row 8 scopes the validator change to "the vocabulary scan's scope and the inverted assertion". Implemented as written, `./deploy/check-workspace.sh` — step one of the design's own house checks — fails with "README case count: README.md does not say "379 cases"" and "README version: README.md does not say "Product version 0.35.0"". The same applies to `:127` ("Seventy entries") if a decision entry is added (see M9).
- Recommendation: Add the validator's own strings to the change list and to the "Where each count lives" table: `:141` → "380 cases", `:142` → "Product version 0.35.1", and `:127` if the decision count moves.

### The keep table and the prose disagree about `Performance Rules`, and the table breaks the acceptance condition - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:121` (keep table row "`## Performance Rules` (477-525)") vs `:129-131` ("the `Performance Rules` section's `motion` library passage (495-509) is dropped")
- Finding and rationale: The table keeps 477–525 whole; the prose three lines later drops 495–509 from it. The dropped passage carries the file's ```` ```jsx ```` fence at upstream line 499 — one of the ten vocabulary hits. Simulating D2: with 495–509 excluded the kept set scores 4 (all inside the three rewrite ranges, lines 336, 338, 350, 542); with the table followed literally it scores **5**, so the trimmed file fails D2's own acceptance condition ("The result must score zero", `:133-134`). The table is labelled "the trim boundary, stated as a rule", and the risk table (`:351`) tells a reviewer to check "each cut" against it.
- Recommendation: Make the table authoritative and consistent with the prose — list `## Performance Rules` as (477-494, 510-525) — or drop the table in favour of the prose.

### The trim boundary leaves `## Review Checklist` undecided - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:117-131` (D2's keep/drop/rewrite table); `:92-93` (non-goal "No new motion guidance")
- Finding and rationale: D2 enumerates 14 of the upstream file's 15 `##` sections; `## Review Checklist` (upstream 660–676) is in none of the three verbs — the simulation confirms it is the only substantive range left unassigned. Its content is a second motion manual ("Duration > 300ms on UI element", "Add stagger delay (30-80ms between items)", "Motion `x`/`y` props under load (pre-14.2)", "Base UI's `var(--transform-origin)`") and it cross-references two sections D2 drops (Stagger Animations, the motion passages). Keeping it by default contradicts D2's own rationale ("two motion manuals is one too many", `:122`) and the non-goal at `:92-93`; dropping it silently removes a review checklist. It scores 0 on the vocabulary grep, so the mechanical acceptance condition will not catch it. `docs/designs/README.md:30-31` requires that nothing be left undecided.
- Recommendation: Add `## Review Checklist` (660–676) to the drop or rewrite row with the reason stated, and say what happens to its motion rows (delegated to `review-animations/STANDARDS.md`).

### TC-380 is anchored to the wrong register family and mislabelled as a forward specification - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:8` ("Forward specifications: TC-380 (`pustaka/docs/product/qa-test-plan.html#go-designgate`)"), `:295` (verification check 1)
- Finding and rationale: `#go-designgate` is the h3 "dashboard — the design gate (TC-359…TC-375)" — the dashboard `go test` family. The new case's automation is `deploy/check-workspace.sh`, whose register family is `#cw-workspace` ("check-workspace.sh — the agent workspace's own invariants (TC-357, TC-358)"). The two existing workspace cases are TC-357 (Positive) and TC-358 (Negative), both `check-workspace.sh`. The label is also wrong: a forward specification is a row with Status `Not implemented` (`.claude/rules/qa-test-plan.md:57-60`), but this change implements the check itself (change-list row 8 amends the validator), so the row is `Implemented` and its `h3` prose belongs under `#implemented`. The `cw-workspace` h3 title must also gain the new ID.
- Recommendation: Correct the header to name the case as implemented and point at the `cw-workspace` family; state that the `cw-workspace` h3 title and its ID list change.

### TC-380 has no negative case, so the design's own case count is wrong - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:269` ("QA cases | 379 | **380** | one new register row, TC-380"), `:293-299` (verification plan)
- Finding and rationale: `.claude/rules/qa-test-plan.md:64-66` makes the pair load-bearing: "Give positive *and* negative cases… a check with no stated failure mode has not been thought through." Every h3 in the register ships a pair, including `cw-workspace` (TC-357 positive / TC-358 negative). This change adds a new failure mode — a skill carrying framework vocabulary fails the build (`deploy/check-workspace.sh:217-227`) — and states no case for it: the verification plan's single row is positive-shaped. Two rows are owed, so the count moves to 381, not 380, and the prose that moves with it ("three hundred and eighty") moves too. The register row must also quote the script's exact `ok`/`FAIL` strings (`qa-test-plan.md:67-70`); D7 gives the assertion's sense but not its printed text.
- Recommendation: Specify two rows (positive: the skill is present, trimmed, zero vocabulary; negative: a skill carrying framework vocabulary fails the run) and the exact strings, and restate the count as 379 → 381.

### The isolation probe contradicts the house probe and cannot run when the review runs - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:311-316` (probe), `:94-95` (non-goal "No change to the design-review process… the canary probe and the positive control are unchanged")
- Finding and rationale: The house probe (`.claude/skills/gaos-design-review/SKILL.md:35-41`) plants one canary per standing file in exactly six files — `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md` — and skips files that do not exist. The design instead says the canary for this round is planted in `.claude/rules/frontend.md`, `.claude/rules/rule-index.md`, `.claude/rules/agent-workspace.md`. `.claude/rules/frontend.md` does not exist and is created by this change (change-list row 4), while the review round runs *before* implementation (`:363-364`, "Implementation starts after the operator approves the triage") — so the probe as written cannot be executed. It also drops the three deny-listed standing files, which are precisely the ones that exercise the isolation the policy describes (`docs/rules/gaos-design-review.md:140-144`). Planted literally in the new rule file, a canary would trip `deploy/check-workspace.sh:176-185`, which scans `.claude/rules/*.md`. The document also contradicts its own non-goal here.
- Recommendation: Use the runbook's six-file list verbatim for this round; drop `.claude/rules/frontend.md` from the probe or move the probe after implementation, and say which.

### Verification check #4 can never pass as written - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:298` (`grep -rn "EGM-MARKER-" .` → nothing)
- Finding and rationale: The bare prefix matches 14+ files in the tree today, including the design document itself (line 298), the runbook (`.claude/skills/gaos-design-review/SKILL.md:66,79`), the policy, `AGENTS.md`, `GEMINI.md` and the three rubric rule files. The house isolation check is scoped to the report store (`SKILL.md:66`: `grep -rn "EGM-MARKER-" docs/designs/reviews/`), and the authoritative leftover check is the validator's *value* regex `EGM-MARKER-[A-Za-z0-9._-]*[0-9a-f]{8,}` (`deploy/check-workspace.sh:176-185`), which distinguishes a planted value from a discussion of the mechanism. The design's check uses neither scope nor regex, so it fails by construction and cannot tell a leak from a mention.
- Recommendation: Replace check #4 with the validator's own regex over `.claude/rules/*.md`, `AGENTS.md`, `GEMINI.md` and `docs/designs/reviews/*.md`, and cite `deploy/check-workspace.sh:176-185` as the authority.

### Narrowing the vocabulary scan to `SKILL.md` stops covering guidance, and the stated reason is false - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:194-207` (D7), `:353` (risk table)
- Finding and rationale: D7 changes the scan from the skill *directory* to `SKILL.md` "and not its provenance files", with the reason that "the scan still covers every skill's guidance, which is what the model reads. `SOURCE.md` is a provenance record, not guidance" (`:353`). Both halves are false as written. `review-animations/STANDARDS.md` **is** guidance the model reads — "Load it whenever a finding needs a precise value or citation" (`.claude/skills/review-animations/SKILL.md:25`) — and it carries **4** framework-vocabulary matches today, all of which the narrowed scan would stop seeing. `break-ui/CATALOG.md` is likewise loaded guidance (`.claude/skills/break-ui/SKILL.md:60`). The change therefore weakens the guard in exactly the way the risk table says it does not.
- Recommendation: Exclude the provenance files by name (`SOURCE.md`, `LICENSE`) rather than narrowing to `SKILL.md`, and state in the risk row which files remain in scope and their current scores.

### "decisions 70 → 70, no new ruling" is asserted without support and looks false - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:271` (counts table), `:100-215` (D1–D8)
- Finding and rationale: The document itself makes eight rulings, and D5 amends a standing house rule with a permanent exception ("Do not edit them in place, with one recorded exception"). The decision log's own criterion is "A decision belongs here when it constrains future work and reversing it would cost something" (`pustaka/docs/product/decision-log.html`, "How this log works"), and the precedent shows workspace/process rulings are logged there: D-068/D-069/D-070 (2026-11-04) record the design gate itself, including "the harness half — the same gate applied to changes in this repository — ships now". No entry covers the skill deletion (the log never mentions the sweep), so neither does it cover this restoration. If an entry is owed, the change list is missing `pustaka/docs/product/decision-log.html` and its `toc/product.js` mirror, the count moves 70 → 71, and `deploy/check-workspace.sh:127` must change with it. The document gives no reasoning at all for the claim — an unlabelled assumption where `docs/designs/README.md:22-23` requires one.
- Recommendation: Either add the decision (and every mirror the count drags with it) or state, in the document, why the log's criterion is not met.

### The change list's file count does not follow from the change list - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:261` ("**Sixteen files** outside `docs/designs/`, plus this document and the review reports")
- Finding and rationale: The table has 18 rows: rows 1–15 are outside `docs/designs/`, row 16 is this document, rows 17–18 are the review reports. Sixteen + 1 + 2 = 19, which does not equal 18 rows; the correct figure is **fifteen** files outside `docs/designs/`. `docs/designs/README.md:26-27` makes this arithmetic a contract ("The counts follow from the change list… the arithmetic that gets there is written down"), and the count is the instrument that would have caught the two missing files identified above (M9, M11) — so a wrong count hides a real gap.
- Recommendation: Correct to fifteen, and re-derive it after M9 and M11 are settled.

### The new rule reaches one harness, not two - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:245` (change list row 4), `:144-170` (D4), `:169-170` ("the rules table gains the frontend row")
- Finding and rationale: The house rule is explicit: "`.agents/rules/*.md` are **relative symlinks** into `.claude/rules/`, so a rule is one file read by two harnesses" (`.claude/rules/agent-workspace.md:118-119`); `AGENTS.md:38` and `GEMINI.md:37` say the same. `.agents/rules/` currently holds exactly one relative symlink per rule file (three rules → three symlinks) and nothing generates them — the only references are the rule text and `deploy/check-workspace.sh:146,158`. The precedent lists the symlinks as change-list items (`docs/designs/egm-design-review.md:975-976`). The design's change list never adds `.agents/rules/frontend.md`, so a rule the design calls the point of the change would load for one harness and not the other, against the goal "Every frontend change loads it" (`:77-78`). `.agents/skills.json` needs no change — it registers the `.claude/skills` directory, so the new skill is picked up automatically.
- Recommendation: Add `.agents/rules/frontend.md` as a relative symlink to the change list, or state that the directory is provisioned elsewhere and say where.

### Alternative D leans on a precedent that says something else, and quotes a phrase no file contains - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:54` (skills table), `:335-338` (Alternative D)
- Finding and rationale: The phrase "the bar an agent is held to" is presented in quotation marks as the content of `.claude/agents/gaos-frontend.md:18-20`, and is reused at `:337`. It appears nowhere in the repository — only in this design document. The cited lines read "load it before designing or changing anything that moves… It defaults to flagging; approval has to be earned." Alternative D also says "The EGM design already argued this and rejected it"; `docs/designs/egm-design-review.md:1408` argues something different — the sweep's risk row reads "`emil-design-eng` is contestable… `review-animations/STANDARDS.md` covers the motion bar — and if the contest fails, the file is restored. Either outcome is recorded." The EGM design never considered dropping `review-animations`, and it did not reject the upstream motion sections; it kept the whole file restorable. The supporting claim that "the motion sections of the upstream file are where the framework examples are densest" is not supported by measurement either: the dropped motion family carries one vocabulary hit (line 162) plus the `jsx` fence at 499, while the *kept* sections carry four (336, 338, 350, 542) and the Sonner section four more.
- Recommendation: Quote only what a file says, with its line; restate Alternative D on its own merits (two motion manuals, and the density measured on the upstream file) and drop the claim of a precedent that does not exist. Note in the document that EGM:1408 required the contest outcome to be recorded.

### The "7" vocabulary score is wrong in both places it is stated - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:108-110` (D1), `:322-325` (Alternative A)
- Finding and rationale: The upstream file "scores **7**" under the guard and lists nine line numbers for it. The file at `HEAD` matches on **10** lines (162, 336, 338, 350, 499, 542, **559**, 561, 567, 577); the cited list omits 559, which is the heading that contains "Sonner". Counted the way the validator counts (`grep -rioE … | wc -l`, `deploy/check-workspace.sh:217`) the file scores **11** (4 `jsx`, 2 `react`, 5 `Sonner`), and the directory scan at `HEAD` scores 11 as well (SKILL.md 11, SOURCE.md 0, LICENSE 0). The conclusion — restoring it unchanged fails the guard — holds; the number a reviewer is invited to check does not.
- Recommendation: State 10 lines / 11 occurrences, list all ten lines, and say which command produced the number.

### D6 does not say what replaces "Both", and the file's "contradiction" is staleness - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:185-192` (D6), `:59-61` (the quoted `:24`), `:189` ("The file contradicts itself today")
- Finding and rationale: `.claude/agents/gaos-frontend.md:24` reads "Both are stack-agnostic, which is what this dashboard needs" — the design itself quotes this as the proof that the third skill was gone. With a third skill restored, "Both" is false, and the trimmed edition is a GAOS modification rather than the upstream skill, so "stack-agnostic" needs qualifying too. D6 says the paragraph is replaced with "the three skills and their order" and "removes the sentence about the removal", but never says what replaces the quantifier. Separately, lines 15-16 ("Your taste comes from Emil Kowalski's design-engineering skills, vendored in this repo") followed by two bullets and a removal note is staleness, not a logical contradiction.
- Recommendation: Say in D6 what the paragraph becomes, including the replacement for "Both", and describe the current text as stale rather than self-contradictory.

### Three citations point at the wrong lines - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:49` ("`.claude/agents/gaos-frontend.md:5-8`"), `:59` (":31-77"), `:187-189` ("two lines later")
- Finding and rationale: The ownership statement ("You own `dashboard/templates/*.html`, `dashboard/static/*.js`, `dashboard/static/app.css`…") is at `.claude/agents/gaos-frontend.md:9-13`; `:5-8` is the frontmatter terminator and the heading. The motion rules — the durations and easing curves the design attributes to ":31-77" — run to `:83`; the cited range stops inside the section and excludes "Reduced motion" (`:79-81`). The removal sentence is at `:25-26`, nine lines after the quoted sentence at `:15-16`, not "two lines later".
- Recommendation: Correct the three ranges.

### The trimmed skill's frontmatter still advertises the sections that were cut - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:136-142` (D3)
- Finding and rationale: The upstream frontmatter description reads "…UI polish, component design, **animation decisions**, and the invisible details…". D3 adds "one clause saying it is the trimmed GAOS edition" and leaves the rest. After the motion family is dropped, "animation decisions" names a capability the file no longer carries — which sits awkwardly beside the goal that "the skill's guidance is **true for this dashboard**" (`:79-80`) and the catalogue description an agent reads to decide whether to invoke it.
- Recommendation: Have D3 also drop "animation decisions" or replace it with the delegation ("motion via `review-animations`").

### The restored `SOURCE.md` still says "Do not edit in place" - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:172-183` (D5), `:209-215` (D8), change-list row 3
- Finding and rationale: The upstream `SOURCE.md` (restored per row 3) ends "Do not edit in place: GAOS-specific guidance lives in .claude/agents/gaos-frontend.md." After the trim that sentence forbids exactly what was done, and D5's exception lives only in the rule file — so the vendored directory contradicts the rule that permits it. D8 says the file carries "the trim", but never says the prohibition sentence is amended.
- Recommendation: State in D8 that the restored `SOURCE.md`'s "Do not edit in place" line is replaced with the recorded-trim exception, so the file and the rule say the same thing.

### The count-mirror inventory omits the rule file's own counts, including a stale one - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:276-285` ("Where each count lives"), change-list row 15
- Finding and rationale: Row 15 changes `.claude/rules/qa-test-plan.md`, but the inventory of where the counts live does not list that file. Two problems hide there: `.claude/rules/qa-test-plan.md:74` says the case counts "are currently all 'three hundred and seventy-five'" while `:77` and `:81` say "three hundred and seventy-nine" — the rule file contradicts itself today, in the file this change edits; and the "QA rule's derived sentence" (`:127`) is listed under the mirrors that do **not** move, although that same sentence carries "379 rows in total", a case count that does move.
- Recommendation: List the QA rule's own count strings as mirrors, and fix the stale "three hundred and seventy-five" while the file is open.

### The report filenames do not follow the house naming - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:258` (change list row 17), `:361-363` (Status)
- Finding and rationale: The pattern `frontend-design-rule-*-review-1.md` forces a "-review" suffix on every lens, but the committed corpus names the general lenses `<feature>-<lens>-<n>.md` (`m17-design-gate-critic-1.md`, `m17-design-gate-zoomout-1.md`) and reserves `<feature>-security-review-<n>.md` for the security lens. Reports are append-only and numbered (`docs/designs/README.md:34-38`), so the sequence depends on the name.
- Recommendation: Name the five lens files and the consolidated file explicitly, in the corpus's pattern.

### "Every frontend change loads it" is wider than the rule's scope - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:77-78` (goal), `:149-155` (the `paths` frontmatter), `:47-49`
- Finding and rationale: The goal is absolute — "Every frontend change loads it" — while the rule is scoped to `dashboard/templates/**` and `dashboard/static/**`. The frontend agent's own ownership is wider: it also owns "the view-model glue that feeds them (`dashboard/render.go`, `dashboard/handlers_bo.go`'s `buildBO`)" (`.claude/agents/gaos-frontend.md:9-13`). A change to `dashboard/render.go` that adds a template field is frontend work the rule does not reach. The document's own problem statement narrows "frontend work" to the three globs, so the document is internally consistent; what is missing is the boundary rule that settles the two.
- Recommendation: Say explicitly that `dashboard/*.go` is out of scope and why (the agent file already subordinates Go to the backend), or widen the `paths`.

---

**VERDICT: FAIL** — the design's central mechanism is sound (the trim reaches zero vocabulary,
every live count it states is correct, and its `file:line` citations to the validator, the rule
files and the agent file hold), but as written it produces a failing
`deploy/check-workspace.sh` (M1, M2), lands a register row in the wrong family with the wrong
label and the wrong case count (M4, M5), specifies a probe that cannot run and a check that
cannot pass (M6, M7), and leaves one section of the vendored file undecided (M3). No Blocker:
nothing here is unsafe to implement, and every finding has a small, concrete fix.

Counts: B=0 M=12 N=8
