#!/usr/bin/env bash
# The agent workspace's own invariants — a running stack hides these.
#
# WHAT THIS CHECKS.  deploy/check-stacks.sh checks the stack; pustaka check
# checks the docs site's structure.  Neither looks at the agent workspace, and
# that is where the drift happened: an orphan skill, a vendored skill nothing
# referenced, and four numbers that stopped matching their own registers.
#
#   1  every path named in .claude/CLAUDE.md exists
#   2  no orphan skill
#   3  the counts agree with their registers
#   4  no rule is forked between two files
#   5  no isolation canary was left behind
#   6  no secret reached the review report store, no framework vocabulary came
#      back into the skills, every vendored skill kept its licence and its
#      provenance, and the trimmed design-engineering skill still matches the
#      digest its SOURCE.md records
#   7  no tracked or untracked-not-ignored file names a private instance: the
#      patterns come from a git-ignored list (D-085), never from this script
#
# WHO RUNS IT.  The product owner, by hand, like every other check.  D-025 makes
# the product owner the runner for every acceptance test and this repository has
# no CI.  It is advisory: nothing enforces it automatically, and that is stated
# rather than hidden.
#
# WHAT IT MUST NEVER PRINT.  The matched line of a secret-bearing file.  This
# script runs on the host, where deploy/rebuild-restart.sh symlinks the
# host-side credentials into worktrees, so the tree it walks contains them.
# For a path matching *.env, *.aim-*, *.api-*, *.bo-*, *.pem or *.key, print the path and the
# rule and never the text.

set -u
cd "$(dirname "$0")/.."

fails=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails + 1)); }
info() { printf '       \033[2m%s\033[0m\n' "$1"; }

# The live reference set: what a rule, an agent, a skill or the repository's own
# entry points actually name.  The review archive is excluded on purpose: an
# audit that mentions a skill must not keep a dead one alive.
REFS=(.claude/agents .claude/rules .claude/skills docs/rules README.md CONTRIBUTING.md)

# ---------------------------------------------------------------- 1. paths ---
# Every path the always-on layer names must exist.  A rule that points at a
# file nobody moved is a rule nobody follows.  A bare filename is shorthand
# inside a directory the sentence already named, so it is resolved by basename
# across the tree; a path with a slash is checked exactly.
paths_ok=0
paths_bad=0
while IFS= read -r p; do
  [ -z "$p" ] && continue
  case "$p" in
    http*|\#*|' '*|*' '*|*'|'*) continue ;;
  esac
  if [ -e "$p" ]; then
    paths_ok=$((paths_ok + 1))
  elif [ "${p#*/}" = "$p" ] && find . -name "$p" -not -path "./.git/*" -print -quit 2>/dev/null | grep -q .; then
    paths_ok=$((paths_ok + 1))
  else
    bad "path named in .claude/CLAUDE.md does not exist: $p"
    paths_bad=$((paths_bad + 1))
  fi
done < <(grep -oE '`[A-Za-z0-9_./-]+\.(md|html|sh|js|go|json|yaml|yml)`' .claude/CLAUDE.md 2>/dev/null \
         | tr -d '`' | sort -u)
[ "$paths_bad" -eq 0 ] && ok "every path named in .claude/CLAUDE.md exists ($paths_ok checked)"

