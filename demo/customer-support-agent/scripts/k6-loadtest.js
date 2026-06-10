// k6 load test against the MockAgents load-target agent (model gpt-3.5-turbo).
//
// Load-test your LLM client path for free: MockAgents supplies realistic,
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
    // The mock injects p95 TTFT ~1.4s; the full streamed body finishes later.
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<5000"],
  },
};

export default function () {
  const res = http.post(
    `${BASE}/v1/chat/completions`,
    JSON.stringify({
      model: "gpt-3.5-turbo",
      stream: true,
      messages: [{ role: "user", content: "hello, I need help with my order" }],
    }),
    {
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${KEY}`,
      },
    },
  );

  check(res, {
    "status is 200": (r) => r.status === 200,
    "streamed body received": (r) => r.body && r.body.includes("data:"),
  });
}
