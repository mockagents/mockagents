#!/usr/bin/env bash
# Multi-tenancy + quota + cost walkthrough (Anthropic Messages surface).
#
# Prereqs:
#   * MockAgents started in multi-tenant mode (MOCKAGENTS_MULTI_TENANT=1).
#   * The bootstrap PLATFORM key from the server logs on first start:
#       docker compose logs mockagents | grep "Bootstrap admin key"
#     Pass it in:  BOOTSTRAP_KEY=<key> ./scripts/multitenant_walkthrough.sh
#   * jq on PATH.
#
# Mirrors the OpenAI demo, but drives LLM traffic at /v1/messages. A platform key
# can only mint keys for its OWN tenant (cross-tenant key creation is blocked by
# design), so we operate on the bootstrap key's "default" tenant.
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
: "${BOOTSTRAP_KEY:?Set BOOTSTRAP_KEY to the platform key printed in the server logs}"
PLAT=(-H "authorization: Bearer ${BOOTSTRAP_KEY}")
JSON=(-H "content-type: application/json")
MODEL="claude-3-5-sonnet-latest"

command -v jq >/dev/null || { echo "jq is required"; exit 1; }
say() { printf '\n\033[36m=== %s ===\033[0m\n' "$1"; }

say "0. (capability demo) Create a second tenant 'acme'"
curl -fsS "${BASE}/api/v1/tenants" "${PLAT[@]}" "${JSON[@]}" -d '{"name":"acme"}' | jq '{id, name}'
echo "  (its first API key is provisioned out of band; we use the default tenant below)"

say "1. Find the default tenant id"
TENANT_ID=$(curl -fsS "${BASE}/api/v1/tenants" "${PLAT[@]}" | jq -r '.[] | select(.name=="default") | .id')
echo "default tenant id: ${TENANT_ID}"

say "2. Mint an editor app key on the default tenant"
TENANT_KEY=$(curl -fsS "${BASE}/api/v1/tenants/${TENANT_ID}/keys" "${PLAT[@]}" "${JSON[@]}" \
  -d '{"name":"acme-claude-app","role":"editor"}' | jq -r '.plaintext // .Plaintext')
echo "app key: ${TENANT_KEY:0:12}..."
# The Anthropic surface accepts the key as a Bearer token (or x-api-key).
TEN=(-H "authorization: Bearer ${TENANT_KEY}")

say "3. Set a tiny quota (1 req/s, burst 2, \$0.001/month)"
curl -fsS -X PUT "${BASE}/api/v1/tenants/${TENANT_ID}/quota" "${PLAT[@]}" "${JSON[@]}" \
  -d '{"rate_per_sec":1,"rate_burst":2,"monthly_spend_usd":0.001}' | jq .

say "4. Burst 10 /v1/messages requests with the app key — expect 429 (rate) and/or 402 (spend)"
for i in $(seq 1 10); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/v1/messages" "${TEN[@]}" "${JSON[@]}" \
    -d "{\"model\":\"${MODEL}\",\"max_tokens\":64,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}")
  echo "  request ${i}: HTTP ${code}"
done
echo "  (200 = ok, 429 = rate limited, 402 = monthly spend cap exceeded)"

say "5. Tenant quota + usage"
curl -fsS "${BASE}/api/v1/quota" "${TEN[@]}" | jq .

say "6. Cost aggregate for the tenant"
curl -fsS "${BASE}/api/v1/costs" "${TEN[@]}" | jq '{total_requests, total_cost_usd, by_model}'

printf '\n\033[32mMulti-tenant walkthrough complete.\033[0m\n'
