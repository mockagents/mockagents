#!/usr/bin/env bash
# Curl-based smoke tests for the Responses API surface — no Python needed.
# Exercises: health, a single-turn message, the tool-call turn, named-event
# streaming, and previous_response_id chaining. Run with MockAgents up on :8080
# serving demo/responses-api-agent/mockagents.
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

say "Responses: single-turn greeting (string input)"
curl -fsS "${BASE}/v1/responses" "${AUTH[@]}" "${JSON[@]}" -d '{
  "model": "gpt-4o",
  "input": "hello"
}'; echo

say "Responses: tool call (turn 1 of the loop, expect a function_call output item)"
curl -fsS "${BASE}/v1/responses" "${AUTH[@]}" "${JSON[@]}" \
  -H "x-session-id: ${SID}" -d '{
  "model": "gpt-4o",
  "input": "where is my order?"
}'; echo

say "Responses: resolution (same session, turn 2, feed the function_call_output back)"
curl -fsS "${BASE}/v1/responses" "${AUTH[@]}" "${JSON[@]}" \
  -H "x-session-id: ${SID}" -d '{
  "model": "gpt-4o",
  "input": [
    {"role":"user","content":"where is my order?"},
    {"type":"function_call_output","call_id":"call_0","output":"{\"status\":\"shipped\"}"}
  ]
}'; echo

say "Responses: streaming — named response.* events (first lines only)"
curl -fsS -N "${BASE}/v1/responses" "${AUTH[@]}" "${JSON[@]}" -d '{
  "model": "gpt-4o",
  "stream": true,
  "input": "hello"
}' | head -n 12; echo

say "Responses: stateful previous_response_id chaining"
RESP1=$(curl -fsS "${BASE}/v1/responses" "${AUTH[@]}" "${JSON[@]}" \
  -H "x-session-id: chain-${SID}" -d '{"model":"gpt-4o","input":"hello"}')
ID1=$(printf '%s' "$RESP1" | sed -n 's/.*"id":"\(resp_[^"]*\)".*/\1/p')
echo "  turn 1 id: ${ID1}"
curl -fsS "${BASE}/v1/responses" "${AUTH[@]}" "${JSON[@]}" \
  -H "x-session-id: chain-${SID}" -d "{
  \"model\": \"gpt-4o\",
  \"previous_response_id\": \"${ID1}\",
  \"input\": \"and my order?\"
}"; echo

printf '\n\033[32mSmoke complete.\033[0m\n'
