# landing-bilingual — review round 1

Design: `docs/designs/landing-bilingual.md`. Four goldfish lenses and two copy editors, 2026-11-25. Reports are verbatim.


---

## Lens: comprehension

## Reviewer note on isolation

While I was reading the rubric, the harness put the contents of `.claude/CLAUDE.md` (in this worktree) and of the main checkout's `.claude/rules/agent-workspace.md` and `.claude/rules/qa-test-plan.md` into my context as system reminders. I did not open those files with a tool, and I have not used anything from `CLAUDE.md` in this review. The author should know this before running the canary check.

## Paraphrase

### What this proposes

1. **Rewrite the landing page in English and add Indonesian.** The English text sits in the markup with `data-i18n` / `data-i18n-attr` keys. A new `js/i18n.js` holds the Indonesian dictionary, records the English `innerHTML` on first run, and swaps text when the language changes. A missing key keeps the English text.
2. **Pick the language without any network call.** The order is: `?lang=` in the URL, then the choice saved in `localStorage`, then the device's time zone (Indonesia's four zones plus a legacy alias give Indonesian), otherwise English.
3. **Avoid showing the wrong language first.** `i18n.js` loads blocking in `<head>`. For an Indonesian result it sets the class `i18n-wait`, which hides `body` until the swap runs on `DOMContentLoaded` (in a `try/finally`).
4. **Add an EN/ID switch in the header.** It is always visible, uses `aria-pressed`, fades `<main>` during the swap, and saves the choice.
5. **Rewrite the copy as a technical writer would**, following a fixed copy deck (§9). It adds quick start, architecture, open source and closing sections, uses English section ids, and keeps the GitHub links even though the repository is private today.
6. **Add twelve motion effects** (M1–M12) in a new `js/motion.js` and `css/04-motion.css`. The main one is a canvas "graph field" background; the rest are pointer and scroll effects. All must respect reduced motion, the no-JavaScript path and the CSP. A Copy button is added to the quick start.
7. **Add a drift check (TC-617)** to `deploy/check-stacks.sh`. It compares the set of keys in the HTML with the set in the dictionary, both ways, and requires at least 100 keys.
8. **Do the usual release work**: the QA register and its counts, README, changelog, semver 0.50.0. The change list has 18 rows.
9. **Approval route:** the author records that the operator delegated approval to the review, so the design counts as approved when no Blocker survives triage.

### How the existing system works, per this document

- The page is a single Indonesian `index.html` (268 lines) with three stylesheets and one blocking script, `theme.js`. That script sets the theme, runs the `.rv` reveal and runs the board loop.
- The CSP allows `'self'` only: no inline script or style, and `connect-src 'none'`. Styles set from script go through CSSOM.
- Reveal styles apply only under `html.js`. Reduced motion removes movement. Colour tokens are defined three times (light, dark, and dark without JavaScript).
- The page's figures (71 types, 49 events) are stale: the recipe in its own comment now gives 74 and 59. The page lists "Review" as unbuilt, but the README says it is built.
- nginx serves the files statically with a real 404. `NOTICE` publishes the GitHub URL. The QA register has 616 rows and its counts are enforced by `check-workspace.sh`.

I checked these against the repository. The facts hold: 31+40+3=74 and 59 event types by the stated recipe, `theme.js:33-34`, `check-workspace.sh:129-130`, `qa-test-plan.html:707`, `compose.yaml:37`, the `NOTICE` third-party sentence, the docs link targets, and BR-07 / BR-35 in the BRD. Several line anchors are wrong, though (see the findings).

## Findings

### Approval route overrides the procedure's source of truth on an unanchored quote - Severity: Major
- Affected scope: header "Author's note on approval" (lines 5-9)
- Finding: `docs/rules/gaos-design-review.md` says it wins over any other document, and step 8 (line 89) says implementation starts only after the operator approves. `docs/designs/README.md` says only Approved designs may be implemented. The design swaps in "approved when no Blocker survives triage". Its only support is a quoted instruction that cannot be checked in the repository; under README rule 2, an unanchored claim must be labelled as an assumption. Under this rule a Major (including this one) never reaches a person.
- Recommendation: Label the delegation as an assumption and say where it is recorded (for example, a decision-log entry). Otherwise keep step 8. At the least, state that the Majors and the triage table still go into the delivery summary.

### Three mechanisms write `transform` on the same hero mock-up element - Severity: Major
- Affected scope: §5.7 M3, M4, and the paragraph saying the existing reveal stays as it is; `landing-page/index.html:69`
- Finding: `.shot-board` already carries `.rv`, whose reveal animates `transform`/`opacity` (`01-tokens.css:169-172`). M3 adds a keyframe entrance with `translateY` on "the mock-up", and M4 writes `transform: perspective() rotateX() rotateY()` onto `.shot-board` through CSSOM. A CSS animation with a fill mode overrides inline style, and the `.rv`/`.rv.in` rules fight the other two. The result depends on implementation order: the tilt may never show, or the entrance may jump.
- Recommendation: Say which element each effect owns. For example: M3 replaces `.rv` on the mock-up (and the "reveal unchanged" sentence names this exception), and M4 writes to an inner wrapper such as `.board`, not to `.shot-board`.

### Card spotlight on `::before` would replace the card's existing accent - Severity: Major
- Affected scope: §5.7 M5
- Finding: `.card::before` is already the brass top tick (`02-layout.css:91`), and `.card::after` is the existing hover overlay (`02-layout.css:89,97`). M5 puts its radial-gradient spotlight on `::before`, which would remove or distort the tick.
- Recommendation: Name the pseudo-element M5 uses: reuse `::after` (and say the current hover overlay is replaced), or add an inner element. State what happens to the current hover effect.

### The drift check compares multisets: shared keys cause false FAILs - Severity: Major
- Affected scope: §5.9 ("sorts both, and compares with `comm`"); §9 (`feat.more` "used by every card link", `unbuilt.tag` "used three times", `bp.pub` "used twice", `nav.github` reused in the footer)
- Finding: The HTML extraction yields duplicate keys by design. `comm` on lists that are sorted but not de-duplicated reports the extra copies as "missing in D", so H − D is never empty and the first check fails on a correct page. "At least 100 keys" is also ambiguous: it could count unique keys or occurrences.
- Recommendation: Specify `sort -u` for both sets. Say the ≥100 threshold counts unique keys; the deck has about 137.

### `i18n-wait` can leave the page blank for every Indonesian visitor - Severity: Major
- Affected scope: §5.3
- Finding: The only failure case covered is "the script fails to load". If `i18n.js` throws after adding `i18n-wait` but before registering the `DOMContentLoaded` handler (for example, in dictionary parsing or `lang` handling), the `finally` never runs and `body` stays `visibility:hidden` with no recovery. The design does not fix the order of "add class" and "register listener", and there is no CSS fallback.
- Recommendation: State that the class is added last, after the listener is registered and inside the same `try`. Add a CSS safety net, for example a short `animation` on `html.i18n-wait body` that ends in `visibility:visible` after about 1s, which needs no JavaScript.

### §5.9 / §5.10 contradict each other on the run-time labels - Severity: Major
- Affected scope: §5.9 lines 259-263; §5.10 lines 271-279
- Finding: §5.9 still says "The two theme-button labels … are present … on hidden `<template>` elements — no: see §5.10", which is a draft self-correction left in the text. It also still counts two theme labels, while §5.10 makes `aria-label` a single key (`nav.theme`). §5.10 opens with "Three controls" and then lists two. A reader cannot tell whether `<template>` elements exist or how many keys the theme button has.
- Recommendation: Delete the struck sentence in §5.9 and replace it with "Run-time labels are covered by §5.10". In §5.10, say "Two controls" and "one theme key, two copy keys".

