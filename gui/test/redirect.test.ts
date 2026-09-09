import { describe, expect, it } from "vitest";

import { safeRedirect } from "@/lib/redirect";

const OTHER = "other.example";

describe("safeRedirect", () => {
  it("keeps a same-origin path", () => {
    expect(safeRedirect("/account")).toBe("/account");
    expect(safeRedirect("/admin/tenants/acme")).toBe("/admin/tenants/acme");
  });

  it("keeps query and fragment on a same-origin path", () => {
    expect(safeRedirect("/logs?agent=support#tail")).toBe("/logs?agent=support#tail");
  });

  it("rejects a backslash-prefixed path (audit M-38)", () => {
    // A browser reads a backslash as a path separator, so these resolve to a
    // different origin. The old startsWith("//") check accepted them.
    expect(safeRedirect("/\\" + OTHER)).toBe("/");
    expect(safeRedirect("/\/" + OTHER)).toBe("/");
    expect(safeRedirect("\\\\" + OTHER)).toBe("/");
  });

  it("rejects a protocol-relative URL", () => {
    expect(safeRedirect("//" + OTHER)).toBe("/");
    expect(safeRedirect("//" + OTHER + "/path")).toBe("/");
  });

  it("rejects an absolute URL to another origin", () => {
    expect(safeRedirect("https://" + OTHER + "/page")).toBe("/");
    expect(safeRedirect("http://" + OTHER)).toBe("/");
  });

  it("rejects a non-http scheme", () => {
    expect(safeRedirect("ftp://" + OTHER + "/file")).toBe("/");
    expect(safeRedirect("data:text/plain,hello")).toBe("/");
    expect(safeRedirect("mailto:someone@" + OTHER)).toBe("/");
  });

  it("falls back to the root for empty or missing input", () => {
    expect(safeRedirect(undefined)).toBe("/");
    expect(safeRedirect(null)).toBe("/");
    expect(safeRedirect("")).toBe("/");
  });

  it("resolves a relative value against the root, staying same-origin", () => {
    expect(safeRedirect("account")).toBe("/account");
    expect(safeRedirect("../../etc/hosts")).toBe("/etc/hosts");
  });
});
