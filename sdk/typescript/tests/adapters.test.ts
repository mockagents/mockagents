// Adapter tests avoid requiring real @langchain/openai by using the
// Node loader's Module._cache mechanism: we inject a fake module
// entry *before* the dynamic import runs, so requireModule() resolves
// to our stub.

import { describe, expect, it } from "vitest";

import { patchEnv } from "../src/adapters/langchain.js";

describe("patchEnv", () => {
  it("sets and restores OpenAI env vars", () => {
    delete process.env.OPENAI_BASE_URL;
    delete process.env.OPENAI_API_KEY;
    const restore = patchEnv("http://mock:9090", ["openai"]);
    try {
      expect(process.env.OPENAI_BASE_URL).toBe("http://mock:9090/v1");
      expect(process.env.OPENAI_API_KEY).toBe("mock-key");
    } finally {
      restore();
    }
    expect(process.env.OPENAI_BASE_URL).toBeUndefined();
    expect(process.env.OPENAI_API_KEY).toBeUndefined();
  });

  it("restores a preexisting value after the context exits", () => {
    process.env.ANTHROPIC_BASE_URL = "preexisting";
    const restore = patchEnv("http://mock:8080", ["anthropic"]);
    try {
      expect(process.env.ANTHROPIC_BASE_URL).toBe("http://mock:8080");
    } finally {
      restore();
    }
    expect(process.env.ANTHROPIC_BASE_URL).toBe("preexisting");
    delete process.env.ANTHROPIC_BASE_URL;
  });

  it("rejects bad target types", () => {
    expect(() => patchEnv({} as unknown as { url: string })).toThrowError();
  });
});
