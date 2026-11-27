# The Auto-fix page: no duplicate drafts, a duplicate check, branch and trigger, a paged waiting list, sections

**Status:** Approved (v2). The operator approved the plan on 2026-11-28 and asked for the change to be built, merged and deployed; round 2 was not run because no Blocker survived v2.
- Round 1 reviewed v1 with five lenses. The reports are `reviews/autofix-dedup-and-tabs-review-1.md`,
  which holds the four lenses and the triage table, and `reviews/autofix-dedup-and-tabs-security-review-1.md`.
- Every `PROPOSE FIX` is applied below.
- v2 removes rule X (a duplicate across defects) and the run guard (v1 §4.4), adds a boot sweep, and amends
  BR-61.

**Feature:** `autofix-dedup-and-tabs`

**Version target:** v0.71.0, a minor release. `main` is at v0.70.0: `pustaka/docs/assets/toc.js:22`,
`dashboard/templates/page.html:48,50,51`.

**Numbers claimed:**
- **D-108**, which amends D-092 and D-102;
- **TC-906 … TC-913**, eight new Go tests;
- **v0.71.0**.

**BRD rules this must obey:**
- **BR-61** (`pustaka/docs/product/brd.html:1308`) currently ends "…never merges and never closes a pull
  request". This design **amends** it through D-108 (§9). It is an amendment, not a new rule, so the
  "Sixty-six rules" count stays.
- **BR-05**: the event catalog is closed. Only `FixPRStateChanged` is used.
- **BR-55**: every new route sits behind the verified session.
- No task status and no schema field is added.

**Security lens:** required. The design adds a GitHub write (closing a pull request) and a back-office
POST route.

All `file:line` anchors are against `main` at `cf78d6c` (v0.70.0), and were re-checked for v2.

## 0. Glossary

- **Fix record**: a `FixPR` (`dashboard/defects.go:153-177`). It is GAOS's record of a draft pull request
  it opened.
  - *Open* means `draft` or `ready`.
  - *Final* means `merged` or `closed` (`fixprpoll.go:72`).
  - The id is `fx-<defect key>-<gen>` (`autofixpr.go:48-50`).
  - The head branch is `gaos/fix-…` (`autofixpr.go:29-46`). `Base` is the target branch.
- **Defect**: `Defect` (`defects.go:77-131`).
  - `Kind` is `fail`, `xfail` or `xpass` (a QA report), `review` (a finding of GAOS's PR reviewer, D-102),
    or `unit` (a failing unit-test check after a merge, D-105).
  - `Gen` is the generation, and `FixPR` names its current record.
- **Retarget** (D-092, `retarget.go`): an auto-fix admin changes the base of an open record.
  - GAOS raises the generation and plans a fix card. That card opens a **new** draft.
  - `OpenFixPR` links the two records: `RetargetedFrom` on the new one, `RetargetedTo` on the old one
    (`autofix.go:436-442`).
  - Under D-092 the old pull request is never closed (`retarget.go:15-16`), and GAOS comments "This pull
    request is left open for a person to close." (`autofixpr.go:558`).
- **The Auto-fix page**: `GET /ui/bo/autofix`. The builder is `autoFixPage` (`autofix_view.go:422-744`).
  The templates are `bo-autofix` (`templates/backoffice.html:1239`) and `af-live` (`backoffice.html:1351`).
  - The page is addressed by its query: `product`, `state`, `difficulty`, `open` and `page`
    (`afHref`, `autofix_view.go:810-835`).
  - A 30 s poll fetches `/ui/bo/autofix/live` (`autofix_view.go:1033-1041`), which replaces `#afLive` out
    of band.
- **Section**: new here. One of the page's three blocks shown one at a time: Fix pull requests, Cards
  waiting, Auto-fix by product. The word "tab" is not used for it, because `AFTab` and `StateTabs` already
  name the PR-state links (`autofix_view.go:85,279-284`).

## 1. Problem

### 1.1 Two open drafts for one defect

The operator saw one defect twice in **Fix pull requests**. The instance's records show the cause. The
names below are invented, but the shape is real:

| id | number | base | state | link |
|---|---|---|---|---|
| `fx-p-1-def-1-1` | #100 | `develop` | draft | `retargetedTo = fx-p-1-def-1-2` |
| `fx-p-1-def-1-2` | #105 | `staging` | draft | `retargetedFrom = fx-p-1-def-1-1` |

D-092 is working as designed here: nobody closed #100. The consequences:
- both drafts count in "Open drafts" (the counter is at `autofix_view.go:510-511`, the tile at
  `autofix_view.go:614`);
- nothing on the row says that one draft replaced the other;
- the row shows neither its branch (`AFRow`, `autofix_view.go:293-306`; the columns are at
  `backoffice.html:1437-1439`) nor **why** the pull request exists.

### 1.2 The page is long, and filters jump to the top

- **Cards waiting** shows 10 cards and then "+ N more", with no way to reach the rest
  (`autofix_view.go:604-606`, `backoffice.html:1401`).
- The three blocks stack vertically (`backoffice.html:1381-1415` and onward).
- A filter change swaps the whole sheet, and the new `.sheet-body` starts at the top. Filter changes are
  the product or difficulty `<select>` (`backoffice.html:1424,1426`, inside the form at `1421-1429`) and a
  state link (`1419`).
  - v0.69.1 restores the scroll only when the request's element is inside `.pager` (`app.js:581`).

## 2. Goal

1. A retarget no longer leaves two open drafts: GAOS closes the pull request it replaced.
2. An operator sees the duplicates among open fix records and, as an auto-fix admin, closes one with one
   click.
3. Each row shows **branch → base** and **what triggered it**, with a link to the earlier record when one
   caused it.
4. Cards waiting pages **5 at a time**.
5. A filter change, a state link or a section link keeps the scroll position.
6. The three blocks become **sections**. Fix pull requests is the default, and `open=` always shows it.

## 3. Non-goals

- **No duplicate detection across defects.** v1's rule X is dropped, as round 1 triaged it (it was
  speculative and could close the live defect).
