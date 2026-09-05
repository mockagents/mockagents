import "./globals.css";
import type { Metadata } from "next";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { getBaseUrl, getIdentity, getServerStatus, type Identity } from "@/lib/api";
import { getAuthStatus, logout } from "@/lib/auth";
import { InstrumentStrip, OfflineBar } from "./InstrumentStrip";
import { Shell } from "./Shell";

export const metadata: Metadata = {
  title: "MockAgents · Console",
  description: "Neutral operator console for the MockAgents mock server — agents, logs, costs, audit, and admin.",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // X-1: the instrument strip belongs to the shell, not to individual pages.
  // The design carries server context on every screen precisely because the
  // expensive mistakes — applying an edit, running a pipeline — happen on the
  // screens that used to show none of it.
  //
  // Both probes run on every navigation. That is cheap against a local mock
  // server, and both are memoized per render (lib/api.ts), so a page that also
  // needs identity or status shares this result rather than fetching again.
  const status = await getServerStatus();
  const auth = await getAuthStatus();

  let identity: Identity | null = null;
  try {
    identity = await getIdentity();
  } catch {
    // A rejected or absent credential is not a reason to fail the whole shell.
    // The strip renders the role as unknown, which is the truth.
    identity = null;
  }

  // Theme is persisted as a non-secret cookie set by the client toggle; reading
  // it here applies the right theme during SSR (no flash, no bootstrap script).
  const themeCookie = (await cookies()).get("mockagents-theme")?.value;
  const theme = themeCookie === "dark" ? "dark" : "light";

  async function logoutAction() {
    "use server";
    await logout();
    redirect("/login");
  }

  return (
    <html lang="en" data-theme={theme}>
      <body>
        <Shell
          apiUrl={getBaseUrl()}
          instrument={
            <>
              <InstrumentStrip
                status={status}
                apiUrl={getBaseUrl()}
                role={identity?.role ?? null}
                tenantId={identity?.tenant_id ?? null}
                mode={identity?.mode ?? null}
              />
              <OfflineBar status={status} />
            </>
          }
          auth={auth}
          // UX-07: null when there is no credential or the server could not be
          // reached — the nav then makes no availability claim at all.
          capabilities={auth && !auth.unreachable ? auth.capabilities : null}
          logoutAction={logoutAction}
        >
          {children}
        </Shell>
      </body>
    </html>
  );
}
