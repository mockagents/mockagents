// Client tests run against an in-process http.Server that mimics the
// MockAgents response shape for the handful of endpoints we care about.
// No real Go binary is spawned.

import { afterAll, beforeAll, describe, expect as vexpect, it } from "vitest";
import { createServer, Server } from "node:http";
import { AddressInfo } from "node:net";

import { MockAgentClient, parseAnthropicResponse, parseOpenAIResponse } from "../src/client.js";
import { HTTPError } from "../src/types.js";

let server: Server;
let port: number;

function sendJSON(res: import("node:http").ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

beforeAll(async () => {
  server = createServer(async (req, res) => {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk as Buffer);
    const body = chunks.length > 0 ? JSON.parse(Buffer.concat(chunks).toString()) : null;

    if (req.method === "POST" && req.url === "/v1/chat/completions") {
      sendJSON(res, 200, {
        id: "chatcmpl-1",
        model: body.model,
        choices: [
          {
            message: {
              content: "pong",
              tool_calls: [
                {
                  id: "call_1",
                  function: { name: "lookup_order", arguments: JSON.stringify({ id: "ORD-1" }) },
                },
              ],
            },
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 5, completion_tokens: 2, total_tokens: 7 },
      });
      return;
    }
    if (req.method === "POST" && req.url === "/v1/messages") {
      sendJSON(res, 200, {
        id: "msg_1",
        model: body.model,
        content: [
          { type: "text", text: "hello" },
          { type: "tool_use", id: "tu_1", name: "search", input: { q: "cats" } },
        ],
        stop_reason: "end_turn",
        usage: { input_tokens: 10, output_tokens: 4 },
      });
      return;
    }
    if (req.method === "GET" && req.url === "/api/v1/health") {
      sendJSON(res, 200, { status: "ok", version: "test" });
      return;
    }
    if (req.method === "GET" && req.url === "/api/v1/agents") {
      sendJSON(res, 200, [
        { name: "a1", model: "gpt-4o", protocol: "openai-chat-completions", scenario_count: 1, tool_count: 0 },
      ]);
      return;
    }
    if (req.method === "GET" && req.url === "/api/v1/agents/boom") {
      sendJSON(res, 404, { error: "not found" });
      return;
    }
    res.writeHead(404);
    res.end();
  });
  await new Promise<void>((resolve) => server.listen(0, resolve));
  port = (server.address() as AddressInfo).port;
});

afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

describe("MockAgentClient.chat", () => {
  it("parses content, tool calls, and usage", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const resp = await client.chat([{ role: "user", content: "ping" }]);
    vexpect(resp.content).toBe("pong");
    vexpect(resp.finishReason).toBe("stop");
    vexpect(resp.toolCalls).toHaveLength(1);
    vexpect(resp.toolCalls[0]).toMatchObject({
      id: "call_1",
      name: "lookup_order",
      arguments: { id: "ORD-1" },
    });
    vexpect(resp.usage?.totalTokens).toBe(7);
    vexpect(resp.statusCode).toBe(200);
    vexpect(resp.latencyMs).toBeGreaterThanOrEqual(0);
  });

  it("propagates the session id via header", async () => {
    const seen: string[] = [];
    const spyFetch: typeof fetch = (input, init) => {
      const headers = init?.headers as Record<string, string> | undefined;
      if (headers && headers["X-Session-Id"]) seen.push(headers["X-Session-Id"]);
      return fetch(input, init);
    };
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}`, fetch: spyFetch });
    await client.chat([{ role: "user", content: "hi" }], { sessionId: "sess-42" });
    vexpect(seen).toEqual(["sess-42"]);
  });
});

describe("MockAgentClient.message", () => {
  it("joins text blocks and extracts tool_use blocks", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const resp = await client.message([{ role: "user", content: "hi" }]);
    vexpect(resp.content).toContain("hello");
    vexpect(resp.toolCalls).toHaveLength(1);
    vexpect(resp.toolCalls[0].arguments).toEqual({ q: "cats" });
    vexpect(resp.finishReason).toBe("end_turn");
    vexpect(resp.usage?.totalTokens).toBe(14);
  });
});

describe("MockAgentClient management API", () => {
  it("returns health", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const h = await client.health();
    vexpect(h.status).toBe("ok");
  });

  it("lists agents", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const agents = await client.listAgents();
    vexpect(agents).toHaveLength(1);
    vexpect(agents[0].name).toBe("a1");
  });

  it("throws HTTPError on 404", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    await vexpect(client.getAgent("boom")).rejects.toBeInstanceOf(HTTPError);
  });
});

describe("pure parsers", () => {
  it("parseOpenAIResponse handles malformed payloads without throwing", () => {
    const resp = parseOpenAIResponse({}, 200, 5);
    vexpect(resp.content).toBe("");
    vexpect(resp.toolCalls).toEqual([]);
  });

  it("parseAnthropicResponse handles malformed payloads", () => {
    const resp = parseAnthropicResponse({}, 200, 5);
    vexpect(resp.content).toBe("");
    vexpect(resp.toolCalls).toEqual([]);
  });
});
