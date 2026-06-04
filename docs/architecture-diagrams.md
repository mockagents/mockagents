# MockAgents Architecture Diagrams

Mermaid architecture diagrams for MockAgents — a Go-based mock server that is a
drop-in replacement for the OpenAI and Anthropic APIs, with a mock MCP server, a
multi-tenant control plane, three language SDKs, and a Next.js web console.

The system ships as a **single static Go binary** (pure-Go SQLite, no cgo) plus
an optional Next.js console. The diagrams below reflect the current
implementation; the package layout maps 1:1 to `internal/*`. See
[`architecture.md`](architecture.md) for prose and
[`sequence-diagrams.md`](sequence-diagrams.md) for per-flow sequences.

---

## 1. System Context Diagram (C4 Level 1)

MockAgents in the context of its external actors and systems.

```mermaid
flowchart TB
    developer["Developer<br/>(CLI + SDKs)"]
    cicd["CI/CD Pipeline<br/>(test / contract diff)"]
    clientapp["Client Application<br/>(OpenAI / Anthropic SDK)"]
    mcpclient["MCP Client<br/>(tools / sampling / roots)"]
    operator["Platform / Tenant Operator<br/>(web console)"]

    mockagents(["MockAgents<br/>(Go binary + Next.js console)"])

    reallm["Real LLM APIs<br/>(OpenAI, Anthropic)"]
    otel["Observability Backend<br/>(OTLP/HTTP collector)"]

    developer -->|"define agents, run server"| mockagents
    cicd -->|"start server, assert, diff contracts"| mockagents
    clientapp -->|"HTTP (OpenAI/Anthropic wire format)"| mockagents
    mcpclient -->|"JSON-RPC 2.0 (HTTP / stdio / SSE)"| mockagents
    operator -->|"manage tenants, keys, view logs/costs"| mockagents
    mockagents -.->|"record mode: proxy + capture"| reallm
    mockagents -.->|"export spans (opt-in)"| otel

    style mockagents fill:#1168bd,stroke:#0b4884,color:#fff
    style developer fill:#08427b,stroke:#052e56,color:#fff
    style cicd fill:#08427b,stroke:#052e56,color:#fff
    style clientapp fill:#08427b,stroke:#052e56,color:#fff
    style mcpclient fill:#08427b,stroke:#052e56,color:#fff
    style operator fill:#08427b,stroke:#052e56,color:#fff
    style reallm fill:#999,stroke:#666,color:#fff
    style otel fill:#999,stroke:#666,color:#fff
```

---

## 2. Container Diagram (C4 Level 2)

The deployable units and how they communicate. The Go binary serves both the
LLM-compatible endpoints and the management API; the console is a separate
Next.js process that talks to the binary over HTTP.

```mermaid
flowchart TB
    subgraph system["MockAgents System"]
        gobinary["Go Binary<br/>net/http server + mock engine<br/>+ MCP server · :8080"]
        gui["Web Console<br/>Next.js 15 (SSR) · :3001"]
        interactions[("interactions<br/>.mockagents.db")]
        tenancydb[("tenancy<br/>.mockagents-tenancy.db")]
        auditdb[("audit log<br/>.mockagents-audit.db")]
        filesystem["Agent YAML configs<br/>./agents/*.yaml"]
    end

    pysdk["Python SDK"]
    tssdk["TypeScript SDK"]
    gosdk["Go SDK<br/>(+ in-process mode)"]

    clientapp["Client Application"]
    mcpclient["MCP Client"]
    operator["Operator (browser)"]

    clientapp -->|"OpenAI/Anthropic compat<br/>HTTP :8080"| gobinary
    mcpclient -->|"MCP JSON-RPC<br/>/mcp, /mcp/events, /mcp/response"| gobinary
    pysdk -->|"HTTP :8080"| gobinary
    tssdk -->|"HTTP :8080"| gobinary
    gosdk -->|"HTTP :8080 / in-process httptest"| gobinary
    operator -->|"HTTPS :3001"| gui
    gui -->|"management API<br/>Bearer (HttpOnly cookie)"| gobinary

    gobinary -->|"SQL"| interactions
    gobinary -->|"SQL (bcrypt keys, RBAC)"| tenancydb
    gobinary -->|"append-only SQL"| auditdb
    gobinary -->|"load + hot reload (fsnotify)"| filesystem

    style gobinary fill:#1168bd,stroke:#0b4884,color:#fff
    style gui fill:#7b3f00,stroke:#5a2d00,color:#fff
    style interactions fill:#2d882d,stroke:#1a5c1a,color:#fff
    style tenancydb fill:#2d882d,stroke:#1a5c1a,color:#fff
    style auditdb fill:#2d882d,stroke:#1a5c1a,color:#fff
    style filesystem fill:#d4a017,stroke:#a67c00,color:#000
    style pysdk fill:#08427b,stroke:#052e56,color:#fff
    style tssdk fill:#08427b,stroke:#052e56,color:#fff
    style gosdk fill:#08427b,stroke:#052e56,color:#fff
    style clientapp fill:#08427b,stroke:#052e56,color:#fff
    style mcpclient fill:#08427b,stroke:#052e56,color:#fff
    style operator fill:#08427b,stroke:#052e56,color:#fff
```

