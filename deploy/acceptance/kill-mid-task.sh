#!/usr/bin/env bash
# Build-order step 5, and its own acceptance test:
#
#   "Kill the orchestrator mid-task; the work survives and is re-adopted."
#
# THIS CANNOT BE A GO TEST, and pretending otherwise would be worse than not
# having it. The property under test is that a child outlives its parent, and
# under `go test` the test binary IS the parent of everything it spawns while
# the harness reaps the process tree on exit -- so the framework undoes the one
# thing being asserted. dashboard/runner_test.go gets as close as Go can, by
# cancelling the context and checking the child survives; that covers
# context.WithoutCancel and nothing beyond it. Surviving a SIGKILL to the
# orchestrator needs a real process tree and a real restart, which is this.
#
#   ./deploy/acceptance/kill-mid-task.sh
#
# TWO LIMITS, BOTH DELIBERATE.
#
# It runs against a SCRATCH ARCADEDB and never against dev or prod. Recovery
# writes to the graph, and the shared instances are the wider master-compose-ai*
# stacks' -- GAOS is a guest there. deploy/schema/scratch.sh exists so there is
# never a reason to point this anywhere else.
#
# It CANNOT run with GAOS_STORE=memory. MemStore loses every task record on a
# restart, so there is no claim left to reconcile -- recovery is a property of
# the ArcadeDB deployment, and saying so here is better than letting somebody
# discover it from a green run that proved nothing.
#
# The provider tool is the FAKE, with a sleep long enough to be killed around.
# The thing under test is the orchestrator's recovery, not the model's output,
# and a real call would add cost and non-determinism to a test that needs
# neither.
set -u

cd "$(dirname "$0")/../.." || exit 1

fails=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails + 1)); }
info() { printf '       %s\n' "$1"; }

PORT="${GAOS_ACCEPT_PORT:-19399}"
BASE="http://127.0.0.1:$PORT"
TMP="$(mktemp -d)"
WORKSPACES="$TMP/ws"
DASH_PID=""
# One tenant per invocation, so the floor starts from the fixtures -- and the
# SAME tenant across both boots, because the second orchestrator has to find the
# first one's claim.
TENANT="accept-$$"

