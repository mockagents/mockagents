"use client";

// YamlEditor is the client island for /editor. It renders the design's
// two-column validator: a code card (line gutter + plain textarea) on the
// left and a result card on the right. The Validate button POSTs to the
// server-action `validateAction`, so the schema rules and the CLI stay in
// lockstep — no JSON-schema-in-the-browser, no ajv dep, no divergence.
//
// The textarea is intentionally plain. A full Monaco drop-in would add
// ~3 MB of bundle for features (autocomplete, folding) most operators will
// never use from the GUI. Line numbers render as a sibling gutter column —
// rows flagged by an error turn red — so the user still gets inline feedback
// without the editor widget.

import { useMemo, useState, useTransition } from "react";

import { Icon } from "@/lib/icons";
import type { ValidateResult, ValidationError, SaveResult } from "@/lib/api";

const SAMPLE_AGENT = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: hello-world
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "Hello from MockAgents"
`;

// A deliberately invalid document so operators can see the validator
// surface real errors without hand-crafting one — mirrors the design's
// "Load broken" affordance.
const BROKEN_AGENT = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: invoice-agent
spec:
  protocol: openai-chats
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        responding:
          content: "How can I help with your invoice?"
`;

interface YamlEditorProps {
  validateAction: (yaml: string) => Promise<ValidateResult>;
  saveAction: (yaml: string) => Promise<SaveResult>;
}

export function YamlEditor({ validateAction, saveAction }: YamlEditorProps) {
  const [yaml, setYaml] = useState(SAMPLE_AGENT);
  const [result, setResult] = useState<ValidateResult | null>(null);
  const [saved, setSaved] = useState<SaveResult | null>(null);
  const [isPending, startTransition] = useTransition();

  const lines = useMemo(() => yaml.split("\n"), [yaml]);
  // Lines the server flagged — rendered red in the gutter for at-a-glance triage.
  // Includes errors from a failed save (422) so a rejected save highlights too.
  const errLines = useMemo(
    () =>
      new Set(
        [...(result?.errors ?? []), ...(saved?.errors ?? [])]
          .map((e) => e.line)
          .filter(Boolean),
      ),
    [result, saved],
  );

  function onValidate() {
    startTransition(async () => {
      const r = await validateAction(yaml);
      setResult(r);
      setSaved(null);
    });
  }

  function onSave() {
    startTransition(async () => {
      const r = await saveAction(yaml);
      setSaved(r);
      setResult(null);
    });
  }

  function load(sample: string) {
    setYaml(sample);
    setResult(null);
    setSaved(null);
  }

  return (
    <div className="view-enter">
      <div className="head-row page-head">
        <div className="grow">
          <h1 className="page-title">Config editor</h1>
          <p className="page-lede">
            Validate against <code>POST /api/v1/config/validate</code> (the same
            validator <code>mockagents validate</code> runs), then <strong>Save</strong>{" "}
            to create or replace the agent on the running server via{" "}
            <code>PUT /api/v1/agents/&#123;name&#125;</code> — live immediately, no
            restart. (Save needs the editor role in multi-tenant mode.)
          </p>
        </div>
        <div className="row gap-2">
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={() => load(BROKEN_AGENT)}
            disabled={isPending}
          >
            Load broken
          </button>
          <button
            type="button"
            className="btn btn-outline btn-sm"
            onClick={() => load(SAMPLE_AGENT)}
            disabled={isPending}
          >
            Load valid
          </button>
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
            className="btn btn-default btn-sm"
            onClick={onSave}
            disabled={isPending}
          >
            <Icon name="save" size={15} />
            {isPending ? "Working…" : "Save"}
          </button>
        </div>
      </div>

      <div
        className="grid"
        style={{ gridTemplateColumns: "1.3fr 1fr", alignItems: "start" }}
      >
        <div className="card" style={{ overflow: "hidden" }}>
          <div className="card-head">
            <Icon name="file-code" size={15} />
            <div className="grow">
              <h3 className="mono">agent.yaml</h3>
            </div>
            <span className="tag">{lines.length} lines</span>
          </div>
          <div className="editor-grid">
            <pre className="editor-gutter" aria-hidden="true">
              {lines.map((_, i) => (
                <span
                  key={i}
                  style={
                    errLines.has(i + 1)
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
              value={yaml}
              onChange={(e) => {
                setYaml(e.target.value);
                // Clear BOTH feedback panels on edit so a stale "Created/Updated"
                // (or 422 highlight) can't outlive the document it described.
                setResult(null);
                setSaved(null);
              }}
              aria-label="Agent YAML"
            />
          </div>
        </div>

        <div className="card card-pad">
          <div className="eyebrow mb-3">{saved ? "save result" : "validation result"}</div>
          {saved ? (
            <SavePanel saved={saved} />
          ) : (
            <ResultPanel result={result} pending={isPending} />
          )}
        </div>
      </div>
    </div>
  );
}

function SavePanel({ saved }: { saved: SaveResult }) {
  if (saved.ok) {
    return (
      <div className="banner banner-ok">
        <div className="row gap-2">
          <Icon name="check-circle" size={16} />
          <div>
            <strong>{saved.status === "created" ? "Created." : "Updated."}</strong>{" "}
            {saved.message} It is serving immediately — no restart.
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="col gap-3">
      <div className="banner banner-error">
        <div className="row gap-2">
          <Icon name="x-circle" size={16} />
          <div>
            <strong>Save failed.</strong> {saved.message}
          </div>
        </div>
      </div>
      {(saved.errors ?? []).map((err, i) => (
        <ErrorCard key={i} err={err} />
      ))}
    </div>
  );
}

function ResultPanel({
  result,
  pending,
}: {
  result: ValidateResult | null;
  pending: boolean;
}) {
  if (!result) {
    return (
      <div className="empty">
        {pending
          ? "Running the server-side schema check…"
          : "Click Validate to run the server-side schema check."}
      </div>
    );
  }

  if (result.ok) {
    return (
      <div className="banner banner-ok">
        <div className="row gap-2">
          <Icon name="check-circle" size={16} />
          <div>
            <strong>Valid.</strong> Parsed as{" "}
            <code className="mono">kind: {result.kind || "?"}</code> with no
            schema errors.
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="col gap-3">
      <div className="banner banner-error">
        <div className="row gap-2">
          <Icon name="x-circle" size={16} />
          <div>
            <strong>
              {result.errors.length} error
              {result.errors.length === 1 ? "" : "s"}.
            </strong>{" "}
            Fix and re-validate.
          </div>
        </div>
      </div>
      {result.errors.map((err, i) => (
        <ErrorCard key={i} err={err} />
      ))}
    </div>
  );
}

function ErrorCard({ err }: { err: ValidationError }) {
  return (
    <div
      className="card-light"
      style={{
        border: "1px solid var(--sr-border)",
        borderRadius: 8,
        padding: 12,
      }}
    >
      <div className="row gap-2 mb-2">
        {err.line ? (
          <span className="badge badge-destructive mono">line {err.line}</span>
        ) : null}
        {err.field && <span className="mono txt-xs muted">{err.field}</span>}
      </div>
      <div className="txt-sm">{err.message}</div>
      {err.suggestion && (
        <div className="code-light mt-2" style={{ padding: "6px 10px" }}>
          {err.suggestion}
        </div>
      )}
    </div>
  );
}
