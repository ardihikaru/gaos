# The Elephant-Goldfish Model in GAOS

- **Author:** the GAOS operator, with a drafting agent
- **Status:** In Review
- **Date:** 2026-11-04
- **Revision:** v3 — revised after two Goldfish review rounds. Round 1 ran four
  lenses and found 42 findings (5 Blockers); round 2 ran the comprehension
  lens and found 15 more (0 Blockers). Both rounds are in `reviews/`; the
  consolidated triage is `reviews/egm-design-review-consolidated-1.md`.

## Overview

GAOS's agent workspace is about to grow a design gate. Google open-sourced the
Elephant-Goldfish Model (EGM) in October 2026 as a plugin for Claude Code, Codex
and Antigravity: the agent that holds the project context drafts a specification,
and separate reviewers with **no memory of that conversation** read the
specification alone and attack it. The context that produced the plan is the
context that cannot see the plan's blind spots.

This document specifies two things.

**The design gate** — the review step between a design and its implementation.
It lands in two places, and the two are called apart throughout this document.
Where this document says "the gate" without qualification, it means the design
gate in both layers.

- **Layer 1, the harness** — GAOS's own development loop. A feature-sized
  change starts as a document in `docs/designs/`, five zero-context reviewer
  agents read that document alone, every finding gets one disposition, and the
  fixes are applied to the *design* rather than to the code. GAOS's own rules
  become the reviewers' rubric, so a reviewer checks the design against the
  same rules an implementer must satisfy.
- **Layer 2, the platform gate** — inside the product, specified but not built.

**The three roles**, used throughout:

| Role | Context | Job |
|---|---|---|
| **Elephant** (the author) | the full conversation, the repo | explore the problem, draft the design, triage the findings |
| **Goldfish** (the reviewer) | the design document and the rubric only | find missing assumptions, unclear requirements, implementation risks |
| **The operator** | the whole picture | resolve the trade-offs, approve the design, decide what gets built |

**Layer 2 — the platform gate, specified but not built.** GAOS is a platform that
*dispatches* agents. The same gate belongs inside the product: a design task
produces a design artifact, a review task reviews it, and the implement task is
not dispatched until the review resolves. That half is written here as two
decisions, five business rules, one milestone and seven forward specifications —
and **no Go code**, because GAOS's own event catalog is closed (BR-05). The
isolation mechanism is decided (D-069, §"Layer 2 — the platform gate"), so M17's
first task is a build task rather than a decision; the milestone itself is still
unbuilt.

Alongside both layers, this change sweeps GAOS's agent workspace. The audit in §4
found an orphan skill that contradicts the agent that owns its job, a 27 KB
React-flavoured skill in a repository with no React, an 11 KB always-on file
that is about 90% test-register bookkeeping, and a dozen numbers that no longer
match their own registers.

## The problem

Three problems, in increasing order of cost.

**P1 — GAOS's agent workspace is bloated and partly wrong.** In the committed
tree, `.claude/` is 95,762 bytes; its four skills are 74,620 of those. One of those four is
referenced by nothing at all and states a fact about how prod is deployed that
the agent responsible for deploying prod contradicts. Another is 27,509 bytes of
React, Sonner and Tailwind guidance for a dashboard whose own agent file says
"plain JS only … no framework, no bundler, no npm"
(`.claude/agents/gaos-frontend.md:34-35`). The always-on layer is one 11 KB
file, of which roughly 140 of its 166 lines are the mechanics of one register.
And the counts inside it have drifted from the register it describes.

**P2 — nothing reviews a design before it is built.** GAOS has a readiness
rubric, an egress pass and a handoff contract.

The **Blueprint** is the workspace GAOS generates for a product: a `CLAUDE.md`
plus a set of `.claude/rules/*.md`, rendered from the product's record and its
policies by `dashboard/blueprint.go` and re-rendered when those change. It is
the product's standing knowledge, and it is what an agent turn loads. It has no step at
which a *proposed design* is read by someone who did not write it. The plan
for a task and the code that implements it are written by the same author, in
the same session, holding the same assumptions. This is the failure the reference note
on this repository's sibling site documented concretely — *Elephant-Goldfish
Model: a hands-on tutorial, tested on my own agents*, 3 November 2026,
<https://mfardiansyah.id/blog/elephant-goldfish-model-tutorial>: a handler
outlived the server's write timeout, and the same bug shipped four times
because each review shared the author's context.

**P3 — GAOS has no way to run a zero-context reviewer.** This is the problem
the website did not have, and it is the one that decides the shape of Layer 2.
Two of GAOS's own rules constrain the gate:

- **BR-38** (`pustaka/docs/product/brd.html:1293`): *"At most one turn is live in
  a product's workspace at any moment. A second ready task in the same product
  waits, blocked with the reason, rather than starting."* The consequence is a
  **per-product turn ceiling**: every review turn occupies the product's only
  slot, so a product's throughput is divided between reviewing and building.
- **The Blueprint loads on every turn.**
  (`pustaka/docs/product/blueprint.html:94-96`): *"Claude Code loads `CLAUDE.md`
  and `.claude/rules/*.md` from its working folder on every turn, including a
  resumed one — ai-manager measured this."*

The second is the sharp one. **A fresh session is therefore not a zero-context
turn.** The Blueprint's standing files — the product's problem statement, its
50 domain rules, its decision list — are in every reviewer's context whether or
not the reviewer asked for them. So the mechanism that gives a website its
Goldfish does not transfer to GAOS by opening a new session, and finding the
right mechanism is not a decision this document can make.

**D-056 is not on this list.** A retry resumes its session *"so the model sees
its own failed reply and the review that rejected it"*
(`pustaka/docs/product/decision-log.html:960-963`), but that rule governs
retries *within one task*. A review is a different task with its own session,
so D-056 never binds a review turn. What D-056 costs us is the opposite
direction, and clause 3 below repays it: the implementer must still receive
the review's findings.

## Goals

- GAOS's agent workspace has a design gate: a feature-sized change is written as
  a document, reviewed by zero-context reviewers, triaged, and approved before
  any code changes.
- GAOS's rules are a **rubric** the reviewers run, not prose an implementer
  skims. The rule layer moves out of the always-on file into `.claude/rules/`,
  scoped by `paths:` so it loads on demand rather than at every session start.
- The gate is **serial per product**, its isolation is **stated honestly**, and
  its cost is **measured** rather than asserted.
- The platform half is specified in GAOS's own registers — decisions, rules,
  milestone, forward specifications — so that M17 has a written target to pass.
  The mechanism that makes a platform reviewer zero-context is **decided**
  (D-069), not left as a question M17 has to open.
- The workspace bloat is swept, and a validator is left behind that fails when
  the bloat or the drift comes back.
- A published note (EN + ID) tells the story with the real numbers.

## Non-goals

- **No change to the product's runtime behaviour.** The published docs change;
  the dashboard, the store, the scheduler and the Blueprint do not. No Blueprint
  rendering change, no new task status, no `dashboard/` behaviour change.
- **No Go implementation of the platform gate.** No change to `dashboard/*.go`,
  no change to the event catalog, no change to the graph schema. Layer 2 is
  specified; M17 builds it.
- **No adoption of the dynamic-analysis agents.** The security *reviewer* is
  adopted, because EGM ships none and a design that touches GAOS's egress
  allowlist, its back-office cookie or its host-side secrets must not be
  reviewed only for wording. The DAST, CodeQL and pentest agents from the
  reference repository are deliberately **not** adopted: GAOS has one
  operator-facing back office and no HTTP API to attack, and `D-025` makes the
  product owner the runner of every check, so a CI-driven scanner has nowhere
  to run. The reason is recorded here so the next reviewer does not reopen it.
- **No fix for the Blueprint's unbounded decision injection** (§4, F8). It is a
  real finding and it is the platform's own version of the Elephant's
  unbounded memory, but it needs its own decision. It was folded into OQ-32
  and D-069 deliberately left it there as its own item rather than closing it
  with the isolation mechanism: it is a context-cost problem, not an
  isolation problem.
- **No change to `pustaka/`'s vendored engine.** The docs site changes, but the
  engine that serves it does not.

## The audit: what is actually wrong with the workspace

Ten findings, each with its evidence. This is the material the reviewers attack,
and it is why the sweep exists.

All sizes are `wc -c` byte counts, taken with one command, so they are
comparable. (An earlier draft of this document quoted `du -sh` block sizes
beside byte counts; the two bases differ by about 1.7× because of 4 KB block
padding, and the byte counts are the honest ones.)

### F1 — `rebuild-restart-prod` is an orphan and it contradicts the agent

`grep -rn "rebuild-restart-prod"` outside the skill itself returns nothing: no
agent, no rule, no document, no script names it. It is a skill with a
`trigger:` line and no automated caller — but it *is* a user-invocable slash
command, and a human may use it. What makes it wrong is the second part.

`.claude/skills/rebuild-restart-prod/SKILL.md:12` says:

> Prod has no separate git worktree yet (documented gap in `deploy/STACKS.md`),
> so it builds from whatever is currently checked out in this working tree

`.claude/agents/gaos-deploy.md:40` says:

> **`--tag <ref>` is required for prod.** It builds from a detached worktree at
> that ref instead of the working tree.

The script refuses a prod deploy with neither `--tag` nor `--from-tree`
(`gaos-deploy.md:49-52`), and `deploy/STACKS.md:175` documents worktrees as a
working mechanism, so the skill's "documented gap" parenthetical is wrong too.
The skill describes a mode the script will not perform.

**Disposition: delete the skill.** `gaos-deploy` stays the single entry point
for deploying, restarting and status-checking prod. The `/rebuild-restart-prod`
slash command goes with it; that surface is a deliberate removal, recorded in
the Risks table.

### F2 — `gaos-deploy.md` contradicts itself about the dev tier

Lines 9-13:

> **THE DEV TIER IS RETIRED (2026-11-03).** Only `prod` runs. Never deploy,
> restart or `up` dev, even when asked by name -- refuse and say dev is retired

Lines 15-16, fourteen lines later:

> You operate the two tunnelled gaos stacks on this VM -- `dev` (port 19301,
> `dev.gaos.mfardiansyah.id`) and `prod` (port 18301, `gaos.mfardiansyah.id`).

