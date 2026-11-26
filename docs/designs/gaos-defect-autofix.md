# Defect triage and auto-fix — a defect becomes a labelled task, an easy one becomes a draft pull request

> **Amended by D-092** (`docs/designs/gaos-pr-overview.md`): a fix record that a retarget replaced, or has pending, never moves its defect, and the fix pull request gains `Base`, `RetargetedFrom` and `RetargetedTo`; the poller also follows review and QA-tool rows. The state table of §5.2 is unchanged for every other record.
>
> **Status:** Implemented (revision 3; round 3 not run, approved by delegation) (review rounds 1 and 2: `docs/designs/reviews/gaos-defect-autofix-consolidated-1.md`)
> **Author:** the GAOS team
> **Reviewers:** the `gaos-goldfish-*` agents (`docs/rules/gaos-design-review.md`)
> **Policy:** `docs/rules/gaos-design-review.md`
> **Related:** `docs/designs/gaos-pr-agent-flows.md` (D-088; §7 and §14 are amended here), the QA tool's
> `docs/designs/QA_DEFECT_TRIAGE.md` (the sending half, in `qa-automation-tools`)
> **Names:** every organisation, repository, product and defect id below is invented (`acme-org/sample-service`,
> `DEF-101`), as D-085 requires. A product's real default policy lives in the private seed and the back office.

## 1. Problem

The automation repository pins known defects as expected failures: `test.fail()` plus an annotation of type `defect`
and an id such as `DEF-101`. Playwright reports an expected failure as a **pass**, so the QA tool stores `pass` and
GAOS never hears about the defect. An unexpected failure is reported, but with no defect id, no automation type and
no idea which application repository to look in. Nobody classifies how hard a defect is to fix, so a one-line
validation bug waits in the same queue as a billing problem.

This design makes GAOS act on a defect: classify it from the product's own policy, and fix the easy ones without a
person starting each fix, ending in a **draft pull request** that a person reviews. It is deliberately narrow: only a
backend-only defect, only three kinds of fix, only a few files, only with an explicit per-product switch and caps.

## 2. What GAOS will do

| Step | What happens | Who |
| --- | --- | --- |
| 1 | The QA tool reports a defect of a case: a pinned expected failure, an unexpected failure, or a pinned failure that now passes. | QA tool |
| 2 | GAOS records it (`DefectReported`) and plans a **triage** card, label `defect:triage`. | GAOS |
| 3 | The `triager` agent reads the product's `defect-triage` policy, the facts and the source files GAOS found, and answers `easy`, `medium` or `complex`. | agent |
| 4 | GAOS re-checks the verdict (§7.2), records it (`DefectTriaged`) and plans a **fix** card **born with** its label `defect:easy`, `defect:medium` or `defect:complex`. The triage card keeps `defect:triage` and ends `DONE`. | GAOS |
| 5a | `easy`, backend-only, switch on, daily cap left: the fix card is dispatched with no operator move (§6.3). | GAOS |
| 5b | `easy` with the switch off or the cap spent: the card **stays `READY`** and is picked up when that changes. `medium`, `complex`, end-to-end, regression: the card is born `BLOCKED` (`complex` also sends a notice). | GAOS |
| 6 | The `fixer` agent answers with edits and tests. | agent |
| 7 | GAOS applies the edits, enforces the caps, creates a branch and a **draft** pull request through the GitHub API, labels it, posts the before/after as a comment. | GAOS |
| 8 | GAOS sends one WhatsApp line with the link; the fix card ends `DONE`. | GAOS |
| 9 | GAOS follows the pull request (draft, ready, merged, closed) and shows it on the Auto-fix view. | GAOS |

## 3. Vocabulary

- **Automation type** — `be` (level `GQL` or `API`, spec under `tests/api/graphql/<service>/` or
  `tests/api/rest/<service>/`) or `e2e` (level `WEB` or `E2E`, spec under `tests/web/<app>/`). The QA tool derives
  it; the automation repository's catalog format does not change.
- **Backend kind** — `graphql` or `rest`, from the spec path; empty for `e2e`.
- **Target repositories** — the application repositories a case exercises, resolved by the QA tool from the spec
  path's service or app directory (several allowed). **GAOS never trusts a repository named in a payload**: it keeps
  only those `productOwns` accepts; if none is left the fix card is born `BLOCKED` `autofix-no-repo`.
- **Operations** — up to 5 GraphQL operation or REST route names the spec calls, extracted by the QA tool. Only
  **search terms** (§7.1); they never widen what a fix may touch.
- **Defect kind** — `xfail` (pinned, expected failure, `DEF-nnn` id), `fail` (unexpected failure, keyed by case id),
  `xpass` (a pinned defect whose test now passes: the fix has landed).

## 4. The QA tool → GAOS contract

### 4.1 The route

`POST /api/v1/products/{id}/qa/defects`, bearer, the QA token and product scope of `qa/results`. A **new route**:
GAOS decodes strictly (unknown field = `400 bad-json`) and `qa/results` must not change. It reads its body through a
decoder that takes its limit as a parameter (`apiDecodeLimit`), **256 KiB**, answering `413 too-large` naming that
limit; at most **50 items**; the QA tool splits larger batches. `source` is `push` (the only value); `runAt` is Unix
seconds.

```
{"source":"push","items":[{
  "caseId":"TC-SAMPLE-008", "defectId":"DEF-101", "kind":"xfail",
  "level":"GQL", "automationType":"be", "backend":"graphql",
  "targetRepos":["acme-org/sample-service"], "operations":["editSampleRecord"],
  "spec":"tests/api/graphql/sample-service/records/edit.spec.ts",
  "title":"a negative quantity is accepted and stored",
  "excerpt":"<failure output or the annotation's reason, at most 4 KiB>",
  "runRef":"run-…", "runAt":1764060000}]}
```

