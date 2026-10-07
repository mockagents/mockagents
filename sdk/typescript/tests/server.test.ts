import { EventEmitter } from "node:events";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

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

  it("url reflects the configured port on IPv4 loopback", () => {
    // 127.0.0.1, not localhost: the binary binds IPv4 only (review K-19).
    const server = new MockAgentServer({ agentsDir: "./examples", port: 12345 });
    expect(server.url).toBe("http://127.0.0.1:12345");
    expect(server.client().baseUrl).toBe("http://127.0.0.1:12345");
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
  it("bounds retained subprocess logs to the most recent 8 MiB", () => {
    const server = new MockAgentServer({ binaryPath: "unused" });
    const appendLog = (server as unknown as { appendLog(value: string): void }).appendLog.bind(server);
    appendLog("old-prefix");
    appendLog("x".repeat(8 * 1024 * 1024));

    expect(Buffer.byteLength(server.getLogs().join(""))).toBeLessThanOrEqual(8 * 1024 * 1024);
    expect(server.getLogs().join("")).not.toContain("old-prefix");
  });

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

// Review K-22: both env-var names, the same precedence as the Python SDK and
// the npx launcher, and no walk up into parent directories.
describe("findBinary discovery", () => {
  const saved = {
    binary: process.env.MOCKAGENTS_BINARY,
    bin: process.env.MOCKAGENTS_BIN,
    cwd: process.cwd(),
  };
  let tmp: string | undefined;

  afterEach(() => {
    process.chdir(saved.cwd);
    for (const [key, value] of [
      ["MOCKAGENTS_BINARY", saved.binary],
      ["MOCKAGENTS_BIN", saved.bin],
    ] as const) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    if (tmp) rmSync(tmp, { recursive: true, force: true });
    tmp = undefined;
  });

  function fakeFile(name: string): string {
    tmp ??= mkdtempSync(join(tmpdir(), "ma-ts-bin-"));
    const p = join(tmp, name);
    writeFileSync(p, "x");
    return p;
  }

  const cases: Array<{ name: string; binary?: string; bin?: string; want: "binary" | "bin" | "fallback" }> = [
    { name: "MOCKAGENTS_BINARY only", binary: "a", want: "binary" },
    { name: "MOCKAGENTS_BIN only", bin: "b", want: "bin" },
    { name: "both set: MOCKAGENTS_BINARY wins", binary: "a", bin: "b", want: "binary" },
    { name: "MOCKAGENTS_BINARY missing on disk: MOCKAGENTS_BIN used", binary: "missing", bin: "b", want: "bin" },
    { name: "neither set", want: "fallback" },
  ];
  for (const c of cases) {
    it(c.name, () => {
      delete process.env.MOCKAGENTS_BINARY;
      delete process.env.MOCKAGENTS_BIN;
      const a = fakeFile("fake-a");
      const b = fakeFile("fake-b");
      if (c.binary) process.env.MOCKAGENTS_BINARY = c.binary === "missing" ? join(tmp!, "nope") : a;
      if (c.bin) process.env.MOCKAGENTS_BIN = b;
      const got = findBinary();
      if (c.want === "binary") expect(got).toBe(a);
      else if (c.want === "bin") expect(got).toBe(b);
      else expect(got).toMatch(/^mockagents(\.exe)?$/);
    });
  }

  it("does not pick up a binary from a parent directory", () => {
    delete process.env.MOCKAGENTS_BINARY;
    delete process.env.MOCKAGENTS_BIN;
    const name = process.platform === "win32" ? "mockagents.exe" : "mockagents";
    fakeFile(name); // <tmp>/mockagents — a stale "repo root" binary
    const nested = join(tmp!, "sdk", "typescript");
    mkdirSync(nested, { recursive: true });
    process.chdir(nested);
    expect(findBinary()).toBe(name);
  });
});

// Review K-23: a child that dies before /health passes must fail start()
// promptly, not after the full health timeout.
describe("MockAgentServer early exit", () => {
  it("rejects fast with the exit status when the child exits before ready", async () => {
    // `node start --port …` exits at once (no module named "start").
    const server = new MockAgentServer({ binaryPath: process.execPath, agentsDir: "." });
    const started = Date.now();
    await expect(server.start(10_000)).rejects.toThrow(/exited before becoming ready/);
    expect(Date.now() - started).toBeLessThan(5_000);
    expect(server.isRunning).toBe(false);
  });
});
