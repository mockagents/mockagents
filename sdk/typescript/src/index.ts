// Public entry point for the MockAgents TypeScript SDK.

export { MockAgentClient, DEFAULT_ANTHROPIC_MODEL, DEFAULT_OPENAI_MODEL } from "./client.js";
export type {
  MockAgentClientOptions,
  ChatOptions,
  MessageOptions,
  MockAgentStream,
  StreamControlOptions,
} from "./client.js";

export { MockAgentServer, findFreePort, findBinary } from "./server.js";
export type { MockAgentServerOptions } from "./server.js";

export { Scenario, runScenario } from "./scenario.js";
export type { ScenarioOptions, ScenarioStep, ScenarioResult } from "./scenario.js";

export { expect, AssertionError } from "./assertions.js";
export type { Expectation, PipelineExpectation } from "./assertions.js";

export type {
  AgentSummary,
  ChatContentPart,
  ChatMessage,
  ChatMessageToolCall,
  ChatResponse,
  PipelineNodeResult,
  PipelineResult,
  StreamChunk,
  StreamStats,
  ToolCall,
  TokenUsage,
} from "./types.js";
export {
  ConfigError,
  HTTPError,
  MockAgentsError,
  ServerError,
  StreamError,
  toAssistantMessage,
} from "./types.js";

export { McpClient, isRequest, paramsOf, parseMcpFrame } from "./mcp.js";
export type {
  McpClientOptions,
  McpEvent,
  McpEventStream,
  JsonRpcEnvelope,
  JsonRpcError,
  McpRequestHandler,
} from "./mcp.js";

export * as adapters from "./adapters/index.js";
