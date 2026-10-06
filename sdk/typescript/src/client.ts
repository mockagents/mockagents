// HTTP client for the MockAgents server. Uses native `fetch` (Node 18+)
// and returns a ChatResponse shape consistent with the Python SDK.

import {
  AgentSummary,
  ChatMessage,
  ChatResponse,
  HTTPError,
  PipelineResult,
  parseToolCallAnthropic,
  parseToolCallOpenAI,
  parseUsageAnthropic,
  parseUsageOpenAI,
  StreamChunk,
  StreamError,
  StreamStats,
  ToolCall,
} from "./types.js";

/** Model sent on OpenAI-protocol calls when none is given. */
export const DEFAULT_OPENAI_MODEL = "gpt-4o";
/** Model sent on Anthropic-protocol calls when none is given. Identical across
 * the Python, TypeScript and Go SDKs so one script routes to the same agent in
 * every language. */
export const DEFAULT_ANTHROPIC_MODEL = "claude-sonnet-4-20250514";
/** Placeholder `X-Api-Key` sent on Anthropic calls when no `apiKey` is set. */
const ANTHROPIC_PLACEHOLDER_KEY = "mock-api-key";

export interface MockAgentClientOptions {
  baseUrl?: string;
  timeoutMs?: number;
  /** Override the global fetch implementation (useful for tests). */
  fetch?: typeof fetch;
  /**
   * API key for multi-tenant deployments. Sent as `Authorization: Bearer` on
   * every request — chat, messages, streams and management calls — and as
   * `X-Api-Key` on Anthropic calls (replacing the placeholder).
   */
  apiKey?: string;
}

/** Cancellation and stream-health knobs shared by every request option type. */
export interface StreamControlOptions {
  /** Abort the request (or an in-flight stream). Rejects with the signal's reason. */
  signal?: AbortSignal;
  /**
   * Streams only: fail with `StreamError` (`reason: "idle_timeout"`) when no
   * bytes arrive for this many milliseconds after the response headers. Off
   * by default — a paced mock stream may legitimately pause.
   */
  idleTimeoutMs?: number;
  /**
   * Streams only: throw `StreamError` at the end of a stream that was truncated
   * (no `[DONE]` / `message_stop`) or carried malformed frames, instead of
   * ending quietly. The same facts are always readable on `stream.stats`.
   */
  failOnStreamFault?: boolean;
}

export interface ChatOptions extends StreamControlOptions {
  model?: string;
  sessionId?: string;
  tools?: unknown[];
  toolChoice?: unknown;
  temperature?: number;
  maxTokens?: number;
  extra?: Record<string, unknown>;
}

export interface MessageOptions extends StreamControlOptions {
  model?: string;
  sessionId?: string;
  system?: string;
  maxTokens?: number;
  tools?: unknown[];
  extra?: Record<string, unknown>;
}

/** What the streaming methods return: an async generator you `for await`
 * over, plus the live `stats` for that stream. Hold on to the object to read
 * `stats` after the loop. */
export type MockAgentStream<T> = AsyncGenerator<T, void, void> & {
  readonly stats: StreamStats;
};

export class MockAgentClient {
  public readonly baseUrl: string;
  public readonly timeoutMs: number;
  private readonly fetchImpl: typeof fetch;
  private readonly apiKey?: string;

