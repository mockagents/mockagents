# Security Policy

## Supported versions

MockAgents is pre-1.0. The `main` branch is the only supported version; earlier
release tags do not receive backported security fixes.

| Version | Supported |
|---|---|
| `main` (latest) | ✅ |
| Earlier releases | ❌ |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report privately via GitHub's built-in advisory flow:
**[Report a vulnerability »](https://github.com/mockagents/mockagents/security/advisories/new)**

Please include:

- a description of the issue and its potential impact,
- steps to reproduce (a minimal agent YAML + request is ideal),
- the affected version or commit SHA.

You'll get an acknowledgement within a few days and, where a fix is warranted, a
remediation timeline.

## Scope

**In scope:** the mock server binary, protocol adapters, the engine,
tenancy/auth middleware, and the storage layer.

**Out of scope:** the documentation site (`site/`), example agent YAML, and the
behaviour of the real upstream providers (MockAgents emulates the wire protocol,
not the models). Note that MockAgents is a **test/development tool**: it serves
deterministic canned responses and is not intended to be exposed to untrusted
traffic on a public network.
