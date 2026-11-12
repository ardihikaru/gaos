# M17.5a — the frontend design rule: `/emil-design-eng`, trimmed for this stack

**Status:** Approved (revision 3)
**Date:** 2026-11-04
**Change:** a policy change to the agent workspace (M17.5a)
**Rules:** the trigger list in `docs/rules/gaos-design-review.md:38-53`; the vendored-skill
rule in `.claude/rules/agent-workspace.md:38-47`
**QA cases:** TC-380 and TC-381, in the `#cw-workspace` family of
`pustaka/docs/product/qa-test-plan.html:2623`
**Round 1 reports:** `docs/designs/reviews/frontend-design-rule-{comprehension,zoomout,critic,readiness,security-review}-1.md`;
the triage is `docs/designs/reviews/frontend-design-rule-consolidated-1.md`

---

## What revision 2 changed

Four lenses ran against revision 1 and returned **2 blockers, 43 majors and 24 nits** between
them. Two of the findings changed the design rather than its wording, and both are recorded
here because they are the design's real content:

1. **The trim was too big.** The zoom-out lens showed that 274 of the 325 lines revision 1
   proposed to keep are the craft mechanics this repository **already holds** in
   `.claude/agents/gaos-frontend.md:31-83` — the same rules, twice, with two authorities to drift
   apart. The genuinely missing layer is the philosophy (upstream 18-36) and the review format
   (upstream 38-60): 42 lines. **Revision 2 keeps those and nothing else.**
2. **The trim boundary left a section undecided.** The comprehension and readiness lenses both
   found it: `## Review Checklist` (upstream 660-676) was in no column of the keep/drop/rewrite
   table, and it names a component library and a second motion manual. Under revision 2's
   boundary **every line of the upstream file is assigned**, and the result needs no rewrite at
   all.

The rest of revision 2's changes are the smaller ones the lenses found, and they are in the
consolidated triage. The ones that moved a number: the upstream file's framework-vocabulary score
is **11 on 10 lines**, not 7 (three lenses checked it); TC-380 needs a **negative** case beside
it, so the case count moves **379 → 381**, not 380; the new rule needs its **cross-harness
symlink**; and the validator's **own two strings** (`deploy/check-workspace.sh:141-142`) had to
move or the house check would fail.

## What revision 3 changed

The **security lens** ran against revision 2 and returned **1 High, 3 Medium and 3 Low**. All
seven are applied here, and three of them changed the design's substance:

1. **The trimmed file had no integrity control (SEC-1, High).** The design grants an exception to
   the "do not edit vendored skills in place" rule, and the only machine check on that file was a
   five-word blacklist. A future re-vendor could change what every frontend session is told and
   nothing in the tree would notice. **Revision 3 pins the file: its SHA-256 is recorded in
   `SOURCE.md` and the validator verifies it** (D6, D8).
2. **The scan's exclusion was the wrong shape (SEC-3, Medium).** Revision 2 excluded `SOURCE.md`
   and `LICENSE` from the vocabulary scan *by name*, which left the one file the skill tells the
   model to read unscanned. **Revision 3 excludes nothing**: the cut record is reworded so it
   names the removed sections without naming a framework, and the scan covers every file in the
   directory as it does today (D8, D9).
3. **The rule's scope missed the markup-producing Go files (SEC-2, Medium).** The rule reached
   templates and static assets, but not `dashboard/view.go` or `dashboard/seq.go`, which build the
   `template.HTML` the back office serves unescaped. **Revision 3 scopes the rule to the
   *rendered surface*** — templates, static, `render.go`, `view.go`, `seq.go` — and says in the
   rule that a `template.HTML`/`template.CSS` value must be escaped where it is built (D4).

The other four: the vendored skills' **licence and provenance are now asserted** by the
validator, the way the house already asserts them for every other vendored third-party asset
(SEC-4, D8); the new rule file joins the runbook's **planting list**, so a future isolation probe
exercises it (SEC-5, the change list); **three figures did not reproduce** and are corrected
(SEC-6, D1 and the counts); and the agent file's "never edit the vendored skill files" now
**names the exception** instead of contradicting the rule (SEC-7, D7).


---

## What this document is

The operator asked for one rule: **frontend work always loads `/emil-design-eng`**.

The skill that name refers to is not in this repository's working tree. It was deleted on
2026-11-04 as part of the Elephant-Goldfish Model (EGM) sweep — 27,509 bytes of React, Tailwind
and Sonner guidance for a dashboard that is server-rendered Go `html/template` plus HTMX and
plain JS (`.claude/agents/gaos-frontend.md:24-28`). The deletion is uncommitted: the three files
are still tracked at `HEAD` (`git ls-files .claude/skills/emil-design-eng/` lists `LICENSE`,
`SKILL.md` and `SOURCE.md`), and the working tree has them removed.
`deploy/check-workspace.sh:229-231` still asserts that the directory stays gone.

Three facts make the request unsatisfiable as it stands:

1. **`/emil-design-eng` does not resolve.** The skill is in neither `.claude/skills/` nor any
   global skill directory on this host, and it is absent from this runtime's skill catalogue.
   A rule naming it would name something that does not exist.
2. **The rule layer does not mention it.** `.claude/rules/agent-workspace.md:42-45` names the
   vendored skills as `break-ui` and `review-animations`, and `.claude/rules/rule-index.md:10-14`
   has no row for frontend work at all. There is no rule to amend; the rule has to be written.
3. **The reason for the deletion was correct.** The upstream skill's examples are a React
   component library's, and this dashboard has none. Restoring it unchanged would put 27 KB of
   guidance in front of every frontend agent, most of it about code that cannot exist here.

**This document specifies the change:** re-vendor the skill as a **trimmed, stack-agnostic
edition**, write the rule that mandates it, and repair the three places that would otherwise
contradict the skill's return — the validator, the frontend agent, and the workspace rule.

The trim is recorded, not silent: `SOURCE.md` names the upstream URL, the commit, the licence and
exactly what was cut, and `NOTICE` carries the attribution with the note that GAOS modified it.

**What "the house" means in this document:** this repository's own established convention — its
wording, its rules and its checks. Where this document says "the house style" it means the
pattern the neighbouring files already use.

---

## The problem

The dashboard's frontend work — `dashboard/templates/*.html`, `dashboard/static/*.js`,
`dashboard/static/app.css` — is done by the `gaos-frontend` agent
(`.claude/agents/gaos-frontend.md:9-13`), and by whatever agent the operator happens to be talking
to. Two skills stand behind it today:

