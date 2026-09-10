#!/usr/bin/env bash
# Validate a release identity before any registry or GitHub mutation.
set -euo pipefail

tag="${1:-${GITHUB_REF_NAME:-}}"
output="${GITHUB_OUTPUT:-/dev/null}"

if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
  echo "release-preflight: malformed semver tag: $tag" >&2
  exit 1
fi

version="${tag#v}"
stable=true
channel=latest
if [[ "$version" == *-* ]]; then
  stable=false
  channel=next
  prerelease="${version#*-}"
  IFS=. read -ra identifiers <<< "$prerelease"
  for identifier in "${identifiers[@]}"; do
    if [[ "$identifier" =~ ^[0-9]+$ && "$identifier" =~ ^0[0-9]+$ ]]; then
      echo "release-preflight: malformed semver tag: $tag" >&2
      exit 1
    fi
  done
fi

if [[ "${RELEASE_PREFLIGHT_CLASSIFY_ONLY:-false}" != true ]]; then
  python_project=$(sed -nE 's/^version = "([^"]+)"/\1/p' sdk/python/pyproject.toml | head -1)
  python_init=$(sed -nE 's/^__version__ = "([^"]+)"/\1/p' sdk/python/mockagents/__init__.py | head -1)
  for actual in "$python_project" "$python_init"; do
    [[ "$actual" == "$version" ]] || { echo "release-preflight: Python version $actual != $version" >&2; exit 1; }
  done

  for package in sdk/npx sdk/typescript sdk/vitest; do
    actual=$(node -p "require('./$package/package.json').version")
    [[ "$actual" == "$version" ]] || { echo "release-preflight: $package version $actual != $version" >&2; exit 1; }
  done

  sdk_peer=$(node -p "require('./sdk/vitest/package.json').peerDependencies['@mockagents/sdk']")
  [[ "$sdk_peer" == "^$version" ]] || {
    echo "release-preflight: @mockagents/vitest peer $sdk_peer does not select candidate ^$version" >&2
    exit 1
  }
fi

{
  echo "tag=$tag"
  echo "version=$version"
  echo "stable=$stable"
  echo "npm-channel=$channel"
} >> "$output"
printf 'release-preflight: %s version=%s stable=%s npm-channel=%s\n' "$tag" "$version" "$stable" "$channel"
