# Landing page: professional copy, two languages, richer motion

**Status:** Approved (round 1: no Blocker; every Major dispositioned — see §11 and `reviews/landing-bilingual-review-1.md`)
**Target release:** v0.50.0 (minor)
**Author's note on approval (an assumption — it has no anchor in the
repository):** in the conversation that requested this change the operator
wrote "NEVER ask me for confirmation or any decision. work among agents to
decide it". The author reads that as delegating step 8 of
`docs/rules/gaos-design-review.md` to the review. The condition used instead:
no Blocker survives; every Major has a disposition; no Major is disposed of
by `CONTEST` or `DECLINE` unless a second lens proposed or accepted that
outcome; and the Majors and the triage table are shown to the operator in the
pull request and the delivery summary.

**Where this document and §11 disagree, §11 wins.** §11 is the amendment
written after review round 1.

## 1. Problem

The landing page (`landing-page/index.html`, 268 lines) has four defects.

1. **It is Indonesian only.** `<html lang="id">` (`index.html:2`), and every
   string is Indonesian. The repository's README, docs and licence are English,
   and the page is the first thing a visitor from outside Indonesia sees.
2. **Its facts are stale.** It says 71 graph types and 49 event types
   (`index.html:123-124`). The recipe in its own comment (`index.html:115-119`)
   gives 31 + 40 + 3 = 74 and 59 today. It lists "Review" as not built
   (`index.html:171`); `README.md:46-47` says the design gate's review is built
   and that the unbuilt parts are the scoring engine, the retry guards and the
   budget guard.
3. **Its voice is a manifesto, not a project page.** The headline is a
   contrast slogan (`index.html:59`), the footer says the page "will not
   pretend otherwise" (`index.html:264`), and there is no quick start, no
   licence, no contribution path and no link to the source. A reader deciding
   whether to try an open-source project finds none of what they look for.
4. **It is nearly static.** One reveal fade and one looping card
   (`landing-page/css/03-devices.css:76-98`). The operator asked for a page
   that is "much more rich and animated, including the background, when
   moving".

## 2. Goals

- G1. English and Indonesian copy, written as a technical writer would: plain
  declarative sentences, facts that can be checked in the repository, no
  slogans, no contrast formulas ("not X but Y"), no superlatives.
- G2. Indonesian by default for a visitor in Indonesia, English otherwise, a
  visible switch, and the choice remembered.
- G3. The sections an open-source visitor expects: quick start, architecture,
  licence, contributing, source link.
- G4. Richer motion: an animated, pointer-reactive background and motion on
  scroll and on pointer movement, without breaking reduced-motion, the
  no-JavaScript path or the page's Content-Security-Policy.
- G5. A check that keeps the two languages from drifting apart.

## 3. Non-goals

- No change to the dashboard, the docs site's content pages (other than the
  changelog entry and the QA register this change requires), the Traefik
  routes or the nginx server block's behaviour.
- No server-side language negotiation and no IP geolocation (§5.2).
- No third language. The mechanism allows one; nothing is built for it.
- No new font, no third-party script, no build step, no npm.
- The Open Graph image (`landing-page/img/og-image.png`) is not redrawn.
- The repository's GitHub visibility is not changed by this work (§5.6).

## 4. Constraints the change must obey

- **CSP.** `index.html:10` sets `default-src 'self'; script-src 'self';
  style-src 'self'; connect-src 'none'`. No inline script, no inline `style`
  attribute, no network call from the page. Styles set from script go through
  CSSOM (`el.style.setProperty`), which `style-src 'self'` permits and which
  `landing-page/js/theme.js` already relies on.
- **No JavaScript must still read.** `landing-page/css/01-tokens.css:163-171`
  only hides `.rv` under `html.js`. The same rule holds for everything added.
- **Reduced motion.** `01-tokens.css:174-180` removes movement. Every new
  effect is off under `prefers-reduced-motion: reduce`.
- **Motion standard.** `transform` and `opacity` only, no `transition: all`,
  the two curves already defined (`01-tokens.css:49-50`), hover effects only
  under `(hover:hover) and (pointer:fine)`.
- **Every colour token exists in three blocks** (light, `[data-theme=dark]`,
  `prefers-color-scheme` without JS): `01-tokens.css:10-12`.
- **D-085.** No tracked file may name an operator's private instance or
  domain. The page therefore uses only relative URLs plus the public
  repository URL that `NOTICE` already publishes
  (`https://github.com/ardihikaru/gaos`).
- **The QA register.** A new check gets a row, prose and four counts in the
  same commit (`.claude/rules/qa-test-plan.md:19-24`).

## 5. Decisions

### 5.1 One HTML file, English in the markup, Indonesian in a dictionary

`index.html` carries the English text. Every translatable element has
`data-i18n="<key>"`; a translatable attribute has
`data-i18n-attr="<attribute>:<key>"` (one attribute per element; where two are
needed the second goes on a wrapper or is avoided). `landing-page/js/i18n.js`
holds one object, the Indonesian dictionary, written one key per line:

```js
var GAOS_ID = {
  'hero.title': 'Manajemen produk dan tugas untuk agen koding AI',
  ...
};
```

On first run the script records each element's original `innerHTML` (and
attribute value) as the English string, so English is never duplicated.
Switching language writes `innerHTML` from the dictionary or from the recorded
original. The dictionary is a static file served from `'self'`; no visitor
input reaches `innerHTML`.

A key missing from the dictionary leaves the English text in place. The page
never shows a key name or an empty element.

`<title>` and `<meta name="description">` carry `data-i18n` /
`data-i18n-attr` like any other element. The Open Graph tags stay English:
link-preview crawlers do not run the script.

**Rejected: two HTML files (`/` and `/id/`) with a redirect.** Better for
search engines, but it duplicates 150 lines of mock-up markup that must stay
identical, the redirect is a visible second navigation, and the nginx block
(`deploy/nginx/landing.conf:30-33`) would need a rule for the new directory.
**Rejected: both languages inline, hidden by CSS on `html[lang]`.** It doubles
the document and puts two languages in one page for every crawler and
find-in-page.

### 5.2 Language detection: the device's time zone, no network

Order of precedence, first match wins:

1. `?lang=en` or `?lang=id` in the URL. The value is saved (step 2's key).
2. `localStorage['gaos.landing.lang']` equal to `en` or `id`.
3. `Intl.DateTimeFormat().resolvedOptions().timeZone` is one of
   `Asia/Jakarta`, `Asia/Pontianak`, `Asia/Makassar`, `Asia/Jayapura`,
   `Asia/Ujung_Pandang` (the legacy alias of Makassar) → `id`.
4. Otherwise `en`.

Any exception (blocked storage, no `Intl`) falls through to the next step.
`navigator.language` is deliberately not consulted: the operator's rule is
about where the visitor is, and the time zone is the only location signal a
browser gives without a network call. A visitor for whom the guess is wrong
changes it with one click, and step 2 remembers it.

**Rejected: IP geolocation.** At the edge it needs a GeoIP database on the
public nginx host (an edge-config change, a third-party data file with its own
licence and update schedule). From the page it needs a third-party lookup,
which `connect-src 'none'` forbids and which would contradict `NOTICE`
("Nothing in this repository transmits data to a third party").
**Rejected: `Accept-Language` at nginx.** It reports language preference, not
location, and it also needs an nginx `map` plus two documents.

### 5.3 No flash of the wrong language

`js/i18n.js` loads in `<head>`, blocking, after `js/theme.js`. Before the body
is parsed it decides the language. If the result is `id` it sets
`<html lang="id">` and adds the class `i18n-wait`, for which
`01-tokens.css` declares `html.i18n-wait body{visibility:hidden}`. On
`DOMContentLoaded` it applies the dictionary inside `try … finally`, and the
`finally` removes `i18n-wait`. If the script fails to load the class is never
added, and the page shows English. An English visitor never gets the class.

### 5.4 The language switch

A two-button group in the header, `EN` and `ID`, each a `<button>` with
`aria-pressed`, inside an element labelled "Language". It stays visible at
every width (the text links already hide below 760px,
`02-layout.css:44`). Clicking saves the choice, sets `<html lang>`, and swaps
the text. With motion allowed, `<main>` fades to opacity 0 in 90ms, the text
is swapped, and it fades back in 180ms; under reduced motion the swap is
immediate. `?lang=` is not written back into the URL.

### 5.5 Copy

American English spelling ("license"), because the page addresses a general
open-source audience; the repository's own documents keep their British
spelling and are not touched. Indonesian uses the terms an Indonesian engineer
uses at work: `graph`, `event`, `Blueprint`, `workspace` is translated ("ruang
kerja"), `user story`, `planner`, `gateway`, `worker`, `bearer token`,
`webhook`, `secret` stay in English. Column names in the mock-up (Backlog,
Ready, Working, Needs you, Review, Done) stay English in both languages
because they are the dashboard's own labels (`dashboard/board.go`).

A task run is called an **attempt** / **percobaan** throughout, matching the
`A-3` labels in the mock-up.

The full copy deck is §9. It is the contract: the implementation uses these
strings verbatim.

### 5.6 The source link points at a repository that is private today

`gh repo view` reports the repository's visibility as `PRIVATE` on
2026-11-25. The page links to `https://github.com/ardihikaru/gaos` in four
places (header, quick start, open-source cards, footer). Until the operator
makes the repository public those links return GitHub's 404 to an anonymous
visitor, and `git clone` in the quick start fails for them.

Decision: ship the links. The operator asked for an "open-source-ready" page;
the URL is already published in `NOTICE`; and a page that hides its source
link is not ready for the day the repository opens. The condition is reported
to the operator in the delivery summary so that flipping visibility is a
known, single remaining step. **Rejected:** hiding the links behind a flag —
a second state of the page to maintain for a condition that changes once.

### 5.7 Motion

All of it lives in a new `landing-page/js/motion.js`, loaded with `defer`, and
a new `landing-page/css/04-motion.css`. `motion.js` returns immediately when
`matchMedia('(prefers-reduced-motion: reduce)')` matches, except where noted.
"Fine pointer" means `matchMedia('(hover:hover) and (pointer:fine)')`.

| # | Effect | Mechanism | Limits |
|---|---|---|---|
| M1 | **Graph field background.** Drifting nodes joined by edges when closer than 150px; a brass pulse travels along a random edge every 2.4s. Near the pointer (180px) nodes are joined to it and pushed away gently. | One `<canvas class="field" aria-hidden="true">`, `position:fixed; inset:0; z-index:-1; pointer-events:none`, first child of `<body>`. Canvas 2D, `requestAnimationFrame`. Node count `clamp(24, viewport area / 26000, 64)`. Speed 6–14 px/s. Device-pixel ratio capped at 2. Colours read from `--brass` and `--ink-3` and re-read when `data-theme` changes or the colour-scheme media query fires. | Stops when `document.hidden`. Under reduced motion one static frame is drawn and no loop runs. Pointer reaction only on a fine pointer. Overall canvas opacity .55 light / .7 dark, so body text keeps its contrast. `.showcase` (`02-layout.css:117`) keeps its opaque band; the other sections are transparent over the field. |
| M2 | **Hero glow follows the pointer.** | A 520px radial-gradient element inside `.hero`, moved by `transform: translate3d()` with a lerp of .12 per frame toward the pointer. | Fine pointer only. Hidden otherwise. |
| M3 | **Hero entrance.** Pill, headline, paragraph, buttons, mock-up enter in that order. | CSS keyframes under `html.js`, `translateY(12px)` + opacity, 500ms `--ease-out`, delays 0/60/120/180/260ms. | Reduced motion: opacity only. |
| M4 | **Board tilt.** The hero mock-up tilts up to 2.5° toward the pointer. | `transform: perspective(1400px) rotateX() rotateY()` written to the `.shot-board` element through CSSOM, lerped; returns to 0 on `pointerleave`. | Fine pointer only. |
| M5 | **Card spotlight.** A soft brass highlight follows the pointer inside a feature card. | `--mx`/`--my` set on the hovered card only (not on `:root`, to avoid page-wide style recalculation); a `::before` radial gradient whose opacity transitions 200ms. | Fine pointer only. |
| M6 | **Scroll progress.** A 2px brass bar under the header. | `transform: scaleX()` from a passive scroll listener, written at most once per frame. | Runs under reduced motion too: it is a position indicator, not decoration. |
| M7 | **Count-up.** The four numbers count from 0 when they enter the viewport. | 900ms, `--ease-out` approximated as `1 - (1-t)^4`. The final value is in the HTML. | Reduced motion and no-JS show the final value. |
| M8 | **Steps line draws in.** | The existing connector (`02-layout.css:138-139`) gets `transform: scaleX(0)` → `1`, 700ms, origin left, when `.steps` enters; on the vertical layout below 900px it is `scaleY`. | — |
| M9 | **Architecture flow.** A dot travels along each of the two connectors between the three layers. | CSS keyframes, `transform: translateX()` on a 6px dot, 2.4s linear, staggered 1.2s. On the stacked layout below 760px the dot travels downward. | Paused off-screen (class toggled by `IntersectionObserver`). |
| M10 | **Terminal.** The three quick-start commands appear one after another with a caret. | Opacity only, 220ms each, 350ms apart, started when the terminal enters the viewport. The text is in the HTML and selectable from the start for assistive technology. | — |
| M11 | **Header state.** A shadow fades in once the page has scrolled 8px. | Class toggle; opacity on a pseudo-element. | — |
| M12 | **Section spy.** The header link of the section in view is marked. | `IntersectionObserver`, `aria-current="true"` on the link. | Runs under reduced motion (no movement). |

The existing reveal (`theme.js:46-73`) and the board loop
(`03-devices.css:76-98`) stay as they are. The comment at
`03-devices.css:76` that calls the board loop "the only decorative loop on the
page" is corrected.

**Performance budget.** The field draws at most 64 nodes and tests
64·63/2 = 2016 pairs per frame, well inside a 16ms frame on a mid-range phone.
No effect reads layout inside a frame: pointer positions come from the event,
element boxes are measured on `pointerenter` and on `resize` only.

### 5.8 The copy button

The quick-start terminal has a "Copy" button that calls
`navigator.clipboard.writeText` with the three commands. `connect-src` does
not govern the clipboard. On success the label reads "Copied" for 1.6s; when
the API is missing or rejects, the button is hidden (the text is selectable).

### 5.9 The drift check (new automation, TC-617)

`deploy/check-stacks.sh` gains one section, placed before the final summary:

```
== the landing page's two languages cover the same keys ==
```

It extracts the set **H** of keys from `landing-page/index.html` (every
`data-i18n="k"` and the part after the colon in every
`data-i18n-attr="attr:k"`) and the set **D** of keys from
`landing-page/js/i18n.js` (every line matching `^\s*'([a-z0-9.-]+)':`), sorts
both, and compares with `comm`.

- `ok   every translatable string on the landing page has an Indonesian entry`
  — when H − D is empty. Otherwise `FAIL` with the same message, followed by
  the missing keys on indented lines.
- `ok   the landing page's Indonesian dictionary has no unused entry` — when
  D − H is empty. Otherwise `FAIL` with the same message and the keys.
- `ok   the landing page's two sets of keys are not empty` — when H has at
  least 100 keys. This is the guard against the check passing because the
  extraction matched nothing.

Keys are restricted to `[a-z0-9.-]` so both extractions are one `grep -oE`.
Labels that change at run time are covered by §5.10.

Register: one row, `TC-617`, automation `check-stacks.sh`, kind `Positive`
(the convention for a check-stacks row, whose negative is described in its
prose — `qa-test-plan.html:707` is the model), status `Implemented`.

### 5.10 Strings that script changes at run time

Two controls change their label by state: the theme button's `aria-label`
and the copy button's text. They use one theme key and two copy keys. Each state is a separate
key and a separate element or attribute in the HTML so that §5.9 sees it:

- The copy button contains two `<span>`s, `data-i18n="qs.copy"` and
  `data-i18n="qs.copied"`; CSS shows one by the button's `data-state`.
- The theme button's `aria-label` is a single state-independent string,
  `data-i18n-attr="aria-label:nav.theme"` ("Switch between light and dark
  theme"), and `theme.js` stops rewriting it (`theme.js:33-34` is removed).

## 6. Page structure after the change

Section ids become English: `#features`, `#blueprint`, `#how-it-works`,
`#architecture`, `#quick-start`, `#open-source`. The old ids `#fitur` and
`#cara` are not kept; nothing in the repository links to them
(`grep -rn '#fitur\|#cara' --include='*.html' --include='*.md' .` matches only
`landing-page/index.html`).

1. Header — brand, links (Features, How it works, Quick start, Docs, GitHub),
   language switch, theme button, "Open Mission Control".
2. Hero — pill, headline, paragraph, two buttons, the board mock-up.
3. Numbers — four.
4. Features — seven cards and the "Not built yet" block.
5. Blueprint showcase — three points and the mock-up.
6. How it works — four steps.
7. Architecture — three layers and two notes.
8. Quick start — terminal, the four paths, three notes.
9. Open source — three cards.
10. Closing call to action.
11. Footer.

## 7. Change list

| # | File | Change |
|---|---|---|
| 1 | `landing-page/index.html` | Rewritten: English copy from §9, `data-i18n` keys, new sections, the canvas, two new script tags, one new stylesheet link. CSP line unchanged. Comments in English. |
| 2 | `landing-page/js/i18n.js` | New. Detection (§5.2), dictionary (§9), apply and switch (§5.1, §5.3, §5.4). |
| 3 | `landing-page/js/motion.js` | New. M1, M2, M4–M12, the copy button. |
| 4 | `landing-page/js/theme.js` | The `aria-label` rewrite removed (§5.10); the footer year id renamed `tahun` → `year`; comments in English. Reveal and board observer unchanged. |
| 5 | `landing-page/css/01-tokens.css` | `html.i18n-wait` rule; comments in English. No new colour token. |
| 6 | `landing-page/css/02-layout.css` | Header additions (language switch, GitHub link), feature grid for seven cards, architecture, quick start, open-source cards, footer; comments in English. |
| 7 | `landing-page/css/03-devices.css` | The "only decorative loop" comment corrected; comments in English. |
| 8 | `landing-page/css/04-motion.css` | New. The field, hero glow, entrance, spotlight, progress bar, connectors, terminal, header state, reduced-motion block. |
| 9 | `deploy/check-stacks.sh` | The §5.9 section. |
| 10 | `deploy/nginx/landing.conf` | Comment only: "three stylesheets and one script" → "four stylesheets and three scripts". |
| 11 | `deploy/Dockerfile.landing` | Comment only, if it repeats a count (it does not today; left untouched unless it does). |
| 12 | `pustaka/docs/product/qa-test-plan.html` | Row `TC-617`; prose under `#implemented` quoting the three `ok` lines; "six hundred and sixteen" → "six hundred and seventeen" twice. |
| 13 | `pustaka/docs/assets/toc/product.js` | The same count twice. |
| 14 | `.claude/rules/qa-test-plan.md` | Highest id `TC-617`, next free `TC-618`; the count sentence; `TC-617` added to the `check-stacks.sh` row; "617 rows in total". |
| 15 | `deploy/check-workspace.sh` | The two `prose_check` strings at lines 129-130 → "six hundred and seventeen". |
| 16 | `README.md` | "Product version 0.50.0"; "617 registered test cases". |
| 17 | `pustaka/docs/changelog.html` | A `v0.50.0` section. |
| 18 | `pustaka/docs/assets/toc.js` | `semver: "0.50.0"`. |

Counts: 3 new files (2, 3, 8), 15 edited, 18 rows. The Go test count (486)
does not change: no `func Test…` is added. The register grows by exactly one
row, 616 → 617.

## 8. Verification

1. `./deploy/check-stacks.sh` — all pass, including the three new lines.
   Negative, by hand: delete one line from the dictionary → the first new line
   prints `FAIL` and names the key; add an unused entry → the second fails.
2. `./deploy/check-workspace.sh`, `cd pustaka && go run . check ./docs &&
   go test ./...`, `cd dashboard && go vet ./... && go test ./...`.
3. Headless Chrome screenshots of the page served from a local static server,
   at 1440×900 and 390×844, in both themes and both languages
   (`?lang=en`, `?lang=id`), plus one with `--force-prefers-reduced-motion`.
4. Detection: headless Chrome with `TZ=Asia/Jakarta` renders Indonesian with
   no `?lang`; with `TZ=Europe/Berlin` renders English.
5. The browser console shows no CSP violation.
6. After deployment: `curl` of `/`, `/js/i18n.js`, `/js/motion.js`,
   `/css/04-motion.css` through each instance returns 200, and a path that
   does not exist still returns 404.

## 9. Copy deck

Keys are `section.name`. `→` separates English from Indonesian. Inline
`<b>`/`<code>` markup is part of the string.

### Document

- `meta.title` — GAOS — product and task management for AI coding agents →
  GAOS — manajemen produk dan tugas untuk agen koding AI
- `meta.desc` — GAOS is an open-source, self-hosted system that keeps a
  product's requirements, tasks and policies in a graph and sends each task to
  an AI coding agent one turn at a time. Apache-2.0. → GAOS adalah sistem
  open source yang di-hosting sendiri. GAOS menyimpan kebutuhan, tugas, dan
  kebijakan produk dalam graph, lalu mengirim setiap tugas ke agen koding AI
  satu giliran demi satu giliran. Berlisensi Apache-2.0.
- `a11y.skip` — Skip to content → Lewati ke konten
- `a11y.brand` — GAOS, back to the top of the page → GAOS, kembali ke awal halaman

### Header

- `nav.label` — Main → Utama
- `nav.features` — Features → Fitur
- `nav.how` — How it works → Cara kerja
- `nav.quick` — Quick start → Mulai cepat
- `nav.docs` — Docs → Dokumentasi
- `nav.github` — GitHub → GitHub
- `nav.lang` — Language → Bahasa
- `nav.theme` — Switch between light and dark theme → Ganti tema terang atau gelap
- `nav.open` — Open Mission Control → Buka Mission Control

### Hero

- `hero.pill` — Open source · Apache-2.0 · Self-hosted → Open source · Apache-2.0 · Hosting sendiri
- `hero.title` — Product and task management for AI coding agents → Manajemen produk dan tugas untuk agen koding AI
- `hero.sub` — GAOS stores each product's requirements, user stories,
  decisions and policies in a graph. It plans the work as tasks on a kanban
  board and sends each task to an agent one turn at a time, in a workspace
  that already holds the product's rules. Every change is written as an event,
  so any earlier state can be replayed. → GAOS menyimpan kebutuhan, user
  story, keputusan, dan kebijakan setiap produk dalam sebuah graph. Pekerjaan
  direncanakan sebagai tugas di papan kanban, lalu setiap tugas dikirim ke
  agen satu giliran demi satu giliran, di ruang kerja yang sudah memuat aturan
  produk tersebut. Setiap perubahan dicatat sebagai event, sehingga keadaan
  sebelumnya dapat diputar ulang.
- `hero.start` — Get started → Mulai
- `hero.docs` — Read the documentation → Baca dokumentasi

### Board mock-up

- `board.aria` — The board of a sample product, Laundry Membership, in
  Mission Control. Columns Backlog, Ready, Working, Needs you, Review and
  Done, in two lanes: the story US-02, Regional pricing, and a Plan lane for
  tasks outside a story. Task T-214 is being worked under Blueprint v7 and
  then moves to Review. Task T-215 waits for the operator's answer. → Papan
  produk contoh, Laundry Membership, di Mission Control. Kolom Backlog, Ready,
  Working, Needs you, Review, dan Done, dalam dua lajur: story US-02, Harga
  per wilayah, dan lajur Plan untuk tugas di luar story. Tugas T-214 sedang
  dikerjakan dengan Blueprint v7 lalu berpindah ke Review. Tugas T-215
  menunggu jawaban operator.
- `board.lane` — US-02 · Regional pricing → US-02 · Harga per wilayah
- `board.t219` — Price variance report per branch → Laporan selisih harga per cabang
- `board.t219m` — waiting on T-214 → menunggu T-214
- `board.t216` — Validate the minimum rate in the API → Validasi tarif minimum di API
- `board.t216m` — ready to dispatch → siap dikirim
- `board.t214` — Migrate prices to the pricing service → Migrasi harga ke layanan pricing
- `board.run` — A-3 · running → A-3 · berjalan
- `board.rev` — A-3 · finished, v7 → A-3 · selesai, v7
- `board.t215` — Price rounding: up or to the nearest? → Pembulatan harga: ke atas atau ke terdekat?
- `board.t211` — Regional rate table schema → Skema tabel tarif wilayah
- `board.t220` — Load-test the price endpoint → Uji beban endpoint harga
- `board.t220m` — not ready → belum siap
- `board.t218` — Prepare sample branch data → Siapkan data contoh cabang
- `board.t218m` — A-1 · running → A-1 · berjalan
- `board.t212` — Rate API documentation → Dokumentasi API tarif
- `board.t209` — Pricing service skeleton → Kerangka layanan pricing

### Numbers

The section's label: `proof.label` — Project figures → Angka proyek

- 74 — `proof.types` — types in the graph schema: 31 vertex, 40 edge, 3
  document → tipe dalam skema graph: 31 vertex, 40 edge, 3 dokumen
- 59 — `proof.events` — event types, in a closed catalog → tipe event, dalam katalog tertutup
- 0 — `proof.deps` — third-party Go dependencies; neither module has a
  go.sum → dependensi Go pihak ketiga; kedua modul tidak memiliki go.sum
- 1 — `proof.port` — port published per deployment → port yang dibuka per deployment

### Features

- `feat.title` — What GAOS does today → Yang sudah dikerjakan GAOS saat ini
- `feat.lead` — Every feature below is implemented, tested and documented.
  Work that is planned but not built is listed separately. → Setiap fitur di
  bawah ini sudah diimplementasikan, diuji, dan didokumentasikan. Pekerjaan
  yang direncanakan tetapi belum dibangun dicantumkan terpisah.
- `feat.more` — Read more → Selengkapnya (one key, used by every card link)

1. `feat.board.h` — A kanban board per product → Papan kanban per produk
   `feat.board.p` — Each product has one board with the columns Backlog,
   Ready, Working, Needs you, Review and Done. The columns are a projection of
   task status in the graph, so the board and the data cannot disagree. Moving
   a card is a request, and the task state machine can refuse it. → Setiap
   produk memiliki satu papan dengan kolom Backlog, Ready, Working, Needs you,
   Review, dan Done. Kolom-kolom itu merupakan proyeksi dari status tugas di
   graph, sehingga papan dan data tidak mungkin berbeda. Memindahkan kartu
   adalah sebuah permintaan, dan state machine tugas dapat menolaknya.
   Link: `/docs/architecture/gateway.html`.
2. `feat.bp.h` — Versioned Blueprint → Blueprint berversi
   `feat.bp.p` — A product's requirements, technical and coding policies, and
   reference repositories are published as one versioned unit and synced into
   the agent's workspace as its rules. A dispatch is refused when the
   workspace no longer matches the published version. → Kebutuhan produk,
   kebijakan teknis dan koding, serta repositori acuan diterbitkan sebagai
   satu unit berversi dan disinkronkan ke ruang kerja agen sebagai aturannya.
   Pengiriman tugas ditolak jika isi ruang kerja tidak lagi sama dengan versi
   yang diterbitkan. Link: `/docs/product/blueprint.html`.
3. `feat.ready.h` — Readiness scoring → Penilaian kesiapan
   `feat.ready.p` — Each product record is scored out of 100 by rules a
   machine can check. Below 45 the planner declines to plan, because an
   unclear specification produces an unclear plan. → Setiap catatan produk
   dinilai dengan skala 100 berdasarkan aturan yang dapat diperiksa mesin. Di
   bawah 45, planner tidak menyusun rencana, karena spesifikasi yang belum
   jelas menghasilkan rencana yang juga tidak jelas.
   Link: `/docs/product/brd.html`.
4. `feat.gate.h` — Design gate → Gerbang desain
   `feat.gate.p` — A planned story starts with a design task. A reviewer
   without access to the author's context reads the design, and the
   implementation task starts only after the review passes. A blocked design
   returns to its author and is reviewed again. → Setiap story yang
   direncanakan diawali dengan tugas desain. Peninjau yang tidak memiliki
   akses ke konteks penulis membaca desain itu, dan tugas implementasi baru
   dimulai setelah tinjauan lolos. Desain yang ditolak dikembalikan ke
   penulisnya dan ditinjau ulang. Link: `/docs/architecture/design-gate.html`.
5. `feat.inbox.h` — Operator inbox → Kotak masuk operator
   `feat.inbox.p` — When an agent or an external client asks a question, the
   task waits in Needs you until a person answers. Each answer records where
   it came from. → Ketika agen atau klien eksternal mengajukan pertanyaan,
   tugas menunggu di kolom Needs you sampai ada orang yang menjawab. Setiap
   jawaban mencatat asalnya. No link.
6. `feat.time.h` — Timeline and replay → Linimasa dan pemutaran ulang
   `feat.time.p` — Every change writes an event to an append-only stream. The
   state at any time is computed from that stream, so a product's history can
   be replayed. If the orchestrator stops in the middle of a run, it rebuilds
   its state from the graph. → Setiap perubahan menulis event ke aliran yang
   hanya dapat ditambah. Keadaan pada waktu tertentu dihitung dari aliran itu,
   sehingga riwayat produk dapat diputar ulang. Jika orchestrator berhenti di
   tengah proses, keadaannya dibangun kembali dari graph. No link.
7. `feat.api.h` — Machine API and QA sync → API mesin dan sinkronisasi QA
   `feat.api.p` — QA tools and other clients connect through a REST API with
   bearer tokens. GAOS keeps a test catalog per product, syncs it with the QA
   tools, and calls a webhook when an operator answers a client's question.
   The Integrations view shows the live state of each connection and never
   displays a secret. → Alat QA dan klien lain terhubung melalui REST API
   dengan bearer token. GAOS menyimpan katalog pengujian per produk,
   menyinkronkannya dengan alat QA, dan memanggil webhook saat operator
   menjawab pertanyaan klien. Tampilan Integrations menunjukkan status terkini
   setiap koneksi dan tidak pernah menampilkan secret.
   Link: `/docs/guide/qa-sync-api.html`.

Grid order on three columns: 1 (two columns wide), 2; 3, 4, 5; 6, 7 (two
columns wide).

**Not built yet**

- `unbuilt.h` — Not built yet → Belum dibangun
- `unbuilt.lead` — These three parts are specified in the documentation and
  not yet implemented. → Tiga bagian ini sudah dispesifikasikan di
  dokumentasi, tetapi belum diimplementasikan.
- `unbuilt.score.h` — Scoring engine → Mesin penilaian;
  `unbuilt.score.p` — Combines five weighted dimensions of an agent's result
  into one score, with a safety limit that stops the work whatever the total.
  → Menggabungkan lima dimensi berbobot dari hasil kerja agen menjadi satu
  skor, dengan batas keamanan yang menghentikan pekerjaan berapa pun totalnya.
- `unbuilt.retry.h` — Retry guards → Pengaman percobaan ulang;
  `unbuilt.retry.p` — Stop and ask for a decision when an attempt fails the
  same way twice. → Berhenti dan meminta keputusan ketika percobaan gagal
  dengan cara yang sama dua kali.
- `unbuilt.budget.h` — Budget guard → Pengaman anggaran;
  `unbuilt.budget.p` — Refuses to dispatch once a product reaches its
  spending cap, and never stops a run that is already in progress. → Menolak
  pengiriman tugas setelah produk mencapai batas pengeluarannya, dan tidak
  pernah menghentikan proses yang sedang berjalan.
- `unbuilt.tag` — Planned → Direncanakan (one key, used three times)

### Blueprint showcase

- `bp.title` — The Blueprint is written into the workspace, and every attempt
  records its version → Blueprint ditulis ke ruang kerja, dan setiap
  percobaan mencatat versinya
- `bp.one` — <b>One versioned unit.</b> Business requirements, technical and
  coding policies, and the repositories whose patterns the agent follows. →
  <b>Satu unit berversi.</b> Kebutuhan bisnis, kebijakan teknis dan koding,
  serta repositori yang polanya diikuti agen.
- `bp.two` — <b>Loaded on every turn.</b> It is written as CLAUDE.md and
  .claude/rules, so the agent loads it on every turn, including a resumed
  one. → <b>Dimuat di setiap giliran.</b> Blueprint ditulis sebagai CLAUDE.md
  dan .claude/rules, sehingga agen memuatnya di setiap giliran, termasuk
  giliran yang dilanjutkan.
- `bp.three` — <b>A published version does not change.</b> A dispatch is
  refused until the workspace holds the published version, and each attempt
  records the version it ran under. → <b>Versi yang sudah terbit tidak
  berubah.</b> Pengiriman ditolak sampai ruang kerja memuat versi yang terbit,
  dan setiap percobaan mencatat versi yang digunakannya.
- `bp.aria` — The Blueprint tab in Mission Control. Version 7 is published
  and synced to the workspace; versions 6 and 5 are below it. It contains
  CLAUDE.md and eight rule files: the GAOS contract, the BRD, stories,
  decisions, technical policy, coding policy, security policy and knowledge
  sources. Attempt A-3 of task T-214 is bound to Blueprint v7. → Tab Blueprint
  di Mission Control. Versi 7 sudah terbit dan disinkronkan ke ruang kerja;
  versi 6 dan 5 berada di bawahnya. Isinya CLAUDE.md dan delapan berkas
  aturan: kontrak GAOS, BRD, story, keputusan, kebijakan teknis, kebijakan
  koding, kebijakan keamanan, dan sumber pengetahuan. Percobaan A-3 untuk
  tugas T-214 terikat pada Blueprint v7.
- `bp.v7` — published · in workspace → terbit · di ruang kerja
- `bp.pub` — published → terbit (used twice)
- `bp.draft` — draft → draf
- `bp.changed` — 2 policies changed → 2 kebijakan diubah
- `bp.turn` — one turn through ai-manager → satu giliran melalui ai-manager
- File sizes use a decimal point in English and a decimal comma in
  Indonesian, one key each: `bp.s1` 2.1 KiB → 2,1 KiB; `bp.s2` 1.4 → 1,4;
  `bp.s3` 6.8 → 6,8; `bp.s4` 1.9 → 1,9; `bp.s5` 3.2 → 3,2; `bp.s6` 4.0 → 4,0;
  `bp.s7` 5.5 → 5,5; `bp.s8` 2.2 → 2,2; `bp.s9` 0.9 → 0,9.

The previous caveat line under the list (`index.html:189`) is removed: the
author could not confirm from the repository whether it is still true, and
the page states nothing it cannot anchor.

### How it works

- `how.title` — From a product record to a recorded attempt, in four steps →
  Dari catatan produk sampai percobaan yang tercatat, dalam empat langkah
- `how.lead` — Each step writes to the graph. The label above a step names
  the record it writes. → Setiap langkah menulis ke graph. Label di atas
  setiap langkah menyebutkan record yang ditulisnya.
- Step 1, label `Product` (not translated). `how.s1.h` — Describe the product
  → Deskripsikan produk; `how.s1.p` — Record the problem, the goals, the
  scope, the domain rules, and the user stories with their acceptance
  criteria. → Catat masalah, tujuan, cakupan, aturan domain, serta user story
  beserta kriteria penerimaannya.
- Step 2, label `how.s2.w` — readiness score → skor kesiapan. `how.s2.h` —
  Pass the readiness gate → Lolos gerbang kesiapan; `how.s2.p` — The record
  is scored out of 100. Planning starts at 45 or above. → Catatan dinilai
  dengan skala 100. Perencanaan dimulai pada skor 45 atau lebih.
- Step 3, label `Task`. `how.s3.h` — Plan the tasks → Rencanakan tugas;
  `how.s3.p` — The planner turns stories into tasks. Each task covers one
  concern and has at least one acceptance criterion, and its dependencies are
  checked for cycles on every write. → Planner mengubah story menjadi tugas.
  Setiap tugas mencakup satu hal dan memiliki minimal satu kriteria
  penerimaan, dan dependensinya diperiksa agar bebas siklus pada setiap
  penulisan.
- Step 4, label `TaskRun, Event`. `how.s4.h` — Run each task through the
  gateway → Jalankan setiap tugas melalui gateway; `how.s4.p` — ai-manager
  runs one turn in the product's workspace under the published Blueprint. A
  worker such as ai-claude-cli does the work, and GAOS records the attempt
  and its events. → ai-manager menjalankan satu giliran di ruang kerja produk
  dengan Blueprint yang sudah terbit. Worker seperti ai-claude-cli
  mengerjakan tugasnya, dan GAOS mencatat percobaan beserta event-nya.

### Architecture

- `arch.title` — Three layers, each with one responsibility → Tiga lapisan,
  masing-masing dengan satu tanggung jawab
- `arch.gaos.h` — GAOS (not translated, no key); `arch.gaos.p` — Shapes the
  work: the product record, the tasks and the Blueprint. GAOS runs no model
  itself. → Membentuk pekerjaan: catatan produk, tugas, dan Blueprint. GAOS
  sendiri tidak menjalankan model.
- ai-manager; `arch.aim.p` — The gateway. It runs every model turn in the
  workspace provisioned for the product. → Gateway. Menjalankan setiap
  giliran model di ruang kerja yang disediakan untuk produk tersebut.
- `arch.worker.h` — Worker → Worker; `arch.worker.p` — An agent such as
  ai-claude-cli that works the task in that workspace. → Agen seperti
  ai-claude-cli yang mengerjakan tugas di ruang kerja tersebut.
- `arch.note1` — Model turns are off by default. Set
  <code>GAOS_MODEL_MODE=aimanager</code> and point <code>GAOS_AIM_URL</code>
  at an ai-manager gateway to turn them on. → Giliran model nonaktif secara
  bawaan. Atur <code>GAOS_MODEL_MODE=aimanager</code> dan arahkan
  <code>GAOS_AIM_URL</code> ke gateway ai-manager untuk mengaktifkannya.
- `arch.note2` — You pay your own model provider. GAOS is never in that
  billing path. → Biaya model dibayar langsung ke penyedia model Anda. GAOS
  tidak pernah berada di jalur penagihan itu.

Anchors: `README.md:14-17` (the three layers), `README.md:90`
(`GAOS_MODEL_MODE`), `README.md:171-172` (billing).

### Quick start

- `qs.title` — Run it locally → Jalankan secara lokal
- `qs.lead` — The local stack needs Docker and publishes one port. → Stack
  lokal membutuhkan Docker dan membuka satu port.
- Terminal, not translated: `git clone https://github.com/ardihikaru/gaos.git`,
  `cd gaos`, `docker compose up -d`.
- `qs.copy` — Copy → Salin; `qs.copied` — Copied → Tersalin;
  `qs.term` (the terminal's `aria-label`) — Quick start commands → Perintah mulai cepat
- `qs.then` — Then open <code>http://localhost:8083/</code>: → Lalu buka
  <code>http://localhost:8083/</code>:
- `qs.p1` — the landing page → halaman utama (path `/`)
- `qs.p2` — Mission Control: products, Blueprint, board and timeline →
  Mission Control: produk, Blueprint, papan, dan linimasa (path `/app/`)
- `qs.p3` — the documentation → dokumentasi (path `/docs/`)
- `qs.p4` — the health check, including database reachability → pemeriksaan
  kesehatan, termasuk keterjangkauan database (path `/api/healthz`)
- `qs.n1` — Without a database the dashboard runs on an in-memory store. Set
  <code>GAOS_STORE=arcadedb</code> to use ArcadeDB. → Tanpa database,
  dashboard berjalan dengan penyimpanan di memori. Atur
  <code>GAOS_STORE=arcadedb</code> untuk menggunakan ArcadeDB.
- `qs.n2` — Change the port with <code>GAOS_EDGE_PORT</code>. → Ubah port
  dengan <code>GAOS_EDGE_PORT</code>.
- `qs.n3` — The full list of settings is in the documentation. → Daftar
  lengkap pengaturan tersedia di dokumentasi. (links to
  `/docs/infra/environments.html`)

Anchors: `README.md:71-86`, `compose.yaml:37`.

### Open source

- `oss.title` — Open source, and complete on its own → Open source dan
  lengkap tanpa komponen tambahan
- `oss.lic.h` — Apache-2.0 license → Lisensi Apache-2.0; `oss.lic.p` — Clone
  it, run it, change it and ship it. There is no license key, no trial and no
  feature that switches off. A fork needs its own name; the NOTICE file
  explains why. → Silakan clone, jalankan, ubah, dan distribusikan. Tidak ada
  kunci lisensi, masa percobaan, atau fitur yang dinonaktifkan. Fork perlu
  memakai nama sendiri; alasannya dijelaskan di berkas NOTICE.
  `oss.lic.a` — Read the license → Baca lisensi
  (`https://github.com/ardihikaru/gaos/blob/main/LICENSE`)
- `oss.con.h` — Contributing → Berkontribusi; `oss.con.p` — Contributions
  take a DCO sign-off (<code>git commit -s</code>) and no CLA. A feature-sized
  change starts as a design document, and independent reviewers read it
  before any code is written. → Kontribusi memerlukan sign-off DCO
  (<code>git commit -s</code>), tanpa CLA. Perubahan seukuran fitur dimulai
  dari dokumen desain yang dibaca peninjau independen sebelum kode ditulis.
  `oss.con.a` — Read the contributing guide → Baca panduan kontribusi
  (`…/blob/main/CONTRIBUTING.md`)
- `oss.stack.h` — Built with → Dibangun dengan; `oss.stack.p` — The Go
  standard library and htmx for the dashboard, ArcadeDB for the graph, and
  Docker Compose for deployment. The Go modules have no third-party
  dependencies. → Pustaka standar Go dan htmx untuk dashboard, ArcadeDB untuk
  graph, dan Docker Compose untuk deployment. Modul Go-nya tidak memiliki
  dependensi pihak ketiga. `oss.stack.a` — Browse the source → Lihat kode
  sumber (`https://github.com/ardihikaru/gaos`)

Anchors: `README.md:162-168`, `CONTRIBUTING.md:11-22`, `NOTICE:17-27`.

### Closing

- `end.title` — Start with one product record → Mulai dari satu catatan produk
- `end.p` — Run the local stack, describe a product, and read what the
  readiness gate reports. → Jalankan stack lokal, deskripsikan satu produk,
  lalu baca hasil dari gerbang kesiapan.
- `end.start` — Get started → Mulai; `end.github` — View on GitHub → Lihat di GitHub

### Footer

- `foot.copy` — GAOS contributors. Apache-2.0. → Kontributor GAOS.
  Apache-2.0. (follows "© <year>")
- `foot.label` — Footer → Kaki halaman
- `foot.docs` — Documentation → Dokumentasi; `foot.log` — Changelog → Catatan
  rilis (`/docs/changelog.html`); `foot.lic` — License → Lisensi;
  `foot.con` — Contributing → Berkontribusi; GitHub (uses `nav.github`)
- `foot.note` — GAOS is under active development. The scoring engine, the
  retry guards and the budget guard are not built yet. The changelog records
  what each release adds. → GAOS masih dikembangkan secara aktif. Mesin
  penilaian, pengaman percobaan ulang, dan pengaman anggaran belum dibangun.
  Catatan rilis mencatat apa yang ditambahkan di setiap rilis.

## 10. Risks

- **A wrong guess.** A visitor in Indonesia who reads English, or the
  reverse, sees the other language first. Mitigation: the switch, and the
  remembered choice.
- **Canvas cost on a weak device.** Mitigation: the node cap, the DPR cap,
  the hidden-tab stop, no pointer work on touch.
- **Contrast.** The field sits behind body text. Mitigation: line alpha
  capped at .22 and node alpha at .5 before the canvas-level opacity; checked
  in both themes in the screenshots of §8.3.
- **The private repository** (§5.6).
- **Search engines see English only** unless they execute the script.
  Accepted: the page had one language before, and English is the wider one.

## 11. Amendment after review round 1

Round 1 returned B=0 from all four lenses (comprehension M=6, zoom-out M=4,
critic M=9, readiness M=7) and two copy-edit tables. The triage table is at
the end of `reviews/landing-bilingual-review-1.md`. This section is the
resulting design; it overrides anything above that it contradicts.

### 11.1 Facts

- The graph has **73** types, not 74: 31 vertex, **39** edge, 3 document.
  The old recipe also matched a SQL comment
  (`dashboard/schema/09-indexes.sql:103`). The recipe is now anchored:
  `grep -hE '^\s*CREATE (VERTEX|EDGE|DOCUMENT) TYPE' dashboard/schema/*.sql`.
  Abstract supertypes are types and are counted.
- The board has nine columns (`dashboard/board.go:133-142`). The copy says so.
- Replay is anchored at `dashboard/handlers_tasks.go:49` (`handleReplay`);
  the version pin at `dashboard/blueprint.go:24`; the cycle check at
  `dashboard/graph.go:134-138`.

### 11.2 Figures are checked, not typed (TC-618)

Each figure on the page is the whole text of an element carrying
`data-fig="<name>"`: `types`, `vertex`, `edge`, `document`, `events`. No
figure appears inside a translated string: the breakdown under the first
number is the untranslated line `31 vertex · 39 edge · 3 document`, built from
three `data-fig` spans. `deploy/check-stacks.sh` gains a second section,

```
== the landing page's figures match the schema and the event catalog ==
```

which computes the four counts with the recipe above (events:
`grep -cE '^\s*[A-Za-z]+\s+EventType = "' dashboard/events.go`), and prints
one line per figure, for example
`ok   the landing page says 39 edge types, and the schema declares 39`
or `FAIL the landing page says 40 edge types, and the schema declares 39`.
Five lines: vertex, edge, document, total, events. This is register row
**TC-618**. The register therefore grows by two rows, 616 → **618**; every
"six hundred and seventeen" / "617" in §7 reads "six hundred and eighteen" /
"618", the highest id is `TC-618` and the next free one `TC-619`.

The unbuilt list is stated once, in the "Not built yet" block. `foot.note`
no longer repeats it.

### 11.3 The key check (TC-617), made exact

```
H = grep -oE 'data-i18n(-attr)?="[^"]+"' landing-page/index.html
      | sed -E 's/.*="([a-z-]+:)?([^"]+)"/\2/' | sort -u
D = grep -oE "^\s*'[a-z0-9.-]+':" landing-page/js/i18n.js
      | tr -d " ':" | sort
```

Four lines: `every translatable string on the landing page has an Indonesian
entry` (H − uniq(D) empty); `the landing page's Indonesian dictionary has no
unused entry` (uniq(D) − H empty); `the landing page's Indonesian dictionary
declares no key twice` (`uniq -d` of D empty); `the landing page declares at
least 100 translatable keys` (unique keys in H). A failing line lists the keys
beneath it.

### 11.4 Translation mechanics

- `data-i18n` goes only on a leaf: an element whose entire content is the
  string. Inline `<b>` and `<code>` inside the string are part of it. Keyed
  elements never nest, and no keyed element contains a node that script
  touches. Consequences: the pill is `<p class="pill"><span class="dot">
  </span><span>…</span></p>`; `foot.copy` is a sibling of the year span; the
  settings note `qs.n3` is itself the `<a>`.
- `hero.pill` is identical in both languages ("Open source · Apache-2.0 ·
  Self-hosted") and has no key. The nine `bp.s*` size keys are dropped: the
  sizes are the dashboard's own rendering and stay as written.
- `<main id="main">`; the skip link targets `#main`.
- Clicking the switch removes `lang` from the URL with
  `history.replaceState`, so a reload does not undo the click.
- The fade on switch uses `--ease-out`.
- **The wait class cannot strand the page.** In `i18n.js` the head-time block
  is one `try`; the `DOMContentLoaded` listener is registered before the
  class is added; the `catch` removes the class. CSS adds a script-free
  safety net: `html.i18n-wait body` gets an animation that sets
  `visibility:visible` after 1.2s. `motion.js` is loaded `async`, not
  `defer`, so it never delays `DOMContentLoaded`.
- Open Graph: `og:title` is "GAOS — product and task management for AI coding
  agents" and `og:description` is the English `meta.desc`. Both are static.
  `img/og-image.png` is **redrawn** (this reverses the non-goal in §3): a
  1200×630 card with the mark, the name, the English headline and
  "Open source · Apache-2.0 · Self-hosted", rendered by headless Chrome from
  a scratch HTML file that uses the page's own tokens and fonts. The scratch
  file is not committed.

### 11.5 Motion, made safe

- **Who owns which transform on the hero mock-up.** `.rv` is removed from
  `.shot-board`; the entrance (M3) animates `.shot-board`; the tilt (M4)
  writes to the inner `.board` frame.
- **The spotlight (M5)** is a new `<span class="spot" aria-hidden="true">`,
  first child of every `.card` (the seven feature cards and the three
  open-source cards). The tick (`::before`) and the hover shadow (`::after`)
  are untouched.
- **Nothing that hides content depends on `motion.js`.** M8 and M10 are pure
  CSS, keyed off the class `.in` that the observer already in `theme.js`
  adds; `theme.js` observes `.rv` and `[data-io]`. Their start state exists
  only under `html.js`, and `theme.js` — the script that sets `html.js` — is
  the script that removes it. M9 is pure CSS, running while `theme.js` holds
  `.is-live` on `[data-live]` (the same toggle the board uses). The copy
  button is wired in `theme.js` and is shown only under `html.js`; when the
  clipboard call is unavailable or rejects, the button is hidden.
- `motion.js` keeps only what needs script: M1, M2, M4, M5, M6, M7, M11,
  M12. It adds `html.motion` when it starts; no rule under `html.motion`
  hides content. M6, M11 and M12 run under reduced motion; the others do not.
- **Reduced motion, effect by effect.** M1 one static frame. M2, M4, M5 off.
  M3 instant (the global override at `01-tokens.css:180` collapses it to its
  end state). M7 final value. M8 line drawn. M9 dots hidden
  (`display:none`). M10 all lines shown.
- **Cost.** On a coarse pointer the field runs at 30 fps and the minimum node
  count is 14; on a fine pointer 60 fps and 24–64 nodes. While `html.motion`
  is set the header drops `backdrop-filter` for a 94% opaque background, so
  the blur is not recomputed over the moving canvas. The "inside 16ms" claim
  is an assumption; §8 adds a measured check.
- The hero's grid (`.hero::before`) and the closing box's grid stay: they are
  local texture, and the field passes behind them.
- The numeric constants in §5.7 are tunable defaults, not contract.

### 11.6 Layout decisions the first draft left open

- **Header.** Text links (`.lnk`) hide at ≤ 980px (was 760px). GitHub is an
  icon button (`.gh`, 38×38, `aria-label` from `nav.github`), hidden at
  ≤ 560px. The brand's text hides at ≤ 480px (was 380px). The language
  switch, the theme button and "Open Mission Control" are always visible.
  The switch is `<div class="lang" role="group" aria-label>` with two
  `<button data-lang>`.
- **Links.** `hero.start` and `end.start` → `#quick-start`. GitHub and the
  licence/contributing links open in the same tab with
  `rel="noopener"`. The footer keeps a "Mission Control" link (no key; it is
  a name).
- **Feature cards** keep an `.on-graph`-style foot line, now a "Read more"
  link where §9 names a docs page and absent otherwise.
- **Architecture.** `<ol class="arch">` of three `<li class="layer">`
  (name in `<h3>`, text in `<p>`), with a `<span class="flow">` connector
  between them holding the travelling dot; horizontal above 900px, stacked
  below, matching `.steps`. Two notes in `<p class="arch-note">`.
- **Quick start.** Two columns above 900px: left, `<div class="term"
  data-io>` with a title bar (three dots, the copy button at the right) and
  `<pre><code>` holding three `<span class="ln">` lines; right, the paths as
  `<dl class="paths">` (`<dt><code>/app/</code></dt><dd data-i18n>`), then
  the notes.
- **Open source.** Three `.card` in the existing `.grid`.
- New class names are prefixed by their section (`arch-`, `term`, `paths`,
  `lang`, `gh`, `field`, `glow`, `spot`, `progress`).

### 11.7 The private repository — an accepted, operator-owned defect

Two lenses flagged §5.6. The alternative they named — leave the clone line
and the GitHub links out until the repository is public — is real and is
rejected for this reason: the operator asked for an open-source-ready page,
and a page with no source link and no way to obtain the code is not one; it
would need a second release on the day visibility changes. The zoom-out lens
offered the outcome taken here: record it as an accepted, operator-visible
defect with an owner. **Owner: the operator. Remedy: make
`github.com/ardihikaru/gaos` public. Until then four links and the clone
command fail for anonymous visitors.** The delivery summary says this in its
first lines. The licence statement itself is true today (`LICENSE`).

The security lens does not apply: the page gains links to an external
origin, not a call to one, and `connect-src 'none'` is unchanged.

### 11.8 Copy changes (these replace the §9 strings with the same key)

English:

- `hero.sub`, last sentence → State is kept in the graph and an append-only
  event stream, so an orchestrator that stops mid-task rebuilds from them.
- `proof.types` → types in the graph schema (the breakdown follows as
  figures, §11.2); `proof.port` → port published by the local stack
- `feat.lead` → The features below are implemented and covered by tests.
  Planned work that is not built yet is listed separately.
- `feat.board.p` → Each product has one board with nine columns: Backlog,
  Ready, Working, Needs you, Review and Done, plus Blocked, Failed and
  Canceled. The columns are computed from task status in the graph; the board
  stores no state of its own. Moving a card is a request that the task state
  machine can refuse.
- `feat.bp.p` → "…synced into the product's workspace as the agent's rules."
- `feat.ready.p`, second sentence → Below 45 the planner refuses to plan and
  returns the list of what is missing.
- `feat.time.p` → Changes are written as events to an append-only stream,
  and the timeline replays a product's history from that stream. If the
  orchestrator stops in the middle of an attempt, it rebuilds its state from
  the graph.
- `unbuilt.score.h` → Result scoring engine; `unbuilt.score.p` ends
  "…regardless of the total."
- `unbuilt.budget.p` → Refuses to dispatch once a product reaches its
  spending cap. It does not stop an attempt that is already in progress.
- `how.s2.p` → The record is scored out of 100. The planner accepts a score
  of 45 or above.
- `arch.gaos.p` → Holds the product record, the tasks and the Blueprint, and
  decides what to dispatch. GAOS runs no model tool itself.
- `arch.worker.p` → An agent such as ai-claude-cli that carries out the task
  in that workspace.
- `oss.lic.p`, first sentence → You may use, modify and redistribute it.
- `oss.con.p`, first sentence → Contributions require a DCO sign-off
  (<code>git commit -s</code>); there is no CLA.
- `foot.note` → GAOS is under active development. The changelog records what
  each release adds.

Indonesian: every replacement in the copy-id table of round 1 is taken as
written, with these adjustments so the two languages still say the same
thing after the English edits above:

- `hero.sub`, last sentence → State disimpan di graph dan stream event yang
  bersifat append-only, sehingga orchestrator yang berhenti di tengah tugas
  dapat membangun ulang keadaannya dari keduanya.
- `proof.types` → tipe dalam skema graph; `proof.port` → port yang dibuka
  oleh stack lokal
- `feat.lead` → Fitur di bawah ini sudah diimplementasikan dan diuji.
  Pekerjaan yang direncanakan tetapi belum dibangun dicantumkan terpisah.
- `feat.board.p` → Setiap produk memiliki satu papan dengan sembilan kolom:
  Backlog, Ready, Working, Needs you, Review, dan Done, ditambah Blocked,
  Failed, dan Canceled. Kolom dihitung dari status tugas di graph; papan
  tidak menyimpan state sendiri. Memindahkan kartu hanyalah permintaan yang
  dapat ditolak oleh state machine tugas.
- `feat.bp.p` → "…disinkronkan ke ruang kerja produk sebagai aturan bagi
  agen."
- `feat.ready.p`, second sentence → Di bawah 45, planner menolak menyusun
  rencana dan mengembalikan daftar hal yang masih kurang.
- `feat.time.p` → Perubahan dicatat sebagai event di stream yang bersifat
  append-only, dan linimasa memutar ulang riwayat produk dari stream itu.
  Jika orchestrator berhenti di tengah percobaan, keadaannya dibangun kembali
  dari graph.
- `unbuilt.score.h` → Mesin penilaian hasil
- `unbuilt.budget.p` → Menolak pengiriman tugas setelah produk mencapai batas
  biayanya. Percobaan yang sedang berjalan tidak dihentikan.
- `how.s2.p` → Catatan dinilai dengan skala 100. Planner menerima skor 45
  atau lebih.
- `arch.gaos.p` → Menyimpan catatan produk, tugas, dan Blueprint, serta
  menentukan apa yang dikirim. GAOS sendiri tidak menjalankan tool model.
- `arch.worker.p` → Agen seperti ai-claude-cli yang mengerjakan tugas di
  ruang kerja tersebut. (unchanged)
- `oss.lic.p` → Anda boleh menggunakan, mengubah, dan mendistribusikannya
  kembali. Tidak ada kunci lisensi, masa uji coba, atau fitur yang dikunci.
  Fork perlu memakai nama sendiri; alasannya dijelaskan di berkas NOTICE.
- `oss.con.p`, first sentence → Kontribusi memerlukan sign-off DCO
  (<code>git commit -s</code>); tidak ada CLA.
- `foot.note` → GAOS masih dikembangkan secara aktif. Catatan rilis memuat
  apa yang ditambahkan di setiap rilis.

### 11.9 Change list, corrected

Row 11 (`deploy/Dockerfile.landing`) is removed: the file holds no count.
Row 10 covers both `deploy/nginx/landing.conf:2` and `:32`. Row 14 covers
`.claude/rules/qa-test-plan.md:50, 73, 76, 80, 114, 128`. One row is added:
`landing-page/img/og-image.png`, redrawn. Result: 3 new files, 15 edited,
18 files.

### 11.10 Phases and exit conditions

1. **Copy, translation and the two checks.** `index.html`, `i18n.js`,
   `theme.js`, the CSS for layout, `check-stacks.sh`. Exit:
   `./deploy/check-stacks.sh` passes; by hand, deleting one dictionary line
   makes the first TC-617 line fail and name the key, and changing one figure
   in the HTML makes its TC-618 line fail.
2. **Motion.** `motion.js`, `04-motion.css`, the image. Exit, from headless
   Chrome against a local static server, at 1440×900, 820×900 and 390×844:
   the document is no wider than the viewport; with `?lang=id` no English
   sentence from §9 remains in `<main>`; with scripts disabled
   (`--blink-settings=scriptEnabled=false`) and with reduced motion forced,
   the three commands, the steps connector and every section's text are
   visible; with `TZ=Asia/Jakarta` and no `?lang` the page is Indonesian and
   with `TZ=Europe/Berlin` it is English; the console reports no CSP
   violation and no error; a 5-second trace at 390×844 with 4× CPU
   throttling is recorded in the pull request.
3. **Register and release.** The QA register, its counts, the rule file,
   `check-workspace.sh`, README, changelog, `toc.js`. Exit:
   `./deploy/check-workspace.sh`, `cd pustaka && go run . check ./docs && go
   test ./...` and `cd dashboard && go vet ./... && go test ./...` pass.

"Each instance" in §8.6 means each deployed copy of the production stack.

### 11.11 Isolation evidence for round 1

Positive control: a general-purpose agent reported the `CLAUDE.md` sentence
in its context (YES). A goldfish agent asked the same question with no tool
call reported NO, and NO again after reading a file under `docs/`. During the
real reviews, three lenses reported that the harness attached
`.claude/CLAUDE.md` to their context as a system reminder once they read
files under `.claude/`; none opened it and none used it. No canary reached a
report (`grep -c EGM-MARKER- reviews/landing-bilingual-review-1.md` → 0).
That is recorded as a partial isolation leak of the harness, not of the
method: the always-on file holds four invariants and no part of this
design's reasoning.
