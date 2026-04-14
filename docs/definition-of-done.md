# Definition of Done

## 1. Document Info

| Field       | Value                                      |
|-------------|--------------------------------------------|
| Version     | 1.0                                        |
| Date        | 2026-04-07                                 |
| Status      | Active                                     |
| Owner       | MockAgents Core Team                       |
| Applies to  | All work items in the MockAgents repository |

---

## 2. Purpose

MockAgents is a mock agent platform that emulates the behavior of real AI provider APIs (OpenAI, Anthropic, and others). Because downstream Python SDKs and integration tests depend on MockAgents producing byte-identical JSON structures, correct SSE streaming sequences, and accurate HTTP status codes, even a subtle deviation in a response field name or error format can silently break consumer code.

A clear, enforceable Definition of Done exists to:

- **Prevent protocol drift.** Every change to adapter output must be verified against the official provider schema before it is considered complete.
- **Maintain cross-language consistency.** The Go engine and the Python SDK must stay in sync; a story is not done until both sides are updated and tested.
- **Protect downstream users.** MockAgents is open-source software that others will depend on for their own CI pipelines. Shipping a broken mock is worse than shipping no mock at all.
- **Keep a small team efficient.** With four engineers, there is no room for rework caused by ambiguous completion criteria. The DoD is the shared contract that lets every team member ship confidently.

---

## 3. Definition of Done -- User Story Level

Every user story must satisfy **all applicable** items below before it can be marked "Done."

### Code Quality

- [ ] Code is implemented and compiles without warnings
- [ ] Code follows Go coding standards (passes `golangci-lint` with project config)
- [ ] Code follows Python coding standards (passes `ruff`, `mypy`) for SDK changes
- [ ] No `TODO` / `FIXME` comments left unresolved
- [ ] Error handling covers all failure paths (no unhandled errors)
- [ ] No hardcoded values that should be configurable

### Testing

- [ ] Unit tests written for all new functions/methods
- [ ] Unit test coverage >= 80% for changed packages
- [ ] Integration tests written for new HTTP endpoints or protocol behavior
- [ ] All existing tests pass (no regressions)
- [ ] Edge cases and error paths tested
- [ ] Test names follow convention: `Test{Function}_{Scenario}_{Expected}`

### Code Review

- [ ] Pull request created with description linking to story ID
- [ ] At least 1 team member approved the PR
- [ ] All review comments addressed or explicitly deferred with issue link
- [ ] No merge conflicts with `main` branch

### Documentation

- [ ] GoDoc comments on all exported types, functions, and methods
- [ ] Python docstrings on all public classes and methods (SDK)
- [ ] README updated if user-facing behavior changed
- [ ] YAML schema reference updated if agent definition fields changed

### Protocol Compliance (for adapter stories)

- [ ] Response JSON matches the official provider schema
- [ ] Tested with the official provider Python SDK (`openai` / `anthropic`)
- [ ] Streaming events follow the correct SSE format
- [ ] Error responses match provider error format

---

## 4. Definition of Done -- Sprint Level

The following checklist must be satisfied before a sprint is considered complete.

- [ ] All committed stories meet the story-level DoD
- [ ] Sprint deliverable is demonstrable in sprint demo
- [ ] CI pipeline passes on `main` branch (all tests green)
- [ ] No critical or high-severity bugs open for sprint scope
- [ ] Sprint retrospective completed, action items logged
- [ ] Updated sprint backlog status in project tracker
- [ ] Technical debt items from this sprint logged as issues

---

## 5. Definition of Done -- Release Level (Alpha v0.1.0)

### Functionality

- [ ] All P0 functional requirements (FR-001 through FR-049 P0s) implemented and tested
- [ ] All P1 requirements implemented or explicitly deferred with issue links
- [ ] OpenAI adapter passes conformance tests against official SDK v1.x
- [ ] Anthropic adapter passes conformance tests against official SDK v0.40+
- [ ] CLI commands (`init`, `start`, `validate`) work on Linux, macOS, Windows
- [ ] Python SDK installable via `pip` and functional

### Quality

- [ ] Overall test coverage >= 80%
- [ ] Zero known critical bugs
- [ ] Performance target met: 1000 req/s non-streaming
- [ ] Binary size < 30 MB, Docker image < 50 MB
- [ ] Security review completed (no path traversal, no template injection)

### Documentation

- [ ] Documentation site live with: quickstart, CLI reference, YAML schema, SDK reference
- [ ] README with badges, install instructions, quickstart
- [ ] CONTRIBUTING.md with dev setup, testing, PR process
- [ ] CHANGELOG.md with all alpha features
- [ ] 3+ example agent definitions in the repository

### Distribution

- [ ] GitHub release with cross-platform binaries + checksums
- [ ] PyPI package published as `mockagents`
- [ ] Docker image pushed to registry
- [ ] GitHub Actions CI pipeline green

### Legal / Compliance

- [ ] Apache 2.0 LICENSE file present
- [ ] All dependencies audited (`govulncheck`, `pip-audit`)
- [ ] No copyleft dependencies in the binary

---

## 6. Definition of Done -- Bug Fix

- [ ] Root cause identified and documented in PR description
- [ ] Fix implemented
- [ ] Regression test added that fails without the fix
- [ ] All existing tests pass
- [ ] PR reviewed and merged

---

## 7. Definition of Done -- Documentation Change

- [ ] Content is technically accurate (verified by an engineer)
- [ ] Code examples are tested and runnable
- [ ] No broken links
- [ ] Renders correctly on documentation site
- [ ] Reviewed by at least 1 team member

---

## 8. DoD Exceptions Process

There will be cases where the full DoD cannot be met -- time pressure, external blockers, or exploratory spikes. The following process applies when an exception is needed:

1. **Document the exception in the PR.** The PR description must explicitly list which DoD items are not satisfied and why.
2. **Obtain approval from the team lead.** The team lead reviews the justification and approves or rejects the exception.
3. **Create a follow-up issue.** A new issue is created with the `tech-debt` label capturing the outstanding DoD items, linked to the original PR.
4. **Track in retrospective.** All exceptions from the sprint are reviewed during the sprint retrospective to identify patterns and systemic issues.

Exceptions are expected to be rare. Repeated exceptions for the same DoD item signal that the item needs to be revised or that the team needs additional support.

---

## 9. DoD Evolution

- The DoD is reviewed every **3 sprints** (approximately 6 weeks).
- Any team member can propose changes by opening a PR against this document.
- Changes require **team consensus** (all 4 engineers agree or no more than 1 dissent with documented rationale).
- All revisions are tracked in this document's revision history below.

### Revision History

| Version | Date       | Author | Changes          |
|---------|------------|--------|------------------|
| 1.0     | 2026-04-07 | Team   | Initial version  |
