// UX-04: the request/session explorer.
//
// The assertions worth having here are about honesty — what the screen claims
// about the completeness of what it is showing — and about not losing the
// operator's place.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LogsConsole, type LogsConsoleProps } from "@/app/logs/LogsConsole";
import type { InteractionLog, LogWindow } from "@/lib/api";
import { DEFAULT_FILTERS } from "@/lib/logfeed";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: (...args: unknown[]) => push(...args) }),
}));

function row(id: number, extra: Partial<InteractionLog> = {}): InteractionLog {
  return {
    id,
    timestamp: "2026-09-03T10:00:00Z",
    agent_name: "support-bot",
    protocol: "openai-chat-completions",
    request: null,
    response: null,
    latency_ms: 12,
    response_status: 200,
    session_id: `sess-${id}`,
    ...extra,
  };
}

function makeWindow(rows: InteractionLog[], extra: Partial<LogWindow> = {}): LogWindow {
  return { rows, limit: 100, offset: 0, mayHaveMore: false, stable: true, ...extra };
}

function setup(overrides: Partial<LogsConsoleProps> = {}) {
  const props: LogsConsoleProps = {
    window: makeWindow([row(3), row(2), row(1)]),
    agents: ["support-bot", "other-bot"],
    filters: DEFAULT_FILTERS,
    recoverAction: vi.fn(async () => []),
    ...overrides,
  };
  render(<LogsConsole {...props} />);
  return props;
}

beforeEach(() => {
  push.mockClear();
});

