# Goldfish review — comprehension lens, round 2

**Target:** `docs/designs/egm-design-review.md` (v2)
**Reviewer:** one fresh-context subagent, zero-context (the "comprehension" lens)
**Date:** 2026-11-04
**Method:** Elephant-Goldfish Model — the author revised the design after round 1;
this lens re-read the revision and checked that it reads. Round 1's other four
lenses are in the sibling reports; the triage is
`egm-design-review-consolidated-1.md`.

**Counts: B=0 M=10 N=5**

No Blocker survived the revision. That is the round's headline: the structure
holds, and what remains is precision — the difference between a document a
reviewer can act on and one they can only nod at.

## What I think this does

### What this proposes

GAOS — a Go platform that dispatches AI agents into per-product workspaces — is
to adopt the *Elephant-Goldfish Model*: whoever holds the context drafts a
design document, and separate reviewer agents with *no memory of that
conversation* read that document alone and attack it. This lands in two places.
**Layer 1 (the harness)** is for GAOS's own repository: a feature-sized change
starts as a document under `docs/designs/`, five specialist reviewer agents
(comprehension, zoom-out, critic, readiness, security) each read it alone, every
finding gets exactly one of four dispositions (`PROPOSE FIX` / `CONTEST` /
`ESCALATE` / `DECLINE`), and fixes are applied to the *design* before any code
is written. **Layer 2 (the platform gate)** is the same idea inside the
product, specified but deliberately not built: decision D-068 (nine clauses),
three new business rules (BR-46…48), one open question (OQ-32), one milestone
(M17) and six forward QA specifications, with no Go code because the event
catalog is closed (BR-05) and the isolation mechanism is still undecided.
Alongside both, the change sweeps the agent workspace: three rule files move
out of the always-on layer into path-scoped rules, two skills are deleted, a
dozen stale counts are corrected, and `deploy/check-workspace.sh` is left behind
to stop the drift returning.

### How the existing system works, per this document

GAOS dispatches agents into per-product workspaces, and the *Blueprint* — the
generated `CLAUDE.md` plus `.claude/rules/*.md` of a product — is re-loaded on
every turn, including a resumed one. So even a brand-new session arrives
carrying the product's problem statement, its 48 domain rules and its whole
decision history, which is why "a fresh session is therefore not a zero-context
turn" and why Layer 2 cannot simply be implemented as "open a new session".
Three vocabularies are closed and may not be widened casually: the event catalog
(BR-05), the `Task.status` vocabulary (a DDL regexp plus a vocabulary test) and
the docs component vocabulary. Work is tracked in four registers — business
rules in the BRD, decisions in the decision log, open questions in the
open-question register, test scenarios in the QA plan — and a QA row whose
status is `Not implemented` is what that register calls a *forward specification*.
The repository's own agent workspace is a separate thing: `.claude/CLAUDE.md` is
always-on, four skills sit under `.claude/skills/`, and the audit found that
layer bloated (~95.8 KB), partly wrong (an orphan skill that contradicts the
deploy agent), and out of step with its own registers.

