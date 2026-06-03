# Action Plan — internal/tenancy review

Ordered by priority. Each item has a "done when". Check off as completed.
Source findings in `01-PER-FILE.md` (F-*) and `02-INTEGRATION.md` (X-TN-*).

> **One S1, no S0.** The store's API-key tenant isolation is sound; the one
> real isolation gap is at the **tenant-CRUD** surface (X-TN-001), and it needs
> a product/design decision, not just a code patch. Everything else is S2/S3
> hardening + test coverage for security-critical paths that currently rely on
> reasoning rather than tests.

## P1 — decide & fix the tenant-CRUD isolation gap

- [x] **Tenant-CRUD cross-tenant access** — `X-TN-001` · S1 · effort:L (design) — **DONE 2026-06-02 — chose Option 1 (platform/super-admin role).** Added `RolePlatform` above admin (`rank 4`; `IsValid` includes it). The tenant-collection route floors (`GET/POST /api/v1/tenants`, `DELETE /tenants/{id}`) are now `RolePlatform`, so a per-tenant admin gets 403. The bootstrap (`cmd/mockagents/start.go`) mints the `bootstrap-admin` key with `RolePlatform` and the existing-key check looks for a platform key. Escalation is blocked: the management API uses new `Role.IsAssignableViaAPI()` (every valid role except platform) in `CreateAPIKey`/`UpdateAPIKeyRole`, so even a platform caller cannot mint/promote a key to platform via the API (bootstrap-only). Per-tenant admins keep key-CRUD scoped to their own tenant. Tests: `tenancy.TestRoleOrderingAndValidity` (rank/AtLeast/IsValid/IsAssignableViaAPI incl. platform), `server.TestMountManaged_PlatformFloorRejectsAdmin` (admin 403 on tenant routes, platform passes — neuter-verified), `server.TestTenancyHandlers_CreateAPIKey_PlatformRoleRejected` (platform-role create → 400 even for a platform caller — neuter-verified), floor table pinned in `TestManagementRouteFloors_FlaggedRoutes`. CLAUDE.md + AGENTS.md updated. Full `go test ./...` + `go vet` green. **Decision record (options considered):**
  1. **Platform/super-admin capability** (recommended if multi-tenant is a real feature): add a `RolePlatform`/super-admin (or a `Principal.Platform bool` minted only for the bootstrap key) and gate tenant-CRUD to it; per-tenant admins keep key-CRUD within their own tenant.
  2. **Self-service only**: scope `ListTenants`/`DeleteTenant` to the caller's own tenant (return only/act only on `principal.TenantID`); drop or platform-gate `CreateTenant`.
  3. **Document & accept**: if tenant-CRUD is intended for a single trusted bootstrap operator, state that explicitly in the route docs + CLAUDE.md and note the model in the auth design.
  - **Done when:** a tenant-A admin calling `GET /api/v1/tenants` cannot see tenant-B, and `DELETE /api/v1/tenants/ten_b` returns 403/404 — OR the trusted-operator model is documented and a test pins the chosen contract.

## P2 — security & correctness hardening (S2)

