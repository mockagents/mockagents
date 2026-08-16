import { describe, expect as vexpect, it } from "vitest";

import { AssertionError, expect } from "../src/assertions.js";
import { ChatResponse } from "../src/types.js";

function fakeResponse(overrides: Partial<ChatResponse> = {}): ChatResponse {
  return {
    content: "hello world",
    model: "gpt-4o",
    toolCalls: [
      { id: "t1", name: "lookup_order", arguments: { id: "ORD-1", region: "us" } },
    ],
    finishReason: "stop",
    raw: {},
    statusCode: 200,
    latencyMs: 42,
    ...overrides,
  };
}

describe("expect() fluent assertions", () => {
  it("chains passing matchers", () => {
    expect(fakeResponse())
      .toHaveResponseContaining("hello")
      .toHaveFinishReason("stop")
      .toHaveStatusCode(200)
      .toHaveLatencyLessThan(100)
      .toHaveToolCallCount(1)
      .toHaveToolCall("lookup_order", { id: "ORD-1" });
  });

  it("throws AssertionError on missing content", () => {
    vexpect(() => expect(fakeResponse()).toHaveResponseContaining("goodbye")).toThrowError(
      AssertionError,
    );
  });

  it("fails when tool arguments do not match", () => {
    vexpect(() =>
      expect(fakeResponse()).toHaveToolCall("lookup_order", { id: "ORD-9" }),
    ).toThrowError(/expected tool call/);
  });

  it("fails when tool call is absent", () => {
    const resp = fakeResponse({ toolCalls: [] });
    vexpect(() => expect(resp).toHaveToolCall("lookup_order")).toThrowError(AssertionError);
  });

  it("fails on latency exceeded", () => {
    vexpect(() => expect(fakeResponse({ latencyMs: 500 })).toHaveLatencyLessThan(100)).toThrowError(
      /latency/,
    );
  });

  it("deep-compares nested objects", () => {
    const resp = fakeResponse({
      toolCalls: [{ id: "t", name: "upsert", arguments: { record: { id: 1, tags: ["a"] } } }],
    });
    expect(resp).toHaveToolCall("upsert", { record: { id: 1, tags: ["a"] } });
    vexpect(() =>
      expect(resp).toHaveToolCall("upsert", { record: { id: 1, tags: ["b"] } }),
    ).toThrowError(AssertionError);
  });
});

// --- Trajectory assertions -------------------------------------------------
//
// Pinned to the Go runner's semantics for the `kind: TestSuite` assertions of
// the same names: read the AGGREGATE across every turn, and compare a sequence
// for full equality rather than as a subsequence.

function call(name: string) {
  return { id: `id-${name}`, name, arguments: {} };
}

/** A ScenarioResult shaped like runScenario's, including the aggregate getter. */
function fakeScenario(...turns: Array<ReturnType<typeof call>[]>) {
  const responses = turns.map((toolCalls) =>
    fakeResponse({ toolCalls, content: "" }),
  );
  return {
    scenarioName: "s",
    responses,
    totalLatencyMs: 1,
    get lastContent() {
      return responses[responses.length - 1]?.content ?? "";
    },
    get last() {
      return responses[responses.length - 1];
    },
    get toolCalls() {
      return responses.flatMap((r) => r.toolCalls);
    },
  };
}

describe("trajectory assertions", () => {
  it("matches an exact tool-call sequence", () => {
    expect(fakeScenario([call("search")], [call("summarize")]))
      .toHaveToolCallSequence(["search", "summarize"]);
  });

  it("aggregates across turns rather than reading only the last", () => {
    const result = fakeScenario([call("a"), call("b")], [call("c")]);
    expect(result).toHaveToolCallSequence(["a", "b", "c"]);
    expect(result).toHaveToolCallCount(3);
  });

  it("rejects the wrong order and names what happened", () => {
    vexpect(() =>
      expect(fakeScenario([call("summarize"), call("search")]))
        .toHaveToolCallSequence(["search", "summarize"]),
    ).toThrow(/summarize/);
  });

  it("is full equality, not a subsequence check", () => {
    vexpect(() =>
      expect(fakeScenario([call("search"), call("rerank"), call("summarize")]))
        .toHaveToolCallSequence(["search", "summarize"]),
    ).toThrow(AssertionError);
  });

  it("handles an empty trajectory", () => {
    expect(fakeScenario([])).toHaveToolCallSequence([]).toHaveToolCallCount(0);
  });

  it("counts calls to a single named tool", () => {
    const result = fakeScenario([call("search"), call("search"), call("summarize")]);
    expect(result).toHaveToolCallCount(2, "search");
    expect(result).toHaveToolCallCount(1, "summarize");
    expect(result).toHaveToolCallCount(0, "absent");
  });

  it("rejects a named-count mismatch", () => {
    vexpect(() =>
      expect(fakeScenario([call("search")])).toHaveToolCallCount(2, "search"),
    ).toThrow(/search/);
  });

  it("finds a tool call from an earlier turn, not just the final one", () => {
    // Regression: these read `target.last` and so missed earlier turns, which
    // disagreed with both the YAML runner and the Python SDK.
    const result = fakeScenario([call("search")], [call("summarize")]);
    expect(result).toHaveToolCall("search");
    expect(result).toHaveToolCallCount(2);
  });

  it("works on a bare ChatResponse", () => {
    expect(fakeResponse({ toolCalls: [call("search")] }))
      .toHaveToolCallSequence(["search"])
      .toHaveToolCallCount(1);
  });
});
