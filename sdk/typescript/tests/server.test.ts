import { describe, expect, it } from "vitest";

import { findBinary, findFreePort, MockAgentServer } from "../src/server.js";

describe("MockAgentServer pure logic", () => {
  it("findFreePort returns a listenable port", async () => {
    const port = await findFreePort();
    expect(port).toBeGreaterThan(0);
    expect(port).toBeLessThan(65536);
  });

  it("findBinary falls back to bare binary name", () => {
    // We don't assert the binary exists — just that findBinary returns
    // _some_ string we can pass to spawn.
    const name = findBinary();
    expect(typeof name).toBe("string");
    expect(name.length).toBeGreaterThan(0);
  });

  it("isRunning is false before start()", () => {
    const server = new MockAgentServer({ agentsDir: "./examples" });
    expect(server.isRunning).toBe(false);
  });

  it("url reflects the configured port", () => {
    const server = new MockAgentServer({ agentsDir: "./examples", port: 12345 });
    expect(server.url).toBe("http://localhost:12345");
  });

  it("stop() is a no-op when never started", async () => {
    const server = new MockAgentServer({ agentsDir: "./examples" });
    await server.stop(); // must not throw
    expect(server.isRunning).toBe(false);
  });
});
