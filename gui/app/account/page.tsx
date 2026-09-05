import Link from "next/link";
import { redirect } from "next/navigation";

import { getQuota, type QuotaResponse } from "@/lib/api";
import { burnSession, getAuthStatus, logout, rotateSelf } from "@/lib/auth";
import { setFlash, takeFlash } from "@/lib/flash";
import { Icon } from "@/lib/icons";

import { DangerConfirm } from "../admin/DangerConfirm";

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

  // UX-07: quota, read at the only scope the server offers it — your own.
  // GET /api/v1/quota is open to any authenticated role and answers for the
  // CALLER's tenant; there is no per-tenant read, so this belongs here rather
  // than on a tenant admin page where it would silently describe the wrong
  // tenant.
  let quota: QuotaResponse | null = null;
  let quotaUnavailable = false;
  let quotaUnknown = false;
  try {
    quota = await getQuota();
    quotaUnavailable = quota === null;
  } catch {
    // A failed read is not "no limits" — say we do not know.
    quotaUnknown = true;
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
              {/* UX-01: the role comes from the server. When it could not be
                  confirmed, say so — a blank badge reads as "no role", and an
                  assumed one is worse. */}
              {auth.role ? (
                <span className="badge badge-secondary">{auth.role}</span>
              ) : (
                <span
                  className="badge badge-outline"
                  title={
                    auth.unreachable
                      ? "The server could not be reached, so its role for this key is unknown."
                      : "This server reports no role for the current session."
                  }
                >
                  unknown
                </span>
              )}
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
              <DangerConfirm
                action={rotateAction}
                triggerLabel={
                  <>
                    <Icon name="rotate-cw" size={14} />
                    Rotate my key
                  </>
                }
                triggerTitle="Regenerate your API key in place"
                triggerClassName="btn btn-outline btn-sm"
                title="Rotate your own key?"
                impact={
                  <>
                    Your key keeps its id, name and role; only the secret changes. This
                    browser is updated automatically, so the console keeps working — but
                    the new secret is shown <strong>exactly once</strong>.
                  </>
                }
                consequences={[
                  "Every other consumer of this key — CI jobs, scripts, other machines — fails from the next request until it is updated.",
                  "The replacement secret is displayed once on the next page load and cannot be retrieved again.",
                  "Rotation cannot be undone; the previous secret is gone.",
                ]}
                confirmLabel="Rotate my key"
              />
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

        <QuotaCard quota={quota} unavailable={quotaUnavailable} unknown={quotaUnknown} />
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

/** UX-07: quota shown at the floors the server actually enforces.
 *
 * Three distinct states that a naive card would collapse into "no limits":
 *   - quotas are not enabled on this server (503) — there is no cap at all;
 *   - the read failed — the cap is UNKNOWN, and rendering 0 would tell an
 *     operator they are unlimited when they may be one request from a 429;
 *   - a limit of 0, which this server genuinely defines as unlimited.
 *
 * Raising a cap is platform-gated (PUT /api/v1/tenants/{id}/quota), so this
 * card is deliberately read-only and says where the change is made instead of
 * offering a control that would 403. */
function QuotaCard({
  quota,
  unavailable,
  unknown,
}: {
  quota: QuotaResponse | null;
  unavailable: boolean;
  unknown: boolean;
}) {
  return (
    <div className="card">
      <div className="card-head">
        <Icon name="gauge" size={16} />
        <div className="grow">
          <h3>Quota</h3>
        </div>
        <span className="sid">UX-07 · GET /api/v1/quota</span>
      </div>
      <div className="card-pad col gap-3">
        {unknown ? (
          <div className="banner banner-warn" role="note">
            <div>
              <strong>Your quota could not be read.</strong> Whether limits apply is
              unknown — this is not the same as being unlimited.
            </div>
          </div>
        ) : unavailable ? (
          <p className="txt-sm muted" style={{ margin: 0 }}>
            Quota enforcement is not enabled on this server, so no rate or spend cap
            applies to your requests.
          </p>
        ) : quota ? (
          <>
            <dl className="kv">
              <dt>tenant</dt>
              <dd className="mono">{quota.tenant_id || "— single-tenant / anonymous"}</dd>
              <dt>request rate</dt>
              <dd className="mono">{describeRate(quota.limits.rate_per_sec, quota.limits.rate_burst)}</dd>
              <dt>monthly spend cap</dt>
              <dd className="mono">
                {quota.limits.monthly_spend_usd === 0
                  ? "unlimited"
                  : `$${quota.limits.monthly_spend_usd.toFixed(2)}`}
              </dd>
              <dt>used this month</dt>
              <dd className="mono">
                ${quota.usage.spend_usd.toFixed(4)} <span className="muted">({quota.usage.month} UTC)</span>
              </dd>
            </dl>
            <p className="hint" style={{ margin: 0 }}>
              Spend is the estimated upstream cost of your captured requests, not money
              charged. Changing a cap is a platform-role action on the tenant, not a
              self-service one.
            </p>
          </>
        ) : null}
      </div>
    </div>
  );
}

/** 0 means unlimited in this server's quota model — spell that out rather than
 * printing "0 req/s", which reads as a total block. */
function describeRate(perSec: number, burst: number): string {
  if (perSec === 0) return "unlimited";
  return `${perSec} req/s · burst ${burst}`;
}
