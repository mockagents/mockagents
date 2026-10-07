// Review fixes for the streaming paths, driven through a fake fetch so each
// test controls the exact bytes (and stalls) the client sees:
//   K-01  the API key reaches every request path, streams included
//   K-14  truncation / malformed frames are observable, not silent
//   K-16  caller cancellation (AbortSignal) and an opt-in idle timeout
//   K-24  the default Anthropic model matches the other SDKs

import { describe, expect as vexpect, it } from "vitest";

import {
  DEFAULT_ANTHROPIC_MODEL,
  MockAgentClient,
} from "../src/client.js";
import { StreamError } from "../src/types.js";

interface Seen {
  url: string;
  headers: Record<string, string>;
  body: any;
}

/** A fetch that records each request and answers with `frames` as one SSE
 * body (or a JSON body when `json` is set). */
function recordingFetch(
  seen: Seen[],
  reply: { frames?: string[]; json?: unknown } = {},
): typeof fetch {
  return (async (input: string | URL | Request, init?: RequestInit) => {
    seen.push({
      url: String(input),
      headers: { ...(init?.headers as Record<string, string>) },
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    if (reply.json !== undefined) {
      return new Response(JSON.stringify(reply.json), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    const text = (reply.frames ?? []).map((f) => f + "\n\n").join("");
    return new Response(text, { status: 200, headers: { "Content-Type": "text/event-stream" } });
  }) as typeof fetch;
}

/** A fetch whose response headers arrive but whose body never enqueues. */
function stalledFetch(): typeof fetch {
  return (async () =>
    new Response(new ReadableStream<Uint8Array>({ start() {} }), {
      status: 200,
      headers: { "Content-Type": "text/event-stream" },
    })) as typeof fetch;
}

const openaiDone = [
  `data: ${JSON.stringify({ choices: [{ delta: { content: "a" } }] })}`,
  `data: ${JSON.stringify({ choices: [{ delta: {}, finish_reason: "stop" }] })}`,
  "data: [DONE]",
];

const user = [{ role: "user" as const, content: "x" }];

async function drain(source: AsyncIterable<unknown>): Promise<unknown[]> {
  const out: unknown[] = [];
  for await (const v of source) out.push(v);
  return out;
}

describe("API key on every request path (K-01)", () => {
  const replies = {
    openai: { frames: openaiDone },
    anthropic: { frames: [`data: ${JSON.stringify({ type: "message_stop" })}`] },
  };

  it("sends Authorization on chatStream, messageStream and iterStream", async () => {
    const seen: Seen[] = [];
    const client = new MockAgentClient({ apiKey: "tenant-key", fetch: recordingFetch(seen, replies.openai) });
    await drain(client.chatStream(user));
    await drain(client.iterStream(user));
    const anth = new MockAgentClient({ apiKey: "tenant-key", fetch: recordingFetch(seen, replies.anthropic) });
    await drain(anth.messageStream(user));
    await drain(anth.iterStream(user, { protocol: "anthropic" }));
    vexpect(seen).toHaveLength(4);
    for (const req of seen) vexpect(req.headers.Authorization).toBe("Bearer tenant-key");
  });

  it("sends the key as X-Api-Key on Anthropic calls instead of the placeholder", async () => {
    const seen: Seen[] = [];
    const client = new MockAgentClient({
      apiKey: "tenant-key",
      fetch: recordingFetch(seen, { json: { content: [], stop_reason: "end_turn" } }),
    });
    await client.message(user);
    vexpect(seen[0].headers["X-Api-Key"]).toBe("tenant-key");
    vexpect(seen[0].headers.Authorization).toBe("Bearer tenant-key");
  });

  it("keeps the placeholder and sends no Authorization without a key", async () => {
    const seen: Seen[] = [];
    const client = new MockAgentClient({ fetch: recordingFetch(seen, replies.anthropic) });
    await drain(client.messageStream(user));
    vexpect(seen[0].headers["X-Api-Key"]).toBe("mock-api-key");
    vexpect(seen[0].headers.Authorization).toBeUndefined();
  });
});

describe("default Anthropic model (K-24)", () => {
  it("is the cross-SDK constant on message and messageStream", async () => {
    vexpect(DEFAULT_ANTHROPIC_MODEL).toBe("claude-sonnet-4-20250514");
    const seen: Seen[] = [];
    const client = new MockAgentClient({
      fetch: recordingFetch(seen, { json: { content: [], stop_reason: "end_turn" } }),
    });
    await client.message(user);
    vexpect(seen[0].body.model).toBe(DEFAULT_ANTHROPIC_MODEL);
  });
});

describe("stream fault visibility (K-14)", () => {
  it("reports a clean stream as completed", async () => {
    const client = new MockAgentClient({ fetch: recordingFetch([], { frames: openaiDone }) });
    const stream = client.chatStream(user);
    await drain(stream);
    vexpect(stream.stats).toEqual({ completed: true, truncated: false, malformedFrames: 0 });
  });

  it("flags an OpenAI stream that ends without [DONE] and counts malformed frames", async () => {
    const frames = [openaiDone[0], 'data: {"choices": [{"delta": {"cont', "data: [1,2]"];
    const client = new MockAgentClient({ fetch: recordingFetch([], { frames }) });
    const stream = client.iterStream(user);
    const chunks = await drain(stream);
    vexpect(chunks).toHaveLength(1);
    vexpect(stream.stats).toEqual({ completed: false, truncated: true, malformedFrames: 2 });
  });

  it("flags an Anthropic stream that ends without message_stop", async () => {
    const frames = [`data: ${JSON.stringify({ type: "message_start", message: {} })}`];
    const client = new MockAgentClient({ fetch: recordingFetch([], { frames }) });
    const stream = client.messageStream(user);
    await drain(stream);
    vexpect(stream.stats.truncated).toBe(true);
    vexpect(stream.stats.completed).toBe(false);
  });

  it("throws StreamError at the end when failOnStreamFault is set", async () => {
    const client = new MockAgentClient({ fetch: recordingFetch([], { frames: [openaiDone[0]] }) });
    const err = await drain(client.chatStream(user, { failOnStreamFault: true })).catch((e) => e);
    vexpect(err).toBeInstanceOf(StreamError);
    vexpect((err as StreamError).reason).toBe("truncated");
  });

  it("failOnStreamFault also rejects malformed frames on an otherwise complete stream", async () => {
    const frames = [openaiDone[0], "data: not-json", "data: [DONE]"];
    const client = new MockAgentClient({ fetch: recordingFetch([], { frames }) });
    const err = await drain(client.chatStream(user, { failOnStreamFault: true })).catch((e) => e);
    vexpect(err).toBeInstanceOf(StreamError);
    vexpect((err as StreamError).reason).toBe("malformed");
    vexpect((err as StreamError).stats.malformedFrames).toBe(1);
  });
});

describe("stream cancellation (K-16)", () => {
  it("rejects with StreamError after idleTimeoutMs when the body never enqueues", async () => {
    const client = new MockAgentClient({ fetch: stalledFetch() });
    const started = Date.now();
    const err = await drain(client.iterStream(user, { idleTimeoutMs: 50 })).catch((e) => e);
    vexpect(err).toBeInstanceOf(StreamError);
    vexpect((err as StreamError).reason).toBe("idle_timeout");
    vexpect(Date.now() - started).toBeLessThan(2_000);
  });

  it("rejects when the caller's AbortSignal fires mid-stream", async () => {
    const client = new MockAgentClient({ fetch: stalledFetch() });
    const controller = new AbortController();
    setTimeout(() => controller.abort(new Error("caller gave up")), 30);
    const err = await drain(client.chatStream(user, { signal: controller.signal })).catch((e) => e);
    vexpect((err as Error).message).toBe("caller gave up");
  });

  it("rejects immediately for an already-aborted signal", async () => {
    const seen: Seen[] = [];
    const client = new MockAgentClient({ fetch: recordingFetch(seen, { frames: openaiDone }) });
    const err = await drain(
      client.messageStream(user, { signal: AbortSignal.abort() }),
    ).catch((e) => e);
    vexpect((err as Error).name).toBe("AbortError");
    vexpect(seen).toHaveLength(0);
  });

  it("forwards the signal to non-streaming calls", async () => {
    const client = new MockAgentClient({
      fetch: ((_input: string | URL | Request, init?: RequestInit) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(init.signal?.reason));
        })) as typeof fetch,
    });
    const controller = new AbortController();
    const pending = client.chat(user, { signal: controller.signal });
    controller.abort(new Error("stop"));
    await vexpect(pending).rejects.toThrow("stop");
  });
});
