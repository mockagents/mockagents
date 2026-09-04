# MockAgents — Production-Readiness Audit

Date: 2026-09-03
Target: working tree at `main` / `f2e8273` plus uncommitted changes (5,525 insertions across 23 modified files and ~25 untracked files)
Method: six parallel package-level reviews with a "cite exact lines or drop the finding" mandate, followed by independent re-verification of every High/Critical finding against the source. Build, vet, and the full Go suite were run on the working tree.

Gate results on the working tree:

| Check | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test ./... -count=1 -cover` | pass, 34 packages |
| GUI `npm audit` | 8 advisories (1 critical dev-only, 4 high, 3 moderate) |

Finding totals: 1 Critical, 13 High, 38 Medium, 44 Low. Every finding cites lines that were read from the working tree; several candidate findings raised during review were dropped after verification and are listed in §2.5 so nobody re-reports them.

---

## 1. Repository Overview

### 1.1 Architecture summary

MockAgents is a single Go binary (module `github.com/mockagents/mockagents`, Go 1.26.4, no cgo) that impersonates the OpenAI, Anthropic, Gemini, Bedrock, Ollama, Cohere and Azure LLM APIs from declarative YAML. Request flow:

```
HTTP (net/http mux, server.go)
  └─ middleware chain: Recovery → StructuredLogger → CORS → MaxBodySize → otel (no-op) → AuthMiddleware → RouteAuthz → QuotaEnforce → InteractionCapture
       └─ adapter/<provider>.go  (wire JSON ⇄ engine types, chaos/strict error rendering)
            └─ engine.ProcessRequestContext  (registry lookup → scenario match → template render → tool synthesis → strict-tools checks → chaos)
                 └─ streaming/<provider>.go  (SSE chunking + pacing physics) | non-stream JSON
  └─ side channels: storage (SQLite interaction log, async LogWorker → LogBroadcaster SSE), audit (SQLite), quota (token bucket + shared spend ledger), metrics (Prometheus text)
  └─ control plane: /api/v1/* (agents CRUD w/ ETags, tenants, keys, logs, costs, audit, pipelines, quota, validate, identity), /auth/* (OIDC)
  └─ satellite surfaces: MCP (JSON-RPC over http/stdio/SSE/streamable), Realtime (WebSocket), A2A, recording proxy/replay, vector store
```

Ancillary: Next.js 15 GUI (`gui/`), three SDKs (Python, TypeScript, Go), Helm chart, GitHub Actions and GitLab CI templates, MkDocs site.

Size: 43.7k lines of non-test Go across 28 internal packages, 1,703 Go test functions, 147 GUI unit tests, 19 Playwright tests, 251 SDK tests.

### 1.2 Key components and their health

| Component | Lines | Coverage | Verdict |
|---|---|---|---|
| `internal/engine` (+state) | 3,564 | 77.8% / 86.2% | Solid core; unbounded session store is the one structural gap |
| `internal/adapter` | 9,352 | 82.7% | Correct but six copies of the same handler skeleton |
| `internal/server` | 6,145 | 81.7% | Auth floors have holes; two god files |
| `internal/tenancy` | 2,334 | **52.7%** | Security boundary with the lowest coverage in the repo |
| `internal/streaming` | 1,099 | 82.8% | Chunker rewrites whitespace; one non-cancellable sleep |
| `internal/mcp` | 2,722 | 86.1% | Three transports share process-global state |
| `internal/realtime` | 2,916 | 92.9% | Clean single-writer design; no origin check on the socket |
| `internal/recording` | 1,849 | 88.5% | Cassette writer has two real bugs |
| `internal/oidcauth` | 96 | **0%** | Trusts unverified email claim |
| `internal/types` | 890 | **0%** | Enum sync with JSON schema asserted nowhere |
| `cmd/mockagents` | 2,947 | 32.2% | Env parsing fail-open; tracing never wired |

### 1.3 Strengths

- **Import graph is acyclic and intentional.** `engine` never imports `tenancy`; `audit` never imports `tenancy`; `streaming → engine → {state, toolschema, chaos, metrics, observability, types}`. The seams (`engine.WithTenantID`, `Recorder.PrincipalFrom`, `tenancy.DenialHook`) are documented and honoured.
- **Security fundamentals are right where they were attempted.** Parameterised SQL everywhere; `crypto/rand` for ids, keys, session tokens and OIDC state; bcrypt cost 10 on a 192-bit secret; auth cache keyed on SHA-256 not plaintext; PKCE plus state cookie; platform role not assignable via API; GUI cookie is HttpOnly + SameSite=Strict + Secure in prod; zero `dangerouslySetInnerHTML` sinks; `Sec-Fetch-Site` guard on proxy routes.
- **Hot-path engineering is deliberate.** Pooled decode buffers, pre-sized session slices, hash-keyed auth cache, bounded async log writer replacing goroutine fan-out, composite `(tenant_id, id DESC)` log index, checked-in benchmark baseline.
- **The http.Server is configured.** ReadTimeout 30s, WriteTimeout 60s, IdleTimeout 120s, ReadHeaderTimeout 10s, body cap; shutdown order broadcaster → pruner → http → log worker → stores is correct for what it covers.
- **Test discipline is real.** 1,700+ Go tests, a cross-adapter conformance suite, a Postgres conformance leg, and the uncommitted CI diff adds a genuine GUI gate (typecheck, build, vitest, Playwright against the real binary).

### 1.4 Weaknesses

- **Multi-tenant mode is not finishable from the API.** A platform operator can create a tenant but cannot mint its first key; ten provider surfaces 401 in multi-tenant mode; pipelines are not tenant-scoped; a viewer can wipe logs.
- **Every deployment artefact assumes one process.** Registry, sessions, interaction log, audit log, rate buckets and (with SQLite) tenancy are process-local, yet the chart ships HPA max 5 and its CI values use two replicas.
- **Config validation stops at the schema file nobody loads.** `status_code: 42` passes `mockagents validate`, the GUI editor and the write API, then panics net/http on every request.
- **Observability is partly fictional.** The OpenTelemetry provider is never constructed, so the documented OTLP env vars are inert, and incoming `traceparent` would not be joined even if it were.
- **Fail-open configuration.** Mistyped quota env vars disable enforcement; `MOCKAGENTS_MULTI_TENANT=true` silently runs with auth off.
- **Duplication where divergence has already caused bugs.** Six adapter handlers, four streaming-config resolvers, two SQLite/Postgres store copies, two ETag contracts, three error envelopes, five body-decoding paths, three chaos validators with different strictness.

---

## 2. Code Review Findings

Format: severity, title, file:lines, evidence (quoted from the working tree), why it matters, fix.

### 2.1 Critical

#### C-01 · Multi-replica deployment is split-brain, and the chart makes it happen automatically
**Files:** `deploy/helm/mockagents/values.yaml:4-8,174-177`; `deploy/helm/mockagents/ci/test-values.yaml:7`; `deploy/helm/mockagents/templates/deployment.yaml:99-106`; `cmd/mockagents/start.go:520-570`; `internal/quota/quota.go:76-79`; `internal/engine/state/store.go:41`; `internal/engine/agent_registry.go:23`

**Evidence:**
```yaml
# values.yaml:174-177
autoscaling:
  maxReplicas: 5
# ci/test-values.yaml:7
replicaCount: 2
```
```go
// start.go:540, 559 — runs on every pod against its own SQLite file
tenant, err = store.CreateTenant(ctx, "default")
result, err := store.CreateAPIKey(ctx, tenant.ID, "bootstrap-admin", tenancy.RolePlatform)
// start.go:565
fmt.Fprintf(os.Stderr, "Bootstrap admin key (shown once): %s\n", result.Plaintext)
```
```go
// quota.go:76-79 — per-process token buckets
buckets map[string]*bucket
```
Persistence defaults to `emptyDir` (deployment.yaml:104-106); no `pvc.yaml` exists in the chart even though `persistence.size`/`storageClass` are declared in values.

**Why it matters:** With the ClusterIP service round-robining, each pod bootstraps its own `default` tenant and prints its own platform key to stderr (the pod log). A key minted on pod A returns 401 on pod B. Agents created via the write API exist on one pod. `/api/v1/logs` shows a random fraction of traffic. Per-tenant rate limits are N× too generous. Only the monthly spend ledger is shared, and only with Postgres. The HPA turns this on under load without operator action.

**Fix:**
1. Template guard: `{{ if and (or (gt .Values.replicaCount 1) .Values.autoscaling.enabled) (not .Values.env.MOCKAGENTS_TENANCY_DSN) }}{{ fail "replicas>1 requires MOCKAGENTS_TENANCY_DSN" }}{{ end }}`.
2. Implement `MOCKAGENTS_BOOTSTRAP_KEY` (the comment at start.go:518 already promises it) and stop printing the plaintext; write it 0600 under `MOCKAGENTS_DATA_DIR` and print only the prefix and path.
3. Add `templates/pvc.yaml` honouring the declared values, or delete them.
4. Document the minimum HA shape: Postgres tenancy, no write API, session affinity for SSE/WS/SSO, per-pod logs accepted or a Postgres audit backend added.

### 2.2 High

#### H-01 · File watcher deletes an agent from every tenant when one tenant deletes its copy
**Files:** `internal/server/watcher.go:287-305`; `internal/engine/agent_registry.go:273-290`; `internal/server/agent_write_handlers.go:163-180`; `cmd/mockagents/start.go:147`

**Evidence:**
```go
// watcher.go:300
if err := w.Engine.Registry.Remove(name); err != nil {
// agent_registry.go:278-290 — Remove sweeps every owner bucket
for owner, byName := range r.agents {
    def, ok := byName[name]
    ...
    delete(byName, name)
```
Boot-time loads call `RegisterWithSource` (start.go:147) but never seed `watcher.fileAgents`, so after tenant A's `DELETE /api/v1/agents/foo` removes `<tenA>.foo.yaml`, `removeIfUnclaimed("foo")` finds no other claimant and calls the name-only `Remove`, which also deletes the global `foo` and every other tenant's `foo`.

**Why it matters:** The registry explicitly allows same-named agents across tenants (agent_registry.go:15-16). With `--watch` on, one tenant's delete is another tenant's outage.

**Fix:** Track `fileAgents[path] = struct{name, tenant string}`; call `RemoveForTenant(name, tenant)` (already exists, agent_registry.go:312-337); seed `fileAgents` from registry sources in `Start()`; deprecate the cross-tenant `Remove`. Test: boot-load global `foo`, create tenant-A `foo` via API with the watcher running, delete it, assert global `foo` still resolves.

#### H-02 · Platform operator cannot mint the first key for a tenant it creates
**Files:** `internal/server/tenancy_handlers.go:38-49` (own-tenant guard), `:132,154,282` (callers); `cmd/mockagents/start.go:540-559`; `internal/server/route_authz.go:51-55`

**Evidence:**
```go
// tenancy_handlers.go:43-47
pathTenant := r.PathValue("id")
if pathTenant != p.TenantID {
    writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
    return "", false
}
```
`grep -n RolePlatform internal/server/tenancy_handlers.go` returns nothing: there is no platform bypass. The only platform key lives in the `default` tenant (start.go:540-559). No CLI command mints keys.

**Why it matters:** `POST /api/v1/tenants` succeeds, `POST /api/v1/tenants/{new}/keys` 404s. A new tenant can only be populated through SSO JIT provisioning. The test that covers this route places the platform principal inside the target tenant, which is why it passes.

**Fix:** In the guard, `if p.Role == tenancy.RolePlatform { return pathTenant, true }` after confirming the tenant exists; thread the resolved id into `UpdateAPIKeyRole`/`RotateAPIKey`/`DeleteAPIKey`. Integration test: platform creates tenant, mints admin key in it, that key lists its own agents.

#### H-03 · Multi-tenant mode fails closed on ten provider surfaces
**Files:** `internal/server/server.go:725-765` (`skipAuth`); `internal/server/quota_middleware.go:22-38`; `internal/tenancy/middleware.go:178-186`

**Evidence:** The exact-match skip list contains `/v1/chat/completions`, `/v1/messages`, `/v1/messages/count_tokens`, `/v1/models`, `/v1/engines/process`, the three realtime paths, the auth paths, and the Azure `/openai/*` paths. Adapter routes that are **not** in the list: `POST /v1/embeddings`, `POST /v1/responses`, `POST /v1/moderations`, `POST /v1beta/models/{modelmethod}`, `POST /v2/rerank`, `/v1/batches*`, `/v1/messages/batches*`, `/v1/files*`, `/v1/conversations*`, and the Bedrock converse routes. The project's own quota list already disagrees with `skipAuth` (quota_middleware.go:24 includes `/v1/responses` and `/v1/embeddings`).

**Why it matters:** server.go:684-687 states the LLM endpoints are open by design because clients send provider keys that MockAgents ignores. In multi-tenant mode an OpenAI SDK calling `/v1/embeddings` with `sk-…` gets 401; a Gemini SDK gets "missing Authorization bearer token". Each 401 also triggers the synchronous audit write in M-08.

**Fix:** Derive the open set from the adapter registry (add `Open bool` to `adapter.Route`) so `skipAuth` and `isLLMProviderPath` share one source. Add a table test iterating every registered adapter route and asserting anonymous reachability in multi-tenant mode.

#### H-04 · Realtime WebSocket accepts any Origin while still attaching a cookie-derived tenant principal
**Files:** `internal/adapter/realtime.go:364-369`; `internal/tenancy/middleware.go:143-172`; `internal/server/server.go:745`

**Evidence:**
```go
// realtime.go:364-369
c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    Subprotocols:       subprotocols,
    InsecureSkipVerify: true,
})
```
```go
// middleware.go:161-170 — the skip branch still resolves the session cookie
if p := bestEffortPrincipal(store, r); p != nil {
    r = r.WithContext(WithPrincipal(r.Context(), p))
}
```
The adapter then scopes the socket to that tenant (realtime.go:377) and meters spend against it (realtime.go:486-503).

**Why it matters:** Browsers attach cookies to cross-origin WebSocket handshakes. Any page an SSO-logged-in operator visits can open the socket, read tenant-private scenario output, and burn the tenant's monthly spend until everyone in the tenant gets 402. The MCP streamable transport already has an origin allowlist (streamable.go:146-172); this is the only WS surface without one.

**Fix:** Drop `InsecureSkipVerify`; set `OriginPatterns` from `Config.CORSAllowedOrigins` plus loopback; or ignore cookie-derived principals on realtime routes when `Origin` is not allow-listed.

#### H-05 · OIDC JIT provisioning maps tenants from an unverified `email` claim
**Files:** `internal/oidcauth/oidcauth.go:349,372-378`; `internal/server/oidc_handlers.go:80-95`

**Evidence:**
```go
var c struct {
    Email string `json:"email"`
}
if err := idToken.Claims(&c); err != nil {
```
`grep -rn email_verified` across the repo returns nothing. The callback does `tenantID, ok := h.DomainMap[emailDomain(email)]` and provisions the user at `MOCKAGENTS_OIDC_DEFAULT_ROLE`.

**Why it matters:** Precondition: the configured issuer permits user-editable or unverified emails (Auth0 social connections, Okta self-service, any federation). Under that precondition an attacker sets `attacker@acme.com` on their own IdP account and is provisioned into `ten_acme`. Signature, audience and issuer checks do not cover this.

**Fix:** Require `email_verified: true`; optionally pin `hd`/`tid` per mapped domain.

#### H-06 · Anonymous requests each pin a 30-minute session; pinned sessions grow forever
**Files:** `internal/adapter/openai.go:513-518` (same pattern anthropic.go:212, gemini.go:174, responses.go:651-663); `internal/engine/state/store.go:79-96`; `internal/engine/state/session.go:116-140`; `cmd/mockagents/start.go:178-179`

**Evidence:**
```go
func extractSessionID(r *http.Request) string {
    if id := r.Header.Get("X-Session-Id"); id != "" { return id }
    return "sess-" + generateID()
}
// store.go:88-94 — every miss allocates and retains
session := NewSession(id, agentName, s.ttl)
s.sessions[id] = session
```
No `MaxSessions` or history cap exists (`grep -n 'MaxSessions\|maxHistory' internal/engine/state/*.go` is empty).

**Why it matters:** The default SDK path sends no `X-Session-Id`, so each request retains a `Session` (16-cap message slice) for 30-35 minutes. The FB-05 load-target feature invites sustained load tests; at 1k rps that is ~2M live sessions before the first sweep. Conversely a client that pins the header grows `Messages` without bound and refreshes `LastAccess` on every call, so it never expires.

**Fix:** Skip persistence for header-less requests (run the turn on a throwaway session, or 60s TTL); add `MaxSessions` with LRU eviction and a `maxHistory` trim in `appendAssistantMessage`; expose as `MOCKAGENTS_SESSION_MAX` / `MOCKAGENTS_SESSION_HISTORY`.

#### H-07 · Streaming rewrites whitespace: newlines, indentation and space runs are lost
**Files:** `internal/streaming/chunker.go:26-50` (used by streaming/openai.go:118, anthropic.go:167, gemini.go:104, adapter/responses_stream.go:125)

**Evidence:**
```go
words := strings.Fields(content)          // splits on \n \t and space runs
...
chunk := strings.Join(words[i:end], " ")  // rejoins with a single space
```

**Why it matters:** A scenario containing markdown or a fenced code block streams as one space-collapsed line while the non-streaming path returns it verbatim. The obvious contract test (concatenate deltas, compare to non-stream body) fails. Real providers stream exact bytes. `chunker_test.go` has no input containing `\n`.

**Fix:** Chunk on byte offsets that fall at word boundaries but keep the original separators (see §4 for the snippet). Add a newline fixture.

#### H-08 · OpenTelemetry tracing is dead code; documented env vars are inert
**Files:** `internal/observability/tracing.go:56-93,122-133`; `cmd/mockagents/start.go:18-31`; `README.md:684-689`; `deploy/helm/mockagents/values.yaml:161-162`

**Evidence:** `grep -rn 'NewTracerProvider(' --include=*.go .` returns only the definition and its two internal SDK calls; `start.go` does not import `observability`; `tracingEnabled` (tracing.go:90) is never set, so `HTTPMiddleware` returns `next` unchanged and `IsEnabled()` is always false. Even if wired, `StartSpan` at tracing.go:131-133 never calls `Extract`, so an incoming `traceparent` would not be joined.

**Why it matters:** README and Helm values tell operators to set `OTEL_EXPORTER_OTLP_ENDPOINT`; they get zero spans and no error.

**Fix:** In `runStart`, `tp, shutdown, err := observability.NewTracerProvider(ctx, "mockagents", version)` before `server.New`, defer `shutdown` after `srv.Shutdown()`; in `HTTPMiddleware`, `ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))`. Test with `MOCKAGENTS_OTEL_STDOUT=1` asserting a span is emitted through the server.

#### H-09 · Bootstrap platform key is written to stderr (pod logs); the documented Secret-injection var does not exist
**Files:** `cmd/mockagents/start.go:518-519,563-565`; `deploy/helm/mockagents/templates/deployment.yaml:54-60`

**Evidence:**
```go
// start.go:518 (comment)   preset a specific plaintext via MOCKAGENTS_BOOTSTRAP_KEY
// start.go:565
fmt.Fprintf(os.Stderr, "Bootstrap admin key (shown once): %s\n", result.Plaintext)
```
`grep -rn MOCKAGENTS_BOOTSTRAP_KEY --include=*.go .` matches only the comment. The chart renders literal `value:` entries only, no `secretKeyRef`.

**Why it matters:** The only platform-role credential, which the API refuses to mint, lands in every log aggregator with a greppable marker.

**Fix:** See C-01 item 2; add `existingSecret` / `extraEnvFrom` to the chart.

#### H-10 · Helm defaults leave every SQLite store unwritable; multi-tenant crash-loops
**Files:** `Dockerfile:41`; `deploy/helm/mockagents/values.yaml:42-47,151-157`; `templates/deployment.yaml:83-107`; `cmd/mockagents/start.go:191-201,233-243,274-279,438-449`

**Evidence:** `readOnlyRootFilesystem: true`, `persistence.enabled: false`, `/data` mounted only when persistence is on, `MOCKAGENTS_DATA_DIR` never set, `WORKDIR /data`. Log store open failure is a `Warn` and continue (start.go:194-197), same for audit (start.go:237-240, contradicting the "always enabled" comment at :229); tenancy store failure is fatal (start.go:274-279).

**Why it matters:** A default `helm install` produces a pod with no interaction log, no audit trail, and readiness reporting green (the `log_store` check is added only when a store exists, server.go:455-460). Setting `MOCKAGENTS_MULTI_TENANT=1` yields CrashLoopBackOff.

**Fix:** Always mount a writable data volume (emptyDir default) and set `MOCKAGENTS_DATA_DIR=/data`; make audit-open failure fatal in multi-tenant mode; document persistence as a multi-tenant prerequisite.

#### H-11 · Agent write API cannot work under the chart (read-only ConfigMap mount)
**Files:** `deploy/helm/mockagents/templates/deployment.yaml:43-44,83-86`; `internal/server/agent_write_handlers.go:332-347`

**Evidence:** `--agents-dir /agents` mounted `readOnly: true`; `persistAndRegister` writes the file first and registers only on success (:342-345); the in-memory path exists only when `AgentsDir == ""` (:337-340).

**Why it matters:** The GUI editor save and `mockagents add` return 500 on every chart deployment.

**Fix:** initContainer copying the ConfigMap into an emptyDir overlay, or an explicit `--agents-write-dir`, or an in-memory mode that reports `persisted:false`.

#### H-12 · GUI ships `next@15.5.15` with four high advisories against server actions
**Files:** `gui/package.json:15`; `gui/package-lock.json`

**Evidence:** `npm audit --json` → `{"high":4,"critical":1,"moderate":3}`; `next` fixed in `>=15.5.21` (GHSA-m99w-x7hq-7vfj server-action DoS, GHSA-89xv-2m56-2m9x SSRF in server actions, GHSA-68g3-v927-f742 cache confusion, GHSA-955p-x3mx-jcvp server function endpoint disclosure). `postcss` and `sharp` have fixes available; the critical is `vitest@2.x` (dev-only).

**Why it matters:** The GUI is the one component holding a raw admin key and it is built on server actions (lib/auth.ts, edit/page.tsx, account/page.tsx).

**Fix:** `next ^15.5.21`, `npm audit fix` for postcss/sharp, plan vitest 2→5, add `npm audit --audit-level=high` to the GUI CI job (ci.yml:262-306).

#### H-13 · No operational runbook; `RUNBOOK.md` is a stale, git-ignored July push checklist
**Files:** `RUNBOOK.md:1-12`; `.gitignore:57`; `docs/` (no ops doc)

**Evidence:** RUNBOOK.md:1 "July-1 Push/Merge Runbook", :3-5 "Local-only. This file is gitignored". `grep -rli 'backup\|restore\|upgrade' docs/*.md docs/guides/*.md` → only RELEASING.md. Bootstrap runs only when no platform key exists (start.go:549-554), so a lost key means editing the DB by hand.

**Fix:** `docs/guides/operations.md` covering backup/restore of the three SQLite files, platform-key recovery, OIDC secret rotation, SQLite→Postgres migration, upgrade with schema changes, replica constraints.

### 2.3 Medium

#### M-01 · Agent validator never checks `chaos.errors` / `latency` / `rate_limit`; out-of-range `status_code` panics net/http per request
**Files:** `internal/config/validator.go:270-300`; `schema/mockagents-v1-agent.json:412-441`; `internal/engine/chaos.go:407-415`; `internal/adapter/encode.go:292-293`
**Evidence:** `validateChaos` checks only `preset` and `connection.{mode,rate,fail_first}`; `grep -n status_code internal/config/validator.go` is empty. The schema declares `status_code` 400..599 but no Go code loads the schema (its only reference is README text in cli/scaffold.go:184). `WriteHeader(42)` and `WriteHeader(1000)` panic (verified by a proof program). `timeout_ms` is uncapped (chaos.go:397 sleeps the full value; latency is capped at :375). `rate_limit.window_ms <= 0` silently disables the limiter (:430-432). The MCP and A2A validators enforce equivalents (mcpserver_validator.go:57-70, a2aserver_validator.go:53-66).
**Why:** Reachable by any editor via the write API, GUI editor and MCP `--manage`; every request to the agent then drops the connection.
**Fix:** `validateChaosErrors/Latency/RateLimit` mirroring the schema; clamp in `pickStatusCode`; add a schema-vs-validator parity test.

#### M-02 · Chaos counters and rate-limit buckets keyed by agent name only, shared across tenants
**Files:** `internal/engine/chaos.go:162-168,214,436-445`
**Evidence:** `counter[agentName]++`; `c.checkRateLimit(agent.Metadata.Name, ...)`. The registry allows same-named agents per tenant.
**Fix:** Key on `TenantID + "\x00" + Name` (the `scopedSessionKey` convention, engine.go:504-506); reset on `Register`/`Remove`.

#### M-03 · `ReloadAgent` mutates the registry outside `agentWriteMu` and parses the whole directory
**Files:** `internal/server/handlers.go:164-230`; `agent_write_handlers.go:51-52,92-93,148-149`
**Evidence:** No lock in `ReloadAgent`; `config.LoadDir(h.AgentsDir)` at :177; `RegisterWithSource` at :209.
**Why:** Reload can interleave with `DELETE` and resurrect the deleted definition; the UX-03 precondition check is unprotected against a concurrent reload.
**Fix:** Take `agentWriteMu`; load only the tracked source via `config.LoadFile`.

#### M-04 · Pipeline routes are not tenant-scoped
**Files:** `internal/server/pipeline_handlers.go:203,288,294,320`; `internal/types/pipeline.go:12`
**Evidence:** `h.Registry.GetPipeline(name)` is global; `atomicWriteFile` + `RegisterWithSource` persist for everyone; `validateAgentRefs` uses `AgentRegistry.Get` (global only) so tenant-owned agent refs are rejected.
**Fix:** Floor `PUT /api/v1/pipelines/{name}` at platform in multi-tenant mode or add `Metadata.TenantID`; use `GetForTenant` in ref validation.

#### M-05 · `DELETE /api/v1/logs` is `roleOpen`
**Files:** `internal/server/route_authz.go:70`; `log_handlers.go:163-188`
**Evidence:** `"DELETE /api/v1/logs": roleOpen,` — the snapshot test pins it.
**Fix:** `tenancy.RoleAdmin`; update snapshot; add a `logs.deleted` audit kind.

#### M-06 · Default CORS is `*` on an unauthenticated single-tenant server; no flag to change it
**Files:** `internal/server/middleware.go:110-131`; `server.go:51-54,252`; `cmd/mockagents/start.go` (no assignment)
**Evidence:** `wildcard := len(allowedOrigins) == 0` → `Access-Control-Allow-Origin: *`. `grep -rn CORSAllowedOrigins` finds only the field and its one use; `runStart` never sets it. The rationale comment (middleware.go:114-115, "Auth is Bearer-token, not cookie") predates the session cookie.
**Why:** Any origin the developer visits can `fetch("http://127.0.0.1:8080/api/v1/logs")` and read every captured body (`LOG_BODIES` defaults to `full`), or `DELETE /api/v1/logs`.
**Fix:** `--cors-origins` / `MOCKAGENTS_CORS_ORIGINS`; default loopback GUI origins; no wildcard in multi-tenant mode; add `Access-Control-Expose-Headers: ETag, X-Mockagents-Revision-*, X-Request-Id, X-Mockagents-Strict-Violation` and `If-Match, If-None-Match` to allowed headers so third-party browser clients can use the conditional-write contract.

#### M-07 · `/v1/engines/process` is mounted unconditionally, open, and quota-exempt
**Files:** `internal/server/server.go:432,453-483`; `quota_middleware.go:22-38`; `internal/engine/engine.go:25`
**Evidence:** Comment says "internal/testing"; in `skipAuth`; not in `isQuotaPath`; caller selects `agent_name` directly; raw engine error text returned (:470).
**Fix:** Behind `Config.EnableEngineEndpoint` (default off) or move under `/api/v1/engine/process` with an editor floor and quota.

#### M-08 · Every 401/403 performs a synchronous SQLite audit insert on the request goroutine; no retention
**Files:** `internal/server/server.go:167-175`; `internal/audit/recorder.go:279-292`; `internal/audit/store.go:108-121`; `internal/tenancy/middleware.go:33-38`
**Evidence:** The hook contract says "must not block"; the only implementation calls `r.Store.Append` synchronously and discards the error. `grep -rn "DELETE FROM audit_events"` is empty.
**Why:** An unauthenticated client drives one durable write per request; combined with H-03 every misrouted SDK call does this. Disk grows without bound.
**Fix:** Route `auth.denied` through a bounded async writer (the `log_worker.go` pattern) with drop-on-full counter; `MOCKAGENTS_AUDIT_MAX_ROWS` reusing `logPruner`; per-IP coalescing.

#### M-09 · Unauthenticated requests force one bcrypt each; no negative cache or failure limiter
**Files:** `internal/tenancy/store.go:762-771`; `postgres_store.go:578-589`; `auth_cache.go:110`
**Evidence:** Timing-equaliser bcrypt on every wrong key; cache stores successes only; no rate limiter anywhere in server/tenancy.
**Fix:** 30s negative cache keyed on `sha256(plaintext)`; per-IP failure bucket in `AuthMiddleware`; bounded semaphore around bcrypt.

#### M-10 · SQLite `UpdateAPIKeyRole` is a non-transactional read-modify-write; the atomicity comment is wrong
**Files:** `internal/tenancy/store.go:428-446` vs `postgres_store.go:282-303`
**Evidence:** Comment claims `MaxOpenConns(1)` serialises the pair; it serialises statements. `prev` can be stale; UPDATE after a concurrent delete affects 0 rows and returns success.
**Fix:** Mirror Postgres: tx → SELECT → UPDATE → `RowsAffected()==0 → ErrNotFound` → commit.

#### M-11 · SQLite `RotateAPIKey` runs bcrypt inside the transaction, pinning the single connection
**Files:** `internal/tenancy/store.go:459,492,506` (contrast the package's own PERF-10 reasoning at :573-579)
**Fix:** Read outside tx, hash, then tx with `WHERE id = ? AND prefix = ?`; 0 rows → `ErrConflict`.

#### M-12 · Quota overrides and rate buckets do not propagate across replicas; package doc is stale
**Files:** `internal/quota/quota.go:4-6,113-120`; `internal/server/quota_handlers.go:151-161`; `cmd/mockagents/start.go:642-660`
**Fix:** Read overrides through the same 5s TTL cache as `currentSpend`; document rate as per-replica or move buckets to the shared store.

#### M-13 · Quota bypassable by omitting credentials (tenant "" never limited)
**Files:** `internal/quota/quota.go:147-149,184-186`; `internal/server/quota_middleware.go:56-60`
**Evidence:** `if tenantID == "" { next.ServeHTTP(w, r); return }`; LLM routes are open, so a bad/absent key runs as tenant "".
**Fix:** `MOCKAGENTS_LLM_REQUIRE_AUTH=1` (SaaS mode), or an anonymous-source rate limit when `tenantID == ""`.

#### M-14 · `audit_events` lacks an index on `actor_tenant`, the filter every multi-tenant read applies
**Files:** `internal/audit/store.go:28-30,173-176,188`; `internal/server/audit_handlers.go:43-44`
**Fix:** `CREATE INDEX IF NOT EXISTS idx_audit_tenant_id ON audit_events(actor_tenant, id DESC)`.

#### M-15 · Audit `actor_ip` trusts client-supplied `X-Forwarded-For`
**Files:** `internal/audit/recorder.go:67-72`
**Fix:** Honour XFF only from `MOCKAGENTS_TRUSTED_PROXIES` CIDRs; take the right-most untrusted hop; record `RemoteAddr` alongside.

#### M-16 · LLM SSE streams have no per-write deadline and are severed by the 60s WriteTimeout
**Files:** `internal/server/server.go:32,277`; no `SetWriteDeadline` in streaming/adapter; contrast `log_handlers.go:771-774`
**Why:** Paced/load-target streams plus chaos latency over 60s are cut mid-frame with no terminal event, indistinguishable from the deliberate `truncateAfter` fault.
**Fix:** Bump the deadline per chunk in the shared SSE writer, or `WriteTimeout: 0` with per-handler deadlines. Test with `WriteTimeout=200ms` and a 500ms paced stream.

#### M-17 · Tool-argument streaming uses `time.Sleep`, ignoring client disconnect
**Files:** `internal/streaming/openai.go:243-264` (every other sleep uses `sleepCtx`)
**Fix:** `sleepCtx(ctx, ...)`; route through `pacer.delayFor(1)` so load-target physics apply to argument deltas.

#### M-18 · Anthropic streaming `output_tokens` uses a different formula than non-streaming
**Files:** `internal/streaming/anthropic.go:268` (`len(resp.Content)/4 + 1`) vs `internal/adapter/anthropic.go:296` (`EstimateTokens`)
**Why:** Identical traffic accrues different `cost_usd` and monthly spend depending on `stream: true`.
**Fix:** Pass the adapter-computed count into `StreamAnthropic` as `promptTokens` already is.

#### M-19 · Engine errors classified by substring match, duplicated across six adapters
**Files:** `internal/adapter/openai.go:207-213`, `anthropic.go:244-250`, `gemini.go:206-212`, `responses.go:362-365`, `ollama.go:124-127`, `bedrock.go:151`
**Evidence:** `strings.Contains(err.Error(), "not found")` → 404; `"empty"` → 400. A scenario named `empty-cart` with a broken template returns 400.
**Fix:** `engineErrorStatus(err)` using `errors.Is` against typed sentinels; per-provider error type by status.

#### M-20 · `MemoryStore.GetOrCreate` takes the exclusive lock on every request
**Files:** `internal/engine/state/store.go:79-96`
**Fix:** RLock fast path for the hit case, re-check under write lock on miss.

#### M-21 · Vector query clones metadata for every candidate before top-k truncation
**Files:** `internal/vector/store.go:403-421`
**Fix:** Collect `{id, score, *Point}`, sort, truncate, then clone the surviving `TopK`.

#### M-22 · `mockagents record`/`replay` bind all interfaces, no auth, relay the operator's provider key; bodies unbounded
**Files:** `cmd/mockagents/record.go:63,74-85`; `replay.go:61,136-147`; `internal/recording/proxy.go:100-103`; `cassette.go:316-326`
**Evidence:** `Addr: fmt.Sprintf(":%d", recordPort)`; `proxyReq.Header.Set("Authorization", "Bearer "+p.UpstreamAPIKey)`; `DrainBody` is bare `io.ReadAll`. `mockagents mcp` defaults to 127.0.0.1 for exactly this reason (mcp.go:64-68).
**Fix:** `--bind` defaulting to 127.0.0.1; `http.MaxBytesReader` around `DrainBody`.

#### M-23 · `mockagents mcp --manage` exposes unauthenticated agent create/replace/delete (disk writes)
**Files:** `cmd/mockagents/mcp.go:64-71,88-97,198-226`; `internal/mcpadmin/manager.go:273-288,326-342`
**Fix:** Require a key when `--manage` is set, or refuse `--manage` with a non-loopback bind.

#### M-24 · Bidirectional MCP admin routes decode bodies with no size cap and honour an unbounded client timeout
**Files:** `internal/mcp/sse.go:395-398,442-453`
**Evidence:** `json.NewDecoder(r.Body)` without `MaxBytesReader` (every other MCP entry has it: http.go:57,139; streamable.go:202,435); `X-MCP-Timeout-Ms: 999999999` parks a goroutine and a `pending` entry.
**Fix:** `MaxBytesReader`; clamp timeout to 60s.

#### M-25 · Legacy `/mcp/notify` has no Origin check and feeds two unbounded process-wide queues
**Files:** `internal/mcp/http.go:133-151`; `server.go:65-76`; `bidirectional.go:122-134`
**Fix:** Cap both queues (drop-oldest); apply `originAllowed`.

#### M-26 · `Cassette.Append` writes outside the lock; concurrent record-on-miss loses interactions; O(n²) rewrite
**Files:** `internal/recording/cassette.go:128-146,276-300`
**Fix:** Single writer or write mutex; `O_APPEND` a single JSON line.

#### M-27 · `bidirectional.Subscribe` drops buffered server-initiated requests when a second subscriber steals the stream
**Files:** `internal/mcp/bidirectional.go:76-82,100-114`
**Fix:** Drain the old channel into `b.outbound` in the steal branch.

#### M-28 · Recording proxy stores non-JSON bodies as `json.RawMessage`, poisoning the cassette for the process lifetime
**Files:** `internal/recording/proxy.go:126-134,142-146`; `cassette.go:35-38,286-289`
**Evidence:** Marshal of an `Interaction` holding `<html>502</html>` fails (verified); `Append` retains it and re-serialises the whole slice, so every later recording fails.
**Fix:** `json.Valid` both bodies; store non-JSON as string/base64; drop un-encodable interactions.

#### M-29 · `writeCassette` creates its temp file in the OS temp dir and renames across filesystems
**Files:** `internal/recording/cassette.go:278,299`
**Evidence:** `os.CreateTemp("", ...)` then `os.Rename` → EXDEV on tmpfs `/tmp` (most container images). `mcpadmin.atomicWriteFile` does it correctly.
**Fix:** `os.CreateTemp(filepath.Dir(path), ...)`; extract a shared `fsutil.AtomicWrite`.

#### M-30 · Streamable HTTP MCP sessions never expire; 256 anonymous `initialize` calls evict every live session
**Files:** `internal/mcp/streamable.go:65,483-498`
**Fix:** Track `lastSeen`; evict idle first; FIFO last resort.

#### M-31 · Loader is recursive but `--watch` is not
**Files:** `internal/config/loader.go:371-419`; `internal/server/watcher.go:85-96`
**Fix:** `fsw.Add` each walked subdirectory, or warn at start.

#### M-32 · Realtime session memory is unbounded per connection on an unauthenticated socket
**Files:** `internal/realtime/session.go:155,169,229-232,565-573,641-642`
**Fix:** Cap `items`/`history`; bound client item id length.

#### M-33 · Recording proxy `http.Client{Timeout: 60s}` covers the whole SSE body; long streams persist as partial cassettes
**Files:** `internal/recording/proxy.go:74,105,167-212`
**Fix:** Per-phase `Transport` timeouts + `r.Context()`.

#### M-34 · Graceful shutdown cannot drain SSE/WS; readiness never flips during drain; 5s hard-coded
**Files:** `internal/server/server.go:39,268-275,639-676`; `cmd/mockagents/start.go:347-365`; `internal/mcp/sse.go:72-77`; `internal/adapter/realtime.go:364`; `readiness_handlers.go:48-70`; chart has no `preStop`
**Evidence:** No `BaseContext`/`RegisterOnShutdown`, so request contexts are never cancelled; hijacked WS conns are ignored by `Shutdown`; K8s default grace is 30s vs 5s here; a deadline error exits 2.
**Fix:** `BaseContext` from a cancellable ctx, cancel after grace; track and close realtime sessions; `draining` flag makes `/ready` 503 first; `preStop` sleep + `terminationGracePeriodSeconds`; `Config.ShutdownTimeout`; treat deadline as warning.

#### M-35 · Env parsing silently swallows invalid values; booleans parse inconsistently
**Files:** `cmd/mockagents/start.go:59-63,262,575-583,612-616,628,680-693`; `internal/observability/tracing.go:58,75`; `internal/adapter/realtime.go:394`
**Evidence:** `pf := func(k string) float64 { v, _ := strconv.ParseFloat(...) }` → garbage = 0 = unlimited; `MOCKAGENTS_MULTI_TENANT` compared to exactly `"1"` while `CHAOS_OFF` accepts `1|true|yes|on`; `MOCKAGENTS_PORT=808O` starts on 8080; unknown log level → info.
**Why:** A typo in the chart's free-form `env:` map runs the control plane with auth off.
**Fix:** One `envBool` parser; fail-closed (return error from `runStart`) for PORT/quota/TTL/MULTI_TENANT; reject unknown levels.

#### M-36 · Single-tenant default exposes the whole management plane unauthenticated on 0.0.0.0 in container/chart
**Files:** `internal/server/route_authz.go:25-27,64-68,99`; `Dockerfile:49`; `values.yaml:70,80-90,207-208`; `templates/NOTES.txt:29-30`
**Fix:** `networkPolicy.enabled=true` when ingress is on; `MOCKAGENTS_LOG_BODIES=sanitized` chart default; consider a `MOCKAGENTS_ADMIN_TOKEN` for write routes in single-tenant mode.

#### M-37 · CI supply chain: floating action tags, inconsistent versions, GoReleaser `latest`, no Dependabot, no govulncheck/SAST, no signing/SBOM, broad release permissions, `:latest` Docker tag never pushed
**Files:** `.github/workflows/ci.yml:33,36,119,156,238,273,310,316-348`; `release.yml:8-10,44-46,73,94-103`; `.goreleaser.yml:43-45`; `go.mod:5-9`; `Makefile:60-62`
**Evidence:** `checkout@v5` vs `@v4`, `setup-go@v6` vs `@v5`, `setup-node@v4/v5/v6` across workflows; `grep -rnE 'uses:.*@[0-9a-f]{40}'` → 0; no `dependabot.yml`; `grep -rni 'govulncheck\|golangci\|gosec\|codeql'` in `.github/ Makefile` → 0 despite go.mod claiming "govulncheck clean"; no `signs:`/`sboms:`; top-level `contents: write, packages: write` inherited by the test job; `type=raw,value=latest,enable={{is_default_branch}}` on a tag-only trigger is always false, so README's `docker run mockagents/mockagents` 404s.
**Fix:** SHA-pin actions; Dependabot for actions/gomod/npm/pip; `govulncheck-action` + `golangci-lint` (gosec, errcheck, bodyclose) jobs; cosign keyless + syft SBOM; job-level permissions; `enable=true` for `latest`.

#### M-38 · GUI: open-redirect guard bypassable with backslash; no fetch timeouts; O(n·m) diff per keystroke; TS SDK CRLF/timeout bugs; Go SDK assertion semantics diverge
**Files:** `gui/app/login/page.tsx:22,25-29`; `gui/lib/api.ts:144-150,608,648,683,723,779,906`; `gui/app/agents/[name]/edit/AgentEditor.tsx:74,84-91`; `gui/lib/diff.ts:22,42-49`; `sdk/typescript/src/client.ts:54,261,296,306-311,391-394`; `sdk/go/mockagents/expect.go:28-43,89-104`
**Evidence:** `next.startsWith("/") && !next.startsWith("//")` admits `/\evil.com` (browsers treat `\` as `/`); `grep -n "signal|AbortSignal|timeout" gui/lib/api.ts` → none; `useMemo(() => diffLines(base, draft))` allocates up to a 9M-cell LCS table per keypress; TS SSE splits on `\n\n` only and its 30s `timeoutMs` is a whole-stream deadline; Go `ExpectScenario` binds to `result.Last()` while Python/TS aggregate.
**Fix:** Same-origin URL parse for `next`; `AbortSignal.timeout(10_000)` in `fetchJSON`; compute diff only in the preview phase; match `\r\n\r\n`; header-only timeout with `reader.cancel()`; aggregate `ToolCalls()` in the Go SDK.

### 2.4 Low

Grouped by area; each carries a file:line anchor.

**Server**
- L-01 `GET /api/v1/agents/{name}` re-reads and triple-parses the source file per request — `handlers.go:133-135`, `agent_revision.go:110-145`. Cache `(mtime,size) → revision`.
- L-02 Revision `Source` hash omitted for a global agent read via a tenant principal (ETag differs by caller) — `agent_revision.go:149-156`. Use `def.Metadata.TenantID`.
- L-03 `Config.MaxBodyBytes == 0` rejects every body; no default fallback — `server.go:251,264-267`; `middleware.go:343-352`.
- L-04 `writeJSON` sends status + empty body on encode failure, logs via global `slog` losing request id — `handlers.go:253-269`; same in `adapter/encode.go:279-295`.
- L-05 Three error envelopes; `http.Error` sends `text/plain` for JSON bodies — `middleware.go:334`; `log_handlers.go:748-753`; `quota_middleware.go:67-71`.
- L-06 `DeleteAgent` falls back to a guessed `<tenant>.<name>.yaml` path, contradicting its comment — `agent_write_handlers.go:157-176`.
- L-07 `atomicWriteFile` does not fsync; pipeline-specific temp prefix used for agents — `pipeline_handlers.go:380-396`.
- L-08 Two divergent conditional-write contracts (agents optional `If-Match` with `*`/lists; pipelines required, 428) — `agent_revision.go:195-266` vs `pipeline_handlers.go:263-281`.
- L-09 `ValidateHandler` method check is dead; strict decode inspects only the first YAML document — `validate_handler.go:458-463`; `agent_strict_fields.go:319-324`.
- L-10 Audit reads hide `auth.denied` (empty tenant) from every multi-tenant reader including platform — `audit_handlers.go:383-385`.
- L-11 Spend hook parses the full response body synchronously on the request goroutine, then the worker parses it again — `server.go:216-231`; `log_handlers.go:471-473`.
- L-12 Watcher lifecycle owned by `cmd`, not `Server`; shutdown timeout not configurable — `server.go:39`; `start.go:333-346`.

**Engine / adapter / streaming**
- L-13 `TemplateContext.Timestamp` never set; `Vars` never written — `response_generator.go:46-54`; `engine.go:338-346`.
- L-14 Streaming drops `refusal` when both `content` and `refusal` are set (Anthropic/Gemini) — `streaming/anthropic.go:153-156`; `gemini.go:98-101`.
- L-15 SSE 200 + headers flushed before the TTFT sleep, so TTFB does not reflect the FB-05 distribution — `sse_writer.go:17-32`; `streaming/openai.go:80,104`.
- L-16 `ChaosInjector` takes the process mutex up to five times per request — `chaos.go:118-175,436`.
- L-17 Template/regex/lower-case caches never evicted; write API and reload make the "finite static set" assumption false — `response_generator.go:133-156`; `scenario_matcher.go:30-34,184-198`.
- L-18 Token estimates ignore tool definitions, tool-call arguments and images — `openai.go:255,266-267`; `anthropic.go:295-296`; `gemini.go:231-232`.
- L-19 `ProcessToolCalls` spawns a goroutine per call for pure-CPU work while holding the session lock — `tool_processor.go:70-81`.
- L-20 Adapters re-resolve the agent post-engine four times over; single-agent fallback allocates a merged map + sorted slice per streaming request — `openai.go:238-249`, `anthropic.go:273-284`, `gemini.go:238-249`, `responses.go:443-458`.
- L-21 `WithTenantID` doc references the dead `X-Mockagents-Tenant` header — `reqmeta.go:268-278`.

**Tenancy / storage / audit / quota**
- L-22 Audit `Since` filter is a lexical compare over variable-width `RFC3339Nano` — `audit/store.go:95-99,112,177-180`. Use fixed-width layout.
- L-23 Bulk rotate can clobber a concurrent single rotate (no prefix guard) — `store.go:580-609,639-642`; `postgres_store.go:387-405`.
- L-24 Global cache flush on every key mutation → cross-tenant bcrypt thundering herd — `auth_cache.go:146-155`.
- L-25 Sessions carry a role/tenant snapshot; no user revocation or role-change path — `identity_sqlite.go:54-58,86`; `store.go:44-125`.
- L-26 Expired sessions never garbage-collected — `identity_sqlite.go:81-85`; `identity_postgres.go:190-193`.
- L-27 `Retry-After` truncated rather than ceiled — `quota_middleware.go:62-66`.
- L-28 Spend backend error fallback is overwritten by the next refresh; comment claims charge is never lost — `quota.go:216-219,261-263`.
- L-29 Postgres pool has no `ConnMaxLifetime`; boot DDL unserialised across replicas — `postgres_store.go:115-116,124`.
- L-30 `ORDER BY created_at` on second precision is non-deterministic — `store.go:306,393,571`. Add `, id ASC`.
- L-31 Unknown models priced at $0 silently, so the 402 cap is unenforceable for current model names — `pricing.go:48-50,66-70,127-130`.
- L-32 `Recorder.Record` swallows append failures without logging — `recorder.go:279-292`.
- L-33 Metrics cardinality ceiling (1000) is permanent and unconfigurable — `metrics.go:37,308-311`.
- L-34 `storage.Log` inserts an empty `timestamp` when the caller leaves it blank — `sqlite.go:28,185-191`.
- L-35 `tracingEnabled` is a bare package global on the hot path — `tracing.go:41,90`. `atomic.Bool`.

**MCP / realtime / a2a / recording / cli**
- L-36 A2A card URL trusts `X-Forwarded-Proto` verbatim — `a2a/server.go:692-701`.
- L-37 A2A `tasks/get` marshals outside the lock while `cancel` mutates under it — `server.go:376-382,390-402`.
- L-38 MCP tool handlers run on `context.Background()`; a handler panic kills a stdio session — `mcp/server.go:401`; `stdio.go:80-83`.
- L-39 A2A JSON-RPC: notifications for known methods get a response; batches → -32700 not -32600 — `server.go:201-234,246-286`.
- L-40 MCP `inited`/`logLvl`/`subscribed`/`pending` are process-global, not per streamable session — `server.go:28-32,172-176,603-606`.
- L-41 `mcpadmin` round-trips YAML non-strictly so typo'd fields vanish before validation — `manager.go:225-245`.
- L-42 `mockagents init --force <dir>` unconditionally `RemoveAll`s `<dir>/agents` and `<dir>/tests` — `cli/scaffold.go:110-120`.
- L-43 Standalone MCP/record/replay servers set only `ReadHeaderTimeout` — `mcp.go:170-174`; `record.go:81-85`; `replay.go:143-147`.
- L-44 Realtime `input_audio_buffer.append` base64-decodes each frame twice — `session.go:482-489`; `vad.go:198-203`.

**Ops / docs / GUI / SDK**
- CI `cancel-in-progress` also cancels `main` runs — `ci.yml:3-7,15-17`.
- Docker image-size gate only warns; Helm never linted in CI — `ci.yml:359-366`.
- `alpine:3.19` is EOL (2025-11-01); UID not pinned while chart hard-codes 100; no digest pins — `Dockerfile:2,20,22-24`; `values.yaml:38`.
- docker-compose runs at `debug` with no hardening — `docker-compose.yml:10-19`.
- ServiceAccount token auto-mounted; NetworkPolicy off — `templates/serviceaccount.yaml`; `values.yaml:207-208`.
- `go.sum` not tidy; GoReleaser runs `go mod tidy` as a before-hook — `.goreleaser.yml:5-8`.
- Composite actions default to `go install …@latest` — `deploy/actions/*/action.yml`.
- 8 `MOCKAGENTS_OIDC_*` vars undocumented; `MOCKAGENTS_BOOTSTRAP_KEY` and `MOCKAGENTS_REALTIME_GA_DEFAULTS` documented but not read; multi-tenant guide says "API keys only" — `docs/guides/multi-tenant.md:174`; `docs/design/realtime-server-vad.md:170`.
- GUI: SSE 401 probe uses HEAD on a GET-only route and opens a real broadcaster subscriber — `LogsConsole.tsx:151-166`; prod CSP allows `'unsafe-inline'` and no HSTS — `next.config.ts:15,30-45`; `saveAgentYAML` forwards raw upstream error bodies — `api.ts:927-931`; inconsistent agent-name path encoding — `logs/[id]/page.tsx:55`; editor has no navigation guard despite promising the draft is never lost — `AgentEditor.tsx:5-9,72`; `yamlPath.ts` misclassifies `spec:  # comment`, never matches quoted keys, leaves `1e3`/`.inf` unquoted — `yamlPath.ts:67-85,204-206,230-241,283-294`.
- SDKs: Python `chat(stream=True)` skips `raise_for_status()` and requires `data: ` with a space — `client.py:80-84,114`; Go SDK lacks `ToHaveToolCallSequence`/`ToHaveToolError`; TS lacks `toHaveToolError`; CHANGELOG `[Unreleased]` spans 65 entries since v0.4.0 — `CHANGELOG.md:13-663`.
- API spec drift: `GET /api/v1/logs/stream`, `POST /api/v1/pipelines/{name}/run`, `POST /v1/engines/process` and 68 adapter routes absent from `docs/api-spec.yaml`; `logs.limit` max 1000 in spec vs 10000 in code (`handlers.go:25`); `since`/`until` params undocumented; 413/503 responses undocumented. `make drift` checks `$ref`s only.