The usage line at 32 still accepts `<dev|prod>`, and line 42 still explains why
"Dev needs no tag and builds from the checkout". The retirement is asserted and
then un-asserted in the same file.

**Disposition: fix the body.** The retirement notice stays; the dev paragraph,
the dev alternative in the usage line and the dev sentence at 42 go.

### F3 — the always-on layer is a register's mechanics

`.claude/CLAUDE.md` is 11,042 bytes and 166 lines. One section holds all of it:
"Tests and automation are not done until the QA plan says so". Within that
section, "The register now covers everything, so keep it that way" runs from
line 78 to line 145 — 68 lines enumerating which `TC-nnn` belongs to which Go
test file. That enumeration is a **snapshot of a register**, and the register
itself is the source of truth.

Every session pays for it. The root `CLAUDE.md` exists only to `@`-import this
file, so this file *is* the always-on layer.

**The scope of a rule is the set of files its subject lives in**: the register
rule covers the register, the two test packages and the deploy automations; the
workspace rule covers `.claude/` and `docs/`.

**Disposition: restructure, and scope the move.** The mechanics move to
`.claude/rules/qa-test-plan.md`. Claude Code's rule loading decides whether that
actually helps: *"Rules without `paths` frontmatter are loaded at launch with the
same priority as `.claude/CLAUDE.md`"*
(https://code.claude.com/docs/en/memory.md). Moving bytes between two halves of
the always-on layer would shrink nothing. So the new rule files **carry
`paths:` frontmatter** and load on demand — `qa-test-plan.md` scopes to
`dashboard/**` and `pustaka/docs/product/qa-test-plan.html`,
`agent-workspace.md` to `.claude/**` and `docs/**`. The reduction is then real,
and the always-on layer is only what belongs in every session.

### F4 — `emil-design-eng` is 27.5 KB for a stack GAOS does not have

`.claude/skills/emil-design-eng/SKILL.md` is 27,509 bytes, the largest file in
`.claude/`. Counting the React-family vocabulary `react|jsx|tailwind|sonner|shadcn`
across the whole skill directory, it scores **7**; `review-animations/` scores
**2** over 18,455 bytes; `break-ui/` scores **0**. Those are the numbers this
document quotes, and the counting command is in the verification plan so the
figure cannot drift again.

GAOS's dashboard is Go `html/template` + HTMX + plain JS;
`.claude/agents/gaos-frontend.md:34-35` says so outright:

> **JavaScript:** plain JS only (`static/app.js`, `static/board.js`, `static/live.js`).
> No framework, no bundler, no npm.

Its motion framework also duplicates `review-animations/STANDARDS.md`, which
covers the same ground for a stack-neutral reader.

**Disposition: delete.** `review-animations` (8,491 B + 9,964 B of standards)
and `break-ui` (14,380 B + 8,268 B of catalogue) are stack-agnostic, are
actually referenced by `gaos-frontend.md:18-24`, and stay. This is a judgement
call and it is contestable — see §9.

### F5 — the always-on layer quotes a stale count

`.claude/CLAUDE.md:59` quotes the register intro as *"All three hundred and
forty-two cases…"*. The register intro actually says three hundred and
fifty-six. The rules file was describing a register it had fallen behind.

**Disposition: fixed by F3** — the quote lives in the mechanics that move.

### F6 — the README understates its counts and names the wrong version

- `README.md:99` — "179 tests". The real number is 243: 221 `func Test…` in
  `dashboard/`, 22 in `pustaka/`.
- `README.md:105` — "315 cases". The real number is 356, then 364 after the
  sweep, and 365 after the OQ-32 resolution.
- `README.md:8` — "**Product version 0.29.0** (`pustaka/docs/assets/toc.js`)".
  The file it points at says `semver: "0.33.1"`
  (`pustaka/docs/assets/toc.js:22`).

**Disposition: corrected.** The version line was missed by the first pass of this
audit: it is the same class of defect — a summary that stopped being true because
nobody re-derived it — and it sits in a file this change already rewrites.

### F7 — two docs-site ledes disagree with their own registers

- `pustaka/docs/product/decision-log.html:18` says "Sixty-three entries"; 67
  rows exist, and 68 after this change.
- `pustaka/docs/product/open-questions.html:17` says "Thirty decisions" and
  `:74` says "All twenty-four in one place"; 31 questions exist, and 32 after
  this change.
- `pustaka/docs/product/brd.html:1213` says "Thirty-seven rules"; 45 exist, and
  48 after this change.

**Disposition: corrected in the same change.**

### F8 — the Blueprint injects every decision into every session, uncapped

`dashboard/blueprint.go:189` calls `renderDecisions(m.Decisions)`, and
`dashboard/blueprint_render.go:90` iterates the whole slice with no limit. The
published Blueprint's caps are byte and file caps — `BlueprintMaxFileBytes`,
`BlueprintMaxTotalBytes`, `BlueprintMaxFiles` at `blueprint.go:108-110` — and
none of them bounds the *number* of decisions. The decision log holds 67 and
grows by one per decision.

Every agent turn in every product workspace loads all of them, because the
Blueprint loads on every turn (`blueprint.html:94-96`). This is the same
disease as F3 — a context that grows without bound and is paid for on every
turn — but it is in the product, not in the repository, and it is unbounded by
construction.

**Disposition: not fixed here.** It is recorded as a deliberate non-change and
folded into OQ-32, because the fix is a product decision (which decisions are
standing knowledge and which are history) rather than a mechanical one.
D-069 answers OQ-32 and leaves this one open by name: the decisions are
product knowledge rather than the author's process, so they do not contaminate
a reviewer, but they do cost context on every turn.

### F9 — the published ToC mirrors every count this change moves

`pustaka/docs/assets/toc/product.js` is the docs site's search and navigation
index, and its prose repeats the registers' counts. **Eight of its count
strings change in this change**, and one was already wrong:

| line | string | now |
|---|---|---|
| 36 | "43 business rules" | 48 |
| 56 | "BR-01 to BR-44" | "BR-01 to BR-48" |
| 66 | "the sixty-seven logged decisions" | sixty-eight |
| 78 | "the thirty-one-question register, twenty of them answered" | thirty-two |
| 110 | "Thirty decisions … nineteen now answered" | Thirty-two |
| 114 | "All thirty-one questions" | All thirty-two |
| 129 | "Sixty-three decisions" | Sixty-eight |
| 170 | "one hundred and eighty-five dashboard … unit tests" | 221 |

**Disposition: corrected, and checked.** Leaving these stale reproduces exactly
the drift class this change exists to kill, and the first draft of this
document's verification plan would not have detected it. The verification grep
now covers them.

### F10 — the same phrase means two different things in the BRD

"Design review" already exists in the BRD with a specific meaning: a
post-failure human or multi-agent **meeting** that records `MeetingStarted`
and later a `Decision` (§7.5 rows at `brd.html:423` and `:428`, the §7.6 flow
diagram at `:537`, §8.5's band at `:891` and prose at `:911`, and **BR-16** at
`:1250`). Layer 2 needs a different thing with a name that does not collide.

**Disposition: the gate is called the "design gate".** All six occurrences of
"design review" keep their existing meaning, and BR-16 is amended to say "a
design review (a meeting — §7.5)" so the business rule and the table agree.

## The five lenses

EGM ships nine reviewer personas. GAOS ships five, and the reason is GAOS's
stack, not EGM's arithmetic: GAOS is one language (Go) and one template system
(Go `html/template` + HTMX), so EGM's `design_critic`, `structural` and `style`
personas would ask the same question of the same design and are merged into one
critic lens. The other two reasons to keep five are operational, and both are
measurable:

- **Isolation is per lens.** Each lens is a separate zero-context session. One
  reviewer running five sections is one session, and one session is one
  author's assumptions applied five times.
- **Triage is per lens.** A finding is triaged against the lens that raised it,
  and a lens that is not asked a question cannot raise a finding in it.

The cost is real and is measured, not assumed: §"Cost" below.

| Agent | EGM origin | Question it asks | Scale |
|---|---|---|---|
| `.claude/agents/gaos-goldfish-comprehension.md` | `comprehension` | can a naive reader follow this, and can they say it back? | Blocker / Major / Nit |
| `.claude/agents/gaos-goldfish-zoom-out.md` | `zoom_out` | is this the smallest thing that removes the defect? | Blocker / Major / Nit |
| `.claude/agents/gaos-goldfish-critic.md` | `design_critic` + `structural` + `style` | does this survive contact with the system it lands in? | Blocker / Major / Nit |
| `.claude/agents/gaos-goldfish-readiness.md` | `implementation_readiness` | can two engineers implement this and get the same result? | Blocker / Major / Nit |
| `.claude/agents/gaos-goldfish-security.md` | *EGM has none* | what is the attack path, and what is the fix? | Critical / High / Medium / Low |

Every one of the five carries `model: opus`, `tools: Read, Grep, Glob` and
`omitClaudeMd: true`. Two constraints on that key are load-bearing and are
recorded in the agent files themselves: `omitClaudeMd` **requires Claude Code
v2.1.271 or later**, and it is **ignored when the agent runs as the main
session agent** via `--agent` or the `agent` setting. GAOS's own Claude Code is
2.1.317, so the floor is met.

The security lens is on its own scale on purpose: a security finding and a
wording finding must never compete for the same label.

### What a reviewer is given

The reviewer receives, in full:

1. **The target document path** — `docs/designs/<feature>.md`.
2. **The rubric path list** — the three files in the repository's
   `.claude/rules/`, plus **the authoritative domain sources named in
   `.claude/rules/rule-index.md`**. That index is the list; it names the BRD,
   the decision log, the open-question register, the docs authoring spec, the
   deploy topology and the contribution rules. The BRD is where the trust model
   lives — BR-32's egress pass, BR-39's drift gate, BR-42's workspace
   allowlist, BR-05's closed catalog — and a rubric that excluded it would
   leave the security lens reasoning about rules it never reads.
3. **The output contract** — the report shape and the severity vocabulary, which
   is in the agent file itself.
4. **The isolation instruction** — the off-limits list.
5. **Nothing else.** No authorial context, no prior turn, no handoff, no
   conversation.

### What a reviewer may and may not read

A reviewer **may** read the repository. It has `Read`, `Grep` and `Glob`, the
design contract requires every claim to carry a `file:line`, and confirming an
author's `file:line` claim independently is the entire value of a zero-context
reviewer. The limit is not reach; it is **intent**. A reviewer has artifacts and
no authorial intent: it cannot know what the author meant, only what the
document says and what the repository shows.

A reviewer may **not** read: `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`,
`.claude/CLAUDE.md`, `*.env`, `*.env.*`, `*.aim-*`, `*.pem`, `*.key`,
`**/credentials*`, `~/.ssh`, `~/.aws`, or any path outside the repository. Those
are the standing files and the credential-bearing files.

### What a reviewer may not write

A reviewer reports **path and line, never the matched text**. This mirrors
GAOS's own rule for a stored finding: a record must never carry matched secret
text (`dashboard/egress.go:222-238`). A finding that quotes a line from
`deploy/stacks/*.env` would put a live credential into a committed, append-only
file that every later session reads.

## Layer 1 — the harness

### The rule layer

**Two different things are called "rules", and they are not the same.** The
**repository's rule layer** is `.claude/rules/` in *this* repository: the rules
for the agents working on GAOS. The **product's Blueprint rules** are
`.claude/rules/` inside a *product workspace*: `00-gaos-contract.md`,
`10-brd.md`, `20-stories.md`, `30-decisions.md`, the `4x`–`6x` policy files and
`70-knowledge-sources.md`, written by GAOS itself. This document says
"the rubric" for the first, "the Blueprint" for the second.

Three files, each **path-scoped** so it loads on demand rather than at every
session start:

| File | `paths:` | Holds |
|---|---|---|
| `.claude/rules/rule-index.md` | none — it is the index, and it is short enough to always be useful | what to read before what; which domain source answers which question |
| `.claude/rules/qa-test-plan.md` | `dashboard/**`, `pustaka/docs/product/qa-test-plan.html` | the QA-register mechanics, moved out of `.claude/CLAUDE.md`, with the count reconciled |
| `.claude/rules/agent-workspace.md` | `.claude/**`, `docs/**` | the workspace layout, what may be edited, the design-gate trigger list, the vendored-skill policy, the reviewer deny-list |

`rule-index.md` is the one file that stays cheap enough to always read, and it
is what a reviewer starts from: it names the other two rule files **and** the
BRD, the decision log and the open-question register, and says which to read
for which question.

`.claude/CLAUDE.md` keeps only what must be in every session's head:

1. the QA-plan-in-the-same-change rule, in three lines, with a pointer to
   `.claude/rules/qa-test-plan.md`;
2. the closed event catalog (BR-05) and the closed component vocabulary;
3. the design-gate trigger list, in one line, pointing at
   `.claude/rules/agent-workspace.md`;
4. pointers to the three rule files and to the docs-site authoring spec.

Nothing is deleted without a destination.

### The policy

`docs/rules/gaos-design-review.md` is the **source of truth** for the procedure.
It holds:

- **The three roles.** *The Elephant* is whoever holds the context — the
  operator, or the agent drafting the change — and drafts the design, holds the
  trade-offs, and triages the findings. *The Goldfish* are the five
  zero-context reviewers, which read the document alone and may not change it.
  *The operator* resolves the trade-offs and approves; nothing is implemented
  from a design that has not been approved.
- **The trigger list.** A design is required when the change adds or changes: a
  Go handler, a store method, or a schema statement; an event type, a task
  status, or a packet kind; a Blueprint policy kind or a rendered Blueprint
  file; a validation script, a test, or a CI workflow; an agent, a skill, a
  rule, `CLAUDE.md`, `AGENTS.md` or `GEMINI.md`; a deployment, a stack file, or
  an edge config; or a published docs page. A **fix** does not need a design: a
  bug fix, a typo, a comment, a one-line config change and routine maintenance
  go straight to implementation.
- **The security criterion.** Run the security lens as well when the design
  touches authentication, authorisation, credentials, tokens, the egress
  allowlist, the workspace allowlist, the back-office surface, a new external
  origin, or an outbound call to one. A **Critical** or **High** security
  finding is a Blocker for the design's status: it cannot go to "Approved" with
  one open.
- **The triage vocabulary.** Exactly one disposition per finding: `PROPOSE
  FIX` (right, and this change fixes it), `CONTEST` (wrong, and the contest is
  checkable), `ESCALATE` (right, out of scope), `DECLINE` (right, but the fix
  costs more than the defect). Two rules load-bearing: a severity is never
  changed, and a comprehension gap is never contested — the document is fixed
  instead.
- **The output contract.** The report shape, and the severity vocabulary per
  lens. The general lenses use Blocker / Major / Nit; the security lens uses
  Critical / High / Medium / Low and `SEC-<n>` identifiers.
- **The limits, stated plainly.** Isolation is a property of the harness, not of
  the method. A reviewer has no authorial intent, only artifacts. Reviewers are
  noisy — roughly a third of what comes back needs triage rather than action.
  It costs tokens and time. A design whose review never completed is not
  approved.

### The runbook

`.claude/skills/gaos-design-review/SKILL.md` is the procedure: draft → review →
isolation probe → save reports → triage → apply → approve. Its steps are the
flow below, with the commands.

### The flow

```
  change request
        │
        ▼
  trigger list matches?  ── no ──▶ implementation
        │ yes
        ▼
  docs/designs/<feature>.md drafted — stands alone, every claim carries file:line
        │
        ▼
  canaries planted in every standing file (see "Isolation" below)
        │
        ▼
  five reviewers, each in a fresh context, each given the target path and the
  rubric path list — and nothing else
        │
        ▼
  reports → docs/designs/reviews/<feature>-review-<n>.md   (append-only, numbered)
        │
        ├── grep the reports for the canaries → must be empty
        ├── POSITIVE CONTROL: one run with the exclusion disabled must show the canary
        └── canaries removed unconditionally
        │
        ▼
  triage: PROPOSE FIX · CONTEST · ESCALATE · DECLINE — one per finding
        │   a severity is never changed; a comprehension gap is never contested
        ▼
  fixes applied to the DESIGN
        │
        ▼
  re-review if a Blocker survives
        │
        ▼
  operator sees the paraphrase, every Blocker and Major, and the triage table → approval
        │
        ▼
  implementation
```

### Isolation, and its honest limit

Under Claude Code the reviewers carry `omitClaudeMd: true`, so the author's
project memory is excluded **by the runtime** — provided the harness honours
the key (v2.1.271+, and not when the agent runs as the main session agent).
Under every other harness it is excluded **by instruction**, and the canary
probe is the only evidence.

The probe is specified so that it **can fail**:

- **Canaries in every standing file.** One unique string per standing file
  (`.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, each `.agents/rules/` target),
  not one token in one file. The grep covers all of them and must return
  nothing.
- **A positive control.** One run in which the exclusion is deliberately
  disabled must produce a report that **does** contain the canary. Without that
  step, a probe that can never detect anything is indistinguishable from a clean
  run.
- **Removal is unconditional.** The run is wrapped so that the canaries are
  removed whether the run succeeded, failed or was interrupted. A leftover
  canary in the always-on layer would be committed by the next commit.
- **The claim is stated precisely.** The probe proves *"the canary did not reach
  a report"*. It does **not** prove *"the file was not read"* — a reviewer could
  read a standing file and never quote it. That is a limit of the method, and it
  is why the probe is one piece of evidence and not the whole.
- **The validator checks for leftovers.** `check-workspace.sh` fails if the
  string `EGM-MARKER-` appears anywhere in the tree, so an aborted run is caught
  by the validator rather than by a later `git diff`.

GAOS already has a stronger mechanism available for the platform case: BR-39's
drift gate compares the *observed context* against a manifest, which is a
positive test of what was read.

## Layer 2 — the platform gate (specified, not built)

### The glossary the ruling uses

Six nouns appear in the ruling that a reader outside GAOS would not know. They
are defined here once; the data model itself is in
`pustaka/docs/product/brd.html` §7 and `artifacts/v1/gaos-arcadedb-model.html`.

| Term | What it is |
|---|---|
| `Artifact` | A stored thing a task produced — a prompt, a reply, a handoff, a diff. Its `kind` is a closed vocabulary. |
| `Task` | One unit of work in a workflow, with a status, a role and dependencies. |
| `TaskRun` | One attempt at a task. A retry is a new `TaskRun` against the same `Task`. |
| request id | The deterministic id GAOS sends with a turn so it can find it again after a restart. |
| `DEPENDS_ON` | An edge between two tasks: this one cannot start until that one is done. The planner already writes these. |
| `BLOCKED` | A task status meaning "waiting, with a reason". It exists today; the gate does not add a status. |

### The ruling

**D-068.** Nine clauses. Each says what is **today** and what is **target** —
the target is what M17 builds, and the two are marked so the reader never has
to guess which half applies.

1. **(rule — today and target)** A product's **implementation task may not be
   dispatched** while its design artifact is unreviewed, and never while the
   review carries an open Blocker.
2. **(target)** Once M17 lands, the design artifact is an `Artifact` of kind
   `design` and the review is a `Task` of kind `review`, with its own `TaskRun`
   and its own request id, on a **fresh session** with reason `reviewer`. Today
   neither kind exists: `Artifact.kind` does not admit `design`
   (`dashboard/schema/11-blueprint.sql:83`) and `Task` has no `kind` property at
   all.
3. The reviewer is fed from **GAOS-held state** (BR-41), never from a resumed
   session. Its packet carries the design artifact and the rubric. It does
   **not** carry the implement task's brief, the handoffs it depends on, or the
   history of failed attempts. This is the deliberate inverse of D-056.
4. **The findings travel back.** The reverse path is explicit: the review's
   dispositioned findings are attached to the implement task's next packet, so
   the implementer D-056 promises to see "the review that rejected it" actually
   does. The reviewer's own packet and the implementer's packet are different
   packets.
5. **The review record.** The gate's findings are recorded as a `Review`
   vertex with `kind = 'design'` — a discriminator, because the existing
   `Review` vertex belongs to the M5 score-band review engine and the two have
   incompatible meanings (§"What is already there"). The finding shape is
   `{severity, disposition, scope}` where `severity` is one of the general
   lenses' three labels or, for the security lens, one of its four; `scope` is a
   `path:line`; and the matched text is never stored. `verdict` takes the
   existing value `ITERATE` while any Blocker is open, and `ACCEPT` when none
   is. `finalScore` and the five dimension scores are **not** used for a design
   review and are left unset — which means the record needs either those
   properties relaxed for this kind, or a second vertex type. That relaxation
   is M17.1's first task, and it is a schema change, not a decision.
   **(Amended by D-069: the gate's record is an `Artifact` of kind `review`, and
   the `Review` vertex is left to M5.)**
6. **The gate is a `DEPENDS_ON` edge plus the existing `BLOCKED` status** — the
   planner's existing mechanism (M3.2) — **not** a new task status. The
   `Task.status` DDL regexp (`dashboard/schema/04-execution.sql:27-28`) and its
   vocabulary test (`TestTheTaskStatusVocabularyMatchesTheDDL`) stay untouched.
   Two DDL widenings are needed when M17 is built: `Task.kind` as a new
   property, and `design` added to the `Artifact.kind` regexp
   (`dashboard/schema/11-blueprint.sql:83`).
7. **A failed review is not a verdict.** The three attempt-scoped limits —
   `max_retry_count`, `max_escalation_count`, `same_failure_signature_limit` —
   are not consumed by a review turn that *returns a verdict*, mirroring BR-34.
   A review turn that *fails* — an agent crash, a gateway 410, a malformed
   packet — consumes none of them either, but it is not silent: it moves the
   review task to `BLOCKED` with reason `review-failed` and the implement task
   stays blocked, and the operator's queue is where the product waits. The
   per-workflow and per-tenant budget caps of BR-17 still apply to review turns
   (clause 8), and the human-intervention threshold is not exempted.
8. **Review turns are metered like any other turn.** They count against the
   same per-workflow and per-tenant budget caps. The budget guard (M10.3) is
   not built, so M17.4 must specify how review turns are metered.
   (**Amended by D-069: this is no longer part of OQ-32**, which was narrowed to
   the isolation mechanism alone; the metering is M17.4's own work.)
9. **The term is "design gate".** The BRD's existing "design review" keeps its
   meaning, and all six of its occurrences are amended in one pass to say
   "a design review (a meeting — §7.5)" so that the business rule and the
   tables agree.

**D-069.** Ten clauses. Decided on 2026-11-04 after reading the code that
assembles a packet, and it **answers OQ-32** rather than narrowing it. It
amends D-068's clauses 3, 5 and 6.

1. **(target)** **The isolation boundary is the packet, not the workspace.** A
   reviewer's packet carries exactly two kinds — `design` and `rules` — and
   not `task`, `context`, `handoff` or `contract`. The reviewer's *workspace*
   is the product's own workspace, holding the published Blueprint whole.
2. **(target)** **The standing files are the rubric, not contamination, and
   they stay exactly as published.** A reviewer judges a design against the
   product's domain rules, so a workspace holding the rubric but not the
   Blueprint would be a drifting workspace under BR-39, and the Blueprint is
   the digest-checked published artifact. The two candidates that reduce the
   standing files stay inadmissible — and the third is unnecessary, because the
   reviewer does not need a different workspace, it needs a different packet.
3. **(target)** **What is withheld is the author's session state, not the
   product's rules:** `handoff-previous.md`, `failure-previous.md` and
   `answer.md` — the previous stage's handoff, the failure lesson drawn from
   it, and the operator's answer to the author's question. Those are how an
   author's assumptions travel. The product's problem statement, domain rules
   and out-of-scope list are the *constraints* the design is judged against,
   and they travel as `rules`.
4. **(today)** **This is the default behaviour, not a hypothetical.**
   `freezePacketFor` is per-desk, not per-task-kind
   (`dashboard/store_mem.go:1316`), and `latestHandoffFor` is desk-scoped on
   purpose (`dashboard/store_mem.go:1248`): the next stage at a desk inherits
   the previous stage's handoff, so a workflow's stages chain. A review is a
   stage at that desk. Without a change, a reviewer inherits the author's
   handoff — the exact contamination the gate exists to prevent.
5. **(target)** **The design travels as a GAOS-held artifact.** A new packet
   part kind `design`, at `.gaos/packet/design.md`, screened outbound like
   every other part. There is no other route: `repoParts` is nil in every
   deployment (`dashboard/store_mem.go:168`), so a reviewer has no folder to
   open, and BR-42's allowlist admits only `CLAUDE.md` and
   `.claude/rules/*.md`. A design that lives in a repository cannot reach a
   reviewer in this product.
6. **(target)** **The findings travel back as context.**
   `.gaos/packet/context/findings.md`, a `KindContext` part, verbatim — the
   same semantics as the previous handoff. This is D-068's clause 4 made
   concrete, and it is what makes D-056's promise — the implementer sees the
   review that rejected it — true rather than aspirational.
7. **(target)** **The gate is the planner's own mechanism.** A `DEPENDS_ON`
   edge, the existing `BLOCKED` status, and the scheduler's existing
   dependency block. No new task status, so the closed `Task.status` DDL
   regexp and its vocabulary test stay untouched.
8. **(target)** **The gate's record is an artifact, not the `Review` vertex.**
   The `Review` vertex belongs to M5's score-band engine — `verdict` is a
   closed six-value regexp and `finalScore` is mandatory
   (`dashboard/schema/06-judgment.sql:14-26`) — and a design gate has no score
   to record and no band to fall in. The gate's live state is the task graph;
   its findings are an `Artifact` of kind `review`. No schema relaxation is
   needed.
9. **(target)** **Three closed vocabularies grow by one value each.**
   `Task` gains a `kind` property with the values `implement|design|review`,
   because the packet builder must be able to tell a review task from an
   implement task; `Artifact.kind` widens to admit `design` and `review`; and
   the citation roles gain `design`, so the design artifact is cited by the
   part that carries it.
10. **(limit)** **A limit, recorded rather than solved.** `Screen()` is
    outbound-only: a reply is stored verbatim as an artifact of kind `reply`
    (`dashboard/runner.go:190-215`). A finding that quotes a secret is
    therefore already in the append-only store by the time the outbound pass
    refuses the next dispatch. The egress pass is the backstop, not the cure;
    inbound screening is a separate piece of work and is not claimed here.

**The content dimension, answered.** The standing files may contain what they
already contain: the published Blueprint, whole. The question was framed as
though the standing files were a leak, and the code says otherwise — they are
the rubric. What may not *leave* the review is the matched text of anything,
and that is already the finding shape: a finding records WHERE a detector
fired and never WHAT it matched (`dashboard/egress.go:222-238`), so a reviewer
cites a standing file by path and line the same way it cites the design. The
content risk is handled at the sink, not at the source.

**What D-069 does not close.** The Blueprint renders every decision into the
standing files with no bound on their number
(`dashboard/blueprint_render.go:90`), so a workspace's context grows with the
decision log. That is a context-cost problem, not an isolation problem — the
decisions are product knowledge, not the author's process — and it stays open
as its own item rather than being folded into the gate.

### Layer 2's reviewer count

**One review task per design, running the five lenses as sections of a single
review packet.** The platform gate gets less signal than the harness on
purpose. BR-38 allows one live turn per product workspace, so five sequential
review turns would occupy the product's only slot five times and halve the
product's building throughput to buy a second opinion on a document. The
harness has no such ceiling — its five lenses are five independent sessions
against a repository, not against a product — so the harness can afford them.

The cost is named: under BR-38 the gate is **serial per product**, and a
product's review turns queue behind everything else in that product. That is
recorded in the Risks table.

### What M17 contains

Five tasks. **T-184 is the first build task** — OQ-32 is closed by D-069, so
the milestone no longer waits on a decision.

| Task | Sub-module | What it does | Fixes | Depends on |
|---|---|---|---|---|
| **T-184** | M17.1 | Build the review-shaped packet: the packet builder learns to tell a review task from an implement task, and a review's packet carries exactly `design` and `rules`. The author's handoff, failure lesson and operator answer are withheld — as they would otherwise be inherited from the desk. | the isolation | D-069, BR-49 |
| **T-185** | M17.1 | The design artifact and the kinds. Widen `Artifact.kind` to admit `design` and `review`; add `Task.kind` with the values `implement\|design\|review`; add a `design` citation role. The `Review` vertex is left to M5. | the schema | D-069, BR-50 |
| **T-186** | M17.2 | The review runner and the reviewer packet. One review task per design, on a fresh session with reason `reviewer`, carrying the design at `.gaos/packet/design.md` and the rubric and nothing else. | the packet | T-184, T-185 |
| **T-187** | M17.3 | The gate and the triage vocabulary. The `DEPENDS_ON` edge, the `BLOCKED` status, the four dispositions, the operator's queue, the gate's record as an `Artifact` of kind `review`, and the findings travelling back at `.gaos/packet/context/findings.md`. | the gate | T-186 |
| **T-188** | M17.4 | The budget accounting and the drift report. How review turns are metered against the per-workflow and per-tenant caps, and how a design that has drifted from the code is reported. | the budget | T-186 |

### Why it stays unbuilt

BR-05 closes the event catalog: *"the catalog in code is the catalog in the
document."* A new event name lands in the data-model document,
`dashboard/events.go` and the fold together, or not at all. And
`dashboard/events_test.go:85-94` asserts the set of unemitted names is exactly
eight, and that a catalog name which is neither emitted nor owed fails the
test.

Declaring M17's events without the code that emits them breaks that test.
Emitting them without the platform code is inventing evidence. So M17's forward
specifications assert **concrete values** and **name the events in prose only**,
with this document recording why the catalog is not touched yet.

### What is already there

The schema is ahead of the code here, which is convenient and also a trap.
`dashboard/schema/06-judgment.sql:14-26` already declares the `Review` vertex
with `verdict` (a closed six-value regexp), `finalScore` (mandatory), the five
dimensions, `weights`, `findings` (an untyped `LIST`), `reasonCode` and
`reviewedAt`; `dashboard/schema/08-edges.sql:71` declares `REVIEWED_BY`. There
is no Go type and no producer — `ReviewCompleted` is emitted only by
`dashboard/sim_test.go:225`.

But "ahead of the code" is not "ready for this feature". The `Review` vertex was
designed for the **M5 review engine**, whose contract is a five-dimension score
with bands (85-100 accept, 70-84 cleanup, 50-69 iterate, 30-49 escalate, 0-29
stop, safety<60 veto) and whose trigger is a *failed task*. The gate's contract
is a document reviewed *before* any task runs, with findings, severities and
dispositions, and no score at all. The two produce the same vertex with
incompatible meanings, so clause 5 adds a `kind` discriminator and says
plainly which properties the gate does not use.

The `Review` vertex's five dimensions — correctness, completeness, safety,
quality, efficiency — map one-to-one onto the five lenses by name, which is a
coincidence worth noting and not a design.

### The rules

A new BRD §9.7 "The design gate":

- **BR-46** — every implementation task has a design artifact and a review.
  Neither is optional. The review happens before the first dispatch.
- **BR-47** — a reviewer sees the design artifact and the rubric, and nothing
  else from the desk: not the brief, not the goal, not the previous handoff,
  not the failure lesson, not the operator's answer, not the stories or
  acceptance criteria. It records path and line, never the matched text.
- **BR-48** — an open Blocker stops the dispatch and the task goes to the
  operator's queue. Only the operator clears it. **The clearance is refused
  until the back office has a real identity to check**: GAOS's back-office
  login is a placeholder — `signedIn()` accepts `gaos_bo=1` from any request
  (`dashboard/handlers_bo.go:15-34`) — so an identity-bearing clearance is a
  **blocking precondition on M17**, not an assumption. The shape to lift is
  already written down next door: pustaka's HMAC-signed cookie, credential
  fingerprint and per-IP throttle (`pustaka/main.go:712`, `:728-753`).
- **BR-49** — the isolation boundary is the packet, not the workspace. The
  reviewer's workspace is the product's own and holds the published Blueprint
  whole; the standing files are the rubric, and a reduced set would be drift
  under BR-39. What is withheld is the author's session state.
- **BR-50** — a design reaches a reviewer only as a GAOS-held artifact in the
  packet, because GAOS reads no folder and its allowlist admits only the
  instruction files. The findings travel back the same way, as context. The
  gate is the planner's own `DEPENDS_ON` edge and `BLOCKED` status, and its
  record is an artifact, not the score-band `Review` vertex.

### OQ-32 — resolved by D-069

**How does a platform-run reviewer become genuinely zero-context, and what may
the standing files it necessarily contains contain?**

Answered on 2026-11-04 by
[D-069](../../pustaka/docs/product/decision-log.html#d-069), which is
reproduced above. In short:

- **The isolation boundary is the packet, not the workspace.** The reviewer
  does not need a different workspace; it needs a different packet.
- **The standing files are the rubric, not contamination**, so they stay
  whole — a reduced set would be drift under BR-39 — and the content question
  resolves at the sink: a finding records where, never what.
- **What is withheld is the author's session state**: the previous handoff,
  the failure lesson and the operator's answer, which a desk-scoped packet
  would otherwise hand to the reviewer by default.
- **The design has to travel**, because GAOS reads no folder: it is a
  GAOS-held artifact in a new `design` packet part, and the findings travel
  back as context.

The candidate set the question was framed with — a reviewer workspace holding
the rubric but not the Blueprint, a Blueprint variant rendered without the
standing files, and a per-turn instruction layer — is now moot. The first two
were inadmissible because a reduced standing-file set is drift, and the third
was never needed: the packet is where the contamination lived.

## The change list

Forty-three files, numbered 1 to 43 — two of the numbered ranges cover three
files each — and fifteen of those same files touched again when OQ-32 was
resolved, numbered 44 to 58. Every group names its file, and the counts are
derived rather than asserted:

```
 9 created   harness        .claude/rules/ (3), .claude/agents/ (5), .claude/skills/ (1)
 8 created   working docs   docs/README.md, docs/rules/ (1), docs/platform/ (1),
                            docs/designs/ (3), docs/articles/ (2)
 7 created   wiring         deploy/check-workspace.sh, GEMINI.md, AGENTS.md,
                            .agents/skills.json, .agents/rules/ (3 symlinks)
 1 created   docs page      pustaka/docs/architecture/design-gate.html
14 modified  code, agents, README, NOTICE, five docs pages, two ToC part
                            files, toc.js, changelog
 4 deleted   skills         rebuild-restart-prod/SKILL.md, emil-design-eng/ (3 files)

 9 + 8 + 7 + 1 + 14 + 4 = 43 files
```

The review reports in `docs/designs/reviews/` are numbered by the round and are
not counted here.

### Created — harness (9)

1. `.claude/rules/rule-index.md` — the index. No `paths:` (it is short, and it is
   what a reviewer starts from). Names the other two rule files and the domain
   sources, and says which answers which question.
2. `.claude/rules/qa-test-plan.md` — `paths: ["dashboard/**",
   "pustaka/docs/product/qa-test-plan.html"]`. The QA-register mechanics moved
   out of `.claude/CLAUDE.md`, with the count reconciled to the live register and
   the Go count to 243. The per-file enumeration that used to sit in the always-on layer is
   replaced by a range-to-area map derived from the register itself, because the
   enumeration was the thing that went stale.
3. `.claude/rules/agent-workspace.md` — `paths: [".claude/**", "docs/**"]`. The
   workspace layout, what may be edited, the design-gate trigger list, the
   vendored-skill policy, and the reviewer's deny-list.
4. `.claude/agents/gaos-goldfish-comprehension.md` — `model: opus`, `tools: Read,
   Grep, Glob`, `omitClaudeMd: true`. Frontmatter carries `name` and a
   `description` naming when to use it, like the two existing agents. Body: the
   paraphrase-first method, the four audit vectors, the three-label severity
   vocabulary, the output contract, and the `omitClaudeMd` version floor and
   `--agent` exception.
5. `.claude/agents/gaos-goldfish-zoom-out.md` — same frontmatter. Body: the
   simplicity method, the audit vectors, the reuse requirement with code
   search, the arithmetic check, and the output contract.
6. `.claude/agents/gaos-goldfish-critic.md` — same frontmatter. Body: the
   architecture/structure/style method, the stress-test requirement, and the
   output contract, including the requirement to verify a claim against the
   repository and cite `path:line` or record it as unverifiable.
7. `.claude/agents/gaos-goldfish-readiness.md` — same frontmatter. Body: the
   zero-decision rule, the file-boundary requirement, the per-change
   verifiability requirement, the identifier-consistency check, and the output
   contract.
8. `.claude/agents/gaos-goldfish-security.md` — same frontmatter. Body: the
   trust-boundary walk, the abuse-case enumeration, the deny-list, the
   path-and-line rule, the four-label severity vocabulary, and the `SEC-<n>`
   output contract.
9. `.claude/skills/gaos-design-review/SKILL.md` — the runbook: the seven steps
   of the flow, with the commands, including the canary planting, the positive
   control and the unconditional removal.

### Created — working docs (8)

10. `docs/README.md` — one paragraph: this is the working tree; the published
    site is `pustaka/docs/`.
11. `docs/rules/gaos-design-review.md` — **the source of truth for the
    procedure**: roles, trigger list, security criterion, triage vocabulary,
    output contract, limits.
12. `docs/platform/publish-note-grpc.md` — the canonical procedure for
    publishing an EN+ID note pair on mfardiansyah.id from this repository: the
    gRPC call shapes, the proposal-mode gate, Ardi's approval gate, the house
    style, and what to verify before and after publishing. Mirrored into each
    CLI's memory store.
13. `docs/designs/README.md` — the directory contract: stands alone, every claim
    carries file:line, the change list is complete, the arithmetic is written
    down, alternatives are real, nothing is left undecided, non-goals are
    listed; status moves Draft → In Review → Approved; reports are append-only
    and numbered.
14. `docs/designs/egm-design-review.md` — **this document**.
15. `docs/designs/reviews/README.md` — the reviews directory's own contract,
    and the placeholder that keeps the directory in git (git cannot track an
    empty directory).
16. `docs/articles/elephant-goldfish-model-in-gaos.md` — the EN source.
17. `docs/articles/elephant-goldfish-model-in-gaos-id.md` — the ID source, a real
    translation.

### Created — wiring and validation (7)

18. `deploy/check-workspace.sh` — the workspace validator. Its checks, output
    format, exit contract and enforcement point are specified in §"The
    validator".
19. `GEMINI.md` — the Antigravity entry point. **Pointers only, no rule text**:
    it names `CLAUDE.md`, `.claude/rules/rule-index.md` and
    `.agents/rules/`. A rule restated here would fork.
20. `AGENTS.md` — the Codex and other-CLI entry point, same pointer-only rule.
    GAOS's own agy worker already links `AGENTS.md`, `GEMINI.md` and
    `.agents/rules` to the same `CLAUDE.md` and `.claude/rules` the Blueprint
    writes (T-178, D-067); this change follows that convention rather than
    inventing one.
21. `.agents/skills.json` — `{"entries":[{"path":".claude/skills"}]}`, which
    registers GAOS's skills with Antigravity, which reads `.agents/` for its
    own rules and skills.
22-24. `.agents/rules/rule-index.md`, `.agents/rules/qa-test-plan.md`,
    `.agents/rules/agent-workspace.md` — **relative** symlinks to
    `../../.claude/rules/<name>.md`. Relative, not absolute, so the checkout
    stays portable; and symlinks, not copies, so a rule is one file read by two
    harnesses and cannot fork.

### Created — docs page (1)

25. `pustaka/docs/architecture/design-gate.html` — the gate, the flow, the
    roles, the cost, and **what is designed and what is not built**. Page id
    `arch-design-gate`, group Architecture, registered in
    `pustaka/docs/assets/toc/architecture.js`. It carries the `page-meta`
    component with `data-tags`, `data-hashtags`, `data-published`,
    `data-updated` and `data-version-from` / `data-version-to`, uses the closed
    component vocabulary, and is written by hand rather than through
    `pustaka new` so its section anchors match the ToC entry.

### Modified (14)

26. `.claude/CLAUDE.md` — **restructured** to the four items listed in §"The
    rule layer". The QA-register mechanics move to
    `.claude/rules/qa-test-plan.md`; the stale "three hundred and forty-two"
    quote goes with them.
27. `.claude/agents/gaos-frontend.md` — the `emil-design-eng` reference is
    replaced by a pointer to `review-animations` and `break-ui`; the sentence
    claiming the taste comes from three vendored Emil skills is corrected; the
    vendored-skill policy is noted.
28. `.claude/agents/gaos-deploy.md` — F2 fixed: the dev paragraph, the dev
    alternative in the usage line and the dev sentence at 42 are removed; the
    retirement notice stays.
29. `README.md` — F6 fixed: "179 tests" → 221 for the dashboard line, the 243
    total added, "315 cases" → 364, "Product version 0.29.0" → 0.34.0; `docs/`
    added to the layout block; one line about the design gate and one about
    `check-workspace.sh`.
30. `NOTICE` — the EGM attribution, quoted in §"NOTICE" below.
31. `pustaka/docs/product/decision-log.html` — **D-068** added as
    `<tr id="d-068">`; the lede becomes "Sixty-eight entries".
32. `pustaka/docs/product/open-questions.html` — **OQ-32** added to the
    register with `data-tags`, plus a `<h3 id="oq-32">` detail block; the lede
    becomes "Thirty-two decisions"; the register intro becomes "All thirty-two
    in one place".
33. `pustaka/docs/product/brd.html` — a new `<h3 id="rules-design-review">`
    §9.7 with BR-46, BR-47 and BR-48; the §9 lede becomes "Forty-eight rules";
    all six "design review" occurrences (`:423`, `:428`, `:537`, `:891`, `:911`,
    `:1250`) are amended to name the meeting they mean.
34. `pustaka/docs/delivery/implementation-plan.html` — **M17** as
    `<h2 id="m17-design-gate">`, four sub-modules with `h3` ids, register rows
    **T-184…T-188** in `<tbody id="ip-index">`, and one scoreboard row in the
    shape of the M16 row.
35. `pustaka/docs/product/qa-test-plan.html` — rows **TC-357…TC-364** in
    `<tbody id="tc-index">`; an `h3` for the new script under `#implemented`;
    an `<h3 id="ni-egm">` block under `#not-implemented`; and **every count
    string** listed in §"The counts that move".
36. `pustaka/docs/assets/toc/architecture.js` — the new page's entry, in the
    mandatory key order `id, file, title, desc, tags, sections`. The file is 102
    lines today and must stay under 200.
37. `pustaka/docs/assets/toc/product.js` — **ten** count strings updated: the
    eight in F9's table, plus the QA-plan pair at `:162` and `:168`, which are
    the first two rows of §"The counts that move".
38. `pustaka/docs/changelog.html` — a new `v0.34.0` section at the top of
    `.clog`, minor bump, dated 2026-11-04.
39. `pustaka/docs/assets/toc.js` — `product.semver` → `0.34.0`.

### Deleted (4)

40. `.claude/skills/rebuild-restart-prod/SKILL.md` — F1.
41-43. `.claude/skills/emil-design-eng/SKILL.md`, `LICENSE`, `SOURCE.md` — F4.

### Modified again — the OQ-32 resolution (15)

The resolution of OQ-32 landed after the sweep, in the same unreleased
version. It touches no Go source and creates no file; it amends files the
sweep already touched, plus five it did not.

44. `pustaka/docs/product/decision-log.html` — **D-069** added as
    `<tr id="d-069">`; D-068 marked superseded in part, with clauses 3, 5 and 6
    amended; the lede becomes "Sixty-nine entries".
45. `pustaka/docs/product/open-questions.html` — OQ-32's register row flips to
    *Answered → D-069*, its detail block becomes the answer, and the lede's
    answered/open counts become twenty-one and eleven.
46. `pustaka/docs/product/brd.html` — **BR-49** and **BR-50** added to §9.7,
    **BR-47** amended to name the parts it withholds, the §9 lede becomes
    "Fifty rules", and the questions count becomes "twenty-one answered".
47. `pustaka/docs/product/qa-test-plan.html` — **TC-365** added, **TC-361**
    sharpened, the `ni-egm` block rewritten against the decided mechanism, and
    the case count becomes "three hundred and sixty-five".
48. `pustaka/docs/delivery/implementation-plan.html` — M17's lede and four
    sub-modules rewritten; T-184 becomes the first build task; the five
    register rows rewritten; the scoreboard row's "next" cell changes.
49. `pustaka/docs/architecture/design-gate.html` — "What is still open" becomes
    "How a platform reviewer is isolated", with the two-column table of what
    travels and what is withheld, the honest-limit box, and the five rules.
50. `pustaka/docs/assets/toc/architecture.js` — the section entry becomes
    `the-isolation-mechanism`, and the `desc` and the rules text are amended.
51. `pustaka/docs/assets/toc/product.js` — **eight** count strings updated
    again; the QA pair, the rule pair, the decision pair and the answered pair.
52. `pustaka/docs/changelog.html` — the v0.34.0 platform-half bullet rewritten;
    no new release, because v0.34.0 is unreleased.
53. `README.md` — "364 cases" → "365 cases".
54. `.claude/rules/qa-test-plan.md` — the four QA count strings, the highest
    case id and the next free number, and the rule/decision count strings.
55. `deploy/check-workspace.sh` — the same five strings, as `prose_check`
    arguments, so the validator's own assertions move with the registers.
56. `docs/designs/egm-design-review.md` — **this document**: D-069's ten
    clauses, the resolved OQ-32 section, BR-49 and BR-50, the rewritten M17
    table, the second count table, and TC-365.
57. `docs/articles/elephant-goldfish-model-in-gaos.md` — an update section
    appended, so the published note does not say OQ-32 is open.
58. `docs/articles/elephant-goldfish-model-in-gaos-id.md` — the same update,
    translated.

```
43 distinct files
+ 15 of them touched a second time when OQ-32 was resolved
= 58 numbered entries in the change list, over 43 files
```

The 15 second-wave touches are all re-touches of files the sweep already
listed — no wave-2 file is new to the change, and no wave-1 file escaped it.
So "58 files" would be wrong; the honest pair is **43 files, 58 entries**.

The five `data-updated` attributes on the pages the resolution touches move
from `2026-11-02` to `2026-11-04`, because the pages changed on 2026-11-04 and
the attribute is the page's own "last updated" line.

## The counts that move

Every count string this change invalidates, with its new wording. The pages
write counts in words and `README.md` writes digits, and each replacement keeps
the form of the string it replaces.

| file | line | now | becomes |
|---|---|---|---|
| `pustaka/docs/product/qa-test-plan.html` | 19 | "three hundred and fifty-six" | "three hundred and sixty-four" |
| `pustaka/docs/product/qa-test-plan.html` | 79 | "All three hundred and fifty-six cases…" | "All three hundred and sixty-four cases…" |
| `pustaka/docs/assets/toc/product.js` | 162 | "three hundred and fifty-six" | "three hundred and sixty-four" |
| `pustaka/docs/assets/toc/product.js` | 168 | "three hundred and fifty-six" and "two hundred and forty-three" | "three hundred and sixty-four"; the Go count stays "two hundred and forty-three" |
| `pustaka/docs/assets/toc/product.js` | 36 | "43 business rules" | "48 business rules" |
| `pustaka/docs/assets/toc/product.js` | 56 | "BR-01 to BR-44" | "BR-01 to BR-48" |
| `pustaka/docs/assets/toc/product.js` | 66 | "the sixty-seven logged decisions" | "the sixty-eight logged decisions" |
| `pustaka/docs/assets/toc/product.js` | 78 | "the thirty-one-question register, twenty of them answered" | "the thirty-two-question register, twenty of them answered" |
| `pustaka/docs/assets/toc/product.js` | 110 | "Thirty decisions … nineteen now answered" | "Thirty-two decisions … twenty now answered" |
| `pustaka/docs/assets/toc/product.js` | 114 | "All thirty-one questions" | "All thirty-two questions" |
| `pustaka/docs/assets/toc/product.js` | 129 | "Sixty-three decisions" | "Sixty-eight decisions" |
| `pustaka/docs/assets/toc/product.js` | 170 | "one hundred and eighty-five dashboard … unit tests" | "two hundred and twenty-one dashboard … unit tests" |
| `pustaka/docs/product/decision-log.html` | 18 | "Sixty-three entries" | "Sixty-eight entries" |
| `pustaka/docs/product/open-questions.html` | 17 | "Thirty decisions" | "Thirty-two decisions" |
| `pustaka/docs/product/open-questions.html` | 74 | "All twenty-four in one place" | "All thirty-two in one place" |
| `pustaka/docs/product/brd.html` | 1213 | "Thirty-seven rules" | "Forty-eight rules" |
| `README.md` | 8 | "Product version 0.29.0" | "Product version 0.34.0" |
| `README.md` | 99 | "179 tests" | "243 tests" |
| `README.md` | 105 | "315 cases" | "364 cases" |

The older counts inside `pustaka/docs/changelog.html` are release records and
stay as written.

### The counts that move again — the OQ-32 resolution

The resolution of OQ-32 moves a second set, and they move with the first set:

| file | now | becomes |
|---|---|---|
| `pustaka/docs/product/qa-test-plan.html` | "three hundred and sixty-four" | "three hundred and sixty-five" |
| `pustaka/docs/assets/toc/product.js` | "three hundred and sixty-four" | "three hundred and sixty-five" |
| `pustaka/docs/assets/toc/product.js` | "48 business rules" | "50 business rules" |
| `pustaka/docs/assets/toc/product.js` | "BR-01 to BR-48" and "§9.6, BR-38 to BR-48" | "BR-01 to BR-50" and "§9.6, BR-38 to BR-45, and §9.7, BR-46 to BR-50" |
| `pustaka/docs/assets/toc/product.js` | "the sixty-eight logged decisions" | "the sixty-nine logged decisions" |
| `pustaka/docs/assets/toc/product.js` | "Sixty-eight decisions" | "Sixty-nine decisions" |
| `pustaka/docs/assets/toc/product.js` | "twenty of them answered" and "twenty now answered" | "twenty-one" in both |
| `pustaka/docs/product/decision-log.html` | "Sixty-eight entries" | "Sixty-nine entries" |
| `pustaka/docs/product/open-questions.html` | "Twenty are answered" and "Of the twelve still open" | "Twenty-one are answered" and "Of the eleven still open" |
| `pustaka/docs/product/brd.html` | "Forty-eight rules" | "Fifty rules" |
| `pustaka/docs/product/brd.html` | "thirty-two in all, twenty answered" | "thirty-two in all, twenty-one answered" |
| `README.md` | "364 cases" | "365 cases" |
| `.claude/rules/qa-test-plan.md` | "three hundred and sixty-four", "TC-364", "Sixty-eight entries", "Forty-eight rules" | "three hundred and sixty-five", "TC-365", "Sixty-nine entries", "Fifty rules" |
| `deploy/check-workspace.sh` | the same five strings, as `prose_check` arguments | the same five strings, moved |

**The answered count does move, this time.** OQ-32 was Open when the first wave
landed, so the register still held twenty answered questions out of thirty-two;
D-069 answers it, so the register holds **twenty-one**. A question that is open
does not become an answered one because a change mentions it — but a question
that is *answered* does stop being open, and that is the one case where the
count moves by one.

**The validator carries the same strings.** `deploy/check-workspace.sh` asserts
each count by grep, so a count that moves in the docs and not in the validator
turns the validator red. It is listed above because it is a fifth location for
four strings, not because it is a fifth count.

### The nine new QA cases

A **forward specification** is a register row whose Status is `Not
implemented`: an asserted value written before the code that produces it, so
that a later phase has a written target to pass. It is a fixed value, not a
wish. The sense is the QA register's own
(`.claude/rules/qa-test-plan.md`), and this document uses it in that sense
throughout.

Each row carries all five register fields, and each says whether it is
mechanism-independent (writable now) or mechanism-dependent (writable only
once the isolation mechanism is built, which is T-184).

| ID | Automation | Kind | Scenario | Asserted value | Status |
|---|---|---|---|---|---|
| TC-357 | `check-workspace.sh` | Positive | every path named in `.claude/CLAUDE.md` exists; every skill is referenced; every count string matches its register | `ok` on all six checks, exit 0 | Implemented |
| TC-358 | `check-workspace.sh` | Negative | a stale path or a drifted count is reported | `FAIL <what>` per defect, exit 1 | Implemented |
| TC-359 | M17 | Positive | the design artifact exists and is attached to the task | `Artifact.kind = "design"`, `Artifact.tenantId` = the product's | Not implemented |
| TC-360 | M17 | Positive | the implement task is not dispatched while the design is unreviewed | the implement task's status is `BLOCKED`, reason `design-unreviewed` | Not implemented |
| TC-361 | M17 | Negative | the reviewer's packet does not carry the brief, the goal, the handoff, the failure lesson or the operator's answer | the reviewer's packet kinds are exactly `design` and `rules` — and not `task`, `context`, `handoff` or `contract`, and specifically not `handoff-previous.md`, `failure-previous.md` or `answer.md` | Not implemented — **mechanism-dependent** |
| TC-362 | M17 | Positive | an open Blocker stops the dispatch and the task is queued for the operator | the review's verdict is `ITERATE` while a Blocker is open; the implement task's status is `BLOCKED`, reason `design-blocked` | Not implemented |
| TC-363 | M17 | Positive | a failed review does not wedge the product | the review task's status is `BLOCKED`, reason `review-failed`; the implement task stays `BLOCKED` | Not implemented |
| TC-364 | M17 | Positive | a cleared design opens exactly one implement dispatch, and the findings reach the implementer | exactly one implement dispatch; the implementer's next packet carries exactly one new kind, `findings` | Not implemented — **mechanism-dependent** |
| TC-365 | M17 | Positive | a review dispatch does not trip the drift gate | the reviewer's workspace holds every file the pinned Blueprint version wrote, its digest matches, and neither `RuleBlueprintDrift` nor `RuleObservedDrift` is raised | Not implemented — **mechanism-dependent** |

TC-365 is the workspace half of the ruling, and it is the one case that says
what the standing files *are*: because the reviewer's workspace is the
product's own and holds the published Blueprint whole, a review dispatch is not
a drifting dispatch. Without it, "the standing files stay whole" would be an
assertion the register never tests.

The two packet kinds the gate adds are `design` (the design artifact) and
`findings` (the dispositioned findings travelling back). The rubric needs no
new kind: it already travels as `rules`
(`dashboard/egress.go:43` — *".gaos/packet/rules.md — domain rules to score
against"*).

```
356 + 8 = 364 cases  →  "three hundred and sixty-four" in all four QA locations
364 + 1 = 365 cases  →  "three hundred and sixty-five" in all four QA locations
Go tests: unchanged at 243  →  "two hundred and forty-three"
```

**243** is 221 `func Test…` in `dashboard/` plus 22 in `pustaka/`, counted from
the files. It does not change: the new validator is a shell script, and the QA
rows it needs are rows in the register, not Go cases.

## The validator

`deploy/check-workspace.sh` is the one artefact that keeps the bloat and the
drift from coming back. It has to earn its trust, so it is specified.

**Scope.** The `.claude/` tree, the repository's own summary files, and the
docs-site count strings. It does **not** re-check what `pustaka check` already
checks — that command owns page registration, file existence and internal
links, and this script does not duplicate it. The division is: `pustaka check`
owns the docs site's structure; `check-workspace.sh` owns the agent workspace's
internal consistency and the counts.

**Checks.** Six, each one line:

| # | check | assertion |
|---|---|---|
| 1 | every path named in `.claude/CLAUDE.md` exists | `test -e` each |
| 2 | no orphan agent or skill | every file in `.claude/agents/` and `.claude/skills/` is named by another file in `.claude/` |
| 3 | the counts agree | the case count, the Go test count, the decision count, the question count and the rule count quoted in `.claude/CLAUDE.md` and `README.md` match the registers and the `func Test…` count |
| 4 | no rule is forked | no rule sentence appears in more than one file across `.claude/rules/`, `.agents/rules/`, `GEMINI.md` and `AGENTS.md` |
| 5 | no leftover canary | the string `EGM-MARKER-` appears nowhere in the tree |
| 6 | no secret in the report store | `docs/designs/reviews/` is scanned with the detector set in `dashboard/egress.go:332-335`. **A missing report store is a pass, not a failure** — there is nothing to scan, and the pre-sweep tree has no reports directory |

Checks 5 and 6 both skip a path that does not exist rather than failing on
it: a missing standing file is checked by check 1, and a missing report store
has nothing to scan.

**Output.** In the house style of `deploy/check-stacks.sh`: `ok <what>` for a
passing check, `FAIL <what>` for a failing one, a `fails` counter, and exit 1
if any check failed.

**Output hygiene.** For any path matching `*.env`, `*.aim-*`, `*.pem` or
`*.key`, the script prints the path and the rule and **never the matched
line**. It runs on the host, where `deploy/rebuild-restart.sh:148-160`
symlinks the secret files into worktrees, so the tree it walks contains them.

**Enforcement point.** Run by hand, by the product owner, like every other
check — `D-025` makes the product owner the runner for every acceptance test
and this repository has no CI. It is listed in the QA plan's cadence section
next to `check-stacks.sh`, so it is a thing the owner knows to run. It is
**advisory**, and that is stated rather than hidden.

## Cost, and how the gate is measured

**Cost.** Five lenses × one Opus session each, per design. The harness has **no
budget cap**: BR-17's caps govern platform tasks in a product workspace, and
the harness runs against GAOS's own repository, entirely outside the platform's
metering. The cost of one review round is therefore the operator's own model
bill, and it is not capped by anything in this repository. The first measured
round is the one this change records; the number goes in the article.

**Latency.** One review round is five parallel sessions. Under Layer 1 they are
parallel, so the wall-clock cost is one round of a single Opus session plus the
triage. Under Layer 2 the review is one task on one product's single live turn,
so it queues behind everything else in that product.

**Success metric.** Two numbers, measured from the append-only reports in
`docs/designs/reviews/`:

- **Blockers caught per design.** Target: at least one per three designs. A
  gate that finds nothing is a gate that is not looking, and the five lenses
  exist because this repository has real defects to find (F1-F10).
- **Findings needing triage rather than action.** The upstream EGM project's
  author, Dave Rensin, estimates that about 30% of Goldfish suggestions are
  worth acting on
  (<https://medium.com/@drensin/elephants-goldfish-and-the-new-golden-age-of-software-engineering-c33641a48874>).
  This document uses that as the **baseline**, not as a measurement: the real
  ratio is computed from the disposition column of the reports, and the first
  number this change records is the measured one.

## The NOTICE

`NOTICE` gains one paragraph in its third-party section:

```
  * The Elephant-Goldfish Model ......... Apache License 2.0
    https://github.com/google/elephant-goldfish-model
    The design-gate method, its reviewer structure and its document
    standards. This is not a supported Google product.
```

## Alternatives considered

### Alternative A — install the EGM plugin and use it as it ships

`claude plugin marketplace add google/elephant-goldfish-model`. It brings
seven skills and nine personas, and it works.

**Rejected, with a caveat.** EGM's review skill is built for a
caller-supplied standard: it passes persona and standard paths to each
subagent, so GAOS's `.claude/rules/` **could** be supplied as the standard
path, and its `egm-review-loop` (concurrent dispatch), `egm-resolve` (triage
and patch application) and `egm-drift-report` (spec-versus-code divergence)
are exactly the machinery this change re-authors. That option was not
evaluated in the first draft and should have been.

It is still rejected, for two reasons that survive the caveat. First, the
plugin's personas are its own: its rubric is EGM's document standards, not
GAOS's rules — so a GAOS design would be reviewed against "the Alternatives
Rule" and "the Zero-Decision Rule" and never against the fact that GAOS's
event catalog is closed, that its Blueprint loads on every turn, or that its
back-office login checks nothing. The rubric is the point. Second, the plugin
assumes the reviewer's context is empty, and GAOS's context is not empty — the
Blueprint loads on every turn, including a fresh session. A plugin that
assumes an isolation GAOS does not have would give a false guarantee, which
is worse than no guarantee.

What is adopted from it: the paraphrase-first rule, the
`tools: [Read, Grep, Glob]` + `omitClaudeMd: true` reviewer shape, the five
review stages, the report format, and the triage discipline — in GAOS's shape,
with GAOS's rubric.

### Alternative B — a new task status for the gate

Give the implement task a `NEEDS_DESIGN` status, widen the `Task.status` DDL
regexp, add a board column, add a CSS selector, and let the scheduler skip it.

**Rejected.** It touches a closed vocabulary: `Task.status` is constrained by a
DDL regexp (`dashboard/schema/04-execution.sql:27-28`) *and* by
`TestTheTaskStatusVocabularyMatchesTheDDL`. It also needs a board column, a
`data-status` CSS rule, and a scheduler change — four surfaces for a gate the
planner's existing `DEPENDS_ON` edge already expresses. M3.2's mechanism is the
dependency graph; a gate is a dependency. The status approach also leaks a
*design* concept into the *execution* vocabulary, which is where BR-38 and the
lease live.

### Alternative C — one reviewer instead of five

A single general reviewer with a good rubric.

**Rejected, and the reasoning is now stated honestly.** The first draft
rejected it with "one lens finds one class of defect" — an assertion, not an
argument — and an anecdote about a different repository with a different stack.
The real reasons to keep five are operational: each lens is a separate
zero-context session, so one reviewer running five sections is one session and
one author's assumptions applied five times; and a finding is triaged against
the lens that raised it, so a lens that is not asked a question cannot raise a
finding in it. The cost is real and is measured (§"Cost").

This alternative stays open. If the measured cost of a five-lens round proves
too high for the rate of feature-sized changes, two lenses — zoom-out and
critic — are the ones to keep, and the rubric is written so that dropping a
lens does not change what the others ask.

### Alternative D — delete the rules and let the design document carry them

Drop `.claude/CLAUDE.md` entirely and require every change to be driven by a
design document.

**Rejected.** It makes every change, including the ones the trigger list
excludes, pay for a document. The rules that must be in every session's head —
the QA plan is the same change, the event catalog is closed, the component
vocabulary is closed — would become something an agent has to remember to go
and read. The always-on layer is small on purpose; this change makes it
smaller, and smaller still.

### Alternative E — keep `emil-design-eng` and drop `review-animations` instead

Both cover motion. Dropping the other one would save the same bytes.

**Rejected.** `review-animations` is stack-agnostic (2 React-family matches
over 18,455 bytes) and its `STANDARDS.md` is the review bar an agent is held
to. `emil-design-eng` scores 7 over the same vocabulary for a stack GAOS does
not have. Keeping the one that matches the stack and dropping the one that does
not is the same decision as F4, argued the other way round.

### Alternative F — build the platform gate now, in Go

Write the task kind, the review runner, the gate and the fold case in this
change.

**Rejected, and this one hurts.** It is the interesting work. It is rejected
because BR-05 closes the event catalog, because M17's own shape depended on
OQ-32 (now answered by D-069), and because the `Review` record's encoding was
not settled when the decision was taken (clause 5 of D-068 hands the schema
question to M17.1; D-069 later settled it the cheaper way, by making the
gate's record an `Artifact` instead). Building the runner before those three
are settled would build the easy half and freeze the hard half into code.

GAOS's own precedent is on the other side of this, and it says something
narrower than the first draft claimed. M14.0 landed the decisions, the rules,
the questions, the pages and thirty-six forward specifications *"before any
code, so every later phase has a written target to pass"* — but M14.0 was the
**first change of that module**, and M14.1 through M14.5 are code: upstream
provisioning, the ai-manager client and adapter, submitting and polling a turn,
storing the prompt and the reply. The precedent establishes **sequencing** —
docs first, then code — not a doctrine of shipping no code. The reason this
change ships no Go code is BR-05 and the fact that M17's shape depended on
OQ-32, not the precedent.

## Risks

| Risk | Handling |
|---|---|
| The isolation probe proves the instruction, not the tooling | A positive control is required, so the probe can fail. The claim is stated precisely: "the canary did not reach a report", not "the file was not read". The runtime key's version floor and `--agent` exception are recorded in the agent files. |
| A reviewer reads the repository, and the author's claims are wrong | A reviewer **can** and **should** check the repository — that is the value of a zero-context reviewer. The limit is intent, not reach: it has artifacts and no authorial intent. |
| A reviewer quotes a credential into a committed report | The deny-list, the path-and-line rule and the validator's secret scan over `docs/designs/reviews/` (check 6). |
| The sweep deletes something useful | `emil-design-eng` is contestable. The contest must be checkable — GAOS's dashboard has no React and no npm, and `review-animations/STANDARDS.md` covers the motion bar — and if the contest fails, the file is restored. Either outcome is recorded. The `/rebuild-restart-prod` slash command is a deliberate removal, recorded here rather than slipped. |
| The always-on layer shrinks and an invariant is lost | Every invariant that leaves `.claude/CLAUDE.md` moves to a rule file and is listed in `rule-index.md`. Nothing is deleted without a destination. |
| BR-48's clearance has no identity to check | The rule text names the precondition and refuses the clearance until the back office has a real identity. It is a blocking precondition on M17, not an assumption. |
| The platform gate is specified and never built | M17's forward specifications are the QA register's own mechanism for exactly this: a written target that no one has to guess. The register says what is owed, and T-184 is the first build task. |
| The Blueprint's decision injection keeps growing | F8. Recorded, not fixed, and deliberately left open by D-069 rather than closed with the isolation mechanism. It is a product decision, not a mechanical one. |
| The gate's cost is uncapped in the harness | Stated, not hidden: the harness has no budget cap and the owner pays the bill. The first measured round is recorded. |
| The article publishes something wrong | Drafts first, preview second, Ardi's OK third. The commentary is rewritten at temperature 0.8, so the posted text is read back and the facts are checked. |

### The article's publication rules

Publishing a note from this repository is an operation on a public site, so four
rules travel with it:

1. **The credential is a host-side file** outside the repository, read into a
   shell variable and never passed as an argument, printed, committed, or put
   into a model prompt. A failed publish prints the error, not the token.
2. **Proposal mode by default.** The publish runs in proposal mode and posts
   only after the operator confirms the exact diff.
3. **The rendered text is scanned** with the detector set in
   `dashboard/egress.go:332-335` before posting.
4. **The text may not name** host paths, tunnel addresses, tenant or product
   identifiers, the internal hostnames, or the unauthenticated back office,
   unless the operator explicitly approves that disclosure.

## Verification plan

**The commands**, run from the repository root before the change is called done:

```bash
cd pustaka   && go run . check ./docs     # structure + registry consistency
cd dashboard && go test ./...             # 221 tests
cd pustaka   && go test ./...             # 22 tests
./deploy/check-stacks.sh                  # the stack invariants
./deploy/check-workspace.sh               # the new workspace validator
```

**The validator must earn its trust.** Run it against the **pre-sweep** tree at
commit `HEAD` — the tree as committed, before any of this change's edits — where
it must fail and name each defect: the orphan skill, the unreferenced
`emil-design-eng`, the stale counts in `README.md` and in the three ledes, and
the `0.29.0` version. Record the count. Then run it against the swept tree,
where it must pass. A validator that has never failed is not a validator.

**The isolation probe must pass.** Plant a canary in each standing file, run the
five reviewers, and `grep -rn "EGM-MARKER-" docs/designs/reviews/` must return
nothing; the positive control must produce a report that **does** contain a
canary; and the canaries must be removed, with `git diff .claude/CLAUDE.md`
showing only the intended restructure.

**The counts must agree.** `grep -rn "three hundred and" pustaka/docs/product/qa-test-plan.html
pustaka/docs/assets/toc/product.js` must find exactly the four QA locations, all
saying "three hundred and sixty-five", with the Go count saying "two hundred and
forty-three". The same grep over `pustaka/docs/product/decision-log.html`,
`pustaka/docs/product/open-questions.html` and `pustaka/docs/product/brd.html`
must find "Sixty-nine", "Thirty-two" and "Fifty" and no stale form.

**The React metric must be reproducible.** `grep -ioE "react|jsx|tailwind|sonner|shadcn" .claude/skills/emil-design-eng/ | wc -l`
is 7 before the deletion and the validator refuses a skill directory that
reintroduces a framework vocabulary without a note saying why.

**The design doc must stand alone.** A reader with no knowledge of this
conversation can judge it: every claim carries a file and a line, the counts
follow from the change list, and nothing is left undecided.

## Execution order

Each phase leaves the tree buildable and testable, and each has its own exit
command.

1. **Rules and policy.** Create `.claude/rules/` (three files, with `paths:`),
   `docs/rules/gaos-design-review.md`, `docs/README.md`,
   `docs/designs/README.md`. *Exit:* the files exist and the three rule files
   carry the frontmatter above.
2. **Agents and skill.** Create the five reviewer agents and
   `.claude/skills/gaos-design-review/SKILL.md`. *Exit:* each agent parses and
   carries `model`, `tools`, `omitClaudeMd`, `name` and `description`.
3. **Validator.** Create `deploy/check-workspace.sh` and run it against
   `HEAD` in a clean worktree — it must fail and name its defects. *Exit:*
   recorded failure count.
4. **Sweep.** Restructure `.claude/CLAUDE.md`; delete the orphan and the
   React-flavoured skill; fix `gaos-deploy.md`; fix the README's three
   numbers. *Exit:* `check-workspace.sh` passes.
5. **Platform registers.** D-068, OQ-32, BR-46…48 with the six "design
   review" amendments, M17 with T-184…T-188, the eight QA rows, and every
   count string. *Exit:* `pustaka check ./docs` passes and the count greps are
   clean.
5b. **Resolve OQ-32.** D-069, BR-49 and BR-50, BR-47 amended, M17 rewritten so
   T-184 builds rather than decides, TC-361 sharpened and TC-365 added, the
   second count set moved, and the design-gate page's "what is still open"
   replaced by the mechanism. *Exit:* `pustaka check ./docs` passes,
   `check-workspace.sh` is green on the moved counts, and the published note
   no longer says OQ-32 is open.
6. **Published page and release.** The new architecture page, its ToC entry,
   the changelog entry and `product.semver` — one change, as the page itself
   says. *Exit:* `pustaka check ./docs` passes and `toc.js` says `0.34.0`.
7. **Cross-harness wiring.** `GEMINI.md`, `AGENTS.md`, `.agents/skills.json`
   and the three relative symlinks. *Exit:* every symlink resolves and no rule
   sentence appears in two files.
8. **Review round two.** Re-run the five lenses against this document as
   revised, with the canaries planted. *Exit:* no Blocker survives.
9. **Article.** Write the EN and ID sources from the real findings, with the
   four publication rules applied. Create both drafts, preview, show Ardi,
   publish on his OK, verify.

---

*Drafted with EGM — the Elephant-Goldfish Model, google/elephant-goldfish-model,
Apache-2.0. Revised after the first Goldfish round.*
