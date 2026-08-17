#!/usr/bin/env bash
# Apply the GAOS graph schema to an ArcadeDB instance.
#
# The DDL in dashboard/schema/ is extracted from
# artifacts/v1/gaos-arcadedb-model.html and reconciled against the pinned
# version -- 25 vertex types, 3 document types, 32 edge types and 56 indexes,
# plus the two supertypes everything extends. See dashboard/schema/
# RECONCILIATION.md for the two deviations and why they exist.
# Until this script runs against something, that schema is a document rather
# than a database, which is the state R-03 is about.
#
#   ./deploy/schema/apply.sh scratch          # a throwaway database, the safe default
#   ./deploy/schema/apply.sh dev
#   ./deploy/schema/apply.sh prod
#   ./deploy/schema/apply.sh --url http://localhost:9580 --db gaos_test
#   ./deploy/schema/apply.sh scratch --dry-run
#
# IDEMPOTENT ON PURPOSE. Every statement in the .sql files carries
# IF NOT EXISTS, so a second run is a no-op and not an error. That is the
# property worth testing: run it twice and the second run must be as quiet as
# the first. A migration you are afraid to re-run is a migration nobody runs.
#
# CREDENTIALS come from the same environment variables the dashboard reads, so
# there is one set of names to know:
#
#   GAOS_ARCADE_URL       http://arcadedb-master-dev:2480
#   GAOS_ARCADE_DB        gaos
#   GAOS_ARCADE_USER      root
#   GAOS_ARCADE_PASSWORD  -
#
# The target version is ArcadeDB 26.7.2, pinned by D-043. The model document
# warns in its own words that the SQL dialect moves between releases, which is
# why `scratch` exists and why it is the first argument you should reach for.
set -u

# The DDL lives under dashboard/schema/ rather than beside this script, and the
# reason is a constraint rather than a preference: the deployed dashboard runs
# on distroless with no shell, so this script cannot run there. The binary
# applies the schema itself from an embedded copy, and //go:embed cannot reach
# outside its own module -- so the .sql files must sit inside dashboard/. One
# copy, two readers: this script for a scratch database from a workstation, and
# the binary for dev and prod.
cd "$(dirname "$0")/../../dashboard/schema" || exit 1

TARGET=""
URL="${GAOS_ARCADE_URL:-}"
DB="${GAOS_ARCADE_DB:-gaos}"
USER="${GAOS_ARCADE_USER:-root}"
PASS="${GAOS_ARCADE_PASSWORD:-}"
DRY=0

while [ $# -gt 0 ]; do
  case "$1" in
    dev)       TARGET=dev;   URL="${URL:-http://arcadedb-master-dev:2480}"; shift ;;
    prod)      TARGET=prod;  URL="${URL:-http://arcadedb-master:2480}";     shift ;;
    scratch)   TARGET=scratch; DB="${GAOS_ARCADE_DB:-gaos_scratch}"
               URL="${URL:-http://localhost:2480}";                         shift ;;
    --url)     URL="$2";  shift 2 ;;
    --db)      DB="$2";   shift 2 ;;
    --dry-run) DRY=1;     shift ;;
    -h|--help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done

if [ -z "$TARGET" ]; then
  printf 'usage: %s <dev|prod|scratch> [--url URL] [--db NAME] [--dry-run]\n' "$0" >&2
  exit 2
fi
if [ -z "$URL" ]; then
  printf 'no database URL: pass --url or set GAOS_ARCADE_URL\n' >&2
  exit 2
fi
URL="${URL%/}"

fails=0
applied=0
skipped=0

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails + 1)); }
info() { printf '       %s\n' "$1"; }

# ---------------------------------------------------------------- statements
#
# Strip -- comments, fold the multi-line CREATE PROPERTY declarations back
# together, and split on the statement terminator. Whitespace is collapsed
# because the wire format is JSON and SQL does not care, but the statement text
# still has to survive intact for the error message to be worth reading.
statements() {
  sed 's/--.*$//' "$1" \
    | tr '\n' ' ' \
    | tr ';' '\n' \
    | sed -e 's/[[:space:]][[:space:]]*/ /g' -e 's/^ //' -e 's/ $//' \
    | grep -v '^$'
}

