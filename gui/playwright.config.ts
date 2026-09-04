import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { defineConfig, devices } from "@playwright/test";

// UX-08: browser smoke tests against a REAL MockAgents server.
//
// Epic §10 is explicit that a green TypeScript compile is not evidence of UX
// correctness, so this layer boots the actual Go binary with the documented
// example fixtures and drives the production Next.js build against it. Nothing
// here is mocked: a broken API contract must fail these tests.
//
// Ports are offset from the developer defaults (8080/3001) so a smoke run
// cannot collide with, or silently pass against, a server the developer is
// already running.
const API_PORT = Number(process.env.SMOKE_API_PORT ?? 8099);
const GUI_PORT = Number(process.env.SMOKE_GUI_PORT ?? 3099);

const API_URL = `http://127.0.0.1:${API_PORT}`;
const GUI_URL = `http://127.0.0.1:${GUI_PORT}`;

// Built by `make build` (or the CI step) into the repository root. Resolved to
// an absolute path: Playwright spawns webServer commands through the platform
// shell, and a relative "../" is not portable across cmd.exe and sh.
// (Playwright loads this config as CommonJS, so __dirname is the portable
// anchor here — import.meta is unavailable.)
const REPO_ROOT = path.resolve(__dirname, "..");

// The suite exercises WRITE endpoints, so the server must never be pointed at
// the repository's own examples/: a test that applies an edit would leave a
// tracked file modified. Serve a throwaway copy instead. (Until this existed,
// CI only avoided dirtying examples/ because every write it made happened to be
// refused — luck, not design.)
function stageExamples(): string {
  const src = path.join(REPO_ROOT, "examples");
  const dst = fs.mkdtempSync(path.join(os.tmpdir(), "mockagents-e2e-"));
  for (const entry of fs.readdirSync(src)) {
    if (!entry.endsWith(".yaml") && !entry.endsWith(".yml")) continue;
    fs.copyFileSync(path.join(src, entry), path.join(dst, entry));
  }
  return dst;
}

const EXAMPLES_DIR = stageExamples();

// `make build` writes `mockagents` on every platform (the -o name is explicit),
// while a bare `go build` on Windows produces `mockagents.exe`. Accept either
// rather than making the suite depend on how the binary happened to be built.
function resolveServerBin(): string {
  const candidates =
    process.platform === "win32"
      ? ["mockagents.exe", "mockagents"]
      : ["mockagents", "mockagents.exe"];
  for (const name of candidates) {
    const candidate = path.join(REPO_ROOT, name);
    if (fs.existsSync(candidate)) return candidate;
  }
  throw new Error(
    `No mockagents binary at ${REPO_ROOT}. Run \`make build\` before the browser suite ` +
      `(or \`make gui-verify\`, which sequences it).`,
  );
}

const SERVER_BIN = resolveServerBin();

export default defineConfig({
  testDir: "./e2e",
  // A smoke suite that needs retries to pass is not a smoke suite.
  retries: process.env.CI ? 1 : 0,
  forbidOnly: !!process.env.CI,
  workers: 1,
  reporter: process.env.CI ? [["github"], ["list"]] : [["list"]],
  timeout: 30_000,
  expect: { timeout: 10_000 },

  use: {
    baseURL: GUI_URL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],

  webServer: [
    {
      command: `"${SERVER_BIN}" start --port ${API_PORT} --agents-dir "${EXAMPLES_DIR}"`,
      url: `${API_URL}/api/v1/ready`,
      // Never reuse: a smoke suite that might be talking to a server someone
      // left running is worse than one that refuses to start. Reuse silently
      // tested a STALE build once already — the run went green against code
      // that was no longer on disk. A leftover process now fails loudly with
      // EADDRINUSE instead.
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      // `start` (not `dev`) so the smoke suite exercises the same production
      // build that ships.
      command: `npm run start -- --port ${GUI_PORT}`,
      url: GUI_URL,
      // Never reuse: a smoke suite that might be talking to a server someone
      // left running is worse than one that refuses to start. Reuse silently
      // tested a STALE build once already — the run went green against code
      // that was no longer on disk. A leftover process now fails loudly with
      // EADDRINUSE instead.
      reuseExistingServer: false,
      timeout: 120_000,
      env: { MOCKAGENTS_API_URL: API_URL },
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
});
