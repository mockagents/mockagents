# Action Plan — `internal/server`

_Execute top-down. Each task maps to a finding ID (detail in `01-PER-FILE.md` / `02-INTEGRATION.md`). Check off when its "Done when" holds. Self-contained — you don't need the other reports to act._

**Gate:** the **P0** box must be checked before the next **multi-tenant** release. (Single-tenant / LLM-mock usage is unaffected by P0.)

## P0 — Blockers (gate the multi-tenant release)

- [x] **Enforce tenant ownership on the management API (cross-tenant IDOR)** — `X-SEC-001` / `F-TN-001..005` · effort:M · owner:claude · **DONE 2026-06-02**
  - **Fix shipped (store-level scoping + handler ownership gate):** `tenancy/store.go` `UpdateAPIKeyRole`/`RotateAPIKey`/`DeleteAPIKey` now take `tenantID` and add `AND tenant_id = ?` to their SELECT/UPDATE/DELETE — a cross-tenant key id affects 0 rows → `ErrNotFound` → 404, atomically (no TOCTOU). The tenant-addressed handlers (`ListAPIKeys`/`CreateAPIKey`/`BulkRotateTenantKeys`) gate on a new `ensureOwnTenant` helper that 404s when the path `{id}` ≠ `principal.TenantID`. The key-addressed handlers pass `principal.TenantID`; the self-service `/keys/me/*` handlers pass `(principal.TenantID, principal.KeyID)` (unchanged behavior, now explicitly scoped). Store-level scoping is defense-in-depth — no future handler can forget the check.
  - **Done when:** ✅ `TestTenancyHandlers_CrossTenantIDOR_Returns404` — a tenant-A admin's rotate/delete/demote/list/create/bulk-rotate of tenant-B resources all return 404 and B's key still resolves with its original role + count. **Regression-verified:** neutering the `ensureOwnTenant` guard + one store scope makes 5/6 cross-tenant ops succeed (200/201) and rotates B's key. Updated existing handler tests to inject an authenticated principal (`servePrincipal` helper). Full `go test ./...` green (19 pkgs); build/gofmt/vet clean. On branch `fix/sec-001-tenant-idor`.
  - **Note:** tenant-level CRUD (`CreateTenant`/`ListTenants`/`DeleteTenant`) remains role-gated but NOT super-admin-scoped — a per-tenant admin can still list/create/delete tenants. That's a separate "no super-admin tier" design gap (`X-SEC-002` territory), out of scope for this key-IDOR fix; flagged for a follow-up.

## P1 — High (this cycle)

- [ ] **Cap request body sizes** — `X-DOS-001` / `F-VL-001` / `F-TN-007` · effort:S · owner:unassigned
  - **Where:** `validate_handler.go:41` (`io.ReadAll`), `tenancy_handlers.go:38/90/128` (`json.Decode`); no body-cap middleware in `server.go`.
  - **Problem:** Unbounded `ReadAll`/`Decode` → a single large POST OOMs the process (reachable by editor/admin). Possible YAML alias-bomb via `ValidateBytes`.
  - **Fix:** add an `http.MaxBytesReader(w, r.Body, N)` wrapper — ideally a small server-wide middleware applied to all POST/PATCH routes, with a sane cap (e.g. 1 MiB for config, smaller for JSON control-plane). Confirm `config.ValidateBytes`'s YAML decoder bounds nesting/alias expansion.
  - **Done when:** a `>N` body returns `413 Request Entity Too Large` (or 400) without large allocation; test `TestValidate_OversizedBody_Rejected` passes.

- [ ] **Role-gate `ReloadAgent`** — `F-HD-001` (+ `F-HD-002` tenant-isolation) · effort:S · owner:unassigned
  - **Where:** route `server.go:191`; handler `handlers.go:99-153`.
  - **Problem:** `POST /api/v1/agents/{name}/reload` (disk + registry mutation) is mounted with no `RequireRole` — a `viewer` can reload (multi-tenant), unauthenticated (single-tenant). The reload also selects the on-disk file by name only, ignoring tenant (`F-HD-002`).
  - **Fix:** wrap with `tenancy.RequireRole(tenancy.RoleEditor, …)` (matching `/config/validate`); in the handler, verify the loaded definition's `Metadata.TenantID` matches the existing agent's before `Register`.
  - **Done when:** a viewer/anonymous reload returns 403/401; a cross-tenant same-name reload does not overwrite the other tenant's agent; tests `TestReloadAgent_RequiresEditor` + `TestReloadAgent_TenantIsolated` pass.