Per item (a bad item is **rejected with a reason**, the rest apply, as `qa/results` does): `caseId` is a case of the
product's catalog; `kind` is `xfail`, `fail` or `xpass`; `defectId` matches `^DEF-\d{1,6}$`, required for `xfail` and
`xpass`; `automationType` is `be` or `e2e`; `backend` is `graphql`, `rest` or empty; `level` is `API|GQL|WEB|E2E`;
`spec` passes the existing spec-path check; `targetRepos` entries are `owner/repo`, at most 8; `operations` entries
match `^[A-Za-z_][A-Za-z0-9_.\-/]{0,80}$`, at most 5; `title` at most 200 characters; `excerpt` at most 4 KiB.
Answer `200 {"applied":n,"unchanged":n,"rejected":[{caseId,reason}]}`.

**Everything in an item is untrusted.** `title` and `excerpt` get BR-53's stripping (backticks, angle brackets and
line breaks removed), are capped and screened by the egress detectors (a hit drops the text, keeps the item, sets
`excerptWithheld`). They reach an agent only as a **context part**, never as a prompt, and never appear in an event,
a card title or a log line. A card title is the defect id plus the screened title by `btTitle`'s rule.

**Limits.** At most **50 new defect keys per product per UTC day** (counted from the `Defect` records' `firstSeen`;
beyond that an item is rejected `defect-limit`) and, as for every write route, **one write in flight per token**
(`beginWrite`, `429 write-in-flight`); there is no per-second rate limit and none is claimed.

### 4.2 What GAOS does with it

- **Keys.** `<product>/<defectId>` for `xfail`/`xpass`, `<product>/<caseId>` for `fail`. A repeat updates `lastSeen`
  and `runRef` and plans nothing.
- **A `fail` is linked to a later `xfail`.** When an `xfail` arrives for a case that already has an open `fail`
  record, the `fail` record is **superseded** (`supersededBy` = the new key; its open cards are canceled, rule
  `autofix-superseded`) and the new record takes over.
- **`xpass`** moves the record to `fixed` from any non-final state and **cancels its open cards** (rule
  `autofix-fixed`); it plans no card.
- **Regression.** A `fail` report for a case whose record is `fixed` or `closed` is looked up **by `caseId` first**
  (any key space). It returns that record to `open`, raises its **generation** and plans a new triage card whose fix
  card is born `BLOCKED` (never auto-fixed). It does not spend a new-key slot; it emits `DefectStateChanged`.
- **BR-54 is amended honestly (D-091).** BR-54 says "a QA token can never create dispatchable agent work". This route
  lets a QA token **plan** cards that **may be dispatched**, only when an operator has switched the product's
  auto-fix on (a standing, recorded consent) and within the caps. With the switch off the cards simply wait (§6.3).
  A QA result still never moves an existing card.

## 5. Records, states, cards, vocabularies

### 5.1 The defect record

Vertex `Defect` (`(tenantId, id)` unique): `id`, `productId`, `defectId`, `caseId`, `kind`, `automationType`,
`backend`, `targetRepos`, `operations`, `spec`, `title`, `excerpt`, `excerptWithheld`, `firstSeen`, `lastSeen`,
`runRef`, `gen`, `difficulty`, `class`, `reason`, `state`, `supersededBy`, `triageTask`, `fixTask`, `fixPR`.
**Retention:** a daily job in the poller's loop (§9.1) empties `excerpt` 30 days after `lastSeen` (a silent field
clear). The stored prompt and context slice that carried it (BR-41) **outlive** that: the 30 days bound the record,
not the audit trail.

### 5.2 States and their events (BR-02)

| From | To | Trigger | Event |
| --- | --- | --- | --- |
| — | `open` | a report with a new key | `DefectReported` |
| `open` | `triaged` | the verdict is accepted | `DefectTriaged`, then `DefectStateChanged` |
| `triaged` | `fixing` | a draft pull request exists | `FixPROpened`, then `DefectStateChanged` |
| `fixing` | `fixed` | its pull request merged | `FixPRStateChanged`, then `DefectStateChanged` |
| `fixing` | `triaged` | its pull request closed unmerged; a person-only fix card is planned `BLOCKED autofix-pr-closed`, `gen`+1 | `FixPRStateChanged`, then `DefectStateChanged` |
| `open`, `triaged`, `fixing` | `fixed` | an `xpass` report | `DefectStateChanged`, open cards canceled |
| any | `closed` | an operator: `POST /products/{id}/defects/{defectId-or-caseId}/close` (the product comes from `{id}`; a record of another product is `404`) | `DefectStateChanged` (actor alias) |
| `fixed`, `closed` | `open` | a regression `fail` report | `DefectStateChanged` |

### 5.3 Cards

All on the product's PR workflow `wf-pr-<product>`, kind `implement`, exempt from BR-46/BR-47 as D-088's cards are.
Dedup keys carry the generation: `defect:<key>:<gen>:triage` and `defect:<key>:<gen>:fix`, so a regression or a
second attempt plans a fresh card. **Defect cards are excluded from the PR flow's open-card cap**
(`openPRCardsLocked` ignores keys that start `defect:`; they have their own caps, §6).

