# Configuration reference

Every knob MockAgents reads from the environment, in one table per area, with
its default and the flag that overrides it where one exists. Flags win over
environment variables; environment variables win over defaults.

Anything not listed here is not a configuration surface — if you find a
`MOCKAGENTS_*` name in a blog post that is missing below, it does not exist in
this version.

## Server

| Variable | Flag | Default | Effect |
| --- | --- | --- | --- |
| `MOCKAGENTS_HOST` | `--host` | `127.0.0.1` | Bind address. `0.0.0.0` exposes the server to the network; do that only with `MOCKAGENTS_MULTI_TENANT=1`, because single-tenant mode has no authentication. |
| `MOCKAGENTS_PORT` | `--port` | `8080` | Listen port. Must be 1–65535; an invalid value fails startup. |
| `MOCKAGENTS_AGENTS_DIR` | `--agents-dir` | `./agents` | Directory of agent, pipeline, MCP and A2A definitions. |
| `MOCKAGENTS_LOG_LEVEL` | `--log-level` | `info` | `debug`, `info`, `warn`, `error`. |
| `MOCKAGENTS_DATA_DIR` | — | working directory | Where the three SQLite files live. Set it whenever the working directory is read-only (containers). |
| `MOCKAGENTS_CORS_ORIGINS` | `--cors-origins` | any origin single-tenant, loopback GUI origins multi-tenant | Comma-separated browser origins allowed by CORS. |
| `MOCKAGENTS_ENGINE_ENDPOINT` | `--engine-endpoint` | `0` | Mounts `POST /v1/engines/process`, the generic test endpoint. It is unauthenticated and unmetered, so leave it off outside a test harness. |

## Shutdown

| Variable | Default | Effect |
| --- | --- | --- |
| `MOCKAGENTS_SHUTDOWN_TIMEOUT` | `20s` | How long in-flight requests may run after SIGTERM before they are cancelled. Kept inside Kubernetes' 30-second grace period by default. |
| `MOCKAGENTS_SHUTDOWN_DRAIN_DELAY` | `0` (immediate) | How long to keep serving, with readiness reporting `draining`, before the listeners close. Gives a load balancer time to stop routing new connections. The Helm chart supplies this window with a preStop sleep instead. |

## Interaction log and audit trail

| Variable | Default | Effect |
| --- | --- | --- |
| `MOCKAGENTS_LOG_BODIES` | `full` | `full` stores request/response bodies verbatim, `sanitized` masks them, `none` drops them but keeps per-agent grouping. Anything else is treated as `full`. |
| `MOCKAGENTS_LOG_MAX_ROWS` | `0` (unlimited) | Keeps only the newest N interaction rows; a background pruner enforces it. |
| `MOCKAGENTS_AUDIT_MAX_ROWS` | `0` (unlimited) | Same retention bound for the audit log. |
| `MOCKAGENTS_PRICING` | unset | Path to a YAML file of per-model prices that overrides the built-in cost table. |

## Multi-tenancy and authentication

These matter only when `MOCKAGENTS_MULTI_TENANT=1`. Single-tenant mode is an
unauthenticated local-development tool.

| Variable | Default | Effect |
| --- | --- | --- |
| `MOCKAGENTS_MULTI_TENANT` | `0` | Turns on API keys, roles, tenants, quotas and the audit trail of denials. |
| `MOCKAGENTS_TENANCY_DSN` | unset (SQLite) | Postgres connection string. Set it whenever more than one replica shares state. |
| `MOCKAGENTS_BOOTSTRAP_KEY` | unset | Supplies the platform key's plaintext instead of having one generated, so it can come from a Secret. |
| `MOCKAGENTS_BOOTSTRAP_KEY_FILE` | `<data dir>/bootstrap-admin.key` | Where a generated platform key is written on first boot. |
| `MOCKAGENTS_AUTH_FAILURES_PER_MINUTE` | `30` | Failed authentications per client IP before `429`. `0` disables the limiter. |
| `MOCKAGENTS_TRUSTED_PROXIES` | unset | Comma-separated CIDRs whose `X-Forwarded-For` is believed. Without it the direct peer address is the client IP. |

## Quotas

