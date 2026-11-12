# Consolidated Goldfish review — the EGM rollout in GAOS

**Target:** `docs/designs/egm-design-review.md` (v1)
**Date:** 2026-11-04
**Method:** Elephant-Goldfish Model — the author (Elephant) drafted the design;
five zero-context reviewers (Goldfish) attacked it under five different lenses;
the author then triaged every finding.

## Reviewer status

| Reviewer | Lens | Status | Report |
|---|---|---|---|
| comprehension | paraphrase first, then findings | **not run in round 1** — exceeded that round's time budget threefold and was stopped before it produced a report. **Run in round 2 against the revision**: 0 Blockers, 10 Majors, 5 Nits | `egm-design-review-comprehension-2.md` |
| zoom-out | simplicity / smallest sufficient change | completed | `egm-design-review-review-1.md` |
| critic | architecture + structure + style, claims verified against the repository | completed | `egm-design-review-critic-1.md` |
| implementation-readiness | zero-decision rule, per-change verifiability | completed | `egm-design-review-readiness-1.md` |
| security | trust boundaries, abuse cases, Critical/High/Medium/Low | completed | `egm-design-review-security-1.md` |

A reviewer that returns nothing **did not review**. The comprehension lens was
stopped after 50 minutes — the other four took between 10 and 15 — so it is
recorded as `not run` and is never counted as a reviewer with zero findings. It
is re-run against the revised document in round 2.

Its partial pass did surface one finding the revision adopts: GAOS's own
register gives "forward specification" a specific meaning — a QA scenario under
*"Test scenarios — not yet implemented"* — which the design did not state. The
revision uses the register's sense throughout and says so.

**Round 1 counts: B=5 M=31 N=6** (42 findings consolidated from 58 raw findings
across the four lenses that completed; the security lens uses a four-label scale
of its own and contributed 4 High, 4 Medium and 1 Low, which fold into the majors
and the nit below).

**Round 2 counts: B=0 M=10 N=5** — the comprehension lens against the revision.
Every one of the fifteen was accepted. The design is now **v3** and is in the
repository as `docs/designs/egm-design-review.md`; the round-2 triage is at the
end of this report.

## Isolation probe

- A canary was planted in `.claude/CLAUDE.md` — the file a context-leaking
  reviewer would read — before the run. **Its value is deliberately not
  reproduced here**, because a report that quotes the canary is indistinguishable
  from a report that leaked it.
- The reviewers were instructed to read only the design document, the rubric
  paths, and the repository files the document names; `CLAUDE.md` was on the
  off-limits list.
- After the run, a grep for the canary's value across `docs/designs/reviews/`
  returned **no match**. The marker existed in exactly one place.
- The marker was then removed and `git diff` showed only the intended change.
- **Result: Isolation PASS** — by instruction plus probe. It is *not* a tooling
  guarantee: MiniMax Code, the harness this run used, has no equivalent of
  Claude Code's `omitClaudeMd: true`, so a reviewer that ignored the instruction
  and read `CLAUDE.md` would have leaked the marker — and did not. That limit is
  recorded honestly in the article.

**The probe was not sufficient, and the reviewers said so.** Two of them found
the same thing independently: under Claude Code the probe is *vacuous by
construction*, because `omitClaudeMd: true` means the marker can never reach the
reviewer, so the probe is guaranteed green whether the mechanism works or the
key is ignored. There was no negative control — no step in which the exclusion
is removed once to prove the probe can fire. The revised design specifies a
positive control, canaries in every standing file, and a precisely worded
claim.

## Cross-reviewer convergence

Four independent lenses converged on the same three conclusions, which is the
strongest signal this run produced.

1. **The design's own change list did not derive from its own entries.** Three
   reviewers found it independently: the "Modified (13)" heading sat above 15
   entries, the summary table counted files while the headings counted entries,
   and `design-gate.html` was filed under "Modified" although it does not
   exist. The document says "the counts are derived rather than asserted" and
   `docs/designs/README.md` rule 4 says "The counts follow from the change
   list" — and its own list did not. The finding is **correct and checkable**.
2. **The headline evidence used two measurement bases at once.** The
   zoom-out and critic lenses both caught that ".claude/ is 164 KB" is a `du -sh`
   block count and "20 React-family mentions" cannot be reproduced by any single
   vocabulary. The bloat claim survives — 27,509 bytes of React guidance for a
   no-React dashboard is the real finding — but the numbers as stated did not.
