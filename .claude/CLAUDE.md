# GAOS — project rules

## Tests and automation are not done until the QA plan says so

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
  `deploy/acceptance/*.sh`, anything else in `deploy/` that asserts or verifies
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

   IDs are sequential and never reused. **The highest in use is `TC-313`; the
   next free number is `TC-314`.** If a case is retired, strike the row, set its
   Status to `Retired` and say why on the row, naming the case that replaces it —
   do not delete it and do not renumber anything after it. `TC-076` is the worked
   example: BR-07 made planning an empty record illegal, so the case that asserted
   the opposite was struck and `TC-160` took over.

2. **The scenario prose** — an `h3` under `<h2 id="implemented">` for anything
   that runs today, or under `<h2 id="not-implemented">` for a forward spec. Two
   house rules there, both load-bearing:
   - Give positive *and* negative cases. The register is built around the pair,
     and a check with no stated failure mode has not been thought through.
   - **Quote the exact `ok` / `FAIL` string the script prints.** The page says
     every message is quoted rather than paraphrased. Read the script and copy
     it; do not reconstruct it from memory. For a Go test the equivalent is its
     `t.Fatalf` / `t.Errorf` message — same rule, read it from the file.

3. **The four counts, which must agree.** Changing the number of cases means
   changing all four or none. They are currently all "three hundred and thirteen", so
   `grep -rn "three hundred and" pustaka/docs/product/qa-test-plan.html pustaka/docs/assets/toc/product.js`
   finds every one. Retired rows still count: the register is every ID ever issued,
   and `TC-076` is one of the three hundred and thirteen. Older changelog notes keep their own
   numbers; they are release records and stay as written:
   - the lede in `pustaka/docs/product/qa-test-plan.html`
   - the register intro on the same page ("All three hundred and thirteen cases…")
   - the `qa-test-plan` `desc` in `pustaka/docs/assets/toc/product.js`
   - that entry's `register` section `text` in the same file — which also carries
     the Go unit-test count, currently "two hundred and twenty-eight"

   Do **not** change the older counts in `pustaka/docs/changelog.html` — the
   "fifty-three" in the v0.3.0 note, the "one hundred and eight" in v0.6.0 and
   v0.8.0, the "one hundred and twelve" in v0.9.0, the "one hundred and thirty"
   in v0.10.0, the "one hundred and forty-eight" in v0.11.0, the "two hundred"
   in v0.15.0, the "three hundred and ten" in v0.23.0, and the "three hundred and twelve" in v0.24.0. Those are release notes, and each records what was true at that
   release.

4. **Release hygiene**, if the change is user-visible: add to
   `pustaka/docs/changelog.html` and bump `product.semver` in
   `pustaka/docs/assets/toc.js`. Those two are one change — the changelog page
   says so itself.

### The register now covers everything, so keep it that way