### Line anchors drift from the files they cite - Severity: Nit
- Affected scope: §1, §4, §5.1, §5.4, §5.7, §9 anchors
- Finding: The anchors that do not match the files are:

  | Design says | File actually has |
  |---|---|
  | `README.md:46-47` (unbuilt list) | lines 47-48 |
  | `README.md:90` (`GAOS_MODEL_MODE`) | line 102 |
  | `README.md:171-172` (billing) | 170 lines in total; the billing text is at 169-170 |
  | `README.md:71-86` (quick start) | about 73-89 |
  | `02-layout.css:44` | line 42 |
  | `02-layout.css:117` (`.showcase`) | line 123 |
  | `02-layout.css:138-139` (connector) | `.steps::before` at 143, vertical variant at 154 |
  | `03-devices.css:76-98` (board loop) | 72-92; the "only decorative loop" comment is at line 72, not 76, and it is in Indonesian, not the quoted English |
  | `landing.conf:30-33` | the `location /` block is 32-36 |
- Recommendation: Re-anchor each claim against the current files.

### Effects with "—" in the Limits column do not say their reduced-motion and no-JS state - Severity: Nit
- Affected scope: §5.7 M8, M10, M11
- Finding: §4 says every new effect is off under reduced motion and readable without JavaScript. M8 (`scaleX(0)` start state) and M10 (commands start at opacity 0) would hide content if the start state is not gated under `html.js` and reduced motion. "—" leaves this to the implementer.
- Recommendation: State it for each row, for example: "start state only under `html.js` and without reduced motion; otherwise the line and commands show statically."

### Translatable elements with children are not specified - Severity: Nit
- Affected scope: §5.1; §9 `qs.n3` ("links to …"), `foot.copy` ("follows © <year>"), `bp.s2`–`bp.s9`
- Finding: Swapping `innerHTML` replaces all child nodes. The deck does not say whether the `qs.n3` link is inside the string or wraps it. It does not say whether the year span is outside the `foot.copy` element. It does not say whether "KiB" belongs to `bp.s2`–`bp.s9`; only `bp.s1` shows it.
- Recommendation: Add a rule ("an element with `data-i18n` contains no element that script touches or that carries its own key"), and give the exact markup for these three cases.

### The Indonesian "percobaan" is used in two senses - Severity: Nit
- Affected scope: §5.5; §9 `oss.lic.p` ("masa percobaan" for "trial"), `unbuilt.retry.h`
- Finding: §5.5 fixes "percobaan" as the word for "attempt" throughout, but `oss.lic.p` uses it for a licence trial. The §5.5 list of terms kept in English also mixes in one translated term (`workspace` → "ruang kerja"), and the deck uses "story" where §5.5 says `user story`. "Readiness scoring" (built) and "Scoring engine" (not built) also sit close together without a note that they differ.
- Recommendation: Use "uji coba" (or similar) for "trial". Split §5.5 into "kept in English" and "translated" lists.

### Counts and change-list rows that are not exact - Severity: Nit
- Affected scope: §7 rows 11 and 14, and the count line
- Finding: Row 11 (`Dockerfile.landing`) is a no-op: I confirmed it has no count, and it copies the whole directory. It is still counted in "15 edited". Row 14 says "the count sentence", but `.claude/rules/qa-test-plan.md` has "six hundred and sixteen" at lines 73, 76 and 80, plus "616 rows" at line 128 and the TC lines at 50 and 114.
- Recommendation: Drop row 11 (14 edited, 17 rows) and list every line row 14 changes.

### Undefined or unverifiable terms - Severity: Nit
- Affected scope: §5.6, §8.6, §5.7 performance budget, §6
- Finding: "each instance" (§8.6) is not defined. The `gh repo view` PRIVATE result and "well inside a 16ms frame on a mid-range phone" cannot be checked from the repository and are not labelled as assumptions. §6 renames the section ids but does not say what happens to `<main id="utama">` and the skip link `#utama`. A `?lang=` in the URL silently overrides a later click on reload, because the parameter is never removed (§5.2 step 1 plus §5.4).
- Recommendation: Define or drop "instance", label the two claims as assumptions, name the new `main` id, and state the intended `?lang=` behaviour on reload.

Solid as written: §3 Non-goals, §5.2's rejected alternatives, and the §1.2 figures, which match the recipe in the page's own comment.

Counts: B=0 M=6 N=6


---

## Lens: zoom-out

# Zoom-out review: `docs/designs/landing-bilingual.md`

**Isolation note.** While I worked, the harness put the contents of the off-limits file `.claude/CLAUDE.md`, and the repository-root copies of two rule files, into my context. I did not ask for them. I opened none of the deny-listed files and did not use that text for any finding below. The author should take this into account when checking the canary.

**What the design is.** It rewrites the static landing page in English, adds an Indonesian dictionary chosen by time zone, refreshes stale facts, adds open-source sections, adds 12 motion effects, and adds a key-drift check to `check-stacks.sh` (TC-617).

**Solid sections:**
- One HTML file plus a dictionary (§5.1).
- Time-zone detection with no network call (§5.2).
- Putting the drift check in the existing `check-stacks.sh` instead of a new script (§5.9).
- I count about 137 keys in §9, so the "at least 100 keys" guard has real margin.
- The recipe gives 74 graph types and 59 event types today. I checked: `dashboard/schema/*.sql` has 77 `CREATE … TYPE` lines, of which 3 sit in `RECONCILIATION.md` and are excluded, leaving 74. `dashboard/events.go` has 59 event types.

---

### The root cause of defect 2 is left in place; the new check guards a smaller risk - Severity: Major
- Affected scope: §1 defect 2, §2 G5, §5.9, §9 "Numbers" and "Not built yet", `foot.note`
- Simplicity rationale: Defect 2 happened because numbers and the "not built" list were typed into the page by hand and nothing re-checked them (`index.html:115-124`, `:171`, `:264`). The design types 74, 59, "31 vertex, 40 edge, 3" and the three unbuilt parts in again. They appear in the HTML and again inside the Indonesian strings `proof.types`, `unbuilt.*` and `foot.note`. The unbuilt list now appears twice per language (the "Not built yet" block and `foot.note`), so four places. The one new check (G5) guards language-key drift, and a missing key only falls back to English (§5.1). The repository already has the right pattern: `deploy/check-workspace.sh:135-149` (`readme_check`) compares README figures with values the script computes, and its comment says a pinned string is how the README went stale.
- Recommendation: Use the existing `readme_check`-style comparison so the page's two figures are checked against the computed schema and event counts. The `index.html` comment already holds the recipe. Remove the duplicate unbuilt list from `foot.note`, and keep the digits out of the dictionary strings where the sentence allows. This is smaller than any later fix and removes the recurrence. TC-617 can stay, but it is not the check that matters most.

### M5's card spotlight collides with both existing card pseudo-elements - Severity: Major
- Affected scope: §5.7 M5; §7 row 6
- Simplicity rationale: `.card::before` is already the brass tick (`landing-page/css/02-layout.css:91`). `.card::after` is already the hover shadow, fading on opacity (`02-layout.css:89-90, 97`). M5 puts a radial gradient on `::before`, which overwrites or distorts the tick, and no pseudo-element is free. The change list for `02-layout.css` does not mention this. The cards already give hover feedback.
- Recommendation: Drop M5; the existing `::after` shadow already does the job. If the operator wants it anyway, draw the gradient as a `background-image` on `.card` itself, and say what happens to the tick.