3. **The isolation probe could not fail.** Four lenses (zoom-out, critic,
   readiness, security) found it, from four different directions: no positive
   control, the wrong files inspected, the marker left planted, and the
   instruction-only harnesses not covered at all.

## Triage

Every finding gets exactly one disposition. No severity was changed. A
comprehension gap was never contested — the document was fixed.

| # | Finding | Lenses | Severity as raised | Disposition | Resolution |
|---|---|---|---|---|---|
| 1 | Rules without `paths:` load at launch, so F3's disposition does not fix F3's defect | zoom-out | Blocker | **PROPOSE FIX** | The three rule files now carry `paths:` frontmatter, and the design quotes Claude Code's own rule that an unscoped rule loads with the same priority as `CLAUDE.md`. F3's claim is now true rather than asserted. |
| 2 | The isolation probe cannot fail; it misses the other standing files; the marker was left planted | zoom-out, critic, security (×2) | Blocker / High / Medium / Low | **PROPOSE FIX** | Canaries in every standing file; a required positive control; unconditional removal; the claim restated as "the canary did not reach a report"; a validator check for leftovers. |
| 3 | The gate's persistent state has no representation; the reused `Review` record cannot express it and collides with M5's semantics | critic, zoom-out | Blocker / Major | **PROPOSE FIX** | D-068 gained a clause specifying the review record: `kind = 'design'`, the finding shape `{severity, disposition, scope}`, `verdict` = `ITERATE` while a Blocker is open, `finalScore` unused, and the schema relaxation handed to M17.1. The M5 collision is now stated rather than assumed away. |
| 4 | The validator has no specified behaviour | readiness | Blocker | **PROPOSE FIX** | Six named checks, the house output format, the exit contract, the output-hygiene rule for secret-bearing paths, and the enforcement point are all specified. |
| 5 | The rubric, the agents and the policy are named, not specified | readiness | Blocker | **PROPOSE FIX** | Each rule file's content and `paths:` are listed; each agent file's frontmatter and method are listed; the policy's roles, triggers, security criterion, triage vocabulary, output contract and limits are enumerated. |
| 6 | The rubric excludes every rule that states the product's trust model | security | High | **PROPOSE FIX** | The rubric now includes the BRD, the decision log and the open-question register, named by `rule-index.md` with which answers which question. |
| 7 | The report store is a new permanent record with no secret-handling rule | security | High | **PROPOSE FIX** | A deny-list, the path-and-line rule, and a validator check that scans `docs/designs/reviews/` with GAOS's existing detector set. |
| 8 | BR-48's "only the operator clears it" has no identity to check | security | High | **PROPOSE FIX** | The rule text now names the precondition — the clearance is refused until the back office has a real identity — and it is a blocking precondition on M17, citing the placeholder auth and the pustaka HMAC shape to lift. |
| 9 | The publication step has no credential, disclosure or proposal-mode rule | security | High | **PROPOSE FIX** | Four publication rules travel with the article: host-side credential never passed or printed, proposal mode by default, rendered text scanned before posting, and a named disclosure boundary. |
| 10 | M17 is scheduled while the decision that shapes it is open | zoom-out, readiness | Blocker / Major | **PROPOSE FIX** | M17 stays, but T-184 is explicitly the task that closes OQ-32, and each of the six M17 forward specs is marked mechanism-independent or mechanism-dependent. The gate's *rules* are decided now; only the *mechanism* is deferred, and it is deferred to a named task. |
| 11 | OQ-32's first two candidates are inadmissible under the document's own non-goals | critic | Major | **PROPOSE FIX** | A table marks candidates 1 and 2 inadmissible and names candidate 3 as the surviving line of enquiry, with the reason each is inadmissible. |
| 12 | The Layer 2 reviewer's standing files are unconstrained by content | security | Medium | **PROPOSE FIX** | The content dimension was added to OQ-32: a redacted index, path-and-line records, and BR-42's allowlist named as the bounding control. |
| 13 | "Five" is EGM's stage count, not a GAOS reduction, and Alternative C is a strawman | zoom-out | Major | **PROPOSE FIX** | The reasoning is restated on GAOS's grounds — isolation per lens, triage per lens — the cost is now measured rather than asserted, and Alternative C says honestly that it stays open and which two lenses to keep if the cost proves too high. |
| 14 | Alternative A never evaluates "install the plugin and supply GAOS's rubric as the standard path" | zoom-out | Major | **PROPOSE FIX** | Added to Alternative A, with the two reasons the plugin is still rejected. |
| 15 | The document contradicts itself on whether a reviewer may read the repository | zoom-out, critic | Major | **PROPOSE FIX** | The Risks row, the Overview and the flow now say the same thing: a reviewer **can** and should check the repository; the limit is intent, not reach. |
| 16 | The rubric is placed in the namespace the Blueprint owns | zoom-out | Major | **PROPOSE FIX** | The rubric is named by explicit path, and the design states that in a product workspace the rubric must arrive by a different mechanism — which is OQ-32's surviving candidate. |
| 17 | The design-gate policy is written in five places | zoom-out | Major | **PROPOSE FIX** | One source of truth is named for the procedure (`docs/rules/gaos-design-review.md`) and one for the product rules (BRD §9.7); the rest are pointers. |
| 18 | The new workspace validator duplicates the validator the repo already has | zoom-out | Major | **CONTEST** | The new script's scope is the `.claude/` tree, the repository's summary files and the docs-site count strings; `pustaka check` owns page registration, file existence and internal links. There is no overlap. Putting the count assertions in `pustaka check` instead would make the check self-referential — the Go test suite would change the Go test count it asserts — and would move that count from 243, invalidating the very table this change writes. The reviewer's valid half is honoured: the scope is now stated explicitly rather than left to the implementer. |
| 19 | The headline numbers mix `du` blocks with byte counts; the React metric is not reproducible | zoom-out, critic | Major | **PROPOSE FIX** | Every figure is now a `wc -c` byte count, stated as such; the React-family vocabulary is written down and the counting command is in the verification plan. The figures become 7 vs 2, not 20 vs 2. |
| 20 | The M14.0 precedent is read as "docs instead of code" when it says "docs first" | zoom-out | Major | **PROPOSE FIX** | The precedent is now cited for sequencing, and BR-05 and OQ-32 are named as the actual reasons this change ships no Go code. |
| 21 | The audit missed a stale number in a file the change already edits | zoom-out | Major | **PROPOSE FIX** | `README.md:8`'s "Product version 0.29.0" is now F6, and the counts table covers it. |
| 22 | The published ToC mirrors eight more count strings the change invalidates | readiness | Major | **PROPOSE FIX** | All eight are in the counts table with their new wording, and the verification grep covers them. |
| 23 | The vendor-alias wiring re-creates an existing convention incompletely | zoom-out, security | Major / Medium | **PROPOSE FIX** | `AGENTS.md` is added, matching the alias set GAOS's own agy worker already uses (T-178, D-067); `GEMINI.md` and `AGENTS.md` are pointer-only so a rule cannot fork; a validator check refuses a rule sentence that appears in two files. |
| 24 | Six occurrences of "design review", not one | critic | Major | **PROPOSE FIX** | All six are amended in one pass — §7.5 twice, the §7.6 diagram, §8.5's band and prose, and BR-16 — and the change list names them. |
| 25 | Clause 5 disarms the only self-repair mechanism on a failed review | critic | Major | **PROPOSE FIX** | A new clause: a failed review is not a verdict, moves the review task to `BLOCKED` with reason `review-failed`, and never wedges the product. |
| 26 | BR-38's real consequence is a per-product turn ceiling; Layer 2's reviewer count is unspecified | critic | Major | **PROPOSE FIX** | Layer 2's count is fixed at one review task running the five lenses as sections, with the reason stated and the cost named. |
| 27 | D-056 is inert for the gate, and nothing carries the findings back to the implementer | critic | Major | **PROPOSE FIX** | D-056 is removed from the list of obstacles and restated as the problem it actually causes; a new clause makes the findings travel back to the implementer's next packet. |
| 28 | The gate is never priced and never measured | critic | Major | **PROPOSE FIX** | A cost section states the harness has no budget cap, the Layer 1 parallelism and the Layer 2 queueing, and names two success metrics with targets and the source they are measured from. |
| 29 | BR-17's rationale is wrong — two of its six limits are not per attempt | critic | Major | **PROPOSE FIX** | The clause now exempts only the three attempt-scoped limits, mirroring BR-34, and says the budget caps and the human-intervention threshold are not exempted. |
| 30 | M17's forward specifications are asserted, not written | critic, readiness | Major | **PROPOSE FIX** | The five concrete values, the review packet's structure, and T-184…T-188 with sub-phase and related identifiers are written into the design. |
| 31 | No execution phases, and the ordering dependencies are unstated | readiness | Major | **PROPOSE FIX** | Nine phases, each with its own exit condition. |
| 32 | The eight QA cases have no per-case verification and no full register fields | readiness | Major | **PROPOSE FIX** | Each row carries all five register fields, its status, and whether it is mechanism-independent or mechanism-dependent. |
| 33 | The "pre-sweep tree" baseline is not reproducible | readiness | Major | **PROPOSE FIX** | The baseline is pinned to `HEAD` in a clean worktree, and the defects the pre-sweep run must report are listed. |
| 34 | The file boundary is wrong for three of the files, and one path is not exact | readiness | Major | **PROPOSE FIX** | `design-gate.html` is created, the ID article has its exact path, and `docs/designs/reviews/README.md` is the named placeholder. |
| 35 | The remaining created files have no content, schema or target specification | readiness | Major | **PROPOSE FIX** | `GEMINI.md` and `AGENTS.md` are pointer-only, `.agents/skills.json`'s schema is given, the symlink targets are named as relative paths, and the new page's id, ToC key order and `page-meta` are specified. |
| 36 | "No change to the published product" contradicts six published pages in this change | critic, readiness | Major / Nit | **PROPOSE FIX** | The non-goal is restated as "No change to the product's runtime behaviour; the published docs change." |
| 37 | The NOTICE text and the article's published destination are unspecified | readiness | Nit | **PROPOSE FIX** | The NOTICE paragraph is quoted, and the publication rules name the destination and the four rules it obeys. |
| 38 | The change list's own labels do not derive | zoom-out, readiness, critic | Nit / Major | **PROPOSE FIX** | One basis: 42 files in 38 numbered groups, with the derivation printed. |
| 39 | The vocabulary test is cited at a comment, not at the test | zoom-out | Nit | **PROPOSE FIX** | The test is named as `TestTheTaskStatusVocabularyMatchesTheDDL` and the comment is cited for the rationale. |
| 40 | "Eight forward specifications" is never enumerated | zoom-out | Nit | **PROPOSE FIX** | The Overview says "six forward specifications" and the eight rows are tabulated with their automation, kind, scenario and status. |
| 41 | "No caller" is not "no user" for a slash-command skill | zoom-out | Nit | **PROPOSE FIX** | F1 says so explicitly, and the removal of the `/rebuild-restart-prod` surface is recorded in the Risks table. |
| 42 | The flow's "nothing else" is not what the reviewer receives | zoom-out | Nit | **PROPOSE FIX** | A subsection enumerates the five things a reviewer is given, and the off-limits list. |

