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

/** HEAD is the live feed's credential probe (LogsConsole calls it after an
 * EventSource error, because EventSource hides the status code).
 *
 * Without this export Next.js answers HEAD by running GET and discarding the
 * body — which opened a real upstream SSE subscription for every probe, the
 * opposite of cheap. Instead this asks the upstream's plain log listing for a
 * single metadata-only row. Both routes sit behind the same authorization
 * floor (route_authz.go: GET /api/v1/logs and GET /api/v1/logs/stream are both
 * open to any authenticated caller), so the status is the answer the stream
 * would have given, and no subscriber is ever created. */
export async function HEAD(req: NextRequest) {
  const blocked = crossSiteForbidden(req);
  if (blocked) return new Response(null, { status: blocked.status });

  const key = await getAuthKey();
  const headers: Record<string, string> = { Accept: "application/json" };
  if (key) headers.Authorization = `Bearer ${key}`;
  let res: Response;
  try {
    res = await fetch(`${getBaseUrl()}/api/v1/logs?limit=1&fields=meta`, {
      headers,
      signal: AbortSignal.timeout(5_000),
      cache: "no-store",
    });
  } catch (err) {
    console.error("logs/stream probe: upstream unreachable:", err);
    return new Response(null, { status: 502 });
  }
  // Drain without reading the row into anything.
  await res.body?.cancel().catch(() => {});
  if (res.status === 401 || res.status === 403) return new Response(null, { status: res.status });
  return new Response(null, { status: res.ok ? 204 : 502 });
}

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

    // UX-04: an expired or revoked credential must not look like a network
    // fault. Collapsing it into 502 made the client reconnect forever against
    // a server that will never accept it, with no way to tell the operator to
    // sign in again. The status is passed through; the body stays generic so
    // no upstream detail leaks to the browser (GUI-07).
    if (upstreamResp.status === 401 || upstreamResp.status === 403) {
      return new Response("not authorized", { status: upstreamResp.status });
    }
    return new Response("upstream request failed", { status: 502 });
  }

  // Next.js does not send the response headers until the first body chunk,
  // and the upstream sends nothing until a log event happens — so on a quiet
  // server the browser's EventSource never saw "open" and the console sat on
  // "reconnecting". An SSE comment line (ignored by EventSource) goes first.
  const reader = upstreamResp.body.getReader();
  let preambleSent = false;
  const body = new ReadableStream<Uint8Array>({
    async pull(controller) {
      if (!preambleSent) {
        preambleSent = true;
        controller.enqueue(new TextEncoder().encode(": connected\n\n"));
        return;
      }
      try {
        const { done, value } = await reader.read();
        if (done) controller.close();
        else controller.enqueue(value);
      } catch (err) {
        controller.error(err);
      }
    },
    // The browser went away: release the upstream subscription too.
    cancel(reason) {
      return reader.cancel(reason);
    },
  });

  return new Response(body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
