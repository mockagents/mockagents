// MockAgentServer — manages the Go binary as a child process with
// automatic free-port discovery and health-check polling. Mirrors the
// Python SDK's MockAgentServer surface.

import { type ChildProcess, spawn } from "node:child_process";
import { createServer } from "node:net";
import { existsSync } from "node:fs";
import { platform } from "node:os";
import { join } from "node:path";

import { MockAgentClient } from "./client.js";
import { ConfigError, ServerError } from "./types.js";

const MAX_CAPTURED_LOG_BYTES = 8 * 1024 * 1024;

export interface MockAgentServerOptions {
  agentsDir?: string;
  /** Port to listen on. 0 (default) auto-selects a free port. */
  port?: number;
  /** Path to the `mockagents` binary. Auto-detected when omitted. */
  binaryPath?: string;
  logLevel?: "debug" | "info" | "warn" | "error";
}

export class MockAgentServer {
  public readonly agentsDir: string;
  public port: number;
  public readonly binaryPath: string;
  public readonly logLevel: string;
  private process: ChildProcess | null = null;
  private stopPromise: Promise<void> | null = null;
  private logs: string[] = [];
  private logBytes = 0;

  constructor(options: MockAgentServerOptions = {}) {
    this.agentsDir = options.agentsDir ?? "./agents";
    this.port = options.port ?? 0;
    this.logLevel = options.logLevel ?? "warn";
    this.binaryPath = options.binaryPath ?? findBinary();
  }

  get url(): string {
    return `http://localhost:${this.port}`;
  }

  get isRunning(): boolean {
    return this.process !== null && !hasExited(this.process);
  }

  /** Returns a client pre-configured for this server. */
  client(): MockAgentClient {
    return new MockAgentClient({ baseUrl: this.url });
  }

  /** Returns the combined stderr/stdout captured since start. */
  getLogs(): string[] {
    return [...this.logs];
  }

  /**
   * Spawn the Go binary and wait for the health endpoint to respond.
   * Throws ServerError on spawn failure and Error on health-check timeout.
   */
  async start(timeoutMs: number = 10_000): Promise<void> {
    if (this.isRunning) return;

    this.logs = [];
    this.logBytes = 0;

    if (this.port === 0) {
      this.port = await findFreePort();
    }

    const args = [
      "start",
      "--port",
      String(this.port),
      "--agents-dir",
      this.agentsDir,
      "--log-level",
      this.logLevel,
    ];

    this.process = spawn(this.binaryPath, args, {
      stdio: ["ignore", "pipe", "pipe"],
    });

    const healthAbort = new AbortController();
    let spawnError: Error | null = null;
    const spawnFailure = new Promise<never>((_resolve, reject) => this.process?.once("error", (err) => {
      spawnError = err;
      this.appendLog(`spawn error: ${err.message}`);
      healthAbort.abort();
      reject(err);
    }));
    this.process.stdout?.on("data", (chunk: Buffer) => {
      this.appendLog(chunk.toString());
    });
    this.process.stderr?.on("data", (chunk: Buffer) => {
      this.appendLog(chunk.toString());
    });

    try {
      await Promise.race([waitForHealth(this.url, timeoutMs, healthAbort.signal), spawnFailure]);
    } catch (err) {
      healthAbort.abort();
      if (spawnError) this.process = null;
      else await this.stop();
      throw new ServerError(
        `server did not become ready within ${timeoutMs}ms: ${(err as Error).message}\n` +
          `logs:\n${this.logs.join("")}`,
      );
    }
  }

