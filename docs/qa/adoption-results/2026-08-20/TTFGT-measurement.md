# Time-to-first-green-test — measured

**Date:** 2026-08-20 · **Method:** clean-room replay of the documented commands,
verbatim, with cold caches · **Targets:** quickstart <5 min, RAG example <10 min
([`docs/ADOPTION_REQUIREMENTS.md`](../../../ADOPTION_REQUIREMENTS.md) R4)

## Headline

| Path | As documented, before this run | After the doc fixes | Target |
| --- | --- | --- | --- |
| Quickstart | ❌ **never reaches green** — 2 of 3 install tabs dead, and the test step's `pip install mockagents` fails | ✅ **36.3 s** | <5 min |
| RAG example (`demo/rag-agent`) | ✅ **72.5 s** cold / **50.0 s** warm | unchanged | <10 min |

**Both targets are met on machine time with roughly 8× headroom.** The finding
that matters is not the number — it is that the *quickstart*, the shorter and
more prominent of the two paths, could not be completed at all by following it.

## What "naive user" means here, and what it does not

This measures **machine time for the documented command sequence, executed
verbatim, from cold caches**. Concretely: a fresh `git clone` from GitHub over
the real network, an empty `GOCACHE` + `GOMODCACHE` (isolated per path so
neither warms the other), a fresh virtualenv, and `pip --no-cache-dir`.

It does **not** measure:

- reading, typing, or deciding — a real first-timer spends more time on the page
  than in the shell;
- recovering from a blocker, which is unbounded and is what the "before" column
  above is really reporting;
- a slower network or machine. Measured on Windows 11, Go 1.26.4, Python 3.12,
  ~300 Mbit. Treat these as a **floor**, not an average.

So this is a necessary-but-not-sufficient check: it proves the commands work and
that no single step is pathologically slow. It does not prove a stranger gets
there in five minutes. That still wants a real person and a stopwatch.

## What was broken

Three of the quickstart's four externally-dependent steps were dead. All were
found by running the page, not by reading it.

**1. The "Binary (recommended)" download 404s.** The page said:

```
https://github.com/mockagents/mockagents/releases/latest/download/mockagents_linux_amd64.tar.gz
→ 404
```

Release assets carry the version in the filename
(`mockagents_0.4.0_linux_amd64.tar.gz`), so there is no version-independent URL
to give. Fixed by pinning `VERSION` in the snippet and naming the real assets.

**2. The Docker tab is not published.** `mockagents/mockagents` returns 404 from
the Docker Hub API. The README's install table already carried a ⏳ marker for
this; the quickstart listed it with no caveat at all. Fixed by adding the
warning and pointing at the install-paths workflow as the source of truth.

**3. `pip install mockagents` fails.** No distribution on PyPI — the same
stalled v0.4.0 release. This is step 5 of the quickstart, the step that produces
the green test, so the page's entire payoff was unreachable. Fixed by
documenting the install-from-checkout route inline, which is the only way to get
the pytest plugin today.

Also corrected: the README claimed prebuilt binaries for "macOS / Linux /
Windows (amd64 and arm64)". There is **no Windows arm64 asset**
(`mockagents_0.4.0_windows_arm64.zip` → 404).

None of these are fixable by publishing docs alone in the sense that matters —
R1 (publish the packages) is the real fix, and it needs credentials. What the
docs can do, and now do, is tell the truth and hand the reader a route that
works.

## Measurements

### Quickstart, after the fixes (cold) — 36.3 s

| Step | Command | Time |
| --- | --- | ---: |
| Q1 | download `mockagents_0.4.0_windows_amd64.zip` | 0.8 s |
| Q2 | unzip | 0.6 s |
| Q3 | `mockagents init my-project` | 1.2 s |
| Q4 | `git clone --depth 1` (for the SDK) | 2.5 s |
| Q5 | `python -m venv` | 3.9 s |
| Q6 | `pip install --no-cache-dir -e mockagents/sdk/python` | 9.4 s |
| Q7 | `pip install --no-cache-dir pytest openai` | 14.8 s |
| Q8 | `pytest` → **1 passed** | 3.1 s |

### RAG example (cold) — 72.5 s

| Step | Command | Time |
| --- | --- | ---: |
| B1 | `git clone --depth 1` from GitHub | 2.1 s |
| B2 | `make build` (empty GOCACHE **and** GOMODCACHE) | 25.5 s |
| B3 | `python -m venv` | 3.8 s |
| B4 | `pip install --no-cache-dir -r requirements.txt` | 24.0 s |
| B5 | `pip install --no-cache-dir -e ../../sdk/python` | 9.0 s |
| B6 | `MOCKAGENTS_BIN=../../mockagents pytest` → **11 passed** | 8.1 s |

### RAG example (warm caches) — 50.0 s

Same sequence, second clone, caches primed: clone 2.0 s · build **4.4 s**
(25.5 → 4.4, the whole cold-start cost is Go) · venv 3.6 s · requirements 22.5 s
· SDK 9.5 s · pytest 8.0 s.

### `go install`, cold — 31.9 s

`go install github.com/mockagents/mockagents/cmd/mockagents@latest` with an
empty `GOPATH`/`GOCACHE`/`GOMODCACHE`: **31.9 s**, exit 0, resolves to v0.4.0.
The one install tab that worked as documented.

## Two things worth knowing

**`@latest` gets you v0.4.0, from 2026-06-20.** Everything on `main` since is
absent from it — including `/metrics` and `/api/v1/ready`. The pytest plugin
lives in the SDK rather than the binary, so the quickstart is unaffected, but
anyone following the docs for a feature added after the tag will not find it.
Another symptom of R1.

**pip dominates both paths.** 24.2 s of the quickstart's 36.3 s and 33 s of the
RAG path's 72.5 s is `pip`, and it barely improves warm (22.5 s vs 24.0 s)
because the work is building and installing, not downloading. If either number
ever needs to come down, that is where it is — not in the Go build, which warms
to 4.4 s.

## Reproducing

The harness is a stopwatch around the documented commands; there is nothing to
install. Per path: make an empty directory, point `GOCACHE`, `GOMODCACHE` and
`GOPATH` at fresh subdirectories of it, create a fresh venv, pass
`--no-cache-dir` to every `pip install`, and time each documented step
individually so a regression names the step rather than the total.

Re-measure when the packages publish (R1) — that removes the clone and the
editable install from the quickstart entirely, and should take it well under
20 s.
