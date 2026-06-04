import Link from "next/link";
import { redirect } from "next/navigation";

import { burnSession, getAuthStatus, logout, rotateSelf } from "@/lib/auth";
import { setFlash, takeFlash } from "@/lib/flash";

type PageProps = {
  searchParams: Promise<{
    error?: string;
    burn?: string;
  }>;
};

// /account is the self-service surface for the currently-signed-in
// operator. It shows who you are, lets you rotate your own key in
// place (the cookie is updated atomically so the page keeps working
// after the swap), and lets you sign out. Admins manage *other*
// tenants' keys over on /admin/tenants.
export default async function AccountPage({ searchParams }: PageProps) {
  const { error, burn } = await searchParams;
  const auth = await getAuthStatus();
  if (!auth) redirect("/login?next=/account");
  const burnConfirming = burn === "confirm";

  // One-time rotated-key plaintext is delivered via a server-side flash store
  // (single-read) rather than the URL, so the secret never lands in history /
  // Referer / proxy logs (GUI-02).
  const flashRaw = await takeFlash();
  let plaintext: string | undefined;
  if (flashRaw) {
    try {
      const data = JSON.parse(flashRaw) as { plaintext?: string };
      if (typeof data.plaintext === "string") plaintext = data.plaintext;
    } catch {
      /* ignore malformed flash */
    }
  }

  async function rotateAction() {
    "use server";
    const result = await rotateSelf();
    if (!result.ok) {
      redirect(`/account?error=${encodeURIComponent(result.error)}`);
    }
    await setFlash(JSON.stringify({ plaintext: result.plaintext }));
    redirect("/account");
  }

  async function burnAction() {
    "use server";
    const result = await burnSession();
    if (!result.ok) {
      redirect(`/account?error=${encodeURIComponent(result.error)}`);
    }
    // burnSession already cleared the cookies — a redirect to
    // /login now presents an anonymous session. The user
    // recovers via an out-of-band admin mint.
    redirect("/login?burned=1");
  }

  async function logoutAction() {
    "use server";
    await logout();
    redirect("/login");
  }

  return (
    <div>
      <div className="breadcrumb">
        <Link href="/">Agents</Link> · Account
      </div>
      <h1 className="page-title">Your session</h1>
      <p className="page-lede">
        Prefix <code>{auth.prefix}…</code> · role <strong>{auth.role}</strong>.
        The full API key is stored in an HttpOnly cookie and forwarded on
        every management-API request automatically.
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>Could not rotate.</strong> {error}
        </div>
      )}

      {plaintext && (
        <div className="banner banner-warn">
          <strong>Your key was rotated. Copy this new secret now.</strong>
          <div className="plaintext-box">
            <code>{plaintext}</code>
          </div>
          <span className="muted">
            The browser cookie has already been updated, so this page keeps
            working. External consumers (CI, scripts) of the old key must
            be updated.
          </span>
        </div>
      )}

      <h2 className="section-title">Actions</h2>
      <div className="account-actions">
        <form action={rotateAction} className="inline">
          <button type="submit" className="btn" title="Regenerate your API key in place">
            Rotate my key
          </button>
        </form>
        <form action={logoutAction} className="inline">
          <button type="submit" className="btn">
            Sign out
          </button>
        </form>
        {!burnConfirming && (
          <Link
            href="/account?burn=confirm"
            className="btn btn-danger"
            title="Rotate your key WITHOUT returning the new plaintext, then sign out. Emergency response to a compromised browser session."
          >
            Burn this session
          </Link>
        )}
      </div>

      <p className="hint">
        Rotation preserves your key id, name, and role — only the secret
        changes. Use this when you suspect your current key has been
        exposed (e.g. committed to a repo by accident).
      </p>

      {burnConfirming && (
        <div className="banner banner-error">
          <strong>Burn this session?</strong> Your current key will be
          rotated on the server, but the new plaintext will NOT be
          returned to this browser. You will be logged out immediately
          and recovery requires an out-of-band channel (a different
          device with an admin credential minting a new key, or the
          CLI bootstrap flow).
          <div className="account-actions" style={{ marginTop: "12px" }}>
            <form action={burnAction} className="inline">
              <button type="submit" className="btn btn-danger">
                Yes, burn it
              </button>
            </form>
            <Link href="/account" className="btn">
              Cancel
            </Link>
          </div>
        </div>
      )}
    </div>
  );
}
