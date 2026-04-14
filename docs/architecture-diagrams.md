# MockAgents Architecture Diagrams

This document contains Mermaid-based architecture diagrams for MockAgents, a platform for mocking AI agent integrations. MockAgents uses a Go core engine, monorepo layout, SQLite storage, and ships as a CLI-only MVP.

---

## 1. System Context Diagram (C4 Level 1)

Shows MockAgents in the context of its external actors and systems.

```mermaid
flowchart TB
    developer["Developer\n(Uses CLI + SDK)"]
    cicd["CI/CD Pipeline\n(Runs Tests)"]
    clientapp["Client Application\n(Sends LLM API Requests)"]

    mockagents(["MockAgents System\n(Go Binary)"])

    reallm["Real LLM APIs\n(OpenAI, Anthropic, etc.)\n-- Future Record Mode --"]

    developer -->|"configure agents\nrun mock server"| mockagents
    cicd -->|"start server\nassert responses"| mockagents
    clientapp -->|"HTTP requests\n(OpenAI/Anthropic format)"| mockagents
    mockagents -.->|"forward requests\n(future record mode)"| reallm

    style mockagents fill:#1168bd,stroke:#0b4884,color:#fff
    style developer fill:#08427b,stroke:#052e56,color:#fff
    style cicd fill:#08427b,stroke:#052e56,color:#fff
    style clientapp fill:#08427b,stroke:#052e56,color:#fff
    style reallm fill:#999,stroke:#666,color:#fff
```

---

## 2. Container Diagram (C4 Level 2)

Shows the high-level containers that make up MockAgents and how they communicate.

```mermaid
flowchart TB
    subgraph MockAgents System
        gobinary["MockAgents Go Binary\n(HTTP Server + Mock Engine + CLI)\nListens on :8080"]
        sqlite[("SQLite Database\n(interactions.db)\nStores logs & session state")]
        filesystem["File System\n(Agent YAML Configs)\n./agents/*.yaml"]
    end

    pythonsdk["Python SDK\n(pip install mockagents)\nSeparate package"]

    clientapp["Client Application"]
    developer["Developer"]

    developer -->|"CLI commands\n(stdin/stdout)"| gobinary
    pythonsdk -->|"HTTP :8080\nREST API"| gobinary
    clientapp -->|"HTTP :8080\nOpenAI/Anthropic\ncompat endpoints"| gobinary
    gobinary -->|"SQL queries"| sqlite
    gobinary -->|"Read YAML files"| filesystem
    developer -->|"import mockagents"| pythonsdk

    style gobinary fill:#1168bd,stroke:#0b4884,color:#fff
    style sqlite fill:#2d882d,stroke:#1a5c1a,color:#fff
    style filesystem fill:#d4a017,stroke:#a67c00,color:#fff
    style pythonsdk fill:#7b3f00,stroke:#5a2d00,color:#fff
    style clientapp fill:#08427b,stroke:#052e56,color:#fff
    style developer fill:#08427b,stroke:#052e56,color:#fff
```

---

## 3. Component Diagram (C4 Level 3)

Shows the internal Go packages and how data flows between them.

```mermaid
flowchart TB
    subgraph server["internal/server"]
        httpserver["HTTP Server\n(Chi Router)"]
    end

    subgraph adapter["internal/adapter"]
        registry["Protocol Adapter\nRegistry"]
        openai["OpenAI\nAdapter"]
        anthropic["Anthropic\nAdapter"]
    end

    subgraph engine["internal/engine"]
        core["Mock Engine\nCore"]
        agentregistry["Agent Registry\n/ Loader"]
        scenariomatcher["Scenario\nMatcher"]
        responsegen["Response Generator\n(Static, Template)"]
        toolcall["Tool Call\nProcessor"]
        statestore["State Store\n(In-Memory)"]
    end

    subgraph storage["internal/storage"]
        logger["Interaction\nLogger"]
        sqlitestorage["SQLite\nStorage"]
    end

    subgraph config["internal/config"]
        configloader["Config Loader\n/ Validator"]
    end

    httpserver -->|"raw HTTP request"| registry
    registry -->|"route to adapter"| openai
    registry -->|"route to adapter"| anthropic
    openai -->|"parsed internal request"| core
    anthropic -->|"parsed internal request"| core
    core -->|"lookup agent"| agentregistry
    core -->|"get/create session"| statestore
    core -->|"find matching scenario"| scenariomatcher
    scenariomatcher -->|"matched scenario"| responsegen
    responsegen -->|"check for tool calls"| toolcall
    toolcall -->|"resolved response"| core
    core -->|"log interaction"| logger
    logger -->|"write to DB"| sqlitestorage
    agentregistry -->|"load YAML"| configloader
    openai -->|"format protocol response"| httpserver
    anthropic -->|"format protocol response"| httpserver

    style httpserver fill:#1168bd,stroke:#0b4884,color:#fff
    style core fill:#d9534f,stroke:#c12e2a,color:#fff
    style sqlitestorage fill:#2d882d,stroke:#1a5c1a,color:#fff
```

