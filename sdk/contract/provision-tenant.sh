#!/usr/bin/env bash
# Provision the multi-tenant leg of the cross-SDK contract suite.
#
# Against a server started with MOCKAGENTS_MULTI_TENANT=1 and a known platform
# key (MOCKAGENTS_BOOTSTRAP_KEY), this:
#   1. creates a tenant                      POST /api/v1/tenants          (platform key)
#   2. mints an editor key for that tenant   POST /api/v1/tenants/{id}/keys (platform key)
#   3. registers the tenant-owned agent      POST /api/v1/agents           (tenant key)
# and prints ONLY the tenant key on stdout, for MOCKAGENTS_CONTRACT_TENANT_KEY.
#
# Inputs (environment):
#   MOCKAGENTS_CONTRACT_URL           server base URL, e.g. http://127.0.0.1:18080
#   MOCKAGENTS_CONTRACT_PLATFORM_KEY  the bootstrap platform key
#
# Note: the agent write API persists the agent into the server's agents
# directory, so start the multi-tenant server on a COPY of sdk/contract/agents.
# Needs only bash, curl and sed.
set -euo pipefail

url="${MOCKAGENTS_CONTRACT_URL:?set MOCKAGENTS_CONTRACT_URL}"
platform_key="${MOCKAGENTS_CONTRACT_PLATFORM_KEY:?set MOCKAGENTS_CONTRACT_PLATFORM_KEY}"
here="$(cd "$(dirname "$0")" && pwd)"
tenant_name="${MOCKAGENTS_CONTRACT_TENANT_NAME:-contract-tenant}"

tenant_json="$(curl -fsS -X POST "$url/api/v1/tenants" \
  -H "Authorization: Bearer $platform_key" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$tenant_name\"}")"
tenant_id="$(printf '%s' "$tenant_json" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
if [ -z "$tenant_id" ]; then
  echo "provision-tenant: could not read the tenant id from: $tenant_json" >&2
  exit 1
fi

key_json="$(curl -fsS -X POST "$url/api/v1/tenants/$tenant_id/keys" \
  -H "Authorization: Bearer $platform_key" \
  -H 'Content-Type: application/json' \
  -d '{"name":"contract-editor","role":"editor"}')"
tenant_key="$(printf '%s' "$key_json" | sed -n 's/.*"plaintext"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
if [ -z "$tenant_key" ]; then
  echo "provision-tenant: the key response carried no plaintext" >&2
  exit 1
fi

status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$url/api/v1/agents" \
  -H "Authorization: Bearer $tenant_key" \
  -H 'Content-Type: application/yaml' \
  --data-binary "@$here/tenant/contract-tenant-agent.yaml")"
if [ "$status" != "201" ]; then
  echo "provision-tenant: registering the tenant agent returned HTTP $status (want 201)" >&2
  exit 1
fi

echo "provision-tenant: tenant $tenant_id ready with its own contract-agent" >&2
printf '%s\n' "$tenant_key"