- **No guard in `run` against a second open draft.** v1 §4.4 is dropped. It would have blocked a reopened
  unit defect's new draft, which reverses D-105 (5), and a chained retarget after a failed close. Rule D
  (§5.1) shows those cases to a person instead.
- **No branch deletion.** Closing keeps the branch.
- **No automatic close** except the retarget's replaced record (§4). Every other close is a person's click.
- **No change to the Pull requests page or the Board**, beyond the scroll selector naming `#prsFilters`
  (§7.3). The selector is scoped by id, so the Board bar and Traffic (`backoffice.html:472,1732`), which
  also use `.af-filters`, are untouched.
- No new event type, task status or schema field.

## 4. Prevention: a retarget closes the pull request it replaced (D-108)

### 4.1 The GitHub write

Add `closePR` and `prState` to `ghWriter` in `githubwrite.go`, after `addComment`
(`githubwrite.go:466-468`):

```go
// closePR closes a pull request (PATCH state=closed). The branch is kept.
func (w *ghWriter) closePR(ctx context.Context, owner, repo string, number int) error {
	b, _ := json.Marshal(map[string]string{"state": "closed"})
	_, err := w.c.do(ctx, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", w.repoURL(owner, repo), number), b)
	return err
}
```

- `w.c.do` takes any method (`github.go:353`).
- The write token already has "Pull requests: write", because `createPR` needs it.
- The method list in the `ghWriter` comment (`githubwrite.go:155-159`) gains "close a pull request it
  opened".

**The guard.** `closableFix(m Master, f FixPR) bool` lives in `retarget.go`, next to
`retargetChecksLocked`. It is true when all of these hold:
- `strings.HasPrefix(f.Branch, fixRefPrefix)`;
- `defectRepo.MatchString(f.Repo)`;
- `f.Number > 0`;
- `productOwns(defectOwnedRepos(m), owner, repo)`.

`retargetChecksLocked` keeps its own two checks, because their refusal sentences differ; the implementation did not merge them (an implementation note, v0.71.0).
lives in one place.

The honest claim: **GAOS closes only pull requests on its own `gaos/fix-` branches, in the product's
repositories, that it holds a record of.** A person who pushed a `gaos/fix-` branch first could have their
pull request adopted by `findPR` (`githubwrite.go:413-426`). That residual is accepted (SEC-5): it needs
write access to the repository, and a close can be reversed.

### 4.2 When it runs: `closeSuperseded`

Add `func (p *defectFixer) closeSuperseded(ctx context.Context, productID string, rec FixPR)` to
`autofixpr.go`. In `run`, call it immediately after the `OpenFixPR` error check and before the `notify`
block (between `autofixpr.go:413` and `:414`), as `p.closeSuperseded(ctx, d.ProductID, rec)`.

1. If `rec.RetargetedFrom == ""` or `p.gh == nil`, return.
2. Load the product's records with `p.st.FixPRs(ctx, productID)` and its master with
   `p.st.Master(ctx, productID)`. Find `old` with `old.ID == rec.RetargetedFrom`.
3. If `old` is not found, is not open, or `!closableFix(m, old)`, return.
4. Derive `oo, or := ghOwnerRepo(old.Repo)`, then call `p.gh.closePR(ctx, oo, or, old.Number)`.
   - **On error:** log `autofix: the superseded pull request %s#%d could not be closed: %s` with
     `ghErrorClass(err)` (`github.go:330-349`). Never log the error's body. Then post the D-092 comment on
     `old`, unchanged: "Superseded by … This pull request is left open for a person to close." Return.
     §5's rule S flags the record on the page, and the boot sweep (§4.4) retries.
   - **On success:** post on `old` "Superseded by %s#%d (base %s). GAOS closed this pull request; its
     branch is kept." Then call `p.st.CloseFixPR(ctx, productID, old.ID, RuleRetarget, "")` (§4.3).

