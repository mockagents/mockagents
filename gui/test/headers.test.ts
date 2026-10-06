// Review 6.6 (API URL disclosure) and the nonce CSP.
import { describe, expect, it } from "vitest";

import { API_CONTENT_SECURITY_POLICY, createNonce, pageContentSecurityPolicy } from "@/lib/csp";
import { apiUrlVisible } from "@/lib/serverState";

describe("apiUrlVisible", () => {
  it("shows the URL in local (single-tenant) mode", () => {
    expect(apiUrlVisible({ mode: "local", authenticated: false }, false, false)).toBe(true);
  });

  it("shows the URL to an authenticated caller", () => {
    expect(apiUrlVisible({ mode: "multi_tenant", authenticated: true }, false, false)).toBe(true);
  });

  it("withholds the URL from an anonymous multi-tenant visitor", () => {
    expect(apiUrlVisible({ mode: "multi_tenant", authenticated: false }, false, false)).toBe(false);
    // A rejected credential leaves identity unknown: still withheld.
    expect(apiUrlVisible(null, false, false)).toBe(false);
    expect(apiUrlVisible(null, false, true)).toBe(false);
  });

  it("shows the URL of an unreachable server only in development", () => {
    expect(apiUrlVisible(null, true, true)).toBe(true);
    expect(apiUrlVisible(null, true, false)).toBe(false);
  });
});

describe("content security policy", () => {
  it("uses a fresh base64 nonce per call", () => {
    const a = createNonce();
    const b = createNonce();
    expect(a).not.toBe(b);
    expect(a).toMatch(/^[A-Za-z0-9+/]+={0,2}$/);
  });

  it("allows scripts only by nonce in production — no 'unsafe-inline' or 'unsafe-eval'", () => {
    const csp = pageContentSecurityPolicy("abc123==", false);
    const script = csp.split("; ").find((d) => d.startsWith("script-src"))!;
    expect(script).toContain("'nonce-abc123=='");
    expect(script).toContain("'strict-dynamic'");
    expect(script).not.toContain("'unsafe-inline'");
    expect(script).not.toContain("'unsafe-eval'");
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("object-src 'none'");
  });

  it("adds 'unsafe-eval' only in development", () => {
    expect(pageContentSecurityPolicy("n", true)).toContain("'unsafe-eval'");
  });

  it("locks route handlers down completely", () => {
    expect(API_CONTENT_SECURITY_POLICY).toContain("default-src 'none'");
  });
});
