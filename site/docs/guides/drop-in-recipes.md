# Drop-in Recipes

MockAgents speaks the OpenAI, Anthropic, and Gemini wire formats, so the only
change to your app is the **base URL** — everything that talks these APIs over
HTTP works unmodified. Start the mock, then pick your stack below.

```bash
mockagents start            # http://localhost:8080
```

## Official SDKs

=== "OpenAI (Python)"

    ```python
    from openai import OpenAI
    client = OpenAI(base_url="http://localhost:8080/v1", api_key="mock")
    # or set OPENAI_BASE_URL=http://localhost:8080/v1 and construct OpenAI()
    ```

=== "OpenAI (JS/TS)"

    ```ts
    import OpenAI from "openai";
    const client = new OpenAI({ baseURL: "http://localhost:8080/v1", apiKey: "mock" });
    ```

=== "Anthropic (Python)"

    ```python
    from anthropic import Anthropic
    client = Anthropic(base_url="http://localhost:8080", api_key="mock")
    # or ANTHROPIC_BASE_URL=http://localhost:8080
    ```

=== "Anthropic (JS/TS)"

    ```ts
    import Anthropic from "@anthropic-ai/sdk";
    const client = new Anthropic({ baseURL: "http://localhost:8080", apiKey: "mock" });
    ```

=== "Gemini (google-genai)"

    ```python
    from google import genai
    # The newer google-genai client honors GOOGLE_GEMINI_BASE_URL, or pass it:
    client = genai.Client(api_key="mock", http_options={"base_url": "http://localhost:8080"})
    ```

    > The **legacy** `google-generativeai` SDK has no base-URL env var — use
    > `genai.configure(client_options={"api_endpoint": "localhost:8080"})`.

## Frameworks

=== "Vercel AI SDK"

    ```ts
    import { createOpenAI } from "@ai-sdk/openai";
    const openai = createOpenAI({ baseURL: "http://localhost:8080/v1", apiKey: "mock" });
    // const anthropic = createAnthropic({ baseURL: "http://localhost:8080" });
    const { text } = await generateText({ model: openai("gpt-4o"), prompt: "hello" });
    ```

=== "LangChain (Python)"

    ```python
    from langchain_openai import ChatOpenAI
    llm = ChatOpenAI(model="gpt-4o", base_url="http://localhost:8080/v1", api_key="mock")

    from langchain_anthropic import ChatAnthropic
    claude = ChatAnthropic(model="claude-3-5-sonnet", base_url="http://localhost:8080", api_key="mock")
    ```

    The Python SDK also ships zero-boilerplate adapters —
    `from mockagents.adapters import chat_openai, crewai_mock_llm, patched_env`.

=== "LlamaIndex"

    ```python
    from llama_index.llms.openai import OpenAI
    llm = OpenAI(model="gpt-4o", api_base="http://localhost:8080/v1", api_key="mock")
    ```

## In tests

- **pytest:** the `mockagents` fixture (ships with `pip install mockagents`)
  sets the base-URL env vars for you — see [Testing AI Agents](testing-agents.md).
- **CI:** the [`setup-mockagents` GitHub Action](https://github.com/mockagents/mockagents/tree/main/deploy/actions/setup-mockagents)
  starts the server and exports `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL`.

> Anything not listed here works too, as long as it lets you set the base URL
> (env var or client option). That's the whole point — MockAgents is a drop-in
> for the wire protocol, not a framework you integrate against.
