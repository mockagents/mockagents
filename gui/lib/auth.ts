// Server Actions for the session: sign in, rotate, burn, sign out. The GUI
// stores the operator's API key in an HttpOnly cookie so that every server
// component can read it via getAuthKey() in api.ts without exposing it to
// client-side JavaScript.
//
// Every export of a "use server" module is a client-callable action, so only
// the mutations live here. Read-only helpers such as getAuthStatus are in
// lib/session.ts, a plain server module, so they are not exposed as RPC
// endpoints (review 6.7).
//
// Failures are returned as ErrorCode values (lib/errors.ts), not prose, so the
// pages can carry them in a URL without that URL becoming a way to put
// arbitrary text on screen (review K-18).

"use server";

import { cookies } from "next/headers";

import { AUTH_COOKIE, SESSION_COOKIE, APIError, burnMyAPIKey, getIdentity, rotateMyAPIKey } from "./api";
import { errorCodeForStatus, type ErrorCode } from "./errors";

// 30-day session — GUI is a dev/ops tool and operators usually want a
// long-lived login.
const SESSION_MAX_AGE = 60 * 60 * 24 * 30;

// sessionCookieOptions are the flags for the two session cookies. The cookie
// value is the raw, bearer-equivalent admin key, so:
//   - Secure (in production) keeps it off plaintext-HTTP requests (GUI-01).
//     Left off in dev so http://localhost login still works.
//   - SameSite=Strict: it's a long-lived raw credential, not a session id, and
//     the GUI has no cross-site inbound flow that needs it on first navigation
//     (the /login page sets it fresh) (GUI-09).
//   - HttpOnly keeps it out of reach of any client JS.
function sessionCookieOptions() {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict" as const,
    path: "/",
    maxAge: SESSION_MAX_AGE,
  };
}

/** Validate a pasted API key against GET /api/v1/identity and persist it.
 *
 * Any authenticated role is accepted. The previous implementation probed
 * GET /api/v1/tenants, which is platform-gated, so viewer, editor and admin
 * keys could not sign in at all — the console was unusable for every role but
 * one. Authorization for individual actions still happens server-side on each
 * request; signing in does not grant anything. */
export async function login(formData: FormData): Promise<{ ok: boolean; error?: ErrorCode }> {
  const raw = (formData.get("key") ?? "").toString().trim();
  if (!raw) {
    return { ok: false, error: "key_required" };
  }
  try {
    const identity = await getIdentity(raw);
    if (identity === null) {
      return { ok: false, error: "unreachable" };
    }
    if (!identity.authenticated) {
      // The server is in local mode: it has no accounts, so there is nothing
      // for this key to authenticate against. Say so plainly rather than
      // reporting the key as bad.
      return { ok: false, error: "local_mode" };
    }
  } catch (err) {
    if (err instanceof APIError) {
      return { ok: false, error: err.status === 401 ? "key_rejected" : errorCodeForStatus(err.status) };
    }
    return { ok: false, error: "request_failed" };
  }

  const store = await cookies();
  store.set(AUTH_COOKIE, raw, sessionCookieOptions());
  return { ok: true };
}

/** Rotate the caller's own API key and update the session cookie
 * to the new plaintext in a single step. Returns the plaintext so
 * the caller can surface it once in a banner — store it somewhere
 * permanent before navigating away, because the server will never
 * emit it again. On transport or auth failures returns a
 * `{ ok: false, error }` shape so the caller can render an
 * inline banner instead of crashing. */
export async function rotateSelf(): Promise<
  { ok: true; plaintext: string; prefix: string } | { ok: false; error: ErrorCode }
> {
  try {
    const result = await rotateMyAPIKey();
    const store = await cookies();
    // Overwrite the auth cookie with the fresh plaintext. The old
    // secret is already invalid on the server side, so subsequent
    // requests MUST use the new value or they will 401. Rotation
    // preserves the key's role on the server.
    store.set(AUTH_COOKIE, result.plaintext, sessionCookieOptions());
    return { ok: true, plaintext: result.plaintext, prefix: result.key.prefix };
  } catch (err) {
    if (err instanceof APIError) {
      return { ok: false, error: errorCodeForStatus(err.status) };
    }
    return { ok: false, error: "request_failed" };
  }
}

/** Rotate-and-burn the caller's own key: the server rotates in
 * place but never returns the new plaintext, and we clear the
 * session cookies locally so the browser is fully logged out.
 * Returns a result shape so the caller can render an inline
 * error on failure instead of redirecting blindly.
 *
 * Use this when the current browser session is suspected to be
 * compromised: the new plaintext never touches the compromised
 * machine, and recovery goes through an out-of-band channel (a
 * different device with an admin credential minting a new key,
 * or the CLI bootstrap flow). */
export async function burnSession(): Promise<{ ok: true } | { ok: false; error: ErrorCode }> {
  try {
    await burnMyAPIKey();
  } catch (err) {
    if (err instanceof APIError) {
      return { ok: false, error: errorCodeForStatus(err.status) };
    }
    return { ok: false, error: "request_failed" };
  }
  // The server has already invalidated our old plaintext; the
  // cookies we're about to clear are the last references to it.
  const store = await cookies();
  store.delete(AUTH_COOKIE);
  return { ok: true };
}

/** Clear the session cookies. Called from the logout form in layout.tsx. Also
 * clears the SSO session cookie; full server-side session revocation happens by
 * navigating to the backend's /auth/logout (or via the session TTL). */
export async function logout(): Promise<void> {
  const store = await cookies();
  store.delete(AUTH_COOKIE);
  store.delete(SESSION_COOKIE);
}
