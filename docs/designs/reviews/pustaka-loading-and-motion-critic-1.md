# Goldfish critic review: `pustaka-loading-and-motion.md`

**Design reviewed:** `/home/ardi/devel/projects/ardihikaru/my-projects/gaos/docs/designs/pustaka-loading-and-motion.md`

## Strongest counter-argument to the design's thesis

The design claims three things: that the CSS placeholders give the final geometry at first paint, that "nothing moves", and that the design is ready to implement. Neither claim holds yet:

- **The plan fails its own verification step.** The change list leaves out three files whose hard-coded counts `deploy/check-workspace.sh` compares against. §7 runs that check, so it fails.
- **Content still moves.** The runtime still injects content near the top of 28 article pages. The single check proposed in §7 would not catch it.
- **The head snippet will not reach browsers that already have a page cached.** The server keeps answering their revalidation with `304`, keyed on the file's modification time.
- **Making the docs public is handled as an afterthought.** The dev tier, the published operating docs and the stated mitigation do not match the repository.

---

### The change list omits the files that `check-workspace.sh` checks the counts against - Severity: Blocker
- **Affected scope:** §3.5 "Counts", §3.7, §6 change list, §7 verification.
- **Finding and rationale:**
  - **The script pins the old values as literal strings:**
    - `deploy/check-workspace.sh:114-115` requires `"five hundred and eighty-six"` and `go_tests = 586`.
    - `deploy/check-workspace.sh:131` requires `"Ninety-one entries"`.
    - After §3.5 and §3.7, there are 587 tests on disk and the decision log says "Ninety-two". Both checks fail.
  - **`README.md` is checked against computed values** (`check-workspace.sh:147-149`). README must then say "587 Go tests", "721 registered test cases" and "Product version 0.55.0". Today it says 586, 720 and 0.54.0 (`README.md:12,137,139`). README is not in the change list.
  - **The QA-register rule file is not in the change list.** That file is `.claude/rules/qa-test-plan.md`; its content was attached by the harness, I did not open it. It holds:
    - "highest in use is TC-720";
    - "720 rows … 586 Go tests";
    - "Ninety-one entries";
    - the automation map that §3.5 says "gains TC-721".
  - The "automation map" in §3.5 is never located in a file, and nothing else in the change list carries it.
  - §7 runs `deploy/check-workspace.sh`, so the plan as written cannot pass its own verification. This also breaks README rule 3: every change must name its file.
- **Recommendation:**
  - Add three rows to §6:
    - `deploy/check-workspace.sh`: 586 becomes 587, "five hundred and eighty-seven", and "Ninety-two entries".
    - `README.md`, lines 12, 137 and 139.
    - The QA-register rule file: highest ID, row count, Go-test count, decision count and the `pustaka (go test)` range.
  - Write down the full list of count locations in one place.

### The head snippet does not reach cached pages, because `ServeContent` answers `304` by the file's mtime - Severity: Major
- **Affected scope:** §3.4 (`http.ServeContent(w, r, full, fi.ModTime(), bytes.NewReader(out))`).
- **Finding and rationale:**
  - **How the stale copy survives:**
    - HTML is served `Cache-Control: no-cache` (`pustaka/main.go:576-577`), so browsers revalidate with `If-Modified-Since`.
    - `ServeContent` compares that against `fi.ModTime()`, which is the source file's mtime.
    - The image copies the docs with mtimes preserved (`deploy/Dockerfile.pustaka:9,15`).
    - So every page whose file did not change in v0.55.0 answers `304`, and the returning reader keeps the cached copy without the snippet.
  - **What follows:**
    - G2 (no dark flash) is silently defeated for exactly the returning readers who have a saved theme.
    - Any later change to `injectHead` has the same problem: the output depends on the binary, but the validator depends only on the file.
  - TC-721 does not exercise conditional requests, so the test suite will not notice.
- **Recommendation:** Pick one fix and state it.
  - Pass `time.Time{}` for injected pages. `ServeContent` then sends no `Last-Modified` and never answers `304`.
  - Or set a strong `ETag` from a hash of the injected bytes.

  Then add a TC-721 sub-case: an `If-Modified-Since` request equal to the file's mtime must still get a body that carries the marker.

### G1 ("nothing moves") is false, and the §7 metric would not show it - Severity: Major
- **Affected scope:** §2 G1, §3.1, §7 "The mid-load box of `main.doc` must equal the final one".
- **Finding and rationale:**
  - **The runtime still inserts content inside the article after first paint:**
    - It fills `.page-meta` (`site.js:264-280`). On a page that has one, this is usually right under the lede, e.g. `decision-log.html:22`.
    - It appends `¶` anchors to every heading (`site.js:296-299`).
    - It appends `.pn` and `.foot` (`site.js:302-316`).
  - **The empty `.page-meta` is a small bordered box; filled, it is much taller.** It has padding, a border and a margin (`site.css:184`). Filled, it holds tags, hashtags and a `dl` (`site.css:186-195`), so everything below it moves down. 29 files carry `class="page-meta"`; 28 of them are content pages.
  - **§7's check measures the wrong thing.** It checks only `main.doc`'s box. That box keeps its x position and width, so an in-article shift is invisible to it. Its height changes anyway, because `.pn` and `.foot` are appended, so "equal box" is either always false or means x and width only. Two engineers would write different checks.