- [ ] **Fix the async-log-worker send-on-closed-channel race** — `F-LW-001` (+ `F-LW-004` test) · effort:S · owner:unassigned
  - **Where:** `log_worker.go:106-127` (`Submit`), `:140-143` (`Shutdown`).
  - **Problem:** `Submit` checks `stopped.Load()` then sends on `w.queue`; `Shutdown` sets the flag and `close()`s the queue non-atomically → a concurrent `Submit` can panic ("send on closed channel") and crash the process. (`-race` is unavailable here, so the logic fix is the only guard.)
  - **Fix:** serialize the stopped-check+send against the close — e.g. `Submit` takes a `sync.RWMutex.RLock` around the check+send and `Shutdown` takes the `Lock` before closing; or never close `queue` and stop workers via a separate `done` channel + drain.
  - **Done when:** a stress test that hammers `Submit` from N goroutines while calling `Shutdown` never panics (run it 1000×); `TestLogWorker_SubmitDuringShutdown` passes.

## P2 — Medium (schedule)

- [ ] **Unify route authorization policy** — `X-AUTHZ-001` / `F-CO-005` / `F-PL-001` · effort:S — define the role floor for every `/api/v1/*` route in one place; decide costs/pipelines (likely `viewer`) and reload (`editor`). **Done when:** a single table/test asserts the floor per route.
- [ ] **Shutdown ordering: stop workers before closing the store; close the broadcaster** — `X-SHUT-001` / `F-LW-002` / `F-SV-001` · effort:M — give workers a cancel ctx so they actually stop at the drain deadline; order `logStore.Close()` after the pool is fully stopped; add `logBroadcaster.Close()` to `Server.Shutdown`. **Done when:** no `store.Log` call can run after `logStore.Close()`; no SSE goroutine leak on graceful exit.
- [ ] **Single error envelope** — `X-ERR-001` / `F-TN-006` · effort:S — one `writeError` helper; stop returning raw store errors (`err.Error()`) to clients. **Done when:** all `internal/server` handlers + the auth middleware emit the same `{"error":…}` shape and no DB internals leak.
- [ ] **Shared `limit` clamp** — `X-LIMIT-001` / `F-LH-002` / `F-AU-003` · effort:S — one `parseLimit(r, def, max)` used by logs/audit/costs. **Done when:** `?limit=99999999` is clamped on every list endpoint.
- [ ] **SSE capture: skip body buffering + tolerant content-type match** — `F-LH-001` / `F-LH-004` / `X-SSE-001` · effort:S — set `cw.capture=false` once `text/event-stream` is seen; match via prefix/`mime.ParseMediaType`. **Done when:** an SSE response is not buffered into `cw.body` and is labeled streaming even with a charset param.
- [ ] **Audit tenant scoping (or document global-admin-only)** — `X-SEC-002` / `F-AU-005` · effort:M — add a tenant marker + filter, or gate audit read as a global-admin capability. **Done when:** a tenant-A admin cannot read tenant-B audit events (or the global-admin contract is explicit + enforced).
- [ ] **`audit_handlers` nil-store guard** — `F-AU-001` · effort:S — add `if h.Store == nil { 503 }` like its siblings. **Done when:** mounting with a nil store returns 503, not a panic.
- [ ] **Watcher lifecycle hardening** — `F-WT-001` / `F-WT-003` / `F-WT-004` / `F-WT-006` · effort:M — `Stop()` stops pending timers + drains in-flight `reloadFile` via a `closed` flag + `WaitGroup`; guard `Stop()` with `sync.Once`; normalize map keys with `filepath.Clean`. **Done when:** `TestWatcher_StopCancelsPendingReload` passes (write→Stop→no registry mutation after Stop) and double-`Stop` is race-safe.
- [ ] **`errors.Is` for sentinels** — `F-SV-002` / `F-SV-003` / `F-WT-005` · effort:S — replace `==`/substring error checks with `errors.Is` (incl. `isTransientMissing` → `errors.Is(err, fs.ErrNotExist)`). **Done when:** wrapped sentinels route to the correct status / branch.
- [ ] **Status-code correctness in tenancy create paths** — `F-TN-008` / `F-TN-009` · effort:S — map duplicate→409, DB failure→500; assert `len(results)==len(oldPrefixes)` in `BulkRotateTenantKeys`. **Done when:** a duplicate name returns 409 and a slice-length mismatch can't panic.
- [ ] **`SetDenialHook` global-mutation footgun** — `F-SV-007` · effort:S — document single-server-per-process or guard the global. **Done when:** noted/guarded.
- [ ] **CORS + auth-header robustness** — `F-MW-001` / `F-MW-002` · effort:S — make CORS origins configurable; parse the `Bearer` scheme case-insensitively + trim. **Done when:** CORS origin is configurable and a malformed `Authorization` is handled deliberately.
- [ ] **Add the missing handler tests** — `X-TEST-001` / `F-SV-008` / `F-MW-007` / `F-CO-001` / `F-AU-002` / `F-TN-011` / `F-HD-005` / `F-LW-004` · effort:M — server-wiring RBAC tests (per-role 401/403/200), an SSE `StreamLogs` test, costs/audit handler tests, a secret-not-logged test, and the IDOR tests from P0. **Done when:** each listed gap has a test.

