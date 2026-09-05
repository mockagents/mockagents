/* Workbench — Mocks inventory + safe agent authoring (UX-03).
   States: draft → validating → invalid → ready → diff → applying →
   persisted | runtime-only | conflict. Viewer-denied variant. */
(function () {
  const { useState } = React;
  const h = React.createElement;
  const { WBBadge: Badge, WBSid: Sid, WB } = window;

  // ---------- Mocks inventory ----------
  function Mocks({ demo, go }) {
    const [tab, setTab] = useState("agents");
    const seeded = demo.install === "seeded";
    const offline = demo.conn === "offline";
    return h("div", null,
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("h1", { className: "page" }, "Mocks"),
          h("p", { className: "lede" }, "Every definition loaded by the server. Revision is per-resource (ETag). Future resource kinds appear here capability-gated — disabled with explanation, never hidden or dead.")),
        h(Sid, null, "UX-03 · read: any role · write: editor")),
      h("div", { className: "tabs mt-4" },
        h("button", { className: "tab" + (tab === "agents" ? " on" : ""), onClick: () => setTab("agents") }, "Agents", h("span", { className: "k" }, seeded ? 4 : 0)),
        h("button", { className: "tab" + (tab === "pipelines" ? " on" : ""), onClick: () => setTab("pipelines") }, "Pipelines", h("span", { className: "k" }, seeded ? 1 : 0)),
        h("button", { className: "tab", disabled: true, title: "Not enabled on this server version", style: { opacity: .45, cursor: "not-allowed" } }, "Vector collections", h("span", { className: "tag", style: { fontSize: 9.5 } }, "capability-gated")),
      ),
      offline ? h("div", { className: "banner bad mt-4", role: "alert" }, h("div", null, h("b", null, "Unreachable — showing definitions loaded at 14:32:04Z. "), "This is a stale snapshot, not the current server inventory. Editing is disabled until the connection is restored; unsent drafts are kept.")) : null,
      tab === "agents" ? (seeded ? h("div", { className: "card mt-4" },
        h("table", { className: "tbl" },
          h("thead", null, h("tr", null, h("th", null, "name"), h("th", null, "protocol"), h("th", { className: "num", style: { textAlign: "right" } }, "scenarios"), h("th", { className: "num", style: { textAlign: "right" } }, "tools"), h("th", null, "revision"), h("th", null, "persistence"), h("th", null, ""))),
          h("tbody", null, WB.AGENTS.map((a) => h("tr", { key: a.name, className: "click", onClick: () => go("editor", a.name) },
            h("td", { className: "mono", style: { fontWeight: 600 } }, a.name, a.starter ? h("span", { className: "tag", style: { marginLeft: 8 } }, "starter") : null, a.chaos ? h("span", { className: "tag b", style: { marginLeft: 8 } }, "chaos") : null),
            h("td", { className: "mono muted" }, a.protocol),
            h("td", { className: "num" }, a.scenarios), h("td", { className: "num" }, a.tools),
            h("td", { className: "mono muted" }, a.revision),
            h("td", null, h(Badge, { kind: "ok" }, "persisted")),
            h("td", { style: { textAlign: "right" } }, h("span", { className: "muted txt-xs" }, "open →")),
          ))),
        ),
        h("div", { className: "card-f" }, "Source: GET /api/v1/agents · full definitions load on open, never regenerated from this summary."),
      ) : h("div", { className: "empty mt-4" }, "No agents loaded. Add a YAML file to your agents directory and reload — see the setup guide on Overview."))
      : tab === "pipelines" ? (seeded ? h("div", { className: "card mt-4" },
        h("table", { className: "tbl" },
          h("thead", null, h("tr", null, h("th", null, "name"), h("th", { style: { textAlign: "right" } }, "agents"), h("th", { style: { textAlign: "right" } }, "edges"), h("th", null, "revision"), h("th", null, "source"), h("th", null, ""))),
          h("tbody", null, WB.PIPELINES.map((p) => h("tr", { key: p.name, className: "click", onClick: () => go("lab") },
            h("td", { className: "mono", style: { fontWeight: 600 } }, p.name), h("td", { className: "num" }, p.agents), h("td", { className: "num" }, p.edges),
            h("td", { className: "mono muted" }, p.revision), h("td", { className: "mono muted" }, p.source),
            h("td", { style: { textAlign: "right" } }, h("span", { className: "muted txt-xs" }, "run in Test Lab →")))))),
        h("div", { className: "card-f" }, "Pipeline editing uses the existing React Flow editor (preserved, not redesigned). Creation is not part of Release A."),
      ) : h("div", { className: "card mt-4 card-pad" },
        h("div", { className: "banner info mb-3" }, h("div", null, h("b", null, "Pipeline creation is not part of Release A. "), "Seed the documented starter to enable Test Lab runs.")),
        h("pre", { className: "codeblock" }, "cp examples/support-triage.yaml agents/\nmockagents reload"))) : null,
    );
  }

  // ---------- Agent editor (UX-03) ----------
  const GOOD = WB.STARTER_YAML;
  const BROKEN = GOOD.replace("response:\n          content: \"Happy to check that order for you.\"", "responding:\n          content: \"Happy to check that order for you.\"");

  function Editor({ demo, go }) {
    const viewer = demo.role === "viewer" || demo.role === "local";
    const offline = demo.conn === "offline";
    const locked = viewer || offline;
    const [mode, setMode] = useState("yaml"); // yaml | form
    const [src, setSrc] = useState(GOOD.replace("Happy to check that order for you.", "Happy to check on that order right away."));
    const [state, setState] = useState("draft"); // draft validating invalid ready diff applying persisted runtimeonly conflict
    const [applyOutcome, setApplyOutcome] = useState("persisted"); // prototype: persisted | runtimeonly | conflict

    function validate() {
      setState("validating");
      setTimeout(() => setState(/responding:/.test(src) ? "invalid" : "ready"), 500);
    }
    function apply() {
      setState("applying");
      setTimeout(() => setState(applyOutcome), 650);
    }
    const errLine = src.split("\n").findIndex((l) => /responding:/.test(l)) + 1;

    return h("div", null,
      h("button", { className: "btn ghost", style: { marginLeft: -8, marginBottom: 8 }, onClick: () => go("mocks") }, "← Mocks"),
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("div", { className: "row gap-3" }, h("h1", { className: "page mono" }, "support-starter"),
            h("span", { className: "tag" }, "revision a41f"), h(Badge, { kind: "ok" }, "persisted"), h("span", { className: "tag" }, "agents/support-starter.yaml")),
          h("p", { className: "lede" }, "The complete server definition is loaded (GET with ETag) — the editor never regenerates YAML from the summary type. Unknown fields would disable Apply with an explanation.")),
        h(Sid, null, "UX-03 · editor · GET/PUT If-Match (proposed revision API)")),

      viewer ? h("div", { className: "banner info mt-4", role: "status" },
        h("div", null, h("b", null, demo.role === "local" ? "Local unauthenticated mode — editing shown read-only in this prototype. " : "Your role (viewer) can read this definition but not modify it. "),
          "This is a permission, not an error — the service is healthy. You can still run pipelines in Test Lab and export drafts.")) : null,
      offline && !viewer ? h("div", { className: "banner bad mt-4", role: "alert" }, h("div", null, h("b", null, "Server unreachable — your draft is preserved locally (this session). "), "Validate and Apply are disabled. Nothing is lost if you keep this tab open; use Export draft to save it externally.")) : null,

      h("div", { className: "grid2 mt-4 two-col", style: { gridTemplateColumns: "1.15fr .85fr", alignItems: "start" } },
        // ---- left: editor ----
        h("div", { className: "card" },
          h("div", { className: "card-h" },
            h("div", { className: "row gap-2" },
              h("button", { className: "pill", "aria-pressed": mode === "form", onClick: () => setMode("form") }, "Form"),
              h("button", { className: "pill", "aria-pressed": mode === "yaml", onClick: () => setMode("yaml") }, "YAML")),
            h("span", { className: "sub" }, "one draft, two views"),
            h("div", { className: "right" },
              !locked ? h("button", { className: "btn ghost", style: { height: 26, fontSize: 11 }, title: "prototype affordance", onClick: () => { setSrc(/responding:/.test(src) ? GOOD : BROKEN); setState("draft"); } }, /responding:/.test(src) ? "(prototype) restore valid draft" : "(prototype) load invalid draft") : null,
              h("button", { className: "btn ghost", style: { height: 26, fontSize: 11.5 } }, "Export draft"),
              state === "draft" || state === "invalid" ? h("span", { className: "tag" }, "draft · unsaved") : null)),
          mode === "yaml" ? h("div", { className: "card-pad" },
            h("div", { className: "codebox" },
              h("div", { className: "lns", "aria-hidden": true }, src.split("\n").map((_, i) => h("div", { key: i, className: state === "invalid" && i + 1 === errLine ? "err" : "" }, i + 1))),
              h("textarea", { value: src, spellCheck: false, disabled: locked, "aria-label": "agent YAML definition", rows: src.split("\n").length + 1, onChange: (e) => { setSrc(e.target.value); setState("draft"); } })),
          ) : h("div", { className: "card-pad col gap-3" },
            h("div", { className: "field" }, h("label", null, "Scenario · order-status · response content"),
              h("textarea", { className: "textarea", rows: 2, disabled: locked, value: "Happy to check on that order right away.", onChange: () => setState("draft") })),
            h("div", { className: "field" }, h("label", null, "Match · content_contains"),
              h("input", { className: "input mono", disabled: locked, defaultValue: "order status" })),
            h("div", { className: "hint" }, "Form edits write into the same draft as the YAML view — switching views never loses changes."),
          ),
          h("div", { className: "card-f row gap-2", style: { padding: "10px 16px" } },
            h("button", { className: "btn out", onClick: validate, disabled: locked || state === "validating" }, state === "validating" ? "Validating…" : "Validate"),
            h("button", { className: "btn pri", onClick: () => setState("diff"), disabled: locked || state !== "ready" }, "Review diff"),
            h("span", { className: "muted txt-xs", style: { marginLeft: "auto" } },
              locked ? (viewer ? "read-only for this role" : "editing disabled while unreachable") : "Apply requires a validated draft + reviewed diff"),
          ),
        ),
        // ---- right: state rail ----
        h("div", { className: "col gap-4" },
          state === "invalid" ? h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Validation failed"), h("div", { className: "right" }, h(Badge, { kind: "bad" }, "invalid"))),
            h("div", { className: "card-pad col gap-3" },
              h("div", { className: "banner bad" }, h("div", null, h("b", null, "line " + errLine + " · scenarios[0].responding"), h("br", null), "unknown field \"responding\" — did you mean \"response\"? Apply stays disabled; your draft is kept.")),
              h("pre", { className: "codeblock" }, "response:\n  content: \"…\"")),
          ) : null,
          state === "ready" ? h("div", { className: "banner ok", role: "status" }, h("div", null, h("b", null, "Valid. "), "Parsed as kind: Agent, no schema errors, no unsupported fields. Review the diff to continue.")) : null,
          (state === "diff" || state === "applying") ? h(DiffCard, { onApply: apply, applying: state === "applying", applyOutcome, setApplyOutcome }) : null,
          state === "persisted" ? h(Receipt, { kind: "persisted" }) : null,
          state === "runtimeonly" ? h(Receipt, { kind: "runtimeonly" }) : null,
          state === "conflict" ? h(Conflict, { onReload: () => { setExtChange(false); setState("draft"); }, onOverwrite: () => { setExtChange(false); setState("persisted"); } }) : null,
          state === "draft" && !locked ? h("div", { className: "card card-pad" },
            h("div", { className: "eyebrow mb-2" }, "definition state"),
            h("dl", { className: "kv" },
              h("dt", null, "state"), h("dd", null, h("span", { className: "tag" }, "draft · in this browser session")),
              h("dt", null, "base revision"), h("dd", { className: "mono" }, "a41f (loaded 14:30:12Z)"),
              h("dt", null, "apply target"), h("dd", null, "explicit replace with If-Match: a41f"),
              h("dt", null, "durable drafts"), h("dd", { className: "muted" }, "not in Release A — use Export draft")),
          ) : null,
        ),
      ),
    );
  }

  function DiffCard({ onApply, applying, applyOutcome, setApplyOutcome }) {
    return h("div", { className: "card" },
      h("div", { className: "card-h" }, h("h2", null, "Review diff"), h("span", { className: "sub" }, "base a41f → draft"), h("div", { className: "right" }, h(Sid, null, "UX-03"))),
      h("div", { className: "card-pad" },
        h("div", { className: "diff" },
          h("div", { className: "dl ctx" }, h("span", { className: "dn" }, "22"), h("span", { className: "dc" }, "      response:")),
          h("div", { className: "dl del" }, h("span", { className: "dn" }, "23"), h("span", { className: "dc" }, '  content: "Happy to check that order for you."')),
          h("div", { className: "dl add" }, h("span", { className: "dn" }, "23"), h("span", { className: "dc" }, '  content: "Happy to check on that order right away."')),
        ),
        h("div", { className: "banner warn mt-3" }, h("div", null, h("b", null, "Applying changes the active runtime. "), "The pipeline support-triage references this agent; its next run will use the new response. No isolated preview exists in Release A.")),
        h("label", { className: "row gap-2 mt-3 txt-xs muted" },
          h("span", null, "(prototype) apply outcome:"),
          h("select", { value: applyOutcome, onChange: (e) => setApplyOutcome(e.target.value), style: { height: 24, fontSize: 11, borderRadius: 5, border: "1px solid var(--sr-input)", background: "var(--sr-bg)", color: "var(--sr-fg)" } },
            h("option", { value: "persisted" }, "persisted (durable save)"),
            h("option", { value: "runtimeonly" }, "runtime-only (file write fails)"),
            h("option", { value: "conflict" }, "conflict (concurrent external write)"))),
      ),
      h("div", { className: "card-f row gap-2", style: { padding: "10px 16px" } },
        h("button", { className: "btn pri", onClick: onApply, disabled: applying }, applying ? "Applying…" : "Apply · replace with If-Match"),
        h("span", { className: "muted txt-xs" }, "create never overwrites; this is an explicit replace")),
    );
  }

  function Receipt({ kind }) {
    return h("div", { className: "card" },
      h("div", { className: "card-h" }, h("h2", null, "Save receipt"), h("div", { className: "right" }, kind === "persisted" ? h(Badge, { kind: "ok" }, "persisted") : h(Badge, { kind: "warn" }, "runtime-only"))),
      h("div", { className: "card-pad col gap-2" },
        kind === "persisted" ? h("div", { className: "banner ok" }, h("div", null, h("b", null, "Applied and persisted. "), "New revision b07e written to agents/support-starter.yaml and active in the runtime. Confirmed by the server — this receipt appears only after confirmation, never as an optimistic toast."))
          : h("div", { className: "banner warn" }, h("div", null, h("b", null, "Active in runtime, but NOT persisted to disk. "), "The file write failed (read-only filesystem). The change is live until restart. Fix permissions and re-apply, or export the draft — do not assume durability.")),
        h("dl", { className: "kv" },
          h("dt", null, "new revision"), h("dd", { className: "mono" }, "b07e"),
          h("dt", null, "runtime"), h("dd", null, h(Badge, { kind: "ok" }, "active")),
          h("dt", null, "file"), h("dd", null, kind === "persisted" ? h(Badge, { kind: "ok" }, "written") : h(Badge, { kind: "bad" }, "write failed")),
          h("dt", null, "audit"), h("dd", { className: "mono" }, "agent.updated · 14:33:20Z")),
      ));
  }

  function Conflict({ onReload, onOverwrite }) {
    return h("div", { className: "card", role: "alertdialog", "aria-label": "edit conflict" },
      h("div", { className: "card-h" }, h("h2", null, "Someone changed this agent while you were editing"), h("div", { className: "right" }, h(Badge, { kind: "warn" }, "conflict 412"))),
      h("div", { className: "card-pad col gap-3" },
        h("div", { className: "txt-sm" }, "Your Apply was rejected: the server revision is now ", h("code", { className: "mono" }, "c1d9"), " (yours was based on ", h("code", { className: "mono" }, "a41f"), "). ", h("b", null, "Both versions are preserved"), " — nothing was discarded."),
        h("div", { className: "grid2" },
          h("div", { className: "receipt-line" }, h("div", null, h("b", { className: "txt-sm" }, "Their change"), h("div", { className: "mono" }, "c1d9 · api client · 14:32:58Z"))),
          h("div", { className: "receipt-line" }, h("div", null, h("b", { className: "txt-sm" }, "Your draft"), h("div", { className: "mono" }, "based on a41f · this session")))),
        h("div", { className: "banner info" }, h("div", null, "Legacy unconditional writers can still bypass If-Match — see the unresolved decision in the handoff. This dialog covers GUI + conditional API writers.")),
      ),
      h("div", { className: "card-f row gap-2", style: { padding: "10px 16px" } },
        h("button", { className: "btn out", onClick: onReload }, "Load current, keep my draft aside"),
        h("button", { className: "btn danger", onClick: onOverwrite }, "Overwrite after review"),
        h("span", { className: "muted txt-xs" }, "no silent merge")),
    );
  }

  Object.assign(window, { WBMocks: Mocks, WBEditor: Editor });
})();
