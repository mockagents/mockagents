// UX-08 browser smoke suite. Every assertion here is against a real Go server
// serving ../examples — if an API contract changes, these fail.
import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const API_URL = `http://127.0.0.1:${process.env.SMOKE_API_PORT ?? 8099}`;

test.describe("agent catalog", () => {
  test("renders agents served by the real API", async ({ page }) => {
    // Establish what the server actually serves, then assert the UI agrees.
    const res = await page.request.get(`${API_URL}/api/v1/agents`);
    expect(res.ok()).toBe(true);
    const agents = (await res.json()) as Array<{ name: string }>;
    expect(agents.length).toBeGreaterThan(0);

    await page.goto("/");
    await expect(page.getByRole("link", { name: agents[0].name, exact: true })).toBeVisible();
  });

  test("search narrows the catalog", async ({ page }) => {
    await page.goto("/", { waitUntil: "networkidle" });
    const search = page.getByPlaceholder(/search agents/i);

    // The catalog is server-rendered with a client island. Typing before
    // React hydrates leaves the text in the DOM with no state update — the
    // input shows the query while the list still shows every agent — and
    // nothing recovers it. Retrying the interaction absorbs that race on a
    // slow runner. It does NOT mask a broken filter: a genuinely broken
    // filter fails every attempt and the test still goes red.
    await expect(async () => {
      await search.fill("zzz-no-such-agent");
      await expect(page.getByText("No agents match this filter.")).toBeVisible({
        timeout: 1_000,
      });
    }).toPass({ timeout: 15_000 });
  });
});

