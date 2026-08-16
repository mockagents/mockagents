// Tiny fluent assertion library tuned for asserting on ChatResponses
// and ScenarioResults. Assertion failures throw Error with a
// descriptive message — mirrors the Python SDK's `expect()` helper.
//
// Outcome vs trajectory
// ---------------------
// The two families read different slices of a ScenarioResult, matching how the
// Go test runner evaluates `kind: TestSuite` assertions:
//
//   outcome     (content, finish reason, status) -> the FINAL turn
//   trajectory  (tool calls, counts, sequences)  -> the AGGREGATE of all turns
//
// The trajectory assertions used to read only the final turn here, which made a
// multi-turn `toHaveToolCall` disagree with both the YAML runner and the Python
// SDK. They now read the aggregate. Single-turn scenarios are unaffected.
//
// `node_sequence` is not ported: a `kind: Pipeline` can only be executed by the
// in-process CLI runner, so an HTTP client cannot produce the node trajectory.
// See https://github.com/mockagents/mockagents/issues/33.

import { ChatResponse, ToolCall } from "./types.js";
import { ScenarioResult } from "./scenario.js";

type Target = ChatResponse | ScenarioResult;

export interface Expectation {
  toHaveToolCall(name: string, args?: Record<string, unknown>): Expectation;
  toHaveResponseContaining(text: string): Expectation;
  toHaveFinishReason(reason: string): Expectation;
  toHaveStatusCode(code: number): Expectation;
  toHaveLatencyLessThan(ms: number): Expectation;
  /**
   * Assert how many tool calls were made across the whole trajectory. With no
   * `name` this is the `tool_call_count` assertion of `kind: TestSuite` YAML.
   * Passing `name` narrows to one tool — an SDK-only convenience with no YAML
   * equivalent.
   */
  toHaveToolCallCount(count: number, name?: string): Expectation;
  /**
   * Assert the exact ordered sequence of tool-call names across the whole
   * trajectory. Full equality, not a subsequence — the `tool_call_sequence`
   * assertion of `kind: TestSuite` YAML.
   */
  toHaveToolCallSequence(names: string[]): Expectation;
}

export function expect(target: Target): Expectation {
  const response = isScenarioResult(target) ? target.last : target;
  const latencyMs = isScenarioResult(target) ? target.totalLatencyMs : target.latencyMs;
  // Trajectory assertions read every turn; outcome assertions read `response`.
  // Both branches of the union expose `toolCalls` — on a ScenarioResult it is
  // the cross-turn aggregate, on a bare ChatResponse it is that one response.
  const toolCalls = target.toolCalls;

  const api: Expectation = {
    toHaveToolCall(name, args) {
      if (!hasToolCall(toolCalls, name, args)) {
        throw new AssertionError(
          `expected tool call ${JSON.stringify({ name, args })}, got ${JSON.stringify(
            toolCalls,
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
    toHaveToolCallCount(count, name) {
      const got =
        name === undefined
          ? toolCalls.length
          : toolCalls.filter((c) => c.name === name).length;
      if (got !== count) {
        const subject = name === undefined ? "tool calls" : `calls to ${JSON.stringify(name)}`;
        throw new AssertionError(
          `expected ${count} ${subject}, got ${got}: ${JSON.stringify(
            toolCalls.map((c) => c.name),
          )}`,
        );
      }
      return api;
    },
    toHaveToolCallSequence(names) {
      const got = toolCalls.map((c) => c.name);
      if (got.length !== names.length || got.some((n, i) => n !== names[i])) {
        throw new AssertionError(
          `expected tool call sequence ${JSON.stringify(names)}, got ${JSON.stringify(got)}`,
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
