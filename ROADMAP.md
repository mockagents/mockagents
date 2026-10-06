# Roadmap

This is the public view of where MockAgents is heading. It is a direction, not a
commitment to dates; discussion happens in
[GitHub Discussions](https://github.com/mockagents/mockagents/discussions), and
work is tracked in issues.

## Now: quality and trust (0.6)

The 2026-10-06 quality review and its resolution plan
(`docs/reviews/2026-10-06-quality-*.md`) drive the next release:

- **Fail closed in a testing tool** — strict YAML decoding, a pinned CLI
  exit-code contract, and SDK assertions that cannot pass on absent data.
- **Wire fidelity** — streamed arguments, error shapes and session semantics
  that match the real provider APIs.
- **Bounded, metered state** — quotas that apply to every path into the engine,
  bounded protocol state for MCP, A2A and Realtime.
- **Open-source process** — governance, DCO, CI gates (format, lint, docs,
  JS SDKs), signed releases with SBOM and provenance, and an OpenSSF
  Scorecard.

## Next

- **Published channels** — Docker image, PyPI, npm, `npx`, `pipx` and Homebrew
  (see the install table in the README).
- **Cross-SDK contract suite** — one set of fixtures and assertions run by the
  Python, TypeScript and Go SDKs and the YAML runner against a real server.
- **Fuzzing** of every parser that takes untrusted input.

## Later

- **Shared state for multi-replica deployments** — sessions, logs, audit and
  rate buckets behind store interfaces with a Postgres backend.
- **Structural refactors** — one adapter dispatch skeleton, a route policy
  table, an exported stream plan, a shared JSON-RPC core.

Want to help with any of these? Start with
[CONTRIBUTING.md](CONTRIBUTING.md) and say which item in a discussion.
