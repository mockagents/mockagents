# Delivery, SDK and GUI release audit — 2026-09-10

{% raw %}

## Scope and method

Reviewed local main at `6ddb03e54a14484e5929a19673f0cfd8a1975f07` using delivery, runtime-SDK, public-contract and GUI personas. Pass 1 inspected release/build manifests, workflows, Helm templates, SDK subprocess/download/client code and GUI request/auth/proxy/test code. Pass 2 traced package-to-release-binary resolution, tag-to-registry publication, chart-to-server authentication/state, and subprocess-to-test-runner lifecycles. No product patches were applied. This is a bounded sub-audit, not a claim that every line of every SDK/GUI component was exhaustively reviewed.

## Surface inventory

| Surface | Implementation and commands | Material boundaries |
| --- | --- | --- |
| Binary | Go module, `go 1.26.1`, toolchain `go1.26.4`; `make build`, `go test ./...`, `go vet ./...` | Pure-Go SQLite, process lifecycle, cross-platform builds |
| Release | `.goreleaser.yml`, `.github/workflows/release.yml` | Linux/macOS amd64+arm64, Windows amd64, GitHub releases/checksums, Homebrew cask, Docker Hub/GHCR, npm, PyPI |
| Container | `Dockerfile`, `docker-compose.yml`; `docker build`, `docker compose up` | Non-root user; writable `/data`; Go builder and Alpine runtime; only Go server packaged, GUI separate |
| Kubernetes | `deploy/helm/mockagents`, chart 0.5.1/app 0.5.0; `helm lint`, `helm template` | Read-only agent ConfigMap, `/data` emptyDir or PVC, envFrom secrets, ingress/HPA/PDB/NetworkPolicy/ServiceMonitor |
| Python | `sdk/python`, setuptools, requests, PyYAML; `python -m pytest tests`, `python -m build` | Python >=3.10; binary cache and bootstrap; pytest process/env fixture; HTTP/SSE/MCP clients |
| TypeScript | `sdk/typescript`, ESM/native fetch; `npm ci`, `npm test`, `npm run typecheck`, `npm run build` | Node >=18 declared; binary process management, SSE cancellation, injectable fetch |
| Test helper | `sdk/vitest`, Vitest and Jest lifecycle bindings | Global provider environment changes, peer dependency, cleanup after startup failure |
| npx | `sdk/npx`, CommonJS, no runtime npm dependencies; `node --test` | HTTPS binary download, fail-closed SHA256 check, external tar extraction, shared user cache |
| Go SDK | `sdk/go/mockagents` within root Go module | HTTP/in-process/subprocess modes, process output concurrency, custom HTTPClient auth possible |
| GUI | `gui`, Next.js 16.3.4/React 19, TypeScript | Server actions, HttpOnly bearer credential cookie, SSR fetch, log proxy and live SSE, interaction payload privacy |
| GUI gates | `npm run typecheck`, `npm run build`, `npm test`, `npm run test:e2e` | Vitest components/accessibility and Playwright against real Go server; fixture copies prevent editing repository examples |
| Documentation | `docs/RELEASING.md`, chart README, SDK READMEs, GUI README, root Makefile | Agent onboarding should distinguish locally available tools, unit tests, real backend tests, and externally published install paths |

Strengths: checksum download verification fails closed; binary artifacts exclude arbitrary archive extraction in Python; main CI includes Go race tests on Linux, Windows tests, Postgres conformance, fixture tests, GUI production build, component/browser gates, Docker smoke, Helm rendering and govulncheck. GUI management calls have deadlines and auth cookies are HttpOnly/Secure in production. Main release action dependencies are predominantly SHA-pinned. These controls do not eliminate the findings below.

## Findings

