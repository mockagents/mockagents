// LangChain / LangGraph adapters for the TS SDK.

import { resolveBaseUrl, requireModule, ServerLike } from "./_common.js";

export interface ChatOpenAIOptions {
  model?: string;
  apiKey?: string;
  [key: string]: unknown;
}

export async function chatOpenAI(
  server: ServerLike,
  options: ChatOpenAIOptions = {},
): Promise<unknown> {
  const baseUrl = resolveBaseUrl(server) + "/v1";
  const mod = await requireModule<{ ChatOpenAI: new (opts: unknown) => unknown }>(
    "@langchain/openai",
    "@langchain/openai",
  );
  const { model = "gpt-4o", apiKey = "mock-key", ...rest } = options;
  return new mod.ChatOpenAI({
    model,
    apiKey,
    configuration: { baseURL: baseUrl },
    ...rest,
  });
}

export interface ChatAnthropicOptions {
  model?: string;
  apiKey?: string;
  [key: string]: unknown;
}

export async function chatAnthropic(
  server: ServerLike,
  options: ChatAnthropicOptions = {},
): Promise<unknown> {
  const baseUrl = resolveBaseUrl(server);
  const mod = await requireModule<{ ChatAnthropic: new (opts: unknown) => unknown }>(
    "@langchain/anthropic",
    "@langchain/anthropic",
  );
  const { model = "claude-3-5-sonnet-latest", apiKey = "mock-key", ...rest } = options;
  return new mod.ChatAnthropic({
    model,
    apiKey,
    anthropicApiUrl: baseUrl,
    ...rest,
  });
}

/**
 * Temporarily patch OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars so
 * frameworks that build their own clients internally (LangGraph, ai-sdk,
 * etc.) pick up the mock. Returns a disposer that restores the previous
 * values — use with `try/finally`.
 */
export function patchEnv(
  server: ServerLike,
  providers: ("openai" | "anthropic")[] = ["openai", "anthropic"],
): () => void {
  const base = resolveBaseUrl(server);
  const vars: Record<string, string> = {};
  if (providers.includes("openai")) {
    vars.OPENAI_BASE_URL = base + "/v1";
    vars.OPENAI_API_KEY = "mock-key";
  }
  if (providers.includes("anthropic")) {
    vars.ANTHROPIC_BASE_URL = base;
    vars.ANTHROPIC_API_KEY = "mock-key";
  }
  const previous: Record<string, string | undefined> = {};
  for (const k of Object.keys(vars)) previous[k] = process.env[k];
  for (const [k, v] of Object.entries(vars)) process.env[k] = v;

  return () => {
    for (const [k, v] of Object.entries(previous)) {
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    }
  };
}
