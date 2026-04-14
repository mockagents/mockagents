import Link from "next/link";
import { notFound } from "next/navigation";

import { APIError, getAgent } from "@/lib/api";

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
  const scenarios = Array.isArray(behavior.scenarios) ? (behavior.scenarios as any[]) : [];
  const tools = Array.isArray(spec.tools) ? (spec.tools as any[]) : [];

  return (
    <div>
      <div className="breadcrumb">
        <Link href="/">← All agents</Link>
      </div>
      <h1 className="page-title">{String(metadata.name ?? name)}</h1>
      {metadata.description !== undefined && (
        <p className="page-lede">{String(metadata.description)}</p>
      )}

      <section className="panel">
        <h2>Overview</h2>
        <dl className="kv">
          <dt>Protocol</dt>
          <dd>{String(spec.protocol ?? "—")}</dd>
          <dt>Model</dt>
          <dd>{String(spec.model ?? "—")}</dd>
          <dt>Scenarios</dt>
          <dd>{scenarios.length}</dd>
          <dt>Tools</dt>
          <dd>{tools.length}</dd>
        </dl>
      </section>

      <section className="panel">
        <h2>Scenarios</h2>
        {scenarios.length === 0 ? (
          <p className="muted">No scenarios declared.</p>
        ) : (
          <ul className="list">
            {scenarios.map((sc, idx) => (
              <li key={idx}>
                <strong>{String(sc.name ?? `#${idx}`)}</strong>
                {sc.match && (
                  <span className="muted">
                    {" "}
                    · match={JSON.stringify(sc.match)}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="panel">
        <h2>Tools</h2>
        {tools.length === 0 ? (
          <p className="muted">No tools declared.</p>
        ) : (
          <ul className="list">
            {tools.map((tool, idx) => (
              <li key={idx}>
                <strong>{String(tool.name ?? `#${idx}`)}</strong>
                {tool.description && (
                  <span className="muted"> — {String(tool.description)}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="panel">
        <h2>Raw definition</h2>
        <pre className="code">{JSON.stringify(definition, null, 2)}</pre>
      </section>
    </div>
  );
}