### 2.5 Verified sound (do not re-report)
Regex DoS (Go RE2 is linear); pooled-buffer aliasing in `decode.go` (`json.Unmarshal` copies); shared `math/rand` in `pacing.go` (per-stream `rand/v2`); `LogWorker` Submit/Shutdown locking; `LogBroadcaster` non-blocking publish; `StreamLogs` ctx cancellation and deadline bumps; otel `statusRecorder` implements `Flush`/`Unwrap`; `checkAgentPrecondition` runs inside `agentWriteMu` and rejects weak ETags; `safeAgentName` + abs-prefix check close traversal; random tenant ids make filename sanitisation collision-free; `SetDenialHook` is atomic; GUI cookie flags, CSRF guard, XSS sinks, live-feed bound (`MAX_ROWS = 500`), SSE proxy pass-through and upstream abort; realtime single-writer goroutine; Go SDK SSE parser (both separators, 1 MiB frame cap, multi-line join).

---

## 3. System Design Review

### 3.1 Architectural flaws

**F1 · The process is the unit of state.** Six stores (registry, sessions, interaction log, audit, rate buckets, chaos counters) are process-local with no abstraction that would let them be shared. Only tenancy has a `Store` interface with a Postgres backend, and only spend rides on it. The chart, HPA and docs describe a horizontally scalable service. This is the root of C-01 and H-10/H-11.

