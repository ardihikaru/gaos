# Goldfish review — implementation-readiness lens

**Target:** `docs/designs/egm-design-review.md` (v1)
**Reviewer:** one fresh-context subagent, zero-context (the "implementation
readiness" lens — the zero-decision rule)
**Date:** 2026-11-04
**Method:** Elephant-Goldfish Model — the author drafted the design; a reviewer with
no transcript applied the zero-decision rule: *if the implementer would have to
stop and ask "which one did they mean?", the document is not ready.*

## Findings

### The workspace validator has no specified behaviour - Severity: Blocker
- Affected scope: change list entry 16 (`deploy/check-workspace.sh`); Goals; the
  eight new QA cases; the verification plan
- Finding: The change's stated goal is "a validator is left behind that fails when
  the bloat or the drift comes back", TC-358 requires it to report "a stale path or
  a drifted count", and the verification plan requires it to fail against the
  pre-sweep tree "and name each defect". The document never states which checks it
  performs (which paths, which count strings, what bloat threshold counts as bloat),
  what it prints, or what it exits with. The sibling script it will live beside has
  a fixed house style it never names (`deploy/check-stacks.sh:13-30`: `set -u`, `cd`
  to repo root, `ok`/`FAIL` helpers, a `fails` counter). Nothing states whether the
  new script joins the deploy test gate that `deploy/rebuild-restart.sh` runs today.
  Two competent engineers would produce materially different validators.
- Recommendation: Enumerate the validator's checks as a numbered list (one line per
  check: the exact file and string it asserts), its output format, its exit-code
  contract, and whether it is wired into the deploy gate or run by hand only; then
  state the expected stdout and exit status for the pre-sweep and post-sweep runs.

### The rubric, the reviewer agents and the policy are named, not specified - Severity: Blocker
- Affected scope: §"Layer 1 — the harness"; change-list entries 1-9 and 11
- Finding: The design makes `.claude/rules/*.md` the rubric every reviewer runs
  ("a rule that a reviewer does not read is a rule that only the author can catch"),
  yet each of the three rule files is specified by a single "Holds" phrase: no rule
  text, no reading order, no list of the invariants that must survive the
  restructure of `.claude/CLAUDE.md`. The policy file's "three roles" are never
  enumerated anywhere in the document, nor are the trigger list's per-item
  rationale, the triage vocabulary's exact dispositions per severity, or the output
  contract. The five reviewer agent files have no prompt content and no
  `description` frontmatter, although the two existing agents both carry
  `name`/`description`/`tools` and no agent in this repo currently uses `model:` or
  `omitClaudeMd:`. The implementer would have to invent the rubric that the entire
  gate exists to enforce.
- Recommendation: Write the required content of each rule file (the invariants, in
  reading order), name the three roles and what each may do, give each agent file
  its full prompt and frontmatter (including `description`), and state how a
  reviewer receives the rubric path under `omitClaudeMd: true`.

### No execution phases, and the ordering dependencies are unstated - Severity: Major
- Affected scope: §"The change list"; §"Verification plan"
- Finding: The change list is grouped by file category, not sequenced into phases
  that each leave the tree buildable and testable, and no checkpoint is defined per
  phase. Several dependencies are real but only one is stated: the validator must
  exist before the sweep it is run against. Others are unstated: `.claude/rules/*`
  must exist before `.claude/CLAUDE.md` is shrunk or the always-on invariants
  vanish; the five agents and the skill need the rules to be reviewable;
  `pustaka/docs/architecture/design-gate.html` must exist before its ToC entry or
  `pustaka check` fails on an unregistered file; `assets/toc.js` `product.semver`
  and the changelog entry are "one change" by the site's own policy.
- Recommendation: Add an explicit phase list, each with its own exit command and
  expected result.

