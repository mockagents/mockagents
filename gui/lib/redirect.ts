/** Sanitizes the `?next=` return target used by the login flow.
 *
 * The previous guard was `next.startsWith("/") && !next.startsWith("//")`,
 * which admits `/\evil.com`: browsers normalize a backslash to a forward slash
 * in a URL path, so that value is the off-origin `//evil.com` by the time it
 * reaches navigation (audit M-38). Anything else a string check misses —
 * tab/newline separators, `/\/`, an absolute URL — has the same shape.
 *
 * Rather than enumerate the tricks, parse the value the way a browser does and
 * keep it only if it resolves to the SAME origin as the placeholder base. A
 * value that escapes the origin, or that fails to parse at all, falls back to
 * the site root.
 */
const sameOriginBase = "https://mockagents.invalid";

export function safeRedirect(next: string | null | undefined): string {
  if (!next) return "/";
  let parsed: URL;
  try {
    parsed = new URL(next, sameOriginBase);
  } catch {
    return "/";
  }
  if (parsed.origin !== sameOriginBase) return "/";
  const dest = `${parsed.pathname}${parsed.search}${parsed.hash}`;
  // A parsed same-origin URL always yields a rooted path, but keep the shape
  // check so this function cannot return something that navigates off-site
  // even if the parser's behavior changes.
  if (!dest.startsWith("/") || dest.startsWith("//")) return "/";
  return dest;
}