| ID | Severity | Evidence | Defect and effect | Release gate |
| --- | --- | --- | --- | --- |
| DS-01 | High | `sdk/go/mockagents/server.go:73`, `:111`, `:112`, `:228` | stdout/stderr have different teeWriter mutexes but the same strings.Builder; Logs reads under a third mutex. Concurrent writes or a read during subprocess logging race and can corrupt/lossily capture output. | Required for Go subprocess SDK |
| DS-02 | High | `sdk/python/mockagents/server.py:141-151`, `:153-193`, `:223-225`; `pytest_plugin.py:97-102` | Both child output streams are PIPEs with no reader until stop. A verbose child can block when its pipe fills. A readiness exception leaves the child live; context-manager entry and fixture startup fail before their finally/exit cleanup is registered. | Required for Python subprocess SDK |
| DS-03 | High | `sdk/typescript/src/server.ts:108-123` | stop sends SIGTERM, then tests `!proc.killed` before SIGKILL. `killed` reports successful signal delivery, not exit. On Unix a child ignoring/delaying SIGTERM survives, while the SDK forgets it and reports stopped. Successful stop also leaves a referenced timeout alive. | Required for TypeScript/test helper |
| DS-04 | High | `sdk/vitest/package.json:46-48`; `sdk/typescript/package.json:3`; `sdk/vitest/package-lock.json:21-23` | Helper 0.5.0 requires SDK `^0.4.0`, which excludes the simultaneously released SDK 0.5.0. Clean npm consumers using both current packages have an incompatible peer dependency; the dev `file:` dependency hides it in the repo. | Required for npm publication |
| DS-05 | High | `.github/workflows/release.yml:15-29`, `:33`, `:63`, `:119`, `:162`, `:193-206`; `.github/workflows/ci.yml:105-132`, `:257-325` | Tag publication only depends on Go tests. It neither runs nor requires the same-SHA Python, GUI, Helm, vulnerability and packaging gates. Main CI has no TypeScript SDK, Vitest helper or npx unit-test jobs. npm publication re-resolves via npm install. Binary-dependent launchers can publish before binaries, or even when binary publishing fails. | Required before next release |
| DS-06 | High | `.github/workflows/release.yml:3-6`, `:79-85`; `.goreleaser.yml:47-48` | Every v* tag publishes Docker `latest`, including semver prereleases. GoReleaser marks prereleases automatically, but Docker's unconditional raw latest tag promotes that prerelease to stable container users. | Required if prerelease tags are accepted |
| DS-07 | Medium | `deploy/helm/mockagents/templates/servicemonitor.yaml:26-39`; `values.yaml:276-280`; `internal/server/route_authz.go:108` | Values documentation requires a bearer token in multi-tenant mode, but no authentication value is rendered into the ServiceMonitor endpoint. Enabling the chart monitor gives unauthenticated scrapes that /metrics refuses. | Required for advertised multi-tenant monitoring |
| DS-08 | Medium | `sdk/npx/lib/binary.js:52-57`, `:131-151`, `:156-159`; `sdk/python/mockagents/_binary.py:156-158`, `:195-198`, `:247-261`, `:306-313` | Python and npx share a single unversioned executable cache. ensureBinary/ensure_binary returns any cached binary before considering requested version. Pinning/upgrading/downgrading the launcher can silently run another release. Extraction writes final paths directly, so interrupted/concurrent installs can expose an incomplete binary. | Required for reproducible bootstrap |
| DS-09 | Medium | `deploy/helm/mockagents/templates/deployment.yaml:15-16`; `README.md:69-75`, `:144-146` | Replica guard tests presence of TENANCY_DSN, not its value/backend, so even an empty value bypasses it. A real Postgres tenancy DSN also bypasses acknowledgment although registry, sessions, logs and rate buckets remain per pod. Secret-based DSNs cannot satisfy the intended guard. | Required before claiming shared multi-replica state |
| DS-10 | Medium | `.github/workflows/install-paths.yml:84-117`, `:148-168`; `scripts/install-paths-report.sh:27-31`, `:47-80` | Post-release checks use latest rather than triggering release version, npm SDK checks only npm view, and the aggregator validates only rows received. A missing macOS artifact plus passing Linux rows can pass the Report job without checking Homebrew. Stale artifacts can also appear healthy after a newer release fails. | Required for reliable release verification |
| DS-11 | Medium | `deploy/actions/mockagents-test/action.yml:109-115` | On a failing test suite, `set -e` exits before exporting junit-report. Exactly when a downstream `if: always()` reporter needs the generated failing report, the documented output is absent. Self-test only covers the green suite. | Required for documented failure-report contract |
| DS-12 | Low | `sdk/python/pyproject.toml:9`; filesystem `sdk/python/README.md` absent | Python metadata points to a README that is not present. Package build metadata/documentation cannot provide the specified long description; build backend behavior must be validated rather than assumed to fail. | Packaging cleanup |

