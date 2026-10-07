// Agent Playground UI: vanilla JS, no build step, no external assets.
// Every value that originates from a model or user is HTML-escaped (esc).
"use strict";

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const fmtJSON = (v) => esc(JSON.stringify(v, null, 2));
const money = (v) => "$" + (Number(v) || 0).toFixed(6);
const badge = (cls, text) => `<span class="badge ${esc(cls)}">${esc(text ?? cls)}</span>`;
const ms = (v) => (v == null ? "" : `${v}`);

const state = {
  tab: "workflows",
  workflows: [],
  selectedRun: null,
  subscribedRun: null,
  runSource: null,
  agents: [],
  tools: [],
  config: null,
  selectedAgent: null,
  traceTimer: null,
};

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------
async function api(method, path, body, headers = {}) {
  const opts = { method, headers: { ...headers } };
  if (body !== undefined) {
    if (typeof body === "string") {
      opts.body = body;
    } else {
      opts.body = JSON.stringify(body);
      opts.headers["Content-Type"] = opts.headers["Content-Type"] || "application/json";
    }
  }
  const token = localStorageGet("playground-token");
  if (token) opts.headers["Authorization"] = "Bearer " + token;
  const resp = await fetch(path, opts);
  const ct = resp.headers.get("Content-Type") || "";
  const data = ct.includes("json") ? await resp.json().catch(() => null) : await resp.text();
  if (!resp.ok) {
    const e = new Error((data && data.error && data.error.message) || `HTTP ${resp.status}`);
    e.status = resp.status;
    e.body = data;
    throw e;
  }
  return data;
}

function localStorageGet(k) {
  try { return localStorage.getItem(k); } catch { return null; }
}

let toastTimer;
function toast(msg, isErr = false) {
  let el = $("#toast");
  if (!el) {
    el = document.createElement("div");
    el.id = "toast";
    document.body.appendChild(el);
  }
  el.className = "toast" + (isErr ? " err" : "");
  el.textContent = msg;
  el.style.display = "block";
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.style.display = "none"), isErr ? 7000 : 3000);
}

function errDetails(e) {
  const d = e.body && e.body.error && e.body.error.details;
  if (!d) return e.message;
  if (Array.isArray(d)) return e.message + "\n" + d.map((x) => (x.path ? `${x.path}: ${x.message}` : String(x))).join("\n");
  return e.message + "\n" + JSON.stringify(d);
}

// ---------------------------------------------------------------------------
// Tabs + header
// ---------------------------------------------------------------------------
function showTab(name) {
  state.tab = name;
  $$("#tabs button").forEach((b) => b.classList.toggle("active", b.dataset.tab === name));
  $$("main > section").forEach((s) => s.classList.toggle("hidden", s.id !== "tab-" + name));
  ({ workflows: loadWorkflows, runs: loadRuns, reviews: loadReviews, agents: loadAgents, chat: loadChat, mock: loadMock, api: loadAPI }[name] || (() => {}))();
  try { history.replaceState(null, "", "#" + name + (name === "runs" && state.selectedRun ? "/" + state.selectedRun : "")); } catch {}
}

async function refreshHeader() {
  try {
    const info = await api("GET", "/api/info");
    const n = info.pending_reviews || 0;
    const c = $("#review-count");
    c.textContent = n;
    c.classList.toggle("hidden", n === 0);
    $("#mock-dot").className = "status-dot up";
    $("#mock-label").textContent = `mock: ${info.mock.mode} · review: ${info.review_mode}`;
    $("#mock-pill").title = info.mock.url;
  } catch {
    $("#mock-dot").className = "status-dot down";
    $("#mock-label").textContent = "playground unreachable";
  }
}

