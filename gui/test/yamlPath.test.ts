import { describe, expect, it } from "vitest";

import { countItems, encodeScalar, readScalar, writeScalar } from "@/lib/yamlPath";

// A document deliberately richer than anything the form renders: chaos,
// streaming, tool JSON-schema, comments, Unicode. The point of every
// preservation test below is that editing one field leaves all of this alone.
const RICH = `# A hand-written agent, with comments the form must not eat.
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support-bot
  description: Tier-1 triage
  tags:
    - support
    - triage
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  chaos:
    preset: flaky
    rate: 0.25
  streaming:
    enabled: true
    chunk_size: 8
  tools:
    - name: lookup_order
      description: Look up an order
      parameters:
        type: object
        properties:
          order_id:
            type: string
            description: "The order's id"
        required:
          - order_id
  behavior:
    scenarios:
      - name: default
        response:
          content: I received your message.
      - name: refund
        response:
          content: Refund started.
`;

function unchangedExcept(before: string, after: string, changedLines: number[]): void {
  const a = before.split("\n");
  const b = after.split("\n");
  expect(b.length).toBe(a.length);
  a.forEach((line, i) => {
    if (changedLines.includes(i)) return;
    expect(b[i], `line ${i} must be untouched`).toBe(line);
  });
}

describe("readScalar", () => {
  it("reads a top-level scalar", () => {
    expect(readScalar(RICH, "kind")).toEqual({ ok: true, value: "Agent" });
  });

  it("reads a nested scalar", () => {
    expect(readScalar(RICH, "spec.model")).toEqual({ ok: true, value: "gpt-4o" });
    expect(readScalar(RICH, "metadata.description")).toEqual({ ok: true, value: "Tier-1 triage" });
  });

  it("reads deeply nested scalars, including inside a tool schema", () => {
    expect(readScalar(RICH, "spec.chaos.preset")).toEqual({ ok: true, value: "flaky" });
    expect(
      readScalar(RICH, "spec.tools.0.parameters.properties.order_id.type"),
    ).toEqual({ ok: true, value: "string" });
  });

  it("reads a quoted value without its quotes", () => {
    expect(
      readScalar(RICH, "spec.tools.0.parameters.properties.order_id.description"),
    ).toEqual({ ok: true, value: "The order's id" });
  });

  it("reads a scalar from a list item's dash line", () => {
    expect(readScalar(RICH, "spec.behavior.scenarios.0.name")).toEqual({
      ok: true,
      value: "default",
    });
    expect(readScalar(RICH, "spec.behavior.scenarios.1.name")).toEqual({
      ok: true,
      value: "refund",
    });
  });

  it("reads a scalar nested inside a list item", () => {
    expect(readScalar(RICH, "spec.behavior.scenarios.1.response.content")).toEqual({
      ok: true,
      value: "Refund started.",
    });
  });

  it("reports an absent key as null rather than an error", () => {
    expect(readScalar(RICH, "metadata.nonexistent")).toEqual({ ok: true, value: null });
  });
});

describe("writeScalar preserves everything it does not own", () => {
  it("changes exactly one line for a nested scalar", () => {
    const r = writeScalar(RICH, "spec.model", "gpt-4o-mini");
    expect(r.ok).toBe(true);
    if (!r.ok) return;

    const modelLine = RICH.split("\n").findIndex((l) => l.includes("model: gpt-4o"));
    unchangedExcept(RICH, r.value, [modelLine]);
    expect(r.value.split("\n")[modelLine]).toBe("  model: gpt-4o-mini");
  });

  // The whole reason this module exists.
  it("keeps chaos, streaming and tool schemas that the form never renders", () => {
    const r = writeScalar(RICH, "spec.model", "claude-sonnet-4");
    expect(r.ok).toBe(true);
    if (!r.ok) return;

    for (const fragment of [
      "chaos:",
      "preset: flaky",
      "rate: 0.25",
      "streaming:",
      "chunk_size: 8",
      "tools:",
      "name: lookup_order",
      "order_id:",
      "required:",
    ]) {
      expect(r.value, `"${fragment}" must survive an unrelated edit`).toContain(fragment);
    }
  });

  it("keeps comments", () => {
    const r = writeScalar(RICH, "spec.model", "x");
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.value).toContain("# A hand-written agent");
  });

  it("keeps a trailing comment on the edited line itself", () => {
    const doc = "spec:\n  model: gpt-4o # the default\n";
    const r = writeScalar(doc, "spec.model", "gpt-4o-mini");
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.value).toBe("spec:\n  model: gpt-4o-mini # the default\n");
  });

  it("edits a scalar inside a list item without touching its siblings", () => {
    const r = writeScalar(RICH, "spec.behavior.scenarios.1.response.content", "Refund complete.");
    expect(r.ok).toBe(true);
    if (!r.ok) return;

    expect(r.value).toContain("Refund complete.");
    // The other scenario is untouched.
    expect(r.value).toContain("content: I received your message.");
    expect(r.value).toContain("name: default");
  });

  it("edits a key that lives on the dash line", () => {
    const r = writeScalar(RICH, "spec.behavior.scenarios.0.name", "fallback");
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value).toContain("- name: fallback");
    expect(r.value).toContain("- name: refund");
  });
});

