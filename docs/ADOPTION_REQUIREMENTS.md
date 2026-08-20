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
| [#33](https://github.com/mockagents/mockagents/issues/33) — pipelines have no HTTP execution surface — is the flagship *agentic* gap (PMF P0) and is absent from the PRD. | Added as Tier 2. It also unblocks `node_sequence` in the SDKs, closing the last trajectory-assertion gap. |
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
| R1 | Publish all advertised packages: npm (`mockagents`, `@mockagents/sdk`, `@mockagents/vitest`), PyPI, Docker Hub + GHCR, Homebrew tap. Needs four credential sets + the `pypi` environment; re-run Release. Zero code changes. | PMF F0 |
| R2 | Keep the [install-paths guard](../.github/workflows/install-paths.yml) authoritative: as each path ships, clear it from `install-paths-pending.txt` and drop the ⏳ README marker (the guard goes red on a *revived* path precisely to force this). | PMF F0 — **shipped**, needs upkeep |
| R3 | Repo storefront: social-preview image (every Slack/X share currently renders a grey card), register `mockagents.dev` **before someone else does — it is already the `$id` of all four shipped JSON Schemas** — and set it as the repo homepage. | PMF §6 · **NEW** (domain-squat risk) |

## Tier 1 — First green test in minutes *(0–4 weeks)*

| # | Requirement | Source |
|---|---|---|
| R4 | A complete runnable RAG example: clone → one command → deterministic green test in **under 10 minutes**; first mock via quickstart in **under 5**. Both numbers measured on people who have never seen the product, not asserted. | FR-K03 + PRD §14 · PMF metrics |
| R5 | Make the already-shipped zero-config ergonomics the *headline* docs path: pytest fixture (no import needed) and `setupMockAgents()` (Vitest/Jest), with trajectory assertions in the first code sample a visitor sees. | PMF F3 — code **shipped**, docs are the gap |
| R6 | Framework recipes as runnable templates, not prose: LangGraph, CrewAI, OpenAI Agents SDK, Vercel AI SDK. | PMF roadmap · FR-K03 |
| R7 | One flagship demo: a guardrail bug caught by a hallucination fixture that a real model would have hidden. This is the "fail on purpose" positioning made concrete. | PMF roadmap |
| R8 | Publish the evals-vs-tests stance (short doc: evals measure model quality; mocks make application logic testable; you need both). Cheap, clarifying, category-defining. | PMF §6 |
| R9 | Operability floor: a Prometheus `/metrics` endpoint (none exists today), and a real **readiness** signal — `/api/v1/health` exists and both Helm probes point at it, but it returns an unconditional 200 the moment the process is up, so readiness and liveness are currently the same check. The PRD's own §11 requires distinguishing them. *(Corrected on review: an earlier draft claimed the main server had no health route and the chart no probes — both false.)* | FR-J02 + PRD §11 |
| R10 | OSS hygiene: CONTRIBUTING quick-path, 5–10 labeled good-first-issues, a stated issue-response norm. Discussions is already on. Non-maintainer issues are the earliest PMF signal — make filing one easy. | **NEW** |

## Tier 2 — Differentiated capability *(1–3 months)*

| # | Requirement | Source |
|---|---|---|
| R11 | **VectorMock** (Pinecone/Qdrant/Chroma wire-compatible; deterministic scores, stable-ID tie-breaks, no network). Differentiate on *retrieval failure modes*: empty result sets, low-confidence-only, contradictory metadata, dimension mismatch, partial results — the RAG bugs that actually ship. | FR-E01–E05 ≡ PMF F2 (convergent — both docs derived it independently) |
| R12 | Search + rerank + moderation mounts (Tavily-shaped, Cohere-shaped; moderation exists — mount it), with the same chaos/fault treatment as everything else. | FR-F01–F04 |
| R13 | **Pipeline execution over HTTP** (#33): an app under test must be able to drive `kind: Pipeline`. Also unblocks `node_sequence` in both SDKs. | PMF P0 · absent from PRD |
| R14 | **Drift detection**: three-way triangulation (SDK types ↔ live provider ↔ mock), scheduled-only (never on PR/fork — cost and key exposure), per-provider skip on missing creds, cost-capped, report names the adapter file to fix. Run the equivalent of the benchguard fire drill before trusting the gate. Note: existing `tools/driftcheck` is unrelated (checks repo API refs/licenses). | FR-H01–H05 ≡ PMF F1 (convergent) |
| R15 | Chaos everywhere with deterministic seeds: extend fault injection to vector/search/rerank/MCP/A2A scopes, seeded + per-request forcible, precedence documented. Extends the "fail on purpose" identity to the whole stack. | FR-I01–I04 |
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