// ---------------------------------------------------------------------------
// Workflows
// ---------------------------------------------------------------------------
async function loadWorkflows() {
  if (!state.workflows.length) state.workflows = (await api("GET", "/api/workflows")).workflows;
  const root = $("#workflow-cards");
  root.innerHTML = state.workflows.map((w) => `
    <div class="panel" data-wf="${esc(w.name)}">
      <h2>${esc(w.title)} <span class="muted small">${esc(w.name)}</span></h2>
      <p class="small">${esc(w.summary)}</p>
      <div class="chips">${w.patterns.map((p) => `<span class="chip">${esc(p)}</span>`).join("")}</div>
      <div class="small muted">agents: ${w.agents.map(esc).join(", ")}</div>
      <h3>Input</h3>
      <div class="row small">${(w.examples || []).map((e, i) => `<button class="ghost ex" data-i="${i}" title="${esc(e.description)}">${esc(e.name)}</button>`).join("")}</div>
      <div class="small muted ex-desc">${esc((w.examples && w.examples[0] && w.examples[0].description) || "")}</div>
      <textarea class="code wf-input">${esc(JSON.stringify((w.examples && w.examples[0] && w.examples[0].input) || {}, null, 2))}</textarea>
      <div class="row">
        <label class="field">Review mode<select class="wf-review"><option value="">default</option><option value="manual">manual (you decide)</option><option value="auto">auto (policy approves)</option></select></label>
        <label class="field">Max retries<select class="wf-retry"><option value="">agent default</option>${[0, 1, 2, 3, 4, 5].map((n) => `<option>${n}</option>`).join("")}</select></label>
        <label class="row small" style="margin-top:16px"><input type="checkbox" class="wf-stream"> stream</label>
        <span style="flex:1"></span>
        <button class="primary wf-run" style="margin-top:16px">Launch run</button>
      </div>
    </div>`).join("");
  $$("[data-wf]", root).forEach((card) => {
    const wf = state.workflows.find((w) => w.name === card.dataset.wf);
    $$(".ex", card).forEach((b) => b.addEventListener("click", () => {
      const ex = wf.examples[Number(b.dataset.i)];
      $(".wf-input", card).value = JSON.stringify(ex.input, null, 2);
      $(".ex-desc", card).textContent = ex.description;
    }));
    $(".wf-run", card).addEventListener("click", async () => {
      let input;
      try { input = JSON.parse($(".wf-input", card).value || "{}"); } catch (e) { return toast("Input is not valid JSON: " + e.message, true); }
      const options = {};
      if ($(".wf-review", card).value) options.review_mode = $(".wf-review", card).value;
      if ($(".wf-retry", card).value !== "") options.retry = { max_retries: Number($(".wf-retry", card).value), initial_backoff_ms: 200, max_backoff_ms: 4000, multiplier: 2, jitter: 0.2 };
      if ($(".wf-stream", card).checked) options.stream = true;
      try {
        const run = await api("POST", `/api/workflows/${encodeURIComponent(wf.name)}/runs`, { input, options });
        toast(`Started ${run.id}`);
        state.selectedRun = run.id;
        showTab("runs");
      } catch (e) { toast(errDetails(e), true); }
    });
  });
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------
async function loadRuns() {
  const status = $("#runs-filter").value;
  const data = await api("GET", "/api/runs?limit=100" + (status ? "&status=" + status : ""));
  const c = data.counts || {};
  $("#run-counts").innerHTML = `${badge("in_progress", `in_progress ${c.in_progress || 0}`)} ${badge("completed", `completed ${c.completed || 0}`)} ${badge("failed", `failed ${c.failed || 0}`)}`;
  $("#runs-body").innerHTML = data.runs.length ? data.runs.map((r) => `
    <tr class="click ${r.id === state.selectedRun ? "selected" : ""}" data-run="${esc(r.id)}">
      <td><b>${esc(r.workflow)}</b><div class="small muted">${esc(r.id)}</div></td>
      <td>${badge(r.status)}${r.status === "in_progress" ? " " + badge(r.phase) : ""}${r.error ? `<div class="small muted">${esc(r.error.code)}</div>` : ""}</td>
      <td class="num">${ms(r.status === "in_progress" ? Date.now() - new Date(r.created_at).getTime() : r.duration_ms)}</td>
    </tr>`).join("") : `<tr><td colspan="3" class="empty">No runs yet.</td></tr>`;
  $$("#runs-body tr[data-run]").forEach((tr) => tr.addEventListener("click", () => selectRun(tr.dataset.run)));
  if (state.selectedRun && state.subscribedRun !== state.selectedRun) selectRun(state.selectedRun);
}

function selectRun(id) {
  state.selectedRun = id;
  state.subscribedRun = id;
  $$("#runs-body tr[data-run]").forEach((tr) => tr.classList.toggle("selected", tr.dataset.run === id));
  if (state.runSource) state.runSource.close();
  state.runSource = new EventSource(`/api/runs/${encodeURIComponent(id)}/events`);
  state.runSource.addEventListener("run", (ev) => renderRun(JSON.parse(ev.data)));
  state.runSource.addEventListener("done", () => {
    state.runSource.close();
    state.runSource = null;
    loadRuns();
    loadRunExtras(id);
  });
  state.runSource.onerror = () => {
    if (state.runSource) state.runSource.close();
    state.runSource = null;
  };
  try { history.replaceState(null, "", "#runs/" + id); } catch {}
}

function renderRun(run) {
  if (run.id !== state.selectedRun) return;
  const s = run.stats || {};
  const root = $("#run-detail");
  const pendingHolder = $("#run-pending");
  const openState = Object.fromEntries($$("details[id]", root).map((d) => [d.id, d.open]));
  root.innerHTML = `
    <div class="panel">
      <div class="row">
        <h2 style="margin:0">${esc(run.workflow)}</h2>${badge(run.status)} ${run.status === "in_progress" ? badge(run.phase) : ""}
        <span class="muted small">${esc(run.id)} · ${ms(run.status === "in_progress" ? Date.now() - new Date(run.created_at).getTime() : run.duration_ms)}ms</span>
        <span style="flex:1"></span>
        ${run.status === "in_progress" ? `<button class="danger" id="run-cancel">Cancel</button>` : ""}
      </div>
      ${run.error ? `<pre style="border-color:var(--err)">${esc(run.error.code)}: ${esc(run.error.message)}${run.error.step ? "\nstep: " + esc(run.error.step) : ""}</pre>` : ""}
      <div class="row" style="margin-top:8px">
        <span class="stat"><b>${s.llm_calls || 0}</b><span>agent calls</span></span>
        <span class="stat"><b><span style="color:var(--slm)">${s.slm_calls || 0}</span> / <span style="color:var(--llm)">${s.llm_tier_calls || 0}</span></b><span>SLM / LLM</span></span>
        <span class="stat"><b>${s.attempts || 0}</b><span>HTTP attempts</span></span>
        <span class="stat"><b>${s.retries || 0}</b><span>retries</span></span>
        <span class="stat"><b>${s.fallbacks || 0}</b><span>fallbacks</span></span>
        <span class="stat"><b>${s.escalations || 0}</b><span>escalations</span></span>
        <span class="stat"><b>${s.tool_calls || 0}</b><span>tool calls</span></span>
        <span class="stat"><b>${money(s.cost_usd)}</b><span>est. cost</span></span>
      </div>
      <div id="run-pending">${pendingHolder ? pendingHolder.innerHTML : ""}</div>
    </div>
    <div class="panel"><h2>Steps</h2><ul class="steps">${run.steps.map(stepHTML).join("") || '<li class="muted">starting…</li>'}</ul></div>
    <div class="panel"><h2>Trace</h2>
      <div class="legend">${["workflow", "step", "agent", "router", "llm", "attempt", "tool", "guard", "review"].map((k) => `<span><i class="k-${k}"></i>${k}</span>`).join("")}<span><i style="background:var(--err)"></i>error</span></div>
      <div class="trace" id="run-trace"><div class="muted small">loading…</div></div></div>
    ${run.output !== undefined && run.output !== null ? `<div class="panel"><h2>Output</h2><pre>${fmtJSON(run.output)}</pre></div>` : ""}
    <div class="panel"><details id="d-events"><summary><b>Live events</b> <span class="muted small">(${(run.events || []).length})</span></summary>
      <table><tbody>${(run.events || []).slice().reverse().map((e) => `<tr><td class="small muted">${esc(new Date(e.time).toLocaleTimeString())}</td><td>${badge("running", e.type)}</td><td class="small">${esc(JSON.stringify(e.data || {}))}</td></tr>`).join("")}</tbody></table></details></div>
    <div class="panel"><details id="mock-side"><summary><b>Mock side of this run</b> <span class="muted small">(mockagents interaction log filtered by session prefix)</span></summary><div id="run-mocklog" class="small muted">loading…</div></details></div>
    <div class="panel"><details id="d-input"><summary><b>Input &amp; options</b></summary><pre>${fmtJSON({ input: run.input, options: run.options, deadline: run.deadline })}</pre></details></div>`;
  const cancel = $("#run-cancel");
  if (cancel) cancel.addEventListener("click", async () => {
    try { await api("POST", `/api/runs/${run.id}/cancel`, { reason: "cancelled from the UI" }); toast("Run cancelled"); } catch (e) { toast(e.message, true); }
  });
  Object.entries(openState).forEach(([id, open]) => { const d = $("#" + id, root); if (d) d.open = open; });
  $("#mock-side").addEventListener("toggle", () => loadRunMockLog(run.id));
  scheduleTrace(run.id);
  if (run.status === "in_progress") loadRunPending(run.id);
  else $("#run-pending").innerHTML = "";
}

function stepHTML(s) {
  const meta = [];
  if (s.tier) meta.push(badge(s.tier));
  if (s.model) meta.push(`<code class="small">${esc(s.model)}</code>`);
  if (s.retries) meta.push(badge("pending", `${s.retries} retries`));
  if (s.fallback) meta.push(badge("revised", "fallback"));
  if (s.escalated) meta.push(badge("llm", "escalated"));
  return `<li class="${esc(s.status)}"><span class="name">${esc(s.name)}</span> ${badge(s.status)} ${meta.join(" ")} <span class="muted small">${ms(s.duration_ms)}ms</span>
    ${(s.notes || []).map((n) => `<div class="note">• ${esc(n)}</div>`).join("")}
    ${s.error ? `<div class="err">${esc(s.error)}</div>` : ""}</li>`;
}

function scheduleTrace(id) {
  if (state.traceTimer) return;
  state.traceTimer = setTimeout(async () => {
    state.traceTimer = null;
    try { renderTrace((await api("GET", `/api/runs/${encodeURIComponent(id)}/trace`)).spans); } catch {}
  }, 400);
}

function renderTrace(spans) {
  const el = $("#run-trace");
  if (!el || !spans.length) return;
  const t = (x) => new Date(x).getTime();
  const t0 = Math.min(...spans.map((s) => t(s.start)));
  const now = Date.now();
  const tEnd = Math.max(...spans.map((s) => (s.end ? t(s.end) : now)));
  const total = Math.max(tEnd - t0, 1);
  const kids = {};
  spans.forEach((s) => (kids[s.parent_id || ""] = kids[s.parent_id || ""] || []).push(s));
  const rows = [];
  const walk = (pid, depth) => (kids[pid] || []).sort((a, b) => t(a.start) - t(b.start)).forEach((s) => { rows.push([s, depth]); walk(s.id, depth + 1); });
  walk("", 0);
  el.innerHTML = rows.map(([s, d]) => {
    const start = t(s.start) - t0;
    const dur = (s.end ? t(s.end) : now) - t(s.start);
    const attrs = Object.entries(s.attrs || {}).filter(([k]) => ["tier", "model", "reason", "http_status", "error_class", "outcome", "action", "finish_reason", "retry_after_ms"].includes(k)).map(([k, v]) => `${k}=${typeof v === "object" ? JSON.stringify(v) : v}`).join(" ");
    const title = esc(JSON.stringify({ attrs: s.attrs, events: s.events, error: s.error }, null, 1));
    return `<div class="line" title="${title}">
      <div class="label" style="padding-left:${d * 14}px">${s.status === "error" ? "✗" : s.status === "running" ? "…" : "✓"} ${esc(s.name)} <span class="muted">${esc(attrs)}</span>${(s.events || []).length ? ` <span class="badge pending">${s.events.length} evt</span>` : ""}</div>
      <div class="track"><div class="bar k-${esc(s.kind)} ${s.status === "error" ? "error" : ""}" style="left:${(start / total) * 100}%;width:${Math.max((dur / total) * 100, 0.3)}%"></div></div>
      <div class="dur">${dur}ms</div></div>`;
  }).join("");
}

async function loadRunPending(id) {
  try {
    const data = await api("GET", `/api/reviews?status=pending&blocking=true&run_id=${encodeURIComponent(id)}`);
    const holder = $("#run-pending");
    if (holder) holder.innerHTML = data.reviews.map(reviewHTML).join("");
    bindReviewButtons(holder, () => {});
  } catch {}
}

async function loadRunExtras(id) {
  scheduleTrace(id);
  const d = $("#mock-side");
  if (d && d.open) loadRunMockLog(id);
}

async function loadRunMockLog(id) {
  const el = $("#run-mocklog");
  if (!el) return;
  try {
    const logs = await api("GET", `/api/mock/logs?limit=200&session_prefix=${encodeURIComponent(id)}`);
    el.innerHTML = logs.length ? mockLogTable(logs) : "No interactions recorded for this run.";
  } catch (e) { el.textContent = e.message; }
}

function mockLogTable(logs) {
  return `<table><thead><tr><th>#</th><th>mock agent</th><th>scenario</th><th>path</th><th class="num">status</th><th class="num">tokens</th><th class="num">cost</th><th>session</th></tr></thead><tbody>${
    logs.map((l) => `<tr><td>${esc(l.id)}</td><td>${esc(l.agent_name)}</td><td><code>${esc(l.scenario_name || "")}</code></td><td class="small">${esc(l.request_path)}${l.streaming ? " (SSE)" : ""}</td>
      <td class="num">${l.response_status >= 400 ? badge("failed", l.response_status) : esc(l.response_status)}</td><td class="num">${esc((l.prompt_tokens || 0) + "/" + (l.completion_tokens || 0))}</td>
      <td class="num">${money(l.cost_usd)}</td><td class="small muted">${esc(l.session_id || "")}</td></tr>`).join("")}</tbody></table>`;
}

// ---------------------------------------------------------------------------
// Reviews
// ---------------------------------------------------------------------------
function reviewHTML(r) {
  const actions = r.status === "pending" ? (r.allowed_actions || []) : [];
  const cls = { approve: "ok", reject: "danger", revise: "primary", flag: "danger" };
  return `<div class="review ${r.blocking ? "blocking" : ""}" data-review="${esc(r.id)}">
    <div class="row"><b>${esc(r.title)}</b> ${badge(r.status)} ${badge("running", r.kind)}
      <span class="muted small">${esc(r.id)} · run <a href="#runs/${esc(r.run_id)}" class="runlink" data-run="${esc(r.run_id)}">${esc(r.run_id)}</a>${r.agent ? " · agent " + esc(r.agent) : ""}</span></div>
    <div class="content">${esc(r.content)}</div>
    ${r.context ? `<details><summary class="small muted">context</summary><pre>${fmtJSON(r.context)}</pre></details>` : ""}
    ${r.decision ? `<div class="small muted">${esc(r.decision.action)} by ${esc(r.decision.reviewer)}${r.decision.comment ? ": " + esc(r.decision.comment) : ""}</div>` : ""}
    ${actions.length ? `<textarea class="rv-comment" placeholder="${actions.includes("revise") ? "Comment (required for revise: tells the editor agent what to change)" : "Optional comment"}"></textarea>
      <div class="row">${actions.map((a) => `<button class="${cls[a] || ""}" data-action="${esc(a)}">${esc(a)}</button>`).join("")}</div>` : ""}
  </div>`;
}

function bindReviewButtons(root, after) {
  if (!root) return;
  $$("[data-review]", root).forEach((card) => {
    $$("button[data-action]", card).forEach((b) => b.addEventListener("click", async () => {
      const comment = ($(".rv-comment", card) || {}).value || "";
      try {
        await api("POST", `/api/reviews/${card.dataset.review}/decision`, { action: b.dataset.action, comment, reviewer: $("#reviewer").value || "playground-user" });
        toast(`${b.dataset.action} → ${card.dataset.review}`);
        refreshHeader();
        after();
      } catch (e) { toast(errDetails(e), true); }
    }));
    $$(".runlink", card).forEach((a) => a.addEventListener("click", (ev) => { ev.preventDefault(); state.selectedRun = a.dataset.run; showTab("runs"); }));
  });
}

async function loadReviews() {
  const params = new URLSearchParams();
  if (!$("#reviews-history").checked) params.set("status", "pending");
  if (!$("#reviews-audit").checked) params.set("blocking", "true");
  const data = await api("GET", "/api/reviews?" + params);
  const root = $("#reviews-list");
  root.innerHTML = data.reviews.length ? data.reviews.map(reviewHTML).join("") : `<div class="panel empty">Nothing to review. Launch <b>support-triage</b> with review mode <b>manual</b> to get a refund approval and a reply gate.</div>`;
  bindReviewButtons(root, loadReviews);
}

// ---------------------------------------------------------------------------
// Agents & config
// ---------------------------------------------------------------------------
async function loadAgents() {
  const [agents, tools, cfg] = await Promise.all([api("GET", "/api/agents"), api("GET", "/api/tools"), api("GET", "/api/config")]);
  state.agents = agents.agents;
  state.tools = tools.tools;
  state.config = cfg;
  $("#agents-body").innerHTML = state.agents.map((a) => {
    const r = a.effective_route || {};
    return `<tr class="click ${a.name === state.selectedAgent ? "selected" : ""}" data-agent="${esc(a.name)}"><td><b>${esc(a.name)}</b><div class="small muted">${esc(a.description)}</div></td>
      <td>${esc(a.role)}${a.tier !== "auto" ? ` <span class="small muted">(pinned ${esc(a.tier)})</span>` : ""}</td>
      <td>${badge(r.tier || "", r.tier)} <code class="small">${esc(r.model ? r.model.provider + "/" + r.model.model : "")}</code></td></tr>`;
  }).join("");
  $$("#agents-body tr").forEach((tr) => tr.addEventListener("click", () => { state.selectedAgent = tr.dataset.agent; loadAgents(); }));
  if (state.selectedAgent) renderAgentEditor(state.agents.find((a) => a.name === state.selectedAgent));
  renderConfigPanel();
}

function renderAgentEditor(a) {
  if (!a) return;
  const retry = a.retry || a.effective_retry || {};
  const toolBoxes = state.tools.map((t) => `<label class="row small"><input type="checkbox" class="ae-tool" value="${esc(t.name)}" ${(a.tools || []).includes(t.name) ? "checked" : ""}> ${esc(t.name)}${t.requires_approval ? " (needs approval)" : ""}</label>`).join("");
  const models = Object.entries(a.models || {}).map(([tier, m]) => `<div class="row small">${badge(tier)} <input class="ae-model" data-tier="${esc(tier)}" data-field="provider" value="${esc(m.provider)}" size="9"> / <input class="ae-model" data-tier="${esc(tier)}" data-field="model" value="${esc(m.model)}"></div>`).join("");
  $("#agent-editor").innerHTML = `<div class="panel">
    <div class="row"><h2 style="margin:0">${esc(a.name)}</h2><span class="muted small">mock fixture(s): ${(a.mock || []).map(esc).join(", ")}</span></div>
    <p class="small muted">${esc(a.description)}</p>
    <div class="row">
      <label class="field">Role<select id="ae-role">${["plan", "reason", "verify", "judge", "generate", "summarize", "classify", "extract", "chat"].map((r) => `<option ${r === a.role ? "selected" : ""}>${r}</option>`).join("")}</select></label>
      <label class="field">Tier<select id="ae-tier">${["auto", "slm", "llm"].map((t) => `<option ${t === a.tier ? "selected" : ""}>${t}</option>`).join("")}</select></label>
      <label class="field">Temperature<input id="ae-temp" type="number" step="0.1" min="0" max="2" value="${a.temperature ?? ""}"></label>
      <label class="field">Max tokens<input id="ae-max" type="number" min="0" value="${a.max_tokens || ""}"></label>
      <label class="field">Attempt timeout (ms)<input id="ae-timeout" type="number" min="0" value="${a.attempt_timeout_ms || ""}" placeholder="default"></label>
      <label class="row small" style="margin-top:16px"><input type="checkbox" id="ae-stream" ${a.stream ? "checked" : ""}> stream</label>
    </div>
    <h3>Models per tier</h3>${models}
    <h3>System prompt</h3><textarea id="ae-prompt">${esc(a.system_prompt)}</textarea>
    <h3>Retry policy</h3>
    <label class="row small"><input type="checkbox" id="ae-retry-on" ${a.retry ? "checked" : ""}> override the default policy (unchecked = inherit defaults.retry)</label>
    <div class="row">
      <label class="field">max_retries (0-5)<input id="ae-r-max" type="number" min="0" max="5" value="${retry.max_retries ?? 3}"></label>
      <label class="field">initial_backoff_ms<input id="ae-r-init" type="number" min="0" value="${retry.initial_backoff_ms ?? 200}"></label>
      <label class="field">max_backoff_ms<input id="ae-r-cap" type="number" min="0" value="${retry.max_backoff_ms ?? 4000}"></label>
      <label class="field">multiplier<input id="ae-r-mult" type="number" step="0.1" min="1" value="${retry.multiplier ?? 2}"></label>
      <label class="field">jitter (0-1)<input id="ae-r-jit" type="number" step="0.05" min="0" max="1" value="${retry.jitter ?? 0.2}"></label>
    </div>
    <h3>Fallback chain</h3>
    <input id="ae-fallback" style="width:100%" placeholder="provider/model, provider/model" value="${esc((a.fallback || []).map((m) => m.provider + "/" + m.model).join(", "))}">
    <h3>Tools</h3><div class="row">${toolBoxes}</div>
    <div class="row" style="margin-top:12px"><button class="primary" id="ae-save">Save (PATCH)</button><span class="muted small">Validated server-side. An invalid change is rejected and the old config stays live.</span></div>
    <h3>Try it</h3>
    <textarea id="ae-try" class="code" rows="3" placeholder="Input for a direct invoke">${esc(sampleInput(a))}</textarea><button id="ae-try-go">Invoke</button>
    <div id="ae-try-out"></div></div>`;
  $("#ae-save").addEventListener("click", () => saveAgent(a));
  $("#ae-try-go").addEventListener("click", async () => {
    const out = $("#ae-try-out");
    out.innerHTML = '<div class="muted small">running…</div>';
    try {
      const r = await api("POST", `/api/agents/${a.name}/invoke`, { input: $("#ae-try").value, options: { review_mode: "auto" } });
      const res = r.result || {};
      out.innerHTML = `<div class="row small">${badge(r.status)} ${res.tier ? badge(res.tier) : ""} <code>${esc(res.model ? res.model.provider + "/" + res.model.model : "")}</code> attempts=${esc(res.attempts)} retries=${esc(res.retries)} fallback=${esc(res.fallback_used)} ${money(res.cost_usd)} · <a href="#runs/${esc(r.run_id)}" id="ae-run">${esc(r.run_id)}</a></div><pre>${esc(res.output || (r.error && r.error.message) || "")}</pre>`;
      $("#ae-run").addEventListener("click", (ev) => { ev.preventDefault(); state.selectedRun = r.run_id; showTab("runs"); });
    } catch (e) { out.innerHTML = `<pre>${esc(errDetails(e))}</pre>`; }
  });
}

function sampleInput(a) {
  return ({
    planner: "Task: plan a research brief\nTopic: retry strategies",
    researcher: "Task: research\nTopic: retry strategies",
    summarizer: "Task: summarize\nTopic: SLM vs LLM routing\nFindings: ...",
    classifier: "Task: classify support ticket\nTicket: I was charged twice",
    assistant: "what can you do?",
    "proposer-a": "Question: should we rewrite the legacy module?",
    "proposer-b": "Question: should we rewrite the legacy module?",
  }[a.name] || "Task: resilience drill\nCase: manual\nReport the status.");
}

async function saveAgent(a) {
  const num = (id) => ($(id).value === "" ? null : Number($(id).value));
  const models = {};
  $$(".ae-model").forEach((i) => { (models[i.dataset.tier] = models[i.dataset.tier] || {})[i.dataset.field] = i.value.trim(); });
  const fallback = $("#ae-fallback").value.split(",").map((x) => x.trim()).filter(Boolean).map((x) => { const [provider, ...rest] = x.split("/"); return { provider, model: rest.join("/") }; });
  const patch = {
    role: $("#ae-role").value,
    tier: $("#ae-tier").value,
    temperature: num("#ae-temp"),
    max_tokens: num("#ae-max") || 0,
    attempt_timeout_ms: num("#ae-timeout") || 0,
    stream: $("#ae-stream").checked,
    system_prompt: $("#ae-prompt").value,
    models,
    fallback: fallback.length ? fallback : null,
    tools: $$(".ae-tool").filter((c) => c.checked).map((c) => c.value),
    retry: $("#ae-retry-on").checked ? { max_retries: num("#ae-r-max"), initial_backoff_ms: num("#ae-r-init"), max_backoff_ms: num("#ae-r-cap"), multiplier: num("#ae-r-mult"), jitter: num("#ae-r-jit") } : null,
  };
  try {
    await api("PATCH", `/api/agents/${a.name}`, patch);
    toast(`${a.name} updated`);
    loadAgents();
  } catch (e) { toast(errDetails(e), true); }
}

function renderConfigPanel() {
  const c = state.config.config;
  const d = c.defaults, r = c.router, w = c.workflows;
  $("#config-panel").innerHTML = `<h2>Global configuration <span class="muted small">version ${esc(state.config.version)}</span></h2>
    <h3>Human review</h3><div class="row">
      <label class="field">mode<select id="cf-mode"><option ${d.review.mode === "manual" ? "selected" : ""}>manual</option><option ${d.review.mode === "auto" ? "selected" : ""}>auto</option></select></label>
      <label class="field">timeout_ms<input id="cf-rtimeout" type="number" value="${d.review.timeout_ms}"></label>
      <label class="field">on_timeout<select id="cf-ontimeout"><option ${d.review.on_timeout === "fail" ? "selected" : ""}>fail</option><option ${d.review.on_timeout === "approve" ? "selected" : ""}>approve</option></select></label>
      <label class="field">max_revisions<input id="cf-maxrev" type="number" min="0" max="5" value="${d.review.max_revisions}"></label></div>
    <h3>Execution limits</h3><div class="row">
      <label class="field">run_timeout_ms<input id="cf-run" type="number" value="${d.run_timeout_ms}"></label>
      <label class="field">attempt_timeout_ms<input id="cf-attempt" type="number" value="${d.attempt_timeout_ms}"></label>
      <label class="field">max_tool_turns<input id="cf-turns" type="number" value="${d.max_tool_turns}"></label>
      <label class="field">watchdog_stall_ms<input id="cf-stall" type="number" value="${w.watchdog_stall_ms}"></label></div>
    <h3>Default retry policy</h3><div class="row">
      <label class="field">max_retries (0-5)<input id="cf-rmax" type="number" min="0" max="5" value="${d.retry.max_retries}"></label>
      <label class="field">initial_backoff_ms<input id="cf-rinit" type="number" value="${d.retry.initial_backoff_ms}"></label>
      <label class="field">max_backoff_ms<input id="cf-rcap" type="number" value="${d.retry.max_backoff_ms}"></label>
      <label class="field">multiplier<input id="cf-rmult" type="number" step="0.1" value="${d.retry.multiplier}"></label>
      <label class="field">jitter<input id="cf-rjit" type="number" step="0.05" value="${d.retry.jitter}"></label></div>
    <h3>SLM / LLM routing</h3><div class="row">${Object.entries(r.policy).sort().map(([role, tier]) => `<label class="field">${esc(role)}<select class="cf-pol" data-role="${esc(role)}"><option ${tier === "slm" ? "selected" : ""}>slm</option><option ${tier === "llm" ? "selected" : ""}>llm</option></select></label>`).join("")}</div>
    <div class="row">
      <label class="field">escalation_threshold<input id="cf-esc" type="number" step="0.05" min="0" max="1" value="${r.escalation_threshold}"></label>
      <label class="row small" style="margin-top:16px"><input type="checkbox" id="cf-escguard" ${r.escalate_on_guard_failure ? "checked" : ""}> escalate on guard failure</label>
      <label class="field">arbitration_threshold<input id="cf-arb" type="number" step="0.05" min="0" max="1" value="${w.arbitration_threshold}"></label>
      <label class="field">max_grounding_regenerations<input id="cf-regen" type="number" min="0" max="3" value="${w.max_grounding_regenerations}"></label></div>
    <div class="row" style="margin-top:12px"><button class="primary" id="cf-save">Save (PATCH /api/config)</button><button id="cf-reset">Reset to startup config</button><a class="btn" href="/api/config" download="playground-config.json">Export JSON</a></div>`;
  const n = (id) => Number($(id).value);
  $("#cf-save").addEventListener("click", async () => {
    const policy = {};
    $$(".cf-pol").forEach((s) => (policy[s.dataset.role] = s.value));
    try {
      await api("PATCH", "/api/config", {
        defaults: { run_timeout_ms: n("#cf-run"), attempt_timeout_ms: n("#cf-attempt"), max_tool_turns: n("#cf-turns"),
          retry: { max_retries: n("#cf-rmax"), initial_backoff_ms: n("#cf-rinit"), max_backoff_ms: n("#cf-rcap"), multiplier: n("#cf-rmult"), jitter: n("#cf-rjit") },
          review: { mode: $("#cf-mode").value, timeout_ms: n("#cf-rtimeout"), on_timeout: $("#cf-ontimeout").value, max_revisions: n("#cf-maxrev") } },
        router: { policy, escalation_threshold: n("#cf-esc"), escalate_on_guard_failure: $("#cf-escguard").checked },
        workflows: { arbitration_threshold: n("#cf-arb"), max_grounding_regenerations: n("#cf-regen"), watchdog_stall_ms: n("#cf-stall") },
      });
      toast("Configuration saved");
      loadAgents();
      refreshHeader();
    } catch (e) { toast(errDetails(e), true); }
  });
  $("#cf-reset").addEventListener("click", async () => { await api("POST", "/api/config/reset"); toast("Configuration reset"); loadAgents(); refreshHeader(); });
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------
async function loadChat() {
  const sel = $("#chat-agent");
  if (sel.options.length) return;
  const agents = (await api("GET", "/api/agents")).agents;
  sel.innerHTML = agents.map((a) => `<option ${a.name === "assistant" ? "selected" : ""}>${esc(a.name)}</option>`).join("");
}

function chatAdd(cls, text) {
  const el = document.createElement("div");
  el.className = cls;
  el.textContent = text;
  $("#chat-log").appendChild(el);
  $("#chat-log").scrollTop = 1e9;
  return el;
}

async function chatSend() {
  const input = $("#chat-input").value.trim();
  if (!input) return;
  $("#chat-input").value = "";
  const agent = $("#chat-agent").value;
  const body = { input, stream: $("#chat-stream").checked };
  if ($("#chat-tier").value) body.tier = $("#chat-tier").value;
  chatAdd("msg user", input);
  const bubble = chatAdd("msg assistant", "");
  if (!body.stream) {
    try {
      const r = await api("POST", `/api/agents/${encodeURIComponent(agent)}/invoke`, body);
      const res = r.result || {};
      bubble.textContent = res.output || (r.error && r.error.message) || "(no output)";
      chatAdd("evt", `${r.status} · ${res.tier || "?"} ${res.model ? res.model.model : ""} · attempts ${res.attempts ?? "?"} · ${money(res.cost_usd)} · ${r.run_id}`);
    } catch (e) { bubble.textContent = e.message; }
    return;
  }
  try {
    const resp = await fetch(`/api/agents/${encodeURIComponent(agent)}/invoke`, { method: "POST", headers: { "Content-Type": "application/json", Accept: "text/event-stream" }, body: JSON.stringify(body) });
    if (!resp.ok) { bubble.textContent = `HTTP ${resp.status}: ${await resp.text()}`; return; }
    const reader = resp.body.getReader();
    const dec = new TextDecoder();
    let buf = "", event = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, i).replace(/\r$/, "");
        buf = buf.slice(i + 1);
        if (line.startsWith("event:")) event = line.slice(6).trim();
        else if (line.startsWith("data:")) handleChatEvent(event, JSON.parse(line.slice(5)), bubble);
      }
    }
  } catch (e) { chatAdd("evt warn", e.message); }
}

