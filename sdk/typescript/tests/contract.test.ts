// Cross-SDK contract runner (TypeScript).
//
// Loads the shared case file sdk/contract/cases.json, executes each case
// through the SDK's public client + assertion API against a running server,
// and checks that every assertion's verdict (pass / fail / error) equals the
// verdict the case file expects. The Python and Go runners read the same file,
// so the three SDKs reach identical verdicts by construction.
//
// Skipped unless MOCKAGENTS_CONTRACT_URL points at a server started with
// `--agents-dir sdk/contract/agents`. With MOCKAGENTS_CONTRACT_TENANT_KEY set
// (the multi-tenant leg) only the cases carrying an `auth` field run; without
// it only the cases that do not. See sdk/contract/README.md.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect as vitestExpect, it } from "vitest";

import { AssertionError, MockAgentClient, Scenario, expect, runScenario } from "../src/index.js";
import type { ChatMessage, ChatResponse, ScenarioResult } from "../src/index.js";

const SDK = "typescript";
const here = dirname(fileURLToPath(import.meta.url));
const CASES_PATH = resolve(here, "..", "..", "contract", "cases.json");
const BASE_URL = (process.env.MOCKAGENTS_CONTRACT_URL ?? "").trim();
const TENANT_KEY = (process.env.MOCKAGENTS_CONTRACT_TENANT_KEY ?? "").trim();

type Verdict = "pass" | "fail" | "error";
type Protocol = "openai" | "anthropic";

interface ContractAssertion {
  kind: string;
  args?: Record<string, unknown>;
  expect: Verdict;
}

interface ContractCase {
  name: string;
  protocol: Protocol;
  stream: boolean;
  model?: string;
  auth?: "none" | "tenant" | "wrong";
  steps: string[];
  assertions: ContractAssertion[];
  known_divergence?: string | string[];
}

interface ContractSuite {
  default_model: string;
  wrong_api_key: string;
  cases: ContractCase[];
}

const suite = JSON.parse(readFileSync(CASES_PATH, "utf-8")) as ContractSuite;

/** The known_divergence note that applies to this SDK, if any. */
function divergence(c: ContractCase): string | undefined {
  if (!c.known_divergence) return undefined;
  const notes = Array.isArray(c.known_divergence) ? c.known_divergence : [c.known_divergence];
  for (const note of notes) {
    const i = note.indexOf(":");
    if (i >= 0 && note.slice(0, i).trim().toLowerCase() === SDK) {
      return note.slice(i + 1).trim();
    }
  }
  return undefined;
}

function clientFor(c: ContractCase): MockAgentClient {
  if (c.auth === "tenant") return new MockAgentClient({ baseUrl: BASE_URL, apiKey: TENANT_KEY });
  if (c.auth === "wrong") {
    return new MockAgentClient({ baseUrl: BASE_URL, apiKey: suite.wrong_api_key });
  }
  return new MockAgentClient({ baseUrl: BASE_URL });
}

function sessionId(): string {
  return `contract-${SDK}-${Math.random().toString(36).slice(2, 14)}`;
}

async function runPlain(
  client: MockAgentClient,
  c: ContractCase,
  model: string,
): Promise<ScenarioResult> {
  const scenario = new Scenario({
    name: c.name,
    steps: c.steps.map((content) => ({ role: "user" as const, content })),
    protocol: c.protocol,
    model,
    sessionId: sessionId(),
  });
  return runScenario(client, scenario);
}

/** Stream every user step through iterStream and fold each turn's chunks into
 * a ChatResponse, so the ordinary matchers can read it. */
