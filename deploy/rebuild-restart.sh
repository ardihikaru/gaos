#!/usr/bin/env bash
# Rebuild/restart/status one gaos tier (dev or prod), the whole stack or one
# named service. The deterministic half of the gaos-deploy tooling -- the
# subagent calls this instead of free-handing docker commands, so behaviour
# doesn't drift between runs. See deploy/STACKS.md for the topology this
# encodes and .claude/agents/gaos-deploy.md for who calls it.
#
#   deploy/rebuild-restart.sh <dev|prod> <deploy|restart|status> [service] \
#       [--tag <ref> | --from-tree] [--allow-dirty] [--full-verify]
#
#   deploy   build [service] then up -d [service] -- rebuild + restart
#   restart  restart [service], no build at all -- restart-only
#   status   docker compose ps + one healthz probe, no mutation
#
# service, if given, is one of: traefik landing dashboard docs
#
# PROD BUILDS FROM A TAG. --tag <ref> checks that ref out into a detached
# worktree and re-runs this script inside it, so every downstream step -- the
# invariant check, the test gate, docker compose build -- reads the tagged tree
# and not whatever happens to be edited right now. prod refuses without it;
# --from-tree is the deliberate escape hatch. dev is unchanged by default.
set -u

cd "$(dirname "$0")/.." || exit 1

SERVICES="traefik landing dashboard docs"
ALLOW_DIRTY=0
FULL_VERIFY=0
ENVIRON=""
ACTION=""
SERVICE=""
TAG=""
FROM_TREE=0
WANT_TAG=0
# The arguments to hand the inner invocation, which is everything EXCEPT the
# provenance flags. It must not be told --tag: the tree it runs in is already
# the tagged one, and the script it runs is that tag's copy of this file, which
# may predate these flags entirely. Dropping them is what lets --tag build a ref
# older than the feature itself.
FORWARD=()
# Set by the outer invocation before it re-execs. Its presence means "you are
# already inside the worktree" and is what stops this recursing forever.
INSIDE="${GAOS_DEPLOY_FROM_WORKTREE:-}"

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
info() { printf '  %s\n' "$1"; }

usage() {
  echo "usage: deploy/rebuild-restart.sh <dev|prod> <deploy|restart|status> [service] [--tag <ref> | --from-tree] [--allow-dirty] [--full-verify]"
  echo "  service, if given, is one of: $SERVICES"
  echo "  --tag <ref>   build from a detached worktree at <ref> -- required for prod"
  echo "  --from-tree   build from the working tree instead (prod: deliberate, and says so)"
  exit 2
}

for arg in "$@"; do
  if [ "$WANT_TAG" -eq 1 ]; then TAG="$arg"; WANT_TAG=0; continue; fi
  case "$arg" in
    --tag) WANT_TAG=1 ;;
    --tag=*) TAG="${arg#--tag=}" ;;
    --from-tree) FROM_TREE=1 ;;
    --allow-dirty) ALLOW_DIRTY=1; FORWARD+=("$arg") ;;
    --full-verify) FULL_VERIFY=1; FORWARD+=("$arg") ;;
    dev|prod)
      [ -n "$ENVIRON" ] && { echo "environment given twice"; usage; }
      ENVIRON="$arg"; FORWARD+=("$arg") ;;
    deploy|restart|status)
      [ -n "$ACTION" ] && { echo "action given twice"; usage; }
      ACTION="$arg"; FORWARD+=("$arg") ;;
    *)
      [ -n "$SERVICE" ] && { echo "unexpected extra argument: $arg"; usage; }
      SERVICE="$arg"; FORWARD+=("$arg") ;;
  esac
done

[ "$WANT_TAG" -eq 1 ] && { echo "--tag needs a ref"; usage; }
[ -n "$TAG" ] && [ "$FROM_TREE" -eq 1 ] && { echo "--tag and --from-tree contradict each other"; usage; }
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
  # A second prod instance (rnd-v3-vpn, gaos.sismedika.com) overrides several things. Pass ALL of
  # them or the stack comes up broken (a missing one is a failed port bind or an ArcadeDB crash loop):
  #   GAOS_TUNNEL_IP=10.10.0.9            bind address (default 10.10.0.2 does not exist on rnd-v3)
  #   GAOS_HOST=gaos.sismedika.com        host the route check expects
  #   GAOS_ARCADE_URL=http://10.10.0.4:9480   the ArcadeDB on rnd-v2 (default is clinodb's, wrong user)
  #   GAOS_ARCADE_DB=gaos_sismedika       its database (the user in prod.dashboard.env owns only this one)
  #   GAOS_UID=1000 GAOS_GID=1000         owner of the secret files
  #   GAOS_QA_URL=http://10.10.0.9:8140 GAOS_API_QA_PRODUCTS=p-6   the QA tool's address and scope
  # The post-deploy checks that `ssh clino` fail when run on rnd-v3 itself: probe
  # http://10.10.0.9:18301/api/healthz and https://gaos.sismedika.com/api/healthz by hand instead.
  HOST="${GAOS_HOST:-gaos.mfardiansyah.id}"
