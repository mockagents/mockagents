#!/usr/bin/env bash
# Static negative-path assertions for the publication graph and channel policy.
set -euo pipefail
cd "$(dirname "$0")/.."
release=.github/workflows/release.yml
install_paths=.github/workflows/install-paths.yml

grep -q 'group: release-${{ github.ref_name }}' "$release"
grep -q 'cancel-in-progress: false' "$release"
grep -q 'uses: ./.github/workflows/verify.yml' "$release"
grep -q '^  postgres:' .github/workflows/verify.yml
grep -q 'os: \[ubuntu-latest, windows-latest\]' .github/workflows/verify.yml
grep -q 'npm run test:e2e' .github/workflows/verify.yml
grep -q 'docker build --tag mockagents-candidate:' .github/workflows/verify.yml
grep -q 'tag: \${{ steps.version.outputs.tag }}' "$release"
grep -q 'name: release-candidate-${{ github.sha }}' "$release"
grep -q 'version: "v2.15.4"' "$release"
grep -q 'args: release --clean --skip=publish' "$release"
grep -q 'govulncheck -mode=binary candidate/inspect/mockagents' "$release"
grep -q 'bash scripts/verify-candidate-artifacts.sh candidate' "$release"
grep -q 'npm publish candidate/npm/mockagents-sdk-' "$release"
grep -q 'packages-dir: candidate/python/' "$release"
grep -q 'docker load --input candidate/images/linux-amd64.tar' "$release"
grep -q 'mockagents-candidate:${GITHUB_SHA}-amd64' "$release"
grep -q 'existing immutable release asset differs' "$release"
if grep -q -- '--clobber' "$release"; then
  echo 'release workflow may overwrite an immutable GitHub asset' >&2
  exit 1
fi
grep -q 'name: Verify publication configuration' "$release"
grep -q 'name: Verify published binary assets' "$release"
grep -q 'sha256sum -c checksums.txt' "$release"
grep -q 'for name in DOCKERHUB_USERNAME DOCKERHUB_TOKEN NPM_TOKEN' "$release"
grep -q 'value=latest,enable=${{ needs.preflight.outputs.stable }}' "$release"
grep -q 'go-version-file: go.mod' "$release"

# Publisher jobs may consume and retag prepared bytes, but may never rebuild.
publisher_body=$(sed -n '/^  release-binaries:/,$p' "$release")
if grep -Eq 'goreleaser.*release|python -m build|npm (ci|run build|pack)([[:space:]]|$)|docker buildx build|docker/build-push-action' <<< "$publisher_body"; then
  echo 'publisher job rebuilds candidate artifacts' >&2
  exit 1
fi

if grep -Eq 'npm install --no-audit|go-version: "1\.26"' "$release"; then
  echo 'release workflow contains an unpinned install/toolchain path' >&2
  exit 1
fi
grep -q 'RELEASE_RUN_TAG: \${{ github.event.workflow_run.head_branch }}' "$install_paths"
grep -Fq '(\.[0-9A-Za-z-]+)*))?$ ]]' "$install_paths"
grep -Fq "grep -Eo '(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)" "$install_paths"
if grep -Fq "s/.*[^0-9]([0-9]+\\.[0-9]+\\.[0-9]+)" "$install_paths"; then
  echo 'install-path workflow truncates prerelease versions' >&2
  exit 1
fi
echo 'release workflow gates and channel policy verified'