  constructor(options: MockAgentClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "http://localhost:8080").replace(/\/+$/, "");
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.fetchImpl = options.fetch ?? fetch;
    this.apiKey = options.apiKey;
  }

  /** Send an OpenAI Chat Completions request. */
  async chat(messages: ChatMessage[], options: ChatOptions = {}): Promise<ChatResponse> {
    const start = performance.now();
    const resp = await this.requestJSON(
      "POST",
      "/v1/chat/completions",
      this.openAIHeaders(options.sessionId),
      openAIPayload(messages, options, false),
      options.signal,
    );
    const latencyMs = performance.now() - start;

    return parseOpenAIResponse(resp.body, resp.status, latencyMs);
  }

  /** Send an Anthropic Messages request. */
  async message(messages: ChatMessage[], options: MessageOptions = {}): Promise<ChatResponse> {
    const start = performance.now();
    const resp = await this.requestJSON(
      "POST",
      "/v1/messages",
      this.anthropicHeaders(options.sessionId),
      anthropicPayload(messages, options, false),
      options.signal,
    );
    const latencyMs = performance.now() - start;

    return parseAnthropicResponse(resp.body, resp.status, latencyMs);
  }

  /** Stream OpenAI Chat Completions chunks as parsed event dicts.
   *
   * Yields the raw delta payloads from each ``data:`` line. The
   * helper terminates on the ``[DONE]`` sentinel. Use
   * :meth:`iterStream` for a protocol-agnostic, typed view. The returned
   * object's ``stats`` reports truncation and malformed frames.
   */
  chatStream(
    messages: ChatMessage[],
    options: ChatOptions = {},
  ): MockAgentStream<Record<string, unknown>> {
    const stats = newStreamStats();
    return withStats(this.openAIEvents(messages, options, stats), stats);
  }

  /** Stream Anthropic Messages events as parsed event dicts.
   *
   * Mirrors :meth:`chatStream` for the Anthropic wire format. Yields
   * ``message_start`` / ``content_block_*`` / ``message_delta`` /
   * ``message_stop`` payloads and terminates after ``message_stop``.
   */
  messageStream(
    messages: ChatMessage[],
    options: MessageOptions = {},
  ): MockAgentStream<Record<string, unknown>> {
    const stats = newStreamStats();
    return withStats(this.anthropicEvents(messages, options, stats), stats);
  }

  /** Iterate a streamed completion as protocol-agnostic
   * :class:`StreamChunk` objects. Pick the wire format by passing
   * ``protocol: "openai"`` (default) or ``"anthropic"``.
   *
   * ```ts
   * const stream = client.iterStream(messages, { protocol: "anthropic" });
   * for await (const chunk of stream) {
   *   process.stdout.write(chunk.text);
   *   if (chunk.finished) break;
   * }
   * if (stream.stats.truncated) throw new Error("stream was cut short");
   * ```
   */
  iterStream(
    messages: ChatMessage[],
    options: { protocol?: "openai" | "anthropic" } & ChatOptions & MessageOptions = {},
  ): MockAgentStream<StreamChunk> {
    const stats = newStreamStats();
    const protocol = options.protocol ?? "openai";
    if (protocol === "openai") {
      return withStats(normalizeOpenAIStream(this.openAIEvents(messages, options, stats)), stats);
    }
    if (protocol === "anthropic") {
      return withStats(
        normalizeAnthropicStream(this.anthropicEvents(messages, options, stats)),
        stats,
      );
    }
    // Fail on first iteration, as the generator form always did.
    return withStats(
      (async function* (): AsyncGenerator<StreamChunk, void, void> {
        throw new Error(`unknown protocol ${String(protocol)}`);
      })(),
      stats,
    );
  }

  async health(): Promise<Record<string, unknown>> {
    return (await this.requestJSON("GET", "/api/v1/health")).body as Record<string, unknown>;
  }

  async listAgents(): Promise<AgentSummary[]> {
    return (await this.requestJSON("GET", "/api/v1/agents")).body as AgentSummary[];
  }

  async getAgent(name: string): Promise<unknown> {
    return (await this.requestJSON("GET", `/api/v1/agents/${encodeURIComponent(name)}`)).body;
  }

  async reloadAgent(name: string): Promise<unknown> {
    return (
      await this.requestJSON(
        "POST",
        `/api/v1/agents/${encodeURIComponent(name)}/reload`,
      )
    ).body;
  }

  /** Execute a loaded pipeline and return its ordered node trajectory. */
  async runPipeline(name: string, input: string, sessionId?: string): Promise<PipelineResult> {
    const payload: Record<string, unknown> = { input };
    if (sessionId) payload.session_id = sessionId;
    const wire = (
      await this.requestJSON(
        "POST",
        `/api/v1/pipelines/${encodeURIComponent(name)}/run`,
        { "Content-Type": "application/json" },
        payload,
      )
    ).body as any;
    const rawNodes = Array.isArray(wire?.nodes) ? wire.nodes : [];
    return {
      pipelineName: String(wire?.pipeline_name ?? ""),
      topology: String(wire?.topology ?? ""),
      nodes: rawNodes.map((node: any) => ({
        nodeId: String(node?.node_id ?? ""),
        agentName: String(node?.agent_name ?? ""),
        response: node?.response && typeof node.response === "object" ? node.response : {},
        latencyNs: Number(node?.latency ?? 0),
      })),
      latencyNs: Number(wire?.latency ?? 0),
    };
  }

  // --- internals ---

  private openAIHeaders(sessionId?: string): Record<string, string> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (sessionId) headers["X-Session-Id"] = sessionId;
    return headers;
  }

  private anthropicHeaders(sessionId?: string): Record<string, string> {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      // A configured key IS the Anthropic credential; the placeholder only
      // satisfies clients/servers that insist on the header being present.
      "X-Api-Key": this.apiKey ?? ANTHROPIC_PLACEHOLDER_KEY,
      "Anthropic-Version": "2023-06-01",
    };
    if (sessionId) headers["X-Session-Id"] = sessionId;
    return headers;
  }

  /** The single place request headers gain the credential. Every request
   * path (JSON and SSE) goes through it, so no call can silently go out
   * anonymous — in multi-tenant mode that routes to a DIFFERENT agent rather
   * than failing (review K-01). */
  private withAuth(headers: Record<string, string>): Record<string, string> {
    const out = { ...headers };
    if (this.apiKey && out.Authorization === undefined) {
      out.Authorization = `Bearer ${this.apiKey}`;
    }
    return out;
  }

  private async *openAIEvents(
    messages: ChatMessage[],
    options: ChatOptions,
    stats: StreamStats,
  ): AsyncGenerator<Record<string, unknown>, void, void> {
    const events = this.requestSSE(
      "/v1/chat/completions",
      this.openAIHeaders(options.sessionId),
      openAIPayload(messages, options, true),
      options,
      stats,
    );
    for await (const event of events) {
      if (event.data === "[DONE]") {
        stats.completed = true;
        assertStreamHealthy(options, stats);
        return;
      }
      const parsed = tryParseEvent(event.data);
      if (parsed === undefined) {
        stats.malformedFrames++;
        continue;
      }
      yield parsed;
    }
    // The body ended without [DONE]: a truncated stream, not a completion.
    stats.truncated = true;
    assertStreamHealthy(options, stats);
  }

  private async *anthropicEvents(
    messages: ChatMessage[],
    options: MessageOptions,
    stats: StreamStats,
  ): AsyncGenerator<Record<string, unknown>, void, void> {
    const events = this.requestSSE(
      "/v1/messages",
      this.anthropicHeaders(options.sessionId),
      anthropicPayload(messages, options, true),
      options,
      stats,
    );
    for await (const event of events) {
      const parsed = tryParseEvent(event.data);
      if (parsed === undefined) {
        stats.malformedFrames++;
        continue;
      }
      if (parsed.type === "message_stop") {
        stats.completed = true;
        assertStreamHealthy(options, stats);
        yield parsed;
        return;
      }
      yield parsed;
    }
    stats.truncated = true;
    assertStreamHealthy(options, stats);
  }

  /** POST a JSON body to ``path`` and yield parsed SSE events. Each
   * yielded value is the raw ``{event, data}`` pair after the
   * server-sent-events frame boundaries; the caller is responsible
   * for parsing ``data`` as JSON.
   */
  private async *requestSSE(
    path: string,
    headers: Record<string, string>,
    body: unknown,
    control: StreamControlOptions,
    stats: StreamStats,
  ): AsyncGenerator<{ event: string; data: string }, void, void> {
    const external = control.signal;
    throwIfAborted(external);
    const controller = new AbortController();
    const onExternalAbort = () => controller.abort(external?.reason);
    external?.addEventListener("abort", onExternalAbort, { once: true });
    // The deadline covers the wait for response HEADERS only. It used to span
    // the whole stream, so any stream that ran longer than timeoutMs (30s by
    // default) was aborted mid-flight — a paced or long-running mock stream
    // would fail for no reason the caller could see (audit M-38). Once headers
    // arrive the server is demonstrably alive; from then on only the opt-in
    // idleTimeoutMs and the caller's signal bound the stream.
    let headerTimer: ReturnType<typeof setTimeout> | undefined = setTimeout(
      () => controller.abort(),
      this.timeoutMs,
    );
    const clearHeaderTimer = () => {
      if (headerTimer !== undefined) {
        clearTimeout(headerTimer);
        headerTimer = undefined;
      }
    };

    let resp: Response;
    try {
      resp = await this.fetchImpl(`${this.baseUrl}${path}`, {
        method: "POST",
        headers: this.withAuth(headers),
        body: JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (err) {
      clearHeaderTimer();
      external?.removeEventListener("abort", onExternalAbort);
      throw err;
    }
    clearHeaderTimer();

    if (!resp.ok) {
      external?.removeEventListener("abort", onExternalAbort);
      const text = await resp.text().catch(() => "");
      throw new HTTPError(resp.status, text);
    }
    if (!resp.body) {
      external?.removeEventListener("abort", onExternalAbort);
      return;
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder("utf-8");
    const splitter = new SSEFrameSplitter();
    try {
      // eslint-disable-next-line no-constant-condition
      while (true) {
        const { value, done } = await readWithLimits(
          reader.read(),
          control.idleTimeoutMs,
          external,
          stats,
        );
        if (done) break;
        for (const frame of splitter.push(decoder.decode(value, { stream: true }))) {
          const event = parseSSEFrame(frame);
          if (event !== null) yield event;
        }
      }
      // Drain any trailing frame that lacked a terminating blank line.
      for (const frame of splitter.push(decoder.decode()).concat(splitter.flush())) {
        const event = parseSSEFrame(frame);
        if (event !== null) yield event;
      }
    } finally {
      clearHeaderTimer();
      external?.removeEventListener("abort", onExternalAbort);
      // Cancel rather than only releasing the lock: a caller that breaks out of
      // the for-await early (took the first chunk, hit an assertion) would
      // otherwise leave the response body — and the socket behind it — open
      // until the process exited. Cancelling also settles a read that an idle
      // timeout or abort left pending.
      try {
        await reader.cancel();
      } catch {
        /* already closed */
      }
      try {
        reader.releaseLock();
      } catch {
        /* already released */
      }
    }
  }

  private async requestJSON(
    method: string,
    path: string,
    headers: Record<string, string> = {},
    body?: unknown,
    signal?: AbortSignal,
  ): Promise<{ status: number; body: unknown }> {
    throwIfAborted(signal);
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    const onAbort = () => controller.abort(signal?.reason);
    signal?.addEventListener("abort", onAbort, { once: true });
    try {
      const resp = await this.fetchImpl(`${this.baseUrl}${path}`, {
        method,
        headers: this.withAuth(headers),
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
      const text = await resp.text();
      if (!resp.ok) {
        throw new HTTPError(resp.status, text);
      }
      const parsed = text.length > 0 ? JSON.parse(text) : {};
      return { status: resp.status, body: parsed };
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    }
  }
}

function openAIPayload(
  messages: ChatMessage[],
  options: ChatOptions,
  stream: boolean,
): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    model: options.model ?? DEFAULT_OPENAI_MODEL,
    messages,
    stream,
  };
  if (options.tools) payload.tools = options.tools;
  if (options.toolChoice !== undefined) payload.tool_choice = options.toolChoice;
  if (options.temperature !== undefined) payload.temperature = options.temperature;
  if (options.maxTokens !== undefined) payload.max_tokens = options.maxTokens;
  if (options.extra) Object.assign(payload, options.extra);
  return payload;
}

function anthropicPayload(
  messages: ChatMessage[],
  options: MessageOptions,
  stream: boolean,
): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    model: options.model ?? DEFAULT_ANTHROPIC_MODEL,
    messages,
    max_tokens: options.maxTokens ?? 1024,
    stream,
  };
  if (options.system) payload.system = options.system;
  if (options.tools) payload.tools = options.tools;
  if (options.extra) Object.assign(payload, options.extra);
  return payload;
}

function newStreamStats(): StreamStats {
  return { completed: false, truncated: false, malformedFrames: 0 };
}

function withStats<T>(
  gen: AsyncGenerator<T, void, void>,
  stats: StreamStats,
): MockAgentStream<T> {
  return Object.defineProperty(gen, "stats", { value: stats, enumerable: true }) as MockAgentStream<T>;
}

function assertStreamHealthy(options: StreamControlOptions, stats: StreamStats): void {
  if (!options.failOnStreamFault) return;
  if (stats.truncated) throw new StreamError("truncated", stats);
  if (stats.malformedFrames > 0) throw new StreamError("malformed", stats);
}

function abortReason(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException("This operation was aborted", "AbortError");
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted) throw abortReason(signal);
}

