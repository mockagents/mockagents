import Link from "next/link";
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";

import {
  APIError,
  APIKey,
  bulkRotateTenantKeys,
  createAPIKey,
  deleteAPIKey,
  listAPIKeys,
  Role,
  rotateAPIKey,
  updateAPIKeyRole,
} from "@/lib/api";
import { getAuthStatus } from "@/lib/auth";
import { setFlash, takeFlash } from "@/lib/flash";
import { Icon } from "@/lib/icons";

type PageProps = {
  params: Promise<{ id: string }>;
  searchParams: Promise<{
    error?: string;
  }>;
};

// BulkRotationResult is the JSON payload we stash in the `bulk`
// query param after a bulk rotation. It's an array of
// {id, name, prefix, plaintext} so the once-only banner can render
// every fresh secret alongside its human name for copying. Parsing
// happens inside the page; malformed payloads fall through to a
// neutral "something happened" banner.
interface BulkRotationEntry {
  id: string;
  name: string;
  prefix: string;
  plaintext: string;
}

const ROLES: Role[] = ["viewer", "editor", "admin"];

export default async function TenantKeysPage({ params, searchParams }: PageProps) {
  const { id } = await params;
  const { error } = await searchParams;

  // One-time key plaintext (single mint/rotate, or a whole bulk-rotation batch)
  // is delivered via the server-side flash store (single-read), never the URL,
  // so the secrets never land in history / Referer / proxy logs (GUI-02).
  let plaintext: string | undefined;
  let plaintextName: string | undefined;
  let bulkRotation: BulkRotationEntry[] | null = null;
  const flashRaw = await takeFlash();
  if (flashRaw) {
    try {
      const data = JSON.parse(flashRaw) as {
        plaintext?: string;
        name?: string;
        bulk?: BulkRotationEntry[];
      };
      if (Array.isArray(data.bulk)) {
        bulkRotation = data.bulk;
      } else if (typeof data.plaintext === "string") {
        plaintext = data.plaintext;
        plaintextName = typeof data.name === "string" ? data.name : undefined;
      }
    } catch {
      /* ignore malformed flash */
    }
  }
  const auth = await getAuthStatus();
  if (!auth) redirect(`/login?next=/admin/tenants/${encodeURIComponent(id)}`);

  let keys: APIKey[] | null;
  try {
    keys = await listAPIKeys(id);
  } catch (err) {
    if (err instanceof APIError && err.status === 404) {
      return (
        <div>
          <h1 className="page-title">Unknown tenant</h1>
          <p className="muted">
            <Link href="/admin/tenants">Back to tenants</Link>
          </p>
        </div>
      );
    }
    if (err instanceof APIError) {
      return (
        <div>
          <h1 className="page-title">Keys for {id}</h1>
          <div className="banner banner-error">{err.message}</div>
        </div>
      );
    }
    throw err;
  }

  if (keys === null) {
    return (
      <div>
        <h1 className="page-title">Keys for {id}</h1>
        <div className="banner banner-warn">
          Your API key cannot list keys for this tenant.{" "}
          <Link href="/login">Switch keys</Link>
        </div>
      </div>
    );
  }

  async function createKeyAction(formData: FormData) {
    "use server";
    const keyName = (formData.get("name") ?? "").toString().trim();
    const role = ((formData.get("role") ?? "viewer").toString() as Role);
    if (!keyName) {
      redirect(`/admin/tenants/${encodeURIComponent(id)}?error=name+required`);
    }
    try {
      const result = await createAPIKey(id, keyName, role);
      revalidatePath(`/admin/tenants/${id}`);
      await setFlash(JSON.stringify({ plaintext: result.plaintext, name: keyName }));
      redirect(`/admin/tenants/${encodeURIComponent(id)}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  async function deleteKeyAction(formData: FormData) {
    "use server";
    const keyId = (formData.get("id") ?? "").toString();
    if (!keyId) return;
    try {
      await deleteAPIKey(keyId);
      revalidatePath(`/admin/tenants/${id}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  async function bulkRotateAction() {
    "use server";
    try {
      const result = await bulkRotateTenantKeys(id, { exceptSelf: true });
      revalidatePath(`/admin/tenants/${id}`);
      const entries: BulkRotationEntry[] = result.results.map((r) => ({
        id: r.key.id,
        name: r.key.name,
        prefix: r.key.prefix,
        plaintext: r.plaintext,
      }));
      // Stash the whole batch in the single-read server-side flash store so the
      // once-only reveal banner renders every fresh plaintext inline — without
      // ever putting the secrets in the URL (GUI-02).
      await setFlash(JSON.stringify({ bulk: entries }));
      redirect(`/admin/tenants/${encodeURIComponent(id)}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  async function rotateKeyAction(formData: FormData) {
    "use server";
    const keyId = (formData.get("id") ?? "").toString();
    const keyName = (formData.get("name") ?? "").toString();
    if (!keyId) return;
    try {
      const result = await rotateAPIKey(keyId);
      revalidatePath(`/admin/tenants/${id}`);
      await setFlash(JSON.stringify({ plaintext: result.plaintext, name: keyName || result.key.name }));
      redirect(`/admin/tenants/${encodeURIComponent(id)}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  async function changeRoleAction(formData: FormData) {
    "use server";
    const keyId = (formData.get("id") ?? "").toString();
    const role = ((formData.get("role") ?? "viewer").toString() as Role);
    if (!keyId) return;
    try {
      await updateAPIKeyRole(keyId, role);
      revalidatePath(`/admin/tenants/${id}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  return (
    <div className="view-enter">
      <div className="breadcrumb">
        <Link href="/admin/tenants">Tenants</Link> · <code>{id}</code>
      </div>
      <div className="page-head">
        <h1 className="page-title">API keys</h1>
        <p className="page-lede">
          Keys for tenant <code>{id}</code>. Roles:{" "}
          <code className="mono">viewer &lt; editor &lt; admin</code> — viewer
          is read-only, editor adds list-keys, admin has full control.
          Plaintext is shown exactly once.
        </p>
      </div>

      {error && (
        <div className="banner banner-error">
          <div className="row gap-2">
            <Icon name="x-circle" size={16} />
            <div>{error}</div>
          </div>
        </div>
      )}
      {plaintext && (
        <div className="banner banner-ok">
          <div className="row gap-2">
            <Icon name="key-round" size={16} />
            <div className="grow">
              <strong>Key minted · {plaintextName}.</strong> Copy it now — it is
              shown exactly once and bcrypt-hashed immediately.
            </div>
          </div>
          <div className="plaintext-box">
            <code>{plaintext}</code>
          </div>
        </div>
      )}
      {bulkRotation && bulkRotation.length > 0 && (
        <div className="banner banner-warn">
          <div className="row gap-2">
            <Icon name="rotate-cw" size={16} />
            <div className="grow">
              <strong>
                Rotated {bulkRotation.length} key
                {bulkRotation.length === 1 ? "" : "s"}.
              </strong>{" "}
              Copy every new secret now — they will never be shown again. Every
              external consumer must be updated before the old secrets die from
              any remaining caches.
            </div>
          </div>
          <ul className="bulk-rotation-list">
            {bulkRotation.map((entry) => (
              <li key={entry.id}>
                <div className="muted txt-xs mb-2">
                  <strong>{entry.name}</strong> · prefix{" "}
                  <code className="mono">{entry.prefix}</code>
                </div>
                <div className="plaintext-box">
                  <code>{entry.plaintext}</code>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="card" style={{ overflow: "hidden" }}>
        <div className="card-head">
          <div className="grow">
            <h3>Keys</h3>
            <div className="sub mono">{id}</div>
          </div>
          {keys.length > 0 && (
            <form action={bulkRotateAction} className="inline">
              <button
                type="submit"
                className="btn btn-outline btn-sm"
                title="Regenerate every key in this tenant atomically. Emergency response to a suspected compromise."
              >
                <Icon name="rotate-cw" size={14} />
                Rotate all
              </button>
            </form>
          )}
          <form action={createKeyAction} className="row gap-2">
            <input
              name="name"
              className="input"
              style={{ width: 150, height: "var(--sr-control-h-sm)" }}
              placeholder="key name"
              required
            />
            <select
              name="role"
              className="select"
              defaultValue="viewer"
              style={{ width: 96, height: "var(--sr-control-h-sm)" }}
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
            <button type="submit" className="btn btn-default btn-sm">
              <Icon name="plus" size={15} />
              Mint key
            </button>
          </form>
        </div>

        {keys.length === 0 ? (
          <div className="empty">No keys for this tenant yet.</div>
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th>name</th>
                <th>prefix</th>
                <th>role</th>
                <th>last used</th>
                <th className="right"></th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td>
                    <div className="col" style={{ gap: 0 }}>
                      <span style={{ fontWeight: 500 }}>{k.name}</span>
                      <span className="muted mono" style={{ fontSize: 10.5 }}>
                        {k.id}
                      </span>
                    </div>
                  </td>
                  <td className="mono" style={{ fontSize: 12 }}>
                    {k.prefix}…
                  </td>
                  <td>
                    <form action={changeRoleAction} className="row gap-2">
                      <input type="hidden" name="id" value={k.id} />
                      <select
                        name="role"
                        className="select"
                        defaultValue={k.role}
                        style={{ width: 96, height: 28, fontSize: 12 }}
                      >
                        {ROLES.map((r) => (
                          <option key={r} value={r}>
                            {r}
                          </option>
                        ))}
                      </select>
                      <button type="submit" className="btn btn-outline btn-xs">
                        Save
                      </button>
                    </form>
                  </td>
                  <td className="muted txt-xs nowrap">{k.last_used ?? "—"}</td>
                  <td>
                    <div
                      className="row gap-2"
                      style={{ justifyContent: "flex-end" }}
                    >
                      <form action={rotateKeyAction} className="inline">
                        <input type="hidden" name="id" value={k.id} />
                        <input type="hidden" name="name" value={k.name} />
                        <button
                          type="submit"
                          className="btn btn-ghost btn-icon btn-xs"
                          title="Regenerate the secret in place"
                        >
                          <Icon name="rotate-cw" size={13} />
                        </button>
                      </form>
                      <form action={deleteKeyAction} className="inline">
                        <input type="hidden" name="id" value={k.id} />
                        <button
                          type="submit"
                          className="btn btn-ghost btn-icon btn-xs"
                          title="Delete key"
                          style={{ color: "var(--sr-danger-fg)" }}
                        >
                          <Icon name="trash" size={13} />
                        </button>
                      </form>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
