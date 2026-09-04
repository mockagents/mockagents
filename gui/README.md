# MockAgents Web Console

A Next.js 15 GUI for inspecting and editing a running MockAgents server. Pages
are server components that fetch directly from the management API; interactive
surfaces are small client islands. No global state store, no build-time data —
every page is server-rendered on demand against the current running server.

> Accuracy note: this file describes what the **source** does. It was rewritten
> under UX-08 after review found it stale about saving, the pipeline editor, SSO
> and the login role gate. If it disagrees with the code, the code wins — and the
> file is a bug.

## Client islands

Nine components carry the interactive surface; everything else is a server
component:

| Component | Route | Responsibility |
|---|---|---|
| `Shell` | all | Sidebar nav, theme toggle |
| `AgentCatalog` | `/` | Search + protocol filter, delete |
| `AgentTabs` | `/agents/[name]` | Overview / scenarios / tools / raw JSON |
| `LogsConsole` | `/logs` | Filters, SSE live feed |
| `YamlEditor` | `/editor` | Validate + save |
| `PipelineEditor` | `/pipelines/[name]/edit` | React Flow DAG editing |
| `AgentEditor` + `GuidedForm` | `/agents/[name]/edit` | Form/YAML tabs, diff preview, conditional apply |
| `RunPanel` | `/pipelines/[name]` | Explicit active-runtime pipeline execution |
| `CopyField` | `/overview` | Copyable SDK settings |
| `ReportExport` | `/reports` | Evidence export, incl. the included-data review |
| `DangerConfirm` | `/admin/*`, `/account` | Impact summary + exact-name gate for irreversible actions |

## Read surfaces

- **Overview** (`/overview`) — UX-02. Liveness and readiness are verified
  *separately* (`/api/v1/health` and `/api/v1/ready`), because a server that is
  up but cannot serve is a different problem from one that is down. Carries the
  **instrument strip**: server, liveness, readiness, tenant/role, exec mode,
  configuration revision, engine version and last refresh — every cell real, and
  "unknown" where a value cannot be established rather than a placeholder.

  Nothing on the page is inferred from an empty list. A catalog that could not
  be *read* is reported as unknown, never as an empty install; readiness while
  unreachable is unknown, never false; an empty pipeline inventory explains that
  pipeline creation is out of scope for Release A instead of offering a dead Run
  button. Also carries the first-run checklist and copyable SDK settings.

- **Agent catalog** (`/`) — cards for every loaded agent with model, protocol,
  scenario/tool counts and tags. Search and protocol filter run client-side over
  the server-fetched list. Cards can delete an agent (editor role).
- **Agent detail** (`/agents/[name]`) — overview, scenario list, tool list and
  the raw JSON definition.
- **Pipelines** (`/pipelines`) — cards for every `kind: Pipeline` loaded from the
  agents directory, with topology badge and agent/edge counts.
- **Pipeline detail** (`/pipelines/[name]`) — static SVG DAG viewer with
  longest-path layered layout, for sequential, parallel and graph topologies.
  Each node links to the underlying agent. Also carries the **Run** panel
  (below), which is a write surface rather than a read one.
- **Interaction logs** (`/logs`) — request/response history, filterable by agent,
  `since` (RFC3339) and limit (25–250). Each row links to a detail page
  (`/logs/[id]`) with full bodies, method/path, latency, scenario match and —
  when pricing is configured — per-row cost in USD. **Live mode** is backed by a
  real SSE stream at `GET /api/v1/logs/stream` through a same-origin proxy, with
  capped-exponential reconnect.
- **Cost estimates** (`/costs`) — the `/api/v1/costs` aggregate: requests
  scanned, prompt/completion tokens, USD, plus by-model and by-agent breakdowns.
  These are *estimated upstream cost for captured requests*, not measured spend
  and not verified savings. The endpoint aggregates over at most a fixed number
  of the most recent rows and reports no truncation flag, so a scan that comes
  back at its cap is labelled a possible partial sum. A request whose model could
  not be identified reads as **unknown**, never `$0.00`.
- **Reports** (`/reports`) — exports the interactions already loaded in the
  console as JSON or a self-contained printable document (UX-06). Every export
  carries its window, source, schema version and an explicit omissions list, and
  declares itself a bounded local snapshot — it is not server-attested and does
  not prove what the server retained. Raw request/response bodies are excluded by
  default; including them requires ticking the option **and** acknowledging a
  review of exactly what would be embedded, because this console applies no
  redaction. Captured bodies are rendered as text in the printable document,
  never as markup.
