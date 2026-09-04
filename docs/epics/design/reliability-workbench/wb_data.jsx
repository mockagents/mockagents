/* Workbench fixtures — realistic synthetic data, explicit provenance.
   Wire semantics per epic: PipelineResult.nodes[{node_id,agent_name,response,latency(ns)}],
   422 carries optional partial `result` + `error`. No fabricated log links. */
(function () {
  const STARTER_YAML = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support-starter
  description: Documented starter agent for first success
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  systemPrompt: |
    You are a helpful support agent for the demo store.
  tools:
    - name: lookup_order
      description: Look up an order by ID
      parameters:
        order_id: { type: string, required: true }
  behavior:
    scenarios:
      - name: order-status
        match:
          content_contains: "order status"
        response:
          content: "Happy to check that order for you."
      - name: default
        response:
          content: "Welcome to the demo store. How can I help?"
    streaming:
      enabled: true
      chunk_size: 4`;

  // node latencies on the wire are nanoseconds (Go duration JSON)
  const RUN_OK = {
    session_id: "sess_a1b2c3", status: 200,
    latency: 412800000, // ns
    nodes: [
      { node_id: "router", agent_name: "support-starter", latency: 96400000, response: { content: "Routing to order lookup." } },
      { node_id: "rag-lookup", agent_name: "rag-agent", latency: 201300000, response: { content: "Order ORD-12345 shipped 2026-08-30, ETA 2026-09-04." } },
      { node_id: "summarizer", agent_name: "summary-writer", latency: 115100000, response: { content: "Your order shipped and arrives Sep 4." } },
    ],
  };
  const RUN_PARTIAL = {
    session_id: "sess_d4e5f6", status: 422,
    error: { code: "NODE_FAILED", node_id: "rag-lookup", message: "scenario match failed: no scenario accepted input after normalization" },
    result: {
      latency: 310500000,
      nodes: [
        { node_id: "router", agent_name: "support-starter", latency: 88200000, response: { content: "Routing to order lookup." } },
        { node_id: "rag-lookup", agent_name: "rag-agent", latency: 222300000, response: null },
        // summarizer absent — unknown, not zero
      ],
    },
  };

  const LOGS = [
    { id: 5121, ts: "14:31:52Z", session: "sess_a1b2c3", agent: "support-starter", path: "/v1/chat/completions", status: 200, latency_ms: 96.4, scenario: "order-status", chaos: null, truncated: false, body_bytes: 1424 },
    { id: 5120, ts: "14:31:52Z", session: "sess_a1b2c3", agent: "rag-agent", path: "/v1/chat/completions", status: 200, latency_ms: 201.3, scenario: "grounded", chaos: null, truncated: false, body_bytes: 2210 },
    { id: 5119, ts: "14:31:51Z", session: "sess_a1b2c3", agent: "summary-writer", path: "/v1/chat/completions", status: 200, latency_ms: 115.1, scenario: "default", chaos: null, truncated: false, body_bytes: 980 },
    { id: 5112, ts: "14:20:07Z", session: "sess_d4e5f6", agent: "rag-agent", path: "/v1/chat/completions", status: 422, latency_ms: 222.3, scenario: null, chaos: null, truncated: false, body_bytes: 310 },
    { id: 5104, ts: "13:58:40Z", session: "sess_8e07aa", agent: "flaky-agent", path: "/v1/chat/completions", status: 503, latency_ms: 201.4, scenario: null, chaos: { fault: "error_injection", rate: 0.1, seed: 4211 }, truncated: false, body_bytes: 188 },
    { id: 5099, ts: "13:41:12Z", session: "sess_77b3d0", agent: "support-starter", path: "/v1/chat/completions", status: 200, latency_ms: 44.0, scenario: "default", chaos: null, truncated: true, body_bytes: 65536 },
  ];

  const AUDIT = [
    { id: 402, ts: "14:29:31Z", kind: "agent.updated", actor: "dev@local", role: "editor", target: "support-starter", detail: "If-Match a41f → persisted" },
    { id: 401, ts: "14:12:09Z", kind: "pipeline.run", actor: "dev@local", role: "editor", target: "support-triage", detail: "session sess_d4e5f6 · 422" },
    { id: 400, ts: "13:55:00Z", kind: "api_key.rotated", actor: "admin@acme.io", role: "admin", target: "key_2c91 (ci-bot)", detail: "one-time display acknowledged" },
    { id: 399, ts: "13:20:44Z", kind: "auth.denied", actor: "viewer@acme.io", role: "viewer", target: "PUT /api/v1/agents/support-starter", detail: "403 editor required" },
    { id: 398, ts: "12:02:10Z", kind: "unknown.future_event", actor: "system", role: "—", target: "—", detail: "rendered read-only; kind not in current taxonomy" },
  ];

  const KEYS = [
    { id: "key_7f3a", name: "bootstrap-admin", prefix: "mak_1c3a9e0f", role: "admin", last_used: "2 min ago" },
    { id: "key_2c91", name: "ci-bot", prefix: "mak_a81f44b2", role: "viewer", last_used: "12 min ago" },
    { id: "key_55de", name: "deploy-bot", prefix: "mak_55de9c10", role: "editor", last_used: "3 h ago" },
  ];

  const AGENTS = [
    { name: "support-starter", kind: "Agent", protocol: "openai-chat-completions", scenarios: 2, tools: 1, revision: "a41f", persisted: true, starter: true },
    { name: "rag-agent", kind: "Agent", protocol: "anthropic-messages", scenarios: 2, tools: 1, revision: "77c0", persisted: true },
    { name: "summary-writer", kind: "Agent", protocol: "openai-chat-completions", scenarios: 1, tools: 0, revision: "b2d8", persisted: true },
    { name: "flaky-agent", kind: "Agent", protocol: "openai-chat-completions", scenarios: 1, tools: 0, revision: "09aa", persisted: true, chaos: true },
  ];
  const PIPELINES = [
    { name: "support-triage", agents: 3, edges: 2, revision: "5e12", source: "examples/support-triage.yaml" },
  ];

  window.WB = { STARTER_YAML, RUN_OK, RUN_PARTIAL, LOGS, AUDIT, KEYS, AGENTS, PIPELINES,
    ns2ms: (ns) => (ns / 1e6).toFixed(1) + " ms" };
})();
