"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Fragment, useEffect, useState, type ReactNode } from "react";

import { Icon, type IconName } from "@/lib/icons";

type NavItem = {
  href: string;
  label: string;
  icon: IconName;
  match: string;
  /** UX-07: the server capability this destination needs. When the server has
   * told us the caller's capabilities and this one is absent, the item renders
   * disabled with the reason — not hidden, and not left live to 403. A missing
   * entry means "no floor above being signed in". */
  requires?: string;
  /** Plain-language floor, shown when `requires` is not held. */
  floor?: string;
};
type NavGroup = { group: string; items: NavItem[] };

// Only the surfaces that exist in the real GUI (scope: restyle existing),
// grouped to match the design's sidebar.
const NAV: NavGroup[] = [
  {
    group: "Workspace",
    items: [
      // UX-02. The approved IA names this section Overview / Mocks / Test Lab /
      // Investigate / Reports / Administration. Only Overview is added here:
      // renaming the existing routes moves every deep link, which epic §5 says
      // needs redirects, and that is its own change rather than a side effect
      // of this one.
      { href: "/overview", label: "Overview", icon: "layout-dashboard", match: "/overview" },
      { href: "/", label: "Agents", icon: "bot", match: "/" },
      { href: "/pipelines", label: "Pipelines", icon: "workflow", match: "/pipelines" },
    ],
  },
  {
    group: "Observability",
    items: [
      { href: "/logs", label: "Logs", icon: "scroll-text", match: "/logs" },
      { href: "/costs", label: "Costs", icon: "bar-chart", match: "/costs" },
      { href: "/reports", label: "Reports", icon: "save", match: "/reports" },
      {
        href: "/audit",
        label: "Audit",
        icon: "shield-search",
        match: "/audit",
        requires: "audit.read",
        floor: "the admin role",
      },
    ],
  },
  {
    group: "Tooling",
    items: [{ href: "/editor", label: "Editor", icon: "file-code", match: "/editor" }],
  },
  {
    group: "Admin",
    items: [
      {
        href: "/admin/tenants",
        label: "Tenants & keys",
        icon: "users",
        match: "/admin",
        // The tenant COLLECTION is platform-gated (route_authz.go), not
        // admin-gated. A tenant admin manages its own keys but cannot list
        // tenants, and the nav has to say that rather than offer a link that
        // 403s.
        requires: "tenants.read",
        floor: "the platform role",
      },
      { href: "/account", label: "Account", icon: "settings", match: "/account" },
    ],
  },
];

const WIDE = ["/logs", "/costs", "/admin"];

export interface ShellProps {
  children: ReactNode;
  apiUrl: string;
  // UX-01/X-1: the instrument strip, rendered by the root layout and passed in
  // as a slot. It is a server component and this is a client component, so it
  // cannot be imported here — but it belongs to the shell, not to any page,
  // because the design carries server context on EVERY screen. A page that
  // renders its own would double it.
  instrument?: ReactNode;
  // UX-01: role is server-reported and may be null — either the server was
  // unreachable, or it runs in local mode with no roles at all. Render the
  // uncertainty; never substitute a guess.
  auth: { prefix: string; role: string | null; unreachable: boolean } | null;
  /** UX-07: capabilities the SERVER reports for this credential, used to make
   * navigation truthful. `null` means we do not know (unreachable, or no
   * credential at all) — and not knowing is never grounds for disabling a
   * destination, because a guess in either direction is a lie. */
  capabilities?: string[] | null;
  logoutAction: () => Promise<void>;
}

function isActive(pathname: string, match: string): boolean {
  return match === "/" ? pathname === "/" : pathname === match || pathname.startsWith(match + "/");
}

// Build breadcrumbs from the pathname. Dynamic segments (agent name, log id,
// tenant id) render as their decoded value.
function crumbsFor(pathname: string): string[] {
  if (pathname === "/") return ["Agents"];
  const seg = pathname.split("/").filter(Boolean).map(decodeURIComponent);
  const head: Record<string, string> = {
    agents: "Agents",
    pipelines: "Pipelines",
    logs: "Logs",
    costs: "Costs",
    audit: "Audit",
    editor: "Editor",
    account: "Account",
    login: "Sign in",
  };
  if (seg[0] === "admin") {
    return ["Tenants & keys", ...seg.slice(2)];
  }
  const label = head[seg[0]] ?? seg[0];
  return [label, ...seg.slice(1)];
}

