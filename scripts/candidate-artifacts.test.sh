#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/candidate/packages"
printf 'verified bytes\n' > "$tmp/candidate/packages/package.tgz"
(cd "$tmp" && sha256sum candidate/packages/package.tgz > candidate/SHA256SUMS)
printf 'commit=abc123\ntag=v1.2.3\nversion=1.2.3\n' > "$tmp/candidate/candidate-manifest.txt"
(cd "$tmp" && sha256sum candidate/SHA256SUMS candidate/candidate-manifest.txt > candidate/SHA256SUMS.root)

EXPECTED_CANDIDATE_COMMIT=abc123 EXPECTED_CANDIDATE_TAG=v1.2.3 \
  EXPECTED_CANDIDATE_VERSION=1.2.3 scripts/verify-candidate-artifacts.sh "$tmp/candidate" >/dev/null
if EXPECTED_CANDIDATE_COMMIT=wrong scripts/verify-candidate-artifacts.sh "$tmp/candidate" >/dev/null 2>&1; then
  echo 'candidate from another commit passed identity verification' >&2
  exit 1
fi
printf 'tampered\n' >> "$tmp/candidate/packages/package.tgz"
if scripts/verify-candidate-artifacts.sh "$tmp/candidate" >/dev/null 2>&1; then
  echo 'tampered candidate artifact passed verification' >&2
  exit 1
fi
echo 'candidate artifact identity verification passed'
