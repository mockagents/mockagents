# Architecture overview

MockAgents is primarily a modular Go process. `cmd/mockagents/start.go` composes the fixture registries, neutral engine, HTTP adapter registry, management handlers, state stores, observability, and optional tenancy. A separate Next.js console calls its management API. `mcp`, `a2a`, `record`, and `replay` are distinct CLI serving modes with their own listener and lifecycle wiring.

The existing [architecture reference](../../ARCHITECTURE.md) gives a longer package walkthrough. This handoff adds current surfaces and operational constraints; [diagrams](diagrams.md) make the paths explicit.

## Request execution

The main HTTP chain enters OTel wrapping when configured, request context, panic recovery, structured logging, CORS, body limit, optional browser Realtime credential extraction, optional tenancy authentication, tenant-context propagation, interaction capture, metrics, quota enforcement, and routing. The wrappers are constructed inside-out in [server.go](../../internal/server/server.go). Capture is outside quota enforcement so quota denials can be observed; rejected authentication takes a separate denial-audit path.

Adapters decode and normalize protocol fields into `engine.InboundRequest`. The engine resolves an agent in tenant scope, stamps metadata, checks cancellation and pre-generation chaos, validates strict-tool request constraints, selects a scenario in a session transaction, renders content, applies tool-loop convergence/tool-choice behavior, resolves simulated tools, commits the turn, and applies post-generation latency. Adapters then build wire JSON or stream frames. Embeddings, moderation, vector/search, resource APIs, and standalone MCP/A2A have their own service logic; they are not all calls through `ProcessRequestContext`.

## Boundaries and extension seams

| Boundary | Contract | Invariant |
| --- | --- | --- |
| Wire ↔ engine | `InboundRequest`, `Response`, request metadata | Engine does not depend on provider wire packages |
| Authentication ↔ engine | `engine.WithTenantID` on request context | Principal-derived tenant is authoritative; client tenant header is not authorization |
| Management routing | `mountManaged` + `managementRouteFloors` | Every managed route declares a role floor; missing floor panics during construction |
| Audit ↔ principal | Principal extraction callback | Audit storage need not import tenancy |
| Pipeline ↔ interaction logging | `NodeRecorder` | Engine can execute without server/storage imports; recorder must not block |
| Tenancy ↔ persistence | `tenancy.Store` | SQLite/Postgres implement the same credential and mutation policy |
| Protocol extension | `adapter.Adapter` (`Name`, `Routes`) | Register routes and preserve tenant, errors, stream, and metadata behavior |
| Session storage | `state.Store` | Returned sessions are shared pointers; mutate through session locks |

These are compiled Go extension seams, not a runtime plugin loader. YAML supplies data and supported template functions; it does not load arbitrary executables.

## Data and concurrency architecture

Agent registry lookup separates global and tenant-owned entries, with tenant-owned matches taking precedence where implemented. Named session keys include tenant, agent, and client session; requests without a session use disposable state. `ApplyTurn` serializes one conversation and commits generated history/variables together; different sessions can progress concurrently. Pipeline nodes have additional run/pipeline/node session scope.

The default engine store is in memory with TTL/history/count bounds. Provider Files, Batches, Conversations/Responses, vector collections, MCP sessions and A2A tasks also have service-specific memory ownership. Conversations and standalone Responses history are tenant-keyed; the empty tenant has an isolated anonymous namespace. SQLite holds interactions and audit records; tenancy defaults to SQLite and can use Postgres. Postgres does not turn these other process-local stores into distributed state. [Data models](data-models.md) describes persistence and relationships.

Interaction logging uses a bounded asynchronous worker and live broadcaster. This protects response latency but makes logs an eventually visible, potentially incomplete diagnostic record under saturation or abrupt termination. Audit and usage data have separate paths. Cost accounting is estimated, post-response work; it is not a payment ledger or a transactional reservation of a spend budget.

## Deployment and integration architecture

The Go binary serves local SDK tests directly or runs in a container/Helm Deployment. `/agents` supplies fixtures and `/data` supplies writable state in the container. The GUI is separately built and hosted; it forwards its server-side credential to the management API. MCP and A2A listeners must be addressed at their configured ports rather than assumed to exist on the main port.

For shared operation, multi-tenant management protection, upstream network access, log body retention, and replica consistency are explicit operator choices. Built-in provider routes are auth-exempt for SDK compatibility and resolve valid MockAgents credentials best-effort. That makes a network perimeter relevant even with management RBAC enabled. See [API auth semantics](api.md#authentication-and-tenant-scope) and [deployment](deployment.md).

## Evolution rules

Adding a provider requires the adapter, streaming/error mapping, registry entry, compatibility tests, documentation, and any protocol enum/schema changes. Adding a definition field requires checking types, defaults, validators, JSON Schema, API, all SDKs, GUI editing, examples, and documentation together. Adding a managed write requires authorization, ownership, validation, atomicity/concurrency behavior, persistence receipts, and failure-path tests. Avoid promoting a mock shortcut into an undocumented guarantee of a real service.
