// MCP bidirectional transport helpers for the TypeScript SDK.
//
// The MockAgents server speaks JSON-RPC 2.0 over HTTP (`POST /mcp`)
// and stdio. v0.3 added a bidirectional channel that lets the server
// push JSON-RPC requests and notifications out to a subscribed
// client — the mechanism that powers `sampling/createMessage` and
// `roots/list`. This module mirrors the Python `McpClient` surface
// one-for-one so TS test harnesses can exercise the same flow:
//
//   - `McpClient.connect()` opens an SSE subscription to /mcp/events
//     and returns an async iterable over typed `McpEvent` objects.
//   - `McpClient.sendResponse(id, {result} | {error})` POSTs a
//     JSON-RPC reply to /mcp/response.
//   - `McpClient.dispatchRequest(event, handlers)` routes one event
//     through a `method -> handler` map, auto-posts the result or
//     a JSON-RPC error, and re-throws when the handler itself
//     throws so test failures stay visible.

import { HTTPError } from "./types.js";
import { SSEFrameSplitter, parseSSEFrame } from "./client.js";

export interface McpClientOptions {
  baseUrl?: string;
  /** Per-request timeout for send/response calls, in milliseconds.
   * Does NOT apply to the long-lived /mcp/events subscription — that
   * is held open as long as the iterator is consumed. */
  timeoutMs?: number;
  fetch?: typeof fetch;
}

/** One parsed SSE frame from `/mcp/events`. `kind` is the SSE
 * `event:` line — the MockAgents server emits `"request"` for
 * server-initiated JSON-RPC requests and `"notification"` for
 * fire-and-forget notifications. */
export interface McpEvent {
  kind: string;
  payload: JsonRpcEnvelope;
}

/** JSON-RPC 2.0 envelope shape. `id` is present on requests and
 * absent on notifications; `method` is always present on server-
 * initiated frames. */
export interface JsonRpcEnvelope {
  jsonrpc?: string;
  id?: number | string | null;
  method?: string;
  params?: Record<string, unknown> | unknown[];
}

/** JSON-RPC 2.0 error object. Use a negative code from the spec
 * range (e.g. -32601 method-not-found, -32603 internal). */
export interface JsonRpcError {
  code: number;
  message: string;
  data?: unknown;
}

/** Handler signature for `dispatchRequest`. Receives the parsed
 * `params` dict and must return a `result` object that will be
 * posted back to the server. */
export type McpRequestHandler = (
  params: Record<string, unknown>,
) => Promise<Record<string, unknown>> | Record<string, unknown>;

/** Async iterable over parsed MCP events. Use via `for await`:
 *
 * ```ts
 * for await (const event of client.connect()) {
 *   if (event.kind === "request") {
 *     await client.dispatchRequest(event, handlers);
 *   }
 * }
 * ```
 *
 * The iterator terminates when the underlying response body ends or
 * when the caller calls `.close()`. Heartbeat comments (`:heartbeat`)
 * are silently skipped so callers never see them.
 */
export interface McpEventStream extends AsyncIterable<McpEvent> {
  close(): Promise<void>;
}

/** HTTP client for the MockAgents MCP bidirectional transport.
 *
 * Stateless beyond the base URL and fetch impl. `connect()` opens
 * the subscription, `sendResponse()` posts individual replies, and
 * `dispatchRequest()` glues the two together for the common case of
 * "route this event through a method→handler map and reply". */
export class McpClient {
  public readonly baseUrl: string;
  public readonly timeoutMs: number;
  private readonly fetchImpl: typeof fetch;

