import "./globals.css";
import type { Metadata } from "next";
import Link from "next/link";

import { getBaseUrl, getHealth } from "@/lib/api";

export const metadata: Metadata = {
  title: "MockAgents",
  description: "Browse agent catalog, inspect definitions, and view interaction logs.",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // Health check runs on every navigation — cheap against the local
  // mock server and lets us surface connectivity issues immediately.
  const health = await getHealth();

  return (
    <html lang="en">
      <body>
        <header className="header">
          <div className="brand">
            <Link href="/">MockAgents</Link>
            <span className="subtitle">Web console · v0.1</span>
          </div>
          <nav className="nav">
            <Link href="/">Agents</Link>
            <Link href="/logs">Logs</Link>
            <a href={getBaseUrl()} target="_blank" rel="noreferrer" className="muted">
              API →
            </a>
          </nav>
          <HealthPill health={health} apiUrl={getBaseUrl()} />
        </header>

        <main className="main">{children}</main>

        <footer className="footer">
          <span>MockAgents · talking to {getBaseUrl()}</span>
        </footer>
      </body>
    </html>
  );
}

function HealthPill({
  health,
  apiUrl,
}: {
  health: { status: string; version?: string } | null;
  apiUrl: string;
}) {
  if (!health) {
    return (
      <div className="pill pill-down" title={`unreachable: ${apiUrl}`}>
        <span className="dot" /> offline
      </div>
    );
  }
  return (
    <div className="pill pill-ok" title={`version ${health.version ?? "unknown"}`}>
      <span className="dot" /> online
    </div>
  );
}