### Verification is suite-level only, and the eight new QA cases have no per-case command - Severity: Major
- Affected scope: §"The eight new QA cases"; §"Verification plan"
- Finding: The verification plan lists six commands; no change entry maps to a
  command and expected result. For the eight new cases specifically: TC-357/TC-358
  map to a script whose behaviour is unspecified and have no expected stdout;
  TC-359…TC-364 describe platform behaviour that this change explicitly does not
  build and have no command, no expected result, and no statement of the register
  fields. The register's table requires five fields per row —
  `ID | Automation | Kind | Scenario | Status` — and the page defines `Kind` as
  Positive/Negative and `Status` as Implemented/Not implemented/Retired. The design
  gives `Kind` only for TC-357 and TC-358, and gives no `Status` or `Automation`
  value for any of the eight, so five of eight rows leave the implementer to choose.
- Recommendation: Give each of TC-357…TC-364 a full register row (automation, kind,
  scenario text, status) plus a verification line; for the six unimplemented rows
  state that they are recorded with `Status = Not implemented` and no runnable
  command yet.

### The published ToC mirrors the numbers this change moves, and the change list does not name them - Severity: Major
- Affected scope: §"The arithmetic"; change-list entries 28 and 34
- Finding: The design fixes four count locations, all inside
  `pustaka/docs/product/qa-test-plan.html` and `pustaka/docs/assets/toc/product.js`,
  and the verification grep only looks for `"three hundred and"`. But `product.js`
  carries at least seven more count strings that this same change invalidates:
  "43 business rules", "BR-01 to BR-44", "sixty-seven logged decisions",
  "thirty-one-question register, twenty of them answered", "Thirty decisions …
  nineteen now answered", "All thirty-one questions" and "Sixty-three decisions" —
  all of which must become 48 rules, 68 decisions and 32 questions. The same file's
  "one hundred and eighty-five dashboard … unit tests" is already wrong against the
  real 221/243, and no line of the design names it. Leaving these mirrors stale
  reproduces exactly the drift class this change exists to kill, and the stated
  verification cannot detect it.
- Recommendation: Enumerate every count string that changes, file, line and new
  wording; extend the verification grep to cover those strings.

### M17, T-184…T-188 and the scoreboard row are named without content - Severity: Major
- Affected scope: §"The ruling"; §"Why it stays unbuilt"; change-list entry 30
- Finding: Entry 30 says only "M17, T-184…T-188, scoreboard row". The document never
  states what the five tasks are, which M17.x sub-phase each belongs to, what each
  task's "Relates to" cell should cite, which status symbol each carries, what the
  M17 section headings are, or what the scoreboard row's six values are. The
  scoreboard's columns are `Module | Sub | ✅ | 🟡 | ❌ | ⊘ | Next` with an "All
  modules" total row, and the prose above it says "Seventeen modules,
  seventy-one sub-modules" — all of which move when M17 lands. The plan promises
  that M17's forward specifications "assert concrete values" but names no value for
  the packet kind set or the blocked reason code, so the forward specifications this
  change publishes cannot be written from the document.
- Recommendation: Write out T-184…T-188 with description, sub-phase, related
  identifiers and status; give the M17 scoreboard row's six values and the two
  totals that change; and state the concrete value for every slot.

### OQ-32 defers the mechanism with three candidates and no selection criterion - Severity: Major
- Affected scope: §"OQ-32 — what is still open"; §"The ruling" clause 6; BR-46…BR-48
- Finding: OQ-32 lists three mutually exclusive isolation mechanisms, states "Three
  candidates, none chosen", and says "It blocks M17". Yet this change publishes M17,
  D-068 clause 6 and BR-46…BR-48 as published rules. "Open — narrowed by D-068" is
  not a selection criterion, and D-068 clause 3 only rules out resumed sessions —
  which the document itself proves insufficient. The implementer therefore cannot
  write BR-47's enforcement or M17's isolation task without deciding which candidate
  the platform will use.
- Recommendation: Either select one candidate with its criterion and mark OQ-32
  answered, or state in D-068 and BR-46…BR-48 that zero-context isolation is an
  open precondition that M17 may not claim until OQ-32 is closed, and make the
  isolation task an explicit "decide and implement" task.

### The "pre-sweep tree" checkpoint has no reproducible baseline - Severity: Major
- Affected scope: §"Verification plan"; change-list entries 10, 12 and 22
- Finding: The plan requires the new validator to be run "against the **pre-sweep**
  tree first, where it must fail and name each defect", but no commit, ref, stash
  procedure or fixture pins that tree — and the working tree is already dirty.
  "Each defect" is also undefined because the validator's check list is missing, so
  the pre-sweep run has no stated expected output either.