**F2 · Authorisation policy is a hand-maintained table plus two hand-maintained path lists.** `route_authz.go` floors, `skipAuth`, and `isLLMProviderPath` are three independent enumerations that already disagree (H-03, M-05, M-07). The adapter registry knows every route but carries no policy metadata.

**F3 · Validation has two sources of truth and the runtime uses the weaker one.** `schema/mockagents-v1-agent.json` declares bounds; `config/validator.go` enforces a subset; nothing asserts they agree (M-01). MCP and A2A have their own validators with different strictness.

**F4 · Tenancy was retrofitted onto name-keyed structures.** Chaos counters (M-02), pipelines (M-04), the watcher (H-01), and cache invalidation (L-24) still key on name alone.

**F5 · Cross-cutting concerns are copied, not composed.** Six adapter handlers (~400 duplicated lines), four streaming-config resolvers, two SQLite/Postgres implementations that are byte-identical except placeholders, five body decoders, three error envelopes, two ETag contracts, two atomic-write helpers (one buggy). Each copy has already diverged at least once (M-18, M-19, M-10, M-29).

### 3.2 Missing abstractions

| Missing | Evidence of need | Shape |
|---|---|---|
| `adapter.Route{Pattern, Handler, Open bool, Quota bool, Kind}` | H-03, M-07, quota_middleware.go:22-38 | Derive `skipAuth`, `isQuotaPath`, and route floors from one table |
| `engineErrorStatus(err) int` on typed sentinels | M-19 | `engine.ErrAgentNotFound`, `ErrEmptyMessage`, `ErrScenarioRender` |
| `sqldialect{Placeholder(i), IsUnique(err)}` | tenancy store duplication, M-10 | Collapse `store.go` + `postgres_store.go` into one implementation |
| `fsutil.AtomicWrite(path, data)` with fsync | M-29, L-07 | Used by agents, pipelines, cassettes, mcpadmin |
| `validateFaultBounds(cfg)` | M-01, three validators | Shared by agent/MCP/A2A chaos blocks |
| `state.Store` interface with `MaxSessions` | H-06, F1 | Memory impl today; Redis/Postgres later |
| `tenancy.Observer` interface | `SetDenialHook` global, `Recorder.PrincipalFrom` | Replaces the last-writer-wins package global |
| `envBool(name) (bool, error)` | M-35 | One parser for every boolean knob |
| `readBoundedBody(r, max) ([]byte, error)` | five decoders | One 413 path |

