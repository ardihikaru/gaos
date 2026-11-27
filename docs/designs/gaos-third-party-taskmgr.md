# TaskMgr as a product-bound third-party platform

> **Status:** Approved (revision 2) — the operator delegated the decisions on this design (2026-11-27)
> **Author:** the GAOS team
> **Round 1:** `docs/designs/reviews/gaos-third-party-taskmgr-review-1.md`
> **Related:**
> - `docs/designs/gaos-external-tasks-and-pr-bundles.md` (D-100: bundles, the missing-code alert, the test
>   planner; D-101: notices)
> - `docs/designs/gaos-pr-agent-flows.md` (D-088: contacts, WhatsApp)
> - TaskMgr's own design, `taskmgr-grpc-integration-grant.md` in TaskMgr's repository, for the narrow grant and
>   TLS
>
> **Decision:** D-104; answers OQ-41
> **Numbering:** Release B holds D-103, BR-62, TC-819–TC-838 and v0.65.0/v0.65.1, so this design takes
> D-104, BR-63, OQ-44, OQ-45, TC-839–TC-849 and v0.66.0.
> **Baseline:** `main` at `8e46cf6` (v0.64.0). Paths are relative to the repo root. `TM/` is TaskMgr's
> application directory in its own repository.
> **Placeholders (D-085):** `<taskmgr-addr>` is TaskMgr's private IP and port, `p-N` the bound GAOS product,
> and `<CODE>` its TaskMgr product code.

## 1. Problem

A WhatsApp notice reaches a person only when an operator typed their GitHub login and number into GAOS's
contact registry (`resolveDestination`, `dashboard/contacts.go:225`). Every miss goes to the pinned group. On
top of that, three gaps:

- A PR's task-manager code (`parsePRTemplate`, `dashboard/prtemplate.go`) is accepted on its shape alone.
- Nobody in particular hears that a bundle's test plan is ready.
- The task in the task manager never learns that its pull requests finished.

TaskMgr, the team's task manager, already holds these facts.

- **People** (`TM/proto/taskmgr/v1/user.proto` `User`): `id`, `username`, `display_name`, `role`,
  `whatsapp`, `whatsapp_active`, `github_login` and `github_checked`.
- **Tasks** (`TM/proto/taskmgr/v1/task.proto` `Task`):
  - The id is the code the PR template carries, `<CODE>-<type>-<seq>` (`TM/internal/domain/task/neo4j_repository.go:625`).
  - `product_code` is field 10, `status` field 5, `is_deleted` field 37.
  - `developer`, `qa` and `qc` are `UserRef`s (fields 20–22).

TaskMgr serves gRPC on a private address, and its REST API leaves numbers and logins out on purpose
(`TM/internal/handler/master_users_rest_api.go:12-26`).

## 2. What changes

A GAOS product can be **bound** to one third-party platform of a kind. The only kind today is `taskmgr`.
Each instance allows at most one bound product per kind. A product that is bound and ready gets:

| # | Use | Trigger |
|---|---|---|
| 1 | **Task-code validation.** A code TaskMgr does not know, a deleted or archived task, or a task of another TaskMgr product gets one `unknown-task-code` alert. | each poll of an open, templated row; re-checked at most every 30 minutes |
| 2 | **`bundle:planned`.** One WhatsApp line to the bundle task's QA when its test plan has cases. | the test-planner card ends DONE |
| 3 | **Login routing.** A notice whose login has no GAOS contact goes to the TaskMgr person with that checked GitHub login, before the group. | every notice of the product |
| 4 | **Write-back.** One comment on the TaskMgr task once the bundle's test plan is done. | once per DONE |
| 5 | **Directory.** The Integrations view shows counts for everyone and the per-person table to auto-fix admins. | refreshed every 15 minutes |

### 2.1 Glossary

- **Ready** (`taskmgrReady(m)`) holds when all of these do:
  - the client settings (§3.3) are usable;
  - the circuit breaker is closed (§4.3);
  - the product has an enabled `taskmgr` binding;
  - the product is in `GAOS_PR_TEMPLATE_PRODUCTS` (`dashboard/bundles.go:17-27`), without which no code is
    parsed.
- **Notifies** (`taskmgrNotifies(m)`): ready, and WhatsApp covers the product (`wa.covers`).
- **Person:** a cached TaskMgr `User`.
- **gRPC frame:** a flag byte, a 4-byte big-endian length, then a protobuf message.
- **The QA tool:** the separate service that receives the test plan's case proposals (D-100).

