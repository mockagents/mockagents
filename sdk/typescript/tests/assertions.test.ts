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
