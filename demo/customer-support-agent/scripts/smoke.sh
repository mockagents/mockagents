#!/usr/bin/env bash
# Curl-based smoke tests for the demo's MockAgents fixtures — no Python needed.
# Exercises: health, scenario matching, the tool-call turn, streaming, and the
# chaos (flaky) agent. Run with MockAgents up on :8080.
#
#   ./scripts/smoke.sh           # against http://localhost:8080
#   BASE=http://host:8080 ./scripts/smoke.sh
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
KEY="${MOCKAGENTS_API_KEY:-mock-key}"
AUTH=(-H "authorization: Bearer ${KEY}")
JSON=(-H "content-type: application/json")
SID="smoke-$(date +%s)-$$"

say() { printf '\n\033[36m=== %s ===\033[0m\n' "$1"; }

say "Health"
curl -fsS "${BASE}/api/v1/health"; echo

say "Models advertised by MockAgents"
curl -fsS "${BASE}/v1/models" "${AUTH[@]}"; echo

say "Scenario match: greeting (no tool, single turn)"
curl -fsS "${BASE}/v1/chat/completions" "${AUTH[@]}" "${JSON[@]}" -d '{
  "model": "gpt-4o",
  "messages": [{"role":"user","content":"hello"}]
}'; echo

say "Tool call: order lookup (turn 1 of the loop, expect finish_reason=tool_calls)"
curl -fsS "${BASE}/v1/chat/completions" "${AUTH[@]}" "${JSON[@]}" \
  -H "x-session-id: ${SID}" -d '{
  "model": "gpt-4o",
  "messages": [{"role":"user","content":"where is my order"}]
}'; echo

say "Final answer: same session, turn 2 (expect finish_reason=stop)"
curl -fsS "${BASE}/v1/chat/completions" "${AUTH[@]}" "${JSON[@]}" \
  -H "x-session-id: ${SID}" -d '{
  "model": "gpt-4o",
  "messages": [
    {"role":"user","content":"where is my order"},
    {"role":"assistant","content":null,"tool_calls":[{"id":"call_0","type":"function","function":{"name":"lookup_order","arguments":"{\"order_id\":\"ORD-12345\"}"}}]},
    {"role":"tool","tool_call_id":"call_0","content":"{\"status\":\"shipped\"}"}
  ]
}'; echo

say "Streaming (SSE deltas) — first lines only"
curl -fsS -N "${BASE}/v1/chat/completions" "${AUTH[@]}" "${JSON[@]}" -d '{
  "model": "gpt-4o",
  "stream": true,
  "messages": [{"role":"user","content":"hello"}]
}' | head -n 6; echo

say "Chaos: flaky agent — first 2 calls 503, 3rd recovers"
for i in 1 2 3; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/v1/chat/completions" \
    "${AUTH[@]}" "${JSON[@]}" -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}')
  echo "  attempt ${i}: HTTP ${code}"
done

say "Costs aggregate (per-model / per-agent)"
curl -fsS "${BASE}/api/v1/costs" "${AUTH[@]}"; echo

printf '\n\033[32mSmoke complete.\033[0m\n'