`linkRetarget` (`autofixpr.go:539-561`) keeps only the comment on the **new** pull request
(`autofixpr.go:555`). Its comment on the old one moves into step 4, so the old pull request never carries a
sentence that the outcome contradicts.

Why the close runs after `OpenFixPR`:
- the new record is stored before the old pull request is closed, so a crash leaves "two open" (flagged)
  and never "none open";
- `OpenFixPR` has set `old.RetargetedTo` (`autofix.go:438-441`), so the store treats the old record as
  *replaced* (`fixPRReplacedLocked`, `pullrequests.go:323-326`).

### 4.3 The store method: `CloseFixPR`

The `Store` interface (`dashboard/store.go`) gains:

```go
CloseFixPR(ctx context.Context, productID, fixPRID, rule, actor string) (Result, error)
```

It has three implementations:
- `MemStore` in `dashboard/retarget.go`;
- `ArcadeStore`, as `s.mutate(ctx, func() (Result, error) { return s.proj.CloseFixPR(…) })`, the same as
  `AdvanceFixPR` (`fixprpoll.go:514-516`);
- `readOnly`, which returns `reject(replayRefusal)`, the same as `OpenFixPR` (`autofix.go:463-465`).

Under `s.mu`, `MemStore.CloseFixPR` does the following:
1. If the record is missing, belongs to another product, or the product is gone, it returns `ErrNotFound`.
2. If the record is final, it returns `Result{}, nil`. The call is idempotent.
3. It re-runs the guard and the finding under the lock (SEC-4):
   - `closableFix(*m, f)` must hold;
   - `afDuplicates` over the product's records and defects, read under the lock, must have a finding with
     `Close == fixPRID`.
   - If either fails, it returns `retargetRefusal(409, "This pull request is not a duplicate any more. Reload the page.")`.
   - The retarget path passes rule `RuleRetarget`. Its record satisfies rule S by construction
     (`RetargetedTo` names the open new record), so the same check applies.
4. It sets `State = closed` and `UpdatedAt = autoFixNow()`.
5. It emits exactly one event, `FixPRStateChanged`:
   - text `pull request <repo>#<n> of defect <ref>: <from> → closed (<why>)`;
   - `<why>` is `superseded by <repo>#<n>` when `f.RetargetedTo` names a record (read under the lock), else
     `duplicate`;
   - payload `{Task: defect key, Rule: rule, To: "closed", Reason: from+"→closed"}`;
   - `Actor: actorAlias(actor)` when `actor != ""`.
6. It returns `Result{Floor: s.floor(), NewEvents: [that event], FixPRs: [f]}`.
7. **The defect is never touched.** Rules S and D (§5.1) never name the defect's live pull request, so
   nothing is left waiting on the record.

Why an unpolled stale record cannot loop: the poller stops reading a record after `fixPRPollLifetime`
(`fixprpoll.go:89-92`). A draft merged on GitHub after that would stay `draft` in GAOS forever. Every close
path therefore reads first:
- `prState(ctx, owner, repo, n)` wraps `pullRequest` (`githubwrite.go:173`) and returns `open`, `closed` or
  `merged`;
- when GitHub already says `closed` or `merged`, the caller does **not** PATCH; it calls
  `AdvanceFixPR(productID, FixPR{ID, State: <that>, UpdatedAt: now})` and treats the outcome as done.

### 4.4 The boot sweep

`resumeFixPRs` (`autofixpr.go:458`) re-runs only `preparing` records (`autofixpr.go:469-472`). So a crash
between `OpenFixPR` and the close, or a failed close, is **not** retried by the resume.

At the end of `resumeFixPRs`, a sweep handles this. For every product and every open record `o` with
`o.RetargetedTo` naming a record that is open or merged, it calls `p.closeSuperseded` on that successor.
That is the same function, so it has the same guards and the same read-first.
- The sweep runs once per boot.
- A failure is logged and left to the page.

### 4.5 The poller's closed path asks whether the record is live

`AdvanceFixPR`'s closed branch (`fixprpoll.go:487-503`) raises the generation and plans a person-only card
whenever the defect is `fixing`. It does not check that the closed record is the one the defect waits on.
Rule D (§5.1) can close a record that is **not** `d.FixPR`, and GitHub can report that close before
`CloseFixPR` lands, for example after a store error. In that case the defect must not move.

