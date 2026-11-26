# Integrations view — status chips, Mandatory/Optional chips, and an ⓘ popover per integration

**Status:** Approved (operator, 2026-11-27)
**Author:** the Elephant (session of 2026-11-27)
**Scope:** the back office's Integrations view (`/app/integrations`, view
`integrations`), plus one read-only bool added to the two Brain Town `Status()`
methods. No handler, route, store method, schema, event, setting or stack file
changes.

Round 1 reports: `docs/designs/reviews/integrations-status-chips-{comprehension,zoom-out,critic,readiness}-1.md`
and `integrations-status-chips-security-review-1.md`; triage §10. Round 2 reports:
`integrations-status-chips-{critic,readiness}-2.md`; triage §11.

## 1. Problem

The Integrations view shows each integration's state as plain text after a middle
dot in its heading. One integration has no state at all.

| Block | Heading today | Anchor |
|---|---|---|
| ai-manager | `ai-manager · connected` / `· off` | `dashboard/templates/backoffice.html:853` |
| Brain Town | `Brain Town · {{.BrainTown.State}}` (`disabled`, `enabled`, `unusable: <reason>`) | `backoffice.html:867`; values `dashboard/braintown.go:599-617` |
| Brain Town reader | `Brain Town reader · {{.BrainTownConsumer.State}}` (same three forms) | `backoffice.html:879`; values `dashboard/braintown_consumer.go:308-330` |
| Pull-request flow | `Pull-request flow` — **no state** | `backoffice.html:891` |
| QA tools | `QA tools · connected` / `· outbound off` | `backoffice.html:932` |
| Machine API | `Machine API · {{.API.State}}` (`enabled (admin=… qa=… …)` or `disabled (<why>)`) | `backoffice.html:945`; values `dashboard/api_v1.go:255-261` |

The operator's requests: make a disabled integration stand out, with a red or a
green chip; give Pull-request flow a state; show which integrations must be
connected; and say on the page what an integration does and what happens when it is
connected or disconnected.

Three of these state strings — `.BrainTown.State`, `.BrainTownConsumer.State` and
`.API.State` — are printed **only** in the heading (the tables below them,
`backoffice.html:868-875`, `:880-887`, `:946-953`, have no state row). The PR states
(`:894`, `:896`, `:898`) and the QA reason (`:935`) are in rows.

## 2. Decisions this design records

**D-098 (new, added to `pustaka/docs/product/decision-log.html` by this change).**
The operator (2026-11-27) classed **ai-manager, QA tools and Machine API** as
*Mandatory* and **Brain Town, Brain Town reader and Pull-request flow** as
*Optional*. "Mandatory" means: **the workflow GAOS is built around — agent turns
(ai-manager) and the QA loop of D-072 and D-075 (the QA tools, reached through the
Machine API) — needs it.** It is the product's own workflow, not one deployment's:
D-072 makes the QA tools the writer of half of every QA case and D-075 gives GAOS the
outbound half. An install without the QA tools runs, and those blocks show red with
the Mandatory chip — which is the truth: the QA loop is not running there. It does
not mean GAOS fails to start: GAOS starts and serves the board and the back office
without any of them, and BR-27 (`pustaka/docs/product/brd.html:1280`, about private
submodules) is unaffected. Each Mandatory popover says so in one sentence. The
classification is a constant in code; making it configurable is a non-goal (§8).

D-098 row, exact cells: date `2026-11-27`; statement as the paragraph above, opening
with **"The Integrations view marks ai-manager, the QA tools and the Machine API as
Mandatory, and Brain Town, its reader and the pull-request flow as Optional."**;
evidence `TC-763…TC-766` (linked to `qa-test-plan.html#go-integ-chips`) and
`docs/designs/integrations-status-chips.md`.

Status colours follow the operator's request ("red or green"): **green = working,
red = not working, amber = working, but with a failure GAOS has seen or a part that
is broken.** An optional integration that is off is red too: the operator asked for
exactly that, and the Optional chip beside it says it is allowed to be off. A
sub-part that is merely *not configured* (staging or WhatsApp `disabled`) does not
turn the pull-request heading amber: not configuring an optional part is a choice,
not a failure (§3.2, `PRChip`).

## 3. The change

### 3.1 A status model built in Go, not in the template

The template must not string-match states: state strings carry free-text reasons,
and logic in a template is untestable. Add to `dashboard/view.go`, after
`IntegrationsPage` (`view.go:214-228`):

```go
// StatusChip is one coloured state chip on the Integrations view. Tone is
// exactly one of "ok", "warn", "bad"; the template prints it as the CSS class
// "st-<tone>". Label is one word or phrase from the closed set in §3.2 —
// never a reason string, a host or anything taken from configuration.
type StatusChip struct{ Label, Tone string }
```

`IntegrationsPage` gains six fields: `AIMChip, BrainTownChip, BrainTownConsumerChip,
PRChip, QAChip, APIChip StatusChip`. `PRFlowRow` (`view.go:231-238`) gains three:
`GitHubChip, StagingChip, WAChip StatusChip`, rendered in the PR rows (§3.3). The
tone names `ok|warn|bad` are deliberately different from the Auto-fix view's
`AFToken.Tone` `ok|warn|off` (`dashboard/autofix_view.go:107`), where `off` is grey:
here the not-working tone is red, so it gets its own name.

Three "is it failing now" signals, each of which clears on the next success:

