---
paths:
  - "dashboard/**"
  - "pustaka/**"
  - "deploy/**"
---

# The QA register

The rules for `pustaka/docs/product/qa-test-plan.html` — the register that says
what runs, what it asserts, and in what order. Read this before you create,
change or delete a test or an automation.

**This file is scoped.** It loads when you work on a matching file, not at every
session start. The always-on layer is `.claude/CLAUDE.md`; this is the detail.

## The one rule

**When you create, change or delete a test or an automation, update
`pustaka/docs/product/qa-test-plan.html` in the same change.** Not in a
follow-up, not "later" — the same commit. A test that exists in code and not in
the register is a test nobody knows to run, and this project has no CI to notice
the difference: `D-025` makes the product owner the runner for every check, and
that page is the only thing telling them what to run.

### What triggers this

Any change to:

- `dashboard/*_test.go`, `pustaka/*_test.go` — Go unit tests
- `deploy/check-stacks.sh`, `deploy/rebuild-restart.sh`,
  `deploy/acceptance/*.sh`, `deploy/check-workspace.sh`, anything else in
  `deploy/` that asserts or verifies
- `pustaka` engine checks reachable through `go run . check ./docs`
- any new script, hook, scheduled job or CI workflow that runs a check

Adding a case, removing one, renaming what it covers, or changing what it
asserts all count. Only pure refactors that leave the observable behaviour of
every check identical are exempt.

### What to update, exactly

1. **The register** — `<tbody id="tc-index">` under `<h2 id="register">`. One
   row per case, columns `ID · Automation · Kind · Scenario · Status`:

   ```html
   <tr data-tags="short keywords for the filter, incl. positive|negative"><td>TC-0nn</td><td>the script or package</td><td>Positive|Negative</td><td>one sentence, what is being asserted</td><td>Implemented|Not implemented|Retired</td></tr>
   ```

   IDs are sequential and never reused. **The highest in use is `TC-750`;
   next free number is `TC-751`.** If a case is retired, strike the row, set its
   Status to `Retired` and say why on the row, naming the case that replaces it
   — do not delete it and do not renumber anything after it. `TC-076` is the
   worked example: BR-07 made planning an empty record illegal, so the case
   that asserted the opposite was struck and `TC-160` took over.

   A **forward specification** is a row with Status `Not implemented`: an
   asserted value written before the code that produces it, so that a later
   phase has a written target to pass. It is not a plan and not a wish — it is
   a fixed value.

2. **The scenario prose** — an `h3` under `<h2 id="implemented">` for anything
   that runs today, or under `<h2 id="not-implemented">` for a forward
   specification. Two house rules there, both load-bearing:
   - Give positive *and* negative cases. The register is built around the pair,
     and a check with no stated failure mode has not been thought through.
   - **Quote the exact `ok` / `FAIL` string the script prints.** The page says
     every message is quoted rather than paraphrased. Read the script and copy
     it; do not reconstruct it from memory. For a Go test the equivalent is its
     `t.Fatalf` / `t.Errorf` message — same rule, read it from the file.

3. **The counts, which must agree.** Changing the number of cases means
   changing all four or none. They are currently all "seven hundred and fifty", so
   `grep -rn "six hundred and" pustaka/docs/product/qa-test-plan.html pustaka/docs/assets/toc/product.js`
   finds every one. Retired rows still count: the register is every ID ever
   issued, and `TC-076` is one of the seven hundred and fifty. Older
   changelog notes keep their own numbers; they are release records and stay as
   written:
   - the lede in `pustaka/docs/product/qa-test-plan.html`
   - the register intro on the same page ("All seven hundred and fifty cases…")
   - the `qa-test-plan` `desc` in `pustaka/docs/assets/toc/product.js`
   - that entry's `register` section `text` in the same file — which also carries
     the Go unit-test count, currently "six hundred and fifteen"

   The same pages carry four more counts that move together with the registers:
   "Fifty-eight rules" in `pustaka/docs/product/brd.html`, "Ninety-four entries"
   in `pustaka/docs/product/decision-log.html`, and "Forty decisions" and
   "All forty in one place" in `pustaka/docs/product/open-questions.html`.
   The published ToC mirrors all of them in `pustaka/docs/assets/toc/product.js`.

   Do **not** change the older counts in `pustaka/docs/changelog.html` — the
   "fifty-three" in the v0.3.0 note, the "one hundred and eight" in v0.6.0 and
   v0.8.0, and every later release's figures. Those are release notes, and each
   records what was true at that release.

4. **Release hygiene**, if the change is user-visible: add to
   `pustaka/docs/changelog.html` and bump `product.semver` in
   `pustaka/docs/assets/toc.js`. Those two are one change — the changelog page
   says so itself.

