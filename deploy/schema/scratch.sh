#!/usr/bin/env bash
# A throwaway ArcadeDB for the store conformance suite.
#
# dashboard/store_conformance_test.go runs one set of cases against both
# implementations of the Store seam. MemStore needs nothing; the ArcadeDB half
# skips unless GAOS_ARCADE_TEST_URL names a reachable database. This brings one
# up, applies the schema to it, and prints the line to export.
#
#   ./deploy/schema/scratch.sh up        # start it, apply the DDL, print the env
#   ./deploy/schema/scratch.sh env       # print the env again for a running one
#   ./deploy/schema/scratch.sh down      # stop and delete it
#   ./deploy/schema/scratch.sh status    # is it up, and does it answer
#
# THROWAWAY ON PURPOSE: --rm, no volume, a port of its own. Nothing here should
# ever be pointed at an instance anyone depends on -- dev and prod are shared
# ArcadeDB instances belonging to the wider master-compose-ai* stacks and GAOS
# is a guest there. That is the same warning dashboard/schema/RECONCILIATION.md
# ends with, and this script exists so there is never a reason to ignore it.
#
# The version is pinned by D-043. An upgrade is a schema event that re-runs the
# validation, not a tag that drifts.
set -u

IMAGE="arcadedata/arcadedb:26.7.2"
NAME="gaos-scratch-db"
PORT="${GAOS_SCRATCH_PORT:-2481}"
PASS="${GAOS_SCRATCH_PASSWORD:-scratchpw}"
DB="${GAOS_ARCADE_TEST_DB:-gaos_conformance}"
URL="http://localhost:$PORT"

cd "$(dirname "$0")/../.." || exit 1

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
info() { printf '       %s\n' "$1"; }

running() { [ -n "$(docker ps -q -f "name=^${NAME}$" 2>/dev/null)" ]; }

# ready polls the endpoint the dashboard probes. 204 is the healthy answer and
# 200 is accepted too -- measured, and the reason dashboard/arcade.go accepts
# both.
ready() {
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' -m 3 "$URL/api/v1/ready" 2>/dev/null)"
  [ "$code" = "204" ] || [ "$code" = "200" ]
}

env_line() {
  echo
  echo "  export GAOS_ARCADE_TEST_URL=$URL"
  echo "  export GAOS_ARCADE_TEST_PASSWORD=$PASS"
  echo
  echo "  cd dashboard && go test -run Conformance ./..."
  echo
}

case "${1:-}" in
  up)
    echo "== scratch arcadedb =="
    if running; then
      ok "$NAME is already up on $URL"
    else
      if ! docker run -d --rm --name "$NAME" -p "$PORT:2480" \
           -e JAVA_OPTS="-Darcadedb.server.rootPassword=$PASS" "$IMAGE" >/dev/null 2>&1; then
        bad "could not start $IMAGE on port $PORT"
        info "is something already bound to $PORT? set GAOS_SCRATCH_PORT to move it"
        exit 1
      fi
      ok "started $IMAGE as $NAME on $URL"
    fi

    printf '       waiting for the ready probe'
    for _ in $(seq 1 60); do
      if ready; then break; fi
      printf '.'
      sleep 1
    done
    printf '\n'
    if ! ready; then
      bad "$URL/api/v1/ready never answered 204"
      info "docker logs $NAME"
      exit 1
    fi
    ok "ready probe answers"

    echo
    echo "== schema =="
    if GAOS_ARCADE_USER=root GAOS_ARCADE_PASSWORD="$PASS" \
       ./deploy/schema/apply.sh scratch --url "$URL" --db "$DB" >/tmp/gaos-scratch-apply.log 2>&1; then
      ok "$(tail -1 /tmp/gaos-scratch-apply.log)"
    else
      bad "the schema did not apply cleanly"
      tail -20 /tmp/gaos-scratch-apply.log
      exit 1
    fi
    env_line
    ;;

  env)
    if ! running; then
      bad "$NAME is not running -- ./deploy/schema/scratch.sh up"
      exit 1
    fi
    env_line
    ;;

  status)
    echo "== scratch arcadedb =="
    if running; then
      ok "$NAME is up on $URL"
    else
      bad "$NAME is not running"
      exit 1
    fi
    if ready; then ok "ready probe answers"; else bad "ready probe does not answer"; exit 1; fi
    ;;

  down)
    echo "== scratch arcadedb =="
    if running; then
      docker stop "$NAME" >/dev/null 2>&1 && ok "stopped and removed $NAME" \
        || { bad "could not stop $NAME"; exit 1; }
    else
      ok "$NAME was not running"
    fi
    ;;

  *)
    echo "usage: deploy/schema/scratch.sh <up|down|status|env>" >&2
    echo "  up      start a throwaway ArcadeDB $IMAGE, apply the DDL, print the env" >&2
    echo "  down    stop and delete it" >&2
    echo "  status  report whether it is up and answering" >&2
    echo "  env     print the export lines again" >&2
    exit 2
    ;;
esac
