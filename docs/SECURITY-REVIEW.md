# Security Review — MockAgents (2026-06-04)

**Reviewer:** security analyst pass (5 parallel surface audits + synthesis)
**Scope:** whole `internal/` + `cmd/` tree — OWASP Top 10 lens (injection, broken
access control, auth failures, crypto, sensitive-data exposure, SSRF,
deserialization, misconfiguration, logging).
**Audience:** the developer agent that will fix the items below.

---

## 1. Verdict

**Overall posture: strong.** This codebase has already absorbed several security
passes (the `X-SEC-001`, `X-TN-001`, `X-TN-002`, `F-TN-006`, `F-MW-007`,
`F-ST-001` fixes), and they were **re-verified correct end-to-end** in this
review. There are **no Critical or High findings** and **no SQL injection,
SSTI, path traversal, SSRF (remote), CORS, or secret-leak vulnerabilities**.

The actionable surface is **1 Medium + 6 Low + a few optional hardening nits**.
The single most worthwhile fix is **SEC-01** (a DoS gap in the *standalone* MCP
HTTP server). The rest are defense-in-depth / consistency / privacy items.

| Severity | Count | IDs |
|----------|------:|-----|
| Critical | 0 | — |
| High | 0 | — |
| Medium | 1 | SEC-01 |
| Low | 6 | SEC-02 … SEC-07 |
| Info / hardening | several | listed under §4 |

> **Do not "re-fix" the verified-safe items in §5** — they are deliberate,
> tested mitigations. Touching them is a regression risk.

---

## 2. Action plan (execute in this order)

> **Status 2026-06-04:** SEC-01, SEC-02, SEC-03, SEC-04, SEC-05, SEC-06
> **implemented** (each with a test; SEC-01/02/05 + the retention prune
> neuter-verified). **SEC-07 declined** — see the note under its checkbox.

- [x] **SEC-01 (Medium) — implemented.** Bound the request body on the standalone MCP HTTP
  transport. Wrap the body in `http.MaxBytesReader` before `io.ReadAll` /
  `json.NewDecoder` in `internal/mcp/http.go` (both `HTTPHandler.ServeHTTP` and
  `NotifyHandler.ServeHTTP`), and/or wrap the mux in `cmd/mockagents/mcp.go`
  with the same `MaxBodySize` middleware the main server uses.
  **Done when:** a multi-MB POST to `mockagents mcp --transport http` is rejected
  with 413 instead of allocating unbounded; a regression test posts an
  oversized body and asserts the cap.

- [x] **SEC-02 (Low) — implemented.** Route the three interaction-log handler 500s through
  `writeServerError` so SQLite/driver internals stop leaking to clients
  (`internal/server/log_handlers.go:106, 145, 184`). Every sibling handler
  already does this (F-TN-006); these three are the inconsistency.
  **Done when:** `ListLogs`/`GetLog`/`DeleteLogs` return the generic
  `{"error":"internal error"}` envelope on a store error and log the detail
  server-side; the 400/404/503 branches are unchanged.

- [x] **SEC-03 (Low) — implemented.** Tighten `skipAuth` to exact-match the LLM/management
  endpoints instead of `strings.HasPrefix` (`internal/server/server.go:534-538`).
  Keep the `/v1/engines/` prefix only if sub-paths are genuinely intended.
  **Done when:** auth-exemption is decided by exact path equality for
  `/v1/chat/completions`, `/v1/models`, `/v1/messages`; a test asserts a
  sibling path like `/v1/models-internal` is NOT auto-exempted.

- [x] **SEC-04 (Low) — implemented.** Add an upper clamp to the `offset` query param so a
  huge `?offset=999999999` can't force a deep table scan
  (`internal/server/handlers.go` `parseBoundedInt(..., "offset", 0, 0)` →
  give it a sane max, e.g. `maxListLimit`).
  **Done when:** `offset` is clamped like `limit`; a test asserts an
  out-of-range offset is capped, not passed through.

