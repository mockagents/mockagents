// UX-05: explicit pipeline execution.
//
// The assertions here are mostly about what the panel must NOT claim: that a
// run was isolated, that a parallel node list is a timeline, that a lost
// response means failure, or that a null node response is an empty answer.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RunPanel, type RunPanelProps } from "@/app/pipelines/[name]/RunPanel";
import type { PipelineAgent, PipelineNodeResult, PipelineRunOutcome } from "@/lib/api";
import { formatDuration, nsToMs } from "@/lib/duration";

const MS = 1_000_000; // one millisecond in nanoseconds

function node(id: string, extra: Partial<PipelineNodeResult> = {}): PipelineNodeResult {
  return {
    node_id: id,
    agent_name: `${id}-agent`,
    response: { content: `output of ${id}`, scenario_name: "default" },
    latency: 12 * MS,
    ...extra,
  };
}

/** The nodes a definition declares. The panel diffs this against what a run
 * reports, so every fixture needs one. */
const DECLARED: PipelineAgent[] = [
  { id: "a", ref: "a-agent" },
  { id: "b", ref: "b-agent" },
];

const OK: PipelineRunOutcome = {
  status: "ok",
  result: {
    pipeline_name: "research",
    topology: "sequential",
    nodes: [node("a"), node("b")],
    latency: 40 * MS,
  },
};

function setup(overrides: Partial<RunPanelProps> = {}) {
  const props: RunPanelProps = {
    pipelineName: "research",
    topology: "sequential",
    nodes: DECLARED,
    canRun: true,
    runAction: vi.fn(async () => OK),
    newSessionId: () => "sess-fixed",
    ...overrides,
  };
  render(<RunPanel {...props} />);
  return props;
}

const runButton = () => screen.getByRole("button", { name: /run pipeline/i });

async function run(user: ReturnType<typeof userEvent.setup>, text = "hello") {
  await user.type(screen.getByLabelText("Input"), text);
  await user.click(runButton());
}

describe("duration conversion", () => {
  // Go marshals time.Duration as nanoseconds. Reading it as milliseconds would
  // under-report by a factor of a million.
  it("treats the wire value as nanoseconds", () => {
    expect(nsToMs(1_500_000)).toBe(1.5);
  });

  it("keeps a sub-millisecond run legible instead of showing 0ms", () => {
    expect(formatDuration(250_000)).toBe("250µs");
  });

  // The engine really does report latency: 0 for an in-memory call — Go's
  // clock cannot resolve it. "0µs" would assert a measurement of no elapsed
  // time; "<1µs" says only what is known.
  it("reports a below-resolution duration as <1µs, never 0µs", () => {
    expect(formatDuration(0)).toBe("<1µs");
    expect(formatDuration(400)).toBe("<1µs");
    expect(formatDuration(999)).toBe("<1µs");
    expect(formatDuration(1_000)).toBe("1µs");
  });

  it("formats milliseconds and seconds", () => {
    expect(formatDuration(12 * MS)).toBe("12ms");
    expect(formatDuration(2_500 * MS)).toBe("2.50s");
  });

  it("reports a nonsensical duration as unknown rather than as 0", () => {
    expect(formatDuration(Number.NaN)).toBe("unknown");
    expect(formatDuration(-1)).toBe("unknown");
  });
});

