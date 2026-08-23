#!/usr/bin/env bash
# The first real model call, and the only script in this repository that spends
# money.
#
# Everything else here runs against dashboard/testdata/fakecli, a shell script
# that writes a fixture and exits. That is the right default and it proves the
# orchestrator, never the contract: a fake tool told to write a document writes
# it, whatever the packet said. What a fake cannot prove is the load-bearing
# assumption of the whole CLI approach -- that a REAL provider tool, handed
# GAOS's packet and nothing else, produces a document GAOS can read.
#
#   GAOS_LIVE_FAKE=1 ./deploy/acceptance/live-cli-run.sh   # the harness, free
#   GAOS_LIVE_OK=1   ./deploy/acceptance/live-cli-run.sh   # the real thing
#
# With neither it prints what it would do and stops. That is not ceremony: the
# run below invokes an agent against a real provider account, and a script that
# did so because somebody tab-completed it would be a bug with an invoice
# attached. The fake mode exists so every part of the harness can be debugged
# for nothing, and only the model's answer costs anything.
#
# WHAT IT TOUCHES. The agent is given a git WORKTREE at a temporary path,
# detached at HEAD, never the working tree you are sitting in. It is removed at
# the end whatever happened; nothing is committed and nothing is pushed.
#
# WHAT IT EXERCISES, in order: the back office writes a real product record; the
# readiness rubric scores it; the planner refuses below 45 and accepts above it
# (BR-07); the scheduler decides what may start as a pure function of the graph
# (D-1); the runner takes the workspace lease, materialises the packet as files
# (D-8) and spawns the tool detached (D-2); the tool writes the handoff document
# the packet demanded (D-9, BR-30); GAOS parses it, records one TaskRun with the
# cost the tool reported (D-5, D-51) and the frozen slice it was given (D-6).
#
# That is build-order steps 3 through 7 in one invocation, which is why it is
# worth the money once.
set -u

cd "$(dirname "$0")/../.." || exit 1

fails=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails + 1)); }
info() { printf '       %s\n' "$1"; }

PORT="${GAOS_ACCEPT_PORT:-19398}"
BASE="http://127.0.0.1:$PORT"
TMP="$(mktemp -d)"
WORKSPACES="$TMP/ws"

# The desk this lands on. Desk ids are product ids, the workspace root is joined
# with the desk id, and p-5 is the one seeded IDLE -- see seedDeskState in
# dashboard/fixtures.go. A busy desk would refuse the dispatch, correctly.
DESK="p-5"
DESK_IDX=4
WORKTREE="$WORKSPACES/$DESK"
DASH_PID=""
JAR="$TMP/cookies"

# The task. Small, real, and checkable without reading prose: the agent has to
# leave a file that did not exist, saying something only the packet could have
# told it.
TASK_FILE="docs/RUNBOOK-egress.md"
ACCEPT="egress"