  constructor(options: McpClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "http://localhost:8080").replace(/\/+$/, "");
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.fetchImpl = options.fetch ?? fetch;
  }

  /** Open a long-lived subscription to `GET /mcp/events` and return
   * an async iterable over parsed `McpEvent` objects. Always call
   * `.close()` (or break out of the for-await loop) when you are
   * done so the underlying fetch reader is released. */
  connect(): McpEventStream {
    const controller = new AbortController();
    // Intentional: no timeoutMs on the subscription — SSE streams
    // are held open for the lifetime of the test. The caller aborts
    // via `.close()` or by breaking out of the iterator.
    const respPromise = this.fetchImpl(`${this.baseUrl}/mcp/events`, {
      method: "GET",
      headers: { Accept: "text/event-stream" },
      signal: controller.signal,
    });

    let closed = false;

    const stream: McpEventStream = {
      async close() {
        if (closed) return;
        closed = true;
        controller.abort();
      },
      [Symbol.asyncIterator]() {
        return iterate();
      },
    };

    async function* iterate(): AsyncGenerator<McpEvent, void, void> {
      let resp: Response;
      try {
        resp = await respPromise;
      } catch (err) {
        if ((err as Error)?.name === "AbortError") return;
        throw err;
      }
      if (!resp.ok) {
        const text = await resp.text().catch(() => "");
        throw new HTTPError(resp.status, text);
      }
      if (!resp.body) return;

      const reader = resp.body.getReader();
      const decoder = new TextDecoder("utf-8");
      // Same splitter as the chat streams, so CRLF / CR line endings frame
      // identically here (review K-12).
      const splitter = new SSEFrameSplitter();
      try {
        // eslint-disable-next-line no-constant-condition
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          for (const frame of splitter.push(decoder.decode(value, { stream: true }))) {
            const event = parseMcpFrame(frame);
            if (event !== null) yield event;
          }
        }
        // Drain the trailing frame if the server didn't terminate
        // with a blank line.
        for (const frame of splitter.push(decoder.decode()).concat(splitter.flush())) {
          const event = parseMcpFrame(frame);
          if (event !== null) yield event;
        }
      } finally {
        try {
          reader.releaseLock();
        } catch {
          /* already released */
        }
      }
    }

    return stream;
  }

  /** POST a JSON-RPC reply for a server-initiated request. Exactly
   * one of `result` or `error` must be set. */
  async sendResponse(
    requestId: number | string,
    reply: { result?: Record<string, unknown>; error?: JsonRpcError },
  ): Promise<void> {
    const hasResult = reply.result !== undefined;
    const hasError = reply.error !== undefined;
    if (hasResult === hasError) {
      throw new Error("mcp: sendResponse needs exactly one of result or error");
    }
    const body: Record<string, unknown> = {
      jsonrpc: "2.0",
      id: requestId,
    };
    if (hasResult) body.result = reply.result;
    if (hasError) body.error = reply.error;

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const resp = await this.fetchImpl(`${this.baseUrl}/mcp/response`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal: controller.signal,
      });
      if (!resp.ok) {
        const text = await resp.text().catch(() => "");
        throw new HTTPError(resp.status, text);
      }
    } finally {
      clearTimeout(timer);
    }
  }

  /** Route a single server-initiated request through a handler map
   * and post the matching response.
   *
   * - If `event` is not a request, throws `Error("…non-request…")`.
   * - If no handler matches, a JSON-RPC `-32601` error is posted and
   *   an `Error(method)` is thrown.
   * - If the handler throws, a JSON-RPC `-32603` error is posted
   *   carrying the error message and the original error is re-thrown
   *   so the test can still see the failure. */
  async dispatchRequest(
    event: McpEvent,
    handlers: Record<string, McpRequestHandler>,
  ): Promise<Record<string, unknown>> {
    if (!isRequest(event)) {
      throw new Error("mcp: dispatchRequest called on a non-request event");
    }
    const method = event.payload.method ?? "";
    const handler = handlers[method];
    if (handler === undefined) {
      await this.sendResponse(event.payload.id as number | string, {
        error: { code: -32601, message: `method ${JSON.stringify(method)} not handled` },
      });
      throw new Error(method);
    }
    let result: Record<string, unknown>;
    try {
      result = await handler(paramsOf(event));
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      await this.sendResponse(event.payload.id as number | string, {
        error: { code: -32603, message },
      });
      throw err;
    }
    await this.sendResponse(event.payload.id as number | string, { result });
    return result;
  }
}

// --- helpers (exported for tests) ---

/** Returns true when the event carries a JSON-RPC id (i.e. is a
 * server-initiated request the client must reply to). */
export function isRequest(event: McpEvent): boolean {
  return (
    event.kind === "request" &&
    event.payload.id !== undefined &&
    event.payload.id !== null
  );
}

/** Returns the event's JSON-RPC params as a dict, or an empty dict
 * when the params are missing or non-object. Handlers can use
 * `params.x` without first null-checking. */
export function paramsOf(event: McpEvent): Record<string, unknown> {
  const p = event.payload.params;
  if (p && typeof p === "object" && !Array.isArray(p)) {
    return p as Record<string, unknown>;
  }
  return {};
}

/** Parse a single SSE frame into an McpEvent. Reuses `parseSSEFrame`
 * for the line-level parsing; adds JSON.parse of the `data:` payload
 * and defensive drop-on-error so a malformed frame cannot kill an
 * active subscription. Exported for tests. */
export function parseMcpFrame(frame: string): McpEvent | null {
  const sse = parseSSEFrame(frame);
  if (sse === null) return null;
  let payload: JsonRpcEnvelope | null;
  try {
    payload = JSON.parse(sse.data);
  } catch {
    return null;
  }
  if (payload === null || typeof payload !== "object") return null;
  return { kind: sse.event || "message", payload };
}
