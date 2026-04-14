// Public entry point for the MockAgents TypeScript SDK.

export { MockAgentClient } from "./client.js";
export type { MockAgentClientOptions, ChatOptions, MessageOptions } from "./client.js";

export { MockAgentServer, findFreePort, findBinary } from "./server.js";
export type { MockAgentServerOptions } from "./server.js";

export { Scenario, runScenario } from "./scenario.js";
export type { ScenarioOptions, ScenarioStep, ScenarioResult } from "./scenario.js";

export { expect, AssertionError } from "./assertions.js";
export type { Expectation } from "./assertions.js";

export type {
  AgentSummary,
  ChatMessage,
  ChatResponse,
  ToolCall,
  TokenUsage,
} from "./types.js";
export {
  ConfigError,
  HTTPError,
  MockAgentsError,
  ServerError,
} from "./types.js";

export * as adapters from "./adapters/index.js";
