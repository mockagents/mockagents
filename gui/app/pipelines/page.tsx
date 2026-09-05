import Link from "next/link";

import { APIError, listPipelineInventory, PipelineSummary } from "@/lib/api";
import { Icon } from "@/lib/icons";

export default async function PipelinesPage() {
  // X-2. Three outcomes, and they are not interchangeable: the routes are not
  // mounted at all (this server cannot have pipelines), they are mounted and
  // hold none, or the read failed. Only the middle one is an empty inventory.
  let pipelines: PipelineSummary[] = [];
  let supported = true;
  let error: string | null = null;
  try {
    const listing = await listPipelineInventory();
    supported = listing.supported;
    if (listing.supported) pipelines = listing.pipelines;
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <div className="page-head">
        <h1 className="page-title">Pipelines</h1>
        <p className="page-lede">
          Multi-agent topologies declared with <code>kind: Pipeline</code>. Sequential, parallel,
          and graph wiring with substring-matched conditional edges. Click one to see its DAG.
        </p>
      </div>

      {error && (
        <div className="banner banner-error">
          <strong>Server error.</strong> {error}
        </div>
      )}

      {!error && !supported ? (
        // The `unsupported` variant of the design's state matrix: reads are
        // allowed elsewhere, this capability is simply absent from the build
        // that is running. Saying "no pipelines" here would send someone
        // looking for a Create button that cannot exist.
        <div className="empty">
          <strong>Pipelines are not enabled on this server.</strong>
          <p className="txt-sm" style={{ marginTop: 8 }}>
            The pipeline routes are mounted only when the server starts with at least one{" "}
            <code>kind: Pipeline</code> document, and this one did not — so this is a
            capability that is absent, not an inventory that is empty.
          </p>
          <pre className="codeblock" style={{ marginTop: 10, textAlign: "left" }}>{`cp examples/research-pipeline.yaml agents/
mockagents start --agents-dir agents`}</pre>
        </div>
      ) : !error && pipelines.length === 0 ? (
        <div className="empty">
          <strong>No pipelines registered.</strong>
          <p className="txt-sm" style={{ marginTop: 8 }}>
            This server supports pipelines and is holding none. Drop a{" "}
            <code>kind: Pipeline</code> YAML into your agents directory and restart.
          </p>
        </div>
      ) : (
        <div className="catalog" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
          {pipelines.map((p) => (
            <PipelineCard key={p.name} p={p} />
          ))}
        </div>
      )}
    </div>
  );
}

function PipelineCard({ p }: { p: PipelineSummary }) {
  return (
    <Link href={`/pipelines/${encodeURIComponent(p.name)}`} className="agent-card" style={{ minHeight: 0 }}>
      <div className="ac-top">
        <div className="agent-icon">
          <Icon name="workflow" size={18} />
        </div>
        <div className="grow">
          <h3>{p.name}</h3>
          <div className="ac-proto">
            {p.agent_count} agents · {p.edge_count} edges
          </div>
        </div>
        <span className="badge badge-outline">{p.topology}</span>
      </div>
      {p.description && (
        <p className="ac-desc" style={{ flex: "none" }}>
          {p.description}
        </p>
      )}
    </Link>
  );
}
