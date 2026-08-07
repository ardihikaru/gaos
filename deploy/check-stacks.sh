#!/usr/bin/env bash
# Assert the things a running stack will never tell you.
#
# A dev stack pointed at the production database starts cleanly, passes every
# smoke test and is completely invisible at runtime. So is a Traefik that
# publishes on 0.0.0.0 instead of the tunnel address. Both are one missing
# prefix away, and neither shows up in `docker compose ps`.
#
# This script is the mechanical version of reading both files side by side. Run
# it before every deploy and in CI.
#
#   ./deploy/check-stacks.sh
set -u

cd "$(dirname "$0")/.." || exit 1

DEV=deploy/stacks/compose.dev.yaml
PROD=deploy/stacks/compose.prod.yaml
fails=0

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails + 1)); }

has()     { grep -qF "$2" "$1" && ok "$3" || bad "$3"; }
hasnt()   { grep -qF "$2" "$1" && bad "$3" || ok "$3"; }
# Regex variants. Needed because grep -F takes a bracket expression literally,
# which makes an absence check pass for the wrong reason -- the quietest kind of
# broken test there is.
hasre()   { grep -qE "$2" "$1" && ok "$3" || bad "$3"; }
hasntre() { grep -qE "$2" "$1" && bad "$3" || ok "$3"; }

echo "== the two tiers never name each other's infrastructure =="
# This is the expensive one. Everything else on this list is loud when it breaks.
hasnt $DEV  "master-compose-ai-net"          "dev does not join the PROD infra network"
has   $DEV  "master-compose-ai-dev-net"      "dev joins the dev infra network"
has   $PROD "master-compose-ai-net"          "prod joins the prod infra network"
hasnt $PROD "master-compose-ai-dev-net"      "prod does not join the DEV infra network"
hasnt $DEV  "http://arcadedb-master:2480"    "dev does not point at the PROD database"
has   $DEV  "http://arcadedb-master-dev:2480" "dev points at the dev database"
has   $PROD "http://arcadedb-master:2480"    "prod points at the prod database"
hasnt $PROD "arcadedb-master-dev"            "prod does not point at the DEV database"

echo
echo "== nothing is published beyond the tunnel =="
# Without the 10.10.0.2 prefix this binds every interface the host has -- the
# LAN and the site-to-site link included -- and nothing says so.
has   $DEV  '"10.10.0.2:19301:80"'           "dev publishes on the tunnel address only"
has   $PROD '"10.10.0.2:18301:80"'           "prod publishes on the tunnel address only"
for f in $DEV $PROD; do
  n=$(grep -c '^\s*- "' "$f")
  [ "$n" = 1 ] && ok "$(basename "$f"): exactly one ports: mapping" \
                || bad "$(basename "$f"): $n ports: mappings, want 1"
done

echo
echo "== the shared infra network is joined by the dashboard alone =="
# Least privilege as topology: the proxy, the landing page and the docs have no
# reason to resolve a neighbour's MySQL, Qdrant or RabbitMQ.
for f in $DEV $PROD; do
  n=$(grep -c 'networks: \[internal, infra\]' "$f")
  [ "$n" = 1 ] && ok "$(basename "$f"): one service on the infra network" \
                || bad "$(basename "$f"): $n services on the infra network, want 1"
done

echo
echo "== project names and subnets are distinct =="
has   $DEV  "name: gaos-dev"                 "dev compose project is gaos-dev"
has   $PROD "name: gaos-prod"                "prod compose project is gaos-prod"
has   $DEV  "172.30.13.0/24"                 "dev subnet is pinned to 172.30.13.0/24"
has   $PROD "172.30.12.0/24"                 "prod subnet is pinned to 172.30.12.0/24"
# 172.30.10/11 belong to the neighbouring product on this host.
hasntre $DEV  "172\\.30\\.1[01]\\.0/24"          "dev does not reuse a neighbour's subnet"
hasntre $PROD "172\\.30\\.1[01]\\.0/24"          "prod does not reuse a neighbour's subnet"

echo
echo "== what must NOT differ between the tiers =="
# The Secure cookie flag is env-driven, never scheme-derived: both tiers sit
# behind real TLS at the edge and see plain HTTP inside the tunnel. Flipping it
# off in dev "because it is dev" breaks the back office there and nowhere else.
has   $DEV  'GAOS_COOKIE_SECURE: "1"'        "dev keeps Secure cookies on"
has   $PROD 'GAOS_COOKIE_SECURE: "1"'        "prod keeps Secure cookies on"
has   $DEV  'GAOS_ARCADE_DB: "gaos"'         "dev uses the gaos database"
has   $PROD 'GAOS_ARCADE_DB: "gaos"'         "prod uses the gaos database"

echo
echo "== the proxy never holds the Docker socket =="
hasnt $DEV  "/var/run/docker.sock"           "dev traefik has no Docker socket"
hasnt $PROD "/var/run/docker.sock"           "prod traefik has no Docker socket"
hasnt deploy/stacks/traefik.yml "docker:"    "the tunnelled proxy uses the file provider"

echo
echo "== no secret is committed =="
for f in deploy/stacks/*.env; do
  [ -e "$f" ] || continue
  case "$f" in *.example) continue;; esac
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    bad "$f is tracked by git"
  else
    ok "$(basename "$f") is untracked"
  fi
done
hasnt $DEV  'PASSWORD: "'                    "dev bakes no password into compose"
hasnt $PROD 'PASSWORD: "'                    "prod bakes no password into compose"

echo
echo "== the trailing-slash redirects exist =="
# Without these the <base href> resolves one level up and every asset 404s,
# while the document itself still answers 200. Silent, browser-side, invisible
# to any status-code check.
has deploy/traefik/dynamic.yml "/app$"       "the /app trailing-slash redirect is present"
has deploy/traefik/dynamic.yml "/docs$"      "the /docs trailing-slash redirect is present"
has deploy/traefik/dynamic.yml "priority: 1" "the landing catch-all has the lowest priority"

echo
if [ "$fails" -eq 0 ]; then
  printf '\033[32mall checks passed\033[0m\n'
else
  printf '\033[31m%d check(s) failed\033[0m\n' "$fails"
fi
exit $((fails > 0))
