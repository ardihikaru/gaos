# Zoom-out review: `docs/designs/pustaka-loading-and-motion.md`

I am the zoom-out reviewer and asked one question: is this the smallest change that removes the defect? The loading fix in §1.1 is mostly the right size. The motion work and the public switch are separate changes riding along with it. The test-count edits will fail the design's own verification step in §7. The new server code can keep sending old cached pages after the upgrade.

### The count changes break `check-workspace.sh`, and three files are missing from the change list - Severity: Major
- Affected scope: §3.5 "Counts", §3.7, §6 change list, §7 verification.
- Simplicity rationale: The design changes "five hundred and eighty-six" to "five hundred and eighty-seven" and "Ninety-one entries" to "Ninety-two". It does not list the files that pin the old values:
  - `deploy/check-workspace.sh:114-118` requires the string "five hundred and eighty-six" in `toc/product.js` and exactly `586` Go tests on disk.
  - `deploy/check-workspace.sh:131` requires "Ninety-one entries" in `decision-log.html`.
  - `deploy/check-workspace.sh:147-149` compare `README.md` with computed values. `README.md:12` says "Product version 0.54.0", `README.md:137` says "586 Go tests" and `README.md:139` says "720 registered test cases". All three go stale.
  - The "automation map" that §3.5 says "gains TC-721" exists only in the rule file `.claude/rules/qa-test-plan.md`. That file also pins "TC-720", "720 rows" and "586". The harness attached it to my context on its own; I did not open it. `qa-test-plan.html` has no such map: `TC-087` appears only at `:178` and `:1767-1769`.

  If the change list is followed as written, the `deploy/check-workspace.sh` step in §7 fails. The "seven hundred and twenty" checks at `:129-130` would still pass, but only because they match a substring of "seven hundred and twenty-one". So they no longer check anything.
- Recommendation: Add `deploy/check-workspace.sh` (lines 114-115 and 131), `README.md` (lines 12, 137 and 139) and `.claude/rules/qa-test-plan.md` to §6. That makes 14 tracked files, not 11. Change the two prose checks so they match the full phrase.

### Two sources of truth: "copied from upstream", but specified to the millisecond, with upstream line numbers - Severity: Major
- Affected scope: §3 preamble, §3.1.4, §1.1 item 1, §3.3 "Reduced motion", §3.5 ("upstream's test verbatim").
- Simplicity rationale: §3 says the three files are "copied from upstream v0.9.0 and then re-applied". It then specifies every duration, easing curve and selector. When the copied file and the written spec disagree, the design does not say which one wins. Several citations seem to come from upstream (outside this repo, so I could not check that directly) and do not match the fork:
  - §3.1.4 says "13 selectors, `site.css:400-415`". The fork has 8 `body.layout-landing` selectors, at lines 400, 401, 402, 409, 410, 411, 412 and 413.
  - §1.1 cites the 232 px column as `site.css:633-636`. In the fork it is at `:611-614`.
  - The reduced-motion block is at `:654-658` in the fork, not "676-680".

  `deploy/Dockerfile.pustaka:3-6` says "an upstream update should be a copy, not a merge". The project's own approach is therefore a pinned copy, not a re-specification.
- Recommendation: Write the design as an upstream sync. Pin the upstream tag or commit, name the files copied, and list only the fork deltas to re-apply (`sitePrefix`, no device-mockup rules). Keep §3.1–§3.3 as a short summary of intent, re-cite against the fork, and state that the copy is authoritative.

### The injected page is validated by the file's mtime, so cached pages without the snippet survive the upgrade - Severity: Major
- Affected scope: §3.4, `http.ServeContent(w, r, full, fi.ModTime(), bytes.NewReader(out))`.
- Simplicity rationale: The response body now depends on the binary (the snippet) as well as the file. `Last-Modified` still comes only from the file. HTML is served `no-cache` (`pustaka/main.go:576-577`), so a returning browser revalidates with `If-Modified-Since`. For any page whose file did not change in this release, it gets a 304 and keeps the cached copy without the snippet. The dark-flash fix would then not reach returning readers until each page is edited. The same happens on every later change to the snippet. `Dockerfile.pustaka:15` copies the docs into the image, and Docker `COPY` keeps the source file's mtime, so unchanged pages keep their old timestamps.
- Recommendation: Pass a zero `time.Time` for injected pages, which stops `Last-Modified` being sent. Or use the later of the file mtime and the process start time. Add one assertion to TC-721 for it.

