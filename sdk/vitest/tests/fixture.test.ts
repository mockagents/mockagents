// Tests that mockagentsFixture wires the handle through Vitest's fixture
// injection. Uses a fake handle, so no binary is spawned.

import { describe, expect, test as baseTest } from "vitest";

import { MockAgentClient, MockAgentServer } from "@mockagents/sdk";
import type { MockAgentsHandle, MockAgentServerLike } from "../src/core.js";
import { mockagentsFixture } from "../src/index.js";

const fakeServer = new MockAgentServer({ port: 6543 });
const fakeClient = new MockAgentClient({ baseUrl: "http://localhost:6543" });

const fakeHandle: MockAgentsHandle = {
  get server(): MockAgentServerLike {
    return fakeServer;
  },
  get client(): MockAgentClient {
    return fakeClient;
  },
  get url(): string {
    return fakeServer.url;
  },
};

describe("mockagentsFixture", () => {
  const test = mockagentsFixture(fakeHandle, baseTest);

  test("injects the server fixture from the handle", ({ mockagents }) => {
    expect(mockagents).toBe(fakeServer);
    expect(mockagents.url).toBe("http://localhost:6543");
  });

  test("injects the client fixture from the handle", ({ mockagentsClient }) => {
    expect(mockagentsClient).toBe(fakeClient);
  });
});