### 3.3 Anti-patterns

- **Comments asserting invariants the code no longer has:** `MaxOpenConns(1)` "serialises the pair" (store.go:428-430); "Auth is Bearer-token, not cookie" (middleware.go:114-115); "Hooks must not block" (tenancy/middleware.go:36-38); "Audit log: always enabled" (start.go:229); "the key is piped in from a Secret" (start.go:518). Each is now a bug report.
- **Fail-open defaults on security knobs:** unparsable quota → unlimited; unparsable `MULTI_TENANT` → auth off; unknown model → $0.
- **Snapshot tests that pin policy:** `TestManagementRouteFloors_Snapshot` enshrines `DELETE /logs: roleOpen`.
- **God files:** `realtime/session.go` (2,072), `tenancy/store.go` (909), `server/log_handlers.go` (861), `adapter/responses.go` (811), `server/server.go` (766), `engine.go` `ProcessRequestContext` (290 lines with a 145-line closure).

### 3.4 Recommended redesigns

**R1 · Route policy table (1 week).**
```
adapter.Registry ──► []Route{Pattern, Handler, Policy{Open, Quota, MinRole}}
                          │
        server.registerRoutes ─┬─► mux.Handle
                               ├─► skipAuth   = Policy.Open
                               ├─► isQuotaPath = Policy.Quota
                               └─► RouteAuthz  = Policy.MinRole
```
Delete `skipAuth`, `isLLMProviderPath` and the literal floor map; one table test asserts every route has a policy.

