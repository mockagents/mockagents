// X-2 (client half): "this server has no pipeline routes" and "this server has
// no pipelines" are different facts, and the console used to render both as an
// empty list. The mapping is pinned here because the page that consumes it is a
// server component, so the browser suite cannot intercept its fetch.
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIError, listPipelineInventory, listPipelines } from "@/lib/api";

vi.mock("next/headers", () => ({
  cookies: async () => ({ get: () => undefined }),
}));

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubStatus(status: number, body = "[]") {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(body, { status, headers: { "Content-Type": "application/json" } })),
  );
}

describe("listPipelineInventory", () => {
  it("reports an unmounted route as unsupported, not as empty", async () => {
    stubStatus(404, '{"error":"not found"}');
    expect(await listPipelineInventory()).toEqual({ supported: false });
  });

  it("reports a real empty inventory as supported", async () => {
    stubStatus(200, "[]");
    expect(await listPipelineInventory()).toEqual({ supported: true, pipelines: [] });
  });

  it("passes the pipelines through when there are some", async () => {
    stubStatus(200, '[{"name":"research","topology":"sequential","agent_count":2,"edge_count":1}]');
    const listing = await listPipelineInventory();
    expect(listing.supported).toBe(true);
    if (listing.supported) expect(listing.pipelines.map((p) => p.name)).toEqual(["research"]);
  });

  // Unreachable and unauthorized are their own states with their own screens.
  // Folding them into "not enabled" would tell an operator to change how the
  // server was started when the real problem is a credential or a dead process.
  it("does not swallow an unauthorized read", async () => {
    stubStatus(403, '{"error":"forbidden"}');
    await expect(listPipelineInventory()).rejects.toBeInstanceOf(APIError);
  });

  it("does not swallow a server error", async () => {
    stubStatus(503, '{"error":"unavailable"}');
    await expect(listPipelineInventory()).rejects.toBeInstanceOf(APIError);
  });
});

describe("listPipelines", () => {
  it("still flattens a 404 to an empty list for callers that only iterate", async () => {
    // The edit page needs a list to scan for references; it has no UI in which
    // the distinction would mean anything, so the older helper stays.
    stubStatus(404, '{"error":"not found"}');
    expect(await listPipelines()).toEqual([]);
  });
});
