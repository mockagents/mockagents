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
  setTenantQuota,
  updateAPIKeyRole,
} from "@/lib/api";
import { getAuthStatus } from "@/lib/auth";
import { setFlash, takeFlash } from "@/lib/flash";
import { Icon } from "@/lib/icons";

import { DangerConfirm } from "../../DangerConfirm";

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

  // UX-07: quota override. Platform-gated on the server
  // (PUT /api/v1/tenants/{id}/quota), so the form only appears for a credential
  // the server says holds that capability — offering it to a tenant admin would
  // be a control that exists only to 403.
  async function setQuotaAction(formData: FormData) {
    "use server";
    const num = (name: string) => Number((formData.get(name) ?? "0").toString().trim() || "0");
    const limits = {
      rate_per_sec: num("rate_per_sec"),
      rate_burst: num("rate_burst"),
      monthly_spend_usd: num("monthly_spend_usd"),
    };
    if (Object.values(limits).some((v) => !Number.isFinite(v) || v < 0)) {
      redirect(
        `/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(
          "quota values must be non-negative numbers",
        )}`,
      );
    }
    try {
      await setTenantQuota(id, limits);
      revalidatePath(`/admin/tenants/${id}`);
      redirect(`/admin/tenants/${encodeURIComponent(id)}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants/${encodeURIComponent(id)}?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  // Only the SERVER's answer decides whether this control is offered. An
  // unreachable server leaves capabilities empty, and the form then stays
  // hidden rather than being rendered on an assumption.
  const canSetQuota = auth.capabilities.includes("tenants.quota.write");

  return (
    <div className="view-enter">
      <div className="breadcrumb">
        <Link href="/admin/tenants">Tenants</Link> · <code>{id}</code>
      </div>
      <div className="page-head">
        <h1 className="page-title">API keys</h1>
        <p className="page-lede">
          Keys for tenant <code>{id}</code>. Roles rank{" "}
          <code className="mono">viewer &lt; editor &lt; admin</code>.{" "}
          {/* UX-07: viewer is NOT a universally read-only server role (epic
              §8.1). It reads agents and logs, RUNS pipelines against the active
              runtime, and rotates its own key. Calling it read-only here made
              the console describe a permission the server does not enforce. */}
          A viewer reads definitions and logs, runs pipelines and rotates its own
          key; an editor also writes agents and lists keys; an admin manages this
          tenant&apos;s keys. Managing tenants themselves is the platform role,
          which the management API refuses to assign. Plaintext is shown exactly
          once.
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
            <DangerConfirm
              action={bulkRotateAction}
              triggerLabel={
                <>
                  <Icon name="rotate-cw" size={14} />
                  Rotate all
                </>
              }
              triggerTitle="Regenerate every key in this tenant"
              triggerClassName="btn btn-outline btn-sm"
              title={`Rotate all ${keys.length} key${keys.length === 1 ? "" : "s"} in ${id}?`}
              impact={
                <>
                  Every key in this tenant gets a new secret at once. The old secrets stop
                  working the moment this completes, and the new ones are displayed{" "}
                  <strong>exactly once</strong> on the page you land on.
                </>
              }
              consequences={[
                "Every deployed client, CI job and script using one of these keys breaks until it is updated with the new secret.",
                "The new plaintexts are shown once and never again — leaving that page without copying them means minting replacements.",
                "Your own key is excluded so this cannot lock you out of the console.",
                "Rotation cannot be undone or rolled back to the previous secrets.",
              ]}
              confirmPhrase={id}
              confirmPhraseLabel="Type the tenant id to confirm"
              confirmLabel="Rotate every key"
            />
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
                      <DangerConfirm
                        action={rotateKeyAction}
                        fields={{ id: k.id, name: k.name }}
                        triggerLabel={<Icon name="rotate-cw" size={13} />}
                        triggerTitle={`Rotate ${k.name}`}
                        triggerClassName="btn btn-ghost btn-icon btn-xs"
                        title={`Rotate the secret for ${k.name}?`}
                        impact={
                          <>
                            The key keeps its id, name and <code>{k.role}</code> role, but
                            its secret is replaced. The new one is shown{" "}
                            <strong>exactly once</strong>.
                          </>
                        }
                        consequences={[
                          "The current secret stops working immediately — anything using it fails on the next request.",
                          "The replacement is displayed once and is unrecoverable afterwards.",
                          "Rotation cannot be undone; the previous secret is gone.",
                        ]}
                        confirmLabel="Rotate key"
                      />
                      <DangerConfirm
                        action={deleteKeyAction}
                        fields={{ id: k.id }}
                        triggerLabel={<Icon name="trash" size={13} />}
                        triggerTitle={`Delete ${k.name}`}
                        triggerClassName="btn btn-ghost btn-icon btn-xs danger-trigger"
                        title={`Delete key ${k.name}?`}
                        impact={
                          <>
                            Permanently removes key <code>{k.id}</code> (prefix{" "}
                            <code>{k.prefix}</code>, role <code>{k.role}</code>) from this
                            tenant.
                          </>
                        }
                        consequences={[
                          "Any client holding this secret is denied from the next request onward.",
                          "Deletion is not rotation: there is no replacement secret, and nothing to hand a consumer.",
                          "The key cannot be restored — a new one must be minted, with a new id.",
                        ]}
                        confirmPhrase={k.name}
                        confirmPhraseLabel="Type the exact key name to confirm"
                        confirmLabel="Delete key"
                      />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {canSetQuota && (
        <div className="card mt-4">
          <div className="card-head">
            <Icon name="gauge" size={16} />
            <div className="grow">
              <h3>Quota override</h3>
              <div className="sub mono">{id}</div>
            </div>
            <span className="sid">UX-07 · PUT /api/v1/tenants/{"{id}"}/quota · platform</span>
          </div>
          <div className="card-pad col gap-3">
            <div className="banner banner-warn" role="note">
              <div>
                {/* GET /api/v1/quota answers for the CALLER's tenant only —
                    there is no per-tenant read. Pre-filling this form with your
                    own limits, or with zeros, would show one tenant's numbers
                    under another tenant's name. */}
                <strong>This server exposes no per-tenant quota read. </strong>
                The values currently in force for <code>{id}</code> are unknown from here.
                Submitting replaces the whole override — every field below is applied,
                including the ones you leave at 0.
              </div>
            </div>
            <form action={setQuotaAction} className="row gap-3 wrap" style={{ alignItems: "flex-end" }}>
              <div className="field" style={{ width: 150 }}>
                <label htmlFor="rate_per_sec">requests / second</label>
                <input
                  id="rate_per_sec"
                  name="rate_per_sec"
                  className="input mono"
                  type="number"
                  min="0"
                  step="0.1"
                  defaultValue="0"
                />
                <span className="hint">0 = unlimited</span>
              </div>
              <div className="field" style={{ width: 150 }}>
                <label htmlFor="rate_burst">burst</label>
                <input
                  id="rate_burst"
                  name="rate_burst"
                  className="input mono"
                  type="number"
                  min="0"
                  step="1"
                  defaultValue="0"
                />
                <span className="hint">derived from rate when 0</span>
              </div>
              <div className="field" style={{ width: 170 }}>
                <label htmlFor="monthly_spend_usd">monthly spend cap (USD)</label>
                <input
                  id="monthly_spend_usd"
                  name="monthly_spend_usd"
                  className="input mono"
                  type="number"
                  min="0"
                  step="0.01"
                  defaultValue="0"
                />
                <span className="hint">0 = unlimited</span>
              </div>
              <button type="submit" className="btn btn-default btn-sm">
                <Icon name="save" size={15} />
                Set override
              </button>
            </form>
            <p className="hint" style={{ margin: 0 }}>
              Spend is measured against the estimated upstream cost of this tenant&apos;s
              captured requests. Over-rate requests are rejected with 429, over-spend with
              402.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
