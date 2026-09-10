#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/work/candidate/images" "$tmp/work/scripts"
cp "$repo/scripts/publish-candidate-images.sh" "$tmp/work/scripts/"
: > "$tmp/work/candidate/images/linux-amd64.tar"
: > "$tmp/work/candidate/images/linux-arm64.tar"
cd "$tmp/work"

cat > "$tmp/bin/docker" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >> "$FAKE_LOG"
case "$1 ${2:-}" in
  'image inspect')
    case "${*: -1}" in *amd64) echo sha256:config-amd64;; *) echo sha256:config-arm64;; esac ;;
  'manifest inspect')
    ref="${*: -1}"; arch=arm64; [[ "$ref" == *amd64 ]] && arch=amd64
    if grep -qxF "$ref" "$FAKE_STATE" 2>/dev/null; then
      printf '{"config":{"digest":"sha256:config-%s"}}\n' "$arch"
      exit
    fi
    case "${FAKE_MODE:-missing}" in
      missing) exit 1 ;;
      mismatch) printf '{"config":{"digest":"sha256:wrong"}}\n' ;;
      *) printf '{"config":{"digest":"sha256:config-%s"}}\n' "$arch" ;;
    esac ;;
  'buildx imagetools')
    if [[ "$*" == *'--format'* ]]; then
      case "$*" in *amd64*) echo '{"digest":"sha256:manifest-amd64"}';; *) echo '{"digest":"sha256:manifest-arm64"}';; esac
    elif [[ "$*" == *'--raw'* ]]; then
      if [[ "${FAKE_MODE:-missing}" == missing ]] && [[ "$*" == *':1.2.3 '* ]]; then
        if grep -qxF "index:${4}" "$FAKE_STATE" 2>/dev/null; then
          printf '{"manifests":[{"digest":"sha256:manifest-amd64"},{"digest":"sha256:manifest-arm64"}]}\n'
          exit
        fi
        exit 1
      fi
      [[ "${FAKE_MODE:-missing}" == identical ]] || exit 1
      printf '{"manifests":[{"digest":"sha256:manifest-amd64"},{"digest":"sha256:manifest-arm64"}]}\n'
    elif [[ "${3:-}" == create && "${4:-}" == --tag && "${5:-}" == *:1.2.3 ]]; then
      echo "index:${5}" >> "$FAKE_STATE"
    fi ;;
  push*)
    echo "${*: -1}" >> "$FAKE_STATE" ;;
esac
FAKE
chmod +x "$tmp/bin/docker"
cat > "$tmp/bin/jq" <<'FAKE_JQ'
#!/usr/bin/env bash
grep -o 'sha256:[^"}]*'
FAKE_JQ
chmod +x "$tmp/bin/jq"

run_publish() {
  PATH="$tmp/bin:$PATH" FAKE_LOG="$tmp/log" FAKE_STATE="$tmp/state" FAKE_MODE="$1" \
    VERSION=1.2.3 GITHUB_SHA=abc GITHUB_REPOSITORY=mockagents/mockagents \
    RELEASE_TAGS=$'mockagents/mockagents:1.2.3\nmockagents/mockagents:latest\nghcr.io/mockagents/mockagents:1.2.3\nghcr.io/mockagents/mockagents:latest' \
    bash scripts/publish-candidate-images.sh
}

: > "$tmp/log"
: > "$tmp/state"
run_publish missing >/dev/null
grep -q 'push mockagents/mockagents:1.2.3-amd64' "$tmp/log"
grep -q 'create --tag mockagents/mockagents:1.2.3 ' "$tmp/log"
grep -q 'create --tag mockagents/mockagents:latest ' "$tmp/log"

: > "$tmp/log"
: > "$tmp/state"
run_publish identical >/dev/null
! grep -q '^push ' "$tmp/log"
! grep -q 'create --tag mockagents/mockagents:1.2.3 ' "$tmp/log"
grep -q 'create --tag mockagents/mockagents:latest ' "$tmp/log"

: > "$tmp/log"
: > "$tmp/state"
if run_publish mismatch >/dev/null 2>&1; then
  echo 'mismatched immutable tag was replaced' >&2
  exit 1
fi
! grep -q '^push ' "$tmp/log"
echo 'candidate image immutability verified'
