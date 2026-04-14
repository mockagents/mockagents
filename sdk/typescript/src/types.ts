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
  arguments: Record<string, unknown>;
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

export interface ChatMessage {
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  // Optional fields carried through for OpenAI tool-response messages.
  tool_call_id?: string;
  name?: string;
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

// Parsers used by MockAgentClient. Extracted so tests can exercise them
// against raw JSON without spinning up HTTP.

export function parseToolCallOpenAI(tc: any): ToolCall {
  const args: Record<string, unknown> =
    typeof tc?.function?.arguments === "string"
      ? safeJSONParse(tc.function.arguments)
      : (tc?.function?.arguments ?? {});
  return {
    id: String(tc?.id ?? ""),
    name: String(tc?.function?.name ?? ""),
    arguments: args,
  };
}

export function parseToolCallAnthropic(block: any): ToolCall {
  return {
    id: String(block?.id ?? ""),
    name: String(block?.name ?? ""),
    arguments: (block?.input ?? {}) as Record<string, unknown>,
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

function safeJSONParse(s: string): Record<string, unknown> {
  try {
    const v = JSON.parse(s);
    return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}
