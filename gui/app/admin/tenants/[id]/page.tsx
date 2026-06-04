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
    <div>
      <div className="breadcrumb">
        <Link href="/admin/tenants">Tenants</Link> · <code>{id}</code>
      </div>
      <h1 className="page-title">API keys</h1>
      <p className="page-lede">
        Keys for tenant <code>{id}</code>. Roles: <strong>viewer</strong>{" "}
        (read-only), <strong>editor</strong> (read + list-keys), <strong>admin</strong>{" "}
        (full control).
      </p>

      {error && <div className="banner banner-error">{error}</div>}
      {plaintext && (
        <div className="banner banner-warn">
          <strong>Copy this now — it will never be shown again.</strong>
          <div className="plaintext-box">
            <code>{plaintext}</code>
          </div>
          <span className="muted">Key name: {plaintextName}</span>
        </div>
      )}
      {bulkRotation && bulkRotation.length > 0 && (
        <div className="banner banner-warn">
          <strong>
            Rotated {bulkRotation.length} key{bulkRotation.length === 1 ? "" : "s"}.
            Copy every new secret now — they will never be shown again.
          </strong>
          <ul className="bulk-rotation-list">
            {bulkRotation.map((entry) => (
              <li key={entry.id}>
                <div className="muted">
                  <strong>{entry.name}</strong> · prefix <code>{entry.prefix}</code>
                </div>
                <div className="plaintext-box">
                  <code>{entry.plaintext}</code>
                </div>
              </li>
            ))}
          </ul>
          <span className="muted">
            Every external consumer of these keys must be updated before the
            old secrets fully die from any remaining caches.
          </span>
        </div>
      )}

      <div className="inline-form">
        <form action={createKeyAction} className="inline">
          <input name="name" placeholder="key name" required />{" "}
          <select name="role" defaultValue="viewer">
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>{" "}
          <button type="submit" className="btn btn-primary">
            Mint key
          </button>
        </form>
        {keys.length > 0 && (
          <form action={bulkRotateAction} className="inline">
            <button
              type="submit"
              className="btn btn-danger"
              title="Regenerate every key in this tenant atomically. Emergency response to a suspected compromise."
            >
              Rotate all keys
            </button>
          </form>
        )}
      </div>

      {keys.length === 0 ? (
        <p className="muted">No keys yet.</p>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Prefix</th>
              <th>Role</th>
              <th>Created</th>
              <th>Last used</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id}>
                <td>
                  <code className="muted">{k.id}</code>
                </td>
                <td>{k.name}</td>
                <td>
                  <code>{k.prefix}…</code>
                </td>
                <td>
                  <form action={changeRoleAction} className="inline">
                    <input type="hidden" name="id" value={k.id} />
                    <select name="role" defaultValue={k.role}>
                      {ROLES.map((r) => (
                        <option key={r} value={r}>
                          {r}
                        </option>
                      ))}
                    </select>
                    <button type="submit" className="btn btn-xsmall">
                      Save
                    </button>
                  </form>
                </td>
                <td className="muted">{k.created_at}</td>
                <td className="muted">{k.last_used ?? "—"}</td>
                <td>
                  <form action={rotateKeyAction} className="inline">
                    <input type="hidden" name="id" value={k.id} />
                    <input type="hidden" name="name" value={k.name} />
                    <button type="submit" className="btn btn-xsmall" title="Regenerate the secret in place">
                      Rotate
                    </button>
                  </form>{" "}
                  <form action={deleteKeyAction} className="inline">
                    <input type="hidden" name="id" value={k.id} />
                    <button type="submit" className="btn btn-danger">
                      Delete
                    </button>
                  </form>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
