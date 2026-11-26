# Readiness review: `docs/designs/pustaka-loading-and-motion.md` (v1, In Review)

**Verdict:** not ready to implement. There is 1 Blocker: the change list leaves out three files that the design's own verification step needs updated. There are also 6 Majors and 7 Nits.

**What checks out:**
- **Numbers:** D-093, TC-721 and v0.55.0 are the next free values. Nothing else in the repository claims them.
- **Count arithmetic:** 564 `func Test` in `dashboard/` + 22 in `pustaka/main_test.go` + 1 = 587.
- **Count locations:** `qa-test-plan.html:19,79`, `toc/product.js:162,168` and `decision-log.html:18` / `toc/product.js:129` are correct.
- **Line references:** most `site.js` and `main.go` references are exact.
- **Test helpers and font:** `writeServeFixture` and `testHandler` exist with the shapes §3.5 assumes, and the preloaded font file exists.
- **Page shape:** every docs page has `<div class="shell">` as the first element of `<body>`, so `body > .shell:first-child` will match.

**Not checked:** I did not open `deploy/stacks/docs.env.example`. It matches the reviewer deny-list (`*.env.*`), so the claims about its `PUSTAKA_AUTH=` line are not checked.

---

### Change list leaves out three files that the verification step needs updated - Severity: Blocker
- Affected scope: §3.5 "Counts", §3.7, §6 Change list, §7 Verification (`deploy/check-workspace.sh`)
- Finding: §7 runs `deploy/check-workspace.sh`, but that script hard-codes the counts this change moves:
  - `deploy/check-workspace.sh:114-118` requires the words "five hundred and eighty-six" and `go_tests = 586`.
  - `:129-130` require "seven hundred and twenty".
  - `:131` requires "Ninety-one entries".
  - `:147-149` compare `README.md` with the computed values. `README.md:12` says "Product version 0.54.0", `:137` says "586 Go tests" and `:139` says "720 registered test cases".
  - §3.5 also says "The automation map's `pustaka (go test)` range gains TC-721" but never names the file. That map, the "next free TC" line and the totals live in `.claude/rules/qa-test-plan.md`.

  None of these three files (`deploy/check-workspace.sh`, `README.md`, `.claude/rules/qa-test-plan.md`) is in §6. As written, either the implementer edits files outside the write allowlist, or the verification step fails. Earlier designs list these same edits explicitly (`docs/designs/design-iterate.md:355`, `docs/designs/landing-bilingual.md:324`).
- Recommendation: add three rows to §6, with exact before and after values:
  - `deploy/check-workspace.sh:114,115,118`: "five hundred and eighty-six" becomes "five hundred and eighty-seven", and 586 becomes 587. `:129-130`: "seven hundred and twenty" becomes "seven hundred and twenty-one". `:131`: "Ninety-one entries" becomes "Ninety-two entries".
  - `README.md:12,137,139`: 0.54.0 becomes 0.55.0, "586 Go tests: 564 … and 22" becomes "587 … and 23", and 720 becomes 721.
  - `.claude/rules/qa-test-plan.md`: the highest-ID line becomes TC-721 with TC-722 next free; the four counts change; the `pustaka (go test)` map row becomes TC-087…TC-108, TC-721; the totals line becomes 721 rows / 23 in `pustaka/` / 587; "Ninety-one entries" changes.

  Then state the expected `check-workspace.sh` output.

### The code itself is taken from an unpinned copy outside the repository - Severity: Major
- Affected scope: §0 Glossary, §3 preamble, §3.2.4, §3.2.5, §3.4, §3.5 ("This is upstream's test verbatim"), §6 row 3
- Finding: the change list names `reAssetBase`, `headMarker` and `headTheme`, but the design never gives:
  - the regex for `reAssetBase`, including which quote styles and characters it accepts for `<base>`;
  - the exact bytes injected, including whether the theme code is wrapped in `<script>…</script>`, plus the order, separators and newlines between marker, script and preload;
  - the CSS for `.pk-progress`, `.pk-progress.run/.done` and `.pk-enter`, and the `transition-delay` reset after a reveal;
  - the body of `TestHeadInjection`.

  All of this is deferred to "upstream v0.9.0" in `../pustaka`, which is outside the tree and pinned to no commit. A reviewer cannot check it. An implementer without that checkout has to invent it. Upstream can also move under the label. Two engineers would produce different regexes and different snippet bytes, and the TC-721 assertions ("exactly once", "before `</head>`") depend on those bytes.
