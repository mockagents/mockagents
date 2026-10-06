# Security Policy

## Supported versions

MockAgents is pre-1.0. Security fixes land on `main` and ship in the next
release; the latest minor release line also receives fixes for High and
Critical issues.

| Version | Supported |
|---|---|
| `main` | ✅ |
| Latest minor release (currently 0.5.x) | ✅ High and Critical fixes |
| Earlier releases | ❌ |

## Reporting a vulnerability

**Please do not open a public GitHub issue, discussion or pull request for a
security vulnerability.**

Report privately via GitHub's built-in advisory flow:
**[Report a vulnerability »](https://github.com/mockagents/mockagents/security/advisories/new)**

Please include:

- a description of the issue and its potential impact,
- steps to reproduce (a minimal agent YAML + request is ideal),
- the affected version (`mockagents --version`) or commit SHA.

## What to expect

| Step | Commitment |
|---|---|
| Acknowledgement | within **3 business days** |
| Assessment and severity (CVSS) | within **7 days** of acknowledgement |
| Fix for High or Critical | target within **30 days**; Medium and Low in a following release |
| Public disclosure | coordinated with you, at the latest **90 days** after the report |

Fixes are developed in a private fork attached to the advisory. When the fix is
released, the advisory is published as a GitHub Security Advisory, with a CVE
requested for anything rated Medium or above, and the reporter is credited
unless they ask not to be. Please keep the details private until then.

## Scope

**In scope:**

- the mock server binary: protocol adapters, the engine, tenancy and
  authentication, quotas, storage, recording/replay and the MCP, A2A and
  Realtime surfaces;
- the web console (`gui/`);
- the SDKs (`sdk/python`, `sdk/typescript`, `sdk/vitest`, `sdk/npx`,
  `sdk/go`), including their binary download and verification;
- the Helm chart (`deploy/helm`), the container image, and the GitHub Actions
  and GitLab CI integrations under `deploy/`;
- the release workflows and published artifacts.

**Out of scope:**

- the documentation site content and example agent YAML (report mistakes as
  normal issues);
- the behaviour of the real upstream providers — MockAgents emulates their
  wire protocols, not the models;
- deployments that expose a single-tenant (unauthenticated) server to an
  untrusted network: MockAgents is a test and development tool, and
  single-tenant mode has no authentication by design. Multi-tenant mode
  (`MOCKAGENTS_MULTI_TENANT=1`) is the supported configuration for shared
  deployments.