**R2 · State backend seam (1 month).**
```
                 ┌─ state.Store (sessions)   ─┬─ memory (default, bounded)
engine ──────────┤                            └─ redis / postgres
                 └─ registry (agents)        ─── memory + file, or shared via tenancy DSN
server ──────────┬─ storage.Store (logs)    ─┬─ sqlite
                 │                            └─ postgres (new)
                 ├─ audit.Store              ─┬─ sqlite
                 │                            └─ postgres (new)
                 └─ quota.Enforcer           ─── buckets on the tenancy store (UPDATE … RETURNING)
```
Until this lands, the chart must refuse `replicas>1` without a DSN.

**R3 · Adapter dispatch seam (2 weeks).**
```go
type provider interface {
    decode(r) (engine.Request, error)
    renderError(w, status, kind, msg)
    renderChaos(w, *engine.ChaosError)
    renderStrict(w, *engine.StrictToolError)
    render(w, *engine.Response)
    stream(w, *engine.Response, StreamingConfig) error
}
func dispatch(h *Handler, p provider) http.HandlerFunc   // one copy of the 130-line skeleton
```

**R4 · Shutdown lifecycle (1 week).** `Server` owns watcher, realtime session registry, MCP streams; `BaseContext` cancellable; `draining` flag flips `/ready`; configurable timeout; chart `preStop`.

