# gaos-pr-overview — review round 1 (consolidated)

- Document: `docs/designs/gaos-pr-overview.md` (revision 1).
- Reviewers: zoom-out (B1 M6 N3), comprehension (B1 M7 N7), readiness (B0 M10 N4), critic (B1 M11 N4), security (High 1, Medium 4, Low 4).
- Canaries were planted in the seven standing files before the round and removed after the next one; no canary value appears in any report.

## What the round forced

Three of the four lenses found the **same Blocker**: the design left the superseded pull request open and told a person to close it, but the existing poller (`fixprpoll.go:259-272`) moves the defect on a close or a merge whenever the defect is `fixing`, without asking whether the record is the defect's current one. Closing the old pull request would raise the generation, demote the defect to medium and strand a live retarget card; merging it would mark the defect fixed under an open replacement.

The zoom-out lens found that the retarget **reinvented the defect generation**: `Defect.Gen` already gives a distinct branch, record id, dedup key and stale-card refusal. It also found the same fact stored three times, a second poller copying the first, a branch-existence call per drawer open where one at post time would do, and that the forward case is already a setting (`AutoFix.BaseBranch`).

Security found one High: a QA-supplied `pr_url` made GAOS read any repository with its read token, breaking the invariant that the repository comes from the product (`github.go:29-30`).

Readiness and comprehension found the change list incomplete (`fixprpoll.go`, `schema/17-defects.sql`, `schema_test.go`, `auth_test.go`, the landing page and BRD counts), the `Base` back-fill impossible as described (two early returns), a `planDefectCardLocked` that cannot carry the promised title and reason, and wrong line anchors.

## Triage

| Finding | Severity | Disposition | Note |
|---|---|---|---|
| Closing or merging the superseded pull request breaks the retarget | Blocker (x3) | PROPOSE FIX | §4.2: a replaced or pending record never moves the defect; a merge while pending blocks the card; tests |
| The retarget reinvents `Defect.Gen` (`fix-<n>` job, counters, `-r` suffixes) | Major | PROPOSE FIX | §6.3 raises `Gen`; `isFixJob`, `Retargets`, `RetargetOf` removed |
| Same fact stored three times; straw-man alternative | Major | PROPOSE FIX | §3.1 hybrid: `PullRequest` only for review and qa |
| Second poller; `Base` back-fill cannot run | Major | PROPOSE FIX | §4.2 one poller; both early returns named |
| QA `pr_url` lets an outside service pick the repository | High | PROPOSE FIX | §4.4 only listed repositories, no row and no call otherwise |
| Stored URL links contradict "never from a stored URL" | Medium | PROPOSE FIX | no `url` column; `qaLink` validated |
| Actor and reason of a privileged action not recorded | Medium | PROPOSE FIX | `planOpts`; `Actor`, `Rule`, `Reason` on the one `TaskPlanned` |
| Three writers share one key without precedence | Medium | PROPOSE FIX | key includes the product; upsert never changes kind/product/task |
| Previous reply returns to an agent as instructions | Medium | PROPOSE FIX | untrusted-data section, cut and stripped |
| Drawer calls GitHub from a GET | Low/Major | PROPOSE FIX | no call from a render; one call at post time |
| New base must reach the CI-secrets check | Low | PROPOSE FIX | stated and tested |
| `AllowedBases` accepts any name; retargets unbounded | Low | PROPOSE FIX / DECLINE | cap of three per chain; default branch not refused (admin's decision, recorded) |
| Published page and read access | Low | PROPOSE FIX | placeholder names; OQ-37 noted |
| Change list incomplete; arithmetic | Major | PROPOSE FIX | §13 recounted |
| Route response, gates and statuses unspecified | Major | PROPOSE FIX | §6.1, §6.2 tables |
| Wrong line anchors, path convention, terms | Nit | PROPOSE FIX | corrected; a terms line added |
| Split the retarget from the page | Major | PROPOSE FIX | §14 phases A and B, separate releases |
