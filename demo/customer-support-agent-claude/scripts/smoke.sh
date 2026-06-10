#!/usr/bin/env bash
# Curl-based smoke tests for the Claude demo's MockAgents fixtures — no Python or
# Node needed. Exercises the Anthropic Messages API surface: health, scenario
# matching, the tool_use turn, streaming, and the chaos (flaky) agent.
#
#   ./scripts/smoke.sh
#   BASE=http://host:8080 ./scripts/smoke.sh
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
KEY="${MOCKAGENTS_API_KEY:-mock-key}"
H=(-H "x-api-key: ${KEY}" -H "anthropic-version: 2023-06-01" -H "content-type: application/json")
SUPPORT="claude-3-5-sonnet-latest"
FLAKY="claude-3-5-sonnet-20241022"

say() { printf '\n\033[36m=== %s ===\033[0m\n' "$1"; }

say "Health"
curl -fsS "${BASE}/api/v1/health"; echo

say "Scenario match: greeting (no tool, single turn)"
curl -fsS "${BASE}/v1/messages" "${H[@]}" -d "{
  \"model\": \"${SUPPORT}\", \"max_tokens\": 256,
  \"messages\": [{\"role\":\"user\",\"content\":\"hello\"}]
}"; echo

say "Tool call: order lookup (turn 1, expect stop_reason=tool_use + mcp__acme__lookup_order)"
curl -fsS "${BASE}/v1/messages" "${H[@]}" -d "{
  \"model\": \"${SUPPORT}\", \"max_tokens\": 256,
  \"messages\": [{\"role\":\"user\",\"content\":\"where is my order\"}]
}"; echo

say "Final answer: turn 2 (send the tool_result back, expect stop_reason=end_turn)"
curl -fsS "${BASE}/v1/messages" "${H[@]}" -d "{
  \"model\": \"${SUPPORT}\", \"max_tokens\": 256,
  \"messages\": [
    {\"role\":\"user\",\"content\":\"where is my order\"},
    {\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"mcp__acme__lookup_order\",\"input\":{\"order_id\":\"ORD-12345\"}}]},
    {\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"toolu_1\",\"content\":[{\"type\":\"text\",\"text\":\"ORDER_RESULT shipped\"}]}]}
  ]
}"; echo

say "Streaming (Anthropic SSE events) — first lines only"
curl -fsS -N "${BASE}/v1/messages" "${H[@]}" -d "{
  \"model\": \"${SUPPORT}\", \"max_tokens\": 256, \"stream\": true,
  \"messages\": [{\"role\":\"user\",\"content\":\"hello\"}]
}" | head -n 8; echo

say "Chaos: flaky agent — first 2 calls 503, 3rd recovers"
for i in 1 2 3; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/v1/messages" "${H[@]}" \
    -d "{\"model\":\"${FLAKY}\",\"max_tokens\":64,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
  echo "  attempt ${i}: HTTP ${code}"
done

say "Costs aggregate (per-model / per-agent)"
curl -fsS "${BASE}/api/v1/costs" -H "authorization: Bearer ${KEY}"; echo

printf '\n\033[32mSmoke complete.\033[0m\n'
