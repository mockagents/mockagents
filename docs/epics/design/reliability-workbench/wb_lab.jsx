/* Workbench — Test Lab (UX-05 active-runtime runs) + Investigate (UX-04 log explorer). */
(function () {
  const { useState } = React;
  const h = React.createElement;
  const { WBBadge: Badge, WBSid: Sid, WB } = window;

  // ---------- Test Lab ----------
  function Lab({ demo }) {
    const seeded = demo.install === "seeded";
    const offline = demo.conn === "offline";
    const [input, setInput] = useState("What's the status of order ORD-12345?");
    const [outcome, setOutcome] = useState("idle"); // idle running ok partial unknown missing
    const [nextResult, setNextResult] = useState("ok"); // demo: ok | partial | unknown | missing
    const [sess] = useState("sess_" + Math.random().toString(36).slice(2, 8));

    function run() {
      if (nextResult === "missing") { setOutcome("missing"); return; }
      setOutcome("running");
      setTimeout(() => setOutcome(nextResult), 900);
    }

    return h("div", null,
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("h1", { className: "page" }, "Test Lab"),
          h("p", { className: "lede" }, "Release A executes pipelines against the ", h("b", null, "active configuration"), " — the shared runtime, not an isolated copy. Isolated experiments and the request composer are Release B.")),
        h(Sid, null, "UX-05 · viewer+ · POST /api/v1/pipelines/{name}/run")),
      !seeded ? h("div", { className: "card mt-4 card-pad" },
        h("div", { className: "banner info" }, h("div", null, h("b", null, "No pipelines exist, so there is nothing to run. "), "Pipeline creation is not part of Release A — seed the documented starter (see Overview) and this page gains a Run form. This is an empty inventory, not an error."))) :
      h("div", { className: "grid2 mt-4 two-col", style: { gridTemplateColumns: ".9fr 1.1fr", alignItems: "start" } },
        // ---- run form ----
        h("div", { className: "card" },
          h("div", { className: "card-h" }, h("h2", null, "Run pipeline"), h("div", { className: "right" }, h("span", { className: "mode-pill" }, "runs against active configuration"))),
          h("div", { className: "card-pad col gap-3" },
            h("div", { className: "field" }, h("label", null, "Pipeline"),
              h("select", { className: "select", "aria-label": "pipeline" }, h("option", null, "support-triage · 3 agents · revision 5e12"))),
            h("div", { className: "field" }, h("label", null, "Input"),
              h("textarea", { className: "textarea mono", rows: 2, value: input, onChange: (e) => setInput(e.target.value), disabled: offline })),
            h("div", { className: "field" }, h("label", null, "Session ID"),
              h("div", { className: "row gap-2" }, h("input", { className: "input mono", value: sess, readOnly: true, style: { maxWidth: 220 } }), h("span", { className: "hint" }, "fresh per run — avoids conversational reuse; does not pin definitions"))),
            h("div", { className: "banner warn" }, h("div", null, h("b", null, "Before you run: "), "this executes the current live definitions (support-starter b07e, rag-agent 77c0, summary-writer b2d8) and advances per-node session state. Recent edits apply immediately.")),
            h("label", { className: "row gap-2 txt-xs muted", style: { cursor: "pointer" } },
              h("span", null, "(prototype) next outcome:"),
              h("select", { value: nextResult, onChange: (e) => setNextResult(e.target.value), style: { height: 24, fontSize: 11, borderRadius: 5, border: "1px solid var(--sr-input)", background: "var(--sr-bg)", color: "var(--sr-fg)" } },
                [["ok", "200 success"], ["partial", "422 partial failure"], ["unknown", "disconnect mid-run"], ["missing", "missing fixture"]].map(([v, l]) => h("option", { key: v, value: v }, l)))),
          ),
          h("div", { className: "card-f row gap-2", style: { padding: "10px 16px" } },
            h("button", { className: "btn pri lg", onClick: run, disabled: offline || outcome === "running" }, outcome === "running" ? "Running…" : "Run against active configuration"),
            outcome === "running" ? h("span", { className: "muted txt-xs" }, "duplicate submission disabled") : null),
        ),
        // ---- result ----
        h("div", { className: "col gap-4" },
          outcome === "idle" ? h("div", { className: "empty" }, "Run results appear here — node evidence, latency, and partial failures.") : null,
          outcome === "running" ? h("div", { className: "card card-pad row gap-3" }, h("span", { className: "dot info pulse" }), h("span", { className: "txt-sm" }, "Running… input submitted with session ", h("code", { className: "mono" }, sess), ". Cancel cannot roll back completed nodes.")) : null,
          outcome === "ok" ? h(RunResult, { r: WB.RUN_OK, partial: false }) : null,
          outcome === "partial" ? h(RunResult, { r: WB.RUN_PARTIAL, partial: true }) : null,
          outcome === "unknown" ? h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Outcome unknown"), h("div", { className: "right" }, h(Badge, { kind: "warn" }, "connection lost"))),
            h("div", { className: "card-pad col gap-3" },
              h("div", { className: "banner warn", role: "alert" }, h("div", null, h("b", null, "The connection dropped after the run was submitted. "), "The pipeline may have completed, partially completed, or failed — the outcome is unknown. Because runs mutate session state, ", h("b", null, "this will not be retried automatically"), ".")),
              h("dl", { className: "kv" },
                h("dt", null, "submitted"), h("dd", { className: "mono" }, "14:35:11Z · session " + sess),
                h("dt", null, "input"), h("dd", { className: "mono" }, input.slice(0, 40) + "…")),
              h("div", { className: "row gap-2" },
                h("button", { className: "btn out" }, "Check logs for this session"),
                h("button", { className: "btn out" }, "Re-check server"),
                h("span", { className: "muted txt-xs" }, "a new run would be a new session")),
            )) : null,
          outcome === "missing" ? h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Run blocked"), h("div", { className: "right" }, h(Badge, { kind: "bad" }, "missing dependency"))),
            h("div", { className: "card-pad" },
              h("div", { className: "banner bad" }, h("div", null, h("b", null, "Pipeline references an agent that is not loaded: "), h("code", { className: "mono" }, "rag-agent"), ". The run was not started. Load the missing definition (agents/rag-agent.yaml) and reload the server, then run again.")))) : null,
        ),
      ),
    );
  }

  function RunResult({ r, partial }) {
    const res = partial ? r.result : r;
    const [sel, setSel] = useState(partial ? "rag-lookup" : "router");
    const node = res.nodes.find((n) => n.node_id === sel);
    return h("div", { className: "card" },
      h("div", { className: "card-h" },
        h("h2", null, "Run result"), h("span", { className: "sub mono" }, r.session_id),
        h("div", { className: "right" }, partial ? h(Badge, { kind: "warn" }, "422 partial") : h(Badge, { kind: "ok" }, "200 complete"),
          h("span", { className: "tag" }, "total " + WB.ns2ms(res.latency)))),
      partial ? h("div", { style: { padding: "10px 16px", borderBottom: "1px solid var(--sr-border)" } },
        h("div", { className: "banner bad" }, h("div", null, h("b", null, "rag-lookup failed: "), r.error.message, ". Completed nodes below are evidence; the summarizer node is ", h("b", null, "absent — unknown, not zero"), "."))) : null,
      h("table", { className: "tbl" },
        h("thead", null, h("tr", null, h("th", null, "node"), h("th", null, "agent"), h("th", null, "status"), h("th", { style: { textAlign: "right" } }, "latency"), h("th", null, ""))),
        h("tbody", null,
          res.nodes.map((n) => h("tr", { key: n.node_id, className: "click" + (sel === n.node_id ? " sel" : ""), onClick: () => setSel(n.node_id) },
            h("td", { className: "mono", style: { fontWeight: 600 } }, n.node_id),
            h("td", { className: "mono muted" }, n.agent_name),
            h("td", null, n.response ? h(Badge, { kind: "ok" }, "responded") : h(Badge, { kind: "bad" }, "failed · null response")),
            h("td", { className: "num" }, WB.ns2ms(n.latency)),
            h("td", { style: { textAlign: "right" } }, h("span", { className: "muted txt-xs" }, "inspect →")))),
          partial ? h("tr", null, h("td", { className: "mono muted" }, "summarizer"), h("td", { className: "mono muted" }, "summary-writer"), h("td", null, h(Badge, { kind: "outline" }, "not executed · unknown")), h("td", { className: "num muted" }, "—"), h("td", null)) : null,
        )),
      h("div", { className: "card-pad", style: { borderTop: "1px solid var(--sr-border)" } },
        h("div", { className: "eyebrow mb-2" }, "node inspector · " + sel),
        node && node.response ? h("pre", { className: "codeblock" }, JSON.stringify(node.response, null, 2)) :
          h("div", { className: "banner bad" }, h("div", null, "Response is null. Error: scenario match failed — no scenario accepted the input after normalization. Match Explain (Release B, proposed API) would show the predicate evidence here.")),
      ),
      h("div", { className: "card-f" }, "Node order is definition order, not completion chronology. Latencies converted from nanosecond wire values. Provider logs for this session may exist in Investigate — no guaranteed linkage."),
    );
  }

  // ---------- Investigate (UX-04) ----------
  function Investigate({ demo }) {
    const offline = demo.conn === "offline";
    const [live, setLive] = useState(false);
    const [sel, setSel] = useState(5112);
    const [reveal, setReveal] = useState(false);
    const [logState, setLogState] = useState("data"); // data | empty
    const rows = logState === "empty" ? [] : WB.LOGS;
    const row = rows.find((r) => r.id === sel);
    return h("div", null,
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("h1", { className: "page" }, "Investigate"),
          h("p", { className: "lede" }, "Metadata-first request explorer. Bodies load only on explicit reveal. Session grouping uses recorded session_id; no causal links are invented across protocols.")),
        h(Sid, null, "UX-04 · any authenticated role · GET /api/v1/logs (+SSE)")),
      h("div", { className: "row gap-3 mt-4 wrap" },
        h("input", { className: "input", placeholder: "Filter: session, agent, status…", style: { maxWidth: 280 }, "aria-label": "filter logs" }),
        h("span", { className: "tag" }, "window: last 6 h · loaded " + rows.length + " of " + rows.length),
        h("div", { className: "row gap-2", style: { marginLeft: "auto" } },
          h("span", { className: "txt-xs muted" }, "Live feed"),
          h("button", { className: "pill", "aria-pressed": live, onClick: () => setLive(!live), disabled: offline }, live ? "on" : "off"),
          h("label", { className: "row gap-2 txt-xs muted" }, "(prototype)",
            h("select", { value: logState, onChange: (e) => setLogState(e.target.value), style: { height: 24, fontSize: 11, borderRadius: 5, border: "1px solid var(--sr-input)", background: "var(--sr-bg)", color: "var(--sr-fg)" } },
              h("option", { value: "data" }, "records"), h("option", { value: "empty" }, "empty store"))))),
      offline ? h("div", { className: "banner warn mt-3", role: "alert" }, h("div", null, h("b", null, "Feed disconnected at 14:32:04Z. "), "Rows below are the loaded snapshot. On reconnect, a bounded history recovery is attempted; any unresolved range is shown as a gap — never silently spliced.")) : null,
      rows.length === 0 ? h("div", { className: "empty mt-4" },
        h("b", null, "No records in this window (last 6 h)."), h("div", { className: "mt-2 muted" }, "The log store is reachable and empty — distinct from unreachable (connection error) and unauthorized (403). Send a request to any mocked endpoint and it appears here.")) :
      h("div", { className: "grid2 mt-4 two-col", style: { gridTemplateColumns: "1.15fr .85fr", alignItems: "start" } },
        h("div", { className: "card", style: { overflow: "hidden" } },
          h("table", { className: "tbl" },
            h("thead", null, h("tr", null, h("th", null, "id"), h("th", null, "time"), h("th", null, "session"), h("th", null, "agent"), h("th", null, "status"), h("th", { style: { textAlign: "right" } }, "latency"))),
            h("tbody", null,
              live ? h("tr", null, h("td", { colSpan: 6, style: { padding: "6px 14px", fontSize: 11, color: "var(--sr-info-fg)", background: "var(--sr-info-bg)" } }, h("span", { className: "dot info pulse", style: { marginRight: 8 } }), "live · selection is preserved while new rows arrive above")) : null,
              rows.slice(0, 3).map((r) => h(LogRow, { key: r.id, r, sel, setSel })),
              h("tr", { className: "gap-row" }, h("td", { colSpan: 6 }, "⚠ unresolved gap · 13:58:41Z – 14:20:06Z · feed dropped, bounded recovery incomplete — records in this range may exist but are not shown")),
              rows.slice(3).map((r) => h(LogRow, { key: r.id, r, sel, setSel })))),
          h("div", { className: "card-f" }, "Offset pagination is not a lossless cursor — window boundaries are labelled, not implied complete."),
        ),
        row ? h("div", { className: "card" },
          h("div", { className: "card-h" }, h("h2", { className: "mono", style: { fontSize: 12.5 } }, "log #" + row.id), h("div", { className: "right" },
            row.status === 200 ? h(Badge, { kind: "ok" }, "200") : row.status === 422 ? h(Badge, { kind: "warn" }, "422") : h(Badge, { kind: "bad" }, String(row.status)))),
          h("div", { className: "card-pad col gap-3" },
            h("dl", { className: "kv" },
              h("dt", null, "session"), h("dd", { className: "mono" }, row.session),
              h("dt", null, "agent"), h("dd", { className: "mono" }, row.agent),
              h("dt", null, "endpoint"), h("dd", { className: "mono" }, "POST " + row.path),
              h("dt", null, "scenario"), h("dd", { className: "mono" }, row.scenario || "— none matched"),
              h("dt", null, "chaos"), h("dd", null, row.chaos ? h("span", { className: "tag b" }, row.chaos.fault + " · rate " + row.chaos.rate + " · seed " + row.chaos.seed) : h("span", { className: "muted" }, "none recorded")),
              h("dt", null, "capture"), h("dd", null, row.truncated ? h(Badge, { kind: "warn" }, "truncated at 64 KB") : h(Badge, { kind: "ok" }, "complete")),
              h("dt", null, "latency"), h("dd", { className: "mono" }, row.latency_ms + " ms")),
            row.truncated ? h("div", { className: "banner warn" }, h("div", null, "Body capture truncated — exact replay from this record is not possible; Release B reproduction would offer manual reconstruction.")) : null,
            h("div", null,
              h("div", { className: "row mb-2", style: { justifyContent: "space-between" } },
                h("span", { className: "eyebrow" }, "body · " + row.body_bytes + " bytes"),
                h("button", { className: "btn out", style: { height: 24, fontSize: 11 }, onClick: () => setReveal(!reveal) }, reveal ? "Hide body" : "Reveal body")),
              reveal ? h("pre", { className: "codeblock" }, '{"model":"gpt-4o","messages":[{"role":"user",\n  "content":"What\'s the status of order ORD-12345?"}]}') :
                h("div", { className: "empty", style: { padding: 16 } }, "Bodies are not fetched until revealed. Rendered as text — never as HTML.")),
          ),
        ) : null,
      ),
    );
  }
  function LogRow({ r, sel, setSel }) {
    return h("tr", { className: "click" + (sel === r.id ? " sel" : ""), onClick: () => setSel(r.id) },
      h("td", { className: "mono muted" }, r.id), h("td", { className: "mono" }, r.ts),
      h("td", { className: "mono" }, r.session), h("td", { className: "mono" }, r.agent),
      h("td", null, r.status === 200 ? h(WBBadgeLocal, { kind: "ok" }, "200") : r.status === 422 ? h(WBBadgeLocal, { kind: "warn" }, "422") : h(WBBadgeLocal, { kind: "bad" }, String(r.status))),
      h("td", { className: "num" }, r.latency_ms + " ms"));
  }
  function WBBadgeLocal({ kind, children }) { return h("span", { className: "badge " + kind }, h("span", { className: "dot " + kind }), children); }

  Object.assign(window, { WBLab: Lab, WBInvestigate: Investigate });
})();
