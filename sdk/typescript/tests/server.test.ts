import { EventEmitter } from "node:events";
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

class FakeChild extends EventEmitter {
  exitCode: number | null = null;
  signalCode: NodeJS.Signals | null = null;
  killed = false;
  signals: NodeJS.Signals[] = [];

  kill(signal: NodeJS.Signals = "SIGTERM"): boolean {
    this.killed = true;
    this.signals.push(signal);
    if (signal === "SIGKILL") {
      this.signalCode = signal;
      queueMicrotask(() => this.emit("exit", null, signal));
    }
    return true;
  }
}

describe("MockAgentServer process lifecycle", () => {
  it("surfaces spawn errors without retaining a process handle", async () => {
    const server = new MockAgentServer({ binaryPath: "definitely-missing-mockagents-binary", port: 65530 });
    await expect(server.start(1_000)).rejects.toThrow(/did not become ready/);
    expect(server.isRunning).toBe(false);
  });

  it("escalates when SIGTERM was delivered but the child did not exit", async () => {
    const server = new MockAgentServer({ binaryPath: "unused" });
    const child = new FakeChild();
    (server as unknown as { process: FakeChild }).process = child;

    await server.stop(5);

    expect(child.signals).toEqual(["SIGTERM", "SIGKILL"]);
    expect(server.isRunning).toBe(false);
  });

  it("coalesces concurrent stop calls and signals once", async () => {
    const server = new MockAgentServer({ binaryPath: "unused" });
    const child = new FakeChild();
    (server as unknown as { process: FakeChild }).process = child;

    await Promise.all([server.stop(5), server.stop(5)]);
    expect(child.signals).toEqual(["SIGTERM", "SIGKILL"]);
  });

  it("does not signal an already-exited child", async () => {
    const server = new MockAgentServer({ binaryPath: "unused" });
    const child = new FakeChild();
    child.exitCode = 1;
    (server as unknown as { process: FakeChild }).process = child;
    await server.stop(5);
    expect(child.signals).toEqual([]);
  });
});
