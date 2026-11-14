# Review record: bo-login

Design: `docs/designs/bo-login.md`. Three rounds, goldfish reviewers with the design and the repository only.

| Round | Reviewers | Result |
|---|---|---|
| 1 (v1) | comprehension, zoom-out, critic, readiness, security | 5 reviewers, all rejected v1. Blockers: five routes check nothing and one GET parks every write (`/events`, `/replay`, `/tasks/{id}/context`, `/products/{id}/readiness`, `/ui/cw*`); the new secret files were not gitignored or linked into a tag worktree; no change list. Majors: a per-address lock locks every operator out behind the proxy; eviction can clear a lock; pustaka's auth should be lifted, not re-invented; test helper cannot sign. |
| 2 (v2) | security, readiness | v2 moved to a default-deny gate at the mux; per-account throttle; `*.bo-*` secret files. New: the gate cannot read `r.Pattern` outside the mux (use `mux.Handler`); starvation through the semaphore and a full table; the operator password must not pass through the agent session; reviewer deny-lists; the count and change-list gaps. |
| 3 (v3) | readiness | The two remaining Blockers were tooling: `check-workspace.sh` count lines already stale, and `hash-password` needing an image that exists only after the deploy it blocks. Both fixed in v4 (build the image from the tag first; move the script's counts with the change). |

Triage notes. Kept deliberately against the zoom-out reviewer: a users file with several accounts rather than one credential (the session carries an email for the audit-identity follow-up). Not done, recorded as follow-ups: a CSP / `pustaka check` rule against third-party scripts, `/healthz` returning the raw database error to anonymous callers, an actor on events. Isolation note: in every round the harness attached `.claude/CLAUDE.md` to the reviewer's context although it is on the deny-list; reviewers said they did not use it. The canary probe was not run.
