// k6 load test against a MockAgents target (FB-05).
//
// Load-test your LLM app's HTTP path for free: point k6 at the mock instead of a
// real provider — no tokens, no rate limits, no bill. The `load-target` agent
// streams with realistic latency percentiles, so TTFT / inter-token timing look
// production-like.
//
// Run:
//   mockagents start --agents-dir examples
//   k6 run examples/loadtest/k6.js
//
// Tune load with env vars: VUS (virtual users), DURATION, BASE (server URL).
import http from 'k6/http'
import { check } from 'k6'
import { Trend } from 'k6/metrics'

const BASE = __ENV.BASE || 'http://localhost:8080'

// k6's plain http client buffers the whole response, so the meaningful metric is
// the TOTAL streamed latency (TTFT + every inter-token delay) — i.e. how long a
// full streamed completion takes end to end. (TTFB / http_req_waiting is NOT
// TTFT for SSE: the 200 headers flush before the time-to-first-token delay.)
const streamTotal = new Trend('stream_total_ms', true)

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || '30s',
  thresholds: {
    http_req_failed: ['rate<0.01'],   // <1% errors
    stream_total_ms: ['p(95)<8000'],  // p95 full streamed completion under 8s
  },
}

export default function () {
  const res = http.post(
    `${BASE}/v1/chat/completions`,
    JSON.stringify({
      model: 'load-target-model',
      stream: true,
      messages: [{ role: 'user', content: 'hello' }],
    }),
    { headers: { 'Content-Type': 'application/json', Authorization: 'Bearer mock' } },
  )
  streamTotal.add(res.timings.duration) // full streamed completion time
  check(res, {
    'status 200': (r) => r.status === 200,
    'is SSE': (r) => String(r.headers['Content-Type'] || '').includes('text/event-stream'),
    'streamed [DONE]': (r) => String(r.body).includes('[DONE]'),
  })
}
