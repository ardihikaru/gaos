# Pustaka: a page that is not broken while it loads, article-only page transitions, and public GAOS docs

**Status:** Approved (v2). The operator approved it on 2026-11-26 ("Approve, login off"). Round 1 reviewed v1 with five lenses: `reviews/pustaka-loading-and-motion-{comprehension,zoomout,critic,readiness}-1.md` and `reviews/pustaka-loading-and-motion-security-review-1.md`. The triage is in `reviews/pustaka-loading-and-motion-review-1.md`. Every `PROPOSE FIX` is applied below.
**Feature:** `pustaka-loading-and-motion`
**Version target:** v0.55.0 (main is v0.54.0, `pustaka/docs/assets/toc.js:22`).
**Numbers claimed:** **D-093**, **TC-721**, **v0.55.0**. No BR is added or amended, and no OQ is opened or answered (see §1.3 on OQ-36).
**BRD rules this must obey:** none of §9's rules govern how the docs site renders. BR-55 covers the back office only. No event is written, so BR-05 is untouched. `pustaka/docs/product/brd.html` §9.
**Security lens:** required. The change switches off an authentication layer and adds an inline script to every page the docs server serves.

## 0. Glossary

- **pustaka**: the documentation server and its runtime. `pustaka/` is a vendored fork of an upstream repository (`deploy/Dockerfile.pustaka:3-6`: "an upstream update should be a copy, not a merge").
- **Upstream**: `github.com/ardihikaru/pustaka`. This change is an **upstream sync to commit `1ddec13`**, the v0.9.1 merge (assumption A1, which a reviewer can check with `git -C <upstream> show 1ddec13`).
  - v0.9.0 (`e2d40d5`) carries the loading fix and the motion.
  - v0.9.1 (`1ddec13`) carries three fixes this design's review found: the cache revalidation, the login reveal and the preload base pattern.
  - Both are deployed on upstream's own sites.
- **The runtime**: `pustaka/docs/assets/site.js`. It builds every page's header, sidebar, on-page ToC, prev/next links and footer in the browser:
  - header: `site.js:139-157`;
  - sidebar: `198-209`;
  - on-page ToC: `286-289`;
  - prev/next and footer: `301-316`.

  Every GAOS page's `<body>` opens with `<div class="shell">`. The readiness lens checked this across all 33 pages. Each page ends with the two runtime scripts (`pustaka/ai/AUTHORING_SPEC.md` §1 rules 4 and 6).
- **Registry parts**: the ToC files listed in `pustaka/docs/assets/toc.js` `parts`. GAOS has eight of them.
- **The login layer**: pustaka's cookie login. It is active when `PUSTAKA_AUTH` is `1`, `true`, `yes` or `on` (`pustaka/main.go:662-663`).

## 1. Problem

### 1.1 A page looks broken until it has loaded

`site.js` runs last in `<body>`, after the whole page has been parsed. The QA plan page is 654 KB. Then `boot()` loads the eight registry parts one after another (`site.js:28-36`), which costs eight round trips before `init()` builds any chrome. Until that finishes, five things are wrong:

1. **The article sits in the sidebar column.** `.shell` is a three-column grid, `248px minmax(0,1fr) 208px` (`site.css:116-121`). `main.doc` is its only child, so auto-placement puts it in the 248 px column. It jumps to the middle when the sidebar arrives (`site.js:209`). The ≤1160 px column is 232 px (`site.css:611-614`).
   - Measured in headless Chrome at 1280 px with the runtime delayed 2 s:
     - mid-load: the article at x=24, 248 px wide;
     - loaded: x=272, 776 px wide.
2. **The page shifts down 56 px** when the header is prepended (`site.js:157`).
3. **The landing hero renders squeezed.** Its styles key on `body.layout-landing` (`site.css:400-413`, 8 selectors), which only the runtime adds (`site.js:158-163`).
4. **Dark-theme readers see light first.** The runtime sets the theme late (`site.js:118-121`), and `body` animates its colours (`site.css:61`). Measured mid-load: background `rgb(251,251,248)`, no `data-theme`.
5. **Reveal blocks are invisible.** `.rv` starts at `opacity:0` (`site.css:580`).

