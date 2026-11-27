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
# Block helpers (D-107). svcblock prints the lines of $PROD after "  <name>:" up to,
# not including, the next line indented by at most two spaces (the next service or
# top-level block). The others test that block and return a status; the caller
# combines them in one if and prints one ok or one FAIL.
svcblock() {
  awk -v n="  $1:" 'f && (/^[a-z]/ || /^ [a-z]/ || /^  [a-z]/) {exit} f {print} $0 == n {f = 1}' "$PROD"
}
inblock()      { svcblock "$1" | grep -qF -e "$2"; }
inblockre()    { svcblock "$1" | grep -qE -e "$2"; }
notinblock()   { ! inblock "$1" "$2"; }
notinblockre() { ! inblockre "$1" "$2"; }

echo "== the two tiers never name each other's database =="
# This is the expensive one. Everything else on this list is loud when it breaks.
# Since 2026-11-02 both tiers use ONE ArcadeDB server on clinodb, reached over
# WireGuard (deploy/clinodb/), so the boundary is the database and its user:
# gaos_dev / gaos_dev and gaos / gaos_prod, each user refused on the other's.
has   $DEV  'GAOS_ARCADE_DB: "gaos_dev"'      "dev uses the gaos_dev database"
hasnt $DEV  'GAOS_ARCADE_DB: "gaos"'           "dev does not use the PROD database"
has   $PROD 'GAOS_ARCADE_DB: "${GAOS_ARCADE_DB:-gaos}"' "prod uses the gaos database (overridable per instance)"
hasnt $PROD 'GAOS_ARCADE_DB: "gaos_dev"'       "prod does not use the DEV database"
has   $DEV  '"http://10.10.0.5:2480"'          "dev reaches the database over WireGuard"
has   $PROD '"${GAOS_ARCADE_URL:-http://10.10.0.5:2480}"' "prod reaches the database over WireGuard"

echo
echo "== nothing is published beyond the tunnel =="
# Without the 10.10.0.2 prefix this binds every interface the host has -- the
# LAN and the site-to-site link included -- and nothing says so.
has   $DEV  '"10.10.0.2:19301:80"'           "dev publishes on the tunnel address only"
has   $PROD '"${GAOS_TUNNEL_IP:-10.10.0.2}:18301:80"' "prod publishes on the tunnel address only"
for f in $DEV $PROD; do
  n=$(grep -c '^\s*- "' "$f")
  [ "$n" = 1 ] && ok "$(basename "$f"): exactly one ports: mapping" \
                || bad "$(basename "$f"): $n ports: mappings, want 1"
done

echo
echo "== no service joins a neighbour's network =="
# Least privilege as topology: since the database moved to clinodb nothing here
# needs to resolve a neighbour's MySQL, Qdrant or RabbitMQ by name, so no service
# joins a shared infra network at all -- and one appearing is a regression.
for f in $DEV $PROD; do
  n=$(grep -cE 'master-compose-ai|networks: \[[^]]*infra' "$f")
  [ "$n" = 0 ] && ok "$(basename "$f"): no service on a shared infra network" \
                || bad "$(basename "$f"): $n line(s) join a shared infra network, want none"
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
# Both tiers run the graph store. A tier that silently fell back to the
# in-memory fixtures would serve a floor, answer /healthz with ok:true and lose
# every write on restart -- which is precisely the failure BR-01 is about, and
# it is invisible from outside without this check.
has   $DEV  'GAOS_STORE: "arcadedb"'         "dev runs the arcadedb store"
has   $PROD 'GAOS_STORE: "arcadedb"'         "prod runs the arcadedb store"

