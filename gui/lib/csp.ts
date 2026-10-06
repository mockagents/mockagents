// Content-Security-Policy for the console (GUI-04, review §4 "nonce CSP").
//
// The console renders attacker-influenceable interaction-log content, so the
// CSP is defense in depth on top of React's escaping. Pages get a per-request
// nonce (proxy.ts): Next.js reads it from the request's CSP header and stamps
// it on every script it emits, so `script-src` no longer needs
// 'unsafe-inline'. 'strict-dynamic' lets those trusted scripts load the
// route chunks they import. Development additionally needs 'unsafe-eval' for
// React's debugging and HMR.
//
// style-src keeps 'unsafe-inline': the console uses React `style={{…}}`
// attributes throughout, and style attributes cannot carry a nonce. Inline
// styles cannot execute script.
//
// Pure and dependency-free so it can be unit-tested and imported by proxy.ts.

/** A fresh, base64 nonce for one response. */
export function createNonce(): string {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}

/** The CSP for an HTML page rendered with `nonce`. */
export function pageContentSecurityPolicy(nonce: string, isDev: boolean): string {
  const script = [`'self'`, `'nonce-${nonce}'`, `'strict-dynamic'`];
  if (isDev) script.push(`'unsafe-eval'`);
  return [
    "default-src 'self'",
    "img-src 'self' data:",
    "style-src 'self' 'unsafe-inline'",
    `script-src ${script.join(" ")}`,
    "connect-src 'self'",
    "font-src 'self'",
    "frame-ancestors 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "object-src 'none'",
  ].join("; ");
}

/** The CSP for responses that are never rendered as a document (route
 * handlers: JSON, SSE). Nothing may load from them. */
export const API_CONTENT_SECURITY_POLICY = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'";
