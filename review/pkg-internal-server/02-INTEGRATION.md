# Cross-File Integration Findings (Pass 2) — `internal/server`

## Relationship / blast-radius map

```
                    cmd/mockagents/start.go
                       │ builds + Shutdown(); defer logStore.Close(); defer watcher.Stop()
                       ▼
   ┌──────────────────────────── server.go ────────────────────────────┐
   │  route wiring + middleware chain + lifecycle (Shutdown)            │
   │   mux: /v1/chat/completions, /v1/messages   → adapter (openai/anthropic)
   │        /api/v1/agents/{name}/reload         → handlers.ReloadAgent  (NO RequireRole)
   │        /api/v1/tenants*, /api/v1/keys*      → tenancy_handlers  (RequireRole only)
   │        /api/v1/audit  (admin) /costs (none) /pipelines (none) /config/validate (editor)
   │        /api/v1/logs*, /api/v1/logs/stream   → log_handlers
   └───────┬───────────────┬──────────────────┬───────────────┬─────────┘
           │ middleware.go  │ InteractionCapture│ StreamLogs    │
           │ Auth/CORS/log  │ (log_handlers)    │ (log_handlers)│
           ▼                ▼                   ▼               ▼
   tenancy.RequireRole   log_worker.Submit → store.Log    log_broadcaster.Subscribe/Publish
   (role level only,     (async pool;        (SQLite)     (SSE fan-out; per-sub buffer)
    NO tenant scope)      Shutdown drains)
           │
           ▼
   tenancy.Store (keys/tenants keyed by GLOBAL id — no (tenant,id) scoping)
```

Shared contracts crossing the seams: `Principal{TenantID,KeyID,Role}` (tenancy/types), the `{"error":…}` JSON envelope, the `text/event-stream` content-type, `storage.InteractionLog`, the `LogWorker`/`LogBroadcaster` lifecycle vs `storage.Close`.

## Findings

