# Design review — M17.5a, the frontend design rule

**Target:** `docs/designs/frontend-design-rule.md` (revision 1)
**Lens:** Comprehension (can a naive reader follow it, and say it back?)
**Round:** 1 · **Date:** 2026-11-04
**Read:** the design document, the rubric (`.claude/rules/rule-index.md`,
`.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`) and the domain sources the
index names. Every `file:line` claim was re-derived from the tree.

---

## What this proposes

1. The operator asked for one rule: frontend work always loads the `/emil-design-eng` skill. That
   skill is not in the tree — it was deleted on 2026-11-04, uncommitted, as part of a sweep — so
   the request is unsatisfiable as stated, for three stated reasons (the name resolves nowhere;
   the rule layer has no frontend row to amend; the deletion was correct for a Go `html/template`
   + HTMX stack).
2. The document therefore specifies a **trimmed re-vendor**: bring the upstream file back, cut the
   motion and framework-specific parts, keep the name, and write the rule that mandates it.
3. Goals: the name resolves; every frontend change loads it; the guidance is true for this stack;
   the trim is recorded and checkable; licence discipline holds. Non-goals: no dashboard change,
   no change to `review-animations`/`break-ui`, no new motion guidance, no change to the
   design-review process, no Go code.
4. Eight decisions (D1–D8): trim rather than restore whole; state the trim boundary as a
   keep/drop/rewrite rule against the upstream's 676 lines; keep the name and add a GAOS-edition
   note; create the path-scoped rule `.claude/rules/frontend.md`; give the vendored-skill rule a
   recorded-trim exception; rewrite the frontend agent's skill paragraph; invert the validator's
   assertion from *absence* to *trimmed presence*; record provenance in `SOURCE.md` and `NOTICE`.
5. A mechanism diagram (path-scoped rule → three skills), a change list of 18 rows, the counts
   that move (QA 379→380; Go tests, decisions, BRD rules, questions unchanged; version
   0.35.0→0.35.1) with the location of each count, a five-row verification plan plus the house
   checks and the isolation probe, five rejected alternatives, and a risk table.
6. Status: In Review (revision 1); implementation starts after the operator approves the triage.

## How the existing system works, per this document

- The dashboard's frontend is `dashboard/templates/*.html`, `dashboard/static/*.js`,
  `dashboard/static/app.css` — server-rendered Go `html/template` plus HTMX and plain JS, one
  hand-written stylesheet, no component library.
- Two skills stand behind it: `review-animations` (the motion bar, its `STANDARDS.md`) and
  `break-ui` (the adversarial edge-case pass). The third — the "taste" layer — was
  `emil-design-eng`, vendored from Emil Kowalski, and is gone; the agent file says "Both are
  stack-agnostic" because the third was, and `break-ui` still points at it.
- `deploy/check-workspace.sh` scans every skill directory that is not on the `break-ui
  review-animations` baseline for `react|jsx|tailwind|sonner|shadcn` and fails on a hit, and
  asserts the `emil-design-eng` directory stays gone.
- `.claude/rules/` holds path-scoped rules; `rule-index.md` is the always-on index and has no
  frontend row; `agent-workspace.md` forbids editing vendored skills in place.
- The QA register holds 379 cases, highest `TC-379`, next free `TC-380`; counts live as prose in
  the register, its ToC mirrors, `README.md` and the changelog; a forward specification is a row
  with Status `Not implemented`, prose under `<h2 id="not-implemented">`.
- The design gate has three roles, five lenses, a canary probe with a positive control, and
  reports filed under `docs/designs/reviews/`.

## Verified clean

