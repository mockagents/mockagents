"use client";

// UX-03 slice B: safe authoring for an existing agent.
//
// The rules this component exists to honour:
//
//   - The draft is never destroyed. Not by a conflict, not by a denied write,
//     not by a failed one. Whatever the server says, the text the user typed is
//     still in the box.
//   - Nothing is applied without being previewed. Apply is reachable only after
//     the change has been shown as a diff against what is actually stored.
//   - A save is not reported as durable unless the server said it was. A
//     runtime-only save says so, because a restart loses it.
//   - An unknown outcome is reported as unknown, and never auto-retried.
//
// Visual vocabulary is the existing console's (cards, banners, the editor
// gutter from /editor). It has NOT been reconciled against the approved
// Reliability Workbench design, which was not reachable from this environment.

import Link from "next/link";
import { useMemo, useState, useTransition } from "react";

import type { ConditionalSaveResult, ValidateResult, ValidationError } from "@/lib/api";
import { collapseUnchanged, diffLines } from "@/lib/diff";
import { Icon } from "@/lib/icons";

import { GuidedForm } from "./GuidedForm";

export interface AgentEditorProps {
  name: string;
  /** Canonical YAML as loaded from the server. */
  original: string;
  /** Revision the document was loaded at, echoed back as If-Match. */
  revision: string;
  /** True when the caller may actually write. A viewer sees a disabled Apply
   * with a reason — never a broken-looking screen. */
  canWrite: boolean;
  validateAction: (yaml: string) => Promise<ValidateResult>;
  saveAction: (yaml: string, ifMatch: string) => Promise<ConditionalSaveResult>;
  reloadAction: () => Promise<{ yaml: string; revision: string } | null>;
}

type Phase = "editing" | "previewing";
type Mode = "form" | "yaml";

