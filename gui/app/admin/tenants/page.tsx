import Link from "next/link";
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";

import {
  APIError,
  createTenant,
  deleteTenant,
  listAPIKeys,
  listTenants,
  Tenant,
} from "@/lib/api";
import { getAuthStatus } from "@/lib/auth";
import { Icon } from "@/lib/icons";

import { DangerConfirm } from "../../DangerConfirm";

type PageProps = {
  searchParams: Promise<{ error?: string; created?: string }>;
};

/** Impact copy for a key count that may be unknown. "0 keys" and "we could not
 * read the keys" are different facts and only one of them is safe to act on. */
function describeKeys(count: number | null): string {
  if (count === null) return "revokes every API key it owns (the count could not be read — unknown, not zero)";
  if (count === 0) return "revokes its API keys (it currently has none)";
  return `revokes its ${count} API key${count === 1 ? "" : "s"} immediately`;
}

export default async function TenantsAdminPage({ searchParams }: PageProps) {
  const { error, created } = await searchParams;
  const auth = await getAuthStatus();
  if (!auth) redirect("/login?next=/admin/tenants");

  let tenants: Tenant[] | null;
  try {
    tenants = await listTenants();
  } catch (err) {
    tenants = null;
    // APIError here means the server returned a non-auth failure —
    // re-surface the message so operators see root cause, not a blank
    // page.
    if (err instanceof APIError) {
      return (
        <div>
          <h1 className="page-title">Tenants</h1>
          <div className="banner banner-error">{err.message}</div>
        </div>
      );
    }
    throw err;
  }

  if (tenants === null) {
    return (
      <div>
        <h1 className="page-title">Tenants</h1>
        <div className="banner banner-warn">
          {/* UX-07: name the actual floor. The tenant collection is
              platform-gated (route_authz.go), NOT admin-gated — a tenant admin
              manages its own keys and never sees this list. Saying "admin" here
              sent admins off to hunt for a broken permission. */}
          <strong>This page requires the platform role. </strong>
          Your credential is valid, but listing tenants is a cross-tenant
          operator action and no tenant-scoped role has it. Platform keys are
          minted only by the server&apos;s bootstrap path — the management API
          refuses to assign that role, so it cannot be granted from here.{" "}
          <Link href="/login">Switch keys</Link>
        </div>
      </div>
    );
  }

  // Impact summaries need the key count per tenant. Read them alongside the
  // list: a confirmation that cannot say what it destroys is not a
  // confirmation. A tenant whose keys cannot be read reports "unknown" rather
  // than a reassuring zero.
  const keyCounts = new Map<string, number | null>();
  await Promise.all(
    tenants.map(async (t) => {
      try {
        const keys = await listAPIKeys(t.id);
        keyCounts.set(t.id, keys === null ? null : keys.length);
      } catch {
        keyCounts.set(t.id, null);
      }
    }),
  );

  async function createAction(formData: FormData) {
    "use server";
    const name = (formData.get("name") ?? "").toString().trim();
    if (!name) redirect("/admin/tenants?error=name+is+required");
    try {
      const tenant = await createTenant(name);
      revalidatePath("/admin/tenants");
      redirect(`/admin/tenants?created=${encodeURIComponent(tenant.id)}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  async function deleteAction(formData: FormData) {
    "use server";
    const id = (formData.get("id") ?? "").toString();
    if (!id) return;
    try {
      await deleteTenant(id);
      revalidatePath("/admin/tenants");
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  return (
    <div className="view-enter">
      <div className="head-row page-head">
        <div className="grow">
          <h1 className="page-title">Tenants &amp; API keys</h1>
          <p className="page-lede">
            Multi-tenant control plane. One row per tenant in{" "}
            <code>.mockagents-tenancy.db</code>; deleting a tenant cascades to
            its API keys — there is no soft-delete.
          </p>
        </div>
        <form action={createAction} className="row gap-2">
          <input
            name="name"
            className="input"
            style={{ width: 200, height: "var(--sr-control-h-sm)" }}
            placeholder="new tenant name"
            required
          />
          <button type="submit" className="btn btn-default btn-sm">
            <Icon name="plus" size={15} />
            New tenant
          </button>
        </form>
      </div>

      {error && <div className="banner banner-error">{error}</div>}
      {created && (
        <div className="banner banner-ok">
          <div className="row gap-2">
            <Icon name="check-circle" size={16} />
            <div>
              Tenant <code>{created}</code> created. Open it to mint its first
              API key.
            </div>
          </div>
        </div>
      )}

      <div className="card" style={{ overflow: "hidden" }}>
        <div className="card-head">
          <h3>Tenants</h3>
          <div className="grow" />
          <span className="tag">{tenants.length}</span>
        </div>
        {tenants.length === 0 ? (
          <div className="empty">No tenants yet.</div>
        ) : (
          tenants.map((t) => (
            <div
              key={t.id}
              className="row gap-3"
              style={{
                padding: "12px 16px",
                borderBottom: "1px solid var(--sr-border)",
              }}
            >
              <div className="agent-icon" style={{ width: 32, height: 32, flex: "0 0 32px" }}>
                <Icon name="users" size={15} />
              </div>
              <Link
                href={`/admin/tenants/${encodeURIComponent(t.id)}`}
                className="grow col"
                style={{ gap: 0, textDecoration: "none", color: "inherit" }}
              >
                <span style={{ fontWeight: 500, fontSize: 13 }}>{t.name}</span>
                <span className="muted mono" style={{ fontSize: 11 }}>
                  {t.id}
                </span>
              </Link>
              <span className="muted txt-xs nowrap">{t.created_at}</span>
              <DangerConfirm
                action={deleteAction}
                fields={{ id: t.id }}
                triggerLabel={<Icon name="trash" size={14} />}
                triggerTitle={`Delete tenant ${t.name}`}
                triggerClassName="btn btn-ghost btn-icon btn-xs danger-trigger"
                title={`Delete tenant ${t.name}?`}
                impact={
                  <>
                    This deletes the tenant record for <code>{t.id}</code> and{" "}
                    {describeKeys(keyCounts.get(t.id) ?? null)}. There is no soft-delete
                    and no undo.
                  </>
                }
                consequences={[
                  "Every API key in this tenant stops working immediately — any client still holding one fails on its next request.",
                  "Audit entries that reference this tenant are left pointing at a tenant that no longer exists.",
                  "Agent and pipeline definitions on disk are NOT deleted; they stay loaded and keep serving.",
                  "Recreating a tenant with the same name produces a different id, so old keys cannot be restored.",
                ]}
                confirmPhrase={t.name}
                confirmPhraseLabel="Type the exact tenant name to confirm"
                confirmLabel="Delete tenant"
              />
            </div>
          ))
        )}
      </div>
    </div>
  );
}
