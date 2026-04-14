// Tiny fluent assertion library tuned for asserting on ChatResponses
// and ScenarioResults. Assertion failures throw Error with a
// descriptive message — mirrors the Python SDK's `expect()` helper.

import { ChatResponse, ToolCall } from "./types.js";
import { ScenarioResult } from "./scenario.js";

type Target = ChatResponse | ScenarioResult;

export interface Expectation {
  toHaveToolCall(name: string, args?: Record<string, unknown>): Expectation;
  toHaveResponseContaining(text: string): Expectation;
  toHaveFinishReason(reason: string): Expectation;
  toHaveStatusCode(code: number): Expectation;
  toHaveLatencyLessThan(ms: number): Expectation;
  toHaveToolCallCount(count: number): Expectation;
}

export function expect(target: Target): Expectation {
  const response = isScenarioResult(target) ? target.last : target;
  const latencyMs = isScenarioResult(target) ? target.totalLatencyMs : target.latencyMs;

  const api: Expectation = {
    toHaveToolCall(name, args) {
      if (!hasToolCall(response.toolCalls, name, args)) {
        throw new AssertionError(
          `expected tool call ${JSON.stringify({ name, args })}, got ${JSON.stringify(
            response.toolCalls,
          )}`,
        );
      }
      return api;
    },
    toHaveResponseContaining(text) {
      if (!response.content.includes(text)) {
        throw new AssertionError(
          `expected response to contain ${JSON.stringify(text)}, got ${JSON.stringify(
            truncate(response.content, 120),
          )}`,
        );
      }
      return api;
    },
    toHaveFinishReason(reason) {
      if (response.finishReason !== reason) {
        throw new AssertionError(
          `expected finish_reason=${reason}, got ${response.finishReason}`,
        );
      }
      return api;
    },
    toHaveStatusCode(code) {
      if (response.statusCode !== code) {
        throw new AssertionError(
          `expected status_code=${code}, got ${response.statusCode}`,
        );
      }
      return api;
    },
    toHaveLatencyLessThan(ms) {
      if (latencyMs >= ms) {
        throw new AssertionError(`expected latency<${ms}ms, got ${latencyMs.toFixed(1)}ms`);
      }
      return api;
    },
    toHaveToolCallCount(count) {
      if (response.toolCalls.length !== count) {
        throw new AssertionError(
          `expected ${count} tool calls, got ${response.toolCalls.length}`,
        );
      }
      return api;
    },
  };
  return api;
}

export class AssertionError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AssertionError";
  }
}

function isScenarioResult(t: Target): t is ScenarioResult {
  return (t as ScenarioResult).scenarioName !== undefined;
}

function hasToolCall(
  calls: ToolCall[],
  name: string,
  expectedArgs?: Record<string, unknown>,
): boolean {
  for (const call of calls) {
    if (call.name !== name) continue;
    if (!expectedArgs) return true;
    if (argsMatch(call.arguments, expectedArgs)) return true;
  }
  return false;
}

function argsMatch(actual: Record<string, unknown>, expected: Record<string, unknown>): boolean {
  for (const [k, v] of Object.entries(expected)) {
    if (!deepEqual(actual[k], v)) return false;
  }
  return true;
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== typeof b) return false;
  if (a === null || b === null) return a === b;
  if (typeof a !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) {
    if ((a as unknown[]).length !== (b as unknown[]).length) return false;
    for (let i = 0; i < a.length; i++) {
      if (!deepEqual((a as unknown[])[i], (b as unknown[])[i])) return false;
    }
    return true;
  }
  const aKeys = Object.keys(a as object);
  const bKeys = Object.keys(b as object);
  if (aKeys.length !== bKeys.length) return false;
  for (const key of aKeys) {
    if (!deepEqual((a as Record<string, unknown>)[key], (b as Record<string, unknown>)[key])) {
      return false;
    }
  }
  return true;
}

function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n) + "...";
}