cleanup() {
  [ -n "$DASH_PID" ] && kill -9 "$DASH_PID" 2>/dev/null
  # The agent is NOT killed here, and that is D-2 rather than an oversight: it
  # runs detached in its own process group precisely so it outlives whatever
  # started it. If this script exits early the run continues, its pgid is in
  # .gaos/run/RUN.json under the workspace, and the next orchestrator on that
  # workspace reconciles it. Killing "everything in my process group" on the way
  # out is how a cleanup ends up killing the cleanup.
  if [ "${GAOS_LIVE_KEEP:-}" = "1" ]; then
    # The evidence outlives the script. Worth having when a run costs money and
    # something about it needs explaining afterwards -- the first live run here
    # took its own worktree down with it and the question "what did the agent
    # actually write" could only be answered by paying again.
    printf '       kept: %s\n' "$WORKTREE"
    return
  fi
  git worktree remove --force "$WORKTREE" >/dev/null 2>&1
  git worktree prune >/dev/null 2>&1
  rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

echo "== live model call =="

# ---------------------------------------------------------------- mode

MODE="dry"
[ "${GAOS_LIVE_FAKE:-}" = "1" ] && MODE="fake"
[ "${GAOS_LIVE_OK:-}" = "1" ] && MODE="live"

if [ "$MODE" = "fake" ]; then
  CLI_CMD="sh $PWD/dashboard/testdata/fakecli/claude"
  SESSION_ENV="FAKE_CLI_MODE,FAKE_CLI_FIXTURES"
  export FAKE_CLI_MODE=complete
  export FAKE_CLI_FIXTURES="$PWD/dashboard/testdata/handoff"
  ok "fake provider tool — the harness runs, nothing is spent, the model proves nothing"
else
  CLI_CMD="${GAOS_CLI_COMMAND:-claude}"
  if ! command -v "$CLI_CMD" >/dev/null 2>&1; then
    bad "$CLI_CMD is not on PATH"
    exit 1
  fi
  ok "$CLI_CMD present — $("$CLI_CMD" --version 2>&1 | head -1)"
  # Named, never inherited: the adapter builds the child's environment
  # explicitly (D-051), so whatever the tool needs to find its own session has
  # to be listed here by NAME. GAOS forwards names and never values it read.
  SESSION_ENV="${GAOS_CLI_SESSION_ENV:-ANTHROPIC_API_KEY,CLAUDE_CODE_OAUTH_TOKEN,XDG_CONFIG_HOME}"
fi

if [ "$MODE" = "dry" ]; then
  echo
  info "NOT RUNNING. The live mode invokes a provider tool against a real account and spends money."
  info "The agent would be given a detached git worktree and asked to write $TASK_FILE."
  info "Nothing is committed and nothing is pushed."
  info ""
  info "  GAOS_LIVE_FAKE=1 $0     # exercise the harness for free"
  info "  GAOS_LIVE_OK=1   $0     # the real call"
  echo
  exit 0
fi

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  bad "not a git repository — the agent is given a worktree, and there is none to make"
  exit 1
fi

# ---------------------------------------------------------------- the worktree

mkdir -p "$WORKSPACES"
if ! git worktree add --detach "$WORKTREE" HEAD >/dev/null 2>&1; then
  bad "could not create a worktree at $WORKTREE"
  exit 1
fi
ok "worktree at $WORKTREE, detached at $(git -C "$WORKTREE" rev-parse --short HEAD)"

if [ -e "$WORKTREE/$TASK_FILE" ]; then
  bad "$TASK_FILE already exists; the assertion below would pass without the agent"
  exit 1
fi

# ---------------------------------------------------------------- the dashboard

if ! (cd dashboard && go build -o "$TMP/dashboard" .); then
  bad "the dashboard did not build"
  exit 1
fi
ok "dashboard built"

# GAOS_STORE=memory, deliberately. What is under test is the packet, the
# contract and the model's answer to them, none of which is a property of the
# store -- and an ArcadeDB dependency here would make the one script that costs
# money also the one most likely to fail for an unrelated reason.
#
# GAOS_EGRESS_ALLOW_PATHS names the one file the task is ABOUT, and getting to
# that value took two refusals that were both correct.
#
# The documented default of src/**,tests/** matches nothing in this repository,
# so the agent would be handed a slice with no source in it at all. The obvious
# repair is ** -- and the whole worktree is far past the 256 KB slice limit, so
# the pass refuses every dispatch, over and over. The next obvious choice was
# dashboard/egress.go, the source of truth for the pass being documented, and
# THAT refused too: the file defining the detector patterns trips the detectors,
# at the line where env-assignment's regexp is written down.
#
# So the packet carries dashboard/modelconfig.go, which is the file that
# actually answers the story's criteria -- every GAOS_EGRESS_* key, their
# defaults, and the three detector classes, in one screenable file. A packet
# carries what the task needs; that is the allowlist working rather than being
# worked around.
GAOS_ADDR=":$PORT" \
GAOS_STORE=memory \
GAOS_COOKIE_SECURE=0 \
GAOS_MODEL_MODE=cli \
GAOS_CLI_COMMAND="$CLI_CMD" \
GAOS_CLI_SESSION_ENV="$SESSION_ENV" \
GAOS_RUNNER=on \
GAOS_WORKSPACE_ROOT="$WORKSPACES" \
GAOS_EGRESS_ALLOW_PATHS="${GAOS_EGRESS_ALLOW_PATHS:-dashboard/modelconfig.go}" \
GAOS_EGRESS_MAX_BYTES="${GAOS_EGRESS_MAX_BYTES:-262144}" \
  "$TMP/dashboard" >>"$TMP/dashboard.log" 2>&1 &
DASH_PID=$!

code=""
n=0
while [ "$n" -lt 80 ]; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -m 2 "$BASE/healthz" 2>/dev/null)"
  [ "$code" = "200" ] && break
  n=$((n + 1)); sleep 0.25
