// UX-02: the connection/readiness state model.
//
// These names are fixed by the design handoff (§3) because they mean different
// things to act on. The tests below are mostly about keeping them distinct —
// especially "not ready" (running but cannot serve) and "unknown" (cannot be
// checked), both of which are easy to collapse into "offline" or "empty".
import { describe, expect, it } from "vitest";

import {
  buildChecklist,
  describeRefresh,
  livenessLabel,
  notReadyReason,
  readinessLabel,
  readinessTone,
  type ServerStatus,
} from "@/lib/serverState";

function status(over: Partial<ServerStatus> = {}): ServerStatus {
  return {
    liveness: "process-up",
    readiness: "ready",
    checks: [],
    version: "0.4.0",
    checkedAt: "2026-09-04T14:32:04.000Z",
    stale: false,
    ...over,
  };
}

describe("labels", () => {
  it("names each state exactly as the handoff fixes it", () => {
    expect(livenessLabel("process-up")).toBe("PROCESS-UP");
    expect(livenessLabel("unreachable")).toBe("UNREACHABLE");
    expect(readinessLabel("ready")).toBe("READY");
    expect(readinessLabel("not-ready")).toBe("NOT-READY");
    expect(readinessLabel("unknown")).toBe("UNKNOWN");
  });

  // Unknown must not borrow a success or failure tone: it is neither.
  it("gives unknown no tone at all", () => {
    expect(readinessTone("ready")).toBe("ok");
    expect(readinessTone("not-ready")).toBe("warn");
    expect(readinessTone("unknown")).toBe("none");
  });
});

describe("notReadyReason", () => {
  it("is null when the server is ready", () => {
    expect(notReadyReason(status())).toBeNull();
  });

  it("names the failing check so the operator knows what to fix", () => {
    const reason = notReadyReason(
      status({
        readiness: "not-ready",
        checks: [
          { name: "fixtures", status: "failed", error: "agents/broken.yaml:12" },
          { name: "log_store", status: "ok" },
        ],
      }),
    );
    expect(reason).toContain("fixtures");
    expect(reason).toContain("agents/broken.yaml:12");
    expect(reason).not.toContain("log_store");
  });

  it("admits when the server did not say which check failed", () => {
    const reason = notReadyReason(status({ readiness: "not-ready", checks: [] }));
    expect(reason).toMatch(/did not say/i);
  });
});

describe("describeRefresh", () => {
  it("reports age for a fresh check", () => {
    const out = describeRefresh("2026-09-04T14:32:04.000Z", new Date("2026-09-04T14:32:08.000Z"), false);
    expect(out).toBe("14:32:04Z · 4s ago");
  });

  // Staleness is stated in seconds rather than hidden: the point of the cell is
  // to let someone judge how much to trust the screen.
  it("marks a stale snapshot with its age", () => {
    const out = describeRefresh("2026-09-04T14:32:04.000Z", new Date("2026-09-04T14:33:05.000Z"), true);
    expect(out).toBe("14:32:04Z · stale (61s)");
  });

  it("reports an unparseable timestamp as unknown", () => {
    expect(describeRefresh("not-a-date", new Date(), false)).toBe("unknown");
  });
});

describe("buildChecklist", () => {
  const base = {
    status: status(),
    agentCount: 2,
    pipelineCount: 1,
    hasTraffic: true,
    catalogReadable: true,
  };

  it("marks the connection item done only when actually ready", () => {
    const ready = buildChecklist({ ...base, status: status() });
    expect(ready[0].done).toBe(true);

    const notReady = buildChecklist({ ...base, status: status({ readiness: "not-ready" }) });
    expect(notReady[0].done).toBe(false);
  });

  it("says the connection cannot be confirmed while unreachable", () => {
    const list = buildChecklist({
      ...base,
      status: status({ liveness: "unreachable", readiness: "unknown" }),
    });
    expect(list[0].done).toBe(false);
    expect(list[0].unknown).toMatch(/unreachable/i);
  });

  // The rule the whole page is built around.
  it("reports an unreadable catalog as UNKNOWN, not as an empty install", () => {
    const list = buildChecklist({ ...base, agentCount: 0, catalogReadable: false });
    const agentItem = list.find((i) => i.id === "agent");
    expect(agentItem?.unknown).toMatch(/unknown — not zero/i);
    // It must not offer "add an agent", which would be advice based on a
    // failed read.
    expect(agentItem?.label).not.toMatch(/Add an agent/i);
  });

  it("distinguishes a genuinely empty install from an unreadable one", () => {
    const list = buildChecklist({ ...base, agentCount: 0, catalogReadable: true });
    const agentItem = list.find((i) => i.id === "agent");
    expect(agentItem?.done).toBe(false);
    expect(agentItem?.label).toMatch(/Add an agent/i);
    expect(agentItem?.unknown).toBeUndefined();
  });

  it("counts loaded agents when the catalog reads", () => {
    const list = buildChecklist({ ...base, agentCount: 3 });
    const agentItem = list.find((i) => i.id === "agent");
    expect(agentItem?.done).toBe(true);
    expect(agentItem?.label).toContain("3");
  });

  it("offers no pipeline action when none exists, rather than a dead link", () => {
    const list = buildChecklist({ ...base, pipelineCount: 0 });
    const pipelineItem = list.find((i) => i.id === "pipeline");
    expect(pipelineItem?.action).toBeUndefined();
    expect(pipelineItem?.label).toMatch(/not in Release A/i);
  });

  it("links to pipelines once one exists", () => {
    const list = buildChecklist({ ...base, pipelineCount: 2 });
    expect(list.find((i) => i.id === "pipeline")?.action?.href).toBe("/pipelines");
  });

  it("tracks whether any request has been served", () => {
    expect(buildChecklist({ ...base, hasTraffic: false }).find((i) => i.id === "request")?.done).toBe(
      false,
    );
    expect(buildChecklist({ ...base, hasTraffic: true }).find((i) => i.id === "request")?.done).toBe(
      true,
    );
  });
});