/** Settle `read` normally, or reject when the caller's signal aborts or no
 * data arrives within `idleTimeoutMs`. Racing (rather than relying on the
 * fetch signal alone) matters: a body that simply stops producing bytes is
 * not always errored by an abort, so the read could otherwise hang forever. */
function readWithLimits<T>(
  read: Promise<T>,
  idleTimeoutMs: number | undefined,
  signal: AbortSignal | undefined,
  stats: StreamStats,
): Promise<T> {
  const idle = idleTimeoutMs !== undefined && idleTimeoutMs > 0;
  if (!idle && !signal) return read;
  return new Promise<T>((resolve, reject) => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const cleanup = () => {
      if (timer !== undefined) clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    };
    const onAbort = () => {
      cleanup();
      reject(abortReason(signal as AbortSignal));
    };
    if (signal?.aborted) {
      onAbort();
      return;
    }
    signal?.addEventListener("abort", onAbort, { once: true });
    if (idle) {
      timer = setTimeout(() => {
        cleanup();
        reject(
          new StreamError("idle_timeout", stats, `no stream data received for ${idleTimeoutMs}ms`),
        );
      }, idleTimeoutMs);
    }
    read.then(
      (value) => {
        cleanup();
        resolve(value);
      },
      (err) => {
        cleanup();
        reject(err);
      },
    );
  });
}

