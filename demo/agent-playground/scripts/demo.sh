#!/usr/bin/env bash
# A curl tour of the Agent Playground API. Start the server first:
#
#   go run ./demo/agent-playground/cmd/playground serve
#   ./demo/agent-playground/scripts/demo.sh            # or BASE=http://host:7070 ./demo.sh
#
# Needs only bash and curl. JSON is pretty-printed with python3 if available.
set -euo pipefail
BASE="${BASE:-http://127.0.0.1:7070}"
AUTH=()
if [[ -n "${PLAYGROUND_TOKEN:-}" ]]; then AUTH=(-H "Authorization: Bearer ${PLAYGROUND_TOKEN}"); fi

pretty() { if command -v python3 >/dev/null; then python3 -m json.tool; else cat; fi; }
field() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }
step() { printf '\n\033[1;34m== %s\033[0m\n' "$*"; }
post() { curl -fsS "${AUTH[@]}" -H 'Content-Type: application/json' -X POST "$BASE$1" -d "$2"; }
get() { curl -fsS "${AUTH[@]}" "$BASE$1"; }

step "Service info"
get /api/info | pretty

step "1. Research brief (orchestrator + tools + SLM summary + guard + judge), auto review"
RUN=$(post '/api/workflows/research-brief/runs?wait=30' '{"input":{"topic":"retry strategies for LLM APIs"},"options":{"review_mode":"auto"}}')
echo "$RUN" | field "d['id'], d['status'], d['stats']"
echo "$RUN" | field "d['output']['summary']"

step "2. Catch a planted hallucination (guard + judge -> LLM regeneration)"
post '/api/workflows/research-brief/runs?wait=30' '{"input":{"topic":"retry strategies","simulate_hallucination":true},"options":{"review_mode":"auto"}}' \
  | field "'regenerations:', d['output']['regenerations'], '| flags caught:', d['steps'][3]['notes']"

step "3. Support triage: ambiguous ticket, SLM unsure -> LLM decides"
post '/api/workflows/support-triage/runs?wait=30' '{"input":{"ticket":"My last invoice looks wrong and since then the app crashes on login."},"options":{"review_mode":"auto"}}' \
  | field "d['output']['classification'], '-> escalated:', d['output']['escalated'], '| routed to', d['output']['routed_to']"

step "4. Decision review: proposers disagree, arbiter decides"
post '/api/workflows/decision-review/runs?wait=30' '{"input":{"question":"Should we rewrite the legacy billing module this quarter?"},"options":{"review_mode":"auto"}}' \
  | field "d['output']['method'], d['output']['verdict']"

step "5. Resilience drill: 429 / 503 / timeout / reset / truncation / bad args / stream cut"
post '/api/workflows/resilience-drill/runs?wait=60' '{"input":{},"options":{"review_mode":"auto"}}' \
  | field "'\n'.join(f\"{c['case']:<17} passed={c['passed']!s:<5} {c['observed']}\" for c in d['output']['cases'])"

step "6. Human in the loop: refund approval, revise, approve (manual review)"
RUN=$(post '/api/workflows/support-triage/runs?wait=30' '{"input":{"ticket":"I was charged twice for order ORD-1001. Please refund the duplicate charge."},"options":{"review_mode":"manual"}}')
ID=$(echo "$RUN" | field "d['id']")
echo "run $ID is $(echo "$RUN" | field "d['status'] + '/' + d['phase']")"
REV=$(get "/api/reviews?status=pending&blocking=true&run_id=$ID" | field "d['reviews'][0]['id']")
echo "approving tool call: $(get "/api/reviews/$REV" | field "d['content']")"
post "/api/reviews/$REV/decision" '{"action":"approve","reviewer":"demo.sh"}' >/dev/null
sleep 1
GATE=$(get "/api/reviews?status=pending&blocking=true&run_id=$ID" | field "d['reviews'][0]['id']")
post "/api/reviews/$GATE/decision" '{"action":"revise","comment":"Add an apology for the delay.","reviewer":"demo.sh"}' >/dev/null
sleep 1
GATE=$(get "/api/reviews?status=pending&blocking=true&run_id=$ID" | field "d['reviews'][0]['id']")
echo "revised draft: $(get "/api/reviews/$GATE" | field "d['content']")"
post "/api/reviews/$GATE/decision" '{"action":"approve","reviewer":"demo.sh"}' >/dev/null
sleep 1
echo "final status: $(get "/api/runs/$ID" | field "d['status']")"

step "7. Reconfigure an agent at runtime: pin the summarizer to the LLM tier"
curl -fsS "${AUTH[@]}" -X PATCH -H 'Content-Type: application/json' "$BASE/api/agents/summarizer" -d '{"tier":"llm"}' | field "d['effective_route']"
post /api/config/reset '{}' >/dev/null && echo "(config reset)"

step "8. The mock's side: interactions for run $ID"
get "/api/mock/logs?session_prefix=$ID&limit=20" | field "'\n'.join(f\"{l['agent_name']:<24} scenario={l.get('scenario_name','')!s:<16} cost=\${l.get('cost_usd',0):.6f}\" for l in d)"

step "Runs by status (none may be stuck)"
get /api/runs | field "d['counts']"