### 1.2 Navigation motion

Partial navigation uses the default root crossfade (`site.js:578`), so the header and sidebar fade too. The fallback is a 300 ms `fadeup`, cleared after 350 ms (`site.js:579`). Nothing signals a slow navigation, and buttons have no press state. The operator asked for this polish explicitly, following Emil Kowalski's motion skills. It ships here because it lives in the same two files as the fix, and the sync is a whole-file copy.

### 1.3 The GAOS docs are behind a login

Both prods answer `{"auth":{"enabled":true,…}}` on `/docs/__pustaka/info` (checked 2026-11-26).

**The repository is already public.** `gh repo view --json visibility` returned `PUBLIC` on 2026-11-26. So every tracked file under `pustaka/docs/` can already be read on GitHub. Turning the login off makes the rendered site public; it does not publish any new content.

- **OQ-36:** OQ-36 (`open-questions.html:884-902`) and D-085 (`decision-log.html:1804-1805`) still describe the repository as private. Moving the register in line with reality means answering OQ-36. That belongs to the operator and is **not** done here; it is escalated in the triage.
- **The decision:** the operator decided to switch the docs login off and keep its code.

## 2. Goals and non-goals

**Goals**

- G1. **First paint already has the final layout.** From the first paint, the article's column (x and width) and the header strip do not move when the chrome arrives. The runtime still fills content *inside* the article afterwards: `.page-meta`, heading anchors, prev/next and the footer.
- G2. **No light flash.** A dark-theme reader never sees the light palette, including a returning reader whose browser has the page cached.
- G3. **The chrome arrives after one round trip.** The chrome is built after one parallel batch of registry downloads.
- G4. **Motion follows the vendored bar.** Each rule is quoted here so the design stands alone:
  - "entering or exiting → ease-out";
  - "never ease-in on UI";
  - "UI animations stay under 300 ms";
  - "button press feedback: scale(0.97) on :active, 160 ms";
  - "stagger 30–80 ms between items; stagger is decorative".

  Two exemptions. The progress bar is constant motion and is not a UI transition. The stagger *delay* is not counted in the duration.
- G5. **The GAOS docs are public on both prods.**

**Non-goals**

- N1. No page's `<head>`, chrome or structure is edited for the rendering fix. Only register and release content changes (§3.6–3.7).
- N2. The login code stays. All 22 pustaka tests (TC-087…TC-108) keep passing.
- N3. No cross-document view transitions: the new document's first frame has no header.
- N4. `dashboard/` is not touched, except the `?v=` asset version in `templates/page.html` that every release bumps (`TestStaticAssetsAreVersionedByTheProductVersion`).
- N5. No caching change other than the `Last-Modified` floor (§3.4).
- N6. No rate limits, no container resource limits, no `robots.txt`. Escalated in the triage.
- N7. The dev tier is retired: its compose file stays in the tree as a record and nothing deploys it. Its `dev.docs.env` is not touched.

## 3. Design

### 3.1 The sync

Copy three files from upstream `1ddec13`, then re-apply the fork's deltas. The copy is authoritative; §3.2–3.4 summarise what it contains and cite the fork's resulting lines.

| File | How it is synced |
|---|---|
| `docs/assets/site.js` | Copied whole. Before the copy it is byte-identical to upstream's pre-change file (assumption A2: `diff` of the fork against upstream `4da28e2:docs/assets/site.js` is empty). |
| `docs/assets/site.css` | Upstream diff `4da28e2..1ddec13` applied as a patch. It applies cleanly with offsets, because the fork lacks only upstream's `.dev`/`.tagpin` device-mockup rules, which the diff does not touch. |
| `main.go` | Upstream diff applied as a patch (clean). The fork's `sitePrefix`/`trimSitePrefix` code is untouched by it. |
| `main_test.go` | Upstream diff applied. Hunk 1 (the import block) is rejected because the fork's imports differ, so `"compress/gzip"` and `"io"` are added by hand. |
| `docs/_login.html` | Upstream `1ddec13` hunk applied: `document.documentElement.classList.add("ready")` at the top of its head script. |