| Card | Role | Labels | Born | Ends |
| --- | --- | --- | --- | --- |
| triage | `triager` | `defect:triage` | `PENDING` → `READY` (the scheduler's readying) | `DONE` when the verdict is accepted (rule `autofix-verdict`); `FAILED` on an unreadable reply |
| fix, `easy`, backend-only | `fixer` | `defect:easy` | `PENDING` → `READY` | `DONE` when the draft pull request exists; `BLOCKED` with a reason; `FAILED` `github-*` |
| fix, `medium` / `complex` / e2e / regression | `fixer` | `defect:medium` / `defect:complex` / `defect:easy` | `BLOCKED` `autofix-not-easy` | a person's move; the run ends in **Review** with its edits attached as an artifact, **never a pull request** |

**Waiting is `READY`, not `BLOCKED`.** A triage or easy fix card that cannot be dispatched because the switch is off
or a cap is spent **stays `READY`**; the Auto-fix view shows why ("auto-fix is off", "fix cap 3/3 until 00:00 UTC").
Nothing needs releasing: the dispatch loop (§6.3) picks it up the moment the condition changes. `BLOCKED` is for
reasons that need a **person**: `autofix-not-easy`, `autofix-no-repo`, `autofix-limits`, `autofix-egress`,
`autofix-ci-secrets`, `autofix-branch-exists`, `autofix-base-moved`, `autofix-unreadable`, `autofix-pr-closed`,
`autofix-search-failed`, `autofix-draft-unsupported`. It is never `AWAITING_INPUT`: BR-44 is untouched.

**Eligibility is decided from the record, not from a label.** A pull request may be opened only if the `Defect`
record has `difficulty == easy`, `automationType == be`, is not a regression, and `fixTask == this card`, **and** the
product's `Enabled` preconditions hold (§6.1) **whoever dispatched the card**. `SetTaskLabel` **refuses the `defect`
family** (`422`): only the planner sets it. A hand-moved `medium`/`complex` card, or an `easy` card of a product whose
preconditions fail, ends in Review with an artifact.

### 5.4 Closed vocabularies (they land together or not at all)

- **Roles:** `triager` (label "Defect Triager", room `qa`, `claude-opus-5`, agent `a-12`) and `fixer` (label "Defect
  Fixer", room `backend`, `claude-sonnet-5`, agent `a-13`). D-088 §14 listed "a fifth agent" as a non-goal; D-091
  amends it. `Task.role` regexp, `roles`, `roleOrder`, `agents`, and `contractsFor` selecting the block by role.
- **Labels:** family `defect` — `defect:triage`, `defect:easy`, `defect:medium`, `defect:complex`.
- **Events:** `DefectReported`, `DefectTriaged`, `DefectStateChanged`, `FixPROpened`, `FixPRStateChanged`,
  `AutoFixConfigured` — in the data-model document, `events.go`, the fold, `events_test.go`, `graph.html`.
- **Vertices:** `Defect`, `FixPR`, in a new `dashboard/schema/17-defects.sql` extracted from the data-model document
  (the DDL has 30 concrete vertex types today; it becomes 32, and `wantVertexTypes` follows). The BRD (25) and
  `graph.html` (28) were already stale; both are corrected to 32 in this change.
- **Move rule:** `MoveAutoFix = "autofix"` (§6.3). **Blueprint policy:** kind `custom`, key `defect-triage` →
  `.claude/rules/65-defect-triage.md` (drift gate BR-39, allowlist BR-42); `blueprint.go` does not change.

## 6. Configuration and dispatch

### 6.1 `Master.AutoFix` — structured, because GAOS enforces it

Not rendered into the Blueprint. Edited in the back office by `POST /products/{id}/autofix`, **restricted to the
accounts listed in `GAOS_AUTOFIX_ADMINS`** (a mode-600 file of addresses; anyone else gets `403`) until back-office
roles exist (OQ-37). Every change is an `AutoFixConfigured` event with the actor's **alias**, the new values (never
the template text) and the **kind of GitHub token in use**.

| Field | Meaning | Default / bound |
| --- | --- | --- |
| `Enabled` | the standing consent for automatic dispatch | off |
| `FixCap`, `TriageCap` | automatic dispatches per product per UTC day | 3 (max 10), 10 (max 50) |
| `AllowedGlobs` | the **only** paths an easy fix may edit (existing files); new test files are added separately | required |
| `ForbiddenGlobs` | extra paths never touched, on top of the built-ins | optional |
| `TestGlobs` | what counts as a test file; each must contain a test marker (`_test.`, `.test.`, `.spec.`, `/test/`, `/tests/`) and may not be `*` or `**` | required |
| `PRTemplate` | the pull-request body with named slots (§8.3); empty = the neutral default | optional |
| `CIConfirmed` | the operator confirms the target repositories' CI runs no secrets on `gaos/fix-*` (GAOS also checks, §8.3) | false |

`Enabled` is refused (`422`) unless `AllowedGlobs`, `TestGlobs` are valid and non-empty, `CIConfirmed` is true,
`PRTemplate` (if set) contains the slots `{{before}}` and `{{after}}`, and the write token is configured (§10).
GAOS always applies **built-in forbidden globs** on top of the product's: `**/*_gen.go`, `**/*.graphqls`,
`**/gqlgen.yml`, `**/migrations/**`, `**/*.sql`, `**/go.mod`, `**/go.sum`, `.github/**`, `**/Dockerfile*`,
`**/auth/**`, `**/*auth*`, `**/billing/**`, `**/tariff/**`, `**/payment/**`, `**/*.env*`, `**/*.pem`, `**/*.key`.
**An operation name is a search term and never widens `AllowedGlobs`.**

### 6.2 What the caps count, and what bounds spend

Automatic **dispatches**, not pull requests: a fix turn that later fails a cap still used a slot. Each automatic
dispatch stamps the task (`autoAt`, a persisted property) and the count for a product and UTC day is the number of
its tasks stamped that day. The Auto-fix view shows dispatches and pull requests side by side. **The caps are the only
spend bound for these cards**: the budget guard (BR-35) is not wired into any dispatch path today (the scheduler's
budget is `BudgetNormal` until M10.3), so this design does not claim it.

### 6.3 Dispatch — a new store method, not `RunOnce`

`GAOS_RUNNER` stays as it is for every other card (D-080, BR-56). Three changes:

1. **The scheduler never dispatches a `defect:*` card, in any mode.** `labelsAllowDispatch` refuses all four labels,
   so under `GAOS_RUNNER=on` too the switch and the caps are not bypassable. The scheduler still *readies* them.
2. **`dispatchNow` is refactored** (`dashboard/board.go`) into one internal function that takes the **move rule** and
   an optional **claim** callback run inside its locked sections: the operator's path passes `MoveDispatch` and no
   claim; the automatic path passes `MoveAutoFix` and a claim that, under the same lock as the `READY → DISPATCHED`
   move, re-checks the switch and the cap and stamps `autoAt`. So **every gate the operator's move applies applies**:
   readiness (BR-07), one live turn (BR-38), drift (BR-39), dependencies, and the egress pass (BR-32), which the
   function already runs between two locked sections.
3. **`Store.AutoDispatch(ctx, productID, taskID)`** wraps that function; `ArcadeStore` wraps it in `mutate`, so the
   `TaskMoved` is **persisted** exactly like an operator's move (calling it from `RunOnce` would lose the event: that
   path persists only through `publish`). A loop in `main.go` (`autoFixLoop`, every 15 s) lists each enabled
   product's `READY` `defect:triage` / `defect:easy` cards and calls `AutoDispatch`; a refusal because of the switch,
   the cap or a live turn leaves the card `READY`. D-091 amends BR-56 for these two labels.