`TC-001`…`TC-053` are `deploy/` and the three BRD acceptance tests — two of
which are still forward specifications, while `TC-050`/`TC-051`, the fold test,
now name the Go tests that run it. `TC-054`…`TC-086` are 33 of the `dashboard/`
unit tests, `TC-087`…`TC-108` the 22 `pustaka/` ones, `TC-109`…`TC-112` the four
schema guards in `dashboard/schema_test.go`, `TC-113`…`TC-126` the fourteen in
`dashboard/arcade_test.go` and `dashboard/store_conformance_test.go`,
`TC-127`…`TC-130` the `check-stacks.sh` store checks plus
`deploy/schema/scratch.sh`, `TC-131`…`TC-146` the fold, the reducer, the state
machine and the closed catalog in `dashboard/fold_test.go`,
`dashboard/state_test.go` and `dashboard/events_test.go`, `TC-147`/`TC-148`
the deploy gate's Go-toolchain fallback, and `TC-149` the projection regression
in `dashboard/main_test.go`. `TC-150`/`TC-151` are the licence assertions in
`check-stacks.sh`, `TC-152`…`TC-155` the tag-and-worktree deploy path in
`rebuild-restart.sh`, and `TC-156`…`TC-160` the readiness rubric and the planning
floor in `dashboard/readiness_test.go`. `TC-161`…`TC-200` are the pre-dispatch
egress pass, the handoff parser, the version pin, the CLI adapter and the
dispatch route in `dashboard/egress_test.go`, `handoff_test.go`, `smoke_test.go`,
`adapter_test.go` and `dispatch_test.go`. `TC-201`…`TC-242` are the persisted task
graph, the task status machine, the scheduler, the lease and the detached runner
in `dashboard/graph_test.go`, `taskstate_test.go`, `scheduler_test.go`,
`lease_test.go`, `runner_test.go` and `reconcile_test.go`; `TC-243`…`TC-246` are
`deploy/kill-mid-task.sh`; `TC-247`…`TC-258` are the attempt record and the
frozen context slice in `dashboard/taskrun_test.go`, `TC-259`…`TC-267` the task
packet and the handoff contract in `dashboard/packet_test.go`, and
`TC-268`…`TC-273` the live model call in `deploy/acceptance/live-cli-run.sh`,
and `TC-274` the schema-apply counts in `dashboard/store_conformance_test.go` — **one row per `func Test…`**,
because that is the unit `go test -run` can select. The register currently covers
228 Go tests: 206 in `dashboard/`, 22 in `pustaka/`. Adding a test means adding
its row and its `h3`; the gap that used to be here is closed and must not reopen.

`TC-275`…`TC-290` became Go tests in v0.24.0 (`aimanager_test.go`, `blueprint_test.go`; their
sections kept the `ni-aim` / `ni-blueprint` anchors), and `TC-311`/`TC-312` are the two Blueprint tests
no forward spec described. `TC-291`…`TC-295` became Go tests in v0.25.0 (`workspace_test.go`, section
`go-workspace`). `TC-296`…`TC-303` became tests in v0.27.0 (`turns_test.go`; `deploy/acceptance/live-aim-run.sh`
for 300–303). `TC-304`…`TC-310` are still **forward specifications** for milestone M14 (the ai-manager
gateway, the Blueprint, the board — D-055…D-062), all `Not implemented`, with their
prose under `<h2 id="not-implemented">` (`ni-aim`, `ni-blueprint`, `ni-workspace`,
`ni-live-aim`, `ni-board`). They were written before the code on purpose. When a
phase lands, the test it writes **takes the forward-spec ID that describes it** —
flip the row to `Implemented`, name the `func Test…` on it, move the `h3` up under
`#implemented`, and replace the asserted value with the test's exact `t.Fatalf`.
A test that matches no forward spec gets the next free ID as usual; a forward spec
the code makes obsolete is retired, never reused. M14.7 retires the CLI execution
layer: the rows that covered it (`TC-180`…`TC-192`, `TC-200`, `TC-220`…`TC-246`,
`TC-261`…`TC-273`, audited one by one) are struck as `Retired` with their
successor named, and the four counts do not change.

The store conformance suite is one `func Test…` per implementation over a shared
table, so a new case in `storeContract` does **not** get its own `TC` — it is
covered by `TC-124` and `TC-125`, whose scenario prose describes the table. A new
top-level `func Test…` always does.

### Before you call it done

```bash
cd pustaka && go run . check ./docs   # structure + registry consistency
cd dashboard && go test ./...
cd ../pustaka && go test ./...
```

`check` will catch a broken link or an unregistered page. It will **not** catch a
count that drifted or a quoted message that is wrong — those are on you.

## Related, and worth knowing before a large edit

- `pustaka/ai/AUTHORING_SPEC.md` is the contract for every docs page. Do not
  invent CSS classes; the component vocabulary is closed.
- The BRD, the decision log and the open questions are registers, not prose.
  Answering a question moves a row from one page to another and adds a `D-`
  number — it never deletes the question.
- The event catalog is closed (`BR-05`). A new event type lands in the data model
  document, `dashboard/events.go` and the fold together, or not at all.
