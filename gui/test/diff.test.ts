import { describe, expect, it } from "vitest";

import { MAX_DIFF_LINES, collapseUnchanged, diffLines } from "@/lib/diff";

describe("diffLines", () => {
  it("reports no changes for identical documents", () => {
    const d = diffLines("a\nb\nc", "a\nb\nc");
    expect(d.added).toBe(0);
    expect(d.removed).toBe(0);
    expect(d.lines.every((l) => l.kind === "same")).toBe(true);
  });

  it("detects a changed line as one removal plus one addition", () => {
    const d = diffLines("a\nb\nc", "a\nB\nc");
    expect(d.added).toBe(1);
    expect(d.removed).toBe(1);
    expect(d.lines.find((l) => l.kind === "del")?.text).toBe("b");
    expect(d.lines.find((l) => l.kind === "add")?.text).toBe("B");
  });

  it("detects a pure insertion without reporting a removal", () => {
    const d = diffLines("a\nc", "a\nb\nc");
    expect(d.added).toBe(1);
    expect(d.removed).toBe(0);
  });

  it("detects a pure deletion", () => {
    const d = diffLines("a\nb\nc", "a\nc");
    expect(d.added).toBe(0);
    expect(d.removed).toBe(1);
  });

  it("keeps line numbers so a reviewer can locate a change", () => {
    const d = diffLines("a\nb\nc", "a\nB\nc");
    const same = d.lines.filter((l) => l.kind === "same");
    expect(same[0]).toMatchObject({ leftNo: 1, rightNo: 1 });
    // The trailing unchanged line is line 3 on both sides.
    expect(same[same.length - 1]).toMatchObject({ leftNo: 3, rightNo: 3 });
  });

  it("handles an empty original (a brand-new document)", () => {
    const d = diffLines("", "a\nb");
    expect(d.removed).toBeLessThanOrEqual(1); // the single empty line
    expect(d.added).toBeGreaterThan(0);
  });

  it("refuses to diff an oversized document instead of hanging", () => {
    const huge = new Array(MAX_DIFF_LINES + 1).fill("x").join("\n");
    const d = diffLines(huge, huge + "\nmore");
    expect(d.tooLarge).toBe(true);
    expect(d.lines).toEqual([]);
  });

  // Realistic case: the YAML the editor actually round-trips.
  it("finds a single-field change in an agent document", () => {
    const before = ["spec:", "  model: gpt-4o", "  protocol: openai-chat-completions"].join("\n");
    const after = ["spec:", "  model: gpt-4o-mini", "  protocol: openai-chat-completions"].join("\n");
    const d = diffLines(before, after);
    expect(d.added).toBe(1);
    expect(d.removed).toBe(1);
    expect(d.lines.find((l) => l.kind === "add")?.text).toContain("gpt-4o-mini");
  });
});

describe("collapseUnchanged", () => {
  it("elides long unchanged runs but keeps context around a change", () => {
    const before = new Array(40).fill(0).map((_, i) => `line ${i}`).join("\n");
    const after = before.replace("line 20", "line TWENTY");

    const rows = collapseUnchanged(diffLines(before, after).lines, 2);
    const kept = rows.filter((r) => r !== null);
    const elisions = rows.filter((r) => r === null);

    expect(elisions.length).toBeGreaterThan(0);
    expect(kept.length).toBeLessThan(41);
    // The change itself is never elided.
    expect(kept.some((r) => r!.text.includes("TWENTY"))).toBe(true);
    expect(kept.some((r) => r!.text === "line 20")).toBe(true);
  });

  it("keeps everything when the document is entirely changed", () => {
    const rows = collapseUnchanged(diffLines("a\nb", "x\ny").lines, 2);
    expect(rows.every((r) => r !== null)).toBe(true);
  });
});