// --- response parsers (exported for tests) ---

export function parseOpenAIResponse(
  data: any,
  statusCode: number,
  latencyMs: number,
): ChatResponse {
  const choices = Array.isArray(data?.choices) ? data.choices : [];
  const choice = choices[0] ?? {};
  const message = choice.message ?? {};
  const toolCalls: ToolCall[] = Array.isArray(message.tool_calls)
    ? message.tool_calls.map(parseToolCallOpenAI)
    : [];
  return {
    content: typeof message.content === "string" ? message.content : "",
    model: typeof data?.model === "string" ? data.model : "",
    toolCalls,
    finishReason: typeof choice.finish_reason === "string" ? choice.finish_reason : "",
    usage: parseUsageOpenAI(data?.usage),
    raw: data,
    statusCode,
    latencyMs,
  };
}

// --- streaming helpers (exported for tests) ---

/**
 * Incremental SSE frame splitter. Line endings are normalised to LF first —
 * the spec allows CRLF, LF or a bare CR, and proxies rewrite one into another
 * — so a frame boundary is always a plain blank line. A CR that ends one chunk
 * and an LF that starts the next count as ONE line ending, so a CRLF split
 * across reads cannot fabricate a blank line (review K-12).
 */
export class SSEFrameSplitter {
  private buffer = "";
  private pendingCR = false;

