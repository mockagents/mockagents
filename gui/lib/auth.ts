// Server-only auth helpers. The GUI stores the operator's API key in
// an HttpOnly cookie so that every server component can read it via
// getAuthKey() in api.ts without exposing it to client-side JavaScript.
//
// Single-tenant deployments never set the cookie and every helper
// below becomes a no-op that returns null / empty string. The UI falls
// back to rendering as an anonymous viewer in that case.

"use server";

import { cookies } from "next/headers";

import {
  AUTH_COOKIE,
  SESSION_COOKIE,
  APIError,
  burnMyAPIKey,
  probeTenants,
  rotateMyAPIKey,
} from "./api";

export interface AuthStatus {
  /** The first 8 characters of the stored key, for display only. Never
   * surface the full secret back to the browser. */
  prefix: string;
  /** Best-effort role inferred from what the token can reach. We can't
   * cleanly ask the server "what role am I?" without adding a new
   * endpoint, so /login records the role that tenants/list succeeded
   * for and stashes it alongside the key. */
  role: "admin" | "unknown";
}

const ROLE_COOKIE = "mockagents_role";

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

/** Read the current session's display metadata. Returns null when the
 * operator is not signed in. Safe to call from any server component. */
export async function getAuthStatus(): Promise<AuthStatus | null> {
  const store = await cookies();
  const key = store.get(AUTH_COOKIE)?.value ?? "";
  if (!key) return null;
  const role = (store.get(ROLE_COOKIE)?.value as AuthStatus["role"]) ?? "unknown";
  return { prefix: key.slice(0, 8), role };
}

/** Validate a pasted API key by probing /api/v1/tenants (admin-only).
 * On success the cookie is persisted and the caller redirects to /.
 * On 401/403 the error message is returned so the login form can
 * display it inline without throwing. */
export async function login(formData: FormData): Promise<{ ok: boolean; error?: string }> {
  const raw = (formData.get("key") ?? "").toString().trim();
  if (!raw) {
    return { ok: false, error: "API key is required." };
  }
  try {
    // A 200 response from /api/v1/tenants proves the key is valid AND
    // has the admin role — the tenants endpoint is admin-gated at the
    // middleware level. That's the only role the GUI admin pages need.
    const tenants = await probeTenants(raw);
    if (tenants === null) {
      return { ok: false, error: "Server unreachable. Is MockAgents running?" };
    }
  } catch (err) {
    if (err instanceof APIError) {
      if (err.status === 401 || err.status === 403) {
        return { ok: false, error: "API key rejected (needs admin role)." };
      }
      return { ok: false, error: `Server returned ${err.status}.` };
    }
    return { ok: false, error: "Unknown error validating key." };
  }

  const store = await cookies();
  store.set(AUTH_COOKIE, raw, sessionCookieOptions());
  store.set(ROLE_COOKIE, "admin", sessionCookieOptions());
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
  { ok: true; plaintext: string; prefix: string } | { ok: false; error: string }
> {
  try {
    const result = await rotateMyAPIKey();
    const store = await cookies();
    // Overwrite the auth cookie with the fresh plaintext. The old
    // secret is already invalid on the server side, so subsequent
    // requests MUST use the new value or they will 401. Keep the
    // role cookie as-is — rotation preserves role.
    store.set(AUTH_COOKIE, result.plaintext, sessionCookieOptions());
    return { ok: true, plaintext: result.plaintext, prefix: result.key.prefix };
  } catch (err) {
    if (err instanceof APIError) {
      return { ok: false, error: `Server returned ${err.status}.` };
    }
    return { ok: false, error: "Unknown error rotating key." };
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
export async function burnSession(): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    await burnMyAPIKey();
  } catch (err) {
    if (err instanceof APIError) {
      return { ok: false, error: `Server returned ${err.status}.` };
    }
    return { ok: false, error: "Unknown error burning session." };
  }
  // The server has already invalidated our old plaintext; the
  // cookies we're about to clear are the last references to it.
  const store = await cookies();
  store.delete(AUTH_COOKIE);
  store.delete(ROLE_COOKIE);
  return { ok: true };
}

/** Clear the session cookies. Called from the logout form in layout.tsx. Also
 * clears the SSO session cookie; full server-side session revocation happens by
 * navigating to the backend's /auth/logout (or via the session TTL). */
export async function logout(): Promise<void> {
  const store = await cookies();
  store.delete(AUTH_COOKIE);
  store.delete(ROLE_COOKIE);
  store.delete(SESSION_COOKIE);
}