The following claims were checked and hold: the upstream file is 676 lines / 27,509 bytes and is
still tracked at `HEAD` with the working tree deletion uncommitted;
`deploy/check-workspace.sh:213-228`, `:217`, `:229-231`, `:144-166`, `:110-114`, `:125-126` all
point where the document says; every section range in the D2 keep/drop table is correct; the
three rewrite ranges sit inside kept ranges; `.claude/skills/break-ui/SKILL.md:3` and `:16` carry
the dangling pointers; `.claude/rules/agent-workspace.md:1-4`, `:38-47`, `:42-45` are accurate;
`docs/rules/gaos-design-review.md:38-53` is the trigger list; `NOTICE:30-44` is the third-party
section; the upstream `SOURCE.md` carries URL, commit and MIT licence; all six current counts
(379 / 264 / 70 / 50 / 32 / 0.35.0) and their mirror locations are correct; `product.semver` is
`pustaka/docs/assets/toc.js:22`.

---

### The trim boundary omits the upstream's last section - Severity: Blocker
- Affected scope: `docs/designs/frontend-design-rule.md:115-134` (D2, "The trim boundary, stated as a rule")
- Finding: D2 asserts "The trim is decided by three verbs" and gives a table that reads as exhaustive. It is not exhaustive. The upstream `SKILL.md` ends with `## Review Checklist` at line **660** (through676). No keep range (8-16, 18-36, 38-60, 197-344, 346-391, 477-525, 527-557), no drop range (62-146, 147-196, 393-438, 439-476, 559-603, 605-637, 639-658) and no rewrite range (336-344, 350-357, 542-545) covers 659-676. The section's content names two libraries: line 669 "use Base UI's `var(--transform-origin)`" and line 674 "Motion `x`/`y` props under load (pre-14.2) … or use `transform: \"translateX()\"`". This is the dangerous case, because the mechanical acceptance condition cannot catch it: lines 660-676 contain **zero** matches for `react|jsx|tailwind|sonner|shadcn`, so a skill that keeps the checklist still scores0 and passes every stated check — while contradicting the document's own goal ("plain CSS, plain DOM, one hand-written stylesheet, **no component library**") and making D2's "the three kept passages that name a framework" four. The same gap exists for one other library name inside a range the document *does* plan to rewrite: upstream line 352 names "Vaul" alongside Sonner, and D2's rewrite rationale names only Sonner.
- Recommendation: Add `## Review Checklist` (660-676) to the drop or rewrite column, or state in D2 that any line not named in the table is dropped. Extend the acceptance condition past the five-word grep — e.g. also fail on `base ui`, `vaul`, `motion` — so it tests the stated goal rather than a narrower proxy.

### The vocabulary baseline is 11, not 7, and the line list is wrong - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:108-110` (D1) and `:322-323` (Alternative A)
- Finding: The validator's own command is `grep -rioE "react|jsx|tailwind|sonner|shadcn" "$d" | wc -l` (`deploy/check-workspace.sh:217`). Run against the upstream directory at `HEAD`, that returns **11** occurrences (4× `jsx`, 1× `react`, 1× `React`, 5× `Sonner`) across **10** distinct lines: 162, 336, 338, 350, 499, 542, **559**, 561, 567, 577. The document says the file "scores **7** under that grep (lines 162, 336, 338, 350, 499, 542, 561, 567, 577)" — the score is wrong and the list omits line 559 while listing nine lines for a claimed seven. No reading of the validator's command yields 7 (case-sensitive line count is 5; case-insensitive line count is 10; occurrence count is 11). The number is stated three times, including as the baseline the "must score zero" condition is contrasted against, and the document calls it "mechanically checkable". The decision it supports (restore-unchanged fails the guard) is correct; the number a reader will reproduce is not.
- Recommendation: State 11 occurrences over 10 lines (add 559 to the list), or restate the baseline as "scores non-zero" and give the command so a reader reproduces it rather than trusting a tally.

