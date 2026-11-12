# EGM Design Review — GAOS

**Effective:** 2026-11-04
**Scope:** every feature-sized change to GAOS that matches a trigger below
**Owner:** the author of the design; the reviewers are the five `gaos-goldfish-*`
agents
**Method:** the Elephant-Goldfish Model (Google, Apache-2.0) —
<https://github.com/google/elephant-goldfish-model>

**This file is the source of truth for the procedure.** The rule layer points at
it; the skill at `.claude/skills/gaos-design-review/SKILL.md` runs it. If any
other document disagrees with this one, this one wins.

## Why this rule exists

The agent that plans a change should not be the only agent that reviews it. The
same context that produced the plan is the context that cannot see its blind
spots, and the failure mode is not stupidity — it is that an assumption made in
the first ten minutes is never questioned by anyone who did not share it.

So: the author holds the context and drafts the design. A **goldfish** reviewer
gets the document and the rubric, and tries to break it.

This repository already found one case where it mattered: the same defect
shipped four times because each review shared the author's context. The audit
that found it is in `docs/designs/egm-design-review.md` (F1–F10).

## The three roles

| Role | Context | Job |
|---|---|---|
| **Elephant** (the author) | the full conversation, the repo, the sibling repos | explore the problem, draft the design, triage the findings, apply the fixes to the design |
| **Goldfish** (the reviewer) | the design document and the rubric only | find missing assumptions, unclear requirements, implementation risks, attack paths |
| **The operator** | the whole picture | resolve the trade-offs, approve the design, decide what gets built |

The author does not implement from a design that has not been approved.

## When a design is required

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

## The flow

1. **Draft** `docs/designs/<feature>.md`. It must stand alone: a reader with no
   knowledge of the conversation must be able to judge it. Every claim carries a
   file and a line. See `docs/designs/README.md` for the contract.
2. **Review.** Run the five lenses with the document path and the rubric path
   list — and nothing else. The rubric is `.claude/rules/`, and
   `.claude/rules/rule-index.md` names the domain sources (the BRD, the
   decision log, the open-question register) that a review must also read.
   When the security criterion matches, the security lens is not optional.
3. **Verify the isolation.** Plant a canary in each standing file, run the
   reviews, and check that no canary reached any report. Then run the
   **positive control** — one review with the exclusion deliberately disabled,
   which *must* produce a report containing a canary. Without that step, a
   probe that can never detect anything is indistinguishable from a clean run.
4. **Save** the reports to `docs/designs/reviews/<feature>-review-<n>.md` (and
   `<feature>-security-review-<n>.md`). Append-only, numbered.
5. **Triage.** Every finding gets exactly one disposition:

   | Disposition | Use when |
   |---|---|
   | `CONTEST` | the finding is wrong — and the contest is checkable |
   | `ESCALATE` | the finding is right and outside this change's scope |
   | `PROPOSE FIX` | the finding is right and this change fixes it |
   | `DECLINE` | the finding is right but the fix costs more than the defect |

   Rules: never change a severity; a contest must be checkable; a comprehension
   gap (the reviewer could not paraphrase it) is never contested — fix the
   document.
6. **Apply** the `PROPOSE FIX` items and update the design. The fix goes into
   the **design**, not into the code.
7. **Re-review** if any Blocker survives.
8. **Ask the operator.** Show the paraphrase, every 🔴 Blocker and 🟡 Major, and
   the triage table. **Implementation starts only after approval.**

## The security criterion

Run `gaos-goldfish-security` in addition when the design touches
authentication, authorisation, credentials, tokens, the egress allowlist, the
workspace allowlist, the back-office surface, a new external origin, or an
outbound call to one.

A **Critical** or **High** security finding is a Blocker for the design's
status: it cannot go to "Approved" with one open.

## The output contract

Each general lens reports:

```
### [Short title] - Severity: [Blocker|Major|Nit]
- Affected scope: <section or file:line inside the design>
- Finding: <what is wrong or missing>
- Recommendation: <what the document should say or do instead>
```

and ends with exactly `Counts: B=<n> M=<n> N=<n>`.

The security lens uses its own vocabulary, on purpose, because a security
finding and a wording finding must never compete for the same label:

```
### SEC-<n> <short title> - Severity: [Critical|High|Medium|Low]
- Component: <area>
- Evidence: <path:line>
- Attack path: <who, what they can do>
- Remediation: <the concrete change>
- Effort: quick-win | small | structural
```

ending with exactly `Counts: Critical=<n> High=<n> Medium=<n> Low=<n>`.

## What a reviewer may and may not read

A reviewer **may** read the repository. It has `Read`, `Grep` and `Glob`, the
design contract requires every claim to carry a `file:line`, and confirming an
author's claim independently is the whole value of a zero-context reviewer. The
limit is intent, not reach: a reviewer has artifacts and no authorial intent.

It may **not** read the author's standing memory or any credential-bearing file.
The full deny-list is in `.claude/rules/agent-workspace.md`. It records **path
and line, never the matched text**.

## Limits — state them, do not hide them

- **Isolation is a property of the harness, not of the method.** In Claude Code
  it is `omitClaudeMd: true` (needs v2.1.271+, and it is ignored when the agent
  runs as the main session agent). In every other CLI it is an instruction, and
  the canary probe is the only evidence. The probe proves *"the canary did not
  reach a report"*, not *"the file was not read"*.
- **A reviewer cannot know the author's intent.** It can check that a file
  exists and says what the document claims. It cannot know why the author
  wanted that.
- **A review does not replace tests or human judgement.** It is a second pair
  of eyes with no memory.
- **It costs tokens and time.** Five parallel Opus sessions per design, and
  the harness has no budget cap — the operator pays the bill.
- **Reviewers are noisy.** Expect roughly a third of the findings to need triage
  rather than action.
- **A review that finds nothing is a failed review.** If a design survives five
  lenses with no findings, suspect the lenses, not the design.

## Related

- `.claude/agents/gaos-goldfish-*.md` — the five reviewer lenses
- `.claude/skills/gaos-design-review/SKILL.md` — the runbook
- `.claude/rules/agent-workspace.md` — the workspace layout and the deny-list
- `.claude/rules/rule-index.md` — what to read before what
- `docs/designs/README.md` — the design-document contract
- `docs/designs/egm-design-review.md` — the first design under this rule, and
  the review that produced it