## 7. The triage agent

### 7.1 The packet (built before dispatch, never under the store lock)

A function `buildDefectContext` runs in the planner and in the loop **before** `AutoDispatch`, outside any lock, and
stores the result on the `Defect` record (`context`, ≤ 96 KiB: head SHA, the files' paths, blob SHAs and text) so
the fix turn **reuses** it:

1. For each kept target repository GAOS reads the default branch's **head SHA** and searches for the operations
   (`GET /search/code?q=<op>+repo:<o>/<r>`), at most **10 searches in all** per defect, at most 8 hits each. A
   **search error** (a secondary-limit `403`, a `5xx`) is **not** "nothing found": it retries with the client's
   back-off, then blocks the card `autofix-search-failed`. An empty result is "nothing found".
2. It keeps at most **6** files and **64 KiB** in all, ordered by search rank then path, **after** the path rules
   (no `..`, not absolute, not built-in forbidden, not credential-shaped, each segment escaped separately), fetching
   each at the **pinned head SHA**; a path that no longer exists there is skipped.
3. If nothing is kept, the triager still answers but any `easy` is downgraded to `medium` (`autofix-no-files`).

The packet is the Blueprint (so `65-defect-triage.md`), the facts and excerpt as a context part, and those files.
**Private source thus goes to the model provider**: this widens BR-32's flow, recorded in D-091; a detector hit on a
fetched file **refuses the dispatch** (`autofix-egress`) and is never silently dropped.

### 7.2 The reply and the re-check

`triager` → `gaos-triage` (selected by role in `contractsFor`), then the handoff:

```gaos-triage
difficulty: easy | medium | complex
class: validation | format | min-value | business-rule | state-guard | auth | billing | schema | multi-repo | unknown
repo: owner/repo
files: path/one.go, path/two.go
reason: <at most 400 characters>
```

Parse errors (`triage-missing`, `triage-bad-value`, `triage-bad-repo`) fail the triage card. An `easy` is **checked
again by GAOS**: `class` is `validation`, `format` or `min-value`; `repo` is a kept target repository; `files` are a
subset of the packet's files and all match `AllowedGlobs`. Otherwise it is downgraded to `medium` and the reason is
in `DefectTriaged`. **`class` is advisory; the deterministic gate is the path set.**

## 8. The fix agent and the pull request

### 8.1 The reply — edits, not whole files

`fixer` → `gaos-fix`, JSON in the block with **plain strings** (no base64, no framing a file could break):

```gaos-fix
{"summary":"…","before":"<sample input and result today>","after":"<same input and result after>",
 "edits":[{"path":"pkg/x/validate.go","find":"if qty == 0 {","replace":"if qty <= 0 {"}],
 "newFiles":[{"path":"pkg/x/validate_test.go","content":"…"}]}
```

An edit's `find` must occur **exactly once** in the pinned file (else `fix-bad-edit`). The whole reply is at most
**64 KiB**, each `find`/`replace` at most 4 KiB, each new file at most 32 KiB (`fix-too-large`). Other parse errors:
`fix-missing`, `fix-bad-json`, `fix-bad-path`. All send the card to `BLOCKED` `autofix-unreadable`. There is **no
`executed` field**: the "after" is **always rendered as agent-reported, not run by GAOS** (the worker has no
checkout; a verified runner is a later change).

### 8.2 The caps GAOS enforces (not the agent)

- an edited path is an **existing** file in the packet, matches `AllowedGlobs`, no forbidden glob; a new file
  matches `TestGlobs`, is not forbidden, and **does not exist at the pinned head** (a tree lookup);
- every path relative, no `..`, no leading `/`, no NUL; at most **3 files**; at least one new or edited test file;
- changed lines = **added + removed** from a **Myers LCS diff** of the old and new text, lines split on `\n` after
  `\r\n` → `\n`, a missing final newline ignored; **non-test ≤ 40, test ≤ 120**;
- every edited file's **blob SHA still equals the packet's** at pull-request time (else `autofix-base-moved`);
- tree entries are created with mode **`100644`** (or the existing `100755`); a path whose current mode is anything
  else (symlink, submodule) is refused;
- the resulting text and the pull-request text contain **no Unicode bidirectional control or zero-width
  character**, no line over 400 characters, and pass the egress detectors.

A failing cap does not retry: the card is `BLOCKED` with `autofix-limits` (or the named reason) and the verdict is kept.

### 8.3 Creating the pull request (GAOS, dashboard process; the write token never enters a workspace or a packet)

The parsed reply is **persisted on the `FixPR` record (`state: preparing`) before the first GitHub write**, so a
restart never pays for a second agent turn. A boot job re-runs the steps for every `preparing` record, and for any
`REVIEW` fix card with no `FixPR` it re-reads the stored reply artifact. The steps are idempotent, keyed by the branch:

0. **CI check.** Read `.github/workflows/*` at the pinned head. A workflow that is triggered by `push` or
   `pull_request` / `pull_request_target` for a branch named `gaos/fix-…` (a conservative **line scanner**, the
   dashboard has no third-party modules: it treats an `on:` with no branch filter, or a filter that matches
   `gaos/**`, as triggered) **and** references `secrets.` other than `secrets.GITHUB_TOKEN`, refuses with
   `autofix-ci-secrets`. `CIConfirmed` is a precondition, not a substitute for this check.
1. read the default branch's head SHA (never from the agent); verify the blob SHAs;
2. create blobs, a tree (`base_tree` = the current head) and a commit: subject
   `fix(<first path segment>): <summary> (<defect id>)`; trailers `GAOS-Defect: <key>` and `AI-Assisted: true`;
   author and committer are those GitHub gives the token's account for an API commit;
3. create `refs/heads/gaos/fix-<slug>`, the slug being the lower-cased defect id or case id with `[^a-z0-9]+` → `-`,
   at most 60 characters, `-<gen>` appended when `gen` > 1. **An existing ref is never moved or overwritten.** After
   a crash GAOS adopts an existing `gaos/fix-<slug>` **only if** its head commit carries our `GAOS-Defect: <key>`
   trailer and its tree equals ours; any other ref of that name → `BLOCKED` `autofix-branch-exists`;
4. open the pull request with `draft: true`, labels `ai-assisted` and `defect-auto-fix` (created when missing), and
   one comment with the before/after. If GitHub refuses a draft (a plan without private-repository drafts) the card is
   `BLOCKED` `autofix-draft-unsupported`: **there is no non-draft fallback**;
5. record `FixPROpened`, send the WhatsApp line, end the card `DONE`.

If step 3 or 4 fails after a ref **this run created**, GAOS deletes that ref (`DELETE git/refs/heads/gaos/fix-…`, the
only delete the client exposes, only for that prefix). GAOS **never** marks a pull request ready, merges, pushes to a
default branch, or force-updates anything.

**Text safety.** `summary` is one line: CR, LF, control and bidi characters removed, at most 72 characters, `@`
rendered as `(at)`. `summary`, `before` and `after` get BR-53's stripping (so no backtick can close a fence), and are
rendered inside a fence longer than any backtick run they contain. The body says plainly that the after was predicted
by the agent and not run by GAOS.

**The body** is built from the product's `PRTemplate` (slots `{{defect}}`, `{{title}}`, `{{summary}}`, `{{before}}`,
`{{after}}`, `{{files}}`, `{{limits}}`) or, if empty, a **neutral English default** shipped in GAOS. A product's own
mandatory form (for example a Bahasa form required by its AI-pull-request policy) is **data in the private seed**,
never tracked code.