| Variable | Default | Effect |
| --- | --- | --- |
| `MOCKAGENTS_DEFAULT_RATE_PER_SEC` | `0` (off) | Per-tenant request rate. |
| `MOCKAGENTS_DEFAULT_RATE_BURST` | `0` | Burst allowance on top of the rate. |
| `MOCKAGENTS_DEFAULT_MONTHLY_SPEND_USD` | `0` (off) | Per-tenant monthly simulated spend cap; over it, requests get `402`. |

Per-tenant overrides are set through `PUT /api/v1/tenants/{id}/quota` and
persist in the tenancy store, so they survive restarts and apply across
replicas.

## SSO (OIDC)

Setting `MOCKAGENTS_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET` and
`_REDIRECT_URL` together enables SSO. A partial or invalid configuration fails
startup rather than silently disabling login.

| Variable | Default | Effect |
| --- | --- | --- |
| `MOCKAGENTS_OIDC_ISSUER` | unset | Provider issuer URL. |
| `MOCKAGENTS_OIDC_CLIENT_ID` | unset | Relying-party client id. |
| `MOCKAGENTS_OIDC_CLIENT_SECRET` | unset | Relying-party secret. |
| `MOCKAGENTS_OIDC_REDIRECT_URL` | unset | Callback URL; must point at this server's `/auth/callback`. |
| `MOCKAGENTS_OIDC_DOMAIN_MAP` | unset | `example.com=tenant-a,acme.io=tenant-b` — maps a verified email domain to a tenant. |
| `MOCKAGENTS_OIDC_DEFAULT_ROLE` | `viewer` | Role for just-in-time provisioned users. `viewer`, `editor` or `admin`; the platform role is never assignable this way. |
| `MOCKAGENTS_OIDC_SESSION_TTL` | `24h` | Session lifetime. Needs a unit: `24h`, not `24`. |
| `MOCKAGENTS_OIDC_SECURE_COOKIES` | `0` | Marks the session cookie `Secure`. Turn it on for any deployment reached over HTTPS. |
| `MOCKAGENTS_OIDC_ALLOW_UNVERIFIED_EMAIL` | `0` | Accepts an ID token whose `email_verified` is false. Leave it off unless your provider genuinely cannot set the claim. |

## Engine behavior

| Variable | Flag | Default | Effect |
| --- | --- | --- | --- |
| `MOCKAGENTS_CHAOS_OFF` | `--chaos-off` | `0` | Disables every configured chaos block, fleet-wide. |
| `MOCKAGENTS_CHAOS_RATE` | `--chaos-rate` | unset | Lowest-precedence server-wide chaos rate, `0.0`–`1.0`. |
| `MOCKAGENTS_CHAOS_SEED` | `--chaos-seed` | unset | Fixed seed, so injected faults repeat run to run. |
| `MOCKAGENTS_STRICT_TOOLS` | — | `off` | Fleet default for strict tool validation: `off`, `warn`, `strict`. Per-agent `spec.behavior.strict_tools` overrides it. |
| `MOCKAGENTS_REALTIME_STRICT` | — | `0` | Rejects Realtime API events that the real service would reject. |
| `MOCKAGENTS_SESSION_MAX` | — | `100000` | Live conversation sessions kept in memory before the oldest are evicted. |
| `MOCKAGENTS_SESSION_HISTORY` | — | `256` | Messages retained per session. |

## Tracing

| Variable | Default | Effect |
| --- | --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | Enables the OTLP/HTTP exporter and points it at your collector. |
| `MOCKAGENTS_OTEL_STDOUT` | `0` | Enables the stdout exporter instead — spans printed to the process's stdout, for local work. |
| `OTEL_SERVICE_NAME` | `mockagents` | Service name attached to every span. |

With neither exporter variable set, the tracer provider is a no-op and the HTTP
layer is not even wrapped, so tracing costs nothing. See
[Observability](../guides/observability.md).

## Client-side (the CLI talking to a running server)

| Variable | Flag | Default | Effect |
| --- | --- | --- | --- |
| `MOCKAGENTS_SERVER` | `--server` | `http://localhost:8080` | Base URL used by `mockagents add` / `mockagents rm`. |
| `MOCKAGENTS_API_KEY` | `--api-key` | unset | API key those commands send to a multi-tenant server. |
| `NO_COLOR` | — | unset | Any value disables colored CLI output. |