  /** Append decoded text; returns every frame it completed, in order. */
  push(text: string): string[] {
    if (this.pendingCR && text.startsWith("\n")) text = text.slice(1);
    if (text.length === 0) return [];
    this.pendingCR = text.endsWith("\r");
    this.buffer += text.replace(/\r\n?/g, "\n");
    const frames: string[] = [];
    let sep = this.buffer.indexOf("\n\n");
    while (sep !== -1) {
      frames.push(this.buffer.slice(0, sep));
      this.buffer = this.buffer.slice(sep + 2);
      sep = this.buffer.indexOf("\n\n");
    }
    return frames;
  }

  /** End of input: returns the unterminated trailing frame, if any. */
  flush(): string[] {
    const tail = this.buffer;
    this.buffer = "";
    this.pendingCR = false;
    return tail.trim().length > 0 ? [tail] : [];
  }
}

/** Parse a single SSE frame ("event: foo\ndata: ...") into its
 * event/data parts. Returns null when the frame has no data line.
 * Multiple data lines are joined with newlines per the SSE spec.
 */
export function parseSSEFrame(frame: string): { event: string; data: string } | null {
  let event = "";
  const dataLines: string[] = [];
  for (const rawLine of frame.split("\n")) {
    const line = rawLine.replace(/\r$/, "");
    if (line.startsWith(":")) continue; // comment
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      // SSE strips exactly one leading space after the colon.
      dataLines.push(line.startsWith("data: ") ? line.slice(6) : line.slice(5));
    }
  }
  if (dataLines.length === 0) return null;
  return { event, data: dataLines.join("\n") };
}

/** Parse one `data:` payload as a provider event. Anything that is not a JSON
 * object (a cut-off frame, an injected `malformed` fault) is undefined. */
function tryParseEvent(text: string): Record<string, unknown> | undefined {
  try {
    const v: unknown = JSON.parse(text);
    return v !== null && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : undefined;
  } catch {
    return undefined;
  }
}

