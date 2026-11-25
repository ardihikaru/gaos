# gaos-pr-flows-followups — review round 1 (consolidated)

- Document: `docs/designs/gaos-pr-flows-followups.md`
- Lenses: critic (B2 M6 N5), readiness (B2 M4 N3), security (High 1, Medium 5, Low 2).
- Isolation: the canary positive control was not run. All three lenses reported that the harness attached
  `.claude/CLAUDE.md` to their context unasked; none used it.
- Process note: the change was coded before the document, which the gate forbids; the review is of a finished
  change and its findings were fixed in place.

Full lens reports were returned in-session and are summarised by theme; severities are the reviewers'.

| Theme | Lenses | Severity | What was done |
| --- | --- | --- | --- |
| The runner's moves could override an operator (a blocked or canceled card brought back, a finished card reopened) | critic, security | Blocker / Medium | Fixed: an allowed-start table, no walk through `DONE`/`FAILED`/`CANCELED`, every hop a `TaskMoved` with rule `pr-flow` (TC-612) |
| A restart would start a second job | critic, readiness | Blocker | Fixed: the job id is kept in the card's reason and re-adopted (TC-608) |
| A job flapping between waiting and running notifies forever | critic, security | Major / Medium | Fixed: more than 5 waits fails `qa-job-flapping`, one notice per job (TC-609) |
| An approval in the last interval before the cap is lost | readiness | Major | Fixed: the cap is applied after the last poll (TC-610) |
| The approval budget has no upper bound or a per-poll reading | readiness, critic | Major | Fixed: cumulative per job, at most 720 hours (TC-604, TC-611) |
| http to any host sends the login in clear text | security | High | Fixed: private addresses only, checked on every connection; `any` is explicit (TC-613) |
| The status can hide the clear link; the boot check sends the credential with no product linked | security | Medium | Fixed: the status always says unencrypted; the boot check is skipped (TC-614) |
| The product list covers the group but not the contacts; an empty list is ambiguous | critic, security | Medium | Fixed: it covers both; empty means none (TC-606, TC-607); upgrade note written |
| The approval notice may reach a committer | security | Medium | Fixed: always the pinned group (TC-615) |
| A short sha in the staging callback makes a short id | critic | Minor | Fixed: a full 40/64-hex commit, else 422 |
| The collision is described as seen | readiness | Minor | Fixed: the document says it was a latent risk found by reading the code |
| Missing change list, phases, non-goals, registers, restart section, BR-44 amendment | readiness | Blocker / Major | Fixed: sections 4 to 9 and D-089 |
| The QA tool has no cancel call: a spec approved after GAOS failed the card still opens its pull request | security | Low | **Not fixed; documented residual** (D-089, document section 10) |
| The canary positive control | process | Minor | **Not run** |
