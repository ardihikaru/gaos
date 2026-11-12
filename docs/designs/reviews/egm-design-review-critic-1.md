# Goldfish review — critic lens

**Target:** `docs/designs/egm-design-review.md` (v1)
**Reviewer:** one fresh-context subagent, zero-context (the "critic" lens:
architecture, structure, style, failure modes)
**Date:** 2026-11-04
**Method:** Elephant-Goldfish Model — the author drafted the design; a reviewer with
no transcript verified its architectural claims against the repository and
attacked the ones it could not verify.

## What I think this does

The document specifies a design gate for GAOS: a feature-sized change is written as
a standalone document under `docs/designs/`, five zero-context reviewer agents read
that document alone, each finding gets one disposition, and the fixes are applied to
the *design* rather than to the code. It also specifies — but does not build — the
same gate inside the product as D-068 plus three business rules, one open question,
one milestone and eight forward specifications, with no Go code in this change.
Alongside both layers it sweeps GAOS's agent workspace: three rule files move out of
the always-on layer, five reviewer agents and one skill are created, two skills are
deleted, a workspace validator is added, and cross-harness wiring is created. It
closes with a change list, a count table, six rejected alternatives, a risk table and
a verification plan.

## Findings

### The isolation probe cannot fail, and its marker is left planted in the always-on file - Severity: Blocker
- Affected scope: §"Isolation, and its honest limit"; §"The flow";
  §"Verification plan"; §Risks row 1
- Finding and rationale: The method's central claim is that reviewers are isolated
  from the author's context. The only runtime mechanism asserted is
  `omitClaudeMd: true`, a frontmatter key with **zero occurrences anywhere in this
  repository** — no existing agent uses it (`.claude/agents/gaos-deploy.md:1-5`,
  `gaos-frontend.md:1-6` use only `name`/`description`/`tools`) and no doc in
  `pustaka/` mentions it. The only *evidence* offered is the marker probe, and the
  probe is structurally incapable of failing: under Claude Code with
  `omitClaudeMd: true` the marker lives in a file the reviewer never loads, so
  "must be empty" is guaranteed green whether the mechanism works or the key is
  inert. In the other harnesses the probe is green whenever the reviewer never
  happens to read the repo. **No negative control is specified** — there is no step
  that removes the exclusion once to confirm the probe fires. Worse, the probe is
  presently **live**: `grep -c "EGM-MARKER" .claude/CLAUDE.md` returns `1`, and the
  always-on file is dirty on the tree this change will land on. The probe also
  cannot distinguish a context leak from a reviewer legitimately grepping the
  repository and quoting it — which the rubric actively encourages — so a green
  probe and a red probe both admit more than one reading.
- Recommendation: (a) Add a negative control: run one review with the exclusion
  deliberately disabled and assert the marker *does* appear in the report; without
  that step the probe is decoration. (b) Extend the probe to the harnesses that will
  actually read the project memory (`GEMINI.md` at minimum). (c) Remove the planted
  marker and re-derive F3/F5 against the current file. (d) In the Risks table,
  replace "Under Claude Code it *is* tooling" with a statement conditioned on a
  passing negative control.

### The gate's persistent state has no representation, so M17 is not implementable from this document - Severity: Blocker
- Affected scope: §"The ruling" clauses 1 and 4; §"What is already there";
  §"Why it stays unbuilt"; §"The arithmetic" TC-359…TC-364
- Finding and rationale: Clause 1 makes dispatch conditional on "the review carries
  an open Blocker" — a durable, machine-checkable fact. The only schema object for
  a review is the `Review` vertex (`dashboard/schema/06-judgment.sql:14-26`), whose
  `verdict` is a six-value regexp
  `ACCEPT|ACCEPT_WITH_CLEANUP|ITERATE|ESCALATE|STOP|SAFETY_VETO` with **no
  "blocked" value**, whose `findings` is an untyped `LIST` with no specified
  element shape, and whose `finalScore` is `mandatory true, notnull true` — with
  **no scoring rubric anywhere in the design** to produce it. The document therefore
  claims that "the review record itself needs no DDL change" while the record cannot
  express the state the ruling depends on. Compounding it: `Review` is already
  claimed by the existing review-engine roadmap (M5.1 score, M5.2 safety veto, M5.3
  score bands, M5.4 failure signature), whose semantics are score-band-driven and
  directly collide with the new severity/disposition vocabulary. The change list
  does not touch the M5 entries. TC-360 and TC-362 cannot be written against an
  undefined state.