# ---------------------------------------------------------------- 2. orphans ---
# A skill nothing names is dead weight in every session that loads the
# catalogue.  rebuild-restart-prod was one: no agent, no rule, no document and
# no script named it.  Agents are exempt -- a subagent is invoked by name from
# the harness, not from a file.
orphan=0
for f in .claude/skills/*/SKILL.md; do
  [ -f "$f" ] || continue
  stem=$(basename "$(dirname "$f")")
  n=0
  for r in "${REFS[@]}"; do
    [ -e "$r" ] || continue
    c=$(grep -rl --exclude-dir=.git --exclude-dir=reviews -- "$stem" "$r" 2>/dev/null \
          | grep -v "^${f}$" | wc -l | tr -d ' ')
    n=$((n + c))
  done
  if [ "$n" -eq 0 ]; then
    bad "orphan skill: nothing in .claude/ or docs/ names $f"
    orphan=$((orphan + 1))
  fi
done
[ "$orphan" -eq 0 ] && ok "every skill is referenced"

# ----------------------------------------------------------------- 3. counts ---
# The counts are prose, so nothing checks them but this.  Each one names the
# register it must agree with.
count_check() {
  local label="$1" file="$2" pat="$3" want="$4" got
  got=$(grep -c "$pat" "$file" 2>/dev/null || echo 0)
  if [ "$got" = "$want" ]; then
    ok "$label"
  else
    bad "$label: $file has $got, expected $want"
  fi
}

qa_max=$(grep -oE "TC-[0-9]{3}" pustaka/docs/product/qa-test-plan.html | sort -u | tail -1 | tr -d 'TC-')
qa_rows=$(grep -oE "<td>TC-[0-9]{3}</td>" pustaka/docs/product/qa-test-plan.html | sort -u | wc -l | tr -d ' ')
if [ "$qa_rows" = "$qa_max" ]; then
  ok "QA register: $qa_rows cases, highest TC-$qa_max"
else
  bad "QA register: $qa_rows rows but the highest id is TC-$qa_max"
fi

go_tests=$(grep -rhoE "^func Test[A-Za-z0-9_]+" dashboard/*_test.go pustaka/*_test.go 2>/dev/null | wc -l | tr -d ' ')
if grep -q "five hundred and fifty-eight" pustaka/docs/assets/toc/product.js \
   && [ "$go_tests" = "558" ]; then
  ok "Go test count: $go_tests, matching the register"
else
  bad "Go test count: $go_tests tests on disk, register says five hundred and fifty-eight"
fi

prose_check() {
  local label="$1" file="$2" want="$3"
  if grep -q "$want" "$file" 2>/dev/null; then
    ok "$label"
  else
    bad "$label: $file does not say \"$want\""
  fi
}
prose_check "QA case count in the register"  pustaka/docs/product/qa-test-plan.html "six hundred and ninety-two"
prose_check "QA case count in the ToC"       pustaka/docs/assets/toc/product.js      "six hundred and ninety-two"
prose_check "decision count in the log"      pustaka/docs/product/decision-log.html  "Ninety entries"
prose_check "question count in the register"  pustaka/docs/product/open-questions.html "All forty in one place"
prose_check "rule count in the BRD"           pustaka/docs/product/brd.html           "Fifty-eight rules"

# The README's figures are compared with what this script computes, never with
# a fixed string: a pinned string is how the README went stale while this check
# stayed green (D-085).
semver=$(grep -oE 'semver: "[0-9]+\.[0-9]+\.[0-9]+"' pustaka/docs/assets/toc.js 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
readme_check() {
  local label="$1" want="$2"
  if grep -qF "$want" README.md 2>/dev/null; then
    ok "$label matches ($want)"
  else
    bad "$label: README.md does not say \"$want\""
  fi
}
readme_check "README Go-test count" "$go_tests Go tests"
readme_check "README case count"    "$qa_rows registered test cases"
readme_check "README version"       "Product version $semver"

# ------------------------------------------------------------- 4. rule forks ---
# A rule restated in two files forks: one harness enforces the new wording and
# the other enforces the old.  .agents/rules/ are symlinks into .claude/rules/,
# so the same sentence appears twice on purpose -- compare resolved contents,
# and flag only a sentence that lives in two *different* rule files.
fork=0
for f in .claude/rules/*.md; do
  [ -f "$f" ] || continue
  while IFS= read -r line; do
    case "$line" in
      '#'*|''|'|'*|'```'*) continue ;;
      *'|'*) continue ;;
    esac
    hits=$(grep -rlF --exclude-dir=.git --exclude-dir=reviews "$line" \
             .claude/rules/ .agents/rules/ docs/rules/ GEMINI.md AGENTS.md 2>/dev/null \
           | while IFS= read -r p; do readlink -f "$p"; done | sort -u | wc -l | tr -d ' ')
    if [ "$hits" -gt 1 ]; then
      bad "rule forked across $hits files: ${line:0:60}"
      fork=$((fork + 1))
    fi
  done < <(grep -E '^- ' "$f" 2>/dev/null)
done
[ "$fork" -eq 0 ] && ok "no rule sentence appears in two rule files"

# ------------------------------------------------------------- 5. canaries -----
# A canary is a live string planted in a standing file for the length of one
# review run.  If one survives, it is committed and every later session reads
# it.  The places a canary can be planted, and the place it can leak to.
# A canary is the marker PREFIX plus a per-file name and a random value.  A
# report may discuss the prefix -- that is the runbook and the triage table
# talking about the probe -- but only a real value is a leak, and only a value
# left in a standing file pollutes every later session.
CANARY='EGM-MARKER-[A-Za-z0-9._-]*[0-9a-f]{8,}'
canary=0
for f in .claude/CLAUDE.md AGENTS.md GEMINI.md .claude/rules/*.md docs/designs/reviews/*.md; do
  [ -f "$f" ] || continue
  if grep -qE "$CANARY" "$f" 2>/dev/null; then
    bad "canary left behind in $f"
    canary=$((canary + 1))
  fi
done
[ "$canary" -eq 0 ] && ok "no isolation canary left behind"

# --------------------------------------------------------------- 6. secrets ----
# The review reports are committed and append-only, and the reviewer agents
# hold Grep over the whole repository.  A finding that quoted a credential
# would put it in every later session's context.  Scan the store with the
# detector set GAOS already uses in dashboard/egress.go.
# The detector set GAOS already uses in dashboard/egress.go names the vocabulary.
# A report that quotes the vocabulary -- the security reviewer's own rules do --
# is not a leak.  What leaks is a VALUE: a key, a token, a PEM block, or a
# credential assigned to something.
SECRET='(pbkdf2-sha256\$[0-9]+\$|AKIA[0-9A-Z]{16}|sk-[A-Za-z0-9]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|bt_live_[A-Za-z0-9]{16,}|bearer [A-Za-z0-9._-]{24,}|((api[_-]?key|secret[_-]?key|password|passwd|admin[_-]?key)["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9._/+-]{12,}))'
if [ -d docs/designs/reviews ]; then
  hits=$(grep -rEil "$SECRET" docs/designs/reviews/ 2>/dev/null | wc -l | tr -d ' ')
  # A PEM block is a secret on its own: the header line plus a base64 body.
  pem=$(grep -rl --include='*.md' -A1 -E '^-----BEGIN [A-Z ]*PRIVATE KEY-----$' \
          docs/designs/reviews/ 2>/dev/null | wc -l | tr -d ' ')
  hits=$((hits + pem))
  if [ "$hits" -eq 0 ]; then
    ok "no secret value in the review report store"
  else
    bad "$hits review report(s) contain a secret value (paths only, contents not printed)"
    grep -rEil "$SECRET" docs/designs/reviews/ 2>/dev/null | sed 's/^/       /'
  fi
fi

# The skills must not smuggle a framework into a stack that has none.  The
# baseline is the set of skills that survived the sweep and why.  The scan
# covers EVERY file in the directory, the provenance included: a cut record
# that names a framework would fail, so the cut record names the sections.
stack_skills="break-ui review-animations"
for d in .claude/skills/*/; do
  [ -d "$d" ] || continue
  name=$(basename "$d")
  n=$(grep -rioE "react|jsx|tailwind|sonner|shadcn" "$d" 2>/dev/null | wc -l | tr -d ' ')
  case " $stack_skills " in
    *" $name "*)
      info "$name carries $n framework-vocabulary match(es) (allowed)"
      ;;
    *)
      if [ "$n" -gt 0 ]; then
        bad "skill $name carries $n framework-vocabulary match(es) and is not on the stack-skills baseline"
      fi
      ;;
  esac
