# System diagrams

These Mermaid diagrams describe implemented paths at the handoff baseline. Optional arrows represent conditional wiring, not separate distributed services inside the Go binary.

## System context and components

```mermaid
flowchart LR
  App[Application and provider SDK] --> HTTP[Go HTTP server]
  Dev[Developer and CI] --> CLI[Cobra commands]
  User[Console user] --> GUI[Next.js console]
  GUI --> HTTP
  CLI --> Load[Config loaders and validators]
  Load --> Registry[Agent and pipeline registries]
  HTTP --> Adapter[Provider adapters]
  HTTP --> Manage[Management API and role floors]
  Adapter --> Engine[Neutral scenario engine]
  Manage --> Registry
  Engine --> Registry
  Engine --> State[Session memory]
  Engine --> Tools[Simulated tool rules]
  HTTP --> Logs[Async interaction capture]
  Logs --> SQLite[Interaction SQLite]
  Manage --> Audit[Audit SQLite]
  HTTP --> Tenancy[Tenancy SQLite or Postgres]
  CLI --> MCP[Standalone MCP HTTP or stdio]
  CLI --> A2A[Standalone A2A HTTP]
  CLI --> Replay[Record or replay listener]
  Replay --> Cassette[JSONL cassette]
  Replay -. Recording modes .-> Provider[Real upstream service]
```

## Provider request and tool-call round trip

```mermaid
sequenceDiagram
  participant C as Client application
  participant H as HTTP middleware
  participant A as Adapter
  participant E as Engine
  participant S as Session
  participant T as Tool fixture processor
  C->>H: Provider request and optional session/credential
  H->>H: Principal scope, capture, quota
  H->>A: Route request
  A->>E: Normalized InboundRequest
  E->>E: Resolve agent, chaos pre-check, strict validation
  E->>S: ApplyTurn under session lock
  S->>E: Proposed turn and variables
  E->>E: First matching scenario and template
  E->>T: Resolve configured tool-call results
  T-->>E: Success/error results per tool
  E-->>S: Commit successful generated turn
  E-->>A: Neutral Response
  A-->>C: Tool calls in provider wire format
  C->>C: Application dispatches its own real/test tool
  C->>H: Tool result with echoed call identifiers
  H->>A: Next request
  A->>E: Normalized tool-result turn
  E->>E: Suppress identical immediate reissued calls
  E-->>A: Content or a different configured tool call
  A-->>C: Provider response or stream
```

The mock's simulated `ToolResults` support engine/suite evidence. They do not execute the application's external function or imply every provider wire returns that internal field.

## Pipeline decision flow

```mermaid
flowchart TD
  R[Run with definition and input] --> T{Topology}
  T -->|sequential| S[Invoke nodes in declaration order]
  S --> SI[Pass prior response content as next input]
  T -->|parallel| P[Invoke all nodes concurrently with same input]
  P --> PI[Collect in declaration order and join errors]
  T -->|graph| G[Reject cycles among known non-self edges]
  G --> Roots[Visit roots in declaration order]
  Roots --> N[Depth-first invoke each unvisited node]
  N --> Edges[Follow every matching outgoing substring guard in order]
  Edges --> Traverse[First visit wins; continue remaining edges and roots]
  Traverse --> GraphResult[Completed traversal or partial prefix on error]
  SI --> Result[Result with nodes and aggregate duration]
  PI --> Result
  GraphResult --> Result
  Result --> Error{Execution error?}
  Error -->|yes| Partial[HTTP 422 with code and partial result]
  Error -->|no| OK[HTTP 200]
```

For a diamond graph, a visited target runs once with the first traversed input; there is no fan-in join. The config validator rejects self-edges even though low-level executor cycle accounting excludes them.

## Configuration edit and persistence

```mermaid
sequenceDiagram
  participant UI as Console/editor
  participant H as Managed handler
  participant V as Config validator
  participant F as Fixture filesystem
  participant R as Registry
  UI->>H: GET definition
  H-->>UI: Definition and ETag/revision metadata
  UI->>H: PUT definition with If-Match
  H->>H: Authenticate, authorize, resolve ownership
  H->>V: Parse and validate fields/references
  V-->>H: Diagnostics or valid definition
  H->>H: Lock and check current revision
  alt Invalid or stale
    H-->>UI: 4xx with diagnostics; draft remains client-side
  else Valid and current
    H->>F: Persist through temporary file and rename
    H->>R: Register live definition
    H-->>UI: Receipt / new revision
  end
```

Agent writes can be runtime-only when no agents directory is configured; the receipt reports `persisted`. Pipeline updates require an existing pipeline and `If-Match`. Exact agent and pipeline response bodies differ.

## Data ownership

```mermaid
flowchart LR
  YAML[Versioned fixture files] --> Parse[Parse, defaults, validate]
  Parse --> Mem[Agent and pipeline registries]
  In[Provider requests] --> Mem
  In --> Sessions[Scoped in-memory sessions]
  In --> Resource[Provider resource and vector memory]
  In --> Capture[Capture and body policy]
  Capture --> LogDB[Interaction SQLite]
  Mutations[Management mutations and auth denials] --> AuditDB[Audit SQLite]
  Keys[Keys, users, login sessions, quotas, spend] --> TenantDB[Tenancy SQLite or Postgres]
  Upstream[Recording response] --> Redact[Optional body redaction]
  Redact --> JSONL[Disk cassette plus in-memory index]
```

## Deployment boundary

```mermaid
flowchart TB
  Clients[SDKs and console backend] --> Service[Main HTTP listener or private Service/Ingress]
  Service --> Pod[MockAgents process]
  Fixtures[Fixture directory or ConfigMap] --> Pod
  Pod --> Local[Per-process memory and local data volume]
  Pod -. Optional shared tenancy .-> PG[Postgres]
  Scrape[Platform-authenticated scraper] --> Pod
  Pod -. Opt-in OTLP .-> Collector[Trace collector]
```

Adding replicas duplicates memory state and local logs. Postgres shares tenancy records and spend; it does not share all runtime state. The Helm acknowledgment guard documents that limitation.