Change the case `d != nil && d.ProductID == productID && d.State == DefectStateFixing` at
`fixprpoll.go:481` to also require `d.FixPR == cur.ID` **for `FixPRClosed` only**:

```go
case d != nil && d.ProductID == productID && d.State == DefectStateFixing &&
	(cur.State != FixPRClosed || d.FixPR == cur.ID):
```

A merge of a non-live record still fixes the defect, which is today's behaviour. A close of a non-live
record updates only itself.

## 5. Detect duplicates, and the one-click close

### 5.1 The rules

`afDuplicates(fixes []FixPR, defs map[string]Defect) []AFDup` is a pure function in `autofix_view.go`.
- It takes **every** record of **one** product.
- It names only *open* records as the record to close.
- Each `AFDup` is `{Rule, Close, Keep string; Sentence string}`.

| Rule | Matches | Close | Keep | Sentence |
|---|---|---|---|---|
| **S** superseded | `x` is open and `x.RetargetedTo = y.ID` names a record `y` that is open or merged | `x` | `y` | `Replaced by <y.Repo>#<y.Number> (base <y.Base>) after a retarget.` |
| **D** same defect | two or more open records share `DefectKey`, and no S finding already names them as Close | every one except the kept one | `defs[key].FixPR` when that record is open; otherwise the greatest `(CreatedAt, Number)` | `Another open pull request fixes the same defect: <keep repo>#<n>.` |

- A record is the **Close** side of at most one finding. S is applied first.
- In the chain A → B → C, S yields (A, keep B) and (B, keep C). A kept record may be closed by another
  finding, and that is correct: only C stays open.
- Output order: S before D, then the close record's `CreatedAt` ascending, then `Number`.
- There is no cap. Each product is small, and step 5 of the route checks against the same full list.
- Every interpolated value goes through `afLine` (SEC-6).
- An open `x` whose successor was closed unmerged matches nothing. It is the only open record of its
  chain, so it is not a duplicate.

### 5.2 The page

- `autoFixPage` calls `afDuplicates` once per product in scope, using the `sc.fixes` and defects it
  already loads (`autofix_view.go:467,466`). It concatenates the results into `p.Dups`.
  - The state and difficulty filters do **not** apply: a duplicate is a duplicate whatever the filter.
- In the **Fix pull requests** section, when `len(p.Dups) > 0`, a panel `<div class="af-dups">` renders
  above the table. There is no toggle, no query key and no cap.
  - The heading is `Duplicates <n>`.
  - It has one `<li>` per finding:
    - the rule word (`Superseded`, `Same defect`);
    - `close <repo>#<n>` and `keep <repo>#<n>`, each linked to its drawer with `open=<id>`;
    - the sentence.
  - For an auto-fix admin, each finding has a form:
    - it posts to `POST /products/{productID}/prs/{close}/close`, with `hx-target="#sink"` and
      `hx-confirm="Close <repo>#<n> on GitHub? Its branch is kept; the defect is not changed."`, and
      every value goes through `afLine`;
    - it carries the hidden fields `product`, `state`, `difficulty` and `page` with the current filter, so
      the answer re-renders the same view.
  - Anyone else sees `An auto-fix admin can close it.`
  - "Is an admin" is `prViewerIsAdmin(r)` (`prview.go:501-504`). `buildAutoFix` passes it into
    `autoFixPage` as a new `admin bool` argument. The poll route passes it too.
- The **Open drafts** tile counts open drafts minus the S findings, so a replaced draft is not counted as
  waiting for a reviewer. Its note becomes `waiting for a reviewer` + `· <n> replaced` when n > 0.

### 5.3 The route

`POST /products/{id}/prs/{fixPRID}/close` is registered in `main.go` next to the retarget route
(`main.go:614`). The handler is `handleCloseFixPR` in `handlers_prs.go`, after `handleRetarget`
(`handlers_prs.go:50-130`). It runs these steps in order:

| Step | Check | Answer |
|---|---|---|
| 1 | no session (`sessionOf`) | 401 JSON `sign in to the back office`, as `handleRetarget:52-54` |
| 2 | not `autoFixAdmin(email)` | 403 `Only accounts on the auto-fix admin list can close a pull request.` |
| 3 | `s.mut(r)` is the replay store (`_, ro := s.mut(r).(readOnly)`) | 409 `This view is a replay; nothing is written.` |
| 4 | body over 64 KiB, or the form cannot be read | 400 `The form could not be read.` |
| 5 | the record is not in `s.store.FixPRs(id)`, or `s.store.Master(id)` fails | 404 `No such pull request.` |
| 6 | `afDuplicates` over the product's current records has no finding with `Close == fixPRID` | 409 `This pull request is not a duplicate any more. Reload the page.` |
| 7 | `!closableFix(m, rec)` | 422 `GAOS only closes pull requests it opened (branch gaos/fix-…).` |
| 8 | `s.fixWriter == nil` | 422 `The GitHub write token is not configured.` |
| 9 | `prState` (§4.3) errors | the class from `ghErrorClass` (SEC-6): `token`, `forbidden` or `notfound` give 502 `GitHub refused the request (<class>); check the write token.`; anything else gives 502 `GitHub cannot be reached; try again in a minute.` |
| 10 | GitHub already says `closed` or `merged` | `s.mut(r).AdvanceFixPR(…)` with that state; render (step 12) with toast `<repo>#<n> was already <state> on GitHub.` |
| 11 | `closePR` errors | the same mapping as step 9 |
| 12 | otherwise `s.mut(r).CloseFixPR(ctx, id, rec.ID, RuleDedup, email)` | a refusal (`*RetargetError`) uses its status and message. On success: `r.URL.RawQuery` = the posted `product`, `state`, `difficulty` and `page`, then `s.renderBOView(w, r, res, "autofix")` (`handlers_bo.go:1108`) with `res.Toast = "Closed <repo>#<n>."` |

- Every failure after step 2 goes through `retargetFail` (`handlers_prs.go:20-25`). That function renders
  the `empty` view with a toast. It does not write JSON, and v1 wrongly said it did.
- `RuleDedup = "autofix-dedup"` and `RuleRetarget` (`retarget.go:34`) are the two rules `CloseFixPR` takes.
  `RuleDedup` is declared next to `RuleRetarget`.
- **The server's write client.** `server` gains `fixWriter *ghWriter`, set in `main.go` inside the
  `if ghw != nil` block (`main.go:418-420`). The field is a pointer, never an interface, so it is never a
  typed nil.
- **CSRF.** The route is a state-changing POST behind the gate's session and origin checks
  (`auth.go:612-660`, a cross-origin non-GET is refused) and a `SameSite=Lax` cookie (`auth.go:503-508`).
  There is no CSRF token. This is the same control as the retarget route.
- If GitHub's close succeeds and `CloseFixPR` then fails, the poller records the close. For S the record
  is replaced, so it changes only its own state (`fixprpoll.go:470-472`). For D the record is not
  `d.FixPR`, so after §4.5 it also changes only its own state. Nothing else moves.

## 6. Branch and trigger

### 6.1 The columns

`AFRow` (`autofix_view.go:293-306`) gains `Branch`, `Base`, `Trigger`, `TriggerHref`, `TriggerGet` and
`Replaced bool`. `afRow` keeps its signature (`autofix_view.go:846`) and fills `Branch`
(`afLine(fx.Branch, 90)`) and `Base` (`afLine(fx.Base, 60)`).

The call site (`autofix_view.go:634`) then sets:
- the trigger, with `afTrigger(fx, defByKey[fx.DefectKey], byID)`;
- `Replaced`, when an S finding names this record.

The table (`backoffice.html:1437-1439`) gains one column, `<th scope="col" class="af-md">Branch</th>`,
between `Pull request` and `Card`. The cell is `<code>branch</code> → <code>base</code>`, and `→ —` while
`Base == ""`.

Under the title, inside the `af-defect` link, a line `<span class="af-ref">Triggered by …</span>` sits next
to the ref. The text is plain and has no nested link. A link back to an earlier record is shown in the
drawer, never inside a row link.

A replaced row also shows the badge `<span class="badge">replaced</span>` after its state.

The drawer's `af-facts` (`backoffice.html:1483`) gains:
- `<div><dt>Triggered by</dt><dd>text, linked when TriggerHref</dd></div>`;
- `Base` printed after `Branch`.

### 6.2 The trigger

`afTrigger(fx FixPR, d Defect, byID map[string]FixPR) (text, openID string)` returns the **first** case
that applies:

| Case | Text | openID |
|---|---|---|
| `o, ok := byID[fx.RetargetedFrom]` | `Retarget of <o.Repo>#<o.Number> (base <o.Base>)` | `o.ID` |
| `d.Key == ""` | `The defect record is gone` | — |
| `d.Kind == review` | `Review finding on <d.Repo>#<reviewNumber(d)> (<d.ReviewBranch>)` (`reviewdefect.go:288`) | — |
| `d.Kind == unit` | `Unit tests failed on <d.UnitBase> after merge <first 7 of d.UnitSHA>` | — |
| `d.Gen > 1`, and a final record `o` of the same `DefectKey` exists whose `CreatedAt < fx.CreatedAt` (the greatest such) | `Second attempt after <o.Repo>#<o.Number> was <o.State>` | `o.ID` |
| otherwise | `QA report <d.CaseID> (<d.Kind>)` | — |

- Every value goes through `afLine(…, 60)`, and the whole text through `afLine(…, 160)`.
- "In scope" means `byID` holds the id. `byID` holds only the records of the products in the current
  filter (`autofix_view.go:624-631`).