- `QAStatus` (`view.go:261-270`) gains `Failing bool`, set in `buildIntegrations`
  where the newest attempted pending item is already found
  (`dashboard/integrations.go:159-170`, the `last` variable):
  `p.QA.Failing = last != nil && last.Delivery.LastError != ""`. A delivered item
  leaves the pending list, so this clears.
- `QAStatus` also gains `GaveUpRecent int`: the gave-up events of the last 24 hours,
  counted in the existing event scan (`integrations.go:124-141`) beside `GaveUp`,
  with `e.At >= now.Add(-24*time.Hour).Unix()`. `GaveUp` (all time) stays as it is
  and is still printed in its row; only the chip uses the 24-hour count, so the chip
  goes back to green a day after the last give-up.
- `BrainTownRow` (`view.go:420-428`) gains `Failing bool`.
  - **Brain Town feed:** `brainTown.Status()` (`dashboard/braintown.go:599-607`) sets
    it to `b.degraded > 0`. `degraded` counts failed sends since the last success
    (`braintown.go:546`) and is reset to 0 on a success (`:530`).
  - **Brain Town reader:** `btConsumer.Status()`
    (`dashboard/braintown_consumer.go:308-330`) sets it to `c.lastErr != ""`.
    `lastErr` is set on "unreachable" or a non-200 answer (`:386`, `:391`) and
    cleared on a successful connection (`:402`).
  - Neither error text reaches the page; only the bool does.

All chips are filled by one new pure function in `dashboard/integrations.go`, called
once at the end of `buildIntegrations` (`integrations.go:70`), after
`p.PR = s.prFlowRow(ctx, products, now)` (line 199):

```go
// integChips derives every chip from the page's own fields. Pure: no server,
// no clock, no I/O — the unit test calls it with a hand-built page.
func integChips(p *IntegrationsPage)
```

### 3.2 The mapping (complete; nothing else produces a chip)

Rules are checked top to bottom; the first match wins.

| Target | Source | Rule | Label · Tone |
|---|---|---|---|
| `AIMChip` | `AIM.Configured` (`integrations.go:90-91`) | false | Off · bad |
| | | true | Connected · ok |
| `BrainTownChip`, `BrainTownConsumerChip` | `.State`, `.Failing` of `BrainTown` / `BrainTownConsumer` | `State == "enabled"` and `Failing` | Degraded · warn |
| | | `State == "enabled"` | Enabled · ok |
| | | `State` prefix `"unusable"` | Unusable · bad |
| | | anything else | Disabled · bad |
| `QAChip` | `QA.Outbound`, `QA.GaveUpRecent`, `QA.Failing` | `Outbound` false | Outbound off · bad |
| | | `GaveUpRecent > 0` or `Failing` | Degraded · warn |
| | | otherwise | Connected · ok |
| `APIChip` | `API.State`, `API.QA`, `API.QAProducts` | `State` prefix `"enabled"`, and `API.QA` false or `len(API.QAProducts) == 0` | Degraded · warn |
| | | `State` prefix `"enabled"` | Enabled · ok |
| | | anything else | Disabled · bad |
| `PR.GitHubChip` | `PR.GitHub` (`"on"` or `"off (<why>)"`; `integrations.go:206-209`, `dashboard/main.go:384-387`) | `== "on"` | On · ok |
| | | anything else | Off · bad |
| `PR.StagingChip` | `PR.StagingState` (`integrations.go:210-217`) | `== "enabled"` | Enabled · ok |
| | | prefix `"unusable"` | Unusable · bad |
| | | anything else | Disabled · bad |
| `PR.WAChip` | `PR.WA.State` (`dashboard/wa.go:244-265`) | `== "enabled"` | Enabled · ok |
| | | prefix `"enabled,"` (a qualifier: `unencrypted`, `linked to no product`) | Degraded · warn |
| | | anything else | Disabled · bad |
| `PRChip` | `PR.GitHubChip`, `BrainTownConsumerChip`, `PR.StagingChip`, `PR.WAChip` | GitHub tone not `ok` | Off · bad |
| | | reader tone not `ok` | Partly connected · warn |
| | | Staging or WA label is `Unusable` or `Degraded` | Partly connected · warn |
| | | otherwise (Staging and WA each `Enabled` or `Disabled`) | Connected · ok |

The closed label set is therefore: **Connected, Off, Enabled, Unusable, Disabled,
Outbound off, Degraded, On, Partly connected** — nine labels.

Why these rules:
- WhatsApp: `wa.go:250-251` builds the state from parts so that "unencrypted" is
  never hidden by another state. A green chip over it would hide it, so any
  qualifier makes the chip amber.
- QA: `Outbound` only means a URL was accepted (`integrations.go:145-148`). Given-up
  items or a failing last attempt make it amber, so a green chip does not claim a
  delivery path that is failing.
- Machine API: the QA tools need the QA token (`API.QA`). "Enabled" without it is
  amber.
- Pull-request flow: the review needs the GitHub client, and pull-request events
  reach GAOS only through the Brain Town reader. GAOS has no GitHub webhook of its
  own: the flow is fed only by the reader's `Drain` (`dashboard/main.go:462`), and
  `dashboard/github.go:21` says the webhook belongs to Brain Town. So:
  - GitHub off: red.
  - Reader not green: amber.
  - A broken sub-part (staging unusable, WhatsApp degraded): amber.
  - A sub-part that is only not configured: ignored, because it is a choice (§2).
- Machine API: BR-52 (`brd.html:1347`) says "an unset list reaches nothing", so
  `GAOS_API_QA_PRODUCTS` empty makes it amber, just as a missing QA token does.
- Brain Town feed and reader: an enabled connection whose last attempt failed is
  amber. It clears on the next success (§3.1).
