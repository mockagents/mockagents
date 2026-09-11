#!/usr/bin/env bash
# Non-destructive release-candidate regression checks for an existing homelab
# deployment. The script creates only uniquely named API/vector fixtures and
# removes them on exit. It never prints credentials or response bodies.
set -euo pipefail

NAMESPACE="${NAMESPACE:-mockagents}"
RELEASE="${RELEASE:-mockagents}"
APP_HOST="${APP_HOST:-mockagents.local}"
BASE_URL="${BASE_URL:-}"
EXPECTED_IMAGE="${EXPECTED_IMAGE:-}"
EXPECTED_IMAGE_ID="${EXPECTED_IMAGE_ID:-}"
EXPECTED_SOURCE_COMMIT="${EXPECTED_SOURCE_COMMIT:-}"
RELEASE_GATE="${RELEASE_GATE:-false}"
IMAGE_REGISTRY="${IMAGE_REGISTRY:-registry.local:5000/mockagents/mockagents}"
PLATFORM_KEY="${MOCKAGENTS_PLATFORM_KEY:-}"
CREDS_FILE="${HOMELAB_CREDS_FILE:-$(cd "$(dirname "$0")" && pwd)/.homelab-credentials}"
RUN_ID="homelab-$(date -u +%Y%m%d%H%M%S)-$$"
TMP_DIR="$(mktemp -d)"
TENANT_ID=""
VIEWER_KEY=""
VIEWER_KEY_ID=""
CHROMA_COLLECTION_ID=""

pass=0
fail=0
log() { printf '[homelab-regression] %s\n' "$*"; }
ok() { pass=$((pass + 1)); log "PASS $*"; }
bad() { fail=$((fail + 1)); log "FAIL $*" >&2; }
need() { command -v "$1" >/dev/null 2>&1 || { log "missing required command: $1" >&2; exit 2; }; }

need kubectl
need curl
need jq

if [ -z "$PLATFORM_KEY" ] && [ -r "$CREDS_FILE" ]; then
  PLATFORM_KEY="$(sed -n 's/^MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=//p' "$CREDS_FILE" | head -1)"
fi
if [ "$RELEASE_GATE" = true ]; then
  [ -n "$EXPECTED_IMAGE" ] || { log 'release gate requires independently supplied EXPECTED_IMAGE' >&2; exit 2; }
  [ -n "$EXPECTED_IMAGE_ID" ] || { log 'release gate requires independently supplied EXPECTED_IMAGE_ID' >&2; exit 2; }
  [ -n "$EXPECTED_SOURCE_COMMIT" ] || { log 'release gate requires independently supplied EXPECTED_SOURCE_COMMIT' >&2; exit 2; }
fi
if [ -z "$EXPECTED_IMAGE" ] && [ -r "$CREDS_FILE" ]; then
  deployed_tag="$(sed -n 's/^IMAGE_TAG=//p' "$CREDS_FILE" | head -1)"
  [ -z "$deployed_tag" ] || EXPECTED_IMAGE="${IMAGE_REGISTRY}:${deployed_tag}"
fi
if [ -r "$CREDS_FILE" ]; then
  [ -n "$EXPECTED_IMAGE_ID" ] || EXPECTED_IMAGE_ID="$(sed -n 's/^IMAGE_ID=//p' "$CREDS_FILE" | head -1)"
  [ -n "$EXPECTED_SOURCE_COMMIT" ] || EXPECTED_SOURCE_COMMIT="$(sed -n 's/^SOURCE_COMMIT=//p' "$CREDS_FILE" | head -1)"
fi
[ -n "$EXPECTED_IMAGE" ] || {
  log 'EXPECTED_IMAGE is required when the credentials file has no IMAGE_TAG' >&2
  exit 2
}
[ -n "$EXPECTED_IMAGE_ID" ] || { log 'EXPECTED_IMAGE_ID is required for exact candidate verification' >&2; exit 2; }
[ -n "$EXPECTED_SOURCE_COMMIT" ] || { log 'EXPECTED_SOURCE_COMMIT is required for exact candidate verification' >&2; exit 2; }

if [ -z "$BASE_URL" ]; then
  TRAEFIK_IP="$(kubectl -n kube-system get service traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
  [ -n "$TRAEFIK_IP" ] || { log 'Traefik has no load-balancer IP' >&2; exit 2; }
  BASE_URL="http://${TRAEFIK_IP}"
