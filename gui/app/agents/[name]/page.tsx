import Link from "next/link";
import { revalidatePath } from "next/cache";
import { notFound } from "next/navigation";

import { APIError, getAgent, reloadAgent } from "@/lib/api";
import { Icon } from "@/lib/icons";
import { Stat } from "../../Stat";
import { AgentTabs } from "./AgentTabs";

type PageProps = {
  params: Promise<{ name: string }>;
};

export default async function AgentDetailPage({ params }: PageProps) {
  const { name } = await params;
  let definition: Record<string, unknown>;
  try {
    definition = await getAgent(name);
  } catch (err) {
    if (err instanceof APIError && err.status === 404) {
      notFound();
    }
    throw err;
  }

  const metadata = (definition.metadata ?? {}) as Record<string, unknown>;
  const spec = (definition.spec ?? {}) as Record<string, unknown>;
  const behavior = (spec.behavior ?? {}) as Record<string, unknown>;
  const scenarios = Array.isArray(behavior.scenarios) ? behavior.scenarios : [];
  const tools = Array.isArray(spec.tools) ? spec.tools : [];
  const streaming = (behavior.streaming ?? {}) as Record<string, unknown>;
  const chaos = (behavior.chaos ?? {}) as Record<string, unknown>;
  const streamingOn = streaming.enabled === true;
  const chaosOn = chaos.enabled === true;
  const tags = Array.isArray(metadata.tags) ? metadata.tags.map(String) : [];
  const displayName = String(metadata.name ?? name);
  const description = metadata.description != null ? String(metadata.description) : "";

  async function reloadAction() {
    "use server";
    try {
      await reloadAgent(name);
    } catch {
      // Best-effort: the page revalidates below and reflects current state.
    }
    revalidatePath(`/agents/${name}`);
  }

  return (
    <div>
      <Link href="/" className="btn btn-ghost btn-sm" style={{ marginLeft: -8, marginBottom: 10 }}>
        <Icon name="arrow-left" size={15} /> Agent catalog
      </Link>

      <div className="head-row mb-4">
        <div className="agent-icon" style={{ width: 44, height: 44, flex: "0 0 44px" }}>
          <Icon name="bot" size={22} />
        </div>
        <div className="grow">
          <div className="row gap-3" style={{ flexWrap: "wrap" }}>
            <h1 className="page-title">{displayName}</h1>
            {chaosOn && <span className="badge badge-warning">chaos active</span>}
          </div>
          <div className="row gap-2 mt-2" style={{ flexWrap: "wrap" }}>
            <span className="tag">{String(spec.protocol ?? "—")}</span>
            <span className="tag">{String(spec.model ?? "—")}</span>
            {tags.map((t) => (
              <span key={t} className="tag">
                {t}
              </span>
            ))}
          </div>
          {description && (
            <p className="page-lede" style={{ marginTop: 10 }}>
              {description}
            </p>
          )}
        </div>
        <form action={reloadAction}>
          <button type="submit" className="btn btn-outline btn-sm" title="Re-read this agent's YAML from disk">
            <Icon name="refresh-cw" size={15} /> Reload
          </button>
        </form>
      </div>

      <div className="grid grid-4 mb-6">
        <Stat icon="list-tree" label="Scenarios" value={String(scenarios.length)} sub="match rules" />
        <Stat icon="wrench" label="Tools" value={String(tools.length)} sub="simulated calls" />
        <Stat
          icon="wifi"
          label="Streaming"
          value={streamingOn ? "on" : "off"}
          sub={streamingOn ? `${String(streaming.chunk_size ?? "")} tok/chunk` : "non-streamed"}
        />
        <Stat
          icon="zap"
          label="Chaos"
          value={chaosOn ? "on" : "off"}
          sub={chaosOn ? "injecting faults" : "no faults"}
        />
      </div>

      <AgentTabs definition={definition} name={name} />
    </div>
  );
}
