# Security Review — MockAgents GUI (Next.js 15) — 2026-06-04

**Reviewer:** web-security analyst pass (3 parallel surface audits + synthesis)
**Scope:** `gui/` — the Next.js 15 / React 19 App-Router console (~25 source
files, no third-party UI deps). Lens: XSS / output encoding, CSP & security
headers, the HttpOnly auth-cookie lifecycle, CSRF, SSRF in the same-origin proxy
routes, server→client trust boundary, open redirect.
**Companion to:** `docs/SECURITY-REVIEW.md` (the Go back-end pass).
**Audience:** the developer agent that will fix the items below.

---

## 1. Verdict

**Good posture, no live XSS, no request-controlled SSRF, no secret leakage to
the client.** The console is built the safe way: every value is rendered as
auto-escaped JSX text (or escaped SVG `<text>`), there is **zero
`dangerouslySetInnerHTML` / `eval` / dynamic `<script>`** anywhere, the raw API
key stays **server-side only** (HttpOnly cookie read via `next/headers`, never a
`NEXT_PUBLIC_` var, never passed to a `"use client"` component, displayed only as
an 8-char prefix), and **all mutations are Next 15 Server Actions** — which carry
the framework's Origin/CSRF check.

The findings are almost entirely **missing hardening**: response-security
headers, a cookie flag, and two real but bounded issues (one-time secrets in URLs;
a read-only confused-deputy on the log proxies).

| Severity | Count | IDs |
|----------|------:|-----|
| Critical | 0 | — |
| High | 1 | GUI-01 |
| Medium | 4 | GUI-02, GUI-03, GUI-04, GUI-05 |
| Low | 4 | GUI-06, GUI-07, GUI-08, GUI-09 |
| Info | 1 | GUI-10 |

> **Note for the implementer:** the CSP (GUI-04) interacts with Next.js's inline
> bootstrap scripts — a too-strict `script-src` can blank the app. Any header
> change MUST be verified with `npm run build` + a manual smoke of the pages
> before commit. Start permissive (`'unsafe-inline'` for `script-src`) or use a
> `middleware.ts` nonce.

---

## 2. Action plan

> **Status 2026-06-04:** GUI-01 … GUI-09 **all implemented** (commit on
> `main`). Verified: `npm run typecheck` (tsc) + `npm run build` green; the five
> security headers confirmed emitting at runtime via a `next start` probe of
> `/login`. **GUI-10** (env-URL validation, Info) left as optional hygiene.
> Follow-ups noted: a strict nonce-based CSP (needs `middleware.ts`) and an
> in-browser CSP smoke before production.

- [x] **GUI-01 (High) — done.** Set `secure: true` on the `mockagents_api_key` (and
  `mockagents_role`) cookie. The cookie carries the **raw, bearer-equivalent
  admin API key**, not a session id; without `Secure` the browser will send it
  over plaintext HTTP (SSL-strip / mixed-content / internal-http exposure). Gate
  on env so localhost dev still works: `secure: process.env.NODE_ENV === "production"`.
  Apply to all three `store.set` calls in `gui/lib/auth.ts` (:76, :82, :109).
  **Done when:** the auth + role cookies carry `Secure` in production builds; dev
  on `http://localhost` still logs in.

- [x] **GUI-02 (Medium) — done** (server-side single-read flash store `lib/flash.ts` + token cookie; no secret in any URL, incl. bulk rotation). Stop passing one-time plaintext secrets through
  redirect query strings (`gui/app/account/page.tsx:31`,
  `gui/app/admin/tenants/[id]/page.tsx` mint/bulk/rotate). URLs with the secret
  land in browser history, the `Referer` header, and proxy access logs — the
  in-code "URL is never committed anywhere" comment is incorrect. Surface the
  one-time plaintext via a **single-read HttpOnly flash cookie** (set in the
  Server Action, `delete` on the next render) or a short-lived server-side store
  keyed by an opaque id. **Never** serialize bulk-rotation plaintext into a query
  param. **Done when:** no freshly-minted/rotated plaintext appears in any URL.

- [x] **GUI-03 (Medium) — done** (`lib/guard.ts` `crossSiteForbidden` rejects `Sec-Fetch-Site: cross-site` on both `/api/logs` routes). Add a same-origin check to the credentialed proxy
  routes (`gui/app/api/logs/route.ts`, `gui/app/api/logs/stream/route.ts`). They
  re-attach the operator's cookie-derived key to the upstream on **any** request
  carrying the cookie, including cross-site GETs (`<img>`, `EventSource`,
  `fetch`) — a read-only confused-deputy/CSRF-on-read. Reject when
  `req.headers.get("sec-fetch-site")` is not `same-origin`/`same-site` (and/or
  require `getAuthStatus()` non-null). **Done when:** a cross-site GET to either
  proxy route is refused before the upstream call.

