// UX-03: the guided form.
//
// Most of these assert what the form must NOT do. A form over a partly-modelled
// config is dangerous by default: the interesting property is that editing one
// field leaves every unmodelled field byte-identical.
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { GuidedForm } from "@/app/agents/[name]/edit/GuidedForm";

const RICH = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support-bot
  description: Tier-1 triage
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  chaos:
    preset: flaky
    rate: 0.25
  tools:
    - name: lookup_order
      parameters:
        type: object
  behavior:
    scenarios:
      - name: default
        response:
          content: I received your message.
      - name: refund
        response:
          content: Refund started.
`;

/** Render with a controlled document, so assertions can inspect the result of
 * an edit exactly as the editor would receive it. */
function setup(yaml = RICH, disabled = false) {
  const onChange = vi.fn();
  render(<GuidedForm yaml={yaml} onChange={onChange} disabled={disabled} />);
  return { onChange };
}

const lastDoc = (onChange: ReturnType<typeof vi.fn>): string =>
  onChange.mock.calls[onChange.mock.calls.length - 1][0];

describe("GuidedForm", () => {
  it("shows the values from the document", () => {
    setup();
    expect(screen.getByLabelText(/^Model/)).toHaveValue("gpt-4o");
    expect(screen.getByLabelText(/^Protocol/)).toHaveValue("openai-chat-completions");
    expect(screen.getByLabelText(/^Description/)).toHaveValue("Tier-1 triage");
  });

  it("enumerates the scenarios it found", () => {
    setup();
    expect(screen.getByLabelText(/Scenario 1 name/)).toHaveValue("default");
    expect(screen.getByLabelText(/Scenario 2 name/)).toHaveValue("refund");
  });

  it("keeps the agent name read-only", () => {
    setup();
    const name = screen.getByLabelText(/^Name/);
    expect(name).toHaveValue("support-bot");
    expect(name).toBeDisabled();
  });

  // The point of the whole design.
  describe("preservation", () => {
    it("editing one field leaves every other line untouched", async () => {
      const user = userEvent.setup();
      const { onChange } = setup();

      await user.type(screen.getByLabelText(/^Model/), "-mini");
      await waitFor(() => expect(onChange).toHaveBeenCalled());

      const before = RICH.split("\n");
      const after = lastDoc(onChange).split("\n");
      expect(after.length).toBe(before.length);
      before.forEach((line, i) => {
        if (line.includes("model:")) return;
        expect(after[i], `line ${i} must be untouched`).toBe(line);
      });
    });

    it("does not drop config the form never renders", async () => {
      const user = userEvent.setup();
      const { onChange } = setup();

      await user.type(screen.getByLabelText(/^Description/), " updated");
      await waitFor(() => expect(onChange).toHaveBeenCalled());

      const doc = lastDoc(onChange);
      for (const fragment of [
        "chaos:",
        "preset: flaky",
        "rate: 0.25",
        "tools:",
        "name: lookup_order",
        "type: object",
      ]) {
        expect(doc, `"${fragment}" must survive`).toContain(fragment);
      }
    });

    it("editing one scenario does not disturb the other", async () => {
      const user = userEvent.setup();
      const { onChange } = setup();

      await user.type(screen.getByLabelText(/^Scenario 1 response content/), "!");
      await waitFor(() => expect(onChange).toHaveBeenCalled());

      expect(lastDoc(onChange)).toContain("content: Refund started.");
    });
  });

  // A form that shows three of nine settings invites the belief that the other
  // six do not exist.
  describe("honesty about coverage", () => {
    it("names the spec sections it does not show", () => {
      setup();
      const note = screen.getByRole("note");
      expect(note).toHaveTextContent("spec.chaos");
      expect(note).toHaveTextContent("spec.tools");
      expect(note).toHaveTextContent(/preserved unchanged/);
    });

    // behavior is only PARTLY covered — the form renders scenarios and nothing
    // else under it. The example agents put chaos config exactly there.
    it("names uncovered keys nested under behavior, not just under spec", () => {
      setup(`metadata:
  name: bot
spec:
  model: gpt-4o
  behavior:
    chaos:
      enabled: true
    scenarios:
      - name: default
        response:
          content: hi
`);
      const note = screen.getByRole("note");
      expect(note).toHaveTextContent("spec.behavior.chaos");
      expect(note).not.toHaveTextContent("spec.behavior.scenarios");
    });

    it("omits the note when there is nothing uncovered", () => {
      setup(`apiVersion: mockagents/v1
kind: Agent
metadata:
  name: bot
spec:
  protocol: openai-chat-completions
  model: gpt-4o
`);
      expect(screen.queryByRole("note")).not.toBeInTheDocument();
    });

    it("marks a field that is absent rather than showing it as empty-and-set", () => {
      setup(`metadata:
  name: bot
spec:
  model: gpt-4o
`);
      // description, protocol and the scenarios are all absent here.
      expect(screen.getAllByText("not set").length).toBeGreaterThan(0);
      const description = screen.getByLabelText(/^Description/);
      expect(description).toHaveValue("");
    });
  });

  describe("refusing unsafe edits", () => {
    it("disables a block-scalar field and explains why", () => {
      setup(`metadata:
  name: bot
  description: |
    a multi-line
    description
spec:
  model: gpt-4o
`);
      expect(screen.getByLabelText(/^Description/)).toBeDisabled();
      expect(screen.getByText(/multi-line block/)).toBeInTheDocument();
      expect(screen.getAllByText(/Use the\s+YAML tab/i).length).toBeGreaterThan(0);
    });

    it("disables a field whose key is ambiguous", () => {
      setup(`metadata:
  name: bot
spec:
  model: a
  model: b
`);
      expect(screen.getByLabelText(/^Model/)).toBeDisabled();
      expect(screen.getByText(/more than once/)).toBeInTheDocument();
    });

    it("explains when scenarios cannot be read instead of showing none silently", () => {
      setup(`metadata:
  name: bot
spec:
  model: gpt-4o
`);
      expect(screen.getByText(/No scenarios found/)).toBeInTheDocument();
    });
  });

  describe("viewer", () => {
    it("disables every editable control but still shows the values", () => {
      setup(RICH, true);
      expect(screen.getByLabelText(/^Model/)).toBeDisabled();
      expect(screen.getByLabelText(/^Description/)).toBeDisabled();
      expect(screen.getByLabelText(/^Model/)).toHaveValue("gpt-4o");
    });
  });

  describe("accessibility", () => {
    it("has no axe violations", async () => {
      const { container } = render(<GuidedForm yaml={RICH} onChange={vi.fn()} />);
      await expect(container).toHaveNoAxeViolations();
    });

    it("labels every control and links its path hint", () => {
      setup();
      // Each input is reachable by its label, and describes which YAML path it
      // writes — so a screen-reader user knows what a control actually edits.
      const model = screen.getByLabelText(/^Model/);
      const describedBy = model.getAttribute("aria-describedby");
      expect(describedBy).toBeTruthy();
      expect(document.getElementById(describedBy!)).toHaveTextContent("spec.model");
    });
  });
});
