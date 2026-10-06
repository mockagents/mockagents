// Navigation guard for the editors (agent, pipeline, /editor).
//
// AgentEditor promises the draft is never destroyed, but a closed tab, a
// reload or a click on a sidebar link used to drop it without a word. While a
// draft is dirty this hook:
//
//   - registers `beforeunload`, so a reload, a tab close, or a full-page
//     navigation gets the browser's own "leave site?" prompt; and
//   - confirms same-origin link clicks. App Router <Link> navigations are
//     client-side and never fire `beforeunload`, so they are intercepted in the
//     capture phase, before React's root listener (and Link's own onClick)
//     sees the event.
//
// Not covered: the browser's back/forward buttons during client-side history
// navigation. The App Router exposes no blocking API for those, and faking one
// by rewriting history is more fragile than the gap. The editors keep their
// manual "Export draft" escape hatch for that case.

import { useEffect } from "react";

export const UNSAVED_CHANGES_MESSAGE =
  "You have unsaved changes. Leave this page and discard them?";

/** Whether a click on `anchor` would navigate this document to another page
 * of the same app (the case `beforeunload` does not cover). */
export function isInAppNavigation(event: MouseEvent, anchor: HTMLAnchorElement): boolean {
  if (event.defaultPrevented) return false;
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
    return false; // new tab / window / download: this page stays open
  }
  if (anchor.target && anchor.target !== "_self") return false;
  if (anchor.hasAttribute("download")) return false;
  let url: URL;
  try {
    url = new URL(anchor.href, window.location.href);
  } catch {
    return false;
  }
  // Another origin is a full navigation, which beforeunload already guards.
  if (url.origin !== window.location.origin) return false;
  // A pure fragment jump on the same page does not unload anything.
  return !(url.pathname === window.location.pathname && url.search === window.location.search);
}

/** Guard against losing unsaved work while `dirty` is true. */
export function useUnsavedChangesGuard(dirty: boolean, message: string = UNSAVED_CHANGES_MESSAGE): void {
  useEffect(() => {
    if (!dirty) return;
    // Set once the user has already agreed to leave, so a full-page navigation
    // from a plain <a> does not ask twice (our confirm, then the browser's).
    let leaving = false;

    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (leaving) return;
      event.preventDefault();
      // Legacy browsers only show the prompt when returnValue is set.
      event.returnValue = "";
    };

    const onClick = (event: MouseEvent) => {
      const target = event.target as Element | null;
      const anchor = target?.closest?.("a[href]") as HTMLAnchorElement | null | undefined;
      if (!anchor || !isInAppNavigation(event, anchor)) return;
      if (window.confirm(message)) {
        leaving = true;
        // The unload prompt for a full navigation fires within this same task;
        // reset afterwards in case the navigation did not unload the editor.
        window.setTimeout(() => {
          leaving = false;
        }, 0);
        return;
      }
      event.preventDefault();
      event.stopPropagation();
    };

    window.addEventListener("beforeunload", onBeforeUnload);
    document.addEventListener("click", onClick, true);
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      document.removeEventListener("click", onClick, true);
    };
  }, [dirty, message]);
}