### The motion half has no defect behind it and no check in §7 - Severity: Major
- Affected scope: §1.2, G4, §3.2 items 3–5, §3.3 (press, drawer, active marker, stagger, progress bar).
- Simplicity rationale: §1.1 states a measured defect. §1.2 lists things that are missing ("no press state", "same 280 ms curve", "nothing tells the reader when a navigation is slow") with no reader-facing failure. The new pieces are:
  - a progress-bar element with a 150 ms arm timer and a 6 s scale animation;
  - per-batch stagger delays;
  - press scaling on five control classes;
  - asymmetric drawer timing;
  - a changed `scan` keyframe.

  G4's acceptance standard is a file under `.claude/`, which this review may not open. §7 has no step that checks any of the motion items. The `.rv` rework (§3.1.6, §3.2.2) also changes how every page behaves: blocks below the fold are visible while the page loads, then hidden once `ready` is set. That is a new visible change, introduced to support the motion work.
- Recommendation: Keep in this design only what §1.1 needs: grid areas, the reserved header, the landing selector, theme without a transition, `.rv` not hidden before `ready`, and parallel parts. Either move the motion polish to its own change, or justify it explicitly as part of a whole-file upstream sync (see the second finding) and add a manual check for it in §7.

### The font preload is an optimisation with no stated problem - Severity: Nit
- Affected scope: §3.4 (preload, `reAssetBase`, the base derivation), §3.5 check 1 (the three base values).
- Simplicity rationale: §1.1 names no font-swap or font-shift defect. The preload is the only part of `injectHead` that parses the page's markup, and it causes the base-path cases in the test.
- Recommendation: Drop it, or add a measured problem to §1.1. Without it, `injectHead` is the marker plus a fixed string.

### The public switch is a separate decision bundled into a UI release - Severity: Nit
- Affected scope: §1.3, G5, §3.6, D-093.
- Simplicity rationale: Switching the login off is a one-line edit to the untracked env file on each VM, plus a decision-log row. It needs no v0.55.0 code. Bundling it means the security lens has to review CSS timing, and the loading fix cannot be approved without the public decision. R4's safety claim rests on D-085's review of private strings. `check-workspace.sh` section 7 also skips silently when no forbidden-strings list is present (`deploy/check-workspace.sh:361`).
- Recommendation: Record D-093 and the env-file edit as their own small change. Run section 7 with the list present as an explicit pre-flight step before the switch.

### The design names the prod VMs - Severity: Nit
- Affected scope: §3.6 (the two host names and checkout paths).
- Simplicity rationale: This design will be committed to a repository that is going public. D-085 forbids naming an operator's private instance. I cannot check the forbidden-strings list, because `deploy/local/` is off limits to me.
- Recommendation: Say "both prod VMs" and leave host names in the operator's private notes. Ask the security lens to confirm.

### The auth test range is wrong - Severity: Nit
- Affected scope: N2, "the 18 auth tests (TC-090…TC-108)".
- Simplicity rationale: TC-090 to TC-108 is 19 IDs. TC-104 is a checker test, not an auth test (`qa-test-plan.html:1767`). `pustaka/main_test.go` has 22 `func Test` in total.
- Recommendation: Write "the auth tests in `pustaka/main_test.go`" or list them exactly.

### A CSS-only dark default was never considered - Severity: Nit
- Affected scope: §4.
- Simplicity rationale: `@media (prefers-color-scheme: dark)` would fix the flash for readers whose system setting is dark. It needs no server code and also works in static mode. Only readers whose saved choice differs from their system setting would still see it. The dark rules are spread over several selectors (`site.css:34, 92, 225, 325, 475`), which is probably why it was rejected. The design should say so.
- Recommendation: Add it as a rejected alternative with that reason.

### R2's note on in-memory pages is unused - Severity: Nit
- Affected scope: §5 R2.
- Simplicity rationale: R2 says `--prod` already holds pages in memory, but the design reads each page from disk on every request.
- Recommendation: Either serve from the in-memory copy or remove the sentence.

**Sound sections:**
- Parallel part loading with `async=false` (§3.2.1) is the smallest fix for G3.
- Named grid areas and the reserved header (§3.1.1–2) fix the geometry with no page edits.
- Rejecting server-side rendering of the chrome (§4) is fairly argued.
- The Go test arithmetic is correct: 564 + 22 + 1 = 587 (`pustaka/main_test.go` has 22 `func Test`).

Counts: B=0 M=4 N=6