- [x] **GUI-04 + GUI-05 + GUI-06 — done** (`next.config.ts` `headers()` emits CSP + `X-Frame-Options: DENY` + `nosniff` + `Referrer-Policy: no-referrer` + `Permissions-Policy` on all routes; dev adds `'unsafe-eval'` for HMR; confirmed at runtime). Add an `async headers()` block to
  `gui/next.config.ts` returning, for `/:path*`:
  - **CSP** (GUI-04, Medium): `default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'` (tighten `script-src` to a nonce later via middleware).
  - **Clickjacking** (GUI-05, Medium): `X-Frame-Options: DENY` (+ the `frame-ancestors 'none'` above).
  - **Hardening** (GUI-06, Low): `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` (important — secrets can transit the URL until GUI-02 lands), `Permissions-Policy: camera=(), microphone=(), geolocation=()`.
  **Done when:** all routes carry the headers and `npm run build` + a page smoke pass.

- [x] **GUI-07 (Low) — done** (both proxy routes now log upstream detail server-side and return a generic message + 502). Don't relay the upstream error status/body verbatim to
  the browser (`gui/app/api/logs/route.ts:27-30`,
  `gui/app/api/logs/stream/route.ts:40-45`, `gui/lib/api.ts:127-130`). Return a
  generic message + 502; log the raw upstream detail server-side only (mirrors
  the back-end SEC-02 / F-TN-006 fix). **Done when:** proxy failures show a
  generic client message, full detail only in server logs.

- [x] **GUI-08 (Low) — done** (`login/page.tsx` now rejects a leading `//`). Tighten the post-login open-redirect guard
  (`gui/app/login/page.tsx:20`): `next.startsWith("/")` currently lets a
  **protocol-relative** `//evil.com` through. Use
  `next.startsWith("/") && !next.startsWith("//")`. **Done when:** `?next=//evil.com`
  redirects to `/`, not off-origin.

- [x] **GUI-09 (Low) — done** (auth + role cookies now `SameSite=Strict` via `sessionCookieOptions`). Change the auth/role cookie to `sameSite: "strict"`. It's
  a long-lived raw admin credential and the GUI has no cross-site inbound deep-link
  that needs the cookie on first navigation (`/login` sets it fresh). Pairs with
  GUI-01 in `gui/lib/auth.ts`. **Done when:** the cookie is `SameSite=Strict`.

### Info
- [ ] **GUI-10** — Optionally validate `MOCKAGENTS_API_URL` parses as an http(s)
  URL at startup (`gui/lib/api.ts:87-89`). The upstream host is a fixed env var
  (not request-derived → no client SSRF), so this is deployment hygiene only.

---

## 3. Findings detail

### GUI-01 — Auth cookie set without `Secure` (High)
- **OWASP:** A02 Cryptographic Failures / A05 Misconfiguration
- **Where:** `gui/lib/auth.ts:76, 82, 109`
- **Evidence:** `store.set(AUTH_COOKIE, raw, { httpOnly: true, sameSite: "lax", path: "/", maxAge: thirtyDays })` — no `secure`.
- **Impact:** the cookie value **is** the raw management API key. Without `Secure`
  the browser transmits it over any `http://` request to the GUI origin
  (SSL-strip, mixed-content, internal plain-HTTP), exposing the admin credential.
- **Fix:** `secure: process.env.NODE_ENV === "production"` on all three sets.

### GUI-02 — One-time plaintext secrets in redirect URLs (Medium)
- **OWASP:** A09 / Sensitive Data Exposure
- **Where:** `gui/app/account/page.tsx:31`; `gui/app/admin/tenants/[id]/page.tsx` (mint ~:105-109, bulk ~:150-151, rotate ~:168-172)
- **Evidence:** `redirect(\`/account?plaintext=${encodeURIComponent(result.plaintext)}\`)`; bulk rotate serializes every key's plaintext into `?bulk=...`.
- **Impact:** secrets persist in browser history, `Referer`, and proxy/access logs.
- **Fix:** single-read HttpOnly flash cookie or short-lived server store; never bulk plaintext in a URL.