export function AgentEditor({
  name,
  original,
  revision,
  canWrite,
  validateAction,
  saveAction,
  reloadAction,
}: AgentEditorProps) {
  // `base` is what the server holds; `draft` is what the user has. They move
  // independently — that separation is what lets a conflict be resolved without
  // discarding either.
  const [base, setBase] = useState(original);
  const [baseRevision, setBaseRevision] = useState(revision);
  const [draft, setDraft] = useState(original);

  const [phase, setPhase] = useState<Phase>("editing");
  // Form and YAML are two views of ONE document. Switching between them never
  // transforms the text, so a round trip through the form cannot reformat or
  // drop anything.
  const [mode, setMode] = useState<Mode>("form");
  const [validation, setValidation] = useState<ValidateResult | null>(null);
  const [result, setResult] = useState<ConditionalSaveResult | null>(null);
  const [conflict, setConflict] = useState<{ message: string; currentRevision?: string } | null>(
    null,
  );
  const [isPending, startTransition] = useTransition();

  const dirty = draft !== base;
  const lines = useMemo(() => draft.split("\n"), [draft]);
  const diff = useMemo(() => diffLines(base, draft), [base, draft]);

  const errorLines = useMemo(() => {
    const errs: ValidationError[] = [
      ...(validation?.errors ?? []),
      ...(result?.status === "invalid" ? result.errors : []),
    ];
    return new Set(errs.map((e) => e.line).filter(Boolean));
  }, [validation, result]);

  function onEdit(next: string) {
    setDraft(next);
    // Any feedback describes the document that produced it; once the text
    // moves, that feedback is stale and must not linger next to new content.
    setValidation(null);
    setResult(null);
    setPhase("editing");
  }

  function onValidate() {
    startTransition(async () => {
      setResult(null);
      setValidation(await validateAction(draft));
    });
  }

  function onPreview() {
    startTransition(async () => {
      const v = await validateAction(draft);
      setValidation(v);
      setResult(null);
      // Previewing an invalid document would invite applying it.
      if (v.ok) setPhase("previewing");
    });
  }

  function onApply() {
    startTransition(async () => {
      const r = await saveAction(draft, baseRevision);
      setResult(r);
      if (r.status === "ok") {
        // The draft is now what the server holds, so the diff legitimately
        // empties. The revision advances so a follow-up edit stays conditional.
        setBase(draft);
        setBaseRevision(r.revision);
        setPhase("editing");
        setConflict(null);
      } else if (r.status === "conflict") {
        // Do NOT touch the draft. The user's work is the only copy of their
        // intent; the server's copy is recoverable at any time.
        // The server's revision comes back on the 412 and is the whole point
        // of the card: without it the operator cannot tell what they are being
        // asked to reconcile against.
        setConflict({ message: r.message, currentRevision: r.currentRevision });
        setPhase("editing");
      }
    });
  }

  /** Fetch the server's current version. This replaces the BASE only — the
   * draft is untouched, so the diff then shows the user exactly how their work
   * differs from what landed while they were editing. */
  function onReloadBase() {
    startTransition(async () => {
      const fresh = await reloadAction();
      if (!fresh) {
        setConflict({
          message:
            "The agent no longer exists on the server. Your draft is kept — " +
            "use Save as new to recreate it.",
        });
        return;
      }
      setBase(fresh.yaml);
      setBaseRevision(fresh.revision);
      setConflict(null);
      setResult(null);
      setPhase("previewing");
    });
  }

  /** Discard local work and return to the server's version. Explicit, and
   * never automatic. */
  function onDiscardDraft() {
    setDraft(base);
    setValidation(null);
    setResult(null);
    setConflict(null);
    setPhase("editing");
  }

  return (
    <div className="view-enter">
      <div className="breadcrumb">
        <Link href="/">Agents</Link> · <Link href={`/agents/${encodeURIComponent(name)}`}>{name}</Link> ·
        Edit
      </div>

      <div className="head-row page-head">
        <div className="grow">
          <h1 className="page-title">Edit {name}</h1>
          <p className="page-lede">
            This is the <strong>canonical</strong> form of the definition the server is
            running. Comments and formatting from the original file are not shown here,
            and applying a change rewrites the file in this form.
          </p>
        </div>
        <div className="row gap-2">
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={onValidate}
            disabled={isPending}
          >
            <Icon name="check-circle" size={15} />
            {isPending ? "Working…" : "Validate"}
          </button>
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={onPreview}
            disabled={isPending || !dirty}
            title={dirty ? undefined : "No changes to review yet"}
          >
            <Icon name="file-code" size={15} />
            Review changes
          </button>
          <button
            type="button"
            className="btn btn-default btn-sm"
            onClick={onApply}
            disabled={isPending || !dirty || phase !== "previewing" || !canWrite}
            title={
              !canWrite
                ? "Changing agents needs the editor role. Your draft is still yours to export."
                : phase !== "previewing"
                  ? "Review the changes before applying them"
                  : undefined
            }
          >
            <Icon name="save" size={15} />
            Apply
          </button>
        </div>
      </div>

      {!canWrite && (
        <div className="banner banner-warn" role="status">
          <strong>Read-only for your role.</strong> You can edit and validate here, but
          applying a change needs the <code>editor</code> role. Nothing is broken — the
          server is refusing the write, not failing.
        </div>
      )}

      {conflict && (
        <ConflictBanner
          message={conflict.message}
          currentRevision={conflict.currentRevision}
          baseRevision={baseRevision}
          pending={isPending}
          onReloadBase={onReloadBase}
          onDiscardDraft={onDiscardDraft}
        />
      )}

      {result && <ResultBanner result={result} name={name} />}

      <div className="grid" style={{ gridTemplateColumns: "1.3fr 1fr", alignItems: "start" }}>
        <div className="card" style={{ overflow: "hidden" }}>
          <div className="card-head">
            <Icon name="file-code" size={15} />
            <div className="grow">
              <h3 className="mono">{name}.yaml</h3>
            </div>
            <span className="tag">{dirty ? "modified" : "unchanged"}</span>
            <span className="tag">{lines.length} lines</span>
          </div>

          {/* One document, two views. Both edit the same text. */}
          <div className="tabs" role="tablist" aria-label="Editing mode">
            <button
              type="button"
              role="tab"
              id="tab-form"
              aria-selected={mode === "form"}
              aria-controls="panel-form"
              className={"tab" + (mode === "form" ? " active" : "")}
              onClick={() => setMode("form")}
            >
              Form
            </button>
            <button
              type="button"
              role="tab"
              id="tab-yaml"
              aria-selected={mode === "yaml"}
              aria-controls="panel-yaml"
              className={"tab" + (mode === "yaml" ? " active" : "")}
              onClick={() => setMode("yaml")}
            >
              YAML
            </button>
          </div>

          {mode === "form" ? (
            <div
              className="card-pad"
              role="tabpanel"
              id="panel-form"
              aria-labelledby="tab-form"
            >
              <GuidedForm yaml={draft} onChange={onEdit} disabled={!canWrite} />
            </div>
          ) : (
            <div className="editor-grid" role="tabpanel" id="panel-yaml" aria-labelledby="tab-yaml">
              <pre className="editor-gutter" aria-hidden="true">
                {lines.map((_, i) => (
                  <span
                    key={i}
                    style={
                      errorLines.has(i + 1)
                        ? { color: "var(--sr-danger-fg)", display: "block" }
                        : { display: "block" }
                    }
                  >
                    {i + 1}
                  </span>
                ))}
              </pre>
              <textarea
                className="editor-textarea"
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="off"
                value={draft}
                onChange={(e) => onEdit(e.target.value)}
                aria-label={`YAML definition for ${name}`}
              />
            </div>
          )}
        </div>

        <div className="card card-pad">
          <div className="eyebrow mb-3">
            {phase === "previewing" ? "changes to apply" : "validation"}
          </div>
          {phase === "previewing" ? (
            <DiffPanel diff={diff} />
          ) : (
            <ValidationPanel validation={validation} pending={isPending} dirty={dirty} />
          )}
          <p className="hint" style={{ marginTop: 12 }}>
            Loaded at revision <code className="mono">{baseRevision.slice(0, 12) || "unknown"}…</code>
          </p>
        </div>
      </div>
    </div>
  );
}

