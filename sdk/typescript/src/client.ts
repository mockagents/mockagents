// HTTP client for the MockAgents server. Uses native `fetch` (Node 18+)
// and returns a ChatResponse shape consistent with the Python SDK.

import {
  AgentSummary,
  ChatMessage,
  ChatResponse,
  HTTPError,
  parseToolCallAnthropic,
  parseToolCallOpenAI,
  parseUsageAnthropic,
  parseUsageOpenAI,
  ToolCall,
} from "./types.js";

export interface MockAgentClientOptions {
  baseUrl?: string;
  timeoutMs?: number;
  /** Override the global fetch implementation (useful for tests). */
  fetch?: typeof fetch;
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

  constructor(options: MockAgentClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "http://localhost:8080").replace(/\/+$/, "");
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.fetchImpl = options.fetch ?? fetch;
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

  // --- internals ---

  private async requestJSON(
    method: string,
    path: string,
    headers: Record<string, string> = {},
    body?: unknown,
  ): Promise<{ status: number; body: unknown }> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const resp = await this.fetchImpl(`${this.baseUrl}${path}`, {
        method,
        headers,
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
