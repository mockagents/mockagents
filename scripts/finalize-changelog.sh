#!/bin/sh
# Finalize CHANGELOG.md for a release: promote the "## [Unreleased]" section to a
# versioned "## [X.Y.Z] - DATE" heading and open a fresh empty "## [Unreleased]"
# above it. Run this on `main` as the last step before tagging (see
# docs/RELEASING.md), AFTER every PR meant for the release has merged, so the new
# heading captures exactly what the release contains.
#
# Usage:
#   scripts/finalize-changelog.sh <version> [YYYY-MM-DD]   # date defaults to today
#
# Idempotency / safety:
#   - errors if there is no "## [Unreleased]" heading,
#   - errors if "## [<version>]" already exists (already finalized),
#   - only the FIRST "## [Unreleased]" line is promoted.
set -eu

version="${1:?usage: scripts/finalize-changelog.sh <version> [YYYY-MM-DD]}"
date="${2:-$(date +%F)}"
file="${CHANGELOG_FILE:-CHANGELOG.md}"

[ -f "$file" ] || { echo "finalize-changelog: $file not found" >&2; exit 1; }
grep -q '^## \[Unreleased\]' "$file" || {
	echo "finalize-changelog: no '## [Unreleased]' heading in $file" >&2; exit 1; }
if grep -q "^## \[$version\]" "$file"; then
	echo "finalize-changelog: '## [$version]' already present in $file" >&2; exit 1
fi

awk -v ver="$version" -v d="$date" '
	!done && /^## \[Unreleased\]/ {
		print "## [Unreleased]"
		print ""
		print "## [" ver "] - " d
		done = 1
		next
	}
	{ print }
' "$file" > "$file.tmp" && mv "$file.tmp" "$file"

echo "finalize-changelog: promoted [Unreleased] -> [$version] - $date in $file"