- Recommendation: pin upstream to a commit SHA, and put the exact Go in the design: the `injectHead` body, the `reAssetBase` pattern and the two constants. Give the new CSS rules' values: height, colour, z-index, and the `done` fade duration. Either put the test body in the design or say it is copied from `<sha>:main_test.go` lines a–b.

### `ServeContent` with the file's modification time will keep old, un-injected pages in browser caches - Severity: Major
- Affected scope: §3.4 (`http.ServeContent(w, r, full, fi.ModTime(), bytes.NewReader(out))`)
- Finding: HTML is served `Cache-Control: no-cache` (`pustaka/main.go:577`), so browsers revalidate with `If-Modified-Since`. Passing `fi.ModTime()` means that after the deploy, any page whose file did not change answers 304. A returning reader then keeps the cached HTML that has no snippet, and the dark flash survives for them until that file changes. The design neither decides nor tests this.
- Recommendation: pick one approach. Either pass `time.Time{}` so `ServeContent` skips `Last-Modified` and conditional handling for injected pages, or set an ETag derived from the file's modification time plus a snippet version. Add the matching assertion to TC-721: a conditional request with the file's modification time must not answer 304 without the marker.

### No phases, no order for the production step, no exit conditions - Severity: Major
- Affected scope: §3.6, §6 (untracked row), §7 "After the deploy"
- Finding: the code change, the decision record that says the docs are public (D-093) and an untracked edit to `prod.docs.env` on two VMs are one undivided block. The design does not say:
  - whether the env edit happens before or after the tag deploy;
  - whether the two hosts are done one after the other or together;
  - what to do if one host's container does not come up;
  - how to roll back: the claimed one-line revert is never written out as a step.

  The D-093 text could also ship while a host still gates the docs.
- Recommendation: split the work into phases, each with an exit command:
  1. Code, tests and registers: exit when `go test` and `check-workspace.sh` are green.
  2. Release tag.
  3. Host 1: edit the env file, then deploy. Exit when the `/info` check shows no `auth` key.
  4. Host 2: the same.

  Write the rollback as commands, starting with `PUSTAKA_AUTH=1`.

### Several changes have no verification, and one check is not concrete - Severity: Major
- Affected scope: §7, and §3.5 "manual-check paragraph"
- Finding:
  - **Headless Chrome step:** no command or script, no way stated to "delay `site.js` and `toc*` by 2 s", and only the geometry is asserted.
  - **No checks at all:** G2 (no dark flash: body background on first paint while `data-theme=dark`), G3 (parts fetched in parallel and run in order), G4 (durations, ease-out, reduced motion), the progress bar's 150 ms threshold, the drawer's 180/280 ms timings and the `.rv` above-the-fold behaviour.
  - **Vague URL:** `/docs/guide/…` is not a URL.
  - **No expected outputs:** "`go test` passes" never names `TestHeadInjection`, and no expected count is given.
- Recommendation:
  - Give the headless script, or the DevTools throttling recipe, with the values asserted per goal. For example, mid-load `getComputedStyle(body).backgroundColor` must be the dark `--paper`, and the part `<script>` tags must have overlapping download timings.
  - Name a concrete page, such as `/docs/guide/getting-started.html`.
  - Add `go test -run TestHeadInjection -v ./...` with the expected `PASS`.

### Published docs that describe the docs login are left stale - Severity: Major
- Affected scope: §3.6, §6
- Finding: once the change ships, these files still describe the login as active:
  - `pustaka/docs/infra/environments.html:166` ("The docs login and its cookie secret");
  - `pustaka/docs/infra/environments.html:181-187` (generate `PUSTAKA_AUTH_SECRET`, or "every deploy … signs everyone out");
  - `deploy/STACKS.md:140,150`.

  §3.6 only *appends* to `compose.prod.yaml:214-218` and leaves "It guards everything including the docs home" in place, so the comment contradicts itself.