---

## 4. Scalability & Performance Analysis

Baseline: `docs/benchmarks/latest.md` is checked in; the hot path is in envelope today. The items below are the ones that break the envelope under realistic multi-tenant or load-test traffic.

| # | Bottleneck | Root cause | Optimisation | Expected impact |
|---|---|---|---|---|
| P1 | Memory grows ~1 session/request under default SDK traffic | H-06: anonymous session persisted 30 min | Throwaway session when header absent; `MaxSessions` LRU | Bounded RSS under load tests; removes the only unbounded per-request allocation on the hot path |
| P2 | Unauthenticated traffic saturates CPU | M-09: one bcrypt (~50-80 ms at cost 10) per bad key | 30 s negative cache + per-IP failure bucket | Bad-key cost drops from ~60 ms CPU to a map lookup; a 200 rps attacker no longer starves LLM traffic |
| P3 | 401 path does a durable SQLite write | M-08 | Async bounded audit writer | Removes fsync from the auth edge; audit cannot fill the disk from anonymous traffic |
| P4 | Every request serialises on one write lock | M-20: `GetOrCreate` takes `mu.Lock()` even on hit | RLock fast path | Hit path becomes read-shared; measurable at >4 cores with pinned sessions |
| P5 | Chaos-enabled agents take a process mutex 5× per request | L-16 | Init maps once; `atomic.Pointer` for global policy; per-goroutine `rand/v2` | Removes a global serialisation point for every agent with a `chaos:` block |
| P6 | Any key mutation evicts every tenant's cached principal | L-24 | Reverse index `keyID → cacheKey`, invalidate only affected entries | Eliminates fleet-wide bcrypt spike on rotate/delete |
| P7 | SQLite rotate pins the single connection for the bcrypt duration | M-11 | Hash outside tx | Concurrent `Resolve` misses no longer block ~100 ms per rotation |
| P8 | Audit dashboard scans the whole table per load | M-14 | `(actor_tenant, id DESC)` index | O(log n + k) instead of O(n) |
| P9 | Vector query clones up to 100k metadata maps under RLock | M-21 | Clone after top-k | Allocation proportional to `TopK` (≤1000) instead of collection size |
| P10 | Streaming tool arguments cannot be cancelled | M-17 | `sleepCtx` | Disconnected clients release the goroutine immediately instead of after `chunks × delay` |
| P11 | Recording append is O(n²) over a session and unlocked | M-26 | `O_APPEND` one line, single writer | Constant per append; no lost interactions |
| P12 | Cost extraction parses up to 1 MiB body twice on the request goroutine | L-11 | Stamp usage on `RequestMeta` in the adapter | One parse, off the request path |
| P13 | `GET /agents/{name}` does file I/O + 3 YAML passes | L-01 | `(mtime,size)` revision cache | Read endpoint becomes in-memory |

