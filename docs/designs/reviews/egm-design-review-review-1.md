# Goldfish review — zoom-out lens

**Target:** `docs/designs/egm-design-review.md` (v1)
**Reviewer:** one fresh-context subagent, zero-context (the "zoom-out" lens)
**Date:** 2026-11-04
**Method:** Elephant-Goldfish Model — the author drafted the design; a reviewer with
no transcript and no authorial intent attacked it under one lens.

## What I think this does

**What this proposes.** GAOS's agent workspace grows a design gate: a feature-sized
change becomes a document in `docs/designs/`, five zero-context reviewer agents read
that document alone, every finding gets one disposition, and the fixes are applied to
the design rather than to the code. GAOS's own rules become the reviewers' rubric.
Separately, GAOS the platform is specified — but not built — to run the same gate
for the products it dispatches: a design task produces a design artifact, a review
task reviews it in a fresh session, and the implement task waits for the review to
resolve. Alongside both, the workspace is swept: an orphan skill, a 27 KB
React-flavoured skill, an 11 KB always-on file that is 90% register mechanics, and
four stale counts.

**How the existing system works, per this document.** GAOS has a Blueprint, a
readiness rubric, an egress pass and a handoff contract, but no step at which a
proposed design is read by someone who did not write it. Three of its own rules
point the wrong way for a Goldfish: BR-38 allows one live turn per product
workspace, D-056 makes a retry resume its session, and the Blueprint loads
`CLAUDE.md` and the rules on every turn — so a fresh session is not a zero-context
turn. The schema is ahead of the code: a `Review` vertex and a `REVIEWED_BY` edge
exist with no Go type and no producer.

## Findings