| skill | what it is | where it is bound |
|---|---|---|
| `review-animations` | the motion bar — "review animation and motion code against a high craft bar" (`.claude/skills/review-animations/SKILL.md:17`) | `.claude/agents/gaos-frontend.md:18-20` |
| `break-ui` | the adversarial edge-case pass — long text, 0 or many items, non-Latin text, emoji | `.claude/agents/gaos-frontend.md:21-23` |

What is missing is the third one: the *taste* layer. The philosophy of why a screen feels
considered rather than assembled — the layer the deleted skill carried. The frontend agent's own
rules (`gaos-frontend.md:31-83`) are the stack's mechanics: the CSS variables, the durations, the
easing curves, the modal behaviour, the focus ring. They are not a philosophy, and the agent
file says as much: "Both are stack-agnostic, which is what this dashboard needs" (`:24`) — *both*,
because the third was gone.

Meanwhile `break-ui`'s own frontmatter still points at the missing skill: "For visual design
critique use emil-design-eng" (`.claude/skills/break-ui/SKILL.md:3`). That pointer has been
dangling since the sweep. So does `break-ui/SKILL.md:16` ("critique its taste (that's
`emil-design-eng`)").

**The operator's rule is therefore also a repair:** the dangling pointers become true again, and
the taste layer comes back with a stack it fits.

**And it answers a contest the EGM design left open.** `docs/designs/egm-design-review.md:1408`
recorded the sweep's own risk: "`emil-design-eng` is contestable … `review-animations/STANDARDS.md`
covers the motion bar — and if the contest fails, the file is restored. Either outcome is
recorded." This document is that outcome: the operator contested it, and the file comes back
trimmed. The outcome is recorded here and in the v0.35.1 changelog entry.

---

## Goals

- `/emil-design-eng` **resolves**: the skill is in the tree, and its name is unchanged, so the
  existing pointers at `.claude/skills/break-ui/SKILL.md:3` and `:16` come true.
- **Every frontend change loads it.** A rule, not an agent file: it applies whether or not the
  `gaos-frontend` agent is invoked.
- The skill's guidance is **true for this dashboard**: plain CSS, plain DOM, one hand-written
  stylesheet, no component library.
- **One authority per rule.** The skill carries what this repository does not have; the
  repository's own rules carry what it does. Nothing is stated twice.
- The trim is **recorded and checkable**: what was cut is written down, and a validator check
  proves both that the result carries no framework vocabulary **and** that the kept half is
  still there.
- The repo's own **licence discipline** holds: the upstream attribution, the upstream commit and
  the MIT notice travel with the trimmed copy.

## Non-goals

- **No change to the dashboard.** No template, no CSS, no JS. This change adds a rule and a
  skill; it restyles nothing.
- **No change to `review-animations` or `break-ui`.** They stay as they are; `review-animations`
  remains the motion bar.
- **No new motion guidance.** The skill delegates motion to `review-animations/STANDARDS.md`
  rather than carrying a second manual.
- **No new craft guidance.** The skill delegates the craft to `.claude/agents/gaos-frontend.md`.
- **No change to the design-review process.** The five lenses, the canary probe and the positive
  control are unchanged; this change is itself reviewed under them.
- **No Go code.** Not one line of `dashboard/` changes, so the Go test count does not move.
- **`rebuild-restart-prod`'s deletion is out of scope.** The working tree also has that skill
  deleted and uncommitted; it is a different sweep item and this change does not touch it.

---

## The decisions

### D1 — Re-vendor, trimmed to the philosophy. Not re-vendor, whole.

The operator chose a trimmed edition. The alternative — restoring the upstream file unchanged —
fails the validator's own guard: `deploy/check-workspace.sh:213-228` scans every skill directory
that is not on the `stack_skills` baseline (`break-ui`, `review-animations`) and **fails** the
build if it carries any match of `react|jsx|tailwind|sonner|shadcn`. The upstream file scores
**11** under that grep, on **10** lines — 162, 336, 338, 350, 499, 542, 559, 561, 567, 577
(`grep -rioE "react|jsx|tailwind|sonner|shadcn" .claude/skills/emil-design-eng/ | wc -l` at
`HEAD`). It also scores 11 in the directory scan the validator already runs, which is how the
sweep justified the deletion.

So the trim is not cosmetic: it is what makes the restoration legal under the repository's own
rules.

### D2 — The trim boundary: keep the philosophy, drop everything this repository already holds

The upstream file is 676 lines. The boundary is stated as a rule, and **every line is assigned**:

| verb | upstream lines | what |
|---|---|---|
| **keep** | 18-36 | `## Core Philosophy` — taste is trained, unseen details compound, beauty is leverage. The layer this repository has no other home for |
| **keep** | 38-60 | `## Review Format (Required)` — the `Before`/`After`/`Why` table. The format `gaos-frontend.md:113` already reports in |
| **drop** | 6-17 | the title's neighbours: `## Initial Response` (8-16) — the upstream's chat ritual, which tells the model to answer with one canned sentence and stop. This repository loads a skill as part of doing work, not as a chat |
| **drop** | 62-196 | `## The Animation Decision Framework` (62-146) and `## Spring Animations` (147-196) — motion, delegated to `review-animations/STANDARDS.md`, the house motion bar |
| **drop** | 197-391 | `## Component Building Principles` (197-344) and `## CSS Transform Mastery` (346-391) — craft mechanics this repository already holds at `gaos-frontend.md:31-83` |
| **drop** | 393-476 | `## clip-path for Animation` (393-438) and `## Gesture and Drag Interactions` (439-476) — motion again |
| **drop** | 477-525 | `## Performance Rules` — craft mechanics, held at `gaos-frontend.md:74-81` |
| **drop** | 527-557 | `## Accessibility` — the same rules, held at `gaos-frontend.md:79-81` |
| **drop** | 559-658 | `## The Sonner Principles` (559-603), `## Stagger Animations` (605-637) and `## Debugging Animations` (639-658) — a component library this dashboard does not have, and motion |
| **drop** | 660-676 | `## Review Checklist` — a second copy of the craft rules and of the motion manual |

**Why the craft sections go.** Their rules are already this repository's rules, stated in
`gaos-frontend.md`: "Only animate `transform` and `opacity`" (`gaos-frontend.md:74-75`),
"Never use `transition: all`" (`:73`), "Never `scale(0)`" (`:64`), "Durations … under 300 ms"
(`:66-71`), "Reduced motion" (`:79-81`), and the hover media query (`:76`). Two authorities for
one rule is how they drift: the agent file says 200 ms for a modal, the upstream says something
else, and an agent cannot tell which one the dashboard wants. **One authority per rule** is the
point, and it is why the kept half is the half this repository does not have.