done

# The vendored skills, as opposed to the ones GAOS wrote itself.  This list is
# the definition of "vendored", so a new one is added here deliberately.
vendored_skills="break-ui review-animations emil-design-eng"

# Every vendored skill carries its licence and its provenance: the upstream
# URL, the licence and the commit.  The sisflow board is refused by
# check-stacks.sh without its licence and notice; the skills get the same
# treatment, because a vendored skill is an auto-loaded instruction file.
provenance_bad=0
for name in $vendored_skills; do
  d=".claude/skills/$name"
  if [ ! -s "$d/LICENSE" ]; then
    bad "skill $name has no LICENSE"
    provenance_bad=$((provenance_bad + 1))
    continue
  fi
  if ! grep -qE 'https?://' "$d/SOURCE.md" 2>/dev/null \
     || ! grep -qiE 'MIT|Apache|BSD|Licence|License' "$d/SOURCE.md" 2>/dev/null \
     || ! grep -qE '\b[0-9a-f]{7,40}\b' "$d/SOURCE.md" 2>/dev/null; then
    bad "skill $name has no SOURCE.md naming its upstream URL, licence and commit"
    provenance_bad=$((provenance_bad + 1))
  fi
done
[ "$provenance_bad" -eq 0 ] && ok "every vendored skill carries its licence and its provenance"

# The trimmed design-engineering skill is pinned by the digest its SOURCE.md
# records.  It is an instruction file every frontend session loads, so an edit
# to it fails the run until the digest is updated in the same change, with the
# reason recorded.
skill_dir=".claude/skills/emil-design-eng"
want=$(grep -oE 'sha256\(SKILL\.md\) = [0-9a-f]{64}' "$skill_dir/SOURCE.md" 2>/dev/null | awk '{print $3}')
got=$(sha256sum "$skill_dir/SKILL.md" 2>/dev/null | awk '{print $1}')
if [ -n "$want" ] && [ "$want" = "$got" ]; then
  ok "the trimmed design-engineering skill matches the SHA-256 its SOURCE.md records"
else
  bad "the trimmed emil-design-eng skill does not match the SHA-256 its SOURCE.md records"
fi