- Recommendation: Specify the review record's encoding before adopting D-068: the
  finding shape (severity, disposition, open/cleared), which `verdict` value
  represents "blocked with an open Blocker", what populates `finalScore` and the
  five dimension scores for a design review, and how this differs from the M5
  score-band `Review`.

### Clause 5 disarms the only self-repair mechanism on the task whose failure blocks all downstream work - Severity: Major
- Affected scope: §"The ruling" clause 5; BR-48; §Risks
- Finding and rationale: Clause 5 exempts the review turn from retry, escalation
  and failure signature. That is the right instinct for a *verdict*, but the
  exemption is written for the review turn as a whole, and a review turn that
  **fails** (agent crash, gateway 410, malformed packet) is not a verdict. The
  task's own counters live on `Task.retryCount`/`Task.escalationCount` and
  `TaskRun.failureSignature`, so a review that fails is recorded as FAILED with no
  retry, no escalation and no signature to detect repetition. Clause 1 then holds
  the implement task closed because the artifact is "unreviewed", and BR-48's
  operator-queue path is written only for "an open Blocker" — not for a failed
  review. There is no stated transition out.
- Recommendation: State the review-failure path explicitly in D-068: a failed review
  must either be exempt only from *retry of a verdict*, not from retry of an
  *error*, or escalate directly to the operator's queue with a named reason code.
  Add the case to the Risks table and to the forward specs.

### BR-38's real consequence is a per-product turn ceiling, and Layer 2's reviewer count is never specified - Severity: Major
- Affected scope: §P3 BR-38 bullet; §Goals bullet 3; §"The flow"; §"The ruling"
- Finding and rationale: BR-38 is quoted correctly and its second sentence confirms
  product-scoped serialisation. But the inference the document draws is the wrong
  one: the gate is serial because clause 1 forbids dispatching the implement task
  until the review resolves — not because of BR-38. BR-38's actual consequence is a
  **throughput ceiling the document never derives**: in a product workspace there
  is exactly one live turn, so every review turn occupies the product's only slot.
  Combined with an unspecified reviewer count this becomes a multiplier. The
  ruling says only "a review task reviews it in a fresh session" (singular) — the
  five-lens structure is never carried into the platform gate.
- Recommendation: State the reviewer count for Layer 2 explicitly. If it is one, say
  so and say why the platform gate gets less signal than the harness. If it is
  five, derive the per-product cost and put it in the Risks table.

### D-056 is inert for the gate, and nothing carries the review findings back to the implementer - Severity: Major
- Affected scope: §P3 D-056 bullet; §"The ruling" clause 3; BR-47
- Finding and rationale: D-056 is quoted correctly, but it is presented as one of
  "three of GAOS's own rules [that] point the wrong way for a Goldfish". It is not
  an obstacle: D-056 governs session continuity *within one task across retries*,
  clause 2 gives the review task its own fresh session, and clause 5 stops review
  turns from being retried — so D-056 never applies to a review turn at all. The
  real damage is in the opposite direction and goes unmentioned: D-056 promises the
  implementer it will see "the review that rejected it" on retry. Clause 3
  deliberately withholds the implementer's brief from the reviewer, but nothing
  specifies the reverse path — no clause, rule or QA row says the review's findings
  are attached to the implement task's next packet. After a Blocker is cleared, the
  implement task's retry resumes a session that does not contain the review, so the
  implementer repeats its mistake blind.
