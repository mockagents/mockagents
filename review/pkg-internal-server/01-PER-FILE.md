# Per-File Findings (Pass 1) — `internal/server`

_Each file judged in isolation. Cross-file issues live in `02-INTEGRATION.md`. IDs are stable and referenced by `03-ACTION-PLAN.md`._

## `server.go` · Go, 518 LOC · role: HTTP server, route wiring, lifecycle

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-SV-001 | S2 | Med | :442 | Concurrency/lifecycle | `Shutdown()` drains `logWorker` but never closes `s.logBroadcaster` — SSE subscriber goroutines/channels are only torn down by `httpServer.Shutdown` ctx expiry → potential leak on graceful exit. Add `s.logBroadcaster.Close()` after the HTTP server stops. |
| F-SV-002 | S2 | Med | :318-323 | Correctness | Error comparison uses `==` (`err == engine.ErrEmptyMessage`) instead of `errors.Is`; a wrapped sentinel falls through to 500. Use `errors.Is`. |
| F-SV-003 | S2 | Med | :480-483 | Correctness | `isNotFound` falls back to `strings.Contains(err.Error(), "agent not found")` — fragile substring match maps unrelated errors to 404. Drop once `errors.Is` is used. |
| F-SV-004 | S2 | Med | :174-180 | Concurrency | `http.Server.WriteTimeout: 60s` is set globally but SSE endpoints (`/api/v1/logs/stream`) are long-lived → the write timeout severs live feeds after 60s. Use a per-stream deadline reset or exempt the streaming mux. |
| F-SV-005 | S2 | Med | :307,:514 | Security | `POST /v1/engines/process` is mounted unconditionally and `skipAuth` exempts `/v1/engines/` from tenancy auth — an internal/testing endpoint reachable and auth-exempt on the real listener. Gate behind a config flag or require a key in multi-tenant mode. |
| F-SV-006 | S2 | Med | :466 | API/doc | `Addr()` returns the *configured* addr (e.g. `:0`) while its doc claims it's useful after listen with port 0 — misleading; delegate to `ListenAddr()` or fix the doc. |
| F-SV-007 | S3 | High | :101-108 | Concurrency | `tenancy.SetDenialHook(...)` writes a package-global in `New()`; two `Server`s in one process race/overwrite it. Document single-server-per-process or guard. |
| F-SV-008 | S1 | High | test | Tests | `server_test.go` has **no** wiring test for any tenancy/audit/logs/costs/pipelines/validate route or the RBAC gating — a wrong `RequireRole` arg would not be caught. Add per-tier 401/403/200 wiring tests. |
| F-SV-009 | S3 | Med | :411 | Style | `err != http.ErrServerClosed` → prefer `errors.Is`. (Works today; sentinel returned unwrapped.) |

