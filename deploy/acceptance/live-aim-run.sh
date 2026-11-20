#!/usr/bin/env bash
# The first turn through the gateway -- live-cli-run.sh's successor (M14.5,
# T-161, TC-300…TC-303), and a script that spends money.
#
# Everything in dashboard/ runs against an httptest fake ai-manager. What a fake
# cannot prove is the bargain the gateway rests on: that a REAL model, in a
# workspace holding the product's Blueprint, handed GAOS's brief and the reply
# contract and nothing else, answers with a handoff block GAOS can read.
#
#   GAOS_LIVE_OK=1 \
#   GAOS_AIM_URL=http://10.10.0.4:8099 \
#   GAOS_AIM_CHAT_AUTH_FILE=deploy/stacks/dev.aim-chat.json \
#   GAOS_AIM_ADMIN_KEY_FILE=deploy/stacks/dev.aim-admin-key \
#   GAOS_ACCEPT_PASSWORD_FILE=~/.config/gaos/acceptance-password \
#     ./deploy/acceptance/live-aim-run.sh
#
# Since the real back-office sign-in (D-079, v0.39.0) every route but /healthz
# needs a session. The script makes its own one-account users file for the
# dashboard it boots -- GAOS_ACCEPT_EMAIL (default acceptance@example.test),
# hashed with `dashboard hash-password` from the first line of
# GAOS_ACCEPT_PASSWORD_FILE -- signs in once, and sends the cookie jar on every
# call after that. The password is read from the file and piped; it is never
# put on a command line, in the environment or in the output.
#
# Without GAOS_LIVE_OK=1 it says what it would do and stops: a turn costs real
# money on a real account, and a script that ran one because somebody
# tab-completed it would be a bug with an invoice attached.
#
# WHAT IT TOUCHES. A dashboard on 127.0.0.1 with the memory store, nothing else
# of GAOS's. Upstream, ONE workspace, gaos-acceptance-p-5, which provisioning
# makes idempotent: a second run reuses it, and its files are rewritten from the
# Blueprint each time. The runner dispatches the workflow's first task only --
# everything after it depends on that task reaching DONE, which needs a review
# nothing here performs -- so one run is one turn.
set -u
cd "$(dirname "$0")/../.."

fails=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails + 1)); }
info() { printf '       %s\n' "$1"; }

PORT="${GAOS_ACCEPT_PORT:-19399}"
BASE="http://127.0.0.1:$PORT"
TMP="$(mktemp -d)"
JAR="$TMP/cookies"
DASH_PID=""
DESK="p-5"

