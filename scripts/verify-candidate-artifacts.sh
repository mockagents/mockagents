#!/usr/bin/env bash
set -euo pipefail

root="${1:-candidate}"
root=$(cd "$root" && pwd)
parent=$(dirname "$root")
name=$(basename "$root")

(cd "$parent" && sha256sum -c "$name/SHA256SUMS")
(cd "$parent" && sha256sum -c "$name/SHA256SUMS.root")

manifest="$root/candidate-manifest.txt"
require_identity() {
  local key="$1" expected="$2" count
  [ -z "$expected" ] && return
  count=$(grep -cFx "${key}=${expected}" "$manifest" || true)
  if [ "$count" -ne 1 ]; then
    echo "candidate identity mismatch: expected ${key}=${expected}" >&2
    exit 1
  fi
}

# Integrity alone only proves that the bundle is internally consistent. Each
# publisher also binds it to the triggering tag and commit so a valid bundle
# from another release can never be published by mistake.
require_identity commit "${EXPECTED_CANDIDATE_COMMIT:-}"
require_identity tag "${EXPECTED_CANDIDATE_TAG:-}"
require_identity version "${EXPECTED_CANDIDATE_VERSION:-}"