## `log_handlers.go` · Go, 500 LOC · role: log REST + SSE stream + `InteractionCapture`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-LH-001 | S2 | High | :286,:347,:242 | Perf/correctness | Doc says streaming responses "skip the body buffer," but `captureWriter.Write` buffers every byte up to 1 MiB unconditionally — SSE chunks are pinned in `cw.body`, contradicting the comment and the perf intent. Set `cw.capture = false` once `Content-Type: text/event-stream` is observed. |
| F-LH-002 | S2 | High | :76-85 | Security/DoS | `?limit=` is rejected for `<1` but has **no upper bound** → `make([]LogWithCost, len(logs))` allocates per caller-controlled size. Clamp to a max. |
| F-LH-003 | S2 | High | :484-486 | Tenant isolation | SSE tenant filtering is post-subscribe client-side: every subscriber receives every tenant's row and the broadcaster's drop/backpressure accounting is shared, so a noisy tenant degrades another's stream. Subscribe with a tenant predicate or document. |
| F-LH-004 | S2 | Med | :286 | Correctness | `cw.Header().Get("Content-Type") == "text/event-stream"` exact-match fails on `text/event-stream; charset=utf-8`. Use `strings.HasPrefix` / `mime.ParseMediaType`. |
| F-LH-005 | S2 | Med | :313 | Error handling | `worker.Submit(entry)` return ignored — silent log loss under overflow with no metric/log. Bump a counter or log at debug. |
| F-LH-006 | S2 | High | :375-379 | Correctness | `captureWriter` implements `Flush` but not `http.Hijacker`/`io.ReaderFrom`/`http.Pusher` — wrapping silently strips those interfaces. SSE works (Flush kept); document or forward the rest. |
| F-LH-007 | S3 | Med | :310-312 | Correctness | `entry.SessionID` is overwritten with the request-id; the `session_id` ListLogs filter (:67) then filters on a column carrying request-ids. Reconcile the field's meaning. |
| F-LH-008 | S3 | Med | :291-297 | Perf | Body-probe `json.Unmarshal(bodySnapshot, &probe)` re-parses the whole (≤1 MiB) snapshot just to read `model` when meta is empty. Use a streaming decoder/early-out. |
| F-LH-009 | S3 | High | :381-384 | Readability | `func init() { _ = json.Marshal }` "ensure import used" is dead/misleading — `json` is used legitimately. Remove. |
| F-LH-010 | — | — | :440-441 | Concurrency (positive) | SSE client-disconnect cleanup is **correct**: `defer cancel()` + `ctx.Done()` arm + channel-close arm all exit cleanly. No goroutine leak. |
| F-LH-011 | S3 | Med | :65-70 | Input trust | `Since`/`Until` filter strings passed through unvalidated (no RFC3339 check) — behavior depends entirely on the store. Validate + bound length. |

## `tenancy_handlers.go` · Go, 360 LOC · role: management API (tenants + API keys, RBAC) — **security-critical**

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-TN-001 | S0 | High | :125-154 | Security/IDOR | `UpdateAPIKeyRole` mutates the key by global `{id}` with no check that the key's `tenant_id` matches the caller's `Principal.TenantID`. **See `X-SEC-001`.** |
| F-TN-002 | S0 | High | :160-180 | Security/IDOR | `RotateAPIKey` — same: rotates any tenant's key by `{id}`. **`X-SEC-001`.** |
| F-TN-003 | S0 | High | :348-360 | Security/IDOR | `DeleteAPIKey` — deletes any tenant's key by `{id}`. **`X-SEC-001`.** |
| F-TN-004 | S0 | High | :68-114 | Security/IDOR | `ListAPIKeys` / `CreateAPIKey` operate on the path `{id}` tenant with no ownership check — read/mint credentials in another tenant. **`X-SEC-001`.** |
| F-TN-005 | S0 | High | :202-243 | Security/IDOR | `BulkRotateTenantKeys` rotates *every* key of the path-supplied tenant — cross-tenant mass credential invalidation (DoS). The `?except=self` derives `KeyID` from context (good), but the target tenant is attacker-controlled. **`X-SEC-001`.** |
| F-TN-006 | S2 | Med | :24,:60,:168,:221,:274,:322,:355 | Error/info-leak | Raw store errors returned verbatim: `{"error": err.Error()}` leaks DB/driver internals. Log server-side, return a generic message. |
| F-TN-007 | S2 | Med | :38,:90,:128 | Security/DoS | `json.NewDecoder(r.Body).Decode` with no `MaxBytesReader` and no `DisallowUnknownFields` — unbounded body + silent field typos. **See `X-DOS-001`.** |
| F-TN-008 | S2 | Med | :42-46,:104,:136-144 | Status codes | All `CreateTenant`/`CreateAPIKey`/`UpdateAPIKeyRole` non-handled errors map to 400, conflating duplicate-name (→409) and DB failures (→500) with bad input. Branch on a conflict sentinel; default to 500. |
| F-TN-009 | S2 | Med | :228-238 | Panic risk | `BulkRotateTenantKeys` indexes `oldPrefixes[i]` in lockstep with `results` with no length assertion → panic (500 mid-loop after partial audit) if the store returns mismatched slices. Guard lengths or return paired structs. |
| F-TN-010 | — | High | :94-97,:132-135 | Validation (positive) | Role is validated via `req.Role.IsValid()` before write; request structs expose no `TenantID`/`ID` → no mass-assignment. Adequate. |
| F-TN-011 | S2 | High | test | Tests | No test asserts tenant-ownership (the IDOR cases — none exists in code), invalid-role→400, decode-error→400, or "list never returns plaintext"; several handlers untested entirely. Add cross-tenant IDOR tests (must currently fail). |
| F-TN-012 | S3 | High | :342-343 | Readability | `BurnMyAPIKey` sets `result.Plaintext = ""` then `result = nil` as a "secret-wipe" gesture that does nothing (Go strings are immutable refs). Clarify the comment; the real work is the store burn. |