| ID | Sev | Conf | Pri | Effort | Related sites | Check | Evidence → Fix |
|----|-----|------|-----|--------|---------------|-------|----------------|
| **X-SEC-001** | **S0** | High | P0 | M | `tenancy/middleware.go:111` (`RequireRole`), `tenancy_handlers.go:54/69/88/126/161/203/349`, `tenancy/store.go` (keys by global id), `tenancy/types.go:83` (`Principal.TenantID`), `cmd/start.go:295` (admin minted into a tenant) | Authz / data flow | **Cross-tenant IDOR.** `RequireRole` enforces only the role *level*; no layer compares the request's `{id}`/`{tenantID}` path param to `PrincipalFrom(ctx).TenantID`, and the store resolves keys by global id. Since admin keys are *per-tenant* and `CreateTenant` lets admins spawn tenants, a tenant-A admin can list/create/rotate/delete tenant-B keys (`F-TN-001..005`) and bulk-rotate B. No `WithPrincipalTenantScope` exists (verified). **Fix:** enforce tenant ownership — preferred: change the affected store methods to take `(tenantID, id)` and scope the WHERE clause; or add a handler-level guard loading the target's `tenant_id` and asserting `== principal.TenantID` (return 404, not 403, on mismatch). The `/keys/me/*` routes already derive from the principal and are fine. |
| **X-DOS-001** | S1 | High | P1 | S | `validate_handler.go:41`, `tenancy_handlers.go:38/90/128`, (no body-cap middleware in `server.go`) | Security / shared gap | **No request-body size cap anywhere.** `validate_handler` `io.ReadAll`s and the tenancy decoders `json.Decode` unbounded. One large POST OOMs the process. **Fix:** a server-wide `http.MaxBytesReader` middleware (or per-handler caps), plus confirm `config.ValidateBytes`'s YAML parser bounds alias expansion (billion-laughs). |
| **X-AUTHZ-001** | S2 | High | P2 | S | `server.go:191` (reload, none), `:281` (costs, none), `:291-292` (pipelines, none) vs `:230` (audit, admin), `:301` (validate, editor), `:196-221` (tenancy) | Interface/contract drift | **Route-authz policy is ad-hoc per registration**, which is exactly how `F-HD-001` (ReloadAgent ungated) slipped in. **Fix:** define the role floor for every `/api/v1/*` route in one table and assert it; decide the intended floor for costs/pipelines (likely `viewer`) and reload (`editor`). |
| **X-SHUT-001** | S2 | Med | P2 | M | `server.go:442-453` (Shutdown drains logWorker), `log_worker.go:130-143` (drain bounds *blocking* only, workers keep writing), `cmd/start.go:132` (`defer logStore.Close()`) | Lifecycle/concurrency | **Use-after-close window.** `logWorker.Shutdown(timeout)` only bounds how long it *blocks*; on timeout, worker goroutines may still call `store.Log` while `logStore.Close()` runs → write on a closed DB. Also `logBroadcaster` is never closed (`F-SV-001`). **Fix:** give workers a cancel ctx so they actually stop at the deadline, and order `logStore.Close()` strictly after the worker pool has fully stopped (not merely after Shutdown's timeout). |
| **X-ERR-001** | S2 | High | P2 | S | `tenancy/middleware.go:130-140` (`{"error":{"type","message"}}`), `tenancy_handlers.go`/`costs`/`audit`/`validate` (`{"error":"string"}`) | Contract drift | **Two error-envelope shapes** depending on whether the failure is middleware-auth (nested object) or handler-level (flat string). API/GUI consumers must special-case both. **Fix:** pick one envelope and use it everywhere (a shared `writeError` helper). |
| **X-LIMIT-001** | S2 | Med | P2 | S | `costs_handler.go:79-81` (clamp 10000), `audit_handlers.go:49-58` (no clamp), `log_handlers.go:76-85` (no clamp) | Duplication/divergence | **Inconsistent `limit` clamping** across list endpoints — costs clamps, audit/logs don't (→ `F-LH-002`, `F-AU-003`). **Fix:** one shared `parseLimit(r, def, max)` helper. |
| **X-SEC-002** | S2 | Med | P2 | M | `audit_handlers.go:60`, `audit` pkg (`Query` has no `TenantID`) | Data flow / isolation | **Audit is not tenant-scoped.** The admin-only gate is the sole isolation, so a tenant-A admin reads tenant-B audit events. Tied to `X-SEC-001` (per-tenant admins). **Fix:** add a tenant marker to audit events + filter by principal tenant (or document that audit read is a global-admin-only capability and gate it accordingly). |
| **X-SSE-001** | S3 | Med | P3 | S | `log_handlers.go:286` (exact `== "text/event-stream"`), `adapter`/`streaming` (sets the header) | Data flow | The capture writer's exact content-type match (`F-LH-004`) breaks if the adapter/streaming layer ever adds `; charset=utf-8`. Pin the adapter's value or use a prefix match. |
| **X-TEST-001** | S2 | High | P2 | M | `server_test.go` (no RBAC wiring test), `log_handlers` (no StreamLogs test), `tenancy_handlers` (no IDOR/CRUD tests), `costs`/`audit` (no tests) | Integration test gaps | The **wired-together** auth/RBAC path, the SSE stream, and the management CRUD/IDOR seams are untested end-to-end. A regression in any cross-file contract above would ship silently. **Fix:** add server-level wiring tests (per-role 401/403/200), an SSE stream test, and IDOR tests (which must fail until `X-SEC-001` is fixed). |
| X-LIFE-001 | — | High | — | — | `cmd/start.go:204` (`watcher.Stop()`), `watcher.go:97` | Lifecycle (resolved) | Verified `watcher.Stop()` **is** called on shutdown — so `F-WT-001`'s reload-after-Stop only fires during process exit (benign today). Recorded so the reader knows it was checked; the fix still matters for any future mid-life Stop. |

## Checks performed (for auditability)

- [x] **1. Signature & call-contract consistency** — clean (no changed signatures in scope; route→handler arity matches).
- [x] **2. Interface ↔ implementation** — `X-AUTHZ-001`: route-gating "interface" applied inconsistently. `LogBroadcaster`/`LogWorker` satisfy their handler-side consumers.
- [x] **3. Data flow across boundaries** — `X-SEC-001` (tenant id not enforced on `{id}`), `X-SEC-002` (audit tenant), `X-SSE-001` (content-type).
- [x] **4. Layering & import direction** — clean: `server` imports engine/tenancy/storage/etc. (allowed; it's the top layer). No cycle; no `engine→tenancy` violation introduced here.
- [x] **5. Duplication & divergence** — `X-ERR-001` (error envelope), `X-LIMIT-001` (limit clamp), ad-hoc `{"error":…}` bodies repeated across handlers.
- [x] **6. Contract/schema/wire drift** — `F-AU-004` (audit kind list vs CLAUDE.md), `X-ERR-001`.
- [x] **7. Concurrency & shared state across files** — `F-LW-001` (send-on-closed), `X-SHUT-001` (worker vs store close), `F-WT-001` (timer vs Stop), `F-SV-007` (global DenialHook). Broadcaster paths verified correct (`F-LB-002/003`).
- [x] **8. Error propagation across layers** — `F-SV-002/003` (`==` vs `errors.Is`), `F-TN-006` (raw errors leaked), `F-TN-008` (4xx/5xx conflation).
- [x] **9. Test integration gaps** — `X-TEST-001`.
- [x] **10. Lifecycle & ordering** — `X-SHUT-001`, `X-LIFE-001` (watcher.Stop wired), `F-SV-001` (broadcaster not closed).