- The drawer link is `afHref(<current filter, Open: openID>)`.

## 7. Paging, sections, scroll

### 7.1 Cards waiting, 5 a page

Replace `autofix_view.go:604-606` with this:

```go
p.WaitPager = paginate(len(p.Waiting), f.WPage, listPageSize, func(n int) (string, string) {
	g := f
	g.Open, g.WPage = "", n
	return afHref(g), afPageHref(g)
})
p.WaitPager.Sync, p.WaitPager.Label = "#afLive:replace", "Waiting cards pages"
f.WPage = p.WaitPager.Page
if f.WPage <= 1 { f.WPage = 0 }
p.Waiting = sliceFor(p.Waiting, p.WaitPager)
```

- `listPageSize` is the existing 5 (`pager.go:16`).
- `AFFilter.WPage` is read with `pageParam(q.Get("wpage"))`.
- `afHref`, and `afPageHref` through it, write `wpage` when it is `> 1`.
- The clamped page is written back **before** any later `afHref` (the pager rule, `pager.go:8-13`). This
  block runs before `p.Pager` and the row hrefs are built.
- `WaitingMore` is removed. The heading count is `{{num .WaitPager.Total}}`, followed by
  `{{template "pager" .WaitPager}}`.

### 7.2 Sections

- `AFFilter.Section` is read by `afSection(q)`:
  - it is `products` or `waiting` when `q.Get("section")` says so;
  - otherwise it is `""`, which means Fix pull requests;
  - a non-empty `open` forces `""`.
  - `afHref` writes `section` only when it is non-empty.
- Every link the page builds carries the current section: the state links, both pagers, the row links, the
  reset link, the drawer's close link and `PollURL`. All of them go through `afHref(f…)`, so carrying
  `Section` in `AFFilter` does it.
- A link to **another** section is `afHref(AFFilter{Product: f.Product, Difficulty: f.Difficulty, Section: s})`.
  It clears `state`, `open`, `page` and `wpage`.
- **The filter form moves out of the Fix pull requests block, above the section nav.** That is
  `#afFilters`'s `<form>` with the product and difficulty selects (`backoffice.html:1421-1429`).
  - It carries `<input type=hidden name=section>` when the section is not `""`, and `state` as today.
  - The product select scopes all three sections.
  - The difficulty select filters only the table, as it does today.
  - The state links (`af-seg`, `backoffice.html:1418-1420`) stay inside the Fix pull requests section.
- The new nav sits in `af-live`, after the tiles and before the blocks:
  `<nav class="af-seg af-sections" aria-label="Auto-fix sections">`. It holds three links, in this order:
  - `Fix pull requests <n>`, where n is `RowsTotal`;
  - `Cards waiting <n>`, where n is `WaitPager.Total`;
  - `Auto-fix by product <n>`, where n is `len(Products) + ProductsMore`.

  The active link carries `aria-current="true"`. That is the attribute `.af-seg`'s active style selects
  (`app.css:1902`), so no new active style is needed.
- In `app.css`, add `.af-sections{margin:0 0 12px;align-self:flex-start}` next to `.af-seg`
  (`app.css:1896`).
- Only the active section's block renders. The poll carries `section`, so it answers the same section, and
  the nav counts refresh with it.
- The tiles, the GitHub notice, the drawer and the Unit tests (CI) sections stay outside and do not change.
- The Cards waiting section renders when there are no cards, with `No defect card is waiting.` (today the
  block is hidden by `{{if .Waiting}}`, `backoffice.html:1399`).

### 7.3 Scroll

- In `app.js:581`, replace `el.closest(".pager")` with
  `el.closest(".pager, #afFilters, #prsFilters, .af-sections, .af-dups")`.
  - The form's `change` request carries the form as `elt`, inside `#afFilters`.
  - The state links sit inside `#afFilters`.
  - `#prsFilters` (`backoffice.html:1566`) is the Pull requests page's filter bar, which has the same
    defect.
  - The Board and Traffic are not touched, because they carry no such id.
- Bump `?v=` on `app.css`, `app.js` and `live.js` to `0.71.0` (`page.html:48,50,51`).

## 8. Files