---

## 3. Component Diagram (C4 Level 3)

Internal Go packages (`internal/*`) and the request data flow. Request path:
**HTTP → middleware → adapter → engine → response generator → adapter → HTTP
(optionally SSE)**.

```mermaid
flowchart TB
    subgraph server["internal/server"]
        mux["net/http ServeMux"]
        mw["Middleware<br/>(auth, CORS, logging)"]
        authz["Route authz<br/>(role floors)"]
        loghandlers["Log / cost / audit /<br/>pipeline / validate handlers"]
        logworker["Async log worker pool"]
        broadcaster["Log broadcaster<br/>(SSE fan-out)"]
    end

    subgraph adapter["internal/adapter"]
        openai["OpenAI translate in/out"]
        anthropic["Anthropic translate in/out"]
        decode["Pooled decode / encode"]
    end

    subgraph engine["internal/engine"]
        core["Engine<br/>ProcessRequestContext"]
        registry["Agent registry<br/>(byModel + tenant index)"]
        matcher["Scenario matcher"]
        respgen["Response generator<br/>(templates)"]
        tools["Tool processor + validator"]
        chaos["Chaos (latency/error/rate)"]
        state["Session state store"]
    end

    subgraph mcp["internal/mcp"]
        mcpdispatch["JSON-RPC dispatch"]
        mcpduplex["Bidirectional SSE<br/>(sampling, roots)"]
    end

    tenancy["internal/tenancy<br/>store + RBAC + auth cache"]
    audit["internal/audit<br/>append-only recorder"]
    pricing["internal/pricing<br/>cost table"]
    recording["internal/recording<br/>record / replay"]
    streaming["internal/streaming<br/>SSE chunker"]
    config["internal/config<br/>loader + validators"]
    storage["internal/storage<br/>SQLite logs"]
    observability["internal/observability<br/>OTel tracer"]

    mux --> mw --> authz
    authz -->|"LLM routes"| openai
    authz -->|"LLM routes"| anthropic
    authz -->|"mgmt routes"| loghandlers
    authz -->|"/mcp*"| mcpdispatch
    openai --> decode
    anthropic --> decode
    openai -->|"internal request"| core
    anthropic -->|"internal request"| core
    core --> registry
    core --> state
    core --> chaos
    core --> matcher --> respgen --> tools --> core
    mw -. resolve principal .-> tenancy
    authz -. role check .-> tenancy
    core -. spans (opt-in) .-> observability
    core -->|"log"| logworker --> storage
    logworker --> broadcaster
    loghandlers --> storage
    loghandlers --> pricing
    loghandlers -. emit .-> audit
    tenancy -. denial hook .-> audit
    registry --> config
    mcpdispatch --> config
    mcpdispatch --> mcpduplex
    openai -->|"format response"| mux
    anthropic -->|"format response / SSE"| streaming --> mux
    broadcaster -->|"event stream"| mux
    recording -. proxy/replay .-> mux

    style mux fill:#1168bd,stroke:#0b4884,color:#fff
    style core fill:#d9534f,stroke:#c12e2a,color:#fff
    style storage fill:#2d882d,stroke:#1a5c1a,color:#fff
    style tenancy fill:#7b3f00,stroke:#5a2d00,color:#fff
    style audit fill:#7b3f00,stroke:#5a2d00,color:#fff
```

