#!/usr/bin/env bash

# Describe the StorageClass used by a chart-managed PVC and warn when its
# provisioner is node-local. This is advisory: custom shared StorageClasses
# remain supported and Kubernetes remains authoritative for PVC binding.
inspect_persistence_storage_class() {
  local requested="${1:-}" selected provisioner

  if [ -n "$requested" ]; then
    selected="$requested"
  else
    selected="$({ kubectl get storageclass \
      -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' \
      2>/dev/null || true; } | head -n 1)"
  fi

  if [ -z "$selected" ]; then
    warn "persistence requested, but no storageClass was selected and no default StorageClass was found; the PVC may remain Pending"
    return 0
  fi

  provisioner="$(kubectl get storageclass "$selected" -o jsonpath='{.provisioner}' 2>/dev/null || true)"
  if [ -z "$provisioner" ]; then
    warn "persistence StorageClass ${selected} was not found; the PVC may remain Pending"
    return 0
  fi

  log "persistence StorageClass: ${selected} (${provisioner})"
  case "$provisioner" in
    rancher.io/local-path|kubernetes.io/host-path|microk8s.io/hostpath|openebs.io/local|local.csi.openebs.io)
      warn "${selected} is node-local storage: data survives container and pod restarts, but a replacement pod may remain Pending when its PV's node is drained or unavailable; use a shared/network StorageClass for cross-node rescheduling"
      ;;
  esac
}
