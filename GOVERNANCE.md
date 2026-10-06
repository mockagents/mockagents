# Governance

MockAgents is a maintainer-led open-source project under the
[Apache License 2.0](LICENSE). This document says who decides what, and how
that changes as the project grows.

## Roles

- **Users** run MockAgents and report what breaks. A bug report or a docs
  correction is a contribution.
- **Contributors** open issues, discussions and pull requests. Every
  contribution is accepted under the project licence with a
  [Developer Certificate of Origin](CONTRIBUTING.md#developer-certificate-of-origin)
  sign-off.
- **Maintainers** ([MAINTAINERS.md](MAINTAINERS.md)) review and merge pull
  requests, triage issues, cut releases and respond to security reports. They
  are the code owners in [.github/CODEOWNERS](.github/CODEOWNERS).

## How decisions are made

Most decisions are made in pull requests by **lazy consensus**: a change that a
maintainer approves and nobody objects to within a reasonable review window is
merged.

Some changes need an explicit discussion first, opened as a GitHub Discussion
or an issue and linked from the pull request:

- changes to the agent definition format (`internal/types`, `schema/`), which
  ripple through every SDK, the GUI and users' files;
- removing or changing a public API, CLI flag or environment variable;
- new runtime dependencies, and anything that would require cgo;
- changes to this document, the licence, or the security policy.

When maintainers disagree and discussion does not converge, the maintainers
decide by simple majority. While there is a single maintainer, that maintainer
decides and records the reasoning in the discussion.

## Releases

Maintainers cut releases following [docs/RELEASING.md](docs/RELEASING.md). Only
maintainers may push `v*` tags or approve the publishing environments.

## Security

Vulnerabilities are handled privately as described in [SECURITY.md](SECURITY.md).
Maintainers form the security response team.

## Code of conduct

Everyone in project spaces follows the [Code of Conduct](CODE_OF_CONDUCT.md).
Maintainers enforce it and may remove comments, close threads, or ban
participants who violate it.

## Becoming a maintainer

A contributor with a track record of quality contributions and reviews in an
area can be nominated by any maintainer (or can ask). Nominations are discussed
openly; with no objection from an existing maintainer within two weeks, the
nominee is added to [MAINTAINERS.md](MAINTAINERS.md) and CODEOWNERS for that area.

## Stepping down

Maintainers can step down at any time by opening a pull request that moves them
to the Emeritus section of MAINTAINERS.md. A maintainer inactive for six months
may be moved to Emeritus after the others have tried to reach them.