echo
echo "== the proxy never holds the Docker socket =="
hasnt $DEV  "/var/run/docker.sock"           "dev traefik has no Docker socket"
hasnt $PROD "/var/run/docker.sock"           "prod traefik has no Docker socket"
hasnt deploy/stacks/traefik.yml "docker:"    "the tunnelled proxy uses the file provider"
# D-106 (TC-883): forwarded headers (X-Real-IP, X-Forwarded-For) are trusted from the edge alone,
# so no tunnel peer and no process on a GAOS VM can pick the Basic routes' lock source.
if [ "$(grep -cE '^[[:space:]]*- "[0-9./]+"' deploy/stacks/traefik.yml)" = 1 ] && grep -qF -e '- "10.10.0.1/32"' deploy/stacks/traefik.yml; then
  ok "traefik trusts forwarded headers from the edge (10.10.0.1/32) only"
else
  bad "traefik trusts forwarded headers from the edge (10.10.0.1/32) only"
fi
hasnt deploy/stacks/traefik.yml '"10.10.0.0/24"' "traefik does not trust the whole tunnel"
hasnt deploy/stacks/traefik.yml '"10.10.0.2/32"' "traefik does not trust the first GAOS VM"
hasnt deploy/stacks/traefik.yml '"10.10.0.9/32"' "traefik does not trust the second GAOS VM"

echo
echo "== the traffic store =="
# D-107 (TC-902): prod's Redis holds the Traffic page's capped streams. It persists
# nothing, publishes nothing, sits on its own internal network with the dashboard
# alone at a pinned address, turns the default user off, and admits the gaos user
# to the traffic keys and three data commands. Prod only: dev is retired.
TRS=deploy/stacks/traffic-redis.sh
if grep -qE '^[[:space:]]+image: redis:[0-9]+\.[0-9]+\.[0-9]+-alpine@sha256:[0-9a-f]{64}$' $PROD; then
  ok "prod pins the Redis image by version and digest"
else
  bad "prod pins the Redis image by version and digest"
fi
if grep -qF -e '--save "" --appendonly no' $TRS \
   && inblock redis 'volumes: ["./traffic-redis.sh:/usr/local/bin/traffic-redis.sh:ro", "./prod.traffic-redis-auth:/run/secrets/traffic-redis-auth:ro"]' \
   && inblock redis 'tmpfs: ["/data:uid=${GAOS_UID:-1001},gid=${GAOS_GID:-1001},mode=0700"]'; then
  ok "prod's Redis persists nothing"
else
  bad "prod's Redis persists nothing"
fi
if grep -qF -e '--maxmemory 64mb' $TRS && grep -qF -e '--maxmemory-policy noeviction' $TRS; then
  ok "prod's Redis has a memory limit and refuses writes rather than evicting a stream"
else
  bad "prod's Redis has a memory limit and refuses writes rather than evicting a stream"
fi
if grep -qF 'user default off' $TRS \
   && grep -qF "user gaos on #%s ~gaos:traffic:* resetchannels -@all +ping +xadd +xtrim +xrevrange\n'" $TRS \
   && grep -qF -e '--aclfile /data/users.acl' $TRS \
   && inblock redis 'entrypoint: ["/bin/sh", "/usr/local/bin/traffic-redis.sh"]' \
   && ! grep -qE '(requirepass|REDIS_PASSWORD|nopass)' $PROD; then
  ok "prod's Redis turns the default user off and admits the gaos user to the traffic keys and three commands"
else
  bad "prod's Redis turns the default user off and admits the gaos user to the traffic keys and three commands"
fi
if notinblockre redis '^[[:space:]]*(ports|expose):'; then
  ok "prod's Redis publishes no port"
else
  bad "prod's Redis publishes no port"
fi
on_traffic=0
for svc in $(awk '/^services:/ {f = 1; next} f && /^[a-z]/ {f = 0} f && /^  [a-z][a-z0-9_-]*:$/ {sub(/:$/, ""); sub(/^  /, ""); print}' $PROD); do
  svcblock "$svc" | grep -qF traffic && on_traffic=$((on_traffic + 1))
done
if inblock redis 'traffic: {ipv4_address: 172.30.14.2}' \
   && notinblockre redis '^[[:space:]]+internal' \
   && notinblock redis 'network_mode' \
   && inblock traffic 'name: gaos-prod-traffic' \
   && inblock traffic 'internal: true' \
   && inblock traffic 'subnet: 172.30.14.0/29' \
   && inblock traffic 'ip_range: 172.30.14.4/30' \
   && [ "$on_traffic" = 2 ]; then
  ok "prod's Redis is on the internal traffic network only, at its pinned address outside the dynamic range"