done
if [ "$code" != "200" ]; then
  bad "the dashboard never became ready"
  info "the boot smoke test runs the tool's --version against the pin and FAILS THE BOOT on a mismatch"
  sed -n '1,40p' "$TMP/dashboard.log" | sed 's/^/       /'
  exit 1
fi
ok "dashboard ready — the boot smoke test passed against the pin"

# ---------------------------------------------------------------- the record

# Free the floor first.
#
# The seeded fixtures put four desks in flight with agents seated at them, and
# the scheduler will not give a task to a busy agent -- "every backend agent is
# busy" is one of the five blocking reasons in §7.4, and it is the correct
# answer on a full floor. Cancelling those four desks releases their agents. It
# is a real operator action on a real route, not a back door: the seeded work is
# a simulation and this is the operator deciding it is not what the floor is
# for.
for i in 0 1 2 3; do
  curl -s -o /dev/null -X POST "$BASE/tasks/$i/cancel"
done
ok "seeded desks cancelled — the agents they held are free for the scheduler"

# The login checks nothing -- T-087, and the register says so rather than this
# script pretending otherwise -- so any address that looks like one opens the
# back office. GAOS_COOKIE_SECURE=0 above is why the cookie comes back: it
# defaults ON, and curl will not return a Secure cookie over plain HTTP. The
# local compose file sets the same flag for the same reason.
curl -s -c "$JAR" -o /dev/null -X POST "$BASE/auth/login" \
  --data-urlencode "email=acceptance@example.test"
grep -q "gaos_bo" "$JAR" || bad "no back-office cookie — the writes below would be refused"

# This is what becomes .gaos/packet/rules.md, and the run is scored against it,
# so it is the real thing rather than filler.
curl -s -b "$JAR" -o /dev/null -X PUT "$BASE/products/$DESK/brd" \
  --data-urlencode "problem=GAOS screens every context slice before it is dispatched to a model, and no operator-facing page explains what that pass does or how to configure it. Operators are reading Go to find out." \
  --data-urlencode "goals=An operator can read one page and configure the egress policy correctly, without opening the source." \
  --data-urlencode "rules=Every claim about behaviour must name the file it is true of.
Do not invent configuration keys; document only ones that exist in the code.
British spelling, no marketing language, no future tense about unbuilt features." \
  --data-urlencode "scope=One runbook page for the pre-dispatch egress pass." \
  --data-urlencode "outofscope=Changing any Go code.
Creating or editing any file other than the new document." \
  --data-urlencode "metrics=An operator configures the policy correctly on the first attempt."

# pri=must, because the criteria-coverage signal is worth 12 points and only
# counts must stories with two or more criteria.
curl -s -b "$JAR" -o /dev/null -X POST "$BASE/products/$DESK/stories" \
  --data-urlencode "pri=must" \
  --data-urlencode "role=operator" \
  --data-urlencode "want=a runbook for the pre-dispatch egress pass" \
  --data-urlencode "so=I can configure it without reading Go" \
  --data-urlencode "ac=The file $TASK_FILE exists and is markdown.
It names the detector classes the pass arms by default.
It names the environment variables that configure the policy, and only ones that exist.
It states what happens on a positive detection, quoting the rule from the code."
ok "product record written — problem, goals, three rules, scope, exclusions, one must story"