cleanup() {
  [ -n "$DASH_PID" ] && kill "$DASH_PID" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

echo "== live turn through the gateway =="

if [ "${GAOS_LIVE_OK:-}" != "1" ]; then
  info "dry run: set GAOS_LIVE_OK=1 to spend one turn. It would:"
  info "  boot a dashboard in aimanager mode against \${GAOS_AIM_URL}, runner on;"
  info "  write a product record, a coding policy, publish and sync the Blueprint;"
  info "  prove a credential in a policy is refused, then plan a workflow;"
  info "  wait for one turn and read its handoff from the reply."
  exit 0
fi
for v in GAOS_AIM_URL GAOS_AIM_CHAT_AUTH_FILE GAOS_AIM_ADMIN_KEY_FILE GAOS_ACCEPT_PASSWORD_FILE; do
  if [ -z "${!v:-}" ] || { [ "$v" != GAOS_AIM_URL ] && [ ! -r "${!v}" ]; }; then
    bad "$v is unset or unreadable"
    exit 1
  fi
done

# ---------------------------------------------------------------- TC-300
(cd dashboard && go build -o "$TMP/dashboard" .) || { bad "dashboard did not build"; exit 1; }
ok "dashboard built"
EMAIL="${GAOS_ACCEPT_EMAIL:-acceptance@example.test}"
pw() { head -n1 "$GAOS_ACCEPT_PASSWORD_FILE" | tr -d '\r\n'; }
HASH="$(pw | "$TMP/dashboard" hash-password 2>/dev/null)" || { bad "the password file's first line could not be hashed (is it empty?)"; exit 1; }
( umask 077; printf '%s:%s\n' "$EMAIL" "$HASH" >"$TMP/bo-users" )
GAOS_BO_USERS_FILE="$TMP/bo-users" GAOS_BO_LABEL=acceptance GAOS_COOKIE_SECURE=0 \
GAOS_STORE=memory GAOS_ADDR="127.0.0.1:$PORT" GAOS_RUNNER=on \
GAOS_MODEL_MODE=aimanager GAOS_AIM_TENANT=acceptance GAOS_MODEL="${GAOS_MODEL:-claude-sonnet-5}" \
GAOS_AIM_URL="$GAOS_AIM_URL" GAOS_AIM_CHAT_AUTH_FILE="$GAOS_AIM_CHAT_AUTH_FILE" \
GAOS_AIM_ADMIN_KEY_FILE="$GAOS_AIM_ADMIN_KEY_FILE" \
  "$TMP/dashboard" >"$TMP/dashboard.log" 2>&1 &
DASH_PID=$!
for _ in $(seq 1 40); do curl -s -o /dev/null "$BASE/healthz" && break; sleep 0.25; done
if ! curl -s "$BASE/healthz" | grep -q "gateway"; then
  bad "the dashboard did not come up in gateway mode"
  info "the boot smoke test asks ai-manager for a claude provider and FAILS THE BOOT without one"
  tail -8 "$TMP/dashboard.log" | sed 's/^/       /'
  exit 1
fi
ok "dashboard ready in gateway mode — the boot smoke test found ai-manager's claude provider"

pw | curl -s -c "$JAR" -o /dev/null -X POST "$BASE/auth/login" \
  --data-urlencode "email=$EMAIL" --data-urlencode "password@-"
if grep -q 'gaos_bo' "$JAR" 2>/dev/null; then
  ok "signed in to the back office as $EMAIL (D-079)"
else
  bad "the back office refused the sign-in for $EMAIL; check GAOS_ACCEPT_PASSWORD_FILE"
  exit 1
fi
bo() { curl -s -b "$JAR" -H "HX-Request: true" "$@"; }
events() { curl -s -b "$JAR" "$BASE/events"; }

# Free the floor: the seeded desks hold the agents the scheduler would need.
for i in 0 1 2 3; do curl -s -b "$JAR" -o /dev/null -X POST "$BASE/tasks/$i/cancel"; done

# ---------------------------------------------------------------- TC-301
bo -o /dev/null -X PUT "$BASE/products/$DESK/brd" \
  --data-urlencode "problem=New agents on this product keep re-deriving its coding rules from the code, and get them wrong. One short statement of the rules would stop that." \
  --data-urlencode "goals=Every agent states the product's coding rules correctly before it writes code." \
  --data-urlencode "rules=Quote rules from the Blueprint; never invent one.
Answer in the reply; there is no repository to change.
No marketing language." \
  --data-urlencode "scope=One statement of the coding policy." \
  --data-urlencode "outofscope=Changing any code." \
  --data-urlencode "metrics=The statement matches the published policy."
bo -o /dev/null -X POST "$BASE/products/$DESK/stories" \
  --data-urlencode "pri=must" --data-urlencode "role=operator" \
  --data-urlencode "want=the coding policy restated in one paragraph" \
  --data-urlencode "so=a new agent follows it" \
  --data-urlencode "ac=The reply states that code is formatted with gofmt.
The reply states that errors are wrapped with %w."
SCORE="$(curl -s -b "$JAR" "$BASE/products/$DESK/readiness" | grep -o '"score":[0-9]*' | head -1 | cut -d: -f2)"
[ -n "$SCORE" ] && [ "$SCORE" -ge 45 ] && ok "readiness $SCORE — above the planning floor of 45 (BR-07)" \
  || bad "readiness ${SCORE:-?} is below the planning floor"

# ---------------------------------------------------------------- TC-302
bo -o /dev/null "$BASE/products/$DESK/blueprint/policies" --data-urlencode "key=go-style" --data-urlencode "kind=coding" \
  --data-urlencode "title=Go style" --data-urlencode "body=Deploy with key AKIAIOSFODNN7EXAMPLE."
bo -o /dev/null -X POST "$BASE/products/$DESK/blueprint/publish"
bo -o /dev/null -X POST "$BASE/products/$DESK/blueprint/sync"
if events | grep -q 'was not written to the workspace'; then
  ok "a Blueprint carrying a credential was refused before the sync, and the refusal is on the timeline (BR-32)"
else
  bad "a Blueprint carrying a credential was not reported as refused"
fi

bo -o /dev/null "$BASE/products/$DESK/blueprint/policies" --data-urlencode "key=go-style" --data-urlencode "kind=coding" \
  --data-urlencode "title=Go style" --data-urlencode "body=Format every file with gofmt. Wrap errors with %w; never discard one."
bo -o /dev/null -X POST "$BASE/products/$DESK/blueprint/publish"
SYNC="$(bo -X POST "$BASE/products/$DESK/blueprint/sync" | grep -o 'Blueprint version [0-9]* is in the workspace [a-z0-9.-]*' | head -1)"
[ -n "$SYNC" ] && ok "$SYNC — provisioned and written through ai-manager" \
  || { bad "the Blueprint did not reach the workspace"; tail -5 "$TMP/dashboard.log" | sed 's/^/       /'; exit 1; }

# ---------------------------------------------------------------- TC-303
CREATED="$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X POST "$BASE/workflows" \
  --data-urlencode "product=$DESK" --data-urlencode "goal=Restate the coding policy for new agents" \
  --data-urlencode "priority=high" --data-urlencode "gate=no" --data-urlencode "stories=US-01")"
[ "$CREATED" = "200" ] || { bad "the planner refused the workflow (HTTP $CREATED)"; exit 1; }
# No desk cancel here. Before v0.39.1 the planner CreateWorkflow seated on the
# desk counted as busy against the desk's own first task, and this script
# cancelled the desk to free it; the seat now serves its own desk's task.
ok "workflow planned — the scheduler owns the graph"

DEADLINE=$(( $(date +%s) + ${GAOS_LIVE_TIMEOUT:-700} ))
info "waiting for one turn (up to ${GAOS_LIVE_TIMEOUT:-700}s)"
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  events | grep -q '"RunExited"' && break
  sleep 5
done
EV="$(events)"
echo "$EV" | grep -q 'blueprint-drift\|holds no Blueprint' && bad "the dispatch was refused as drift; the synced Blueprint was not the published one"
echo "$EV" | grep -q '"SessionOpened"' && ok "SessionOpened — the turn ran in a new session in gaos-acceptance-$DESK" \
  || bad "no SessionOpened; the turn never reached a session"
OUTCOME="$(echo "$EV" | python3 -c '
import json,sys
for e in json.load(sys.stdin)["events"]:
    if e["Type"] == "RunExited":
        print(e.get("Text",""))' 2>/dev/null | tail -1)"
case "$OUTCOME" in
  *"outcome complete"*|*"outcome blocked"*|*"outcome partial"*)
    ok "the reply's gaos-handoff block parsed: $OUTCOME" ;;
  *"outcome failed"*)
    bad "GAOS recorded a failed attempt: $OUTCOME"
    info "an attempt GAOS cannot read is an attempt that did not happen (BR-30)" ;;
  *) bad "no RunExited within the deadline"; tail -10 "$TMP/dashboard.log" | sed 's/^/       /' ;;
esac

echo
if [ "$fails" -eq 0 ]; then
  printf '\033[32mlive turn passed\033[0m\n'
else
  printf '\033[31m%d live check(s) failed\033[0m\n' "$fails"
  exit 1
fi
