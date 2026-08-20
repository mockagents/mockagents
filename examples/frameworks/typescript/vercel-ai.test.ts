/**
 * Vercel AI SDK against MockAgents — a runnable recipe.
 *
 *     npm install && npm test
 *
 * The AI SDK builds its own HTTP client through a provider factory, so there is
 * no base-URL environment variable to patch: you create the provider pointed at
 * the mock and pass the model it returns. That is the entire integration.
 *
 * This file starts the mock itself rather than using `@mockagents/vitest`, to
 * keep the recipe to one dependency you might not already have. In a real
 * suite, `setupMockAgents({ agentsDir: "../agents" })` replaces the
 * beforeAll/afterAll below.
 */
import { createOpenAI } from "@ai-sdk/openai";
import { generateText, streamText, tool } from "ai";
import { spawn, type ChildProcess } from "node:child_process";
import { createServer } from "node:net";
import { afterAll, beforeAll, expect, test } from "vitest";
import { z } from "zod";

const MODEL = "gpt-4o-frameworks";

let server: ChildProcess;
let baseURL: string;

/** Ask the kernel for a free port so parallel suites never collide. */
async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, () => {
      const { port } = srv.address() as { port: number };
      srv.close(() => resolve(port));
    });
  });
}

beforeAll(async () => {
  const bin = process.env.MOCKAGENTS_BIN ?? "mockagents";
  const port = await freePort();
  baseURL = `http://127.0.0.1:${port}/v1`;
  server = spawn(
    bin,
    ["start", "--port", String(port), "--agents-dir", "../agents", "--log-level", "warn"],
    { stdio: "ignore" },
  );
  // Poll readiness rather than sleeping: /api/v1/ready 503s until the fixtures
  // are actually loaded, so a 200 means the next line can safely make a call.
  const deadline = Date.now() + 15_000;
  for (;;) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/api/v1/ready`);
      if (res.ok) break;
    } catch {
      /* not up yet */
    }
    if (Date.now() > deadline) throw new Error("mock server never became ready");
    await new Promise((r) => setTimeout(r, 100));
  }
}, 30_000);

afterAll(() => {
  server?.kill();
});

/** One provider pointed at the mock. This is the whole redirect. */
function mockProvider() {
  return createOpenAI({ baseURL, apiKey: "mock-key" });
}

const lookupOrder = tool({
  description: "Look up an order by id.",
  inputSchema: z.object({ order_id: z.string() }),
  execute: async ({ order_id }) => ({ order_id, status: "shipped", tracking: "1Z999AA1" }),
});

test("generateText reaches the mock", async () => {
  const { text } = await generateText({
    model: mockProvider()(MODEL),
    prompt: "hello there",
  });
  expect(text).toContain("order"); // the fixture's default nudge
});

test("the mock decides which tool is called, and with what", async () => {
  const { toolCalls } = await generateText({
    model: mockProvider()(MODEL),
    prompt: "where is my order?",
    tools: { lookup_order: lookupOrder },
    // One step: assert the CALL the model made, without running the loop.
    stopWhen: () => true,
  });

  expect(toolCalls).toHaveLength(1);
  expect(toolCalls[0].toolName).toBe("lookup_order");
  expect(toolCalls[0].input).toEqual({ order_id: "ORD-42" });
});

test("streamText yields real SSE deltas", async () => {
  const result = streamText({
    model: mockProvider()(MODEL),
    prompt: "hello there",
  });

  const chunks: string[] = [];
  for await (const delta of result.textStream) chunks.push(delta);

  // Chunked, not one blob: the mock streams token deltas over real SSE, so a
  // parser bug in your app surfaces here rather than in production.
  expect(chunks.length).toBeGreaterThan(1);
  expect(chunks.join("")).toContain("order");
});
