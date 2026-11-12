---
name: emil-design-eng
description: Emil Kowalski's philosophy on UI polish and the invisible details that make software feel great, and the review format for saying what is wrong with an interface. GAOS edition — the stack-agnostic half only; this dashboard's own craft mechanics live in .claude/agents/gaos-frontend.md, motion in review-animations, edge cases in break-ui.
---

# Design Engineering

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

## Core Philosophy

### Taste is trained, not innate

Good taste is not personal preference. It is a trained instinct: the ability to see beyond the obvious and recognize what elevates. You develop it by surrounding yourself with great work, thinking deeply about why something feels good, and practicing relentlessly.

When building UI, don't just make it work. Study why the best interfaces feel the way they do. Reverse engineer animations. Inspect interactions. Be curious.

### Unseen details compound

Most details users never consciously notice. That is the point. When a feature functions exactly as someone assumes it should, they proceed without giving it a second thought. That is the goal.

> "All those unseen details combine to produce something that's just stunning, like a thousand barely audible voices all singing in tune." - Paul Graham

Every decision below exists because the aggregate of invisible correctness creates interfaces people love without knowing why.

### Beauty is leverage

People select tools based on the overall experience, not just functionality. Good defaults and good animations are real differentiators. Beauty is underutilized in software. Use it as leverage to stand out.

## Review Format (Required)

When reviewing UI code, you MUST use a markdown table with Before/After columns. Do NOT use a list with "Before:" and "After:" on separate lines. Always output an actual markdown table like this:

| Before | After | Why |
| --- | --- | --- |
| `transition: all 300ms` | `transition: transform 200ms ease-out` | Specify exact properties; avoid `all` |
| `transform: scale(0)` | `transform: scale(0.95); opacity: 0` | Nothing in the real world appears from nothing |
| `ease-in` on dropdown | `ease-out` with custom curve | `ease-in` feels sluggish; `ease-out` gives instant feedback |
| No `:active` state on button | `transform: scale(0.97)` on `:active` | Buttons must feel responsive to press |
| `transform-origin: center` on popover | `transform-origin: var(--transform-origin)` | Popovers should scale from their trigger (not modals — modals stay centered) |

Wrong format (never do this):

```
Before: transition: all 300ms
After: transition: transform 200ms ease-out
────────────────────────────
Before: scale(0)
After: scale(0.95)
```

Correct format: A single markdown table with | Before | After | Why | columns, one row per issue found. The "Why" column briefly explains the reasoning.
