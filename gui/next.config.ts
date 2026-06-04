import type { NextConfig } from "next";

const isDev = process.env.NODE_ENV !== "production";

// Baseline security headers for every route (GUI-04/05/06). The console renders
// attacker-influenceable interaction-log content, so a CSP is defense-in-depth
// against any future raw-HTML/script-injection regression, on top of React's
// auto-escaping. `script-src 'unsafe-inline'` is required by Next's inline
// bootstrap; dev additionally needs `'unsafe-eval'` for HMR. Tightening
// `script-src` to a per-request nonce (needs middleware.ts) is a follow-up.
const csp = [
  "default-src 'self'",
  "img-src 'self' data:",
  "style-src 'self' 'unsafe-inline'",
  isDev ? "script-src 'self' 'unsafe-inline' 'unsafe-eval'" : "script-src 'self' 'unsafe-inline'",
  "connect-src 'self'",
  "font-src 'self'",
  "frame-ancestors 'none'",
  "base-uri 'none'",
  "form-action 'self'",
  "object-src 'none'",
].join("; ");

const config: NextConfig = {
  // Server components fetch the MockAgents management API directly;
  // disable static optimization so each page is always rendered against
  // the latest server state.
  experimental: {},
  reactStrictMode: true,
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "Content-Security-Policy", value: csp },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "X-Content-Type-Options", value: "nosniff" },
          // no-referrer is the safest choice: full URLs must never leak via the
          // Referer header (relevant on the key-rotation flows).
          { key: "Referrer-Policy", value: "no-referrer" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
        ],
      },
    ];
  },
};

export default config;
