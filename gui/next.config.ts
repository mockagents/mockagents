import type { NextConfig } from "next";

import { API_CONTENT_SECURITY_POLICY } from "./lib/csp";

const isProd = process.env.NODE_ENV === "production";

// Baseline security headers for every route (GUI-04/05/06). The page CSP is
// per-request (it carries a script nonce) and is set by proxy.ts; route
// handlers, which never render a document, get the locked-down static policy
// below. See lib/csp.ts.
const common = [
  { key: "X-Frame-Options", value: "DENY" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  // no-referrer is the safest choice: full URLs must never leak via the
  // Referer header (relevant on the key-rotation flows).
  { key: "Referrer-Policy", value: "no-referrer" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
  // HSTS in production only: sent over plain-HTTP localhost in development it
  // would be ignored at best, and pin a developer's browser to HTTPS for
  // localhost at worst. Behind a TLS-terminating proxy the browser sees it on
  // the HTTPS response, which is what matters.
  ...(isProd
    ? [{ key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains" }]
    : []),
];

const config: NextConfig = {
  reactStrictMode: true,
  async headers() {
    return [
      { source: "/:path*", headers: common },
      {
        source: "/api/:path*",
        headers: [{ key: "Content-Security-Policy", value: API_CONTENT_SECURITY_POLICY }],
      },
    ];
  },
};

export default config;