### 8.4 WhatsApp

Through the existing notifier and `GAOS_WA_PRODUCTS`, to the pinned group. The caller passes
`"<product id> · auto-fix draft PR <defect id>: <url>"` and `notify.go` appends its own ` · card <id>`; a `complex`
verdict passes `"<product id> · <defect id> needs a person"`. Kinds `fix:pr` and `defect:complex`; for a `defect:`
or `fix:` key `loginFor` answers "" **by design** (the group). Never a code excerpt or a defect text. A repository
URL in the group is accepted and recorded in D-091.

## 9. Following the pull request: the monitor

### 9.1 The record and the poller

`FixPR` (vertex): `id`, `productId`, `defectKey`, `repo`, `number`, `url`, `state` (`preparing`, `draft`, `ready`,
`merged`, `closed`), `createdAt`, `updatedAt`, `taskId`, `difficulty`, `before`, `after` (each ≤ 1 KiB), `branch`,
`baseSHA`, `commitSHA`, `waSent`, `reply` (the stored parsed reply). A poller (`fixprpoll.go`) reads each non-final
record (`GET /repos/{r}/pulls/{n}`) every 5 minutes for 7 days, then hourly, and stops after 30 days; a change is a
`FixPRStateChanged`. The same loop runs the daily excerpt retention (§5.1).

### 9.2 The Auto-fix view

A back-office view `/ui/bo/autofix` in the navigation, for every product (the back office has no roles, OQ-37).
**Tiles:** open drafts, ready for review, merged in 7 days, defects awaiting triage, cards waiting for a person,
automatic dispatches today (`2 / 3` fix, `4 / 10` triage). **A table:** state chip (Draft / Ready / Merged /
Closed), defect id and title, difficulty badge, product, `repo#n` link to GitHub, the card, age, WhatsApp sent.
**Filters** by product, state and difficulty, in the URL. **A detail panel** with the before/after, the changed
files, the caps' numbers and the timeline. An `htmx` poll of 30 s; states for empty, loading, GitHub unreachable and
"auto-fix is off for this product" (with the reason). It shows the **kind of GitHub write token in use**
(`fine-grained`, `classic (broad)`), no secret and no excerpt. Built by the `gaos-frontend` agent under
`.claude/rules/frontend.md` with the vendored skills; no CSS class outside the dashboard's vocabulary.

