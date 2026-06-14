# MockAgents — Enhancement Backlog (from 2026-06-10 PMF research)

Companion to `2026-06-10-mockagents-pmf-research.md`. Ordered by leverage for **pure-OSS community adoption**. Effort: **S** ≤1 day · **M** ≤1 week · **L** >1 week. Each item has a one-line "done-when."

> Guiding principle: **distribution before features.** The product is good; it is uninstallable and invisible. Do P0 first, in order. Do **not** chase aimock's surface area (vector DBs, A2A, AG-UI, 14 providers) — win on depth, fault injection, load physics, and the single Go binary.

---

## P0 — Distribution & launch (nothing else matters until these ship) 🔴

> **Decision locked (2026-06-11):** keep the **`mockagents`** brand everywhere — do **not** rename code/paths to `anandtopu`. The `go.mod`-vs-repo mismatch is resolved by **registering the `mockagents` org + namespaces** (option A), not by a code rewrite. `go.mod` stays `github.com/mockagents/mockagents`.
>
> **UPDATE (2026-06-14): the org + canonical repo are LIVE.** The project was migrated to **`github.com/mockagents/mockagents`** (now the canonical `origin`, default branch `main`; still **Private** pending the public flip). `hooks/pre-push` now guards the **`mockagents/mockagents`** URL. The old `anandtopu/mock-agents` is kept as-is (local remote `legacy`) — there are no longer any `anandtopu` references anywhere except that legacy mirror. **D-00 below is the prerequisite for D-01/D-03/D-04/D-05** — claim the remaining namespaces (Docker/PyPI/npm), then make the repo Public.

### D-00 — Org & namespace registration checklist (prerequisite; external account actions) 🔑

These are account/registration actions (not code). Until they're done, the existing paths/badges don't resolve and `go install` fails — but **no code change is needed once they exist.** Claim every namespace as **`mockagents`** to keep the brand consistent.

- [x] **GitHub org `mockagents`** — ✅ registered (2026-06-14). *(Unblocks the `go.mod` module path, `go install`, CI/Go-Report badges, Releases links, and the Homebrew tap in one stroke.)*
- [x] **Repo `mockagents/mockagents`** — ✅ migrated (2026-06-14): `main` + `autobuild/state` pushed, default branch `main`, now the canonical `origin`. Still **Private** (flip to Public when ready). Old `anandtopu/mock-agents` kept as-is (local remote `legacy`); `hooks/pre-push` now guards `mockagents/mockagents`.
- [ ] **Enable GitHub Pages / CI under the org** so the badge URLs (`github.com/mockagents/mockagents/actions/...`) and `goreportcard.com/report/github.com/mockagents/mockagents` light up.
- [ ] **Docker Hub org `mockagents`** + repo `mockagents/mockagents` — claim it so `docker run mockagents/mockagents` and the Helm/k8s `image:` refs resolve. *(If the org name is taken, fall back to GHCR `ghcr.io/mockagents/mockagents` and update the README `docker run` line + Helm `values.yaml` — still under the `mockagents` name.)*
- [ ] **PyPI project `mockagents`** — register/own the project name (reserve early; squatting is common).
- [ ] **npm org/scope `@mockagents`** — create the npm org so `@mockagents/sdk` (TS SDK) and the `npx mockagents` wrapper publish under scope.
- [ ] **Homebrew tap repo `mockagents/homebrew-tap`** — create it under the org so `brew install mockagents/tap/mockagents` works (goreleaser can push the formula here).
- [ ] **(defensive) Reserve the name on adjacent registries** — Go Report Card auto-resolves from the repo (no claim needed); optionally reserve the GitHub org's `mockagents` handle variants to prevent impersonation.

**Done when:** the org exists with the repo under it, and a fresh-machine `go install github.com/mockagents/mockagents/cmd/mockagents@latest` resolves (after D-02 publishes a tag).

### D-01..D-08 — code/release tasks (run after D-00)

