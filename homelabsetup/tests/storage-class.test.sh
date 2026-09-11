#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/homelabsetup/lib/storage-class.sh"

MESSAGES=""
log() { MESSAGES+="log:$*"$'\n'; }
warn() { MESSAGES+="warn:$*"$'\n'; }
kubectl() {
  case "$*" in
    "get storageclass -o jsonpath="*) [ "${LIST_FAIL:-false}" != true ] || return 1; printf '%s\n' "${DEFAULT_CLASS:-}" ;;
    "get storageclass local-path -o jsonpath={.provisioner}") printf '%s' 'rancher.io/local-path' ;;
    "get storageclass shared -o jsonpath={.provisioner}") printf '%s' 'driver.longhorn.io' ;;
    "get storageclass static -o jsonpath={.provisioner}") printf '%s' 'kubernetes.io/no-provisioner' ;;
    "get storageclass unknown -o jsonpath={.provisioner}") printf '%s' 'storage.example.test/local-compatible' ;;
    *) return 1 ;;
  esac
}

DEFAULT_CLASS=local-path
inspect_persistence_storage_class ""
grep -q 'warn:local-path is node-local storage' <<<"$MESSAGES"

MESSAGES=""
inspect_persistence_storage_class shared
grep -q 'log:persistence StorageClass: shared (driver.longhorn.io)' <<<"$MESSAGES"
if grep -q '^warn:' <<<"$MESSAGES"; then exit 1; fi

MESSAGES=""
inspect_persistence_storage_class static
grep -q 'kubernetes.io/no-provisioner' <<<"$MESSAGES"
if grep -q '^warn:' <<<"$MESSAGES"; then exit 1; fi

MESSAGES=""
inspect_persistence_storage_class unknown
grep -q 'storage.example.test/local-compatible' <<<"$MESSAGES"
if grep -q '^warn:' <<<"$MESSAGES"; then exit 1; fi

MESSAGES=""
DEFAULT_CLASS=""
inspect_persistence_storage_class ""
grep -q 'no storageClass was selected' <<<"$MESSAGES"

MESSAGES=""
LIST_FAIL=true inspect_persistence_storage_class ""
grep -q 'no storageClass was selected' <<<"$MESSAGES"

case_missing_storage_class_value() (
  # Exercise the parser without reaching deploy preflight.
  output="$("${ROOT}/homelabsetup/deploy-homelab.sh" --storage-class --skip-build 2>&1)" && return 1
  grep -q -- '--storage-class requires a name' <<<"$output"
)

case_option_like_equals_value() (
  output="$("${ROOT}/homelabsetup/deploy-homelab.sh" --storage-class=--skip-build 2>&1)" && return 1
  grep -q -- '--storage-class requires a name' <<<"$output"
)

case_missing_storage_class_value
case_option_like_equals_value

printf 'storage-class mocked cases: PASS\n'
