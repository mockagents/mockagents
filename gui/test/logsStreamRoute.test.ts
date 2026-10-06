// @vitest-environment node
//
// The live feed's credential probe must not open an upstream SSE subscription
// (Next.js answers HEAD with the GET handler unless HEAD is exported), and the
// stream must announce itself before the first log event.
import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api", () => ({
  getAuthKey: async () => "ma_test_key",
  getBaseUrl: () => "http://upstream.invalid",
}));

const { GET, HEAD } = await import("@/app/api/logs/stream/route");

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function req(method: string, headers: Record<string, string> = {}): NextRequest {
  return new NextRequest("http://localhost:3001/api/logs/stream", { method, headers });
}

describe("HEAD /api/logs/stream (probe)", () => {
  it("checks the one-row log listing, never the stream", async () => {
    fetchMock.mockResolvedValue(new Response("[]", { status: 200 }));
    const res = await HEAD(req("HEAD"));
    expect(res.status).toBe(204);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe("http://upstream.invalid/api/v1/logs?limit=1&fields=meta");
    expect(String(url)).not.toContain("/stream");
    expect((init?.headers as Record<string, string>).Authorization).toBe("Bearer ma_test_key");
  });

  it.each([401, 403])("passes %i through so the console can ask for sign-in", async (status) => {
    fetchMock.mockResolvedValue(new Response("nope", { status }));
    expect((await HEAD(req("HEAD"))).status).toBe(status);
  });

  it("reports any other upstream failure as 502", async () => {
    fetchMock.mockResolvedValue(new Response("boom", { status: 500 }));
    expect((await HEAD(req("HEAD"))).status).toBe(502);
    fetchMock.mockRejectedValue(new TypeError("fetch failed"));
    expect((await HEAD(req("HEAD"))).status).toBe(502);
  });

  it("refuses a cross-site probe without calling upstream", async () => {
    const res = await HEAD(req("HEAD", { "sec-fetch-site": "cross-site" }));
    expect(res.status).toBe(403);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("GET /api/logs/stream", () => {
  it("sends an SSE comment first, then the upstream bytes unchanged", async () => {
    const upstream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('event: log\ndata: {"id":1}\n\n'));
        c.close();
      },
    });
    fetchMock.mockResolvedValue(
      new Response(upstream, { status: 200, headers: { "Content-Type": "text/event-stream" } }),
    );
    const res = await GET(req("GET"));
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toBe("text/event-stream");
    expect(await res.text()).toBe(': connected\n\nevent: log\ndata: {"id":1}\n\n');
  });

  it("passes a rejected credential through instead of streaming", async () => {
    fetchMock.mockResolvedValue(new Response("denied", { status: 401 }));
    const res = await GET(req("GET"));
    expect(res.status).toBe(401);
    expect(await res.text()).toBe("not authorized");
  });
});