- Recommendation: Name the baseline explicitly (a commit sha or a git worktree at
  that sha, plus how the uncommitted changes are treated), and list the defects the
  pre-sweep run must report, one per validator check.

### The file boundary is wrong for three of the 41 files, and one path is not exact - Severity: Major
- Affected scope: change-list entries 10, 12, 15 and 32
- Finding: Entry 32, `pustaka/docs/architecture/design-gate.html`, is a new page
  listed under the Modified heading, so the create/modify/delete split is wrong.
  Entry 15 gives the Indonesian article only as "its `-id` twin", so no exact
  repository-relative path exists for it and a write allowlist cannot be built from
  the list. Entry 14, `docs/designs/reviews/`, is a directory whose files are
  excluded while entry 38's directory contributes three files, and no placeholder
  file is named for the directory git cannot track empty.
- Recommendation: Move entry 32 into the created group; write the exact second
  article path; and name a placeholder file for `docs/designs/reviews/`.

### The remaining created files have no content, schema or target specification - Severity: Major
- Affected scope: change-list entries 10, 12, 17, 18, 19-21, 32 and 33
- Finding: `GEMINI.md`, `.agents/skills.json`, the three `.agents/rules/*` symlinks,
  and the new page plus its ToC entry are specified only by their paths. Nothing
  says whether `GEMINI.md` imports the three rule files or mirrors the root
  `CLAUDE.md`; nothing gives the schema of `.agents/skills.json` or which skills
  it registers; the symlink targets are named only as "the three files in
  `.claude/rules/`", with no relative-vs-absolute decision (absolute targets break
  portability); and the new page must satisfy `pustaka check`, which enforces
  registration, a `data-page` id, `page-meta` and asset-prefix depth, plus the ToC
  key order — none of which is given for this page.
- Recommendation: Give each of these files the exact content or the schema and the
  values to fill in.

### The change list's own derived labels do not derive - Severity: Nit
- Affected scope: §"The change list" and the "Modified (13)" heading
- Finding: The 38-entry / 41-file arithmetic is reproducible from the document, but
  the labels around it are not: the heading "### Modified (13)" contradicts entries
  22-36, which are 15 entries; the summary's itemisation omits the new page and
  does not add to 15; and entry 14's directory is counted as one file while entry
  38's directory contributes three.
- Recommendation: Fix the heading to 15, state the create/modify/delete split on one
  basis, and use one convention for directory entries.

### Exact replacement wording is given for the QA counts only - Severity: Nit
- Affected scope: §"The arithmetic"; change-list entries 25, 27, 28, 29, 34
- Finding: The design pins exact wording for the two QA strings but gives no
  replacement wording for the decision-log lede, the two open-questions ledes, the
  BRD §9 lede or the README figures. The pages write counts in words while the
  README writes digits, so the implementer must pick a surface form for each.
- Recommendation: Quote each replacement string, in the same words-or-digits style
  as the string it replaces.

### "No change to the published product" contradicts six published pages in this same change - Severity: Nit
- Affected scope: §Non-goals; change-list entries 27-36
- Finding: The non-goal reads "No change to the published product", while the
  change list modifies six published pages plus three ToC files and adds a seventh
  page. The enumerated clauses make the intent clear (no runtime behaviour change),
  but the first sentence reads as a blanket prohibition.
- Recommendation: Rewrite the first sentence as "No change to the product's runtime
  behaviour; the published docs change."