**Why the motion sections go.** `review-animations/STANDARDS.md` is the bar
(`gaos-frontend.md:28`), and the upstream motion family is where the framework examples are
densest (the `jsx` fence at upstream 499, the `Framer Motion` prose at 160, the `npm` reference
at 561).

**The result needs no rewrite at all.** Every kept line is stack-agnostic and names no framework,
so there is no hand-edited MIT prose to defend, and the acceptance condition is true by
construction rather than by inspection: the trimmed file scores **0** on the validator's
vocabulary grep.

**The new file is 70 lines**: the frontmatter (rewritten, D3), the title, a new `## GAOS edition`
section, `## Core Philosophy` and `## Review Format (Required)` — the last two verbatim, and the
three `##` headings are what the validator's heading-count check reads.

*Rejected:* keeping the craft sections, on the grounds that they are richer than the agent
file's summary. They are — and that is the drift. *Rejected:* rewriting the framework examples
into plain CSS. That is hand-editing vendored MIT prose to reproduce rules the repository
already states.

### D3 — The skill keeps its name, and its catalogue line tells the truth

The name stays `emil-design-eng`, because the operator's request, the `break-ui` pointers and the
agent file all refer to it by name. The frontmatter `description` is rewritten, not appended to:
the upstream's reads "…UI polish, component design, **animation decisions**, and the invisible
details…" (`HEAD`, line 3), and two of those three things the trim removes. The new description
names what the file now holds.

**The frontmatter, verbatim:**

```yaml
---
name: emil-design-eng
description: Emil Kowalski's philosophy on UI polish and the invisible details that make software feel great, and the review format for saying what is wrong with an interface. GAOS edition — the stack-agnostic half only; this dashboard's own craft mechanics live in .claude/agents/gaos-frontend.md, motion in review-animations, edge cases in break-ui.
---
```

**The `## GAOS edition` section, verbatim** — it sits between the title and the philosophy, and
it is what tells an agent that the skill's absence of craft is deliberate:

```markdown
## GAOS edition

This is Emil Kowalski's skill, vendored under its MIT licence and trimmed to the
half that fits this repository: the philosophy, and the review format. What was
cut, and why, is in `SOURCE.md` beside this file.

Three things this repository already holds, and this skill does not:

- **The dashboard's craft mechanics** — the CSS custom properties, the durations,
  the easing curves, the modal behaviour, the focus ring — live in
  `.claude/agents/gaos-frontend.md`. Read them before you write CSS or a
  template, and where the two disagree, the dashboard's own rules win.
- **Motion** — the bar is `review-animations/STANDARDS.md`. Load
  `review-animations` for anything that moves, before and after.
- **Edge cases** — the adversarial pass is `break-ui`. Run it before you call a
  list, form or card done.

The stack is fixed: server-rendered Go `html/template`, HTMX, plain JS, and one
hand-written `app.css`. No component library, no bundler, no npm.
```

### D4 — The rule is a rule, and it is path-scoped

**New file `.claude/rules/frontend.md`**, with the same frontmatter shape the other rules use
(`.claude/rules/agent-workspace.md:1-5`):

```yaml
---
paths:
  - "dashboard/templates/**"
  - "dashboard/static/**"
  - "dashboard/render.go"
  - "dashboard/view.go"
  - "dashboard/seq.go"
---
```

**The scope is the *rendered surface*, and it is written down rather than implied.** The rule
reaches the templates, the static assets and the three Go files that build what the browser
renders: `dashboard/render.go` (the renderer and embedder), `dashboard/view.go` (the view
fields) and `dashboard/seq.go` (the SVG builder). The security lens found the gap that made this
matter: `dashboard/seq.go:33-104` and `dashboard/view.go:127-131` return `template.HTML`, and
`dashboard/templates/backoffice.html:309` prints it unescaped — so the escaping is *earned* by
an explicit `template.HTMLEscapeString` at construction, in a file the rule did not reach. A
change that drops one escape call is frontend work, and now it loads the rule.

It does **not** reach `dashboard/handlers_bo.go` or any other Go handler: the agent file
subordinates Go to the backend (`gaos-frontend.md:14-16`), and the rule says the same in its own
words — while naming the back office's view-model assembly (`buildBO`) as the frontend's, so the
boundary is about *what is rendered*, not about which language it is written in.

**One sentence of the rule is a security rule**, and it is there because of the same finding: a
`template.HTML` or `template.CSS` value must be escaped where it is built, because the rule does
not reach the Go handlers that print it.

**The rule says, in the house voice:**

1. **Load `/emil-design-eng` first** for any frontend work — a new screen, a component, a
   polish pass, a visual review. It is the taste layer: the philosophy and the review format.
2. **`/review-animations`** for anything that moves, before and after.
3. **`/break-ui`** before calling a list, form or card done.
4. **The stack is fixed** — `html/template`, HTMX, plain JS, one hand-written `app.css` — and the
   dashboard's own craft rules live in `.claude/agents/gaos-frontend.md`.
5. The repo's own rules still apply (`.claude/CLAUDE.md`), and a changed test means the QA
   register changes in the same change.

`.claude/rules/rule-index.md:10-14` gains the row that names it, so a session that touches
`dashboard/templates/`, `dashboard/static/` or `dashboard/render.go` is pointed at the rule
before it starts. **The row, verbatim:**

```markdown
| **Working on the dashboard's frontend** — `dashboard/templates/`, `dashboard/static/`, `dashboard/render.go` | `.claude/rules/frontend.md` — the three skills to load and the stack they fit |
```

### D5 — The cross-harness symlink, and the rule reaches two harnesses

`.agents/rules/` holds a relative symlink for **every** rule file — `agent-workspace.md`,
`qa-test-plan.md`, `rule-index.md` — because `.claude/rules/agent-workspace.md:112-125` states
"`.agents/rules/*.md` are relative symlinks into `.claude/rules/`, so a rule is one file read by
two harnesses". `AGENTS.md:38` and `GEMINI.md:37` say the same. Nothing generates them.

**A new rule without its symlink reaches one harness.** So the change creates
`.agents/rules/frontend.md` as a relative symlink to `../../.claude/rules/frontend.md`, and the
change list carries it.

`.agents/skills.json` needs no change: it registers the `.claude/skills` directory, so the
restored skill is picked up automatically.

### D6 — The vendored-skill rule gains a recorded-trim exception, and the file is pinned

