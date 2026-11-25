---
name: gaos-design-review
description: Use when a GAOS change is big enough to need a design document — it walks the trigger list, drafts docs/designs/<feature>.md, runs the zero-context goldfish reviewers with a canary probe and a positive control, triages the findings, and asks for approval before anything is implemented.
---

# GAOS Design Review

The end-to-end runbook for the Elephant-Goldfish Model in GAOS. The policy is
`docs/rules/gaos-design-review.md`; the directory contract is
`docs/designs/README.md`; the reviewer lenses are `.claude/agents/gaos-goldfish-*.md`.

## When this applies

Run it when the request adds or changes: a Go handler, a store method, or a
schema statement; an event type, a task status, or a packet kind; a Blueprint
policy kind or a rendered Blueprint file; a validation script, a test, or a CI
workflow; an agent, a skill, a rule, `CLAUDE.md`, `AGENTS.md` or `GEMINI.md`; a
deployment, a stack file, or an edge config; or a published docs page.

**A fix does not need a design** — bug fixes, typos, comments, one-line config
changes and routine maintenance go straight to implementation.

## Steps

### 1. Draft the design

Write `docs/designs/<feature>.md`. It must stand alone: every claim anchored to
`file:line`, the change list complete, the arithmetic written down, the
alternatives real, nothing left undecided, the non-goals listed.

### 2. Plant the canaries

One unique string per standing file, so a leak in any of them is visible:

```bash
for f in .claude/CLAUDE.md AGENTS.md GEMINI.md .claude/rules/rule-index.md \
         .claude/rules/qa-test-plan.md .claude/rules/agent-workspace.md \
         .claude/rules/frontend.md; do
  [ -f "$f" ] || continue
  printf '\n<!-- EGM-MARKER-%s-%s -->\n' "$(basename "$f" | tr -c 'a-zA-Z0-9' -)" \
    "$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')" >> "$f"
done
```

Record which files were touched. **Removal is unconditional** — see step 5.

### 3. Run the reviewers

Spawn the lens agents with **only** the document path and the rubric path
list. Do not paste the design into the prompt. Do not summarise it. Do not pass
the conversation.

- `gaos-goldfish-comprehension` — always.
- `gaos-goldfish-zoom-out` — always.
- `gaos-goldfish-critic` — always.
- `gaos-goldfish-readiness` — always.
- `gaos-goldfish-security` — additionally, when the design touches auth,
  authorisation, credentials, tokens, the egress allowlist, the workspace
  allowlist, the back-office surface, a new external origin, or an outbound
  call to one.

Run them in parallel; they share nothing but the target file and the rubric.

### 4. Verify the isolation

```bash
grep -rn "EGM-MARKER-" docs/designs/reviews/    # must return nothing
```

Then the **positive control** — one review with the exclusion deliberately
disabled, which **must** produce a report containing a canary. Without it, a
probe that can never detect anything is indistinguishable from a clean run.

#### The CLAUDE.md probe (verified 2026-11-25, Claude Code 2.1.332)

Reviewers carry `omitClaudeMd: true` (frontmatter; Claude Code 2.1.271+), which
keeps the project `CLAUDE.md` and its `@` imports out of their context. Prove it
rather than assume it. **A canary appended to `CLAUDE.md` during a session proves
nothing**: agents inherit the session-start snapshot, so the canary is not in it
(both probes below answered NONE for exactly that reason). Probe with a sentence
that is already in the snapshot instead, and use two agents with **no tool call
before the first answer**:

1. *Positive control* — a `general-purpose` agent, asked "does your context contain
   the sentence starting `Four invariants, and where to read the rest`?" **must
   answer YES**. If it says NO the probe cannot see the file and proves nothing.
2. *The test* — a `gaos-goldfish-*` agent, the same question, **must answer NO**;
   then have it `Read` one file under `docs/` and ask again: **still NO**.

Expected side effect, not a leak: reading under `docs/` or `.claude/` attaches the
path-scoped rule `.claude/rules/agent-workspace.md` as a system reminder to every
agent, reviewers included. It is a rubric file a reviewer may read (it mentions
`CLAUDE.md` by name, which earlier rounds misread as `CLAUDE.md` being loaded). The
canaries for the *rules* files are verified by a reviewer that is made to read one.

### 5. Remove the canaries

Remove every planted line, whether the run succeeded, failed or was
interrupted, and confirm the tree is clean:

```bash
grep -rn "EGM-MARKER-" . && echo "LEFTOVER — remove before committing"
```

`deploy/check-workspace.sh` fails on a leftover, so an aborted run is caught by
the validator rather than by a later `git diff`.

### 6. Save the reports

Write each report to `docs/designs/reviews/<feature>-review-<n>.md` (and
`<feature>-security-review-<n>.md`). Append-only, numbered — the sequence of
rounds is the record of convergence. Never overwrite one.

### 7. Triage

One disposition per finding: `CONTEST` (wrong, with a checkable reason),
`ESCALATE` (right, out of scope), `PROPOSE FIX` (right, fixed here), `DECLINE`
(right, the fix costs more than the defect). Never change a severity. A
comprehension gap is never contested — fix the document instead.

### 8. Apply and re-review

Apply the `PROPOSE FIX` items to the **design**, update it, and run another
round if any Blocker survives. Keep the reports.

### 9. Ask the operator

Show the paraphrase, every 🔴 Blocker and 🟡 Major, and the triage table.
**Implementation starts only after approval.**

## The trap this workflow exists to catch

The author and the reviewer share the assumption that broke the last thing. A
plan whose numbers do not follow from its own change list is the classic
symptom: nobody outside the conversation ever checked the arithmetic.

Run the arithmetic twice — once by the author, once by a reviewer that cannot
see the conversation. And remember what the reviews found here: the first round
caught the plan's own change list disagreeing with its own headings, a headline
figure that mixed two measurement bases, and an isolation probe that could not
fail. The method does not only find defects in the product; it finds them in
the plan.

## Related

- `docs/rules/gaos-design-review.md` — the policy (the source of truth)
- `docs/designs/README.md` — the design-document contract
- `.claude/rules/agent-workspace.md` — the workspace layout and the deny-list
- `.claude/rules/rule-index.md` — what to read before what