Chunker fix for H-07 (keeps original separators):
```go
func (c *Chunker) Chunk(content string) []string {
    var chunks []string
    start, words := 0, 0
    for i := 0; i < len(content); i++ {
        atWordStart := !unicode.IsSpace(rune(content[i])) && (i == 0 || unicode.IsSpace(rune(content[i-1])))
        if atWordStart {
            if words == c.ChunkSize { chunks = append(chunks, content[start:i]); start, words = i, 0 }
            words++
        }
    }
    return append(chunks, content[start:])
}
```

Horizontal scaling limit today: 1 replica for anything except stateless single-tenant mocking from a read-only ConfigMap. See C-01 and R2.

---

## 5. Reliability & Production Hardening

### 5.1 Failure modes observed

| Trigger | Behaviour today | Reference |
|---|---|---|
| Editor saves `chaos.errors.status_code: 42` | Every request to that agent panics `WriteHeader`; connection dropped | M-01 |
| Tenant deletes its `foo` with `--watch` on | Global and all other tenants' `foo` disappear | H-01 |
| `helm install` with defaults, then `MOCKAGENTS_MULTI_TENANT=1` | CrashLoopBackOff (tenancy DB unwritable) | H-10 |
| HPA scales to 2 | Half of control-plane calls 401; two platform keys in logs | C-01 |
| Rolling update with an open MCP event stream or Realtime socket | Full 5 s timeout, exit code 2, `/ready` stays 200 during drain | M-34 |
| Upstream returns an HTML 502 during `record` | Every later recording fails for the process lifetime | M-28 |
| `record` in a container with tmpfs `/tmp` | Every cassette write fails with EXDEV | M-29 |
| Load test without `X-Session-Id` | RSS grows until the 30-min sweep | H-06 |
| Stream longer than 60 s | Cut mid-frame with no terminal event | M-16 |
| `MOCKAGENTS_DEFAULT_RATE_PER_SEC=10rps` | Rate limiting silently off | M-35 |
| `MOCKAGENTS_MULTI_TENANT=true` | Control plane runs with auth off | M-35 |
| Lost platform key | No recovery path except DB surgery | H-13 |
| Disk full / SQLite locked on audit write | Silently lost, no log line | L-32 |

### 5.2 Resilience gaps

- **No retry/backoff anywhere** in the server (acceptable: it has no upstream dependencies except the recording proxy, which needs per-phase timeouts, M-33).
- **No circuit breaker** around Postgres tenancy; a slow DB stalls every cache-miss auth. A bounded semaphore plus the 5 s spend cache is the right shape; add a `store_latency` readiness signal.
- **Input validation** is strong at the wire (body caps, strict YAML fields, ETags) and weak at the semantic layer (M-01).
- **Configuration management** is env-only with inconsistent parsing; no single reference; no fail-closed behaviour on security switches.
- **Environment separation**: single-tenant is explicitly local-dev, but the container and chart default to it on 0.0.0.0 (M-36).
- **Deployment safety**: no release gate beyond `go test -race`; no signing; `:latest` never published (M-37).
- **Graceful degradation** exists for the log store (warn and continue) but not for the audit store in multi-tenant mode, where silent absence is the wrong default (H-10).
- **Disaster recovery**: no backup/restore procedure for three SQLite files; no platform-key recovery (H-13).

### 5.3 Production readiness score

