// UX-07: role-truthful navigation.
//
// Three states have to stay distinct in the sidebar, because acting on a
// confusion between them wastes an operator's afternoon:
//
//   * the credential HAS the capability      → a live link;
//   * the server SAYS it does not            → visible, disabled, with the floor named;
//   * we do not know (offline, no credential) → a live link, because a guess in
//     either direction is a lie and the server authorizes every request anyway.
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Shell } from "@/app/Shell";

vi.mock("next/navigation", () => ({
  usePathname: () => "/overview",
}));

function renderShell(capabilities: string[] | null) {
  return render(
    <Shell
      apiUrl="http://localhost:8080"
      online
      version="0.4.2"
      auth={{ prefix: "mak_1c3a", role: "viewer", unreachable: capabilities === null }}
      capabilities={capabilities}
      logoutAction={async () => {}}
    >
      <div>content</div>
    </Shell>,
  );
}

const EVERYTHING = ["agents.read", "audit.read", "tenants.read", "logs.read", "costs.read"];

describe("capability gating", () => {
  it("links to a destination the credential can use", () => {
    renderShell(EVERYTHING);
    expect(screen.getByRole("link", { name: /audit/i })).toHaveAttribute("href", "/audit");
    expect(screen.getByRole("link", { name: /tenants & keys/i })).toHaveAttribute(
      "href",
      "/admin/tenants",
    );
  });

  it("shows a denied destination rather than hiding it, and names the floor", () => {
    renderShell(["agents.read", "logs.read"]);

    // Still present — hiding it would leave the operator unable to tell "not in
    // this product" from "not for this role".
    const audit = screen.getByTitle(/Audit requires the admin role/i);
    expect(audit).toBeInTheDocument();
    expect(audit).toHaveAttribute("aria-disabled", "true");
    expect(screen.queryByRole("link", { name: /audit/i })).toBeNull();

    const tenants = screen.getByTitle(/Tenants & keys requires the platform role/i);
    expect(tenants).toHaveAttribute("aria-disabled", "true");
  });

  // The tenant COLLECTION is platform-gated, not admin-gated. Saying "admin"
  // here sent admins hunting for a permission that does not exist.
  it("names the platform role for tenants, not admin", () => {
    renderShell(["agents.read"]);
    expect(screen.getByText("the platform role")).toBeInTheDocument();
    expect(screen.queryByText(/tenants & keys requires the admin role/i)).toBeNull();
  });

  it("makes no availability claim when capabilities are unknown", () => {
    renderShell(null);
    // Unknown is not denial: every destination stays reachable and the server
    // gets to answer for itself.
    expect(screen.getByRole("link", { name: /audit/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /tenants & keys/i })).toBeInTheDocument();
  });

  it("never gates destinations that have no role floor", () => {
    renderShell([]);
    for (const label of [/overview/i, /logs/i, /costs/i, /reports/i, /account/i]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
  });
});

describe("reports", () => {
  it("offers the evidence export from the observability group", () => {
    renderShell(EVERYTHING);
    expect(screen.getByRole("link", { name: /reports/i })).toHaveAttribute("href", "/reports");
  });
});