## `middleware.go` · Go, 160 LOC · role: auth-extract / logging / CORS / request-id

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-MW-001 | S2 | Med | :64 | Security/CORS | `Access-Control-Allow-Origin: *` unconditional on every route incl. tenant admin — no config gate. Mitigated by Bearer-not-cookie auth (no `Allow-Credentials`), but overly permissive for a control plane. Make origins configurable. |
| F-MW-002 | S2 | Med | :113-118 | Security/parsing | `strings.HasPrefix(auth, "Bearer ")` is case-sensitive/whitespace-strict; `bearer x` or extra spaces → silently anonymous instead of a clean reject. Match scheme case-insensitively + `TrimSpace`. |
| F-MW-003 | — | High | :39-56 | Security (positive) | `StructuredLogger` logs method/path/status/duration/request-id/remote-addr and **not** the `Authorization` header or key — verified no secret leak. |
| F-MW-004 | S2 | Low | :148-152 | Correctness | `statusWriter` implements `Flush` but not `Hijacker`/`ReaderFrom`/`Pusher` — strips them downstream (OK today; SSE needs only Flush). |
| F-MW-005 | S2 | Low | :143-146 | Correctness | `statusWriter.WriteHeader` not guarded against double-call (stdlib "superfluous WriteHeader"). Add a `wroteHeader` guard. |
| F-MW-006 | S3 | Med | :154-159 | Correctness | request-id fallback `req-<unixnano>` on `crypto/rand` failure is non-unique/predictable under concurrency (not a security token; low). |
| F-MW-007 | S1 | High | test | Tests | No test asserts the logger omits `Authorization`/key; `security_test.go:34` *confirms* the permissive `*` CORS rather than guarding a policy. Add a secret-not-logged test + a CORS-policy test. |

## `handlers.go` · Go, 169 LOC · role: OpenAI/Anthropic passthrough + mgmt handlers

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-HD-001 | S1 | High | :99 (route server.go:191) | Security/authz | `ReloadAgent` mounted with **no `RequireRole`** wrapper, unlike every other mutation — a `viewer` can reload (multi-tenant); unauthenticated (single-tenant). Wrap with `RequireRole(RoleEditor, …)`. |
| F-HD-002 | S2 | High | :112-139 | Tenant isolation | Reload selects the on-disk file by `Metadata.Name` only, ignoring tenant; `Register`s whatever `TenantID` is in the YAML → with same-name agents across tenants, can register the wrong tenant's file. Verify `TenantID` matches before registering. |
| F-HD-003 | S2 | Med | :118-153 | Correctness | First-name-match wins silently if `LoadDir` returns duplicate names; combined with F-HD-002, nondeterministic. Detect/reject duplicate-name collisions. |
| F-HD-004 | S2 | Med | :112-115 | Error handling | `LoadDir` errors only `Warn`-logged; a parse failure of the *target* file falls through to a misleading 404 instead of 400. Surface the parse error for the requested file. |
| F-HD-005 | S2 | Med | test | Tests | No test that ReloadAgent rejects an unauthorized/viewer caller or a cross-tenant name collision. |