else
  bad "prod's Redis is on the internal traffic network only, at its pinned address outside the dynamic range"
fi
if inblock redis '/usr/local/bin/traffic-redis.sh", "health"]'; then
  ok "prod's Redis has an authenticated healthcheck"
else
  bad "prod's Redis has an authenticated healthcheck"
fi
if inblock dashboard 'networks: [internal, traffic]' \
   && grep -qF 'GAOS_REDIS_ADDR: "172.30.14.2:6379"' $PROD \
   && grep -qF 'GAOS_REDIS_AUTH_FILE: "/run/secrets/traffic-redis-auth"' $PROD \
   && inblock dashboard './prod.traffic-redis-auth:/run/secrets/traffic-redis-auth:ro'; then
  ok "prod's dashboard joins the traffic network and reaches Redis by address, with the shared password file"
else
  bad "prod's dashboard joins the traffic network and reaches Redis by address, with the shared password file"
fi
edge_go="$(grep -oE '^const edgeHop = "[0-9.]+"' dashboard/traffic.go | grep -oE '[0-9.]+')"
edge_traefik="$(grep -E '^[[:space:]]*- "[0-9./]+"' deploy/stacks/traefik.yml | grep -oE '[0-9./]+')"
if [ -n "$edge_go" ] && [ "$edge_go/32" = "$edge_traefik" ]; then
  ok "the dashboard's edge hop is the one address Traefik trusts"
else
  bad "the dashboard's edge hop is the one address Traefik trusts"
fi
if ! grep -qE '^[[:space:]]*depends_on:' $PROD; then
  ok "nothing in prod has a start-order dependency"
else
  bad "nothing in prod has a start-order dependency"
