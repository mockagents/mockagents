// UX-04: the request/session explorer.
//
// The assertions worth having here are about honesty — what the screen claims
// about the completeness of what it is showing — and about not losing the
// operator's place.
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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

/** Minimal EventSource stand-in. jsdom has none, and the live path — dedup,
 * recovery, the gap verdict — is the part of this screen most likely to break
 * silently, so it needs to be drivable from a test. */
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  private listeners = new Map<string, Set<(e: unknown) => void>>();
  closed = false;

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener(type: string, fn: (e: unknown) => void) {
    const set = this.listeners.get(type) ?? new Set();
    set.add(fn);
    this.listeners.set(type, set);
  }
  removeEventListener() {}
  close() {
    this.closed = true;
  }
  emit(type: string, data?: unknown) {
    for (const fn of this.listeners.get(type) ?? []) {
      fn(data === undefined ? {} : { data: JSON.stringify(data) });
    }
  }
}

beforeEach(() => {
  push.mockClear();
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
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
      expect(screen.getByText(/Widen the filter/i)).toBeInTheDocument();
      expect(screen.getByText(/session nope/)).toBeInTheDocument();
    });

    // U4-3. "No rows" is the most ambiguous thing a log view can show, so this
    // branch — which is only reached when the store ANSWERED — says which of
    // the three it is.
    it("names the empty store as reachable, not unreachable or unauthorized", () => {
      setup({ window: makeWindow([]) });
      expect(screen.getByText(/reachable and empty/i)).toBeInTheDocument();
      expect(screen.getByText(/unauthorized \(403\)/i)).toBeInTheDocument();
    });

    it("states the window an empty result covers", () => {
      setup({ window: makeWindow([]) });
      // This API has no default period: unfiltered means the newest N of
      // everything. Implying a time window that does not exist would let
      // "empty" be read as "empty for the period I care about".
      expect(screen.getByText(/the newest 100, no time filter/i)).toBeInTheDocument();
    });

    it("states a time-filtered window in the operator's own terms", () => {
      setup({
        window: makeWindow([]),
        filters: { ...DEFAULT_FILTERS, since: "2026-09-01T00:00:00Z" },
      });
      expect(screen.getByText(/2026-09-01T00:00:00Z → now/)).toBeInTheDocument();
    });

    it("offers a next step rather than dead-ending", () => {
      setup({ window: makeWindow([]) });
      expect(screen.getByText(/Send a request to any mocked endpoint/i)).toBeInTheDocument();
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

  // U4-1 / U4-2. The gap machinery was already correct — a full recovery page
  // means "cannot prove we caught up" — but it was reported in a banner above
  // the table, which scrolls away from the rows it describes.
  describe("an unresolved gap in the feed", () => {
    /** Turn on the live feed and force a reconnect whose recovery hits its
     * bound. The component treats a second `open` as a reconnect, which is the
     * condition under test; a real EventSource reaches it by dropping first. */
    async function reconnectWithFullRecovery(rows: InteractionLog[]) {
      const user = userEvent.setup();
      // A FULL page (RECOVERY_LIMIT) is what makes the gap unresolved.
      const recovered = Array.from({ length: 200 }, (_, i) => row(1000 + i));
      setup({ window: makeWindow(rows), recoverAction: vi.fn(async () => recovered) });

      await user.click(screen.getByRole("switch", { name: /live/i }));
      const es = FakeEventSource.instances[0];
      expect(es).toBeDefined();
      await act(async () => {
        es.emit("open");
        es.emit("open");
      });
      return { recovered };
    }

    it("marks the break in the table, between the rows it separates", async () => {
      await reconnectWithFullRecovery([row(5), row(4), row(3)]);

      const gapRow = await screen.findByText(/Unresolved gap/i);
      const tr = gapRow.closest("tr");
      expect(tr).not.toBeNull();

      // Placed at the boundary: the recovered rows are above it, and the last
      // row seen before the drop is immediately below.
      const previous = tr!.previousElementSibling;
      const next = tr!.nextElementSibling;
      expect(previous?.textContent).toContain("1000");
      expect(next?.textContent).toContain("5");
    });

    it("states the range it could not account for", async () => {
      await reconnectWithFullRecovery([row(5), row(4), row(3)]);
      // Both ends are known locally, so both are shown — a range is what tells
      // an operator whether the hole overlaps the incident they are chasing.
      const tr = (await screen.findByText(/Unresolved gap/i)).closest("tr");
      expect(tr!.textContent).toMatch(/\d\d:\d\d:\d\dZ – \d\d:\d\d:\d\dZ/);
    });

    it("still says so in the banner, for anyone not looking at the table", async () => {
      await reconnectWithFullRecovery([row(5), row(4), row(3)]);
      const banner = await screen.findByText(/this view is incomplete/i);
      expect(banner.closest("[role=alert]")).not.toBeNull();
    });

    it("does not mark a gap when recovery proved it caught up", async () => {
      const user = userEvent.setup();
      // A SHORT page means the feed is continuous again.
      setup({ recoverAction: vi.fn(async () => [row(4)]) });
      await user.click(screen.getByRole("switch", { name: /live/i }));
      const es = FakeEventSource.instances[0];
      await act(async () => {
        es.emit("open");
        es.emit("open");
      });

      expect(await screen.findByText(/Reconnected and caught up/i)).toBeInTheDocument();
      expect(screen.queryByText(/Unresolved gap/i)).toBeNull();
    });

    it("has no axe violations while a gap is shown", async () => {
      await reconnectWithFullRecovery([row(5), row(4), row(3)]);
      await screen.findByText(/Unresolved gap/i);
      await expect(document.body).toHaveNoAxeViolations();
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
