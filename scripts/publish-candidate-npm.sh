#!/usr/bin/env bash
set -euo pipefail
root="${1:?candidate npm directory is required}"
channel="${2:?npm channel is required}"

publish_or_verify() {
  local package="$1" file="$2" version name spec tmp remote
  version=$(node -p "require('./${package}/package.json').version")
  name=$(node -p "require('./${package}/package.json').name")
  spec="${name}@${version}"
  if npm view "$spec" version >/dev/null 2>&1; then
    tmp=$(mktemp -d)
    remote=$(npm pack "$spec" --pack-destination "$tmp" --silent)
    if ! cmp -s "$file" "$tmp/$remote"; then
      echo "existing npm package differs from candidate: $spec" >&2
      rm -rf "$tmp"
      return 1
    fi
    rm -rf "$tmp"
    echo "verified existing npm package: $spec"
  else
    npm publish "$file" --access public --tag "$channel"
  fi
}

publish_or_verify sdk/typescript "$root"/mockagents-sdk-*.tgz
publish_or_verify sdk/vitest "$root"/mockagents-vitest-*.tgz
publish_or_verify sdk/npx "$root"/mockagents-[0-9]*.tgz