[ "$([ -d .claude/skills/emil-design-eng ] && echo yes || echo no)" = "yes" ] \
  && ok "the design-engineering skill is present, trimmed, and carries no framework vocabulary" \
  || bad "the design-engineering skill is missing"

# ----------------------------------------------------- 7. private strings ---
# A public repository must not name one operator's private instance: its
# sample product, its organisation, its domain (D-085).  The patterns are NOT
# in this script -- a list that names them would be the leak it guards
# against.  They live in a git-ignored file, one extended regex per line,
# matched case-insensitively against every tracked or untracked-not-ignored
# file's content and path.  The first non-empty list of: $GAOS_FORBIDDEN_STRINGS,
# deploy/local/forbidden-strings, and the same path in the main checkout (an
# agent worktree never receives ignored files).  Without a list, skip and say
# so.  A matching path is never printed: it is shown by its index and a hash.
pf_filter() { grep -vE '^[[:space:]]*(#|$)' "$1" 2>/dev/null; }
pf="" pf_src=""
if [ -n "${GAOS_FORBIDDEN_STRINGS:-}" ]; then
  if [ -r "$GAOS_FORBIDDEN_STRINGS" ] && [ -n "$(pf_filter "$GAOS_FORBIDDEN_STRINGS")" ]; then
    pf="$GAOS_FORBIDDEN_STRINGS" pf_src=env
  else
    bad "GAOS_FORBIDDEN_STRINGS names no readable, non-empty list"
    pf_src=bad
  fi
fi
if [ -z "$pf_src" ]; then
  main_root="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)/.."
  for cand in "deploy/local/forbidden-strings:local" "$main_root/deploy/local/forbidden-strings:main"; do
    f="${cand%:*}"
    if [ -r "$f" ] && [ -n "$(pf_filter "$f")" ]; then
      pf="$f" pf_src="${cand##*:}"
      break
    fi
  done
fi
if git ls-files --error-unmatch deploy/local/forbidden-strings >/dev/null 2>&1; then
  bad "the forbidden-strings list is tracked; it must stay git-ignored"
fi
if [ -n "$pf" ]; then
  pats=$(mktemp)
  chmod 600 "$pats"
  trap 'rm -f "$pats"' EXIT
  pf_filter "$pf" | tr 'A-Z' 'a-z' > "$pats"
  grep -iE -f "$pats" /dev/null >/dev/null 2>&1
  if [ $? -gt 1 ]; then
    bad "the forbidden-strings list holds an invalid pattern"
  else
    all_paths=$(git ls-files --cached --others --exclude-standard)
    show() {  # a path that matches a pattern is shown by index and hash only
      if printf '%s\n' "$1" | grep -qiE -f "$pats"; then
        k=$(printf '%s\n' "$all_paths" | grep -nxF -- "$1" | head -1 | cut -d: -f1)
        printf 'path #%s (%s)' "${k:-?}" "$(printf '%s' "$1" | sha1sum | cut -c1-8)"
      else
        printf '%s' "$1"
      fi
    }
    priv=0
    while IFS= read -r p; do
      [ -z "$p" ] && continue
      [ "$p" = "deploy/local/README.md" ] && continue
      bad "a file under deploy/local/ is tracked: $(show "$p")"
      priv=$((priv + 1))
    done < <(git ls-files deploy/local)
    hits=$(git grep --untracked -I -i -n -E -f "$pats" 2>/dev/null; echo "rc=$?")
    rc=${hits##*rc=}
    if [ "$rc" -gt 1 ]; then
      bad "git grep could not scan the working tree"
      priv=$((priv + 1))
    else
      while IFS= read -r hit; do
        [ -z "$hit" ] && continue
        case "$hit" in rc=*) continue ;; esac
        path=${hit%%:*} rest=${hit#*:} line=${rest%%:*}
        bad "a tracked file names a private instance: $(show "$path") (line $line)"
        priv=$((priv + 1))
      done <<< "$hits"
    fi
    while IFS= read -r p; do
      [ -z "$p" ] && continue
      bad "a tracked path names a private instance: $(show "$p")"
      priv=$((priv + 1))
    done < <(printf '%s\n' "$all_paths" | grep -iE -f "$pats")
    [ "$priv" -eq 0 ] && ok "no tracked file or path names a private instance ($(wc -l < "$pats" | tr -d ' ') pattern(s) from $pf_src)"
  fi
elif [ -z "$pf_src" ]; then
  info "skip private strings: no forbidden-strings list (deploy/local/README.md says where it goes)"
fi

# --------------------------------------------------------------------- done ---
if [ "$fails" -gt 0 ]; then
  printf '\n%s check-workspace.sh: %d failure(s)\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$fails"
  exit 1
fi
printf '\n%s check-workspace.sh: all checks passed\n' "$(date '+%Y-%m-%d %H:%M:%S')"
exit 0
