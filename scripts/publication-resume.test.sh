#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/candidate"
printf sdk > "$tmp/candidate/mockagents-sdk-1.2.3.tgz"
printf vitest > "$tmp/candidate/mockagents-vitest-1.2.3.tgz"
printf launcher > "$tmp/candidate/mockagents-1.2.3.tgz"

cat > "$tmp/bin/node" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *sdk/typescript*name*) echo @mockagents/sdk;; *sdk/vitest*name*) echo @mockagents/vitest;;
  *sdk/npx*name*) echo mockagents;; *) echo 1.2.3;;
esac
EOF
cat > "$tmp/bin/npm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  view) grep -qxF "$2" "$NPM_STATE" 2>/dev/null;;
  publish)
    case "$2" in *sdk*) spec=@mockagents/sdk@1.2.3;; *vitest*) spec=@mockagents/vitest@1.2.3;; *) spec=mockagents@1.2.3;; esac
    echo "$spec" >> "$NPM_STATE"; cp "$2" "$NPM_STORE/${2##*/}";;
  pack)
    shift; spec="$1"; shift 2; dest="$1"
    case "$spec" in @mockagents/sdk*) file=mockagents-sdk-1.2.3.tgz;; @mockagents/vitest*) file=mockagents-vitest-1.2.3.tgz;; *) file=mockagents-1.2.3.tgz;; esac
    cp "$NPM_STORE/$file" "$dest/$file"; echo "$file";;
esac
EOF
chmod +x "$tmp/bin/node" "$tmp/bin/npm"
: > "$tmp/state"; mkdir "$tmp/store"
PATH="$tmp/bin:$PATH" NPM_STATE="$tmp/state" NPM_STORE="$tmp/store" bash "$repo/scripts/publish-candidate-npm.sh" "$tmp/candidate" latest >/dev/null
PATH="$tmp/bin:$PATH" NPM_STATE="$tmp/state" NPM_STORE="$tmp/store" bash "$repo/scripts/publish-candidate-npm.sh" "$tmp/candidate" latest >/dev/null
printf corrupt > "$tmp/store/mockagents-sdk-1.2.3.tgz"
if PATH="$tmp/bin:$PATH" NPM_STATE="$tmp/state" NPM_STORE="$tmp/store" bash "$repo/scripts/publish-candidate-npm.sh" "$tmp/candidate" latest >/dev/null 2>&1; then
  echo 'npm mismatch accepted' >&2; exit 1
fi

mkdir "$tmp/python"
printf wheel > "$tmp/python/mockagents-1.2.3-py3-none-any.whl"
sha=$(sha256sum "$tmp/python/mockagents-1.2.3-py3-none-any.whl" | awk '{print $1}')
printf '{"urls":[{"filename":"mockagents-1.2.3-py3-none-any.whl","digests":{"sha256":"%s"}}]}' "$sha" > "$tmp/pypi.json"
python_root="$tmp/python"; python_script="$repo/scripts/verify-pypi-candidate.py"; json_url="file:///$tmp/pypi.json"
if command -v cygpath >/dev/null 2>&1; then
  python_root=$(cygpath -w "$python_root"); python_script=$(cygpath -w "$python_script")
  json_url="file:///$(cygpath -m "$tmp/pypi.json")"
fi
PYPI_JSON_URL="$json_url" python "$python_script" "$python_root" 1.2.3 >/dev/null
printf bad > "$tmp/python/mockagents-1.2.3-py3-none-any.whl"
if PYPI_JSON_URL="$json_url" python "$python_script" "$python_root" 1.2.3 >/dev/null 2>&1; then
  echo 'PyPI mismatch accepted' >&2; exit 1
fi
echo 'publication resume verification passed'