`.claude/rules/agent-workspace.md:42-45` says "**Do not edit them in place.**" The trimmed copy
is an edit of a vendored file, so the rule must say what the exception is, or the tree holds two
contradictory rules.

**The exception is conditional, and the condition is mechanical.** The security lens's High
finding (SEC-1) is the shape of the risk: the skill's prose *is* the instruction the model
follows when it writes the dashboard's front end, the rule converts an absolute prohibition into
a conditional, and the only machine check on that file was a five-word keyword list — which a
payload does not need to contain. The realistic trigger is not an attacker: it is the **next
re-vendor**, the maintenance action this very exception licenses, where a line inside
`## Core Philosophy` changes and nothing in the tree notices.

So the file is **pinned**: `SOURCE.md` records the SHA-256 of the trimmed `SKILL.md`, and
`deploy/check-workspace.sh` verifies it (D8). Any edit fails the check until the hash is updated
in the same change, with the reason recorded — which is what "recorded and checkable" has to mean
if it is to mean anything.

**The paragraph, verbatim:**

```markdown
- **The vendored skills** — `break-ui`, `review-animations`, `emil-design-eng` (the
  trimmed GAOS edition). Each carries a `SOURCE.md` with its upstream URL, commit
  and licence, and a `LICENSE`. **Do not edit them in place**, with one recorded
  exception: `emil-design-eng` is the upstream file with its motion, framework and
  duplicate-craft sections cut, the cut is listed in its `SOURCE.md`, and
  `deploy/check-workspace.sh` proves the result — no framework vocabulary, and the
  SHA-256 the `SOURCE.md` records. GAOS-specific guidance goes in
  `.claude/agents/gaos-frontend.md`.
```

### D7 — The frontend agent says what is true

`.claude/agents/gaos-frontend.md:15-28` currently reads: "Your taste comes from Emil Kowalski's
design-engineering skills, vendored in this repo" — and nine lines later says the philosophy skill
"was removed on 2026-11-04" (`:25-26`). The file is **stale**, not self-contradictory: it was
written for a tree that had two skills.

**The paragraph becomes** (in place of `:15-28`):

```markdown
Your taste comes from Emil Kowalski's design-engineering skills, vendored in this
repo (MIT, pinned — see each `SOURCE.md`):

- **`emil-design-eng`** (the trimmed GAOS edition): load it first for any frontend
  work — a new screen, a component, a polish pass, a visual review. It is the taste
  layer: the philosophy, and the review format.
- **`review-animations`** (with `STANDARDS.md`): load it before designing or
  changing anything that moves, and to review motion afterwards. It defaults to
  flagging; approval has to be earned.
- **`break-ui`** (with `CATALOG.md`): load it before you call a list, form or
  card done. It feeds worst-case data.

`emil-design-eng` is trimmed to the stack-agnostic half — the craft mechanics live
here, in this file, and motion lives in `review-animations`. Where the two differ,
this file wins. This dashboard is server-rendered Go `html/template` plus HTMX and
plain JS, so the skill's examples are principles and not code to copy.

Never edit the vendored skill files — except the recorded trim in
`emil-design-eng`, whose hash `deploy/check-workspace.sh` verifies. GAOS-specific
rules live here.
```

**The self-review step** (`:110-113`) gains the first skill:

```markdown
3. Self-review with `emil-design-eng` for taste and the review format,
   `review-animations` for anything that moves, and run a `break-ui` pass for
   long text, 0 or many items, non-Latin text and emoji, and a dropped connection.
```

### D8 — The validator checks the trim instead of the absence

`deploy/check-workspace.sh:213-231` becomes:

- **The vocabulary scan keeps covering every file in a skill directory** — it is not narrowed,
  and nothing is excluded by name. Revision 2 excluded `SOURCE.md` and `LICENSE`, and the
  security lens (SEC-3) found the shape wrong: the exclusion was a basename rule, so *any*
  guidance file called `SOURCE.md` in *any* skill directory went unscanned — and the file the
  skill tells the model to read in every frontend session was the one file the guard stopped
  reading. **The cut record is reworded instead** (D9): it names the removed sections without
  naming a framework, so it passes the scan it is in. The guard weakens by nothing.
- the baseline stays `break-ui review-animations`; the trimmed edition must score **0**, so it
  passes the same loop as every other non-baseline skill.
- **a new check pins the trimmed file** (SEC-1): `SOURCE.md` records the SHA-256 of the trimmed
  `SKILL.md`, and the validator recomputes it and fails when they differ — the exact string:

  ```bash
  bad "the trimmed emil-design-eng skill does not match the SHA-256 its SOURCE.md records"
  ```

- **a new check asserts every vendored skill's provenance** (SEC-4). The house already refuses to
  deploy the sisflow board without its licence and notice
  (`deploy/check-stacks.sh:147-156`, D-063), but nothing asserts that a vendored *skill* still
  carries its `LICENSE` and its `SOURCE.md` — so deleting either would strip the MIT attribution
  and the chain of custody from an auto-loaded instruction file while every other check still
  passed. The new check requires, for every directory under `.claude/skills/` that is not
  GAOS-authored: a non-empty `LICENSE`, and a non-empty `SOURCE.md` naming the upstream URL, the
  licence and the commit id. The exact strings:

  ```bash
  bad "skill $name has no LICENSE"
  bad "skill $name has no SOURCE.md naming its upstream URL, licence and commit"
  ```

- the explicit assertion at `:229-231` ("the stack-mismatched vendored skill is gone") is
  replaced by the inverted one — **the exact string**:

  ```bash
  ok "the design-engineering skill is present, trimmed, and carries no framework vocabulary"
  ```

  and its failure, when the skill comes back untrimmed, is the loop's existing message:
  `skill <name> carries <n> framework-vocabulary match(es) and is not on the stack-skills baseline`.
- **the validator's own two count strings move** (`:141-142`): `"379 cases"` → `"381 cases"` and
  `"Product version 0.35.0"` → `"Product version 0.35.1"`. Without these the house check fails
  on the README even though the change did what it says.

### D9 — The provenance is recorded in two places

**`SOURCE.md`, verbatim after the amendment** (note that it names the cut sections by their
**upstream headings** and never by a framework's name, so it passes the vocabulary scan it lives
in — SEC-3):