/** Shown when a conditional write was refused because the agent moved (U3-2).
 *
 * The design specifies `role="alertdialog"`. This stays a non-modal
 * `role="alert"` banner deliberately: `alertdialog` denotes a modal with
 * trapped focus, and announcing a modal that does not exist is worse for a
 * screen-reader user than announcing an alert that does. What the design is
 * actually right about — and what was missing — is the CONTENT: which revision
 * the server holds, which one the draft was based on, and the fact that a
 * conditional write is not a lock. */
function ConflictBanner({
  message,
  currentRevision,
  baseRevision,
  pending,
  onReloadBase,
  onDiscardDraft,
}: {
  message: string;
  currentRevision?: string;
  baseRevision: string;
  pending: boolean;
  onReloadBase: () => void;
  onDiscardDraft: () => void;
}) {
  return (
    // assertive: the user just pressed Apply and needs to know it did not land.
    <div className="banner banner-error" role="alert" aria-label="edit conflict">
      <div className="col gap-3">
        <div className="row gap-2">
          <Icon name="x-circle" size={16} />
          <div>
            <strong>Not applied — the agent changed since you loaded it.</strong> {message}
          </div>
        </div>

        {/* Both versions, side by side and named. "Something changed" is not
            actionable; "yours is based on a41f, the server is on c1d9" is. */}
        <div className="grid" style={{ gridTemplateColumns: "1fr 1fr", gap: 12 }}>
          <div className="receipt-line">
            <div className="eyebrow">on the server now</div>
            <div className="mono txt-sm">{shortRev(currentRevision) ?? "unknown"}</div>
            {!currentRevision && (
              <div className="muted txt-xs">
                The server did not name a revision, so this is unknown — not unchanged.
              </div>
            )}
          </div>
          <div className="receipt-line">
            <div className="eyebrow">your draft is based on</div>
            <div className="mono txt-sm">{shortRev(baseRevision) ?? "unknown"}</div>
            <div className="muted txt-xs">kept in this browser session</div>
          </div>
        </div>

        <p className="txt-sm">
          <strong>Both versions are preserved.</strong> Your edits are untouched. Load the
          server&apos;s current version to see how it differs from your draft, then re-apply
          deliberately — nothing is merged for you.
        </p>

        {/* Binding disclosure (handoff §8): the conditional route is additive,
            so it constrains this editor and other conditional writers — not
            every writer that exists. Claiming otherwise would promise a lock
            the server does not implement. */}
        <p className="hint">
          This check is a precondition, not a lock. A client that writes without{" "}
          <code className="mono">If-Match</code> — an older SDK, a script, a direct{" "}
          <code className="mono">PUT</code> — can still overwrite this agent without ever
          seeing this message.
        </p>

        <div className="row gap-2">
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={onReloadBase}
            disabled={pending}
          >
            Compare with current
          </button>
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={onDiscardDraft}
            disabled={pending}
          >
            Discard my changes
          </button>
        </div>
      </div>
    </div>
  );
}