function handleChatEvent(ev, data, bubble) {
  switch (ev) {
    case "delta": bubble.textContent += data.text; $("#chat-log").scrollTop = 1e9; break;
    case "route": chatAdd("evt", `route → ${data.model} (${data.reason})`); break;
    case "retry": chatAdd("evt warn", `retry after attempt ${data.attempt}: ${data.reason}, waiting ${data.delay_ms}ms`); break;
    case "reset": bubble.textContent = ""; chatAdd("evt warn", `stream broken (${data.reason}), retrying without streaming`); break;
    case "fallback": chatAdd("evt warn", `fallback ${data.from} → ${data.to}`); break;
    case "tool_call": chatAdd("evt", `tool ${data.tool}(${data.arguments})`); break;
    case "tool_result": chatAdd("evt", `tool ${data.tool} → ${data.outcome}`); break;
    case "continuation": chatAdd("evt warn", `answer truncated, requesting continuation #${data.n}`); break;
    case "approval_requested": case "review_requested": chatAdd("evt warn", `waiting for human review (see the Reviews tab)`); refreshHeader(); break;
    case "done": {
      const res = data.result || {};
      if (!bubble.textContent && res.output) bubble.textContent = res.output;
      if (data.error) chatAdd("evt warn", `${data.error.code}: ${data.error.message}`);
      chatAdd("evt", `${data.status} · ${res.tier || "?"} ${res.model ? res.model.model : ""} · attempts ${res.attempts ?? "?"} · ${money(res.cost_usd)} · ${data.run_id}`);
      break;
    }
  }
}

