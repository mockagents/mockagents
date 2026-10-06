// Server-only helpers for reporting a failed Server Action on the page it
// redirects back to (review K-18).
//
// The redirect URL carries only a short ErrorCode. The upstream's own message,
// when it has one worth showing (a 409 "name already in use", a 400 naming the
// bad field), goes through the single-read flash store instead, so it is never
// in the URL and a crafted link cannot supply it. Not a "use server" module, so
// none of this is a client-callable action.

import { APIError } from "./api";
import { boundDetail, errorCodeForStatus, type ErrorCode } from "./errors";
import { setFlash } from "./flash";

/** `base?error=<code>`, keeping any query string `base` already has. */
export function withErrorCode(base: string, code: ErrorCode): string {
  return `${base}${base.includes("?") ? "&" : "?"}error=${code}`;
}

/** Where to send the browser after an action failed with `err`, stashing the
 * upstream detail in the flash store. Returns null when `err` is not an
 * upstream HTTP failure — the caller rethrows it, which also lets Next's own
 * redirect signal through. */
export async function errorRedirect(base: string, err: unknown): Promise<string | null> {
  if (!(err instanceof APIError)) return null;
  if (err.detail) await setFlash(JSON.stringify({ errorDetail: err.detail }));
  return withErrorCode(base, errorCodeForStatus(err.status));
}

/** Pull a flashed error detail out of a parsed flash payload. */
export function flashedErrorDetail(data: unknown): string | null {
  if (!data || typeof data !== "object") return null;
  const v = (data as { errorDetail?: unknown }).errorDetail;
  return typeof v === "string" ? boundDetail(v) : null;
}
