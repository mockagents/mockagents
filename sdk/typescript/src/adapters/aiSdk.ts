// Vercel AI SDK adapter. Returns a preconfigured OpenAI provider
// pointed at a MockAgents server so `generateText({ model: provider("gpt-4o") })`
// hits the mock without setting any env vars.

import { resolveBaseUrl, requireModule, ServerLike } from "./_common.js";

export interface MockOpenAIProviderOptions {
  apiKey?: string;
  headers?: Record<string, string>;
  [key: string]: unknown;
}

export async function mockOpenAIProvider(
  server: ServerLike,
  options: MockOpenAIProviderOptions = {},
): Promise<unknown> {
  const baseUrl = resolveBaseUrl(server) + "/v1";
  const mod = await requireModule<{
    createOpenAI: (opts: unknown) => unknown;
  }>("@ai-sdk/openai", "@ai-sdk/openai");
  return mod.createOpenAI({
    baseURL: baseUrl,
    apiKey: options.apiKey ?? "mock-key",
    ...options,
  });
}