| Dimension | Score /10 | Basis |
|---|---|---|
| Core engine correctness | 8 | Solid, well-tested; two streaming fidelity bugs (H-07, M-18) |
| Security | 5 | Fundamentals right; auth floors, CORS, CSWSH, OIDC email, log-key exposure open |
| Multi-tenancy | 4 | Onboarding dead-end, ten surfaces fail closed, four name-keyed leaks |
| Deployment topology | 3 | Single-process design shipped with HPA and read-only mounts |
| Observability | 4 | Metrics good; tracing fictional; audit fail-silent; no runbook |
| Test quality | 7 | 1,700+ tests, conformance suites; tenancy at 52%, several bugs in untested branches |
| Supply chain / CI | 4 | Real GUI gate pending; no vuln scan, no pinning, no signing |
| **Overall** | **5.2** | Excellent as a single-process developer tool; not yet deployable as a shared multi-tenant service |

### 5.4 Required before "stable"

1. C-01 chart guard + bootstrap key handling (H-09).
2. H-01, H-02, H-03, H-04, H-05: the multi-tenant correctness set.
3. M-01 chaos bounds validation.
4. H-06 session bounds; H-07 chunker.
5. M-34 shutdown lifecycle; M-16 stream deadlines.
6. M-35 fail-closed env parsing.
7. H-08 wire tracing or remove the docs.
8. M-37 govulncheck + SHA pins + signing.
9. H-13 operations guide.

---

## 6. Test Coverage & Quality

### 6.1 Measured coverage (working tree)

| Package | Coverage | Note |
|---|---|---|
| internal/oidcauth | 0.0% | Claim parsing untested; H-05 invisible |
| internal/types | 0.0% | Enum ↔ JSON schema sync asserted nowhere |
| cmd/mockagents | 32.2% | Env parsing, bootstrap, shutdown paths |
| internal/tenancy | 52.7% | Lowest of any security package |
| internal/observability | 60.0% | Never wired, so end-to-end untested |
| internal/vector | 64.1% | |
| internal/config | 77.5% | Chaos bounds untested |
| internal/engine | 77.8% | Session bounds untested |
| internal/server | 81.7% | Watcher + write API interaction untested |
| internal/adapter | 82.7% | |
| everything else | 84-98% | |

### 6.2 Risky untested paths (each maps to a finding above)

- Watcher running alongside `CreateAgent`/`DeleteAgent` across tenant buckets (H-01).
- Platform principal creating a tenant then minting a key in it (H-02); existing test places the principal inside the target tenant.
- Anonymous reachability of every adapter route in multi-tenant mode (H-03).
- Realtime handshake with a foreign `Origin` plus a session cookie (H-04).
- `validateChaos` with `status_code` outside 400-599, `timeout_ms` above the cap, `window_ms: 0` (M-01).
- Chunker input containing `\n`, tabs, double spaces (H-07).
- Ceiling on `state.MemoryStore.Count()` and `len(Messages)` (H-06).
- `UpdateAPIKeyRole` concurrent with `DeleteAPIKey` (M-10); bulk rotate concurrent with single rotate (L-23).
- Two `quota.Enforcer`s sharing one `SpendBackend` after `SetOverride` (M-12).
- Streaming vs non-streaming `output_tokens` parity (M-18); refusal + content both set (L-14).
- `Reload ‖ Delete` and `Reload ‖ conditional PUT` races (M-03).
- Streaming against `WriteTimeout=200ms` (M-16); ctx cancel inside tool-argument streaming (M-17).
- Non-JSON body through `recording.Proxy`; concurrent `Append` then `Load`; stream > 60 s (M-28, M-26, M-33).
- `bidirectional.Subscribe` steal with a buffered message (M-27).
- Streamable MCP session expiry; oversized body on `/mcp/response` (M-30, M-24).
- `--watch` with a nested agent file (M-31).
- Zero-value `server.Config{}` (L-03); `writeJSON` encode failure (L-04).
- Audit `Since` at sub-second boundaries (L-22); `Recorder` append failure reporting (L-32).
- GUI: `lib/auth.ts` cookie flags and `?next=` validation; SSE proxy route; `LogsConsole` live toggle/reconnect; no Playwright run under `MOCKAGENTS_MULTI_TENANT=1`.
- SDKs: TS CRLF and >30 s stream; Python `chat(stream=True)` on 4xx; Go multi-turn assertion.

### 6.3 Recommended test plan

**Tier 1, policy invariants (table tests, cheap, highest leverage)**
1. Every adapter route has a `Policy`; every open route is reachable anonymously in multi-tenant mode; every management route's floor is asserted negatively (viewer cannot do editor actions).
2. JSON schema enums and numeric bounds equal the Go validator's (`types.ChaosPresets`, `StrictToolLevels`, chaos bounds).
3. Every `MOCKAGENTS_*` var read in code appears in the docs reference page (drive from a generated list).

**Tier 2, tenancy and concurrency (raise tenancy to ≥80%)**
4. Conformance suite additions: role change ‖ delete, rotate ‖ bulk rotate, session expiry sweep, user revocation once L-25 lands. Run against both backends.
5. Watcher + write API scenario from H-01.
6. Platform onboarding scenario from H-02.

**Tier 3, streaming fidelity**
7. For each provider: concatenated deltas == non-stream body byte-for-byte on a fixture with markdown, code fence, tabs, double spaces.
8. Usage parity stream vs non-stream per provider.
9. Stream survives `WriteTimeout` shorter than the stream; cancels on client disconnect during argument deltas.

**Tier 4, operational**
10. `helm lint` + `helm template` + `kubeconform` job; assert the `replicas>1` guard fails without DSN.
11. Shutdown test: open SSE + WS, send SIGTERM, assert `/ready` 503 within 100 ms and exit 0 within the grace.
12. OTLP stdout exporter test through the HTTP middleware with an incoming `traceparent`.
13. `go test -race` on Linux is already in the pending CI diff; keep it, and add `govulncheck`.

**Tier 5, GUI/SDK**
14. Vitest: `auth.ts` cookie flags and `next` guard table; SSE proxy route with a fake upstream; `LogsConsole` live toggle.
15. Playwright multi-tenant profile: login, role gating, bearer SSE proxy, conditional save 412 path.
16. SDK fixtures: CRLF SSE, `data:` without space, multi-line data, long stream, multi-turn tool-call assertions in all three languages.

---

## 7. Final Recommendations

### 7.1 Top 10 changes

1. **Route policy table** (R1): one source for open/quota/floor; fixes H-03, M-05, M-07 and prevents the next drift.
2. **Chart replica guard + `MOCKAGENTS_BOOTSTRAP_KEY` + writable data volume** (C-01, H-09, H-10, H-11): makes the chart honest about what it can run.
3. **Tenant-scoped watcher removal and platform tenant bypass** (H-01, H-02): the two bugs that make multi-tenant mode unusable or destructive.
4. **Realtime origin check and `email_verified`** (H-04, H-05): the two cross-tenant access paths.
5. **Chaos bounds validation** (M-01): stops an editor from taking down an agent with one integer.
6. **Session store bounds and throwaway anonymous sessions** (H-06): the only unbounded per-request allocation.
7. **Chunker separator fidelity and usage parity** (H-07, M-18): streaming must equal non-streaming.
8. **Fail-closed env parsing with one boolean parser** (M-35): `MULTI_TENANT=true` must not mean "auth off".
9. **Shutdown lifecycle** (M-34, M-16): `BaseContext`, draining readiness, configurable timeout, per-chunk write deadlines.
10. **Wire tracing, async audit, operations guide** (H-08, M-08, H-13): make the observability the docs already describe real.

### 7.2 Roadmap

**Week 1 (stop the bleeding; all small, all testable)**
- H-01 watcher `RemoveForTenant`; H-02 platform bypass; M-05 `DELETE /logs` admin floor; H-04 `OriginPatterns`; H-05 `email_verified`; M-01 chaos bounds + clamp; M-35 env parsing; H-09 stop printing the key + implement `BOOTSTRAP_KEY`; H-12 `next ^15.5.21`; M-14 audit index; M-27 `Retry-After` ceil; M-29 temp-dir fix; C-01 chart `fail` guard. Commit the tidy `go.sum`. Add the Tier 1 table tests.

**Month 1 (structural fixes and CI)**
- R1 route policy table (H-03, M-07). H-06 session bounds. H-07 chunker + Tier 3 parity tests. M-08 async audit + retention. M-09 negative cache + limiter. M-10/M-11 SQLite tx fixes and the `sqldialect` collapse. M-16/M-17/M-34 stream deadlines and shutdown lifecycle. M-06 `--cors-origins`. M-22/M-23 bind defaults. M-28/M-26 cassette fixes. H-08 tracing wired with propagation. M-37 SHA pins, Dependabot, govulncheck, golangci-lint, cosign, `:latest`. H-10/H-11 chart volumes. H-13 operations guide plus an env-var reference page. Raise tenancy coverage to 80%. Cut v0.5.0 across all packages.

**Quarter 1 (make the architecture match the deployment story)**
- R2 state backend seam: Postgres backends for audit and interaction logs; shared rate buckets; bounded `state.Store` interface; then lift the replica guard. R3 adapter dispatch seam (removes ~400 lines and M-19). M-04 tenant-scoped pipelines. L-25 user revocation and session GC. M-12 override propagation. Per-session MCP state (L-40) and streamable expiry (M-30). Playwright multi-tenant profile. Postgres tenancy conformance in every CI run. Re-baseline `make bench-report` after P1-P7 and confirm the hot path stays inside the envelope.
