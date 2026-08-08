#!/usr/bin/env bash
# Rebuild/restart/status one gaos tier (dev or prod), the whole stack or one
# named service. The deterministic half of the gaos-deploy tooling -- the
# subagent calls this instead of free-handing docker commands, so behaviour
# doesn't drift between runs. See deploy/STACKS.md for the topology this
# encodes and .claude/agents/gaos-deploy.md for who calls it.
#
#   deploy/rebuild-restart.sh <dev|prod> <deploy|restart|status> [service] \
#       [--allow-dirty] [--full-verify]
#
#   deploy   build [service] then up -d [service] -- rebuild + restart
#   restart  restart [service], no build at all -- restart-only
#   status   docker compose ps + one healthz probe, no mutation
#
# service, if given, is one of: traefik landing dashboard docs
set -u

cd "$(dirname "$0")/.." || exit 1

SERVICES="traefik landing dashboard docs"
ALLOW_DIRTY=0
FULL_VERIFY=0
ENVIRON=""
ACTION=""
SERVICE=""

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
info() { printf '  %s\n' "$1"; }

usage() {
  echo "usage: deploy/rebuild-restart.sh <dev|prod> <deploy|restart|status> [service] [--allow-dirty] [--full-verify]"
  echo "  service, if given, is one of: $SERVICES"
  exit 2
}

for arg in "$@"; do
  case "$arg" in
    --allow-dirty) ALLOW_DIRTY=1 ;;
    --full-verify) FULL_VERIFY=1 ;;
    dev|prod)
      [ -n "$ENVIRON" ] && { echo "environment given twice"; usage; }
      ENVIRON="$arg" ;;
    deploy|restart|status)
      [ -n "$ACTION" ] && { echo "action given twice"; usage; }
      ACTION="$arg" ;;
    *)
      [ -n "$SERVICE" ] && { echo "unexpected extra argument: $arg"; usage; }
      SERVICE="$arg" ;;
  esac
done

[ -n "$ENVIRON" ] && [ -n "$ACTION" ] || usage

if [ -n "$SERVICE" ]; then
  case " $SERVICES " in
    *" $SERVICE "*) ;;
    *) echo "unknown service '$SERVICE' -- valid: $SERVICES"; exit 2 ;;
  esac
fi

if [ "$ENVIRON" = dev ]; then
  COMPOSE=deploy/stacks/compose.dev.yaml
  PORT=19301
  HOST=dev.gaos.mfardiansyah.id
else
  COMPOSE=deploy/stacks/compose.prod.yaml
  PORT=18301
  HOST=gaos.mfardiansyah.id
fi
TUNNEL=10.10.0.2

dc() { docker compose -f "$COMPOSE" "$@"; }

# ---- status: read-only, no pre-flight ----
if [ "$ACTION" = status ]; then
  echo "== $ENVIRON ($COMPOSE) =="
  dc ps --format '{{.Service}}\t{{.Status}}\t{{.Ports}}'
  echo
  echo "== healthz through the tunnel =="
  ssh -o ConnectTimeout=8 clino "curl -sS -H 'Host: $HOST' http://$TUNNEL:$PORT/api/healthz" 2>&1
  echo
  exit 0
fi

# ---- pre-flight, deploy and restart alike ----
echo "== pre-flight =="
./deploy/check-stacks.sh
CHECK_RC=$?
if [ "$CHECK_RC" -ne 0 ]; then
  bad "check-stacks.sh failed -- refusing to $ACTION"
  exit 1
fi
ok "check-stacks.sh passed"

if [ "$ENVIRON" = prod ]; then
  DIRTY="$(git status --porcelain)"
  if [ -n "$DIRTY" ]; then
    if [ "$ALLOW_DIRTY" -eq 1 ]; then
      info "working tree is dirty -- proceeding, --allow-dirty was passed"
      printf '%s\n' "$DIRTY" | sed 's/^/    /'
    else
      bad "working tree is dirty -- refusing to touch prod"
      printf '%s\n' "$DIRTY" | sed 's/^/    /'
      echo
      echo "  prod builds from this same working tree (no separate worktree yet --"
      echo "  see deploy/STACKS.md, 'Known gaps'). Commit or stash first, or pass"
      echo "  --allow-dirty if this is intentional."
      exit 1
    fi
  else
    ok "working tree is clean"
  fi