No Critical issue was established in this bounded audit. No CVE is inferred from a dependency version. Live registry/package vulnerability and cluster state were not verified here.

## Concrete patches and required regression tests

### DS-01: one synchronized log sink

Replace the builder plus unrelated locks with a single reusable synchronized sink, assigned identically to both streams. Reset it at Start, and let Logs call its synchronized String method. Keep process lifecycle mutex separate to avoid blocking writers while Stop waits for the child.

```go
type logBuffer struct {
    mu sync.Mutex
    buf strings.Builder
}
func (b *logBuffer) Write(p []byte) (int, error) {
    b.mu.Lock(); defer b.mu.Unlock()
    return b.buf.Write(p)
}
func (b *logBuffer) String() string {
    b.mu.Lock(); defer b.mu.Unlock()
    return b.buf.String()
}
// Server.logs becomes logBuffer.
cmd.Stdout, cmd.Stderr = &s.logs, &s.logs
```

Add `server_test.go` regression that concurrently writes stdout/stderr while repeatedly reading Logs, then asserts both streams' complete line counts. Run with `go test -race ./sdk/go/mockagents -run 'Server|Log'` on Linux. Also cover spontaneous child exit (IsRunning currently relies on ProcessState updated only by Wait), restart log reset and cleanup after startup failure. Document bounded capture or implement a configurable ring buffer; unbounded logs in long suites should not grow without limit.

### DS-02: drain Python pipes while the child lives; own cleanup on start failure

```python
try:
    self._wait_for_ready(timeout)
except BaseException:
    self.stop()
    raise
```

This try/except belongs immediately after successful Popen. Separately replace PIPE-with-delayed-communicate with two reader threads started immediately, or a temporary log file that cannot backpressure the child. If using threads, stop must signal, wait/kill/reap, join both readers, and close streams; do not concurrently call communicate while threads also read the same pipes. Bound retained log bytes and retain the tail in startup exceptions. Tests: child emits >1 MiB on both streams and still serves; child never becomes ready and leaves no PID after timeout; failing `with MockAgentServer(...)` and failing pytest fixture clean up; normal stop remains bounded. Update Python subprocess lifecycle docs and test fixture contract.

### DS-03: distinguish signal delivery from process exit

Implement one exit/close promise registered before signaling; return immediately for an already-reaped child; clear all timeout handles in finally. On graceful deadline check `exitCode === null && signalCode === null`, send SIGKILL and wait for actual exit under a second bounded deadline. Only mark stopped after exit, or surface an explicit failure retaining the handle for retry.

```ts
// The escalation condition must use process completion, never proc.killed.
if (proc.exitCode === null && proc.signalCode === null) {
  proc.kill("SIGKILL");
}
```

