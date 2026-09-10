# Security, privacy, dependency and runtime review

Reviewed candidate: `6ddb03e54a14484e5929a19673f0cfd8a1975f07`, 2026-09-10. Pass 1 inspected tenancy middleware/stores/cache, role floors/key handlers, metrics, recording redaction/proxy, server lifecycle and build-toolchain declarations. Pass 2 traced authenticated principal → target key → mutation, shared database → process-local cache, tenant request → global metrics, upstream byte chunks → cassette redaction, and declared Go floor → compiled standard library. Parent auditor validated the four security probes and completed this report after the security subagent ended before report delivery.

## Findings

| ID | Severity | Evidence | Impact / release decision |
|---|---|---|---|
| SR-01 | Critical | `internal/server/route_authz.go:59-65`; `internal/server/tenancy_handlers.go:44-83,285-301,347-365`; `internal/tenancy/store.go:480-552`; `cmd/mockagents/start.go:642-690` | A tenant admin co-located with a platform key can rotate it and receive a new platform secret. Reproduced HTTP 200 and principal role=platform. Blocks multi-tenant release. |
| SR-02 | Medium | `internal/server/route_authz.go:115-119`; `internal/server/metrics_handlers.go:17-29`; `internal/metrics/metrics.go:1-2,83+` | Any viewer can read the process-wide metrics registry, including other tenants' agent/scenario labels. Reproduced 200 with a confidential other-tenant label. Blocks tenant-isolation signoff until restricted or scoped. |
| SR-03 | High | `cmd/mockagents/start.go:360,368`; `internal/tenancy/store.go:704-750`; `internal/tenancy/postgres_store.go:535-550`; `internal/tenancy/auth_cache.go:77-98,148+` | Revocation/rotation/demotion only clears the mutating process's cache; another cached store continues accepting stale credentials/roles up to five minutes. Reproduced with two independent stores sharing a DB. Blocks immediate-revocation claims in replicated deployments. |
| SR-04 | High | `internal/recording/proxy.go:239-317`; `internal/recording/redact.go:63-70,143-163` | Redaction operates on arbitrary network chunks. Even an intact GitHub token survives when the surrounding JSON frame is split, because invalid JSON falls back to narrower prefix sanitization. Reproduced with synthetic token; `--redact` does not reliably protect recorded streams. |
| SR-05 | High | `go.mod:5-9`; local `go version`; `govulncheck ./...` | Declared Go 1.26.4 toolchain is affected by eight reachable standard-library advisories. Scan exits 1. At least Go 1.26.6 fixes this identified set; re-scan the selected compiler and shipped binaries before release. |

No claim is made that every deployment exposes every issue: SR-01 needs an admin account in a tenant containing a platform credential; SR-03 needs multiple independently cached processes; SR-04 needs recorded sensitive stream content. Single-tenant mode is explicitly a local unauthenticated developer tool (`SECURITY.md`), not a public-network security boundary.

## SR-01: authorize the target credential, not only its tenant

The handler's admin floor and tenant ownership checks are insufficient when a platform key belongs to the same tenant. The bootstrap design deliberately stores the platform key in `default`. The store preserves the target role during rotation, then the handler returns plaintext. Bulk rotation includes the same target. PATCH and DELETE also need target-role protections to prevent lower-privilege users demoting/deleting platform credentials.

Immediate conservative patch: change admin-targeted key mutations to platform-only; retain `/keys/me/rotate` and `/keys/me/burn` as own-key operations. This changes tenant-admin capability and must be reflected in GUI capabilities/docs. It is a small fail-closed containment patch while implementing the fuller policy.

```go
// internal/server/route_authz.go
"POST /api/v1/tenants/{id}/keys/rotate": tenancy.RolePlatform,
"PATCH /api/v1/keys/{id}":              tenancy.RolePlatform,
"POST /api/v1/keys/{id}/rotate":        tenancy.RolePlatform,
"DELETE /api/v1/keys/{id}":             tenancy.RolePlatform,
```

Full fix: extend store mutation APIs to accept authenticated actor authority and check target role inside the same transaction/conditional update as mutation. Authorize tenant admins to mutate only non-platform keys in their tenant. Bulk operations must reject a forbidden target atomically or explicitly exclude platform keys with a documented response; never return a forbidden secret. Keep platform creation bootstrap-only. A handler pre-read alone is not a sufficient long-term concurrency guard.

