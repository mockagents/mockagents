// Audit M-38 guards for the SSE reader: CRLF frame boundaries, and a timeout
// that bounds the wait for HEADERS rather than the whole stream.

import { afterAll, beforeAll, describe, expect as vexpect, it } from "vitest";
import { createServer, Server } from "node:http";
import { AddressInfo } from "node:net";

import { MockAgentClient, findFrameBoundary } from "../src/client.js";

let server: Server;
let port: number;

const chunkFrames = [
  `data: ${JSON.stringify({ choices: [{ delta: { content: "a" } }] })}`,
  `data: ${JSON.stringify({ choices: [{ delta: { content: "b" } }] })}`,
  `data: [DONE]`,
];

function sseHeaders(res: import("node:http").ServerResponse) {
  res.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
}

beforeAll(async () => {
  server = createServer((req, res) => {
    // A server (or proxy) that terminates frames with CRLF CRLF, which the
    // SSE spec allows. Splitting on "\n\n" alone saw one unterminated frame.
    if (req.url === "/crlf") {
      sseHeaders(res);
      for (const frame of chunkFrames) res.write(frame.replace(/\n/g, "\r\n") + "\r\n\r\n");
      res.end();
      return;
    }
    // Headers arrive immediately; the body then trickles out over a period
    // longer than the client's timeout. This must NOT be aborted.
    if (req.url === "/slow-body") {
      sseHeaders(res);
      res.write(chunkFrames[0] + "\n\n");
      setTimeout(() => {
        res.write(chunkFrames[1] + "\n\n");
        res.write(chunkFrames[2] + "\n\n");
        res.end();
      }, 250);
      return;
    }
    // Never sends headers at all: the deadline must fire here.
    if (req.url === "/no-headers") {
      setTimeout(() => res.end(), 5_000);
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

describe("findFrameBoundary", () => {
  it("finds an LF LF boundary", () => {
    vexpect(findFrameBoundary("data: x\n\nrest")).toEqual({ end: 7, sepLen: 2 });
  });

  it("finds a CRLF CRLF boundary", () => {
    vexpect(findFrameBoundary("data: x\r\n\r\nrest")).toEqual({ end: 7, sepLen: 4 });
  });

  it("prefers whichever boundary comes first", () => {
    const buf = "a\n\nb\r\n\r\nc";
    vexpect(findFrameBoundary(buf)).toEqual({ end: 1, sepLen: 2 });
  });

  it("returns null for an incomplete frame", () => {
    vexpect(findFrameBoundary("data: partial\r\n")).toBeNull();
  });
});

describe("SSE reader", () => {
  it("parses a CRLF-terminated stream", async () => {
    // chatStream posts to /v1/chat/completions; a rewriting fetch points it at
    // the CRLF route so the real reader path is exercised. With the old
    // LF-only frame split the whole body was one unterminated frame and this
    // yielded a single merged event instead of two chunks.
    const rewriting: typeof fetch = (_input, init) =>
      fetch(`http://localhost:${port}/crlf`, init);
    const client = new MockAgentClient({
      baseUrl: `http://localhost:${port}`,
      fetch: rewriting,
    });
    const events: any[] = [];
    for await (const chunk of client.chatStream([{ role: "user", content: "x" }])) {
      events.push(chunk);
    }
    vexpect(events.length).toBe(2);
  });

  it("does not abort a stream that outlives the timeout", async () => {
    const rewriting: typeof fetch = (_input, init) =>
      fetch(`http://localhost:${port}/slow-body`, init);
    const client = new MockAgentClient({
      baseUrl: `http://localhost:${port}`,
      fetch: rewriting,
      timeoutMs: 100, // shorter than the 250ms body gap
    });
    const events: any[] = [];
    for await (const chunk of client.chatStream([{ role: "user", content: "x" }])) {
      events.push(chunk);
    }
    vexpect(events.length).toBe(2);
  });

  it("still times out when headers never arrive", async () => {
    const rewriting: typeof fetch = (_input, init) =>
      fetch(`http://localhost:${port}/no-headers`, init);
    const client = new MockAgentClient({
      baseUrl: `http://localhost:${port}`,
      fetch: rewriting,
      timeoutMs: 150,
    });
    await vexpect(async () => {
      for await (const _ of client.chatStream([{ role: "user", content: "x" }])) {
        /* not reached */
      }
    }).rejects.toThrow();
  });
});