### F3's disposition does not fix F3's defect — unscoped rules load at launch - Severity: Blocker
- Affected scope: §"The audit" — F3; §"Layer 1 — the harness" → "The rule layer"
- Simplicity rationale: F3's stated defect is "Every session pays for it." The
  disposition moves the content from `.claude/CLAUDE.md` into
  `.claude/rules/qa-test-plan.md` and calls that the end of the always-on layer. It
  is not. Per Claude Code's own loading rules, a rule file **without `paths`
  frontmatter** "is loaded at launch with the same priority as `.claude/CLAUDE.md`",
  and "Rules load into context every session or when matching files are opened"
  (https://code.claude.com/docs/en/memory.md). The union of `CLAUDE.md` + unscoped
  rules is the always-on layer; moving bytes between the two halves of it does not
  shrink it. The doc's own stated purpose ("where a reader goes when a test or an
  automation is what they are changing") only holds if the rule carries `paths:`
  frontmatter, which the doc never mentions. P1's headline defect survives this
  change untouched, and the plan would report a saving that does not exist.
- Recommendation: Add `paths:` frontmatter to `qa-test-plan.md` (e.g.
  `paths: ["dashboard/**", "pustaka/docs/product/qa-test-plan.html"]`) and to
  `agent-workspace.md`, or drop F3's context-cost claim and restate the move as
  reviewer-accessibility only.

### M17 is scheduled while the decision that shapes it is open - Severity: Blocker
- Affected scope: §"Layer 2 — the platform gate"; OQ-32; change list entries 30-31
- Simplicity rationale: OQ-32 states that the isolation mechanism "blocks M17" and
  lists three candidates, "none chosen". The change nevertheless writes M17, five
  implementation tasks, six QA rows, three business rules and a decision into the
  permanent registers. Those rows can only be written from a guess at a mechanism
  that is explicitly undetermined — and the doc's own §4 audit is a catalogue of
  exactly that failure mode (registers whose contents stopped matching reality). It
  also violates the repo's own contract at `docs/designs/README.md` rule 6: "Nothing
  is left undecided." Roughly a third of the 41-file change therefore buys rows
  that must be re-derived later.
- Recommendation: Land only what is decided — D-068, BR-46…48, OQ-32 — and defer
  M17, T-184…T-188, TC-359…TC-364 and the new architecture page until OQ-32 is
  answered.

### The isolation marker probe cannot fail where the guarantee is claimed, and misses the files that matter - Severity: Blocker
- Affected scope: §"Isolation, and its honest limit"; §"The flow"; §"Verification plan"
- Simplicity rationale: The probe plants `EGM-MARKER-<hex>` in `.claude/CLAUDE.md`
  and requires it to appear in no report. But the five reviewer agents carry
  `omitClaudeMd: true`, which per the Claude Code docs excludes the project
  `CLAUDE.md` from the subagent's initial context — so under the one harness
  Layer 1 actually targets, the marker can never appear and the probe is vacuous by
  construction. Under every other harness the always-on files are the ones this
  same change creates — `GEMINI.md` and `.agents/rules/*.md` — and the probe
  inspects none of them. The probe tests the one file that is guaranteed to be
  excluded and misses the two that are not, while §"Isolation" presents it as "the
  only evidence" for exactly those harnesses. It also mutates a tracked file
  mid-run; an interrupted run commits the marker.
- Recommendation: Plant one marker per always-on file per harness and grep all of
  them; or drop the probe under Claude Code (where `omitClaudeMd` *is* the tooling)
  and keep it only for the harnesses where it is the sole evidence.

### "Five" is EGM's own stage count, not a GAOS reduction, and Alternative C is a strawman - Severity: Major
- Affected scope: §"The five lenses"; §"Alternatives considered" → Alternative C
- Simplicity rationale: EGM's `egm-review` skill already runs a **five-stage**
  pipeline — Initial Zoom Out → Comprehension → Design Critique → Intermediate Zoom
  Out → Implementation Readiness. "Five" is EGM's stage count, not a GAOS-derived
  number; the doc presents EGM's own figure as if it were a GAOS judgement.
  Alternative C's rejection then rests on "One lens finds one class of defect"
  (unevidenced) plus an anecdote about a different repository with a different
  stack. The obvious alternative — one reviewer running the same five questions as
  sections of the rubric this change already writes — is never considered, and the
  cost of five Opus contexts per design change is never stated.
- Recommendation: State the real reason for N (parallel zero-context isolation, or
  triage parallelism) and measure before fixing it: start with two lenses
  (zoom-out + critic), and record what one review costs.

### Alternative A never considers the option that actually exists - Severity: Major
- Affected scope: §"Alternatives considered" → Alternative A; change list entries 4-9
- Simplicity rationale: EGM's design is explicitly built for a caller-supplied
  standard: the review skill passes "persona/standard paths" to each subagent. GAOS's
  rejection reasons are "its rubric is EGM's document standards" (an argument against
  *the default*, not against the plugin) and "it assumes the reviewer's context is
  empty" (true, and handled by `omitClaudeMd`). The option the document never
  evaluates is: install the plugin, keep its personas and dispatch loop, and supply
  GAOS's `.claude/rules/` as the standard path. Meanwhile GAOS re-authors five
  personas, a dispatch skill, a triage vocabulary, a report format and a drift
  checker, while EGM already ships `egm-review-loop` (concurrent/hierarchical
  dispatch), `egm-resolve` (triage + patch application) and `egm-drift-report`.
- Recommendation: Add "install EGM, supply GAOS's rubric as the standard path" to the
  alternatives and either adopt it or state the concrete blocker.

### The gate's own triage vocabulary cannot be stored in the record it claims to reuse - Severity: Major
- Affected scope: §"The flow"; §"Layer 2" → "What is already there"; D-068 clause 2
- Simplicity rationale: The doc asserts "the review record itself needs no DDL
  change" because the `Review` vertex already exists. But `Review.verdict` is a
  closed regexp — `ACCEPT|ACCEPT_WITH_CLEANUP|ITERATE|ESCALATE|STOP|SAFETY_VETO` —
  with one `reasonCode` and one `finalScore` for the whole review. The design's
  dispositions are `PROPOSE FIX · CONTEST · ESCALATE · DECLINE`: only ESCALATE
  exists in the closed set, DECLINE has no counterpart, and the security lens's
  separate Critical/High/Medium/Low scale has nowhere to live.
- Recommendation: Use the existing `verdict` vocabulary as the disposition set, or
  state explicitly where the four dispositions and the second severity scale are
  stored and why "no DDL change" still holds.

### The document contradicts itself on whether a reviewer may read the repository - Severity: Major
- Affected scope: §"The five lenses"; §"The flow"; §"Risks" table
- Simplicity rationale: The reviewer agents get `tools: Read, Grep, Glob`, and the
  repo's own design contract demands "Every claim carries a file and a line" — a
  claim only checkable against the repository. But the Risks table says "A reviewer
  cannot check the repository… The author's own claims about the repository are
  checked by hand, not delegated." Both cannot hold. Which branch is true decides
  whether the gate can catch a false file:line claim, which is the entire value of
  the layer.
- Recommendation: Decide. If reviewers may read the repo (they have the tools), say
  so and drop the "cannot check the repository" risk row.

### The rubric is placed in the namespace the Blueprint owns - Severity: Major
- Affected scope: §"The five lenses"; §"The rule layer"; D-068 clause 3; OQ-32
- Simplicity rationale: "The rubric is **every file in `.claude/rules/`**." Inside a
  product workspace that directory belongs to the Blueprint: `dashboard/blueprint.go`
  writes `.claude/rules/00-gaos-contract.md`, `10-brd.md`, `20-stories.md`,
  `30-decisions.md` there. So in the platform case "every file in `.claude/rules/`"
  resolves to the product's own problem statement, domain rules and decision list —
  precisely the standing files P3 names as the reason a fresh session is not a
  zero-context turn. The document diagnoses the leak and then puts the rubric
  inside it.
- Recommendation: Name the rubric files explicitly (a distinct directory, or a
  prefix the Blueprint never writes) and state how a reviewer workspace holds the
  rubric without the Blueprint's files.

### The design-gate policy is written in five places, each of which can drift - Severity: Major
- Affected scope: §"The policy"; §"The rules"; change list entries 11, 12, 13, 32
- Simplicity rationale: One policy lands in `docs/rules/gaos-design-review.md`, in
  BRD §9.7 as BR-46…48, in a new `pustaka/docs/architecture/design-gate.html`, in
  `docs/designs/README.md`, and in the skill's trigger list — across two
  directories both called "rules". That is five statements to re-derive when the
  policy changes, which is the exact drift class the document's own F5/F6/F7 audit
  is about. The change is generating more of the disease it is treating.
- Recommendation: Name one source of truth and make the other four pointers that
  say where the authoritative copy lives.

### The new workspace validator duplicates the validator the repo already has - Severity: Major
- Affected scope: change list entry 16; TC-357/TC-358; §"Verification plan"
- Simplicity rationale: GAOS already ships a docs consistency checker:
  `pustaka check` verifies that every page on disk is registered in the ToC, that
  every registered file exists, that every internal link resolves, and that the ToC
  registry is free of duplicates — with a Go test suite behind it. TC-358's "a
  stale path … is reported" is already covered there. The genuinely new capability
  (prose counts) belongs in the checker that already parses the ToC parts where
  those counts live.
- Recommendation: Extend `pustaka check` with the count assertions, or scope
  `check-workspace.sh` strictly to the `.claude/` tree and leave the docs registers
  to `pustaka check`.

### The audit's headline numbers mix measurement bases and one contrast is not reproducible - Severity: Major
- Affected scope: §"The problem" P1; F4; Alternative E
- Simplicity rationale: ".claude/ is 164 KB. Four skills are 124 KB of it" — those
  are `du -sk` block numbers (164 KB and 124 KB exactly, confirmed). The real
  content payload is 95,796 bytes for `.claude/` and 74,620 bytes for the skills:
  the bloat headline is inflated ~1.7x by 4 KB block padding, while the same
  paragraph quotes exact byte counts for individual files (27,509 B). Separately,
  "20 React-family mentions" and "two React-family matches" cannot both be produced
  by one metric.
- Recommendation: Recompute every figure with one tool and one basis (`wc -c`),
  state the command in the document, and stop quoting `du` blocks beside byte counts.

### The M14.0 precedent is read as "docs instead of code" when the register says "docs first" - Severity: Major
- Affected scope: §"Overview"; §"Alternatives considered" → Alternative F
- Simplicity rationale: The quoted sentence is accurate, but its subject is M14.0,
  the **first change of the module** — and M14.1 through M14.5 (T-144…T-161) are
  code: upstream provisioning, the ai-manager client and adapter, submitting and
  polling a turn, storing the prompt and the reply. The precedent establishes
  *sequencing* — docs first, then code — not a doctrine of shipping no code.
- Recommendation: Cite M14.0 for what it shows (a module may open with written
  targets) and rely on BR-05 for the decision to defer the code.

### The audit missed a stale number in a file the change already edits - Severity: Major
- Affected scope: §"The audit" → F6; change list entry 25
- Simplicity rationale: F6 finds two stale counts in `README.md`, and entry 25
  rewrites that file. The same file's line 8 says "**Product version 0.29.0**",
  pointing at the very file that says `semver: "0.33.1"`. The document's stated
  method — "a summary that stopped being true because nobody re-derived it" —
  applies here too.
- Recommendation: Add the README version line to F6 and to the count list that must
  move together.

### The vendor-alias wiring re-creates an existing convention incompletely - Severity: Major
- Affected scope: change list entries 17-21; §"Isolation, and its honest limit"
- Simplicity rationale: GAOS already solved "one digest across harnesses". T-178
  and D-067 both specify that the agy worker links **`AGENTS.md`, `GEMINI.md` and
  `.agents/rules`** to the same `CLAUDE.md` and `.claude/rules` the Blueprint
  writes. The change hand-creates `GEMINI.md` and `.agents/rules/*.md` but not
  `AGENTS.md`. It also introduces `.agents/skills.json`, a file that exists nowhere
  in the repository and whose consumer the document never names.
- Recommendation: Reuse the T-178 alias set and cite T-178/D-067 as the precedent;
  name what reads `.agents/skills.json` or drop entry 18.

### The change list's own counts contradict each other - Severity: Nit
- Affected scope: §"The change list", headings against the summary table
- Simplicity rationale: The table says 9 / 7 / 6 / 15 / 4 = 41 files; the section
  headings say "Created — working docs (6)" and "Modified (13)" where the
  enumerated entries are 6 and 15 respectively. The headings are entry counts, the
  table is file counts, and `docs/designs/reviews/` — a directory — is counted as a
  file while the same paragraph says its contents "are therefore not counted here".
- Recommendation: Make the headings count files, drop the directory from the file
  total, and reconcile against the actual `git status`.

### The vocabulary test is cited at a comment, not at the test - Severity: Nit
- Affected scope: D-068 clause 4; Alternative B
- Simplicity rationale: `dashboard/taskstate.go:21-25` is a comment block explaining
  the rule; the test itself is `TestTheTaskStatusVocabularyMatchesTheDDL`. The
  document twice presents the former as "its vocabulary test".
- Recommendation: Cite the test file for the test and the comment for the rationale.

### "Eight forward specifications" is never enumerated - Severity: Nit
- Affected scope: §"Overview"; §"The arithmetic"; D-068
- Simplicity rationale: The Overview promises "one milestone and eight forward
  specifications". The only list of eight is the QA cases TC-357…TC-364, two of
  which belong to Layer 1's validator, not to M17; D-068 has seven clauses. A
  reader cannot tell what the eight are.
- Recommendation: List them explicitly, or say "eight QA rows, of which six are
  M17's".

### "No caller" is not "no user" for a slash-command skill - Severity: Nit
- Affected scope: F1; §"Risks" table
- Simplicity rationale: The skill carries `trigger: /rebuild-restart-prod` and is a
  user-invocable slash command that delegates to `gaos-deploy`; the evidence
  establishes that no *automated* caller exists, not that no human uses it.
- Recommendation: Say what happens to the `/rebuild-restart-prod` surface, and flag
  F1 in the Risks table alongside F4.

### The flow's "nothing else" is not what the reviewer receives - Severity: Nit
- Affected scope: §"The flow"
- Simplicity rationale: "each given the document path and nothing else" — but each
  reviewer also needs the rubric pointer, the triage vocabulary, the output
  contract, the report path and the isolation instruction. The flow understates the
  prompt, which is the thing the harness is judged on.
- Recommendation: Enumerate what the reviewer is actually given.

### `omitClaudeMd` carries a version floor and an exception - Severity: Nit
- Affected scope: §"The five lenses"; §"Isolation, and its honest limit"
- Simplicity rationale: Per the Claude Code docs the field "Requires Claude Code
  v2.1.271 or later" and is "Ignored when the agent runs as the main session agent
  via `--agent` or the `agent` setting". Neither constraint appears in the document
  that makes "excluded by the runtime" a load-bearing claim.
- Recommendation: Record the minimum Claude Code version in the five agent files and
  note the `--agent` exception next to the isolation claim.

## Reuse opportunities

- `pustaka check` — already validates page registration, file existence, internal
  links and ToC registry consistency.
- `deploy/check-stacks.sh` — the `ok`/`FAIL` helper convention a new repo validator
  should mirror.
- `dashboard/schema/06-judgment.sql` — the existing `Review` vertex, whose five
  dimensions map one-to-one onto the five goldfish lenses.
- `dashboard/schema/08-edges.sql` — the existing `REVIEWED_BY` edge.
- `pustaka/docs/delivery/implementation-plan.html` (T-178) and
  `pustaka/docs/product/decision-log.html` (D-067) — the existing vendor-alias
  convention.
- `dashboard/blueprint.go` — the Blueprint's own `.claude/rules/` writer and its
  `NN-` prefix scheme; the rubric must avoid that namespace.
- EGM's plugin (Apache-2.0) — `egm-review`, `egm-review-loop`, `egm-resolve`,
  `egm-drift-report`, and the caller-supplied "standard paths" affordance.
- `docs/designs/README.md` — the design-document contract already in the tree.

## Not verified

- The three rubric files do not exist yet — this change creates them — so no
  compliance check against a rubric was possible.
- `.claude/CLAUDE.md` is off-limits, so F3's line range, F5's quoted string and the
  "roughly 140 of its 166 lines" figure were checked against the document's own
  description rather than the file.
- The metric behind "20 React-family mentions" / "two React-family matches" is
  unstated; no single metric reproduces both.
- EGM's internals were read from the public repository, not a local checkout.
- What consumes `.agents/skills.json` — no such file or reference exists anywhere.

Counts: B=3 M=11 N=6