describe("LogsConsole", () => {
  it("lists the rows it was given", () => {
    setup();
    expect(screen.getByRole("button", { name: "3" })).toBeInTheDocument();
    expect(screen.getByText("3 rows")).toBeInTheDocument();
  });

  describe("fields the console never used to surface", () => {
    it("shows the session, and lets it become a filter", async () => {
      const user = userEvent.setup();
      setup();

      await user.click(screen.getAllByRole("button", { name: /sess-3/ })[0]);
      expect(push).toHaveBeenCalledWith(expect.stringContaining("session_id=sess-3"));
    });

    it("flags an injected fault distinctly from a real error", async () => {
      const user = userEvent.setup();
      setup({
        window: makeWindow([
          row(1, { chaos_action: "error", chaos_source: "agent", chaos_seed: 42, chaos_rate: 0.1 }),
        ]),
      });

      expect(screen.getByText("chaos:error")).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "1" }));
      expect(screen.getByText(/Fault injected: error/)).toBeInTheDocument();
      expect(screen.getByText(/shaped by/)).toBeInTheDocument();
      expect(screen.getByText("42")).toBeInTheDocument();
    });

    it("says when a captured body is clipped", async () => {
      const user = userEvent.setup();
      setup({ window: makeWindow([row(1, { truncated: true })]) });

      expect(screen.getByText("truncated")).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "1" }));
      expect(screen.getByText(/not the complete payload/)).toBeInTheDocument();
    });

    it("surfaces an engine error", async () => {
      const user = userEvent.setup();
      setup({ window: makeWindow([row(1, { error: "upstream exploded" })]) });
      await user.click(screen.getByRole("button", { name: "1" }));
      expect(screen.getByText("upstream exploded")).toBeInTheDocument();
    });
  });

  describe("bodies", () => {
    // Metadata-first: a body can contain anything a client sent.
    it("hides bodies until explicitly revealed", async () => {
      const user = userEvent.setup();
      setup({ window: makeWindow([row(1, { request_body: "SECRET-PAYLOAD" })]) });
      await user.click(screen.getByRole("button", { name: "1" }));

      expect(screen.queryByText("SECRET-PAYLOAD")).not.toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: /show request\/response bodies/i }));
      expect(screen.getByText("SECRET-PAYLOAD")).toBeInTheDocument();
    });
  });

  describe("honesty about the window", () => {
    it("says the window is bounded rather than implying a complete history", () => {
      setup({ window: makeWindow([row(1)], { mayHaveMore: true, limit: 100 }) });
      expect(screen.getByText(/showing the 100 most recent/i)).toBeInTheDocument();
    });

    it("does not claim a total it cannot know", () => {
      setup({ window: makeWindow([row(1)], { mayHaveMore: true }) });
      expect(screen.queryByText(/\bof \d+\b/)).not.toBeInTheDocument();
    });

    // Offset paging is not a cursor, and the UI must say so.
    it("warns that a paged view can shift", () => {
      setup({
        window: makeWindow([row(1)], { stable: false, offset: 100 }),
        filters: { ...DEFAULT_FILTERS, offset: 100 },
      });
      expect(screen.getByText(/rows can shift/i)).toBeInTheDocument();
      expect(screen.getByText(/not by a\s+stable cursor/i)).toBeInTheDocument();
    });

    it("distinguishes a filtered empty result from no traffic", () => {
      setup({
        window: makeWindow([]),
        filters: { ...DEFAULT_FILTERS, session_id: "nope" },
      });
      expect(screen.getByText(/No interactions match this filter/i)).toBeInTheDocument();
    });

    it("says an unfiltered empty store is empty, not filtered", () => {
      setup({ window: makeWindow([]) });
      expect(screen.getByText(/No interactions recorded yet/i)).toBeInTheDocument();
    });
  });

  describe("filters", () => {
    it("puts filters in the URL so the view is shareable", async () => {
      const user = userEvent.setup();
      setup();

      await user.selectOptions(screen.getByLabelText("Agent"), "other-bot");
      await user.type(screen.getByLabelText("Session"), "sess-42");
      await user.click(screen.getByRole("button", { name: /apply/i }));

      await waitFor(() => expect(push).toHaveBeenCalled());
      const url = String(push.mock.calls[0][0]);
      expect(url).toContain("agent=other-bot");
      expect(url).toContain("session_id=sess-42");
    });

    it("resets paging when a filter changes", async () => {
      const user = userEvent.setup();
      setup({ filters: { ...DEFAULT_FILTERS, offset: 200 } });

      await user.selectOptions(screen.getByLabelText("Agent"), "other-bot");
      await user.click(screen.getByRole("button", { name: /apply/i }));

      await waitFor(() => expect(push).toHaveBeenCalled());
      // Keeping offset=200 across a filter change would point at unrelated rows.
      expect(String(push.mock.calls[0][0])).not.toContain("offset=");
    });

    it("offers a way back to the unfiltered view", async () => {
      const user = userEvent.setup();
      setup({ filters: { ...DEFAULT_FILTERS, session_id: "sess-1" } });

      await user.click(screen.getByRole("button", { name: /clear/i }));
      expect(push).toHaveBeenCalledWith("/logs");
    });
  });

  describe("selection", () => {
    it("keeps the operator's selection when they pick a row", async () => {
      const user = userEvent.setup();
      setup();

      await user.click(screen.getByRole("button", { name: "2" }));
      expect(screen.getByRole("button", { name: "2" })).toHaveAttribute("aria-pressed", "true");
      // The detail pane follows the selection, not the newest row.
      const detail = screen.getByText("request 2");
      expect(detail).toBeInTheDocument();
    });

    it("selects rows from the keyboard", async () => {
      const user = userEvent.setup();
      setup();

      const target = screen.getByRole("button", { name: "2" });
      target.focus();
      await user.keyboard("{Enter}");
      expect(target).toHaveAttribute("aria-pressed", "true");
    });
  });

  describe("accessibility", () => {
    it("has no axe violations", async () => {
      const { container } = render(
        <LogsConsole
          window={makeWindow([row(2, { chaos_action: "latency", truncated: true }), row(1)])}
          agents={["support-bot"]}
          filters={DEFAULT_FILTERS}
          recoverAction={vi.fn(async () => [])}
        />,
      );
      await expect(container).toHaveNoAxeViolations();
    });

    it("labels every filter control", () => {
      setup();
      for (const label of ["Agent", "Session", "Since", "Until"]) {
        expect(screen.getByLabelText(label)).toBeInTheDocument();
      }
    });

    it("conveys status with text, not colour alone", () => {
      setup({ window: makeWindow([row(1, { response_status: 503 })]) });
      const table = screen.getByRole("table");
      expect(within(table).getByText("503")).toBeInTheDocument();
    });
  });
});