---

## 4. Request Processing Pipeline

How an incoming LLM-compatible request is processed end-to-end, including the
multi-tenant and live-feed paths.

```mermaid
flowchart TD
    A["HTTP Request"] --> B["net/http ServeMux"]
    B --> C["Middleware<br/>(CORS, logging,<br/>opportunistic auth)"]
    C --> D{"Multi-tenant<br/>mode?"}
    D -->|Yes| E["Resolve API-key principal<br/>→ tenant scope (auth cache)"]
    D -->|No| F["Anonymous scope"]
    E --> G["Protocol Adapter<br/>(OpenAI / Anthropic)"]
    F --> G
    G --> H["Parse to internal request"]
    H --> I["Resolve agent<br/>(byModel + tenant visibility)"]
    I --> J["Get / create session<br/>(per-session ApplyTurn lock)"]
    J --> K{"Chaos?<br/>latency / error / rate"}
    K -->|Inject| L["Sleep / 429 / 5xx"]
    K -->|Pass| M["Match scenario"]
    M --> N["Generate response<br/>(template expansion)"]
    N --> O{"Tool calls?"}
    O -->|Yes| P["Resolve + validate tool calls"]
    P --> Q["Format protocol response"]
    O -->|No| Q
    Q --> R{"Streaming?"}
    R -->|Yes| S["SSE chunker"]
    R -->|No| T["JSON response"]
    S --> U["Async log worker → SQLite"]
    T --> U
    L --> U
    U --> V["Broadcaster → SSE live feed<br/>(GUI /logs?live=1)"]
    U --> W["Return to client"]

    style A fill:#08427b,stroke:#052e56,color:#fff
    style W fill:#2d882d,stroke:#1a5c1a,color:#fff
    style D fill:#d4a017,stroke:#a67c00,color:#000
    style K fill:#d4a017,stroke:#a67c00,color:#000
    style O fill:#d4a017,stroke:#a67c00,color:#000
    style R fill:#d4a017,stroke:#a67c00,color:#000
```

---

## 5. Package / Module Diagram

Go package structure and dependency direction. Two conventions are enforced:
**`tenancy` may import `engine`, never the reverse** (the engine uses
`engine.WithTenantID` / `TenantIDFromContext`), and **`audit` does not import
`tenancy`** (the server injects a principal-extraction function).

```mermaid
flowchart TD
    cmd["cmd/mockagents/<br/>(Cobra CLI)"]
    server["internal/server/"]
    adapter["internal/adapter/"]
    engine["internal/engine/"]
    mcp["internal/mcp/"]
    tenancy["internal/tenancy/"]
    audit["internal/audit/"]
    pricing["internal/pricing/"]
    recording["internal/recording/"]
    streaming["internal/streaming/"]
    config["internal/config/"]
    storage["internal/storage/"]
    observability["internal/observability/"]
    runner["internal/runner/"]
    contract["internal/contract/"]
    types["internal/types/"]

    sdks["sdk/{python,typescript,go}"]
    gui["gui/ (Next.js)"]

    cmd --> server
    cmd --> config
    cmd --> engine
    cmd --> mcp
    cmd --> recording
    cmd --> runner
    cmd --> contract
    server --> adapter
    server --> engine
    server --> tenancy
    server --> audit
    server --> pricing
    server --> storage
    server --> streaming
    server --> observability
    adapter --> engine
    adapter --> types
    engine --> config
    engine --> storage
    engine --> observability
    engine --> types
    tenancy --> engine
    config --> types
    storage --> types
    mcp --> config

    sdks -.->|"HTTP :8080"| server
    gui -.->|"mgmt API :8080"| server

    style cmd fill:#1168bd,stroke:#0b4884,color:#fff
    style engine fill:#d9534f,stroke:#c12e2a,color:#fff
    style types fill:#999,stroke:#666,color:#fff
    style tenancy fill:#7b3f00,stroke:#5a2d00,color:#fff
    style audit fill:#7b3f00,stroke:#5a2d00,color:#fff
    style sdks fill:#08427b,stroke:#052e56,color:#fff
    style gui fill:#08427b,stroke:#052e56,color:#fff
```

