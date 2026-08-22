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
  ToolCall,
} from "./types.js";

export interface MockAgentClientOptions {
  baseUrl?: string;
  timeoutMs?: number;
  /** Override the global fetch implementation (useful for tests). */
  fetch?: typeof fetch;
  /** Viewer-or-higher management API key for multi-tenant deployments. */
  apiKey?: string;
}

export interface ChatOptions {
  model?: string;
  sessionId?: string;
  tools?: unknown[];
  toolChoice?: unknown;
  temperature?: number;
  maxTokens?: number;
  extra?: Record<string, unknown>;
}

export interface MessageOptions {
  model?: string;
  sessionId?: string;
  system?: string;
  maxTokens?: number;
  tools?: unknown[];
  extra?: Record<string, unknown>;
}

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
    const payload: Record<string, unknown> = {
      model: options.model ?? "gpt-4o",
      messages,
      stream: false,
    };
    if (options.tools) payload.tools = options.tools;
    if (options.toolChoice !== undefined) payload.tool_choice = options.toolChoice;
    if (options.temperature !== undefined) payload.temperature = options.temperature;
    if (options.maxTokens !== undefined) payload.max_tokens = options.maxTokens;
    if (options.extra) Object.assign(payload, options.extra);

    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (options.sessionId) headers["X-Session-Id"] = options.sessionId;

    const start = performance.now();
    const resp = await this.requestJSON("POST", "/v1/chat/completions", headers, payload);
    const latencyMs = performance.now() - start;

    return parseOpenAIResponse(resp.body, resp.status, latencyMs);
  }

  /** Send an Anthropic Messages request. */
  async message(messages: ChatMessage[], options: MessageOptions = {}): Promise<ChatResponse> {
    const payload: Record<string, unknown> = {
      model: options.model ?? "claude-3-5-sonnet-latest",
      messages,
      max_tokens: options.maxTokens ?? 1024,
      stream: false,
    };
    if (options.system) payload.system = options.system;
    if (options.tools) payload.tools = options.tools;
    if (options.extra) Object.assign(payload, options.extra);

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Api-Key": "mock-api-key",
      "Anthropic-Version": "2023-06-01",
    };
    if (options.sessionId) headers["X-Session-Id"] = options.sessionId;

    const start = performance.now();
    const resp = await this.requestJSON("POST", "/v1/messages", headers, payload);
    const latencyMs = performance.now() - start;

    return parseAnthropicResponse(resp.body, resp.status, latencyMs);
  }

  /** Stream OpenAI Chat Completions chunks as parsed event dicts.
   *
   * Yields the raw delta payloads from each ``data:`` line. The
   * helper terminates on the ``[DONE]`` sentinel. Use
   * :meth:`iterStream` for a protocol-agnostic, typed view.
   */
  async *chatStream(
    messages: ChatMessage[],
    options: ChatOptions = {},
  ): AsyncGenerator<Record<string, unknown>, void, void> {
    const payload: Record<string, unknown> = {
      model: options.model ?? "gpt-4o",
      messages,
      stream: true,
    };
    if (options.tools) payload.tools = options.tools;
    if (options.toolChoice !== undefined) payload.tool_choice = options.toolChoice;
    if (options.temperature !== undefined) payload.temperature = options.temperature;
    if (options.maxTokens !== undefined) payload.max_tokens = options.maxTokens;
    if (options.extra) Object.assign(payload, options.extra);

    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (options.sessionId) headers["X-Session-Id"] = options.sessionId;

    for await (const event of this.requestSSE("/v1/chat/completions", headers, payload)) {
      if (event.data === "[DONE]") return;
      const parsed = tryParseJSON(event.data);
      if (parsed !== undefined) yield parsed as Record<string, unknown>;
    }
  }

  /** Stream Anthropic Messages events as parsed event dicts.
   *
   * Mirrors :meth:`chatStream` for the Anthropic wire format. Yields
   * ``message_start`` / ``content_block_*`` / ``message_delta`` /
   * ``message_stop`` payloads and terminates after ``message_stop``.
   */
  async *messageStream(
    messages: ChatMessage[],
    options: MessageOptions = {},
  ): AsyncGenerator<Record<string, unknown>, void, void> {
    const payload: Record<string, unknown> = {
      model: options.model ?? "claude-3-5-sonnet-latest",
      messages,
      max_tokens: options.maxTokens ?? 1024,
      stream: true,
    };
    if (options.system) payload.system = options.system;
    if (options.tools) payload.tools = options.tools;
    if (options.extra) Object.assign(payload, options.extra);

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Api-Key": "mock-api-key",
      "Anthropic-Version": "2023-06-01",
    };
    if (options.sessionId) headers["X-Session-Id"] = options.sessionId;

    for await (const event of this.requestSSE("/v1/messages", headers, payload)) {
      const parsed = tryParseJSON(event.data) as
        | Record<string, unknown>
        | undefined;
      if (parsed === undefined) continue;
      yield parsed;
      if (parsed.type === "message_stop") return;
    }
  }

  /** Iterate a streamed completion as protocol-agnostic
   * :class:`StreamChunk` objects. Pick the wire format by passing
   * ``protocol: "openai"`` (default) or ``"anthropic"``.
   *
   * ```ts
   * for await (const chunk of client.iterStream(messages, { protocol: "anthropic" })) {
   *   process.stdout.write(chunk.text);
   *   if (chunk.finished) break;
   * }
   * ```
   */
  async *iterStream(
    messages: ChatMessage[],
    options: { protocol?: "openai" | "anthropic" } & ChatOptions & MessageOptions = {},
  ): AsyncGenerator<StreamChunk, void, void> {
    const protocol = options.protocol ?? "openai";
    if (protocol === "openai") {
      yield* normalizeOpenAIStream(this.chatStream(messages, options));
    } else if (protocol === "anthropic") {
      yield* normalizeAnthropicStream(this.messageStream(messages, options));
    } else {
      throw new Error(`unknown protocol ${String(protocol)}`);
    }
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

  /** POST a JSON body to ``path`` and yield parsed SSE events. Each
   * yielded value is the raw ``{event, data}`` pair after the
   * server-sent-events frame boundaries; the caller is responsible
   * for parsing ``data`` as JSON.
   */
  private async *requestSSE(
    path: string,
    headers: Record<string, string>,
    body: unknown,
  ): AsyncGenerator<{ event: string; data: string }, void, void> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    let resp: Response;
    try {
      resp = await this.fetchImpl(`${this.baseUrl}${path}`, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (err) {
      clearTimeout(timer);
      throw err;
    }

    if (!resp.ok) {
      clearTimeout(timer);
      const text = await resp.text().catch(() => "");
      throw new HTTPError(resp.status, text);
    }
    if (!resp.body) {
      clearTimeout(timer);
      return;
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder("utf-8");
    let buffer = "";
    try {
      // eslint-disable-next-line no-constant-condition
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        // SSE frames are terminated by a blank line. Process every
        // complete frame that the buffer currently holds.
        let sep = buffer.indexOf("\n\n");
        while (sep !== -1) {
          const frame = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);
          const event = parseSSEFrame(frame);
          if (event !== null) yield event;
          sep = buffer.indexOf("\n\n");
        }
      }
      // Drain any trailing frame that lacked a terminating blank line.
      const tail = buffer.trim();
      if (tail.length > 0) {
        const event = parseSSEFrame(tail);
        if (event !== null) yield event;
      }
    } finally {
      clearTimeout(timer);
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
  ): Promise<{ status: number; body: unknown }> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const requestHeaders = { ...headers };
      if (this.apiKey && requestHeaders.Authorization === undefined) {
        requestHeaders.Authorization = `Bearer ${this.apiKey}`;
      }
      const resp = await this.fetchImpl(`${this.baseUrl}${path}`, {
        method,
        headers: requestHeaders,
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
    }
  }
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

function tryParseJSON(text: string): unknown | undefined {
  try {
    return JSON.parse(text);
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