- **Recommendation:**
  - Narrow G1 to "the article's column and the header strip do not move".
  - Or reserve the `.page-meta` height in CSS with `.page-meta:not([data-rendered])` and a `min-height`, and list that as a change.
  - Define the §7 metric precisely: x, y and width of `main.doc`, plus the y of the first `h2`, mid-load against loaded.

### The motion goal G4 contradicts the design's own numbers, and its bar cannot be read by a reviewer - Severity: Major
- **Affected scope:** §2 G4, §3.1.6, §3.2.3, §3.2.4, §3.3.
- **Finding and rationale:**
  - **The design breaks its own "under 300 ms" bar:**
    - The `.rv` transition is "300 ms ease-out". That is not under 300 ms.
    - The stagger adds up to 300 ms of delay, so the last block finishes about 600 ms after the batch.
    - The progress bar runs a 6 s `scaleX` transition.
  - **"Nothing under reduced motion" is not met.** §3.3 never addresses the progress bar. The global reduced-motion rule (`site.css:655`) only shortens durations, so the bar still appears and jumps to `.8`.
  - **The bar itself cannot be checked here.** G4's acceptance bar is a file under `.claude/` (`STANDARDS.md` under the review-animations skill), which this lens is not allowed to open. The design must stand on its own (README rule 1).
- **Recommendation:**
  - Quote the specific motion rules inline.
  - Either bring `.rv` and the stagger under the bar, or state the exemptions explicitly (reveal, progress indicator).
  - Say what the progress bar does under reduced motion.

### Making the docs public is under-specified: the dev tier, stale operating docs, and live credentials left in the environment - Severity: Major
- **Affected scope:** §1.3, §2 G5, §3.6, §6.
- **Finding and rationale:**
  - **The dev tier is never addressed.** The dev stack runs its own docs service with `env_file: [./dev.docs.env]` (`deploy/stacks/compose.dev.yaml:100,105`). The design covers "both prods" only. Without a stated choice, dev stays gated by accident or is flipped ad hoc.
  - **The new `docs.env.example` comment** ("leave this empty on both prods") gives a dev operator no instruction.
  - **Published and committed operating text becomes false, and none of it is in the change list:**
    - `pustaka/docs/infra/environments.html:166,181-187` describe the docs login and its cookie secret as current practice.
    - `deploy/STACKS.md:140` and `deploy/Dockerfile.pustaka:20-25` say the same.
    - The comment at `compose.prod.yaml:214-218` says the layer "guards everything including the docs home". §3.6 only appends a sentence after it, so the comment ends up contradicting itself.
  - **Live credentials stay in a public container.** Leaving `PUSTAKA_AUTH_USER`, `PUSTAKA_AUTH_PASS` and `PUSTAKA_AUTH_SECRET` in the env file still injects them into a now-public service's environment. "Inert" describes the code, not where the credentials live.
- **Recommendation:**
  - Decide the dev tier explicitly.
  - Rewrite the compose comment rather than appending to it.
  - Add `environments.html`, `STACKS.md` and the Dockerfile comment to §6.
  - Comment out the three credential lines rather than leaving them active. Switching back on is then still a short change.

### R4's stated mitigation is not what the cited check does - Severity: Major
- **Affected scope:** §5 R4.
- **Finding and rationale:**
  - R4 says the docs "were reviewed for private strings by D-085, and `deploy/check-workspace.sh` section 7 enforces that".
  - **The check is narrower than that.** Section 7 (`check-workspace.sh:281-361`) only matches a git-ignored list of one operator's private-instance names. If that list is absent, it skips with `info`, so it passes.
  - **The docs being made public publish internal topology** that no such list covers, for example:
    - a WireGuard-side address (`infra/environments.html:199`);
    - VM names and tunnel ports (`brd.html:312,1602`).
  - **The design does not say whether the repository is already public.** If it is not, G5 publishes this material before the source does.
- **Recommendation:**
  - Restate R4 truthfully: the docs content becomes as public as the repository, and the operator accepts that the infrastructure detail in it is published.
  - State the order relative to the repository going public.
  - Defer the attack-path analysis to the security lens, which this design already marks as required.