- [x] **`randID` swallows the crypto/rand error** — `F-ST-001` · effort:S — return the error from `randID` and propagate through `CreateTenant`/`CreateAPIKey` so a (rare) rand failure can't mint a zero-entropy id. **Done when:** `randID` returns `(string, error)` and callers handle it; a fault-injected rand error fails the create.
- [x] **Auth-cache key bit-width doc mismatch** — `F-AC-001`/`F-AC-002` · effort:S — the code truncates SHA-256 to 128 bits (`sum[:16]`) while the doc claims 256 (`[:32]`); fix the comment, and (optional, cache isn't memory-bound) key on the full 32-byte digest. **Done when:** doc and code agree and the collision claim is birthday-bound-correct.
- [x] **Cache eviction prefers expired entries** — `F-AC-003`/`F-AC-004` · effort:S — on capacity, scan-and-drop an expired entry before random eviction, and skip eviction when overwriting an existing key. **Done when:** a hot non-expired key survives capacity pressure while an expired one is dropped first (test).
- [x] **Redact `NewAPIKeyResult.Plaintext`** — `F-TY-003` · effort:S — add a `LogValue()`/`String()` redaction (or a `Secret` wrapper) so an accidental `%v`/`slog` of the result can't spill the key; marshaling for the one-time response stays intact. **Done when:** `fmt.Sprintf("%v", result)` / `slog` does not contain the plaintext, but the JSON response still does.
- [x] **Prefix-existence timing oracle** — `X-TN-002` · effort:S — run a fixed dummy `bcrypt.CompareHashAndPassword` on a prefix-miss in `Resolve` so a wrong-prefix and a wrong-secret take comparable time. **Done when:** prefix-miss and prefix-hit-wrong-secret latencies are within noise (or the risk is documented as accepted).
- [x] **API-key shape validation + prefix-length constant** — `F-ST-003`/`F-ST-015` · effort:S — validate the `mak_<8hex>_` shape in `Resolve` (not a bare `len < 13`) and extract `const apiKeyPrefixLen = 12` shared by `generateAPIKey`/`Resolve`. **Done when:** a malformed prefix is rejected as ErrInvalidKey and the two functions derive the length from one constant.
- [x] **Bearer token internal-whitespace reject** — `F-MW-001` · effort:S — reject a credential containing internal whitespace rather than forwarding `"ab cd"`. **Done when:** `Bearer ab cd` is a clean 401, not a store lookup of a 2-token string.

> **P2 security cluster DONE 2026-06-02 (branch `fix/tenancy-p2-security`).** `randID` returns `(string, error)`, propagated by `CreateTenant`/`CreateAPIKey`. Auth-cache key is the **full 256-bit** SHA-256 digest (doc fixed); `evictOneLocked` prefers an expired entry + skips on overwrite + empty-plaintext guard on `Set`. `NewAPIKeyResult` got `LogValue()` (slog) + `String()` (fmt) redaction — JSON still carries the plaintext. `Resolve` runs a fixed dummy bcrypt on a no-candidate prefix-miss (`timingDummyHash`, X-TN-002) and validates the `mak_<8hex>_` shape via shared `const apiKeyPrefixLen = 12`. `ParseBearerToken` rejects internal whitespace. Tests in `p2_security_test.go` + `bearer_test.go` (eviction + redaction neuter-verified). _X-TN-002's latency equalization is verified by construction, not a flaky timing assertion._ Full `go test ./...` + `go vet` green.

## P2 — missing tests for security-critical paths (S2)

- [x] **Store-level cross-tenant IDOR tests** — `F-ST-010` · effort:M — assert `RotateAPIKey`/`DeleteAPIKey`/`UpdateAPIKeyRole` return ErrNotFound for a key in another tenant. **Done when:** each has a wrong-tenant test.
- [x] **BulkRotate rollback test** — `F-ST-009` · effort:M — fault-inject a mid-loop failure and assert the tx rolls back and **zero** rows changed. **Done when:** the all-or-nothing guarantee is pinned.
- [x] **RBAC ordering + IsValid tests** — `F-TY-005` + `F-MW-006` · effort:S — table test `rank`/`AtLeast`/`IsValid` incl. the `"".AtLeast(viewer)==false` and unknown-required cases the gate relies on. **Done when:** the ordering invariants are locked against a refactor.
- [x] **Middleware fail-closed test** — `F-MW-008` + `F-MW-009` · effort:S — a fake Store returning a raw error yields 500 with `next` not reached; a valid key on a skip route populates the principal. **Done when:** both contracts are tested.
- [x] **APIKey-has-no-secret marshal test** — `F-TY-002` · effort:S — assert `json.Marshal(APIKey{})` contains no `hash`/`secret`/`plaintext` key, with a doc line stating "metadata only". **Done when:** the property is enforced by a test.

> **P2 test batch DONE 2026-06-02 (branch `fix/tenancy-p2-tests`).** New `security_paths_test.go`: `TestStore_CrossTenantKeyOps_ReturnNotFound` (rotate/delete/update of another tenant's key → ErrNotFound + key untouched; **neuter-verified** — dropping `AND tenant_id=?` makes the cross-tenant rotate succeed), `TestBulkRotate_RollsBackOnFailure` (a 40ms ctx vs 8 bcrypts forces a failure → zero keys rotated; the rollback is `defer tx.Rollback()` + no `Commit` on error — the test pins the all-or-nothing *outcome*, not a partial-then-rollback which can't be deterministically forced without a fault-injecting DB), `TestAuthMiddleware_FailsClosedOnStoreError` (fake `errorStore` → 500, `next` not reached; **neuter-verified** — calling `next` on the error branch makes it 200), `TestAuthMiddleware_SkipRoutePrincipal` (F-MW-009). `TestAPIKey_JSONHasNoSecret` (F-TY-002) in `p2_security_test.go`. RBAC ordering (F-TY-005/F-MW-006) was already covered by `TestRoleOrderingAndValidity` from the platform-role work. Full `go test ./...` + `go vet` green.

## P3 — clarity, docs, minor robustness (S3)

- [ ] **Doc/comment fixes** — `F-ST-011` (dedupe the duplicated `BulkRotateTenantKeys` interface doc), `F-MW-007` (RequireRole checks level-only, not tenant ownership), `F-AC-005` (TTL = worst-case stale-auth bound), `F-ST-004` (cascade depends on MaxOpenConns=1), `F-MW-003` (skip-route best-effort-anonymous contract) · effort:S.
- [ ] **Type hygiene** — `F-TY-001` (`LastUsed *time.Time` or drop `omitempty`), `F-TY-006` (`Principal` `json:"-"`/"never serialized"), `F-TY-007` (`AllRoles()`/`ParseRole`), `F-ST-002` (named bcrypt-cost const), `F-AC-009` (empty-plaintext guard) · effort:S.
- [ ] **Minor robustness/perf** — `F-ST-005` (don't discard timestamp parse errors), `F-ST-013` (BulkRotate existence check inside tx, or document), `F-ST-014` (`UPDATE … RETURNING` for atomic role read-write), `F-AC-006` (RWMutex on the cache hot path), `F-MW-002` (add `ErrNotFound` to the 401 branch for label accuracy), `F-ST-012` (note the GetTenant pre-check duplicates the FK), `X-TN-003`/`F-ST-008` (`GetTenantByName` on/off the interface) · effort:S–M.

## Suggested execution order

1. **X-TN-001** decision first (it may change handler/route code the tests below touch).
2. The P2 security cluster (randID, cache bit-width/eviction, plaintext redaction, timing, key-shape) — small, independent, high-value.
3. The P2 test batch (cross-tenant IDOR, bulk rollback, RBAC ordering, fail-closed) — locks the security contracts.
4. P3 sweep (docs + type hygiene + minor robustness) in one or two passes.
