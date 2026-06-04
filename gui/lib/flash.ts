// Server-only one-time "flash" store for surfacing freshly-minted API-key
// plaintext to the operator WITHOUT putting it in the URL (GUI-02). A Server
// Action stashes the secret server-side under a random token and sets only the
// small, HttpOnly token in a cookie; the next page render reads the token,
// pulls the secret exactly once, and the in-memory entry is deleted. The secret
// therefore never enters the URL (no leak via history / Referer / proxy logs)
// and is never readable from client JS.
//
// Single-process assumption: the store is in-memory, matching the single-
// instance ops-console deployment model (same as the Go server). A multi-replica
// GUI would need a shared store; the graceful fallback is simply "secret not
// shown — rotate again".
//
// This is a plain server-only module (not a "use server" action file): the
// cookie mutation in setFlash is permitted because it is *called from* a Server
// Action, and leaving the file un-marked avoids exposing these helpers as
// client-callable RPC endpoints.

import { cookies } from "next/headers";

const FLASH_COOKIE = "mockagents_flash";
const TTL_MS = 2 * 60 * 1000; // 2 minutes

interface Entry {
  value: string;
  expires: number;
}

const store = new Map<string, Entry>();

function sweep(now: number): void {
  for (const [k, e] of store) {
    if (e.expires <= now) store.delete(k);
  }
}

/** Stash a one-time payload server-side and set the token cookie. Must be called
 * from a Server Action or Route Handler (cookie mutation is only allowed there).
 * The value is an opaque string — callers JSON-encode richer payloads. */
export async function setFlash(value: string): Promise<void> {
  const now = Date.now();
  sweep(now);
  const token = globalThis.crypto.randomUUID();
  store.set(token, { value, expires: now + TTL_MS });
  const jar = await cookies();
  jar.set(FLASH_COOKIE, token, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: 120,
  });
}

/** Read and consume the one-time payload (single-read). Returns "" when absent
 * or expired. Safe to call during a Server Component render: it only reads the
 * cookie and mutates the in-memory map — it never mutates the cookie store
 * (which Next.js forbids during render). The token cookie is left to expire on
 * its own; after the first read its map entry is gone, so a refresh shows
 * nothing. */
export async function takeFlash(): Promise<string> {
  const jar = await cookies();
  const token = jar.get(FLASH_COOKIE)?.value;
  if (!token) return "";
  const entry = store.get(token);
  if (!entry) return "";
  store.delete(token); // single-read
  if (Date.now() > entry.expires) return "";
  return entry.value;
}
