// @mockagents/vitest — Vitest entry point.
//
// `setupMockAgents()` spawns the MockAgents server once per suite (in a
// `beforeAll`) and redirects the OpenAI / Anthropic / Gemini SDK environment
// variables at it, so your existing application code reaches the mock with no
// changes. `mockagentsFixture()` layers idiomatic per-test fixtures on top.

import { afterAll, beforeAll, test as baseTest } from "vitest";
import type { TestAPI } from "vitest";

import { MockAgentClient, MockAgentServer, createSetup } from "./core.js";
import type { MockAgentsHandle } from "./core.js";

/**
 * Spawn a MockAgents server for the current test file and (by default) point
 * the provider SDK env vars at it. Registers a `beforeAll` to start + patch and
 * an `afterAll` to restore + stop.
 *
 * ```ts
 * import { setupMockAgents } from "@mockagents/vitest";
 * import { expect, test } from "vitest";
 *
 * const mock = setupMockAgents({ agentsDir: "examples" });
 *
 * test("the mock answers", async () => {
 *   const res = await mock.client.chat([{ role: "user", content: "hi" }]);
 *   expect(res.content).toBeTruthy();
 * });
 * ```
 */
export const setupMockAgents = createSetup({ beforeAll, afterAll });

/** Fixtures injected by {@link mockagentsFixture}. */
export interface MockAgentsFixtures {
  /** The running MockAgents server. */
  mockagents: MockAgentServer;
  /** A client bound to the running server. */
  mockagentsClient: MockAgentClient;
}

/**
 * Wrap a {@link MockAgentsHandle} as a Vitest test with `mockagents` and
 * `mockagentsClient` fixtures, for code that prefers Vitest's fixture-injection
 * style over reaching through the handle:
 *
 * ```ts
 * const mock = setupMockAgents({ agentsDir: "examples" });
 * const test = mockagentsFixture(mock);
 *
 * test("uses the injected client", async ({ mockagentsClient }) => {
 *   const res = await mockagentsClient.chat([{ role: "user", content: "hi" }]);
 *   expect(res.content).toBeTruthy();
 * });
 * ```
 *
 * The handle is shared, so the server is still started once by
 * `setupMockAgents`'s `beforeAll`; the fixtures only forward it per test.
 */
export function mockagentsFixture(
  handle: MockAgentsHandle,
  base: TestAPI = baseTest,
): TestAPI<MockAgentsFixtures> {
  return base.extend<MockAgentsFixtures>({
    // `handle.server` is typed `MockAgentServerLike`, but `setupMockAgents`'s
    // default factory always yields a concrete `MockAgentServer`, so exposing
    // the richer type to consumers is sound; the fixture only forwards it.
    // eslint-disable-next-line no-empty-pattern
    mockagents: async ({}, use) => {
      await use(handle.server as MockAgentServer);
    },
    // eslint-disable-next-line no-empty-pattern
    mockagentsClient: async ({}, use) => {
      await use(handle.client);
    },
  });
}

export {
  createSetup,
  providerEnv,
  MockAgentClient,
  MockAgentServer,
} from "./core.js";
export type {
  MockAgentsHandle,
  MockAgentServerLike,
  SetupOptions,
  LifecycleHooks,
  MockAgentServerOptions,
} from "./core.js";