- **Audit log** (`/audit`) — `/api/v1/audit` with categorized badges and
  structured detail rendering. Requires the admin role. Filters cover the twelve
  event kinds this server emits; an unrecognized kind is still filterable and
  still rendered, so a newer server's events stay reachable.

## Authoring surfaces

- **Agent editor** (`/agents/[name]/edit`) — edits an existing agent, with a
  **Form** tab and a **YAML** tab over the *same* document. The form is a lens,
  not a model: each control rewrites only the YAML path it owns, so config it
  does not render (`spec.behavior.chaos`, `spec.tools`, …) is carried through
  byte-for-byte, and a banner names what is not shown. A value it cannot edit
  safely — a multi-line block scalar, inline flow syntax, a duplicate key — is
  disabled with the reason rather than guessed at.

  Applying is deliberate: **Review changes** shows a diff of exactly what will
  be written, and only then does **Apply** enable. The save is conditional
  (`If-Match` on the revision loaded), so a concurrent change returns 412 and
  the draft is kept, with an explicit choice between comparing against the
  server's current version and discarding your work. The receipt distinguishes
  a persisted save from a runtime-only one.

  Because the request carries a precondition, it also opts into strict field
  checking: an unsupported field is reported with its line rather than silently
  dropped.

- **Config editor** (`/editor`) — textarea + line gutter, **Validate** and
  **Save**. Validate posts to `POST /api/v1/config/validate` (the same validator
  as `mockagents validate`) and renders parse/schema errors inline with line
  numbers. Save persists through the agent write API and takes effect
  immediately; it needs the editor role in multi-tenant mode.
- **Pipeline run** (`/pipelines/[name]`, Run panel) — enter an input and
  execute the pipeline through `POST /api/v1/pipelines/{name}/run`.

  This runs against the **active runtime**: the same engine that serves live
  traffic, using the definitions currently loaded, and it advances per-node
  session state. The panel says so before the button, not after. Each run gets
  a fresh session id so turns are not reused — which does *not* pin definitions
  or isolate fixtures; the isolated boundary is Release B.

  Node results are rendered from the real `PipelineResult`: `node_id`,
  `agent_name`, scenario, output and latency. Latencies arrive as Go
  **nanoseconds** and are converted (`lib/duration.ts`); a duration below the
  clock's resolution reads `<1µs`, never `0µs`. A null node response is shown
  as "no response", not as an empty answer. A 422 is a *partial* run: the nodes
  that completed are kept and shown, because they really ran. For a parallel or
  graph topology the panel states that the node order is definition order and
  not a completion timeline. A lost response is reported as an unknown outcome
  and is never retried automatically, because a run is stateful; duplicate
  submission is disabled while one is in flight.

- **Pipeline editor** (`/pipelines/[name]/edit`) — React Flow (`@xyflow/react`)
  drag-to-rewire editing, saved with `PUT /api/v1/pipelines/{name}` under
  `If-Match` optimistic concurrency. A stale edit returns 412 and is surfaced as
  a conflict rather than silently overwriting.

## Admin surfaces (multi-tenant mode)

- **Login** (`/login`) — accepts an API key and validates it server-side, then
  stores it in an HttpOnly `mockagents_api_key` cookie which is injected as
  `Authorization: Bearer` on every management-API request.

  Any authenticated role can sign in. The key is validated against
  `GET /api/v1/identity`, which every role may read; signing in grants nothing
  on its own, because each request is still authorized server-side. The role
  shown in the header and on `/account` is the one the **server** reports, read
  fresh — a role change takes effect on the next page load rather than living in
  a cookie. When the server cannot be reached the role reads "unknown" instead
  of signing you out.

  When `MOCKAGENTS_SSO_ENABLED=1` the page also offers **Sign in with SSO**,
  linking to the backend's `/auth/login` OIDC start. SSO needs the API and GUI to
  share an origin so the backend's session cookie is readable.
- **Tenants admin** (`/admin/tenants`) — list, create and delete tenants. The
  tenant *collection* is **platform**-gated, not admin-gated: a tenant admin
  manages its own keys but never sees this list, and the page says so instead of
  offering a link that 403s. Deletion opens a confirmation naming how many keys
  it revokes, what survives it (agent definitions on disk) and what does not,
  and it stays disabled until the tenant name is typed **exactly**.
