# Release remediation implementation evidence

This register records implementation evidence on
`codex/release-readiness-remediation`. A code or documentation change alone
does not close a release gate. Linux race, real PostgreSQL, packaged-consumer,
container/Helm, homelab, and exact-candidate checks remain open until their
artifacts are attached to the final candidate SHA.

| Work | Findings | Status | Implemented evidence | Verified so far | Required external evidence |
| --- | --- | --- | --- | --- | --- |
| R01 | SR-05, CL-05 | implemented; candidate gate open | `5847764`, `ed68f6f`, `a696953`, `da60572`, `22dfc9d` | full Go tests/vet and source `govulncheck` pass on Go 1.26.6; candidate workflow records binary toolchain and scan | execute binary/image scans on the exact candidate and record registry digests |
| R02–R04 | SR-01, SR-03, SR-04 | implemented; candidate gates open | `1c7d3d4`, `bccee94`, `a7c1a6b` | tenancy, recording, and server focused tests reported passing | PostgreSQL conformance and Linux race |
| R05–R09 | CL-01–03, AR-01–08 | implemented; candidate gates open | `a69aa59`, `0da5b05`, `f80864a` | focused packages, full `go test ./...`, `go vet ./...`, driftcheck and liquidcheck pass | Linux race and exact-candidate regression |
| R10–R13 | DS-01–04, DS-08, DS-12 | implemented; candidate gates open | `4f9dd20`, `5ec0147` | Go/Python/TypeScript/npx/Vitest suites and builds reported passing | Linux process/race and clean packed consumer |
| R14 | SR-02, DS-07, DS-09 | implemented; candidate gates open | `cc87377`, `4c3926f` | full server suite and authorization regressions pass; independent static Helm/topology review passed after bypass fixes | Helm matrix, live authenticated scrape, homelab topology |
| R15–R16 | DS-05, DS-06, DS-10, DS-11 | implemented; candidate gates open | `a696953`, `da60572`, `22dfc9d`, `088e3d1`, `b8d25ce`, `0c20b06` | preflight 6/6, install aggregation 13/13, packed consumer, candidate tamper/identity, immutable image retry and release-graph tests pass | execute hosted workflow, disposable-registry retry matrix, and exact public-channel installs |
| R17 | CL-04 | implemented; documentation QA passed | `685a1ef`, `9ec171c`, `22dfc9d`; tracked `AGENTS.md`, reconciled runbooks and component guides, `tools/doccheck` | doccheck unit test, driftcheck, liquidcheck, and local paths in all 119 tracked Markdown files pass | repeat documentation/site build on the exact candidate |
| R18 | all | open | independent code, SDK, security, platform, documentation and release reviews completed on the integration branch | Windows Go/Python/JavaScript/GUI suites and builds pass; source vulnerability scan finds no called vulnerabilities | exact-SHA Linux race, PostgreSQL, Playwright, Docker/Helm, homelab deploy/regression/rollback, hosted checks and merge evidence |

Do not replace an open cell with “passed” from a different commit. Record the
candidate SHA, command and tool versions, result artifact, owner, and independent
reviewer when each external gate runs.