describe("RunPanel", () => {
  it("says the run is against active configuration before it is pressed", () => {
    setup();
    expect(screen.getByText(/Runs against active configuration/i)).toBeInTheDocument();
    expect(screen.getByText(/not an isolated preview/i)).toBeInTheDocument();
  });

  it("will not run without an input", () => {
    setup();
    expect(runButton()).toBeDisabled();
  });

  it("sends a fresh session id with the input", async () => {
    const user = userEvent.setup();
    const props = setup();
    await run(user, "analyse this");
    await waitFor(() => expect(props.runAction).toHaveBeenCalledWith("analyse this", "sess-fixed"));
  });

  it("mints a different session id for each run", async () => {
    const user = userEvent.setup();
    let n = 0;
    const props = setup({ newSessionId: () => `sess-${++n}` });

    await run(user, "one");
    await waitFor(() => expect(props.runAction).toHaveBeenCalledTimes(1));
    await user.click(runButton());
    await waitFor(() => expect(props.runAction).toHaveBeenCalledTimes(2));

    const sessions = (props.runAction as ReturnType<typeof vi.fn>).mock.calls.map((c) => c[1]);
    expect(new Set(sessions).size).toBe(2);
  });

  // A pipeline run is stateful; submitting twice would advance session state
  // twice.
  it("disables duplicate submission while a run is in flight", async () => {
    const user = userEvent.setup();
    let release: (o: PipelineRunOutcome) => void = () => {};
    const runAction = vi.fn(
      () => new Promise<PipelineRunOutcome>((resolve) => (release = resolve)),
    );
    setup({ runAction });

    await run(user, "hello");
    await waitFor(() => expect(runButton()).toBeDisabled());
    expect(screen.getByText(/Duplicate submission is disabled/i)).toBeInTheDocument();

    release(OK);
    await waitFor(() => expect(runButton()).toBeEnabled());
    expect(runAction).toHaveBeenCalledTimes(1);
  });

  describe("evidence", () => {
    it("records the input and session that were actually submitted", async () => {
      const user = userEvent.setup();
      setup();
      await run(user, "the submitted input");

      // The evidence list, not the textarea — both hold the same text, so the
      // assertion has to say which one it means.
      await screen.findByText(/Run completed/i);
      const shown = screen.getAllByText("the submitted input");
      expect(shown.some((el) => el.tagName === "DD")).toBe(true);
      expect(screen.getByText("sess-fixed")).toBeInTheDocument();
    });

    it("renders node results with converted latencies", async () => {
      const user = userEvent.setup();
      setup();
      await run(user);

      const table = await screen.findByRole("table");
      expect(within(table).getByText("a")).toBeInTheDocument();
      expect(within(table).getByText("b-agent")).toBeInTheDocument();
      // 12ms, not 12000000.
      expect(within(table).getAllByText("12ms").length).toBe(2);
      expect(screen.queryByText(/12000000/)).not.toBeInTheDocument();
    });

    // A null response is a distinct outcome, not an empty answer.
    it("marks a node that produced no response", async () => {
      const user = userEvent.setup();
      setup({
        runAction: vi.fn(async () => ({
          status: "ok" as const,
          result: {
            pipeline_name: "research",
            topology: "sequential",
            nodes: [node("a", { response: null })],
            latency: 5 * MS,
          },
        })),
      });
      await run(user);
      expect(await screen.findByText("no response")).toBeInTheDocument();
    });

    // A result that carries no node array at all. This used to render nothing
    // below the banner; it now accounts for the declared nodes as unreported.
    // Silence implies there is nothing to see, when in fact the server claimed
    // success while naming no nodes — which is the least accountable answer it
    // can give, and the one most worth surfacing.
    it("accounts for declared nodes when the result carries none", async () => {
      const user = userEvent.setup();
      setup({
        runAction: vi.fn(async () => ({
          status: "ok" as const,
          result: {
            pipeline_name: "research",
            topology: "sequential",
            nodes: null,
            latency: 1 * MS,
          },
        })),
      });
      await run(user);
      expect(await screen.findByText(/Run completed/i)).toBeInTheDocument();

      const table = within(screen.getByRole("table"));
      expect(table.getAllByText("not executed · unknown")).toHaveLength(2);
      expect(screen.getByText(/2 declared nodes produced no result/i)).toBeInTheDocument();
    });

    it("renders nothing below the banner when the definition declares no nodes", async () => {
      // Nothing declared and nothing returned: there is genuinely nothing to
      // account for, and an empty table would be noise.
      const user = userEvent.setup();
      setup({
        nodes: [],
        runAction: vi.fn(async () => ({
          status: "ok" as const,
          result: {
            pipeline_name: "research",
            topology: "sequential",
            nodes: null,
            latency: 1 * MS,
          },
        })),
      });
      await run(user);
      expect(await screen.findByText(/Run completed/i)).toBeInTheDocument();
      expect(screen.queryByRole("table")).not.toBeInTheDocument();
    });
  });

  describe("partial failure (422)", () => {
    const partial: PipelineRunOutcome = {
      status: "partial",
      error: 'node "b" missing agent ref',
      result: {
        pipeline_name: "research",
        topology: "sequential",
        nodes: [node("a")],
        latency: 20 * MS,
      },
    };

    it("keeps the nodes that did complete", async () => {
      const user = userEvent.setup();
      setup({ runAction: vi.fn(async () => partial) });
      await run(user);

      expect(await screen.findByText(/Partial run/i)).toBeInTheDocument();
      expect(screen.getByText('node "b" missing agent ref')).toBeInTheDocument();
      expect(within(screen.getByRole("table")).getByText("a")).toBeInTheDocument();
      // Those nodes really ran — their effects are not undone.
      expect(screen.getByText(/effects stand/i)).toBeInTheDocument();
    });

    it("says nothing is known when a 422 carries no result", async () => {
      const user = userEvent.setup();
      setup({
        runAction: vi.fn(async () => ({
          status: "partial" as const,
          error: "boom",
          result: null,
        })),
      });
      await run(user);
      expect(await screen.findByText(/nothing is known about what ran/i)).toBeInTheDocument();
    });

    // U5-1: the design's central rule, on the screen it was written for. A
    // sequential run stops at the failing node, so every node after it is
    // simply missing from the response. Rendering only what came back shows a
    // truncated run as if the pipeline were shorter than it is.
    it("renders a declared node the run never reported as unknown", async () => {
      const user = userEvent.setup();
      setup({ runAction: vi.fn(async () => partial) });
      await run(user);

      const table = within(await screen.findByRole("table"));
      expect(table.getByText("not executed · unknown")).toBeInTheDocument();
      // Named, so it can be found in the definition — not just counted.
      expect(table.getByText("b")).toBeInTheDocument();
      expect(table.getByText("b-agent")).toBeInTheDocument();
    });

    it("does not guess why a node did not run", async () => {
      const user = userEvent.setup();
      setup({ runAction: vi.fn(async () => partial) });
      await run(user);

      // A graph edge condition and a run that stopped early are both "absent"
      // in the response, and the server does not distinguish them.
      expect(await screen.findByText(/reports what ran, never what did not/i)).toBeInTheDocument();
    });

    it("adds no unknown rows when every declared node reported", async () => {
      const user = userEvent.setup();
      setup(); // OK covers both declared nodes
      await run(user);

      await screen.findByRole("table");
      expect(screen.queryByText("not executed · unknown")).toBeNull();
      expect(screen.queryByText(/produced no result/i)).toBeNull();
    });
  });

  // U5-2 / epic §10 `blocked-missing-dependency`. A pipeline naming an agent
  // nobody loaded is fixed by loading a definition; a scenario that failed to
  // match is fixed by editing one. Collapsing them into one "partial" state
  // sends the operator to the wrong file.
  describe("blocked by a missing dependency", () => {
    const blocked: PipelineRunOutcome = {
      status: "blocked",
      error: 'pipeline "research" node "b": agent not found: "b-agent"',
      result: {
        pipeline_name: "research",
        topology: "sequential",
        nodes: [node("a"), node("b", { response: null })],
        latency: 20 * MS,
      },
    };

    it("names the cause and the remedy, not just the failure", async () => {
      const user = userEvent.setup();
      setup({ runAction: vi.fn(async () => blocked) });
      await run(user);

      expect(await screen.findByText(/agent that is not loaded/i)).toBeInTheDocument();
      expect(screen.getByText(/Load the missing definition/i)).toBeInTheDocument();
    });

    // The design prototype says "The run was not started." That is false here:
    // refs resolve at node-execution time, so a sequential pipeline has already
    // executed everything before the missing node and advanced its session
    // state. Telling an operator nothing happened would invite a blind re-run.
    it("does not claim the run never started when earlier nodes ran", async () => {
      const user = userEvent.setup();
      setup({ runAction: vi.fn(async () => blocked) });
      await run(user);

      expect(await screen.findByText(/The run did start/i)).toBeInTheDocument();
      expect(screen.getByText(/session state has advanced/i)).toBeInTheDocument();
      expect(screen.queryByText(/was not started/i)).toBeNull();
    });

    it("says so plainly when nothing ran before the missing node", async () => {
      const user = userEvent.setup();
      setup({
        runAction: vi.fn(async () => ({
          status: "blocked" as const,
          error: 'pipeline "research" node "a": agent not found: "a-agent"',
          result: {
            pipeline_name: "research",
            topology: "sequential",
            nodes: [node("a", { response: null })],
            latency: 1 * MS,
          },
        })),
      });
      await run(user);

      expect(await screen.findByText(/Nothing executed before it/i)).toBeInTheDocument();
    });

    it("is not reported as a partial run", async () => {
      const user = userEvent.setup();
      setup({ runAction: vi.fn(async () => blocked) });
      await run(user);

      await screen.findByText(/agent that is not loaded/i);
      expect(screen.queryByText(/Partial run/i)).toBeNull();
    });
  });

  describe("honesty", () => {
    // The server reports definition order, not completion times.
    it("warns that a parallel node list is not a timeline", async () => {
      const user = userEvent.setup();
      setup({
        topology: "parallel",
        runAction: vi.fn(async () => ({
          status: "ok" as const,
          result: {
            pipeline_name: "research",
            topology: "parallel",
            nodes: [node("a"), node("b")],
            latency: 30 * MS,
          },
        })),
      });
      await run(user);
      expect(await screen.findByText(/Definition order, not a timeline/i)).toBeInTheDocument();
    });

    it("does not warn about ordering for a sequential pipeline", async () => {
      const user = userEvent.setup();
      setup();
      await run(user);
      await screen.findByRole("table");
      expect(screen.queryByText(/not a timeline/i)).not.toBeInTheDocument();
    });

    it("does not claim the results are linked to interaction logs", async () => {
      const user = userEvent.setup();
      setup();
      await run(user);
      expect(await screen.findByText(/not linked to interaction-log/i)).toBeInTheDocument();
    });

    // The single most important one: a lost response is not a failure.
    it("reports a lost response as unknown and does not retry", async () => {
      const user = userEvent.setup();
      const runAction = vi.fn(async () => ({
        status: "unknown" as const,
        message: "The server could not be reached.",
      }));
      setup({ runAction });
      await run(user);

      expect(await screen.findByText(/Outcome unknown/i)).toBeInTheDocument();
      expect(screen.getByText(/not retried automatically/i)).toBeInTheDocument();
      expect(runAction).toHaveBeenCalledTimes(1);
      // Never presented as a completed or failed run.
      expect(screen.queryByText(/Run completed/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/Partial run/i)).not.toBeInTheDocument();
    });
  });

  describe("permissions", () => {
    it("disables running with a reason instead of failing on click", () => {
      setup({ canRun: false });
      expect(runButton()).toBeDisabled();
      expect(screen.getByLabelText("Input")).toBeDisabled();
      expect(screen.getByText(/refusing the action, not failing/i)).toBeInTheDocument();
    });

    it("explains a server-side denial", async () => {
      const user = userEvent.setup();
      setup({
        runAction: vi.fn(async () => ({
          status: "denied" as const,
          message: "You do not have permission to run pipelines on this server.",
        })),
      });
      await run(user);
      expect(await screen.findByText(/do not have permission/i)).toBeInTheDocument();
    });

    it("explains a server with execution disabled", async () => {
      const user = userEvent.setup();
      setup({
        runAction: vi.fn(async () => ({
          status: "unavailable" as const,
          message: "This server has pipeline execution disabled, so there is nothing to run.",
        })),
      });
      await run(user);
      expect(await screen.findByText(/execution disabled/i)).toBeInTheDocument();
    });
  });

  describe("accessibility", () => {
    it("has no axe violations before a run", async () => {
      const { container } = render(
        <RunPanel
          pipelineName="research"
          topology="sequential"
          nodes={DECLARED}
          canRun
          runAction={vi.fn(async () => OK)}
        />,
      );
      await expect(container).toHaveNoAxeViolations();
    });

    it("has no axe violations showing partial-failure evidence", async () => {
      const user = userEvent.setup();
      const { container } = render(
        <RunPanel
          pipelineName="research"
          topology="parallel"
          nodes={DECLARED}
          canRun
          newSessionId={() => "sess-a11y"}
          runAction={vi.fn(async () => ({
            status: "partial" as const,
            error: "boom",
            result: {
              pipeline_name: "research",
              topology: "parallel",
              nodes: [node("a"), node("b", { response: null })],
              latency: 9 * MS,
            },
          }))}
        />,
      );
      await user.type(within(container).getByLabelText("Input"), "hi");
      await user.click(within(container).getByRole("button", { name: /run pipeline/i }));
      await within(container).findByText(/Partial run/i);

      await expect(container).toHaveNoAxeViolations();
    });
  });
});
