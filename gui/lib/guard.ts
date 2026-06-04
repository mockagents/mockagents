import { NextRequest } from "next/server";

/** crossSiteForbidden returns a 403 Response when the request is an explicit
 * cross-site request (`Sec-Fetch-Site: cross-site`), else null.
 *
 * The /api/logs* proxy routes attach the operator's cookie-derived API key to
 * the upstream call, so a cross-site `<img>`/`EventSource`/`fetch` from a
 * malicious page would make the GUI server act as a confused deputy with the
 * victim's credentials (GUI-03). The Sec-Fetch-Site header is set by the browser
 * and cannot be spoofed by page JS. Absent header (very old clients) and
 * same-origin / same-site / none (direct navigation) are allowed for
 * compatibility — the attack vector is exactly the `cross-site` value. */
export function crossSiteForbidden(req: NextRequest): Response | null {
  if (req.headers.get("sec-fetch-site") === "cross-site") {
    return new Response("forbidden: cross-site request", { status: 403 });
  }
  return null;
}
