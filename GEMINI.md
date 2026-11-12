# GAOS — workspace context

> [!IMPORTANT]
> **Primary environment:** this repository is configured for **Claude Code**.
> `CLAUDE.md` and `.claude/*` are the single authoritative source of truth for the
> project layout, the coding policies, the agent roster and the design gate.
> Antigravity MUST read, respect and apply all instructions from `CLAUDE.md`
> and `.claude/*`.

---

## 1. Read this first

| Before doing… | Read |
|---|---|
| ANY task, ANY CLI (Claude Code, Antigravity, MiniMax Code, Codex) | `.claude/rules/rule-index.md` — the rule index and the authoritative domain sources |
| Creating, changing or deleting a test, an automation or a CI workflow | `.claude/rules/qa-test-plan.md` |
| Working in `.claude/` or `docs/` — an agent, a skill, a rule, a design document | `.claude/rules/agent-workspace.md` |
| A feature-sized change to anything | `docs/rules/gaos-design-review.md` — the design gate's policy |

## 2. The project rules

`CLAUDE.md` holds the always-on layer: four invariants and where to read the
rest. It is small on purpose, and the root `CLAUDE.md` imports
`.claude/CLAUDE.md` so it loads whichever discovery path the harness uses.

Read `.claude/CLAUDE.md` before changing anything. It is short.

## 3. Agents, skills and rules

- **`.claude/agents/`** — the subagent roster: `gaos-frontend` and `gaos-deploy`,
  plus the five `gaos-goldfish-*` reviewers that the design gate dispatches.
- **`.claude/skills/`** — procedural runbooks, loaded when invoked.
- **`.claude/rules/`** — path-scoped rules. They load on demand, not at every
  session start.

`.agents/rules/` holds relative symlinks into `.claude/rules/`, and
`.agents/skills.json` registers `.claude/skills`, so both harnesses read one
copy of each rule. **Never copy a rule into a second file** — a forked rule
forks the policy, and `deploy/check-workspace.sh` refuses one.

## 4. The design gate

A feature-sized change to GAOS starts as a document in `docs/designs/`, is read
by five zero-context reviewers, and is approved before anything is implemented.
The policy is `docs/rules/gaos-design-review.md`; the runbook is the
`gaos-design-review` skill. A bug fix, a typo, a comment and routine
maintenance do not need a design.

## 5. Where the product's own rules live

`.claude/rules/` **in this repository** is the rule layer for agents working on
GAOS. `.claude/rules/` **inside a product workspace** is the product's
Blueprint, written by GAOS itself, and it loads on every turn. Never edit the
second one; a change there is drift, and the drift gate refuses the next
dispatch.

The product's own constraints are in `pustaka/docs/product/brd.html`,
`pustaka/docs/product/decision-log.html` and
`pustaka/docs/product/open-questions.html`.

## 6. Publishing a note to mfardiansyah.id

`docs/platform/publish-note-grpc.md` is the canonical procedure: the gRPC call
shapes, the proposal-mode gate, the house style, and what to verify before and
after publishing. It is mirrored into each CLI's memory store, so it is
available from here too.

## 7. Before you call it done

```bash
cd pustaka   && go run . check ./docs   # structure + registry consistency
cd dashboard && go test ./...
cd ../pustaka && go test ./...
./deploy/check-stacks.sh                # the stack invariants
./deploy/check-workspace.sh             # the agent workspace invariants
```

There is no CI: `D-025` makes the product owner the runner for every check.


