import Link from "next/link";

import {
  APIError,
  can,
  getAgentSource,
  getIdentity,
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
      validateAction={validateAction}
      saveAction={saveAction}
      reloadAction={reloadAction}
    />
  );
}
