// Error codes for `?error=` query parameters, and the fixed copy each one
// renders (review K-18).
//
// A page that prints `?error=` verbatim lets anyone craft a link that shows
// their own text under the console's chrome — "your key expired, paste it at
// …" inside a red "Login failed." banner. React escapes it, so this is content
// spoofing rather than XSS, but it is still phishing copy with our branding.
//
// So the URL carries only a short code. The page maps a known code to a fixed
// message and anything else to a generic one; nothing from the query string is
// ever rendered. Upstream detail that is worth showing travels through the
// server-side flash store (lib/flash.ts), which a crafted link cannot write.
//
// This module is deliberately free of server-only imports so client components
// and unit tests can use it.

export const ERROR_MESSAGES = {
  // Sign-in
  key_required: "An API key is required.",
  key_rejected: "API key rejected. Check the value and try again.",
  local_mode: "This server runs in local mode and does not use API keys.",
  unreachable: "Server unreachable. Is MockAgents running?",
  // Form validation done by the console itself
  name_required: "A name is required.",
  invalid_quota: "Quota values must be non-negative numbers.",
  // Outcomes mapped from an upstream HTTP status
  bad_request: "The server rejected the request as invalid.",
  unauthorized: "Your session is no longer valid. Sign in again.",
  forbidden: "Your credential does not have permission for that action.",
  not_found: "That item no longer exists on the server.",
  conflict: "That conflicts with the server's current state (for example, a name already in use).",
  rate_limited: "The server is rate-limiting requests. Wait a moment and try again.",
  server_error: "The server hit an internal error. Check the server logs.",
  request_failed: "The request failed.",
} as const;

export type ErrorCode = keyof typeof ERROR_MESSAGES;

/** Shown for any `?error=` value that is not a known code. Deliberately says
 * nothing specific: the value came from a URL anyone can write. */
export const GENERIC_ERROR_MESSAGE = "Something went wrong. Try again, or check the server logs.";

export function isErrorCode(value: unknown): value is ErrorCode {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(ERROR_MESSAGES, value);
}

/** The fixed message for a `?error=` value. Returns null when there is no
 * error to show, a fixed message for a known code, and the generic message for
 * anything else. Never returns the input. */
export function errorMessage(code: string | string[] | null | undefined): string | null {
  if (code === undefined || code === null || code === "") return null;
  if (Array.isArray(code)) return GENERIC_ERROR_MESSAGE;
  return isErrorCode(code) ? ERROR_MESSAGES[code] : GENERIC_ERROR_MESSAGE;
}

/** The error code for an upstream HTTP status. */
export function errorCodeForStatus(status: number): ErrorCode {
  if (status === 400 || status === 422) return "bad_request";
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 409 || status === 412) return "conflict";
  if (status === 429) return "rate_limited";
  if (status >= 500) return "server_error";
  return "request_failed";
}

/** Longest upstream detail the console will pass along. */
export const MAX_DETAIL_LENGTH = 200;

/** Extract a bounded, single-line message from an upstream error body.
 *
 * The management API answers errors with a JSON envelope (`{"error": "..."}`,
 * internal/server/handlers.go writeError). That message is written for API
 * clients and is reasonable to show an operator. Anything else — an HTML error
 * page from a proxy, a stack trace, a body of unknown shape — is NOT forwarded:
 * this returns null and the caller falls back to a status-based message.
 *
 * Control characters are removed and whitespace collapsed so the result cannot
 * smuggle line breaks or terminal escapes, and it is cut to MAX_DETAIL_LENGTH. */
export function sanitizeUpstreamDetail(body: string): string | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
  const obj = parsed as Record<string, unknown>;
  let message: unknown = obj.error;
  // Tolerate the provider-style nested shape {"error": {"message": "..."}}.
  if (message && typeof message === "object" && !Array.isArray(message)) {
    message = (message as Record<string, unknown>).message;
  }
  if (typeof message !== "string") message = obj.message;
  if (typeof message !== "string") return null;
  return boundDetail(message);
}

/** Strip control characters, collapse whitespace and bound the length. */
export function boundDetail(message: string): string | null {
  const clean = message
    .replace(/[\u0000-\u001f\u007f-\u009f]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (!clean) return null;
  return clean.length > MAX_DETAIL_LENGTH ? clean.slice(0, MAX_DETAIL_LENGTH - 1) + "…" : clean;
}
