import "./globals.css";
import type { Metadata } from "next";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { getBaseUrl, getHealth } from "@/lib/api";
import { getAuthStatus, logout } from "@/lib/auth";
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
  // Health check runs on every navigation — cheap against the local mock server
  // and lets the topbar surface connectivity immediately.
  const health = await getHealth();
  const auth = await getAuthStatus();

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
          online={health !== null}
          version={health?.version}
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
