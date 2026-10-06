// Review K-18: `?error=` must never put the URL's own text on screen, and raw
// upstream bodies must never reach the browser.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ErrorBanner } from "@/app/ErrorBanner";
import {
  ERROR_MESSAGES,
  GENERIC_ERROR_MESSAGE,
  MAX_DETAIL_LENGTH,
  boundDetail,
  errorCodeForStatus,
  errorMessage,
  sanitizeUpstreamDetail,
} from "@/lib/errors";

const PHISH = "Your key expired. Paste it at https://evil.example/recover";

describe("errorMessage", () => {
  it("returns null when there is no error", () => {
    expect(errorMessage(undefined)).toBeNull();
    expect(errorMessage(null)).toBeNull();
    expect(errorMessage("")).toBeNull();
  });

  it("maps every known code to its fixed message", () => {
    for (const [code, message] of Object.entries(ERROR_MESSAGES)) {
      expect(errorMessage(code)).toBe(message);
    }
  });

  it("never echoes an unknown value", () => {
    expect(errorMessage(PHISH)).toBe(GENERIC_ERROR_MESSAGE);
    expect(errorMessage("toString")).toBe(GENERIC_ERROR_MESSAGE); // prototype keys are not codes
    expect(errorMessage("__proto__")).toBe(GENERIC_ERROR_MESSAGE);
    expect(errorMessage(["key_rejected", "x"])).toBe(GENERIC_ERROR_MESSAGE);
  });
});

describe("errorCodeForStatus", () => {
  it.each([
    [400, "bad_request"],
    [422, "bad_request"],
    [401, "unauthorized"],
    [403, "forbidden"],
    [404, "not_found"],
    [409, "conflict"],
    [412, "conflict"],
    [429, "rate_limited"],
    [500, "server_error"],
    [503, "server_error"],
    [418, "request_failed"],
  ])("%i -> %s", (status, code) => {
    expect(errorCodeForStatus(status)).toBe(code);
  });
});

describe("sanitizeUpstreamDetail", () => {
  it("passes the management API's own error message", () => {
    expect(sanitizeUpstreamDetail('{"error":"tenant name already exists"}')).toBe(
      "tenant name already exists",
    );
  });

  it("accepts the nested provider-style shape", () => {
    expect(sanitizeUpstreamDetail('{"error":{"message":"bad role","type":"x"}}')).toBe("bad role");
  });

  it("drops anything that is not the JSON envelope", () => {
    expect(sanitizeUpstreamDetail("<html><body>502 Bad Gateway</body></html>")).toBeNull();
    expect(sanitizeUpstreamDetail("panic: runtime error\ngoroutine 1 [running]")).toBeNull();
    expect(sanitizeUpstreamDetail('["not","an","object"]')).toBeNull();
    expect(sanitizeUpstreamDetail('{"error":42}')).toBeNull();
    expect(sanitizeUpstreamDetail("")).toBeNull();
  });

  it("removes control characters and collapses whitespace", () => {
    expect(sanitizeUpstreamDetail('{"error":"line one\\nline two\\u001b[31m red"}')).toBe(
      "line one line two [31m red",
    );
  });

  it("bounds the length", () => {
    const long = "x".repeat(5000);
    const out = sanitizeUpstreamDetail(JSON.stringify({ error: long }));
    expect(out).not.toBeNull();
    expect(out!.length).toBe(MAX_DETAIL_LENGTH);
    expect(out!.endsWith("…")).toBe(true);
  });

  it("treats a whitespace-only message as absent", () => {
    expect(boundDetail("   \n\t ")).toBeNull();
  });
});

describe("ErrorBanner", () => {
  it("renders nothing without a code", () => {
    const { container } = render(<ErrorBanner code={undefined} title="Login failed." />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders the fixed copy for a known code", () => {
    render(<ErrorBanner code="key_rejected" title="Login failed." />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Login failed.");
    expect(alert).toHaveTextContent(ERROR_MESSAGES.key_rejected);
  });

  it("renders a generic message for an unknown value and never the value itself", () => {
    render(<ErrorBanner code={PHISH} title="Login failed." />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(GENERIC_ERROR_MESSAGE);
    expect(alert.textContent).not.toContain("evil.example");
    expect(alert.textContent).not.toContain("Paste it");
  });

  it("shows flashed upstream detail alongside a code", () => {
    render(<ErrorBanner code="conflict" detail="tenant name already exists" icon />);
    expect(screen.getByRole("alert")).toHaveTextContent("Server said: tenant name already exists");
  });

  it("has no axe violations", async () => {
    const { container } = render(<ErrorBanner code="forbidden" title="Not deleted." icon />);
    await expect(container).toHaveNoAxeViolations();
  });
});