## What changed in the design

The revised document (`docs/designs/egm-design-review.md`, v2, "In Review")
differs from v1 in these ways:

- **The always-on layer is actually smaller.** v1 moved the QA mechanics into
  an unscoped rule file, which Claude Code loads at launch anyway — so v1's
  headline saving did not exist. v2 scopes every rule file with `paths:`.
- **The probe can fail.** v2 adds a positive control, canaries in every
  standing file, unconditional removal, and a validator check for leftovers.
- **The gate has a record.** v2 specifies the finding shape, the verdict
  values, the `kind` discriminator and the M5 collision, and hands the schema
  relaxation to M17.1.
- **The validator is specified.** Six checks, the house output format, the
  exit contract, the output-hygiene rule and the enforcement point.
- **The rule layer is specified.** Each file's content and `paths:`, each
  agent's frontmatter and method, the policy's roles, triggers, security
  criterion, triage vocabulary and output contract.
- **The security layer is real.** A deny-list, the path-and-line rule, a
  scan of the report store, the BR-48 identity precondition, and four
  publication rules for the article.
- **The evidence is one basis.** Every figure is a `wc -c` byte count, and the
  React-family vocabulary is written down.
- **The platform half is honest about what is deferred.** M17 stays; T-184 is
  the task that closes OQ-32; two candidates are marked inadmissible; two of
  the six forward specs are marked mechanism-dependent.
