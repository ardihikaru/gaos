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

echo "== the two tiers never name each other's database =="
# This is the expensive one. Everything else on this list is loud when it breaks.
# Since 2026-11-02 both tiers use ONE ArcadeDB server on clinodb, reached over
# WireGuard (deploy/clinodb/), so the boundary is the database and its user:
# gaos_dev / gaos_dev and gaos / gaos_prod, each user refused on the other's.
has   $DEV  'GAOS_ARCADE_DB: "gaos_dev"'      "dev uses the gaos_dev database"
hasnt $DEV  'GAOS_ARCADE_DB: "gaos"'           "dev does not use the PROD database"
has   $PROD 'GAOS_ARCADE_DB: "gaos"'          "prod uses the gaos database"
hasnt $PROD 'GAOS_ARCADE_DB: "gaos_dev"'       "prod does not use the DEV database"
has   $DEV  '"http://10.10.0.5:2480"'          "dev reaches the database over WireGuard"
has   $PROD '"http://10.10.0.5:2480"'          "prod reaches the database over WireGuard"

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