## 3. The binding

### 3.1 Data

`Master` (`dashboard/model.go:386`) gains a field. Its type:

```go
ThirdParty []ThirdPartyLink // D-104, at most one entry per Kind

type ThirdPartyLink struct {
	Kind        string `json:"kind"`        // closed set thirdPartyKinds = {"taskmgr"}
	Enabled     bool   `json:"enabled"`
	ProductCode string `json:"productCode"` // ^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$
}
```

It is stored as `props["thirdParty"] = jsonList(m.ThirdParty)` beside `props["autoFix"]`
(`dashboard/store_arcade.go:925`). It is read back through a new `ThirdParty json.RawMessage` column, added
next to `AutoFix` in the load row and the SELECT (`store_arcade.go:1598`, `:1604`), and `decodeInto`. There is
no DDL: it is a Product document like `autoFix` (D-009). `Master.Clone` copies the slice.

### 3.2 Saving it

The route is `POST /products/{id}/third-party`. It is registered beside `/products/{id}/autofix` and gated by
`autoFixAdmin` (`dashboard/handlers_autofix.go:17`): no session means 401, a non-admin gets 403. The form
fields are `kind`, `enabled` (`formBool`) and `productCode`.

`SetThirdParty(ctx, productID, link, actor)`:

1. Validates the kind and the code, answering 422 through `reject`.
2. If another product holds an enabled binding of this kind, answers the new sentinel `ErrConflict`, which the
   handler maps to **409**, naming that product's id.
3. Replaces the product's entry of that kind. Disabling keeps the entry with `Enabled=false`; nothing is
   deleted.
4. Emits `ThirdPartyConfigured` with `EventPayload{Actor, Rule: "third-party-config", Reason:
   "kind=taskmgr enabled=<b> code=<CODE>"}`.
5. Clears the validation cache (§5.1).

The event joins the catalog in the same commit:

- `dashboard/events.go`, beside `AutoFixConfigured` (`:158`);
- `dashboard/fold.go:441`, the timeline-only list;
- `dashboard/events_test.go:39`;
- `artifacts/v1/gaos-arcadedb-model.html` §08;
- `pustaka/docs/architecture/graph.html`, the event list.

### 3.3 Instance settings

| Variable | Rule |
|---|---|
| `GAOS_TASKMGR_GRPC_ADDR` | `ip:port`, an IP literal. Empty means off. |
| `GAOS_TASKMGR_CA_FILE` | The PEM certificate TaskMgr serves, pinned as the only root. The server name checked is the IP. Required unless the IP is loopback, which tests use; a non-loopback address without it is refused (`TaskMgr is reached over TLS only`). |
| `GAOS_TASKMGR_AUTH_FILE` | A regular file with no group or other permission bits. It holds one line, `username:password`. |

The credential file in prod is `deploy/stacks/prod.pr-taskmgr-auth`, mounted at `/run/secrets/pr-taskmgr-auth`.
The certificate is `prod.pr-taskmgr-ca.pem`, mounted at `/run/secrets/pr-taskmgr-ca.pem`. Both names match the
`*.pr-*` class, so the existing secret guards cover them with no new rule:

- the `.gitignore` entry;
- the tracked-file check (`deploy/check-stacks.sh`);
- the tag-worktree link loop and the required-files list (`deploy/rebuild-restart.sh`).

An empty credential file means off.