### Scoping the scan to `SKILL.md` drops two guidance files the document calls guidance - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:198-202` (D7 bullet 1) and `:353` (Risks table)
- Finding: D7 proposes that "the vocabulary scan covers each skill's **`SKILL.md`**", and the risk row's stated handling is "the scan still covers every skill's **guidance**, which is what the model reads". That is false for two files. `.claude/skills/review-animations/STANDARDS.md` is guidance — the document itself calls it "the house motion bar" (`:122`) and `.claude/agents/gaos-frontend.md:18,28` binds it — and it carries **4** framework-vocabulary hits today (`jsx` at 119 and 171, `Sonner` at 132 and 187), while `review-animations/SKILL.md` carries 0. `.claude/skills/break-ui/CATALOG.md` is likewise guidance (`.claude/agents/gaos-frontend.md:21-22` binds it). After the change, neither file is scanned: the motion bar — the file this document delegates all motion to — becomes unmonitored, and a future framework mention in it passes silently. The current validator prints "review-animations carries 4 framework-vocabulary match(es) (allowed)"; the proposed scan would report 0 and quietly lose that coverage. The rationale D7 gives (provenance files must be able to name what was cut) argues for excluding `SOURCE.md`/`LICENSE`/`NOTICE`, not for excluding guidance.
- Recommendation: Scope the scan to the guidance files (`SKILL.md` plus `CATALOG.md` and `STANDARDS.md`), or to the directory minus provenance files, and amend the risk row so the handling claim matches what the code does.

### The TC-380 pointer lands in the implemented section - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:8` (front matter) and `:251` (change list row 10)
- Finding: The front matter says "**Forward specifications:** TC-380 (`pustaka/docs/product/qa-test-plan.html#go-designgate`)". In the register, `#go-designgate` is the `<h3>` at line **2640**, titled "dashboard — the design gate (TC-359…TC-375)", and it sits inside `<h2 id="implemented">` (line 475). The h3 that actually reaches the newest cases is `#go-designgate-2` at line 2678 ("TC-366…TC-379"), also inside the implemented section; the forward-spec section `<h2 id="not-implemented">` begins at line 2726. The rubric is explicit: `.claude/rules/qa-test-plan.md:57-60` defines a forward specification as a row with Status `Not implemented`, and `:62-64` requires its prose as an `h3` under `<h2 id="not-implemented">`. The change list row 10 says only "the QA row TC-380 and the case count", so the placement of TC-380's scenario prose — which the house rule requires — is never stated anywhere in the document. A reader following the front-matter link puts a "Not implemented" spec inside the implemented section, under a heading whose TC range does not even reach TC-379.
- Recommendation: Point the front matter at `#not-implemented` (or name the new `h3` id), and add to row 10 that TC-380 gets an `h3` under `<h2 id="not-implemented">` with the exact assertion strings the check prints.

### A quotation attributed to `STANDARDS.md` is not in that file - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:54` (The problem, skill table)
- Finding: The table says `review-animations` is "the motion bar — `STANDARDS.md` is "the bar an agent is held to"". The phrase does not exist: a repo-wide search for "bar an agent is held to" across `.md` and `.html` returns only this design document, and `.claude/skills/review-animations/STANDARDS.md` contains no occurrence of "held" at all. The nearest real wording is `.claude/skills/review-animations/SKILL.md:17` — "review animation and motion code against a high craft bar" — and `SKILL.md:3`, "against a high craft bar". The document's own house standard is exact quotation (`.claude/rules/qa-test-plan.md:67-70` demands the exact string, read from the file), so a double-quoted string attributed to a vendored file that the file does not contain is a defect a reader will hit immediately.
- Recommendation: Quote `SKILL.md:17` ("against a high craft bar") and cite it, or drop the quotation marks and keep "the motion bar" as the author's characterisation — the phrase itself is sound, since `gaos-frontend.md:28` uses exactly that wording.

