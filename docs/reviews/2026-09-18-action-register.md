# Review action register: developer handoff

Date: 2026-09-18. Owners are suggested areas, not assigned people.

| ID | Severity | Status | Owner area | Finding | Evidence | Recommended action | Validation expected | Dependencies |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| D-01 | P2 | Open | API/docs | Partial OpenAPI coverage | Route catalog vs api-spec paths | Add omitted operations and machine-readable route coverage check | Every built-in operation represented or explicit scoped exclusion | Decide provider-subset schema strategy |
| D-02 | P2 | Open | Server/providers | Ollama/Bedrock omitted from shared quota/capture/metrics classifiers | Mounts + quota_middleware.go + log/metrics predicates | Decide intended telemetry/quota coverage, align classifier and docs | HTTP requests to each intended provider produce expected logs/metrics and tenant quota denials; excluded surfaces remain explicit | Product quota policy |
| D-03 | P2 | Completed | Config docs | Misleading fleet-wide chaos-off claim | start.go inherited rate + explicit engine trigger branches | Configuration reference corrected | Existing chaos/CLI tests pass; prose matches precedence guide | None |
| D-04 | P2 | Open | Schema/config | No standalone A2A/Search schemas | Types/loaders/validators vs schema directory | Add schemas and parity tests where supported tooling needs them | Valid/invalid fixtures agree across Go validators and schema | Supported schema coverage policy |
| D-05 | P3 | Completed | Docs/GUI | Next.js major-version drift | gui/package.json vs overview prose | README, architecture and GUI overview corrected | Compare manifest and updated text; links checked | None |
| D-06 | P3 | Open | Engine/tenancy/quota | Stale tenant-header and persistence comments | types/agent.go, tenancy/types.go, quota/quota.go introductions | Reconcile comments with current scope/backend wiring | Source review and no behavioral change | None |
| D-07 | P2 | Completed | API/docs | Auth introduction omitted probes and optional provider principal | OpenAPI intro vs open_routes/AuthMiddleware | Introduction corrected, detailed handoff provided | Auth/server Go suites pass; compare exempt route semantics | None |
| D-08 | P2 | Completed | README/examples | Pipeline example names a nonexistent `research` pipeline | examples/research-pipeline.yaml declares `research-pipeline` | Correct README and handoff URL | Live HTTP smoke returns the expected two-node trajectory | None |

## Status summary

| Status | Count |
| --- | ---: |
| Open | 4 |
| Completed | 4 |

Optional future architecture directions (shared session/catalog storage, bounded dynamic template caches, graph joins/retries) are discussed in [trade-offs](../handoff/design-tradeoffs.md). They are not promoted to defects without a requirement and focused reproduction.
