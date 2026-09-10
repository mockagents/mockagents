# Release remediation implementation evidence

This register records implementation evidence on
`codex/release-readiness-remediation`. A code or documentation change alone
does not close a release gate. Linux race, real PostgreSQL, packaged-consumer,
container/Helm, homelab, and exact-candidate checks remain open until their
artifacts are attached to the final candidate SHA.

| Work | Findings | Status | Implemented evidence | Verified so far | Required external evidence |
| --- | --- | --- | --- | --- | --- |
| R01 | SR-05, CL-05 | implemented; candidate gate open | `5847764`, `ed68f6f` | focused config-doc scan and local Go tests reported passing | binary/image vulnerability scan and exact candidate toolchain |
| R02–R04 | SR-01, SR-03, SR-04 | implemented; candidate gates open | `1c7d3d4`, `bccee94`, `a7c1a6b` | tenancy, recording, and server focused tests reported passing | PostgreSQL conformance and Linux race |
| R05–R09 | CL-01–03, AR-01–08 | implemented; candidate gates open | `a69aa59`, `0da5b05` | focused packages and full `go test ./...` reported passing | Linux race and final candidate regression |
| R10–R13 | DS-01–04, DS-08, DS-12 | implemented; candidate gates open | `4f9dd20`, `5ec0147` | Go/Python/TypeScript/npx/Vitest suites and builds reported passing | Linux process/race and clean packed consumer |
| R14 | SR-02, DS-07, DS-09 | implemented; candidate gates open | `cc87377` | focused server authorization and Helm render tests reported passing | Helm matrix, live authenticated scrape, homelab topology |
| R15–R16 | DS-05, DS-06, DS-10, DS-11 | in progress | working-tree candidate workflow/preflight changes | no closure claim | deliberate gate failures, exact-channel install, retained JUnit |
| R17 | CL-04 | implemented; documentation QA passed with follow-up fixes in working tree | `685a1ef`; tracked `AGENTS.md`, reconciled runbooks and component guides, `tools/doccheck` | doccheck unit test, driftcheck, liquidcheck, and local paths in all 119 tracked Markdown files pass | commit the QA follow-up and repeat on the final candidate |
| R18 | all | open | none | none | exact-SHA full regression, independent re-audit, homelab deployment and rollback evidence |

Do not replace an open cell with “passed” from a different commit. Record the
candidate SHA, command and tool versions, result artifact, owner, and independent
reviewer when each external gate runs.