### The register covers everything, so keep it that way

The map below is derived from the register's own `Automation` column, not from
memory. It is a range→area map; the file-level mapping inside a range is the
register's job, and the register is the source of truth. To read it:

```bash
grep -oE '<td>TC-0[0-9][0-9]</td><td>[^<]*' pustaka/docs/product/qa-test-plan.html \
  | sed 's/<td>//g' | grep -E 'TC-(0[5-9][0-9]|1[0-9][0-9])'
```

| Automation | Status | Cases |
|---|---|---|
| `check-stacks.sh` | Implemented | TC-001…TC-016, TC-127, TC-128, TC-150, TC-151, TC-310, TC-435, TC-436, TC-451, TC-452, TC-499, TC-523, TC-599, TC-616, TC-622, TC-623, TC-687 |
| `rebuild-restart.sh` | Implemented | TC-017…TC-047, TC-147, TC-148, TC-152…TC-155, TC-438, TC-600, TC-688, TC-733 |
| `scratch.sh` | Implemented | TC-129, TC-130 |
| `live-aim-run.sh` | Implemented | TC-300…TC-303 |
| `check-workspace.sh` | Implemented | TC-357, TC-358, TC-380, TC-381, TC-437, TC-510 |
| `seed.sh` | Implemented | TC-411 |
| `dashboard (go test)` | Implemented | TC-050, TC-051, TC-054…TC-075, TC-077…TC-086, TC-109…TC-126, TC-131…TC-146, TC-149, TC-156…TC-179, TC-185, TC-191, TC-193…TC-219, TC-227…TC-230, TC-247…TC-261, TC-266, TC-267, TC-274…TC-299, TC-304…TC-309, TC-311…TC-356, TC-359…TC-379, TC-382…TC-410, TC-412…TC-434, TC-439…TC-450, TC-453…TC-498, TC-500…TC-509, TC-511, TC-512, TC-513…TC-522, TC-524…TC-598, TC-601…TC-615, TC-617…TC-621, TC-624…TC-686, TC-689…TC-720, TC-722…TC-732, TC-734…TC-750 |
| `pustaka (go test)` | Implemented | TC-087…TC-108, TC-721 |
| `dashboard (go test)` | Retired | TC-076, TC-180…TC-184, TC-186…TC-190, TC-192, TC-220…TC-226, TC-231…TC-242, TC-262…TC-265 |
| `kill-mid-task.sh` | Retired | TC-243…TC-246 |
| `live-cli-run.sh` | Retired | TC-268…TC-273 |
| Unsatisfiable task | Not implemented | TC-048, TC-049 |
| Second run | Not implemented | TC-052, TC-053 |

**750 rows in total. 592 in `dashboard/` and 23 in `pustaka/`** — 615 Go tests,
and **one register row per `func Test…`**, because that is the unit `go test
-run` can select. A new top-level `func Test…` always gets its own row. The
store conformance suite is the exception: it is one `func Test…` per
implementation over a shared table, so a new case in `storeContract` does **not**
get its own `TC` — it is covered by `TC-124` and `TC-125`, whose scenario prose
describes the table.

The retired rows record what was, and they stay: a retired row is not deleted
and nothing after it is renumbered. The same is true of the two scripts they
name, `deploy/kill-mid-task.sh` and `deploy/acceptance/live-cli-run.sh` — both
were removed with the CLI execution layer in v0.29.0, and both are named in the
register only as the record of what was retired.

M14.7 (v0.29.0) retired the CLI execution layer and the floor UI: forty-four
rows were struck as `Retired` with their successor named. The rest of that
range survived, re-based on the fake gateway (`gateway_fixture_test.go`) and
keeping their IDs.

The rule the forward specifications followed still holds for the next ones:
they were written before the code on purpose, and when a phase lands, the test
it writes **takes the forward-spec ID that describes it** — flip the row to
`Implemented`, name the `func Test…` on it, move the `h3` up under
`#implemented`, and replace the asserted value with the test's exact
`t.Fatalf`. A test that matches no forward spec gets the next free ID as
usual; a forward spec the code makes obsolete is retired, never reused.

The simulator is test-only (`sim_test.go`): it still feeds the fold and catalog
tests, and is not a `func Test…` itself.


### Before you call it done

```bash
cd pustaka && go run . check ./docs   # structure + registry consistency
cd dashboard && go test ./...
cd ../pustaka && go test ./...
./deploy/check-stacks.sh              # the stack invariants
./deploy/check-workspace.sh           # the agent workspace invariants
```

`check` will catch a broken link or an unregistered page. It will **not** catch a
count that drifted or a quoted message that is wrong — those are on you, and
`check-workspace.sh` catches the counts.


