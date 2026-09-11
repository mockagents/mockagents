#!/usr/bin/env bash
# Platform-key handoff helpers. The caller provides kubectl, curl, sleep and
# error; resolved plaintext is returned only through shell variables.

platform_key_valid() {
  [[ "${1:-}" =~ ^mak_[0-9a-fA-F]{8}_[A-Za-z0-9_-]{32}$ ]]
}

resolve_platform_key() {
  local namespace="$1" pod="$2" credentials_file="$3" base_url="$4" app_host="$5"
  local candidate="" attempt exists=false status=""
  BOOTSTRAP_KEY=""
  BOOTSTRAP_KEY_FROM_POD=false

  # A newly bootstrapped pod is authoritative even when an old local file
  # survived. Retry transient exec failures before deciding the file is absent.
  for attempt in $(seq 1 15); do
    if kubectl -n "$namespace" exec "$pod" -- test -e /data/bootstrap-admin.key 2>/dev/null; then
      exists=true
      candidate="$(kubectl -n "$namespace" exec "$pod" -- cat /data/bootstrap-admin.key 2>/dev/null | tr -d '\r\n' || true)"
      [ -n "$candidate" ] && break
    fi
    sleep 2
  done
  if $exists; then
    platform_key_valid "$candidate" || error "pod bootstrap key file is not a valid platform credential"
    BOOTSTRAP_KEY="$candidate"
    BOOTSTRAP_KEY_FROM_POD=true
    return 0
  fi

  [ -r "$credentials_file" ] || error "no pod bootstrap key or retained local credential is available; see docs/guides/multi-tenant.md"
  candidate="$(sed -n 's/^MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=//p' "$credentials_file" | head -1)"
  platform_key_valid "$candidate" || error "retained local platform credential is invalid; see docs/guides/multi-tenant.md"
  status="$(
    local auth_dir auth_file
    auth_dir="$(mktemp -d)"
    chmod 700 "$auth_dir" 2>/dev/null || true
    trap 'rm -rf "$auth_dir"' EXIT
    auth_file="${auth_dir}/authorization-header"
    (umask 077; printf 'Authorization: Bearer %s\n' "$candidate" > "$auth_file")
    # Tenant collection reads require the platform role. A viewer/admin key
    # can authenticate on /identity but must never qualify as the retained
    # bootstrap operator credential.
    curl -sS -o /dev/null -w '%{http_code}' -H "Host: ${app_host}" -H "@${auth_file}" "${base_url}/api/v1/tenants" || true
  )"
  [ "$status" = 200 ] || error "retained local credential does not have working platform access"
  BOOTSTRAP_KEY="$candidate"
}

remove_pod_platform_key() {
  local namespace="$1" pod="$2" attempt
  for attempt in $(seq 1 5); do
    kubectl -n "$namespace" exec "$pod" -- rm -f /data/bootstrap-admin.key 2>/dev/null || true
    if kubectl -n "$namespace" exec "$pod" -- test ! -e /data/bootstrap-admin.key 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  error "credentials were saved locally, but the pod-side plaintext key file could not be removed after 5 attempts"
}