| File | Change | § |
|---|---|---|
| `dashboard/githubwrite.go` | `closePR`, `prState`; the method-list comment | 4.1 |
| `dashboard/retarget.go` | `closableFix`; `retargetChecksLocked` uses it; `MemStore`/`ArcadeStore`/`readOnly` `CloseFixPR`; `RuleDedup`; the D-092 header comment notes D-108 | 4.1, 4.3 |
| `dashboard/store.go` | the `Store` interface gains `CloseFixPR` | 4.3 |
| `dashboard/autofixpr.go` | `closeSuperseded`; its call after `OpenFixPR`; the boot sweep in `resumeFixPRs`; `linkRetarget` keeps one comment; the header comment (`autofixpr.go:16-20`) | 4.2, 4.4 |
| `dashboard/fixprpoll.go` | the closed path requires `d.FixPR == cur.ID` | 4.5 |
| `dashboard/reviewdefect.go:22` | the comment: "never merges; closes only its own superseded or duplicate drafts (D-108)" | 9 |
| `dashboard/autofix_view.go` | `AFDup`, `afDuplicates`, `afTrigger`, `afSection`; `AFFilter.Section`, `.WPage`; `AFRow` fields; `WaitPager`; `Dups`; the admin argument; the tile | 5, 6, 7 |
| `dashboard/handlers_prs.go` | `handleCloseFixPR` | 5.3 |
| `dashboard/main.go` | the route; `srv.fixWriter = ghw` | 5.3 |
| `dashboard/templates/backoffice.html` | `bo-autofix` / `af-live` / `af-drawer`: the section nav, the form moved, the dups panel, the Branch column, the trigger, the waiting pager | 5.2, 6, 7 |
| `dashboard/static/app.css` | `.af-sections`, `.af-dups` (built from existing tokens, like `.af-wait`) | 7.2 |
| `dashboard/static/app.js` | the selector | 7.3 |
| `dashboard/templates/page.html` | `?v=0.71.0` | 7.3 |
| `dashboard/fixpr_fake_test.go` | `ServeHTTP` answers `GET /repos/o/r/pulls/{n}` (the state) and `PATCH /repos/o/r/pulls/{n}`, with a `closeFail int` knob for a 500 | 8 |
| `dashboard/autofix_dedup_test.go` (new) | TC-906 … TC-913 | 10 |
| `dashboard/retarget_test.go` | `TestTheRetargetFixerUsesTheNewBase` (TC-716, `retarget_test.go:353`) asserts the new superseded comment | 10 |
| `pustaka/docs/product/brd.html:1308` | BR-61 amended | 9 |
| `pustaka/docs/product/decision-log.html` | D-108, and "One hundred and six" becomes "One hundred and seven" (`:18`) | 9 |
| `pustaka/docs/assets/toc/product.js` | the decision-log and QA counts | 9 |
| `pustaka/docs/product/qa-test-plan.html` | rows TC-906 … TC-913, prose, the counts 905 → 913; the TC-716 prose (`qa-test-plan.html:807`) | 10 |
| `.claude/rules/qa-test-plan.md` | "highest in use `TC-913`; next free `TC-914`", the Implemented range gains `TC-906…TC-913`, "913 rows … 748 in `dashboard/` … 773 Go tests" | 10 |
| `pustaka/docs/integrations/pull-requests.html:83,91` | the old-pull-request row; "What it never does" loses "close" | 9 |
| `pustaka/docs/integrations/pr-agents.html:129` | "GAOS never merges a pull request, and closes only its own superseded or duplicate drafts (D-108)" | 9 |
| `pustaka/docs/integrations/autofix.html` | the sections, the duplicate panel, branch and trigger; the closed-unmerged row (`:193`) adds "a pull request GAOS closed as replaced or duplicate plans nothing" | 9 |
| `docs/designs/gaos-pr-overview.md` | §6.4 gets the note "Amended by D-108: the old pull request is closed" | 9 |
| `pustaka/docs/changelog.html`, `pustaka/docs/assets/toc.js:22` | v0.71.0 | 9 |
| `README.md` | the version line, if it carries one, as v0.69.1 did | 9 |

## 9. Registers and docs

- **D-108** (dated 2026-11-28) has three parts:
  1. **Amends D-092:** once a retarget's new draft is recorded, GAOS closes the pull request it replaced.
     It keeps the branch and comments with the sentence in §4.2. A boot sweep retries a failed close.
  2. An auto-fix admin may close an open fix record that the page lists as a duplicate, under rule S or
     rule D. The defect is not changed.
  3. **Amends D-102 (BR-61):** "never closes a pull request" becomes "closes only its own `gaos/fix-`
     drafts that a retarget replaced or that an auto-fix admin closed as a duplicate".

  GAOS still never merges.
- **BR-61's last sentence** becomes: "GAOS never opens a review fix against a shared or protected branch
  and never merges a pull request; it closes only its own `gaos/fix-` drafts that a retarget replaced or an
  auto-fix admin closed as a duplicate (D-108)."

## 10. Tests