## `log_worker.go` · Go, 212 LOC · role: bounded async log writer pool

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-LW-001 | S1 | High | :106-127,:140-143 | Concurrency | `Submit` checks `stopped.Load()` then sends on `w.queue`; `Shutdown` does `stopped.Store(true); close(w.queue)` — non-atomic. A `Submit` that passes the check before `close` runs panics ("send on closed channel"). Guard send+close with an `RWMutex` (Submit RLock, Shutdown Lock) or signal workers via a `done` channel instead of closing the queue. |
| F-LW-002 | S2 | Med | :130-143 | Concurrency/contract | Closing the queue drains *all* buffered entries regardless of the drain timeout (workers keep ranging) — so workers may still write to `store` after a timed-out `Shutdown` returns → use-after-close if the caller then closes the store. **See `X-SHUT-001`.** Hard-stop workers via a cancel ctx, or document that the store must outlive the workers. |
| F-LW-003 | S2 | Med | :107-109 | API/contract | `store == nil` short-circuit returns `true` (success) without enqueuing — silently "succeeds" while persisting nothing. Document or treat as a drop. |
| F-LW-004 | S2 | High | test | Tests | The concurrent Submit-vs-Shutdown overlap (the F-LW-001 panic window) is untested — `TestLogWorker_ConcurrentSubmit` joins all submitters *before* Shutdown. Add a stress test that Submits during Shutdown. |
| F-LW-005 | — | High | :118-123 | Correctness (positive) | Counter rollback `submitted.Add(^uint64(0))` on the full-queue path keeps `Written ≤ Submitted` and `attempts = Submitted + Dropped`. Sound. |

## `log_broadcaster.go` · Go, 200 LOC · role: SSE fan-out

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-LB-001 | S2 | High | :99-108 | Perf/concurrency | `Publish` holds `b.mu` across the whole fan-out loop, serializing every worker write against Subscribe/Cancel/Snapshot. Sends are non-blocking so it's short and correct — flag only for scale-out, not a bug. |
| F-LB-002 | — | High | :79-87 | Concurrency (positive) | `cancel` is idempotent + race-free (re-checks map membership under `b.mu` before `delete`+`close`); Publish sends under the same lock so no send-on-closed. The tricky path is correct. |
| F-LB-003 | — | High | :50-52,:104 | Concurrency (positive) | `dropped` is `atomic.Uint64`, incremented under lock, read lock-free — monotonic, correct. |

## `costs_handler.go` · Go, 171 LOC · role: `GET /api/v1/costs`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-CO-001 | S2 | Med | test | Tests | No `costs_handler_test.go` — aggregation math, `(unknown)` keys, limit clamp, tenant filter, nil-store 503 all untested. |
| F-CO-002 | S2 | Med | :85-102 | Perf | Full bounded scan (≤10000 rows) + per-row `pricing.ExtractUsage([]byte(row.ResponseBody))` JSON re-parse per request — push aggregation into the store if hit often. |
| F-CO-003 | S3 | Med | :71-83 | API | `limit` caps rows *scanned*, so totals are silently partial beyond it with no `truncated` flag. Document or signal. |
| F-CO-004 | S3 | Low | :62-64 | Input trust | `since`/`until` not RFC3339-validated (audit *does* validate) — inconsistent. |
| F-CO-005 | S3 | Low | route server.go:281 | Authz | No `RequireRole` on `/api/v1/costs` even in multi-tenant mode. **See `X-AUTHZ-001`.** |

## `pipeline_handlers.go` · Go, 71 LOC · role: `GET /api/v1/pipelines[/{name}]`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-PL-001 | S3 | Med | route server.go:291 | Authz | No role gate; exposes all configured pipeline topology to any authenticated (or anonymous, single-tenant) caller. Pipelines are global/boot-only so not cross-tenant data, but confirm intended. **`X-AUTHZ-001`.** |
| F-PL-002 | S3 | Low | :30,:51 | Readability | `if h == nil || h.Registry == nil` — `h==nil` half is dead. |
| F-PL-003 | S3 | Low | :45-61 | Tests | Empty-name (400) and nil-registry-detail paths untested. |

## `audit_handlers.go` · Go, 68 LOC · role: `GET /api/v1/audit`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-AU-001 | S2 | High | :60 | Error/nil-deref | No `h.Store == nil` guard (unlike costs/pipelines) → panic if mounted with a nil store. Add a 503 guard. |
| F-AU-002 | S2 | Med | test | Tests | No `audit_handlers_test.go` — kind validation, bad-since, limit, success, admin gate all untested. |
| F-AU-003 | S3 | Med | :49-58 | Correctness | `limit` not clamped to the documented max (1000); `limit=1000000` accepted. **See `X-LIMIT-001`.** |
| F-AU-004 | S3 | High | :24,:35 | Doc-drift | Doc/error message omit `api_key.rotated` (CLAUDE.md lists 8 kinds). Regenerate from one source. |
| F-AU-005 | S2 | Med | — | Tenant isolation | Audit `List` has no tenant filter; admin-only gate is the sole isolation, so a tenant-A admin sees tenant-B's `tenant.created`/`api_key.*` events. **See `X-SEC-002`.** |

