import Link from "next/link";
import { redirect } from "next/navigation";

import { burnSession, getAuthStatus, logout, rotateSelf } from "@/lib/auth";
import { setFlash, takeFlash } from "@/lib/flash";
import { Icon } from "@/lib/icons";

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
    <div className="view-enter">
      <div className="breadcrumb">
        <Link href="/">Agents</Link> · Account
      </div>
      <div className="page-head">
        <h1 className="page-title">Your session</h1>
        <p className="page-lede">
          The full API key is stored in an HttpOnly cookie and forwarded on
          every management-API request automatically. Admins manage other
          tenants&apos; keys over on{" "}
          <Link href="/admin/tenants">Tenants &amp; keys</Link>.
        </p>
      </div>

      {error && (
        <div className="banner banner-error">
          <div className="row gap-2">
            <Icon name="x-circle" size={16} />
            <div>
              <strong>Could not rotate.</strong> {error}
            </div>
          </div>
        </div>
      )}

      {plaintext && (
        <div className="banner banner-ok">
          <div className="row gap-2">
            <Icon name="key-round" size={16} />
            <div className="grow">
              <strong>Your key was rotated.</strong> Copy this new secret now.
              The browser cookie is already updated, so this page keeps working;
              external consumers (CI, scripts) of the old key must be updated.
            </div>
          </div>
          <div className="plaintext-box">
            <code>{plaintext}</code>
          </div>
        </div>
      )}

      <div className="grid grid-2" style={{ alignItems: "start" }}>
        <div className="card">
          <div className="card-head">
            <Icon name="shield" size={16} />
            <div className="grow">
              <h3>Identity</h3>
            </div>
          </div>
          <div className="card-pad col gap-4">
            <div className="row" style={{ justifyContent: "space-between" }}>
              <span className="txt-sm" style={{ fontWeight: 500 }}>
                Key prefix
              </span>
              <code className="mono txt-sm">{auth.prefix}…</code>
            </div>
            <div className="row" style={{ justifyContent: "space-between" }}>
              <span className="txt-sm" style={{ fontWeight: 500 }}>
                Role
              </span>
              <span className="badge badge-secondary">{auth.role}</span>
            </div>
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <Icon name="key-round" size={16} />
            <div className="grow">
              <h3>Actions</h3>
            </div>
          </div>
          <div className="card-pad col gap-3">
            <div className="row gap-2">
              <form action={rotateAction} className="inline">
                <button
                  type="submit"
                  className="btn btn-outline btn-sm"
                  title="Regenerate your API key in place"
                >
                  <Icon name="rotate-cw" size={14} />
                  Rotate my key
                </button>
              </form>
              <form action={logoutAction} className="inline">
                <button type="submit" className="btn btn-outline btn-sm">
                  <Icon name="log-out" size={14} />
                  Sign out
                </button>
              </form>
              {!burnConfirming && (
                <Link
                  href="/account?burn=confirm"
                  className="btn btn-outline btn-sm"
                  title="Rotate your key WITHOUT returning the new plaintext, then sign out. Emergency response to a compromised browser session."
                  style={{ color: "var(--sr-danger-fg)" }}
                >
                  <Icon name="trash" size={14} />
                  Burn this session
                </Link>
              )}
            </div>
            <p className="hint">
              Rotation preserves your key id, name, and role — only the secret
              changes. Use it when you suspect your current key has been exposed
              (e.g. committed to a repo by accident).
            </p>
          </div>
        </div>
      </div>

      {burnConfirming && (
        <div className="banner banner-error" style={{ marginTop: 18 }}>
          <div className="row gap-2">
            <Icon name="alert-triangle" size={16} />
            <div className="grow">
              <strong>Burn this session?</strong> Your current key will be
              rotated on the server, but the new plaintext will NOT be returned
              to this browser. You will be logged out immediately and recovery
              requires an out-of-band channel (a different device with an admin
              credential minting a new key, or the CLI bootstrap flow).
            </div>
          </div>
          <div className="row gap-2" style={{ marginTop: 4 }}>
            <form action={burnAction} className="inline">
              <button type="submit" className="btn btn-danger btn-sm">
                Yes, burn it
              </button>
            </form>
            <Link href="/account" className="btn btn-outline btn-sm">
              Cancel
            </Link>
          </div>
        </div>
      )}
    </div>
  );
}