SCORE="$(curl -s "$BASE/products/$DESK/readiness" | grep -o '"score":[0-9]*' | head -1 | cut -d: -f2)"
if [ -n "$SCORE" ]; then
  ok "readiness $SCORE — the planning floor is 45 (BR-07)"
else
  info "readiness endpoint returned nothing parseable; continuing"
fi

# ---------------------------------------------------------------- plan

# The wizard, because the objective set here is what the desk carries into the
# packet -- going through the planner is what makes this a product flow rather
# than a demonstration of a subprocess.
STORY_ID="$(curl -s -b "$JAR" "$BASE/products/$DESK/readiness" >/dev/null; echo "US-01")"
CREATED="$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X POST "$BASE/workflows" \
  --data-urlencode "product=$DESK" \
  --data-urlencode "goal=Write the operator runbook for the pre-dispatch egress pass" \
  --data-urlencode "priority=high" \
  --data-urlencode "gate=no" \
  --data-urlencode "stories=$STORY_ID")"
if [ "$CREATED" != "200" ]; then
  bad "the planner refused the workflow (HTTP $CREATED)"
  info "below an effective 45 that refusal is correct — BR-07 — and the record above is what to fix"
  exit 1
fi
ok "workflow planned — the desk carries the objective and a task graph"

# AND NOW THE DESK IS CANCELLED, WHICH LOOKS BACKWARDS AND IS THE FINDING.
#
# CreateWorkflow seats the planner agent at the desk it just made Active. The
# first planned task has role `planner`. The seeded floor holds exactly one
# planner. So the first task of every planned workflow blocks on "every planner
# agent is busy" -- waiting for an agent that is busy being this workflow's own
# desk. That is a real deadlock and a capacity-model problem (T-045), not
# something this script should paper over quietly.
#
# Cancelling the desk releases that agent while leaving the objective and the
# task text the planner wrote, so the scheduler can start the graph it planned.
#
# ONE DISPATCHER, and that is the second thing learned by paying for it. An
# earlier version of this script also pressed the operator's dispatch button on
# the same desk. Both routes worked -- and then the scheduler took the workspace
# the moment the operator's run released its lease, started a SECOND real model
# call, and truncated the first run's handoff document before anything on disk
# could be read. GAOS itself was fine (it had already parsed the document, which
# is exactly why T-064 stores it rather than trusting the workspace); the bill
# was not. Two dispatchers aimed at one workspace is two invoices.
curl -s -o /dev/null -X POST "$BASE/tasks/$DESK_IDX/cancel"
ok "desk released — the planner agent is free and the scheduler owns the graph"

# ---------------------------------------------------------------- wait

DEADLINE=$(( $(date +%s) + ${GAOS_LIVE_TIMEOUT:-900} ))
info "waiting for the scheduler to dispatch and the run to end (up to ${GAOS_LIVE_TIMEOUT:-900}s)"
started=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  EV="$(curl -s "$BASE/events")"
  if [ "$started" -eq 0 ] && echo "$EV" | grep -q '"RunStarted"'; then
    started=1
    ok "RunStarted — the lease is held, the packet is on disk and the tool is running"
    # The packet, as files. T-061: the difference between a tool with an
    # instruction and a tool with a working directory.
    for want in ".gaos/packet/task.md" ".gaos/packet/rules.md" ".gaos/packet/handoff.md"; do
      [ -f "$WORKTREE/$want" ] && ok "packet: $want" || bad "packet: $want was never written"
    done
  fi
  echo "$EV" | grep -q '"RunExited"' && break
  sleep 5
done

if ! curl -s "$BASE/events" | grep -q '"RunExited"'; then
  bad "no RunExited within the deadline"
  info "the agent may still be running; its log is under $WORKTREE/.gaos/run/log"
  tail -25 "$TMP/dashboard.log" | sed 's/^/       /'
  exit 1
fi
ok "RunExited — the orchestrator saw the run end"

# ---------------------------------------------------------------- assertions