**The TaskMgr account** is a rest-client with the **`GRPCIntegration`** grant, never `GRPCAdmin`. That grant
allows only `ListUsers`, `GetTask`, `ListComments` and `AddComment`, and only on the client's TaskMgr products
(TaskMgr's design). The account has a 1-year expiry and access to product `<CODE>` only.

## 4. The client (`dashboard/taskmgr.go`, `dashboard/protowire.go`)

The dashboard is stdlib-only (`dashboard/go.mod`). gRPC is used because TaskMgr's REST API does not return
numbers or logins (§11).

### 4.1 Transport

- An `http.Transport` with `Protocols` set to HTTP/2 only (`SetHTTP2(true)`) and `TLSClientConfig{RootCAs:
  pinned, ServerName: <ip>, MinVersion: TLS 1.3}`.
- For a loopback test address only, it uses `SetUnencryptedHTTP2(true)` and no TLS.
- `DialContext` refuses any resolved IP other than the configured one.
- Redirects are refused.

### 4.2 Calls

A call is `POST https://<taskmgr-addr>/taskmgr.v1.<Service>/<Method>` with the headers `content-type:
application/grpc`, `te: trailers` and `authorization: Basic …`.

- **Request:** the body is one frame.
- **Answer:** read whole and capped at 1 MiB. It must be exactly one uncompressed frame; anything else counts
  as unavailable and is never decoded.
- **Status:** `grpc-status` from the trailers (or from the headers, for a trailers-only answer):

| Status | Class |
|---|---|
| `0` | ok |
| `5` | `errNotFound` |
| `7`, `8`, `16` | `errAuth` |
| `3` | `errInvalid` (not retried) |
| anything else, a transport failure or a deadline | `errUnavailable` |

- `grpc-message` is never logged.
- **Deadlines:** 3 seconds on the poll path; 10 seconds for the directory and the write-back.

`protowire.go` builds varints with `encoding/binary` (`AppendUvarint` / `Uvarint`). It adds tags,
length-delimited fields, and skipping of unknown fields of every wire type.

| Call | Request | Read |
|---|---|---|
| `UserService/ListUsers` | `active` (2) = true, `page` (4) = {1: page, 2: 100} | `users` (1): 1 `id`, 2 `username`, 3 `display_name`, 4 `role`, 11 `active`, 14 `whatsapp`, 15 `whatsapp_active`, 16 `github_login`, 19 `github_checked`; `page` (2) → 3 `total` |
| `TaskService/GetTask` | 1 `task_id` | `task` (1): 1 `id`, 5 `status`, 10 `product_code`, 21 `qa` → `UserRef` 1 `id`, 37 `is_deleted` |
| `TaskService/AddComment` | 1 `task_id`, 2 `text` | status only |

### 4.3 The circuit breaker

- **`errAuth`** opens the breaker. It stays open until the credential file's modification time changes, or for
  1 hour, whichever comes first. The Integrations status is then *Unusable*, with the reason
  `TaskMgr refused the credential`.
- **Three consecutive `errUnavailable`** open the breaker for 1 minute. Each further failure doubles the time,
  up to 30 minutes.
- **While the breaker is open, nothing is called.** No code counts as checked and no notice uses TaskMgr;
  every use falls back to today's behaviour.

## 5. The four uses

### 5.1 Task-code validation

This runs inside `pollRow`'s template hook (`dashboard/fixprpoll.go`, the `D-100` block), after
`ApplyBundleLinks`. It applies when `taskmgrReady(m)` holds and the row's `TaskCode` is not empty.

1. **The cache.** `taskChecks` is an LRU of 512 entries keyed by `(productCode, code)`:

   ```go
   type taskCheck struct {
	Result string    // "ok" | "unknown"
	QAID   string    // TaskMgr id of the task's QA, for §5.2
	At     time.Time
   }
   ```

   An entry is fresh for 30 minutes, and `SetThirdParty` clears the cache. A miss calls `GetTask(code)` with
   the 3-second deadline, outside any store lock. The result is:
   - **`errNotFound`** → `unknown`;
   - **`is_deleted`, a status of `DELETED` or `ARCHIVED`, or a `product_code` different from the binding's** →
     `unknown`;
   - **anything else ok** → `ok`;
   - **any other error** → no entry. The code stays unchecked (never assumed valid) and is retried on a later
     poll, subject to the breaker.

   Codes are matched exactly as written: TaskMgr ids are case-sensitive.
2. **The alert.** It fires when all of these hold. There is **no** `BundleKey` condition: every coded row has
   one.

   ```go
   res == "unknown" && row.TemplateSeen && row.State != PRStateDraft && !prFinal(row.State) &&
   now.Sub(time.Unix(row.OpenedAt, 0)) >= alertGraceCode &&
   (row.Alerted == "" || row.Alerted == alertNoTaskCode)
   ```

   When it fires, in order:
   1. Write `alerted = unknown-task-code` (`SetPRAlerted`).
   2. Post one COMMENT: `GAOS: the task-manager code <code> is not an open task of this product in TaskMgr.
      Check "Kode (jika dari task manager)".` The one text serves both cases, so it does not reveal which tasks
      exist.
   3. Send one `NotifyPR` line, kind `pr:unknown-task-code`: `A pull request names a task-manager code TaskMgr
      does not know`.

   The new kind joins `notifyKinds` (`contacts.go:67`) and the generated `ValidContact` error text.
3. **Bundles are unchanged.** A row with an unknown code keeps its `taskmgr:` bundle. Editing the code
   supersedes the key before planning (D-100 §7.2), and the alert is not repeated.

### 5.2 `bundle:planned`

`testPlanner.afterRun` (`dashboard/testplanner.go:177`) handles it, only after `ResolvePRCard(… StatusDone …)`
returns nil, at `:238` and `:262`. There must also be at least one created or linked case, a `taskmgr:` bundle
key, and `taskmgrNotifies`. Then:

1. A fresh `GetTask(code)` (10-second deadline, no cache) must give `ok` (§5.1).
2. If the task's `qa.id` is a person in the directory with a usable number, send one line to that number:
   `A test plan is ready: <n> draft case(s), <m> link(s)`, kind `bundle:planned`, through `Notify` on the card.
   Otherwise the line goes to the group.

The new kind joins `notifyKinds`. `bundle:failure` stays group-only, as today.

### 5.3 Login routing

`resolveDestination(book, productID, login, kind)` keeps its body and its two call sites (`notify.go:81`,
`:113`). A second step follows it, only when it answered the group **and no contact exists for the login at
all**:

```go
dest, alias := resolveDestination(book, productID, login, kind)
if dest == "" && n.people != nil && login != "" && !book.Has(productID, login) && taskmgrNotifies(m) {
	if p, ok := n.people.byLogin(login); ok && kind != "e2e:approval" && !strings.HasPrefix(kind, "bundle:") {
		dest, alias = p.jid(), "tm:"+sanitizeIDPart(p.Username)
	}
}
```

- **A contact that exists but filters a kind out still sends it to the group.** Its opt-out stands.
- `e2e:approval` and every `bundle:` kind keep their group routing.
- `notifier` gains `people *taskmgrPeople` and `masters func(id string) (Master, bool)`, both wired in `main.go`
  where `nt` is built.

### 5.4 Write-back

This follows the same trigger as §5.2 (after a successful DONE, a `taskmgr:` key, `taskmgrReady`), with the same
fresh `GetTask` that must answer `ok`. Then one `AddComment(code, text)`:

```
GAOS: the pull requests of this task are finished: <m> merged, <c> closed without merging.
<up to 10 merged PR URLs, then "… and k more">
Test plan: <n> draft case(s) created, <l> linked, <r> refused: <up to 20 case codes>.
[gaos bundle <bundleKey> · card <cardID>]
```

The rules for the text:

- Case codes are kept only when they match `apiCaseCodePattern` (`dashboard/api_cards.go:96`).
- The text is cut to 1500 runes before the marker line, so the marker survives.
- It is **best-effort, once per DONE transition**: `afterRun` runs once per run (`testplanner.go:178`). A
  failure is logged by class, never fails the card, and is not retried.
- A `pr:` bundle writes nothing.
- The marker is for people reading the task, not a guard; GAOS does not read comments back.

## 6. The directory

`taskmgrPeople` runs on its own goroutine. It refreshes every 15 minutes **only while a binding is enabled**
(and at the first one). It pages `ListUsers` up to `page.total` or 20 pages. It keeps:

- **`byID`**;
- **`byLogin`**: lower-case `github_login` of `github_checked` people. A login held by more than one checked
  person is dropped and counted as a conflict.
- **Counts:** people, GitHub-linked, usable numbers and conflicts;
- **The last success**, and the last error class.

The refresh also handles two edge cases:

- **A failed refresh keeps the previous directory**, shown as `stale since <time>`.
- **Disabling the binding empties the directory.**

A number is usable when `whatsapp_active` is true and it matches `^[1-9][0-9]{6,14}$`. It is sent as
`<digits>@s.whatsapp.net`.

**BR-63:** numbers live only in this memory. They are never stored, logged, put in a packet, sent in Brain
Town's feed, or written into an event. The one stored identifier is the alias `tm:<sanitized username>` in
`ContactNotified`. Pages show numbers only through `maskDestination` of the raw digits.

## 7. The Integrations view

A new block follows the pull-request flow's block in `{{define "bo-integrations"}}`
(`dashboard/templates/backoffice.html:945`).

- **The heading:**
  - the text "TaskMgr";
  - a status chip from `integChips` (`dashboard/integrations.go:225`);
  - the marks `chip need-opt` *Optional* and `chip third-party` *Third-party*. The second is a new class in
    `app.css`, next to `.need-opt`. Neither is a status label, so `chipLabels` is unchanged.
- **The status**, in order of precedence:
  - *Unusable* (bad), when the settings are invalid or the breaker is open on `errAuth`;
  - *Degraded* (warn), when the breaker is open on unavailability, the directory is stale, or the product is
    bound but not in the template list;
  - *Connected* (ok), when it is ready and refreshed;
  - *Off* (off), otherwise.
- **The rows:** the bound product id and its code; the address host (`urlHost`); the last refresh; and the
  counts (people, GitHub-linked, usable numbers, login conflicts).
- **The people table** (name, role, GitHub login, masked number) is rendered **only** for a session that
  `autoFixAdmin` admits.
- **The binding form,** for admins: a product `<select>`, the code field and the enabled box. It posts to §3.2.
- **The popover `ip-taskmgr`:** "What it does", "When connected", "When disconnected", and a requirement line.

## 8. Security

| Boundary | Rule |
|---|---|
| **Credential** | A `*.pr-*` file covered by every guard, never env, logs or pages. The account holds only `GRPCIntegration`: four methods, one TaskMgr product, 1-year expiry. |
| **Transport** | TLS 1.3 to a pinned self-signed certificate, so the WireGuard hub relays ciphertext only. The configured IP is the only one dialled. |
| **Personal data** | BR-63 (§6). The table is admin-only; everyone else sees counts. |
| **Untrusted input** | A code reaches TaskMgr only after the parser's pattern. Comment text is built from URLs GAOS derived, counts, and pattern-filtered case codes. One alert text for both failure cases. |
| **Availability** | The circuit breaker (§4.3). A 3-second deadline on the poll path. Nothing calls TaskMgr while the breaker is open. |
| **Binding** | Admin-gated, one product per kind, an event for every change. |

## 9. Change list

| File | Change | Phase |
|---|---|---|
| `dashboard/protowire.go` (new), `dashboard/protowire_test.go` (new) | the codec | 1 |
| `dashboard/taskmgr.go` (new), `dashboard/taskmgr_test.go` (new) | settings, transport, calls, the breaker, `taskmgrPeople`, `taskChecks` | 1 |
| `dashboard/model.go`, `dashboard/thirdparty.go` (new), `dashboard/thirdparty_test.go` (new) | `ThirdParty`, `SetThirdParty` (Mem), `taskmgrReady` / `taskmgrNotifies`, `ErrConflict` | 2 |
| `dashboard/store.go`, `dashboard/store_arcade.go`, `dashboard/store_conformance_test.go` (readOnly table) | the interface, Arcade, readOnly, `writeMaster`, the load | 2 |
| `dashboard/handlers_thirdparty.go` (new), `dashboard/main.go` | the route; wiring the client, the directory, the notifier and the poller | 2 |
| `dashboard/events.go`, `dashboard/fold.go`, `dashboard/events_test.go`, `artifacts/v1/gaos-arcadedb-model.html`, `pustaka/docs/architecture/graph.html` | `ThirdPartyConfigured` | 2 |
| `dashboard/fixprpoll.go`, `dashboard/bundles.go` | validation and the alert | 3 |
| `dashboard/contacts.go`, `dashboard/notify.go`, `dashboard/notices_test.go` | `notifyKinds` (`pr:unknown-task-code`, `bundle:planned`), `ContactBook.Has`, the login step | 3 |
| `dashboard/testplanner.go` | `bundle:planned` and the write-back | 3 |
| `dashboard/integrations.go`, `dashboard/view.go`, `dashboard/templates/backoffice.html`, `dashboard/static/app.css`, `dashboard/integrations_test.go` | the block (the seventh info button, `integNames`) | 4 |
| `dashboard/taskmgr_flow_test.go` (new) | §10's flow tests | 3 |
| `deploy/stacks/compose.prod.yaml`, `deploy/rebuild-restart.sh`, `deploy/check-stacks.sh` | the two mounts and three variables; the files join the `prod.pr-*` required list; the read-only mount asserted | 5 |
| `README.md`, `pustaka/docs/infra/environments.html` | the variables | 5 |
| `pustaka/docs/integrations/taskmgr.html` (new), `pustaka/docs/assets/toc/integrations.js`, `pustaka/docs/integrations/pr-agents.html`, `pustaka/docs/integrations/pull-requests.html`, `pustaka/docs/integrations/overview.html` | the page, the notice routing and the data flow | 5 |
| `pustaka/docs/product/{brd,decision-log,open-questions,qa-test-plan}.html`, `pustaka/docs/assets/toc/product.js`, `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js`, `.claude/rules/qa-test-plan.md`, `deploy/check-workspace.sh` | D-104, BR-63, OQ-41 answered, OQ-44 and OQ-45, TC-839–TC-848, counts, v0.66.0 | 5 |

## 10. Tests (TC-839 to TC-848)

| TC | Test | File |
|---|---|---|
| 839 | `TestProtowireRoundTrip`: varints, strings, nested messages, unknown fields of each wire type skipped | `protowire_test.go` |
| 840 | `TestTaskMgrClientCalls`: an in-test h2c gRPC server; OK, 5, 7/8/16 → auth, 3, trailers-only, oversized, two frames, deadline; the Basic header sent; `grpc-message` not logged | `taskmgr_test.go` |
| 841 | `TestTaskMgrConfig`: a non-loopback address without a CA is refused; a hostname is refused; an open auth file is refused; another resolved IP is refused at dial | `taskmgr_test.go` |
| 842 | `TestTaskMgrBreaker`: auth latches until mtime change or 1 h; unavailable backs off, then doubles; nothing is called while open | `taskmgr_test.go` |
| 843 | `TestTaskMgrPeopleDirectory`: paging, checked-only logins, duplicate logins dropped, usable numbers, a stale directory kept, emptied on disable | `taskmgr_test.go` |
| 844 | `TestThirdPartyBinding`: kind and code checks, 409 on a second product, the event, the admin gate, cache cleared, Arcade props (fake graph) | `thirdparty_test.go` |
| 845 | `TestTaskCodeValidationAlert`: unknown, deleted and other-product alert once each; a row already `no-task-code` is alerted; breaker open → none; ok → none; the cache spares a second call | `taskmgr_flow_test.go` |
| 846 | `TestNotifyLoginThroughTaskMgr`: no contact → the TaskMgr person; a filtering contact → the group; `e2e:approval` → the group; the alias, with no number in events | `taskmgr_flow_test.go` |
| 847 | `TestTestPlanReadyAndWriteBack`: `bundle:planned` to the QA; one comment within 1500 runes with the marker; other-product → nothing; `pr:` bundle → nothing; case codes filtered | `taskmgr_flow_test.go` |
| 848 | `TestIntegrationsShowTaskMgr`: the block, its status order, counts for everyone, the table for admins only | `integrations_test.go` |

There are 10 dashboard tests, so the Go test count rises by 10.

## 11. Alternatives

- **TaskMgr REST:** rejected. `/master/users` omits numbers and logins on purpose.
- **REST for tasks and gRPC for users:** rejected. It means two auth paths and two error maps for four calls.
- **gRPC with a generated library:** rejected. It breaks the stdlib-only rule.
- **Copying TaskMgr people into GAOS contacts:** rejected. It would store numbers in GAOS's graph, against
  BR-63.
- **An env-only binding:** rejected by the operator. The binding is a product setting with an event.

## 12. Risks

| Risk | Mitigation |
|---|---|
| A TaskMgr outage | The breaker; every use falls back to today's behaviour. |
| A wrong or rotated credential | The auth latch; the status shows *Unusable*. |
| The certificate expires (3 years) | Shown in the client's TLS error class. Renewal: replace both files and restart. |
| A wrong code on a real PR | One comment; the author fixes it. |

## 13. Non-goals

- Changing TaskMgr task or release status.
- Creating tasks.
- Writing contacts into TaskMgr.
- A second platform kind.
- mTLS.

## 14. Registers

| Register | Entry |
|---|---|
| D-104 | Product-bound third-party platforms; TaskMgr over gRPC with TLS and the narrow grant; the five uses. |
| BR-63 | §6. |
| OQ-41 | Answered by D-104. |
| OQ-44 | Should a second platform kind share the binding? (open; the model allows it) |
| OQ-45 | Should the reviewer deny-list name every `deploy/stacks/prod.*` secret, and be enforced by the harness? (escalated from SEC-10) |

## 15. Exit checklist

1. **Phases 1–4:** `cd dashboard && go test ./... && go test -race ./...`.
2. **Phase 5:**
   - `pustaka check`, `check-workspace.sh` and `check-stacks.sh` all green;
   - the ArcadeDB scratch run is green.
3. **Live** (the instance's own values, from `deploy/local/`):
   1. Bind `p-N` to `<CODE>`. The block shows *Connected* with counts.
   2. A test PR with an unknown code gets one comment and one line.
   3. A manual-review label on the operator's own PR, with no GAOS contact, reaches the operator's own number
      (`tm:` alias).
