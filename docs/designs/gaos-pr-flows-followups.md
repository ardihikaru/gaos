# The pull-request flows — follow-ups (v0.49.0)

> **Status:** Implemented; review round 1 saved at `docs/designs/reviews/gaos-pr-flows-followups-consolidated-1.md`;
> written after the code, which the gate forbids — recorded as a deviation.
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/gaos-pr-agent-flows.md` (§6.2, §8.1, §8.3, §13), D-089

## 1. Problem

Three things reading the code and the first live test of D-088 showed.

1. **A card id could collide.** A staging card's id carried the first 8 characters of the commit. Two commits of one
   repository sharing that prefix would get the same id, and the second would be read as a replay of the first: no
   card. This was a latent risk found by reading the code; it was never observed in use.
2. **A person approving a spec fails the card.** A QA-tool `create` job waits for a human to approve the spec it
   wrote. GAOS polls every 15 s for 40 minutes and then fails the card, although the job is healthy and the person
   is only slow.
3. **The WhatsApp gateway in use is http, and it is linked to one product.** GAOS refuses an http gateway, and the
   notifier would send for every product once on.

## 2. Card ids

The id carries 12 characters of the commit (a prefix collision is 1 in 2^48). The id is not the identity, the key
is: before a card is planned, an id already held by a card with another key gets a numeric suffix (`-2`, `-3`). A
replay still finds its card by key and plans nothing. The staging callback now requires a full 40- or 64-hex commit;
a short one is `422`, so a short sha never reaches the id.

## 3. The approval wait

The QA tool reports a job status **`awaiting_approval`** while the job's newest approval is open.

| Job status | The GAOS card |
| --- | --- |
| `running` | `RUNNING`; the 40-minute work clock runs |
| `awaiting_approval` | `AWAITING_INPUT` ("Needs you"); the reason carries `qa-job:<id>`; the work clock stops; one notice (kind `e2e:approval`) per job, always to the pinned group, never to a committer |
| back to `running` | `RUNNING`; the clock runs again |
| `succeeded` / `failed` | `DONE` / `FAILED` as before |

- **Budget.** The wait is cumulative per job: `GAOS_QA_APPROVAL_WAIT_HOURS`, default 24, at most 720. Past it the
  card fails `qa-approval-timeout`. The cap is applied after the last poll is read, so an approval given in the
  final interval is seen.
- **Flapping.** A job that enters the wait more than 5 times fails the card `qa-job-flapping`; the notice is still
  one per job.
- **Allowed starts.** The runner's moves are limited by a table (`resolveFrom`): a card an operator blocked or
  canceled stays there, a finished card never comes back, and the runner's walk through the state machine
  (`AWAITING_INPUT -> READY -> DISPATCHED -> RUNNING`) never passes through `DONE`, `FAILED` or `CANCELED`. Every
  hop is a `TaskMoved` with the rule `pr-flow`, so the stream shows `READY` and `DISPATCHED` hops the agent never
  made.
- **Old QA tools.** One that answers `running` for a waiting job leaves GAOS behaving as before.

## 4. Restart and recovery

The job id is kept in the card's reason (`qa-job:<id> …`). On a restart the runner re-adopts every `RUNNING` and
`AWAITING_INPUT` e2e card from that id and polls it; it starts no second job. A card with no id in its reason is
treated as not yet started, as before.

## 5. WhatsApp over http, and the products it serves

- `GAOS_WA_ALLOW_HTTP=1` allows an `http` gateway **only** at a loopback, RFC 1918, CGNAT or link-local address,
  checked on every connection (the dial, not the URL). `any` allows a public address and is the operator's explicit
  choice. A scheme that is neither http nor https is refused whatever the switch says.
- The status **always** says "unencrypted" for an http link, including when the notifier is otherwise off.
- With no product linked, the boot group check (the one call that sends the credential) is skipped.
- `GAOS_WA_PRODUCTS` lists the products the notifier serves, group **and** contacts. A card of any other product
  never notifies and leaves no `ContactNotified` event. Empty means none: the notifier is silent (fail closed).
- **Upgrade note:** a v0.48 deployment that sets `GAOS_WA_URL` must now also set `GAOS_WA_PRODUCTS`, or it stops
  notifying.

**Amends BR-44.** Besides an agent, the QA-job runner may put an e2e card in `AWAITING_INPUT` while the QA tool
waits for a person; the card leaves by the job resuming (`RUNNING`), failing, or a cancel.

## 6. Change list

Counted from `git status --short` against `HEAD`: 24 changed and 2 new files, 26 in all.

| File | What |
| --- | --- |
| `dashboard/prflow.go` | 12-character id, `freeCardID`, `resolveFrom` table, walk that avoids finished states |
| `dashboard/board.go` | the card's reason is kept server-side (`BlockReason`) |
| `dashboard/qajobs.go` | `awaiting_approval`, budget, flapping, adoption by job id, env bound |
| `dashboard/notify.go` | the approval notice to the group |
| `dashboard/wa.go` | http guard, product list, status, boot check |
| `dashboard/staging.go` | full 40/64-hex commit |
| `dashboard/prflow_test.go`, `qajobs_test.go`, `wa_test.go`, `notify_test.go`, `staging_test.go` | TC-601 to TC-615 (staging_test.go gains a case in an existing test, no row) |
| `deploy/stacks/compose.prod.yaml` | passes the three new variables from the deploy shell |
| `deploy/check-stacks.sh` | five assertions, TC-616 |
| `deploy/check-workspace.sh` | the counts |
| `pustaka/docs/product/qa-test-plan.html`, `decision-log.html`, `brd.html`, `changelog.html`, `integrations/pr-agents.html` | register, D-089, BR-44, release, settings |
| `pustaka/docs/assets/toc.js`, `toc/product.js` | version and counts |
| `README.md`, `.claude/rules/qa-test-plan.md` | counts and next free id |
| `docs/designs/gaos-pr-agent-flows.md` | "amended by D-089" notes |
| `docs/designs/gaos-pr-flows-followups.md`, `reviews/gaos-pr-flows-followups-consolidated-1.md` | new: this document and its review |

Arithmetic: 24 changed (7 Go source, 5 Go test, 3 deploy, 5 docs pages plus `brd.html`, 2 toc, README, rules file,
the parent design) + 2 new = 26. Go tests 478 -> 486 (+8: TC-608..615); register 607 -> 616 (+9 with TC-616).

## 7. Phases

1. Code and tests: `cd dashboard && go test ./...`
2. Deploy assertions: `./deploy/check-stacks.sh`
3. Register and docs: `cd pustaka && go run . check ./docs`
4. Counts and workspace: `./deploy/check-workspace.sh`

## 8. Non-goals

- A cancel call to the QA tool (it has none; see §10).
- GAOS answering an approval itself.
- Making http the default, or inferring it from the host.
- Per-contact product lists (the product list is one setting).

## 9. Registers

- **D-089** in the decision log; amends BR-44.
- **TC-601 to TC-616**: 607 -> 616 rows; 478 -> 486 Go tests (464 in `dashboard/`, 22 in `pustaka/`); next free TC-617.
- Release v0.49.0.

## 10. Alternatives, failure and residual risk

- **Raise the poll limit.** Rejected: any number fails a card for a slower person, and a long limit hides a hung
  job for as long.
- **Have GAOS answer the approval.** Rejected: it is a human judgement of generated code (D-088 §14).
- **Make http the default.** Rejected: a credential in clear text must be a choice.
- **Link the group to every product by default.** Rejected: the group belongs to one product's team.
- A QA tool that never leaves `awaiting_approval` fails the card at the budget with its own reason.
- **Residual:** the QA tool has no cancel call, so a spec approved after GAOS failed its card still opens its pull
  request; the QA tool's reject is the remedy, and a person rejecting a spec shows as `qa-job-failed`.
- **Residual:** a public http gateway (`any`) sends the login in clear text.

## 11. Done when

- [x] Two commits sharing a 12-character prefix plan two cards; a replay plans none.
- [x] A waiting job puts the card in Needs you, survives past the work limit, resumes, and fails at the budget with
      one notice; a restart starts no second job.
- [x] An http gateway is refused without the switch, refused at a public address without `any`, and shown as
      unencrypted; a product the notifier does not serve never notifies.
- [x] TC-601 to TC-616 are registered.