fi

if [ "$ACTION" = deploy ]; then
  echo
  echo "== tests =="
  ( cd dashboard && go test ./... ) || { bad "dashboard tests failed"; exit 1; }
  ok "dashboard tests passed"
  ( cd pustaka && go test ./... ) || { bad "pustaka tests failed"; exit 1; }
  ok "pustaka tests passed"
  ( cd pustaka && go run . check ./docs ) || { bad "pustaka docs check failed"; exit 1; }
  ok "pustaka docs check passed"
fi

# ---- the action itself ----
echo
if [ "$ACTION" = deploy ]; then
  echo "== build =="
  dc build $SERVICE || { bad "build failed"; exit 1; }
  echo
  echo "== up -d =="
  dc up -d $SERVICE || { bad "up -d failed"; exit 1; }
else
  echo "== restart (no rebuild) =="
  dc restart $SERVICE || { bad "restart failed"; exit 1; }
fi

# ---- verification ----
echo
echo "== containers =="
dc ps --format '{{.Service}}\t{{.Status}}\t{{.Ports}}'

vfails=0
vok()  { ok "$1"; }
vbad() { bad "$1"; vfails=$((vfails + 1)); }

echo
echo "== bind address =="
BIND="$(ss -ltn | grep -E ":$PORT\b")"
case "$BIND" in
  "$TUNNEL:$PORT"*|*" $TUNNEL:$PORT "*) vok "bound to $TUNNEL:$PORT only" ;;
  *) vbad "unexpected bind: $BIND" ;;
esac

echo
echo "== healthz through the tunnel =="
HEALTH="$(ssh -o ConnectTimeout=8 clino "curl -sS -H 'Host: $HOST' http://$TUNNEL:$PORT/api/healthz" 2>&1)"
echo "    $HEALTH"
case "$HEALTH" in
  *'"ok":true'*) vok "healthz ok:true" ;;
  *) vbad "healthz did not report ok:true" ;;
esac

echo
echo "== routes =="
for pair in "/:200" "/app:301" "/docs:301" "/app/:200"; do
  path="${pair%:*}"; want="${pair#*:}"
  got="$(ssh -o ConnectTimeout=8 clino "curl -sS -o /dev/null -w '%{http_code}' -H 'Host: $HOST' http://$TUNNEL:$PORT$path" 2>&1)"
  if [ "$got" = "$want" ]; then vok "$path -> $got"; else vbad "$path -> $got, want $want"; fi
done

echo
echo "== base href survived the strip =="
BASE="$(ssh -o ConnectTimeout=8 clino "curl -sS -H 'Host: $HOST' http://$TUNNEL:$PORT/app/" 2>&1 | grep -o '<base href="[^"]*">')"
if [ "$BASE" = '<base href="/app/">' ]; then vok "$BASE"; else vbad "base href: '$BASE', want '<base href=\"/app/\">'"; fi

if [ "$FULL_VERIFY" -eq 1 ]; then
  echo
  echo "== X-Forwarded-For trust list =="
  ssh -o ConnectTimeout=8 clino "curl -sS -H 'Host: $HOST' -H 'X-Forwarded-For: 203.0.113.9' http://$TUNNEL:$PORT/api/healthz > /dev/null" 2>&1
  sleep 1
  LOGLINE="$(dc logs --tail 5 traefik 2>&1 | grep '/api/healthz' | tail -1)"
  case "$LOGLINE" in
    "203.0.113.9"*) vok "ClientHost is 203.0.113.9: $LOGLINE" ;;
    *) vbad "ClientHost not 203.0.113.9: $LOGLINE" ;;
  esac

  echo
  echo "== public HTTPS =="
  PUB="$(curl -sS --max-time 15 "https://$HOST/api/healthz" 2>&1)"
  echo "    $PUB"
  case "$PUB" in
    *'"ok":true'*) vok "public healthz ok:true" ;;
    *) vbad "public healthz did not report ok:true" ;;
  esac
fi

echo
if [ "$vfails" -eq 0 ]; then
  printf '\033[32mverification passed\033[0m\n'
  exit 0
else
  printf '\033[31m%d verification check(s) failed\033[0m\n' "$vfails"
  exit 1
fi
