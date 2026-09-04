// UX-03 slice B: the safety behaviours of the agent editor.
//
// These tests are mostly about what must NOT happen — the draft must not be
// lost, an unreviewed change must not be applicable, and a save must not be
// reported as durable when the server said it was not.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentEditor, type AgentEditorProps } from "@/app/agents/[name]/edit/AgentEditor";
import type { ConditionalSaveResult, ValidateResult } from "@/lib/api";

const ORIGINAL = [
  "apiVersion: mockagents/v1",
  "kind: Agent",
  "metadata:",
  "  name: support-bot",
  "spec:",
  "  model: gpt-4o",
  "  protocol: openai-chat-completions",
].join("\n");

const VALID: ValidateResult = { ok: true, kind: "Agent", errors: [] };

function setup(overrides: Partial<AgentEditorProps> = {}) {
  const props: AgentEditorProps = {
    name: "support-bot",
    original: ORIGINAL,
    revision: "rev-1",
    canWrite: true,
    validateAction: vi.fn(async () => VALID),
    saveAction: vi.fn(
      async (): Promise<ConditionalSaveResult> => ({
        status: "ok",
        created: false,
        persisted: true,
        file: "support-bot.yaml",
        revision: "rev-2",
      }),
    ),
    reloadAction: vi.fn(async () => ({ yaml: ORIGINAL, revision: "rev-1" })),
    ...overrides,
  };
  render(<AgentEditor {...props} />);
  return props;
}

const editor = () => screen.getByRole("textbox", { name: /YAML definition for support-bot/i });
const applyButton = () => screen.getByRole("button", { name: /apply/i });
const reviewButton = () => screen.getByRole("button", { name: /review changes/i });

// The editor opens on the guided Form tab; these tests drive the raw document,
// so they switch to YAML first. Both tabs edit the same text.
async function openYaml(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("tab", { name: "YAML" }));
}

async function editDraft(user: ReturnType<typeof userEvent.setup>, text: string) {
  await openYaml(user);
  await user.clear(editor());
  await user.type(editor(), text);
}