### The canary-probe paragraph names the wrong files, and nothing removes them - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:311-316` (verification plan) and `:240-260` (change list)
- Finding: The document says the canary "is planted in the rule files this change touches (`.claude/rules/frontend.md`, `.claude/rules/rule-index.md`, `.claude/rules/agent-workspace.md`), because those are the standing files the reviewers were about to read". Two problems. First, `.claude/rules/frontend.md` does not exist — the rules directory holds only `agent-workspace.md`, `qa-test-plan.md` and `rule-index.md` — so it cannot be a "standing file" and cannot hold a canary. The canaries actually planted this round are in `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.claude/rules/agent-workspace.md`, `.claude/rules/qa-test-plan.md` and `.claude/rules/rule-index.md` (paths only; values not read). `.claude/rules/qa-test-plan.md` holds one and is missing from the document's list although it is row 15 of its own change list. Second, and more consequential: the change list has no row for removing the standing canaries, and `./deploy/check-workspace.sh` currently exits with **6 failure(s)**, all of them "canary left behind". Since the document lists `./deploy/check-workspace.sh` among "the house checks, unchanged" and states check #4 as `grep -rn "EGM-MARKER-" .` → nothing, a reader following the change list exactly cannot reach a green validator.
- Recommendation: Correct the host list to the files that actually carry canaries (and drop `frontend.md`, or state that it is created by this change and therefore cannot host one), and add a change-list row for removing the six standing canaries, or state explicitly that probe cleanup is a separate step that must complete before the house checks run.

### "EGM" is never expanded - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:16-17` and `:335-338` (Alternative D)
- Finding: "the EGM sweep" and "the EGM design already argued this" are used twice as load-bearing terms — the sweep explains the deletion, the EGM design explains why the motion sections went. "EGM" appears in none of the three rubric files (`.claude/rules/rule-index.md`, `qa-test-plan.md`, `agent-workspace.md`), and the document's Related section links neither `docs/designs/egm-design-review.md` nor `docs/articles/elephant-goldfish-model-in-gaos.md`, where the model is actually named. A naive reader with only the document and the rubric cannot decode the acronym or find what the sweep did.
- Recommendation: Expand on first use — "the Elephant-Goldfish Model (EGM) sweep" — and link `docs/designs/egm-design-review.md` next to the first mention, so "the EGM design" in Alternative D has a target.

### The change-list arithmetic is off by one - Severity: Major
- Affected scope: `docs/designs/frontend-design-rule.md:261` (change list summary)
- Finding: "**Sixteen files** outside `docs/designs/`, plus this document and the review reports." The table has 18 rows; rows 1-15 are the files outside `docs/designs/` (15, not 16), row 16 is this document, rows 17-18 are the review reports. The house contract this document is subject to is explicit — `docs/designs/README.md` rule 4: "The counts follow from the change list. If the target says '12 files', the arithmetic that gets there is written down." The document states a count without the arithmetic and states it wrong, in a document whose subject is counts.
- Recommendation: Write "Fifteen files outside `docs/designs/` (rows 1-15), plus this document (16) and the review reports (17-18)", and show the arithmetic.

### `gaos-frontend.md:5-8` does not contain the ownership statement - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:48-49`
- Finding: The document says the frontend work "is done by the `gaos-frontend` agent (`.claude/agents/gaos-frontend.md:5-8`)". Lines 5-8 are the frontmatter terminator, a blank line and the `# gaos-frontend` heading. The statement the claim rests on is at `:9-13` ("You are the design engineer for the GAOS dashboard. You own `dashboard/templates/*.html`, …"), with the frontmatter `description` at `:3`.
- Recommendation: Cite `.claude/agents/gaos-frontend.md:9-13`.

### "two lines later" is nine lines later - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:187-190` (D6)
- Finding: "and then, two lines later, says the philosophy skill 'was removed on 2026-11-04'". The quotation is at `.claude/agents/gaos-frontend.md:15-16`; the removal sentence is at `:25` — nine lines later.
- Recommendation: Say "nine lines later", or cite `:25` directly.

### The `git ls-files` command shown lists more than the three files - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:19-22`
- Finding: "`git ls-files .claude/skills/` lists `LICENSE`, `SKILL.md` and `SOURCE.md`" — the command as written lists 12 tracked files across four skill directories (`break-ui` ×4, `emil-design-eng` ×3, `rebuild-restart-prod` ×1, `review-animations` ×4). The parenthetical reads as a three-file listing. Separately, the working tree also has `.claude/skills/rebuild-restart-prod/SKILL.md` deleted and uncommitted, which the document's description of the tree state does not mention.
- Recommendation: Use `git ls-files .claude/skills/emil-design-eng/`, and either mention the second uncommitted skill deletion or say why it is out of scope for this change.

