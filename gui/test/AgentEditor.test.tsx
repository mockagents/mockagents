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

  // U3-7: what is loaded, where it lives, and whether the draft has moved —
  // beside the title rather than in a hint below the fold.
  describe("header chips", () => {
    it("names the loaded revision and where the definition lives", () => {
      setup({ persistence: "file", file: "support-bot.yaml" });
      // Scoped to the header: the card below also names the file, and the point
      // of this fix is that the facts are visible WITHOUT scrolling to it.
      const header = screen.getByRole("heading", { name: /Edit support-bot/ }).parentElement!;
      expect(within(header).getByText("rev-1")).toBeInTheDocument();
      expect(within(header).getByText("persisted")).toBeInTheDocument();
      expect(within(header).getByText("support-bot.yaml")).toBeInTheDocument();
    });

    it("shows nothing rather than guessing when the server did not say", () => {
      setup();
      for (const label of ["persisted", "runtime-only", "file missing"]) {
        expect(screen.queryByText(label)).toBeNull();
      }
    });

    it("marks the draft as unsaved once it differs from the server's copy", async () => {
      const user = userEvent.setup();
      setup();
      expect(screen.queryByText(/draft · unsaved/)).toBeNull();
      await editDraft(user, "changed: true");
      expect(screen.getByText(/draft · unsaved/)).toBeInTheDocument();
    });
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

      // U3-1: a receipt, not a toast. The revision matters most — it is what
      // the next conditional write is based on and what an audit entry names.
      expect(await screen.findByText(/Applied and persisted/i)).toBeInTheDocument();
      expect(screen.getByText("Save receipt")).toBeInTheDocument();
      expect(screen.getByText(/written to disk/i)).toBeInTheDocument();

      // The revision is named as its own field, not buried in prose: it is the
      // handle on what is now running and on what the next write is based on.
      expect(screen.getByText("new revision").nextElementSibling).toHaveTextContent("rev-2");
      // And the editor really did advance to it — a follow-up edit is
      // conditional on the new revision, not the stale one.
      expect(screen.getByText(/Loaded at revision/)).toHaveTextContent("rev-2");
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

      // Live and durable are different states, and only one survives a
      // restart. The receipt must not let the first imply the second.
      expect(await screen.findByText(/NOT written to disk/i)).toBeInTheDocument();
      expect(screen.getByText(/lost when this server restarts/i)).toBeInTheDocument();
      expect(screen.getByText("runtime-only")).toBeInTheDocument();
      expect(screen.queryByText(/Applied and persisted/i)).not.toBeInTheDocument();
      // Runtime-only is a qualified success: it is announced, not left to be
      // noticed, because acting on it as if it were durable loses work.
      expect(screen.getByText(/NOT written to disk/i).closest("[role=alert]")).not.toBeNull();
    });

    it("advances the revision so a follow-up edit stays conditional", async () => {
      const user = userEvent.setup();
      const props = setup();

      await editDraft(user, "first");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());
      await screen.findByText(/Applied and persisted/i);

      await user.type(editor(), "-second");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
      await user.click(applyButton());

      await waitFor(() =>
        expect(props.saveAction).toHaveBeenLastCalledWith("first-second", "rev-2"),
      );
    });
  });

  // U3-3. The editor writes to the LIVE runtime — there is no isolated preview
  // in Release A — so the pipelines that depend on this agent are the blast
  // radius, and they belong next to the diff rather than after the fact.
  describe("what an apply will change", () => {
    async function review(user: ReturnType<typeof userEvent.setup>) {
      await editDraft(user, "changed: true");
      await user.click(reviewButton());
      await waitFor(() => expect(applyButton()).toBeEnabled());
    }

    it("names the pipelines whose next run would use the change", async () => {
      const user = userEvent.setup();
      setup({ referencingPipelines: ["support-triage", "research-pipeline"] });
      await review(user);

      expect(screen.getByText(/Applying changes the active runtime/i)).toBeInTheDocument();
      expect(screen.getByText("support-triage")).toBeInTheDocument();
      expect(screen.getByText("research-pipeline")).toBeInTheDocument();
      expect(screen.getByText(/no isolated preview/i)).toBeInTheDocument();
    });

    it("says nothing depends on it only when that was actually established", async () => {
      const user = userEvent.setup();
      setup({ referencingPipelines: [] });
      await review(user);
      expect(screen.getByText(/No registered pipeline references this agent/i)).toBeInTheDocument();
    });

    // An empty list and an unreadable list are different facts, and only one of
    // them means "nothing depends on this".
    it("reports an unreadable pipeline inventory as unknown, not as none", async () => {
      const user = userEvent.setup();
      setup({ referencingPipelines: [], pipelinesReadable: false });
      await review(user);

      expect(screen.getByText(/unknown/i)).toBeInTheDocument();
      expect(screen.queryByText(/No registered pipeline references this agent/i)).toBeNull();
    });
  });

  // U3-5. Offline is not a permission problem, and it is not a defect in the
  // document being edited. Before this, both write controls stayed enabled and
  // an Apply against a stopped server came back as "Outcome unknown" — alarming
  // language for a request that never left the machine.
  describe("offline", () => {
    it("disables the controls that need the server, and says why", () => {
      setup({ online: false });

      expect(screen.getByRole("button", { name: /validate/i })).toBeDisabled();
      expect(reviewButton()).toBeDisabled();
      expect(applyButton()).toBeDisabled();
      expect(screen.getByRole("alert")).toHaveTextContent(/Server unreachable/i);
    });

    it("keeps the draft editable and exportable while the server is down", async () => {
      const user = userEvent.setup();
      setup({ online: false });

      // Editing still works — the draft is the user's, not the server's.
      await editDraft(user, "written-while-offline: true");
      expect(editor()).toHaveValue("written-while-offline: true");
      expect(screen.getByRole("button", { name: /export draft/i })).toBeEnabled();
    });

    it("does not also claim a role problem", () => {
      // Two banners blaming different things for one symptom sends the
      // operator looking for a permission that is not the issue.
      setup({ online: false });
      expect(screen.queryByText(/Read-only for your role/i)).toBeNull();
    });
  });

  // U3-4. Handoff §8 keeps a durable draft store out of Release A, so the
  // browser really is the only copy — which makes export part of the honesty
  // story rather than a convenience.
  describe("export draft", () => {
    it("is offered even to a viewer who cannot apply", () => {
      setup({ canWrite: false });
      expect(screen.getByRole("button", { name: /export draft/i })).toBeEnabled();
      expect(applyButton()).toBeDisabled();
    });

    it("says the draft lives only in this browser session", () => {
      setup();
      expect(screen.getByText(/in this browser session\s*only/i)).toBeInTheDocument();
      expect(screen.getByText(/Durable drafts are not part of Release A/i)).toBeInTheDocument();
    });

    it("reports a blocked download instead of appearing to have saved", async () => {
      // jsdom has no createObjectURL, so this exercises the real failure path.
      const user = userEvent.setup();
      setup();
      await user.click(screen.getByRole("button", { name: /export draft/i }));
      expect(await screen.findByText(/Could not start the download/i)).toBeInTheDocument();
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

    // U3-2. "Something changed" is not actionable. Which revision the server
    // holds and which one the draft was based on is what lets an operator
    // decide whether to reload, overwrite, or walk away.
    it("names both revisions, so the conflict can actually be reasoned about", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting });

      await applyIntoConflict(user);
      const alert = screen.getByRole("alert");
      expect(within(alert).getByText("on the server now").nextElementSibling).toHaveTextContent(
        "rev-9",
      );
      expect(
        within(alert).getByText("your draft is based on").nextElementSibling,
      ).toHaveTextContent("rev-1");
    });

    it("reports an unnamed server revision as unknown, not as unchanged", async () => {
      const user = userEvent.setup();
      setup({
        saveAction: vi.fn(
          async (): Promise<ConditionalSaveResult> => ({
            status: "conflict",
            message: "agent changed since it was loaded",
            currentRevision: "",
          }),
        ),
      });

      await applyIntoConflict(user);
      const alert = screen.getByRole("alert");
      expect(within(alert).getByText("on the server now").nextElementSibling).toHaveTextContent(
        "unknown",
      );
      expect(alert).toHaveTextContent(/unknown — not unchanged/i);
    });

    // Binding disclosure (handoff §8). The conditional route is additive, so it
    // constrains this editor and other conditional writers — not every writer
    // that exists. Presenting it as a lock would promise safety the server
    // does not provide.
    it("discloses that the precondition is not a lock", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting });

      await applyIntoConflict(user);
      const alert = screen.getByRole("alert");
      expect(alert).toHaveTextContent(/precondition, not a lock/i);
      expect(alert).toHaveTextContent(/can still overwrite this agent/i);
    });

    it("says both versions are preserved, because they are", async () => {
      const user = userEvent.setup();
      setup({ saveAction: conflicting });

      await applyIntoConflict(user);
      expect(screen.getByRole("alert")).toHaveTextContent(/Both versions are preserved/i);
      expect(screen.getByRole("alert")).toHaveTextContent(/nothing is merged for you/i);
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
