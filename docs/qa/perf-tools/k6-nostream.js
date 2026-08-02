// TC-PERF-02 — non-streaming throughput ramp (MA-QA-PTP-001).
// Usage: k6 run docs/qa/perf-tools/k6-nostream.js
// Env:   BASE (default http://127.0.0.1:8080)
//
// Use 127.0.0.1, not localhost: on Windows the IPv6-first resolution costs
// ~200ms per request against an IPv4-bound server (see TROUBLESHOOTING.md).
import http from 'k6/http'
import { check } from 'k6'

export const options = {
  scenarios: {
    ramp: {
      executor: 'ramping-vus',
      stages: [
        { duration: '60s', target: 10 },
        { duration: '60s', target: 50 },
        { duration: '60s', target: 200 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    http_req_duration: ['p(95)<50'],
  },
}

export default function () {
  const res = http.post(
    `${__ENV.BASE || 'http://127.0.0.1:8080'}/v1/chat/completions`,
    JSON.stringify({
      model: 'perf-echo-model',
      messages: [{ role: 'user', content: 'hello' }],
    }),
    { headers: { 'Content-Type': 'application/json', Authorization: 'Bearer mock' } },
  )
  check(res, { 'status 200': (r) => r.status === 200 })
}