/** Revisions are long hashes. Twelve characters is enough to compare two by eye
 * without wrapping; the full value is never something anyone retypes. */
function shortRev(revision?: string): string | null {
  if (!revision) return null;
  return revision.length > 12 ? revision.slice(0, 12) + "…" : revision;
}

/** The post-apply receipt (U3-1).
 *
 * A receipt rather than a toast, because the two facts an operator needs after
 * a write are not "it worked": they are WHICH version is now running, and
 * whether it survives a restart. The revision is the handle on both — it is
 * what the next If-Match is based on, and what an audit entry names.
 *
 * This renders only after the server has confirmed. There is no optimistic
 * state anywhere in this flow, by design. */
function SaveReceipt({
  result,
  name,
}: {
  result: Extract<ConditionalSaveResult, { status: "ok" }>;
  name: string;
}) {
  const persisted = result.persisted;
  return (
    <div className={"card " + (persisted ? "" : "card-warn")}>
      <div className="card-head">
        <Icon name={persisted ? "check-circle" : "alert-triangle"} size={15} />
        <div className="grow">
          <h3>Save receipt</h3>
        </div>
        <span className={"badge " + (persisted ? "badge-ok" : "badge-warn")}>
          {persisted ? "persisted" : "runtime-only"}
        </span>
      </div>
      <div className="card-pad col gap-3">
        <div
          className={"banner " + (persisted ? "banner-ok" : "banner-warn")}
          role={persisted ? "status" : "alert"}
        >
          {persisted ? (
            <>
              <strong>{result.created ? "Created and persisted." : "Applied and persisted."}</strong>{" "}
              <code className="mono">{name}</code> is serving immediately and was written to
              disk. Confirmed by the server — this appears only after confirmation, never as
              an optimistic toast.
            </>
          ) : (
            <>
              {/* The distinction this exists for: live and durable are not the
                  same state, and only one of them survives a restart. */}
              <strong>Active in the runtime, but NOT written to disk.</strong>{" "}
              <code className="mono">{name}</code> is serving immediately, and the change is
              lost when this server restarts. Export the draft or configure an agents
              directory — do not assume durability.
            </>
          )}
        </div>

        <dl className="kv">
          <dt>new revision</dt>
          <dd className="mono">{shortRev(result.revision) ?? "not reported"}</dd>
          <dt>runtime</dt>
          <dd>
            <span className="badge badge-ok">active</span>
          </dd>
          <dt>file</dt>
          <dd>
            {persisted ? (
              <span className="mono">{result.file ?? "written"}</span>
            ) : (
              <span className="badge badge-warn">not written</span>
            )}
          </dd>
          <dt>audit</dt>
          {/* Not invented: the write API does not return an audit id, and
              correlating one needs GET /api/v1/audit, which is admin-floored. */}
          <dd className="muted txt-sm">
            recorded server-side; this response carries no reference to it
          </dd>
        </dl>

        <p className="hint">
          Later edits from this tab are conditional on{" "}
          <code className="mono">{shortRev(result.revision) ?? "the new revision"}</code>.
        </p>
      </div>
    </div>
  );
}