---

## 6. Deployment Diagrams

### 6a. Local Development

```mermaid
flowchart LR
    subgraph dev["Developer Machine"]
        cli["mockagents start --watch<br/>(:8080, binds 127.0.0.1)"]
        gui["npm run dev<br/>(:3001)"]
        dbs[("SQLite files<br/>interactions / tenancy / audit")]
        yamls["./agents/*.yaml"]
        app["Client App / Tests"]
    end

    app -->|"HTTP :8080"| cli
    gui -->|"mgmt API :8080"| cli
    cli -->|"read + fsnotify reload"| yamls
    cli -->|"read/write"| dbs

    style cli fill:#1168bd,stroke:#0b4884,color:#fff
    style gui fill:#7b3f00,stroke:#5a2d00,color:#fff
    style dbs fill:#2d882d,stroke:#1a5c1a,color:#fff
    style yamls fill:#d4a017,stroke:#a67c00,color:#000
```

### 6b. CI/CD Pipeline

```mermaid
flowchart LR
    subgraph runner["CI Runner (GitHub Actions / GitLab CI)"]
        cli["mockagents start<br/>(Docker container)"]
        sqlite[("Ephemeral SQLite")]
        yamls["Mounted agent configs"]
        tests["TestSuite / SDK tests<br/>(mockagents test → JUnit XML)"]
        contractdiff["contract diff<br/>(fail on breaking change)"]
    end

    tests -->|"HTTP :8080"| cli
    contractdiff -->|"compare to checked-in JSON"| cli
    cli -->|"read"| yamls
    cli -->|"read/write"| sqlite
    tests -->|"junit report"| report["MR / PR test UI"]

    style cli fill:#1168bd,stroke:#0b4884,color:#fff
    style sqlite fill:#2d882d,stroke:#1a5c1a,color:#fff
    style yamls fill:#d4a017,stroke:#a67c00,color:#000
    style tests fill:#08427b,stroke:#052e56,color:#fff
    style contractdiff fill:#08427b,stroke:#052e56,color:#fff
```

### 6c. Kubernetes (Helm chart)

```mermaid
flowchart TB
    subgraph k8s["Kubernetes Cluster"]
        subgraph deploy["Deployment (non-root, read-only rootfs)"]
            pod1["Pod: mockagents<br/>--host 0.0.0.0"]
            pod2["Pod: mockagents"]
        end
        svc["Service (ClusterIP)"]
        ingress["Ingress (optional)"]
        cm["ConfigMap<br/>(agent YAML)"]
        hpa["HPA (opt-in)"]
        pdb["PodDisruptionBudget (opt-in)"]
        netpol["NetworkPolicy (opt-in)"]
        sm["ServiceMonitor (opt-in)"]
    end
    client["Clients / SDKs"]

    client --> ingress --> svc --> pod1
    svc --> pod2
    cm -.->|"mount /agents"| pod1
    cm -.->|"mount /agents"| pod2
    hpa -.->|"scale"| deploy
    pdb -.->|"guard"| deploy
    sm -.->|"scrape"| deploy

    style pod1 fill:#1168bd,stroke:#0b4884,color:#fff
    style pod2 fill:#1168bd,stroke:#0b4884,color:#fff
    style cm fill:#d4a017,stroke:#a67c00,color:#000
    style svc fill:#2d882d,stroke:#1a5c1a,color:#fff
```

---

## 7. Data Flow Diagram

How the primary data categories — agent definitions, requests, responses,
interaction logs, tenancy/auth, audit events, costs, and the live feed — move
through the system.

