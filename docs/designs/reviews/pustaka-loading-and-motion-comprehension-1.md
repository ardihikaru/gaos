# Comprehension review: `docs/designs/pustaka-loading-and-motion.md`

## Paraphrase

### What this proposes

1. **Geometry without JavaScript (`site.css`).** The `.shell` grid gets named areas (`nav doc toc`), so the article sits in its own column before the runtime adds the sidebar. While no `<header class="hd">` exists, a top margin and a fixed placeholder strip reserve the 56 px header. A static, unanimated sidebar skeleton fills the nav area until the real sidebar arrives. Landing rules key on `body[data-layout="landing"]` rather than on a class the runtime adds. Body colour transitions are off until `html.ready`. Reveal blocks (`.rv`) are visible by default and hidden only once the runtime is ready.
2. **Runtime changes (`site.js`).**
   - The ToC part scripts download in parallel and still run in order (`async=false` plus `Promise.allSettled`).
   - `ready` is added to `<html>` after the first `wirePage()`. Reveal blocks already on screen get `.in` at once.
   - The IntersectionObserver staggers reveals.
   - A delayed progress bar shows during partial navigation.
   - Under reduced motion there is no transition. Otherwise the swap uses the View Transitions API, with a `.pk-enter` class as the fallback.
3. **Motion CSS.** A new ease-out curve. The page transition animates only the article: the root crossfade is off and old and new articles each get their own fade. Buttons get a press state. The drawer closes faster than it opens. The active marker animates over a shorter distance. Reduced motion switches off every view-transition animation.
4. **Server-side head injection (`main.go`).** For non-underscore `.html` files, the static handler inserts a marker, an inline theme script and a font preload before the first `</head>`, then serves the result through `ServeContent`. Assets, the partial endpoint and the login page are untouched.
5. **Tests and registers.** TC-721 / `TestHeadInjection` is added, plus the QA register row, its prose and the count updates.
6. **Login off.** `PUSTAKA_AUTH=` is set empty in each prod VM's untracked `prod.docs.env`. Comments in `docs.env.example` and `compose.prod.yaml` say so.
7. **Release.** v0.55.0, a changelog entry, and decision D-093 with the decision-log count updated.

### How the existing system works, per this document

- Every page's chrome (header, sidebar, page ToC, prev/next, footer) is built in the browser by `site.js`.
- `site.js` sits at the end of `<body>` and loads eight ToC parts one after another before `init()`.
- Until it runs, the article sits in the 248 px sidebar column. The header arrives late and pushes the page down 56 px. The landing hero is not full-bleed. A dark-theme reader sees light first, then an animated switch. `.rv` blocks are invisible.
- Partial navigation uses the default root crossfade, or a `fadeup` fallback.
- The prod docs sit behind pustaka's cookie login, which is on when `PUSTAKA_AUTH` is truthy.
- `pustaka/` is a vendored fork of an upstream repository and differs from upstream mainly in `/docs` prefix handling.

I could paraphrase every section.

## Findings

### Change list omits README.md, check-workspace.sh and the QA rule file, so verification §7 fails - Severity: Major
- Affected scope: §3.5 "Counts", §3.7, §6 Change list, §7 Verification (`deploy/check-workspace.sh`)
- Finding: Moving the counts from 720 to 721, 586 to 587, 91 to 92 entries, and the version to 0.55.0 also breaks fixed values in files the change list does not name:
  - `deploy/check-workspace.sh:114-115` hard-codes `"five hundred and eighty-six"` and `586`.
  - `deploy/check-workspace.sh:129-131` hard-codes `"seven hundred and twenty"` and `"Ninety-one entries"`.
  - `deploy/check-workspace.sh:147-149` compares against `README.md`, which says `586 Go tests` (`README.md:137`), `720 registered test cases` (`README.md:139`) and `Product version 0.54.0` (`README.md:12`).

  §7 runs `deploy/check-workspace.sh`, and it will fail. §3.5 also says "the automation map's `pustaka (go test)` range gains TC-721", but that map lives in the QA rule file (the attached `qa-test-plan` rule). That file also states "highest in use is TC-720" and "586". It is not in §6 either. This breaks rule 3 of `docs/designs/README.md` ("The change list is complete").