## `validate_handler.go` · Go, 90 LOC · role: `POST /api/v1/config/validate`

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-VL-001 | S1 | High | :41 | Security/DoS | `io.ReadAll(r.Body)` with no `MaxBytesReader` → unbounded allocation (and a YAML alias-bomb via `ValidateBytes`). **`X-DOS-001`.** Wrap `r.Body` in `MaxBytesReader`. |
| F-VL-002 | S3 | High | :46 | Correctness | `defer r.Body.Close()` registered *after* the read + its early-return, so a read error skips the close. Move the defer before the read. |
| F-VL-003 | S3 | Med | :54-61 | Robustness | JSON-wrapper with empty/missing `yaml` key silently feeds raw JSON to `ValidateBytes` as YAML with no signal. Distinguish empty-wrapper from raw doc. |

## `watcher.go` · Go, 275 LOC · role: agent dir hot-reload (recently modified)

| ID | Sev | Conf | Line | Dimension | Evidence → Fix |
|----|-----|------|------|-----------|----------------|
| F-WT-001 | S2 | High | :97-108,:150-155 | Concurrency/lifecycle | `Stop()` cancels the ctx + waits for `loop`, but never stops pending debounce timers or waits for in-flight `reloadFile` callbacks → a timer firing after `Stop()` calls `Registry.Register/Remove` post-teardown. (Downgraded from S1: `Stop` is only called at process shutdown today, so the late registry write is benign — but it's a real lifecycle bug if `Stop` is ever used mid-life.) Add a `closed` flag checked under `w.mu` in the callback, `timer.Stop()` all pending in `Stop()`, and a `WaitGroup` to drain in-flight callbacks. |
| F-WT-002 | S2 | Med | :232-245 | Concurrency/TOCTOU | `removeIfUnclaimed` releases `w.mu` before `Registry.Remove`; a concurrent `rememberFile` could re-claim the name in the window → a live agent is deregistered. (The unlock-before-I/O is the *correct* anti-deadlock choice; the gap is the TOCTOU.) Document, or make claim-check+remove atomic. |
| F-WT-003 | S2 | Med | :98-107 | Concurrency | `Stop()` idempotency is only safe sequentially — the `w.cancel == nil` check is read/written without a lock; two concurrent `Stop()`s could double-`Close`/double-`<-done`. Guard with `sync.Once` or hold `w.mu`. |
| F-WT-004 | S2 | Med | :147-149 | Correctness | `pending`/`fileAgents` keyed by raw `event.Name` (not `filepath.Clean`/Abs) — if `w.Dir` is uncleaned, a Create-keyed and Remove-keyed entry may not match → rename-away never unregisters. Normalize keys once. |
| F-WT-005 | S2 | Med | :267-275 | Error handling | `isTransientMissing` matches error-string substrings (locale/wrapping-fragile). Prefer `errors.Is(err, fs.ErrNotExist)`. |
| F-WT-006 | S1→S2 | High | test | Tests | `TestWatcherStopIdempotent` calls `Stop()` twice with **no pending timers** — the F-WT-001 reload-after-Stop path is untested. Add a test: write a file (arm a timer), `Stop()` immediately, assert no registry mutation after Stop. |
| F-WT-007 | — | High | :113,:232-241 | Concurrency (positive) | The event-loop shutdown handshake (`defer close(w.done)` ↔ `<-w.done`) is race-free, and unlocking `w.mu` before the registry call is the correct lock-ordering (no `w.mu`↔registry deadlock). |
| F-WT-008 | S3 | Low | :80 | Doc | Watch is non-recursive (subdirs ignored) but the contract isn't documented. |