function ResultBanner({ result, name }: { result: ConditionalSaveResult; name: string }) {
  if (result.status === "ok") return <SaveReceipt result={result} name={name} />;
  if (result.status === "invalid") {
    return (
      <div className="col gap-3">
        <div className="banner banner-error" role="alert">
          <div className="row gap-2">
            <Icon name="x-circle" size={16} />
            <div>
              <strong>Not applied.</strong> The document was rejected, and nothing on the
              server changed.
            </div>
          </div>
        </div>
        {result.errors.map((err, i) => (
          <div className="card card-pad" key={i}>
            <div className="row gap-2">
              <code className="mono txt-sm">{err.field}</code>
              {err.line ? <span className="tag">line {err.line}</span> : null}
            </div>
            <p className="txt-sm" style={{ marginTop: 6 }}>
              {err.message}
            </p>
            {err.suggestion && <p className="hint">{err.suggestion}</p>}
          </div>
        ))}
      </div>
    );
  }
  if (result.status === "denied") {
    return (
      <div className="banner banner-warn" role="alert">
        <strong>Not applied.</strong> {result.message}
      </div>
    );
  }
  if (result.status === "error") {
    return (
      <div className="banner banner-error" role="alert">
        <strong>Outcome unknown.</strong> {result.message}
      </div>
    );
  }
  return null;
}

function DiffPanel({ diff }: { diff: ReturnType<typeof diffLines> }) {
  if (diff.tooLarge) {
    return (
      <p className="txt-sm">
        This document is too large to preview as a diff. Review it in the editor before
        applying.
      </p>
    );
  }
  if (diff.added === 0 && diff.removed === 0) {
    return <p className="txt-sm">No changes — the draft matches what the server holds.</p>;
  }

  const rows = collapseUnchanged(diff.lines);
  return (
    <div className="col gap-3">
      <div className="row gap-2">
        <span className="tag">+{diff.added}</span>
        <span className="tag">−{diff.removed}</span>
      </div>
      {/* Wide content scrolls inside its own box rather than the page. */}
      <div style={{ overflowX: "auto", maxHeight: 420, overflowY: "auto" }}>
        <table className="tbl mono" style={{ fontSize: 12, width: "100%" }}>
          <caption className="sr-only">
            Line-by-line changes between the stored definition and your draft
          </caption>
          <tbody>
            {rows.map((row, i) =>
              row === null ? (
                <tr key={i}>
                  <td className="muted" style={{ padding: "2px 8px" }}>
                    ⋯
                  </td>
                </tr>
              ) : (
                <tr key={i}>
                  <td
                    style={{
                      padding: "1px 8px",
                      whiteSpace: "pre",
                      background:
                        row.kind === "add"
                          ? "var(--sr-success-bg)"
                          : row.kind === "del"
                            ? "var(--sr-danger-bg)"
                            : undefined,
                    }}
                  >
                    {/* A symbol as well as colour: status is never colour-only. */}
                    <span aria-hidden="true">
                      {row.kind === "add" ? "+" : row.kind === "del" ? "−" : " "}
                    </span>
                    {row.kind !== "same" && (
                      <span className="sr-only">{row.kind === "add" ? "added: " : "removed: "}</span>
                    )}
                    {" " + row.text}
                  </td>
                </tr>
              ),
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ValidationPanel({
  validation,
  pending,
  dirty,
}: {
  validation: ValidateResult | null;
  pending: boolean;
  dirty: boolean;
}) {
  if (pending) return <p className="txt-sm">Working…</p>;
  if (!validation) {
    return (
      <p className="txt-sm">
        {dirty
          ? "Validate, then review the changes before applying them."
          : "No changes yet."}
      </p>
    );
  }
  if (validation.ok) {
    return (
      <div className="banner banner-ok" role="status">
        <strong>Valid.</strong> Reviewed changes can be applied.
      </div>
    );
  }
  return (
    <div className="col gap-3">
      <div className="banner banner-error" role="alert">
        <strong>Invalid.</strong> Fix these before applying.
      </div>
      {(validation.errors ?? []).map((err, i) => (
        <div className="card card-pad" key={i}>
          <div className="row gap-2">
            <code className="mono txt-sm">{err.field}</code>
            {err.line ? <span className="tag">line {err.line}</span> : null}
          </div>
          <p className="txt-sm" style={{ marginTop: 6 }}>
            {err.message}
          </p>
          {err.suggestion && <p className="hint">{err.suggestion}</p>}
        </div>
      ))}
    </div>
  );
}
