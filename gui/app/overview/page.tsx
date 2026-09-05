import Link from "next/link";

import {
  APIError,
  getIdentity,
  getServerStatus,
  listAgents,
  listLogs,
  listPipelineInventory,
  type Identity,
} from "@/lib/api";
import { Icon } from "@/lib/icons";
import {
  buildChecklist,
  notReadyReason,
  readinessLabel,
  type ChecklistItem,
  type ServerStatus,
} from "@/lib/serverState";

import { CopyField } from "./CopyField";

export const dynamic = "force-dynamic";

// UX-02: trustworthy shell and onboarding.
//
// The rule that shapes this page: nothing here is inferred from an empty list.
// A catalog that could not be READ is not an empty install; a server that is up
// but not ready is not offline; a readiness that cannot be checked is unknown,
// not false. Each of those is a different thing to do next, so each gets its
// own state.
export default async function OverviewPage() {
  const status = await getServerStatus();
  const apiUrl = process.env.MOCKAGENTS_API_URL ?? "http://localhost:8080";

  let identity: Identity | null = null;
  try {
    identity = await getIdentity();
  } catch {
    identity = null;
  }

  // Each read is independent: one failing must not make the others look empty.
  let agentCount = 0;
  let catalogReadable = true;
  let catalogError: string | null = null;
  try {
    agentCount = (await listAgents()).length;
  } catch (err) {
    catalogReadable = false;
    catalogError =
      err instanceof APIError ? `The server returned ${err.status}.` : "The server could not be reached.";
  }

  // X-2: "this server cannot have pipelines" and "it has none" lead to
  // different next steps, so they are tracked separately.
  let pipelineCount = 0;
  let pipelinesReadable = true;
  let pipelinesSupported = true;
  try {
    const listing = await listPipelineInventory();
    pipelinesSupported = listing.supported;
    if (listing.supported) pipelineCount = listing.pipelines.length;
  } catch {
    pipelinesReadable = false;
  }

  let hasTraffic = false;
  let logsReadable = true;
  try {
    hasTraffic = (await listLogs({ limit: 1 })).length > 0;
  } catch {
    logsReadable = false;
  }

  const checklist = buildChecklist({
    status,
    agentCount,
    pipelineCount,
    hasTraffic,
    catalogReadable,
  });
  const done = checklist.filter((c) => c.done).length;

  return (
    <div>
      <div style={{ padding: "20px 0 0" }}>
        <div className="head-row page-head">
          <div className="grow">
            <h1 className="page-title">Overview</h1>
            <p className="page-lede">
              Readiness and liveness are verified separately. Nothing on this page is
              inferred from an empty list.
            </p>
          </div>
          <span className="sid">UX-02 · GET /api/v1/health, /api/v1/ready</span>
        </div>

        <NotReadyBanner status={status} />

        <ReceiptGrid status={status} identity={identity} apiUrl={apiUrl} />

        <div
          className="grid mt-4"
          style={{ gridTemplateColumns: "1.25fr .75fr", alignItems: "start", gap: 16 }}
        >
          <div className="col gap-4">
            <div className="card">
              <div className="card-head">
                <h3>First-run checklist</h3>
                <span className="muted txt-sm">
                  {done} of {checklist.length} complete
                </span>
              </div>
              {checklist.map((item) => (
                <ChecklistRow key={item.id} item={item} />
              ))}
            </div>

            <RecentActivity readable={logsReadable} hasTraffic={hasTraffic} />
          </div>

          <div className="col gap-4">
            <StarterCard
              apiUrl={apiUrl}
              agentCount={agentCount}
              catalogReadable={catalogReadable}
              catalogError={catalogError}
            />
            <PipelinesCard
              readable={pipelinesReadable}
              supported={pipelinesSupported}
              count={pipelineCount}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

function NotReadyBanner({ status }: { status: ServerStatus }) {
  const reason = notReadyReason(status);
  if (!reason) return null;
  return (
    <div className="banner banner-warn mb-4" role="alert">
      <div>
        <strong>Process is up but not ready. </strong>
        <span className="mono">{reason}</span> — catalog reads may be unavailable.{" "}
        <strong>This is not an empty install.</strong>
      </div>
    </div>
  );
}

function ReceiptGrid({
  status,
  identity,
  apiUrl,
}: {
  status: ServerStatus;
  identity: Identity | null;
  apiUrl: string;
}) {
  const live = status.liveness === "process-up";
  return (
    <div className="receipt-grid">
      <div className="rc">
        <div className="l">Server</div>
        <div className="v">
          <span className="mono">{apiUrl}</span>
        </div>
        <div className="s">
          {identity?.mode === "local"
            ? "local mode · unauthenticated (explicit)"
            : identity
              ? `authenticated · ${identity.tenant_id ?? "unknown tenant"}`
              : "identity unknown"}
        </div>
      </div>

      <div className="rc">
        <div className="l">Liveness</div>
        <div className="v" style={{ color: live ? "var(--sr-success-fg)" : "var(--sr-danger-fg)" }}>
          <span className={"sdot " + (live ? "ok" : "bad")} aria-hidden="true" />
          {live ? "Process up" : "Unreachable"}
        </div>
        <div className="s">checked {status.checkedAt.slice(11, 19)}Z</div>
      </div>

      <div className="rc">
        <div className="l">Readiness</div>
        <div
          className="v"
          style={{
            color:
              status.readiness === "ready"
                ? "var(--sr-success-fg)"
                : status.readiness === "not-ready"
                  ? "var(--sr-warning-fg)"
                  : undefined,
          }}
        >
          {status.readiness !== "unknown" && (
            <span
              className={"sdot " + (status.readiness === "ready" ? "ok" : "warn")}
              aria-hidden="true"
            />
          )}
          {readinessLabel(status.readiness) === "READY"
            ? "Ready"
            : status.readiness === "not-ready"
              ? "Not ready"
              : "Unknown"}
        </div>
        <div className="s">
          {status.readiness === "unknown"
            ? "cannot verify while unreachable"
            : status.checks.length > 0
              ? status.checks.map((c) => `${c.name}: ${c.status}`).join(" · ")
              : "no checks reported"}
        </div>
      </div>

      <div className="rc">
        <div className="l">Configuration revision</div>
        <div className="v">
          <span className="mono">per-resource</span>
        </div>
        <div className="s">no global revision — shown on each mock</div>
      </div>
    </div>
  );
}

function ChecklistRow({ item }: { item: ChecklistItem }) {
  return (
    <div className="check-row">
      <span className={"check-mark" + (item.done ? " done" : "")} aria-hidden="true">
        {item.done ? "✓" : ""}
      </span>
      <span className={item.done ? "done-label" : undefined}>
        {item.label}
        {/* Screen readers get the state as text, not as a tick glyph. */}
        <span className="sr-only">{item.done ? " — complete" : " — not complete"}</span>
      </span>
      <span style={{ marginLeft: "auto" }}>
        {item.unknown ? (
          <span className="muted txt-xs">{item.unknown}</span>
        ) : item.meta ? (
          <span className="mono muted txt-xs">{item.meta}</span>
        ) : item.action ? (
          <Link href={item.action.href} className="btn btn-ghost btn-sm">
            {item.action.label}
          </Link>
        ) : null}
      </span>
    </div>
  );
}

function StarterCard({
  apiUrl,
  agentCount,
  catalogReadable,
  catalogError,
}: {
  apiUrl: string;
  agentCount: number;
  catalogReadable: boolean;
  catalogError: string | null;
}) {
  return (
    <div className="card">
      <div className="card-head">
        <h3>Point an SDK at this server</h3>
        <span className="sid">UX-02</span>
      </div>
      <div className="card-pad col gap-3">
        <CopyField label="base URL" value={`${apiUrl}/v1`} />
        <CopyField
          label="api key"
          value="mock-any-string"
          hint="Any string. This server ignores provider credentials by design."
        />
        {!catalogReadable ? (
          <div className="banner banner-error" role="alert">
            <strong>The agent catalog could not be read.</strong> {catalogError} This is not
            an empty install — the number of agents is unknown.
          </div>
        ) : agentCount === 0 ? (
          <div className="banner banner-warn" role="note">
            <strong>No agents loaded.</strong> Add a YAML file to the agents directory, or
            create one in the editor.
          </div>
        ) : (
          <p className="txt-sm muted">
            {agentCount} agent definition{agentCount === 1 ? "" : "s"} loaded and serving.
          </p>
        )}
        <div className="row gap-2">
          <Link href="/" className="btn btn-outline btn-sm">
            <Icon name="bot" size={15} /> Open catalog
          </Link>
          <Link href="/editor" className="btn btn-outline btn-sm">
            <Icon name="file-code" size={15} /> Editor
          </Link>
        </div>
      </div>
    </div>
  );
}

function PipelinesCard({
  readable,
  supported,
  count,
}: {
  readable: boolean;
  supported: boolean;
  count: number;
}) {
  return (
    <div className="card">
      <div className="card-head">
        <h3>Pipelines</h3>
        <span className="sid">UX-02 · empty state</span>
      </div>
      <div className="card-pad">
        {!readable ? (
          <div className="banner banner-error" role="alert">
            <strong>Pipelines could not be read.</strong> This is unknown, not zero.
          </div>
        ) : !supported ? (
          // Not an empty inventory: the routes are not mounted on this server.
          <div className="banner banner-info" role="note">
            <strong>Pipelines are not enabled on this server. </strong>
            The routes mount only when it starts with a <code>kind: Pipeline</code>{" "}
            document. This is an absent capability, not an empty list.
          </div>
        ) : count > 0 ? (
          <div className="col gap-3">
            <p className="txt-sm">
              {count} pipeline{count === 1 ? "" : "s"} registered.
            </p>
            <Link href="/pipelines" className="btn btn-outline btn-sm">
              Open pipelines
            </Link>
          </div>
        ) : (
          <div className="col gap-3">
            <div className="banner banner-warn" role="note">
              <strong>Pipeline creation is not part of Release A. </strong>
              Seed one from the documented starter to enable runs.
            </div>
            <pre className="codeblock">{`cp examples/research-pipeline.yaml agents/
mockagents start --agents-dir agents`}</pre>
          </div>
        )}
      </div>
      {/* The design is explicit: a Run control only appears once a pipeline
          exists, so there is never a dead button to press. */}
      <div className="card-pad prov" style={{ borderTop: "1px solid var(--sr-border)" }}>
        A Run control appears only when a pipeline exists — never a dead button.
      </div>
    </div>
  );
}

function RecentActivity({ readable, hasTraffic }: { readable: boolean; hasTraffic: boolean }) {
  return (
    <div className="card">
      <div className="card-head">
        <h3>Recent activity</h3>
        <span className="sid">UX-02 · UX-04</span>
      </div>
      <div className="card-pad">
        {!readable ? (
          <div className="banner banner-error" role="alert">
            <strong>Interaction logs could not be read.</strong> Whether any requests have
            been served is unknown — not none.
          </div>
        ) : hasTraffic ? (
          <div className="col gap-3">
            <p className="txt-sm">This server has captured interactions.</p>
            <Link href="/logs" className="btn btn-outline btn-sm">
              Investigate requests
            </Link>
          </div>
        ) : (
          <div className="empty">
            No interactions captured yet. Send a request to this server and it will appear
            in the logs.
          </div>
        )}
      </div>
    </div>
  );
}