| TC | Test (`dashboard/autofix_dedup_test.go`) | Asserts |
|---|---|---|
| 906 | `TestARetargetClosesThePullRequestItReplaced` | Using `retargetedEnv` (`retarget_test.go:289`) and `fakeGHW`, after the retarget's draft opens: one `PATCH /repos/o/r/pulls/<old>` with `{"state":"closed"}`; the old record is `closed`; one `FixPRStateChanged` with rule `autofix-retarget`; the defect stays `fixing` with `FixPR` = the new id; no `autofix-pr-closed` card; the old pull request's comment is the success sentence |
| 907 | `TestARetargetCloseFailureIsFlaggedAndSwept` | `closeFail` answers 500. The old record stays `draft`, its comment is the D-092 sentence, and `afDuplicates` gives one S finding. After the knob is cleared, `resumeFixPRs` closes it |
| 908 | `TestAfDuplicatesRules` | S with an open or merged successor; no S with a closed successor; the chain A → B → C gives two S findings; D keeps `d.FixPR`, else the newest; a record closed by S is not reported by D; final records are never Close; the order |
| 909 | `TestTheCloseRoute` | 401, 403, 409 replay, 404 (another product's id), 409 (not a duplicate), 422 (the branch is not `gaos/fix-`), 502 (`closeFail`), "already merged" (the fake says merged: no PATCH, the record becomes merged), then success: the record is closed, the rule is `autofix-dedup`, the defect is untouched, and the answer is the Auto-fix view of the posted product |
| 910 | `TestAClosedNonLiveRecordLeavesTheDefect` | `AdvanceFixPR` closing a record that is not `d.FixPR` on a `fixing` defect: no generation change, no card; closing `d.FixPR` behaves as today |
| 911 | `TestTheAutoFixRowShowsBranchAndTrigger` | each case of §6.2, its text and openID; the branch and base cells; `→ —` with no base; a title with a bidi control is stripped |
| 912 | `TestCardsWaitingPagesByFive` | 12 waiting cards give 5, 5 and 2; `wpage=9` clamps to 3; the pager hrefs keep `product` and drop `open` |
| 913 | `TestTheAutoFixSectionsCarryTheQuery` | the default is Fix pull requests; `section=waiting` renders only that block, and `products` likewise; `open=` forces Fix pull requests; `PollURL`, the state links and the pagers carry `section`; an unknown `section` is ignored; the active link has `aria-current="true"`; the filter form renders in every section |

Existing tests that change:
- **TC-716** `TestTheRetargetFixerUsesTheNewBase` (`retarget_test.go:353`): the superseded comment is now
  the success sentence. Its register prose (`qa-test-plan.html:807`) says "closing the old pull request
  afterwards leaves the defect alone", which is still true, and gains "GAOS now closes it itself (D-108)".
- Any test that renders the default Auto-fix page and expects the products or waiting block on it gets
  `section=` in its query. The implementer runs `grep -n "af-prods\|af-wait\|Cards waiting" dashboard/*_test.go`
  and updates each hit in place, keeping its TC.

The counts move together:
- the register: 905 → **913** rows, 740 → **748** in `dashboard/`, 765 → **773** Go tests;
- the highest TC in use: **TC-913**;
- the decision log: 106 → **107** entries.

## 11. Phases

| Phase | Content | Exit |
|---|---|---|
| 1 | §4.1, §4.3, §4.5; the fake's PATCH and GET | `cd dashboard && go vet ./... && go test -run 'TestAClosedNonLiveRecordLeavesTheDefect\|TestRetarget\|TestTheRetarget' ./...` |
| 2 | §4.2, §4.4 | TC-906, TC-907 and the rest of `retarget_test.go` pass |
| 3 | §5 | TC-908 and TC-909 pass |
| 4 | §6, §7 | TC-911 to TC-913 pass, then `go test ./...` |
| 5 | §9, §10 registers | `cd pustaka && go test ./... && go run . check ./docs`; `./deploy/check-workspace.sh`; `./deploy/check-stacks.sh` |

## 12. Alternatives

- **Mark the old record "superseded" in GAOS and leave GitHub alone.** Rejected by the operator: reviewers
  still see two drafts on GitHub.
- **Close at retarget time, before the new draft opens.** Rejected: a failed fix card would leave the
  defect with no open pull request.
- **List duplicates with a link only, and let a person close on GitHub.** Rejected by the operator, who
  chose the one-click close.
- **Client-side sections.** Rejected: the page is addressed by its query and works as plain links
  (`autofix_view.go:28-30`).
- **Rule X, a duplicate across defects with the same title.** Dropped in round 1. It was speculative, it
  could close the live defect (critic), and it made the store method branch on the rule (zoom-out).
  Escalated as a separate design if real cases appear.

## 13. Risks

- **The write token closes the wrong pull request.** Mitigated by the guard and by the re-check under the
  lock. The residual is SEC-5 (§4.1).
- **A race between a click and the poller.** `CloseFixPR` returns early on a final record, and so does
  `AdvanceFixPR`. A refusal under the lock answers 409.
- **A replay view issues a GitHub write.** Step 3 refuses before any call.
