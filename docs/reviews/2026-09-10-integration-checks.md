# Cross-file integration review — 2026-09-10

Scope: `6ddb03e54a14484e5929a19673f0cfd8a1975f07`. This is pass 2; per-file evidence and exact patches live in the four persona reports.

| Boundary traced | Existing protection | Observed gap / decision | Required integration test |
|---|---|---|---|
| Bootstrap → role floors → target key mutation → plaintext response | Platform cannot be minted via ordinary create; tenant ownership enforced | SR-01: same-tenant platform target retains privilege through admin rotation | Admin/default-tenant matrix for rotate/bulk/patch/delete; confirm no side effects |
| Mutation in instance A → auth cache in instance B | Mutation invalidates A; indexed DB rows persist | SR-03: B accepts stale principal | Two-store SQLite and PostgreSQL revoke/rotate/demote plus concurrent lookup |
| Tenant request → global metrics → Helm scraper | Authenticated metrics; bounded cardinality | SR-02 leaks labels; DS-07 fails to transmit key | Platform scrape 200, tenant viewer/admin403, absent key401; chart renders secret reference |
| Provider byte reads → SSE recorder → redactor → disk | Complete JSON string-value redaction | SR-04: incomplete JSON frame takes narrower fallback | Every byte split, multiline event, custom patterns, partial EOF, serialized cassette inspection |
| Valid agent → generator exception → locked session | Session mutex and normal-turn cap | AR-03: error path commits/retains partial turn | Repeat failure/retry, nested variable rollback, history count/bytes bounded |
| A2A request → map → get/cancel → JSON encoding | Mutex protects map operations | AR-01/02/07: unbounded tasks, mutable responses, ignored continuation ID | Capacity/expiry, snapshot/race and send↔stream lifecycle matrix |
| Chroma add → metadata store → similarity query | Documents retained for get | AR-04/05: query projection/config metric lost | Real document-bearing SDK query, each metric/default, include matrix |
| Shared vector ranking → Qdrant threshold/wire score | Internal similarity ordering | AR-06: Euclidean threshold and response domain mismatch | Known distances 0/5/10 and threshold boundary equality |
| Finite request vector → arithmetic → pooled encoder | Reject explicit non-finite inputs | AR-08: computed non-finite score becomes empty 200 | Overflow cases and direct encoder failure produce structured non-2xx |
| CLI directory → loader → validators → runner → exit | Full validators exist separately | CL-01 discards load errors and skips suite semantics | Mixed valid/broken discovery and zero-case suites fail closed |
| Repeat run → session identifier → turn matching | Per-agent/tenant scope and per-session lock | CL-02 reuses suite/case names | Repeat/concurrent same-named suites preserve independent turn1 |
| Contract extract → schema comparator → exit | Required/property differences checked | CL-03 ignores other root constraints | Extra-property/type/enum/combinator tightening cannot exit0 |
| Test framework startup → child logs/readiness → teardown | All SDKs own process helper abstractions | DS-01/02/03: shared buffer race, blocked pipes and unreaped child | Verbose child, timeout, stubborn child, parallel stop under Linux/race |
| Helper manifest → npm consumer | Local file dev dependency/build succeeds | DS-04 incompatible published peer range | Clean strict-peer install of packed SDK+helper, both entry points |
| Package version → downloader → completed cache path | Archive checksum verification | DS-08 old/shared/incomplete binary accepted | Version A/B, concurrent/interrupted install; returned executable version exact |
| Tag → verification → artifact build → registry → smoke | Go race preflight; pinned workflow actions | DS-05/06/10: missing tag gates, prerelease promotion, stale/partial checks | Negative publish gates, rc channel metadata, complete exact-version manifest |
| Helm DSN → replica admission → runtime state | Guard requires DSN key or acknowledgment | DS-09 confuses shared tenancy with shared runtime | Empty DSN/secret/HPA cases; one-pod default; restart/rolling-update session behavior |
| Failing YAML case → shell strict mode → action outputs → CI reporter | JUnit file redirected before exit | DS-11 loses output path on failure | Intentional failure preserves status and report path/content |
| Docs/agent map → actual developer commands | Architecture map, Makefile, extensive guides | CL-04 stale current versions/platform jobs; CL-05 scans caches | Tracked onboarding doc, version checks and scoped doc scanner |

## Contracts not promoted into unsupported findings

Provider endpoints intentionally allow anonymous/provider-key access and attach a valid tenant principal best-effort; this is distinct from management authentication. No general auth-bypass finding was inferred from this design. Main server tenancy isolation tests already cover client tenant-header spoofing and registry fallback. Graph execution has no located explicit fan-in aggregation contract; document or reject unsupported fan-in before extending it. MCP session-specific notification behavior needs a separate pinned-protocol test before it becomes a release finding.

## Integration release decision

The critical path is SR-01 containment, SR-05 patched artifact builds, CL-01/03 trustworthy gates, AR-03 state commit, AR-01/02/07 task-store lifecycle, DS-01–05 package lifecycle/publication, then metric/protocol/install validation. Coordinate SR-02 with DS-07, AR-01/02 with AR-07, AR-05/06 with AR-08, and DS-04/08/10 with DS-05; independently correct-looking fixes can otherwise break the neighboring component.
