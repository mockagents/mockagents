# Release remediation implementation evidence

This register records implementation evidence on
`codex/release-readiness-remediation`. A code or documentation change alone
does not close a release gate. The final publication step must bind these
results to the pushed commit and retain the hosted workflow URLs.

| Work | Findings | Status | Implemented evidence | Verified so far | Required external evidence |
| --- | --- | --- | --- | --- | --- |
| R01 | SR-05, CL-05 | implemented; local gate passed | `5847764`, `ed68f6f`, `a696953`, `da60572`, `22dfc9d` | Go 1.26.6 vet/tests and `govulncheck` v1.3.0 pass with no called vulnerabilities; OCI revision and running digest are bound to the candidate | retain hosted scan and registry digest URLs |
| R02–R04 | SR-01, SR-03, SR-04 | implemented; external gate passed | `1c7d3d4`, `bccee94`, `a7c1a6b` | Linux CGO race and PostgreSQL 16 tenancy/security conformance pass | retain hosted race result |
| R05–R09 | CL-01–03, AR-01–08 | implemented; regression passed | `a69aa59`, `0da5b05`, `f80864a` | all Go packages, vet, driftcheck, liquidcheck, build/validate, and the 22-check exact-image homelab regression pass | retain hosted verification URL |
| R10–R13 | DS-01–04, DS-08, DS-12 | implemented; external gate passed | `4f9dd20`, `5ec0147` | Python 148, examples 140, TypeScript 77, Vitest real-server 12, npx 5, stubborn-child cleanup, and packed-consumer gates pass | retain hosted package jobs |
| R14 | SR-02, DS-07, DS-09 | implemented; deployment gate passed with documented topology limit | `cc87377`, `4c3926f`, `94f7e31`, `22c5737`, `36e57fb` | Helm matrix, authenticated metrics, PVC restart persistence, rolling update, rollback/forward restore, and exact-image homelab regression pass; k3s `local-path` cross-node drain is explicitly unsupported and now warned/documented | use and test shared storage when cross-node rescheduling is a release requirement; repair the pre-existing homelab Prometheus volume before target-discovery validation |
| R15–R16 | DS-05, DS-06, DS-10, DS-11 | implemented; local publication simulations passed | `a696953`, `da60572`, `22dfc9d`, `088e3d1`, `b8d25ce`, `0c20b06`, `ba49aa4` | preflight 6/6, install aggregation 13/13, candidate tamper/identity, immutable retry, publication resume, release graph, and packed consumer pass | hosted workflow and public-channel install evidence after push |
| R17 | CL-04 | implemented; documentation QA passed | `685a1ef`, `9ec171c`, `22dfc9d`; tracked `AGENTS.md`, reconciled runbooks and component guides, `tools/doccheck` | doccheck, driftcheck, liquidcheck, Markdown path checks, GUI typecheck/build, and independent documentation review pass | retain hosted docs/site result |
| R18 | all | final local gate passed; hosted gate pending push | independent code, SDK, security, platform, homelab, storage-topology, documentation, and release reviewers | Windows and Linux suites, PostgreSQL, container/Helm, exact-image homelab deploy/regression, persistence, rollout, and rollback evidence pass; the tracked tree contains no credentials | push the single integrated history, require hosted checks, then merge/record the immutable main SHA; retain the generated homelab credential only in its ignored owner-only file |

The candidate SHA is taken from `git rev-parse HEAD` immediately before each
external run and is also encoded in the immutable image tag and OCI revision.
Do not reuse a result after runtime-affecting changes. Record command/tool
versions, result artifacts, owner, independent reviewer, and hosted URLs in the
release run.
