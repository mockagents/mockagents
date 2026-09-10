#!/usr/bin/env bash
# Publish prepared per-platform images without replacing immutable version tags.
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${RELEASE_TAGS:?RELEASE_TAGS is required}"

docker load --input candidate/images/linux-amd64.tar
docker load --input candidate/images/linux-arm64.tar

ensure_arch() {
  local registry="$1" arch="$2"
  local source="mockagents-candidate:${GITHUB_SHA}-${arch}"
  local target="${registry}:${VERSION}-${arch}"
  local local_config remote_json remote_config
  local_config=$(docker image inspect --format '{{.Id}}' "$source")

  if remote_json=$(docker manifest inspect "$target" 2>/dev/null); then
    remote_config=$(jq -r '.config.digest // empty' <<<"$remote_json")
    if [ -z "$remote_config" ] || [ "$remote_config" != "$local_config" ]; then
      echo "immutable image tag differs: $target" >&2
      return 1
    fi
    echo "immutable image already present: $target"
  else
    docker tag "$source" "$target"
    docker push "$target"
    remote_json=$(docker manifest inspect "$target")
    remote_config=$(jq -r '.config.digest // empty' <<<"$remote_json")
    [ "$remote_config" = "$local_config" ] || {
      echo "published image identity mismatch: $target" >&2
      return 1
    }
  fi
}

manifest_digest() {
  docker buildx imagetools inspect "$1" --format '{{json .Manifest}}' | jq -r '.digest'
}

ensure_version_index() {
  local registry="$1"
  local target="${registry}:${VERSION}"
  local amd="${registry}:${VERSION}-amd64" arm="${registry}:${VERSION}-arm64"
  local amd_digest arm_digest raw actual desired
  amd_digest=$(manifest_digest "$amd")
  arm_digest=$(manifest_digest "$arm")
  [ -n "$amd_digest" ] && [ "$amd_digest" != null ]
  [ -n "$arm_digest" ] && [ "$arm_digest" != null ]
  desired=$(printf '%s\n%s\n' "$amd_digest" "$arm_digest" | sort)

  if raw=$(docker buildx imagetools inspect "$target" --raw 2>/dev/null); then
    actual=$(jq -r '.manifests[]?.digest' <<<"$raw" | sort)
    if [ "$actual" != "$desired" ]; then
      echo "immutable image index differs: $target" >&2
      return 1
    fi
    echo "immutable image index already present: $target"
  else
    docker buildx imagetools create --tag "$target" \
      "${amd}@${amd_digest}" "${arm}@${arm_digest}"
  fi
}

for registry in mockagents/mockagents "ghcr.io/${GITHUB_REPOSITORY}"; do
  ensure_arch "$registry" amd64
  ensure_arch "$registry" arm64
  ensure_version_index "$registry"
done

# Version tags above are immutable. The remaining metadata tags are deliberate
# moving channels (stable major/minor/latest); prereleases produce none.
while IFS= read -r tag; do
  [ -n "$tag" ] || continue
  [ "$tag" = "${tag%:*}:${VERSION}" ] && continue
  base="${tag%:*}"
  docker buildx imagetools create --tag "$tag" \
    "${base}:${VERSION}-amd64" "${base}:${VERSION}-arm64"
done <<<"$RELEASE_TAGS"
