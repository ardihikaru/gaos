---
paths:
  - "dashboard/templates/**"
  - "dashboard/static/**"
  - "dashboard/render.go"
  - "dashboard/view.go"
  - "dashboard/seq.go"
---

# The dashboard's frontend

The dashboard is an operator console, used all day, in a browser, with no build step.
This rule says which skills to load before you touch its frontend, and what the
stack is when you do.

## Load these three, in this order

1. **`/emil-design-eng`** — first, for any frontend work: a new screen, a
   component, a polish pass, a visual review. It is the taste layer: the
   philosophy, and the review format for saying what is wrong with an interface.
   Its **GAOS edition** is the upstream skill trimmed to this stack (`SOURCE.md`
   beside it records the trim); read it as principles, not as code to copy.
2. **`/review-animations`** — for anything that moves, before you write the
   motion and again after. Its `STANDARDS.md` is the motion bar. It defaults to
   flagging; approval has to be earned.
3. **`/break-ui`** — before you call a list, form or card done. It feeds
   worst-case data: long text, unbreakable emails, one-letter names, 0 or many
   items, non-Latin text, emoji, extreme numbers.

Load the first one for *taste* and *review*, the second for *motion*, the third
for *edge cases*. They do not overlap, and none of them replaces your own
reading of the code you are about to change.

## The stack is fixed

- **Templates:** server-rendered Go `html/template`.
- **htmx:** 2.0.4, vendored at `static/htmx.min.js`. Every request targets
  `#sink`, and real updates arrive as `hx-swap-oob` elements.
- **JavaScript:** plain JS only (`static/app.js`, `static/board.js`,
  `static/live.js`). No framework, no bundler, no npm.
- **CSS:** one hand-written `static/app.css`. A new colour gets a variable in
  both themes or it does not land.

The design-engineering skill's examples come from a component library this
dashboard does not have. Take the reasoning — origin-aware popovers, a
`scale(0.95)` entry, `transform` and `opacity` only, no `transition: all` — and
write it in the stack above. The `gaos-frontend` agent holds the mechanical
rules: the CSS variables, the durations, the easing curves, the modal
behaviour, the focus ring.

## The scope is the rendered surface

This rule reaches the templates, the static assets and the three Go files that
build what the browser renders: `render.go`, `view.go` and `seq.go`. It does not
reach the Go handlers that print it — the back office's own view-model assembly
(`buildBO`) is the frontend's work, and a handler is the backend's.

One rule follows from that boundary: **a `template.HTML` or `template.CSS`
value must be escaped where it is built.** The back office prints such values
unescaped, so the escaping is earned by an explicit `template.HTMLEscapeString`
at construction. Dropping one is a frontend change, and this rule is what
reviews it.

## Report a review as a table

Every frontend review is a markdown table, one row per issue:

| Before | After | Why |
|---|---|---|
| `transition: all 300ms` | `transition: transform 200ms ease-out` | `all` animates properties you did not choose |

That is the design-engineering skill's own format, and it is the one the
`gaos-frontend` agent reports in. One table, one row per issue, and the *why*
beside it.

## What else applies

- `.claude/CLAUDE.md` — the four invariants. The first one is yours here: **if
  you add or change a test or an automation, the QA register changes in the
  same change** (`pustaka/docs/product/qa-test-plan.html`).
- `.claude/rules/qa-test-plan.md` — the register's mechanics and its counts.
- A template error is a test failure, not a style issue: templates are parsed
  at boot, and handler tests render them. Verify with
  `cd dashboard && go vet ./... && go test ./...`.
- Never edit the product's Blueprint files in a workspace — they are GAOS's,
  and a change there is drift.

## Related

- `.claude/agents/gaos-frontend.md` — the agent that owns this work
- `.claude/skills/emil-design-eng/` — the trimmed edition, with `SOURCE.md`
- `.claude/skills/review-animations/` — the motion bar
- `.claude/skills/break-ui/` — the edge-case pass
