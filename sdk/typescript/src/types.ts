// Shared types returned by MockAgentClient. These mirror the Python
// SDK's ChatResponse/ToolCall/TokenUsage shape so examples translate
// one-to-one between the two languages.

export interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
}

export interface ToolCall {
  id: string;
  name: string;
  /**
   * Parsed arguments. `{}` when the wire arguments were malformed or not a
   * JSON object — check `argumentsValid` to tell that apart from a call that
   * genuinely took no arguments.
   */
  arguments: Record<string, unknown>;
  /**
   * The arguments exactly as they arrived on the wire (OpenAI sends a JSON
   * string; for Anthropic this is the `input` object re-serialized). Empty
   * when the wire carried no arguments at all. Set by every SDK parser;
   * optional only so hand-built ToolCall literals stay valid.
   */
  rawArguments?: string;
  /**
   * False when the wire arguments did not decode to a JSON object (a
   * malformed-arguments fault fixture, a bare array, a missing field). Set by
   * every SDK parser; `undefined` on a hand-built ToolCall means "not checked".
   */
  argumentsValid?: boolean;
}

export interface ChatResponse {
  content: string;
  model: string;
  toolCalls: ToolCall[];
  finishReason: string;
  usage?: TokenUsage;
  raw: unknown;
  statusCode: number;
  latencyMs: number;
}

/** One provider-shaped content part (`{type: "text", text}`, `{type:
 * "image_url", ...}`, Anthropic `tool_result` blocks, ...). Passed through to
 * the wire untouched. */
export interface ChatContentPart {
  type: string;
  [key: string]: unknown;
}

/** An assistant tool call in OpenAI wire shape, for replaying a tool round
 * trip (`assistant` turn with `tool_calls`, then `tool` turns). */
export interface ChatMessageToolCall {
  id: string;
  type: "function";
  function: { name: string; arguments: string };
}

export interface ChatMessage {
  role: "user" | "assistant" | "system" | "tool";
  content: string | ChatContentPart[];
  // Optional fields carried through for OpenAI tool-response messages.
  tool_call_id?: string;
  name?: string;
  /** Assistant tool calls, so a tool round trip can be replayed verbatim. */
  tool_calls?: ChatMessageToolCall[];
}

/**
 * Build the assistant history turn for `response`, including its tool calls in
 * OpenAI wire shape (arguments are the raw wire string when the SDK saw one).
 * Append it, then one `{role: "tool", tool_call_id, content}` turn per call, to
 * continue a tool round trip — required when strict-tools validates ids.
 */
export function toAssistantMessage(response: ChatResponse): ChatMessage {
  const message: ChatMessage = { role: "assistant", content: response.content };
  if (response.toolCalls.length > 0) {
    message.tool_calls = response.toolCalls.map((call) => ({
      id: call.id,
      type: "function",
      function: {
        name: call.name,
        arguments:
          call.rawArguments !== undefined && call.rawArguments !== ""
            ? call.rawArguments
            : JSON.stringify(call.arguments ?? {}),
      },
    }));
  }
  return message;
}

/**
 * Protocol-agnostic chunk yielded by ``MockAgentClient.iterStream``.
 * Both OpenAI and Anthropic SSE streams normalize into this shape so
 * user code can iterate the same way regardless of provider.
 *
 * - ``text`` — incremental text delta. Empty for non-text events.
 * - ``toolCallDelta`` — ``[index, name, argumentsFragment]`` triple
 *   when the chunk carries a partial tool call. ``argumentsFragment``
 *   is the raw JSON fragment as the provider streams it; callers
 *   that need parsed arguments should accumulate fragments themselves.
 * - ``finishReason`` — set on the terminal chunk only (e.g. "stop",
 *   "tool_calls", "end_turn"). Empty otherwise.
 * - ``finished`` — true on the terminal chunk so consumers can
 *   ``break`` out of a loop without inspecting strings.
 * - ``raw`` — the original event dict from the wire, for callers
 *   that need provider-specific fields the normalization dropped.
 */
export interface StreamChunk {
  text: string;
  toolCallDelta?: [number, string, string];
  finishReason: string;
  finished: boolean;
  raw: unknown;
}