describe("writeScalar round-trips", () => {
  it("what is written is what is read back", () => {
    const cases = [
      ["spec.model", "gpt-4o-mini"],
      ["metadata.description", "A new description"],
      ["spec.behavior.scenarios.0.response.content", "Something else entirely"],
    ] as const;

    for (const [path, value] of cases) {
      const w = writeScalar(RICH, path, value);
      expect(w.ok, `${path} should be writable`).toBe(true);
      if (!w.ok) continue;
      expect(readScalar(w.value, path)).toEqual({ ok: true, value });
    }
  });

  // §8.2 names Unicode explicitly.
  it("round-trips Unicode, emoji and CJK", () => {
    for (const value of ["café ☕", "日本語のテキスト", "emoji 🎉 mid-string", "Ünïcödé —dash"]) {
      const w = writeScalar(RICH, "metadata.description", value);
      expect(w.ok).toBe(true);
      if (!w.ok) continue;
      expect(readScalar(w.value, "metadata.description")).toEqual({ ok: true, value });
    }
  });

  it("round-trips values that would otherwise be misread as YAML", () => {
    for (const value of ["true", "false", "null", "12345", "3.14", "yes", "no", "~", "on"]) {
      const w = writeScalar(RICH, "metadata.description", value);
      expect(w.ok).toBe(true);
      if (!w.ok) continue;
      expect(readScalar(w.value, "metadata.description")).toEqual({ ok: true, value });
    }
  });

  it("round-trips values containing YAML punctuation", () => {
    for (const value of ["key: value", "a # not a comment", "- leading dash", "{braces}", "[brackets]"]) {
      const w = writeScalar(RICH, "metadata.description", value);
      expect(w.ok).toBe(true);
      if (!w.ok) continue;
      expect(readScalar(w.value, "metadata.description")).toEqual({ ok: true, value });
    }
  });

  it("round-trips an empty value", () => {
    const w = writeScalar(RICH, "metadata.description", "");
    expect(w.ok).toBe(true);
    if (w.ok) expect(readScalar(w.value, "metadata.description")).toEqual({ ok: true, value: "" });
  });
});

describe("writeScalar inserts a missing key", () => {
  it("adds a scalar under an existing parent at the right indent", () => {
    const doc = "metadata:\n  name: bot\nspec:\n  model: gpt-4o\n";
    const r = writeScalar(doc, "metadata.description", "Added");
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value).toContain("  description: Added");
    expect(readScalar(r.value, "metadata.description")).toEqual({ ok: true, value: "Added" });
    // The rest is intact.
    expect(r.value).toContain("  name: bot");
    expect(r.value).toContain("  model: gpt-4o");
  });

  it("refuses when the parent does not exist rather than inventing structure", () => {
    const doc = "metadata:\n  name: bot\n";
    const r = writeScalar(doc, "spec.chaos.preset", "flaky");
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.reason).toMatch(/not present/);
  });
});

describe("writeScalar refuses what it cannot do safely", () => {
  it("refuses a block scalar", () => {
    const doc = "spec:\n  description: |\n    line one\n    line two\n";
    const read = readScalar(doc, "spec.description");
    expect(read.ok).toBe(false);
    if (!read.ok) expect(read.reason).toMatch(/multi-line/);

    const write = writeScalar(doc, "spec.description", "x");
    expect(write.ok).toBe(false);
  });

  it("refuses inline flow syntax", () => {
    const doc = "metadata: { name: bot, description: hi }\n";
    const r = writeScalar(doc, "metadata.description", "x");
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.reason).toMatch(/flow/);
  });

  it("refuses an ambiguous duplicate key instead of picking one", () => {
    const doc = "spec:\n  model: a\n  model: b\n";
    const r = writeScalar(doc, "spec.model", "c");
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.reason).toMatch(/more than once/);
  });

  it("refuses to descend through a scalar", () => {
    const doc = "spec:\n  model: gpt-4o\n";
    const r = writeScalar(doc, "spec.model.nested", "x");
    expect(r.ok).toBe(false);
  });

  it("refuses a list index that does not exist", () => {
    const r = writeScalar(RICH, "spec.behavior.scenarios.9.name", "x");
    expect(r.ok).toBe(false);
  });

  it("does not confuse a same-named key at a different depth", () => {
    // `name` exists under metadata AND inside each tool and scenario.
    expect(readScalar(RICH, "metadata.name")).toEqual({ ok: true, value: "support-bot" });
    expect(readScalar(RICH, "spec.tools.0.name")).toEqual({ ok: true, value: "lookup_order" });
  });
});

describe("countItems", () => {
  it("counts list entries", () => {
    expect(countItems(RICH, "spec.behavior.scenarios")).toBe(2);
    expect(countItems(RICH, "spec.tools")).toBe(1);
    expect(countItems(RICH, "metadata.tags")).toBe(2);
  });

  it("returns 0 for a missing or non-list path", () => {
    expect(countItems(RICH, "spec.nope")).toBe(0);
    expect(countItems(RICH, "spec.model")).toBe(0);
  });
});

describe("encodeScalar", () => {
  it("leaves plain values unquoted", () => {
    expect(encodeScalar("gpt-4o")).toBe("gpt-4o");
    expect(encodeScalar("openai-chat-completions")).toBe("openai-chat-completions");
    expect(encodeScalar("Tier-1 triage")).toBe("Tier-1 triage");
  });

  it("quotes anything YAML would reinterpret", () => {
    expect(encodeScalar("true")).toBe('"true"');
    expect(encodeScalar("123")).toBe('"123"');
    expect(encodeScalar("")).toBe('""');
    expect(encodeScalar("has: colon")).toContain('"');
  });

  it("escapes embedded quotes", () => {
    expect(encodeScalar('say "hi"')).toBe('"say \\"hi\\""');
  });
});