fi

curl_status() {
  local method="$1" path="$2" body="${3:-}" key="${4:-}" output="${5:-${TMP_DIR}/response.json}"
  local args=(-sS -o "$output" -w '%{http_code}' -X "$method" -H "Host: ${APP_HOST}")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data "$body")
  # Keep credentials out of the curl process arguments, which can be visible
  # to other users through the process table on a shared validation host.
  if [ -n "$key" ]; then
    local auth_file="${TMP_DIR}/curl-auth"
    (umask 077; printf 'Authorization: Bearer %s\n' "$key" > "$auth_file")
    args+=(-H "@${auth_file}")
  fi
  curl "${args[@]}" "${BASE_URL}${path}"
}

expect_status() {
  local want="$1" name="$2" method="$3" path="$4" body="${5:-}" key="${6:-}"
  local got
  got="$(curl_status "$method" "$path" "$body" "$key")" || got='transport-error'
  if [ "$got" = "$want" ]; then ok "$name ($got)"; else bad "$name (got $got, want $want)"; fi
}

cleanup() {
  if [ -n "$PLATFORM_KEY" ] && [ -n "$TENANT_ID" ]; then
    curl_status DELETE "/api/v1/tenants/${TENANT_ID}" '' "$PLATFORM_KEY" >/dev/null 2>&1 || true
  fi
  if [ -n "$CHROMA_COLLECTION_ID" ]; then
    curl_status DELETE "/api/v2/tenants/default_tenant/databases/default_database/collections/${CHROMA_COLLECTION_ID}" '' "$PLATFORM_KEY" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

log "candidate namespace=${NAMESPACE} release=${RELEASE} host=${APP_HOST}"
kubectl -n "$NAMESPACE" rollout status "deployment/${RELEASE}" --timeout=240s >/dev/null
desired="$(kubectl -n "$NAMESPACE" get deployment "$RELEASE" -o jsonpath='{.spec.replicas}')"
ready="$(kubectl -n "$NAMESPACE" get deployment "$RELEASE" -o jsonpath='{.status.readyReplicas}')"
[ "$desired" = "$ready" ] && ok "rollout ready (${ready}/${desired})" || bad "rollout ready (${ready:-0}/${desired})"
image="$(kubectl -n "$NAMESPACE" get deployment "$RELEASE" -o jsonpath='{.spec.template.spec.containers[0].image}')"
log "deployed image=${image}"
[ "$image" = "$EXPECTED_IMAGE" ] && ok 'deployed image matches candidate' || bad "deployed image mismatch (${image})"
image_id="$(kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=${RELEASE}" -o jsonpath='{.items[0].status.containerStatuses[0].imageID}')"
[ "$image_id" = "$EXPECTED_IMAGE_ID" ] && ok 'running image digest matches candidate' || bad 'running image digest mismatch'
case "$image" in
  *"${EXPECTED_SOURCE_COMMIT:0:12}") ok 'image tag identifies source commit' ;;
  *) bad 'image tag does not identify expected source commit' ;;
esac

expect_status 200 health GET /api/v1/health
expect_status 200 readiness GET /api/v1/ready
expect_status 200 'OpenAI chat' POST /v1/chat/completions '{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}' "$PLATFORM_KEY"
expect_status 200 'OpenAI embeddings' POST /v1/embeddings '{"model":"text-embedding-3-small","input":"homelab regression"}' "$PLATFORM_KEY"
# examples/rag-agent.yaml is the explicitly model-addressable Anthropic fixture
# mounted by deploy-homelab.sh. Keep this request tied to that declared model:
# when multiple Anthropic agents are loaded, an unknown model correctly returns
# 404 rather than selecting an ambiguous fallback.
expect_status 200 'Anthropic messages' POST /v1/messages '{"model":"claude-3-opus","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}' "$PLATFORM_KEY"

# Exercise vector creation, projection and deletion with a collision-free name.
chroma_body="{\"name\":\"${RUN_ID}\",\"metadata\":{\"hnsw:space\":\"cosine\"}}"
status="$(curl_status POST /api/v2/tenants/default_tenant/databases/default_database/collections "$chroma_body" "$PLATFORM_KEY")"
if [ "$status" = 200 ] || [ "$status" = 201 ]; then
  CHROMA_COLLECTION_ID="$(jq -r '.id // empty' "${TMP_DIR}/response.json")"
  [ -n "$CHROMA_COLLECTION_ID" ] && ok 'Chroma collection create' || bad 'Chroma create response lacks id'
