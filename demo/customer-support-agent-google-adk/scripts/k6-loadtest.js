// k6 load test against the MockAgents Gemini load-target agent.
//
// Load-test your Gemini client path for free: MockAgents supplies realistic,
// long-tailed streaming latency (lognormal TTFT/ITL from load-target-gemini.yaml)
// with no tokens, no rate limits, no bill.
//
//   k6 run scripts/k6-loadtest.js
//   k6 run -e BASE=http://localhost:8080 -e VUS=50 -e DURATION=1m scripts/k6-loadtest.js
//
// Install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/

import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE || "http://localhost:8080";
const KEY = __ENV.MOCKAGENTS_API_KEY || "mock-key";
const MODEL = "gemini-1.5-flash"; // routes to the load-target agent

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
    `${BASE}/v1beta/models/${MODEL}:streamGenerateContent?alt=sse`,
    JSON.stringify({
      contents: [{ role: "user", parts: [{ text: "hello, I need help with my order" }] }],
    }),
    { headers: { "content-type": "application/json", "x-goog-api-key": KEY } },
  );

  check(res, {
    "status is 200": (r) => r.status === 200,
    "streamed body received": (r) => r.body && r.body.includes("candidates"),
  });
}
