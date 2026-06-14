// End-to-end test: spawn the real `mockagents` Go binary via setupMockAgents,
// patch the provider env, and exercise a real chat round-trip. Skipped when the
// binary is absent (e.g. a checkout that hasn't run `make build`), so the unit
// suite stays green everywhere.

import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { setupMockAgents } from "../src/index.js";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../../.."); // tests -> vitest -> sdk -> repo root
const binary = process.env.MOCKAGENTS_BIN ?? resolve(repoRoot, "mockagents");
const agentsDir = resolve(here, "fixtures");

describe.skipIf(!existsSync(binary))("e2e against the real binary", () => {
  const mock = setupMockAgents({
    agentsDir,
    binaryPath: binary,
    logLevel: "error",
  });

  it("patches the OpenAI base-URL env var at the running mock", () => {
    expect(process.env.OPENAI_BASE_URL).toBe(`${mock.url}/v1`);
    expect(process.env.OPENAI_API_KEY).toBeTruthy();
  });

  it("serves a deterministic chat completion from the fixture agent", async () => {
    const res = await mock.client.chat([{ role: "user", content: "ping" }], {
      model: "vitest-echo-model",
    });
    expect(res.statusCode).toBe(200);
    expect(res.content).toContain("pong");
  });

  it("exposes a reachable health endpoint", async () => {
    const health = await mock.client.health();
    expect(health.status).toBe("ok");
  });
});