---

## 4. Request Processing Pipeline

Step-by-step flow of how an incoming HTTP request is processed and a response is returned.

```mermaid
flowchart TD
    A["HTTP Request"] --> B["Chi Router"]
    B --> C["Protocol Adapter\n(OpenAI / Anthropic)"]
    C --> D["Parse to Internal Request"]
    D --> E["Load Agent Definition"]
    E --> F["Get or Create\nSession State"]
    F --> G["Match Scenario"]
    G --> H["Generate Response\n(Static / Template)"]
    H --> I{"Has Tool Calls?"}
    I -->|Yes| J["Resolve Tool Calls"]
    J --> K["Format Protocol Response"]
    I -->|No| K
    K --> L{"Streaming?"}
    L -->|Yes| M["SSE Stream\n(Server-Sent Events)"]
    L -->|No| N["JSON Response"]
    M --> O["Log Interaction"]
    N --> O
    O --> P["Return to Client"]

    style A fill:#08427b,stroke:#052e56,color:#fff
    style P fill:#2d882d,stroke:#1a5c1a,color:#fff
    style I fill:#d4a017,stroke:#a67c00,color:#000
    style L fill:#d4a017,stroke:#a67c00,color:#000
```

---

## 5. Package / Module Diagram

Shows the Go module structure and dependency relationships between packages.

```mermaid
flowchart TD
    cmd["cmd/mockagents/\n(CLI Entry Point)"]
    server["internal/server/\n(HTTP Server)"]
    adapter["internal/adapter/\n(Protocol Adapters)"]
    engine["internal/engine/\n(Core Engine)"]
    config["internal/config/\n(Config Loading)"]
    storage["internal/storage/\n(SQLite)"]
    types["internal/types/\n(Shared Types)"]
    pythonsdk["sdk/python/\n(Python SDK)"]

    cmd --> server
    cmd --> config
    cmd --> engine
    server --> adapter
    server --> engine
    server --> types
    adapter --> types
    adapter --> engine
    engine --> config
    engine --> storage
    engine --> types
    config --> types
    storage --> types

    pythonsdk -.->|"HTTP :8080"| server

    style cmd fill:#1168bd,stroke:#0b4884,color:#fff
    style engine fill:#d9534f,stroke:#c12e2a,color:#fff
    style types fill:#999,stroke:#666,color:#fff
    style pythonsdk fill:#7b3f00,stroke:#5a2d00,color:#fff
```

---

## 6. Deployment Diagrams

### 6a. Local Development

How a developer runs MockAgents on their own machine during development or manual testing.

```mermaid
flowchart LR
    subgraph dev["Developer Machine"]
        cli["mockagents serve\n(Go Binary)"]
        sqlite[("SQLite File\n./data/interactions.db")]
        yamls["Agent YAML Dir\n./agents/*.yaml"]
        app["Client App / Tests\n(localhost:8080)"]
    end

    app -->|"HTTP :8080"| cli
    cli -->|"read"| yamls
    cli -->|"read/write"| sqlite

    style cli fill:#1168bd,stroke:#0b4884,color:#fff
    style sqlite fill:#2d882d,stroke:#1a5c1a,color:#fff
    style yamls fill:#d4a017,stroke:#a67c00,color:#000
```

### 6b. CI/CD Pipeline

How MockAgents runs inside a continuous integration environment.