- Not done, with reasons:
  - The ai-manager provider list (`integrations.go:101-115`) is filled only when a
    product's AI vendors tab asks, and usually reads "not asked since boot". A chip
    built on it would be stale or empty.
  - The feed's lifetime `Failed` and `Dropped` counts never clear, so they are not
    used. They stay printed in their row.

### 3.3 Template (`dashboard/templates/backoffice.html`, `bo-integrations`, lines 848-974)

**(a) Headings.** Each of the six integration headings becomes a wrapper `div`
holding the `h4`, the chips and the info button. The button sits outside the `h4`,
so the heading's accessible text is the name alone. Example for Brain Town:

```html
<div class="integ-h">
  <h4>Brain Town</h4>
  <span class="chip st-{{.BrainTownChip.Tone}}">{{.BrainTownChip.Label}}</span>
  <span class="chip need-opt">Optional</span>
  <button type="button" class="info-btn" popovertarget="ip-bt" aria-label="Brain Town: info">i</button>
</div>
<div id="ip-bt" class="info-pop" popover>
  <h5>What it does</h5><p>…</p>
  <h5>When connected</h5><p>…</p>
  <h5>When disconnected</h5><p>…</p>
</div>
```

The requirement chip is static markup per block: `<span class="chip need-req">Mandatory</span>`
on ai-manager, QA tools and Machine API, and `<span class="chip need-opt">Optional</span>`
on the other three. Contacts and Registered clients (`:906`, `:956`) keep their plain
`h4` with no chip and no button.

| Block | chip field | requirement | popover id | aria-label |
|---|---|---|---|---|
| ai-manager | `.AIMChip` | Mandatory | `ip-aim` | `ai-manager: info` |
| Brain Town | `.BrainTownChip` | Optional | `ip-bt` | `Brain Town: info` |
| Brain Town reader | `.BrainTownConsumerChip` | Optional | `ip-btc` | `Brain Town reader: info` |
| Pull-request flow | `.PRChip` | Optional | `ip-pr` | `Pull-request flow: info` |
| QA tools | `.QAChip` | Mandatory | `ip-qa` | `QA tools: info` |
| Machine API | `.APIChip` | Mandatory | `ip-api` | `Machine API: info` |

**(b) State rows (keeps the reasons on the page).** Three tables gain a first row
that prints the full state string, which the heading no longer carries:

```html
<tr><th scope="row">State</th><td class="mono">{{.BrainTown.State}}</td></tr>          <!-- Brain Town, before "Receiver" -->
<tr><th scope="row">State</th><td class="mono">{{.BrainTownConsumer.State}}</td></tr>  <!-- reader, before "Source" -->
<tr><th scope="row">State</th><td class="mono">{{.API.State}}</td></tr>                <!-- Machine API, before "Admin token" -->
```

So `unusable: <why>` and `disabled (<why>)` stay readable directly below a red chip,
and the existing test `TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing`
(`dashboard/integrations_test.go:151-188`, TC-512), which requires
`"disabled (no API token is configured)"` at line 182, still passes unchanged: that
text now comes from the Machine API State row.

**(c) PR row chips.** The three PR rows put the chip before the existing text,
which is unchanged:

```html
<tr><th scope="row">GitHub client</th><td><span class="chip st-{{.PR.GitHubChip.Tone}}">{{.PR.GitHubChip.Label}}</span> <span class="mono">{{.PR.GitHub}}</span></td></tr>
<tr><th scope="row">Staging callback</th><td><span class="chip st-{{.PR.StagingChip.Tone}}">{{.PR.StagingChip.Label}}</span> <span class="mono">{{.PR.StagingState}}</span></td></tr>
<tr><th scope="row">WhatsApp</th><td><span class="chip st-{{.PR.WAChip.Tone}}">{{.PR.WAChip.Label}}</span> <span class="mono">{{.PR.WA.State}}{{if .PR.WA.Host}} &middot; {{.PR.WA.Host}}{{end}}</span></td></tr>
```

**(d) Machine API docs link.** The Machine API block (`:944-954`) gains the same
docs link line the QA block has: `<p class="tm-line"><a href="../docs/integrations/webhooks-and-clients.html" target="_blank" rel="noopener">Webhooks and external clients</a></p>`.
Then every one of the six blocks has a docs link, which is the fallback for a browser
without the Popover API (§3.5).