- Recommendation: Add these to §6, each with the exact old and new strings:
  - `README.md`, three lines;
  - `deploy/check-workspace.sh`, three prose literals and one numeric literal;
  - the QA rule file (automation map, highest TC, Go-test count).

  Also say where "the automation map" lives.

### "Copy from upstream" and the itemised spec disagree; several anchors point at upstream line numbers, not the fork - Severity: Major
- Affected scope: §0 Glossary "pustaka"; §3 preamble; §1.1 item 1; §3.1.4; §3.3 "Reduced motion"
- Finding: The document gives two implementation methods. It says to copy the three files from upstream v0.9.0 and re-apply the fork's differences, and it also lists the edits one by one. The facts that tie these together do not hold up in this repository:
  - The Glossary says the fork differs from upstream "in only two ways" (prefix handling, content and ToC). §3 adds a third: `site.css` lacks upstream's device-mockup rules.
  - §3.1.4 says "13 selectors, `site.css:400-415`". The fork has 8 `body.layout-landing` selectors: lines 400, 401, 402, 409, 410, 411, 412 and 413.
  - "At 1024 px the narrow column is 232 px (`site.css:633-636`)" is wrong for the fork. The rule is at `site.css:612`, and lines 633-636 are the 560 px block.
  - The reduced-motion block is cited as "`site.css:676-680` upstream; the same block in the fork". In the fork it is at `site.css:654-658`.

  These look like upstream line numbers. If upstream has five more landing selectors, then "differs only by mockup rules" is false, and copying upstream's `site.css` would bring in rules that this document does not describe. The upstream claims ("`diff` is empty", "upstream's test verbatim", "Upstream released this exact change as its v0.9.0") cannot be checked from this repository, because the upstream is outside the tree.
- Recommendation: Pick one authoritative method. Either list the edits against the fork's own lines, or name the upstream commit and list the full fork-versus-upstream diff that will be re-applied. Fix the anchors to the fork's lines (612, 654-658) and the count (8). Label the upstream-equivalence claims as assumptions the operator must check.

### `.pk-progress` and `.pk-enter` have no CSS specification - Severity: Major
- Affected scope: §3.2.4, §3.2.5, §3.3
- Finding: §3.2 relies on the classes `pk-progress`, `run`, `done` and `pk-enter`, and §3.3 is the motion CSS section. §3.3 defines neither the progress bar nor the `.pk-enter` animation: no position, height, colour, transform origin, `done` fade duration or easing, and no keyframes for `.pk-enter`. §3.2.4 gives only "`scaleX(.8)` over 6 s" and "scale to 1, then fade". It is also undefined whether the bar shows or animates under reduced motion. §3.3's reduced-motion block covers only view transitions and `.rv`. Two implementers would build different bars.
- Recommendation: Add a bullet to §3.3 with the exact rules for `.pk-progress`, `.pk-progress.run`, `.pk-progress.done` and `.pk-enter`, including what each does under `prefers-reduced-motion`.

### Injected pages keep the file's Last-Modified, so returning readers may never receive the snippet - Severity: Major
- Affected scope: §3.4 (`http.ServeContent(w, r, full, fi.ModTime(), …)`), N1, G2
- Finding: Pages are served `Cache-Control: no-cache` (`main.go:576-577`), so browsers revalidate with `If-Modified-Since`. N1 says no page HTML changes, so page mtimes stay the same across this release. A browser holding a cached copy will get `304` and keep the old HTML without the theme script. G2 then fails for exactly the returning readers whose saved theme is dark. The same happens whenever the snippet changes in a later release. The document does not mention this side effect.
- Recommendation: State the cache-validation behaviour. Either pass a modtime that also covers the snippet's version (for example, the later of the file mtime and a build or snippet timestamp), or record the stale-cache window as an accepted risk in §5.