## P3 — Low (opportunistic)

- [ ] **Dead/odd code** — `F-LH-009` (remove `init(){_=json.Marshal}`), `F-PL-002` (dead `h==nil`), `F-TN-012` (misleading "secret-wipe" comment) · effort:S.
- [ ] **Doc drift** — `F-AU-004` (audit kind list omits `api_key.rotated`), `F-SV-006` (`Addr()` doc), `F-WT-008` (non-recursive watch), `F-CO-003` (costs `limit` truncates totals) · effort:S.
- [ ] **Input validation parity** — `F-CO-004` / `F-LH-011` (RFC3339-validate `since`/`until` like audit does) · effort:S.
- [ ] **SSE write-timeout** — `F-SV-004` (global `WriteTimeout: 60s` severs SSE) · effort:S — exempt streaming routes. _(Verify whether the streaming framework already resets the deadline before acting.)_
- [ ] **Minor robustness** — `F-VL-002` (defer-close before read), `F-VL-003` (empty JSON wrapper), `F-MW-004/005` (statusWriter interface forwarding + double-WriteHeader guard), `F-LW-003` (`store==nil`→success contract), `F-LH-005/F-LH-007/F-LH-008` · effort:S.

## Workstream clusters (batch these)

- **Tenant-isolation hardening (highest value):** `X-SEC-001` (P0) + `X-SEC-002` + `F-HD-002` — all about scoping a request to the caller's tenant; design the ownership-check pattern once and apply everywhere.
- **Input-limits & error-envelope:** `X-DOS-001` + `X-LIMIT-001` + `X-ERR-001` + `F-TN-006` — one middleware pass for body caps + shared `parseLimit` + shared `writeError`.
- **Lifecycle & concurrency:** `F-LW-001` + `X-SHUT-001` + `F-SV-001` + `F-WT-001/003` — the async-logging and watcher teardown paths.
- **Route-policy unification:** `X-AUTHZ-001` + `F-HD-001` + `F-CO-005` + `F-PL-001` — one role-floor table.

## Needs investigation (low-confidence / dependency-gated)

- [ ] `X-DOS-001` YAML half — confirm `config.ValidateBytes`'s YAML lib bounds alias expansion (billion-laughs); if not, the body cap alone is insufficient. Check `internal/config`.
- [ ] `X-SEC-001` store half — decide whether tenant scoping lives in `tenancy/store.go` signatures or the handler; the store package was not reviewed here.
- [ ] `F-LH-003` SSE cross-tenant interference — confirm whether the broadcaster should grow a per-tenant subscription predicate (touches `log_broadcaster.go`).
