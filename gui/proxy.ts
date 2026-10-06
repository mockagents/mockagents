// Per-request CSP nonce (Next.js 16 "proxy", formerly middleware).
//
// Next.js reads the nonce from the *request's* Content-Security-Policy header
// while rendering and adds it to every <script> it emits; the same policy is
// then sent on the response. Every page in the console is already dynamically
// rendered (the root layout reads cookies), so a per-request nonce costs no
// static optimisation. See lib/csp.ts for the policy itself.

import { NextResponse, type NextRequest } from "next/server";

import { createNonce, pageContentSecurityPolicy } from "@/lib/csp";

export function proxy(request: NextRequest) {
  const nonce = createNonce();
  const csp = pageContentSecurityPolicy(nonce, process.env.NODE_ENV !== "production");

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", csp);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", csp);
  return response;
}

export const config = {
  matcher: [
    {
      // Pages only. Route handlers (/api/*) get the static API policy from
      // next.config.ts, and build assets need no policy of their own.
      source: "/((?!api/|_next/static|_next/image|favicon.ico).*)",
      // A prefetch response is not executed as a document.
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