A scratch run of exactly this procedure passed `go test ./...` and `go run . check ./docs` (33 pages) on 2026-11-26.

### 3.2 `site.css`

**Layout before the runtime arrives:**
- `.shell` gets named areas, `"nav doc toc"`. The ≤1160 px rule gets `"nav doc"`, ≤920 px gets `"doc"`, and landing gets `"doc"`. Then:
  - `.shell > .doc{grid-area:doc}`
  - `.shell > .sidebar{grid-area:nav}`
  - `.shell > .pagetoc{grid-area:toc}`
- The header is reserved by `body > .shell:first-child{margin-top:var(--header-h)}`.
- A fixed 56 px placeholder strip in the header's colours renders while `body:not(:has(> .hd))`.
- On non-landing pages wider than 920 px, a static sidebar skeleton renders while `.shell:not(:has(> .sidebar))`. It is a `repeating-linear-gradient` with a mask and no animation.
- All 8 `body.layout-landing` selectors become `body[data-layout="landing"]`.

**Theme and reveal:**
- `html:not(.ready) body{transition:none}`.
- `.rv` is no longer hidden by default.
- `html.ready .rv:not(.in){opacity:0; transform:translateY(12px)}`.
- `.rv` transitions over 280 ms ease-out.

**Motion:**
- `--ease-out: cubic-bezier(.23,1,.32,1)`. No rule uses ease-in.
- Page transition:
  - `.doc{view-transition-name:pustaka-doc}`.
  - The root and the `pustaka-doc` group are not animated.
  - Old article: `pk-doc-out`, 100 ms, to `opacity:0; translateY(-4px)`.
  - New article: `pk-doc-in`, 180 ms with a 50 ms delay, from `opacity:0; translateY(6px)`.
  - Fallback: `.doc.pk-enter{animation:pk-doc-in .18s var(--ease-out) both}`.
- Press: `.icon-btn`, `.search-btn`, `.btn`, `.pn a` and `.top-btn.show` take `:active{transform:scale(.97)}`.
  - `.icon-btn` gets `transition:background .15s, color .15s, transform .16s var(--ease-out)`.
  - `.search-btn` adds `transform .16s var(--ease-out)` to its existing list.
  - `.btn` (`transform .2s var(--ease)`, `site.css:323`) and `.pn a` (`site.css:276`) keep their own transitions unchanged.
- Drawer, at ≤920 px and on landing: closes in 180 ms, opens in 280 ms (`.sidebar.open`), both ease-out.
- Active marker: `scan` runs from `scaleY(.4)` with opacity 0, over 240 ms ease-out.
- Progress bar:
  - `.pk-progress`: `position:fixed; z-index:120; top:0; left:0; right:0; height:2px; background:var(--accent); transform-origin:left; transform:scaleX(0); opacity:0; pointer-events:none`.
  - `.run`: `opacity:1; transform:scaleX(.8); transition:transform 6s cubic-bezier(.1,.6,.2,1), opacity .12s`.
  - `.done`: `opacity:0; transform:scaleX(1); transition:transform .18s var(--ease-out), opacity .2s .12s`.

**Reduced motion.** The existing global block shortens every transition to 0.01 ms (`site.css:654-658`), so the bar appears at 0.8 and disappears without movement. It also gains:
- `::view-transition-group(*),::view-transition-old(*),::view-transition-new(*){animation:none !important}`;
- `html.ready .rv:not(.in){opacity:1; transform:none}`.