## 10. Failure, recovery, security

| Failure | What happens |
| --- | --- |
| The same defect is reported again | `lastSeen` moves; no card. |
| Over 50 new keys in a day | The item is rejected `defect-limit`. |
| The triager's reply is unreadable or has a bad value | The triage card **Failed** with the class; the record stays `open`. |
| `easy` with a class outside the allowlist, files outside the packet or `AllowedGlobs`, or no files | Downgraded to `medium`; `DefectTriaged` says why. |
| A cap, the egress screen, the base, the CI check, the branch or the draft fails | Fix card `BLOCKED` with the named reason; nothing was written to GitHub, or the ref this run created is deleted. |
| GitHub refuses a write | Fix card **Failed** `github-token`; a ref this run created is deleted. |
| GAOS restarts mid-way | `preparing` records resume from their stored reply; an existing ref is adopted only by §8.3's rule. |
| WhatsApp is down | The pull request stays; one failed `ContactNotified`. |
| The switch is off, a cap is spent, a turn is live | The card stays `READY`; the view says why. |
| A fix merges and the pinned spec then passes | The QA tool reports `xpass`; the record becomes `fixed`; open cards are canceled. |

**Security.**
- Every field of a report, a triage reply and a fix reply is untrusted. The target repository, the base branch, the
  branch name and every path set are chosen or checked by GAOS, never taken from text.
- **A separate write token.** D-088's `GAOS_GITHUB_TOKEN_FILE` keeps its read-and-comment scope for the review flow.
  Writes use **`GAOS_GITHUB_WRITE_TOKEN_FILE`** (mode 600; read at boot; its own `prod.pr-github-write-token` mount).
  Unset: auto-fix cannot be enabled. The client for it exposes only: read ref, create blob/tree/commit, **create** a
  ref and **delete** a `gaos/fix-` ref created this run, create a pull request, add labels and a comment, and read
  search/contents. The permissions needed are **Contents: read & write**, **Pull requests: read & write** and
  **Issues: read & write** (label creation) on the application repositories only; **a fine-grained token limited to
  those is the recommended credential**. A classic `ghp_` token is refused unless `GAOS_AUTOFIX_ALLOW_CLASSIC=1` is
  set in the deploy shell (a recorded, visible operator choice shown on the view and in `AutoFixConfigured`).
  GitHub Apps are out of scope for v1 (the loader accepts only `ghp_` and `github_pat_`).
- **CI exposure** is handled by §8.3 step 0 and `CIConfirmed`; branch protection on the default branch is recommended
  and warned about on the view when GAOS can read it.
- Paths are checked when fetched (§7.1) and when written (§8.2); content is screened before it leaves.
- The route is reached only through the QA token; the auto-fix path only through: the switch, the caps, a re-checked
  verdict, the record-based eligibility, §8.2's limits and a human review on GitHub. A person always has the last word
  on **merging**.

## 11. Default policy content (neutral)

The text below is the *shape*; a product's real default is written in the private seed and edited in the back
office. A neutral copy ships in `deploy/seed/example/product.json` as a `custom` policy with the key `defect-triage`.

- **easy** — a fix a reviewer can judge from the diff alone, in one resolver or route, with a unit test: (1) a
  **validation** missing or wrong in a GraphQL resolver or a REST route, for example a missing date or datetime
  format policy, a length the contract does not enforce, an inverted check; (2) a **formatting** problem in input
  parameters, a wrong format or a missing formatting policy; (3) a **minimum or maximum value** check, a negative or
  zero amount, quantity or count accepted.
- **medium** — a business rule or a state guard inside **one service** that needs a person's judgement: a duplicate
  accepted, a status that should block an action, an always-true guard, a missing nil guard, a field never written.
- **complex** — authentication, authorisation or anonymous access; billing, tariff, price, stock or double posting; a
  schema or migration; more than one service or repository; unimplemented code; a non-deterministic defect; anything
  whose expected behaviour is a business decision.
- **An easy fix never** edits generated code or a schema, a migration, a dependency file, CI, authentication or
  anything that prices or stocks; changes more than 3 files; or lacks a unit test.
- **Reply format:** the blocks of §7.2 and §8.1, exactly.

## 12. Amendments (all recorded in D-091)

- **D-088 §14** ("an agent opening a pull request"): for `defect:easy` fixes only, **GAOS** (not the agent) opens a
  **draft** pull request in a product repository. **D-088 §7**: a second, write-scoped token for that path only.
  **D-088 §14** "a fifth agent": two roles are added.
- **BR-54:** the `qa/defects` route lets a QA token plan cards that may be dispatched, only with the product's recorded
  consent and within the caps; a QA result still never moves an existing card.
- **BR-56:** `defect:triage` and `defect:easy` are dispatched automatically for a product with the switch on, through
  `dispatchNow`'s gates (BR-07, BR-38, BR-39, dependencies, BR-32). BR-35 is **not** among them (it is not wired).
- **BR-46/47:** defect cards are exempt, as D-088's PR cards are. **BR-32:** private source goes to the provider.
- **BR-53:** applied to the new text fields; extended with a per-product daily limit on new keys.

## 13. Alternatives

- **The agent pushes and opens the pull request.** Rejected (D-088): a write token in a workspace that reads failure
  output written by a test run is the prompt-injection path.
