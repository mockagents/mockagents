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

function renderShell(capabilities: string[] | null, instrument?: React.ReactNode) {
  return render(
    <Shell
      apiUrl="http://localhost:8080"
      instrument={instrument}
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

// X-1. The strip is the design's persistent server context, so its home is the
// shell rather than any one page. These pin the two properties that make it
// persistent: it renders above the page, and the shell no longer states
// connectivity on its own.
describe("instrument slot", () => {
  it("renders the strip above the page content", () => {
    const { container } = renderShell(EVERYTHING, <div data-testid="strip">strip</div>);
    const strip = screen.getByTestId("strip");
    const content = container.querySelector(".content-inner");
    expect(strip).toBeInTheDocument();
    expect(content).not.toBeNull();
    // DOCUMENT_POSITION_FOLLOWING: content comes after the strip. Reading order
    // is the point — a status banner announced below the thing it qualifies is
    // read too late to act on.
    expect(strip.compareDocumentPosition(content!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("no longer claims connectivity in the topbar", () => {
    // The removed pill said "online" from a single health probe, which could
    // sit next to a NOT-READY strip. One source of truth, or none.
    renderShell(EVERYTHING, <div data-testid="strip">strip</div>);
    expect(screen.queryByText(/^online$/i)).toBeNull();
    expect(screen.queryByText(/^offline$/i)).toBeNull();
  });

  it("renders without a strip, so a page can still mount if the slot is absent", () => {
    renderShell(EVERYTHING);
    expect(screen.getByText("content")).toBeInTheDocument();
  });
});
