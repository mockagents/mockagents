import { redirect } from "next/navigation";

import { login } from "@/lib/auth";

type PageProps = {
  searchParams: Promise<{ error?: string; next?: string; burned?: string }>;
};

export default async function LoginPage({ searchParams }: PageProps) {
  const { error, next, burned } = await searchParams;

  async function loginAction(formData: FormData) {
    "use server";
    const result = await login(formData);
    if (!result.ok) {
      const params = new URLSearchParams({ error: result.error ?? "unknown" });
      if (next) params.set("next", next);
      redirect(`/login?${params.toString()}`);
    }
    // Only allow a local path as the redirect target. `startsWith("/")` alone
    // would accept a protocol-relative `//evil.com` (an off-origin absolute
    // URL), so reject a leading `//` too (GUI-08).
    const dest = next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
    redirect(dest);
  }

  return (
    <div className="login-wrap">
      <h1 className="page-title">Sign in</h1>
      <p className="page-lede">
        Paste an API key from <code>mockagents start</code> bootstrap output or{" "}
        <code>POST /api/v1/tenants/&lt;id&gt;/keys</code>. The key is stored in
        an HttpOnly cookie and forwarded on every management-API request.
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>Login failed.</strong> {error}
        </div>
      )}

      {burned === "1" && (
        <div className="banner banner-warn">
          <strong>Session burned.</strong> Your previous API key was
          rotated on the server; the new plaintext was not returned to
          this browser. Recover via an out-of-band channel (a different
          device with an admin credential minting a new key, or the
          CLI bootstrap flow).
        </div>
      )}

      <form action={loginAction} className="login-form">
        <label>
          <span>API key</span>
          <input
            type="password"
            name="key"
            autoComplete="off"
            autoFocus
            required
            placeholder="ma_abc1_..."
          />
        </label>
        <button type="submit" className="btn btn-primary">
          Sign in
        </button>
      </form>

      <p className="hint">
        Running in single-tenant mode? You can skip this page — every
        endpoint is already open. The login flow only matters when the
        server is started with <code>MOCKAGENTS_MULTI_TENANT=1</code>.
      </p>
    </div>
  );
}
