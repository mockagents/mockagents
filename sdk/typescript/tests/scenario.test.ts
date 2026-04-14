import { afterAll, beforeAll, describe, expect as vexpect, it } from "vitest";
import { createServer, Server } from "node:http";
import { AddressInfo } from "node:net";

import { MockAgentClient } from "../src/client.js";
import { Scenario, runScenario } from "../src/scenario.js";
import { expect } from "../src/assertions.js";

let server: Server;
let port: number;

// Server counts the user messages it has seen and returns a numbered
// response so the scenario can assert on per-step content.
let userTurn = 0;

beforeAll(async () => {
  server = createServer(async (req, res) => {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk as Buffer);
    if (req.method === "POST" && req.url === "/v1/chat/completions") {
      userTurn++;
      const payload = {
        id: `chatcmpl-${userTurn}`,
        model: "gpt-4o",
        choices: [
          {
            message: {
              content: `reply ${userTurn}`,
              tool_calls:
                userTurn === 1
                  ? [
                      {
                        id: "call_1",
                        function: {
                          name: "lookup_order",
                          arguments: JSON.stringify({ id: "ORD-1" }),
                        },
                      },
                    ]
                  : [],
            },
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 3, completion_tokens: 1, total_tokens: 4 },
      };
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(payload));
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

describe("runScenario", () => {
  it("walks user steps in order and collects responses", async () => {
    userTurn = 0;
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const scenario = new Scenario({
      name: "two-turn",
      steps: [
        { role: "user", content: "first" },
        { role: "user", content: "second" },
      ],
    });
    const result = await runScenario(client, scenario);
    vexpect(result.responses).toHaveLength(2);
    vexpect(result.responses[0].content).toBe("reply 1");
    vexpect(result.responses[1].content).toBe("reply 2");
    vexpect(result.lastContent).toBe("reply 2");
    vexpect(result.last.content).toBe("reply 2");
  });

  it("works with the expect() fluent assertions", async () => {
    userTurn = 0;
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const scenario = new Scenario({
      name: "order-lookup",
      steps: [{ role: "user", content: "where is my order" }],
    });
    const result = await runScenario(client, scenario);

    expect(result)
      .toHaveResponseContaining("reply 1")
      .toHaveToolCall("lookup_order", { id: "ORD-1" })
      .toHaveFinishReason("stop")
      .toHaveStatusCode(200);
  });

  it("throws on empty steps", () => {
    vexpect(() => new Scenario({ name: "x", steps: [] })).toThrow();
  });
});