```markdown
Vendored from https://github.com/emilkowalski/skill (skills/emil-design-eng) at commit
f1090bec277140d02ed202440185ad9d02ac1490, 2026-11-02. MIT, see LICENSE.

GAOS edition, trimmed 2026-11-04 — the design that says why is
docs/designs/frontend-design-rule.md. What was cut from the upstream file:

  * `## Initial Response` (upstream 8-16) — the upstream's chat ritual: it tells
    the model to answer with one canned sentence and stop. This repository loads a
    skill as part of doing work, not as a chat.
  * `## The Animation Decision Framework` (62-146), `## Spring Animations` (147-196),
    `## clip-path for Animation` (393-438), `## Gesture and Drag Interactions`
    (439-476), the component-library principles (559-603), `## Stagger Animations`
    (605-637) and `## Debugging Animations` (639-658) — motion, and a component
    library this dashboard does not have. The motion bar is
    `review-animations/STANDARDS.md`.
  * `## Component Building Principles` (197-344), `## CSS Transform Mastery`
    (346-391), `## Performance Rules` (477-525), `## Accessibility` (527-557) and
    `## Review Checklist` (660-676) — the craft mechanics, which this repository
    already holds in `.claude/agents/gaos-frontend.md`. Two authorities for one rule
    is how they drift.

What was kept, verbatim: `## Core Philosophy` (18-36) and `## Review Format (Required)`
(38-60), plus a new `## GAOS edition` section. The upstream file scored 11 matches on
the validator's framework-vocabulary grep; this edition scores 0.

sha256(SKILL.md) = <the digest of the trimmed file, verified by deploy/check-workspace.sh>

GAOS-specific guidance lives in .claude/agents/gaos-frontend.md and
.claude/rules/frontend.md. Do not edit this file beyond the recorded trim.
```

The digest is computed when the trimmed file is written —
`sha256sum .claude/skills/emil-design-eng/SKILL.md` — and the validator recomputes it
(D8). Note what the last line says: the upstream's "**Do not edit in place:** GAOS-specific
guidance lives in .claude/agents/gaos-frontend.md" is **replaced**, not left standing — otherwise
the provenance file would forbid exactly the edit that was made, and contradict the rule that
permits it (D6).

**`NOTICE`'s third-party section** (`:30-49`) gains one bullet for **all three** vendored skills,
because they share one upstream, one commit and one licence, and the section currently lists
neither of the other two:

```markdown
  * The Emil Kowalski design skills ...... MIT License
    https://github.com/emilkowalski/skill
    `break-ui`, `review-animations` and `emil-design-eng`, vendored under
    .claude/skills/ at commit f1090bec277140d02ed202440185ad9d02ac1490.
    `emil-design-eng` is a GAOS-modified edition: the upstream file with its
    motion, framework and duplicate-craft sections cut, recorded in its
    SOURCE.md.