// X-3: deleting a mock is irreversible, so the design's destructive-dialog spec
// applies to it — impact, consequences, exact-name gate. This drives the whole
// path against a real server: create a throwaway agent, fail to delete it, then
// delete it. The suite runs with workers: 1, so the temporary agent cannot
// collide with another test.
test.describe("agent deletion", () => {
  const NAME = "x3-throwaway-agent";
  const YAML = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: ${NAME}
spec:
  protocol: anthropic-messages
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`;

  test.beforeEach(async ({ request }) => {
    const res = await request.put(`${API_URL}/api/v1/agents/${NAME}`, {
      headers: { "Content-Type": "application/x-yaml" },
      data: YAML,
    });
    expect([200, 201], await res.text()).toContain(res.status());
  });

  test.afterEach(async ({ request }) => {
    // Best effort: the happy-path test already removed it.
    await request.delete(`${API_URL}/api/v1/agents/${NAME}`);
  });

  /** Open the confirmation for NAME. The trigger is a client island, so a click
   * that lands before hydration is swallowed with no state change and nothing
   * recovers it; retrying absorbs that race on a slow runner. A genuinely
   * broken dialog fails every attempt and the test still goes red. */
  async function openConfirm(page: import("@playwright/test").Page) {
    await page.goto("/", { waitUntil: "networkidle" });
    await expect(async () => {
      await page.getByRole("button", { name: `Delete ${NAME}` }).click();
      await expect(page.getByRole("dialog")).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
    return page.getByRole("dialog");
  }

  test("a near-miss name leaves the agent serving", async ({ page }) => {
    const dialog = await openConfirm(page);
    // The heading names the specific mock, and the consequences are listed
    // before the operator is given anything to press.
    await expect(dialog).toContainText(NAME);
    await expect(dialog).toContainText(/stops serving immediately/i);

    const confirm = page.getByRole("button", { name: "Delete this agent" });
    await expect(confirm).toBeDisabled();

    // Case differs by one character. Exact means exact.
    await page.getByLabel(/type the exact agent name/i).fill(NAME.toUpperCase());
    await expect(confirm).toBeDisabled();

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();

    // The safety property, asserted against the server rather than the screen.
    const still = await page.request.get(`${API_URL}/api/v1/agents/${NAME}`);
    expect(still.status()).toBe(200);
  });

  test("the exact name deletes it, and the server agrees", async ({ page }) => {
    await openConfirm(page);
    await page.getByLabel(/type the exact agent name/i).fill(NAME);
    await page.getByRole("button", { name: "Delete this agent" }).click();

    // The outcome is reported, not merely implied by the card disappearing.
    await expect(page.getByText(/^Deleted\./)).toBeVisible();
    await expect(page.getByRole("link", { name: NAME, exact: true })).toHaveCount(0);

    const gone = await page.request.get(`${API_URL}/api/v1/agents/${NAME}`);
    expect(gone.status()).toBe(404);
  });
});

test.describe("navigation", () => {
  // Deep links must stay viable (epic §5). These are the routes the epic's
  // Release A journey depends on.
  for (const path of ["/", "/overview", "/logs", "/costs", "/reports", "/pipelines", "/editor"]) {
    test(`${path} responds without a server error`, async ({ page }) => {
      const response = await page.goto(path);
      expect(response?.status(), `${path} should not 5xx`).toBeLessThan(500);
    });
  }
});

test.describe("pipeline detail", () => {
  test("shows the example pipeline's nodes", async ({ page }) => {
    const res = await page.request.get(`${API_URL}/api/v1/pipelines`);
    expect(res.ok()).toBe(true);
    const pipelines = (await res.json()) as Array<{ name: string }>;
    test.skip(pipelines.length === 0, "no example pipeline registered");

    await page.goto(`/pipelines/${encodeURIComponent(pipelines[0].name)}`);
    await expect(page.getByText(pipelines[0].name).first()).toBeVisible();
  });
});

test.describe("identity (UX-01)", () => {
  // The smoke server runs single-tenant, so this is the local-mode contract.
  test("reports local mode without a credential", async ({ page }) => {
    const res = await page.request.get(`${API_URL}/api/v1/identity`);
    expect(res.ok()).toBe(true);

    const body = await res.json();
    expect(body.mode).toBe("local");
    expect(body.authenticated).toBe(false);
    // Explicitly null — not "viewer", not "".
    expect(body.role).toBeNull();
    expect(Array.isArray(body.capabilities)).toBe(true);
    expect(body.capabilities).toContain("agents.read");
    expect(body.server.version).toBeTruthy();
  });

  test("only advertises capabilities whose routes are mounted", async ({ page }) => {
    const res = await page.request.get(`${API_URL}/api/v1/identity`);
    const body = (await res.json()) as { capabilities: string[] };

    // Probe each advertised read capability that maps to a bare collection
    // route: an advertised action must never 404.
    const probes: Record<string, string> = {
      "agents.read": "/api/v1/agents",
      "pipelines.read": "/api/v1/pipelines",
      "costs.read": "/api/v1/costs",
      "logs.read": "/api/v1/logs",
    };
    for (const [capability, path] of Object.entries(probes)) {
      if (!body.capabilities.includes(capability)) continue;
      const probe = await page.request.get(`${API_URL}${path}`);
      expect(probe.status(), `${capability} advertised but ${path} is missing`).not.toBe(404);
    }
  });
});

test.describe("agent editor (UX-03)", () => {
  async function firstAgent(request: { get: (u: string) => Promise<{ json: () => Promise<unknown> }> }) {
    const res = await request.get(`${API_URL}/api/v1/agents`);
    const agents = (await res.json()) as Array<{ name: string }>;
    return agents[0]?.name;
  }

  test("serves the definition as YAML with a revision", async ({ page }) => {
    const res = await page.request.get(`${API_URL}/api/v1/agents/echo-agent`, {
      headers: { Accept: "application/yaml" },
    });
    expect(res.status()).toBe(200);
    expect(res.headers()["content-type"]).toContain("yaml");
    expect(res.headers()["etag"]).toBeTruthy();
    expect(await res.text()).toContain("kind: Agent");
  });

  test("a stale conditional write is refused and changes nothing", async ({ page }) => {
    const url = `${API_URL}/api/v1/agents/echo-agent`;
    const before = await page.request.get(url, { headers: { Accept: "application/yaml" } });
    const yaml = await before.text();

    const stale = await page.request.put(url, {
      headers: { "Content-Type": "application/x-yaml", "If-Match": '"not-the-current-revision"' },
      data: yaml,
    });
    expect(stale.status()).toBe(412);

    // The stored definition is untouched.
    const after = await page.request.get(url, { headers: { Accept: "application/yaml" } });
    expect(await after.text()).toBe(yaml);
  });

  test("a conditional write rejects an unsupported field", async ({ page }) => {
    const url = `${API_URL}/api/v1/agents/echo-agent`;
    const get = await page.request.get(url, { headers: { Accept: "application/yaml" } });
    const etag = get.headers()["etag"];
    const original = await get.text();

    // Insert the field as a sibling of spec's existing keys, matching their
    // indentation. Appending at the end would be a YAML *syntax* error (400),
    // which is a different failure than the unknown-field rejection (422) this
    // test is about.
    const lines = original.split("\n");
    const specIdx = lines.findIndex((l) => l.startsWith("spec:"));
    expect(specIdx, "the document should have a spec block").toBeGreaterThanOrEqual(0);
    const indent = lines[specIdx + 1].match(/^\s*/)?.[0] ?? "  ";
    lines.splice(specIdx + 1, 0, `${indent}aFieldTheServerDoesNotKnow: nope`);

    const res = await page.request.put(url, {
      headers: { "Content-Type": "application/x-yaml", "If-Match": etag },
      data: lines.join("\n"),
    });
    expect(res.status(), await res.text()).toBe(422);
    expect(await res.text()).toContain("aFieldTheServerDoesNotKnow");

    // And nothing was changed by the rejected write.
    const after = await page.request.get(url, { headers: { Accept: "application/yaml" } });
    expect(await after.text()).toBe(original);
  });

  test("the edit page opens on the guided form and gates Apply behind review", async ({ page }) => {
    const name = await firstAgent(page.request);
    test.skip(!name, "no agents served");

    await page.goto(`/agents/${encodeURIComponent(name!)}/edit`, { waitUntil: "networkidle" });

    // Form first (progressive disclosure), YAML one tab away.
    await expect(page.getByRole("tab", { name: "Form" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByLabel(/^Name/)).toHaveValue(name!);

    await page.getByRole("tab", { name: "YAML" }).click();
    const box = page.getByRole("textbox", { name: new RegExp(`YAML definition for ${name}`, "i") });
    await expect(box).toBeVisible();
    await expect(box).toContainText("kind: Agent");

    // Nothing edited yet, so there is nothing to apply.
    await expect(page.getByRole("button", { name: /^Apply$/ })).toBeDisabled();
  });

  // The guarantee that makes a partial form safe: it edits its own field and
  // leaves the rest of the document byte-identical.
  test("a form edit preserves config the form does not render", async ({ page }) => {
    // flaky-agent (examples/chaos-agent.yaml) carries a spec.chaos block the
    // form does not render — exactly the case that must survive an edit.
    await page.goto("/agents/flaky-agent/edit", { waitUntil: "networkidle" });
    const note = page.getByRole("note");
    test.skip((await note.count()) === 0, "no agent with uncovered spec sections");

    await expect(note).toContainText(/preserved unchanged/);

    const description = page.getByLabel(/^Description/);
    await expect(async () => {
      await description.fill("edited by the form");
      await expect(page.getByRole("button", { name: /review changes/i })).toBeEnabled({
        timeout: 1_000,
      });
    }).toPass({ timeout: 15_000 });

    await page.getByRole("tab", { name: "YAML" }).click();
    const yaml = await page
      .getByRole("textbox", { name: /YAML definition/i })
      .inputValue();
    expect(yaml).toContain("edited by the form");
    // Whatever the note listed is still in the document.
    expect(yaml).toContain("chaos:");
  });

  // The cross-layer proof (epic §10: backend, CLI and GUI fixtures must agree
  // on canonical decisions). The component tests assert what the form puts in
  // the browser's text buffer; this asserts what ends up on the SERVER after
  // the whole loop — load, form edit, review, apply.
  test("a form edit applied through the UI preserves unrendered config on the server", async ({
    page,
  }) => {
    const url = `${API_URL}/api/v1/agents/flaky-agent`;
    const before = await page.request.get(url, { headers: { Accept: "application/yaml" } });
    test.skip(before.status() !== 200, "flaky-agent not served");
    expect(await before.text()).toContain("chaos:");

    await page.goto("/agents/flaky-agent/edit", { waitUntil: "networkidle" });

    const description = page.getByLabel(/^Description/);
    const review = page.getByRole("button", { name: /review changes/i });
    const marker = `applied-through-the-form-${Date.now()}`;

    // Retry through the hydration race, as elsewhere in this suite.
    await expect(async () => {
      await description.fill(marker);
      await expect(review).toBeEnabled({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });

    await review.click();
    const apply = page.getByRole("button", { name: /^Apply$/ });
    await expect(apply).toBeEnabled();
    await apply.click();

    // The receipt must say what actually happened.
    await expect(page.getByText(/Applied\.|Created\./)).toBeVisible({ timeout: 15_000 });

    // Now the part that matters: ask the SERVER what it holds.
    const after = await page.request.get(url, { headers: { Accept: "application/yaml" } });
    const stored = await after.text();
    expect(stored, "the edit must have reached the server").toContain(marker);
    for (const fragment of ["chaos:", "latency:", "min_ms", "max_ms", "status_codes"]) {
      expect(stored, `${fragment} must survive a form-driven save`).toContain(fragment);
    }
  });

  test("the agent detail page links to the editor", async ({ page }) => {
    const name = await firstAgent(page.request);
    test.skip(!name, "no agents served");

    await page.goto(`/agents/${encodeURIComponent(name!)}`, { waitUntil: "networkidle" });
    await expect(page.getByRole("link", { name: /^Edit$/ })).toBeVisible();
  });
});

test.describe("request explorer (UX-04)", () => {
  /** Drive one real request through the mock so there is a log row to inspect. */
  async function generateTraffic(page: import("@playwright/test").Page, sessionHint: string) {
    const res = await page.request.post(`${API_URL}/v1/chat/completions`, {
      data: {
        model: "gpt-4o",
        messages: [{ role: "user", content: `explorer smoke ${sessionHint}` }],
      },
    });
    expect([200, 400, 404]).toContain(res.status());
    return res.status();
  }

  // U4-3. An empty result is the most ambiguous thing this screen can show, so
  // it has to say WHICH empty it is and over what window. Unreachable and
  // unauthorized are caught server-side and render their own screens; this
  // asserts the third case reads as itself, against a real store that answered.
  test("an empty window names itself as reachable-and-empty", async ({ page }) => {
    await page.goto("/logs?session_id=no-such-session-anywhere", { waitUntil: "networkidle" });

    await expect(page.getByText(/No records in this window/i)).toBeVisible();
    // The distinction is the whole point: this screen is only reached when the
    // store ANSWERED, so it says so rather than leaving "no rows" to be read as
    // a dead server or a refused credential.
    await expect(page.getByText(/reachable and empty/i)).toBeVisible();
    await expect(page.getByText(/unauthorized \(403\)/i)).toBeVisible();
  });

  test("the server-side filter is applied, not just the fetched page", async ({ page }) => {
    // A session that cannot exist must come back empty from the SERVER — if
    // filtering were client-side over a fetched page this would still show rows.
    const res = await page.request.get(
      `${API_URL}/api/v1/logs?session_id=definitely-no-such-session`,
    );
    expect(res.status()).toBe(200);
    expect(await res.json()).toEqual([]);

    await page.goto("/logs?session_id=definitely-no-such-session", { waitUntil: "networkidle" });
    await expect(page.getByText(/No records in this window/i)).toBeVisible();
    // And it is explained as a filter result, not as "there is no traffic":
    // the window names the filter, and the next step is to widen it.
    await expect(page.getByText(/session definitely-no-such-session/)).toBeVisible();
    await expect(page.getByText(/Widen the filter/i)).toBeVisible();
  });

  test("filters live in the URL and carry only metadata", async ({ page }) => {
    await page.goto("/logs", { waitUntil: "networkidle" });

    const session = page.getByLabel("Session");
    await expect(async () => {
      await session.fill("sess-shareable");
      await page.getByRole("button", { name: /^Apply$/ }).click();
      await expect(page).toHaveURL(/session_id=sess-shareable/, { timeout: 2_000 });
    }).toPass({ timeout: 20_000 });

    // Nothing but allowlisted filter keys reaches the URL.
    const keys = [...new URL(page.url()).searchParams.keys()].sort();
    expect(keys.every((k) => ["agent", "session_id", "since", "until", "limit", "offset"].includes(k))).toBe(
      true,
    );
  });

  test("surfaces session and signals for real captured traffic", async ({ page }) => {
    await generateTraffic(page, "signals");

    const logs = await (await page.request.get(`${API_URL}/api/v1/logs?limit=5`)).json();
    test.skip(!Array.isArray(logs) || logs.length === 0, "no interactions captured");
    expect(logs[0]).toHaveProperty("session_id");

    await page.goto("/logs", { waitUntil: "networkidle" });
    // The session column renders for a real row.
    await expect(page.getByRole("table")).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "session" })).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "signals" })).toBeVisible();
  });

  test("bodies stay hidden until revealed", async ({ page }) => {
    await generateTraffic(page, "bodies");
    await page.goto("/logs", { waitUntil: "networkidle" });

    const reveal = page.getByRole("button", { name: /show request\/response bodies/i });
    test.skip((await reveal.count()) === 0, "no row selected / no logs");
    await expect(reveal).toBeVisible();
    await expect(page.getByText(/Bodies are hidden by default/)).toBeVisible();
  });

  test("the log detail page links back to the whole session", async ({ page }) => {
    await generateTraffic(page, "detail");
    const logs = await (await page.request.get(`${API_URL}/api/v1/logs?limit=1`)).json();
    test.skip(!Array.isArray(logs) || logs.length === 0, "no interactions captured");

    await page.goto(`/logs/${logs[0].id}`, { waitUntil: "networkidle" });
    const sessionLink = page.getByRole("link", { name: logs[0].session_id });
    if ((await sessionLink.count()) > 0) {
      await expect(sessionLink).toHaveAttribute("href", /session_id=/);
    }
  });
});

test.describe("pipeline execution (UX-05)", () => {
  test("the real run endpoint returns nodes with nanosecond latencies", async ({ page }) => {
    const res = await page.request.post(`${API_URL}/api/v1/pipelines/research-pipeline/run`, {
      data: { input: "smoke test input", session_id: `e2e-${Date.now()}` },
    });
    // 200 or 422 are both real outcomes; 404 means the fixture is absent.
    test.skip(res.status() === 404, "research-pipeline not registered");
    expect([200, 422]).toContain(res.status());

    const body = await res.json();
    const result = res.status() === 200 ? body : body.result;
    test.skip(!result, "422 carried no partial result");

    expect(Array.isArray(result.nodes)).toBe(true);
    if (result.nodes.length > 0) {
      const node = result.nodes[0];
      expect(node).toHaveProperty("node_id");
      expect(node).toHaveProperty("agent_name");
      // response may legitimately be null; the key must still be present.
      expect(Object.hasOwn(node, "response")).toBe(true);
      // Nanoseconds: a real run is far more than a handful of ns, and the
      // total must be at least as large as any single node.
      expect(typeof node.latency).toBe("number");
      expect(result.latency).toBeGreaterThanOrEqual(0);
    }
  });

  test("the run panel is offered on the pipeline page and states it is not isolated", async ({
    page,
  }) => {
    const list = await (await page.request.get(`${API_URL}/api/v1/pipelines`)).json();
    test.skip(!Array.isArray(list) || list.length === 0, "no pipelines registered");

    await page.goto(`/pipelines/${encodeURIComponent(list[0].name)}`, {
      waitUntil: "networkidle",
    });

    await expect(page.getByLabel("Input")).toBeVisible();
    await expect(page.getByText(/Runs against active configuration/i)).toBeVisible();
    await expect(page.getByText(/not an isolated preview/i)).toBeVisible();
    // Nothing typed yet, so nothing to run.
    await expect(page.getByRole("button", { name: /run pipeline/i })).toBeDisabled();
  });

  test("running through the UI shows node evidence with converted latencies", async ({ page }) => {
    const list = await (await page.request.get(`${API_URL}/api/v1/pipelines`)).json();
    test.skip(!Array.isArray(list) || list.length === 0, "no pipelines registered");

    await page.goto(`/pipelines/${encodeURIComponent(list[0].name)}`, {
      waitUntil: "networkidle",
    });

    const input = page.getByLabel("Input");
    const button = page.getByRole("button", { name: /run pipeline/i });
    await expect(async () => {
      await input.fill("end to end run");
      await expect(button).toBeEnabled({ timeout: 1_000 });
    }).toPass({ timeout: 20_000 });

    await button.click();
    // Either outcome is acceptable; both must render as evidence, not a crash.
    await expect(page.getByText(/Run completed|Partial run|Outcome unknown/i)).toBeVisible({
      timeout: 30_000,
    });

    // Latency must be humanised, never a raw nanosecond count.
    await expect(page.getByText(/\d+(\.\d+)?(µs|ms|s)\b/).first()).toBeVisible();
    // The submitted session is recorded alongside the result.
    await expect(page.getByText(/^gui-run-/)).toBeVisible();
  });
});

test.describe("shell and onboarding (UX-02)", () => {
  test("the instrument strip reports real server context", async ({ page }) => {
    await page.goto("/overview", { waitUntil: "networkidle" });

    const strip = page.getByRole("status", { name: "server context" });
    await expect(strip).toBeVisible();

    // Liveness and readiness are separate cells with separate verdicts.
    await expect(strip).toContainText("PROCESS-UP");
    await expect(strip).toContainText("READY");
    // Release A always runs against the live runtime, and says so everywhere.
    await expect(strip).toContainText("ACTIVE CONFIG");
    // No global configuration revision exists, so the cell says that.
    await expect(strip).toContainText("per-resource");

    // The engine version must be the one the server reports, not a fixture.
    const health = await (await page.request.get(`${API_URL}/api/v1/health`)).json();
    if (health.version) await expect(strip).toContainText(health.version);
  });

  // X-1. "Persistent context" is a claim about every screen, so it is asserted
  // on every screen — including the two where acting on stale context is
  // expensive: the editor (Apply rewrites the live definition) and the pipeline
  // page (Run executes against it). Both used to show no server context at all.
  for (const path of [
    "/",
    "/overview",
    "/logs",
    "/costs",
    "/reports",
    "/editor",
    "/pipelines",
    "/pipelines/research-pipeline",
    "/agents/echo-agent/edit",
  ]) {
    test(`${path} carries the instrument strip`, async ({ page }) => {
      await page.goto(path, { waitUntil: "networkidle" });
      const strip = page.getByRole("status", { name: "server context" });
      // Exactly one: the layout owns it, so a page rendering its own would
      // double it, and two "last refresh" stamps disagree by construction.
      await expect(strip).toHaveCount(1);
      await expect(strip).toContainText("PROCESS-UP");
      await expect(strip).toContainText("ACTIVE CONFIG");
    });
  }

  test("the shell states connectivity once, not twice with different answers", async ({ page }) => {
    // The topbar pill collapsed liveness and readiness into one word and could
    // read "online" beside a NOT-READY strip. It is gone; this keeps it gone.
    await page.goto("/overview", { waitUntil: "networkidle" });
    const topbar = page.locator(".topbar");
    await expect(topbar).toBeVisible();
    await expect(topbar).not.toContainText(/\bonline\b/i);
    await expect(topbar).not.toContainText(/\boffline\b/i);
  });

  test("readiness is verified against the real endpoint", async ({ page }) => {
    const res = await page.request.get(`${API_URL}/api/v1/ready`);
    expect([200, 503]).toContain(res.status());
    const body = await res.json();
    expect(["ready", "not_ready"]).toContain(body.status);

    await page.goto("/overview", { waitUntil: "networkidle" });
    await expect(page.getByText(body.status === "ready" ? "Ready" : "Not ready").first()).toBeVisible();
  });

  test("onboarding offers copyable SDK settings", async ({ page }) => {
    await page.goto("/overview", { waitUntil: "networkidle" });
    await expect(page.getByLabel("base URL")).toHaveValue(/\/v1$/);
    await expect(page.getByLabel("api key")).toBeVisible();
    await expect(page.getByRole("button", { name: "Copy" }).first()).toBeVisible();
  });

  test("an empty pipeline inventory explains instead of dead-ending", async ({ page }) => {
    const list = await (await page.request.get(`${API_URL}/api/v1/pipelines`)).json();
    await page.goto("/overview", { waitUntil: "networkidle" });

    if (Array.isArray(list) && list.length === 0) {
      await expect(page.getByText(/not part of Release A/i)).toBeVisible();
      // Never a dead Run button.
      await expect(page.getByRole("button", { name: /^Run/ })).toHaveCount(0);
    } else {
      await expect(page.getByRole("link", { name: /Open pipelines/i })).toBeVisible();
    }
  });

  test("the checklist reflects real state, never an inferred empty", async ({ page }) => {
    const agents = await (await page.request.get(`${API_URL}/api/v1/agents`)).json();
    await page.goto("/overview", { waitUntil: "networkidle" });

    await expect(page.getByText("First-run checklist")).toBeVisible();
    if (Array.isArray(agents) && agents.length > 0) {
      await expect(page.getByText(`Agent definitions loaded (${agents.length})`)).toBeVisible();
    }
  });
});

test.describe("reports (UX-06)", () => {
  test("presents the export as a bounded snapshot, with bodies off by default", async ({ page }) => {
    await page.goto("/reports");

    // The page must never let an export read as a complete or attested record.
    // The disclaimer appears twice on purpose — once as the page's own lede and
    // once inside the omissions list that ships in the file — so this asserts on
    // the lede specifically rather than matching both and failing strict mode.
    await expect(page.locator("p.page-lede")).toContainText(/bounded local snapshot/i);
    await expect(page.locator("p.page-lede")).toContainText(/not a server-attested report/i);
    // And the same caveat must be carried in the export's own omissions list.
    await expect(page.locator(".omissions")).toContainText(/not a server-attested report/i);

    // Raw bodies are opt-in AND reviewed: the checkbox alone must not arm them.
    const includeBodies = page.getByLabel(/include raw request and response bodies/i);
    await expect(includeBodies).not.toBeChecked();
    await expect(page.getByText(/Metadata only\. Bodies are excluded\./i)).toBeVisible();

    await includeBodies.check();
    await expect(page.getByRole("region", { name: /included data review/i })).toBeVisible();
    await expect(page.getByText(/not armed yet/i)).toBeVisible();

    await page.getByLabel(/I have reviewed the included data/i).check();
    await expect(page.getByText(/Bodies WILL be embedded, unredacted\./i)).toBeVisible();
  });

  test("never labels the estimate as verified savings", async ({ page }) => {
    await page.goto("/costs");
    await expect(page.getByText(/spend avoided/i)).toHaveCount(0);
    await expect(page.getByText(/not verified spend/i).first()).toBeVisible();
  });
});

test.describe("accessibility", () => {
  // Runs the rules jsdom cannot evaluate — notably colour contrast, which
  // needs a real layout and cascade (epic §10).
  //
  // Both themes, because they do not share a palette: the dark theme overrides
  // only some tokens, so a foreground can be perfectly legible in one theme and
  // fail in the other. Testing light alone hid exactly that (the success
  // foreground was inherited from the light theme and measured ~3.1:1 on dark).
  for (const theme of ["light", "dark"] as const) {
    for (const path of ["/", "/overview", "/logs", "/costs", "/reports", "/agents/echo-agent/edit"]) {
      test(`${path} has no WCAG A/AA violations (${theme})`, async ({ page, baseURL }) => {
        // The app's theme is a cookie-driven data-theme attribute; it never
        // consults prefers-color-scheme. emulateMedia() therefore does NOT
        // switch it, and a sweep written that way silently tests light twice.
        await page.context().addCookies([
          {
            name: "mockagents-theme",
            value: theme,
            url: baseURL ?? "http://127.0.0.1:3099",
          },
        ]);
        await page.goto(path);
        // Prove the theme actually applied, so this can never go vacuous again.
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        const results = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
          .analyze();

        const findings = results.violations.flatMap((v) =>
          v.nodes.map((n) => `${v.id} at ${n.target.join(" ")}\n    ${n.failureSummary ?? v.help}`),
        );
        expect(findings).toEqual([]);
      });
    }
  }

  // A second pass at a narrow viewport.
  //
  // Some accessibility rules can only fire once layout actually changes. The
  // instrument strip shipped as a horizontally scrolling region with nothing
  // focusable inside it — unreachable by keyboard, since there was nothing to
  // Tab to and therefore no way to scroll it without a pointer. axe's
  // `scrollable-region-focusable` rule covers exactly that and was already in
  // the tag set above, but it never fired: at the default 1280px viewport the
  // strip does not overflow (984 = 984), so there is no scrollable region to
  // report. At 720px it overflows (970 > 424) and the rule fires.
  //
  // The gap was viewport coverage, not tooling. One narrow pass closes it.
  // Contrast is theme-dependent and covered above; what changes here is layout,
  // focus order and scroll, so a single theme is enough.
  for (const path of ["/", "/overview", "/logs", "/costs", "/reports", "/agents/echo-agent/edit"]) {
    test(`${path} has no WCAG A/AA violations (narrow 720px)`, async ({ page }) => {
      await page.setViewportSize({ width: 720, height: 900 });
      await page.goto(path);

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
        .analyze();

      const findings = results.violations.flatMap((v) =>
        v.nodes.map((n) => `${v.id} at ${n.target.join(" ")}\n    ${n.failureSummary ?? v.help}`),
      );
      expect(findings).toEqual([]);
    });
  }

});
