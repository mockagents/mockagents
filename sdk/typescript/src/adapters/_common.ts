// Helpers shared by adapter modules.

export type ServerLike = string | { url: string };

export function resolveBaseUrl(target: ServerLike): string {
  if (typeof target === "string") return target.replace(/\/+$/, "");
  if (typeof target?.url === "string") return target.url.replace(/\/+$/, "");
  throw new TypeError(
    "expected a MockAgentServer or base URL string, got " + typeof target,
  );
}

export async function requireModule<T>(specifier: string, extras: string): Promise<T> {
  try {
    return (await import(specifier)) as T;
  } catch (err) {
    const e = new Error(
      `${specifier} is required for this adapter. Install it with: npm install ${extras}`,
    );
    (e as Error & { cause?: unknown }).cause = err;
    throw e;
  }
}
