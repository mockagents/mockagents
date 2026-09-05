// U3-4: exporting a draft is the only way it survives a closed tab, so the
// naming and the failure path both matter more than they look.
import { afterEach, describe, expect, it, vi } from "vitest";

import { downloadText, draftFilename } from "@/lib/download";

const AT = new Date("2026-09-05T07:12:44Z");

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("draftFilename", () => {
  it("keeps a plain agent name readable", () => {
    expect(draftFilename("support-bot", AT)).toBe("support-bot-draft-2026-09-05T07-12-44.yaml");
  });

  it("strips separators, so the name cannot steer where the file lands", () => {
    // Agent names are permissive — tenant-prefixed and path-like forms reach
    // this function verbatim, and a separator here would reach the filesystem.
    const name = draftFilename(["..", "..", "elsewhere"].join("/"), AT);
    expect(name).not.toContain("/");
    expect(name).not.toContain("\\");
    expect(name.startsWith("elsewhere")).toBe(true);
  });

  it("does not produce a dotfile from a leading dot", () => {
    expect(draftFilename(".hidden", AT).startsWith(".")).toBe(false);
  });

  it("falls back rather than producing a nameless file", () => {
    expect(draftFilename("///", AT)).toBe("agent-draft-2026-09-05T07-12-44.yaml");
  });
});

describe("downloadText", () => {
  it("reports failure instead of appearing to succeed when the browser cannot", () => {
    // jsdom has no createObjectURL. A silent no-op here would let the UI claim
    // a draft was saved when nothing ever left the page.
    expect(downloadText("a.yaml", "kind: Agent")).toBe(false);
  });

  it("hands the browser a blob and cleans up the object URL afterwards", () => {
    const createObjectURL = vi.fn(() => "blob:fake");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
    vi.useFakeTimers();

    const clicked: string[] = [];
    const realCreate = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = realCreate(tag);
      if (tag === "a") el.click = () => clicked.push((el as HTMLAnchorElement).download);
      return el;
    });

    expect(downloadText("support-bot-draft.yaml", "kind: Agent")).toBe(true);
    expect(clicked).toEqual(["support-bot-draft.yaml"]);

    // Revoked on the next tick, not synchronously: revoking during the click
    // cancels the download in browsers that fetch the blob asynchronously.
    expect(revokeObjectURL).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:fake");
    vi.useRealTimers();
  });
});