- **Triage in the QA tool.** Rejected: the policy and the audit trail belong in the product's Blueprint in GAOS.
- **`GAOS_RUNNER=on`.** Rejected, and the design is safe under it: the scheduler never dispatches `defect:*`.
- **A new Blueprint policy kind.** Rejected: `custom` already renders any key.
- **Widening `qa/results`.** Rejected: strict decoding would reject the whole push for a QA tool one release ahead.
- **Whole files in the fix reply.** Rejected: model-written base64 of whole files is error-prone and turns unchanged
  lines into diff; search-and-replace edits applied by GAOS are exact.
- **A fork for the branch** (so a fork-PR's no-secrets rule applies). Deferred: it needs a fork per repository.

## 14. Non-goals

- Auto-fixing `medium`, `complex`, regression or any end-to-end defect; a pull request from a card that fails the
  record-based eligibility.
- Marking ready, approving or merging a pull request; pushing to a default branch.
- **Running tests** (a verified runner is a later change; v1's "after" is agent-reported).
- Fixing the defect in the **automation** repository (the spec stays pinned until the fix merges).
- A GitHub App, a task-manager ticket, a new Blueprint policy kind, per-account roles (OQ-37).

## 15. Change list (reconciled with `git diff main --stat` and `git status --short` when the change was made)

**New, Go and DDL (22):** 12 implementations (`dashboard/autofix.go`, `autofix_view.go`, `autofixcheck.go`,
`autofixglob.go`, `autofixpr.go`, `defects.go`, `defecttriage.go`, `fixprpoll.go`, `githubwrite.go`,
`handlers_autofix.go`, `prbody.go`, `store_arcade_defects.go`), 9 test files (`autofix_test.go`, `autofix_view_test.go`,
`autofixglob_test.go`, `defects_test.go`, `defecttriage_test.go`, `fixpr_fake_test.go`, `fixpr_test.go`,
`fixprpoll_test.go`, `store_arcade_defects_test.go`) and `dashboard/schema/17-defects.sql`.
**Changed, Go and DDL (35 files in the commits, of which 29 are code):** `model.go`, `labels.go`, `labels_test.go`,
`fixtures.go`, `events.go`, `events_test.go`, `fold.go`, `store.go`, `store_mem.go`, `store_arcade.go`,
`store_conformance_test.go`, `api_v1.go`, `board.go`, `prflow.go`, `runner.go`, `adapter_aimanager.go`, `github.go`,
`notify.go`, `integrations.go`, `view.go`, `handlers_bo.go`, `main.go`, `auth_test.go`, `schema_test.go`,
`schema/04-execution.sql`, `static/app.css`, `static/app.js`, `templates/backoffice.html` (the Auto-fix view lives in
the back-office template; there is no separate `autofix.html` template), plus `artifacts/v1/gaos-arcadedb-model.html`,
`deploy/check-stacks.sh`, `deploy/rebuild-restart.sh`, `deploy/stacks/compose.prod.yaml`,
`pustaka/docs/assets/toc/product.js`, `pustaka/docs/product/qa-test-plan.html` and `.claude/rules/qa-test-plan.md`.
**Docs, registers and landing page, written after the code (12 more files):** `README.md`,
`deploy/check-workspace.sh`, `landing-page/index.html`, `pustaka/docs/architecture/graph.html`,
`pustaka/docs/assets/toc.js`, `pustaka/docs/assets/toc/integrations.js`, `pustaka/docs/changelog.html`,
`pustaka/docs/integrations/pr-agents.html`, `pustaka/docs/product/{brd,decision-log,open-questions}.html` and the
new `pustaka/docs/integrations/autofix.html`.
Arithmetic: 22 new + 35 changed = **57 files** in the commits against `main` (`git diff main..HEAD`), + 12 docs
files = **69**, + this document and the consolidated review = 71. Not changed, against the first plan: `scheduler.go`
(`labelsAllowDispatch` lives in `labels.go`), `designgate_test.go`, `deploy/seed/example/product.json`, `.gitignore`
and `webhooks-and-clients.html`; the new `matchGlob` and the separate `autofix.html` template were not needed.

## 16. Phases and exits (`cd dashboard && go test -run '<regexp>' ./...`; every exit also runs the whole package)

| # | Phase | Depends on | Exit (named tests; TC ids allocated from TC-624) |
| --- | --- | --- | --- |
| 1 | QA tool: derive type/backend/targets/operations, read the `defect` annotation, push `xfail`/`fail`/`xpass` (its own design and PR) | — | its tests |
| 2 | Vocabularies: roles, labels, events, vertices, DDL, model document, `SetTaskLabel` refuses `defect` | — | `TestTheClosedVocabulariesMatchTheDDL`, `TestEveryEmittedEventIsInTheClosedCatalog`, `TestTheTaskRoleVocabularyMatchesTheDDL`, `TestSchemaDeclaresTheDocumentedTypeCounts`, `TestDefectLabelFamilyIsPlannerOnly` |
| 3 | Intake and triage (TC-624..637, 14): route and limits, record, dedup, supersede, regression, xpass cancel, context builder, parser, re-check | 2 | `TestDefectRouteRejectsABadItem`, `…Limit413`, `…RepeatPlansNoCard`, `…NewKeyLimit`, `…XpassCancelsCards`, `…RegressionLooksUpByCase`, `…FailSupersededByXfail`, `…ExcerptNeverInEventOrTitle`, `…BR53Stripping`, `…SearchErrorIsNotEmpty`, `…ContextBuiltOutsideTheLock`, `…TriageParseErrors`, `…EasyIsRecheckedAndDowngraded`, `…DefectCardsExcludedFromPRCap` |
| 4 | Dispatch and caps (TC-638..647, 10) | 3 | `TestAutoFixConfigRefusedWithoutPreconditions`, `…ConfigAdminsOnly`, `…SchedulerNeverDispatchesDefectCardsInAnyMode`, `…AutoDispatchPersistsUnderArcade`, `…CapCountsDispatches`, `…EachGateIsApplied` (BR-07, BR-38, BR-39, dependencies, egress), `…SwitchOffLeavesReady`, `…MediumNeverAutoDispatches`, `…HandMovedMediumEndsInReviewWithoutPR`, `…RelabelledCardNeverOpensAPR` |
| 5 | Fix and pull request (TC-648..663, 16) | 4 | `TestFixPRPathRules`, `…EditFindMustOccurOnce`, `…NewFileMustNotExist`, `…LineCountIsMyers`, `…NonTestAndTestCaps`, `…BlobShaMovedBlocks`, `…ModeIs100644`, `…BidiRefused`, `…CIWorkflowSecretsRefuse`, `…BranchExistsBlocks`, `…CrashAdoptsByTrailer`, `…DeleteRefOnlyOurs`, `…NeverReadyOrMerge`, `…BodySlotsAndFence`, `…SummaryOneLine`, `…WhatsAppText` |
| 6 | The Auto-fix view and the poller (TC-664..672, 9) | 5 | `TestAutoFixViewTiles`, `…Filters`, `…Drawer`, `…OffState`, `…NoExcerptOrSecret`, `…TokenKindShown`, `TestFixPRPollerStates`, `…Backoff`, `…ExcerptRetention`; then a stated browser checklist (tiles, chips, filters in the URL, drawer, the four empty states) |
| 7 | Deploy (TC-673 `check-stacks.sh`, TC-674 `rebuild-restart.sh`) both prods, VM checkout first, and the live run | 1–6 | §17 |

Registers: **D-091**; **TC-624..TC-688** (65 rows: 63 Go tests, TC-687 for `check-stacks.sh` and TC-688 for
`rebuild-restart.sh`; the register is 688, "six hundred and eighty-eight", and the Go tests are 554); release
**v0.52.0**.

## 17. Done when

- [ ] A defect report plans one triage card; a repeat plans none; an `e2e` defect, a regression and a
      `medium`/`complex` defect never get an automatic pull request.
- [ ] `Enabled` is refused without `AllowedGlobs`, `TestGlobs`, the CI confirmation and the write token; config is
      limited to the admin list; the view shows the token kind.
- [ ] Automatic dispatch needs the switch and cap left, applies every gate of §6.3, counts dispatches, is persisted
      under ArcadeDB, and cannot be bypassed by `GAOS_RUNNER=on` or by relabelling a card.
- [ ] Ten blocked defect cards do not refuse a `pr.opened`.
- [ ] A fix over 3 files, over its line caps, without a test, outside the packet or `AllowedGlobs`, on a moved base,
      behind a CI that runs secrets, or touching a forbidden path makes no pull request and names its reason.
- [ ] A draft pull request is created once: template, before/after, labels and one comment; never marked ready; an
      existing branch is never overwritten; a crash resumes without a second agent turn.
- [ ] The group gets exactly the line of §8.4; no code or defect text.
- [ ] The Auto-fix view lists the draft, follows its state, and renders no excerpt or secret.
- [ ] The sample defect end to end on the real product; a `complex` defect triaged with a notice and no pull request.
- [ ] `go test -race ./...`, `pustaka check`, `check-stacks.sh`, `check-workspace.sh` are green and every test is registered.

## 18. As built

The deviations from this design, stated as they are:

- **No new `matchGlob`.** The one in `egress.go` is reused (`autofixglob.go` holds the glob rules and their tests).
- **`Defect.class` is stored as `triageClass`**, on the assumption that `class` is a reserved word in ArcadeDB. That
  assumption is untested against a real ArcadeDB.
- `Defect` also stores `regression`, `repo` and `files`.
- **`readyDefectCards` runs at the start of `RunOnce`**, because the scheduler skips defect cards and something must
  ready them.
- **`Store.OpenFixPR` was added:** the record, `FixPROpened`, the defect's `fixing` state and the card's `DONE` are one
  transaction.
- `resolveFrom[BLOCKED]` is widened to include `REVIEW`.
- **The write client has `findPR`**, so a crash cannot open a second pull request.
- A `422` from GitHub records only a `Draft` flag, never the response body.
- **The close route needs only a signed-in operator**; the admin list guards the configuration.
- A **non-eligible fixer card just stays in `REVIEW`**: the run's reply artifact holds the edits.
- The bidi, long-line and secret checks run on **changed lines only**.
- **A failed label or comment keeps the branch** and the pull request.
- `WaSent` means the line was handed to the notifier, not delivered.
- **The `preparing` record is stored after the read-only checks and before the first write.**
- **A blocked or failed fix closes its `FixPR` record**, so a record with `Number` 0 in state `closed` can exist.
- The CI scanner matches `pull_request` filters against the base branch as well as `gaos/fix-*`.
- **An egress refusal of an auto-dispatched card sets `BlockReason` `autofix-egress`.**
- `autoFixLoop` and the resume job start inside the PR-hooks block in `main.go`.
- **A pull request closed unmerged sets the defect's difficulty to `medium`**, so a person moving the blocked card
  cannot open an automatic pull request.
- The dispatch tile sums the caps over **enabled products only**.
- The live-turn signal is read from **PR cards only**.
- **`AutoFix.BaseBranch` was added**: optional, validated; the default is the repository's default branch.