/**
 * Live bookkeeping for one stream, readable through the `stats` property of
 * the object `chatStream` / `messageStream` / `iterStream` return. Fault
 * injection (`streaming.truncateAfter`, `streaming.malformed`) shows up here
 * instead of looking like a normal completion.
 *
 * - `completed` — the terminal sentinel arrived (`[DONE]` / `message_stop`).
 * - `truncated` — the body ended without that sentinel.
 * - `malformedFrames` — `data:` frames that were not valid JSON (skipped).
 *
 * A consumer that stops iterating early sees all three still false / zero.
 */
export interface StreamStats {
  completed: boolean;
  truncated: boolean;
  malformedFrames: number;
}

export interface AgentSummary {
  name: string;
  description?: string;
  model: string;
  protocol: string;
  scenario_count: number;
  tool_count: number;
  tags?: string[];
}

export interface PipelineNodeResult {
  nodeId: string;
  agentName: string;
  response: Record<string, unknown>;
  latencyNs: number;
}

export interface PipelineResult {
  pipelineName: string;
  topology: string;
  nodes: PipelineNodeResult[];
  latencyNs: number;
}

export class MockAgentsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "MockAgentsError";
  }
}

export class ConfigError extends MockAgentsError {
  constructor(message: string) {
    super(message);
    this.name = "ConfigError";
  }
}

export class ServerError extends MockAgentsError {
  constructor(message: string) {
    super(message);
    this.name = "ServerError";
  }
}

export class HTTPError extends MockAgentsError {
  public readonly status: number;
  public readonly body: string;

  constructor(status: number, body: string) {
    super(`HTTP ${status}: ${body}`);
    this.name = "HTTPError";
    this.status = status;
    this.body = body;
  }
}

/** Thrown by a stream that ended badly: `reason` says how. `"idle_timeout"`
 * comes from `idleTimeoutMs`; `"truncated"` / `"malformed"` only when the
 * caller opted in with `failOnStreamFault`. */
export class StreamError extends MockAgentsError {
  public readonly reason: "truncated" | "malformed" | "idle_timeout";
  public readonly stats: StreamStats;

  constructor(reason: "truncated" | "malformed" | "idle_timeout", stats: StreamStats, message?: string) {
    super(
      message ??
        `stream ${reason === "truncated" ? "ended without its terminal event" : "carried malformed frames"} ` +
          `(completed=${stats.completed}, truncated=${stats.truncated}, malformedFrames=${stats.malformedFrames})`,
    );
    this.name = "StreamError";
    this.reason = reason;
    this.stats = { ...stats };
  }
}

// Parsers used by MockAgentClient. Extracted so tests can exercise them
// against raw JSON without spinning up HTTP.

export function parseToolCallOpenAI(tc: any): ToolCall {
  const wire = tc?.function?.arguments;
  let raw = "";
  let parsed: unknown;
  if (typeof wire === "string") {
    raw = wire;
    parsed = tryParse(wire);
  } else if (wire !== undefined) {
    // Non-standard: arguments sent as an object rather than a JSON string.
    raw = JSON.stringify(wire) ?? "";
    parsed = wire;
  }
  const valid = isPlainObject(parsed);
  return {
    id: String(tc?.id ?? ""),
    name: String(tc?.function?.name ?? ""),
    arguments: valid ? (parsed as Record<string, unknown>) : {},
    rawArguments: raw,
    argumentsValid: valid,
  };
}

export function parseToolCallAnthropic(block: any): ToolCall {
  const input = block?.input;
  const valid = isPlainObject(input);
  return {
    id: String(block?.id ?? ""),
    name: String(block?.name ?? ""),
    arguments: valid ? (input as Record<string, unknown>) : {},
    rawArguments: input === undefined ? "" : (JSON.stringify(input) ?? ""),
    argumentsValid: valid,
  };
}

export function parseUsageOpenAI(u: any): TokenUsage {
  return {
    promptTokens: Number(u?.prompt_tokens ?? 0),
    completionTokens: Number(u?.completion_tokens ?? 0),
    totalTokens: Number(u?.total_tokens ?? 0),
  };
}

export function parseUsageAnthropic(u: any): TokenUsage {
  const input = Number(u?.input_tokens ?? 0);
  const output = Number(u?.output_tokens ?? 0);
  return {
    promptTokens: input,
    completionTokens: output,
    totalTokens: input + output,
  };
}

function tryParse(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return undefined;
  }
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}