// ---------------------------------------------------------------------------
// Mock
// ---------------------------------------------------------------------------
async function loadMock() {
  try {
    const st = await api("GET", "/api/mock/status");
    $("#mock-status").innerHTML = `<h2>mockagents</h2><dl class="kv"><dt>URL</dt><dd><code>${esc(st.url)}</code></dd><dt>mode</dt><dd>${esc(st.mode)}</dd>
      <dt>health</dt><dd>${esc(JSON.stringify(st.health))}</dd><dt>fixtures</dt><dd>${st.agents.length}</dd></dl>
      <details><summary class="small muted">fixtures</summary><table><tbody>${st.agents.map((a) => `<tr><td>${esc(a.name)}</td><td><code>${esc(a.model)}</code></td><td class="small muted">${esc(a.protocol)}</td></tr>`).join("")}</tbody></table></details>`;
    const sel = $("#fixture-name");
    if (!sel.options.length) sel.innerHTML = st.agents.map((a) => `<option>${esc(a.name)}</option>`).join("");
  } catch (e) { $("#mock-status").innerHTML = `<h2>mockagents</h2><pre>${esc(e.message)}</pre>`; }
  try {
    const c = await api("GET", "/api/mock/costs");
    const rows = (c.by_model || []).filter((r) => r.key !== "(unknown)");
    const max = Math.max(...rows.map((r) => r.cost_usd), 1e-9);
    $("#mock-costs").innerHTML = `<h2>Cost by model <span class="muted small">${c.total_requests} requests · ${money(c.total_cost_usd)}</span></h2>
      <table><tbody>${rows.map((r) => `<tr><td><code>${esc(r.key)}</code> ${badge(r.key.startsWith("slm") ? "slm" : "llm", r.key.startsWith("slm") ? "SLM" : "LLM")}</td><td class="num">${r.requests}</td>
      <td style="width:45%"><div style="background:${r.key.startsWith("slm") ? "var(--slm)" : "var(--llm)"};height:8px;border-radius:4px;width:${(r.cost_usd / max) * 100}%"></div></td><td class="num">${money(r.cost_usd)}</td></tr>`).join("")}</tbody></table>
      <p class="small muted">Priced with <code>config/mock-pricing.yaml</code>. SLM-tier aliases cost about 1/16 of LLM-tier ones, which is what routing optimizes.</p>`;
  } catch (e) { $("#mock-costs").innerHTML = `<pre>${esc(e.message)}</pre>`; }
  loadMockLogs();
}