- **The cost is named.** The harness has no budget cap, and two success
  metrics are stated with targets.

## Convergence

One Blocker survived the revision, and it is the one this review round could
not settle by argument: **the Layer 2 isolation mechanism is still open**
(OQ-32). That is by design — the question is recorded, narrowed, and given a
named task — but it means M17's first task is a decision rather than an
implementation, and no part of the platform gate can be built until it closes.


## Round 2 — the comprehension lens against the revision

Every finding was accepted; none was contested. The round found no Blocker, which
is the result the revision was for: the structure holds, and what remained was
precision.

| # | Finding | Severity as raised | Disposition | Resolution |
|---|---|---|---|---|
| R1 | "Forward specification" is never defined, and the rows published assert no concrete values | Major | **PROPOSE FIX** | The term is defined once, in the register's own sense ("a register row whose Status is `Not implemented`"), and each of TC-359…TC-364 now carries an **Asserted value** column — `Artifact.kind = "design"`, the `BLOCKED` reason codes `design-unreviewed`, `design-blocked` and `review-failed`, the reviewer's packet kinds `design` and `rules`, and the new packet kind `findings`. |
| R2 | D-068's nine clauses use six undefined nouns, and clause 2 asserts a schema clause 6 says does not exist | Major | **PROPOSE FIX** | A glossary under the ruling defines `Artifact`, `Task`, `TaskRun`, request id, `DEPENDS_ON`/`BLOCKED` and the `Review` vertex; every clause now opens with **today** or **target**, and clause 2 says the kinds do not exist today. |
| R3 | M17 is named, counted and cross-referenced, but never described | Major | **PROPOSE FIX** | A five-row table gives T-184…T-188 their description, sub-module, what each fixes and what it depends on — with **T-184 a decision, not an implementation**. |
| R4 | "Blueprint" is used eighteen times and never defined | Major | **PROPOSE FIX** | Defined once in P2 as the workspace GAOS generates for a product, with the file names. |
| R5 | The repository's `.claude/rules/` and the product's Blueprint `.claude/rules/` are the same path | Major | **PROPOSE FIX** | The rule-layer section now separates them, and "the rubric" and "the Blueprint" are used consistently. |
| R6 | "Elephant" and "Goldfish" are defined 430 lines in | Major | **PROPOSE FIX** | The three-role table moved into the Overview. |
| R7 | The "answered" count rises for a question that is explicitly Open | Major | **PROPOSE FIX** | The count stays at twenty. OQ-32 is Open, so it is not an answered question, and the design says so. |
| R8 | The count strings are described three times with three totals, and one cross-reference points at the wrong entry | Major | **PROPOSE FIX** | One number — ten count strings in `toc/product.js` — and the bad pointer is gone. |
| R9 | "Rensin" is unintroduced and the figure behind a published target has no source | Major | **PROPOSE FIX** | Named, linked to the essay, and described as the baseline rather than the measurement. |
| R10 | The motivating failure is cited to an artifact that does not exist in this repository | Major | **PROPOSE FIX** | The note is named, linked and dated, with the four shipping incidents. |
| R11 | "The gate" has three names | Nit | **PROPOSE FIX** | Introduced once in the Overview; the two layers are called apart throughout. |
| R12 | Two `file:line` anchors do not point at what they claim, and the byte count mixes two trees | Nit | **PROPOSE FIX** | `gaos-frontend.md:34-35` and `SKILL.md:12`; 95,762 bytes for the committed tree. |
| R13 | The rubric is fixed by enumeration that is out of step with the rule file it specifies | Nit | **PROPOSE FIX** | The design points at the index instead of enumerating. |
| R14 | The validator's behaviour on a path that does not exist is unspecified | Nit | **PROPOSE FIX** | A missing report store is a pass, not a failure — and the validator now says so. |
| R15 | Nothing says what decides a rule's `paths:` scope | Nit | **PROPOSE FIX** | F3 states the principle: a rule's scope is the set of files its subject lives in. |

## The validator's own verdict

The validator was written first and run against the **pre-sweep** tree at
`HEAD`, in a clean worktree, where it reported **18 failures** — the orphan
skill, the stack-mismatched vendored skill, four deleted files still named in
the project rules, and every stale count. After the sweep it reports **none**,
and exits 0.

Two of the eighteen were findings no reviewer had named: `.claude/CLAUDE.md`
still pointed at `deploy/kill-mid-task.sh`, `lease_test.go`,
`reconcile_test.go` and `smoke_test.go`, all deleted with the CLI execution
layer in v0.29.0. A rule that names a file nobody moved is a rule nobody
follows, and the always-on layer is the only place a session would have read
them.