  /** Terminate the subprocess if it is running. Safe to call repeatedly. */
  async stop(timeoutMs: number = 5_000): Promise<void> {
    if (!this.process) return;
    if (this.stopPromise) return this.stopPromise;
    const proc = this.process;
    this.stopPromise = (async () => {
      if (hasExited(proc)) {
        if (this.process === proc) this.process = null;
        return;
      }

      proc.kill("SIGTERM");
      if (!(await waitForExit(proc, timeoutMs))) {
        // ChildProcess.killed only means signal delivery succeeded. Completion
        // is represented by exitCode/signalCode or an exit/close event.
        if (!hasExited(proc)) proc.kill("SIGKILL");
        if (!(await waitForExit(proc, Math.max(timeoutMs, 1_000)))) {
          throw new ServerError("server process did not exit after SIGKILL");
        }
      }
      if (this.process === proc) this.process = null;
    })();
    try {
      await this.stopPromise;
    } finally {
      this.stopPromise = null;
    }
  }

  private appendLog(value: string): void {
    let bytes = Buffer.byteLength(value);
    if (bytes > MAX_CAPTURED_LOG_BYTES) {
      value = Buffer.from(value).subarray(bytes - MAX_CAPTURED_LOG_BYTES).toString();
      bytes = Buffer.byteLength(value);
      while (bytes > MAX_CAPTURED_LOG_BYTES) {
        value = value.slice(1);
        bytes = Buffer.byteLength(value);
      }
    }
    this.logs.push(value);
    this.logBytes += bytes;
    while (this.logBytes > MAX_CAPTURED_LOG_BYTES && this.logs.length > 1) {
      const removed = this.logs.shift();
      if (removed !== undefined) this.logBytes -= Buffer.byteLength(removed);
    }
  }
}

function hasExited(proc: ChildProcess): boolean {
  return proc.exitCode !== null || proc.signalCode !== null;
}

function waitForExit(proc: ChildProcess, timeoutMs: number): Promise<boolean> {
  if (hasExited(proc)) return Promise.resolve(true);
  return new Promise((resolve) => {
    let settled = false;
    let timer: NodeJS.Timeout | undefined;
    const finish = (exited: boolean) => {
      if (settled) return;
      settled = true;
      if (timer) clearTimeout(timer);
      proc.off("exit", onExit);
      proc.off("close", onExit);
      resolve(exited);
    };
    const onExit = () => finish(true);
    proc.once("exit", onExit);
    proc.once("close", onExit);
    timer = setTimeout(() => finish(hasExited(proc)), timeoutMs);
    timer.unref();
  });
}

/** Resolve a free TCP port by asking the kernel for one. */
export async function findFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, () => {
      const addr = srv.address();
      if (!addr || typeof addr === "string") {
        reject(new Error("unable to determine free port"));
        return;
      }
      const port = addr.port;
      srv.close(() => resolve(port));
    });
  });
}

/**
 * Locate the `mockagents` binary. Honors MOCKAGENTS_BIN, then checks the
 * monorepo root, then falls back to whatever the OS PATH finds via the
 * bare name.
 */
export function findBinary(): string {
  const envBin = process.env.MOCKAGENTS_BIN;
  if (envBin && existsSync(envBin)) return envBin;

  const binaryName = platform() === "win32" ? "mockagents.exe" : "mockagents";
  const repoRoot = join(process.cwd(), "..", "..", binaryName);
  if (existsSync(repoRoot)) return repoRoot;
  const localRoot = join(process.cwd(), binaryName);
  if (existsSync(localRoot)) return localRoot;

  // Fall back to PATH lookup at spawn time.
  return binaryName;
}

async function waitForHealth(baseUrl: string, timeoutMs: number, signal?: AbortSignal): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown;
  while (Date.now() < deadline) {
    if (signal?.aborted) throw new Error("health check aborted");
    try {
      const resp = await fetch(`${baseUrl}/api/v1/health`, {
        signal: AbortSignal.timeout(500),
      });
      if (resp.ok) return;
    } catch (err) {
      lastErr = err;
    }
    await sleep(100);
  }
  throw new Error(`health check failed: ${lastErr ?? "timeout"}`);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// Re-exported so tests can construct ConfigError without pulling in types.js.
export { ConfigError };
