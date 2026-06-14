// Framework-agnostic core for @mockagents/vitest.
//
// The lifecycle wiring is parameterized over a pair of `beforeAll` / `afterAll`
// hooks and a server factory so the exact same logic powers both the Vitest
// entry point (`./index.ts`, which binds Vitest's hooks) and the Jest entry
// point (`./jest.ts`, which binds Jest's globals) — and so the wiring can be
// unit-tested with fakes, without spawning the Go binary.

import { MockAgentClient, MockAgentServer } from "@mockagents/sdk";
import type { MockAgentServerOptions } from "@mockagents/sdk";

/** The subset of {@link MockAgentServer} the lifecycle depends on. Lets tests
 * substitute a fake server without spawning the real binary. */
export interface MockAgentServerLike {
  readonly url: string;
  start(timeoutMs?: number): Promise<void>;
  stop(timeoutMs?: number): Promise<void>;
  client(): MockAgentClient;
}

export interface SetupOptions extends MockAgentServerOptions {
  /**
   * Patch the `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` / `GOOGLE_GEMINI_BASE_URL`
   * (and matching dummy API-key) environment variables to point at the mock for
   * the lifetime of the suite, so application code under test reaches the mock
   * with zero changes. Restored after the suite. Default `true`.
   */
  patchEnv?: boolean;
  /**
   * Extra environment variables to set for the duration of the suite, merged
   * *after* the provider patch (so they win on a key collision). Restored to
   * their prior values — or unset, if they were unset — afterwards.
   */
  env?: Record<string, string>;
  /** Health-check timeout (ms) passed to `server.start()`. */
  startTimeoutMs?: number;
}

/** A started-on-demand MockAgents server plus a bound client. Returned by
 * {@link createSetup}'s `setupMockAgents`; the underlying server is spawned in
 * the registered `beforeAll`, so `.server` / `.client` / `.url` only resolve
 * once the suite is running. */
export interface MockAgentsHandle {
  /** The running server. Throws if accessed before the `beforeAll` ran. */
  readonly server: MockAgentServerLike;
  /** A client bound to the running server. Throws before the `beforeAll` ran. */
  readonly client: MockAgentClient;
  /** The base URL of the running server. Throws before the `beforeAll` ran. */
  readonly url: string;
}

/** The two lifecycle hooks the setup needs. Both Vitest and Jest expose
 * compatible `beforeAll` / `afterAll` functions. */
export interface LifecycleHooks {
  beforeAll: (fn: () => void | Promise<void>) => unknown;
  afterAll: (fn: () => void | Promise<void>) => unknown;
}

const NOT_STARTED =
  "MockAgents server is not running yet. `setupMockAgents()` starts it in a " +
  "`beforeAll` hook — access `.server` / `.client` / `.url` from inside a test " +
  "or `beforeEach`, not at the top level of the module.";

class HandleImpl implements MockAgentsHandle {
  private srv: MockAgentServerLike | null = null;
  private cli: MockAgentClient | null = null;

  /** @internal */
  bind(server: MockAgentServerLike): void {
    this.srv = server;
    this.cli = server.client();
  }

  /** @internal */
  unbind(): void {
    this.srv = null;
    this.cli = null;
  }

  get server(): MockAgentServerLike {
    if (!this.srv) throw new Error(NOT_STARTED);
    return this.srv;
  }

  get client(): MockAgentClient {
    if (!this.cli) throw new Error(NOT_STARTED);
    return this.cli;
  }

  get url(): string {
    return this.server.url;
  }
}

/** The environment variables the provider SDKs read for base-URL redirection,
 * paired with dummy keys (the official clients refuse to send a request without
 * *some* key, even against a mock). Mirrors the Python `pytest` plugin. */
export function providerEnv(url: string): Record<string, string> {
  return {
    // OpenAI (and OpenAI-compatible clients honoring OPENAI_BASE_URL). The
    // `/v1` suffix matches what the official SDK appends to its base URL.
    OPENAI_BASE_URL: `${url}/v1`,
    OPENAI_API_KEY: "mock-key",
    // Anthropic — no `/v1` suffix; the SDK appends the full path itself.
    ANTHROPIC_BASE_URL: url,
    ANTHROPIC_API_KEY: "mock-key",
    // Google Gemini — honored by the newer `google-genai` client (and the
    // Vercel AI SDK's Google provider) only; the legacy `google-generativeai`
    // package has no base-URL env var.
    GOOGLE_GEMINI_BASE_URL: url,
    GEMINI_API_KEY: "mock-key",
    GOOGLE_API_KEY: "mock-key",
  };
}

/** A record of each touched env var's prior value (`undefined` = was unset),
 * captured once at patch time so restore is exact and idempotent. */
type EnvSnapshot = Map<string, string | undefined>;

function applyEnv(vars: Record<string, string>): EnvSnapshot {
  const snapshot: EnvSnapshot = new Map();
  for (const [key, value] of Object.entries(vars)) {
    // Snapshot the ORIGINAL value once, even if the same key appears twice.
    if (!snapshot.has(key)) snapshot.set(key, process.env[key]);
    process.env[key] = value;
  }
  return snapshot;
}

function restoreEnv(snapshot: EnvSnapshot): void {
  for (const [key, prev] of snapshot) {
    if (prev === undefined) delete process.env[key];
    else process.env[key] = prev;
  }
}

/** Build a `setupMockAgents` bound to the given lifecycle hooks (and,
 * optionally, an alternate server factory for tests). Exported so the Vitest
 * and Jest entry points can each bind their framework's hooks. */
export function createSetup(
  hooks: LifecycleHooks,
  makeServer: (options: MockAgentServerOptions) => MockAgentServerLike = (
    options,
  ) => new MockAgentServer(options),
): (options?: SetupOptions) => MockAgentsHandle {
  return function setupMockAgents(options: SetupOptions = {}): MockAgentsHandle {
    const {
      patchEnv = true,
      env: extraEnv,
      startTimeoutMs,
      ...serverOptions
    } = options;

    const handle = new HandleImpl();
    let envSnapshot: EnvSnapshot | null = null;
    let started: MockAgentServerLike | null = null;

    hooks.beforeAll(async () => {
      const server = makeServer(serverOptions);
      await server.start(startTimeoutMs);
      started = server;
      handle.bind(server);

      const vars: Record<string, string> = {
        ...(patchEnv ? providerEnv(server.url) : {}),
        ...(extraEnv ?? {}),
      };
      if (Object.keys(vars).length > 0) {
        envSnapshot = applyEnv(vars);
      }
    });

    hooks.afterAll(async () => {
      if (envSnapshot) {
        restoreEnv(envSnapshot);
        envSnapshot = null;
      }
      handle.unbind();
      if (started) {
        const server = started;
        started = null;
        await server.stop();
      }
    });

    return handle;
  };
}

// Re-exported for convenience so consumers can `new MockAgentServer(...)` or
// type against the client without a second `@mockagents/sdk` import.
export { MockAgentClient, MockAgentServer };
export type { MockAgentServerOptions };