```mermaid
flowchart LR
    subgraph runner["GitHub Actions Runner"]
        subgraph container["Docker Container"]
            cli["mockagents serve\n(Go Binary)"]
            sqlite[("Ephemeral SQLite\n/tmp/interactions.db")]
        end
        yamls["Mounted Agent Configs\n(from repo ./agents/)"]
        tests["Test Suite\n(pytest / go test)"]
    end

    tests -->|"HTTP :8080"| cli
    cli -->|"read"| yamls
    cli -->|"read/write"| sqlite

    style container fill:#1a1a2e,stroke:#16213e,color:#fff
    style cli fill:#1168bd,stroke:#0b4884,color:#fff
    style sqlite fill:#2d882d,stroke:#1a5c1a,color:#fff
    style yamls fill:#d4a017,stroke:#a67c00,color:#000
    style tests fill:#08427b,stroke:#052e56,color:#fff
```

---

## 7. Data Flow Diagram

Shows how the four primary categories of data -- agent definitions, requests, responses, and logs -- move through the system.

```mermaid
flowchart LR
    yamlfiles["Agent YAML\nConfig Files"]
    client["Client\nApplication"]
    mockengine["Mock\nEngine"]
    statestore["In-Memory\nState Store"]
    sqlitedb[("SQLite\nDatabase")]
    developer["Developer\n(CLI)"]

    yamlfiles -->|"1. Load agent\ndefinitions at startup"| mockengine
    client -->|"2. Send LLM API\nrequest (HTTP)"| mockengine
    mockengine -->|"3. Read/update\nsession state"| statestore
    mockengine -->|"4. Return mock\nresponse (HTTP)"| client
    mockengine -->|"5. Write interaction\nlog (request + response)"| sqlitedb
    developer -->|"6. Query logs\nvia CLI"| sqlitedb

    style mockengine fill:#d9534f,stroke:#c12e2a,color:#fff
    style sqlitedb fill:#2d882d,stroke:#1a5c1a,color:#fff
    style yamlfiles fill:#d4a017,stroke:#a67c00,color:#000
    style statestore fill:#5bc0de,stroke:#31b0d5,color:#000
```

---

## 8. State Diagram -- Conversation Session Lifecycle

Shows the states a conversation session goes through from creation to expiration.

```mermaid
stateDiagram-v2
    [*] --> Created : First request for session ID
    Created --> Active : Request processed
    Active --> Active : Subsequent requests
    Active --> Idle : No requests for idle_timeout
    Idle --> Active : New request received
    Idle --> Expired : idle_timeout exceeded
    Active --> Destroyed : Explicit reset via API/CLI
    Idle --> Destroyed : Explicit reset via API/CLI
    Expired --> [*]
    Destroyed --> [*]
```

---

## 9. Evolution Roadmap Diagram

Shows how the MockAgents architecture is planned to evolve across phases.

```mermaid
flowchart LR
    subgraph mvp["MVP (Phase 1)"]
        direction TB
        m1["Go Monolith"]
        m2["CLI Interface"]
        m3["SQLite Storage"]
        m4["OpenAI + Anthropic\nAdapters"]
        m5["Static + Template\nResponses"]
    end

    subgraph phase2["Phase 2"]
        direction TB
        p2a["Built-in Test Runner"]
        p2b["Multi-Agent\nOrchestration"]
        p2c["Python SDK"]
        p2d["Scenario Sequencing"]
    end

    subgraph phase3["Phase 3"]
        direction TB
        p3a["Chaos Engine\n(Latency, Errors)"]
        p3b["MCP Protocol\nSupport"]
        p3c["Internal\nMessage Bus"]
        p3d["Record & Replay\nMode"]
    end

    subgraph phase4["Phase 4"]
        direction TB
        p4a["PostgreSQL\nOption"]
        p4b["Kubernetes\nDeployment"]
        p4c["Web GUI\nDashboard"]
        p4d["Cloud-Hosted\nService"]
    end

    mvp -->|"+ testing\n+ multi-agent"| phase2
    phase2 -->|"+ chaos\n+ MCP"| phase3
    phase3 -->|"+ scale\n+ GUI"| phase4

    style mvp fill:#2d882d,stroke:#1a5c1a,color:#fff
    style phase2 fill:#1168bd,stroke:#0b4884,color:#fff
    style phase3 fill:#d4a017,stroke:#a67c00,color:#000
    style phase4 fill:#d9534f,stroke:#c12e2a,color:#fff
```
