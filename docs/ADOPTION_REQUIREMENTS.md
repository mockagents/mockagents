# Consolidated requirements: making MockAgents attractive to open-source developers

**Status:** Consolidation for implementation planning
**Date:** 2026-08-20
**Sources:** the Product-Market Fit Assessment (rev 2, artifact) and
[`MOCKAGENTS_AIMOCK_PRD.md`](MOCKAGENTS_AIMOCK_PRD.md) (the AIMock-parity PRD, FR-A…FR-K).
Every requirement below carries its source tag; items marked **NEW** appear in
neither document but fell out of reconciling them.

## How the two documents disagree — and how this resolves it

| Tension | Resolution |
|---|---|
| The PRD contains **no distribution requirements at all**. Its ~11–19 weeks of phased build produce packages that, as of today, nobody can install — npm, PyPI, Docker Hub and the Homebrew tap all 404 (PMF §1). | Publishing is Tier 0. Every FR in the PRD is unreachable by users until it lands. |
| The PRD sequences **build-first** (Phase 0 freezes schemas for 1–2 weeks before any user-visible value). The PMF memo sequences **validate-first** (design partners before major bets). | Tiers below order by adoption funnel — gettable → first success → daily value — and gate the largest architectural bet (MockStack) on design-partner evidence. |
| The PRD's biggest item, `kind: MockStack` (FR-A01–A05), is the project's largest bet, specified with zero external users consulted. | Deferred to Tier 3, explicitly gated: build after ≥3 design partners confirm the one-config-whole-stack pain is theirs, not ours. |
| [#33](https://github.com/mockagents/mockagents/issues/33) — pipelines had no HTTP execution surface — was the flagship *agentic* gap (PMF P0) and is absent from the PRD. | Added as Tier 2 and now shipped: HTTP execution plus typed SDK `node_sequence` assertions close the last trajectory-assertion gap. |
| The PRD lists as to-build several things that **already shipped** this cycle: README repositioning, trajectory assertions in pytest/Vitest (part of FR-B04), Discussions. | Marked DONE below so effort isn't double-counted. |

## The adoption funnel this optimizes

An OSS developer adopts in stages: **discover → trust → install → first green test →
daily habit → advocate.** Today the funnel breaks at *install* (4 of 6 advertised paths
404) — so nothing downstream of it can be measured, and everything upstream of it
(positioning, now shipped) is wasted until it's fixed.

---

## Tier 0 — Be gettable *(days; blocks everything)*

| # | Requirement | Source |
|---|---|---|
| R1 | ⛔ **Blocked — needs credentials only the maintainer has.** Publish all advertised packages: npm (`mockagents`, `@mockagents/sdk`, `@mockagents/vitest`), PyPI, Docker Hub + GHCR, Homebrew tap. Needs four credential sets + the `pypi` environment; re-run Release. Zero code changes. | PMF F0 |
| R2 | Keep the [install-paths guard](../.github/workflows/install-paths.yml) authoritative: as each path ships, clear it from `install-paths-pending.txt` and drop the ⏳ README marker (the guard goes red on a *revived* path precisely to force this). | PMF F0 — **shipped**, needs upkeep |
| R3 | ⛔ **Blocked — needs a domain purchase.** Repo storefront: social-preview image (every Slack/X share currently renders a grey card), register `mockagents.dev` **before someone else does — it is already the `$id` of all four shipped JSON Schemas** — and set it as the repo homepage. | PMF §6 · **NEW** (domain-squat risk) |

## Tier 1 — First green test in minutes *(0–4 weeks)*

**Status: all seven shipped** (2026-08-20). Every code sample in the touched
docs was executed before being written down, and every new CI gate was
fire-drilled — deliberately made to fail — before being trusted.

| # | Requirement | Status |
|---|---|---|
| R4 | A complete runnable RAG example: clone → one command → deterministic green test in **under 10 minutes**; first mock via quickstart in **under 5**. Both numbers measured on people who have never seen the product, not asserted. *(FR-K03 + PRD §14 · PMF metrics)* | ✅ [`demo/rag-agent`](../demo/rag-agent) — 10 tests, one `pytest`, CI-verified; retrieval now runs through VectorMock on the same server. **Measured**, cold caches, commands run verbatim: RAG path **72.5s**, quickstart **36.3s** — both inside target ([results](qa/adoption-results/2026-08-20/TTFGT-measurement.md)). Caveat: the timing predates the faster one-process VectorMock migration and is machine time, not a stranger with a stopwatch. |
| R5 | Make the already-shipped zero-config ergonomics the *headline* docs path: pytest fixture (no import needed) and `setupMockAgents()` (Vitest/Jest), with trajectory assertions in the first code sample a visitor sees. *(PMF F3)* | ✅ README, quickstart, both SDK guides, testing-agents cookbook. Running the samples first turned up a 58.8s hang in `MockAgentServer.stop()` that made the headline path unusable, and a cookbook command that never worked (six examples share `model: gpt-4o`). |
| R6 | Framework recipes as runnable templates, not prose: LangGraph, CrewAI, OpenAI Agents SDK, Vercel AI SDK. *(PMF roadmap · FR-K03)* | ✅ [`examples/frameworks`](../examples/frameworks) — 15 tests against one shared fixture, one venv per framework (CrewAI pins `openai<3`, the Agents SDK needs `openai>=3`), all in CI. |
| R7 | One flagship demo: a guardrail bug caught by a hallucination fixture that a real model would have hidden. *(PMF roadmap)* | ✅ Folded into R4 rather than built twice: `naive_grounding_ok` — "did the model cite anything?" — returns True for a fabricated citation, and a test asserts the gap against the real check. |
| R8 | Publish the evals-vs-tests stance (short doc: evals measure model quality; mocks make application logic testable; you need both). *(PMF §6)* | ✅ [`EVALS_VS_TESTS.md`](EVALS_VS_TESTS.md), linked from the README's "What it is *not*". |
| R9 | Operability floor: a Prometheus `/metrics` endpoint, and a real **readiness** signal distinct from liveness. *(FR-J02 + PRD §11)* | ✅ `GET /metrics` (hand-written exposition, no runtime dependency, validated against the upstream Prometheus parser) and `GET /api/v1/ready`; the chart's readiness probe now points at it. The chart's docs claimed expvar counters that never existed — corrected. |
| R10 | OSS hygiene: CONTRIBUTING quick-path, 5–10 labeled good-first-issues, a stated issue-response norm. *(NEW)* | ✅ Five-minute path (`go build ./... && go test ./internal/...` is the entire setup), [#37–#44](https://github.com/mockagents/mockagents/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) filed and labeled after re-verifying each against the code, and a stated norm: first response within a week, bump after two. |

## Tier 2 — Differentiated capability *(1–3 months)*

| # | Requirement | Source |
|---|---|---|
| R11 | ✅ **VectorMock**: provider-neutral bounded core; tenant-isolated Qdrant, Pinecone, and Chroma v2 profiles; deterministic scores/stable-ID ties; namespaces and equality filters; recursive `VectorCollection` fixtures; empty/weak/contradictory/dimension/top-k/partial-result failure modes; and the RAG demo migrated to the shared no-egress vector surface. | FR-E01–E05 ≡ PMF F2 (convergent — both docs derived it independently) |
| R12 | ✅ **Search + rerank + moderation mounts**: declarative Tavily substring/regex fixtures with ordered results, `published_date`, domain/date filtering, and provider-compatible `max_results` windows; deterministic Cohere v2 rerank; OpenAI moderation; and shared latency/status/malformed/disconnect/partial controls with provider-shaped errors. | FR-F01–F04 |
| R13 | ✅ **Pipeline execution over HTTP** (#33): `POST /api/v1/pipelines/{name}/run` returns ordered nodes and partial results; typed Python/TypeScript clients expose exact `node_sequence` assertions. | PMF P0 · absent from PRD |
| R14 | **Drift detection**: three-way triangulation (SDK types ↔ live provider ↔ mock), scheduled-only (never on PR/fork — cost and key exposure), per-provider skip on missing creds, cost-capped, report names the adapter file to fix. **In progress:** provider-neutral offline body/header shape, enum, stream-event order, and named error-contract comparison with configured volatile-path exclusions, JSON/Markdown/SARIF/JUnit reports, a seeded critical-drift test, and a schedule-only scrubbed Cohere baseline are shipped; credentialed collectors, versioned baselines, and expiring exceptions remain. Note: existing `tools/driftcheck` is unrelated (checks repo API refs/licenses). | FR-H01–H05 ≡ PMF F1 (convergent) |
| R15 | **Chaos everywhere with deterministic seeds. In progress:** a provider-neutral fixed-seed decision layer, explicit per-request force/off override, response decision metadata, and documented precedence are shipped for search/rerank/moderation, VectorMock (Qdrant, Pinecone, Chroma), MCP HTTP and stdio (bounded latency/timeouts, protocol-shaped errors, malformed JSON, and per-operation/fixture/sequence rate overrides), and A2A JSON-RPC with the same scoped controls while preserving legacy always-on fixtures. Broader fault actions, journal recording, explicit strict-replay controls, and global/service scopes remain. | FR-I01–I04 |
| R16 | Selective provider expansion, demand-ranked and capped: Ollama (local-dev default, cheapest), Bedrock (enterprise blocker), then Vertex/OpenRouter *on request*. Each provider added is a permanent drift-surface obligation — that carrying cost is why the list is four, not thirteen. No provider merges without its drift test. | FR-C02 + PMF F5 |

## Tier 3 — Unified stack *(gated on evidence, not on the calendar)*

| # | Requirement | Gate | Source |
|---|---|---|---|
| R17 | `kind: MockStack` — one config, one port, mounts for `/v1` `/mcp` `/a2a` `/vector` `/services`, lifecycle CLI, cross-reference validation. | ≥3 design partners independently confirm the one-config pain | FR-A01–A05 |
| R18 | Whole-stack record/replay: capture all declared services, pre-write redaction, strict no-egress mode that *proves* zero upstream calls. | With R17 | FR-G01–G05 |
| R19 | Unified journal with correlation IDs across every protocol + console views. | With R17 | FR-J01, J03–J04 |
| R20 | Migration guides and converters — including `mockagents convert aimock`, making the switch *to* us a one-command move (vcrpy + OpenAI-stored importers already exist as the pattern). | After Tier 1 lands | FR-K04 + PMF F8 |

## Declined — from both documents, restated so they stay declined

Video generation mocking · chasing all 13 aimock providers · a Node-native embedded
engine (the static Go binary **is** the differentiator) · relaxed-by-default matching
(determinism is the promise; any fallback is opt-in) · building a production vector DB,
crawler, or reranker · evaluating prompt/model quality · replacing WireMock for non-LLM
HTTP.

## Success metrics (consolidated, deduplicated)

| Metric | Target | Source |
|---|---|---|
| Install-path checks green in the [guard workflow](../.github/workflows/install-paths.yml), with an empty pending file | 11/11 (today: 3/11 work, 8 declared pending) | PMF |
| First non-zero weekly download count | exists at all | PMF |
| Time-to-first-green-test (naive user, measured) | <5 min quickstart · <10 min RAG example | PMF + PRD §14 |
| Issues/Discussions opened by non-maintainers | the earliest real PMF signal | PMF |
| Repos still running it in CI ≥2 weeks after first use | tried-it vs depends-on-it | PMF |
| Strict-replay upstream egress | 0, proven | PRD §14 |
| Canonical response repeatability | ≥99.9% | PRD §14 |
| Live AI calls eliminated in reference CI | ≥80% | PRD §14 |

## The one-paragraph version

Publish the packages (R1–R3, days). Make the first ten minutes excellent and loudly
documented (R4–R10, weeks). Then build the three things that no competitor can copy
cheaply — retrieval failure modes, pipeline execution over HTTP, and three-way drift
detection (R11–R16) — while validating the unified-stack bet with real external users
before committing to it (R17–R19). Every tier is sequenced so that the project is
*measurably adoptable* before it gets bigger.