```mermaid
flowchart LR
    yamlfiles["Agent YAML"]
    client["Client App"]
    operator["Operator (console)"]
    engine["Mock Engine"]
    tenancy["Tenancy<br/>(auth + RBAC)"]
    state["Session State"]
    interactions[("Interactions DB")]
    auditdb[("Audit DB")]
    pricing["Pricing table"]
    feed["SSE Live Feed"]

    yamlfiles -->|"1. load + hot reload"| engine
    operator -->|"2. Bearer key"| tenancy
    tenancy -->|"3. tenant scope"| engine
    client -->|"4. LLM request"| engine
    engine -->|"5. read/update"| state
    engine -->|"6. mock response"| client
    engine -->|"7. write log (+ tenant_id)"| interactions
    interactions -->|"8. stream new rows"| feed
    feed -->|"9. live tail"| operator
    interactions -->|"10. aggregate"| pricing
    pricing -->|"11. cost_usd"| operator
    tenancy -. mutations + auth.denied .-> auditdb
    auditdb -->|"12. query"| operator

    style engine fill:#d9534f,stroke:#c12e2a,color:#fff
    style interactions fill:#2d882d,stroke:#1a5c1a,color:#fff
    style auditdb fill:#2d882d,stroke:#1a5c1a,color:#fff
    style tenancy fill:#7b3f00,stroke:#5a2d00,color:#fff
    style yamlfiles fill:#d4a017,stroke:#a67c00,color:#000
    style state fill:#5bc0de,stroke:#31b0d5,color:#000
```

---

## 8. State Diagram — Conversation Session Lifecycle

Sessions are keyed by id, pre-size their history slice, and serialize same-id
turns under a per-session `ApplyTurn` critical section so concurrent requests
cannot interleave append / match / generate / append.

```mermaid
stateDiagram-v2
    [*] --> Created : First request for session id
    Created --> Active : ApplyTurn (locked) completes
    Active --> Active : Subsequent turns (serialized)
    Active --> Idle : No requests for idle window
    Idle --> Active : New request received
    Idle --> Expired : idle window exceeded
    Active --> Destroyed : Explicit reset
    Idle --> Destroyed : Explicit reset
    Expired --> [*]
    Destroyed --> [*]
```

---

## 9. Evolution Roadmap Diagram

What has landed (Phases 1–4, internal milestones v0.1 → v0.3) and what remains.

```mermaid
flowchart LR
    subgraph p1["Phase 1 — Foundation (done)"]
        direction TB
        a1["Go core + net/http"]
        a2["OpenAI + Anthropic adapters"]
        a3["Static + template responses"]
        a4["SQLite logs · CLI"]
    end

    subgraph p2["Phase 2 — Testing & Multi-Agent (done)"]
        direction TB
        b1["TestSuite runner + JUnit"]
        b2["Pipelines (seq/par/graph)"]
        b3["Record & replay"]
        b4["Python/TS/Go SDKs"]
    end

    subgraph p3["Phase 3 — Resilience & MCP (done)"]
        direction TB
        c1["Chaos engine"]
        c2["MCP v0.1–v0.3<br/>(HTTP/stdio/SSE duplex)"]
        c3["Streaming cassettes"]
    end

    subgraph p4["Phase 4 — Enterprise & Scale (v0.3)"]
        direction TB
        d1["Contracts + OTel"]
        d2["Multi-tenant RBAC<br/>(+ platform role)"]
        d3["Costs · audit · Helm"]
        d4["Web console + live feed"]
    end

    subgraph next["Deferred / Next"]
        direction TB
        e1["Postgres tenancy store"]
        e2["SSO / OAuth · billing"]
        e3["Per-tenant name isolation"]
        e4["Pipeline workflow editor<br/>(drag-to-rewire)"]
    end

    p1 --> p2 --> p3 --> p4 --> next

    style p1 fill:#2d882d,stroke:#1a5c1a,color:#fff
    style p2 fill:#2d882d,stroke:#1a5c1a,color:#fff
    style p3 fill:#2d882d,stroke:#1a5c1a,color:#fff
    style p4 fill:#1168bd,stroke:#0b4884,color:#fff
    style next fill:#d9534f,stroke:#c12e2a,color:#fff
```
