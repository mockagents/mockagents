# MockAgents Web Console (v0.3)

A Next.js 15 GUI for inspecting a running MockAgents server. Server
components fetch directly from the management API; two client islands
(`AutoRefreshLogs` and `YamlEditor`) handle the interactive surface.
No global state store, no build-time data — every page is
server-rendered on demand and talks to the current running server.

## What's here (v0.3)

### Read surfaces

- **Agent catalog** (`/`) — cards for every loaded agent with model,
  protocol, scenario/tool counts, and tags.
- **Agent detail** (`/agents/[name]`) — overview, scenario list, tool
  list, and the raw JSON definition for power users.
- **Pipelines** (`/pipelines`) — cards for every `kind: Pipeline`
  loaded from the agents directory, with topology badge and
  agent/edge counts.
- **Pipeline detail** (`/pipelines/[name]`) — static SVG DAG viewer
  with longest-path layered layout. Works for sequential, parallel,
  and graph topologies. Each node links to the underlying agent
  definition.
- **Interaction logs** (`/logs`) — request/response history with:
  - Filter by agent, by `since` (RFC3339), and by limit (25–250).
  - Per-row link into a **log detail page** (`/logs/[id]`) showing
    the full request/response bodies, method/path, latency, scenario
    match, and (when pricing is configured) per-row cost in USD.
  - **Live mode** (`?live=1` or the checkbox) backed by a real
    server-sent-events stream at `GET /api/v1/logs/stream`. New rows
    appear sub-second after the Go LogWorker finishes its SQLite
    write (no more 3-second polling). Capped-exponential reconnect
    on upstream failure.
- **Cost estimates** (`/costs`) — surfaces the `/api/v1/costs`
  aggregate. Shows total requests / prompt tokens / completion
  tokens / USD plus by-model and by-agent breakdowns.
- **Audit log** (`/audit`) — surfaces `/api/v1/audit` with
  categorized badges for tenant / key / auth-denial / agent-reload
  events. Renders structured details from the JSON blob.

### Admin surfaces (multi-tenant mode)

- **Login** (`/login`) — form that accepts an API key, validates it
  server-side against `/api/v1/tenants` (admin-gated), and
  persists it in an HttpOnly `mockagents_api_key` cookie. The
  cookie is injected as `Authorization: Bearer` on every
  management-API request automatically.
- **Tenants admin** (`/admin/tenants`) — list + create + delete
  tenants.
- **API keys admin** (`/admin/tenants/[id]`) — list + mint + delete
  keys, plus **inline role change** (viewer/editor/admin dropdown)
  and **inline Rotate button** that regenerates a key's secret in
  place without changing its id. Newly minted and newly rotated
  keys show their plaintext exactly once in a prominent banner.
- **Logout** via a `Sign out` button in the header auth pill that
  clears the session cookies and redirects to `/login`.

### Authoring surfaces

- **Config editor** (`/editor`) — textarea + line gutter + Validate
  button. Posts to `POST /api/v1/config/validate`, which runs the
  same validator as `mockagents validate`. Renders parse and schema
  errors inline with line numbers. Validation playground only — it
  does **not** persist back to disk; edit the YAML on disk and
  rely on hot reload (or restart) to apply.

### Header

- **Live health pill** — goes red when the server is unreachable;
  every navigation re-runs the health probe.
- **Auth pill** — "sign in" link when anonymous; 8-character token
  prefix + Sign out form when logged in.

## Running

```bash
# Terminal 1: start the mock server
mockagents start --agents-dir ./agents

# Terminal 2: start the GUI in dev mode
cd gui
npm install
npm run dev   # http://localhost:3001
```

To point the GUI at a non-default MockAgents URL, set
`MOCKAGENTS_API_URL` before running `npm run dev` or `npm start`.

> **Port 3001 already in use?** (`Error: listen EADDRINUSE: address already
> in use :::3001`) — run Next.js on another port directly:
>
> ```bash
> npx next dev --port 3002
> ```
>
> Or free the port first: `npx kill-port 3001`, or on Windows
> `netstat -ano | findstr :3001` + `taskkill /PID <pid> /F`.

To exercise the admin surfaces, start the server in multi-tenant
mode and copy the bootstrap admin key from stderr:

```bash
MOCKAGENTS_MULTI_TENANT=1 mockagents start --agents-dir ./agents
```

> **Windows note:** if the health pill is stuck on "offline" even
> though the server is running, set
> `MOCKAGENTS_API_URL=http://127.0.0.1:8080`. Node's DNS resolver
> prefers IPv6 for `localhost` on Windows, while the Go binary only
> binds to IPv4 unless you configure otherwise.

## Build

```bash
npm run build     # production build (tsc strict mode runs here)
npm run typecheck # tsc --noEmit only
```

A clean build renders **15 routes** with 102 kB shared JS and at
most 1.27 kB per-page JS (`/logs` carries the EventSource client).

## Design notes

- **Server components by default.** Every page except
  `AutoRefreshLogs` and `YamlEditor` is a server component. Auth
  state lives in an HttpOnly cookie read via `next/headers`, so
  client code never sees the token.
- **SSE instead of WebSockets for the live feed.** The server has
  no WS endpoint — the live feed uses the same stdlib SSE path
  that powers MCP v0.3. A same-origin proxy route at
  `/api/logs/stream` threads the auth cookie into the upstream
  request so `EventSource` works without custom headers.
- **Server-side schema validation.** The editor is a thin
  textarea that posts raw YAML to `/api/v1/config/validate` — no
  Monaco, no ajv, no JSON-schema-in-the-browser. The server-side
  validator stays the single source of truth forever.
- **Static SVG DAG for pipelines.** The viewer uses a longest-path
  layered layout in ~150 lines of pure SVG. A drag-to-rewire
  editor would need React Flow; that's a future slice.

## What's intentionally not in v0.3

- **Workflow editor** — drag-to-rewire for `kind: Pipeline`. The
  read-only viewer shipped; the editor is a future slice that
  needs React Flow or a comparable DAG widget.
- **Component-level unit tests** — `next build` runs `tsc --strict`
  which covers the type surface end-to-end; component tests are
  still a follow-up.
- **Self-service user signup** — the admin pages work against the
  existing key-minting API; there is no user-table / email /
  password flow and no SSO.
