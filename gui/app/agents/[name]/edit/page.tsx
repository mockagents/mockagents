import Link from "next/link";

import {
  APIError,
  can,
  getAgentSource,
  getIdentity,
  getPipeline,
  getServerStatus,
  listPipelines,
  saveAgentConditional,
  validateYAML,
  type ConditionalSaveResult,
  type ValidateResult,
} from "@/lib/api";

import { AgentEditor } from "./AgentEditor";

type PageProps = { params: Promise<{ name: string }> };

// UX-03 slice B. The page is a server component so the auth cookie never
// reaches the browser; every mutation goes through a server action.
export default async function EditAgentPage({ params }: PageProps) {
  const { name: raw } = await params;
  const name = decodeURIComponent(raw);

  let source;
  try {
    source = await getAgentSource(name);
  } catch (err) {
    // A server that cannot be reached is NOT an empty or missing agent, and
    // must not be rendered as one (epic §5).
    const status = err instanceof APIError ? err.status : 0;
    return (
      <div>
        <div className="breadcrumb">
          <Link href="/">Agents</Link> · Edit
        </div>
        <h1 className="page-title">Edit {name}</h1>
        <div className="banner banner-error" role="alert">
          <strong>Could not load this agent.</strong>{" "}
          {status === 401
            ? "Your session is no longer valid — sign in again."
            : status
              ? `The server returned ${status}.`
              : "The server could not be reached. It may be stopped."}{" "}
          Nothing has been changed.
        </div>
      </div>
    );
  }

  if (!source) {
    return (
      <div>
        <div className="breadcrumb">
          <Link href="/">Agents</Link> · Edit
        </div>
        <h1 className="page-title">Edit {name}</h1>
        <div className="empty">
          No agent named <code className="mono">{name}</code> is visible to you. It may have
          been deleted, or belong to another tenant.
        </div>
      </div>
    );
  }

  // Capability drives whether Apply is offered. The server still authorizes the
  // write — this only decides whether we show an action that would be refused.
  let canWrite = true;
  try {
    const identity = await getIdentity();
    // In local mode every capability is present; if identity is unavailable we
    // leave the control enabled and let the server refuse, rather than locking
    // an operator out because one probe failed.
    if (identity) canWrite = can(identity, "agents.write");
  } catch {
    canWrite = true;
  }

  // U3-5: liveness, so the write controls can be disabled with a reason instead
  // of failing on click. Memoized per render, so this shares the layout's probe.
  const status = await getServerStatus();
  const online = status.liveness === "process-up";

  // U3-3: which pipelines reference this agent — the blast radius of an Apply.
  // The list endpoint returns counts, not refs, so each definition has to be
  // read. That is bounded by how many pipelines a server has (they are
  // hand-authored YAML documents, not user data), and a failure to read any one
  // of them makes the whole answer UNKNOWN rather than silently shorter.
  let referencingPipelines: string[] = [];
  let pipelinesReadable = true;
  try {
    const summaries = await listPipelines();
    const defs = await Promise.all(summaries.map((p) => getPipeline(p.name).catch(() => null)));
    pipelinesReadable = defs.every((d) => d !== null);
    referencingPipelines = defs
      .filter((d) => d !== null)
      .filter((d) => (d.spec.agents ?? []).some((a) => a.ref === name))
      .map((d) => d.metadata.name);
  } catch {
    pipelinesReadable = false;
  }

  async function validateAction(yaml: string): Promise<ValidateResult> {
    "use server";
    return validateYAML(yaml);
  }

  async function saveAction(yaml: string, ifMatch: string): Promise<ConditionalSaveResult> {
    "use server";
    return saveAgentConditional(name, yaml, { ifMatch });
  }

  async function reloadAction(): Promise<{ yaml: string; revision: string } | null> {
    "use server";
    try {
      const fresh = await getAgentSource(name);
      return fresh ? { yaml: fresh.yaml, revision: fresh.revision } : null;
    } catch {
      return null;
    }
  }

  return (
    <AgentEditor
      name={name}
      original={source.yaml}
      revision={source.revision}
      canWrite={canWrite}
      online={online}
      referencingPipelines={referencingPipelines}
      pipelinesReadable={pipelinesReadable}
      validateAction={validateAction}
      saveAction={saveAction}
      reloadAction={reloadAction}
    />
  );
}
