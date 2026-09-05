// X-1: the instrument strip now renders on every screen, so its blast radius is
// the whole console. These pin the properties that make it worth having there —
// each one is a claim the strip must never get wrong, because an operator acts
// on it before touching anything else.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { InstrumentStrip, OfflineBar } from "@/app/InstrumentStrip";
import type { ServerStatus } from "@/lib/serverState";

function status(over: Partial<ServerStatus> = {}): ServerStatus {
  return {
    liveness: "process-up",
    readiness: "ready",
    checks: [{ name: "agents", status: "ok" }],
    version: "0.4.2",
    checkedAt: "2026-09-04T14:32:04.000Z",
    stale: false,
    ...over,
  };
}

/** The strip's own region, addressed the way a screen reader would reach it. */
function strip() {
  return screen.getByRole("status", { name: "server context" });
}

describe("instrument strip", () => {
  it("states liveness and readiness as separate values", () => {
    // The whole reason UX-02 exists. A process that is up but cannot serve is
    // not offline, and collapsing the two sends someone to restart a healthy
    // server — which is what the topbar pill this replaced used to do.
    render(<InstrumentStrip status={status({ readiness: "not-ready" })} apiUrl="http://localhost:8080" role="editor" tenantId="acme-prod" mode="multi_tenant" />);
    const s = strip();
    expect(within(s).getByText("PROCESS-UP")).toBeInTheDocument();
    expect(within(s).getByText("NOT-READY")).toBeInTheDocument();
  });

  it("reports readiness as UNKNOWN, not false, when the server is unreachable", () => {
    render(<InstrumentStrip status={status({ liveness: "unreachable", readiness: "unknown", version: null })} apiUrl="http://localhost:8080" role={null} tenantId={null} mode={null} />);
    const s = strip();
    expect(within(s).getByText("UNREACHABLE")).toBeInTheDocument();
    expect(within(s).getByText("UNKNOWN")).toBeInTheDocument();
    // Not "NOT-READY": nothing was checked, so nothing may be claimed.
    expect(within(s).queryByText("NOT-READY")).toBeNull();
  });

  it("renders an unknown engine version as unknown, never as a placeholder", () => {
    // The prototype hardcoded v0.4.2. A wrong version is worse than an absent
    // one — someone will file a bug against the version the strip invented.
    render(<InstrumentStrip status={status({ version: null })} apiUrl="http://localhost:8080" role="viewer" tenantId="acme-prod" mode="multi_tenant" />);
    expect(within(strip()).getByText("unknown")).toBeInTheDocument();
    expect(within(strip()).queryByText(/v?0\.4\.2/)).toBeNull();
  });

  it("names local mode explicitly rather than showing a blank role", () => {
    // Local unauthenticated mode is not a signed-in viewer. A UI that blurs the
    // two shows access control where there is none.
    render(<InstrumentStrip status={status()} apiUrl="http://localhost:8080" role={null} tenantId={null} mode="local" />);
    expect(within(strip()).getByText(/local mode · unauthenticated/)).toBeInTheDocument();
  });

  it("says the runtime is live on every screen, not just on the run button", () => {
    render(<InstrumentStrip status={status()} apiUrl="http://localhost:8080" role="editor" tenantId="acme-prod" mode="multi_tenant" />);
    expect(within(strip()).getByText("ACTIVE CONFIG")).toBeInTheDocument();
  });

  it("is reachable by keyboard, because it scrolls horizontally", () => {
    // Regression guard for the bug that shipped in #99: a scrollable region
    // with nothing focusable inside cannot be scrolled without a pointer.
    // jsdom cannot detect the overflow that makes this matter, so the property
    // is pinned structurally here and behaviourally in the 720px Playwright
    // sweep, where axe can actually see the region scroll.
    render(<InstrumentStrip status={status()} apiUrl="http://localhost:8080" role="editor" tenantId="acme-prod" mode="multi_tenant" />);
    expect(strip()).toHaveAttribute("tabindex", "0");
  });

  it("has no axe violations", async () => {
    const { container } = render(<InstrumentStrip status={status({ readiness: "not-ready" })} apiUrl="http://localhost:8080" role="viewer" tenantId="acme-prod" mode="multi_tenant" />);
    await expect(container).toHaveNoAxeViolations();
  });
});

describe("offline bar", () => {
  it("stays out of the way while the server is reachable", () => {
    const { container } = render(<OfflineBar status={status()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("says what is frozen and promises no automatic retry", async () => {
    // A stateful action retried behind the operator's back is the failure mode
    // this bar exists to rule out.
    const { container } = render(<OfflineBar status={status({ liveness: "unreachable", readiness: "unknown", stale: true })} />);
    const bar = screen.getByRole("alert");
    expect(bar).toHaveTextContent(/Reads are frozen; writes and runs are disabled/i);
    expect(bar).toHaveTextContent(/Nothing stateful is retried automatically/i);
    await expect(container).toHaveNoAxeViolations();
  });
});