export function Shell({
  children,
  apiUrl,
  instrument,
  auth,
  capabilities = null,
  logoutAction,
}: ShellProps) {
  const pathname = usePathname() || "/";
  const [theme, setTheme] = useState<"light" | "dark">("light");

  // Sync with whatever the no-flash <head> script already applied.
  useEffect(() => {
    const current = document.documentElement.getAttribute("data-theme");
    setTheme(current === "dark" ? "dark" : "light");
  }, []);

  function toggleTheme() {
    const next = theme === "light" ? "dark" : "light";
    setTheme(next);
    document.documentElement.setAttribute("data-theme", next);
    // Persist as a (non-secret) cookie so the server layout can set data-theme
    // during SSR — no flash, no inline bootstrap script.
    document.cookie = `mockagents-theme=${next}; path=/; max-age=31536000; samesite=lax`;
  }

  const crumbs = crumbsFor(pathname);
  const wide = WIDE.some((m) => pathname === m || pathname.startsWith(m + "/"));
  const host = apiUrl.replace(/^https?:\/\//, "");

  return (
    <div className="app">
      <aside className="side">
        <div className="brand">
          <div className="brand-mark">
            <BrandGlyph />
          </div>
          <div>
            <div className="name">MockAgents</div>
            <div className="sub">console v0.3</div>
          </div>
        </div>
        <div className="nav-scroll">
          {NAV.map((g) => (
            <div className="nav-group" key={g.group}>
              <div className="nav-label">{g.group}</div>
              {g.items.map((it) => {
                // Only claim a destination is unavailable when the server has
                // actually said so. Unknown capabilities leave the link live:
                // the server authorizes every request anyway, so an honest
                // 403 beats a nav that hides a section on a hunch.
                const denied =
                  it.requires !== undefined &&
                  capabilities !== null &&
                  capabilities !== undefined &&
                  !capabilities.includes(it.requires);
                if (denied) {
                  return (
                    <span
                      key={it.href}
                      className="nav-item nav-item-denied"
                      aria-disabled="true"
                      title={`${it.label} requires ${it.floor ?? "a higher role"}. Your credential does not have it.`}
                    >
                      <Icon name={it.icon} size={16} />
                      <span>{it.label}</span>
                      <span className="nav-lock">{it.floor ?? "restricted"}</span>
                    </span>
                  );
                }
                return (
                  <Link
                    key={it.href}
                    href={it.href}
                    className={"nav-item" + (isActive(pathname, it.match) ? " active" : "")}
                  >
                    <Icon name={it.icon} size={16} />
                    <span>{it.label}</span>
                  </Link>
                );
              })}
            </div>
          ))}
        </div>
        <div className="side-foot">
          <div className="env-pill">
            <Icon name="circle-dot" size={13} />
            <span>{host}</span>
          </div>
        </div>
      </aside>

      <div className="main">
        <header className="topbar">
          <div className="crumbs">
            <Icon name="layout-dashboard" size={15} />
            {crumbs.map((c, i) => (
              <Fragment key={i}>
                <span className="sep">/</span>
                <span className={i === crumbs.length - 1 ? "cur" : ""}>{c}</span>
              </Fragment>
            ))}
          </div>
          <div className="topbar-right">
            {/* The old online/offline pill lived here. It collapsed liveness and
                readiness into one word, so it could read "online" while the
                strip below reads NOT-READY — the exact conflation UX-02 exists
                to undo. The strip states both separately, with the engine
                version, so the pill is gone rather than left to contradict it. */}
            <button
              type="button"
              className="btn btn-ghost btn-icon btn-sm"
              onClick={toggleTheme}
              title="toggle theme"
              aria-label="toggle theme"
            >
              <Icon name={theme === "light" ? "moon" : "sun"} size={16} />
            </button>
            {auth ? (
              <form action={logoutAction} style={{ display: "inline-flex", gap: 8, alignItems: "center" }}>
                <Link
                  href="/account"
                  className="spill"
                  title={
                    auth.unreachable
                      ? "signed in · role unknown (server unreachable) — click for self-rotation"
                      : `signed in · role ${auth.role ?? "unknown"} — click for self-rotation`
                  }
                >
                  <Icon name="key-round" size={13} />
                  <span className="mono">{auth.prefix}…</span>
                </Link>
                <button type="submit" className="btn btn-sm btn-ghost">
                  Sign out
                </button>
              </form>
            ) : (
              <Link href="/login" className="spill">
                <span className="dot" style={{ background: "var(--sr-fg-muted)" }} />
                sign in
              </Link>
            )}
          </div>
        </header>

        {instrument}

        <main className="content">
          <div className={"content-inner view-enter" + (wide ? " wide" : "")} key={pathname}>
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}

function BrandGlyph() {
  return (
    <svg viewBox="0 0 24 24" width={18} height={18} fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M8 7 4 12l4 5" />
      <path d="m16 7 4 5-4 5" />
      <circle cx={12} cy={12} r={1.4} fill="currentColor" stroke="none" />
    </svg>
  );
}