### R4 relies on D-085 for more than D-085 covers - Severity: Major
- Affected scope: §1.3, §5 R4, D-093 wording in §3.7
- Finding: R4 says the docs "were reviewed for private strings by D-085" and that `check-workspace.sh` section 7 enforces it. D-085 (`decision-log.html:1786-1809`) scoped that review to "one operator's private instance": a sample product's name, an organisation and domains. Section 7 matches a git-ignored pattern list that this document cannot show. Infrastructure detail is outside that scope, and the published pages contain it: for example, the prod host names appear in `pustaka/docs/infra/overview.html`, `qa-collaboration.html` and `qa-test-plan.html`. Making the site public exposes it.

  §1.3 states "GAOS is going public" with no decision reference. D-085 and OQ-36 (`open-questions.html:884-889`) say the repository stays private until OQ-36 is answered. The document never explains how publishing the docs site relates to that gate, yet it declares "no OQ is opened or answered".
- Recommendation: Cite the decision or instruction that makes the docs public. Say explicitly whether OQ-36 gates this change, and why not if it does not. Widen R4 into a review of what infrastructure detail the public pages expose, rather than relying on D-085's narrower scope.

### Contradictory figure for today's fallback animation - Severity: Nit
- Affected scope: §1.2 vs §3.2.5
- Finding: §1.2 says the fallback is "a 300 ms `fadeup`". §3.2.5 says "previously an inline `fadeup` for 350 ms". `site.js:579` runs a 0.3 s animation and clears it after a 350 ms timer.
- Recommendation: Say "a 300 ms `fadeup`, cleared after 350 ms" in both places.

### G4 says "under 300 ms", but §3.1.6 sets 300 ms and the stagger adds up to 300 ms more - Severity: Nit
- Affected scope: §2 G4, §3.1.6, §3.2.3
- Finding: The reveal transition is exactly 300 ms, and the last staggered block starts up to 300 ms late. G4 does not say whether reveals or the 6 s progress bar count as "navigation motion". The standard G4 cites lives under `.claude/`, which this review may not open, so the reader cannot check its scope.
- Recommendation: Say which animations G4 covers, and either make the reveal less than 300 ms or state that reveals are exempt.

### `body > .shell:first-child` assumes `.shell` is the first child of `<body>` on every page - Severity: Nit
- Affected scope: §0 Glossary "The runtime", §3.1.2
- Finding: The Glossary cites `AUTHORING_SPEC.md` §1 rule 6 for the page shape. Rule 6 covers only the two closing scripts (`AUTHORING_SPEC.md:28-30`), and no rule requires `.shell` to be the first child of `<body>`. A page with any element before `.shell` would not get the header reservation. R3 shows the author checked `</head>` on every page, but there is no equivalent check here.
- Recommendation: Cite the right rules (rules 4 and 6, plus the observed convention), and state the `.shell`-is-first-child check as part of the implementation, as R3 does for `</head>`.

### Undefined workflow terms in the deploy step - Severity: Nit
- Affected scope: §3.6
- Finding: "The tag deploy then recreates the `docs` container" assumes the reader knows the prod deploy procedure: which script runs it, who runs it, and in what order relative to the env-file edit. The dev stack (`compose.dev.yaml:105`, `dev.docs.env`) is not mentioned, so it is unclear whether dev stays behind the login. `deploy/STACKS.md:140` still describes `docs.env.example` as "the docs login".
- Recommendation: Name the deploy command and say whether the env edit comes before or after it. Add dev to the goals or the non-goals. Decide whether `STACKS.md` changes.

### Minor anchor drift and an ambiguous ID range - Severity: Nit
- Affected scope: §3.4, §5 R2, N2
- Finding:
  - The partial endpoint is at `main.go:514-551`, not 503-550, and its `ReadFile` is at `main.go:526`, not 523.
  - R2's "In prod pages are already held in memory for the index" has no anchor.
  - N2's "18 auth tests (TC-090…TC-108)" is a range of 19 IDs. TC-104 is a checker case (`qa-test-plan.html:1767`), so the range is not all auth tests.
- Recommendation: Correct the lines, anchor R2, and list the auth test IDs explicitly or note the TC-104 exception.

Sections confirmed accurate:
- §1.1: the cited `site.js` and `site.css` line references match, apart from the 1024 px reference already reported.
- §3.4: the handler and gzip claims match.
- §3.6: `loadAuthConfig` treats an empty value as off (`main.go:662-665`), and the hero CTA is at `index.html:226-246`.
- The count locations in §3.5 and §3.7 are correct.

Counts: B=0 M=5 N=5