| ID | Item | Effort | Done when |
|---|---|---|---|
| **D-01** | **Verify the identity is consistent post-registration.** With the `mockagents` org live, confirm `go.mod`, README badges/URLs, `go install`/`go get` lines, and `uses:` Action refs all point at `github.com/mockagents/mockagents` (they already do — this is a no-rename verification, *not* a rewrite). | S | All install paths/badges resolve against the real org; no `anandtopu` refs outside `hooks/pre-push`. |
| **D-02** | **Cut a real release.** Run the already-wired `make release` (goreleaser) for real, tag `v0.1.0`, attach cross-platform binaries to a GitHub Release (under the `mockagents` org). | S | `gh release view v0.1.0` lists binaries; README "Binary" link works. |
| **D-03** | **Publish a container image** as `mockagents/mockagents` (Docker Hub, per D-00) — or `ghcr.io/mockagents/mockagents` if Docker Hub org is unavailable; keep the README `docker run` line matching whatever was claimed. | S | `docker run -p 8080:8080 mockagents/mockagents` starts the server. |
| **D-04** | **PyPI `mockagents`** — binary-bootstrap wheel (release-readiness notes say the py binary bootstrap already exists) so `pipx run mockagents start` works. | M | `pipx run mockagents start` boots on a clean machine. |
| **D-05** | **npm wrapper** — binary-download wrapper package (esbuild/biome pattern) so `npx mockagents start` works; publish `@mockagents/sdk` (TS SDK) under the `@mockagents` scope. | M | `npx mockagents start` boots; `npm i @mockagents/sdk` resolves. |
| **D-06** | **Fix license detection** — restore verbatim Apache-2.0 `LICENSE` text; move extra copyright lines to `NOTICE`. | S | GitHub API stops returning `NOASSERTION`; Apache-2.0 chip shows. |
| **D-07** | **Add discovery topics** — `mock-server, api-mocking, testing, openai-api, anthropic, gemini, mcp, sse, llm-testing, chaos-engineering`. | S | Topics live on the repo. |
| **D-08** | **Embed the demo GIF** (README `TODO(RR-03)`): 12s of `mockagents start` + console SSE. | S | GIF renders in the README. |

---

## P1 — Reach + the two credibility gaps 🟠

| ID | Item | Effort | Done when |
|---|---|---|---|
| **A-01** | **OpenAI Responses API adapter** (`/v1/responses` + `response.*` SSE events + stateful `previous_response_id` + stub built-in tools). *The single highest-impact feature* — it's the **default** OpenAI Agents SDK path. | L | `openai-agents-python` with its **default** model runs green against MockAgents. |
| **M-01** | **Streamable-HTTP MCP transport** — `Mcp-Session-Id`, SSE-on-POST, GET resumability (`Last-Event-ID`), `MCP-Protocol-Version` header, Origin 403; bump default to `2025-11-25`; target the **2026-07-28 stateless RC**; replace the custom `X-MCP-Pending-Notifications` envelope. | L | An official MCP SDK client (2025-11-25) connects without a shim. |
| **M-02** | **MCP conformance badge** — run `npx @modelcontextprotocol/conformance server --url …` in CI; re-cast Sampling/Roots/Logging as "legacy-spec compatibility testing." | S | CI runs conformance; README badges "conformance-validated mock MCP server." |
| **E-01** | **`pytest-mockagents`** — fixture that spawns the binary and patches `OPENAI_BASE_URL`/`ANTHROPIC_BASE_URL`/`GOOGLE_GEMINI_BASE_URL`, with request-assertion helpers. Ship on PyPI. | M | `pytest` example boots+asserts in <5 lines; rides vcrpy's search traffic. |
| **E-02** | **`@mockagents/vitest` (+ Jest helper)** — same auto-spawn/patch ergonomics. | M | Vitest example green in <5 lines. |
| **E-03** | **Marketplace GitHub Action `setup-mockagents`** — wrap `mockagents start` with a `fixtures` input (publish the existing `deploy/actions` composite). | S | `uses: <org>/setup-mockagents@v1` works in a sample workflow. |
| **E-04** | **Testcontainers modules** (Go + Python + Java) wrapping the image; submit to `testcontainers.com/modules`. *(needs D-03)* | M | Listed on testcontainers.com; quickstart runs. |
| **DOC-01** | **Per-framework "Testing with MockAgents" docs PRs** for the 4 frameworks with **no** official mock story: OpenAI Agents SDK, CrewAI, Google ADK, Claude Agent SDK (demos already in `demo/`). Plus LangChain/LangGraph "fake backends" recipe. | M | PRs opened against each framework's docs; in-repo guides published. |
| **A-02** | **`/v1/embeddings`** — `text-embedding-3` shape, deterministic hashed/seeded vectors, configurable `dimensions`, usage tokens. | S | An SDK `embeddings.create` against the mock returns stable vectors. |
| **A-03** | **Structured-outputs strict mode** — when `response_format={type:json_schema, strict:true}`, emit schema-conforming JSON from the fixture + simulate a `refusal`. | M | SDK `.parse()` (Pydantic/Zod) round-trips; refusal path testable. |

---

## P2 — Depth, agent-channel, repo health 🟡

