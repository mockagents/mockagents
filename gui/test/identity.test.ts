// UX-01 (client half): the capability helper and the identity fetch's
// error semantics. The distinction these tests protect is the one the story is
// about — "rejected" (401), "unreachable" (transport), and "allowed" must
// never collapse into each other.
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIError, can, getIdentity, type Identity } from "@/lib/api";

// lib/api.ts reaches for next/headers, which only exists inside a Next server
// runtime. Stub it so the module can be exercised directly.
vi.mock("next/headers", () => ({
  cookies: async () => ({ get: () => undefined }),
}));

const IDENTITY: Identity = {
  mode: "multi_tenant",
  authenticated: true,
  tenant_id: "t_1",
  key_id: "k_1",
  role: "editor",
  capabilities: ["agents.read", "agents.write", "logs.read", "pipelines.run.write"],
  server: { version: "0.4.0" },
};

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(impl: () => Promise<Response> | Response) {
  vi.stubGlobal("fetch", vi.fn(impl));
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("can", () => {
  it("reports a granted capability", () => {
    expect(can(IDENTITY, "agents.write")).toBe(true);
  });

  it("reports a withheld capability", () => {
    expect(can(IDENTITY, "audit.read")).toBe(false);
  });

  it("is false for a null identity rather than throwing", () => {
    // An unknown identity must never read as permission.
    expect(can(null, "agents.read")).toBe(false);
  });

  it("does not treat a capability prefix as a match", () => {
    // "agents.read" must not satisfy "agents.reload.write".
    expect(can(IDENTITY, "agents")).toBe(false);
    expect(can(IDENTITY, "agents.reload.write")).toBe(false);
  });

  it("distinguishes pipelines.run.write from pipelines.write", () => {
    // A viewer may run a pipeline but not save one; conflating these is
    // exactly what the backend collision test forbids.
    expect(can(IDENTITY, "pipelines.run.write")).toBe(true);
    expect(can(IDENTITY, "pipelines.write")).toBe(false);
  });
});

describe("getIdentity", () => {
  it("returns the parsed identity on success", async () => {
    stubFetch(() => jsonResponse(IDENTITY));
    await expect(getIdentity("key")).resolves.toEqual(IDENTITY);
  });

  it("sends the supplied key as a bearer token", async () => {
    const fetchMock = vi.fn((_url: string, _init?: RequestInit) => jsonResponse(IDENTITY));
    vi.stubGlobal("fetch", fetchMock);

    await getIdentity("my-key");

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toContain("/api/v1/identity");
    expect((init?.headers as Record<string, string>).Authorization).toBe("Bearer my-key");
  });

  // The three outcomes that must stay distinguishable.
  it("throws APIError(401) when the credential is rejected", async () => {
    stubFetch(() => jsonResponse({ error: "invalid api key" }, 401));
    await expect(getIdentity("bad")).rejects.toBeInstanceOf(APIError);
    await expect(getIdentity("bad")).rejects.toMatchObject({ status: 401 });
  });

  it("throws APIError(403) rather than reporting unreachable", async () => {
    stubFetch(() => jsonResponse({ error: "forbidden" }, 403));
    await expect(getIdentity("k")).rejects.toMatchObject({ status: 403 });
  });

  it("returns null when the server cannot be reached at all", async () => {
    // A transport failure is not an authorization verdict: signing the
    // operator out because the mock restarted would be wrong.
    stubFetch(() => Promise.reject(new TypeError("fetch failed")));
    await expect(getIdentity("k")).resolves.toBeNull();
  });

  it("reports local mode as unauthenticated with a null role", async () => {
    const local: Identity = {
      mode: "local",
      authenticated: false,
      role: null,
      capabilities: ["agents.read"],
      server: { version: "dev" },
    };
    stubFetch(() => jsonResponse(local));

    const got = await getIdentity();
    expect(got?.mode).toBe("local");
    expect(got?.authenticated).toBe(false);
    // Never coerced into a role — the UI must be able to tell local mode from
    // a signed-in viewer.
    expect(got?.role).toBeNull();
  });
});
