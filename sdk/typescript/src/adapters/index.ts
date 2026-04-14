// Framework adapters for the TS SDK. Lazy-import peer frameworks so
// installing `mockagents` never drags LangChain or Vercel AI SDK in.

export { chatOpenAI, chatAnthropic, patchEnv } from "./langchain.js";
export { mockOpenAIProvider } from "./aiSdk.js";
