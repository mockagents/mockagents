import Link from "next/link";
import { notFound } from "next/navigation";

import { getPipeline, listAgents } from "@/lib/api";
import { Icon } from "@/lib/icons";
import { PipelineEditor } from "./PipelineEditor";

type PageProps = {
  params: Promise<{ name: string }>;
};

export default async function PipelineEditPage({ params }: PageProps) {
  const { name } = await params;
  const pipeline = await getPipeline(name);
  if (!pipeline) notFound();

  // Agent names populate the node ref picker. Failing to list agents (e.g. a
  // permission error) degrades to an empty picker rather than blocking edits.
  let agentNames: string[] = [];
  try {
    agentNames = (await listAgents()).map((a) => a.name).sort();
  } catch {
    agentNames = [];
  }

  return (
    <div>
      <Link
        href={`/pipelines/${encodeURIComponent(name)}`}
        className="btn btn-ghost btn-sm"
        style={{ marginLeft: -8, marginBottom: 10 }}
      >
        <Icon name="arrow-left" size={15} /> {name}
      </Link>

      <div className="head-row mb-4">
        <div className="agent-icon" style={{ width: 44, height: 44, flex: "0 0 44px" }}>
          <Icon name="workflow" size={22} />
        </div>
        <div className="grow">
          <div className="row gap-3" style={{ flexWrap: "wrap" }}>
            <h1 className="page-title">Edit {name}</h1>
            <span className="badge badge-outline">{pipeline.spec.topology}</span>
          </div>
          <p className="page-lede" style={{ marginTop: 8 }}>
            Drag nodes to rearrange, and in <code>graph</code> topology connect handles to rewire.
          </p>
        </div>
      </div>

      <PipelineEditor pipeline={pipeline} agentNames={agentNames} />
    </div>
  );
}
