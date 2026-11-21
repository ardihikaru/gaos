---
paths:
  - ".claude/**"
  - "docs/**"
---

# The agent workspace, and the design gate

**This file is scoped.** It loads when you work on `.claude/` or `docs/`, not at
every session start. The always-on layer is `.claude/CLAUDE.md`; this is the
detail.

## The workspace layout

```
CLAUDE.md                 ← the always-on layer. Four items, and pointers.
.claude/CLAUDE.md         ← the same file, imported by the root CLAUDE.md so it
                             loads whichever discovery path the harness uses.
.claude/rules/            ← path-scoped rules. Loaded on demand, not at launch.
.claude/agents/           ← subagent definitions.
.claude/skills/           ← procedural runbooks, loaded when invoked.
docs/                     ← working documents: rules, designs, articles.
pustaka/docs/             ← the published documentation site.
```

**Two different things are called "rules", and they are not the same.**

- **`.claude/rules/`** — this repository's rules for the agents working *on
  GAOS*. Path-scoped, so they load on demand.
- **`.claude/rules/` inside a product workspace** — the *product's* Blueprint,
  written by GAOS itself (`dashboard/blueprint.go`). `CLAUDE.md`,
  `00-gaos-contract.md`, `10-brd.md`, `20-stories.md`, `30-decisions.md`, the
  `4x`–`6x` policy files and `70-knowledge-sources.md`. Those load on every
  turn in that product, including a resumed one.

When this document says "the rubric", it means the first kind.

## What may be edited

- **`.claude/agents/`, `.claude/skills/`, `.claude/rules/`** — yours, for
  GAOS's own workflow.
- **The vendored skills** — `break-ui`, `review-animations`, `emil-design-eng` (the
  trimmed GAOS edition). Each carries a `SOURCE.md` with its upstream URL, commit
  and licence, and a `LICENSE`. **Do not edit them in place**, with one recorded
  exception: `emil-design-eng` is the upstream file with its motion, framework and
  duplicate-craft sections cut, the cut is listed in its `SOURCE.md`, and
  `deploy/check-workspace.sh` proves the result — no framework vocabulary, and the
  SHA-256 the `SOURCE.md` records. GAOS-specific guidance goes in
  `.claude/agents/gaos-frontend.md`.
- **Never** the product's Blueprint files. GAOS owns them; a change there is
  drift, and the drift gate refuses the next dispatch.

## The design gate

A feature-sized change starts as a **design document**, not as a code edit.

### When a design is required

Write `docs/designs/<feature>.md` **before** any file outside `docs/designs/`
changes when the request:

- adds or changes a Go handler, a store method, or a schema statement;
- adds or changes an event type, a task status, or a packet kind;
- adds or changes a Blueprint policy kind or a rendered Blueprint file;
- adds or changes a validation script, a test, or a CI workflow;
- adds or changes an agent, a skill, a rule, `CLAUDE.md`, `AGENTS.md` or
  `GEMINI.md`;
- adds or changes a deployment, a stack file, or an edge config;
- adds or changes a published docs page.

A **fix** does not need a design. A bug fix, a typo, a comment, a one-line
config change and routine maintenance go straight to implementation.

### The three roles

| Role | Context | Job |
|---|---|---|
| **The Elephant** (the author) | the full conversation, the repo | explore the problem, draft the design, triage the findings |
| **The Goldfish** (the reviewer) | the design document and the rubric only | find missing assumptions, unclear requirements, implementation risks |
| **The operator** | the whole picture | resolve the trade-offs, approve the design, decide what gets built |

The policy — the roles, the trigger list, the security criterion, the triage
vocabulary, the output contract and the limits — is
`docs/rules/gaos-design-review.md`. It is the source of truth. The runbook is
`.claude/skills/gaos-design-review/SKILL.md`.

### The reviewer agents

Five lenses, each a separate zero-context session:

| Agent | Asks |
|---|---|
| `gaos-goldfish-comprehension` | can a naive reader follow this, and can they say it back? |
| `gaos-goldfish-zoom-out` | is this the smallest thing that removes the defect? |
| `gaos-goldfish-critic` | does this survive contact with the system it lands in? |
| `gaos-goldfish-readiness` | can two engineers implement this and get the same result? |
| `gaos-goldfish-security` | what is the attack path, and what is the fix? |

### The reviewer's deny-list

A reviewer may read the repository — that is how it checks the author's
`file:line` claims independently. It may **not** read:

- `CLAUDE.md`, `.claude/CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — the author's
  standing memory, and the whole point of a zero-context review;
- `*.env`, `*.env.*`, `*.aim-*`, `*.api-*`, `*.bo-*` — the host-side credentials;
- `*.pem`, `*.key`, `privkey.pem`, `**/credentials*`;
- `deploy/local/**` — in this checkout and in any other checkout of the
  repository, the main one included: one operator's private instance material
  and the forbidden-strings list (D-085);
- `~/.ssh`, `~/.aws`;
- any path outside the repository.

And a reviewer records **path and line, never the matched text**. A finding that
quotes a line from a secret file would put a live credential into a committed,
append-only report that every later session reads. GAOS's own rule for a stored
finding is the same: a record never carries matched secret text.

## The cross-harness wiring

`GEMINI.md` and `AGENTS.md` are **pointers, not copies**. They name `CLAUDE.md`
and `.claude/rules/` and hold no rule text of their own: a rule restated in
two places forks, and one harness ends up enforcing the older wording.

`.agents/rules/*.md` are **relative symlinks** into `.claude/rules/`, so a rule
is one file read by two harnesses. `.agents/skills.json` registers
`.claude/skills` for Antigravity.

This mirrors the convention GAOS already uses for product workspaces, where the
agy worker links `AGENTS.md`, `GEMINI.md` and `.agents/rules` to the same
`CLAUDE.md` and `.claude/rules` the Blueprint writes (T-178, D-067) — one
digest, one set of files, several harnesses.

## The workspace validator

`deploy/check-workspace.sh` checks the agent workspace's internal consistency:
that every path named in `.claude/CLAUDE.md` exists, that no agent or skill is
an orphan, that every count agrees with its register, that no rule is forked,
that no isolation canary was left behind, and that no secret reached the report
store.

Run it by hand, like every other check — `D-025` makes the product owner the
runner, and this repository has no CI.


