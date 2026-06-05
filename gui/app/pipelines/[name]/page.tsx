import Link from "next/link";
import { notFound } from "next/navigation";

import { getPipeline, PipelineDefinition } from "@/lib/api";
import { Icon } from "@/lib/icons";
import { DAGViewer } from "./DAGViewer";

type PageProps = {
  params: Promise<{ name: string }>;
};

export default async function PipelineDetailPage({ params }: PageProps) {
  const { name } = await params;
  const pipeline = await getPipeline(name);
  if (!pipeline) notFound();

  const agents = pipeline.spec.agents ?? [];
  const edges = normalizeEdges(pipeline);
  const tags = pipeline.metadata.tags ?? [];

  return (
    <div>
      <Link href="/pipelines" className="btn btn-ghost btn-sm" style={{ marginLeft: -8, marginBottom: 10 }}>
        <Icon name="arrow-left" size={15} /> Pipelines
      </Link>

      <div className="head-row mb-4">
        <div className="agent-icon" style={{ width: 44, height: 44, flex: "0 0 44px" }}>
          <Icon name="workflow" size={22} />
        </div>
        <div className="grow">
          <div className="row gap-3" style={{ flexWrap: "wrap", alignItems: "center" }}>
            <h1 className="page-title">{name}</h1>
            <span className="badge badge-outline">{pipeline.spec.topology}</span>
            <Link
              href={`/pipelines/${encodeURIComponent(name)}/edit`}
              className="btn btn-outline btn-sm"
              style={{ marginLeft: "auto" }}
            >
              <Icon name="file-code" size={15} /> Edit
            </Link>
          </div>
          <div className="row gap-2 mt-2" style={{ flexWrap: "wrap" }}>
            <span className="tag">
              {agents.length} agent{agents.length === 1 ? "" : "s"}
            </span>
            {edges.length > 0 && (
              <span className="tag">
                {edges.length} edge{edges.length === 1 ? "" : "s"}
              </span>
            )}
            {tags.map((t) => (
              <span key={t} className="tag">
                {t}
              </span>
            ))}
          </div>
          {pipeline.metadata.description && (
            <p className="page-lede" style={{ marginTop: 10 }}>
              {pipeline.metadata.description}
            </p>
          )}
        </div>
      </div>

      <DAGViewer topology={pipeline.spec.topology} agents={agents} edges={edges} />

      <h2 className="section-title">Agents</h2>
      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>Node ID</th>
              <th>Agent ref</th>
            </tr>
          </thead>
          <tbody>
            {agents.map((a) => (
              <tr key={a.id}>
                <td className="mono">{a.id}</td>
                <td>
                  <Link href={`/agents/${encodeURIComponent(a.ref)}`}>{a.ref}</Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// normalizeEdges converts the three topology shapes into a single
// adjacency list the viewer can render. Sequential pipelines implicit
// a linear chain; parallel pipelines have no edges; graph pipelines
// hand us edges directly.
function normalizeEdges(pipeline: PipelineDefinition) {
  const agents = pipeline.spec.agents ?? [];
  const topology = pipeline.spec.topology;
  if (topology === "sequential") {
    const out: { from: string; to: string; when_contains?: string }[] = [];
    for (let i = 0; i + 1 < agents.length; i++) {
      out.push({ from: agents[i].id, to: agents[i + 1].id });
    }
    return out;
  }
  if (topology === "parallel") {
    return [];
  }
  return pipeline.spec.edges ?? [];
}
