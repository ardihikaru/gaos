# Agent Roster

> **Before any work:** read `CLAUDE.md` and `.claude/rules/rule-index.md` — they define the
> mandatory read order and the per-CLI context mapping. This file is a role index;
> `.claude/agents/*.md` holds the authoritative role prompts.

Primary roles:
- **gaos-frontend** — the dashboard's design engineer: Go `html/template`, HTMX fragments,
  vanilla JS, `app.css`, motion and interaction. Reports every review as a markdown table.
- **gaos-deploy** — rebuilds, restarts and reports on the gaos **prod** stack. The dev tier
  is retired: refuse it by name. Driven through `deploy/rebuild-restart.sh`.

Design-review roles (Elephant-Goldfish Model):
- **gaos-goldfish-comprehension** — zero-context reviewer for a design document in
  `docs/designs/`; rubric = every file in `.claude/rules/` plus the domain sources the rule
  index names; findings are 🔴 Blocker / 🟡 Major / 🟢 Nit
- **gaos-goldfish-zoom-out** — zero-context reviewer asking whether the plan is the smallest
  sufficient change and whether existing code already solves it
- **gaos-goldfish-critic** — zero-context reviewer of architecture, structure and style; it
  verifies the document's claims against the repository and cites `path:line`
- **gaos-goldfish-readiness** — zero-context reviewer applying the zero-decision rule: if the
  implementer would have to ask "which one did they mean?", the document is not ready
- **gaos-goldfish-security** — zero-context security reviewer (auth, credentials, the egress
  pass, the workspace allowlist, the back-office surface); findings are Critical / High /
  Medium / Low

Policy `docs/rules/gaos-design-review.md` · runbook skill `gaos-design-review` · the rubric in
`.claude/rules/` · reports in `docs/designs/reviews/`.

Skills:
- `gaos-design-review` — the runbook: draft, review, isolation probe, triage, approve
- `review-animations` — load it before designing or changing anything that moves, and to review
  motion afterwards; it defaults to flagging
- `break-ui` — load it before calling a list, form or card done; it feeds worst-case data

See `.claude/agents/*.md` for detailed scope, and `.claude/skills/*/SKILL.md` for the runbooks.

**Never copy a rule into a second file.** `.agents/rules/` holds relative symlinks into
`.claude/rules/`, so both harnesses read one copy of each rule; a copied rule forks the
policy, and `deploy/check-workspace.sh` refuses one.