### Viewport triggers duplicate the reveal observer that already exists - Severity: Major
- Affected scope: §5.7 M7, M8, M9, M10, M12; §7 row 3
- Simplicity rationale: `landing-page/js/theme.js:66-73` already adds `.in` to every `.rv` element as it enters the viewport. Its CSS (`01-tokens.css:169-181`) already handles the `html.js` gate for no-JavaScript and the reduced-motion fallback. `theme.js:77-81` already shows the pause-off-screen toggle (`is-live`). The design does not say whether `motion.js` reuses these or builds its own observers. If it builds its own, M8 and M10 must redo the no-JavaScript gating themselves. As written, M8 (`scaleX(0)`) and M10 (opacity) have no stated `html.js` gate, so without JavaScript they would hide the connector and the commands.
- Recommendation: Mark `.steps` and the terminal as `.rv` and run M8 and M10 in CSS off `.rv.in`; they then need no JavaScript. Run M9's pause through the same `is-live` pattern. Keep `motion.js` for the work that truly needs script: the field, pointer effects, count-up, progress bar and section spy.

### Shipping "open source" claims and a failing `git clone` to a private repository - Severity: Major
- Affected scope: §5.6; §9 `hero.pill`, the quick-start terminal, `oss.*`, `end.github`
- Simplicity rationale: G1 requires facts that can be checked. Today the pill says "Open source", and the first quick-start command fails for every anonymous visitor (§5.6 says so). The only rejected alternative, a feature flag, is a strawman. The obvious smaller option was not considered: leave the GitHub links and the clone line out until the repository is public, and add them in the same small commit that changes visibility. The local quick start (`docker compose up -d`, `README.md:75-78`) works without the clone line. The §5.6 visibility claim (`gh repo view`) has no anchor in the repository, so it should be labelled as an assumption.
- Recommendation: State the alternative fairly. Either take it, or record that publishing an unreachable source link is an accepted, operator-visible defect, and list it in §10 with an owner.

### Twelve effects, two of which do the same job - Severity: Nit
- Affected scope: §5.7 M1, M2, M4
- Simplicity rationale: The operator asked for a richer, moving background, and M1 delivers that, including reacting to the pointer. M2 (hero glow following the pointer) and M4 (board tilt) are a second and third pointer-follow effect in the same viewport. Each adds a lerp loop and has to be checked under fine pointer, reduced motion and touch.
- Recommendation: Ship M1 as the pointer reaction, and add M2 and M4 only if the screenshots show the hero looks flat.

### The hero ends up with two backgrounds - Severity: Nit
- Affected scope: §5.7 M1 ("the other sections are transparent over the field")
- Simplicity rationale: `.hero::before` already draws a grid (`02-layout.css:51-56`), and `.finale .box::before` draws another (`:162-167`). The design does not say whether these stay under the new graph field.
- Recommendation: Decide in one line: keep, remove, or fade the grids.

### Nine dictionary keys exist only to change decimal separators - Severity: Nit
- Affected scope: §9 `bp.s1`…`bp.s9`
- Simplicity rationale: §5.5 keeps the mock-up's column labels in English because it is the dashboard's own screen. The same reasoning covers file sizes on that screen. These nine keys add no meaning.
- Recommendation: Drop them and leave the sizes as one string. The key total falls from about 137 to about 128, still above the 100 guard.

### Draft text left in §5.9 - Severity: Nit
- Affected scope: §5.9, the paragraph that begins "The two theme-button labels…"
- Simplicity rationale: The sentence describes `<template>` elements and then reverses itself mid-sentence ("— no: see §5.10"). Two readers could take different instructions from it.
- Recommendation: Delete it and point to §5.10.

### The change-list count includes a file that is not changed - Severity: Nit
- Affected scope: §7 row 11; the line "3 new files, 15 edited, 18 rows"
- Simplicity rationale: Row 11 (`deploy/Dockerfile.landing`) is "left untouched unless it does", and the file repeats no count (`Dockerfile.landing:1-14`). From the list I get 3 new files and 14 edited, 17 files changed in total, with 18 rows because one row is a no-op.
- Recommendation: Remove row 11 and state 3 + 14 = 17.

### The third `ok` line's text does not match its test - Severity: Nit
- Affected scope: §5.9, the third bullet
- Simplicity rationale: The message says "two sets of keys are not empty", but the condition tests only that H (the keys in the HTML) has at least 100 entries.
- Recommendation: Make the message say what the test checks. The register prose will quote this string word for word.

### Line anchors are off by a few lines - Severity: Nit
- Affected scope: §4, §5.4, §5.7, §9 anchors
- Simplicity rationale: Cited against actual:
  - `02-layout.css:44` is actually 42; `:117` is 123; `:138-139` is 143-144.
  - `03-devices.css:76-98` is 72-92, and the "only decorative loop" comment is at 72, not 76.
  - `README.md:46-47` is 47-48; `:90` is 102; `:171-172` is 169-170.
- Recommendation: Re-anchor these lines.

### Code-level constants make the motion table hard to read - Severity: Nit
- Affected scope: §5.7 table (lerp .12, 1400px perspective, 26000 px² per node, 90/180ms fade)
- Simplicity rationale: These are tuning values, not design decisions, and they bury the table's real content: what moves, what triggers it, and the fallback.
- Recommendation: Move the constants to a short appendix, or say they are tunable defaults.

### Copy-button precedent not cited - Severity: Nit
- Affected scope: §5.8
- Simplicity rationale: `pustaka/docs/assets/site.js:342-352` already has a copy button with the same 1.6s reset. It falls back to `execCommand`, where the design hides the button instead.
- Recommendation: Cite it as the pattern and state which fallback the page uses, so the two stay consistent on purpose.

### The approval rule ignores Majors - Severity: Nit
- Affected scope: header "Author's note on approval"
- Simplicity rationale: `docs/rules/gaos-design-review.md:88-90` shows every Blocker and Major to the operator, and that file wins over any other (`:11-12`). "Approved when no Blocker survives triage" lets Majors pass without anyone seeing them. The quoted delegation cannot be checked from the repository.
- Recommendation: Add "and every Major has a disposition recorded in the triage table", and label the delegation as an unanchored assumption.

Counts: B=0 M=4 N=9


---

## Lens: critic

# Design review: landing-bilingual (critic lens)

Reviewed `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/.claude/worktrees/landing-bilingual/docs/designs/landing-bilingual.md` against the rubric and the files it cites.

**Isolation note for the canary check:** I did not open `.claude/CLAUDE.md`, `CLAUDE.md`, `AGENTS.md` or `GEMINI.md`. The harness did insert the contents of `.claude/CLAUDE.md` into my context through a system reminder. I did not use it for any finding. Record this as a leak in the isolation evidence for this round.

**What holds up.** The QA register arithmetic checks out:
- The highest ID is TC-616 (`qa-test-plan.html:707`).
- The four count strings are at `qa-test-plan.html:19,79` and `toc/product.js:162,168`.
- The two `prose_check` strings are at `check-workspace.sh:129-130`.
- The event count of 59 is correct (`dashboard/events.go`, 59 `EventType = "` lines).
- The old anchors `#fitur`, `#cara` and `tahun` are used only in `landing-page/index.html` and `landing-page/js/theme.js`.
- The BRD backs the 45-point readiness floor (`brd.html:230`), the five scoring dimensions (`brd.html:877`) and "never kills a run already in flight" (`brd.html:992`, J-13).