### 3.3 `site.js`

1. **Parallel parts.** `loadPart` sets `t.async = false`. Scripts inserted dynamically with `async=false` download in parallel and execute in insertion order. `boot()` awaits `Promise.allSettled(parts.map(loadPart))` and logs each rejection with the existing `pustaka: failed to load toc part:`.
2. **Ready and reveal.** After the first `wirePage()`, `<html>` gets `ready`. `wirePage()` gives `.in` to every `.rv` whose bounding box intersects the viewport, and observes the rest. This also runs after a swap.
3. **Stagger.** In one IntersectionObserver batch, item *k* gets `transitionDelay = min(k,6)×50 ms`, or none under reduced motion.
4. **Progress.** One `.pk-progress` element.
   - `progStart()` resets its class to `pk-progress`, which ends any previous run, and arms a 150 ms timer that adds `run`.
   - `progEnd()` clears the timer and replaces `run` with `done`.
   - Overlapping swaps (for example a `popstate` during a fetch) restart the bar; the last `progEnd` wins.
   - A failed fetch falls back to a full load, as today (`site.js:536`).
5. **Swap.** Under reduced motion, `apply()` runs directly. Otherwise it uses `startViewTransition(apply)`, or the `.pk-enter` class for 240 ms where that is unavailable.

### 3.4 `main.go`: the pre-paint head snippet

**Where it runs.** In the static handler, after the `Cache-Control` switch, `.html` files whose base name does not start with `_` take a new path:
- read with `io.ReadAll(f)`;
- served with `http.ServeContent(w, r, full, servedModTime(fi.ModTime()), bytes.NewReader(injectHead(b)))`;
- a read error answers 500 `read error`.

**The cache floor.** `servedModTime` returns the later of the file's mtime and `startedAt` (process start, UTC, truncated to the second). A revalidation of an unchanged page therefore gets 200 with the snippet after a deploy, never 304 for bytes it does not have. The same holds when the snippet changes in a later release.

**What `injectHead` inserts.**
- It inserts, verbatim, before the first case-insensitive `</head>`, followed by `\n`:
  - the marker `<!-- pustaka:head -->`;
  - `headTheme`, which is `<script>try{var t=localStorage.getItem("pustaka-theme");document.documentElement.dataset.theme=t||(matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light")}catch(e){}</script>`;
  - when `reAssetBase` (`href="((?:\.\./)*)assets/site\.css"`) matches, `<link rel="preload" href="<base>assets/fonts/atkinson-hyperlegible-latin-400-normal.woff2" as="font" type="font/woff2" crossorigin>`.
- A page that already carries the marker, or has no `</head>`, is returned unchanged.

**Invariant: the snippet holds no request-derived bytes** (no Host, no header, no query). It is built only from the page file and constants. This matters because `/docs` and `/app` share one origin (`deploy/traefik/dynamic.yml:1-2,40-48`). A future CSP can allow the script by its hash.

**What it does not reach:**
- assets;
- `_template.html` and `_login.html`;
- the partial endpoint (`main.go:512-551`, JSON without a `<head>`);
- the login page, which `renderLogin` serves and which carries its own theme script.

### 3.5 Tests and the register

