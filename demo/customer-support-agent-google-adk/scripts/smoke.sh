#!/usr/bin/env bash
# Curl-based smoke tests for the ADK demo's MockAgents fixtures — no Python needed.
# Exercises the native Gemini surface: health, scenario matching, the functionCall
# turn, streaming, and the chaos (flaky) agent.
#
#   ./scripts/smoke.sh
#   BASE=http://host:8080 ./scripts/smoke.sh
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
KEY="${MOCKAGENTS_API_KEY:-mock-key}"
H=(-H "x-goog-api-key: ${KEY}" -H "content-type: application/json")
SUPPORT="gemini-2.0-flash"
FLAKY="gemini-2.5-flash"

say() { printf '\n\033[36m=== %s ===\033[0m\n' "$1"; }

say "Health"
curl -fsS "${BASE}/api/v1/health"; echo

say "Scenario match: greeting (no tool, single turn)"
curl -fsS "${BASE}/v1beta/models/${SUPPORT}:generateContent" "${H[@]}" -d '{
  "contents":[{"role":"user","parts":[{"text":"hello"}]}]
}'; echo

say "Function call: order lookup (turn 1, expect a functionCall part for lookup_order)"
curl -fsS "${BASE}/v1beta/models/${SUPPORT}:generateContent" "${H[@]}" -d '{
  "contents":[{"role":"user","parts":[{"text":"where is my order"}]}],
  "tools":[{"functionDeclarations":[{"name":"lookup_order","parameters":{"type":"object","properties":{"order_id":{"type":"string"}}}}]}]
}'; echo

say "Final answer: turn 2 (send the functionResponse back, expect finishReason STOP)"
curl -fsS "${BASE}/v1beta/models/${SUPPORT}:generateContent" "${H[@]}" -d '{
  "contents":[
    {"role":"user","parts":[{"text":"where is my order"}]},
    {"role":"model","parts":[{"functionCall":{"name":"lookup_order","args":{"order_id":"ORD-12345"}}}]},
    {"role":"user","parts":[{"functionResponse":{"name":"lookup_order","response":{"ORDER_RESULT":true,"status":"shipped"}}}]}
  ]
}'; echo

say "Streaming (Gemini SSE) — first lines only"
curl -fsS -N "${BASE}/v1beta/models/${SUPPORT}:streamGenerateContent?alt=sse" "${H[@]}" -d '{
  "contents":[{"role":"user","parts":[{"text":"hello"}]}]
}' | head -n 6; echo

say "Chaos: flaky agent — first 2 calls 503, 3rd recovers"
for i in 1 2 3; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/v1beta/models/${FLAKY}:generateContent" "${H[@]}" \
    -d '{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}')
  echo "  attempt ${i}: HTTP ${code}"
done

say "Costs aggregate (per-model / per-agent)"
curl -fsS "${BASE}/api/v1/costs" -H "authorization: Bearer ${KEY}"; echo

printf '\n\033[32mSmoke complete.\033[0m\n'
