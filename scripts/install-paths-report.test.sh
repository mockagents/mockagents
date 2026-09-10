#!/usr/bin/env bash
# Tests for install-paths-report.sh.
#
# The gate this script drives exists because a previous guard was never proved
# to fail. So prove this one: every branch, including the empty-array cases that
# a `set -e` shell aborts on.
#
# Usage: scripts/install-paths-report.test.sh

set -uo pipefail
cd "$(dirname "$0")/.."
SCRIPT=scripts/install-paths-report.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0

# run <name> <expected-exit> <results-content> <pending-content> [expected-substring]
run() {
  local name="$1" want="$2" results="$3" pending="$4" needle="${5:-}"
  printf '%s\n' "$results" > "$tmp/results.txt"
  printf '%s\n' "$pending" > "$tmp/pending.txt"
  : > "$tmp/summary.md"

  local out got
  cut -d'|' -f1 "$tmp/results.txt" | sed '/^$/d' | sort -u > "$tmp/required.txt"
  out=$(bash "$SCRIPT" "$tmp/results.txt" "$tmp/pending.txt" "$tmp/required.txt" 0.5.0 "$tmp/summary.md" 2>&1)
  got=$?

  local ok=1
  [ "$got" -eq "$want" ] || ok=0
  if [ -n "$needle" ] && ! grep -qF "$needle" <<<"$out$(cat "$tmp/summary.md")"; then ok=0; fi

  if [ "$ok" -eq 1 ]; then
    echo "  ok   $name"
    pass=$((pass + 1))
  else
    echo "  FAIL $name (exit want=$want got=$got)"
    echo "$out" | sed 's/^/         /'
    fail=$((fail + 1))
  fi
}

echo "install-paths-report.sh"

run "all working, nothing pending" 0 \
  'go|ok|0.5.0|go install
binary|ok|0.5.0|prebuilt binary' \
  '# none pending'

run "known-pending path fails -> green" 0 \
  'go|ok|0.5.0|go install
npx|fail|none|npx mockagents' \
  'npx' \
  "Still unpublished"

run "non-pending path fails -> red" 1 \
  'go|fail|none|go install
npx|fail|none|npx mockagents' \
  'npx' \
  "Advertised install path broken"

run "pending path starts working -> red" 1 \
  'go|ok|0.5.0|go install
npx|ok|0.5.0|npx mockagents' \
  'npx' \
  "now works"

run "comments and blanks ignored in pending file" 0 \
  'npx|fail|none|npx mockagents' \
  '# a comment

  # indented comment
npx'

run "empty results -> red" 1 \
  '' \
  'npx' \
  "No results produced"

run "everything pending and failing -> green" 0 \
  'npx|fail|none|npx
pypi|fail|none|pip install' \
  'npx
pypi'

run "mixed: one regression, one revival, one known" 1 \
  'go|fail|none|go install
npx|ok|0.5.0|npx
pypi|fail|none|pip install' \
  'npx
pypi'

run "stale successful result -> red" 1 \
  'go|ok|0.4.0|go install' \
  '# none' \
  "expected 0.5.0"

# Validate completeness, duplicates and status vocabulary against an explicit set.
printf 'go\nbinary\n' > "$tmp/required.txt"
printf 'go|ok|0.5.0|go install\n' > "$tmp/results.txt"
if bash "$SCRIPT" "$tmp/results.txt" "$tmp/pending.txt" "$tmp/required.txt" 0.5.0 >/dev/null 2>&1; then
  echo "  FAIL missing required result"; fail=$((fail + 1))
else echo "  ok   missing required result"; pass=$((pass + 1)); fi
printf 'go|ok|0.5.0|one\ngo|ok|0.5.0|two\n' > "$tmp/results.txt"
printf 'go\n' > "$tmp/required.txt"
if bash "$SCRIPT" "$tmp/results.txt" "$tmp/pending.txt" "$tmp/required.txt" 0.5.0 >/dev/null 2>&1; then
  echo "  FAIL duplicate result"; fail=$((fail + 1))
else echo "  ok   duplicate result"; pass=$((pass + 1)); fi
printf 'go|maybe|0.5.0|bad status\n' > "$tmp/results.txt"
if bash "$SCRIPT" "$tmp/results.txt" "$tmp/pending.txt" "$tmp/required.txt" 0.5.0 >/dev/null 2>&1; then
  echo "  FAIL invalid status"; fail=$((fail + 1))
else echo "  ok   invalid status"; pass=$((pass + 1)); fi

# The real pending file must parse and cover the ids the workflow emits.
echo
echo "consistency with the committed pending file"
ids=$(grep -oE '^[[:space:]]*check [a-z-]+' .github/workflows/install-paths.yml \
        | awk '{print $2}' | sort -u)
ids="$ids homebrew"
missing=""
for id in $ids; do
  if ! grep -qxF "$id" <(grep -vE '^[[:space:]]*(#|$)' .github/install-paths-pending.txt) \
     && ! grep -qxF "$id" <<<"go
binary
go-sdk"; then
    missing="$missing $id"
  fi
done
if [ -z "$missing" ]; then
  echo "  ok   every workflow id is either pending or expected-working"
  pass=$((pass + 1))
else
  echo "  FAIL unclassified ids:$missing"
  fail=$((fail + 1))
fi

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
