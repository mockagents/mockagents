#!/usr/bin/env bash
set -euo pipefail

root="${1:-candidate}"
root=$(cd "$root" && pwd)
parent=$(dirname "$root")
name=$(basename "$root")

(cd "$parent" && sha256sum -c "$name/SHA256SUMS")
(cd "$parent" && sha256sum -c "$name/SHA256SUMS.root")
