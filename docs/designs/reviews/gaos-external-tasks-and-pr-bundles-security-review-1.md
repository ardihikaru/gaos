# Security review 1: `gaos-external-tasks-and-pr-bundles.md` (revision 1)

The `gaos-goldfish-security` lens ran with zero context. It got the design path and the rubric paths
(`docs/rules/gaos-design-review.md`, `docs/designs/README.md`) and was told to open nothing under `.claude/`.

Counts: Critical=0 High=3 Medium=9 Low=3. The triage is in `gaos-external-tasks-and-pr-bundles-review-1.md`.

The reviewer corrected one premise of the rubric. The label route requires a verified session
(`handlers_board.go:205-209`, `auth.go:380-396`), so Release C's operator path has an identity. No finding
was raised there.

- **SEC-1 (High): an API client can take over a PR's natural key.**
  - It can plan `pr-review` (manual, or `dispatch:false`) or `unit-test` for any PR before Brain Town does,
    then cancel the card.
  - `taskByDedupKeyLocked` matches CANCELED cards (`prflow.go:166-173`), so the real event becomes a replay
    (`prflow.go:228-235`). The automatic review, or the unit tests on merge, never happen.
  - Fix: no cancel of natural-key cards, `unit-test` only on a merged PR, or distinct API keys.
- **SEC-2 (High): a client can fill `GAOS_PR_MAX_OPEN`.**
  - API cards count toward the shared cap (`prflow.go:176-191`). A full cap skips real Brain Town events
    (`prflow.go:257-259`), and after B5 the cursor passes them.
  - Fix: a per-client cap, excluded from the PR cap.
- **SEC-3 (High): only `pr-review` gets the draft, fork and association gate.**
  - `unit-test` and `automation-create` can run a code-writing agent on a stranger's fork PR
    (`prflow.go:62-65,365-381`).
  - Fix: `prepare` for every kind that takes `pr`, `503` when GitHub is off, and `unit-test` only when
    merged.
- **SEC-4 (Medium): B4 closes a secret-held review card on merge.**
  - It closes the `needs-manual` card set because the reply carried a secret (`prreview.go:240-244`), and the
    signal is lost.
  - Fix: a distinct reason, never auto-finished, and the notice ships no later than B4.
- **SEC-5 (Medium): unclear whether stranger PRs pass the template parse and bundle join.**
  - If they do, anyone can gate, break or inject notes into a bundle.
  - Fix: parse only PRs that passed the gates; a stranger's linked PR never gates.
- **SEC-6 (Medium): polling same-owner, unfollowed members leaks data.**
  - Their titles and file lists go into the model packet and the QA tool.
  - Fix: state only, or restrict to `productOwns`. Bound the polls.
- **SEC-7 (Medium): codes allow delimiter characters.**
  - `TaskCode`, `caseCode` (`validCursor`, `qacatalog.go:418`) and `externalId` allow `:`, `@`, `#`, `/` and
    `.`. These confuse `prKeyParts` (`prreview.go:194-209`) and reach the QA tool's codes.
  - Fix: strict patterns, and `prKeyParts` refuses non-PR prefixes.
- **SEC-8 (Medium): `automation-run *` can be repeated without limit.**
  - Each new `externalId` starts a new run.
  - Fix: one open run per product and code, or a rate limit.
- **SEC-9 (Medium): the packet fence is a Markdown heading.**
  - The body is appended raw (`prreview.go:182-184`). `link` codes are checked against text the attacker
    wrote, and the contract has no "DATA, never instruction" clause.
  - Fix: a per-packet random fence, that clause, and validation of `link` codes against the GAOS catalog.
- **SEC-10 (Medium): lookup and fetch order is unspecified.**
  - If `prepare` runs before the lookup, retries burn the GitHub rate limit.
  - Fix: look up first, fetch only when creating.
- **SEC-11 (Medium): any client can GET any card on the lane.**
  - Cancel ownership by "prefix" lets `acme` cancel `acme-2`'s cards.
  - Fix: own cards only, and an exact name compare.
- **SEC-12 (Medium): deploy wiring is missing.**
  - Prod mounts no clients file or client tokens.
  - The container runs as `GAOS_UID` (1001 or 1000, `compose.prod.yaml:198-203`), not 65532, so the cursor
    volume would not be writable.
  - Fix: read-only mounts and `check-stacks.sh` rows; ownership matched to `GAOS_UID`, never world-writable.
- **SEC-13 (Low):** the review token is called "read", but it posts reviews (`github.go:270-280`).
- **SEC-14 (Low):** `review: manual` lets an outside client trigger WhatsApp messages to PR authors.
  - Fix: group only, with the client named.
- **SEC-15 (Low):** the public tutorial and the event messages add to what is published.
  - Fix: placeholders, and confirm the outbound feed redacts.