**(e) Popover texts (final, static; no field is interpolated).** Each popover is
three `h5` + `p` pairs, in this order. A Mandatory block's popover ends with a
fourth paragraph, `<p class="info-req">Mandatory: the workflow this product runs needs it. GAOS still starts and serves the board without it.</p>`;
an Optional block's popover ends with `<p class="info-req">Optional: GAOS's workflow does not depend on it.</p>`.

| id | What it does | When connected | When disconnected |
|---|---|---|---|
| `ip-aim` | The AI gateway. Every agent turn GAOS runs goes through ai-manager, which holds the model providers. | With the runner on or manual, agents take turns and the board can dispatch work; with the runner off, no turn runs. | No turn can run and work waits. The board and the back office still work. |
| `ip-bt` | Pushes GAOS's own task and product changes to Brain Town, the shared activity town. | Each change is sent. A product not listed as public is sent hidden: its name and the task titles are replaced (alias, task id), but the product id, task ids, kinds, columns and times are still sent. | Nothing is sent. GAOS works normally; the town does not show GAOS. |
| `ip-btc` | Reads pull-request events (opened, merged) from Brain Town for the repositories the products declare. It is GAOS's only source of pull-request events: GAOS has no GitHub webhook of its own. | A pull request opened or merged in a declared repository starts the pull-request flow. | GAOS hears about no pull request, so no review starts. Only the staging callback still creates end-to-end cards. |
| `ip-pr` | Reviews pull requests, creates the end-to-end and unit-test cards, posts the review comment and sends WhatsApp notices. | With the GitHub client on, GAOS reads the diff and posts its review; the staging callback adds the end-to-end card, and WhatsApp sends the notices. | Without the GitHub client no review is posted. A staging callback still creates its end-to-end card if the staging credential is set. |
| `ip-qa` | The QA tools: the catalogue sync and the webhook that tells them a question was answered. | Answers and catalogue changes reach the QA tools within seconds. | Answers wait in the queue ("Waiting to be delivered") and are retried until delivered or given up; a given-up answer is not sent again. |
| `ip-api` | The machine API that the QA tools and the registered clients call, one bearer token per role. | The QA tools can sync, ask and read answers; clients can use their scoped routes. | Every bearer-token call is refused; the board and the back office are unaffected. The staging callback has its own credential and is controlled by the pull-request flow's staging setting. |

Claims checked against the code:
- `ip-aim`: the runner modes, `dashboard/runner.go:52-67`.
- `ip-bt`: what is sent for a hidden product, `braintown.go:370-372` (key, alias), `:409`, `:456`.
- `ip-qa`: gave-up is terminal, `integrations.go:121-123`.
- `ip-api`: the staging route goes to `apiStaging` (`dashboard/api_v1.go:415-417`, `dashboard/staging.go:196`), which checks only the staging credential.

### 3.4 CSS (`dashboard/static/app.css`)

Appended **at the end of `app.css`**, so it comes after `.block h4{margin:0 0 10px}`
(`app.css:860-863`). The selectors below then win on order, at equal specificity.
Every variable used already exists in both themes: `:root` (the dark theme, `app.css:7`) at `:19-31`, and
`[data-theme="light"]` (`app.css:83`) at `:94-104`. No new variable.

```css
.integ-h{display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-bottom:8px}
.integ-h h4{margin:0}
.chip.st-ok,.chip.st-warn,.chip.st-bad{color:var(--ink);font-weight:600}
.chip.st-ok{border-color:var(--done);background:color-mix(in srgb,var(--done) 22%,transparent)}
.chip.st-warn{border-color:var(--block);background:color-mix(in srgb,var(--block) 22%,transparent)}
.chip.st-bad{border-color:var(--fail);background:color-mix(in srgb,var(--fail) 22%,transparent)}
.chip.need-req{color:var(--ink);border-color:var(--ink-2);font-weight:600}
.chip.need-opt{color:var(--ink-2)}
td .chip{margin-right:6px}
.info-btn{
  width:22px;height:22px;border-radius:50%;border:1px solid var(--line);
  background:var(--panel-2);color:var(--ink-2);font-family:var(--font-mono);font-size:12px;
  line-height:20px;padding:0;cursor:pointer;
}
.info-btn:hover{color:var(--ink);border-color:var(--ink-2)}
.info-pop{
  max-width:min(440px,calc(100vw - 32px));padding:16px 18px;
  background:var(--panel);color:var(--ink);border:1px solid var(--line);border-radius:var(--r-s);
  box-shadow:0 12px 32px rgba(0,0,0,.35);
  opacity:0;transform:scale(.95);
  transition:opacity 150ms ease-out,transform 150ms ease-out;
}
.info-pop:popover-open{opacity:1;transform:scale(1)}
@starting-style{.info-pop:popover-open{opacity:0;transform:scale(.95)}}
.info-pop h5{margin:10px 0 2px;font-size:12px;text-transform:uppercase;letter-spacing:.08em;color:var(--ink-2)}
.info-pop h5:first-child{margin-top:0}
.info-pop p{margin:0}
.info-pop .info-req{margin-top:12px;color:var(--ink-2)}
@media (prefers-reduced-motion:reduce){.info-pop{transition:none}}
```

Decisions written into these rules:
- **Position:** the browser default for `[popover]`, centred in the viewport. No
  anchoring.
- **Motion:** the open is animated (opacity and scale, 150 ms, ease-out). The close
  is not: no `transition-behavior: allow-discrete`, so the popover disappears at
  once.
- **Focus:** the global `:focus-visible` ring (`app.css:136`) applies to
  `.info-btn`. No ring of its own.
- **Contrast.** Status chips are coloured by fill and border, and their text is
  `--ink`. The coloured text the first draft used (`--fail` on `--panel` in the dark
  theme) measures about 4.48:1 at 11 px, under 4.5:1. `--ink` on a 22 % tint of
  any of the three colours keeps the panel's own text contrast, which is above 10:1
  in both themes. The colour carries the meaning, and the label says it in words.
  "Optional" uses `--ink-2`, not `--ink-3`: at 11 px, `--ink-3` is below 4.5:1.
- **`color-mix()`.** Supported by every browser that has the Popover API (2023+). In
  an older browser the fill is dropped and the coloured border remains.

`--panel`, `--panel-2` and `--line` are defined in both themes (`app.css:11-14`, `:87-90`);
`--r-s` and `--font-mono` once, theme-independent (`app.css:74-76`).

### 3.5 Why the native Popover API

The ⓘ is a `<button popovertarget>` and the panel a `<div popover>`. The browser
handles open on click, close on Esc and on an outside click, the top layer and focus
return, with no JavaScript. The dashboard's JavaScript is three plain files
(`dashboard/static/app.js`, `board.js`, `live.js`) and this adds none. The view
arrives as the out-of-band `#boSheet` element (`dashboard/templates/backoffice.html:46`). A popover inside swapped content needs no
re-initialisation, because the attributes are declarative. A browser without the
Popover API (before 2024) shows a button that does nothing. Each block's docs link
(§3.3d) still explains the integration.

