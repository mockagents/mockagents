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

const noopDelete: DeleteAction = () => {};

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
    render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);

    expect(screen.getByRole("link", { name: "support-bot" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "claude-router" })).toBeInTheDocument();
    expect(screen.getByText("2 of 2")).toBeInTheDocument();
  });

  it("filters by free-text search across name, model and tags", async () => {
    const user = userEvent.setup();
    render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);

    await user.type(screen.getByPlaceholderText(/search agents/i), "routing");

    expect(screen.getByRole("link", { name: "claude-router" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "support-bot" })).not.toBeInTheDocument();
    expect(screen.getByText("1 of 2")).toBeInTheDocument();
  });

  it("filters by protocol and reflects the choice in aria-pressed", async () => {
    const user = userEvent.setup();
    render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);

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
    render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);

    await user.type(screen.getByPlaceholderText(/search agents/i), "no-such-agent");

    // Epic §5: a filtered-to-zero result is its own state, not an error and
    // not a disconnected server.
    expect(screen.getByText("No agents match this filter.")).toBeInTheDocument();
  });

  // The dialog's own mechanics — focus trap, Esc, exact-phrase arming — are
  // covered in DangerConfirm.test.tsx. What matters here is the WIRING: that
  // the catalog uses it at all, gates on the agent's own name, and tells the
  // truth about what deleting this particular agent does.
  describe("delete", () => {
    async function openDialog() {
      const user = userEvent.setup();
      const deleteAction = vi.fn<DeleteAction>(() => {});
      render(<AgentCatalog agents={AGENTS} deleteAction={deleteAction} />);
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));
      return { user, deleteAction };
    }

    it("asks before it acts, instead of deleting on a single click", async () => {
      // The regression this replaces: a native window.confirm whose default
      // button is focused, so a stray Enter deleted a mock outright.
      const { deleteAction } = await openDialog();
      expect(screen.getByRole("dialog")).toBeInTheDocument();
      expect(deleteAction).not.toHaveBeenCalled();
    });

    it("gates the destructive button on the agent's exact name", async () => {
      const { user, deleteAction } = await openDialog();
      const confirm = screen.getByRole("button", { name: "Delete this agent" });
      expect(confirm).toBeDisabled();

      // A near miss is still a miss — typing the name is what proves the
      // operator read which mock they are about to destroy.
      await user.type(screen.getByLabelText(/type the exact agent name/i), "Support-Bot");
      expect(confirm).toBeDisabled();
      expect(deleteAction).not.toHaveBeenCalled();
    });

    it("submits the agent name once the phrase matches", async () => {
      const { user, deleteAction } = await openDialog();
      await user.type(screen.getByLabelText(/type the exact agent name/i), "support-bot");
      await user.click(screen.getByRole("button", { name: "Delete this agent" }));

      await waitFor(() => expect(deleteAction).toHaveBeenCalledTimes(1));
      const formData = deleteAction.mock.calls[0][0] as FormData;
      expect(formData.get("name")).toBe("support-bot");
    });

    it("states consequences the server actually produces", async () => {
      await openDialog();
      const dialog = screen.getByRole("dialog");
      // Each of these is a real effect of DELETE /api/v1/agents/{name}: the
      // registry entry goes, the backing file goes with it, pipeline nodes
      // referencing it fail, and the logs keep the now-dangling name.
      expect(dialog).toHaveTextContent(/stops serving immediately/i);
      expect(dialog).toHaveTextContent(/that file is deleted from the agents directory/i);
      expect(dialog).toHaveTextContent(/pipeline with a node referencing it fails/i);
      expect(dialog).toHaveTextContent(/logs are kept/i);
    });

    it("warns about the single-agent misroute only when it applies", async () => {
      const user = userEvent.setup();
      const { unmount } = render(
        <AgentCatalog agents={AGENTS} deleteAction={noopDelete} singleAgentFallback={false} />,
      );
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));
      expect(screen.getByRole("dialog")).not.toHaveTextContent(/silent misroute/i);
      unmount();

      // Two agents on an anonymous server: deleting one leaves a survivor that
      // the engine then answers EVERY request with, including requests naming
      // the agent that was deleted. That is worth saying before the fact.
      render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} singleAgentFallback />);
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));
      expect(screen.getByRole("dialog")).toHaveTextContent(/silent misroute/i);
    });

    it("disables the control and names the floor when the server says no", async () => {
      const user = userEvent.setup();
      render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} canWrite={false} />);

      const button = screen.getByRole("button", { name: "Delete support-bot" });
      // Visible and disabled, not hidden: "this console cannot delete agents"
      // and "my key cannot" are different facts.
      expect(button).toBeDisabled();
      expect(button).toHaveAttribute("title", expect.stringMatching(/needs the editor role/i));

      await user.click(button);
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  describe("accessibility", () => {
    it("has no axe violations in its default state", async () => {
      const { container } = render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);
      await expect(container).toHaveNoAxeViolations();
    });

    it("has no axe violations with the confirmation open", async () => {
      // The dialog is where a keyboard user is trapped on purpose, so it is
      // the one state where a naming or role slip is most costly.
      const user = userEvent.setup();
      const { container } = render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);
      await user.click(screen.getByRole("button", { name: "Delete support-bot" }));
      await expect(container).toHaveNoAxeViolations();
    });

    it("has no axe violations in its empty state", async () => {
      const user = userEvent.setup();
      const { container } = render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);
      await user.type(screen.getByPlaceholderText(/search agents/i), "zzz");

      await expect(container).toHaveNoAxeViolations();
    });

    it("is fully operable from the keyboard", async () => {
      const user = userEvent.setup();
      render(<AgentCatalog agents={AGENTS} deleteAction={noopDelete} />);

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
