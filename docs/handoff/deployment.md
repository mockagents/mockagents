# Deployment and operations handoff

Use [configuration reference](../../site/docs/reference/configuration.md), [operations guide](../../site/docs/guides/operations.md), [Helm README](../../deploy/helm/mockagents/README.md), and [release runbook](../RELEASING.md) for detailed supported knobs. This page explains their implementation consequences. It is not evidence that a public release or registry account is configured.

## Local and container topology

`mockagents start` defaults to loopback. `mockagents start --agents-dir examples` loads validated agents and optional other document kinds, initializes local stores, and starts the main HTTP listener. It requires at least one valid Agent; invalid documents are reported and may be skipped when valid agents remain. Run `validate` separately when every fixture must pass.

[Dockerfile](../../Dockerfile) builds the Go binary with the pinned toolchain and `CGO_ENABLED=0`, then runs it as a non-root user. `/agents` holds fixture files and `/data` is the writable working directory and data volume. The default container binds `0.0.0.0:8080`; network reachability is therefore different from the local CLI default. The GUI is not embedded in this container.

[Compose](../../docker-compose.yml) mounts `./agents` read-only and persists `/data`. Read-only fixtures allow serving but prevent successful persisted edits to those files. A write error is not a successful runtime-only save. For authoring deployments, deliberately provide a writable fixture directory; for immutable deployments, use versioned fixtures and hide/avoid edit workflows accordingly.

### Listener defaults

| Mode | Default bind | Address control / authentication |
| --- | --- | --- |
| `start` | `127.0.0.1:8080` | `--host`, `--port`; optional management tenancy middleware |
| `mcp --transport http` | `127.0.0.1:8081` | `--bind`, `--port`; separate listener, no main-server tenancy middleware |
| `a2a` | `:8083` (all interfaces) | `--port` only; no bind flag or main-server tenancy middleware |
| `record`, `replay` | `127.0.0.1:8080` | `--bind`, `--port`; separate listener; recording can forward upstream credentials |
| Default Docker command | `0.0.0.0:8080` | Explicit `start --host 0.0.0.0`; external access also depends on port publishing/network policy |

These defaults are verified in [start.go](../../cmd/mockagents/start.go), [mcp.go](../../cmd/mockagents/mcp.go), [a2a.go](../../cmd/mockagents/a2a.go), [record.go](../../cmd/mockagents/record.go), and [replay.go](../../cmd/mockagents/replay.go). Do not infer A2A network isolation from the main CLI's loopback default.

## Storage and scaling

`MOCKAGENTS_DATA_DIR` selects local state location. Interaction, audit and default tenancy SQLite stores are separate. Back up them and the fixture directory using the supported operations procedure, accounting for live SQLite/WAL consistency. Provider memory state and engine conversations are not restored by these backups. Review cassette contents independently before sharing them.

The Helm chart defaults to one replica. A replica count above one, or an autoscaling range that can exceed one, requires `multiReplica.acknowledged=true`; merely enabling an autoscaler capped at one does not trigger that guard. A Postgres tenancy DSN alone does not bypass the guard. The chart also rejects `persistence.enabled=true` whenever multiple replicas are requested, even after acknowledgment, because its single claim is not a shared multi-writer SQLite design. Evidence: [deployment template guards](../../deploy/helm/mockagents/templates/deployment.yaml).

Each pod still owns its registry, sessions, provider resources, interaction/audit data and rate buckets. An edit on one pod is not an atomic fleet update. Postgres shares durable tenancy credentials, quota settings and spend. Quota overrides load at startup; a quota PUT changes only the receiving process's live override map. Other running replicas keep their prior limits until restarted/reconfigured. Spend totals use a separate 5-second cache and post-response accrual; a backend read failure uses the last-known total, and a failed increment falls back locally. This is not a strict transactional billing reservation. Evidence: [startup loading](../../cmd/mockagents/start.go), [quota handler](../../internal/server/quota_handlers.go), [enforcer](../../internal/quota/quota.go).