- Recommendation: list exact replacement text for `compose.prod.yaml:214-218`, `environments.html:166,181-187` and `STACKS.md:140,150`. Or state explicitly that they are out of scope, and why.

### The register and decision-log entries have no content specified - Severity: Major
- Affected scope: §3.5 (register row, `h3`, manual check), §3.7 (D-093 row, changelog)
- Finding: decision-log rows have a fixed structure: a bold decision sentence, Context, Decision, Residual, and an evidence column (see `decision-log.html:1982-2004`). The design gives D-093 only a title, with no evidence link, no Residual (for example, the leftover credentials kept on disk) and no date. For the QA plan, these are all left to the implementer:
  - the TC-721 row's Scenario text and `data-tags`;
  - the `h3` id, title and position (near `#pk-prefix`, `qa-test-plan.html:1872`, or at the end);
  - which of the twelve messages fall under "Positive" and which under "Negative".

  The v0.55.0 changelog body is also unspecified.
- Recommendation: write out the D-093 row text, with evidence pointing to `docs/designs/pustaka-loading-and-motion.md` and to the TC-721 anchor. Give the register row verbatim, the `h3` id and its position, and the changelog bullet list.

### Selector count is wrong - Severity: Nit
- Affected scope: §3.1.4 ("13 selectors, `site.css:400-415`")
- Finding: `site.css` contains 8 `body.layout-landing` selectors (lines 400, 401, 402, 409, 410, 411, 412, 413). 13 looks like the upstream count.
- Recommendation: change it to 8 and list the lines.

### Line references that do not match the fork - Severity: Nit
- Affected scope: §1.1 item 1, §3.3 reduced motion, §3.4, §5 R2
- Finding:
  - The 232 px rule is at `site.css:611-614`, not `633-636`.
  - The partial endpoint is `main.go:512-551`, not `503-550`.
  - The file read is `main.go:526`, not `523`.
  - The gzip middleware is `405-422`.
  - The fork's reduced-motion block is `site.css:654-658`; the design gives only upstream's line numbers.
- Recommendation: replace these with the fork's line numbers.

### Auth test count and range disagree - Severity: Nit
- Affected scope: §2 N2 ("the 18 auth tests (TC-090…TC-108)")
- Finding: TC-090…TC-108 is 19 IDs, and the range includes non-auth tests (`TestSitePrefix`, `TestTrimSitePrefix`, `TestSiteMeta`).
- Recommendation: say "all 22 pustaka tests (TC-087…TC-108) keep passing", or list the auth tests by name.

### Press-state transition is ambiguous for `.btn` and `.pn a` - Severity: Nit
- Affected scope: §3.3 "Press"
- Finding: `.btn` (`site.css:323`) and `.pn a` (`site.css:276`) already transition `transform .2s var(--ease)`. The design does not say whether "transform … at 160 ms ease-out" *replaces* that or is added as a second entry.
- Recommendation: give the final `transition` value for each of the five selectors.

### How to read the file is left open - Severity: Nit
- Affected scope: §3.4 "It reads the file"
- Finding: the handler already has `f` open (`main.go:568`). Using `io.ReadAll(f)` or `os.ReadFile(full)` changes what can fail and what the 500 path covers.
- Recommendation: name the call. Use `io.ReadAll(f)` to reuse the open handle.

### Device-mockup claim does not match the fork - Severity: Nit
- Affected scope: §3 preamble ("GAOS does not carry" upstream's device-mockup rules)
- Finding: `site.css:563-565` contains `.devices` rules.
- Recommendation: state exactly which upstream hunks are excluded, so the "copy and re-apply" step can be decided.

### N1 wording contradicts the change list - Severity: Nit
- Affected scope: §2 N1 ("Not one of the docs pages … is edited")
- Finding: §6 edits `changelog.html`, `decision-log.html` and `qa-test-plan.html`.
- Recommendation: reword it as "no page's `<head>`, chrome or structure is edited for the rendering fix; only register and release content changes."

Counts: B=1 M=6 N=7
