// Unit tests for the framework-agnostic lifecycle core. These use fake hooks
// and a fake server, so they never spawn the Go binary.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { MockAgentClient } from "@mockagents/sdk";
import {
  createSetup,
  providerEnv,
  type LifecycleHooks,
  type MockAgentServerLike,
} from "../src/core.js";

/** Capture the fns registered with beforeAll/afterAll so the test can drive
 * the lifecycle by hand. */
function captureHooks(): {
  hooks: LifecycleHooks;
  runBeforeAll: () => Promise<void>;
  runAfterAll: () => Promise<void>;
  counts: { before: number; after: number };
} {
  let before: (() => void | Promise<void>) | null = null;
  let after: (() => void | Promise<void>) | null = null;
  const counts = { before: 0, after: 0 };
  return {
    counts,
    hooks: {
      beforeAll: (fn) => {
        counts.before++;
        before = fn;
      },
      afterAll: (fn) => {
        counts.after++;
        after = fn;
      },
    },
    runBeforeAll: async () => {
      if (!before) throw new Error("no beforeAll registered");
      await before();
    },
    runAfterAll: async () => {
      if (!after) throw new Error("no afterAll registered");
      await after();
    },
  };
}

function fakeServer(url = "http://localhost:54321"): MockAgentServerLike & {
  start: ReturnType<typeof vi.fn>;
  stop: ReturnType<typeof vi.fn>;
  boundClient: MockAgentClient;
} {
  const boundClient = new MockAgentClient({ baseUrl: url });
  return {
    url,
    boundClient,
    start: vi.fn(async () => {}),
    stop: vi.fn(async () => {}),
    client: () => boundClient,
  };
}

const PROVIDER_KEYS = [
  "OPENAI_BASE_URL",
  "OPENAI_API_KEY",
  "ANTHROPIC_BASE_URL",
  "ANTHROPIC_API_KEY",
  "GOOGLE_GEMINI_BASE_URL",
  "GEMINI_API_KEY",
  "GOOGLE_API_KEY",
];

describe("providerEnv", () => {
  it("appends /v1 only for OpenAI and uses the bare URL for the others", () => {
    const env = providerEnv("http://localhost:9000");
    expect(env.OPENAI_BASE_URL).toBe("http://localhost:9000/v1");
    expect(env.ANTHROPIC_BASE_URL).toBe("http://localhost:9000");
    expect(env.GOOGLE_GEMINI_BASE_URL).toBe("http://localhost:9000");
    // Dummy keys so the SDKs will actually send a request.
    expect(env.OPENAI_API_KEY).toBeTruthy();
    expect(env.ANTHROPIC_API_KEY).toBeTruthy();
  });
});

describe("createSetup lifecycle", () => {
  // Snapshot+restore the provider env around every test so a leak can't bleed.
  let saved: Record<string, string | undefined>;
  beforeEach(() => {
    saved = {};
    for (const k of [...PROVIDER_KEYS, "CUSTOM_VAR"]) {
      saved[k] = process.env[k];
      delete process.env[k];
    }
  });
  afterEach(() => {
    for (const [k, v] of Object.entries(saved)) {
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    }
  });

  it("registers exactly one beforeAll and one afterAll", () => {
    const { hooks, counts } = captureHooks();
    createSetup(hooks, () => fakeServer())();
    expect(counts.before).toBe(1);
    expect(counts.after).toBe(1);
  });

  it("starts the server, patches env, then restores and stops", async () => {
    const { hooks, runBeforeAll, runAfterAll } = captureHooks();
    const srv = fakeServer("http://localhost:7777");
    const setup = createSetup(hooks, () => srv);
    const handle = setup({ agentsDir: "examples" });

    // Before the beforeAll runs, accessing the handle throws a clear error.
    expect(() => handle.server).toThrow(/not running yet/i);

    await runBeforeAll();
    expect(srv.start).toHaveBeenCalledOnce();
    expect(handle.url).toBe("http://localhost:7777");
    expect(handle.client).toBe(srv.boundClient);
    expect(process.env.OPENAI_BASE_URL).toBe("http://localhost:7777/v1");
    expect(process.env.ANTHROPIC_BASE_URL).toBe("http://localhost:7777");

    await runAfterAll();
    expect(srv.stop).toHaveBeenCalledOnce();
    // env unset again (was undefined before patch)
    expect(process.env.OPENAI_BASE_URL).toBeUndefined();
    expect(process.env.ANTHROPIC_BASE_URL).toBeUndefined();
    // handle is unbound again
    expect(() => handle.client).toThrow(/not running yet/i);
  });

  it("forwards server options and the start timeout", async () => {
    const { hooks, runBeforeAll } = captureHooks();
    const srv = fakeServer();
    const seen: unknown[] = [];
    const setup = createSetup(hooks, (opts) => {
      seen.push(opts);
      return srv;
    });
    setup({ agentsDir: "examples", port: 1234, startTimeoutMs: 250 });
    await runBeforeAll();
    expect(seen[0]).toEqual({ agentsDir: "examples", port: 1234 });
    expect(srv.start).toHaveBeenCalledWith(250);
  });

  it("does not patch provider env when patchEnv is false", async () => {
    const { hooks, runBeforeAll, runAfterAll } = captureHooks();
    const setup = createSetup(hooks, () => fakeServer());
    setup({ patchEnv: false });
    await runBeforeAll();
    // No provider env var is touched at all.
    for (const key of PROVIDER_KEYS) {
      expect(process.env[key]).toBeUndefined();
    }
    await runAfterAll(); // must not throw with no snapshot
  });

  it("merges extra env (overriding the provider patch) and restores it", async () => {
    const { hooks, runBeforeAll, runAfterAll } = captureHooks();
    const setup = createSetup(hooks, () => fakeServer("http://localhost:8888"));
    setup({
      env: { CUSTOM_VAR: "hello", OPENAI_API_KEY: "override-key" },
    });
    await runBeforeAll();
    expect(process.env.CUSTOM_VAR).toBe("hello");
    // extra env wins over the provider patch on a key collision
    expect(process.env.OPENAI_API_KEY).toBe("override-key");
    // ...but the base URL is still patched
    expect(process.env.OPENAI_BASE_URL).toBe("http://localhost:8888/v1");
    await runAfterAll();
    expect(process.env.CUSTOM_VAR).toBeUndefined();
    expect(process.env.OPENAI_API_KEY).toBeUndefined();
  });

  it("restores a pre-existing env var to its original value, not unset", async () => {
    process.env.OPENAI_API_KEY = "preexisting";
    const { hooks, runBeforeAll, runAfterAll } = captureHooks();
    const setup = createSetup(hooks, () => fakeServer());
    setup({});
    await runBeforeAll();
    expect(process.env.OPENAI_API_KEY).toBe("mock-key");
    await runAfterAll();
    expect(process.env.OPENAI_API_KEY).toBe("preexisting");
  });
});