## 4. Tests

Four new top-level tests in `dashboard/integrations_test.go`:

| TC | `func Test…` | Positive | Negative |
|---|---|---|---|
| TC-763 | `TestIntegChipsFollowTheState` (pure, table-driven over `integChips`; each case has a name, and the message names it with `%s`) | every rule row of §3.2 gives its Label·Tone | `"unusable: x"` is never `ok`. An unknown Brain Town state is `bad`. `enabled` + `Failing` is never `ok`. WhatsApp `"enabled, over http to any address (unencrypted)"` and `"enabled, linked to no product (GAOS_WA_PRODUCTS)"` are never `ok`. QA with `GaveUpRecent=1` is never `ok`; QA with `GaveUp=5, GaveUpRecent=0` and not `Failing` **is** `ok` (an old give-up clears). API enabled without the QA token, or with no QA products, is never `ok`. GitHub on + staging `"unusable: x"` is never `Connected`. GitHub on + reader `Disabled` is never `Connected`. GitHub on + reader ok + staging and WA `disabled` **is** `Connected`. GitHub off + staging on is `Off`. |
| TC-764 | `TestTheIntegrationsViewColoursEveryHeading` (renders `/ui/bo/integrations` with `newIntegrationsFixture`, after setting `f.srv.brainOff = BrainTownRow{State: "unusable: BRAIN_TOWN_URL is not set"}` and `f.srv.btConsumerOff = BrainTownRow{State: "disabled"}`) | each of the six `<div class="integ-h">` holds exactly one `chip st-ok\|st-warn\|st-bad`. The three PR rows hold one `st-*` chip each. The section contains the exact cells `<td class="mono">unusable: BRAIN_TOWN_URL is not set</td>` and `<td class="mono">disabled</td>`, and a State cell starting `enabled (admin=`. The Brain Town heading's chip is `Unusable`. | Every `st-*` chip's text is one of the nine labels of §3.2. No `<h4>` inside `#boIntegrations` contains `&middot;`. |
| TC-765 | `TestTheIntegrationsViewMarksMandatoryIntegrations` | ai-manager, QA tools and Machine API headings carry `Mandatory` | Brain Town, Brain Town reader and Pull-request flow carry `Optional` and never `Mandatory`. Contacts and Registered clients carry neither. |
| TC-766 | `TestEveryIntegrationInfoButtonOpensItsPopover` | six `popovertarget` values, each matching exactly one `popover` element `id`. Each popover holds the three `h5` headings in order. `ip-api` names the staging callback. | No `id` of the six appears twice in the section. No `popovertarget` points to a missing id. |

Both existing view tests — `TestTheIntegrationsViewShowsEachIntegrationAndNoSecret`
(`integrations_test.go:104`, its whole-body secret check at `:128-131`) and
`TestTheIntegrationsViewNeedsASessionAndSaysWhatIsMissing` (`:151`) — stay unchanged
and must pass.

Messages follow the register's convention: a format string, `%s`/`%q`/`%d` allowed,
quoted verbatim in the register.

## 5. Registers and counts (the arithmetic)

- Next free TC is TC-763. The highest in use is TC-762 (`qa-test-plan.html`, checked
  by `deploy/check-workspace.sh:105-111`). If `main` moves before merge, the ids and
  the counts are renumbered on rebase, as commit `30c1244` did.
- **Register:** 762 + 4 = 766.
- **Go tests:** 626 + 4 = 630.
- **Dashboard tests:** 602 + 4 = 606; pustaka stays 24.
- **Decisions:** the page's own figure, "Ninety-seven entries" (`decision-log.html:18`),
  plus D-098 = "Ninety-seven entries". The figure is the register's prose count and
  is pinned by `check-workspace.sh:131`; this change adds one to it and does not
  recount the table.
- **Open questions:** unchanged (forty).
- **Version:** 0.59.0 → 0.60.0. The version is also the cache key of the static
  assets: `dashboard/templates/page.html:48`, `:50`, `:51` load `app.css`, `app.js`
  and `live.js` with `?v=0.59.0`, the comment at `:46-47` says to bump it with
  `product.semver`, and `TestStaticAssetsAreVersionedByTheProductVersion`
  (`dashboard/external_inbox_test.go:893-915`, TC-690) fails if they differ. Static
  files are cached for a week (`dashboard/main.go:605-614`), so without the bump
  prod would serve the old CSS.

Every literal that moves:

| File | Literal now | becomes |
|---|---|---|
| `pustaka/docs/product/qa-test-plan.html` lede (`:19`) and register intro (`:79`) | seven hundred and sixty-two | seven hundred and sixty-six |
| `pustaka/docs/assets/toc/product.js:162`, `:168` | seven hundred and sixty-two | seven hundred and sixty-six |
| `pustaka/docs/assets/toc/product.js:168` | six hundred and thirty-six | six hundred and thirty |
| `pustaka/docs/assets/toc/product.js:170` | six hundred and two dashboard | six hundred and six dashboard |
| `pustaka/docs/product/decision-log.html:18` | Ninety-seven entries | Ninety-seven entries |
| `pustaka/docs/assets/toc/product.js:129` | Ninety-seven decisions | Ninety-seven decisions |
| `deploy/check-workspace.sh:114-115`, `:118` | `six hundred and thirty-six` (×2), `626` | `six hundred and thirty` (×2), `630` |
| `deploy/check-workspace.sh:129-130` | `seven hundred and sixty-two` (×2) | `seven hundred and sixty-six` (×2) |
| `deploy/check-workspace.sh:131` | `Ninety-seven entries` | `Ninety-seven entries` |
| `README.md:12` | Product version 0.59.0 | Product version 0.60.0 |
| `README.md:137` | 626 Go tests: 602 in `dashboard/` | 630 Go tests: 606 in `dashboard/` |
| `README.md:139` | 762 registered test cases | 766 registered test cases |
| `pustaka/docs/assets/toc.js:22` | `semver: "0.59.0"` | `semver: "0.60.0"` |
| `dashboard/templates/page.html:48`, `:50`, `:51` | `?v=0.59.0` (×3) | `?v=0.60.0` (×3) |
| `.claude/rules/qa-test-plan.md` | "highest in use is `TC-762`; next free number is `TC-763`", "seven hundred and sixty-two", "six hundred and thirty-six", "762 rows… 602… 626", the dashboard range | TC-766 / TC-767, sixty-six, thirty, 766 / 606 / 630, the dashboard range gains `TC-763…TC-766` |

Register content added to `qa-test-plan.html`:
- four rows in `tbody#tc-index`;
- **one** `h3`, `<h3 id="go-integ-chips">dashboard &mdash; the Integrations view's chips and popovers (TC-763 &hellip; TC-766)</h3>`,
  placed after the `#go-paging` group (the last dashboard group on `main`). It holds one
  `<p><strong>TC-n</strong> …</p>` per case, the register's convention, each
  quoting the test's messages verbatim.

Changelog: `<h3 id="v0-60-0">v0.60.0 <time datetime="2026-11-27">2026-11-27</time> <span class="bump minor">minor</span></h3>`
above `v0-59-0` (`changelog.html:39`), with one paragraph:
"The Integrations view shows each integration's state as a green, amber or red chip,
marks it Mandatory or Optional (D-098), and explains it in an ⓘ popover. Brain Town,
its reader and the Machine API gain a State row; the pull-request flow gains an
overall state. TC-763 … TC-766."

The D-098 row goes in `decision-log.html` after `d-097`, with the cells
given in §2.

Before the list is called complete, the implementer runs
`grep -rn "0\.58\.1\|seven hundred and sixty-two\|six hundred and thirty-six\|five hundred and ninety-two\|Ninety-five" --include=*.html --include=*.js --include=*.md --include=*.sh . | grep -v changelog.html | grep -v "docs/designs/"`.
Expected: nothing outside the changelog's release history and the design records.

## 6. Change list (complete)

| # | File | Change |
|---|---|---|
| 1 | `dashboard/view.go` | `StatusChip`; six fields on `IntegrationsPage`; three on `PRFlowRow`; `Failing`, `GaveUpRecent` on `QAStatus`; `Failing` on `BrainTownRow` |
| 2 | `dashboard/integrations.go` | `integChips(p)`; one call at the end of `buildIntegrations`; `p.QA.Failing`, `p.QA.GaveUpRecent` |
| 2a | `dashboard/braintown.go` | `Status()` sets `Failing: b.degraded > 0` |
| 2b | `dashboard/braintown_consumer.go` | `Status()` sets `Failing: c.lastErr != ""` |
| 3 | `dashboard/templates/backoffice.html` | six heading wrappers, six popovers, three State rows, three PR row chips, the Machine API docs link |
| 4 | `dashboard/static/app.css` | the rules in §3.4 |
| 5 | `dashboard/integrations_test.go` | four tests (§4) |
| 6 | `pustaka/docs/product/qa-test-plan.html` | four rows, four `h3`, two counts |
| 7 | `pustaka/docs/assets/toc/product.js` | four counts (`:129`, `:162`, `:168` ×2, `:170`) |
| 8 | `pustaka/docs/changelog.html` | v0.60.0 entry |
| 9 | `pustaka/docs/assets/toc.js` | `product.semver` 0.60.0 |
| 10 | `pustaka/docs/product/decision-log.html` | D-098 row; count |
| 11 | `deploy/check-workspace.sh` | the pinned counts (§5) |
| 12 | `README.md` | version and three figures |
| 13 | `.claude/rules/qa-test-plan.md` | the counts and the range (§5) |
| 13a | `dashboard/templates/page.html` | `?v=0.60.0` ×3 (§5) |
| 14 | `docs/designs/integrations-status-chips.md` + its review reports | this document |

Sixteen product files (rows 1–13a) plus the design record. Not touched: any event,
status, route, store method, schema statement, setting or stack file.

## 7. Phases and their checks

1. **Model.** Changes 1, 2 and the TC-763 test.
   - Run `cd dashboard && go test -run TestIntegChipsFollowTheState ./...`
   - Expected: `ok`.
2. **View.** Changes 3, 4 and tests TC-764…756.
   - From the repository root, run `(cd dashboard && go vet ./... && go test ./...)`
     and `grep -hoE '^func Test[A-Za-z0-9_]+' dashboard/*_test.go | wc -l`
   - Expected: `ok`, and the count prints `606`.
