# Internal dependency map

Generated from non-test Go imports. These are direct package dependencies, not runtime network calls. Third-party versions remain in [go.mod](../../../go.mod); SDK and GUI dependencies remain in their package manifests.

| Package | Direct internal / SDK Go imports |
| --- | --- |
| `cmd/mockagents` | `internal/a2a`, `internal/audit`, `internal/cli`, `internal/clientip`, `internal/config`, `internal/contract`, `internal/conversion`, `internal/drift`, `internal/engine/state`, `internal/engine`, `internal/mcp`, `internal/mcpadmin`, `internal/metrics`, `internal/observability`, `internal/oidcauth`, `internal/pricing`, `internal/quota`, `internal/recording`, `internal/runner`, `internal/server`, `internal/storage`, `internal/tenancy`, `internal/types`, `internal/vector` |
| `docs/reviews/security-probe` | `internal/engine/state`, `internal/engine`, `internal/metrics`, `internal/recording`, `internal/server`, `internal/tenancy` |
| `internal/a2a` | `internal/chaos`, `internal/types` |
| `internal/adapter` | `internal/chaos`, `internal/engine`, `internal/realtime`, `internal/streaming`, `internal/types`, `internal/vector` |
| `internal/audit` | `internal/clientip` |
| `internal/chaos` |  |
| `internal/cli` |  |
| `internal/clientip` |  |
| `internal/config` | `internal/types`, `internal/vector` |
| `internal/contract` | `internal/types` |
| `internal/conversion` | `internal/config`, `internal/types` |
| `internal/drift` |  |
| `internal/engine` | `internal/chaos`, `internal/engine/state`, `internal/metrics`, `internal/observability`, `internal/toolschema`, `internal/types` |
| `internal/engine/state` |  |
| `internal/mcp` | `internal/chaos`, `internal/toolschema`, `internal/types` |
| `internal/mcpadmin` | `internal/config`, `internal/engine`, `internal/mcp`, `internal/types` |
| `internal/metrics` |  |
| `internal/observability` |  |
| `internal/oidcauth` |  |
| `internal/pricing` |  |
| `internal/quota` |  |
| `internal/realtime` | `internal/engine`, `internal/types` |
| `internal/recording` | `internal/storage` |
| `internal/runner` | `internal/engine`, `internal/types` |
| `internal/server` | `internal/adapter`, `internal/audit`, `internal/config`, `internal/engine`, `internal/metrics`, `internal/observability`, `internal/oidcauth`, `internal/pricing`, `internal/quota`, `internal/storage`, `internal/streaming`, `internal/tenancy`, `internal/types`, `internal/vector` |
| `internal/storage` |  |
| `internal/streaming` | `internal/engine`, `internal/types` |
| `internal/tenancy` | `internal/clientip`, `internal/quota` |
| `internal/toolschema` | `internal/types` |
| `internal/types` |  |
| `internal/vector` | `internal/chaos` |
| `sdk/go/mockagents` | `internal/adapter`, `internal/config`, `internal/engine/state`, `internal/engine` |
| `tools/benchguard` |  |
| `tools/benchreport` |  |
| `tools/coheredriftscrub` |  |
| `tools/doccheck` |  |
| `tools/driftcheck` | `internal/adapter`, `internal/audit`, `internal/quota`, `internal/server`, `internal/storage`, `internal/streaming`, `internal/tenancy`, `internal/types` |
| `tools/handoffcatalog` |  |
| `tools/liquidcheck` |  |
| `tools/openaimoderationdriftscrub` |  |
| `tools/tavilydriftscrub` |  |
