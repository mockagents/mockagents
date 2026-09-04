"use client";

// A copyable SDK setting (UX-02: "copyable SDK settings").
//
// The one client island on Overview. Copy feedback is announced rather than
// only shown, and the value stays selectable so copying works even when the
// clipboard API is unavailable (an insecure origin, a locked-down browser) —
// a copy button that silently does nothing is worse than no button.

import { useState } from "react";

export function CopyField({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setState("copied");
    } catch {
      setState("failed");
    }
    setTimeout(() => setState("idle"), 2000);
  }

  return (
    <div className="field">
      <label htmlFor={`copy-${label}`}>{label}</label>
      <div className="row gap-2">
        <input
          id={`copy-${label}`}
          className="input mono"
          readOnly
          value={value}
          onFocus={(e) => e.currentTarget.select()}
        />
        <button type="button" className="btn btn-outline btn-sm" onClick={copy}>
          Copy
        </button>
      </div>
      <p className="hint" role="status">
        {state === "copied"
          ? "Copied to clipboard."
          : state === "failed"
            ? "Could not copy — select the value and copy it manually."
            : (hint ?? " ")}
      </p>
    </div>
  );
}