### "The house" is used eight times and never defined - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:122, 158, 213, 215, 251, 301, 311, 352`
- Finding: "the house motion bar", "in the house voice", "in the house style", "what the house requires", "(house rule 1)", "The house checks", "the house one", "the house forbids" — the term carries the document's expectations about wording (`:158`, `:213`) and about which checks run (`:301`), and it is never defined or linked. A naive reader cannot tell what style the new rule and the NOTICE entry are supposed to be written in.
- Recommendation: Define it once — e.g. "the house style, this repository's existing convention" — or, if no style document exists, say so plainly so the implementer is not left hunting.

### The `Performance Rules` keep range overlaps a range the document drops - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:121` versus `:129-131` (D2)
- Finding: The keep table says "keep … `## Performance Rules` (477-525)", but three paragraphs later the document says the `motion` library passage (495-509) is dropped. A reader working from the table keeps 495-509.
- Recommendation: Write the keep range as 477-494 and 511-525, or add "except 495-509" to the table row.

### The change-list glob for the lens reports matches no house convention - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:258` and `:361-364` (Status)
- Finding: `docs/designs/reviews/frontend-design-rule-*-review-1.md` matches neither the repo's existing per-lens naming (`egm-design-review-comprehension-2.md`, `-critic-1.md`, `-readiness-1.md`, `-security-1.md`, `-review-1.md`) nor the canonical `docs/designs/reviews/<feature>-review-<n>.md` that `docs/rules/gaos-design-review.md` step 4 specifies for this feature (`frontend-design-rule-review-1.md`). The five lens reports therefore have no stated filenames.
- Recommendation: Name the five files explicitly, or state the naming rule that produces them.

### The positive control is used without a pointer to its definition - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:311-314`
- Finding: The document relies on "the positive-control, one review with the exclusion deliberately disabled, which must produce a report containing a canary", but the front matter cites `docs/rules/gaos-design-review.md:38-53`, which is the trigger list. The positive control is defined at `docs/rules/gaos-design-review.md:67` and in the runbook `.claude/skills/gaos-design-review/SKILL.md:69`.
- Recommendation: Cite `docs/rules/gaos-design-review.md:67` where the positive control is introduced.

### The upstream URL and commit never appear in the document - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:40-41` and `:209-215` (D8)
- Finding: The document says "`SOURCE.md` names the upstream URL, the commit, the licence", but never states the upstream itself, so the provenance and the MIT-licence claim cannot be checked from the design document. The values are at `HEAD`: `https://github.com/emilkowalski/skill`, commit `f1090bec277140d02ed202440185ad9d02ac1490`, MIT.
- Recommendation: State the upstream URL and commit in the body, so D8 and the licence claim are checkable without opening a file the change has not yet restored.

### The frontmatter change leaves "animation decisions" in the description - Severity: Nit
- Affected scope: `docs/designs/frontend-design-rule.md:136-142` (D3)
- Finding: D3 says the frontmatter `description` "gains one clause saying it is the trimmed GAOS edition". The upstream description reads "This skill encodes Emil Kowalski's philosophy on UI polish, component design, **animation decisions**, and the invisible details that make software feel great" — but the `## The Animation Decision Framework` section (62-146) is dropped by D2. The clause the catalogue line promises is the one the trim removes, and the new `## GAOS edition` section will say motion is delegated to `review-animations`, contradicting the description one line above it.
- Recommendation: Say the description also drops "animation decisions" and names the delegation, rather than only gaining a clause.

---

Two claims could not be checked from inside this repository: the absence of the skill from "any
global skill directory on this host" (paths outside the repository are out of bounds for this
review), and the upstream licence attribution beyond the `LICENSE` file already at `HEAD`. The
claim that the skill is absent from this runtime's skill catalogue is corroborated — the only
skill this session can invoke is the browser-control skill.

Counts: B=1 M=7 N=9
