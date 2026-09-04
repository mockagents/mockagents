// UX-07: administration safety.
//
// This component stands between an operator and an action that cannot be
// undone, so the tests are written as the things it must never allow: firing
// without confirmation, firing on a near-miss name, or being the first thing
// the keyboard lands on.
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { DangerConfirm } from "@/app/admin/DangerConfirm";

function setup(props: Partial<React.ComponentProps<typeof DangerConfirm>> = {}) {
  const action = vi.fn(async (_formData: FormData) => {});
  const utils = render(
    <DangerConfirm
      action={action}
      fields={{ id: "t-1" }}
      triggerLabel="Delete"
      triggerTitle="Delete tenant acme-prod"
      title="Delete tenant acme-prod?"
      impact={<>This revokes its 3 API keys immediately.</>}
      consequences={[
        "Every API key in this tenant stops working immediately.",
        "Agent definitions on disk are NOT deleted.",
      ]}
      confirmPhrase="acme-prod"
      confirmPhraseLabel="Type the exact tenant name to confirm"
      confirmLabel="Delete tenant"
      {...props}
    />,
  );
  return { action, ...utils };
}

describe("arming", () => {
  it("does nothing until the dialog is opened", () => {
    setup();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the destructive action disabled until the phrase matches exactly", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /delete/i }));

    const confirm = screen.getByRole("button", { name: "Delete tenant" });
    expect(confirm).toBeDisabled();

    await user.type(screen.getByLabelText(/type the exact tenant name/i), "acme-prod");
    expect(confirm).toBeEnabled();
  });

  // A trimmed or case-folded comparison would accept a name the operator did
  // not actually read. These are the near-misses that matter.
  it.each(["acme-pro", "acme-prod ", "Acme-Prod", " acme-prod"])(
    "refuses the near-miss %o",
    async (typed) => {
      const user = userEvent.setup();
      setup();
      await user.click(screen.getByRole("button", { name: /^delete$/i }));
      await user.type(screen.getByLabelText(/type the exact tenant name/i), typed);
      expect(screen.getByRole("button", { name: "Delete tenant" })).toBeDisabled();
    },
  );

  it("submits the action with its hidden fields once armed", async () => {
    const user = userEvent.setup();
    const { action } = setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await user.type(screen.getByLabelText(/type the exact tenant name/i), "acme-prod");
    await user.click(screen.getByRole("button", { name: "Delete tenant" }));

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    const formData = action.mock.calls[0][0];
    expect(formData.get("id")).toBe("t-1");
  });

  // Some actions (rotating your own key) have no name worth typing; they still
  // must not fire from a single click on the row.
  it("still requires the dialog when no phrase is configured", async () => {
    const user = userEvent.setup();
    const { action } = setup({ confirmPhrase: undefined });
    expect(action).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    expect(screen.getByRole("button", { name: "Delete tenant" })).toBeEnabled();
    expect(action).not.toHaveBeenCalled();
  });
});

describe("what the operator is told", () => {
  it("states the impact and every irreversible consequence", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));

    expect(screen.getByText(/revokes its 3 API keys immediately/i)).toBeInTheDocument();
    expect(screen.getByText(/Every API key in this tenant stops working/i)).toBeInTheDocument();
    expect(screen.getByText(/Agent definitions on disk are NOT deleted/i)).toBeInTheDocument();
    expect(screen.getByText(/Irreversible\./i)).toBeInTheDocument();
  });

  it("names the specific thing being destroyed in the heading", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    expect(screen.getByRole("dialog", { name: "Delete tenant acme-prod?" })).toBeInTheDocument();
  });
});

describe("keyboard and focus", () => {
  // The destructive button must never be where focus lands, or an operator
  // who opens the dialog and hits Enter has already done the thing.
  it("does not focus the destructive button on open", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await waitFor(() =>
      expect(document.activeElement).not.toBe(screen.getByRole("button", { name: "Delete tenant" })),
    );
    expect(document.activeElement).toBe(screen.getByLabelText(/type the exact tenant name/i));
  });

  it("focuses Cancel when there is no phrase to type", async () => {
    const user = userEvent.setup();
    setup({ confirmPhrase: undefined });
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" })),
    );
  });

  it("cancels on Escape without invoking the action", async () => {
    const user = userEvent.setup();
    const { action } = setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(action).not.toHaveBeenCalled();
  });

  // Re-opening must not inherit the previous attempt's typing, or a cancelled
  // deletion leaves the next one pre-armed.
  it("clears a typed phrase when the dialog is dismissed", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await user.type(screen.getByLabelText(/type the exact tenant name/i), "acme-prod");
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    expect(screen.getByLabelText(/type the exact tenant name/i)).toHaveValue("");
    expect(screen.getByRole("button", { name: "Delete tenant" })).toBeDisabled();
  });

  it("keeps Tab inside the dialog", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    const dialog = screen.getByRole("dialog");

    // Walk past the end of the dialog's focusable elements; focus must wrap
    // rather than escape to the page behind the overlay.
    for (let i = 0; i < 6; i++) await user.tab();
    expect(dialog.contains(document.activeElement)).toBe(true);
  });
});

describe("accessibility", () => {
  it("has no axe violations while open", async () => {
    const user = userEvent.setup();
    const { container } = setup();
    await user.click(screen.getByRole("button", { name: /^delete$/i }));
    await expect(container).toHaveNoAxeViolations();
  });
});
