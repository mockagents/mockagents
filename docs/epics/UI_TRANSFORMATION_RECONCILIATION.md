# UI-01 reconciliation: epic/design vs. actual APIs

Date: 2026-09-02
Baseline inspected: local `main` at `f2e8273`, working tree clean except `docs/`
Status: pre-implementation findings. Blocking items marked **DECISION**.

Method: read `internal/server/route_authz.go`, `internal/server/pipeline_handlers.go`,
`internal/engine/pipeline.go`, `internal/tenancy/{types,middleware}.go`,
`gui/lib/{api,auth,guard}.ts`, `.github/workflows/ci.yml`. GUI baseline captured
before any change (typecheck clean, production build green, 17 routes).

## 1. Epic claims that the source CONFIRMS

| Epic claim | Evidence | Verdict |
|---|---|---|
| §8.1 role floor table | [route_authz.go:29-96](../../internal/server/route_authz.go#L29) | Accurate in every row, including log deletion at any authenticated role |
| UX-05: result has `nodes`, not `node_sequence` | [pipeline.go:33-38](../../internal/engine/pipeline.go#L33) | Confirmed |
| UX-05: node has `node_id`/`agent_name`/`response`/`latency` | [pipeline.go:25-30](../../internal/engine/pipeline.go#L25) | Confirmed |
| UX-05: `latency` is nanoseconds | `time.Duration` marshals as int64 ns | Confirmed — needs /1e6 for ms |
| UX-05: 422 may carry partial `result` | [pipeline_handlers.go:40-43,119](../../internal/server/pipeline_handlers.go#L40) | Confirmed |
| UX-05: node responses may be null | `Response *Response \`json:"response"\`` — no `omitempty` | Confirmed — emits `null` |
| §3: pipeline editor has ETag concurrency | GET sets ETag; PUT requires `If-Match` → 428/412 | Confirmed |
| §5: no global configuration revision | Revision is per-file hash, agents have none | Confirmed — must render "not available" |
| §8: `/identity` + `/capabilities` are NEW | No such route in `route_authz.go` or `server.go` | Confirmed absent |
| UX-02: readiness separate from liveness | `/api/v1/health` and `/api/v1/ready` both exist | Confirmed — already separable |
| UX-08: GUI is absent from CI | `ci.yml` jobs: test-go, test-go-postgres, test-python, demo-rag, framework-recipes, framework-recipe-vercel, lint, docker | Confirmed — no GUI job at all |

No epic claim about wire semantics was found to be wrong. The Revision-2 corrections
hold up against the source.

## 2. Contradictions found (defects, not decisions)

### C1 — The GUI login gate is `platform`, not `admin`, and the error text is wrong

[`login()`](../../gui/lib/auth.ts#L80) validates a pasted key by calling
[`probeTenants`](../../gui/lib/api.ts#L294) → `GET /api/v1/tenants`, whose floor is
[`RolePlatform`](../../internal/server/route_authz.go#L46).

Consequences in multi-tenant mode:

- A **viewer**, **editor**, *or* **admin** key cannot sign in at all. The cookie is
  never set, so every subsequent page renders anonymously.
- The rejection message says *"API key rejected (needs admin role)"* — wrong role name.
- The GUI's non-admin pages (catalog, logs, costs, pipelines) need no tenant-list
  permission whatsoever, so the gate is far stricter than the pages behind it.

Nuance: the SSO path is unaffected — it sets `mockagents_session` directly and
[`getAuthKey`](../../gui/lib/api.ts#L99) falls back to it, bypassing `probeTenants`.
So an SSO viewer can hold a usable session while a raw-key viewer cannot.

This is precisely UX-01's stated criterion ("Viewer login succeeds without tenant-list
permission"). No product decision needed — the epic already decides it.

### C2 — The GUI `Role` union omits `platform`

[`api.ts:267`](../../gui/lib/api.ts#L267) declares `"viewer" | "editor" | "admin"`.
The backend has four roles, [`RolePlatform`](../../internal/tenancy/types.go#L35) being
the one that can actually reach the admin pages. Omitting it is *correct* for key
**creation** — [`IsAssignableViaAPI`](../../internal/tenancy/types.go#L66) forbids
assigning platform via the API — but wrong for **display**: a platform operator's own
role is currently unrepresentable.

### C3 — The stored role is a fabrication

[`auth.ts:96`](../../gui/lib/auth.ts#L96) writes `ROLE_COOKIE = "admin"` unconditionally
on successful login, and [`AuthStatus.role`](../../gui/lib/auth.ts#L30) is typed
`"admin" | "unknown"`. Two problems:

- The value is asserted, never read from the server.
- A cookie cannot satisfy epic §8.1's "capabilities must … refresh on role change".
  A server-side role downgrade leaves the browser believing it is still admin.

### C4 — `gui/README.md` is stale (already flagged in epic §3, confirmed)

Source, not the README's "future" list, defines the baseline. Correcting it is in
UX-08 scope.

## 3. Design-handoff access — BLOCKING for visual stories

The approved Claude Design project
(`claude.ai/design/p/799d7d75-5789-455d-953e-8f091350f80f`) **could not be retrieved**
in this session. Four routes attempted:

| Route | Result |
|---|---|
| `DesignSync` MCP | Requires `/design-login`; cannot run in a non-interactive session |
| `WebFetch` | HTTP 403 |
| Local filesystem | Files not seeded into the workspace |
| Claude in Chrome | No connected browser instances |

Consequence: I cannot verify "matches the approved design", and cannot produce the
before/after comparison against design frames that epic §13 requires for each PR.

**This does not block the current slices.** Epic §15 permits a story to start with an
"explicit nonvisual designation", and both UX-08 (CI/test harness) and UX-01
(identity contract + capability plumbing) are non-visual by nature. It *does* block
UX-02/03/06/07, whose deliverable is substantially the screen design.

To unblock: run `/design-login` once from an interactive Claude Code session on this
machine, or export the seven files (`Reliability Workbench.html`, `tokens.css`,
`workbench.css`, `wb_{shell,mocks,lab,data,admin}.jsx`) into the repo.

## 4. Missing product decisions

> **Status 2026-09-02:** all four are settled. DECISION-1 and DECISION-3 were
> answered by the product owner (single endpoint; capabilities derived from the
> route table) and are implemented in UX-01. DECISION-2 was proceeded on as
> recommended. DECISION-4 was approved as the **additive conditional-write
> route** and is implemented in UX-03 slice A.

### DECISION-1 — One identity endpoint or two? **ANSWERED: single endpoint**

Epic §8 names `GET /api/v1/identity` **and** `/capabilities` in a single contract row
but never says whether they are one resource or two. Every server-rendered page needs
this on each render, so two round-trips is a real cost in the BFF.

**Recommendation (proceeding on this default unless overridden):** one endpoint,
`GET /api/v1/identity`, returning principal + capabilities + server mode together.
`/capabilities` can be added later as a projection without breaking clients.

### DECISION-2 — What does local unauthenticated mode report? **(default taken)**

Epic §8.1 is explicit that local mode "is an explicit exception, not a synthetic viewer
identity", and the design brief forbids showing "safe" merely because a server is local.
In single-tenant mode `tenancyH == nil`, no auth middleware runs, and
[`PrincipalFrom`](../../internal/tenancy/middleware.go#L28) returns nil.

**Recommendation (proceeding on this default):** report
`{"mode":"local","authenticated":false,"role":null}` with capabilities computed as
"everything this build exposes", and require the UI to render local mode as its own
visually distinct state — never as a viewer, never as a green "secure" badge.

### DECISION-3 — Capability granularity **ANSWERED: derive from route floors**

Epic wants "allowed actions" but the server's authorization is per-route. Deriving
capabilities from anything other than `managementRouteFloors` would create a second
source of truth that can drift — exactly the failure `mountManaged`'s panic exists to
prevent.

**Recommendation (proceeding on this default):** derive the capability set
mechanically from `managementRouteFloors` at request time, so a floor change cannot
desynchronize the UI. Capabilities are advisory for rendering only; the server stays
authoritative on every call.

### DECISION-4 — Agent conditional-write route **ANSWERED: additive conditional-write route**

Epic §8.2 requires approving "either an additive conditional-write route or a
versioned/deprecated transition" *before* UX-03, and warns that conditional writes
alone don't protect against legacy unconditional writers. `PUT /api/v1/agents/{name}`
today is unconditional with no ETag. **Not needed for UX-08/UX-01; raising it now
because UX-03 cannot start until it is answered.**

## 5. Scope confirmations for the current slices

- Release B (UX-09…UX-17) is out of scope and will not be implemented.
- No publish, push, or merge without explicit instruction.
- Prototype fixtures are never wired as a backend; every state rendered must come
  from a real API response or an explicitly-labelled unknown.
- Vitest is already the repo's JS runner ([sdk/typescript](../../sdk/typescript/package.json)
  uses vitest 2.1.0), so the GUI harness adopts it rather than introducing a second one.

## 6. Implementation note — a third finding, surfaced during UX-01

Capabilities were first derived from `managementRouteFloors` alone. An
integration test then failed because `/api/v1/audit` and `/api/v1/costs`
returned **404, not 403**: both are mounted only when their store is
configured, so the policy table is a *superset* of any given process's real
surface. Deriving from it alone would have advertised an audit capability on a
server with no audit store — a dead-end action, which the epic's acceptance
gate forbids ("No dead-end actions").

Capabilities are therefore derived from the routes `mountManaged` actually
registers, intersected with the caller's role floor. `TestIdentity_
DoesNotAdvertiseUnmountedRoutes` pins it, and the browser suite re-checks it
against a real server.

A second invariant guards the naming scheme:
`TestCapabilityNames_NoFloorCollision` fails if two routes at *different*
floors ever derive the same capability name. Without it,
`POST /api/v1/pipelines/{name}/run` (viewer) and `PUT /api/v1/pipelines/{name}`
(editor) would both become `pipelines.write`, and the console would offer Save
to a viewer whom the server refuses.

## 7. UX-03 slice A — a fourth finding

The first cut of the revision contract compared the backing file's raw bytes
against the canonical marshaled definition to decide whether the YAML had been
edited out of band. Running it against a real server immediately disproved it:
a freshly loaded `examples/minimal-agent.yaml` already reported the two as
different, because hand-authored YAML never matches canonical output byte for
byte — comments, key order and unset defaults all differ. Every conflict would
have been explained as "edited on disk", including conflicts that had nothing
to do with the file.

Drift is now decided by canonicalizing the file the same way the effective
revision is computed (decode → apply defaults → marshal) and comparing those.
The raw-bytes hash is still folded into the ETag, deliberately: a save rewrites
the file canonically and would destroy a human's comments, so a cosmetic edit
on disk should still block a conditional write — it just is not reported as a
*semantic* change. `TestAgentRevision_HandAuthoredFileIsNotReportedAsDrifted`
pins the distinction, and it was confirmed live: a comment-only edit yields
"changed since it was loaded", a changed `spec.model` yields "was edited on
disk".

## 8. UX-03 slice C — round-trip verification, and a shipped defect it found

Slice C pushes every shipped example through the real `GET → conditional PUT →
GET` path and asserts two properties: canonical form is a **fixed point** (a
no-op save produces a byte-identical document), and **no scalar leaf present in
the hand-authored file is missing afterwards**. 23 of 23 examples pass, which
also establishes that the Go types fully model every field the project ships —
nothing is being silently discarded at load.

### Defect found: `chunk_delay_ms: 0` is overwritten with 50 — **FIXED 2026-09-04**

`examples/gemini-agent.yaml` asks for `spec.behavior.streaming.chunk_delay_ms:
0` — stream with no artificial delay — and the server runs it at 50 ms. The
example does not do what it says.

Three parts of the system already agree that `0` is legal and meaningful:

| Layer | Says |
|---|---|
| [JSON schema](../../schema/mockagents-v1-agent.json) | `"minimum": 0` |
| Streaming engine (`streaming/{openai,anthropic,gemini,pacing}.go`) | `if ChunkDelayMs >= 0 { delayMs = ChunkDelayMs }` — honours an explicit 0 |
| The example itself | sets it to 0 deliberately |

Only [`config/defaults.go:23`](../../internal/config/defaults.go#L23) disagrees:
`if s.ChunkDelayMs == 0 { s.ChunkDelayMs = defaultChunkDelayMs }`. Because the
field is a plain `int`, an explicit `0` is indistinguishable from "unset", and
defaulting runs before the engine ever sees the value — so the engine's `>= 0`
check is dead logic. `yaml:"chunk_delay_ms,omitempty"` compounds it: a genuine 0
would be omitted from marshalled output anyway.

`ChunkSize` uses the same zero-as-unset idiom, but a chunk size of 0 is
meaningless, so it is not affected. This is a one-off, not a pattern.

**Fixed 2026-09-04.** `types.StreamingConfig.ChunkDelayMs` is now a `*int`, so
unset (nil) and an explicit zero are distinguishable. `ApplyDefaults` fills only
a nil; each streaming path falls back to `DefaultChunkDelayMs` for a config that
never went through defaulting, which is what the old `>= 0` guard was reaching
for. `types.Ptr` was added for constructing these values, replacing the local
`intPtr` helpers that had grown in two test packages.

Evidence: `TestPacer_ChunkDelayFromConfig` pins the deterministic mapping from
config to pacer delay, and two real-stream tests show the behaviour — an
explicit zero completes in ~0 ms, an unset field still paces at ~200 ms for the
same content. `TestApplyDefaults_ExplicitZeroChunkDelayIsPreserved` includes a
subtest asserting the shipped gemini example now keeps its zero.

`knownRoundTripDefects` is now empty, and `TestRoundTrip_NoKnownDefects` keeps it
that way: adding an entry has to be a deliberate act rather than a quiet way to
silence a failing corpus.

### A second fix, in the harness

The browser suite pointed the server at the repository's own `examples/`. Every
write it made happened to be *refused* (412/422), so it never dirtied a tracked
file — luck, not design. Adding a test that applies a successful edit would have
started modifying `examples/` on every CI run. `playwright.config.ts` now stages
a throwaway copy of the examples and serves that instead.