3. **Registers.** Changes 6–13. From the repository root:

   ```bash
   (cd dashboard && go test ./...)      # again: TC-690 now sees the new semver
   (cd pustaka && go run . check ./docs && go test ./...)
   ./deploy/check-stacks.sh
   ./deploy/check-workspace.sh
   ```

   Expected: every check `ok`, and `check-workspace.sh` reports
   `QA register: 766 cases, highest TC-766` and `Go test count: 630`.
4. **Live.** After deploy, open `/app/integrations`. Expected:
   - six status chips and six requirement chips, on one line with their heading
     (the heading text and the chips share a centre line);
   - each ⓘ opens its popover; Esc and an outside click close it;
   - checked in the dark and the light theme, and at 375 px width (no horizontal
     scroll, popover within the viewport).

## 8. Non-goals and known issues

- **A configurable "mandatory".** The classification is the operator's fixed answer
  (D-098).
- **Health probing.** No new reachability check; the chips use only what the page
  already knows (§3.2).
- **Known issue, tracked outside this design — the Brain Town reader's cursor is
  never saved in prod.**
  - `deploy/stacks/compose.prod.yaml:191` mounts `prod.bt-consumer-cursor` `:ro`,
    and `deploy/check-stacks.sh:158` asserts the mount.
  - `writeCursor` (`dashboard/braintown_consumer.go:622-631`) writes `<cursor>.tmp`
    and renames it, so in prod it always fails, and the error is discarded.
  - The reader reads the file on every connection (`braintown_consumer.go:381-383`)
    and sends its value as `Last-Event-ID` when it is above 0.
  - **Empty file:** no `Last-Event-ID`. Brain Town then streams live events only,
    with no replay. This is an **external** anchor: the separate brain-town repository,
    `backend/internal/admin/consumer.go:121-142`, read at commit `9acf910`. Events
    during a gap are lost.
  - **File holding a value N above 0:** every reconnect replays from N. The PR flow
    drops some duplicates (`dashboard/prflow.go:395`).

  The fix needs a writable state directory, a change to the secret linking in
  `deploy/rebuild-restart.sh` and to the `check-stacks.sh` assertion. That is a
  deployment change with its own design. It is filed as a GitHub issue when this
  change's PR opens, and the PR links it.
- Changing any state string, the Contacts form or a docs page.

## 9. Security

The page is behind the back-office session. The mux is default-deny
(`dashboard/auth.go:541-590`), and `/ui/bo/{view}` is not in the public list
(`auth.go:516-524`). What reaches the page:
- **New, fixed text:** the chip labels (the nine of §3.2) and the popover texts
  (§3.3e).
- **A CSS class built from `Tone`:** one of three fixed words, escaped by
  `html/template`.
- **Three State rows:** values the page printed before this change, moved from the
  heading into a row.

No new value derived from configuration reaches the page. TC-764 asserts that every
chip's text belongs to the closed label set, so a later edit that put a reason or a
host into a chip would fail. The existing whole-body secret check
(`integrations_test.go:128-131`) still covers the page.

Two security signals improve:
- An unencrypted or product-less WhatsApp link is amber, not green.
- The `ip-api` text no longer claims that turning the Machine API off closes every
  inbound machine route: the staging callback has its own credential.


**Renumbered on rebase (2026-11-27).** While this design was in review, PR #54
(`feat/board-list-ux`) merged and took D-097, TC-753…TC-762 and v0.59.0. Following
§5's rule, this change now uses **D-098, TC-763…TC-766 and v0.60.0**, and every count
in §5 starts from main's figures at `bf0e863`: 762 cases, 626 Go tests (602
dashboard), "Ninety-six entries". The triage tables below keep the numbers each
round saw.

## 10. Triage of round 1

