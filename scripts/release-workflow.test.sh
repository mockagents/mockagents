#!/usr/bin/env bash
# Static negative-path assertions for the publication graph and channel policy.
set -euo pipefail
cd "$(dirname "$0")/.."
release=.github/workflows/release.yml
install_paths=.github/workflows/install-paths.yml

grep -q 'group: release-${{ github.ref_name }}' "$release"
grep -q 'cancel-in-progress: false' "$release"
grep -q 'uses: ./.github/workflows/verify.yml' "$release"
grep -q 'tag: \${{ steps.version.outputs.tag }}' "$release"
grep -q 'needs: \[preflight, prepare-artifacts, publish-preflight, verify-binary-assets\]' "$release"
grep -q 'name: Verify publication configuration' "$release"
grep -q 'name: Verify published binary assets' "$release"
grep -q 'sha256sum -c checksums.txt' "$release"
grep -q 'for name in DOCKERHUB_USERNAME DOCKERHUB_TOKEN NPM_TOKEN' "$release"
grep -q 'value=latest,enable=${{ needs.preflight.outputs.stable }}' "$release"
grep -q 'npm publish --access public --tag "${{ needs.preflight.outputs.npm-channel }}"' "$release"
grep -q 'PRERELEASE: ${{ needs.preflight.outputs.stable == '\''false'\'' }}' "$release"
grep -q 'go-version-file: go.mod' "$release"

if grep -Eq 'npm install --no-audit|go-version: "1\.26"' "$release"; then
  echo 'release workflow contains an unpinned install/toolchain path' >&2
  exit 1
fi
grep -q 'RELEASE_RUN_TAG: \${{ github.event.workflow_run.head_branch }}' "$install_paths"
grep -Fq '(\.[0-9A-Za-z-]+)*))?$ ]]' "$install_paths"
echo 'release workflow gates and channel policy verified'
