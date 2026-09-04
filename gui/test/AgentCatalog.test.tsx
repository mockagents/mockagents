// UX-08 seed suite: proves the component harness can drive a real client
// island through interaction, failure and accessibility paths.
//
// AgentCatalog is a good canary — it owns search, filtering, a destructive
// action and an inline error surface, which are the four shapes every later
// Release A screen reuses.
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AgentCatalog, protoShort, type DeleteAction } from "@/app/AgentCatalog";
import type { AgentSummary } from "@/lib/api";

// Fixture shaped exactly like GET /api/v1/agents rows — no invented fields.
const AGENTS: AgentSummary[] = [
  {
    name: "support-bot",
    description: "Tier-1 customer support triage",
    model: "gpt-4o",
    protocol: "openai-chat",
    scenario_count: 4,
    tool_count: 2,
    tags: ["support", "triage"],
  },
  {
    name: "claude-router",
    description: "Routes to the right specialist",
    model: "claude-sonnet-4",
    protocol: "anthropic-messages",
    scenario_count: 7,
    tool_count: 0,
    tags: ["routing"],
  },
];

const okDelete: DeleteAction = async () => ({ ok: true, message: "deleted" });

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("protoShort", () => {
  it("shortens the long wire protocol values", () => {
    expect(protoShort("openai-chat")).toBe("openai");
    expect(protoShort("anthropic-messages")).toBe("anthropic");
  });

  it("passes an unrecognised protocol through unchanged", () => {
    // Never guess at a protocol the server added after this build shipped.
    expect(protoShort("gemini-generate")).toBe("gemini-generate");
  });
});

describe("AgentCatalog", () => {
  it("renders every agent with the counts the API actually returned", () => {
    render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);

    expect(screen.getByRole("link", { name: "support-bot" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "claude-router" })).toBeInTheDocument();
    expect(screen.getByText("2 of 2")).toBeInTheDocument();
  });

  it("filters by free-text search across name, model and tags", async () => {
    const user = userEvent.setup();
    render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);

    await user.type(screen.getByPlaceholderText(/search agents/i), "routing");

    expect(screen.getByRole("link", { name: "claude-router" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "support-bot" })).not.toBeInTheDocument();
    expect(screen.getByText("1 of 2")).toBeInTheDocument();
  });

  it("filters by protocol and reflects the choice in aria-pressed", async () => {
    const user = userEvent.setup();
    render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);

    const anthropic = screen.getByRole("button", { name: "anthropic" });
    await user.click(anthropic);

    expect(anthropic).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "all protocols" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(screen.queryByRole("link", { name: "support-bot" })).not.toBeInTheDocument();
  });

  it("shows an explicit empty state rather than a blank grid", async () => {
    const user = userEvent.setup();
    render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);

    await user.type(screen.getByPlaceholderText(/search agents/i), "no-such-agent");

    // Epic §5: a filtered-to-zero result is its own state, not an error and
    // not a disconnected server.
    expect(screen.getByText("No agents match this filter.")).toBeInTheDocument();
  });

  describe("delete", () => {
    it("does not call the server when the confirmation is dismissed", async () => {
      const user = userEvent.setup();
      const deleteAction = vi.fn(okDelete);
      vi.stubGlobal("confirm", vi.fn(() => false));

      render(<AgentCatalog agents={AGENTS} deleteAction={deleteAction} />);
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));

      expect(deleteAction).not.toHaveBeenCalled();
    });

    it("calls the server action with the agent name once confirmed", async () => {
      const user = userEvent.setup();
      const deleteAction = vi.fn(okDelete);
      vi.stubGlobal("confirm", vi.fn(() => true));

      render(<AgentCatalog agents={AGENTS} deleteAction={deleteAction} />);
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));

      await waitFor(() => expect(deleteAction).toHaveBeenCalledWith("support-bot"));
    });

    // Failure path: a denied or failed mutation must surface the server's
    // reason in the UI, not vanish. This is the shape UX-01 relies on to show
    // a viewer why an edit was refused.
    it("surfaces a failed delete inline and keeps the card mounted", async () => {
      const user = userEvent.setup();
      vi.stubGlobal("confirm", vi.fn(() => true));
      const deleteAction: DeleteAction = async () => ({
        ok: false,
        message: "403: editor role required",
      });

      render(<AgentCatalog agents={AGENTS} deleteAction={deleteAction} />);
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));

      expect(await screen.findByText("403: editor role required")).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "support-bot" })).toBeInTheDocument();
    });

    it("clears a previous error when the action is retried", async () => {
      const user = userEvent.setup();
      vi.stubGlobal("confirm", vi.fn(() => true));
      let attempt = 0;
      const deleteAction: DeleteAction = async () => {
        attempt += 1;
        return attempt === 1 ? { ok: false, message: "transient failure" } : { ok: true, message: "" };
      };

      render(<AgentCatalog agents={AGENTS} deleteAction={deleteAction} />);
      const button = screen.getByRole("button", { name: "Delete support-bot" });

      await user.click(button);
      expect(await screen.findByText("transient failure")).toBeInTheDocument();

      await user.click(button);
      await waitFor(() => expect(screen.queryByText("transient failure")).not.toBeInTheDocument());
    });
  });

  describe("accessibility", () => {
    it("has no axe violations in its default state", async () => {
      const { container } = render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);
      await expect(container).toHaveNoAxeViolations();
    });

    it("has no axe violations in its empty state", async () => {
      const user = userEvent.setup();
      const { container } = render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);
      await user.type(screen.getByPlaceholderText(/search agents/i), "zzz");

      await expect(container).toHaveNoAxeViolations();
    });

    it("is fully operable from the keyboard", async () => {
      const user = userEvent.setup();
      render(<AgentCatalog agents={AGENTS} deleteAction={okDelete} />);

      // Epic §10 requires keyboard-only completion of core workflows; the
      // filter controls must therefore be reachable and activatable by Tab
      // and Enter alone.
      await user.tab();
      expect(screen.getByPlaceholderText(/search agents/i)).toHaveFocus();

      await user.tab();
      const allProtocols = screen.getByRole("button", { name: "all protocols" });
      expect(allProtocols).toHaveFocus();

      await user.tab();
      await user.keyboard("{Enter}");
      expect(screen.getByRole("button", { name: "anthropic" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    });
  });
});
