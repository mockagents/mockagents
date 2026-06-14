// @mockagents/vitest/jest — Jest entry point.
//
// Jest injects `beforeAll` / `afterAll` as globals, so this entry binds them
// off `globalThis` rather than importing from `vitest`. Importing this module
// therefore pulls in *no* test-framework dependency — the hooks are only
// touched when `setupMockAgents()` is called inside a Jest test file.

import { createSetup } from "./core.js";

type Hook = (fn: () => void | Promise<void>) => unknown;

const globalHooks = globalThis as unknown as {
  beforeAll?: Hook;
  afterAll?: Hook;
};

function requireHook(name: "beforeAll" | "afterAll"): Hook {
  const hook = globalHooks[name];
  if (typeof hook !== "function") {
    throw new Error(
      `@mockagents/vitest/jest: global \`${name}\` was not found. ` +
        "Call setupMockAgents() from inside a Jest test file (the global " +
        "lifecycle hooks are only available there).",
    );
  }
  return hook;
}

/**
 * Jest-flavored {@link setupMockAgents}. Same ergonomics as the Vitest entry,
 * but wired to Jest's global `beforeAll` / `afterAll`:
 *
 * ```ts
 * import { setupMockAgents } from "@mockagents/vitest/jest";
 *
 * const mock = setupMockAgents({ agentsDir: "examples" });
 *
 * test("the mock answers", async () => {
 *   const res = await mock.client.chat([{ role: "user", content: "hi" }]);
 *   expect(res.content).toBeTruthy();
 * });
 * ```
 */
export const setupMockAgents = createSetup({
  // Resolve the hooks lazily (at registration time, inside the test file) so
  // importing this module outside a Jest run does not throw.
  beforeAll: (fn) => requireHook("beforeAll")(fn),
  afterAll: (fn) => requireHook("afterAll")(fn),
});

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