Tests in `internal/server/tenancy_platform_scope_test.go` plus SQLite/Postgres conformance: default-tenant admin attempts individual and bulk platform rotation → 403 with no changed hash/prefix; admin cannot PATCH/DELETE platform target; foreign tenant remains inaccessible; ordinary own-tenant admin operations still work under full fix; platform self-rotation still works; denied mutations produce audit events without plaintext; concurrency cannot swap the role between authorization and mutation. Update API security descriptions, role matrix and key-rotation runbook. Treat any previously exposed replacement secret as compromised only if deployment evidence shows this path was used; this audit used newly created synthetic keys.

## SR-02: separate operator monitoring from tenant visibility

The metrics registry has no tenant dimension and `MetricsHandler` does not filter by principal. Making it merely admin-only would still expose cross-tenant data to tenant admins. Restrict the existing global endpoint to platform operators:

```go
"GET /metrics": tenancy.RolePlatform,
```

Update the comment, `docs/api-spec.yaml` operation security and Helm monitoring docs accordingly. Wire the ServiceMonitor authorization secret (see DS-07 in delivery report) so this change preserves scraping. If tenant metrics are needed, implement a distinct tenant-scoped endpoint with an explicit bounded-cardinality design.

Tests: unauthenticated → 401, viewer/editor/admin from either tenant → 403, platform → 200; create actual traffic from two tenants and verify labels. Retain intentionally open single-tenant local monitoring behavior and document that boundary.

## SR-03: give revocation a cross-process authority check

The two-store reproduction establishes cache behavior, not a PostgreSQL deployment test. PostgreSQL uses the same cache-first mechanism; validate it in the existing service-container conformance job. Local `Invalidate()` cannot notify other instances. Reducing TTL reduces exposure but does not implement immediate revocation.

Concrete design: cache the successful expensive bcrypt result together with key ID and a credential version. Before accepting a cached principal, perform a cheap indexed lookup of key existence, tenant existence, role and credential version in the authoritative store. Increment version on rotation; deleted rows reject; changed roles replace the cached role. Guard cache population against concurrent mutations using a generation/version check. Publish/subscribe invalidation can optimize this later but must have a safe failure mode.

```sql
-- Migration for BOTH tenancy stores; choose a migration sequence owned by Store startup.
ALTER TABLE api_keys ADD COLUMN credential_version INTEGER NOT NULL DEFAULT 1;
-- Rotation transaction increments credential_version with hash/prefix replacement.
-- Cached authentication performs an indexed read by id:
SELECT tenant_id, role, credential_version FROM api_keys WHERE id = ?;
```

Postgres uses `$1` placeholders; verify tenant existence in the same query or enforce tested cascading deletion. A missing/changed version invalidates cached proof; fail closed if authority is unavailable. As temporary containment, disable positive auth caching for shared-backend deployments by omitting `EnableAuthCache`, rather than adding undocumented guarantees to a shorter TTL. Measure the resulting bcrypt cost and retain rate limiting.

Tests: two live stores, prime B, rotate/delete/demote on A, immediately attempt B; repeat with sessions if they gain caching; concurrent lookup vs invalidation; store outage; ledger and quota unaffected. Update `site/docs/reference/configuration.md` and revocation SLO. Do not claim globally atomic revocation beyond the selected in-flight-request semantics.

## SR-04: redact complete SSE frames before persistence

Proxy reads at most 4096 bytes per read, which are not SSE event boundaries. `redactStreamData` uses `storage.SanitizeBody` on incomplete JSON; that fallback omits GitHub tokens and custom patterns. Existing comment mentions secrets split across chunks, but this reproduction keeps the whole token in one chunk. CLI's best-effort caveat does not remove the implementation defect.

Refactor recorded events through a bounded SSE frame assembler. Preserve live upstream forwarding unchanged; redact the complete recorded payload values, then store frame-safe events with documented delay semantics. Handle multiline `data:` fields, LF/CRLF, EOF partial frames, multiple frames per read and UTF-8 boundaries. Oversized or incomplete sensitive frames must not silently fall back to raw persistence; produce an explicit recording error/drop according to documented policy.

```go
// Proposed interface used inside Redactor.Apply for streaming interactions.
frames, err := assembleRecordedSSE(it.StreamEvents, maxRecordedFrameBytes)
if err != nil {
    return fmt.Errorf("redacting recorded SSE: %w", err) // propagate to cassette writer
}
for i := range frames {
    frames[i].Data, err = r.redactCompleteSSEFrame(frames[i].Data)
    if err != nil { return err }
}
it.StreamEvents = frames
```

