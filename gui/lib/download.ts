// Client-side file export (UX-03, U3-4).
//
// Release A drafts live in browser memory and nowhere else — handoff §8 keeps a
// durable draft store out of scope until retention and encryption are signed
// off. Export is therefore the ONLY way a draft survives a closed tab, which
// makes it part of the honesty story rather than a convenience: a UI that lets
// someone accumulate an unsaved draft without offering a way out of the browser
// is quietly setting them up to lose it.
//
// Kept out of the component so the naming and the object-URL lifetime are
// testable without a DOM click.

/** A filename that is safe on every platform and still recognisable.
 *
 * Agent names are permissive (the server allows dots, colons and slashes in
 * some tenant-prefixed forms), and a name that reaches the filesystem verbatim
 * can escape the download directory or simply fail to save. Anything outside a
 * conservative set becomes "-", and an empty result falls back rather than
 * producing a dotfile. */
export function draftFilename(agentName: string, now = new Date()): string {
  const safe = agentName.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^[.-]+/, "");
  const stamp = now.toISOString().replace(/[:.]/g, "-").slice(0, 19);
  return `${safe || "agent"}-draft-${stamp}.yaml`;
}

/** Hand the browser a file to save. Returns false when the environment has no
 * object-URL support (jsdom, a locked-down browser), so a caller can say the
 * export failed rather than appearing to succeed. */
export function downloadText(filename: string, text: string): boolean {
  if (typeof URL === "undefined" || typeof URL.createObjectURL !== "function") return false;
  if (typeof document === "undefined") return false;

  // text/yaml rather than application/x-yaml: some browsers offer to open the
  // latter with a helper app instead of saving it.
  const blob = new Blob([text], { type: "text/yaml;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
    return true;
  } finally {
    // Revoked on the next tick: revoking synchronously can cancel the download
    // in browsers that fetch the blob asynchronously after the click.
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }
}