- [x] **SEC-05 (Low, privacy) — implemented 2026-06-04.** Operators now control
  response-body capture via **`MOCKAGENTS_LOG_BODIES`** = `full` (default,
  back-compat) | `sanitized` (wires in the formerly-dead `storage.SanitizeBody`
  to redact `sk-`/`key-`/`Bearer` tokens while keeping the usage block so cost
  annotation still works) | `none` (drop the body; the model probe still runs on
  the raw body so by_agent grouping is preserved). Retention is bounded via
  **`MOCKAGENTS_LOG_MAX_ROWS`** = N, enforced by a background `logPruner` (1-min
  tick, also prunes once at boot) calling the new
  `storage.PruneToMaxRows(ctx, max)` which keeps the newest N rows; 0 = unlimited.
  Tests: `storage/prune_test.go`, `server/log_bodymode_test.go` (mode parsing +
  `none`/`sanitized` integration + pruner lifecycle); `none`, `sanitized`, and
  the prune threshold are neuter-verified. **Note:** in `none` mode, body-derived
  cost/token annotation is unavailable for those rows (documented tradeoff —
  `sanitized` keeps it).

- [x] **SEC-06 (Low) — implemented.** Harden the record-mode proxy: validate `--upstream`
  scheme is `http`/`https` in `recording.NewProxy`, and normalize/clean the
  forwarded `r.URL.Path` to stop `..`-style path escapes from reaching
  unintended upstream routes (`internal/recording/proxy.go:69-83`,
  `cmd/mockagents/record.go:39`). This is a dev CLI tool, not the served
  product, so the SSRF is operator-bounded — hardening only.
  **Done when:** `NewProxy` rejects a non-http(s) upstream and the joined path
  cannot traverse above the upstream base.

- [ ] **SEC-07 (Low, optional) — DECLINED 2026-06-04 (net-negative).** Run the
  timing-defense dummy bcrypt on the malformed-key-format reject branch too
  (`internal/tenancy/store.go:647-648`).
  **Why declined:** the format check rejects strings that don't match the
  **publicly documented** `mak_<8hex>_<secret>` envelope. An attacker
  constructs their own guess, so they already know whether it's malformed —
  the timing distinguishes only public format-shape, i.e. **nothing secret**
  (the real existence oracle, X-TN-002, is already closed). Adding a dummy
  bcrypt would make every malformed/garbage request pay ~36 ms of CPU (a mild
  DoS amplification) to defend a zero-value oracle. Net-negative; not
  implemented. (Re-open only if strict uniform-reject-timing is a hard
  requirement, paired with auth rate-limiting.)

### Optional hardening (Info — do only if cheap)
- [ ] Cap the MCP outbound residual slice in `enqueue` (drop-oldest) so an
  undrained admin queue can't grow unbounded (`internal/mcp/bidirectional.go`).
- [ ] Log the resolved `MOCKAGENTS_PRICING` path at startup so an unexpected
  pricing source is visible (`cmd/mockagents/start.go`).
- [ ] Document that the first-boot bootstrap-key stderr line should be captured
  then scrubbed from log aggregators (`cmd/mockagents/start.go`).
- [ ] Consider `DisallowUnknownFields` on the adapter JSON decoders for stricter
  provider parity (`internal/adapter/decode.go`) — not a security need here.

---

## 3. Findings detail

### SEC-01 — Unbounded request body on standalone MCP HTTP transport (Medium)
- **OWASP:** A05 Misconfiguration / DoS
- **Where:** `internal/mcp/http.go:37` (`io.ReadAll(r.Body)`), `:128`
  (`json.NewDecoder(r.Body)`); `cmd/mockagents/mcp.go:92-103` (no body cap on
  the standalone server).
- **Evidence:** `body, err := io.ReadAll(r.Body)` with no `MaxBytesReader`; the
  standalone `mockagents mcp --transport http` server mounts `/mcp` and
  `/mcp/notify` **without** the `MaxBodySize` middleware the main server applies
  (`server.go:178`).
- **Impact:** an attacker who can reach `mockagents mcp --transport http` streams
  a multi-GB POST → unbounded allocation in `io.ReadAll` → OOM the process.
- **Fix:** `r.Body = http.MaxBytesReader(w, r.Body, 1<<20)` at the top of both
  handlers, or wrap the mux with `server.MaxBodySize`.

### SEC-02 — Log handlers leak raw store/driver errors (Low)
- **OWASP:** A09 / internal-detail exposure
- **Where:** `internal/server/log_handlers.go:106, 145, 184`
- **Evidence:** `writeJSON(w, 500, map[string]string{"error": fmt.Sprintf("querying logs: %s", err)})`
  — the store wraps SQLite errors with `%w` (`sqlite.go:219`), so driver
  internals (schema names, error codes, possibly the DB path) reach the client.
