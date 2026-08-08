# Contributing to GAOS

GAOS is Apache-2.0 and self-hosted. Anyone may fork it, run it, change it and
ship it. This file says how a change gets back into this repository, and what
this repository will and will not take.

Read `pustaka/docs/product/brd.html` before a substantial change — it is the
business source of truth, and most "why is it like this?" questions are answered
there or in the decision log beside it.

## Sign your work — the DCO

There is no CLA. Contributions stay under the licence they arrive with, and
copyright stays with whoever wrote the change. What is required instead is a
sign-off: a line at the end of each commit message certifying that you have the
right to submit the work under Apache-2.0.

```
Signed-off-by: Your Name <your.email@example.com>
```

`git commit -s` adds it. The text of the certification is the
[Developer Certificate of Origin 1.1](https://developercertificate.org/), and
signing off means you agree to it. Use a real name and a reachable address —
the same one you would be happy to be contacted at years from now.

Why a DCO and not a CLA: a CLA exists so a project can relicense contributions
later. GAOS does not need that. The paid components live in separate private
repositories the project owns outright, so nothing in this repository ever needs
to be relicensed, and asking contributors to sign paperwork for a power the
project has decided not to use would be a cost with no purpose.

## What belongs in this repository

The rule that governs the boundary is not a preference, it is a requirement:

> **The open repository is a complete working system on its own. No private
> component may ever be required to install, run, plan, execute, review or
> replay.**
> — BRD `BR-27`

So: anything the system needs in order to work belongs here. Optional capability
that a self-hoster can live without may live in a separate paid component, and
what those are is still being decided in the open — see `OQ-19` on the open
questions page.

Two things are settled and worth stating so nobody wastes work on a change that
will be refused:

* **No safety control, termination guard, budget cap or audit record is ever
  gated, degraded, or moved behind a paid component** (`BR-28`). A patch that
  weakens a guard so a paid version can restore it will be rejected on sight.
* **Fixes land here, for everyone.** Support subscriptions buy attention, never
  correctness. There is no private branch where bugs get fixed first.

## Before you open a pull request

This repository has no CI. Everything below runs locally, and the maintainer
runs the same commands — see `pustaka/docs/product/qa-test-plan.html` for the
full plan and what each check is actually testing.

```bash
cd dashboard && go test ./...          # stdlib only, race-clean, no dependencies
cd pustaka   && go test ./...
cd pustaka   && go run . check ./docs  # docs structure + registry consistency
./deploy/check-stacks.sh               # deployment invariants a running stack hides
```

Constraints that are deliberate, not accidental — a change that breaks one of
them needs to argue for itself first:

* `dashboard/` and `pustaka/` are **Go standard library only**, with no
  `go.sum`. Adding a dependency is a design decision, not a convenience.
* The documentation site makes **no third-party requests** at render time.
  Fonts and libraries are vendored.
* The **event catalog is closed** (`BR-05`). A new event type is a schema change
  to the fold, and has to land in the data model document, in
  `dashboard/events.go`, and in the reducer together — never in one of the three
  alone.
* Documentation pages follow `pustaka/ai/AUTHORING_SPEC.md`. `pustaka check`
  enforces the structural half of it; the component vocabulary is the other
  half, and inventing new CSS classes is not in it.

## Documentation changes

Docs are part of the product here, not an afterthought — three of the registers
(the BRD, the decision log, the open questions) are how this project decides
things. If a change makes one of them wrong, fixing it is part of the change.

A release updates `pustaka/docs/changelog.html` and `product.semver` in
`pustaka/docs/assets/toc.js` together. They are one change, and the changelog
page says so.

## Reporting a security issue

Do not open a public issue. Report it privately to the address on the project's
GitHub profile, and give it a reasonable window before disclosing.

The two areas where a report is most valuable, because they are where GAOS can
hurt someone without anything appearing to fail: **egress** — anything that
sends more of a repository to a model provider than the operator asked for or
can see (BRD domain L) — and the **spend guards**, since an unattended agent
holding a model credential is spending real money on the operator's own account.

## Trademarks

The code is Apache-2.0. The name is not — see `NOTICE`. Build on GAOS, say you
built on GAOS, and give your fork its own name.
