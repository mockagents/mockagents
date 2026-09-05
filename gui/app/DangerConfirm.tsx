"use client";

// The one place an irreversible action is confirmed — administrative (UX-07)
// or otherwise. It moved out of app/admin/ when agent deletion started using
// it (X-3): destroying a mock is as irreversible as destroying a tenant, and
// the design's destructive-dialog spec is not an admin-only rule.
//
// The shape is fixed by the design's destructive-dialog spec:
//
//   impact summary → irreversible consequences → exact-name entry → action
//
// and by three rules that are easy to lose in a refactor:
//
//   * the destructive button is never the initially focused control, so a
//     stray Enter cannot fire it;
//   * when a confirmation phrase is required, the button stays disabled until
//     the phrase matches EXACTLY — no trimming, no case folding. Typing the
//     name is the point: it proves the operator read which thing they are
//     about to destroy;
//   * Esc cancels and focus is trapped, so the dialog cannot be dismissed by
//     accident or tabbed out of into the page it is blocking.
//
// The confirm control submits a server action form, so authorization still
// happens on the server; this component only makes the decision deliberate.

import { useEffect, useRef, useState, type ReactNode } from "react";

export interface DangerConfirmProps {
  /** Server action invoked on confirm. */
  action: (formData: FormData) => void | Promise<void>;
  /** Hidden fields the action needs (ids and the like). */
  fields?: Record<string, string>;
  /** The control that opens the dialog. */
  triggerLabel: ReactNode;
  triggerTitle: string;
  /** Extra classes for the trigger, e.g. to make it an icon button. */
  triggerClassName?: string;
  /** Dialog heading — name the specific thing being acted on. */
  title: string;
  /** What exists right now and will be affected. */
  impact: ReactNode;
  /** Each consequence as its own sentence. */
  consequences: string[];
  /** When set, the operator must type this exactly to enable the action. */
  confirmPhrase?: string;
  /** Label for the field asking for the phrase. */
  confirmPhraseLabel?: string;
  confirmLabel: string;
}

export function DangerConfirm({
  action,
  fields = {},
  triggerLabel,
  triggerTitle,
  triggerClassName = "btn btn-ghost btn-xs",
  title,
  impact,
  consequences,
  confirmPhrase,
  confirmPhraseLabel = "Type the exact name to confirm",
  confirmLabel,
}: DangerConfirmProps) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const dialogRef = useRef<HTMLDivElement>(null);
  const firstRef = useRef<HTMLElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  // Exact match. A trimmed or lower-cased comparison would quietly accept
  // "acme-Prod " for "acme-prod", which defeats the purpose of typing it.
  const armed = confirmPhrase === undefined || typed === confirmPhrase;

  useEffect(() => {
    if (!open) return;
    // Focus the safest control in the dialog: the confirmation field when
    // there is one, otherwise Cancel. Never the destructive button.
    firstRef.current?.focus();

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        close();
        return;
      }
      if (e.key !== "Tab") return;
      const root = dialogRef.current;
      if (!root) return;
      const focusable = root.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input, [href], select, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const active = document.activeElement;
      if (e.shiftKey && active === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
    // close is stable for the lifetime of the component.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  function close() {
    setOpen(false);
    setTyped("");
    // Return focus where it came from, so a cancelled action does not dump the
    // operator at the top of the document.
    triggerRef.current?.focus();
  }

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={triggerClassName}
        title={triggerTitle}
        onClick={() => setOpen(true)}
      >
        {triggerLabel}
      </button>

      {open && (
        <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && close()}>
          <div
            className="dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="danger-confirm-title"
            ref={dialogRef}
          >
            <div className="dialog-head">
              <h3 id="danger-confirm-title">{title}</h3>
            </div>
            <div className="dialog-body col gap-3">
              <div className="banner banner-error" role="alert">
                <div className="col gap-2">
                  <div>
                    <strong>Irreversible.</strong> {impact}
                  </div>
                  <ul className="consequences">
                    {consequences.map((c) => (
                      <li key={c}>{c}</li>
                    ))}
                  </ul>
                </div>
              </div>

              <form action={action} className="col gap-3">
                {Object.entries(fields).map(([name, value]) => (
                  <input key={name} type="hidden" name={name} value={value} />
                ))}

                {confirmPhrase !== undefined && (
                  <div className="field">
                    <label htmlFor="danger-confirm-phrase">{confirmPhraseLabel}</label>
                    <input
                      id="danger-confirm-phrase"
                      className="input mono"
                      value={typed}
                      autoComplete="off"
                      spellCheck={false}
                      placeholder={confirmPhrase}
                      onChange={(e) => setTyped(e.target.value)}
                      ref={(el) => {
                        firstRef.current = el;
                      }}
                    />
                  </div>
                )}

                <div className="dialog-foot">
                  <button
                    type="button"
                    className="btn btn-outline btn-sm"
                    onClick={close}
                    ref={(el) => {
                      if (confirmPhrase === undefined) firstRef.current = el;
                    }}
                  >
                    Cancel
                  </button>
                  <button type="submit" className="btn btn-danger btn-sm" disabled={!armed}>
                    {confirmLabel}
                  </button>
                </div>
              </form>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