async function loadMockLogs() {
  const prefix = $("#logs-prefix").value.trim();
  try {
    const logs = await api("GET", "/api/mock/logs?limit=50" + (prefix ? "&session_prefix=" + encodeURIComponent(prefix) : ""));
    $("#mock-logs").innerHTML = logs.length ? mockLogTable(logs) : '<div class="empty">No interactions yet.</div>';
  } catch (e) { $("#mock-logs").innerHTML = `<pre>${esc(e.message)}</pre>`; }
}

async function runPipeline() {
  try {
    const r = await api("POST", "/api/mock/pipelines/pg-native-brief/run", { input: $("#pipeline-input").value });
    $("#pipeline-out").innerHTML = (r.nodes || []).map((n) => `<h3>${esc(n.node_id)} <span class="muted small">${esc(n.agent_name)}</span></h3><pre>${esc(((n.response || {}).content) || JSON.stringify(n.response))}</pre>`).join("");
  } catch (e) { $("#pipeline-out").innerHTML = `<pre>${esc(errDetails(e))}</pre>`; }
}

async function loadFixture() {
  try {
    $("#fixture-body").value = await api("GET", `/api/mock/agents/${encodeURIComponent($("#fixture-name").value)}?format=yaml`, undefined, { Accept: "application/yaml" });
    $("#fixture-out").innerHTML = '<p class="small muted">Loaded the canonical YAML that mockagents is serving. Comments from the source file are not preserved.</p>';
  } catch (e) { $("#fixture-out").innerHTML = `<pre>${esc(e.message)}</pre>`; }
}