ConfigMap fixture mounts are read-only. The chart supplies service/optional ingress, non-root/read-only-rootfs contexts, resource settings, health/readiness probes, and optional network policy, PDB, HPA and ServiceMonitor. Evaluate these features with the intended replica/persistence model rather than interpreting their existence as evidence of distributed consistency.

## Health, observability and shutdown

Liveness (`/api/v1/health`) answers whether the process responds. Readiness (`/api/v1/ready`) checks fixture availability, the configured interaction store, and draining state. Readiness does not prove every external integration or tenant resource is usable. `/metrics` exposes process-wide metrics and requires a platform credential in multi-tenant mode; configure a dedicated scrape credential through the chart's supported secret reference.

OTel is opt-in via OTLP endpoint or local stdout exporter configuration. Interaction body policy can be full, sanitized or none; row retention is separately configurable. Body sanitization, cassette redaction, structured logs and console exports are different paths. Track log queue/drop behavior if evidence completeness matters.

Shutdown can first mark readiness as draining, wait the configured delay, then drain requests within the timeout and cancel remaining work. The default main timeout is 20 seconds. Helm's default preStop sleep is 5 seconds inside a 30-second grace period. Keep those budgets consistent with the longest expected stream and storage flush, and distinguish the chart's sleep from an additional configured server drain delay.

## Authentication and recovery

Multi-tenant startup initializes tenancy and bootstrap platform credentials; supply secrets through supported operator configuration and follow [operations](../../site/docs/guides/operations.md) for recovery. Never assume a key can be read back from the database: only hash/prefix is retained. Rotation replaces credential material, so clients need the new one-time plaintext.

OIDC setup runs only in multi-tenant mode. All four issuer/client ID/client secret/redirect values must be nonempty before `buildSSO` enables it. If any is absent, startup succeeds with SSO disabled and its routes unmounted. Once all four are present, a missing domain map, invalid default role/duration/boolean, or provider initialization failure can fail startup. HTTPS redirect URLs automatically select Secure cookies; the explicit secure-cookie setting can also enable them. Check `/auth/login` using a valid management credential when diagnosing an unmounted route, since an unauthenticated request can be rejected by the outer auth middleware first. Evidence: [buildSSO](../../cmd/mockagents/start.go#L764).

Management RBAC does not make provider routes require a MockAgents credential, and standalone MCP/A2A/record listeners do not inherit main-server protections. Recording can forward a configured upstream credential for anyone who reaches that listener. Scope listener access to the intended test environment. These are actual product boundaries, not interchangeable deployment modes.

## Release handoff

The release workflow verifies a candidate, checks publishing prerequisites, builds a commit-keyed artifact bundle once, then publishes verified artifacts to enabled channels. Package versions must match the binary tag. Binaries/checksums, Python distributions, npm packages and container images are separate release outputs; stable Homebrew publication is macOS-specific. Retries must consume the same prepared candidate and must not replace immutable registry bytes.

Before a release, follow the complete exact-SHA gate in [RELEASING.md](../RELEASING.md) and [.github/workflows/verify.yml](../../.github/workflows/verify.yml), including SDK/GUI, security, container, Helm and install-channel checks. Passing the documentation checks or Go suite in this handoff is not release approval. No deployment, tag, registry publication, or external message is performed by this documentation work.

## Troubleshooting map

| Symptom | First evidence | Likely next action |
| --- | --- | --- |
| No valid agents / ready 503 | Startup diagnostics and fixture validation | Fix directory/kind/validation; vector-only definitions are insufficient |
| Edit fails in container | Write receipt/error and mount permissions | Use a deliberate writable fixture mount or update immutable fixtures |
| State disappears between requests | Replica routing, session ID, TTL and restart history | Correct session usage and topology assumptions |
| Managed request 401/403 | Identity response, current key role, auth audit | Supply valid credential/required role and correct owner |
| Provider request unexpectedly global | Credential resolution and visible model catalog | Use valid MockAgents key; do not trust a tenant header |
| Replay miss | Request hash/body/path and cassette match rules | Reproduce exact request or choose intended ignore fields |
| Logs absent or delayed | Body policy, async queue/drop counters, tenant filter | Separate missing capture from filtering/retention |