| ID | Item | Effort | Done when |
|---|---|---|---|
| **R-01** | **Record/replay v2: record modes** — `--record-mode=once\|new_episodes\|none\|all` + `--upstream` (wire the existing `Replay.Fallback` seam for record-on-miss). | M | record-on-miss + replay-with-passthrough work from the CLI. |
| **R-02** | **Configurable matchers + miss diagnostics** — `match:{ignore_fields:[temperature,seed,stream,metadata]}`; on miss, return nearest interaction + JSON diff in the 404 body. | M | A drifted prompt returns a diff, not an opaque hash. |
| **R-03** | **Cassette redaction** — `--redact` (regex/JSONPath over bodies, default-mask `sk-*`/`anthropic` keys) reusing `storage.SanitizeBody`. | S | Recorded bodies are "safe to commit." |
| **R-04** | **Sequenced playback** — store duplicate-hash interactions as an ordered list; replay in order (+ repeat-last). | S | A multi-turn loop replays correct per-turn responses. |
| **R-05** | **Importers** — `mockagents import vcr <cassette.yaml>` and `import openai-stored-completions <export.jsonl>` → cassettes/agent YAML (adoption funnels). | M | A vcrpy cassette replays through MockAgents. |
| **MCP-03** | **Expose MockAgents over MCP** (`mockagents serve-mcp`) wrapping the existing agent write API: `create_agent`, `add_scenario`, `inject_fault`, `explain_unmatched_request`, `get_interactions`, `spin_up_mock(provider, scenarios_yaml)` (uses the in-process engine for ms-fast disposable mocks). Target the stateless RC transport. | L | A Claude Code/Cursor session creates+drives a mock conversationally. |
| **MCP-04** | **Register in every agent-discovery surface** — official MCP registry, Smithery, mcp.so, PulseMCP; a Claude Code plugin (skill+MCP); in-repo Cursor rules; `llms.txt`; awesome-mcp-servers / awesome-claude-code PRs. | M | "mockagents" returns hits in the MCP registry (currently 0). |
| **A-04** | **Anthropic depth** — `cache_creation/cache_read` usage fields (driven by `cache_control`), extended-thinking blocks (beta-header gated), `/v1/messages/count_tokens`. | M | Cost-cache + thinking-trace tests run offline. |
| **A-05** | **Vision input parsing** (OpenAI `image_url`/data-URL, Anthropic `source.type=base64`) + scenario matching on image presence/count. | M | A vision-agent test asserts on multimodal request handling. |
| **A-06** | **Azure OpenAI URL routing** — `/openai/deployments/{id}/chat/completions?api-version=…` + new `/openai/v1`. | M | An `AzureOpenAI()` client runs unchanged. |
| **A-07** | **`/v1/moderations`** (omni-moderation shape: `flagged` + per-category scores) — complements hallucination fixtures. | S | A guardrail pipeline asserts flagged/category scores. |
| **A-08** | **Batch API** (OpenAI `/v1/batches` upload→poll→retrieve over chat/embeddings/moderations; Anthropic Message Batches) with simulated lifecycle/latency. | L | A batch upload→poll→retrieve flow completes against the mock. |
| **H-01** | **Repo health** — honest README comparison table (vs aimock/ai-mocks/mockllm/WireMock; position against LocalStack's 2026 free-tier retirement: "Apache-2.0, no open-core, no paid tier"); enable Discussions; seed 8–10 good-first-issues (deferred **FB-03 slice 5 connection faults** is a natural one); add `CONTRIBUTING.md`; fix the CI badge. | M | Repo reads "alive"; comparison table is the SEO answer page. |
| **H-02** | **Awesome-list + comparison-SEO placement** — PRs into awesome-llm-agents, Awesome-LLMOps, awesome-agent-harness, awesome-go (Testing); one "test AI agents without burning tokens" post into the empty provider-mock slot in 2026 LLM-testing comparison content. | M | MockAgents appears in ≥3 curated lists. |

---

## Explicitly NOT doing (cede to aimock / out of scope)

- Vector-DB mocks (Pinecone/Qdrant/Chroma), A2A, AG-UI, WebSocket Realtime/Gemini Live, 14-provider breadth, TTS/transcription. These are aimock's land; chasing them dilutes the depth/fault-injection/load-physics wedge.
- Eval/scoring/LLM-as-judge. MockAgents is the deterministic plumbing layer; stay *complementary* to promptfoo/Langfuse/Braintrust.

## Pre-existing item surfaced (separate ticket)

- **`decode.go` unbounded body read** — `decodeJSONBody` reads the entire request body with no size limit (`maxPooledBodyBufBytes` only governs pool reuse). Unbounded-allocation DoS on every adapter route. Fix: wrap in `http.MaxBytesReader`. *(Flagged earlier in the agent-sdk-demos review; still unfiled.)*
