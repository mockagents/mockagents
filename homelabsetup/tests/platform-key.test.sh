#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/homelabsetup/lib/platform-key.sh"

VALID_NEW='mak_1234abcd_abcdefghijklmnopqrstuvwxyz012345'
VALID_OLD='mak_deadbeef_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345'
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

error() { printf 'error:%s\n' "$*" >&2; exit 99; }
sleep() { :; }
curl() {
  local arg auth_file=""
  for arg in "$@"; do
    [[ "$arg" != *mak_* ]] || return 88
    [[ "$arg" != @* ]] || auth_file="${arg#@}"
  done
  [ -n "$auth_file" ] && [ -r "$auth_file" ] || return 89
  grep -q '^Authorization: Bearer mak_' "$auth_file" || return 90
  [[ "${!#}" == */api/v1/tenants ]] || return 91
  [ -z "${TEST_CURL_TRACE:-}" ] || printf '%s' "$auth_file" > "$TEST_CURL_TRACE"
  printf '%s' "${CURL_STATUS:-200}"
}
kubectl() {
  case "$*" in
    *'test -e /data/bootstrap-admin.key') $POD_PRESENT ;;
    *'cat /data/bootstrap-admin.key') printf '%s\n' "${POD_KEY:-}" ;;
    *'rm -f /data/bootstrap-admin.key') RM_CALLS=$((RM_CALLS + 1)); [ "$RM_CALLS" -ge "${RM_SUCCEEDS_ON:-1}" ] && POD_PRESENT=false; return 0 ;;
    *'test ! -e /data/bootstrap-admin.key') ! $POD_PRESENT ;;
    *) return 1 ;;
  esac
}

case_first_deploy() (
  POD_PRESENT=true POD_KEY="$VALID_NEW"
  resolve_platform_key ns pod "$TMP/missing" http://example.test mockagents.local
  [ "$BOOTSTRAP_KEY_FROM_POD" = true ] && [ "$BOOTSTRAP_KEY" = "$VALID_NEW" ]
)

case_normal_redeploy() (
  POD_PRESENT=false CURL_STATUS=200 TEST_CURL_TRACE="$TMP/curl-trace"
  rm -f "$TEST_CURL_TRACE"
  printf 'MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=%s\n' "$VALID_OLD" > "$TMP/retained"
  resolve_platform_key ns pod "$TMP/retained" http://example.test mockagents.local
  [ "$BOOTSTRAP_KEY_FROM_POD" = false ] && [ "$BOOTSTRAP_KEY" = "$VALID_OLD" ]
  auth_file="$(cat "$TEST_CURL_TRACE")"
  [ -n "$auth_file" ] && [ ! -e "$auth_file" ]
)

case_stale_local_new_pod() (
  POD_PRESENT=true POD_KEY="$VALID_NEW" CURL_STATUS=401
  printf 'MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=%s\n' "$VALID_OLD" > "$TMP/stale"
  resolve_platform_key ns pod "$TMP/stale" http://example.test mockagents.local
  [ "$BOOTSTRAP_KEY_FROM_POD" = true ] && [ "$BOOTSTRAP_KEY" = "$VALID_NEW" ]
)

case_delete_retry() (
  POD_PRESENT=true RM_CALLS=0 RM_SUCCEEDS_ON=2
  remove_pod_platform_key ns pod
  [ "$RM_CALLS" -eq 2 ] && ! $POD_PRESENT
)

case_invalid_sources() (
  POD_PRESENT=true POD_KEY='mak_REDACTED'
  resolve_platform_key ns pod "$TMP/missing" http://example.test mockagents.local
)

case_rejected_local() (
  POD_PRESENT=false CURL_STATUS=401
  printf 'MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=%s\n' "$VALID_OLD" > "$TMP/rejected"
  resolve_platform_key ns pod "$TMP/rejected" http://example.test mockagents.local
)

case_viewer_local() (
  POD_PRESENT=false CURL_STATUS=403
  printf 'MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=%s\n' "$VALID_OLD" > "$TMP/viewer"
  resolve_platform_key ns pod "$TMP/viewer" http://example.test mockagents.local
)

case_invalid_local() (
  POD_PRESENT=false CURL_STATUS=200
  printf 'MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=mak_REDACTED\n' > "$TMP/invalid-local"
  resolve_platform_key ns pod "$TMP/invalid-local" http://example.test mockagents.local
)

case_first_deploy
case_normal_redeploy
case_stale_local_new_pod
case_delete_retry
if case_invalid_sources >/dev/null 2>&1; then exit 1; else [ "$?" -eq 99 ]; fi
if case_rejected_local >/dev/null 2>&1; then exit 1; else [ "$?" -eq 99 ]; fi
if case_viewer_local >/dev/null 2>&1; then exit 1; else [ "$?" -eq 99 ]; fi
if case_invalid_local >/dev/null 2>&1; then exit 1; else [ "$?" -eq 99 ]; fi
printf 'platform-key mocked cases: PASS\n'