async function saveFixture() {
  try {
    const r = await api("PUT", `/api/mock/agents/${encodeURIComponent($("#fixture-name").value)}`, $("#fixture-body").value, { "Content-Type": "application/yaml" });
    $("#fixture-out").innerHTML = `<pre>${fmtJSON(r)}</pre>`;
    toast("Fixture saved to the live mock registry");
  } catch (e) { $("#fixture-out").innerHTML = `<pre>${esc(errDetails(e))}</pre>`; }
}

async function reloadFixture() {
  try {
    const r = await api("POST", `/api/mock/agents/${encodeURIComponent($("#fixture-name").value)}/reload`);
    $("#fixture-out").innerHTML = `<pre>${fmtJSON(r)}</pre>`;
    toast("Reloaded from disk (chaos counters re-armed)");
  } catch (e) { $("#fixture-out").innerHTML = `<pre>${esc(errDetails(e))}</pre>`; }
}

// ---------------------------------------------------------------------------
// API explorer
// ---------------------------------------------------------------------------
async function loadAPI() {
  const doc = await api("GET", "/openapi.json");
  const byTag = {};
  Object.entries(doc.paths).forEach(([p, item]) => Object.entries(item).forEach(([m, op]) => {
    if (m === "parameters") return;
    const tag = (op.tags || ["other"])[0];
    (byTag[tag] = byTag[tag] || []).push({ m, p, op });
  }));
  $("#api-ops").innerHTML = (doc.tags || []).map((t) => `<h3>${esc(t.name)} <span class="muted small">${esc(t.description)}</span></h3>${(byTag[t.name] || []).map(({ m, p, op }) =>
    `<div class="op"><span class="method ${m}">${m.toUpperCase()}</span><code>${m === "get" && !p.includes("{") && !p.endsWith("events") ? `<a href="${esc(p)}" target="_blank">${esc(p)}</a>` : esc(p)}</code><span class="small">${esc(op.summary)}</span></div>`).join("")}`).join("");
}

