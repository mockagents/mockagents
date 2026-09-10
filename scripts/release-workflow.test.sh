#!/usr/bin/env bash
# Static negative-path assertions for the publication graph and channel policy.
set -euo pipefail
cd "$(dirname "$0")/.."
release=.github/workflows/release.yml

grep -q 'group: release-${{ github.ref_name }}' "$release"
grep -q 'cancel-in-progress: false' "$release"
grep -q 'uses: ./.github/workflows/verify.yml' "$release"
grep -q 'needs: \[preflight, prepare-artifacts, release-binaries\]' "$release"
grep -q 'value=latest,enable=${{ needs.preflight.outputs.stable }}' "$release"
grep -q 'npm publish --access public --tag "${{ needs.preflight.outputs.npm-channel }}"' "$release"
grep -q 'PRERELEASE: ${{ needs.preflight.outputs.stable == '\''false'\'' }}' "$release"
grep -q 'go-version-file: go.mod' "$release"

if grep -Eq 'npm install --no-audit|go-version: "1\.26"' "$release"; then
  echo 'release workflow contains an unpinned install/toolchain path' >&2
  exit 1
fi
echo 'release workflow gates and channel policy verified'