async function runStreamed(
  client: MockAgentClient,
  c: ContractCase,
  model: string,
): Promise<ScenarioResult> {
  const sid = sessionId();
  const history: ChatMessage[] = [];
  const responses: ChatResponse[] = [];
  for (const step of c.steps) {
    history.push({ role: "user", content: step });
    let text = "";
    let finishReason = "";
    let finished = false;
    for await (const chunk of client.iterStream(history.slice(), {
      protocol: c.protocol,
      model,
      sessionId: sid,
    })) {
      text += chunk.text;
      if (chunk.finished) {
        finishReason = chunk.finishReason;
        finished = true;
      }
    }
    if (!finished) throw new Error(`stream for step ${JSON.stringify(step)} ended without a finished chunk`);
    const response: ChatResponse = {
      content: text,
      model,
      toolCalls: [],
      finishReason,
      raw: {},
      statusCode: 200,
      latencyMs: 0,
    };
    responses.push(response);
    history.push({ role: "assistant", content: text });
  }
  const last = responses[responses.length - 1];
  return {
    scenarioName: c.name,
    responses,
    totalLatencyMs: 0,
    get lastContent() {
      return last?.content ?? "";
    },
    get last() {
      if (!last) throw new Error("scenario produced no responses");
      return last;
    },
    get toolCalls() {
      return responses.flatMap((r) => r.toolCalls);
    },
  };
}

async function check(
  a: ContractAssertion,
  result: ScenarioResult,
  nonstream: () => Promise<string>,
): Promise<void> {
  const args = a.args ?? {};
  switch (a.kind) {
    case "response_contains":
      expect(result).toHaveResponseContaining(args.text as string);
      return;
    case "tool_call":
      expect(result).toHaveToolCall(
        args.name as string,
        args.arguments as Record<string, unknown> | undefined,
      );
      return;
    case "tool_call_count":
      expect(result).toHaveToolCallCount(args.count as number, args.name as string | undefined);
      return;
    case "tool_call_sequence":
      expect(result).toHaveToolCallSequence(args.names as string[]);
      return;
    case "finish_reason":
      expect(result).toHaveFinishReason(args.reason as string);
      return;
    case "status_code":
      expect(result).toHaveStatusCode(args.code as number);
      return;
    case "stream_text_matches_nonstream": {
      const streamed = result.last.content;
      const plain = await nonstream();
      if (streamed !== plain) {
        throw new AssertionError(
          `streamed text ${JSON.stringify(streamed)} !== non-streamed text ${JSON.stringify(plain)}`,
        );
      }
      return;
    }
    default:
      throw new Error(`unknown assertion kind ${JSON.stringify(a.kind)}`);
  }
}

describe.skipIf(!BASE_URL)("cross-SDK contract (sdk/contract/cases.json)", () => {
  const multiTenant = TENANT_KEY !== "";
  for (const c of suite.cases) {
    const note = divergence(c);
    const wrongLeg = (c.auth !== undefined) !== multiTenant;
    const test = note || wrongLeg ? it.skip : it;
    test(c.name, async () => {
      const model = c.model ?? suite.default_model;
      let result: ScenarioResult | undefined;
      let error: unknown;
      try {
        result = c.stream
          ? await runStreamed(clientFor(c), c, model)
          : await runPlain(clientFor(c), c, model);
      } catch (err) {
        error = err; // the SDK threw: every verdict is "error"
      }

      let plainText: string | undefined;
      const nonstream = async (): Promise<string> => {
        if (plainText === undefined) {
          plainText = (await runPlain(clientFor(c), c, model)).last.content;
        }
        return plainText;
      };

      const mismatches: string[] = [];
      for (const [i, a] of c.assertions.entries()) {
        let verdict: Verdict;
        let detail = "";
        if (error !== undefined || result === undefined) {
          verdict = "error";
          detail = String(error);
        } else {
          try {
            await check(a, result, nonstream);
            verdict = "pass";
          } catch (err) {
            if (!(err instanceof AssertionError)) throw err;
            verdict = "fail";
            detail = err.message;
          }
        }
        if (verdict !== a.expect) {
          mismatches.push(
            `#${i} ${a.kind} ${JSON.stringify(a.args ?? {})}: expected ${a.expect}, got ${verdict} (${detail})`,
          );
        }
      }
      vitestExpect(mismatches, `${c.name} [${SDK}]`).toEqual([]);
    });
  }
});