### Citations and counts that do not match the repository - Severity: Nit
- **Affected scope:** §0, §1.1, §2 N2, §3.1.4, §3.3, §3.4, §3.7.
- **Finding and rationale:**
  - **Line numbers:**
    - The partial endpoint is `main.go:512-551`, not 503-550. Its `ReadFile` is at 526, not 523.
    - The 232 px column is `site.css:611-612`, not 633-636; 633-636 is inside the ≤560 px block.
    - The fork's reduced-motion block is `site.css:654-658`.
  - **Counts:**
    - The fork has **8** `body.layout-landing` selectors (`site.css:400-413`), not 13.
    - "18 auth tests (TC-090…TC-108)": that range has 19 IDs and includes non-auth cases TC-103 and TC-104 (`qa-test-plan.html:194-195`).
  - **Other facts:**
    - `PUSTAKA_AUTH` turns the layer on for `1/true/yes/on`, not only `1` (`main.go:662-663`).
    - `toc/product.js:129` reads "Ninety-one *decisions*", not "entries".
- **Recommendation:** Correct each citation and count.

### R2 claims pages are held in memory in `--prod`. They are not. - Severity: Nit
- **Affected scope:** §5 R2.
- **Finding and rationale:**
  - `rebuild()` keeps only the extracted records and the inverted index (`main.go:305-331`). It does not keep page bytes.
  - With this change, each view of the 654 KB page also stops streaming from the file: it reads the page into memory and builds a second copy in `injectHead`.
- **Recommendation:** State the real cost per request, or cache the injected bytes once in `--prod`; the image is immutable (`Dockerfile.pustaka:18`).

### Claims about upstream are unlabelled assumptions - Severity: Nit
- **Affected scope:** §0, §3 lead-in, §3.5 ("upstream's test verbatim").
- **Finding and rationale:** "Byte-identical", "differs only by prefix code" and "upstream v0.9.0" all refer to a sibling repository outside this tree. A reviewer cannot check them.
- **Recommendation:** Label them as assumptions (README rule 2), or give the commit hash and the exact `diff` command.

### N1 contradicts §3.7 - Severity: Nit
- **Affected scope:** §2 N1.
- **Finding and rationale:** "Not one of the docs pages … is edited", yet §3.5 and §3.7 edit `qa-test-plan.html`, `decision-log.html` and `changelog.html`.
- **Recommendation:** Reword N1 to "no structural or template change to any page".

### The progress-bar lifecycle is undecided - Severity: Nit
- **Affected scope:** §3.2.4.
- **Finding and rationale:**
  - A `popstate` can start a second `swap()` while the first is in flight (`site.js:607`). The design has one timer and one element, and says nothing about overlapping swaps.
  - It also does not say when `done` is cleared before the next `progStart()`.
- **Recommendation:** Specify a generation counter, or "restart on start". Also specify when `done` is reset.

### The placeholders become permanent if the runtime fails - Severity: Nit
- **Affected scope:** §3.1.2, §3.1.3, §5.
- **Finding and rationale:**
  - If every registry part fails, `FLAT` is empty and `HERE` is undefined, so `paintSidebar()` throws (`site.js:47,228`).
  - The fixed header strip and the sidebar skeleton then stay on screen forever.
  - Today the page degrades to readable but unstyled.
- **Recommendation:** Add this case to §5, and accept it explicitly or time the placeholders out.

### Public docs have no stated crawler stance - Severity: Nit
- **Affected scope:** §3.6.
- **Finding and rationale:** There is no `robots.txt` in `pustaka/docs/`. Once the login is off, every page, `/__pustaka/index.json` and every partial can be crawled.
- **Recommendation:** State whether search indexing is wanted.

### Upstream's login page loses its entrance animation - Severity: Nit
- **Affected scope:** §3.1.6, §2 N2.
- **Finding and rationale:** `_login.html:36,143` relies on `.rv` starting hidden. That page never gets `html.ready`, so its reveal becomes a no-op. This is harmless while auth is off, but N2 promises the login stays intact.
- **Recommendation:** Note it, or scope the change to `.rv` outside the login page.

### A middle-ground alternative was not considered - Severity: Nit
- **Affected scope:** §4.
- **Finding and rationale:**
  - Once the server rewrites `<head>`, it could also emit a static header and sidebar.
  - It already parses the registry (`loadRegistry`, `main.go:148`).
  - That would remove the duplicated geometry: CSS placeholders that must track JS-built chrome.
- **Recommendation:** Add it with a reason for rejecting it.

**Sections that hold up:**
- §1.1's runtime citations, from `site.js:28-36` through 157, 209 and 578, are accurate.
- The `async=false` ordering argument is sound.
- `writeServeFixture` and `testHandler` exist and fit TC-721 (`main_test.go:103,173`).
- The 564 + 22 + 1 = 587 arithmetic is correct.
- TC-721 is the next free ID.
- No BR in `brd.html` §9 governs the docs site; BR-55 covers the back office only.

Counts: B=1 M=5 N=9