cleanup() {
  [ -n "$DASH_PID" ] && kill -9 "$DASH_PID" 2>/dev/null
  # Anything the fake tool left behind. It is in its own process group, which
  # is the whole point, so it does not die with the shell.
  [ -f "$TMP/childpid" ] && kill -9 "-$(cat "$TMP/childpid")" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

echo "== preconditions =="
if [ -z "${GAOS_ARCADE_TEST_URL:-}" ]; then
  bad "GAOS_ARCADE_TEST_URL is unset — run deploy/schema/scratch.sh up first"
  info "recovery is an ArcadeDB property: a memory store has no claim left to reconcile"
  exit 1
fi
ok "scratch database at $GAOS_ARCADE_TEST_URL"

mkdir -p "$WORKSPACES"
# Built from inside dashboard/, because each component here is its own module --
# there is no workspace at the repository root and `go build ./dashboard` from
# there finds no main module at all.
if ! (cd dashboard && go build -o "$TMP/dashboard" .); then
  bad "the dashboard did not build"
  exit 1
fi
ok "dashboard built"

start_dashboard() {
  # Job control off for the launch, so the shell does not print a "Killed"
  # notice for the SIGKILL this test exists to send.
  set +m
  GAOS_ADDR=":$PORT" \
  GAOS_STORE=arcadedb \
  GAOS_ARCADE_URL="$GAOS_ARCADE_TEST_URL" \
  GAOS_ARCADE_USER="${GAOS_ARCADE_TEST_USER:-root}" \
  GAOS_ARCADE_PASSWORD="${GAOS_ARCADE_TEST_PASSWORD:-scratchpw}" \
  GAOS_ARCADE_DB="${GAOS_ARCADE_TEST_DB:-gaos_conformance}" \
  GAOS_ARCADE_TENANT="$TENANT" \
  GAOS_MODEL_MODE=cli \
  GAOS_CLI_COMMAND="sh $PWD/dashboard/testdata/fakecli/claude" \
  GAOS_CLI_SESSION_ENV="FAKE_CLI_MODE,FAKE_CLI_SLEEP,FAKE_CLI_FIXTURES" \
  GAOS_RUNNER=on \
  GAOS_WORKSPACE_ROOT="$WORKSPACES" \
  FAKE_CLI_MODE=complete \
  FAKE_CLI_SLEEP="${GAOS_ACCEPT_SLEEP:-30}" \
  FAKE_CLI_FIXTURES="$PWD/dashboard/testdata/handoff" \
    "$TMP/dashboard" >>"$TMP/dashboard.log" 2>&1 &
  DASH_PID=$!
}

wait_for_ready() {
  n=0
  while [ "$n" -lt 50 ]; do
    code="$(curl -s -o /dev/null -w '%{http_code}' -m 2 "$BASE/healthz" 2>/dev/null)"
    [ "$code" = "200" ] && return 0
    n=$((n + 1)); sleep 0.2
  done
  return 1
}

echo
echo "== first orchestrator =="
start_dashboard
if wait_for_ready; then ok "the dashboard answers /healthz"; else
  bad "the dashboard never became ready"; sed -n '1,40p' "$TMP/dashboard.log"; exit 1
fi

# Plan a workflow, so there are Task vertices for the scheduler to schedule and
# for boot reconciliation to find afterwards.
#
# NOT the operator dispatch route. That freezes a packet with a DISPLAY task id
# and has no graph task behind it, so a run started that way leaves nothing
# claiming to be dispatched -- and reconciliation over a graph with no claim is
# a test that passes by having nothing to check.
curl -s -o /dev/null -X POST "$BASE/workflows" \
  --data-urlencode "product=p-1" \
  --data-urlencode "goal=Survive being killed mid-task" \
  --data-urlencode "stories=US-01"
sleep 1

# Then free the desk's agent, or nothing is ever scheduled.
#
# CreateWorkflow seats a-9 on the desk, and a-9 is the fixtures' only planner --
# so the scheduler correctly refuses the plan's first task for capability, and
# the floor sits idle for a reason it can state. That is the scheduler working,
# not a bug in this script, and it is a real finding about the desk/task duality
# rather than something to hide: the desk-level seating predates a scheduler that
# seats per task, and the two now double-book the same agent.
#
# Cancelling clears the desk without touching the graph -- the planned tasks
# outlive it, which is the point of persisting them -- so the scheduler picks the
# first one up on its next tick.
curl -s -o /dev/null -X POST "$BASE/tasks/0/cancel"

n=0; RUNJSON=""
while [ "$n" -lt 60 ]; do
  RUNJSON="$(find "$WORKSPACES" -name RUN.json -print -quit 2>/dev/null)"
  [ -n "$RUNJSON" ] && grep -q '"pid"' "$RUNJSON" && break
  n=$((n + 1)); sleep 0.2
done
if [ -n "$RUNJSON" ] && grep -q '"pid"' "$RUNJSON"; then
  ok "a run started and recorded its process"
else
  bad "no RUN.json with a pid appeared; nothing was supervised"
  sed -n '1,40p' "$TMP/dashboard.log"; exit 1
fi

CHILD="$(sed -n 's/.*"pid": *"\([0-9]*\)".*/\1/p' "$RUNJSON" | head -1)"
echo "$CHILD" > "$TMP/childpid"
info "child pid $CHILD"

echo
echo "== kill the orchestrator =="
kill -9 "$DASH_PID" 2>/dev/null
wait "$DASH_PID" 2>/dev/null
DASH_PID=""
sleep 1

# THE ASSERTION THE WHOLE SCRIPT EXISTS FOR. D-2: "an orchestrator restart must
# not kill a paid model call in flight."
if kill -0 "$CHILD" 2>/dev/null; then
  ok "the child outlived the orchestrator that started it"
else
  bad "the child died with its orchestrator — D-2 says work runs in detached processes that survive it"
fi

echo
echo "== restart, within the staleness window =="
start_dashboard
if wait_for_ready; then ok "the dashboard came back"; else
  bad "the dashboard did not come back"; sed -n '1,40p' "$TMP/dashboard.log"; exit 1
fi

if grep -q "boot reconciliation" "$TMP/dashboard.log"; then
  ok "boot reconciliation ran before the listener came up"
else
  info "no claimed run to reconcile — see the note on GAOS_STORE below"
fi

EVENTS="$(curl -s -m 5 "$BASE/events" 2>/dev/null)"
if printf '%s' "$EVENTS" | grep -q "adopted"; then
  ok "the run was adopted rather than restarted or orphaned"
else
  bad "no adoption in the timeline — a lease held and a beat fresh is the adopt branch (D-4)"
fi

echo
if [ "$fails" -eq 0 ]; then
  printf '  \033[32mok\033[0m   step 5: the work survived and was re-adopted\n'
else
  printf '  \033[31mFAIL\033[0m %d assertion(s) failed\n' "$fails"
fi
exit "$fails"