// ---------------------------------------------------------------------------
// Boot
// ---------------------------------------------------------------------------
document.addEventListener("DOMContentLoaded", () => {
  $$("#tabs button").forEach((b) => b.addEventListener("click", () => showTab(b.dataset.tab)));
  $("#runs-filter").addEventListener("change", loadRuns);
  $("#reviews-audit").addEventListener("change", loadReviews);
  $("#reviews-history").addEventListener("change", loadReviews);
  $("#chat-send").addEventListener("click", chatSend);
  $("#chat-input").addEventListener("keydown", (e) => { if (e.key === "Enter") chatSend(); });
  $("#chat-clear").addEventListener("click", () => ($("#chat-log").innerHTML = ""));
  $("#logs-refresh").addEventListener("click", loadMockLogs);
  $("#pipeline-run").addEventListener("click", runPipeline);
  $("#fixture-load").addEventListener("click", loadFixture);
  $("#fixture-save").addEventListener("click", saveFixture);
  $("#fixture-reload").addEventListener("click", reloadFixture);

  const [tab, runID] = (location.hash || "#workflows").slice(1).split("/");
  if (runID) state.selectedRun = runID;
  showTab(["workflows", "runs", "reviews", "agents", "chat", "mock", "api"].includes(tab) ? tab : "workflows");
  refreshHeader();
  setInterval(() => {
    refreshHeader();
    if (state.tab === "runs") loadRuns().catch(() => {});
    if (state.tab === "reviews" && !document.activeElement.matches("textarea, input")) loadReviews().catch(() => {});
  }, 2500);
});