- **API keys admin** (`/admin/tenants/[id]`) — list, mint and delete keys, plus
  inline role change and **Rotate**, which regenerates a key's secret in place
  without changing its id. Newly minted and rotated keys show their plaintext
  exactly once, delivered through a single-read server-side flash so the secret
  never reaches the URL. Delete, rotate and rotate-all each confirm first with
  their own impact summary; delete and rotate-all also require typing the exact
  name. A **platform** credential additionally gets the quota-override form
  (`PUT /api/v1/tenants/{id}/quota`) — write-only, because the server exposes no
  per-tenant quota read and pre-filling it would show the wrong tenant's numbers.
- **Account** (`/account`) — self-service rotate and burn for your own key, both
  behind a confirmation, plus your own quota and month-to-date spend from
  `GET /api/v1/quota` (any authenticated role). A quota that cannot be read
  reports **unknown**, not "unlimited".
- **Logout** — clears the session cookies and redirects to `/login`.

## Header

- **Health pill** — goes red when the server is unreachable; every navigation
  re-runs the probe.
- **Auth pill** — "sign in" link when anonymous; 8-character token prefix plus a
  Sign out form when logged in.

## Running

```bash
mockagents start --agents-dir ./examples
```

```bash
cd gui && npm install && npm run dev
```

The console is then on <http://localhost:3001>. Point it at a non-default server
with `MOCKAGENTS_API_URL`.

To exercise the admin surfaces, start the server in multi-tenant mode and copy
the bootstrap key from stderr:

```bash
MOCKAGENTS_MULTI_TENANT=1 mockagents start --agents-dir ./examples
```

> **Port 3001 in use?** (`EADDRINUSE`) — run `npx next dev --port 3002`, or free
> the port with `npx kill-port 3001` (on Windows: `netstat -ano | findstr :3001`
> then `taskkill /PID <pid> /F`).

> **Windows:** if the health pill is stuck on "offline" while the server is
> running, set `MOCKAGENTS_API_URL=http://127.0.0.1:8080`. Node's resolver
> prefers IPv6 for `localhost`, while the Go binary binds IPv4 by default.

## Verification

Four layers, all wired into PR CI as the `GUI` job (UX-08). A green typecheck is
explicitly *not* treated as evidence of UX correctness.

```bash
make gui-verify
```

| Layer | Command | Covers |
|---|---|---|
| Types | `make gui-typecheck` | `tsc --noEmit` under `strict` |
| Build | `make gui-build` | Production `next build` |
| Component | `make gui-test` | Vitest + Testing Library in jsdom, incl. axe for names/roles/structure |
| Browser | `make gui-test-e2e` | Playwright/Chromium against a **real** `mockagents` binary serving `examples/`, incl. axe rules needing real layout (colour contrast, target size) |

The browser suite builds nothing itself — run `make build` and `make gui-build`
first, or use `make gui-verify`, which sequences all of it. It boots the server
on port 8099 and the console on 3099 so a smoke run cannot collide with, or
silently pass against, a server you already have running.

Accessibility target is WCAG 2.2 AA. The jsdom layer disables `color-contrast`
and `target-size` because jsdom has no layout engine — those two are asserted in
the Chromium layer instead, not waived.

## Design notes

- **Server components by default.** Auth state lives in an HttpOnly cookie read
  via `next/headers`, so client code never sees the token.
- **SSE, not WebSockets.** The server has no WS endpoint. A same-origin proxy at
  `/api/logs/stream` threads the auth cookie upstream so `EventSource` works
  without custom headers, and rejects `Sec-Fetch-Site: cross-site` requests so
  the GUI cannot be used as a confused deputy.
- **Server-side schema validation.** The editor posts raw YAML to the server — no
  Monaco, no ajv, no JSON-schema in the browser. The Go validator stays the
  single source of truth.
- **Page files export `default` only.** Next.js restricts what a `page.tsx` may
  export, so helper components live in sibling files (`LogsConsole.tsx` next to
  `logs/page.tsx`, and so on).

## Not implemented

- Pipeline **creation** (the editor edits existing definitions only).
- Agent **creation** from the guided form. `/editor` creates agents from a blank
  document; the guided form edits agents that already exist.
- Guided editing of `spec.chaos`, `spec.tools`, streaming and other sections —
  the form names them as present and preserves them, but only YAML edits them.
- Isolated/offline request preview, match explanation and suite history — these
  are Release B in the [UI transformation epic](../docs/epics/UI_TRANSFORMATION_EPIC.md)
  and have no backend contract yet.
- Self-service user signup. The admin pages drive the existing key-minting API;
  SSO provisions users just-in-time from the verified OIDC email.
