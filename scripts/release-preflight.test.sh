#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.."
script=scripts/release-preflight.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

pass=0 fail=0
run() {
  local name="$1" tag="$2" want="$3" needle="$4" classify="${5:-false}" out_file="$tmp/out"
  : > "$out_file"
  GITHUB_OUTPUT="$out_file" RELEASE_PREFLIGHT_CLASSIFY_ONLY="$classify" bash "$script" "$tag" >"$tmp/log" 2>&1
  local got=$?
  if [[ "$got" == "$want" ]] && grep -qF "$needle" "$tmp/log" "$out_file"; then
    echo "ok - $name"; pass=$((pass + 1))
  else
    echo "not ok - $name (exit=$got expected=$want)"; cat "$tmp/log"; cat "$out_file"
    fail=$((fail + 1))
  fi
}

run stable v0.5.0 0 stable=true
run prerelease v0.5.0-rc.1 0 npm-channel=next true
run malformed v0.5 1 malformed
run leading-zero v00.5.0 1 malformed
run prerelease-leading-zero v0.5.0-rc.01 1 malformed true
run arbitrary-prefix release-0.5.0 1 malformed

echo "$pass passed, $fail failed"
[[ "$fail" == 0 ]]