# The document on disk. Read with the caveat that the workspace is a LEASE: a
# later attempt truncates this file before it starts, so an empty one here does
# not by itself mean the agent wrote nothing. The authority is GAOS's reading of
# it, asserted next.
OUT="$WORKTREE/.gaos/handoff/OUTPUT.md"
if [ ! -s "$OUT" ]; then
  bad "no handoff document in the workspace"
  info "the contract it was given is at $WORKTREE/.gaos/packet/handoff.md"
  info "if the outcome below is complete, a later attempt truncated it — the workspace is a lease"
else
  ok "the agent wrote .gaos/handoff/OUTPUT.md"
  head -1 "$OUT" | grep -q -- "---" \
    && ok "it opens with front matter" \
    || bad "it does not open with --- — the parser reads nothing and the attempt failed"
  for key in "task:" "run:" "role:" "status:" "criteria:"; do
    grep -q "^$key" "$OUT" || bad "no $key in the front matter — mandatory, and its absence fails the whole attempt"
  done
fi

# GAOS's own reading of it, which is the assertion that matters: the document
# above could be perfect and still not have been parsed.
#
# From RunExited's TEXT, not from a payload. Events() strips payloads on the way
# out of the seam, so the failure signature is not in this response at all --
# an earlier version of this script grepped for "handoff-missing" here and
# passed every time, including on the run where no document was written. A
# vacuous assertion is worse than none: it reports success for the thing it
# cannot see.
EVENTS="$(curl -s "$BASE/events")"
OUTCOME="$(echo "$EVENTS" | python3 -c '
import json,sys
for e in json.load(sys.stdin)["events"]:
    if e["Type"] == "RunExited":
        print(e.get("Text",""))
' 2>/dev/null | tail -1)"
case "$OUTCOME" in
  *"outcome complete"*) ok "GAOS read the document: $OUTCOME" ;;
  *"outcome blocked"*|*"outcome partial"*)
    ok "GAOS read the document: $OUTCOME"
    info "blocked and partial are legitimate answers; the contract says so" ;;
  *"outcome failed"*)
    bad "GAOS recorded a failed attempt: $OUTCOME"
    info "the work may well have been done — an attempt GAOS cannot read is an attempt that did not happen (BR-30)" ;;
  *) bad "no RunExited text to read an outcome from" ;;
esac

# The work itself, and the ONLY assertion in this file a fake cannot pass.
#
# The fake tool copies a fixture document and does nothing else, so in fake mode
# the absence of the file is the expected result rather than a failure -- saying
# FAIL there would train everyone to ignore the free mode's output. This is the
# line the money buys.
if [ -f "$WORKTREE/$TASK_FILE" ]; then
  ok "$TASK_FILE exists — the agent did the work in the leased worktree"
  grep -qi "$ACCEPT" "$WORKTREE/$TASK_FILE" \
    && ok "and it is about what the packet asked for ($(wc -l < "$WORKTREE/$TASK_FILE") lines)" \
    || bad "the file exists but never mentions \"$ACCEPT\"; the packet may not have reached the model"
elif [ "$MODE" = "fake" ]; then
  info "$TASK_FILE was not created — expected: the fake tool writes a fixture and does no work."
  info "This assertion is the one only GAOS_LIVE_OK=1 can answer."
else
  bad "$TASK_FILE was not created"
fi

COST="$(echo "$EVENTS" | grep -o '"Cost":[0-9.]*' | tail -1 | cut -d: -f2)"
if [ -n "$COST" ] && [ "$COST" != "0" ]; then
  ok "cost recorded from the tool's own report: \$$COST (D-051: measured, not estimated)"
else
  info "no cost in the stream — a subscription plan reports none, and BR-37 records that as unavailable rather than zero"
fi

echo
if [ "$fails" -eq 0 ]; then
  printf '  \033[32mok\033[0m   the whole loop ran: record, plan, schedule, lease, packet, model, document, record\n'
else
  printf '  \033[31mFAIL\033[0m %d check(s) failed\n' "$fails"
fi
echo
info "the document the agent wrote:"
sed 's/^/       | /' "$OUT" 2>/dev/null | head -32
exit "$fails"
