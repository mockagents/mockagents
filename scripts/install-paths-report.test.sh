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
  out=$(bash "$SCRIPT" "$tmp/results.txt" "$tmp/pending.txt" "$tmp/summary.md" 2>&1)
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
  'go|ok|go install
binary|ok|prebuilt binary' \
  '# none pending'

run "known-pending path fails -> green" 0 \
  'go|ok|go install
npx|fail|npx mockagents' \
  'npx' \
  "Still unpublished"

run "non-pending path fails -> red" 1 \
  'go|fail|go install
npx|fail|npx mockagents' \
  'npx' \
  "Advertised install path broken"

run "pending path starts working -> red" 1 \
  'go|ok|go install
npx|ok|npx mockagents' \
  'npx' \
  "now works"

run "comments and blanks ignored in pending file" 0 \
  'npx|fail|npx mockagents' \
  '# a comment

  # indented comment
npx'

run "empty results -> red" 1 \
  '' \
  'npx' \
  "No results produced"

run "everything pending and failing -> green" 0 \
  'npx|fail|npx
pypi|fail|pip install' \
  'npx
pypi'

run "mixed: one regression, one revival, one known" 1 \
  'go|fail|go install
npx|ok|npx
pypi|fail|pip install' \
  'npx
pypi'

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