# JSON string escaping, deliberately minimal. The extracted DDL contains no
# double quote and no backslash outside a comment, and comments are gone by the
# time anything reaches here -- so rather than write an escaper that would be
# wrong in some untested corner, assert the precondition and refuse loudly if a
# future statement breaks it.
json_safe() {
  case "$1" in
    *\\*|*\"*)
      printf '\n  \033[31mFAIL\033[0m statement contains a quote or backslash and this\n' >&2
      printf '       script does not escape them. Fix the escaper before landing it:\n' >&2
      printf '       %s\n' "$1" >&2
      return 1 ;;
  esac
  return 0
}

# ------------------------------------------------------------------- request
#
# POST /api/v1/command/{db} is the write path. The reachability probe the
# dashboard uses is GET /api/v1/ready, and it answers 204 rather than 200 --
# measured, and the reason dashboard/arcade.go accepts both.
send() {
  local db="$1" sql="$2" out code
  out="$(curl -sS -m 30 -w '\n%{http_code}' \
          -u "$USER:$PASS" \
          -H 'Content-Type: application/json' \
          -X POST "$URL/api/v1/command/$db" \
          -d "{\"language\":\"sql\",\"command\":\"$sql\"}" 2>&1)"
  code="${out##*$'\n'}"
  BODY="${out%$'\n'*}"
  [ "$code" = "200" ]
}

printf '\033[1mGAOS schema\033[0m -> %s/%s  (target: %s)\n\n' "$URL" "$DB" "$TARGET"

if [ "$DRY" = 1 ]; then
  echo "== dry run: statements that would be sent =="
  for f in [0-9]*.sql; do
    n=$(statements "$f" | wc -l)
    printf '  %-22s %3d statements\n' "$f" "$n"
  done
  echo
  echo "no request was made"
  exit 0
fi

# ------------------------------------------------------------- the database
#
# `create database` is a server command, not a database one, so it goes to a
# different endpoint. An existing database is the normal case on a re-run and
# is not a failure.
echo "== database =="
CREATE_BODY="$(curl -sS -m 30 -w '\n%{http_code}' \
  -u "$USER:$PASS" -H 'Content-Type: application/json' \
  -X POST "$URL/api/v1/server" \
  -d "{\"command\":\"create database $DB\"}" 2>&1)"
CREATE_CODE="${CREATE_BODY##*$'\n'}"
case "$CREATE_CODE" in
  200) ok "created database $DB" ;;
  *)   case "$CREATE_BODY" in
         *lready*exist*) ok "database $DB already exists" ;;
         *) bad "could not create or reach database $DB (HTTP $CREATE_CODE)"
            info "${CREATE_BODY%$'\n'*}" ;;
       esac ;;
esac

# ------------------------------------------------------------------ the DDL
for f in [0-9]*.sql; do
  echo
  echo "== $f =="
  file_fails=0
  while IFS= read -r stmt; do
    [ -z "$stmt" ] && continue
    json_safe "$stmt" || { fails=$((fails + 1)); file_fails=$((file_fails + 1)); continue; }
    if send "$DB" "$stmt"; then
      applied=$((applied + 1))
    else
      # IF NOT EXISTS is on every statement, so "already exists" should never
      # surface -- but ArcadeDB reports some re-declarations as errors anyway,
      # and a re-run reporting a failure it does not have would make this
      # script useless as the idempotence check it is meant to be.
      case "$BODY" in
        *lready*exist*) skipped=$((skipped + 1)) ;;
        *) bad "${stmt:0:72}"
           info "${BODY}"
           file_fails=$((file_fails + 1)) ;;
      esac
    fi
  done <<EOF
$(statements "$f")
EOF
  [ "$file_fails" = 0 ] && ok "$f applied clean"
done

echo
printf '%s applied, %s already present, \033[%sm%s failed\033[0m\n' \
  "$applied" "$skipped" "$([ "$fails" = 0 ] && echo 32 || echo 31)" "$fails"
[ "$fails" = 0 ] || exit 1
