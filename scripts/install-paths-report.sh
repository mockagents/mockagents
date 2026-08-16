#!/usr/bin/env bash
# Aggregate install-path check results and decide whether the run fails.
#
# Usage: install-paths-report.sh <results-file> <pending-file> [summary-file]
#
# Results file: one "<id>|<ok|fail>|<description>" per line.
# Pending file: ids that are known-unpublished, "#" comments and blanks ignored.
#
# Exit 0 only when reality matches the declared expectation. Two ways to fail:
#
#   broken  — a path NOT listed as pending does not work. A regression.
#   revived — a path listed as pending DOES work. The package shipped, so the
#             pending file and the README install table are now stale.
#
# The second is the unusual one, and it is deliberate: without it, a successful
# publish leaves the docs claiming the path is unavailable and nobody notices.
#
# Lives in a script rather than inline YAML so it can be tested without running
# the workflow — see scripts/install-paths-report.test.sh.

set -uo pipefail

results="${1:?usage: $0 <results-file> <pending-file> [summary-file]}"
pending="${2:?usage: $0 <results-file> <pending-file> [summary-file]}"
summary="${3:-/dev/null}"

if [ ! -s "$results" ]; then
  echo "::error::No results produced — the check jobs did not run."
  exit 1
fi

# Pending ids, comments and blank lines stripped.
pending_ids=$(grep -vE '^[[:space:]]*(#|$)' "$pending" 2>/dev/null || true)

is_pending() {
  printf '%s\n' "$pending_ids" | grep -qxF "$1"
}

broken=()
revived=()
still_pending=()
rows=0

{
  echo "## Install-path check"
  echo
  echo "| Path | Expected | Result |"
  echo "|---|---|---|"
} >> "$summary"

while IFS='|' read -r id status desc; do
  [ -z "${id:-}" ] && continue
  rows=$((rows + 1))
  if is_pending "$id"; then expected="pending"; else expected="working"; fi
  if [ "$status" = "ok" ]; then result="works"; else result="fails"; fi
  echo "| \`$id\` — $desc | $expected | **$result** |" >> "$summary"

  if [ "$status" = "ok" ]; then
    if is_pending "$id"; then revived+=("$id — $desc"); fi
  else
    if is_pending "$id"; then
      still_pending+=("$id — $desc")
    else
      broken+=("$id — $desc")
    fi
  fi
done < "$results"

echo >> "$summary"

# A results file that exists but carries no parseable rows means the check jobs
# produced nothing — the same silent-pass failure mode this gate replaced. The
# `-s` test above only catches a zero-byte file; whitespace slips past it.
if [ "$rows" -eq 0 ]; then
  echo "::error::No results produced — the check jobs did not run."
  exit 1
fi

if [ ${#broken[@]} -gt 0 ]; then
  {
    echo "### An advertised install path is broken"
    echo
    for x in "${broken[@]}"; do echo "- $x"; done
    echo
    echo "Not listed in \`$pending\`, so it was expected to work. This is a regression."
    echo
  } >> "$summary"
fi

if [ ${#revived[@]} -gt 0 ]; then
  {
    echo "### A pending path now works"
    echo
    for x in "${revived[@]}"; do echo "- $x"; done
    echo
    echo "Remove it from \`$pending\` and drop the hourglass marker from the README install table."
    echo
  } >> "$summary"
fi

if [ ${#still_pending[@]} -gt 0 ]; then
  {
    echo "### Still unpublished (known, not failing this run)"
    echo
    for x in "${still_pending[@]}"; do echo "- $x"; done
    echo
  } >> "$summary"
fi

status=0
for x in "${broken[@]:-}"; do
  [ -z "$x" ] && continue
  echo "::error::Advertised install path broken: $x"
  status=1
done
for x in "${revived[@]:-}"; do
  [ -z "$x" ] && continue
  echo "::error::Pending path now works — $pending and the README are stale: $x"
  status=1
done

exit $status
