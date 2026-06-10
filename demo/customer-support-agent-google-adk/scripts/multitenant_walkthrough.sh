#!/usr/bin/env bash
# Multi-tenancy + quota + cost walkthrough (native Gemini surface).
#
# Prereqs:
#   * MockAgents started in multi-tenant mode (MOCKAGENTS_MULTI_TENANT=1).
#   * The bootstrap PLATFORM key from the server logs on first start:
#       docker compose logs mockagents | grep "Bootstrap admin key"
#     Pass it in:  BOOTSTRAP_KEY=<key> ./scripts/multitenant_walkthrough.sh
#   * jq on PATH.
#
# IMPORTANT: on the Gemini surface the tenant API key must go in the
# `Authorization: Bearer` header. The genai client's normal `x-goog-api-key`
# header is NOT used for tenant resolution, so quota/cost would not attribute to
# the tenant. (MockAgents now logs + quota-enforces the Gemini route, so with the
# Bearer header this attributes correctly.)
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
: "${BOOTSTRAP_KEY:?Set BOOTSTRAP_KEY to the platform key printed in the server logs}"
PLAT=(-H "authorization: Bearer ${BOOTSTRAP_KEY}")
JSON=(-H "content-type: application/json")
MODEL="gemini-2.0-flash"

command -v jq >/dev/null || { echo "jq is required"; exit 1; }
say() { printf '\n\033[36m=== %s ===\033[0m\n' "$1"; }

say "1. Find the default tenant id (the bootstrap key's tenant)"
TENANT_ID=$(curl -fsS "${BASE}/api/v1/tenants" "${PLAT[@]}" | jq -r '.[] | select(.name=="default") | .id')
echo "default tenant id: ${TENANT_ID}"

say "2. Mint an editor app key on the default tenant"
TENANT_KEY=$(curl -fsS "${BASE}/api/v1/tenants/${TENANT_ID}/keys" "${PLAT[@]}" "${JSON[@]}" \
  -d '{"name":"adk-app","role":"editor"}' | jq -r '.plaintext // .Plaintext')
echo "app key: ${TENANT_KEY:0:12}..."
# Pass the tenant key as a Bearer token (see note above).
TEN=(-H "authorization: Bearer ${TENANT_KEY}")

say "3. Set a tiny quota (2 req/s, burst 3, \$0.00003/month — Gemini is cheap)"
curl -fsS -X PUT "${BASE}/api/v1/tenants/${TENANT_ID}/quota" "${PLAT[@]}" "${JSON[@]}" \
  -d '{"rate_per_sec":2,"rate_burst":3,"monthly_spend_usd":0.00003}' | jq .

say "4. Burst 10 generateContent requests — expect 429 (rate) and/or 402 (spend)"
for i in $(seq 1 10); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/v1beta/models/${MODEL}:generateContent" \
    "${TEN[@]}" "${JSON[@]}" \
    -d '{"contents":[{"role":"user","parts":[{"text":"hello there how are you today"}]}]}')
  echo "  request ${i}: HTTP ${code}"
done
echo "  (200 = ok, 429 = rate limited, 402 = monthly spend cap exceeded)"

say "5. Tenant quota + usage"
curl -fsS "${BASE}/api/v1/quota" "${TEN[@]}" | jq .

say "6. Cost aggregate for the tenant"
curl -fsS "${BASE}/api/v1/costs" "${TEN[@]}" | jq '{total_requests, total_cost_usd, by_model}'

printf '\n\033[32mMulti-tenant walkthrough complete.\033[0m\n'