---

### The copy deck states two facts the repository contradicts - Severity: Major
- Affected scope: §9 Numbers (`proof.types`, "74 … 40 edge"); §9 Features 1 (`feat.board.p`); §5.5
- Finding and rationale: G1 promises "facts that can be checked in the repository", and §5.5 makes the deck a verbatim contract. Two facts in it are wrong.
  1. **The edge count.** The 40 comes from the comment's naive `grep 'CREATE EDGE TYPE'` recipe. That grep also matches a SQL comment, `dashboard/schema/09-indexes.sql:103` ("-- The model document declares these as `CREATE EDGE TYPE ... UNIQUE`"). The real statements number 39 (08-edges 32, 01-supertypes 1, 11-blueprint 5, 16-qa-catalog 1). The total is therefore 73, not 74. The 31 vertex types also include the abstract `GaosVertex` supertype (`01-supertypes.sql:15`), and the edges include `GaosEdge` (`:23`).
  2. **The board columns.** `feat.board.p` says each board has "the columns Backlog, Ready, Working, Needs you, Review and Done". `dashboard/board.go:133-142` defines nine columns, including Blocked, Failed and Canceled. The old copy said so (`index.html:142`). The new sentence "the board and the data cannot disagree" is also the kind of absolute claim G1 excludes.
- Recommendation: Recount with comment lines excluded (`grep -hE '^\s*CREATE EDGE TYPE'`) and decide whether abstract supertypes count. Put the corrected recipe in the HTML comment that carries the numbers. Rewrite `feat.board.p` to name all nine columns, or say "six columns are shown in the mock-up". Drop "cannot disagree".

### Motion initial states are not specified for the no-JS, script-failure and reduced-motion paths - Severity: Major
- Affected scope: §4 "Reduced motion", §4 "No JavaScript", §5.7 M3, M7, M8, M9, M10
- Finding and rationale:
  - **Script-gated hidden states.** M8 starts the connector at `scaleX(0)`, and M10 starts the terminal lines at opacity 0. Both are revealed only by `motion.js`, which "returns immediately" under reduced motion. Neither has an exception noted, and both Limits cells say "—".
  - **The wrong gate.** The only gate class available is `html.js`, which `theme.js:16` sets, not `motion.js`. If `motion.js` returns early, fails to load or throws, the steps line and the quick-start commands stay invisible. The quick-start commands are the core content of G3.
  - **§4 against the table.** §4 says "every new effect is off" under reduced motion, but M6, M8, M10, M11 and M12 run or say "—".
  - **The global override.** `01-tokens.css:180` forces `animation-duration:.001ms !important; animation-iteration-count:1` on `*`. M3's "reduced motion: opacity only" therefore cannot be a keyframe animation. M9's infinite dot runs once and parks at the end of its path, which is an undefined final position.
- Recommendation: Gate every script-revealed initial state on a class that `motion.js` itself adds, for example `html.motion`. Define the end state as the default. State each effect's behaviour under reduced motion explicitly, and reconcile that with §4. Say whether M3 and M9 are exempt from the `01-tokens.css:180` override, and how.

### The innerHTML swap destroys child elements, and nesting is unconstrained - Severity: Major
- Affected scope: §5.1, §9 (`hero.pill`, `qs.n3`, `foot.copy`, `nav.github`)
- Finding and rationale:
  - **Child elements are lost.** Switching language writes `innerHTML` on the keyed element. Any child that is not inside the string is destroyed: the pill's `<span class="dot">` (`index.html:58`), an icon inside the GitHub link, and the link that `qs.n3` "links to" (is the `<a>` inside the string or around it?). Destroyed children also lose any listeners or classes that `motion.js` attached.
  - **Nesting is not ruled out.** If a keyed element contains another keyed element, the outer write clobbers the inner one and the recorded English drifts.
- Recommendation: Add a rule: `data-i18n` goes only on a leaf whose whole content is the string, and keyed elements never nest. Specify the exact element for `hero.pill`, `qs.n3`, `foot.copy` and `nav.github`. Consider extending the §5.9 check to reject a `data-i18n` element that contains another one.

### The wait class blanks the page until deferred scripts load, and nothing removes it if setup fails - Severity: Major
- Affected scope: §5.3
- Finding and rationale:
  - **Coupling to `motion.js`.** `DOMContentLoaded` does not fire until deferred scripts have executed, `motion.js` included (§5.7). An Indonesian visitor therefore sees a blank `visibility:hidden` body until the whole document is parsed and `motion.js` is fetched and run.
  - **No fallback.** If anything between adding `i18n-wait` and registering the listener throws (detection code, a storage-access exception the step chain did not anticipate), nothing ever removes the class. The `try … finally` covers only the apply step. There is no timeout fallback.
  - **The fixed canvas is also hidden.**
- Recommendation: Wrap the whole head-time block so that any exception removes `i18n-wait`. Add a hard fallback, for example `setTimeout(remove, 1500)`. Apply the dictionary on `DOMContentLoaded` without depending on `motion.js`, or apply it from a small script at the end of `<body>`, and say which. Add a verification step that throttles the network.

### Card spotlight collides with the existing pseudo-elements - Severity: Major
- Affected scope: §5.7 M5
- Finding and rationale: M5 puts a radial gradient on `.card::before`. That pseudo-element is already the brass accent bar (`02-layout.css:91`), and `.card::after` is the hover shadow (`02-layout.css:89-90`). As written, the spotlight either replaces the accent or fights its geometry (`left:24px; width:28px; height:2px`). The design does not say which.
- Recommendation: Name the element that carries the spotlight: an inner `<span class="spot" aria-hidden="true">`, or move the accent to a border or box-shadow. State what happens to the accent bar.

### The performance budget counts pair tests and ignores fill and compositing - Severity: Major
- Affected scope: §5.7 "Performance budget", M1, §10 "Canvas cost"
- Finding and rationale: "2016 pairs per frame, well inside a 16ms frame on a mid-range phone" has no source or measurement behind it, and pair tests are the cheap part. The real costs are not counted:
  - A full-viewport clear and redraw at DPR 2 on every frame, about 1.3 M pixels at 390×844.
  - Re-blurring the sticky header's `backdrop-filter: blur(10px)` (`02-layout.css:18`) over an animating layer on every frame.
  - The loop running forever on touch devices while the tab is visible. Only `document.hidden` stops it.

  Battery cost is not mentioned.
- Recommendation: Add a frame-rate cap (for example 30 fps on coarse pointers), an idle stop or a reduced node count on mobile, and a measured number in §8: Chrome performance trace, frame time at 390×844 with CPU throttling at 4×. Otherwise label the 16ms claim an assumption.