### The NOTICE attribution text and the article's published destination are unspecified - Severity: Nit
- Affected scope: change-list entries 15 and 26; the Risks row
- Finding: Entry 26 says only "NOTICE — EGM attributed" with no text, and upstream
  EGM is Apache-2.0, so the notice must carry the Apache-2.0 attribution and the
  exact wording is a decision. Deleting `.claude/skills/emil-design-eng/` also
  falsifies `.claude/agents/gaos-frontend.md:15-16` ("Your taste comes from Emil
  Kowalski's design-engineering skills, vendored in this repo"), which the design
  does not mention rewriting. The risk row promises a publication gate, but the
  change list creates only the two source files; the published destination is not
  named.
- Recommendation: Quote the NOTICE paragraph, state the replacement text for
  `gaos-frontend.md:15-24`, and name where the published note lands.

## Per-change verifiability

| Change | Has a stated verification? | Note |
|---|---|---|
| 1 `.claude/rules/rule-index.md` | No | No command; content unspecified |
| 2 `.claude/rules/qa-test-plan.md` | No | No command; which lines move out of `.claude/CLAUDE.md` is not enumerated |
| 3 `.claude/rules/agent-workspace.md` | No | No command; content unspecified |
| 4-8 the five reviewer agents | Indirect | Only the marker probe exercises them; no prompt/format check |
| 9 `.claude/skills/gaos-design-review/SKILL.md` | No | No command; the runbook's steps are unspecified |
| 10 `docs/README.md` | No | No command |
| 11 `docs/rules/gaos-design-review.md` | No | No command; "three roles" never enumerated |
| 12 `docs/designs/README.md` | No | No command |
| 13 `docs/designs/egm-design-review.md` | No | The document itself |
| 14 `docs/designs/reviews/` | No | No placeholder named; no command |
| 15 `docs/articles/…` + twin | No | No command; second path not exact |
| 16 `deploy/check-workspace.sh` | Partial | Script + expected pre-sweep fail / post-sweep pass, but the checks, output and exit contract are unspecified |
| 17 `GEMINI.md` | No | No command; content unspecified |
| 18 `.agents/skills.json` | No | No command; schema unspecified |
| 19-21 `.agents/rules/*.md` symlinks | No | No command; targets not stated as paths |
| 22 `.claude/CLAUDE.md` restructure | Partial | Only "shows only the intended restructure"; the intended content is not stated |
| 23 `gaos-frontend.md` | No | No command; the replacement text is not given |
| 24 `gaos-deploy.md` (F2) | No | No command; exact surviving wording unspecified |
| 25 `README.md` | Partial | The count grep covers the docs site only; README's figures have no check |
| 26 `NOTICE` | No | No command; text unspecified |
| 27 `decision-log.html` | Partial | Structure only; no check on the lede count or the new row |
| 28 `open-questions.html` | Partial | Structure only; no count check |
| 29 `brd.html` | Partial | Structure only; no count check |
| 30 `implementation-plan.html` | No | No command; content and totals unspecified |
| 31 `qa-test-plan.html` | Partial | The count grep checks the QA strings only; no per-row check |
| 32 `design-gate.html` | Partial | `pustaka check` validates registration, but the id/meta to use are unspecified |
| 33 `toc/architecture.js` | Partial | `pustaka check` validates the entry; no expected content |
| 34 `toc/product.js` | Partial | The other mirrored counts are unchecked |
| 35 `changelog.html` | No | No command; the entry's content and bump type unspecified |
| 36 `toc.js` (`product.semver`) | No | No command asserting `0.34.0` |
| 37 delete `rebuild-restart-prod/SKILL.md` | Partial | Only via the validator, whose checks are unspecified |
| 38 delete `emil-design-eng/` | Partial | Only via the validator; no check that the 3 files are gone |

## Not verified

- The three rubric files could not be read — `.claude/rules/` is not present in the
  tree — so the review used the task's stated lens only.
- `.claude/CLAUDE.md` was off limits, so the design's claims about it (11,042
  bytes, 166 lines, the 68-line snapshot, the stale count at line 59, and the four
  items it is to keep) are unverified. It is also already modified in the working
  tree.
- `CLAUDE.md`, `AGENTS.md` and `GEMINI.md` were not opened, per the off-limits list;
  `GEMINI.md` does not currently exist.
- The upstream EGM licence was confirmed as Apache-2.0 from public sources, but
  EGM's "seven skills and nine personas" and its per-persona content were not
  verified against the upstream repository.
- Whether `pustaka check` enforces anything about the new register rows beyond page
  structure.

Counts: B=2 M=8 N=4
