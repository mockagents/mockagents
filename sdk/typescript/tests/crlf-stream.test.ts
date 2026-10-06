// Audit M-38 guards for the SSE reader: CRLF frame boundaries, and a timeout
// that bounds the wait for HEADERS rather than the whole stream.

import { afterAll, beforeAll, describe, expect as vexpect, it } from "vitest";
import { createServer, Server } from "node:http";
import { AddressInfo } from "node:net";

import { MockAgentClient, SSEFrameSplitter } from "../src/client.js";

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

// Review K-12: line endings are normalised to LF before frames are split, so
// every spec-legal ending (and mixtures of them) frames identically.
describe("SSEFrameSplitter", () => {
  const split = (...chunks: string[]): string[] => {
    const s = new SSEFrameSplitter();
    const frames = chunks.flatMap((c) => s.push(c));
    return frames.concat(s.flush());
  };

  const cases: Array<[string, string]> = [
    ["LF", "data: a\n\ndata: b\n\n"],
    ["CRLF", "data: a\r\n\r\ndata: b\r\n\r\n"],
    ["CR", "data: a\r\rdata: b\r\r"],
    ["mixed CRLF then LF", "data: a\r\n\r\ndata: b\n\n"],
    ["an LF + CRLF blank line (\\n\\r\\n)", "data: a\n\r\ndata: b\n\n"],
    ["CRLF + CR, then CRLF + LF", "data: a\r\n\rdata: b\r\n\n"],
  ];
  for (const [name, wire] of cases) {
    it(`splits ${name} into two frames`, () => {
      vexpect(split(wire)).toEqual(["data: a", "data: b"]);
    });
  }

  it("treats a CRLF split across two chunks as one line ending", () => {
    // "\r" ends chunk 1 and "\n" starts chunk 2: that is ONE line break, so
    // no blank line (and no frame) may appear until the real terminator.
    vexpect(split("data: a\r", "\ndata: b\r\n\r\n")).toEqual(["data: a\ndata: b"]);
  });

  it("finds a boundary split across chunks", () => {
    vexpect(split("data: a\r\n", "\r\ndata: b", "\n\n")).toEqual(["data: a", "data: b"]);
    vexpect(split("data: a\n", "\n")).toEqual(["data: a"]);
  });

  it("drains an unterminated trailing frame on flush", () => {
    vexpect(split("data: a\n\ndata: tail")).toEqual(["data: a", "data: tail"]);
  });

  it("holds an incomplete frame until more input arrives", () => {
    const s = new SSEFrameSplitter();
    vexpect(s.push("data: partial\r\n")).toEqual([]);
    vexpect(s.push("\r\n")).toEqual(["data: partial"]);
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
