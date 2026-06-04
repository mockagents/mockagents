import Link from "next/link";
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";

import {
  APIError,
  createTenant,
  deleteTenant,
  listTenants,
  Tenant,
} from "@/lib/api";
import { getAuthStatus } from "@/lib/auth";
import { Icon } from "@/lib/icons";

type PageProps = {
  searchParams: Promise<{ error?: string; created?: string }>;
};

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
          Your API key is valid but cannot list tenants. This page requires an
          admin-role key. <Link href="/login">Switch keys</Link>
        </div>
      </div>
    );
  }

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
              <form action={deleteAction} className="inline">
                <input type="hidden" name="id" value={t.id} />
                <button
                  type="submit"
                  className="btn btn-ghost btn-icon btn-xs"
                  title="Delete tenant (cascades to its keys)"
                  style={{ color: "var(--sr-danger-fg)" }}
                >
                  <Icon name="trash" size={14} />
                </button>
              </form>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