### GUI-03 — Credentialed proxy confused-deputy / CSRF-on-read (Medium)
- **OWASP:** A01 Broken Access Control
- **Where:** `gui/app/api/logs/route.ts`, `gui/app/api/logs/stream/route.ts`
- **Evidence:** both GET handlers read the cookie key and attach `Authorization: Bearer ${key}` upstream with no caller/origin check.
- **Impact:** a cross-site page can make the GUI server issue authenticated
  upstream log reads using the victim's session (the browser can't read the
  cross-origin response, but the server still acts). Bounded to read-only `/logs`.
- **Fix:** require `Sec-Fetch-Site: same-origin` (and/or a non-null session) before proxying.

### GUI-04 / GUI-05 / GUI-06 — Missing security headers (Medium / Medium / Low)
- **OWASP:** A05 Misconfiguration
- **Where:** `gui/next.config.ts` (no `headers()`; no `middleware.ts`)
- **Evidence:** config sets only `experimental`/`reactStrictMode`; no CSP, no
  `X-Frame-Options`/`frame-ancestors`, no `nosniff`/`Referrer-Policy`/`Permissions-Policy`.
- **Impact:** no defense-in-depth for the log-rendering console (CSP), admin
  actions are framable (clickjacking), and full URLs (which can carry secrets
  until GUI-02) can leak via `Referer`.
- **Fix:** one `headers()` block (see action plan), verified with `npm run build`.

### GUI-07 — Upstream error body relayed to the browser (Low)
- **OWASP:** A05 / Information Disclosure
- **Where:** `gui/app/api/logs/route.ts:27-30`, `gui/app/api/logs/stream/route.ts:40-45`, `gui/lib/api.ts:127-130`
- **Evidence:** the stream route returns `await upstreamResp.text()` + upstream status verbatim; `fetchJSON` includes `body.slice(0,200)` of the upstream error in the thrown message, echoed by `logs/route.ts`.
- **Impact:** backend diagnostics (driver errors, internal paths) reach the browser. Low (mock diagnostics, not secrets), but widens recon.
- **Fix:** generic client message + 502; log raw detail server-side only.

### GUI-08 — Protocol-relative open-redirect bypass (Low)
- **OWASP:** A01 / Open Redirect
- **Where:** `gui/app/login/page.tsx:20`
- **Evidence:** `next && next.startsWith("/") ? next : "/"` — `//evil.com` passes (browsers treat `//host` as scheme-relative absolute).
- **Fix:** `next.startsWith("/") && !next.startsWith("//")`.

### GUI-09 — SameSite=Lax on a raw-credential cookie (Low)
- **OWASP:** A01 (CSRF surface, defense-in-depth)
- **Where:** `gui/lib/auth.ts:78, 84, 111`
- **Impact:** mutations are already covered by Server Actions, so Lax is adequate
  today; `Strict` is the safer posture for a long-lived admin credential.
- **Fix:** `sameSite: "strict"`.

---

## 4. Verified-safe — do not regress

- **No XSS sinks:** zero `dangerouslySetInnerHTML` / `eval` / `new Function` /
  dynamic `<script>` in `gui/`. All data — log bodies, request paths, agent
  names, audit details, DAG node/edge labels — renders as auto-escaped JSX text
  (`logs/[id]/page.tsx` uses `<pre>{prettyOrRaw(...)}</pre>`) or escaped SVG
  `<text>{...}</text>`. URL sinks use `encodeURIComponent`.
- **Key is server-only:** the raw key lives only in the HttpOnly cookie, read via
  `next/headers` in Server Components / Actions / route handlers; injected as
  `Authorization: Bearer` only in server-side `fetch`. No `NEXT_PUBLIC_*`. The two
  `"use client"` components receive only non-secret props. `getAuthStatus` returns
  only `key.slice(0,8)`.
- **CSRF:** every mutation (login, logout, burn, rotate, create/delete tenant,
  mint/delete/rotate key, bulk rotate, change role, validate YAML) is a Next 15
  Server Action (framework Origin check). The `/api/logs*` routes are GET-only.
- **SSRF:** upstream base is the fixed env var `MOCKAGENTS_API_URL`; only the
  literal `/api/v1/...` path + client query params are forwarded — no
  request-controlled host.
- **Login:** pre-validates the key against `/api/v1/tenants` before setting the
  cookie; raw key not logged; base open-redirect guard present (tightened by GUI-08).

---

## 5. Not covered

- Dependency audit of the GUI's npm tree (`npm audit` / the Next.js advisory
  feed) — run separately; pin/upgrade `next@^15` to the latest patch.
- Runtime CSP nonce strategy (needs `middleware.ts`) if a strict `script-src`
  without `'unsafe-inline'` is desired.
- Authenticated-session fixation / concurrent-session limits (out of scope for a
  single-operator ops console).