Add mocked fake-child test and a Linux child that ignores SIGTERM; assert SIGKILL, OS process reaped, no outstanding referenced timer. Cover already-exited child, spawn error and two concurrent stops. [Node's child-process documentation](https://nodejs.org/api/child_process.html#subprocesskilled) explicitly distinguishes successful signal delivery from termination. Windows kill behavior differs; the stubborn graceful-child reproduction belongs on Linux, not an assertion of Unix signals on Windows.

### DS-04: align published peer ranges and test packed packages

Change `sdk/vitest/package.json` peer to `"@mockagents/sdk": "^0.5.0"` (or explicitly `"^0.4.0 || ^0.5.0"` only if both are tested), regenerate its lockfile. Build SDK and helper, `npm pack` each, install both tarballs into an empty temp project with `--strict-peer-deps`, and import both public entry points. Exercise the Jest entry too. This is a package-consumer test; a local `file:` development link is insufficient.

### DS-05: share a full release gate and stage publication

Add `workflow_call` to CI or extract `.github/workflows/verify.yml`; run it from both PR/main and tag workflows against the caller checkout. Add explicit JS SDK/helper/npx test jobs, installing/building the SDK before the helper. Run package version/peer/tag checks before any mutation. Replace both release `npm install` commands with `npm ci --no-audit --no-fund`.

```yaml
# release.yml — conceptual job graph; preserve existing pinned actions.
jobs:
  verify:
    uses: ./.github/workflows/verify.yml
  prepare-artifacts:
    needs: verify
    # Build every package/image/archive and verify packed/wheel contents.
  release-binaries:
    needs: prepare-artifacts
    # Publish verified artifacts, then download+verify all target assets.
  release-python:
    needs: [prepare-artifacts, release-binaries]
  release-npm:
    needs: [prepare-artifacts, release-binaries]
```

The ellipses/comments are implementation instructions, not a complete drop-in workflow. For a complete implementation retain current runner/setup/publish jobs, introduce the dependencies above and an artifact manifest keyed by tag and SHA. Registry publishes are not transactional: document resume/retry for already-published versions, and only announce completion after every required registry passes exact-version smoke. Add concurrency per tag and an explicit stable/prerelease policy. Test negative gates by a failing SDK test, mismatched peer/tag and missing release asset in a nonpublishing workflow. Include GUI, chart, package installation, dependency scan and API drift results in the release manifest.

### DS-06: separate stable and prerelease channel updates

Compute a semver-validated `stable` output before Docker metadata (`v1.2.3` true, `v1.2.3-rc.1` false, malformed tag rejected). Change raw latest metadata tag to `type=raw,value=latest,enable=${{ steps.version.outputs.stable }}`. Apply the same stable policy to npm dist-tags (`next` for prereleases) and any Homebrew stable cask. Golden-test metadata for stable and rc tags; assert rc output contains neither `latest` nor stable major/minor channel updates. Document the commands and expected channels in RELEASING.md.

### DS-07: authenticated ServiceMonitor

Add `serviceMonitor.authorization: {}` to values and render it inside `spec.endpoints[0]`:

```yaml
      {{- with .Values.serviceMonitor.authorization }}
      authorization:
        {{- toYaml . | nindent 8 }}
      {{- end }}
```

Document values example `{ type: Bearer, credentials: { name: mockagents-metrics, key: token } }`, dedicated platform scrape credential rotation, and the requirement that the Secret be in the ServiceMonitor namespace. Coordinate with the security audit's SR-02: current viewer access exposes global tenant labels, so that fix raises /metrics to RolePlatform; do not deploy this chart fix with a tenant viewer key. Replace misleading bearerTokenSecret prose unless backward compatibility support is also added. [Prometheus Operator API](https://prometheus-operator.dev/docs/api-reference/api/) supports endpoint authorization with a Secret selector. Test Helm render with/without authorization and an integration scrape returning 200 with the dedicated platform key, 401 absent token, and 403 for a tenant viewer. Correct comments claiming CRD absence silently skips the template: the current template only checks enabled.

### DS-08: versioned, atomically populated caches

Use `<cache>/mockagents/<version>/<os>-<arch>/<binary>` in both launchers; pass desired package/explicit version into cache lookup, excluding the legacy unversioned cache unless its binary version is verified. Keep explicit user binary override supported and document it as an override. Download/extract to a unique sibling temp directory, verify archive checksum and executable, then atomically rename into the completed cache slot; use a lock or atomic publish protocol for concurrent installers. Tests: request B with A cached; two simultaneous installs; interrupted extraction; old cache migration; Python/npx cache interoperability; checksum mismatch does not publish a completed cache entry.

### DS-09: explicit multi-replica topology contract

Remove `hasKey ...TENANCY_DSN` as an automatic acknowledgment bypass. Require `multiReplica.acknowledged=true` for every static/HPA replica count >1 while state remains per pod; separately validate supported tenancy topology for multi-tenant mode. Include empty DSN and DSN supplied through Secret in tests. State precisely that shared tenant/key storage does not share agent edits, sessions, interaction/audit histories or per-pod rate limits. For stateful conversational workloads default to one active pod and test rolling-update overlap; a future shared session/registry design is required before advertising transparent HA. Add values.schema.json to validate types and conflicting settings.

### DS-10: exact-version, complete install-path verification

Pass release tag/SHA from the triggering Release workflow into checks; on scheduled runs resolve the intended latest stable tag once and reuse it everywhere. Check binary `--version`, both image tags, npm tarball imports, wheel import/version, and launcher binary version against that value. Keep pending paths an explicit pre-release exception, not evidence of public availability. Define the expected path-ID set centrally and reject missing, duplicate or invalid result rows. Also fail Report if either prerequisite job's result is not success. Add shell fixtures for only-linux, only-macos, duplicate rows, unknown status and stale version. A missing results file for one platform must not become a green partial report.

### DS-11: publish the report output before running a potentially failing suite

```bash
mkdir -p "$(dirname "$REPORT")"
echo "report=$REPORT" >> "$GITHUB_OUTPUT"
mockagents "${args[@]}" > "$REPORT"
```

Preserve the failing exit code. If failed execution can produce no valid XML, expose a separate report-generated output after validating content while still retaining failure status. Add self-test running an intentionally failing suite with `continue-on-error: true`, then assert action outcome failure, output path nonempty, XML present and failure count nonzero. README downstream example must use `if: always()`.

### DS-12: ship package documentation

Add `sdk/python/README.md` with pip installation, binary bootstrap/offline override, basic client/context manager, pytest fixture, supported Python range and link to API docs. Build sdist and wheel and inspect wheel METADATA for nonempty Description and correct version/license. Add `python -m twine check dist/*` or equivalent metadata check to prepare-artifacts.

## Validation evidence

| Check | Observed result |
| --- | --- |
| `sdk/typescript`: `npm test` | 8 test files, 72 tests passed |
| `sdk/typescript`: `npm run build` | Passed |
| `sdk/npx`: `npm test` | 3 tests passed; tests do not cover downloader/version cache |
| `gui`: `npm test` | 17 test files, 329 tests passed |
| `gui`: `npm run typecheck` | Passed |
| `gui`: `npm run build` | Next.js production build passed |
| `gui`: `npm run test:e2e` | API-only tests ran successfully; browser tests blocked with `browserType.launch: spawn EPERM` in generated error-context files; interrupted the run after repeated failures and stalled teardown, so no complete suite verdict; not evidence of a GUI defect |
| `sdk/python`: bundled Python `-m pytest tests -q` | Not run: bundled runtime lacks pytest; also requests absent |
| `sdk/vitest`: `npm test` | Cannot run: helper dependencies absent |
| helper `npm ci --ignore-scripts --offline` | Environment blocked: npm cache stat EPERM |
| Helm/Docker | Tools absent from PATH; render/cluster/container validation still required |
| Semver peer reproduction | npm semver.satisfies(`0.5.0`, `^0.4.0`) returned false |
| TypeScript stop fake-child reproduction | After stop(10), signals `[SIGTERM]`, child exitCode null, server.isRunning false; SIGKILL never sent |

The fake-child reproduction imports the built `sdk/typescript/dist/server.js` and injects an EventEmitter child whose kill records successful signal delivery without emitting exit. It deterministically exercises the same branch as a Unix child that ignores SIGTERM. Go concurrency and Python process findings are source-verified, not represented as race-detector/process stress tests run on this machine.

## Execution plan and release acceptance

1. **Immediate blockers:** SDK owners fix DS-01/02/03/04 with targeted process/race/packed-package regression tests. Release owner implements DS-05/06 and runs every gate on the intended release SHA. Do not publish current SDK/helper combination or promote prereleases to stable.
2. **Reliability and contracts:** Platform owner fixes DS-07/09 and tests an authenticated scrape and chart topology negatives. SDK owner fixes DS-08 atomic versioned cache. Delivery owner fixes DS-10/11, including intentionally failing action self-tests. Docs owner ships DS-12 and updates release/SDK/chart runbooks alongside fixes.
3. **Maintainability:** Add Dependabot coverage for `sdk/vitest` and Docker images; update all remaining floating workflow/composite action refs to reviewed SHAs. Pin release tooling versions and capture SBOM/provenance for artifacts. Establish Node minimum/current-LTS and Python declared-range matrices, packed-consumer tests, binary process lifecycle stress tests, and published protocol contract parity checks. Document one command per independent verification surface in the repo agent instructions.

Release acceptance requires: no known High finding open; all package tarballs/wheels install and import in clean environments with compatible peers; the exact release tag/SHA passes Go race/Windows, Python, JS SDK/helper/npx, GUI production/browser, schema, Helm/Docker and dependency gates; launchers select the requested binary version and reap failures; stable channels exclude prereleases; every claimed registry is verified at that version; monitoring authenticates in multi-tenant mode. Exception signoff must state the specific unsupported feature/channel, not simply mark a skipped job green.

**Bounded verdict: Not ready** for the complete advertised SDK/distribution release. The GUI build/component gates are healthy in this environment, but that does not compensate for process lifecycle, peer-installation and publication-gate defects.

{% endraw %}