Severities are as reported. Dispositions: PROPOSE FIX (fixed in this revision),
CONTEST, DECLINE, ESCALATE.

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| Removing the heading suffix deletes the only display of three states; TC-512 breaks | all four + critic | Blocker | PROPOSE FIX | State rows, §3.3b; TC-512 named in §4 |
| Change list omits `check-workspace.sh` and `README.md` pins; §8 fails | critic | Blocker | PROPOSE FIX | §5 table, §6 #11–13 |
| "Mandatory" contradicts BR-27, no decision recorded | critic; comprehension, readiness, zoom-out (Major/Nit) | Blocker | PROPOSE FIX | D-097 defines the term (workflow, not start-up); popover sentence; BR-27 is about submodules and holds |
| Tone ordering: optional Disabled red, Unusable amber | critic | Major | PROPOSE FIX (part) | Unusable is now red. Optional Disabled stays red: the operator asked for red/green explicitly. The Optional chip carries the "allowed" signal. §2 |
| Green chips claim unchecked health (QA 500, API without QA token, WA unencrypted) | critic, zoom-out, readiness, SEC-1, SEC-4 | Major/Medium/Low | PROPOSE FIX | Degraded·warn rules, §3.2; `QA.Failing` |
| PR roll-up hides a broken sub-part | critic | Major | PROPOSE FIX | new `PRChip` rule |
| PR row chips unplaced / untested | comprehension, zoom-out, critic | Major | PROPOSE FIX | §3.3c markup; TC-754 positive |
| CSS left as placeholder comments; position and exit motion undecided | readiness | Major | PROPOSE FIX | §3.4 written out; centred; no exit animation |
| No phases; verification from wrong directory | readiness, comprehension | Major/Nit | PROPOSE FIX | §7 |
| Dashboard count 592 not in the edits | readiness, zoom-out | Major/Nit | PROPOSE FIX | §5 |
| Decision and escalation have no id | readiness, comprehension, zoom-out, critic | Major/Nit | PROPOSE FIX | D-097; the cursor issue goes to a GitHub issue linked from the PR (it is a defect, not a product question, so not an OQ) |
| `ip-api` claims every machine call is refused (staging is open) | SEC-2 | Medium | PROPOSE FIX | §3.3e text; TC-756 asserts it |
| Cursor bullet describes replay backwards | SEC-3 | Medium | PROPOSE FIX | §8 states both outcomes, with Brain Town's no-id behaviour verified |
| `ip-bt` overstates anonymisation | SEC-5 | Low | PROPOSE FIX | §3.3e text |
| TC-756 secret check cannot fail; no closed-label check | SEC-6, zoom-out | Low/Nit | PROPOSE FIX | dropped; TC-754 closed-set check |
| Tone vocabulary collides with Auto-fix `off` | zoom-out | Nit | PROPOSE FIX | `ok/warn/bad`, §3.1 |
| `Mandatory` bool in the view model | zoom-out | Nit | PROPOSE FIX | static markup; `IntegHead` removed |
| Popover overclaims (runner, board, given-up) | critic | Nit | PROPOSE FIX | §3.3e |
| PR popover vs "Partly connected" | comprehension | Nit | PROPOSE FIX | §3.3e `ip-pr`, and the roll-up rule |
| Machine API has no docs link | comprehension, zoom-out, critic | Nit | PROPOSE FIX | §3.3d |
| Accessibility: `--ink-3` contrast, button label, button inside `h4` | critic, readiness | Nit | PROPOSE FIX | `--ink-2`; `aria-label "<name>: info"`; button outside `h4` |
| Wrong anchors (integrations.go, app.css themes, test line, compose path, `#sink`, auth.go) | all | Nit/Low | PROPOSE FIX | corrected throughout |
| Claim rests on `.claude/rules/frontend.md` | comprehension | Nit | PROPOSE FIX | replaced with the static file list |
| "One h3" ambiguous | comprehension | Nit | PROPOSE FIX | one `h3` per test |
| Concurrent work may take the same ids | critic | Nit | PROPOSE FIX | renumber-on-rebase rule, §5 |
| No risks section | critic | Nit | DECLINE | the risks raised (alarm fatigue, stale chips, old browsers) are each handled in §2, §3.2 and §3.5; a separate section would repeat them |

## 11. Triage of round 2 (critic, readiness)

Round-1 Blockers: two **resolved** by both lenses (State rows; count pins), one
**partly resolved** per the critic (D-097), now completed below.

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| Version bump leaves `page.html` `?v=` behind; TC-690 fails and prod serves old CSS | critic, readiness | Blocker | PROPOSE FIX | §5 row, change 13a, phase 3 re-runs the dashboard suite, and a stale-literal grep before "complete" |
| PR roll-up ignores the reader, the only PR event source | critic | Major | PROPOSE FIX | `PRChip` rules include the reader; `ip-btc` text rewritten; TC-753 negative |
| QA amber from all-time `GaveUp` never clears | critic, readiness | Major | PROPOSE FIX | `GaveUpRecent` (24 h) drives the chip; TC-753 case "old give-up clears" |
| "Degraded" applied to QA only; triage overstated | critic | Major | PROPOSE FIX (part), DECLINE (part) | Fixed: feed and reader `Failing` (both clear on success), Machine API with no QA products. Declined, with reasons in §3.2: ai-manager provider availability (stale cache, usually "not asked since boot") and the feed's lifetime counters (never clear). |
| D-097 encodes one deployment's view in the open product | critic | Major | CONTEST | The QA loop is GAOS's own product workflow (D-072, D-075), not this deployment's. An install without the QA tools shows the blocks red with Mandatory, which is true: its QA loop is not running. §2 now says this and cites both decisions. |
| `.block h4` overrides `.integ-h h4` | critic, readiness | Nit/Major | PROPOSE FIX | rules appended at the end of `app.css`; live check for alignment |
| TC-754 State-row check vacuous for two rows | critic, readiness | Nit/Major | PROPOSE FIX | fixture sets `brainOff`/`btConsumerOff`; exact cells asserted |
| Red chip text under 4.5:1 in dark theme | critic | Nit | PROPOSE FIX | `--ink` text on a 22 % tint; colour on fill and border |
| §3.5 `#sink` claim wrong | critic | Nit | PROPOSE FIX | `#boSheet` out-of-band, `backoffice.html:46` |
| Register mechanics: h3 per test, ids, D-097 cells, changelog | critic, readiness | Nit | PROPOSE FIX | one `h3#go-integ-chips`; D-097 cells in §2; changelog entry in §5; `%s`/`%q` allowed |
| `ip-aim` runner overclaim | critic, readiness | Nit | PROPOSE FIX | text names on/manual/off |
| Unconfigured optional sub-part reads as a problem | critic | Nit | PROPOSE FIX | §2 amber definition; `disabled` staging/WA ignored by `PRChip` |
| External brain-town anchor unlabelled | critic | Nit | PROPOSE FIX | labelled, with commit `9acf910` |
| Phase-2 grep from wrong directory | readiness | Nit | PROPOSE FIX | subshell |
| "State word" undefined | readiness, critic | Nit | PROPOSE FIX | "no `h4` contains `&middot;`" |

No Blocker survives round 2. Round 3 is not run: every round-2 finding is resolved by
a fixed value in this document, and the operator reviews it next.

**Implementation note (2026-11-27).** The requirement chips are `chip need-req` and
`chip need-opt`, not `req`/`opt` as drafted: `.opt` is already the answer-option
card (`app.css:1165`), and on the chip it drew a padded box.