fi
TUNNEL="${GAOS_TUNNEL_IP:-10.10.0.2}"

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

# ---- provenance: decide WHICH TREE the rest of this script reads ----
#
# Everything below -- check-stacks.sh, the test gate, docker compose build -- is
# relative to $PWD, and both compose files address their build contexts
# relatively (context: ../../dashboard). So checking a ref out into a worktree
# and re-running this script from inside it is the whole mechanism: no path
# rewriting, no second code path, and the tier that gets built is the tier that
# was verified.
#
# Runs only on the outer invocation. `status` never reaches here -- it returned
# above, because reading a running stack has nothing to do with which source
# built it.
if [ -z "$INSIDE" ]; then
  echo "== provenance =="
  if [ -n "$TAG" ]; then
    # Resolve BEFORE creating anything. A typo should cost nothing, and
    # `worktree add` on a bad ref leaves a half-made directory behind.
    SHA="$(git rev-parse --verify --quiet "$TAG^{commit}")" || SHA=""
    if [ -z "$SHA" ]; then
      bad "no such ref: $TAG"
      info "nothing was created. \`git tag\` lists what is available."
      exit 1
    fi
    WT=".worktrees/$(printf '%s' "$TAG" | tr '/ ' '--')"
    if [ -d "$WT" ] && [ "$(git -C "$WT" rev-parse HEAD 2>/dev/null)" = "$SHA" ]; then
      ok "worktree already at $TAG ($(git rev-parse --short "$SHA"))"
    else
      [ -e "$WT" ] && git worktree remove --force "$WT" >/dev/null 2>&1
      git worktree prune
      git worktree add --detach "$WT" "$SHA" >/dev/null 2>&1 || {
        bad "could not create a worktree at $TAG"
        exit 1
      }
      ok "worktree at $TAG ($(git rev-parse --short "$SHA")) -> $WT"
    fi

    # The tier's env files are secrets: chmod 600, created by hand, never
    # committed -- so they are absent from a fresh worktree and compose would
    # stop at "env file not found". Symlinked rather than copied, so a secret
    # exists once on this disk and a rotation cannot leave a stale duplicate
    # behind in a worktree nobody removed.
    # The gateway's two secret files (deploy/stacks/<tier>.aim-*) are the same
    # kind of thing and are linked the same way.
    # D-072: a tag whose compose mounts the machine API's token files needs them
    # on this disk BEFORE `up`. A missing bind-mount source is created by Docker
    # as a root-owned directory the deploy user cannot then replace. An EMPTY
    # file is fine (it leaves that token unset and the API closed).
    if [ "$ENVIRON" = prod ] && grep -q 'prod.api-admin' "$WT/deploy/stacks/compose.prod.yaml" 2>/dev/null; then
      for f in prod.api-admin prod.api-qa prod.api-qa-out; do
        if [ ! -f "deploy/stacks/$f" ]; then
          bad "deploy/stacks/$f is missing (or is not a regular file)"
          info "create it, mode 600 (empty is fine) -- see compose.prod.yaml -- then re-run."
          exit 1
        fi
      done
      ok "the machine API's token files exist"
    fi
    for e in deploy/stacks/*.env deploy/stacks/*.aim-* deploy/stacks/*.api-*; do
      [ -e "$e" ] || continue
      case "$e" in *.example) continue;; esac
      ln -sfn "$PWD/$e" "$WT/$e"
    done
    ok "env files linked from the working tree (they are not in any commit)"

    ok "re-running inside the worktree"
    echo
    GAOS_DEPLOY_FROM_WORKTREE="$TAG" exec "$PWD/$WT/deploy/rebuild-restart.sh" "${FORWARD[@]}"
  elif [ "$ENVIRON" = prod ] && [ "$FROM_TREE" -eq 0 ]; then
    bad "prod builds from a tag -- refusing to $ACTION from the working tree"
    echo
    echo "  Pass --tag <ref> so what runs in production is a ref you can name"
    echo "  later, or --from-tree if building the checkout is deliberate."
    echo "  Available: $(git tag | tr '\n' ' ')"
    exit 1
  else
    ok "building from the working tree ($(git rev-parse --short HEAD))"
  fi
  echo
else
  echo "== provenance =="
  ok "inside the worktree at $INSIDE ($(git rev-parse --short HEAD))"
  echo
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

# Only on the working-tree path. A detached worktree at a ref is clean by
# construction, so this check has nothing left to say once --tag was used.
if [ "$ENVIRON" = prod ] && [ -z "$INSIDE" ]; then
  DIRTY="$(git status --porcelain)"
  if [ -n "$DIRTY" ]; then
    if [ "$ALLOW_DIRTY" -eq 1 ]; then
      info "working tree is dirty -- proceeding, --allow-dirty was passed"
      printf '%s\n' "$DIRTY" | sed 's/^/    /'
    else
      bad "working tree is dirty -- refusing to touch prod"
      printf '%s\n' "$DIRTY" | sed 's/^/    /'
      echo
      echo "  --from-tree builds this checkout as it stands. Commit or stash"
      echo "  first, pass --tag <ref> to build a named ref instead, or pass"
      echo "  --allow-dirty if this is intentional."
      exit 1
    fi
  else
    ok "working tree is clean"
  fi
fi

# run_go runs a go command in a module directory, with or without a local Go.
#
# The VM these stacks live on has Docker and no Go toolchain, anywhere -- not on
# PATH, not under a login shell, not in /usr/local/go. That made the test gate
# below unpassable there and it has no bypass flag, so the documented deploy path
# could not complete on the machine it deploys to, and the workaround was to run
# docker compose by hand -- exactly the thing .claude/agents/gaos-deploy.md tells
# the subagent never to do. A gap in the tooling that forces the rule to be
# broken is a gap in the tooling.
#
# So: native go when there is one, the pinned golang image when there is not.
# GOCACHE is inside the container because /src is a bind mount of the repo and a
# build cache does not belong in it. GOFLAGS carries through so a caller can pass
# -count=1 without knowing which arm ran.
#
# If neither exists this fails loudly and the deploy stops. A gate that skips
# itself when it cannot run is worse than one that cannot run, because the output
# still says the tests were part of the deploy.
GO_IMAGE="${GAOS_GO_IMAGE:-golang:1.24-alpine}"

if command -v go >/dev/null 2>&1; then
  GO_MODE="local $(go env GOVERSION 2>/dev/null || echo go)"
  run_go() { ( cd "$1" && shift && go "$@" ); }
elif command -v docker >/dev/null 2>&1; then
  GO_MODE="docker $GO_IMAGE"
  run_go() {
    local mod="$1"; shift
    docker run --rm \
      -v "$PWD":/src -w "/src/$mod" \
      -e GOCACHE=/tmp/gocache -e GOFLAGS="${GOFLAGS:-}" \
      "$GO_IMAGE" go "$@"
  }
else
  GO_MODE=""
  run_go() { return 127; }
fi

if [ "$ACTION" = deploy ]; then
  echo
  echo "== tests =="
  if [ -z "$GO_MODE" ]; then
    bad "no go toolchain and no docker -- the test gate cannot run"
    info "install Go, or install Docker so the gate can run in $GO_IMAGE"
    exit 1
  fi
  ok "go toolchain: $GO_MODE"
  run_go dashboard test ./... || { bad "dashboard tests failed"; exit 1; }
  ok "dashboard tests passed"
  run_go pustaka test ./... || { bad "pustaka tests failed"; exit 1; }
  ok "pustaka tests passed"
  run_go pustaka run . check ./docs || { bad "pustaka docs check failed"; exit 1; }
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
# POLLED, not probed once. The dashboard applies its schema before it listens,
# and since the database moved to clinodb (2026-11-02) that is 365 statements
# over the tunnel -- seconds, not milliseconds. A single probe straight after
# `up -d` reported a healthy deploy as three failures every time. Up to
# GAOS_HEALTH_WAIT seconds (default 90); a dashboard still down after that is
# a real failure, and says so below.
HEALTH=""
waited=0
while :; do
  HEALTH="$(ssh -o ConnectTimeout=8 clino "curl -sS -m 5 -H 'Host: $HOST' http://$TUNNEL:$PORT/api/healthz" 2>&1)"
  case "$HEALTH" in *'"ok":true'*) break ;; esac
  [ "$waited" -ge "${GAOS_HEALTH_WAIT:-90}" ] && break
  sleep 3
  waited=$((waited + 3))
done
echo "    $HEALTH  (after ${waited}s)"
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
