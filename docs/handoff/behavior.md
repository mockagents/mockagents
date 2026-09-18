# Observable behavior and limitations

This reference distinguishes an intentional mock behavior from an operational failure. It describes source behavior, not every upstream provider feature.

| Situation | Observable behavior | Test implication |
| --- | --- | --- |
| No visible matching agent/model | Resolution error, adapter-specific not-found response | Check model, tenant scope and loaded fixtures |
| No matching scenario | First configured default, otherwise `_fallback` content | HTTP success can hide missing fixture coverage; assert scenario/metrics |
| Empty user input | Engine rejects unless image-only or supported empty tool-result case | Exercise each adapter's normalization |
| Same session concurrent calls | Turn generation serialized under session lock | Turn order follows lock acquisition, not a promised client ordering |
| Failed template generation | No turn/history/variable commit | Retry can still target same proposed turn |
| Client disconnect after generation | Output may be lost after state committed | No request idempotency/exactly-once guarantee |
| Tool result error | Individual internal result carries error | Does not automatically mean an HTTP error |
| Auth failure on managed route | `401`; insufficient role `403`; auth-store failure fails closed | Do not infer identity from UI cookies or list contents |
| Invalid credential on provider route | Auth-exempt route can continue anonymously | Use managed endpoints to verify credentials; provider success is not an auth test |
| Known Responses `previous_response_id` from another tenant | History lookup accepts the ID without ownership checking | Do not claim tenant isolation for this history store; application fix remains open |
| Responses `store: false` | Flag is echoed but history is still retained/addressable | Do not use the flag as a retention guarantee in this mock |
| Named-tenant quota exceeded | `429` rate / `402` spend where enforced | Anonymous traffic has no tenant quota |
| Pipeline node fails | `422`, classification code and partial trajectory | Assert what ran before failure |
| Live log queue/backpressure | Bounded asynchronous evidence can be dropped/delayed | Do not assume immediate complete audit-grade interaction history |
| Process restart | Memory sessions/resource stores reset | Persistent tenancy does not restore provider resources or conversations |
| Default replay miss | `404` without fallback | Fix cassette matching or explicitly choose recording mode |
| Strict replay miss | `503`, no upstream fallback | Strong offline test boundary |
| Readiness fails | `503` with named dependency | Process may still be live; fix fixtures/store/draining condition |
| Incomplete OIDC settings | SSO remains disabled; startup can succeed | Require all four primary settings and the domain map when deploying SSO |

## Determinism boundary

Static fixture selection and canned content are reproducible when request input, session state, declaration order and configuration are controlled. UUIDs, timestamps, fake/random template functions, tool `error_rate`, wall-clock latency and concurrent scheduling can differ. Seeded chaos is a scoped fault-selection mechanism, not a global seed for every random behavior. Session cleanup by the suite runner does not reset all agent-level failure counters.

For robust CI, use explicit content, unique sessions, authored error responses or deterministic fail-first faults, and assertions on semantic fields rather than volatile provider IDs. Pin fixture revision and binary/package version. Real SDK compatibility needs adapter/conformance tests and, where intended, scrubbed provider drift evidence.

## Fault layers

Agent chaos supports latency distributions, probabilistic/first-N HTTP errors, timeout simulation, rate-window limits, and connection faults. Streaming adds time-to-first-token, pacing, jitter, percentile-based delay sampling, truncation, and malformed frames. Semantic fixtures include refusals, finish-reason overrides, deliberately invalid OpenAI arguments, and labeled hallucinations.

Connection reset/empty/garbage faults attempt transport-level behavior; when hijacking is unavailable (for example HTTP/2) the implementation uses an HTTP fallback rather than pretending a TCP reset occurred. Stream truncation intentionally omits normal termination; it is not a complete successful stream. Always assert the client's recovery behavior at the layer being simulated.

Shared service fault policies exist for MCP/A2A, search/rerank/moderation, and vector partial results. Their fields and operation scope differ from `Agent.behavior.chaos`; use the relevant definition type and examples rather than copying one block into every kind. Request overrides can take precedence over inherited server settings; consult [chaos guide](../../site/docs/guides/chaos.md) and implementation before assuming a server rate is an absolute override.

## Fidelity and capacity limits

Embeddings are deterministic synthetic vectors; rerank/moderation/search are mock algorithms or fixture data. Realtime synthesizes protocol/audio behavior; it is not speech recognition or a voice model. MCP and A2A advertise and exercise implemented capabilities, not every possible extension. Provider Files/Batches and conversation stores are bounded in-memory models, not durable cloud resource services.

The vector store caps dimensions at 65,536, top-k at 1,000, and points per collection at 100,000. It performs local scoring, not an approximate-nearest-neighbor distributed index. Filters are supported subsets, not arbitrary provider query languages. The default A2A store bounds tasks (10,000), retained bytes (64 MiB), task history (256), and terminal task TTL (30 minutes), with CLI options to adjust them.

The main HTTP service needs at least one valid Agent; vector/search fixtures alone do not satisfy readiness. Agent writes are limited to 1 MiB, as are pipeline bodies. The global main-server body limit is configured in `server.Config`; do not infer it from an individual endpoint limit. Each file/cassette/stream surface has its own bounds.

## Known design constraints and improvement candidates

- Replicas retain separate registries, sessions, local logs, rate buckets and provider resource state. Sticky routing may help continuity but does not synchronize edits or evidence.
- Quota PUT persists settings and updates only the receiving process; another running replica's override map stays unchanged. Shared spend accounting is a separate mechanism.
- Interaction cost aggregates scan a bounded set of recent rows; a capped result is not guaranteed to represent all historical spend. Costs are estimates, not verified savings.
- Graph execution has no fan-in reducer, durable queue, retry scheduler, or distributed task recovery.
- Regex/template caches retain distinct authored entries for process lifetime; high-churn dynamic fixture installs merit cache-bound measurements.
- The OpenAPI document covers selected operations; the generated route catalog is the full built-in HTTP mounting inventory. Growing machine-readable coverage is a documented follow-up.
- Some source comments describe earlier implementation stages. Verify current wiring and tests before copying a comment into an external contract.

The Responses ownership/retention behavior above was reproduced with synthetic data during the [documentation re-review](../reviews/2026-09-18-documentation-review-summary.md). Other items are documented boundaries or improvement candidates. The [current action register](../reviews/2026-09-18-documentation-action-register.md) separates corrected prose from unresolved application and API-coverage work.