else
  bad "Chroma collection create (got $status)"
fi
if [ -n "$CHROMA_COLLECTION_ID" ]; then
  expect_status 200 'Chroma upsert' POST "/api/v2/tenants/default_tenant/databases/default_database/collections/${CHROMA_COLLECTION_ID}/upsert" '{"ids":["evidence"],"embeddings":[[1,0]],"documents":["release evidence"],"uris":["file:///evidence"]}' "$PLATFORM_KEY"
  status="$(curl_status POST "/api/v2/tenants/default_tenant/databases/default_database/collections/${CHROMA_COLLECTION_ID}/query" '{"query_embeddings":[[1,0]],"n_results":1,"include":["documents","uris","distances"]}' "$PLATFORM_KEY")"
  if [ "$status" = 200 ] && jq -e '.documents[0][0] == "release evidence" and .uris[0][0] == "file:///evidence" and .distances[0][0] == 0' "${TMP_DIR}/response.json" >/dev/null; then
    ok 'Chroma projection and cosine distance'
  else
    bad "Chroma projection and cosine distance (got $status)"
  fi
fi

if [ -z "$PLATFORM_KEY" ]; then
  expect_status 200 'single-tenant metrics' GET /metrics
  log 'SKIP multi-tenant role/revocation checks (no platform key supplied)'
else
  expect_status 401 'metrics reject anonymous' GET /metrics
  expect_status 200 'metrics allow platform' GET /metrics '' "$PLATFORM_KEY"

  tenant_body="{\"name\":\"${RUN_ID}\"}"
  status="$(curl_status POST /api/v1/tenants "$tenant_body" "$PLATFORM_KEY")"
  if [ "$status" = 201 ]; then
    TENANT_ID="$(jq -r '.id // empty' "${TMP_DIR}/response.json")"
    [ -n "$TENANT_ID" ] && ok 'create disposable tenant' || bad 'tenant response lacks id'
  else
    bad "create disposable tenant (got $status)"
  fi

  if [ -n "$TENANT_ID" ]; then
    key_body="{\"name\":\"${RUN_ID}-viewer\",\"role\":\"viewer\"}"
    status="$(curl_status POST "/api/v1/tenants/${TENANT_ID}/keys" "$key_body" "$PLATFORM_KEY")"
    if [ "$status" = 201 ]; then
      VIEWER_KEY="$(jq -r '.plaintext // empty' "${TMP_DIR}/response.json")"
      VIEWER_KEY_ID="$(jq -r '.key.id // empty' "${TMP_DIR}/response.json")"
      [ -n "$VIEWER_KEY" ] && [ -n "$VIEWER_KEY_ID" ] && ok 'mint disposable viewer key' || bad 'viewer key response incomplete'
    else
      bad "mint disposable viewer key (got $status)"
    fi
  fi

  if [ -n "$VIEWER_KEY" ]; then
    expect_status 403 'metrics reject viewer' GET /metrics '' "$VIEWER_KEY"
    expect_status 200 'viewer data-plane request' POST /v1/chat/completions '{"model":"gpt-4o","messages":[{"role":"user","content":"viewer"}]}' "$VIEWER_KEY"
    status="$(curl_status POST "/api/v1/keys/${VIEWER_KEY_ID}/rotate?tenant=${TENANT_ID}" '' "$PLATFORM_KEY")"
    if [ "$status" = 200 ]; then
      rotated="$(jq -r '.plaintext // empty' "${TMP_DIR}/response.json")"
      if [ -n "$rotated" ]; then
        ok 'rotate disposable viewer key'
        # Provider routes deliberately accept anonymous clients, so an invalid
        # credential there still reaches the mock. Verify revocation against a
        # protected management endpoint where authentication is mandatory.
        expect_status 401 'rotated credential rejected immediately' GET /api/v1/identity '' "$VIEWER_KEY"
        expect_status 200 'new credential accepted' GET /api/v1/identity '' "$rotated"
      else
        bad 'rotate response lacks plaintext credential'
      fi
    else
      bad "rotate disposable viewer key (got $status)"
    fi
  fi
fi

expect_status 200 'interaction log API' GET /api/v1/logs '' "$PLATFORM_KEY"

log "result pass=${pass} fail=${fail}"
[ "$fail" -eq 0 ]