- Recommendation: Add an explicit forward path — the review's dispositioned
  findings are delivered to the implement task's next packet, distinct from the
  reviewer's own packet — and add a QA row for it. Restate the D-056 bullet as "D-056
  governs retries within a task, not the gate; what it costs us is the implementer's
  lost continuity, which clause 3 must repay."

### OQ-32's first candidate is unimplementable under the document's own non-goals - Severity: Major
- Affected scope: §"OQ-32 — what is still open"; §Non-goals; §"The ruling" clauses 3
- Finding and rationale: OQ-32 offers three candidates for making a platform
  reviewer genuinely zero-context, and the first is "a reviewer workspace that holds
  the rubric but not the product Blueprint". The rubric *is* the product Blueprint:
  `dashboard/blueprint.go:183-185` renders `renderBRD(m.BRD)` into
  `.claude/rules/10-brd.md`, and `renderBRD` explicitly emits "Domain rules — your
  work is scored against these". Separating rubric from Blueprint therefore
  requires a Blueprint rendering change — which §Non-goals forbids. The second
  candidate is the same change by another name. The third is the only one that does
  not require it, and it collides with BR-42's allowlist. Candidate 1 also runs into
  BR-39: a workspace whose synced files do not match the pinned Blueprint version
  refuses dispatch, so a reviewer workspace without the product Blueprint is by
  definition a drifting workspace. The document presents all three as candidates
  without marking which are admissible.
- Recommendation: Mark candidates 1 and 2 as requiring a Blueprint rendering change
  (and therefore a Non-goals amendment or a new decision), and record that candidate
  1 additionally conflicts with BR-39's drift gate. Reduce OQ-32 to the one
  candidate compatible with the current non-goals, or state what the non-goal must
  become.

### The existing `Review` semantics and the new gate semantics are never reconciled - Severity: Major
- Affected scope: §"What is already there"; §"Why it stays unbuilt"; change list entry 30
- Finding and rationale: The document leans on the schema being "ahead of the code"
  — true for the `Review` vertex and `REVIEWED_BY` edge (no Go type, no producer:
  `ReviewCompleted` is emitted only at `dashboard/sim_test.go:225`). But "ahead of
  the code" is not "ready for this feature". The `Review` vertex was designed for
  the M5 review engine, whose contract is a five-dimension score with bands and
  whose trigger is a *failed task*. The new gate's contract is a design document
  reviewed *before* any task runs, with findings, severities and dispositions. The
  two produce the same vertex with incompatible meanings, and the change list does
  not touch the M5 entries.
- Recommendation: Add an explicit paragraph stating how the gate's review record
  differs from the M5 score-band `Review`, whether a second instance type is needed
  (a `kind`/purpose discriminator), and what M17 must do to keep both.

