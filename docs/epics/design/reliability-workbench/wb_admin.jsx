/* Workbench — Reports (UX-06) + Administration (UX-07). */
(function () {
  const { useState } = React;
  const h = React.createElement;
  const { WBBadge: Badge, WBSid: Sid, WB } = window;

  // ---------- Reports ----------
  function Reports({ demo }) {
    const [includeBodies, setIncludeBodies] = useState(false);
    return h("div", null,
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("h1", { className: "page" }, "Reports"),
          h("p", { className: "lede" }, "Release A exports the currently loaded, already-authorized snapshot — a bounded local export, not a server-attested or complete-history report. Missing information is “unknown”, never zero.")),
        h(Sid, null, "UX-06 · any authenticated role · browser export of loaded data")),
      h("div", { className: "grid2 mt-4 two-col", style: { alignItems: "start" } },
        h("div", { className: "card" },
          h("div", { className: "card-h" }, h("h2", null, "Export run evidence"), h("div", { className: "right" }, h("span", { className: "tag" }, "JSON canonical · HTML printable"))),
          h("div", { className: "card-pad col gap-3" },
            h("dl", { className: "kv" },
              h("dt", null, "scope"), h("dd", null, "run sess_d4e5f6 + 4 session log records currently loaded"),
              h("dt", null, "window"), h("dd", { className: "mono" }, "2026-09-02 08:32Z – 14:32Z (loaded view)"),
              h("dt", null, "revision"), h("dd", null, "support-triage 5e12; node agent revisions ", h("b", null, "unknown"), " (not recorded at run time)"),
              h("dt", null, "generated"), h("dd", { className: "mono" }, "on export · engine v0.4.2 · schema wb-export/1"),
              h("dt", null, "completeness"), h("dd", null, h(Badge, { kind: "warn" }, "partial"), h("span", { className: "muted txt-xs", style: { marginLeft: 8 } }, "1 unresolved log gap inside window")),
              h("dt", null, "omissions"), h("dd", { className: "muted" }, "1 truncated body · summarizer node not executed (unknown) · auth headers redacted")),
            h("div", { className: "receipt-line" },
              h("input", { type: "checkbox", id: "incbodies", checked: includeBodies, onChange: (e) => setIncludeBodies(e.target.checked) }),
              h("label", { htmlFor: "incbodies", className: "txt-sm", style: { cursor: "pointer" } }, "Include raw bodies (requires review below)")),
            includeBodies ? h("div", { className: "banner warn" }, h("div", null, h("b", null, "Included-data review required. "), "2 bodies will be embedded as text. Preview shows exactly what leaves this browser; secrets are redacted by policy ", h("code", { className: "mono" }, "redact/2026-06"), " — verify before sharing.")) : null,
          ),
          h("div", { className: "card-f row gap-2", style: { padding: "10px 16px" } },
            h("button", { className: "btn pri" }, "Preview export content"),
            h("button", { className: "btn out" }, "Download JSON"),
            h("button", { className: "btn out" }, "Printable HTML")),
        ),
        h("div", { className: "col gap-4" },
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Estimated upstream cost"), h("div", { className: "right" }, h(Sid, null, "UX-06 · cost labels"))),
            h("div", { className: "card-pad col gap-3" },
              h("div", { className: "row", style: { alignItems: "baseline", gap: 8 } },
                h("span", { style: { fontSize: 24, fontWeight: 650, fontFamily: "var(--sr-font-mono)" } }, "$3.41"),
                h("span", { className: "muted txt-sm" }, "estimated upstream cost for captured requests")),
              h("div", { className: "banner info" }, h("div", null, "Estimate, not verified savings. Pricing table ", h("code", { className: "mono" }, "pricing/2026-08"), " · token coverage 92% of captured requests · sampled scan of 4,988 rows — ", h("b", null, "visibly partial"), ", not a full-window total.")),
              h("dl", { className: "kv" },
                h("dt", null, "rows scanned"), h("dd", { className: "mono" }, "4,988 of unknown total (scan cap)"),
                h("dt", null, "unpriced models"), h("dd", { className: "mono" }, "1 (stream-faulty) — shown as unknown, not $0"))),
          ),
          h("div", { className: "card card-pad" },
            h("div", { className: "eyebrow mb-2" }, "not in release a"),
            h("div", { className: "txt-sm muted" }, "Server-attested reports, complete-history windows, scheduled delivery, and PDF are later capabilities ", h("span", { className: "tag b" }, "Release B+"), ". This page never presents the loaded snapshot as a complete record.")),
        ),
      ),
    );
  }

  // ---------- Administration ----------
  function Admin({ demo }) {
    const role = demo.role;
    const [delOpen, setDelOpen] = useState(false);
    const [delName, setDelName] = useState("");
    const [minted, setMinted] = useState(false);
    const canAudit = role === "admin" || role === "platform";
    const canTenant = role === "platform";
    const canKeys = role === "editor" || role === "admin" || role === "platform";
    return h("div", null,
      h("div", { className: "row", style: { alignItems: "flex-start" } },
        h("div", { className: "grow" },
          h("h1", { className: "page" }, "Administration"),
          h("p", { className: "lede" }, "Navigation is role-truthful: sections you cannot use say why. Viewer is not a strict read-only role — it can run pipelines and rotate its own key.")),
        h(Sid, null, "UX-07 · floors: keys editor+ · audit admin · tenants platform")),
      role === "local" ? h("div", { className: "banner info mt-4" }, h("div", null, h("b", null, "Local unauthenticated mode. "), "Tenant and key administration do not apply; this is an explicit mode, not a synthetic viewer identity. Deployment guidance warns against exposing this mode remotely.")) : null,
      h("div", { className: "grid2 mt-4 two-col", style: { alignItems: "start" } },
        h("div", { className: "col gap-4" },
          // keys
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "API keys · acme-prod"), h("div", { className: "right" },
              canKeys ? h("button", { className: "btn pri", style: { height: 28 }, onClick: () => setMinted(true) }, "Mint key") : h("span", { className: "tag" }, "own-key rotation only for " + role))),
            minted ? h("div", { style: { padding: "12px 16px", borderBottom: "1px solid var(--sr-border)" } },
              h("div", { className: "banner ok" }, h("div", { className: "grow" }, h("b", null, "Key created — shown exactly once. "), "It is bcrypt-hashed after this dialog; there is no second look.",
                h("div", { className: "row gap-2 mt-2" }, h("code", { className: "mono", style: { background: "var(--sr-bg)", padding: "3px 9px", borderRadius: 6, fontSize: 11.5 } }, "mak_f81b22c9_9a4d1e0b77c3"), h("button", { className: "copybtn" }, "copy"))),
                h("button", { className: "bx", onClick: () => setMinted(false), "aria-label": "dismiss" }, "×"))) : null,
            h("table", { className: "tbl" },
              h("thead", null, h("tr", null, h("th", null, "name"), h("th", null, "prefix"), h("th", null, "role"), h("th", null, "last used"), h("th", null, ""))),
              h("tbody", null, WB.KEYS.map((k) => h("tr", { key: k.id },
                h("td", null, h("span", { style: { fontWeight: 600 } }, k.name)),
                h("td", { className: "mono muted" }, k.prefix + "…"),
                h("td", null, h("span", { className: "tag" }, k.role)),
                h("td", { className: "muted txt-xs" }, k.last_used),
                h("td", { style: { textAlign: "right" } }, h("button", { className: "btn ghost", style: { height: 24, fontSize: 11.5 }, disabled: !canKeys && k.name !== "ci-bot" }, "rotate")))))),
            h("div", { className: "card-f" }, "Plaintext is never re-displayed or auto-copied. Locked out? Recovery requires filesystem access to the server host — see recovery guide."),
          ),
          // tenants (platform only)
          h("div", { className: "card" },
            h("div", { className: "card-h" }, h("h2", null, "Tenants"), h("div", { className: "right" }, h(Sid, null, "platform role"))),
            canTenant ? h("div", null,
              h("table", { className: "tbl" },
                h("thead", null, h("tr", null, h("th", null, "tenant"), h("th", null, "keys"), h("th", null, "quota"), h("th", null, ""))),
                h("tbody", null,
                  h("tr", null, h("td", { style: { fontWeight: 600 } }, "acme-prod"), h("td", { className: "num" }, "3"), h("td", { className: "mono muted" }, "10k req/day"), h("td", { style: { textAlign: "right" } }, h("button", { className: "btn ghost", style: { height: 24, fontSize: 11.5, color: "var(--sr-danger-fg)" }, onClick: () => setDelOpen(true) }, "delete"))),
                  h("tr", null, h("td", { style: { fontWeight: 600 } }, "globex"), h("td", { className: "num" }, "2"), h("td", { className: "mono muted" }, "10k req/day"), h("td", { style: { textAlign: "right" } }, h("button", { className: "btn ghost", style: { height: 24, fontSize: 11.5, color: "var(--sr-danger-fg)" } }, "delete")))))) :
              h("div", { className: "card-pad" }, h("div", { className: "banner info" }, h("div", null, h("b", null, "Requires the platform role. "), "Your role (" + role + ") cannot list or manage tenants — cross-tenant controls are forbidden, and this panel says so rather than rendering an empty list."))),
          ),
        ),
        // audit
        h("div", { className: "card" },
          h("div", { className: "card-h" }, h("h2", null, "Audit timeline"), h("div", { className: "right" }, h(Sid, null, "admin role"))),
          canAudit ? h("div", null,
            h("table", { className: "tbl" },
              h("thead", null, h("tr", null, h("th", null, "event"), h("th", null, "actor"), h("th", null, "target"), h("th", null, "when"))),
              h("tbody", null, WB.AUDIT.map((e) => h("tr", { key: e.id },
                h("td", null, h("span", { className: "tag" + (e.kind === "auth.denied" ? " b" : "") }, e.kind)),
                h("td", null, h("div", null, h("span", { style: { fontWeight: 500, fontSize: 12 } }, e.actor), h("div", { className: "muted mono", style: { fontSize: 10 } }, e.role))),
                h("td", { className: "mono muted", style: { fontSize: 11 } }, e.target),
                h("td", { className: "mono muted", style: { fontSize: 11 } }, e.ts))))),
            h("div", { className: "card-f" }, "Unknown future event kinds render read-only (see unknown.future_event) — the taxonomy filter never hides them."))
            : h("div", { className: "card-pad" }, h("div", { className: "banner info" }, h("div", null, h("b", null, "Audit requires the admin role. "), "There is no viewer audit tab or export — this section states the floor instead of pretending to be empty."))),
        ),
      ),
      delOpen ? h("div", { className: "overlay", role: "dialog", "aria-modal": true, "aria-label": "confirm tenant deletion" },
        h("div", { className: "dialog" },
          h("div", { className: "dialog-h" }, h("h3", null, "Delete tenant acme-prod?")),
          h("div", { className: "dialog-b col gap-3" },
            h("div", { className: "banner bad" }, h("div", null, h("b", null, "Irreversible. "), "Deletes the tenant record, revokes 3 API keys immediately, and orphans this tenant's audit references. Mock definitions on disk are not deleted. Sessions using revoked keys fail on their next request.")),
            h("div", { className: "field" },
              h("label", { htmlFor: "delname" }, "Type the exact tenant name to confirm"),
              h("input", { id: "delname", className: "input mono", value: delName, onChange: (e) => setDelName(e.target.value), placeholder: "acme-prod", autoFocus: true })),
          ),
          h("div", { className: "dialog-f" },
            h("button", { className: "btn out", onClick: () => { setDelOpen(false); setDelName(""); } }, "Cancel"),
            h("button", { className: "btn danger", disabled: delName !== "acme-prod" }, "Delete tenant")),
        )) : null,
    );
  }

  Object.assign(window, { WBReports: Reports, WBAdmin: Admin });
})();