**TC-721, `TestHeadInjection`** (upstream's test, from `1ddec13:main_test.go`). It uses `writeServeFixture` and `testHandler(t, root, nil)`.

Positive:
1. `/`, `/index.html` and `/guide/x.html` answer 200. Each has the marker once, before `</head>`, plus the theme script and the preload with base `""`, `""` and `"../"`. `Content-Length` matches the body.
2. Gzip carries the marker once.
3. `injectHead` is idempotent.

Negative:
1. `/assets/site.css`, `/_template.html` and the partial carry no snippet.
2. A revalidation at an hour-old file mtime is not a 304 without the snippet.
3. The snippet is identical for `Host: a.example` and for a hostile `Host`/`X-Forwarded-Prefix`.
4. A base of `https://x/` gets no preload.
5. A page without `</head>` is unchanged.

Messages, from the file:
- `%s: marker appears %d times, want 1`
- `%s: snippet is not inside <head>`
- `%s: theme script missing`
- `%s: preload %s missing`
- `%s: Content-Length %q, body %d bytes`
- `an asset must not be injected`
- `an underscore page must not be injected`
- `the partial endpoint must not carry the head snippet`
- `Content-Encoding %q, want gzip`
- `the gzip body lost or doubled the snippet`
- `a revalidation at the file's mtime answered %d without the snippet`
- `the snippet varies with the request:\n%s\n%s`
- `a non-relative base produced a preload: %s`
- `injectHead is not idempotent`
- `a page without </head> changed: %q`

**Register.**
- Row: `<tr data-tags="go test unit pustaka head injection theme preload cache positive negative"><td>TC-721</td><td>pustaka (go test)</td><td>Positive</td><td>Served pages carry the pre-paint theme snippet once, before &lt;/head&gt;, never on assets, underscore pages or partials, never request-derived, and never lost to a 304 (<code>TestHeadInjection</code>)</td><td>Implemented</td></tr>`, appended after TC-720.
- Prose: `h3 id="pk-head"` titled "pustaka — the pre-paint head snippet (TC-721)", placed directly after `#pk-prefix` (`qa-test-plan.html:1872`). It carries the positive and negative halves with the messages above, plus a **manual check** paragraph (headless Chrome, recipe in §7) for G1–G3 and for reduced motion.

**Counts.**
- 720 → 721 in four places: `qa-test-plan.html:19,79` and `toc/product.js:162,168`.
- 586 → 587 Go tests (`toc/product.js:168`): 564 + 22 + 1.
- "Ninety-one entries" → "Ninety-two entries" (`decision-log.html:18`), and "Ninety-one decisions" → "Ninety-two decisions" (`toc/product.js:129`).

### 3.6 Switching the GAOS docs login off

- **Host (untracked, both prod VMs):** in `deploy/stacks/prod.docs.env`, set `PUSTAKA_AUTH=` and comment out `PUSTAKA_AUTH_USER`, `PUSTAKA_AUTH_PASS` and `PUSTAKA_AUTH_SECRET` with a leading `# off (D-093): `. They then no longer reach the container's environment.
  - Switching back on means removing those prefixes, setting `PUSTAKA_AUTH=1` and generating a fresh secret (`openssl rand -hex 32`).
  - With the layer off:
    - no auth route is registered and `/info` has no `auth` key;
    - the header shows no sign-in control (`site.js:612-613`);
    - the hero shows no Sign-in button (`docs/index.html:226-246`).
- **`deploy/stacks/docs.env.example`:** add, above `PUSTAKA_AUTH=`: `# GAOS's own docs are public (D-093): leave this empty on the prods.`
- **`deploy/stacks/compose.prod.yaml:214-218`:** replace the comment with: `# The login layer is OFF on GAOS's prods (D-093): the docs are public, as the repository is. It still works behind the /docs strip if switched on (pustaka reads X-Forwarded-Prefix, ../traefik/dynamic.yml). Names in docs.env.example.`
- **`deploy/STACKS.md:140`:** `docs.env.example (the docs login)` becomes `docs.env.example (the docs login, off since D-093)`.
- **`deploy/STACKS.md:150-151`:** prefix with "Only if the docs login is switched back on:".
- **`pustaka/docs/infra/environments.html:166`:** "The docs login and its cookie secret" becomes "The docs login (off since D-093) and its cookie secret".
- **`pustaka/docs/infra/environments.html:181-187`:** the admonition's title becomes "If the docs login is switched back on, generate its cookie secret on the host".
- **`deploy/Dockerfile.pustaka:20-25`:** unchanged. It describes how the layer works, which remains true.

### 3.7 Registers and release

**D-093**, appended to the decision log in its row format:

> **GAOS's own documentation is public: the pustaka login layer is switched off on both prods, and its code is kept.**
>
> *Context.* The repository is public (2026-11-26), so the docs content already is. The login only hid the rendered site.
>
> *Decision.* Set `PUSTAKA_AUTH` empty on both prods and comment out the credential lines. Keep the code (N2).
>
> *Residual.* The published infra pages carry real hostnames and addresses that are equally visible in the repository; a redaction is a separate change (triage, SEC-1). OQ-36 is not yet answered in the register.
>
> *Evidence:* `docs/designs/pustaka-loading-and-motion.md`, `qa-test-plan.html#pk-head`.

**Changelog v0.55.0.** One entry: Fixed (the page is broken while it loads; root cause and fix as in §1.1 and §3.2), Fixed (dark flash), Changed (parallel parts), Added (article-only transitions, progress bar, press, drawer), Changed (the docs are public, D-093), Added (TC-721). Bump `toc.js` semver to 0.55.0.

### 3.8 Files outside the docs that pin the counts

- **`deploy/check-workspace.sh`:**
  - `:114` and `:118`: "five hundred and eighty-six" → "five hundred and eighty-seven".
  - `:115`: `586` → `587`.
  - `:129-130`: "seven hundred and twenty" → "seven hundred and twenty-one".
  - `:131`: "Ninety-one entries" → "Ninety-two entries".
- **`README.md`:**
  - `:12`: 0.54.0 → 0.55.0.
  - `:137`: "586 Go tests: 564 in `dashboard/` and 22 in `pustaka/`" → "587 Go tests: 564 in `dashboard/` and 23 in `pustaka/`".
  - `:139`: 720 → 721.
- **`.claude/rules/qa-test-plan.md`:**
  - the highest ID becomes TC-721, next free TC-722;
  - "seven hundred and twenty" becomes "…twenty-one";
  - "586" and "eighty-six" become "587" and "eighty-seven";
  - "Ninety-one entries" becomes "Ninety-two entries";
  - the `pustaka (go test)` map row becomes `TC-087…TC-108, TC-721`;
  - the totals line becomes "721 rows … 564 in `dashboard/` and 23 in `pustaka/` — 587 Go tests".

## 4. Alternatives rejected

- **Remove the login code.** Rejected by the operator.
- **A head script in every page.** It edits 33 pages and the authoring contract. Server injection reaches every page with no edits.
- **Server-render the chrome.** The server already parses the registry (`loadRegistry`, `main.go:148`), but this means rewriting the runtime and dropping static mode (`site.js:2-10`), several times this change's size. The CSS placeholders remove every visible defect listed in §1.1.
- **CSS-only dark default (`@media (prefers-color-scheme: dark)`).** It would not cover a reader whose saved choice differs from their system setting, and the dark palette is keyed on `[data-theme="dark"]` across several selectors.
- **Cross-document view transitions.** The header would blink (N3).
- **Drop the font preload** (zoom-out N). Declined: it is the one fetch the browser cannot discover until CSS is parsed, and it costs one regex.

## 5. Risks

- R1. **`:has()`** support (Chrome 105, Safari 15.4, Firefox 121). Without it the placeholders do not render; the grid fix does not depend on `:has()`.
- R2. **Per-request read.** Each page view reads the file into memory and builds one copy with the snippet, as the partial endpoint already does (`main.go:526`). Accepted. A per-path cache in `--prod` is escalated with the other resource limits (N6).
- R3. **A literal `</head>` before the real one.** Not present: every page has exactly one (`grep -ci '</head>'` = 1 across `pustaka/docs/*.html` and `*/*.html`).
- R4. **Exposure.** The rendered site becomes as public as the repository already is. The infra pages name real hosts and addresses (security SEC-1). Those are already public on GitHub, and their redaction is a separate change (ESCALATE).
- R5. **Runtime failure.** If `init()` throws (for example, every registry part fails, so `HERE` is undefined at `site.js:47,228`), the header strip and sidebar skeleton stay on screen. Today such a page is unstyled chrome-less text. Accepted: it is equally broken, and more readable.

## 6. Change list

| # | File | Change |
|---|---|---|
| 1 | `pustaka/docs/assets/site.css` | §3.1–3.2 |
| 2 | `pustaka/docs/assets/site.js` | §3.1, §3.3 |
| 3 | `pustaka/main.go` | §3.1, §3.4 |
| 4 | `pustaka/main_test.go` | §3.1, §3.5 |
| 5 | `pustaka/docs/_login.html` | §3.1 |
| 6 | `pustaka/docs/product/qa-test-plan.html` | §3.5 |
| 7 | `pustaka/docs/assets/toc/product.js` | §3.5 |
| 8 | `pustaka/docs/product/decision-log.html` | §3.5, §3.7 |
| 9 | `pustaka/docs/changelog.html` | §3.7 |
| 10 | `pustaka/docs/assets/toc.js` | §3.7 |
| 11 | `pustaka/docs/infra/environments.html` | §3.6 |
| 12 | `deploy/stacks/docs.env.example` | §3.6 |
| 13 | `deploy/stacks/compose.prod.yaml` | §3.6 |
| 14 | `deploy/STACKS.md` | §3.6 |
| 15 | `deploy/check-workspace.sh` | §3.8 |
| 16 | `README.md` | §3.8 |
| 17 | `.claude/rules/qa-test-plan.md` | §3.8 |
| — | `prod.docs.env` on both prod VMs | untracked, §3.6 |

17 tracked files. The count is the 11 in v1, plus `_login.html` (the upstream fix), `STACKS.md` and `environments.html` (stale prose), and the three count pins in §3.8. That is 11 + 1 + 2 + 3 = 17.

## 7. Phases and verification

1. **Code and registers.**
   - Exit when all of these pass:
     - `cd pustaka && go test -run TestHeadInjection -v ./...` → `--- PASS: TestHeadInjection`;
     - `go test ./...` → `ok`;
     - `go run . check ./docs` → `✓ 33 pages valid`;
     - `cd dashboard && go test ./...` → `ok`;
     - `deploy/check-workspace.sh` with the forbidden-strings list present → 0 FAIL, with the count lines reading `Go test count: 587`;
     - `deploy/check-stacks.sh` → 0 FAIL.
   - **Manual check.** Headless Chrome (Playwright, system Chrome) against `go run . serve docs`.
     - Setup: the route `/assets\/(site|toc)\.js$|assets\/toc\//` is delayed 2 s. Measure at 600 ms after `main.doc` attaches, and again after `.hd` exists. Run at 1280, 1024 and 390 px, in light and dark.
     - G1: `main.doc`'s x, y and width are equal in both measurements.
     - G2: in dark, the mid-load `getComputedStyle(body).backgroundColor` is `rgb(12, 20, 24)`.
     - Navigation: a partial navigation delayed 600 ms shows `.pk-progress.run`, then `.done`, with no page error.
     - G3: in DevTools, the eight part requests overlap in the network waterfall.
2. **Release.** PR (repo template) → merge → tag `v0.55.0`.
3. **Prod 1.** Edit `prod.docs.env` per §3.6, then `deploy/rebuild-restart.sh prod deploy --tag v0.55.0`.
   - Exit when:
     - `curl -s https://<host>/docs/__pustaka/info` has no `"auth"`;
     - `/docs/guide/getting-started.html` answers 200 with no cookie and carries `<!-- pustaka:head -->` once;
     - `/docs/assets/toc.js` shows `semver: "0.55.0"`.
   - If the docs container does not come up, restore the env file's three lines plus `PUSTAKA_AUTH=1` and redeploy the previous tag.
4. **Prod 2.** Same as prod 1, with that instance's override file.
