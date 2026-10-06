import Link from "next/link";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import {
  AgentSummary,
  APIError,
  can,
  deleteAgentByName,
  getBaseUrl,
  getHealth,
  getIdentity,
  listAgents,
  type Identity,
} from "@/lib/api";
import { withErrorCode } from "@/lib/actionError";
import { Icon } from "@/lib/icons";
import { apiUrlVisible } from "@/lib/serverState";
import { AgentCatalog } from "./AgentCatalog";
import { ErrorBanner } from "./ErrorBanner";
import { Stat } from "./Stat";

// What `?deleted=` may contain: an agent's metadata.name. Anything else is not
// echoed, so a crafted link cannot put its own text in the success banner.
const AGENT_NAME = /^[a-z0-9][a-z0-9-]{0,62}$/;

type PageProps = {
  searchParams: Promise<{ error?: string; deleted?: string }>;
};

export default async function HomePage({ searchParams }: PageProps) {
  const { error: deleteError, deleted } = await searchParams;

  // Server action submitted by the confirmation dialog's form. The outcome
  // travels back in the query string rather than in client state, because the
  // dialog unmounts with the card on success — the same pattern the tenants
  // page uses. Only a short error code goes in the URL, mapped to fixed copy on
  // render (K-18): no bodies, no credential, no free text.
  async function deleteAction(formData: FormData): Promise<void> {
    "use server";
    const name = String(formData.get("name") ?? "");
    if (!name) redirect(withErrorCode("/", "name_required"));
    const r = await deleteAgentByName(name);
    if (!r.ok) redirect(withErrorCode("/", r.code ?? "request_failed"));
    revalidatePath("/");
    redirect("/?deleted=" + encodeURIComponent(name));
  }

  let agents: AgentSummary[] = [];
  let error: string | null = null;
  try {
    agents = await listAgents();
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }
  const health = await getHealth();

  // U1-2: offer a write control only when the server has not said it would be
  // refused. If identity cannot be read at all we leave the controls enabled
  // and let the server answer — locking an operator out over a failed probe is
  // worse than an honest 403.
  let canWrite = true;
  let anonymous = false;
  let identity: Identity | null = null;
  try {
    identity = await getIdentity();
    if (identity) {
      canWrite = can(identity, "agents.write");
      anonymous = identity.mode === "local";
    }
  } catch {
    canWrite = true;
  }
  // With one agent left and no tenant scoping the engine uses the survivor as
  // the default for every request (engine.go resolveAgent), so deleting one of
  // two turns a would-be failure into a silent misroute. The dialog says so.
  const singleAgentFallback = anonymous && agents.length === 2;

  // 6.6: the upstream address is not shown to an anonymous visitor of a
  // multi-tenant deployment.
  const host = apiUrlVisible(identity, health === null, process.env.NODE_ENV !== "production")
    ? getBaseUrl().replace(/^https?:\/\//, "")
    : null;

  const totalScenarios = agents.reduce((s, a) => s + a.scenario_count, 0);
  const totalTools = agents.reduce((s, a) => s + a.tool_count, 0);

  return (
    <div>
      <div className="page-head">
        <div className="head-row">
          <div className="grow">
            <h1 className="page-title">Agent catalog</h1>
            <p className="page-lede">
              Every agent loaded by the running MockAgents server
              {host ? (
                <>
                  {" "}
                  at <code>{host}</code>
                </>
              ) : null}
              . Click an
              agent to inspect its scenarios, tools, and chaos config.
            </p>
          </div>
          <div className="row gap-2">
            {canWrite ? (
              <Link href="/editor" className="btn btn-default btn-sm">
                <Icon name="plus" size={15} /> New agent
              </Link>
            ) : (
              <span
                className="btn btn-default btn-sm"
                aria-disabled="true"
                title="Creating agents needs the editor role. Your credential does not have it."
                style={{ opacity: 0.55, pointerEvents: "none" }}
              >
                <Icon name="plus" size={15} /> New agent
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Outcome of the last delete. Failure is assertive — the operator just
          pressed a destructive button and needs to know it did not land. */}
      <ErrorBanner code={deleteError} title="Not deleted." />
      {deleted && AGENT_NAME.test(deleted) && (
        <div className="banner banner-ok" role="status">
          <strong>Deleted.</strong> <code className="mono">{deleted}</code> has stopped
          serving.
        </div>
      )}

      {error && (
        <div className="banner banner-error">
          <strong>MockAgents server unreachable.</strong> {error}
          <span className="hint">
            Start it with <code>mockagents start --agents-dir ./agents</code>, or set{" "}
            <code>MOCKAGENTS_API_URL</code> when launching the GUI.
          </span>
        </div>
      )}

      {!error && (
        <>
          <div className="grid grid-4 mb-6">
            <Stat icon="bot" label="Agents loaded" value={String(agents.length)} sub="from the agents directory" />
            <Stat icon="list-tree" label="Scenarios" value={String(totalScenarios)} sub="total match rules" />
            <Stat icon="wrench" label="Tools" value={String(totalTools)} sub="simulated tool calls" />
            <Stat
              icon="circle-dot"
              label="Server"
              value={health ? "online" : "offline"}
              sub={health?.version ? `v${health.version}` : (host ?? undefined)}
            />
          </div>

          {agents.length === 0 ? (
            <div className="empty">
              No agents loaded. Add a YAML file to the agents directory and reload.
            </div>
          ) : (
            <AgentCatalog
              agents={agents}
              deleteAction={deleteAction}
              canWrite={canWrite}
              singleAgentFallback={singleAgentFallback}
            />
          )}
        </>
      )}
    </div>
  );
}
