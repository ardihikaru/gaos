# M17.5a — the frontend design rule: consolidated review

**Design:** `docs/designs/frontend-design-rule.md`
**Rounds:** 1 · **Date:** 2026-11-04
**Lens reports:** `frontend-design-rule-comprehension-1.md`, `frontend-design-rule-zoomout-1.md`,
`frontend-design-rule-critic-1.md`, `frontend-design-rule-readiness-1.md`,
`frontend-design-rule-security-review-1.md`

**Totals across the five lenses:** 2 blockers, 43 majors, 24 nits. The comprehension, zoom-out
and readiness lenses ran against revision 1; the critic ran against revision 1 as well; the
security lens ran against revision 2, because revision 1's trim boundary no longer existed by
then. Findings several lenses made independently are counted once here.

## The triage table

| # | Finding | Severity | Disposition | Note |
|---|---|---|---|---|
| 1 | The trim boundary leaves `## Review Checklist` (upstream 660-676) unassigned, and the `Performance Rules` keep cell contradicts the prose | Blocker | **PROPOSE FIX** | revision 2 replaces the boundary with one that assigns **every** upstream line, and the keep/drop/rewrite table is gone: keep 18-36 and 38-60, drop the rest, named |
| 2 | The trim keeps 274 lines the repository already holds in `.claude/agents/gaos-frontend.md:31-83`, and buys them with three hand-rewrites of vendored MIT prose | Major (zoom-out) | **PROPOSE FIX** | revision 2's **Trim A**: the philosophy and the review format only — 70 lines, no rewrite, and the acceptance condition true by construction |
| 3 | The upstream file's framework-vocabulary score is 7 | Major (comprehension, zoom-out, critic) | **PROPOSE FIX** | **11 occurrences on 10 lines** — 162, 336, 338, 350, 499, 542, 559, 561, 567, 577 — with the validator's own command quoted |
| 4 | The vocabulary scan is narrowed so that guidance files (`STANDARDS.md`, `CATALOG.md`) stop being covered | Major (comprehension, zoom-out, critic) | **PROPOSE FIX** | revision 2 scans the whole directory as today; revision 3 excludes **nothing** and rewords the cut record instead (SEC-3) |
| 5 | TC-380 is labelled a forward specification and anchored to the wrong register family | Major (comprehension, zoom-out, critic, readiness) | **PROPOSE FIX** | it is `Implemented` and lives in `#cw-workspace`; the header no longer calls it a forward specification |
| 6 | TC-380 has no negative case, so the case count is wrong | Major (critic, readiness) | **PROPOSE FIX** | **TC-381** is added: the negative case, covering the vocabulary, the provenance and the digest. The count moves **379 → 381** |
| 7 | The quote "the bar an agent is held to" is in no file | Major (comprehension, critic) | **PROPOSE FIX** | replaced with `review-animations/SKILL.md:17` — "against a high craft bar" — and cited |
| 8 | The canary probe names files that do not exist, and nothing removes the canaries | Major (comprehension, critic, readiness) | **PROPOSE FIX** | the runbook's six standing files, verbatim; the removal is a step, and the validator's leftover check confirms it |
| 9 | The isolation check `grep -rn "EGM-MARKER-" .` can never pass — the design document contains the literal | Major (zoom-out, critic, readiness) | **PROPOSE FIX** | the house's **value** pattern, scoped to the standing files and the report store, and the validator cited as the authority |
| 10 | "EGM" is never expanded | Major (comprehension) | **PROPOSE FIX** | "the Elephant-Goldfish Model (EGM) sweep", with `docs/designs/egm-design-review.md` linked at the first mention |
| 11 | The change-list arithmetic is wrong | Major (all four lenses) | **PROPOSE FIX** | the arithmetic is written down: **17 files outside `docs/designs/`**, this document, six review files — **24 in all** |
| 12 | The zero-score acceptance condition cannot detect an over-trim | Major (zoom-out, readiness) | **PROPOSE FIX** | the check now also asserts the kept sections survive — `grep -c '^## '` is 3 — so a file that deletes everything fails |
| 13 | The validator's own strings at `:141-142` are not in the change list, so the house check fails | Major (zoom-out, critic, readiness) | **PROPOSE FIX** | both are named, with their before and after |
| 14 | The new rule reaches one harness — the `.agents/rules/` symlink is missing | Major (zoom-out, critic, readiness) | **PROPOSE FIX** | `.agents/rules/frontend.md` is in the change list, a relative symlink to `../../.claude/rules/frontend.md` |
| 15 | The two designs cannot both be "halves of this change" and each own the version bump | Major (zoom-out) | **PROPOSE FIX** | the ordering is stated: this change lands first (379 → 381, 0.35.0 → 0.35.1), M17.5 follows |
| 16 | NOTICE would list one of three identically-licensed vendored skills | Nit (zoom-out) | **PROPOSE FIX** | one bullet naming all three — same upstream, same commit, same licence |
| 17 | The rule's path scope omits the frontend work the agent owns | Major (zoom-out, critic, readiness) | **PROPOSE FIX** | the scope is the **rendered surface** — templates, static, `render.go`, `view.go`, `seq.go` — and the rule says a `template.HTML` value must be escaped where it is built |
| 18 | The published v0.35.0 record of the removal becomes false | Nit (zoom-out) | **PROPOSE FIX** | the v0.35.1 entry says plainly that the skill comes back trimmed, and names the removal it reverses |
| 19 | Whether the ruling belongs in the product decision log is not decided | Major (critic, readiness) | **PROPOSE FIX** | **D10** decides it: no entry, and the reason — the EGM sweep, the same class of change, added none; a workspace rule is recorded in the workspace. The decision count stays 70 |
| 20 | Alternative D leans on a precedent that says something else, and quotes a phrase no file contains | Major (critic) | **PROPOSE FIX** | Alternative D restated on its own merits, and the EGM contest row (`egm-design-review.md:1408`) quoted for what it says — "if the contest fails, the file is restored. Either outcome is recorded" |
| 21 | The three rewrites have no specified text, and the frontmatter's "animation decisions" advertises a dropped section | Major + Nit (readiness, comprehension, critic) | **PROPOSE FIX** | the trim has no rewrites at all (Trim A); the frontmatter is rewritten, not appended to, and names what the file holds |
| 22 | There are no phases, no exit conditions and no dependency order | Major (readiness) | **PROPOSE FIX** | five phases with an exit condition each, and the order that matters: the skill lands before the rule that names it |
| 23 | Most changes have no verification line | Major (readiness) | **PROPOSE FIX** | one row per changed file, with the command and the expected result — 23 rows |
| 24 | The review reports are named by a glob that matches no house convention | Major (critic, readiness) | **PROPOSE FIX** | the six paths are named in the change list |
| 25 | The edits to `.claude/rules/qa-test-plan.md` are not pinned to exact strings | Major (readiness) | **PROPOSE FIX** | every literal is given, with its line — including the stale "three hundred and seventy-five" at `:73-74`, which is corrected |
| 26 | **SEC-1 — the trimmed file is an auto-loaded instruction file with no integrity control, and the design widens the permission to edit it** | **High (security)** | **PROPOSE FIX** | the file is **pinned by SHA-256** in `SOURCE.md` and the validator verifies it, so an edit fails the check until the digest is updated in the same change with the reason recorded |
| 27 | **SEC-2 — the rule's scope skips the Go files that build the served markup** | Medium (security) | **PROPOSE FIX** | `view.go` and `seq.go` join the scope, and the rule carries the escaping rule (finding 17) |
| 28 | **SEC-3 — the file the design tells the model to read is the file the guard stops scanning** | Medium (security) | **PROPOSE FIX** | nothing is excluded; the cut record names the sections without naming a framework (finding 4) |
| 29 | **SEC-4 — the licence and the provenance are protected by no check** | Medium (security) | **PROPOSE FIX** | the validator asserts, for every vendored skill, a non-empty `LICENSE` and a `SOURCE.md` naming the upstream URL, the licence and the commit — the sisflow precedent |
| 30 | **SEC-5 — the new rule file becomes a seventh standing file no future probe exercises** | Low (security) | **PROPOSE FIX** | the runbook's planting list gains `.claude/rules/frontend.md` |
| 31 | **SEC-6 — three figures do not reproduce** | Low (security) | **PROPOSE FIX** | the `Framer Motion` line is 160, not 433; the directory scan is 11, not 7; the report format is `gaos-frontend.md:113`; the trimmed file is 70 lines |
| 32 | **SEC-7 — two contradictory sentences about editing the same vendored file** | Low (security) | **PROPOSE FIX** | the agent file's sentence names the exception, so the two files carry one authority |
| 33 | The citation drift: `gaos-frontend.md:5-8` → `:9-13`, `:31-77` → `:31-83`, "two lines later" → nine, `agent-workspace.md:1-4` → `:1-5`, `NOTICE:30-44` → `:30-49` | Nit (comprehension, zoom-out, critic) | **PROPOSE FIX** | every range re-derived and corrected |
| 34 | "The house" is used eight times and never defined | Nit (comprehension) | **PROPOSE FIX** | defined once, in the body |
| 35 | `git ls-files .claude/skills/` lists twelve files, not three, and the second uncommitted skill deletion is unmentioned | Nit (comprehension) | **PROPOSE FIX** | the command is the skill's own directory, and `rebuild-restart-prod`'s deletion is named as out of scope |
| 36 | The upstream URL and commit never appear in the document | Nit (comprehension) | **PROPOSE FIX** | both are in the body, and in the `SOURCE.md` text |
| 37 | The restored `SOURCE.md` still says "Do not edit in place" | Nit (comprehension, critic, readiness) | **PROPOSE FIX** | that sentence is replaced by the recorded-trim wording, verbatim |
| 38 | D6 does not say what replaces "Both", and the agent file's state is staleness, not a contradiction | Nit (critic) | **PROPOSE FIX** | the new paragraph is given verbatim, and the current text is called stale |
| 39 | The prose deliverables — the rule-index row, the NOTICE bullet, the changelog entry — have no specified text | Nit (readiness) | **PROPOSE FIX** | all three are given verbatim, with the `bump patch` class |
| 40 | The positive control is used without a pointer to its definition | Nit (comprehension) | **PROPOSE FIX** | `docs/rules/gaos-design-review.md:67` is cited where it is introduced |
| 41 | The author's own finding, found while assembling the trim: the upstream text names a framework the design's rewrite list had not seen | Nit | **PROPOSE FIX** | moot under Trim A — the dropped sections are named, not rewritten |
| 42 | The author's own finding: the sibling design's change-list arithmetic did not follow from its own table | Major | **PROPOSE FIX** | `docs/designs/m17.5-reviewer-workspace.md`: "twelve modified code and test files" → eleven, 27 → 26 files, and the phase arithmetic written down |

## Escalations

| # | Finding | Disposition | Note |
|---|---|---|---|
| E1 | SEC-1's stronger remediation: keep the upstream text **in-tree outside** `.claude/skills/` so the script can diff the kept sections against it, and prove "verbatim" rather than assert it | **ESCALATE** | the pinned digest proves the file has not moved since the trim; it cannot prove the kept lines are the upstream's, because the pinned upstream commit is not in the local object store. That is a separate change, and the design says so in its limits |
| E2 | The back office authenticates nobody — `signedIn` accepts any `gaos_bo=1` cookie — and the front end it serves is what the restored skill's prose shapes | **ESCALATE** | pre-existing, already BR-48's stated blocking precondition, and out of this change's scope. The design makes no claim about it |

## Nothing was contested

No finding was contested. Every one of them was either a fact about the repository that the
document had to carry, or a defect in a document whose subject is facts. The two dispositions
above are escalations, not contests: the finding was right and the fix belongs to another change.
