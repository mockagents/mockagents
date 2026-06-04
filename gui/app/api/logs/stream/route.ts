// Same-origin SSE proxy for the live log feed. The browser cannot
// send an Authorization header on an EventSource connection, so we
// proxy through this Next.js route — server-side we read the
// auth cookie and forward it as a Bearer token when hitting the
// upstream /api/v1/logs/stream endpoint.
//
// The response body is piped straight through without re-framing so
// every `event:` / `data:` line the backend emits reaches the client
// intact. Disconnects propagate both ways: the client closes the
// EventSource → AbortController → upstream request cancels.

import { NextRequest } from "next/server";

import { getAuthKey, getBaseUrl } from "@/lib/api";
import { crossSiteForbidden } from "@/lib/guard";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  // This route attaches the operator's cookie-derived key upstream; refuse
  // cross-site callers so it can't be used as a confused deputy (GUI-03).
  const blocked = crossSiteForbidden(req);
  if (blocked) return blocked;

  const upstream = `${getBaseUrl()}/api/v1/logs/stream`;
  const key = await getAuthKey();
  const headers: Record<string, string> = { Accept: "text/event-stream" };
  if (key) headers.Authorization = `Bearer ${key}`;

  // Tie upstream lifetime to the browser's EventSource. When the
  // client aborts, req.signal fires and the fetch cancels — that
  // closes the backend handler's request context on the Go side.
  let upstreamResp: Response;
  try {
    upstreamResp = await fetch(upstream, {
      headers,
      signal: req.signal,
      // next/fetch caches by default; streams must opt out.
      cache: "no-store",
    });
  } catch (err) {
    // Log detail server-side, return a generic message to the browser (GUI-07).
    console.error("logs/stream proxy: upstream unreachable:", err);
    return new Response("upstream request failed", { status: 502 });
  }

  if (!upstreamResp.ok || !upstreamResp.body) {
    const detail = await upstreamResp.text().catch(() => "");
    console.error(`logs/stream proxy: upstream ${upstreamResp.status}: ${detail.slice(0, 500)}`);
    return new Response("upstream request failed", { status: 502 });
  }

  return new Response(upstreamResp.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
