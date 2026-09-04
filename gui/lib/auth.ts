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
  getIdentity,
  rotateMyAPIKey,
  type Identity,
  type PrincipalRole,
} from "./api";

export interface AuthStatus {
  /** The first 8 characters of the stored key, for display only. Never
   * surface the full secret back to the browser. */
  prefix: string;
  /** The role the SERVER reports for this credential, read fresh on every
   * call. null means we could not confirm it — either the server was
   * unreachable or it is running in local mode. Never a guess: a stale or
   * assumed role is how a downgraded key keeps rendering admin controls. */
  role: PrincipalRole | null;
  /** Tenant the credential belongs to, when the server reports one. */
  tenantId: string | null;
  /** Capabilities the server says this credential has. Empty when unknown. */
  capabilities: string[];
  /** True when the credential exists but the server could not be reached.
   * The UI must show this as "unknown", not as signed-out and not as
   * confirmed — an offline server is not an authorization failure. */
  unreachable: boolean;
}

/** Legacy cookie that cached a fabricated role before UX-01. Nothing writes it
 * any more, but logout still clears it so a browser carrying one from an older
 * build does not keep a stale value around. Remove once no deployed build
 * writes it. */
const LEGACY_ROLE_COOKIE = "mockagents_role";

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

/** Read the current session's identity from the SERVER. Returns null when
 * there is no credential at all, or when the server rejected the one we
 * hold — both mean "show the sign-in affordance".
 *
 * A server that cannot be reached is NOT a rejection: the credential is
 * reported back with unreachable=true so the shell can say "unknown" rather
 * than silently signing the operator out whenever the mock restarts.
 *
 * Safe to call from any server component. */
export async function getAuthStatus(): Promise<AuthStatus | null> {
  const store = await cookies();
  const key = store.get(AUTH_COOKIE)?.value ?? store.get(SESSION_COOKIE)?.value ?? "";
  if (!key) return null;

  const prefix = key.slice(0, 8);
  let identity: Identity | null;
  try {
    identity = await getIdentity();
  } catch (err) {
    // 401 means this credential is no longer valid — it was revoked, burned,
    // or the session expired. Surface it as signed out so the operator is
    // prompted, instead of rendering controls that will all fail.
    if (err instanceof APIError && err.status === 401) return null;
    // Any other HTTP error is a server problem, not an identity verdict.
    return { prefix, role: null, tenantId: null, capabilities: [], unreachable: true };
  }
  if (identity === null) {
    return { prefix, role: null, tenantId: null, capabilities: [], unreachable: true };
  }

  return {
    prefix,
    role: identity.role,
    tenantId: identity.tenant_id ?? null,
    capabilities: identity.capabilities,
    unreachable: false,
  };
}

/** Validate a pasted API key against GET /api/v1/identity and persist it.
 *
 * Any authenticated role is accepted. The previous implementation probed
 * GET /api/v1/tenants, which is platform-gated, so viewer, editor and admin
 * keys could not sign in at all — the console was unusable for every role but
 * one. Authorization for individual actions still happens server-side on each
 * request; signing in does not grant anything. */
export async function login(formData: FormData): Promise<{ ok: boolean; error?: string }> {
  const raw = (formData.get("key") ?? "").toString().trim();
  if (!raw) {
    return { ok: false, error: "API key is required." };
  }
  try {
    const identity = await getIdentity(raw);
    if (identity === null) {
      return { ok: false, error: "Server unreachable. Is MockAgents running?" };
    }
    if (!identity.authenticated) {
      // The server is in local mode: it has no accounts, so there is nothing
      // for this key to authenticate against. Say so plainly rather than
      // reporting the key as bad.
      return {
        ok: false,
        error: "This server runs in local mode and does not use API keys.",
      };
    }
  } catch (err) {
    if (err instanceof APIError) {
      if (err.status === 401) {
        return { ok: false, error: "API key rejected. Check the value and try again." };
      }
      return { ok: false, error: `Server returned ${err.status}.` };
    }
    return { ok: false, error: "Unknown error validating key." };
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
  store.delete(LEGACY_ROLE_COOKIE);
  return { ok: true };
}

/** Clear the session cookies. Called from the logout form in layout.tsx. Also
 * clears the SSO session cookie; full server-side session revocation happens by
 * navigating to the backend's /auth/logout (or via the session TTL). */
export async function logout(): Promise<void> {
  const store = await cookies();
  store.delete(AUTH_COOKIE);
  store.delete(LEGACY_ROLE_COOKIE);
  store.delete(SESSION_COOKIE);
}
