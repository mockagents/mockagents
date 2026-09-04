import { describe, expect, it } from "vitest";

import type { InteractionLog } from "@/lib/api";
import {
  DEFAULT_FILTERS,
  filtersToQuery,
  highestId,
  interpretRecovery,
  isFiltered,
  mergeRows,
  parseFilters,
} from "@/lib/logfeed";

function row(id: number, extra: Partial<InteractionLog> = {}): InteractionLog {
  return {
    id,
    timestamp: new Date(1_700_000_000_000 + id * 1000).toISOString(),
    agent_name: "support-bot",
    protocol: "openai-chat-completions",
    request: null,
    response: null,
    latency_ms: 10,
    ...extra,
  };
}

describe("mergeRows", () => {
  it("keeps rows newest-first", () => {
    const merged = mergeRows([row(2)], [row(5), row(3)], { max: 10 });
    expect(merged.map((r) => r.id)).toEqual([5, 3, 2]);
  });

  // A reconnect re-delivers rows the client already has. Without dedup an
  // operator counting requests would count them twice.
  it("deduplicates by id rather than appending a second copy", () => {
    const merged = mergeRows([row(3), row(2)], [row(3), row(4)], { max: 10 });
    expect(merged.map((r) => r.id)).toEqual([4, 3, 2]);
  });

  it("lets a re-delivered row refresh the stored copy", () => {
    const merged = mergeRows([row(3, { latency_ms: 10 })], [row(3, { latency_ms: 99 })], {
      max: 10,
    });
    expect(merged).toHaveLength(1);
    expect(merged[0].latency_ms).toBe(99);
  });

  it("caps the retained rows", () => {
    const merged = mergeRows([], [row(1), row(2), row(3), row(4)], { max: 2 });
    expect(merged.map((r) => r.id)).toEqual([4, 3]);
  });

  // Losing the row you are reading because unrelated traffic arrived is the
  // most annoying way for a live feed to behave.
  it("never evicts the selected row to honour the cap", () => {
    const merged = mergeRows([row(1)], [row(10), row(11), row(12)], { max: 2, pinnedId: 1 });
    expect(merged.map((r) => r.id)).toContain(1);
    expect(merged).toHaveLength(2);
    // The newest row is still kept alongside it.
    expect(merged.map((r) => r.id)).toContain(12);
  });

  it("does not grow past the cap when pinning", () => {
    const incoming = Array.from({ length: 50 }, (_, i) => row(100 + i));
    const merged = mergeRows([row(1)], incoming, { max: 10, pinnedId: 1 });
    expect(merged).toHaveLength(10);
    expect(merged.map((r) => r.id)).toContain(1);
  });

  it("is a no-op when the pinned row is already retained", () => {
    const merged = mergeRows([row(9)], [row(10)], { max: 5, pinnedId: 9 });
    expect(merged.map((r) => r.id)).toEqual([10, 9]);
  });

  // Concurrent inserts: rows can arrive out of order across a reconnect.
  it("orders correctly when rows arrive out of order", () => {
    const merged = mergeRows([row(5)], [row(3), row(9), row(7)], { max: 10 });
    expect(merged.map((r) => r.id)).toEqual([9, 7, 5, 3]);
  });
});

describe("highestId", () => {
  it("finds the resume point", () => {
    expect(highestId([row(3), row(9), row(5)])).toBe(9);
  });
  it("is null for an empty feed", () => {
    expect(highestId([])).toBeNull();
  });
});

describe("interpretRecovery", () => {
  it("reports a closed gap when the recovery page was not full", () => {
    const outcome = interpretRecovery([row(11), row(12)], 200, 10);
    expect(outcome.gap).toEqual({ kind: "closed", recovered: 2 });
  });

  it("reports no gap when nothing had been seen before the drop", () => {
    const outcome = interpretRecovery([row(1)], 200, null);
    expect(outcome.gap.kind).toBe("none");
  });

  // The important one: a FULL page means there may be more beyond the bound,
  // so "caught up" would be a lie.
  it("reports an UNRESOLVED gap when recovery hit its bound", () => {
    const fetched = Array.from({ length: 200 }, (_, i) => row(1000 + i));
    const outcome = interpretRecovery(fetched, 200, 10);
    expect(outcome.gap.kind).toBe("unresolved");
    if (outcome.gap.kind === "unresolved") {
      expect(outcome.gap.reason).toMatch(/missing/i);
    }
  });

  it("still returns the rows it did recover when the gap is unresolved", () => {
    const fetched = Array.from({ length: 200 }, (_, i) => row(1000 + i));
    expect(interpretRecovery(fetched, 200, 10).rows).toHaveLength(200);
  });
});

describe("parseFilters", () => {
  it("reads the allowlisted filters", () => {
    const f = parseFilters(
      new URLSearchParams("agent=bot&session_id=s-1&since=2026-01-01T00:00:00Z&limit=25&offset=50"),
    );
    expect(f).toEqual({
      agent: "bot",
      session_id: "s-1",
      since: "2026-01-01T00:00:00Z",
      until: "",
      limit: 25,
      offset: 50,
    });
  });

  it("ignores keys that are not allowlisted", () => {
    const f = parseFilters(new URLSearchParams("agent=bot&request_body=secret&api_key=leak"));
    expect(f.agent).toBe("bot");
    expect(JSON.stringify(f)).not.toContain("secret");
    expect(JSON.stringify(f)).not.toContain("leak");
  });

  it("falls back to defaults for junk values", () => {
    const f = parseFilters(new URLSearchParams("limit=notanumber&offset=-5"));
    expect(f.limit).toBe(DEFAULT_FILTERS.limit);
    expect(f.offset).toBe(0);
  });

  it("clamps an absurd limit rather than forwarding it", () => {
    expect(parseFilters(new URLSearchParams("limit=999999")).limit).toBe(1000);
  });

  it("accepts a plain object as well as URLSearchParams", () => {
    expect(parseFilters({ agent: "bot" }).agent).toBe("bot");
  });
});

describe("filtersToQuery", () => {
  it("omits defaults so a plain link stays short", () => {
    expect(filtersToQuery(DEFAULT_FILTERS)).toBe("");
  });

  it("round-trips through parseFilters", () => {
    const f = { ...DEFAULT_FILTERS, agent: "bot", session_id: "s-1", offset: 100, limit: 25 };
    expect(parseFilters(new URLSearchParams(filtersToQuery(f)))).toEqual(f);
  });

  // Epic §5: a shareable filter link must never carry a body or a credential.
  it("emits only filter metadata", () => {
    const qs = filtersToQuery({
      ...DEFAULT_FILTERS,
      agent: "bot",
      session_id: "s-1",
      since: "2026-01-01T00:00:00Z",
      until: "2026-01-02T00:00:00Z",
    });
    const keys = [...new URLSearchParams(qs).keys()].sort();
    expect(keys).toEqual(["agent", "session_id", "since", "until"]);
  });
});

describe("isFiltered", () => {
  it("is false for the default view", () => {
    expect(isFiltered(DEFAULT_FILTERS)).toBe(false);
  });
  it("is true once anything narrows the view", () => {
    expect(isFiltered({ ...DEFAULT_FILTERS, session_id: "s" })).toBe(true);
    expect(isFiltered({ ...DEFAULT_FILTERS, offset: 100 })).toBe(true);
  });
});