```

### D10 — No product decision entry, and why

The decision log's own criterion: "A decision belongs here when it constrains future work and
reversing it would cost something" (`pustaka/docs/product/decision-log.html:30-34`).

This change **does not** qualify, and the reason is the precedent rather than a preference: **the
EGM sweep — the same class of change, and a bigger one — added no decision entry.** It deleted a
vendored skill, rewrote `.claude/agents/gaos-frontend.md`, amended
`.claude/rules/agent-workspace.md` and amended the validator, and the log never mentions it
(`docs/designs/egm-design-review.md` is where that work is recorded). D-063, the one vendoring
ruling in the log, is a third-party **licence grant for a component in the product** (the sisflow
board) answered from an open question — not a vendored tool. D-068/D-069/D-070 record the
design gate, which is **product behaviour**.

A rule that tells an agent which skill to load is workspace policy, and workspace policy is
recorded in the workspace: in `.claude/rules/frontend.md`, in `.claude/rules/agent-workspace.md`,
in the skill's `SOURCE.md`, and in this document. The decision count therefore stays **70**, and
no `decision-log.html` change rides with this one.

---

## The mechanism

```
a session touches dashboard/templates/**, dashboard/static/** or dashboard/render.go
  │
  ├─ .claude/rules/frontend.md loads        (path-scoped, and symlinked into .agents/)
  │    ├─ /emil-design-eng      the trimmed edition — the taste layer
  │    ├─ /review-animations    anything that moves
  │    └─ /break-ui             before calling a list, form or card done
  │
  └─ the work, with the dashboard's own craft rules (.claude/agents/gaos-frontend.md)
```

Two independent routes reach the same guidance, and neither depends on the other: the rule reaches
a session that edits a template; the agent file reaches the `gaos-frontend` agent. The `break-ui`
skill's existing pointers become true.

---

## The phases

Five phases, in dependency order. **The order is load-bearing:** a rule that names a skill that
does not exist yet is the very defect this change repairs, so the skill lands first.

### Phase 1 — the skill (files 1-3)

Restore the three files from `HEAD` and trim `SKILL.md` per D2. `SOURCE.md` is amended per D9.

*Exit:* `grep -c '^## ' .claude/skills/emil-design-eng/SKILL.md` is **3** (`## GAOS edition`,
`## Core Philosophy`, `## Review Format (Required)`), `grep -rioE 'react|jsx|tailwind|sonner|shadcn' .claude/skills/emil-design-eng/SKILL.md | wc -l` is **0**, and
`grep -c "GAOS edition" .claude/skills/emil-design-eng/SOURCE.md` is **1**.

### Phase 2 — the rule and its wiring (files 4-6)

Create `.claude/rules/frontend.md`, its symlink `.agents/rules/frontend.md`, and the
`rule-index.md` row.

*Exit:* `grep -c "emil-design-eng" .claude/rules/frontend.md` is **3**, `readlink
.agents/rules/frontend.md` is `../../.claude/rules/frontend.md`, and `grep -c "frontend.md"
.claude/rules/rule-index.md` is **1**.

### Phase 3 — the agent and the workspace rule (files 7-8)

Replace the `gaos-frontend.md` skill paragraph and its self-review step; replace the
`agent-workspace.md` vendored-skills paragraph.

*Exit:* `grep -c "removed on 2026-11-04" .claude/agents/gaos-frontend.md` is **0**,
`grep -c "emil-design-eng" .claude/agents/gaos-frontend.md` is **3**, and `grep -c "recorded"
.claude/rules/agent-workspace.md` is **1**.

### Phase 4 — the validator and the runbook (files 9-10)

Rescope the vocabulary scan (nothing is excluded), invert the assertion, add the SHA-256 pin and
the provenance check, move the validator's own two count strings, and add the new rule to the
runbook's planting list.

*Exit:* `./deploy/check-workspace.sh` passes — the planted canaries are removed before this phase
runs, and the validator's own strings already match the new counts.

### Phase 5 — the registers, the mirrors and the changelog (files 11-17)

The QA rows, the count mirrors, the version, the README, the changelog entry and the NOTICE
bullet.

*Exit:* `cd pustaka && go run . check ./docs` passes, `grep -rn "three hundred and eighty-one"
pustaka/docs/product/qa-test-plan.html pustaka/docs/assets/toc/product.js` finds every mirror,
and the house checks pass.

---

## The change list

| # | file | change |
|---|---|---|
| 1 | `.claude/skills/emil-design-eng/SKILL.md` | **restored** from `HEAD` and trimmed (D2, D3) |
| 2 | `.claude/skills/emil-design-eng/LICENSE` | **restored** from `HEAD`, unchanged (MIT) |
| 3 | `.claude/skills/emil-design-eng/SOURCE.md` | **restored** from `HEAD` and amended with the trim record (D9) |
| 4 | `.claude/rules/frontend.md` | **created** — the path-scoped rule (D4) |
| 5 | `.agents/rules/frontend.md` | **created** — a relative symlink to `../../.claude/rules/frontend.md` (D5) |
| 6 | `.claude/rules/rule-index.md` | the rules table gains the frontend row (D4) |
| 7 | `.claude/rules/agent-workspace.md` | the vendored-skills paragraph and the recorded-trim exception (D6) |
| 8 | `.claude/agents/gaos-frontend.md` | the skill paragraph and the self-review step (D7) |
| 9 | `deploy/check-workspace.sh` | the scan's scope, the inverted assertion, the SHA-256 pin and the provenance check (D8), and the two count strings at `:141-142` |
| 10 | `.claude/skills/gaos-design-review/SKILL.md` | the planting list gains `.claude/rules/frontend.md`, so a future probe exercises it (SEC-5) |
| 11 | `NOTICE` | the third-party components section gains the three vendored skills (D9) |
| 12 | `pustaka/docs/product/qa-test-plan.html` | the QA rows TC-380 and TC-381, the `#cw-workspace` section, the counts |
| 13 | `pustaka/docs/assets/toc/product.js` | the case-count mirror |
| 14 | `pustaka/docs/assets/toc.js` | `product.semver` → `0.35.1` |
| 15 | `pustaka/docs/changelog.html` | the v0.35.1 section |
| 16 | `README.md` | the case count and the version |
| 17 | `.claude/rules/qa-test-plan.md` | the counts and the next free number |
| 18 | `docs/designs/frontend-design-rule.md` | this document |
| 19 | `docs/designs/reviews/frontend-design-rule-comprehension-1.md` | the comprehension lens |
| 20 | `docs/designs/reviews/frontend-design-rule-zoomout-1.md` | the zoom-out lens |
| 21 | `docs/designs/reviews/frontend-design-rule-critic-1.md` | the critic lens |
| 22 | `docs/designs/reviews/frontend-design-rule-readiness-1.md` | the readiness lens |
| 23 | `docs/designs/reviews/frontend-design-rule-security-review-1.md` | the security lens |
| 24 | `docs/designs/reviews/frontend-design-rule-consolidated-1.md` | the triage |

**The arithmetic.** Seventeen files outside `docs/designs/` (rows 1-17), this document (18) and six
review files (19-24) — **24 files in all**.

---

## The counts that move

| count | now | becomes | why |
|---|---|---|---|
| QA cases | 379 | **381** | two new register rows: TC-380 (Positive) and TC-381 (Negative) |
| Go tests | 264 | **264** | no Go file changes |
| decisions | 70 | **70** | no ruling — D10 says why |
| BRD rules | 50 | **50** | no rule is added or amended |
| questions | 32 | **32** | no question is added or answered |
| version | 0.35.0 | **0.35.1** | a change with no new product capability |

**Why two rows and not one.** The house rule is a pair: "Give positive *and* negative cases. The
register is built around the pair, and a check with no stated failure mode has not been thought
through" (`.claude/rules/qa-test-plan.md:64-66`). This change adds a failure mode — a skill
carrying framework vocabulary fails the build — and the existing pair for the workspace validator
is TC-357 (Positive) and TC-358 (Negative) (`pustaka/docs/product/qa-test-plan.html:448-449`).
So the new rows are:

| id | automation | kind | scenario | status |
|---|---|---|---|---|
| TC-380 | `check-workspace.sh` | Positive | The design-engineering skill is vendored, trimmed to the stack-agnostic half, carries no framework vocabulary, and matches the SHA-256 its `SOURCE.md` records | Implemented |
| TC-381 | `check-workspace.sh` | Negative | A skill carrying framework vocabulary, a vendored skill without its licence or its provenance, or a trimmed skill whose digest moved is reported and the run exits 1 | Implemented |

Both land in the `#cw-workspace` family (`pustaka/docs/product/qa-test-plan.html:2623`), whose
title becomes "check-workspace.sh — the agent workspace's own invariants (TC-357, TC-358, TC-380,
TC-381)", and whose scenario prose gains the exact strings the script prints — the new `ok` line
from D8 and the loop's existing failure message.

**Where each count lives** — the four QA locations the QA rule names
(`.claude/rules/qa-test-plan.md:72-95`): the lede and the register intro in `qa-test-plan.html`,
and the `desc` and the register section's `text` in `toc/product.js`. The same rule names the
other mirrors, and each moves as an exact string:

| where | now | becomes |
|---|---|---|
| `qa-test-plan.html` lede | "three hundred and seventy-nine cases" | "three hundred and eighty-one cases" |
| `qa-test-plan.html` register intro | "All three hundred and seventy-nine cases…" | "All three hundred and eighty-one cases…" |
| `toc/product.js` `desc` | "three hundred and seventy-nine positive and negative test cases" | "three hundred and eighty-one …" |
| `toc/product.js` register `text` | "All three hundred and seventy-nine TC-nnn cases…" | "All three hundred and eighty-one TC-nnn cases…" |
| `README.md` | "379 cases" | "381 cases" |
| `README.md` | "Product version 0.35.0" | "Product version 0.35.1" |
| `toc.js` | `product.semver: "0.35.0"` | `product.semver: "0.35.1"` |
| `check-workspace.sh:125` | "three hundred and seventy-nine" | "three hundred and eighty-one" |
| `check-workspace.sh:141` | "379 cases" | "381 cases" |
| `check-workspace.sh:142` | "Product version 0.35.0" | "Product version 0.35.1" |
| `.claude/rules/qa-test-plan.md:50-51` | "The highest in use is `TC-379`; next free number is `TC-380`." | "The highest in use is `TC-381`; next free number is `TC-382`." |
| `.claude/rules/qa-test-plan.md:73-74` | "They are currently all "three hundred and seventy-five"" | "They are currently all "three hundred and eighty-one"" — **this line is stale today** (the register says 379) and is corrected here |
| `.claude/rules/qa-test-plan.md:77` | "one of the three hundred and seventy-nine" | "one of the three hundred and eighty-one" |
| `.claude/rules/qa-test-plan.md:81` | "All three hundred and seventy-nine cases…" | "All three hundred and eighty-one cases…" |
| `.claude/rules/qa-test-plan.md:127` | "**379 rows in total. 242 in `dashboard/` and 22 in `pustaka/`** — 264 Go tests" | "**381 rows in total. 242 in `dashboard/` and 22 in `pustaka/`** — 264 Go tests" |

**The Go test count does not move**, so none of its four mirrors moves either
(`toc/product.js`'s "two hundred and sixty-four", `deploy/check-workspace.sh:110-114`,
`README.md`'s "264 tests" and "242 tests", the QA rule's `:84`).

**The changelog entry, verbatim** — a patch release, in the house format
(`pustaka/docs/changelog.html:39-45`):

```html
    <section class="rel rv">
      <h3 id="v0-35-1">v0.35.1 <time datetime="2026-11-04">2026-11-04</time> <span class="bump patch">patch</span></h3>
      <p style="color:var(--faint); font-size:13.5px; margin:0 0 10px">The taste layer comes back, trimmed for this stack.</p>
      <ul>
        <li><span class="chg add">Added</span> <b>the <code>emil-design-eng</code> skill, trimmed</b>:
        Emil Kowalski's design-engineering philosophy and his review format, vendored under
        its MIT licence and cut down to the half that fits a server-rendered Go and HTMX
        dashboard. v0.29.0 removed it as 27 KB of React and Tailwind guidance for a stack
        this repository does not have; the contest the EGM design left open is answered, and
        the file comes back trimmed. What was cut and why is in its
        <code>SOURCE.md</code>.</li>
        <li><span class="chg add">Added</span> <b>a rule for the dashboard's frontend</b>: any
        session that touches <code>dashboard/templates/</code>, <code>dashboard/static/</code>
        or <code>dashboard/render.go</code> loads the design-engineering skill first, then
        <code>review-animations</code> for anything that moves, then <code>break-ui</code> before
        a list, form or card is done.</li>
      </ul>
    </section>
```

---

## The verification plan

One row per changed file, with the command and the expected result.

| # | file | check | expected |
|---|---|---|---|
| 1 | `SKILL.md` | `grep -c '^## ' .claude/skills/emil-design-eng/SKILL.md` | `3` — the kept half is still there, so the trim cannot pass by deleting everything |
| 2 | `SKILL.md` | `grep -rioE 'react\|jsx\|tailwind\|sonner\|shadcn' .claude/skills/emil-design-eng/SKILL.md \| wc -l` | `0` |
| 3 | `SOURCE.md` | `grep -c "GAOS edition" .claude/skills/emil-design-eng/SOURCE.md` | `1` — the trim is recorded |
| 4 | `frontend.md` | `grep -c "emil-design-eng" .claude/rules/frontend.md` | `3` |
| 5 | `frontend.md` | `grep -A3 '^paths:' .claude/rules/frontend.md` | the three paths, and no more |
| 6 | `.agents/rules/frontend.md` | `readlink .agents/rules/frontend.md` | `../../.claude/rules/frontend.md` |
| 7 | `rule-index.md` | `grep -c "frontend.md" .claude/rules/rule-index.md` | `1` |
| 8 | `agent-workspace.md` | `grep -c "recorded" .claude/rules/agent-workspace.md` | `1` — the exception is stated |
| 9 | `gaos-frontend.md` | `grep -c "removed on 2026-11-04" .claude/agents/gaos-frontend.md` | `0` — the stale sentence is gone |
| 10 | `gaos-frontend.md` | `grep -c "emil-design-eng" .claude/agents/gaos-frontend.md` | `3` |
| 11 | `check-workspace.sh` | `./deploy/check-workspace.sh` | every check `ok`, and the line "the design-engineering skill is present, trimmed, and carries no framework vocabulary" |
| 12 | `SOURCE.md` (the pin) | `grep -o 'sha256(SKILL.md) = .*' .claude/skills/emil-design-eng/SOURCE.md` | the digest, and it equals `sha256sum .claude/skills/emil-design-eng/SKILL.md` |
| 13 | `check-workspace.sh` (the provenance check) | `ls .claude/skills/*/LICENSE .claude/skills/*/SOURCE.md` | every vendored skill has both, and each `SOURCE.md` names an upstream URL, a licence and a commit |
| 14 | `gaos-design-review/SKILL.md` | `grep -c "rules/frontend.md" .claude/skills/gaos-design-review/SKILL.md` | `1` — the new rule joins the planting list |
| 15 | `NOTICE` | `grep -c "emilkowalski/skill" NOTICE` | `1` — all three skills, one bullet |
| 16 | `qa-test-plan.html` | `grep -c "<td>TC-381</td>" pustaka/docs/product/qa-test-plan.html` | `1` |
| 17 | `qa-test-plan.html` | `grep -oE "<td>TC-[0-9]{3}</td>" pustaka/docs/product/qa-test-plan.html \| sort -u \| wc -l` | `381`, and the highest id is TC-381 |
| 18 | `toc/product.js` | `grep -c "three hundred and eighty-one" pustaka/docs/assets/toc/product.js` | `2` — the `desc` and the register `text` |
| 19 | `toc.js` | `grep -n 'semver' pustaka/docs/assets/toc.js` | `semver: "0.35.1"` |
| 20 | `changelog.html` | `grep -c 'id="v0-35-1"' pustaka/docs/changelog.html` | `1` |
| 21 | `README.md` | `grep -c "381 cases" README.md` and `grep -c "Product version 0.35.1" README.md` | `1` each |
| 22 | `qa-test-plan.md` (the rule) | `grep -c "three hundred and eighty-one" .claude/rules/qa-test-plan.md` | `4` — `:73-74`, `:77`, `:81`, `:127` |
| 23 | `qa-test-plan.md` (the rule) | `grep -c "TC-381" .claude/rules/qa-test-plan.md` | `1` |

And the house checks:

```
cd pustaka   && go run . check ./docs
cd dashboard && go test ./...
cd ../pustaka && go test ./...
./deploy/check-stacks.sh
./deploy/check-workspace.sh
```

### The isolation probe for this round

The house procedure (`.claude/skills/gaos-design-review/SKILL.md:35-41`): the runbook's six
standing files, verbatim — `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md`,
`.claude/rules/rule-index.md`, `.claude/rules/qa-test-plan.md`, `.claude/rules/agent-workspace.md`
— one canary each. `.claude/rules/frontend.md` is **not** among them: it does not exist until
this change lands, and a canary planted in a file the reviewers were about to read would be a
canary in a file the reviewers were told to read.

**The check, and it is the house's own**: the *value* pattern, not the prefix — a report may
discuss the prefix, only a planted value is a leak
(`deploy/check-workspace.sh:169-185`, `docs/designs/reviews/README.md:34-39`):

```bash
grep -rnE 'EGM-MARKER-[A-Za-z0-9._-]*[0-9a-f]{8,}' .claude/ AGENTS.md GEMINI.md docs/designs/reviews/
# must return nothing
```

**The positive control** (`docs/rules/gaos-design-review.md:67`): one review with the exclusion
deliberately disabled, which **must** produce a report containing a canary. Without it, a probe
that can never detect anything is indistinguishable from a clean run.

**The removal**: every planted canary is removed unconditionally after the round, whether it
succeeded, failed or was interrupted, and `./deploy/check-workspace.sh` — which fails on a
leftover (`deploy/check-workspace.sh:178-185`) — confirms the tree is clean before phase 4.

---

## Alternatives considered

**A — Restore the upstream file unchanged.** Rejected: it scores 11 on the validator's own
framework-vocabulary guard (`deploy/check-workspace.sh:217`), so the tree would not pass its own
check, and 27 KB of guidance for a component library this dashboard does not use would load in
every frontend session.

**B — Write the rule without restoring the skill.** Rejected: `/emil-design-eng` would name
something that does not exist, and the `break-ui` pointers
(`.claude/skills/break-ui/SKILL.md:3`, `:16`) would stay dangling.

**C — Put the rule in `.claude/agents/gaos-frontend.md` instead of a rule file.** Rejected: an
agent file reaches only the agent that is invoked; a rule reaches every session that touches the
frontend, including the operator talking to a different agent. The operator asked for a rule.

**D — Keep the motion sections and drop `review-animations` instead.** Rejected: `review-animations`
is stack-agnostic and its `STANDARDS.md` is the bar this repository holds the agent to
(`gaos-frontend.md:28`); the upstream motion family is where the framework examples are densest
(the `jsx` fence at upstream 499, `Framer Motion` at 160, `npm` at 561). Two motion manuals is one
too many, and the house already picked one.

**E — Trim by deleting the whole skill and writing the philosophy from scratch.** Rejected: the
philosophy is the part worth keeping and it is not ours to write. A vendored, attributed,
MIT-licensed text trimmed to what applies is a smaller and more honest change than a
paraphrase that loses the attribution.

**F — Keep the craft sections, rewritten into plain CSS.** Rejected: that is hand-editing
vendored MIT prose to restate rules this repository already states in
`.claude/agents/gaos-frontend.md:31-83`. Two authorities for one rule is how they drift.

---

## Risks

| Risk | Handling |
|---|---|
| The trim loses guidance this dashboard could use | the boundary is written down section by section (D2), so a reviewer can check each cut. The craft is not lost — it is in `gaos-frontend.md`, which the rule and the agent both point at |
| The rule duplicates the agent file | the rule carries the *stack-agnostic* policy (which skills, when, and where the craft lives); the agent file carries the stack's mechanics (the variables, the durations, the curves). They answer different questions. The house forbids a rule sentence appearing in two rule files (`deploy/check-workspace.sh:144-166`) — the agent file is not a rule file, so the fork check does not fire, and the rule-index names one |
| The validator's guard is weakened by excluding the provenance files | the guard still covers every guidance file in every skill directory — `SKILL.md`, `CATALOG.md` and `STANDARDS.md` — which is what the model reads. Only `SOURCE.md` and `LICENSE` are out, because the record of the trim has to be able to name what was cut. The trimmed edition must score **0**, which is a *stricter* outcome than the absence the check asserted before |
| A modified vendored file is a fork of upstream | the modification is recorded in `SOURCE.md` and `NOTICE`, MIT permits modification with the notice retained, and the upstream URL and commit stay in the tree |
| The rule loads on every frontend session and costs context | the rule is short, path-scoped, and points at three skills rather than restating them |
| The published record of the removal becomes false | the v0.35.1 changelog entry says plainly that the skill comes back trimmed, and names the v0.29.0 removal it reverses |
| **The trimmed file is an auto-loaded instruction file, and a rule is prose** | SEC-1: the file is **pinned by SHA-256** in `SOURCE.md` and verified by the validator, so an edit fails the check until the digest is updated in the same change with the reason recorded (D6, D8) |
| **The scan's exclusion left the file the model reads unscanned** | SEC-3: nothing is excluded — the cut record names the removed sections without naming a framework, so it passes the scan it lives in, and the guard covers every file in every skill directory (D8, D9) |
| **A vendored skill's licence and provenance could be deleted silently** | SEC-4: the validator now asserts, for every vendored skill, a non-empty `LICENSE` and a `SOURCE.md` naming the upstream URL, the licence and the commit — the sisflow precedent, applied to the skills (D8) |
| **The rule's scope could skip the files that build the served markup** | SEC-2: the scope is the rendered surface — templates, static, `render.go`, `view.go`, `seq.go` — and the rule says a `template.HTML` value must be escaped where it is built (D4) |

---

## Status

**Approved (revision 3).** Revision 1 was reviewed by four lenses (comprehension, zoom-out,
critic, readiness); their reports are `docs/designs/reviews/frontend-design-rule-*-1.md`. The
**security lens ran against revision 2**, because revision 1's trim boundary no longer existed by
then, and its seven findings are in `docs/designs/reviews/frontend-design-rule-security-review-1.md`.
The triage is `docs/designs/reviews/frontend-design-rule-consolidated-1.md`.

Implementation starts after the operator approves the triage.

## Related

- `docs/designs/m17.5-reviewer-workspace.md` — the other change of this milestone, which lands
  **after** this one: this change takes the case count 379 → 381 and the version 0.35.0 → 0.35.1,
  and M17.5 starts from 381 and 0.35.1
- `docs/designs/egm-design-review.md` — the design whose contest this change answers
- `.claude/agents/gaos-frontend.md` — the frontend agent this rule serves
- `.claude/rules/agent-workspace.md` — the workspace layout and the vendored-skill rule
- `.claude/rules/qa-test-plan.md` — the register this change updates
- `deploy/check-workspace.sh` — the validator this change amends