- **Impact:** an authenticated caller hitting a DB fault gets SQLite internals in
  the 500 body — the exact leak `writeServerError`/F-TN-006 prevents elsewhere.
- **Fix:** replace the three 500 branches with `writeServerError(w, err)`.

### SEC-03 — `skipAuth` prefix matching wider than intended routes (Low)
- **OWASP:** A01 Broken Access Control
- **Where:** `internal/server/server.go:534-538`
- **Evidence:** `strings.HasPrefix(path, "/v1/messages") || …` — any path with
  these prefixes is auth-exempt.
- **Impact:** **not currently exploitable** (the mux 404s unmatched paths before
  a handler runs), but a future route mounted under one of these prefixes would
  silently inherit anonymous access.
- **Fix:** exact-match the LLM endpoints.

### SEC-04 — `offset` query param has no upper bound (Low)
- **OWASP:** A04 Insecure Design / DoS
- **Where:** `internal/server/handlers.go` (`parseBoundedInt(..., "offset", 0, 0)`)
- **Evidence:** `limit` is clamped to `[1, 1000]`; `offset` is only floored at 0,
  no ceiling, so `?offset=999999999` forces a deep scan on the log table.
- **Impact:** mild attacker-driven query cost amplification (auth'd, tenant-scoped).
- **Fix:** clamp `offset` to a sane max.

### SEC-05 — Response bodies persisted with no redaction/retention (Low, privacy)
- **OWASP:** A09 / sensitive-data exposure
- **Where:** `internal/server/log_handlers.go:306-316`, `internal/storage/sqlite.go`
- **Evidence:** `ResponseBody: respBody` persists up to 1 MiB verbatim;
  `SanitizeBody`/`TruncateBody` exist but are **dead code** (only their own unit
  tests reference them). No retention/TTL bound — the table grows until
  `DELETE /api/v1/logs`.
- **Impact:** mostly mitigated — **request** bodies are never persisted (verified),
  reads are tenant-scoped, and responses are canned/mock. Residual: a scenario
  response can carry fake-PII / operator-sensitive content, with no opt-out.
- **Fix:** flag-gate body capture + wire `SanitizeBody`, add a retention knob.

### SEC-06 — Record-proxy forwards request path/query to operator upstream (Low)
- **OWASP:** A10 SSRF (bounded)
- **Where:** `internal/recording/proxy.go:69-83`, `cmd/mockagents/record.go:39`
- **Evidence:** outbound host/scheme are pinned to the operator `--upstream`
  flag; only path/query are request-influenced. `net/http` won't do `file://`.
- **Impact:** an attacker **cannot** redirect to `169.254.169.254`/internal
  hosts (host is operator-fixed); worst case is hitting unintended *paths* on
  the operator-chosen upstream. `record` is a dev CLI, not the served product.
- **Fix:** validate `--upstream` scheme + normalize the joined path.

### SEC-07 — Residual format-shape timing oracle in `Resolve` (Low)
- **OWASP:** A07 Auth Failures
- **Where:** `internal/tenancy/store.go:647-648`
- **Evidence:** a malformed-format key returns in ns (before the X-TN-002 dummy
  bcrypt at `:695-697`); a well-formed wrong key runs a full bcrypt.
- **Impact:** distinguishes only the **public** `mak_<8hex>_` envelope shape —
  reveals no prefix/key existence. The real existence-oracle (X-TN-002) is
  correctly closed.
- **Fix:** dummy-compare on the malformed branch too (defense-in-depth).

---

## 4. OWASP coverage summary

| OWASP 2021 | Result |
|---|---|
| A01 Broken Access Control | **Pass** — tenant isolation + RBAC floors verified end-to-end (X-SEC-001, X-TN-001). Nit: SEC-03 prefix match. |
| A02 Cryptographic Failures | **Pass** — crypto/rand + bcrypt(cost 10) on every auth path; math/rand confined to cosmetic IDs that gate nothing. |
| A03 Injection (SQL/SSTI/cmd) | **Pass** — all SQL parameterized; no request-driven SSTI (templates are config-only, request data is template *data* not *text*); RE2 = no ReDoS. |
| A04 Insecure Design | Minor — SEC-04 offset clamp. |
| A05 Misconfiguration | **Pass** — CORS safe (no credentials, exact-origin allowlist), all server timeouts set, default bind 127.0.0.1. Gap: SEC-01 MCP body cap. |
| A06 Vulnerable Components | Out of scope (no concrete dep issue surfaced); yaml.v3 bounds alias bombs. |
| A07 Auth Failures | **Pass** — bearer parsing robust, timing defense correct (X-TN-002). Nit: SEC-07. |
| A08 Data Integrity / Deserialization | **Pass** — yaml.v3 + body caps; JSON depth-bounded; no unsafe/gob decode. |
| A09 Logging / Sensitive Data | Minor — SEC-02 error leak, SEC-05 body retention. Secrets never logged (F-MW-007 verified). |
| A10 SSRF | **Pass (remote)** — MCP makes no outbound calls; record proxy host is operator-pinned (SEC-06 hardening only). |

---

## 5. Verified-safe — DO NOT regress these

These were checked and are correct. Re-touching them is a regression:

- **SQL:** every user value is `?`-bound; the only `%d`-formatted SQL uses
  clamped ints; the one identifier interpolation (`PRAGMA table_info`) uses a
  compile-time constant.
- **Tenant isolation (X-SEC-001):** all `{id}` key/log/audit routes carry
  `AND tenant_id = ?` (or `ensureOwnTenant`) and re-check on read.
- **Platform escalation (X-TN-001):** `Role.IsAssignableViaAPI` blocks platform
  via the API; platform is minted only by the bootstrap path.
- **Timing oracle (X-TN-002):** prefix-miss runs a real dummy bcrypt.
- **Secrets:** API-key plaintext is returned once, never stored/logged; only the
  bcrypt hash is persisted; `NewAPIKeyResult` redacts via `LogValuer`/`Stringer`;
  StructuredLogger never logs `Authorization` (F-MW-007).
- **Randomness map:** API-key secret/prefix + tenant/key ids + `{{ uuid }}` →
  crypto/rand; `req-`/`chatcmpl-`/`sess-`/`toolu_`/`msg-` → math/rand/v2 and
  gate nothing (the `sess-` id is only a state-map key scoped by the
  authenticated tenant+agent).
- **CORS:** no `Access-Control-Allow-Credentials` anywhere; allowlist reflects
  only exact-match origins; bearer (non-cookie) auth makes wildcard ACAO safe.
- **`mountManaged`** panics at startup on any route missing a role floor, so an
  ungated management route is a boot failure, not a silent bypass.
- **Server timeouts:** Read/Write/Idle/ReadHeader all set with a fallback;
  slow-loris hardened (PERF-21).
- **YAML/JSON:** yaml.v3 alias-bomb bounded (test-proven); JSON depth-bounded;
  validate endpoint capped at 1 MiB.

---

## 6. What this review did NOT cover

- ~~Dependency CVE scanning~~ **— done 2026-06-04.** `govulncheck ./...`
  initially flagged **11 reachable vulnerabilities, all in the go1.26.1 standard
  library** (crypto/x509, crypto/tls, html/template, net, net/textproto +
  golang.org/x/net HTTP/2). Most have low *practical* exposure for the default
  deployment (plain HTTP, no TLS termination; the engine uses `text/template`,
  not `html/template`), but the fix is trivial: **`toolchain go1.26.4`** in
  `go.mod` (clears all 11) + **`golang.org/x/net` v0.52.0→v0.53.0**. Re-scan:
  **0 reachable vulnerabilities.** Remaining import/module-level findings are
  unreachable (govulncheck confirms the symbols aren't called) and clear with
  routine dependency bumps. Re-run periodically — new stdlib CVEs land often.
- ~~The Next.js GUI (`gui/`)~~ **— done 2026-06-04, see `docs/SECURITY-REVIEW-GUI.md`.**
  No live XSS (no `dangerouslySetInnerHTML`, all data auto-escaped), key stays
  server-only, all mutations are CSRF-protected Server Actions, no
  request-controlled SSRF. Findings are hardening: **1 High** (auth cookie missing
  `Secure`), **4 Medium** (one-time secrets in redirect URLs; credentialed
  log-proxy confused-deputy; no CSP; no clickjacking header), **4 Low/Info**.
- Runtime/infra (Helm/network policies, TLS termination, secrets at rest).
- Fuzzing of the adapter/JSON-RPC parsers.