Change `Redactor.Apply` to return errors and handle them in proxy/import callers; update tests accordingly. Add a table splitting a JSON frame at every byte offset, with synthetic GitHub/AWS/Google/Bearer/custom tokens, and assert no token appears in serialized cassette while JSON and replay order stay valid. Test secret-free byte-fidelity expectations explicitly when redesigning stored chunk layout. Document supported redaction and size limits.

## SR-05: rebuild with a patched compiler and gate the actual artifact

On 2026-09-10, `govulncheck ./...` using Go 1.26.4 reported:

| Advisory | Package | Fixed compiler |
|---|---|---|
| [GO-2026-6218](https://pkg.go.dev/vuln/GO-2026-6218) | net/url | 1.26.6 |
| [GO-2026-6091](https://pkg.go.dev/vuln/GO-2026-6091) | html/template | 1.26.6 |
| [GO-2026-6090](https://pkg.go.dev/vuln/GO-2026-6090) | crypto/tls | 1.26.6 |
| [GO-2026-6089](https://pkg.go.dev/vuln/GO-2026-6089) | net/http | 1.26.6 |
| [GO-2026-6088](https://pkg.go.dev/vuln/GO-2026-6088) | encoding/xml | 1.26.6 |
| [GO-2026-5972](https://pkg.go.dev/vuln/GO-2026-5972) | encoding/asn1 | 1.26.6 |
| [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856) | crypto/tls | 1.26.5 |
| [GO-2026-5026](https://pkg.go.dev/vuln/GO-2026-5026) | net/http bundled IDNA | 1.26.6 |

This is symbol-reachability evidence, not proof of exploitability of all eight advisories in this app. Official Go advisory pages independently confirm the 1.26.6 boundary for URL, TLS and HTTP timeout findings. Five additional package/module findings were not reported reachable; do not label those exploitable without analysis.

```diff
--- a/go.mod
+++ b/go.mod
@@
-toolchain go1.26.4
+toolchain go1.26.6
```

Replace the stale “govulncheck clean” comment with a dated policy referencing the release scan. Select an approved supported Go patch >=1.26.6 and pin that version consistently in CI/release and the builder image, preferably by reviewed image digest. Docker currently uses floating `golang:1.26-alpine`, so this audit does NOT establish that a newly pulled container image contains 1.26.4. CI also requests floating `1.26`, which can differ from the locally selected compiler. Capture `go version` and `go version -m <binary>` for each release artifact; run source and binary scans and retain outputs. Do not infer published artifact toolchains from go.mod alone.

Existing CI has a `vuln` job (`.github/workflows/ci.yml:442-458`); release does not depend on it. Add it to the shared exact-candidate release gate (DS-05), alongside a container OS-package scan. Re-run all Go tests with the chosen compiler. No compiler upgrades were installed during this audit.

## Validation and residual checks

Fresh temporary SQLite databases and synthetic keys only; the four probes returned:

```text
SR-01: admin rotates platform key: HTTP=200, new principal role=platform
SR-02: viewer metrics: HTTP=200, other tenant metric exposed=true
SR-03: deleted key accepted by other cached store=true, authoritative lookup rejected=true
SR-04: complete GitHub token inside split JSON SSE frame survives redaction=true
```

Source is retained in `security-probe/main.go` for local reproduction (`go run ./docs/reviews/security-probe`). A workspace-local GOCACHE was required after default cache permission errors. The probe asserts vulnerable behavior for diagnosis, not the fixed regression expectation. Invert assertions when creating permanent tests.

Positive protections inspected: central route-floor registration, tenant ownership checks, parameterized SQL, bounded session/metrics structures on normal paths, request body limits, redaction of ordinary complete JSON, loopback default for recording proxy, restricted key creation and audit recording. These do not cover the distinct integration failures above.

Further targeted review required before security signoff: credential-origin provenance for WebSocket cookie authentication and optional authentication work on public routes; no confirmed exploit or severity is assigned here. Real PostgreSQL revocation, fresh-browser SSO/CSRF, full race suite, deployment ingress policy and backup/restore were not exercised locally. Repository settings, credentials, external registry ownership and published binary contents were not available as audit evidence.

