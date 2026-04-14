import Link from "next/link";

export default function NotFound() {
  return (
    <div>
      <h1 className="page-title">Not found</h1>
      <p className="page-lede">
        That resource doesn&apos;t exist on the MockAgents server.
      </p>
      <Link href="/">← Back to agent catalog</Link>
    </div>
  );
}
