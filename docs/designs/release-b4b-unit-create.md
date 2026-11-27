# Release B part 4b — the QA tool writes missing unit tests

> **Status:** Draft. Requirements carried from `docs/designs/release-b4-unit-tests.md` rounds 1–5 (reports `docs/designs/reviews/release-b4-unit-tests-*-{1..5}.md`). Not reviewed; not approved. It builds on part 4 (`unit-run`, D-105) once that ships.
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/release-b4-unit-tests.md` (part 4, `unit-run`: the card, the runner, the unit defect and the hold this design reuses), `docs/designs/release-b-e2e-kinds-merge-unit-tests.md` (D-103; its §5 is the operator's choice).
> **Example.** The invented product **Route Planner** (`p-9`), application repository `acme/route-api`. Published pages use only `acme/*` names.

## 0. Why this is a separate design

The operator chose (D-103 (5), sibling design §5): the QA tool runs the unit tests **and may write missing ones; a create opens a draft pull request through a separate identity limited to `qa-unit/*`**. Part 4 (rev 6) delivers the run; five review rounds showed the create half holds most of the open security and readiness findings, so it is designed here, on top of part 4 once part 4 is built. The scope split was taken under the operator's delegation of 2026-11-27 (*"dont ask for any of my further confirmation and decision. work and collaborate among agents yourselves."*) and is recorded in D-105.

## 1. What this design must specify (carried requirements)

1. **Mode.** `unit-create`: the QA tool runs the existing suite (reported first, as part 4's `unit`), then writes tests for changed source files without one. A `UnitQA.Create` setting, refused without the operator's CI statement and the unit identity's login; a capability `unit-create`.
2. **Approval of generated tests.** By an authenticated, named person in the QA tool, recorded; never through GAOS. If GAOS is to enforce it too, it must be at answer time and at the poll read, bound to data GAOS owns (the card's job id), with BR-57 amended — not only in the inbox webhook (part 4 round 5: critic Blocker on §8.5, SEC-2; readiness §8.5).
3. **Approval wait.** A create card enters Needs you (BR-44's runner exception); the act-once marker must be writable while AWAITING_INPUT (part 4 rev 6 `ResolveUnitQACard` gains that move here, with a test).
4. **The pull request.** A draft on `qa-unit/<sha12>-<job8>` from the same repository, only test files, never merged by the QA tool, opened by a **separate identity** (no `workflows` permission). Part 4's runner ignores `pr_url`; this design adds the row.
5. **The row.** A Pull requests row kind `qa-unit` (`PRKindQAUnit`, schema widening `PullRequest.kind`, `PullRequest.headSha`), written only after GAOS read the pull request from GitHub (repository equal to the card's, exact head ref, author, draft, not a fork); a refused pull request keeps a row marked `refused:<why>` and is re-checked; a kind-collision refusal in `upsertPullRequestLocked`; a transient read keeps the job tracked past `final()` (bounded tries).
6. **The check at every head.** Draft, author, head ref, fork, `pullFiles` complete, `contextPathOK`, no `.github/`, `TestGlobs`; re-run whenever the head moves and at merge; a store method to write the note (`NoteQAUnitRow`); the poller's notifier; a standing alert on the Pull requests and Auto-fix pages.
7. **What the branch rule proves.** Part 4 round 5 found that `GET rules/branches/<ref>` proves a rule exists, not who may bypass it (SEC-5, critic "check 0 / identity"); that the head ref must be validated before it is put in a URL (SEC-6); that the check cannot live in the store method under the lock and belongs in the handler (SEC-7, readiness, critic); and that `creation`/`deletion` rules matter. This design must say what is verifiable (a GitHub App with a dedicated bypass, or an accepted residual), and on a private repository on GitHub's free plan the rules endpoint is not available at all.
8. **Commit authorship.** Advisory only (GitHub maps commits to logins by email); state it or require verified signatures.
9. **Notices.** `unit:approval` (group), `unit:files` — with the verdict class only, never a path, in group chats (SEC-8).
10. **Own pull requests.** Part 4 round 5 (critic) found that a merge carries no e2e spec, so an own-PR rule on merge removes nothing; decide whether any rule is needed, and that GAOS does not review `qa-unit` pull requests (a residual to restate).
11. **CI on `qa-unit/*`.** The operator's statement: no repository secrets, read-only workflow token, no `pull_request_target`.
12. **Registers and contract.** Its own D-number, BR text, TCs, and the QA tool's contract rows (capability, start mode, status fields, approval, identity, pull request shape).
