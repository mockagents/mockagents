"use client";

// UX-03: the guided half of "guided form + YAML".
//
// The form is a LENS over the YAML document, not a model of it. Every control
// reads its own path out of the text and writes that one path back; nothing
// else in the document is ever re-serialized. That is the difference between a
// form that is safe on a config it only partly understands and one that
// quietly deletes the parts it does not render.
//
// Where a value cannot be edited safely — a multi-line block scalar, inline
// flow syntax, an ambiguous duplicate key — the control is disabled and says
// why, rather than guessing at an edit. The YAML tab is always the complete
// escape hatch.

import { useMemo } from "react";

import { Icon } from "@/lib/icons";
import { countItems, listKeys, readScalar, writeScalar } from "@/lib/yamlPath";

// Paths the form owns. Everything else in the document is untouched — and
// named, so the user knows it is there.
//
// `behavior` is only PARTLY covered: the form renders spec.behavior.scenarios
// and nothing else under it. Treating the whole subtree as covered would hide
// spec.behavior.chaos, which is exactly where the example agents put chaos
// config — the most important thing not to look like it does not exist.
const COVERED_SPEC_KEYS = new Set(["model", "protocol", "behavior"]);
const COVERED_BEHAVIOR_KEYS = new Set(["scenarios"]);

export interface GuidedFormProps {
  yaml: string;
  onChange: (next: string) => void;
  /** Mirrors the editor's write permission: a viewer can look, not change. */
  disabled?: boolean;
}

interface FieldState {
  value: string;
  /** Non-null when the value cannot be edited safely; explains why. */
  blocked: string | null;
  /** True when the key is absent and would be inserted on first edit. */
  absent: boolean;
}

function fieldState(yaml: string, path: string): FieldState {
  const r = readScalar(yaml, path);
  if (!r.ok) return { value: "", blocked: r.reason, absent: false };
  return { value: r.value ?? "", blocked: null, absent: r.value === null };
}

export function GuidedForm({ yaml, onChange, disabled = false }: GuidedFormProps) {
  const scenarioCount = useMemo(() => countItems(yaml, "spec.behavior.scenarios"), [yaml]);

  // Anything under spec: this form does not render. Naming them is the point:
  // the user must not conclude the form is the whole agent.
  const uncovered = useMemo(
    () => [
      ...listKeys(yaml, "spec")
        .filter((k) => !COVERED_SPEC_KEYS.has(k))
        .map((k) => `spec.${k}`),
      ...listKeys(yaml, "spec.behavior")
        .filter((k) => !COVERED_BEHAVIOR_KEYS.has(k))
        .map((k) => `spec.behavior.${k}`),
    ],
    [yaml],
  );

  function set(path: string, value: string) {
    const r = writeScalar(yaml, path, value);
    // A refused write leaves the document exactly as it was. The control that
    // produced it is disabled, so this is a guard, not an expected path.
    if (r.ok) onChange(r.value);
  }

  return (
    <div className="col gap-4">
      <p className="hint">
        This form edits the fields below <em>in place</em>. Everything else in the
        document — including anything this form does not show — is left exactly as it
        is. Switch to YAML for full control.
      </p>

      {uncovered.length > 0 && (
        <div className="banner banner-warn" role="note">
          <strong>Not shown here:</strong>{" "}
          {uncovered.map((k) => (
            <code className="mono" key={k} style={{ marginRight: 6 }}>
              {k}
            </code>
          ))}
          <div className="txt-sm" style={{ marginTop: 4 }}>
            These are part of this agent and are preserved unchanged. Edit them on the
            YAML tab.
          </div>
        </div>
      )}

      <Field
        label="Name"
        path="metadata.name"
        yaml={yaml}
        onSet={set}
        disabled
        hint="The agent's identity. Renaming here would create a different agent, so it is read-only."
      />
      <Field
        label="Description"
        path="metadata.description"
        yaml={yaml}
        onSet={set}
        disabled={disabled}
        hint="Shown in the agent catalog."
      />
      <Field
        label="Model"
        path="spec.model"
        yaml={yaml}
        onSet={set}
        disabled={disabled}
        hint="The model name this mock reports. It is never called."
      />
      <Field
        label="Protocol"
        path="spec.protocol"
        yaml={yaml}
        onSet={set}
        disabled={disabled}
        hint="Wire format this agent serves, e.g. openai-chat-completions."
      />

      <div>
        <div className="eyebrow mb-3">
          Scenarios{" "}
          <span className="tag">{scenarioCount}</span>
        </div>
        {scenarioCount === 0 ? (
          <p className="txt-sm muted">
            No scenarios found at <code className="mono">spec.behavior.scenarios</code>, or
            they use a shape this form cannot read. The YAML tab shows the document as it
            is.
          </p>
        ) : (
          <div className="col gap-3">
            {Array.from({ length: scenarioCount }, (_, i) => (
              <div className="card card-pad" key={i}>
                <Field
                  label={`Scenario ${i + 1} name`}
                  path={`spec.behavior.scenarios.${i}.name`}
                  yaml={yaml}
                  onSet={set}
                  disabled={disabled}
                />
                {/* Labels are scoped per scenario: two controls sharing the
                    accessible name "Response content" would be indistinguishable
                    to a screen-reader user moving through the form. */}
                <Field
                  label={`Scenario ${i + 1} response content`}
                  path={`spec.behavior.scenarios.${i}.response.content`}
                  yaml={yaml}
                  onSet={set}
                  disabled={disabled}
                  multiline
                />
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function Field({
  label,
  path,
  yaml,
  onSet,
  disabled,
  hint,
  multiline,
}: {
  label: string;
  path: string;
  yaml: string;
  onSet: (path: string, value: string) => void;
  disabled?: boolean;
  hint?: string;
  multiline?: boolean;
}) {
  const state = fieldState(yaml, path);
  const id = `field-${path.replace(/\./g, "-")}`;
  const describedBy = `${id}-hint`;
  const isDisabled = disabled || state.blocked !== null;

  const common = {
    id,
    value: state.value,
    disabled: isDisabled,
    "aria-describedby": describedBy,
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      onSet(path, e.target.value),
  };

  return (
    <div className="field">
      <label htmlFor={id}>
        {label}
        {state.absent && !state.blocked && <span className="tag" style={{ marginLeft: 8 }}>not set</span>}
      </label>
      {multiline ? (
        <textarea className="textarea mono" rows={3} {...common} />
      ) : (
        <input className="input mono" type="text" {...common} />
      )}
      <p className="hint" id={describedBy}>
        <code className="mono">{path}</code>
        {state.blocked ? (
          <>
            {" — "}
            <span style={{ color: "var(--sr-danger-fg)" }}>
              <Icon name="x-circle" size={12} /> not editable here: {state.blocked}. Use the
              YAML tab.
            </span>
          </>
        ) : hint ? (
          <> — {hint}</>
        ) : null}
      </p>
    </div>
  );
}
