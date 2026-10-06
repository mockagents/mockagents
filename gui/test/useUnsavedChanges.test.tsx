// The editors' navigation guard: unsaved work must not vanish on a reload, a
// tab close, or a click on a sidebar link.
import { fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { UNSAVED_CHANGES_MESSAGE, useUnsavedChangesGuard } from "@/lib/useUnsavedChanges";

function beforeUnload(): Event {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event;
}

function Harness({ dirty }: { dirty: boolean }) {
  useUnsavedChangesGuard(dirty);
  return (
    <div>
      {/* Plain anchors on purpose: <Link> renders exactly this element, and the
          guard works on the DOM click, not on Next's router. */}
      {/* eslint-disable-next-line @next/next/no-html-link-for-pages */}
      <a href="/logs">in-app link</a>
      <a href="#section">fragment link</a>
      <a href="/logs" target="_blank">new tab link</a>
      <a href="https://example.com/docs">external link</a>
      <a href="/download.yaml" download>
        download link
      </a>
    </div>
  );
}

/** Click a link and report whether the guard stopped it. */
function click(name: string, init: MouseEventInit = {}): boolean {
  const link = screen.getByText(name);
  // jsdom does not navigate, so default-prevented is the observable outcome.
  return !fireEvent.click(link, { button: 0, ...init });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useUnsavedChangesGuard: beforeunload", () => {
  it("does nothing while clean", () => {
    renderHook(() => useUnsavedChangesGuard(false));
    expect(beforeUnload().defaultPrevented).toBe(false);
  });

  it("asks the browser to confirm while dirty", () => {
    renderHook(() => useUnsavedChangesGuard(true));
    expect(beforeUnload().defaultPrevented).toBe(true);
  });

  it("stops asking once the work is saved", () => {
    const { rerender } = renderHook(({ dirty }) => useUnsavedChangesGuard(dirty), {
      initialProps: { dirty: true },
    });
    expect(beforeUnload().defaultPrevented).toBe(true);
    rerender({ dirty: false });
    expect(beforeUnload().defaultPrevented).toBe(false);
  });

  it("removes its listeners on unmount", () => {
    const { unmount } = renderHook(() => useUnsavedChangesGuard(true));
    unmount();
    expect(beforeUnload().defaultPrevented).toBe(false);
  });
});

describe("useUnsavedChangesGuard: in-app links", () => {
  it("lets links through without asking while clean", () => {
    const confirm = vi.spyOn(window, "confirm");
    render(<Harness dirty={false} />);
    expect(click("in-app link")).toBe(false);
    expect(confirm).not.toHaveBeenCalled();
  });

  it("blocks an in-app navigation the user declines", () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<Harness dirty />);
    expect(click("in-app link")).toBe(true);
    expect(confirm).toHaveBeenCalledWith(UNSAVED_CHANGES_MESSAGE);
  });

  it("lets an in-app navigation through when the user confirms", () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<Harness dirty />);
    expect(click("in-app link")).toBe(false);
  });

  it("does not ask for links that leave this page open or are covered by beforeunload", () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<Harness dirty />);
    expect(click("fragment link")).toBe(false);
    expect(click("new tab link")).toBe(false);
    expect(click("external link")).toBe(false);
    expect(click("download link")).toBe(false);
    expect(click("in-app link", { ctrlKey: true })).toBe(false);
    expect(confirm).not.toHaveBeenCalled();
  });
});
