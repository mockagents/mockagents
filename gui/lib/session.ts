// Read-only session helpers for server components.
//
// Deliberately NOT a "use server" module: every export of such a module
// becomes a client-callable Server Action, and there is no reason for the
// browser to be able to invoke getAuthStatus as an RPC endpoint (review 6.7).
// Mutations (login, rotate, burn, logout) stay in lib/auth.ts. lib/flash.ts
// follows the same split for the same reason.

import { cookies } from "next/headers";

import { AUTH_COOKIE, SESSION_COOKIE, APIError, getIdentity, type Identity, type PrincipalRole } from "./api";

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

