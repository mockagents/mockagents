# Glossary

| Term | Meaning in this repository |
| --- | --- |
| Agent | Declarative scenario/tool response fixture served by the neutral engine |
| Adapter | Compiled Go translator between a provider wire protocol and engine/service types |
| Scenario | Ordered match rule and response; a missing match block is a default |
| Fallback | Built-in `_fallback` response when no explicit rule or default answers |
| Tool call | Configured request for a function/tool in a provider response |
| Tool result | Simulated engine result, or the application's returned wire result; context distinguishes them |
| Strict tools | Off/warn/strict checks for IDs, choice constraints and strict schemas |
| Pipeline | In-process topology over named mock agents |
| Node | Pipeline-local ID referring to an agent definition |
| Trajectory | Ordered tool calls or pipeline nodes observed across execution turns |
| Engine session | Tenant/agent/client-scoped conversation state in memory |
| Login session | Revocable SSO credential stored by tenancy; unrelated to conversation state |
| MCP session | Transport lifecycle state for a standalone MCP connection/session |
| A2A task | Protocol task containing status/history/artifacts in a separate task store |
| Principal | Server-resolved tenant/role/credential identity on request context |
| Global fixture | Agent with empty tenant ownership, visible in the global catalog |
| Role floor | Minimum management privilege declared for a mounted route |
| Capability | Advisory identity response derived from actual mounted routes and role |
| ETag | Opaque conditional-write version; not the same as effective configuration hash |
| Effective revision | Hash describing the definition currently running in memory |
| Source revision | Hash describing the backing fixture's current bytes |
| Cassette | JSONL recording plus in-memory request-hash index |
| Strict replay | Offline replay mode that rejects upstream configuration and returns 503 on misses |
| Chaos | Configured timing, status, connection, stream or service fault injection |
| Hallucination fixture | Deliberately incorrect labeled output for testing application guardrails |
| Interaction log | Diagnostic request/node evidence; bounded asynchronous capture and retention apply |
| Audit event | Control-plane mutation/denial record, distinct from request body history |
| Estimated spend | Price-table calculation from mock usage; not actual invoicing |
| Conformance | Tests of the implemented wire profile against expected client/protocol contracts |
| Drift | Difference between fixtures/types/schemas or SDK/provider/mock artifacts, depending on tool |
| Candidate SHA | Exact source commit whose artifacts and release checks are being verified |
