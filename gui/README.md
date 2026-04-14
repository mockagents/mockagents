# MockAgents Web Console (v0.1)

A minimal Next.js 15 GUI for browsing a running MockAgents server. Server
components fetch directly from the management API; there are no
client-side stores or authentication flows to worry about.

## What's here

- **Agent catalog** (`/`) — cards for every loaded agent with model,
  protocol, scenario/tool counts, and tags.
- **Agent detail** (`/agents/[name]`) — overview, scenario list, tool
  list, and the raw JSON definition for power users.
- **Interaction logs** (`/logs`) — the 50 most recent request/response
  pairs captured by the server's SQLite store.
- **Live health pill** in the header — goes red when the server is
  unreachable; every navigation re-runs the health probe.

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

## What's intentionally not in v0.1

- No workflow editor — that's its own slice.
- No agent definition editor — agents are still edited as YAML files.
- No authentication — the GUI is a dev/ops tool for local MockAgents
  instances.
- No WebSocket live-update feed — the dashboard re-fetches on each
  navigation. Hit refresh or click between pages to see new data.
- No unit tests yet — `next build` runs `tsc` strict which covers the
  type surface; end-to-end tests are a follow-up slice.