*Solid on inspection:* the isolation argument (§"Isolation, and its honest
limit"), the triage vocabulary, the flow diagram, the validator spec and the
audit's F1/F7/F8 evidence all read cleanly; the BR-38 quote, the
Blueprint-loads-every-turn quote, the D-056 quote, the `signedIn()` placeholder,
the three schema DDL anchors and the egress detector set were verified and all
six check out.

## Findings

### "Forward specification" is never defined, and the rows the document publishes do not do what it says they do - Severity: Major
- Affected scope: §Overview; §Goals; §"Why it stays unbuilt"; §"The eight new QA
  cases"; §Risks
- Finding: GAOS's register gives the term a fixed meaning — a QA row whose Status
  is `Not implemented`: "an asserted value written before the code that produces
  it… **it is a fixed value**". The design uses "forward specification" five
  times and never states that meaning or links it, so a reader cannot match the
  Overview's "six forward specifications" to §"The eight new QA cases", which
  tabulates *eight* rows (TC-357/358 `Implemented`; TC-359…364 `Not
  implemented`) — the six only emerges if you already know the register's Status
  vocabulary. Worse, the document asserts that "M17's forward specifications
  assert **concrete values**", but the six rows it publishes assert none: TC-360
  says only "the implement task is not dispatched while the design is
  unreviewed", TC-361 only "the reviewer's packet does not carry the brief, the
  handoffs or the failure history". Neither names the packet-kind set, the
  `Task.kind` value or the `Artifact.kind` value that clauses 2 and 6 require.
  Under the rule file this design creates, such a row is not a forward
  specification but a wish. The round-1 consolidated report states that "the
  revision uses the register's sense throughout **and says so**" — v2 does not
  say so.
- Recommendation: Add one sentence — "A *forward specification* is a register row
  whose Status is `Not implemented` — an asserted value written before the code
  that produces it" — and give each of TC-359…TC-364 the fixed value it
  promises.

### D-068's nine clauses are not individually readable, and clause 2 asserts a schema clause 6 says does not exist - Severity: Major
- Affected scope: §"The ruling", clauses 2, 5, 6; §"What is already there"
- Finding: The ruling uses six GAOS-specific nouns with no definition anywhere
  in the document: `Artifact`, `Task`, `TaskRun`, "request id", session "reason
  `reviewer`", and `DEPENDS_ON`/`BLOCKED`; clause 5 adds the `Review` vertex,
  `verdict`, `finalScore` and "the five dimension scores"; clause 6 adds
  `Task.kind`, the `Artifact.kind` regexp and "the planner's existing mechanism
  (M3.2)". Their definitions live in the BRD's data model, which the ruling never
  points to — and §"What is already there", which does explain the `Review`
  vertex, comes 45 lines *after* it. A reader who has not read the rest of the
  document cannot parse clause 2. Clause 2 also states `Artifact` kind `design`
  and `Task` kind `review` as present-tense facts while clause 6 says "Two DDL
  widenings are needed when M17 is built" — the ruling contradicts itself unless
  the reader knows which half is "today" and which is "target". This matters
  concretely: `Artifact.kind`'s regexp does not admit `design`, so a write using
  it would be refused at commit.
- Recommendation: Put a short glossary under the ruling, link the BRD's data
  model, and mark each clause's assertions as *today* or *target*.

### The milestone M17 is named, counted and cross-referenced, but never described - Severity: Major
- Affected scope: §Overview; clause 5; clause 8; §"Why it stays unbuilt"; change
  list entry 33; §"The eight new QA cases"; §Execution order
- Finding: The change list promises that the implementation plan gains "M17 as
  `<h2 id="m17-design-gate">`, **four sub-modules** with h3 ids, register rows
  **T-184…T-188**", and the body depends on two of those sub-modules by name
  (`M17.1`, `M17.4`) — but no sub-module is named, no task is described, and no
  table lists them. The "five concrete values" that the round-1 consolidated
  report says were written into the design do not exist in v2: the phrase
  "concrete value" occurs exactly once, as a claim rather than as a list. A
  reader cannot tell what M17 contains, which is the document's own stated
  reason for writing it.
- Recommendation: Add a five-row table under §"The ruling": T-184…T-188 with
  description, sub-module, related identifiers and the concrete values each
  fixes.

### "Blueprint" is used about eighteen times and never defined, though it is the premise of the whole Layer 2 argument - Severity: Major
- Affected scope: §"The problem" P2, P3; §Non-goals; §F8; §"The rules" trigger
  list; §OQ-32
- Finding: The reader is told the Blueprint "loads on every turn" and that its
  standing files are "the product's problem statement, its 48 domain rules, its
  decision list", but never what the Blueprint *is*: GAOS's generated
  per-product agent workspace, written by `dashboard/blueprint.go` as
  `CLAUDE.md`, `.claude/rules/00-gaos-contract.md`, `10-brd.md`,
  `20-stories.md`, `30-decisions.md`. Because "the Blueprint loads on every turn"
  is the premise that makes a fresh session *not* a zero-context turn, and that
  premise is what decides the shape of Layer 2 and the refusal to build, a reader
  who does not know the term cannot follow the argument.
- Recommendation: One sentence in P2, then use it consistently.

### The repository's `.claude/rules/` and the product's Blueprint `.claude/rules/` are the same path and the design never separates them - Severity: Major
- Affected scope: §"The rule layer"; §"What a reviewer is given" item 2; §OQ-32;
  change-list entries 18-19 and 21-23
- Finding: The document writes `.claude/rules/rule-index.md` (the repository's
  own rules for agents working *on* GAOS) and `.claude/rules/*.md` (the product's
  Blueprint files, written by GAOS itself) without ever distinguishing them. A
  reader reaches OQ-32's key sentence — "The rubric the reviewer needs *is* part
  of the Blueprint — `dashboard/blueprint.go:183-185` renders `renderBRD(m.BRD)`
  into `.claude/rules/10-brd.md`" — with no way to know that these are two
  different directories in two different places, and will read a collision rather
  than the fact that the open question is *which* rules the reviewer can be
  given. The rule file this change creates resolves it explicitly ("Two different
  things are called 'rules', and they are not the same…") — but the design, which
  is the artefact under review, does not.
- Recommendation: Add the same two-sentence disambiguation to §"The rule layer",
  and say "the repository's rule layer" / "the product's Blueprint rules"
  consistently in OQ-32 and §"What a reviewer is given".

### "Elephant" and "Goldfish" — the document's two central terms — are defined 430 lines in, after the metaphor has been used - Severity: Major
- Affected scope: title and §Overview; §Non-goals; §"The policy"; §"Cost"
- Finding: The first definition of the two roles is in §"The policy", after the
  Overview, the problem statement, the ten-finding audit and the non-goals. The
  document's spine is a role metaphor — Elephant, Goldfish, operator, harness —
  and it is what the title, the NOTICE and the published article are built on,
  yet nothing in the first 430 lines says which role the author of *this*
  document plays, and the term is used before definition in the non-goals. A
  naive reader meets the metaphor as the frame before meeting the roles it
  names.
- Recommendation: Move the three-role table from §"The policy" into the
  Overview, one line each, and have §"The policy" link back to it.

### Two of the eight count strings this change publishes are wrong: the "answered" count rises for a question that is explicitly Open - Severity: Major
- Affected scope: §"The counts that move" rows for `toc/product.js` lines 78 and
  110; §"The eight new QA cases"
- Finding: The register holds 31 open questions, 20 of them answered. This
  change adds OQ-32, which the document itself records as **Open**. The change
  list nonetheless rewrites the ToC to "the thirty-two-question register,
  **twenty-one** of them answered" and "Thirty-two decisions … **twenty** now
  answered" — the answered count goes up by one for a question that is open, and
  the two strings disagree with each other (21 vs 20) exactly as they already do
  today (20 vs 19). The validator's check 3 is then required to encode whatever
  is written here.
- Recommendation: Keep the answered count at twenty, or name the question this
  change answers. It answers none.

### The same set of count strings is described three times with three different totals, and one cross-reference points at the wrong entry - Severity: Major
- Affected scope: §F9; change-list entry 36; §"The counts that move"
- Finding: F9's prose says the ToC "invalidates at least seven of them" and the
  table immediately beneath it has eight rows. Change-list entry 36 says "**eight**
  count strings updated: the seven in F9's table plus the QA-plan pair (entry
  38)". The counts table lists *ten* `toc/product.js` lines. And "(entry 38)"
  points at `pustaka/docs/assets/toc.js`, which holds `product.semver` and no
  QA-plan pair — the QA-plan pair is `toc/product.js:162` and `:168`, inside
  entry 36 itself. An implementer who updates "eight strings" leaves two stale
  strings in the published ToC, which is precisely the drift class this change
  exists to kill.
- Recommendation: State one number and drop the "(entry 38)" pointer.

### "Rensin" is an unintroduced name and the figure behind a published target has no source - Severity: Major
- Affected scope: §"Cost, and how the gate is measured"
- Finding: "Rensin estimates about 30% of Goldfish suggestions are worth acting
  on" is the sole origin of the number. The name is never introduced and there
  is no link. The same paragraph converts it into a published target — "Target:
  at most one third" — and §Risks calls the metric "measurable rather than
  quoted", while the baseline *is* a quote.
- Recommendation: Name the author and link the essay, and say plainly that the
  figure is the baseline and the measured ratio is the real number.

### The motivating failure is cited to an artifact that does not exist in this repository - Severity: Major
- Affected scope: §"The problem" P2
- Finding: "This is the failure the reference note on this repository's sibling
  site documented concretely: a handler outlived the server's write timeout, and
  the same bug shipped four times" — no note is named, linked or dated, and
  "write timeout" appears nowhere in `pustaka/docs/`. The document's own
  standard makes this an unchecked claim, and it is the concrete evidence the
  whole problem statement rests on.
- Recommendation: Cite the note with its path or URL, its date and the four
  shipping incidents, or restate P2 from the evidence the repository carries.

### "The gate" has three names and is used with the definite article before any of them is introduced - Severity: Nit
- Affected scope: §Overview; §F10; §"Layer 1 — the harness"; §"Layer 2"; clause 5
- Finding: The Overview opens with "GAOS's own development loop gets **the
  gate**", as though the reader already knows which gate; F10 then decides that
  the *product's* gate is called "the design gate"; Layer 2's heading calls it
  "the platform gate"; Layer 1's heading calls it "the harness"; the published
  page is `design-gate.html`. A reader cannot tell whether "the gate" in Layer 1
  and "the design gate" in Layer 2 are one thing or two.
- Recommendation: Introduce it once in the Overview and use those two names
  consistently.

### Two `file:line` anchors do not point at what they claim, and the headline byte count cannot be re-derived - Severity: Nit
- Affected scope: §P1; §F1; §F4
- Finding: `.claude/agents/gaos-frontend.md:37-40` is cited twice for the quote
  "plain JS only … No framework, no bundler, no npm"; that text is at lines
  **34-35** of that file. `.claude/skills/rebuild-restart-prod/SKILL.md:11` is
  cited for "Prod has no separate git worktree yet…"; that sentence begins at
  line **12**. And `.claude/` "is 95,796 bytes" while the sum of the file bytes
  in the committed tree is **95,762** — the 34-byte gap is exactly the current
  worktree edit to `.claude/CLAUDE.md`, so the two numbers come from two
  different trees. The document says the sizes were "taken with one command" and
  that the counting command is in the verification plan, but the verification
  plan contains only the React-vocabulary command.
- Recommendation: Correct the anchors, state 95,762 for the committed tree, and
  add the byte-count command to the verification plan.

### The rubric is fixed by enumeration in the design, and the enumeration is already out of step with the rule file it specifies - Severity: Nit
- Affected scope: §"What a reviewer is given" item 2; §"The rule layer"
- Finding: The design says the reviewer's rubric is "the three files in
  `.claude/rules/`, plus the authoritative domain sources named in
  `.claude/rules/rule-index.md`: the BRD … the decision log … and the
  open-question register" — three domain sources. The `rule-index.md` this
  change creates names five more: the authoring spec, the authoring guide,
  `deploy/STACKS.md`, `deploy/edge/README.md` and `CONTRIBUTING.md`. A reviewer
  who trusts the design's list will not read the docs authoring spec, even
  though this change publishes a docs page.
- Recommendation: Replace the enumeration with the reference.

### The validator's behaviour on paths that do not exist yet is unspecified - Severity: Nit
- Affected scope: §"The validator" checks 5 and 6; §Verification plan
- Finding: Check 6 scans `docs/designs/reviews/`, which does not exist at HEAD.
  The pre-sweep run is required to "fail and name each defect" and says nothing
  about a missing report store; a shell `grep` on a missing directory exits 2,
  which is neither the script's `ok` line nor a clean `FAIL` line.
- Recommendation: State the expected result for a missing report store.

### The design does not say what decides a rule's `paths:` scope, so the scope can drift without contradicting it - Severity: Nit
- Affected scope: §"The rule layer" table; change-list entries 2 and 3
- Finding: F3 makes the point that the scope is what makes the saving real,
  yet the design pins the scope by example only and never says what a rule
  *owns*. The `qa-test-plan.md` written so far loads on `dashboard/**`,
  `pustaka/**` and `deploy/**` — a materially larger footprint than the
  design's, and one the design does not forbid.
- Recommendation: Add one line to F3 stating the principle.

## Not verified

- The three rubric files did not exist when this review began — `.claude/rules/`
  was absent, and the three files appeared during the review, i.e. they are the
  *output* of this design, written in parallel with the reading. They were read
  as they stood at that moment and may still change; these findings are about
  the design, not about those files.
- Not checked: the external claims at `code.claude.com/docs/en/memory.md` (the
  `paths` frontmatter semantics and the `omitClaudeMd` v2.1.271 floor);
  `dashboard/sim_test.go:225` as the only emitter of `ReviewCompleted`;
  `deploy/STACKS.md:175`; the React-vocabulary counts (7 / 2 / 0); the `Rensin`
  30% figure; the M16 row shape in the implementation plan.
- Verified but not re-verified in depth: the Go test counts (221 + 22 = 243 ✓),
  `TestTheTaskStatusVocabularyMatchesTheDDL` (exists in `dashboard/schema_test.go`
  ✓), the six "design review" occurrences in `brd.html` ✓, and the
  `Artifact.kind` / `Task.status` / `Review` DDL anchors ✓.

Counts: B=0 M=10 N=5
