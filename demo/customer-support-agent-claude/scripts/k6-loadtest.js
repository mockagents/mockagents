// k6 load test against the MockAgents Anthropic load-target agent.
//
// Load-test your Claude client path for free: MockAgents supplies realistic,
// long-tailed streaming latency (lognormal TTFT/ITL from load-target.yaml) with
// no tokens, no rate limits, no bill.
//
//   k6 run scripts/k6-loadtest.js
//   k6 run -e BASE=http://localhost:8080 -e VUS=50 -e DURATION=1m scripts/k6-loadtest.js
//
// Install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/

import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE || "http://localhost:8080";
const KEY = __ENV.MOCKAGENTS_API_KEY || "mock-key";

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || "30s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<5000"],
  },
};

export default function () {
  const res = http.post(
    `${BASE}/v1/messages`,
    JSON.stringify({
      model: "claude-3-sonnet-20240229", // routes to the load-target agent
      max_tokens: 512,
      stream: true,
      messages: [{ role: "user", content: "hello, I need help with my order" }],
    }),
    {
      headers: {
        "content-type": "application/json",
        "x-api-key": KEY,
        "anthropic-version": "2023-06-01",
      },
    },
  );

  check(res, {
    "status is 200": (r) => r.status === 200,
    "streamed body received": (r) => r.body && r.body.includes("event: message_start"),
  });
}