### Clause 7's remedy leaves the banned phrase in a business rule, a diagram and a prose paragraph - Severity: Major
- Affected scope: §"The ruling" clause 7; change list entry 29
- Finding and rationale: The ruling is right that the BRD already means something
  else by "design review" — §8.5's 0-29 band and §7.5 — and the fix shape is
  right. But the phrase occurs in **six** places and the change list amends one:
  `brd.html:423` (§7.5 "Unproductive retry"), `:428` (§7.5 "Total collapse"), `:537`
  (the §7.6 CLI flow diagram node), `:891` (§8.5's band — the one amended), `:911`
  (§8.5 prose), and `:1250` — **BR-16, a business rule**: "An identical failure
  signature twice routes to a design review, not to a tier escalation." BR-16 is
  the authoritative place; leaving the phrase there while banning it in a table cell
  inverts the precedence the ruling is trying to protect.
- Recommendation: Amend all six occurrences in one pass and widen change-list entry
  29 from "§8.5 wording" to "the six 'design review' occurrences in §7.5, §7.6,
  §8.5 and BR-16".

### The change list's own counts do not derive from its own entries - Severity: Major
- Affected scope: §"The change list", the summary block
- Finding and rationale: The document promises "Every entry names its file, and the
  counts are derived rather than asserted" — and its own repository rule says "The
  counts follow from the change list." They do not. The heading "### Modified (13)"
  sits above entries 22-36, which are **15** entries. The heading "### Created —
  working docs (6)" counts entries 10-15 (six), while the summary block counts the
  same bucket as **7** files. Entry 32, `pustaka/docs/architecture/design-gate.html`,
  is filed under "Modified" but the file does not exist. Entry 14,
  `docs/designs/reviews/`, is counted as a file but is an empty directory, and git
  cannot track an empty directory. This is precisely the drift class the document
  says it exists to eliminate, reproduced in its own change list.
- Recommendation: Recompute the buckets on one basis and state it.

### The audit's headline React-mention count is not reproducible - Severity: Major
- Affected scope: §F4; §"Alternatives considered" Alternative E
- Finding and rationale: F4 states `emil-design-eng` "carries 20 mentions of
  React-family technology", and Alternative E repeats "20 React-family matches in
  27,509 bytes" against review-animations' "two React-family matches across
  18,491 bytes". The comparison uses one vocabulary for one file and a different
  one for the other. The honest figures are 7-vs-2 (narrow) or 19-vs-7 (broad); the
  doc states 20-vs-2, a 10× density gap that neither vocabulary supports. The
  deletion decision still stands on the independent ground ("GAOS's dashboard has
  no React and no npm"), but the audit states a number that cannot be reproduced.
- Recommendation: State the counting vocabulary in the document and recompute both
  figures from it.

### The gate is never priced and never measured - Severity: Major
- Affected scope: §Goals; §Risks; §"The policy"
- Finding and rationale: The document measures everything else to the byte but
  never states what the gate costs or whether it works. The only economics is one
  clause, and "roughly a third of what comes back needs triage rather than action"
  is asserted with no source and no measurement behind it. There is no per-review
  token or dollar estimate, no latency estimate, and no ceiling. The harness's cost
  has **no cap at all**: it runs five opus reviewers entirely outside the platform's
  metering. Nor is there any success metric.
- Recommendation: Add a cost line to §Goals and §Risks, and a success metric with a
  target and a statement of how it will be measured from the append-only reports.

### The Risks table denies the method its premise - Severity: Major
- Affected scope: §Risks row 2; §Overview; §"The flow"; §"The five lenses"
- Finding and rationale: The Risks table says "A reviewer cannot check the
  repository" and resolves it with "A finding about the repo's facts is the
  author's to verify." But the reviewers are granted `tools: Read, Grep, Glob`
  over the whole repository, and the rubric is made of repository files. A reviewer
  *can* and will check the repository — that is the entire value of a zero-context
  reviewer: independently confirming the author's `file:line` claims rather than
  taking them on trust. The Overview sells the opposite, and the flow says each
  reviewer is "given the document path and nothing else" while the same design
  hands it Grep, Glob and a rubric that is itself a set of repository rules.
- Recommendation: Rewrite the Risks row to the truth — a reviewer *can* check the
  repository, and the rubric requires it to; the limit is that it has no authorial
  intent, only artifacts.

### BR-17's rationale is wrong: two of its six limits are not per attempt - Severity: Major
- Affected scope: §"The ruling" clause 5
- Finding and rationale: Clause 5 justifies the exemption with "BR-17's guards are
  per attempt". BR-17 lists six limits: "3 retries, 2 escalations, 2 identical
  signatures, capped exponential backoff, per-workflow and per-tenant budget caps,
  and human intervention at 2 escalations or any safety veto". The budget caps are
  per-workflow and per-tenant, not per attempt. Clause 6 preserves the intent, so
  the conclusion survives — but the stated reason does not, and a reader checking
  BR-17 will find the rationale false.
- Recommendation: Restate clause 5 as covering the three attempt-scoped limits only,
  mirroring BR-34, with the budget caps still applying and the human-intervention
  threshold not exempted.

### M17's forward specifications are asserted, not written, and two of the eight QA rows are not about the gate - Severity: Major
- Affected scope: §"Why it stays unbuilt"; §Overview; §"The arithmetic"; change list entry 30
- Finding and rationale: The precedent is sound and verified. But M17's forward
  specifications are claimed to "assert concrete values" and are **not in this
  document**. Only two of the five values are stated anywhere. `Task.kind`'s value,
  the packet kind set and the blocked reason code are named as "concrete" and left
  unspecified. The eight registered QA rows are two shell-validator rows and six
  gate rows, not "eight forward specifications" for the platform half as the
  Overview claims.
- Recommendation: Put the five concrete values in this document, state the review
  packet's structure, and list T-184…T-188 with titles.

## Claim verification

| Claim in the design | Verdict | Evidence |
|---|---|---|
| BR-38: "At most one turn is live in a product's workspace at any moment" | confirmed | `brd.html:1293` |
| BR-38 forces the gate to be serial per product | refuted (the gate is serial by clause 1; BR-38's real effect is a per-product turn ceiling, and Layer 2's reviewer count is unspecified) | `brd.html:1293`; design `:367-368`, `:370` |
| D-056: a retry resumes its session "so the model sees its own failed reply and the review that rejected it" | confirmed | `decision-log.html:960-963` |
| D-056 is incompatible with a zero-context reviewer | refuted — D-056 governs retries *within* one task; its real cost is the implementer's lost visibility of the review on retry | `decision-log.html:960-963`; design `:370`, `:383-385` |
| Blueprint loads `CLAUDE.md` and `.claude/rules/*.md` on every turn, including a resumed one | confirmed | `blueprint.html:95-96`; `blueprint.go:181-189` |
| A fresh session is therefore not a zero-context turn | confirmed | `blueprint.html:95-96`; `blueprint_render.go:68` |
| `Review` vertex exists with verdict, finalScore, five dimensions, weights, findings, reasonCode, reviewedAt | confirmed | `06-judgment.sql:14-26` |
| `REVIEWED_BY` edge exists | confirmed | `08-edges.sql:71` |
| No Go type and no producer for the `Review` vertex | confirmed | `ReviewCompleted` emitted only at `sim_test.go:225` |
| `Task.status` is constrained by a DDL regexp | confirmed | `04-execution.sql:27-28` (contains `BLOCKED`, not `NEEDS_DESIGN`) |
| `Task.status` is constrained by a vocabulary test | confirmed (the cited line range is a doc comment, not the test) | `taskstate.go:21-25` describes the test |
| The `DEPENDS_ON`-edge + `BLOCKED` alternative avoids touching both | confirmed | `08-edges.sql:41`; `04-execution.sql:27-28`; `taskstate.go:37` |
| A review turn consumes no retry, escalation or failure signature | partially confirmed — BR-34 grants exactly those three, but BR-17's six limits include per-workflow and per-tenant budget caps | `brd.html:1282`, `:1251` |
| The review record needs no DDL change | refuted at the semantic level — `verdict` has no blocked value, `findings` is untyped, `finalScore` is mandatory with no scoring rule | `06-judgment.sql:15-26`; `04-execution.sql:25-41`; `11-blueprint.sql:83` |
| `design` must be added to the `Artifact.kind` regexp | confirmed | `02-master.sql:56-57`; `11-blueprint.sql:83` |
| `blueprint.go:189` calls `renderDecisions(m.Decisions)` | confirmed | `blueprint.go:189` |
| `blueprint_render.go:90` iterates the whole slice with no limit | confirmed | `blueprint_render.go:90,96` |
| Blueprint caps are at `blueprint.go:108-110` | confirmed | `blueprint.go:108-110` |
| `events_test.go` asserts exactly eight unemitted names | confirmed | `events_test.go:85-95` |
| The `Review` vertex's existing semantics are compatible with the new gate | refuted — `Review` is the M5 score-band record | `06-judgment.sql:15-26`; `implementation-plan.html:903-930` |
| OQ-32 candidate 1 is available | refuted — the rubric *is* rendered into the Blueprint; BR-39 refuses dispatch on Blueprint drift | `blueprint.go:183-185`; `brd.html:1294`; design `:106`, `:440` |
| D-025 makes the product owner the runner of every check | confirmed | `decision-log.html:243-249` |
| Counts: 67 decisions, 31 open questions, 45 rules, 356 QA cases, 243 Go tests | confirmed | verified across all four registers |
| F6: `README.md:99` "179 tests", `README.md:105` "315 cases" | confirmed | `README.md:99`, `:105` |
| F7 ledes: "Sixty-three entries", "Thirty decisions"/"All twenty-four", "Thirty-seven rules" | confirmed | `decision-log.html:18`; `open-questions.html:17`, `:74`; `brd.html:1213` |
| The four count locations that must move together | confirmed | `qa-test-plan.html:19`, `:79`; `toc/product.js:162`, `:168` |
| F1: the skill is referenced by nothing outside itself | confirmed | grep matches only the skill itself |
| F1: the skill's quote at `SKILL.md:11` | confirmed | `SKILL.md:11-13` |
| F1: the skill's parenthetical "(documented gap in `deploy/STACKS.md`)" | refuted — STACKS.md documents worktrees as a working mechanism | `deploy/STACKS.md:175`, `:192-197` |
| F2: `gaos-deploy.md` lines 9-13 vs 15-16, 32, 40, 42, 49-52 | confirmed | `gaos-deploy.md:9-13`, `:15-16`, `:32`, `:40`, `:42`, `:49-52` |
| F3: `.claude/CLAUDE.md` is 11,042 B / 166 lines | confirmed for HEAD, stale for the working tree (11,076 B / 168 lines) | `git show HEAD:.claude/CLAUDE.md` → 166 / 11,042 |
| F4: `emil-design-eng/SKILL.md` is 27,509 B, the largest file in `.claude/` | confirmed | `wc -c` |
| F4: "20 mentions of React-family technology" | refuted — 7 under the narrow vocabulary; 19 under a broad one | grep counts |
| `.claude/` is 164 KB and four skills are 124 KB of it | confirmed under `du -sh` (block-rounded); apparent file bytes are 95,796 and 74,620 | `du -sh` vs `du -sb` |
| The isolation marker is planted and removed around each run | refuted for the present state — the marker is still in `.claude/CLAUDE.md` | `grep -c "EGM-MARKER" .claude/CLAUDE.md` → 1 |
| M14.0 precedent: 36 forward specifications landed before code | confirmed | `implementation-plan.html:1266-1270` |
| Next-free identifiers D-067, OQ-31, BR-45, M16, T-183, TC-356, version 0.33.1 | confirmed | all four registers plus `toc.js:22` |
| `design-gate.html` is a new page | confirmed — the file does not exist | directory listing |
| "No change to the published product" | refuted — the change list modifies six published pages and adds a seventh | design `:106` vs `:507-515` |

## Not verified

- The three rubric files do not exist — `.claude/rules/` is not present in the
  tree, so the rubric could not be read and no compliance check against it was
  possible.
- `.claude/CLAUDE.md` contents — off limits by instruction. F3's byte/line figures
  were checked against HEAD via `git show` and against the working tree via `wc`.
- Whether `omitClaudeMd` is a real, honoured Claude Code agent-frontmatter key, and
  whether `model: opus` resolves as expected — external to this repository.
- Whether `.agents/skills.json` and `.agents/rules/*.md` are consumed by any
  harness — nothing in the repository references them.
- The publication path for `docs/articles/*` to mfardiansyah.id — no publishing
  mechanism exists in this repository.
- Whether ai-manager can provision a workspace with a rubric but no product
  Blueprint (OQ-32's precondition) — only the BR-39/BR-42 constraints were checked.

Counts: B=2 M=13 N=0
