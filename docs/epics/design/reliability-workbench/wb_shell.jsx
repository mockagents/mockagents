/* Workbench shell: instrument strip (hybrid), nav, demo controls, Overview. */
(function () {
  const { useState } = React;
  const h = React.createElement;

  function Badge({ kind, children }) { return h("span", { className: "badge " + kind }, h("span", { className: "dot " + (kind === "neutral" || kind === "outline" ? "info" : kind) }), children); }
  function Sid({ children }) { return h("span", { className: "sid" }, children); }
  function CopyBtn() { const [d, sD] = useState(false); return h("button", { className: "copybtn", onClick: () => { sD(true); setTimeout(() => sD(false), 1100); } }, d ? "copied" : "copy"); }

  // ---------- instrument strip ----------
  function Strip({ demo }) {
    const conn = demo.conn; // ready | notready | offline
    return h("div", { className: "strip", role: "status", "aria-label": "server context" },
      h("div", { className: "cell" }, h("span", { className: "l" }, "server"), h("span", { className: "v" }, "localhost:8080")),
      h("div", { className: "cell" }, h("span", { className: "l" }, "liveness"),
        conn === "offline" ? h("span", { className: "v bad" }, h("span", { className: "dot bad" }), "UNREACHABLE")
          : h("span", { className: "v ok" }, h("span", { className: "dot ok" }), "PROCESS-UP")),
      h("div", { className: "cell" }, h("span", { className: "l" }, "readiness"),
        conn === "ready" ? h("span", { className: "v ok" }, h("span", { className: "dot ok" }), "READY")
          : conn === "notready" ? h("span", { className: "v warn" }, h("span", { className: "dot warn" }), "NOT-READY")
          : h("span", { className: "v" }, "UNKNOWN")),
      h("div", { className: "cell" }, h("span", { className: "l" }, "tenant / role"),
        h("span", { className: "v" }, demo.role === "local" ? "local mode · unauthenticated" : "acme-prod · " + demo.role)),
      h("div", { className: "cell" }, h("span", { className: "l" }, "exec mode"), h("span", { className: "v warn" }, "ACTIVE CONFIG")),
      h("div", { className: "cell" }, h("span", { className: "l" }, "revision"), h("span", { className: "v" }, "per-resource")),
      h("div", { className: "cell" }, h("span", { className: "l" }, "engine"), h("span", { className: "v" }, "v0.4.2")),
      h("div", { className: "cell grow" }, h("span", { className: "l" }, "last refresh"),
        h("span", { className: "v" }, conn === "offline" ? h("span", { className: "warn" }, "14:32:04Z · stale (61s)") : "14:32:04Z · 4s ago")),
    );
  }

  // ---------- demo controls (prototype-only) ----------
  function DemoBar({ demo, setDemo }) {
    const [open, setOpen] = useState(true);
    return h("div", { className: "demo-bar" },
      h("div", { className: "dh", onClick: () => setOpen(!open) }, "Prototype states", h("span", { style: { marginLeft: "auto" } }, open ? "–" : "+")),
      open ? h("div", { className: "db" },
        h("div", { className: "dr" }, h("span", null, "role"),
          h("select", { value: demo.role, onChange: (e) => setDemo({ ...demo, role: e.target.value }), "aria-label": "demo role" },
            ["editor", "viewer", "admin", "platform", "local"].map((r) => h("option", { key: r, value: r }, r)))),
        h("div", { className: "dr" }, h("span", null, "connection"),
          h("select", { value: demo.conn, onChange: (e) => setDemo({ ...demo, conn: e.target.value }), "aria-label": "demo connection" },
            [["ready", "ready"], ["notready", "not-ready"], ["offline", "unreachable"]].map(([v, l]) => h("option", { key: v, value: v }, l)))),
        h("div", { className: "dr" }, h("span", null, "install"),
          h("select", { value: demo.install, onChange: (e) => setDemo({ ...demo, install: e.target.value }), "aria-label": "demo install" },
            [["seeded", "seeded starter"], ["empty", "empty install"]].map(([v, l]) => h("option", { key: v, value: v }, l)))),
        h("div", { className: "dr" }, h("span", null, "theme"),
          h("select", { value: demo.theme, onChange: (e) => { setDemo({ ...demo, theme: e.target.value }); document.documentElement.setAttribute("data-theme", e.target.value); }, "aria-label": "demo theme" },
            ["dark", "light"].map((t) => h("option", { key: t, value: t }, t)))),
      ) : null,
    );
  }

  // ---------- shell ----------
  const NAV = [
    ["overview", "Overview"], ["mocks", "Mocks"], ["lab", "Test Lab"],
    ["investigate", "Investigate"], ["reports", "Reports"], ["admin", "Administration"],
  ];
  function Shell({ demo, setDemo, route, go, children }) {
    return h("div", { className: "app" },
      h("aside", { className: "side" },
        h("div", { className: "brand" }, h("div", { className: "mark" }, "MA"),
          h("div", null, h("b", null, "MockAgents"), h("span", null, "Reliability workbench"))),
        h("div", { className: "palette", role: "button", tabIndex: 0 }, "Search or jump to…", h("span", { className: "k" }, "⌘K")),
        h("nav", { className: "nav" },
          NAV.map(([id, label]) => h("button", {
            key: id, className: "nav-item" + (route.view === id ? " on" : ""), onClick: () => go(id),
          }, label,
            id === "mocks" && demo.install === "seeded" ? h("span", { className: "k" }, "5") : null,
            id === "admin" && demo.role === "viewer" ? h("span", { className: "lock", title: "limited for viewer" }, "limited") : null,
          )),
        ),
        h("div", { className: "side-foot" }, "localhost:8080 · engine v0.4.2"),
      ),
      h("div", { className: "main" },
        h(Strip, { demo }),
        demo.conn === "offline" ? h("div", { className: "offline-bar", role: "alert" },
          h("b", null, "Server unreachable."), "Showing data loaded at 14:32:04Z. Reads are frozen; writes and runs are disabled. No automatic retry of stateful actions.",
          h("span", { className: "when" }, "last attempt 14:33:05Z"),
          h("button", { className: "btn out", style: { height: 24, fontSize: 11 } }, "Retry connection")) : null,
        h("main", { className: "content" }, h("div", { className: "content-inner" }, children)),
      ),
      h(DemoBar, { demo, setDemo }),
    );
  }

  // ---------- Overview (UX-02) ----------
  function Overview({ demo, go }) {
    const seeded = demo.install === "seeded";
    const offline = demo.conn === "offline";
    const notready = demo.conn === "notready";
    return h("div", null,
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("h1", { className: "page" }, "Overview"),
          h("p", { className: "lede" }, "Readiness and liveness are verified separately. Nothing on this page is inferred from an empty list."),
        ),
        h(Sid, null, "UX-02 · editor+viewer · GET /healthz, /readyz")),
      notready ? h("div", { className: "banner warn mt-4", role: "alert" },
        h("div", null, h("b", null, "Process is up but not ready. "), "Agents directory failed to parse: ", h("code", { className: "mono" }, "agents/broken.yaml:12"), " — fix the file or remove it, then reload. Catalog reads are unavailable; this is not an empty install.")) : null,
      h("div", { className: "receipt-grid mt-4" },
        h("div", { className: "rc" }, h("div", { className: "l" }, "Server"), h("div", { className: "v" }, h("span", { className: "mono" }, "localhost:8080")), h("div", { className: "s" }, demo.role === "local" ? "local mode · unauthenticated (explicit)" : "authenticated · acme-prod")),
        h("div", { className: "rc" }, h("div", { className: "l" }, "Liveness"),
          offline ? h("div", { className: "v", style: { color: "var(--sr-danger-fg)" } }, h("span", { className: "dot bad" }), "Unreachable") : h("div", { className: "v", style: { color: "var(--sr-success-fg)" } }, h("span", { className: "dot ok" }), "Process up"),
          h("div", { className: "s" }, offline ? "last success 14:32:04Z" : "checked 14:32:04Z")),
        h("div", { className: "rc" }, h("div", { className: "l" }, "Readiness"),
          offline ? h("div", { className: "v" }, "Unknown") : notready ? h("div", { className: "v", style: { color: "var(--sr-warning-fg)" } }, h("span", { className: "dot warn" }), "Not ready") : h("div", { className: "v", style: { color: "var(--sr-success-fg)" } }, h("span", { className: "dot ok" }), "Ready"),
          h("div", { className: "s" }, offline ? "cannot verify while unreachable" : notready ? "agents dir parse error" : "agents dir parsed · store open")),
        h("div", { className: "rc" }, h("div", { className: "l" }, "Configuration revision"), h("div", { className: "v" }, h("span", { className: "mono" }, "per-resource")), h("div", { className: "s" }, "no global revision — shown on each mock")),
      ),
      h("div", { className: "grid2 mt-4 two-col", style: { gridTemplateColumns: "1.25fr .75fr", alignItems: "start" } },
        h("div", { className: "col gap-4" },
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "First-run checklist"), h("span", { className: "sub" }, seeded ? "2 of 4 complete" : "0 of 3 complete"), h("div", { className: "right" }, h(Sid, null, "UX-02"))),
            seeded ? [
              h(Check, { key: 1, done: true, label: "Server connected and ready", meta: "14:31:58Z" }),
              h(Check, { key: 2, done: true, label: "Starter agent support-starter loaded", act: "Open agent", onAct: () => go("mocks") }),
              h(Check, { key: 3, done: false, label: "Point an SDK at this server and send one request", act: "Copy settings" }),
              h(Check, { key: 4, done: false, label: "Run seeded pipeline support-triage against active configuration", act: "Open Test Lab", onAct: () => go("lab") }),
            ] : [
              h(Check, { key: 1, done: demo.conn === "ready", label: "Server connected and ready", meta: demo.conn === "ready" ? "14:31:58Z" : null }),
              h(Check, { key: 2, done: false, label: "Add an agent YAML to your agents directory", act: "Setup guide" }),
              h(Check, { key: 3, done: false, label: "Seed the documented starter pipeline (CLI)", act: "Instructions" }),
            ],
          ),
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Recent failed runs"), h("span", { className: "sub" }, offline ? "frozen at 14:32:04Z" : "last 24 h · loaded 3 of 3"), h("div", { className: "right" }, h(Sid, null, "UX-02 · UX-04"))),
            seeded ? h("table", { className: "tbl" },
              h("thead", null, h("tr", null, h("th", null, "session"), h("th", null, "pipeline / agent"), h("th", null, "outcome"), h("th", null, "failed node"), h("th", { style: { textAlign: "right" } }, "latency"), h("th", { style: { textAlign: "right" } }, "when"))),
              h("tbody", null,
                h("tr", { className: "click", onClick: () => go("lab") }, h("td", { className: "mono" }, "sess_d4e5f6"), h("td", { className: "mono" }, "support-triage"), h("td", null, h(Badge, { kind: "warn" }, "422 partial")), h("td", { className: "mono" }, "rag-lookup"), h("td", { className: "num" }, "310.5 ms"), h("td", { className: "num muted" }, "11m ago")),
                h("tr", { className: "click", onClick: () => go("investigate") }, h("td", { className: "mono" }, "sess_8e07aa"), h("td", { className: "mono" }, "flaky-agent"), h("td", null, h(Badge, { kind: "bad" }, "503 chaos")), h("td", { className: "mono muted" }, "—"), h("td", { className: "num" }, "201.4 ms"), h("td", { className: "num muted" }, "42m ago")),
                h("tr", { className: "click", onClick: () => go("investigate") }, h("td", { className: "mono" }, "sess_77b3d0"), h("td", { className: "mono" }, "support-starter"), h("td", null, h(Badge, { kind: "bad" }, "timeout")), h("td", { className: "mono" }, "summarizer"), h("td", { className: "num" }, "5,000.0 ms"), h("td", { className: "num muted" }, "3h ago")),
              )) : h("div", { className: "card-pad" }, h("div", { className: "empty" }, "No runs yet — runs appear here after your first Test Lab execution.")),
            h("div", { className: "card-f prov" }, "Synthetic fixture data · provenance: seeded demo store · latencies converted from nanosecond wire values"),
          ),
        ),
        h("div", { className: "col gap-4" },
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Starter · SDK settings"), h("div", { className: "right" }, h(Sid, null, "UX-02"))),
            h("dl", { className: "kv card-pad", style: { paddingBottom: 8 } },
              h("dt", null, "base URL"), h("dd", { className: "mono row gap-2" }, "http://localhost:8080/v1 ", h(CopyBtn)),
              h("dt", null, "api key"), h("dd", { className: "mono row gap-2" }, "mock-any-string ", h(CopyBtn)),
              h("dt", null, "starter agent"), h("dd", { className: "mono" }, seeded ? "support-starter" : "— not loaded"),
              h("dt", null, "protocol"), h("dd", { className: "mono" }, "openai-chat-completions"),
            ),
            h("div", { className: "card-pad row gap-2", style: { paddingTop: 8, borderTop: "1px solid var(--sr-border)" } },
              h("button", { className: "btn pri", disabled: !seeded || offline, onClick: () => go("mocks") }, "Open starter agent"),
              h("button", { className: "btn out" }, "First-request guide")),
          ),
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Pipelines"), h("div", { className: "right" }, h(Sid, null, "UX-02 · empty state"))),
            seeded ? h("div", { className: "card-pad" },
              h("div", { className: "row", style: { justifyContent: "space-between" } },
                h("div", null, h("div", { className: "mono", style: { fontWeight: 600, fontSize: 13 } }, "support-triage"), h("div", { className: "muted txt-xs" }, "3 agents · 2 edges · revision 5e12")),
                h("button", { className: "btn out", disabled: offline, onClick: () => go("lab") }, "Open in Test Lab")),
            ) : h("div", { className: "card-pad" },
              h("div", { className: "banner info mb-3" }, h("div", null, h("b", null, "Pipeline creation is not part of Release A. "), "Seed one from the documented starter to enable Test Lab runs.")),
              h("pre", { className: "codeblock" }, "cp examples/support-triage.yaml agents/\nmockagents reload"),
              h("button", { className: "btn out mt-3" }, "Setup instructions"),
            ),
            h("div", { className: "card-f" }, "A Run control only appears when a pipeline exists — never a dead button."),
          ),
        ),
      ),
    );
  }
  function Check({ done, label, meta, act, onAct }) {
    return h("div", { style: { display: "flex", alignItems: "center", gap: 12, padding: "11px 16px", borderBottom: "1px solid var(--sr-border)", fontSize: 13 } },
      h("span", { style: { width: 19, height: 19, borderRadius: 999, flex: "0 0 19px", display: "grid", placeItems: "center", fontSize: 11, background: done ? "var(--sr-success-bg)" : "none", color: done ? "var(--sr-success-fg)" : "var(--sr-fg-muted)", border: done ? "none" : "1.5px dashed var(--sr-border)" } }, done ? "✓" : ""),
      h("span", { className: done ? "muted" : "", style: done ? { textDecoration: "line-through" } : null }, label),
      meta ? h("span", { className: "mono muted", style: { marginLeft: "auto", fontSize: 10.5 } }, meta) :
        act ? h("button", { className: "btn ghost", style: { marginLeft: "auto", height: 24, fontSize: 12, textDecoration: "underline", textUnderlineOffset: 2 }, onClick: onAct }, act) : null,
    );
  }

  Object.assign(window, { WBShell: Shell, WBOverview: Overview, WBBadge: Badge, WBSid: Sid, WBCopyBtn: CopyBtn });
})();