fi

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
for f in deploy/stacks/*.aim-*; do
  # The gateway's two secrets (the ai-stack login, ai-manager's admin key) are
  # host-side files mounted read-only; committing one would publish both.
  [ -e "$f" ] || continue
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    bad "$f is tracked by git"
  else
    ok "$(basename "$f") is untracked"
  fi
done
for f in deploy/stacks/*.api-*; do
  # The machine API's bearer tokens (D-072): host-side files mounted read-only.
  # prod.api-clients is the clients DIRECTORY (D-099): mode 700, its files 600. In a tag
  # worktree it is a symlink to the checkout's directory, so every test follows the link.
  [ -e "$f" ] || continue
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    bad "$f is tracked by git"
  else
    ok "$(basename "$f") is untracked"
  fi
  if [ "$(basename "$f")" = prod.api-clients ]; then
    if [ ! -d "$f" ]; then
      bad "$f is not a directory (the API clients file and tokens live in it)"
    elif [ "$(stat -L -c %a "$f")" != 700 ]; then
      bad "$f is mode $(stat -L -c %a "$f"), want 700"
    else
      open_files=$(find -L "$f/" -type f ! -perm 600 2>/dev/null | wc -l | tr -d ' ')
      if [ "$open_files" != 0 ]; then
        bad "$f holds $open_files file(s) that are not mode 600"
      else
        ok "prod.api-clients is a mode-700 directory of mode-600 files"
      fi
    fi
    continue
  fi
  if [ ! -f "$f" ]; then
    bad "$f is not a regular file (a missing bind-mount source is created by Docker as a root-owned directory)"
  fi
done
for f in deploy/stacks/*.bo-*; do
  # The back office's accounts file and session key (D-079): host-side files.
  [ -e "$f" ] || continue
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    bad "$f is tracked by git"
  else
    ok "$(basename "$f") is untracked"
  fi
  if [ ! -f "$f" ]; then
    bad "$f is not a regular file (a missing bind-mount source is created by Docker as a root-owned directory)"
  fi
done
for f in deploy/stacks/*.bt-*; do
  # The Brain Town receiver's token (D-086): a host-side file, never tracked.
  # prod.bt-state is the reader's writable cursor DIRECTORY (v0.60.1): mode 700.
  [ -e "$f" ] || continue
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    bad "$f is tracked by git"
  else
    ok "$(basename "$f") is untracked"
  fi
  if [ "$(basename "$f")" = prod.bt-state ]; then
    if [ ! -d "$f" ]; then
      bad "$f is not a directory (the reader's cursor lives in it)"
    elif [ "$(stat -L -c %a "$f")" != 700 ]; then
      bad "$f is mode $(stat -L -c %a "$f"), want 700"
    else
      ok "prod.bt-state is a mode-700 directory"
    fi
    continue
  fi
  if [ ! -f "$f" ]; then
    bad "$f is not a regular file (a missing bind-mount source is created by Docker as a root-owned directory)"
  fi
done
has $PROD 'prod.api-clients:/run/secrets/api-clients:ro' "prod mounts the API clients directory read-only"
has $PROD 'GAOS_API_CLIENTS_FILE: "/run/secrets/api-clients/clients"' "prod reads the API clients file from the clients directory"
has $PROD 'GAOS_API_AUTORUN_PRODUCTS: "${GAOS_API_AUTORUN_PRODUCTS:-}"' "prod passes the unattended-start list from the deploy shell, empty (off) by default"
has $PROD 'GAOS_PR_TEMPLATE_PRODUCTS: "${GAOS_PR_TEMPLATE_PRODUCTS:-}"' "prod passes the PR-template product list from the deploy shell, empty (off) by default"
has $PROD 'prod.bt-token:/run/secrets/bt-token:ro' "prod mounts the Brain Town token file read-only"
has $PROD 'BRAIN_TOWN_ENABLED: "${BRAIN_TOWN_ENABLED:-}"' "prod passes the Brain Town switch from the deploy shell, empty (off) by default"
hasnt $PROD 'BRAIN_TOWN_TOKEN:' "prod passes the Brain Town token only as a file, never as an environment variable"
has $PROD 'prod.bt-consumer-token:/run/secrets/bt-consumer-token:ro' "prod mounts the Brain Town consumer token file read-only"
has $PROD 'prod.bt-state:/var/lib/gaos-bt' "prod mounts the Brain Town reader's cursor directory writable"
hasnt $PROD 'prod.bt-consumer-cursor' "prod no longer mounts the read-only cursor file (#56)"
has $PROD 'BRAIN_TOWN_CONSUMER_ENABLED: "${BRAIN_TOWN_CONSUMER_ENABLED:-}"' "prod passes the Brain Town reader switch from the deploy shell, empty (off) by default"
hasnt $PROD 'BRAIN_TOWN_CONSUMER_TOKEN:' "prod passes the Brain Town reader token only as a file, never as an environment variable"
for f in deploy/stacks/*.pr-*; do
  # The pull-request flow's credentials (D-088): host-side files, never tracked.
  [ -e "$f" ] || continue
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    bad "$f is tracked by git"
  else
    ok "$(basename "$f") is untracked"
  fi
  if [ ! -f "$f" ]; then
    bad "$f is not a regular file (a missing bind-mount source is created by Docker as a root-owned directory)"
  fi
done
for f in deploy/stacks/*.traffic-*; do
  # The traffic store's password (D-107, TC-902): generated by rebuild-restart.sh,
  # host-side, never tracked, mode 600.
  [ -e "$f" ] || continue
  if ! git ls-files --error-unmatch "$f" >/dev/null 2>&1 && [ -f "$f" ] \
     && [ "$(stat -L -c %a "$f")" = 600 ]; then
    ok "the traffic store's password file is untracked, a regular file, mode 600"
  else
    bad "the traffic store's password file is untracked, a regular file, mode 600"
  fi
done
has $PROD 'prod.pr-github-token:/run/secrets/pr-github-token:ro' "prod mounts the GitHub token file read-only"
hasnt $PROD 'GAOS_GITHUB_TOKEN:' "prod passes the GitHub token only as a file, never as an environment variable"
has $PROD 'prod.pr-staging-auth:/run/secrets/pr-staging-auth:ro' "prod mounts the staging callback's credential file read-only"
hasnt $PROD 'GAOS_STAGING_BASIC_AUTH:' "prod passes the staging credential only as a file, never as an environment variable"
# D-106 (TC-883): Jenkins' unit-results credential is its own file, never an environment value,
# and the build-link origin comes from the deploy shell.
has $PROD 'prod.pr-unit-results-auth:/run/secrets/pr-unit-results-auth:ro' "prod mounts the unit-results credential file read-only"
hasnt $PROD 'GAOS_UNIT_RESULTS_BASIC_AUTH:' "prod passes the unit-results credential only as a file, never as an environment variable"
has $PROD 'GAOS_JENKINS_ORIGIN: "${GAOS_JENKINS_ORIGIN:-}"' "prod passes the Jenkins origin from the deploy shell, empty (no build links) by default"
has $PROD 'prod.pr-wa-auth:/run/secrets/pr-wa-auth:ro' "prod mounts the WhatsApp login file read-only"
has $PROD 'prod.pr-wa-group:/run/secrets/pr-wa-group:ro' "prod mounts the WhatsApp pinned-group file read-only"
hasnt $PROD 'GAOS_WA_BASIC_AUTH:' "prod passes the WhatsApp login only as a file, never as an environment variable"
has $PROD 'prod.pr-github-write-token:/run/secrets/pr-github-write-token:ro' "prod mounts the auto-fix write token file read-only"
has $PROD 'prod.pr-autofix-admins:/run/secrets/pr-autofix-admins:ro' "prod mounts the auto-fix admin list read-only"
hasnt $PROD 'GAOS_GITHUB_WRITE_TOKEN:' "prod passes the auto-fix write token only as a file, never as an environment variable"
has $PROD 'prod.pr-taskmgr-auth:/run/secrets/pr-taskmgr-auth:ro' "prod mounts the TaskMgr credential file read-only"
has $PROD 'prod.pr-taskmgr-ca.pem:/run/secrets/pr-taskmgr-ca.pem:ro' "prod mounts the TaskMgr pinned certificate read-only"
hasnt $PROD 'GAOS_TASKMGR_AUTH:' "prod passes the TaskMgr credential only as a file, never as an environment variable"
has $PROD 'GAOS_TASKMGR_GRPC_ADDR: "${GAOS_TASKMGR_GRPC_ADDR:-}"' "prod passes the TaskMgr address from the deploy shell, empty (off) by default"
has $PROD 'GAOS_AUTOFIX_ALLOW_CLASSIC: "${GAOS_AUTOFIX_ALLOW_CLASSIC:-}"' "prod passes the classic-token switch from the deploy shell, empty (refused) by default"
hasnt $PROD 'GAOS_AUTOFIX_ALLOW_CLASSIC: "1"' "prod never hard-codes the classic-token switch"
has $PROD 'GAOS_WA_URL: "${GAOS_WA_URL:-}"' "prod passes the WhatsApp gateway URL from the deploy shell, empty (off) by default, with no default host"
has $PROD 'GAOS_WA_ALLOW_HTTP: "${GAOS_WA_ALLOW_HTTP:-}"' "prod passes the WhatsApp http switch from the deploy shell, empty (https only) by default"
has $PROD 'GAOS_WA_PRODUCTS: "${GAOS_WA_PRODUCTS:-}"' "prod passes the products the WhatsApp notifier serves from the deploy shell, empty (none) by default"
has $PROD 'GAOS_QA_APPROVAL_WAIT_HOURS: "${GAOS_QA_APPROVAL_WAIT_HOURS:-}"' "prod passes the QA approval wait from the deploy shell"
hasnt $PROD 'GAOS_WA_ALLOW_HTTP: "1"' "prod never hard-codes the WhatsApp http switch"
hasnt $PROD 'GAOS_WA_ALLOW_HTTP: "any"' "prod never hard-codes the WhatsApp public-http switch"
has $PROD 'GAOS_BO_LABEL' "prod passes the deployment label that binds a session to it"
# D-083: the QA tool's origin is recorded on every answer it sends; prod's
# GAOS_QA_URL is a tunnel IP, so the name has to come from the deploy shell.
has $PROD 'GAOS_QA_ORIGIN: "${GAOS_QA_ORIGIN:-}"' "prod passes the QA tool's origin from the deploy shell"
# D-080: the runner is the permission to spend. It comes from the deploy shell,
# empty (off) by default, and from nowhere else -- a second GAOS_RUNNER line in
# any spelling would be a hard-coded value.
has $PROD 'GAOS_RUNNER: "${GAOS_RUNNER:-}"' "prod passes the runner switch from the deploy shell, empty (off) by default"
[ "$(grep -c 'GAOS_RUNNER:' $PROD)" = 1 ] \
  && ok "prod sets GAOS_RUNNER in exactly one place, the pass-through" \
  || bad "prod sets GAOS_RUNNER in more than one place; the runner must come only from the deploy shell"
hasnt $DEV  'PASSWORD: "'                    "dev bakes no password into compose"
hasnt $PROD 'PASSWORD: "'                    "prod bakes no password into compose"

echo
echo "== every vendored asset ships its licence =="
# Apache-2.0 redistribution means keeping the licence text of what you bundle.
# Dockerfile.pustaka copies docs/ wholesale, so a font added without its OFL text
# ships in every image built afterwards and nothing anywhere says so. This is the
# only place that would notice.
ASSETS=pustaka/docs/assets
# One check per family, not per weight: the licence covers the family, and ten
# woff2 files would otherwise print the same three lines three times over.
SEEN_FAMILIES=""
for f in "$ASSETS"/fonts/*.woff2; do
  [ -e "$f" ] || continue
  fam="$(basename "$f")"; fam="${fam%%-latin-*}"
  case " $SEEN_FAMILIES " in *" $fam "*) continue;; esac
  SEEN_FAMILIES="$SEEN_FAMILIES $fam"
  [ -s "$ASSETS/fonts/LICENSE-$fam.txt" ] && ok "$fam has its licence text" \
                                          || bad "$fam has no LICENSE-$fam.txt"
done
[ -s "$ASSETS/vendor/LICENSE-echarts.txt" ] && ok "echarts has its licence text" \
                                            || bad "echarts has no LICENSE-echarts.txt"
# Apache 2.0 section 4(d): the NOTICE travels with the work, not just the licence.
[ -s "$ASSETS/vendor/NOTICE-echarts.txt" ] && ok "echarts has its NOTICE" \
                                           || bad "echarts has no NOTICE-echarts.txt"
# ECharts embeds d3 and its own LICENSE points at a text it does not contain.
[ -s "$ASSETS/vendor/LICENSE-d3.txt" ] && ok "the d3 subcomponent has its licence text" \
                                       || bad "echarts cites LICENSE-d3 and none is vendored"
# The sisflow board (T-165, D-063) is vendored into the dashboard image, not
# the docs, and ships under the owner's Apache-2.0 grant: the licence and the
# NOTICE recording the grant travel with it, or the board does not ship.
SISFLOW=dashboard/static/vendor/sisflow
if [ -e "$SISFLOW/board/index.js" ]; then
  [ -s "$SISFLOW/LICENSE-sisflow.txt" ] && ok "sisflow has its licence text" \
                                        || bad "sisflow is vendored without LICENSE-sisflow.txt"
  [ -s "$SISFLOW/NOTICE-sisflow.txt" ] && ok "sisflow has its NOTICE" \
                                       || bad "sisflow is vendored without NOTICE-sisflow.txt"
fi

echo
echo "== the trailing-slash redirects exist =="
# Without these the <base href> resolves one level up and every asset 404s,
# while the document itself still answers 200. Silent, browser-side, invisible
# to any status-code check.
has deploy/traefik/dynamic.yml "/app$"       "the /app trailing-slash redirect is present"
has deploy/traefik/dynamic.yml "/docs$"      "the /docs trailing-slash redirect is present"
has deploy/traefik/dynamic.yml "priority: 1" "the landing catch-all has the lowest priority"

echo
echo "== the landing page's two languages cover the same keys =="
# English is the text in landing-page/index.html; Indonesian is one line per
# key in landing-page/js/i18n.js. A key the page uses and the dictionary lacks
# shows English in the middle of an Indonesian page; a key the dictionary keeps
# and the page dropped is a translation nobody reads. Neither is visible unless
# you read the page in both languages, top to bottom, every release.
LP=landing-page/index.html
LD=landing-page/js/i18n.js
lp_keys=$(grep -oE 'data-i18n(-attr)?="[^"]+"' "$LP" | sed -E 's/.*="([a-z-]+:)?([^"]+)"/\2/' | sort -u)
ld_all=$(grep -oE "^[[:space:]]*'[a-z0-9.-]+':" "$LD" | tr -d " ':" | sort)
ld_keys=$(printf '%s\n' "$ld_all" | sort -u)
listed() { [ -z "$2" ] && ok "$1" || { bad "$1"; printf '%s\n' "$2" | sed 's/^/         /'; }; }
listed "every translatable string on the landing page has an Indonesian entry" \
       "$(comm -23 <(printf '%s\n' "$lp_keys") <(printf '%s\n' "$ld_keys"))"
listed "the landing page's Indonesian dictionary has no unused entry" \
       "$(comm -13 <(printf '%s\n' "$lp_keys") <(printf '%s\n' "$ld_keys"))"
listed "the landing page's Indonesian dictionary declares no key twice" \
       "$(printf '%s\n' "$ld_all" | uniq -d)"
# The guard against all three passing because the extraction matched nothing.
n=$(printf '%s\n' "$lp_keys" | grep -c .)
[ "$n" -ge 100 ] && ok "the landing page declares at least 100 translatable keys" \
                 || bad "the landing page declares $n translatable keys, want at least 100"

echo
echo "== the landing page's figures match the schema and the event catalog =="
# The page once said 71 types and 49 events for months after both had moved:
# the figures were typed in by hand and nothing recounted them. Each figure is
# now the whole text of an element marked data-fig, and is compared with the
# count taken from the code. The line start matters -- a bare grep for
# 'CREATE EDGE TYPE' also counts a comment in 09-indexes.sql.
fig() { grep -oE "data-fig=\"$1\">[0-9]+<" "$LP" | grep -oE '[0-9]+' | head -n 1; }
n_vertex=$(grep -hE '^[[:space:]]*CREATE VERTEX TYPE'   dashboard/schema/*.sql | wc -l | tr -d ' ')
n_edge=$(grep -hE   '^[[:space:]]*CREATE EDGE TYPE'     dashboard/schema/*.sql | wc -l | tr -d ' ')
n_doc=$(grep -hE    '^[[:space:]]*CREATE DOCUMENT TYPE' dashboard/schema/*.sql | wc -l | tr -d ' ')
n_event=$(grep -cE  '^[[:space:]]*[A-Za-z]+[[:space:]]+EventType = "' dashboard/events.go)
figure() { # figure <data-fig name> <what the page calls it> <computed> <where it is declared>
  local said; said=$(fig "$1")
  [ "${said:-none}" = "$3" ] && ok  "the landing page says $3 $2, and $4 $3" \
                             || bad "the landing page says ${said:-nothing for} $2, and $4 $3"
}
figure vertex   "vertex types"   "$n_vertex" "the schema declares"
figure edge     "edge types"     "$n_edge"   "the schema declares"
figure document "document types" "$n_doc"    "the schema declares"
figure types    "graph types"    "$((n_vertex + n_edge + n_doc))" "the schema declares"
figure events   "event types"    "$n_event"  "the catalog declares"

echo
if [ "$fails" -eq 0 ]; then
  printf '\033[32mall checks passed\033[0m\n'
else
  printf '\033[31m%d check(s) failed\033[0m\n' "$fails"
fi
exit $((fails > 0))