describe("AgentEditor", () => {
  it("loads the complete definition it was given", async () => {
    const user = userEvent.setup();
    setup();
    await openYaml(user);
    expect(editor()).toHaveValue(ORIGINAL);
  });

  it("opens on the guided form, with the YAML a tab away", () => {
    setup();
    expect(screen.getByRole("tab", { name: "Form" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "YAML" })).toHaveAttribute("aria-selected", "false");
    // The form is a view of the same document, so it shows its values.
    expect(screen.getByLabelText(/^Model/)).toHaveValue("gpt-4o");
  });

  it("switching tabs does not alter the document", async () => {
    const user = userEvent.setup();
    setup();
    await openYaml(user);
    expect(editor()).toHaveValue(ORIGINAL);
    await user.click(screen.getByRole("tab", { name: "Form" }));
    await openYaml(user);
    expect(editor()).toHaveValue(ORIGINAL);
    expect(screen.getByText("unchanged")).toBeInTheDocument();
  });

  it("says the document is unchanged until it is edited", () => {
    setup();
    expect(screen.getByText("unchanged")).toBeInTheDocument();
  });

  describe("preview before apply", () => {
    it("disables Apply until the change has been reviewed", async () => {
      const user = userEvent.setup();
      setup();

      // Nothing changed yet.
      expect(applyButton()).toBeDisabled();

      await editDraft(user, "changed: true");
      // Changed, but not reviewed.
      expect(applyButton()).toBeDisabled();

      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
    });

    it("shows the diff of what will be applied", async () => {
      const user = userEvent.setup();
      setup();

      await editDraft(user, "one\ntwo");
      await user.click(reviewButton());

      const panel = await screen.findByRole("table");
      expect(within(panel).getByText(/one/)).toBeInTheDocument();
      // The original content appears as a removal.
      expect(within(panel).getByText(/kind: Agent/)).toBeInTheDocument();
    });

    it("does not offer to apply an invalid document", async () => {
      const user = userEvent.setup();
      setup({
        validateAction: vi.fn(async () => ({
          ok: false,
          kind: "Agent",
          errors: [{ file: "", field: "spec.protocol", message: "unknown protocol", line: 6 }],
        })),
      });

      await editDraft(user, "broken: yes");
      await user.click(reviewButton());

      expect(await screen.findByText(/Invalid\./)).toBeInTheDocument();
      expect(applyButton()).toBeDisabled();
    });

    it("re-arms the review requirement after further edits", async () => {
      const user = userEvent.setup();
      setup();

      await editDraft(user, "first");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());

      // Typing again means what was reviewed is no longer what would be sent.
      await user.type(editor(), "-more");
      expect(applyButton()).toBeDisabled();
    });
  });

  describe("apply", () => {
    it("sends the loaded revision as the precondition", async () => {
      const user = userEvent.setup();
      const props = setup();

      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      await waitFor(() =>
        expect(props.saveAction).toHaveBeenCalledWith("changed: true", "rev-1"),
      );
    });

    it("reports a persisted save as durable", async () => {
      const user = userEvent.setup();
      setup();

      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      expect(await screen.findByText(/survives a restart/i)).toBeInTheDocument();
    });

    // The distinction the epic insists on: a runtime-only save is real, but a
    // restart loses it, and it must not read as an ordinary "saved".
    it("reports a runtime-only save as NOT durable", async () => {
      const user = userEvent.setup();
      setup({
        saveAction: vi.fn(
          async (): Promise<ConditionalSaveResult> => ({
            status: "ok",
            created: false,
            persisted: false,
            revision: "rev-2",
          }),
        ),
      });

      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      expect(await screen.findByText(/Runtime only/i)).toBeInTheDocument();
      expect(screen.getByText(/lost when it restarts/i)).toBeInTheDocument();
      expect(screen.queryByText(/survives a restart/i)).not.toBeInTheDocument();
    });

    it("advances the revision so a follow-up edit stays conditional", async () => {
      const user = userEvent.setup();
      const props = setup();

      await editDraft(user, "first");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());
      await screen.findByText(/Applied\./);

      await user.type(editor(), "-second");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      await waitFor(() =>
        expect(props.saveAction).toHaveBeenLastCalledWith("first-second", "rev-2"),
      );
    });
  });

  describe("conflict", () => {
    const conflicting = vi.fn(
      async (): Promise<ConditionalSaveResult> => ({
        status: "conflict",
        message: "agent changed since it was loaded",
        currentRevision: "rev-9",
      }),
    );

    async function applyIntoConflict(user: ReturnType<typeof userEvent.setup>) {
      await editDraft(user, "my-precious-draft: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());
      await screen.findByRole("alert");
    }

    // The single most important behaviour in this component.
    it("keeps the draft intact", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting });

      await applyIntoConflict(user);

      expect(editor()).toHaveValue("my-precious-draft: true");
      expect(screen.getByText(/Not applied/)).toBeInTheDocument();
    });

    it("announces the failure assertively rather than silently", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting });

      await applyIntoConflict(user);
      const alert = screen.getByRole("alert");
      expect(alert).toHaveTextContent(/changed since it was loaded/i);
    });

    it("offers comparison with the server's version without discarding work", async () => {
      const user = userEvent.setup();
      setup({
        saveAction: conflicting,
        reloadAction: vi.fn(async () => ({ yaml: "someone: else\nwrote: this", revision: "rev-9" })),
      });

      await applyIntoConflict(user);
      await user.click(screen.getByRole("button", { name: /compare with current/i }));

      // The draft survives the comparison...
      await waitFor(() => expect(editor()).toHaveValue("my-precious-draft: true"));
      // ...and the diff is now against what the server actually holds.
      const panel = await screen.findByRole("table");
      expect(within(panel).getByText(/someone: else/)).toBeInTheDocument();
    });

    it("discards the draft only when explicitly asked", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting });

      await applyIntoConflict(user);
      await user.click(screen.getByRole("button", { name: /discard my changes/i }));

      await waitFor(() => expect(editor()).toHaveValue(ORIGINAL));
    });

    it("explains a deleted agent instead of silently recreating it", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting, reloadAction: vi.fn(async () => null) });

      await applyIntoConflict(user);
      await user.click(screen.getByRole("button", { name: /compare with current/i }));

      expect(await screen.findByText(/no longer exists/i)).toBeInTheDocument();
      expect(editor()).toHaveValue("my-precious-draft: true");
    });
  });

  describe("failure paths", () => {
    it("reports a lost response as UNKNOWN and does not retry", async () => {
      const user = userEvent.setup();
      const saveAction = vi.fn(
        async (): Promise<ConditionalSaveResult> => ({
          status: "error",
          message: "The server could not be reached, so the outcome of this save is unknown.",
        }),
      );
      setup({ saveAction });

      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      expect(await screen.findByText(/Outcome unknown/i)).toBeInTheDocument();
      // An automatic retry of a write whose outcome is unknown could apply it
      // twice; the epic forbids it.
      expect(saveAction).toHaveBeenCalledTimes(1);
      expect(editor()).toHaveValue("changed: true");
    });

    it("surfaces unsupported-field errors with their line", async () => {
      const user = userEvent.setup();
      setup({
        saveAction: vi.fn(
          async (): Promise<ConditionalSaveResult> => ({
            status: "invalid",
            errors: [
              {
                file: "",
                field: "someFutureField",
                line: 7,
                message: 'unsupported field "someFutureField": this server does not recognise it',
                suggestion: "Remove the field, or check it against the agent schema.",
              },
            ],
          }),
        ),
      });

      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      expect(await screen.findByText(/unsupported field/i)).toBeInTheDocument();
      expect(screen.getByText("line 7")).toBeInTheDocument();
      expect(screen.getByText(/nothing on the\s+server changed/i)).toBeInTheDocument();
    });

    it("tells a denied user the service is working, not broken", async () => {
      const user = userEvent.setup();
      setup({
        saveAction: vi.fn(
          async (): Promise<ConditionalSaveResult> => ({
            status: "denied",
            message: "You do not have permission to change agents.",
          }),
        ),
      });

      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      expect(await screen.findByText(/do not have permission/i)).toBeInTheDocument();
    });
  });

  describe("viewer", () => {
    // A viewer must be able to tell "you may not do this" from "this is broken".
    it("disables Apply with an explanation but keeps editing usable", async () => {
      const user = userEvent.setup();
      setup({ canWrite: false });

      expect(screen.getByText(/Read-only for your role/i)).toBeInTheDocument();
      expect(screen.getByText(/Nothing is broken/i)).toBeInTheDocument();

      await editDraft(user, "still: editable");
      expect(editor()).toHaveValue("still: editable");

      await user.click(reviewButton());
      await waitFor(() => expect(screen.getByRole("table")).toBeInTheDocument());
      expect(applyButton()).toBeDisabled();
    });
  });

  describe("accessibility", () => {
    it("has no axe violations while editing", async () => {
      const { container } = render(
        <AgentEditor
          name="support-bot"
          original={ORIGINAL}
          revision="rev-1"
          canWrite
          validateAction={vi.fn(async () => VALID)}
          saveAction={vi.fn(async () => ({ status: "error", message: "x" }) as ConditionalSaveResult)}
          reloadAction={vi.fn(async () => null)}
        />,
      );
      await expect(container).toHaveNoAxeViolations();
    });

    it("has no axe violations in the conflict state", async () => {
      const user = userEvent.setup();
      const { container } = render(
        <AgentEditor
          name="support-bot"
          original={ORIGINAL}
          revision="rev-1"
          canWrite
          validateAction={vi.fn(async () => VALID)}
          saveAction={vi.fn(
            async () =>
              ({
                status: "conflict",
                message: "changed",
                currentRevision: "rev-9",
              }) as ConditionalSaveResult,
          )}
          reloadAction={vi.fn(async () => null)}
        />,
      );

      await user.click(within(container).getByRole("tab", { name: "YAML" }));
      const box = within(container).getByRole("textbox", { name: /YAML definition/i });
      await user.clear(box);
      await user.type(box, "x: 1");
      await user.click(within(container).getByRole("button", { name: /review changes/i }));
      await waitFor(() =>
        expect(within(container).getByRole("button", { name: /apply/i })).toBeEnabled(),
      );
      await user.click(within(container).getByRole("button", { name: /apply/i }));
      await within(container).findByRole("alert");

      await expect(container).toHaveNoAxeViolations();
    });
  });
});
