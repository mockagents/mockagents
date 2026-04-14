// Scenario authoring surface: build conversational test cases in code
// and run them against a MockAgentClient. Mirrors the Python SDK.

import { MockAgentClient } from "./client.js";
import { ChatMessage, ChatResponse } from "./types.js";

export interface ScenarioStep {
  role: "user" | "assistant" | "system";
  content: string;
}

export interface ScenarioOptions {
  name: string;
  steps: ScenarioStep[];
  /** Conversation protocol to use. Defaults to openai. */
  protocol?: "openai" | "anthropic";
  /** Session id. Random UUID when omitted. */
  sessionId?: string;
  model?: string;
}

export class Scenario {
  public readonly name: string;
  public readonly steps: ScenarioStep[];
  public readonly protocol: "openai" | "anthropic";
  public readonly sessionId: string;
  public readonly model?: string;

  constructor(options: ScenarioOptions) {
    if (!options.name) throw new Error("Scenario name is required");
    if (options.steps.length === 0) {
      throw new Error("Scenario must have at least one step");
    }
    this.name = options.name;
    this.steps = options.steps;
    this.protocol = options.protocol ?? "openai";
    this.sessionId = options.sessionId ?? `scenario-${randomId()}`;
    this.model = options.model;
  }
}

export interface ScenarioResult {
  scenarioName: string;
  responses: ChatResponse[];
  totalLatencyMs: number;
  /** Shortcut to the final response's content. */
  readonly lastContent: string;
  /** Shortcut to the final ChatResponse (throws if empty). */
  readonly last: ChatResponse;
}

/**
 * Execute every step in the scenario against the given client. Each
 * user step is sent as a single request; assistant/system steps are
 * accumulated as prior context but not sent.
 */
export async function runScenario(
  client: MockAgentClient,
  scenario: Scenario,
): Promise<ScenarioResult> {
  const history: ChatMessage[] = [];
  const responses: ChatResponse[] = [];
  const start = performance.now();

  for (const step of scenario.steps) {
    history.push({ role: step.role, content: step.content });
    if (step.role !== "user") continue;

    const response = await (scenario.protocol === "anthropic"
      ? client.message(history.slice(), {
          sessionId: scenario.sessionId,
          model: scenario.model,
        })
      : client.chat(history.slice(), {
          sessionId: scenario.sessionId,
          model: scenario.model,
        }));
    responses.push(response);
    // Mirror the returned content into history so subsequent user steps
    // see the conversation the way the server does.
    history.push({ role: "assistant", content: response.content });
  }

  const totalLatencyMs = performance.now() - start;
  const last = responses.length > 0 ? responses[responses.length - 1] : undefined;

  return {
    scenarioName: scenario.name,
    responses,
    totalLatencyMs,
    get lastContent() {
      return last?.content ?? "";
    },
    get last() {
      if (!last) throw new Error("scenario produced no responses");
      return last;
    },
  };
}

function randomId(): string {
  return Math.random().toString(36).slice(2, 10);
}