/** Normalize OpenAI Chat Completions chunks to StreamChunks. */
export async function* normalizeOpenAIStream(
  chunks: AsyncIterable<Record<string, unknown>>,
): AsyncGenerator<StreamChunk, void, void> {
  for await (const chunk of chunks) {
    const choices = Array.isArray((chunk as any).choices) ? (chunk as any).choices : [];
    if (choices.length === 0) continue;
    const choice = choices[0] ?? {};
    const delta = (choice.delta ?? {}) as Record<string, unknown>;

    const text = typeof delta.content === "string" ? delta.content : "";

    let toolCallDelta: [number, string, string] | undefined;
    const toolCalls = Array.isArray(delta.tool_calls) ? delta.tool_calls : [];
    if (toolCalls.length > 0) {
      const tc = toolCalls[0] as any;
      const idx = typeof tc?.index === "number" ? tc.index : 0;
      const func = (tc?.function ?? {}) as Record<string, unknown>;
      toolCallDelta = [
        idx,
        typeof func.name === "string" ? func.name : "",
        typeof func.arguments === "string" ? func.arguments : "",
      ];
    }

    const finishReason =
      typeof choice.finish_reason === "string" ? choice.finish_reason : "";
    const finished = finishReason !== "";

    if (text === "" && !toolCallDelta && !finished) continue;

    yield {
      text,
      toolCallDelta,
      finishReason,
      finished,
      raw: chunk,
    };
  }
}

/** Normalize Anthropic Messages events to StreamChunks. */
export async function* normalizeAnthropicStream(
  events: AsyncIterable<Record<string, unknown>>,
): AsyncGenerator<StreamChunk, void, void> {
  let currentToolIndex = -1;
  let currentToolName = "";
  let finalStop = "";
  for await (const event of events) {
    const et = typeof (event as any).type === "string" ? (event as any).type : "";

    if (et === "content_block_start") {
      const block = ((event as any).content_block ?? {}) as Record<string, unknown>;
      if (block.type === "tool_use") {
        const idx = typeof (event as any).index === "number" ? (event as any).index : currentToolIndex + 1;
        currentToolIndex = idx;
        currentToolName = typeof block.name === "string" ? block.name : "";
        yield {
          text: "",
          toolCallDelta: [currentToolIndex, currentToolName, ""],
          finishReason: "",
          finished: false,
          raw: event,
        };
      }
    } else if (et === "content_block_delta") {
      const delta = ((event as any).delta ?? {}) as Record<string, unknown>;
      const dt = typeof delta.type === "string" ? delta.type : "";
      if (dt === "text_delta") {
        const text = typeof delta.text === "string" ? delta.text : "";
        if (text.length > 0) {
          yield { text, finishReason: "", finished: false, raw: event };
        }
      } else if (dt === "input_json_delta") {
        const fragment =
          typeof delta.partial_json === "string" ? delta.partial_json : "";
        if (fragment.length > 0) {
          yield {
            text: "",
            toolCallDelta: [currentToolIndex, currentToolName, fragment],
            finishReason: "",
            finished: false,
            raw: event,
          };
        }
      }
    } else if (et === "message_delta") {
      const delta = ((event as any).delta ?? {}) as Record<string, unknown>;
      if (typeof delta.stop_reason === "string" && delta.stop_reason.length > 0) {
        finalStop = delta.stop_reason;
      }
    } else if (et === "message_stop") {
      yield {
        text: "",
        finishReason: finalStop || "end_turn",
        finished: true,
        raw: event,
      };
      return;
    }
  }
}

export function parseAnthropicResponse(
  data: any,
  statusCode: number,
  latencyMs: number,
): ChatResponse {
  const blocks = Array.isArray(data?.content) ? data.content : [];
  const textParts: string[] = [];
  const toolCalls: ToolCall[] = [];
  for (const block of blocks) {
    if (block?.type === "text" && typeof block.text === "string") {
      textParts.push(block.text);
    } else if (block?.type === "tool_use") {
      toolCalls.push(parseToolCallAnthropic(block));
    }
  }
  return {
    content: textParts.join(" "),
    model: typeof data?.model === "string" ? data.model : "",
    toolCalls,
    finishReason: typeof data?.stop_reason === "string" ? data.stop_reason : "",
    usage: parseUsageAnthropic(data?.usage),
    raw: data,
    statusCode,
    latencyMs,
  };
}