### The page ships "Open source", a clone command and four links to a repository visitors cannot see - Severity: Major
- Affected scope: §5.6, §9 Quick start, §9 Open source, `hero.pill`
- Finding and rationale: The design says the repository is private today. The page's first line says "Open source · Apache-2.0". The primary call to action is a `git clone` that fails for every anonymous visitor, and four links return 404. This is a visible false claim on the project's front page, on every deploy until an operator action outside this change happens. The design calls that action "known" but does not sequence it. "The URL is already in NOTICE" does not help here: NOTICE is not a call to action.
- Recommendation: Make deployment conditional. Either the deploy step in §8.6 checks repository visibility first, or the release is held until visibility flips. Otherwise point the quick start at the local path (README's `docker compose up -d`) and link the licence to a copy served by the site. Record the choice as a decision (D-090).

### The Open Graph tags and image keep the slogan G1 removes - Severity: Major
- Affected scope: §5.1 "The Open Graph tags stay English", §3 non-goal on `og-image.png`, §9
- Finding and rationale: The OG tags are Indonesian today, not English. `og:title` is the contrast slogan (`index.html:15-16`). The image at `landing-page/img/og-image.png` prints the same slogan in Indonesian. The copy deck has no `og.title` or `og.description` strings, so "stay English" either keeps the Indonesian slogan or leaves the implementer to invent text. Link previews are where most visitors from outside Indonesia first see the page, so defects 1 and 3 survive exactly there.
- Recommendation: Add `og:title` and `og:description` strings to §9 and put them in the change list. Either move the image redraw into scope, or remove `og:image` until it is redrawn, and say which.

### Self-approval replaces the operator gate without a recorded decision - Severity: Major
- Affected scope: "Author's note on approval" (lines 5-9)
- Finding and rationale: `docs/rules/gaos-design-review.md:36,89` makes the operator the approver and says the policy wins over any other document (`:11-12`). The design substitutes "approved when no Blocker survives triage", but the author is the one who triages, and triage allows CONTEST and DECLINE. So the author can approve their own design. That is the single-context failure the policy exists to prevent (`:16-19`). The operator's quoted delegation is not anchored to any file or decision.
- Recommendation: Record the delegation as a decision-log entry, or point to where the operator gave it. Tighten the condition: no Blocker or Major disposed of by CONTEST or DECLINE without a second lens confirming it. Show the triage table in the delivery summary.

### The drift check's set comparison breaks on keys that repeat - Severity: Nit
- Affected scope: §5.9
- Finding and rationale: `feat.more`, `unbuilt.tag`, `bp.pub` and `nav.github` repeat in the HTML by design. "Sorts both, and compares with `comm`" with plain `sort` reports the duplicates as H − D. The check also does not catch a key declared twice in `i18n.js`, where the last one silently wins.
- Recommendation: Specify `sort -u` for both sets, and add a `uniq -d` check on D with its own `FAIL` line.

### Leftover self-correction in §5.9 and §5.10 - Severity: Nit
- Affected scope: §5.9 last paragraph, §5.10 first sentence
- Finding and rationale: "…on hidden `<template>` elements — no: see §5.10" is drafting residue. §5.10 says "Three controls … (two states)" and then lists two controls, and §5.9 still speaks of "two theme-button labels" that §5.10 merges into one.
- Recommendation: Delete the `<template>` sentence and make the count say "two controls".

### Anchor drift in cited line numbers - Severity: Nit
- Affected scope: §1, §4, §5.4, §5.7, §9 anchors
- Finding and rationale: Several cited lines do not hold what the design says they hold:

  | Citation | Actually at |
  |---|---|
  | `README.md:46-47` | `:47-48` |
  | `README.md:90` (`GAOS_MODEL_MODE`) | `:102` |
  | `README.md:171-172` (billing) | `:169-170`; the file ends at 171 |
  | `README.md:71-86` | `:73-89` |
  | `02-layout.css:44` | `:42` |
  | `02-layout.css:117` (`.showcase`) | `:123` |
  | `02-layout.css:138-139` (connector) | `:143-144`; 138-140 is the comment |
  | `03-devices.css:76` (comment) | `:72` |
  | `03-devices.css:76-98` (loop) | `:72-92` |
  | `deploy/nginx/landing.conf:30-33` | `:32-36` is the `location /` block |

- Recommendation: Re-anchor all of them.

### The change-list count includes a no-op row - Severity: Nit
- Affected scope: §7 row 11, counts line
- Finding and rationale: Row 11 (`Dockerfile.landing`) is "left untouched unless it does", and it does not: `deploy/Dockerfile.landing:1-14` holds no count. "15 edited" is therefore 14.
- Recommendation: Drop row 11, or move it to the non-goals, and restate the counts as 3 new and 14 edited.

### Unresolved small choices - Severity: Nit
- Affected scope: §9 Hero (`hero.start`), §5.8, §2 / security criterion
- Finding and rationale:
  - The target of "Get started" is not stated (`#quick-start`? `/app/`?).
  - The copy button is visible but does nothing without JS.
  - The design adds links to a new external origin (github.com) but does not say whether the security lens criterion (`gaos-design-review.md:94-97`) applies.
- Recommendation: Name the link target. Hide the copy button unless `html.js` is set. Add one line on whether the security lens applies.

Counts: B=0 M=9 N=5


---

## Lens: readiness

# Readiness review: `docs/designs/landing-bilingual.md`

**Isolation:** the harness put the contents of `.claude/CLAUDE.md` and a main-checkout copy of `.claude/rules/agent-workspace.md` into my context as system reminders. I did not open them myself, and nothing below relies on them. Record this when you check the canaries.

**What I checked against the repository:**
- **Facts:** 74 graph types and 59 event types are correct. There is no `go.sum` in either module, and the seven linked docs pages exist.
- **Old section ids:** `#fitur` and `#cara` are used only in `landing-page/index.html`.
- **QA register:** TC-617 is the next free id. The count strings at `deploy/check-workspace.sh:129-130` match the design, `README.md:139` is checked by the README line at `check-workspace.sh:148`, and the `ok`/`FAIL` format matches `check-stacks.sh:21-22`.
- **Sound parts:** goals, non-goals, CSP constraints, language detection (§5.2) and the identifier allocation are clear.

### The copy button and two effects break under reduced motion - Severity: Major
- Affected scope: §5.7 (opening paragraph, M8, M10), §5.8, §7 row 3
- Finding: `motion.js` "returns immediately" under `prefers-reduced-motion: reduce`, except for effects marked otherwise. Three things are not marked:
  - **The copy button (§5.8).** It is wired in `motion.js` (row 3), so it does nothing for these visitors. §5.8 hides it only when the clipboard API is missing.
  - **The terminal (M10).** The lines are presumably hidden first and shown in turn. If `motion.js` never starts, the quick-start commands stay at opacity 0.
  - **The steps connector (M8).** It starts at `scaleX(0)` and only grows when the script adds a class, so it never appears.
  
  The design does not say which class hides the starting state. If it is `html.js` (set by `theme.js`), the content disappears both under reduced motion and when `motion.js` fails to load.
- Recommendation: Move the copy button out of the early return, or into its own file. Name the class that hides the starting state (for example `html.motion`), set by `motion.js` only after its reduced-motion check. Add to §8 a reduced-motion check that the terminal and connector are visible and the copy button works.

### Three owners of `transform` on the hero mock-up - Severity: Major
- Affected scope: §5.7 M3, M4, and the closing paragraph ("existing reveal … stay as they are")
- Finding: `.shot-board` already has `.rv` (`index.html:69`), and `01-tokens.css:169-172` animates its opacity and transform. M3 also gives it a keyframe entrance on `transform`, and M4 writes `transform: perspective() rotateX() rotateY()` to the same element through CSSOM. A keyframe animation with fill-mode `both` overrides the inline transform, so the tilt silently stops working. Removing the fill-mode breaks the entrance instead. Two engineers would layer these differently.
- Recommendation: State that `.rv` is removed from `.shot-board`, so M3 replaces the reveal there. Put the M4 tilt on a different element: either an inner wrapper or `.board` itself. Name that element.

### M5 spotlight's `::before` is already used - Severity: Major
- Affected scope: §5.7 M5
- Finding: `.card::before` is the brass accent tick (`02-layout.css:91`), and `.card::after` is the hover shadow (`02-layout.css:89-90`). M5 asks for "a `::before` radial gradient". The implementer must choose between losing the tick, moving it, or adding an element. It is also unclear whether "feature card" includes the open-source cards that §6 adds.
- Recommendation: Name the host of the spotlight. For example, a `<span class="spot" aria-hidden="true">` inside each `.card`, or move the tick to a `border-top` gradient. Say which card sets get M5.

### The drift check's `sort` produces false failures - Severity: Major
- Affected scope: §5.9
- Finding: Some keys appear several times in the HTML:
  - `feat.more` in every card link
  - `unbuilt.tag` three times
  - `bp.pub` twice
  - `nav.github` twice
  
  With a plain `sort`, `comm` treats lines as a multiset, so `comm -23 H D` prints the extra copies. The "every translatable string … has an Indonesian entry" line then fails on a correct page. The "at least 100 keys" guard also does not say whether it counts unique keys. The deck has about 137 unique keys, so either reading passes today, but the rule is ambiguous.
- Recommendation: Specify `sort -u` for both sets and say the threshold counts unique keys. Write out the two `grep -oE` patterns exactly, so the scenario prose in the QA register can quote them.

### The markup for the new sections is not specified - Severity: Major
- Affected scope: §6, §7 rows 1 and 6, §9 (Architecture, Quick start, Open source)
- Finding: The copy deck fixes the strings but not the structure. These are left open:
  - **Architecture:** the layout of the three layers, and their breakpoint (M9 says 760px, while the steps use 900px).
  - **Quick-start terminal:** its markup and the copy button's position.
  - **The four paths:** whether the path `<code>` is inside or outside the keyed string.
  - **Open-source cards:** which card class they use.
  - **Link targets:** where `hero.start`, `end.start` and the GitHub links point (`#quick-start`? a new tab?).
  - **Dropped content:** whether the existing `.on-graph` lines (`index.html:143` etc.) and the footer's Mission Control link (`index.html:263`) are kept. The deck has no keys for them, so keeping them breaks the "every string translated" goal.
  - **Language group role:** for §5.4, `role="group"` with `aria-label` is not stated.
  - **New class names** for `02-layout.css` and `04-motion.css`.
  
  Two engineers would build visibly different pages.
- Recommendation: Add, per new section, a short markup skeleton with class names and `href`s. State outright that `.on-graph` and the footer's Mission Control link are kept with keys, or removed.

### The header may overflow between 760px and about 950px - Severity: Major
- Affected scope: §5.4, §6 item 1, §8.3
- Finding: The header goes from three links to five, plus the language switch, the theme button and the "Open Mission Control" button. Links hide only at 760px or narrower (`02-layout.css:42`). A rough width sum (brand about 110px, links about 425px, switch, theme button, the button about 190px, gaps) exceeds the space at roughly 761–940px. The screenshots in §8 are taken only at 1440px and 390px, so nothing would catch this.
- Recommendation: Choose a breakpoint at which the text links (or some of them, named) collapse. Add a screenshot at 820px to §8.3. Also say whether the GitHub header link is a `.lnk` that hides on mobile.

### No phases, no exit conditions, subjective verification - Severity: Major
- Affected scope: §7, §8
- Finding: Eighteen changes land as one step. Nothing says what must come first. For example, the `data-i18n` keys in `index.html` and the dictionary must land together, or the TC-617 check fails in between. The register, `qa-test-plan.md` and `check-workspace.sh` counts must also move together. §8.3 (screenshots) and the contrast check (§10) have no pass criterion, so in effect "it looks right". There is no check with JavaScript disabled, although §4 states that constraint.
- Recommendation: Split the work into phases with an exit command for each. For example:
  1. Copy, i18n and the drift check together, with `check-stacks.sh` passing.
  2. Motion, checked with screenshots and a console free of CSP errors.
  3. Register, counts and release, with `check-workspace.sh` and `pustaka check` passing.
  
  Give each screenshot an explicit criterion ("no horizontal scroll; terminal text visible; the `?lang=id` page has no English text in `<main>`"). Add a JavaScript-disabled pass.

### Change list counts a file that is not changed - Severity: Nit
- Affected scope: §7 row 11 and the "Counts" line
- Finding: `deploy/Dockerfile.landing` contains no asset count (I read lines 1-14), so row 11 changes nothing. The total "15 edited, 18 rows" should be 14 and 17. A conditional row also weakens the write allowlist.
- Recommendation: Remove row 11 and correct the counts.

### The nginx comment fix misses a second count - Severity: Nit
- Affected scope: §7 row 10
- Finding: `deploy/nginx/landing.conf:32` also says "its one script". Only line 2 is listed for correction.
- Recommendation: List line 32 too.

### Leftover drafting in §5.9 and §5.10 - Severity: Nit
- Affected scope: §5.9 (last paragraph), §5.10 (first sentence), §5.5
- Finding:
  - §5.9 still has the abandoned `<template>` idea ("… elements — no: see §5.10").
  - §5.10 says "Three controls" and lists two.
  - The §5.5 term list mixes "`workspace` is translated" into a list of terms kept in English.
- Recommendation: Delete the `<template>` sentence, change "Three" to "Two", and split §5.5 into a "kept in English" list and a "translated" list.

### Line anchors have drifted - Severity: Nit
- Affected scope: §1, §4, §5.4, §5.7
- Finding: Several cited lines are off by a few:

  | Design cites | Actual line(s) |
  |---|---|
  | `02-layout.css:44` | `:42` |
  | `02-layout.css:117` (`.showcase`) | `:123` |
  | `02-layout.css:138-139` (connector) | `:143-144` |
  | `03-devices.css:76` (the "only decorative loop" comment) | `:72` |
  | `01-tokens.css:163-171` | `:164-172` |
  | `01-tokens.css:174-180` | `:174-181` |
  | `README.md:46-47` | `:47-48` |

- Recommendation: Fix the line numbers. The `03-devices.css` comment is an edit target, so that one matters most.

### Language-switch fade and the `i18n-wait` fail-safe are not fully specified - Severity: Nit
- Affected scope: §5.3, §5.4
- Finding:
  - The easing for the 90ms/180ms fade is not named.
  - §5.3 does not fix the order of "add the `i18n-wait` class" and "register the `DOMContentLoaded` handler". If something throws between them, the body stays hidden permanently.
  - M3's entrance plays while the body is hidden for Indonesian visitors.
- Recommendation: Name `--ease-out`. Require the listener to be registered before the class is added. Say whether M3 waits for `i18n-wait` to be removed.

### Approval delegation quote has no anchor - Severity: Nit
- Affected scope: header "Author's note on approval"
- Finding: The quoted delegation has no file and line, which `docs/designs/README.md` rule 2 requires. It also overrides step 8 of `docs/rules/gaos-design-review.md` (show Blockers and Majors to the operator) on the author's word. This is outside the readiness lens and is noted for whoever triages.
- Recommendation: Anchor the quote or label it as an assumption, and state how the Majors are dispositioned without the operator.

Counts: B=0 M=7 N=6


---

## Lens: copy-en

I edited no files. Thirteen English strings in section 9 need changes; the rest read clean.

| key | problem | replacement |
|---|---|---|
| `hero.sub` (last sentence) | "Every change … any earlier state can be replayed" claims more than the README, which only says state lives in an append-only event stream and the orchestrator rebuilds from the graph | State is kept in the graph and an append-only event stream, so an orchestrator that stops mid-task rebuilds from them. |
| `proof.port` | "per deployment" overclaims; the README supports one port for the local stack only (production adds an nginx edge) | port published by the local stack |
| `feat.lead` | Rhythm triplet; "documented" is untrue for the two cards marked "No link" | The features below are implemented and covered by tests. Planned work that is not built yet is listed separately. |
| `feat.board.p` (sentence 2) | "cannot disagree" is an absolute nobody can check | The columns are computed from task status in the graph; the board stores no state of its own. |
| `feat.bp.p` | "the agent's workspace" conflicts with "the product's workspace" used in the README and in `how.s4.p` | …synced into the product's workspace as the agent's rules. (rest unchanged) |
| `feat.ready.p` (sentence 2) | "an unclear specification produces an unclear plan" is an aphorism, not a fact | Below 45 the planner refuses to plan and returns the list of what is missing. |
| `feat.time.p` | "Every change" is absolute; replay is not in the README; "run" should be "attempt" | Changes are written as events to an append-only stream, and the timeline is read from that stream. If the orchestrator stops in the middle of an attempt, it rebuilds its state from the graph. |
| `unbuilt.score.h` | Reads as contradicting the built "Readiness scoring" card | Result scoring engine |
| `unbuilt.score.p` | "whatever the total" is awkward | …with a safety limit that stops the work regardless of the total. |
| `unbuilt.budget.p` | "run" should be "attempt" | Refuses to dispatch once a product reaches its spending cap. It does not stop an attempt that is already in progress. |
| `how.s2.p` | "Planning starts at 45" is non-idiomatic; a score does not start anything | The record is scored out of 100. The planner accepts a score of 45 or above. |
| `arch.gaos.p` | "Shapes the work" is vague; the README says "no model tool", not "no model" | Holds the product record, the tasks and the Blueprint, and decides what to dispatch. GAOS runs no model tool itself. |
| `arch.worker.p` | "works the task" is non-idiomatic | An agent such as ai-claude-cli that carries out the task in that workspace. |
| `oss.lic.p` (sentence 1) | Four-beat imperative chain directly before a second triplet | You may use, modify and redistribute it. (rest unchanged) |
| `oss.con.p` (sentence 1) | "take … and no CLA" is awkward | Contributions require a DCO sign-off (<code>git commit -s</code>); there is no CLA. |

**Clean sections:** Document, Header, Board mock-up, Quick start, Closing and Footer. In the partly edited sections, `feat.gate`, `feat.inbox`, `feat.api`, `unbuilt.retry`, `how.s1`, `how.s3`, `how.s4`, `arch.aim`, both `arch.note` strings and `oss.stack` are also clean.

**Facts the README does not carry:**
- **Confirmed elsewhere in the repo:** zero Go dependencies and no `go.sum` (`dashboard/go.mod`, `pustaka/go.mod`), and the 45 floor with "planner refuses, returns the missing list" (`pustaka/docs/product/brd.html`).
- **Not checked:** the 74 schema types, the 59 event types, "each attempt records its Blueprint version", "a published version does not change", and "dependencies checked for cycles on every write". Anchor these before shipping.

**Other notes:**
- "Mission Control" (`nav.open`, `qs.p2`, both aria strings) is not in the README, which says "the dashboard". The name does appear in the dashboard code, so I left it.
- If you take the `unbuilt.score.h` rename, `foot.note` should say "the result scoring engine" to match.
- The README line anchors in the deck are stale: `GAOS_MODEL_MODE` is at line 102 (not 90), billing at 169-170 (not 171-172), quick start at 73-89 (not 71-86).


---

## Lens: copy-id

Saya tidak menyunting berkas apa pun. Sebagian besar deck sudah baik; 19 string di bawah ini perlu diubah.

| key | masalah | string pengganti |
|---|---|---|
| `meta.desc` | "di-hosting sendiri" kaku; "satu giliran demi satu giliran" harfiah | GAOS adalah sistem open source yang dijalankan di server sendiri. GAOS menyimpan kebutuhan, tugas, dan kebijakan produk dalam graph, lalu mengirim setiap tugas ke agen koding AI giliran demi giliran. Berlisensi Apache-2.0. |
| `a11y.skip` | "Lewati ke" tidak lazim | Langsung ke konten |
| `hero.pill` | "Hosting sendiri" tidak dipakai engineer | Open source · Apache-2.0 · Self-hosted |
| `hero.sub` | "sebuah graph" harfiah; "any earlier state" hilang | GAOS menyimpan kebutuhan, user story, keputusan, dan kebijakan setiap produk dalam graph. Pekerjaan direncanakan sebagai tugas di papan kanban, lalu setiap tugas dikirim ke agen giliran demi giliran, di ruang kerja yang sudah memuat aturan produk tersebut. Setiap perubahan dicatat sebagai event, sehingga keadaan mana pun sebelumnya dapat diputar ulang. |
| `board.t215` | "ke terdekat" janggal | Pembulatan harga: ke atas atau ke nilai terdekat? |
| `feat.title` | Makna bergeser: "dikerjakan" berarti pekerjaan selesai, bukan kemampuan | Yang sudah bisa dilakukan GAOS saat ini |
| `feat.board.p` (kalimat akhir) | "adalah sebuah permintaan" harfiah | Memindahkan kartu hanyalah permintaan; state machine tugas dapat menolaknya. |
| `feat.gate.p` (dua kalimat akhir) | "tinjauan lolos" terbalik; "ditolak" lebih keras dari "blocked" | …dan tugas implementasi baru dimulai setelah desain lolos tinjauan. Desain yang tidak lolos dikembalikan ke penulisnya dan ditinjau ulang. |
| `feat.inbox.p` (kalimat akhir) | Jawaban "mencatat" dirinya sendiri | Asal setiap jawaban ikut dicatat. |
| `feat.time.p` | "menulis event ke aliran yang hanya dapat ditambah" harfiah; "run" seharusnya "percobaan" | Setiap perubahan dicatat sebagai event di stream yang bersifat append-only. Keadaan pada waktu mana pun dihitung dari stream itu, sehingga riwayat produk dapat diputar ulang. Jika orchestrator berhenti di tengah percobaan, keadaannya dibangun kembali dari graph. |
| `unbuilt.score.p` | "keamanan" berarti security; yang dimaksud safety | Menggabungkan lima dimensi berbobot dari hasil kerja agen menjadi satu skor, dengan batas pengaman yang menghentikan pekerjaan berapa pun totalnya. |
| `unbuilt.budget.p` | "run" seharusnya "percobaan"; "pengeluaran" kaku | Menolak pengiriman tugas setelah produk mencapai batas biayanya, dan tidak pernah menghentikan percobaan yang sedang berjalan. |
| `how.lead` | "record" tidak konsisten dengan "catatan" | Setiap langkah menulis ke graph. Label di atas setiap langkah menyebutkan catatan yang ditulisnya. |
| `how.s3.p` (kalimat akhir) | "pada setiap penulisan" kaku | Setiap tugas mencakup satu hal dan memiliki minimal satu kriteria penerimaan; setiap kali ditulis, dependensinya diperiksa agar tidak membentuk siklus. |
| `arch.note1` | "secara bawaan" terasa terjemahan | Secara default, giliran model nonaktif. Atur `<code>GAOS_MODEL_MODE=aimanager</code>` dan arahkan `<code>GAOS_AIM_URL</code>` ke gateway ai-manager untuk mengaktifkannya. |
| `qs.p4` | "keterjangkauan" tidak lazim | pemeriksaan kesehatan, termasuk koneksi ke database |
| `oss.lic.p` (kalimat kedua) | "masa percobaan" bentrok dengan istilah "percobaan"; "dinonaktifkan" kurang tepat | Tidak ada kunci lisensi, masa uji coba, atau fitur yang dikunci. |
| `end.p` | "baca hasil dari" harfiah | Jalankan stack lokal, deskripsikan satu produk, lalu lihat hasil gerbang kesiapan. |
| `foot.note` (kalimat akhir) | "Catatan rilis mencatat" berulang | Catatan rilis memuat apa yang ditambahkan di setiap rilis. |

**Istilah yang Anda tanyakan**
- **"agen koding AI"**: wajar dan langsung dipahami; pertahankan. Saya tidak memeriksa apakah "koding" bentuk baku di KBBI; bila harus baku, pakai "agen AI pemrograman" dan "kebijakan penulisan kode".
- **"Hosting sendiri"**: ganti dengan "Self-hosted" (label) atau "dijalankan di server sendiri" (kalimat).
- **"keterjangkauan database"**: ganti dengan "koneksi ke database".
- **"Mulai cepat", "Gerbang desain", "Pengaman percobaan ulang", "Pengaman anggaran", "Linimasa dan pemutaran ulang", "halaman utama"**: wajar, pertahankan.
- **"Mesin penilaian"**: dapat diterima; "Mesin skor" lebih selaras dengan "skor kesiapan", tetapi tidak wajib.
- **"Catatan rilis"**: dapat diterima untuk changelog, asalkan judul halaman dokumentasinya sama.

**Yang sudah baik**
- Navigasi, tombol, dan semua kartu papan selain T-215.
- Bagian angka proyek dan seluruh showcase Blueprint.
- `feat.bp.p`, `feat.ready.p`, `feat.api.p`, `unbuilt.retry.p`.
- Langkah 1, 2, dan 4, serta `arch.note2`.
- `oss.con.p` dan `oss.stack.p`.
- Pemakaian "giliran", "pengiriman", "ruang kerja", dan "papan" konsisten; "percobaan" konsisten kecuali di tiga string yang saya tandai.

Tidak ada string yang bernada iklan.


---

## Triage (author)

Isolation: see §11.11 of the design. Severities are the reviewers' own.

| Finding | Lens | Severity | Disposition | Note |
|---|---|---|---|---|
| Two facts the repository contradicts (73 types, nine columns) | critic | Major | PROPOSE FIX | §11.1, §11.8; recipe anchored to line start |
| Root cause of stale figures left in place | zoom-out | Major | PROPOSE FIX | §11.2: `data-fig` + TC-618; unbuilt list stated once |
| Motion start states unsafe under no-JS, script failure, reduced motion | critic, readiness, zoom-out, comprehension (Nit) | Major | PROPOSE FIX | §11.5: CSS off `.in`, nothing hidden depends on `motion.js` |
| Copy button dead under reduced motion | readiness | Major | PROPOSE FIX | §11.5: wired in `theme.js` |
| Three owners of `transform` on the hero mock-up | comprehension, readiness | Major | PROPOSE FIX | §11.5 |
| M5 spotlight collides with card pseudo-elements | all four | Major | PROPOSE FIX | §11.5: `.spot` element. Zoom-out's "drop M5" not taken: the operator asked for motion on pointer movement |
| Drift check compares multisets | comprehension, readiness (Major), critic (Nit) | Major | PROPOSE FIX | §11.3: `sort -u`, duplicate-key line |
| `i18n-wait` can strand the page; DCL waits on deferred script | comprehension, critic | Major | PROPOSE FIX | §11.4: one `try`, listener first, CSS safety net, `async` |
| innerHTML swap destroys children; nesting unconstrained | critic (Major), comprehension (Nit) | Major | PROPOSE FIX | §11.4 leaf rule. Extending the check to detect nesting: DECLINE — not expressible in `grep`, and the leaf rule is checked by the id-language screenshot criterion |
| Performance budget ignores fill and compositing | critic | Major | PROPOSE FIX | §11.5: 30 fps and fewer nodes on coarse pointers, no backdrop blur over the field, claim labelled an assumption, trace in §11.10 |
| Open Graph tags and image keep the slogan | critic | Major | PROPOSE FIX | §11.4: tags rewritten, image redrawn |
| Header may overflow 760–950px | readiness | Major | PROPOSE FIX | §11.6 breakpoints; 820px added to verification |
| Markup of new sections unspecified | readiness | Major | PROPOSE FIX | §11.6 |
| No phases, no exit conditions | readiness | Major | PROPOSE FIX | §11.10 |
| §5.9 / §5.10 contradict each other | comprehension (Major), others (Nit) | Major | PROPOSE FIX | leftover removed in place |
| Viewport triggers duplicate the reveal observer | zoom-out | Major | PROPOSE FIX | §11.5 |
| "Open source" claims and a failing clone against a private repository | critic, zoom-out | Major | DECLINE | §11.7. The fix (ship without source links) costs a second release and fails the operator's stated goal; zoom-out offered "accepted, operator-visible defect with an owner", which is what is recorded. Owner: the operator |
| Approval route overrides the policy on an unanchored quote | comprehension, critic (Major); zoom-out, readiness (Nit) | Major | PROPOSE FIX | header note: labelled an assumption, condition tightened, Majors and this table go to the operator. A decision-log entry: DECLINE — comprehension offered the lighter remedy taken here |
| Twelve effects, two redundant (M2, M4) | zoom-out | Nit | DECLINE | the request was for more motion on pointer movement |
| Hero ends up with two backgrounds | zoom-out | Nit | PROPOSE FIX | §11.5: kept, stated |
| Nine keys for decimal separators | zoom-out | Nit | PROPOSE FIX | dropped |
| Change list counts a no-op row; row 14 incomplete; nginx second count | several | Nit | PROPOSE FIX | §11.9 |
| Third `ok` line's text does not match its test | zoom-out | Nit | PROPOSE FIX | §11.3 |
| Line anchors drifted | all four | Nit | DECLINE | the cited lines are in files this change rewrites; the anchors that matter for edits are restated in §11.9 |
| Constants bury the motion table | zoom-out | Nit | PROPOSE FIX | §11.5 last bullet |
| Copy-button precedent not cited | zoom-out | Nit | PROPOSE FIX | pattern follows `pustaka/docs/assets/site.js:342-352`; fallback is to hide |
| Fade easing, `?lang=` on reload, `main` id, "instance" | readiness, comprehension | Nit | PROPOSE FIX | §11.4, §11.10 |
| "percobaan" used in two senses; term list mixed | comprehension | Nit | PROPOSE FIX | §11.8 ("masa uji coba") |
| Unresolved small choices (link target, security lens) | critic | Nit | PROPOSE FIX | §11.6, §11.7 |
| English copy table (15 strings) | copy-en | — | PROPOSE FIX | all taken, §11.8; replay kept because `handleReplay` anchors it |
| Indonesian copy table (19 strings) | copy-id | — | PROPOSE FIX | all taken, §11.8 |

No Blocker in any lens, so no second round is required by the policy.
